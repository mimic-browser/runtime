//go:build (windows || linux) && amd64

package gov8

import (
	"fmt"
	"runtime"
	"sync"
	"unsafe"
)

// Startup data and snapshot creation, mirroring the pinned crate's
// SnapshotCreator / StartupData / snapshot-consumption surface:
//
// - NewSnapshotCreator / NewSnapshotCreatorFromExistingSnapshot build a
//   creator isolate (the isolate is engine-entered and owned by the C++
//   SnapshotCreator; it is consumed by CreateBlob exactly like the pinned
//   OwnedIsolate::create_blob, or released by SnapshotCreator.Close).
// - SetDefaultContext / AddContext / AddIsolateData / AddContextData mirror
//   the scope-level creator methods; AddContext returns indices in insertion
//   order starting at 0 (the engine reserves slot 0 for the default
//   context, so added-context indices are independent of it).
// - NewIsolateFromSnapshot mirrors CreateParams::snapshot_blob via
//   Isolate::new; recovered added contexts come through
//   (*Scope).ContextFromSnapshot and the exactly-once data retrieval
//   through (*Scope).GetIsolateDataFromSnapshotOnce /
//   GetContextDataFromSnapshotOnce with NoData/BadType outcomes.
//
// Intentional deviation (documented, fatal-call guard): in the pinned crate
// StartupData::is_valid on data shorter than the snapshot version header
// trips a V8 CHECK and aborts the process (characterized upstream as
// mode=invalid-startup-data-fatal). This wrapper answers `invalid` without
// touching the engine for such data, and every consumer entry point
// validates a blob before any fatal engine call. Everything else matches
// observable engine behavior, including the upstream caveat that a wrongly
// typed (BadType) retrieval consumes the exactly-once slot.

// FunctionCodeHandling mirrors v8::SnapshotCreator::FunctionCodeHandling.
type FunctionCodeHandling int32

const (
	// FunctionCodeClear drops compiled function code from the snapshot.
	FunctionCodeClear FunctionCodeHandling = 0
	// FunctionCodeKeep keeps compiled function code in the snapshot.
	FunctionCodeKeep FunctionCodeHandling = 1
)

// minValidStartupSize is the smallest blob the engine's version check can
// examine: Snapshot::VersionIsValid CHECKs
// raw_size > kVersionStringOffset(16) + kVersionStringLength(64) and aborts
// otherwise. Data at or below this bound is reported invalid locally.
const minValidStartupSize = 81

// StartupData holds a snapshot blob. Blobs produced by CreateBlob carry a
// Go-owned copy of the engine bytes. Blobs used for isolate creation keep
// engine-owned copies alive until Release: the engine reads the original
// blob bytes on every Context::New of a snapshot-backed isolate, so the
// copies must outlive every isolate created from the blob.
type StartupData struct {
	mu             sync.Mutex
	bytes          []byte
	refs           map[*Isolate]uintptr // isolate -> engine blob copy (consumers)
	released       bool
	pendingCreates int
	// Set for blobs produced by a creator whose effective external-reference
	// table was non-empty. Such blobs must not reach V8 without a table.
	requiresExternalReferences bool
	// False for StartupDataFromBytes: V8 exposes no safe query for whether an
	// arbitrary serialized blob contains external references.
	externalReferencesKnown bool
}

// StartupDataFromBytes wraps raw snapshot bytes (the analog of the pinned
// crate's StartupData::from(Vec<u8>), e.g. a blob loaded from a file). The
// bytes are used as-is; validity is checked at use. Because a raw blob does
// not reveal whether it contains external references, consume it through
// NewIsolateFromSnapshotWithParams with an explicit table; the table may be
// empty for a blob known by the caller not to contain external references.
func StartupDataFromBytes(b []byte) *StartupData {
	if len(b) == 0 {
		return &StartupData{}
	}
	c := make([]byte, len(b))
	copy(c, b)
	return &StartupData{bytes: c}
}

// IsEmpty reports whether the blob carries no bytes.
func (s *StartupData) IsEmpty() bool {
	return s == nil || len(s.bytes) == 0
}

// Bytes returns the blob contents. The caller must not mutate the result.
func (s *StartupData) Bytes() []byte {
	if s == nil {
		return nil
	}
	return s.bytes
}

// IsValid reports whether the blob carries a version header matching this
// engine. Unlike the pinned crate, data shorter than the snapshot version
// header is answered locally (false) instead of tripping a fatal V8 CHECK.
func (s *StartupData) IsValid() bool {
	return s != nil && startupBytesValid(s.bytes)
}

// CanBeRehashed reports whether V8 can rehash the startup blob when loading
// it. The pinned crate documents this query for blobs returned by
// SnapshotCreator.CreateBlob. Invalid and truncated embedder-provided data is
// answered locally as false because passing it to V8 can trip a fatal CHECK.
func (s *StartupData) CanBeRehashed() bool {
	if s == nil || len(s.bytes) < minValidStartupSize {
		return false
	}
	var out int32
	r1, _, _ := proc("gov8_snapshot_blob_can_be_rehashed").Call(
		uintptr(unsafe.Pointer(&s.bytes[0])), uintptr(len(s.bytes)),
		uintptr(unsafe.Pointer(&out)))
	if int64(r1) < 0 {
		return false
	}
	return out == 1
}

func startupBytesValid(b []byte) bool {
	if len(b) < minValidStartupSize {
		return false
	}
	var out int32
	r1, _, _ := proc("gov8_snapshot_blob_is_valid").Call(
		uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)),
		uintptr(unsafe.Pointer(&out)))
	if int64(r1) < 0 {
		return false
	}
	return out == 1
}

// validForCreation is the fatal-call guard for consumption paths: the blob
// must be present, longer than the version header, and version-valid.
func (s *StartupData) validForCreation() error {
	if s == nil || len(s.bytes) == 0 {
		return fmt.Errorf("gov8: startup blob is empty")
	}
	if len(s.bytes) < minValidStartupSize {
		return fmt.Errorf("gov8: startup blob is shorter than the snapshot version header (%d bytes); refusing a fatal engine call", len(s.bytes))
	}
	if !startupBytesValid(s.bytes) {
		return fmt.Errorf("gov8: startup blob is not valid for this engine (version mismatch)")
	}
	return nil
}

// trackRef registers a consumer isolate together with its engine blob copy.
func (s *StartupData) trackRef(i *Isolate, holder uintptr) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.refs == nil {
		s.refs = make(map[*Isolate]uintptr)
	}
	s.refs[i] = holder
}

// Release frees the engine-owned copies of the blob. It fails while any
// isolate created from the blob is still open: the engine reads the blob
// bytes for context creation until the isolate is disposed. Releasing only
// after the isolates are closed makes the failure mode of forgetting this
// call a bounded memory leak, never a use-after-free. It is safe to call
// twice (and on Go-only blobs produced by CreateBlob, which hold no engine
// memory).
func (s *StartupData) Release() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if s.released {
		s.mu.Unlock()
		return nil
	}
	if s.pendingCreates != 0 {
		s.mu.Unlock()
		return fmt.Errorf("gov8: startup blob is being consumed by %d isolate constructor(s)", s.pendingCreates)
	}
	var live int
	for i := range s.refs {
		if !isolateClosed(i) {
			live++
		}
	}
	if live > 0 {
		s.mu.Unlock()
		return fmt.Errorf("gov8: startup blob still in use by %d open isolate(s); close them before Release", live)
	}
	holders := make([]uintptr, 0, len(s.refs))
	for _, h := range s.refs {
		holders = append(holders, h)
	}
	s.refs = nil
	s.released = true
	s.mu.Unlock()
	for _, h := range holders {
		if err := callErr("StartupData.Release", proc("gov8_snapshot_blob_release"), h); err != nil {
			return err
		}
	}
	return nil
}

// isolateClosed reports whether the isolate wrapper has been closed. It is
// safe from any thread (the closed flag is mutex-guarded) and is the guard
// that keeps engine-dead wrappers from re-entering the engine.
func isolateClosed(i *Isolate) bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.closed
}

// SnapshotCreator wraps a creator isolate (v8::SnapshotCreator). The engine
// isolate is owned by the C++ SnapshotCreator: CreateBlob consumes it —
// exactly like the pinned OwnedIsolate::create_blob — and Close releases it
// without producing a blob (Go resolves the pinned crate's
// drop-without-create_blob panic into an explicit, safe operation; see
// Close). All methods must run on the creator's owning thread.
type SnapshotCreator struct {
	iso                        *Isolate
	wrap                       uintptr
	closed                     bool
	requiresExternalReferences bool
}

// NewSnapshotCreator creates a fresh creator isolate.
func NewSnapshotCreator() (*SnapshotCreator, error) {
	return newSnapshotCreator(nil)
}

// NewSnapshotCreatorFromExistingSnapshot creates a creator isolate seeded
// from an existing startup blob (snapshot-of-snapshot chains). The blob is
// validated before any engine call.
func NewSnapshotCreatorFromExistingSnapshot(blob *StartupData) (*SnapshotCreator, error) {
	if err := blob.validForCreation(); err != nil {
		return nil, err
	}
	if !blob.externalReferencesKnown || blob.requiresExternalReferences {
		return nil, fmt.Errorf("gov8: snapshot may require external references; use NewSnapshotCreatorFromExistingSnapshotWithExternalReferences")
	}
	return newSnapshotCreator(blob)
}

func newSnapshotCreator(blob *StartupData) (*SnapshotCreator, error) {
	if err := requireInitialized(); err != nil {
		return nil, err
	}
	runtime.LockOSThread()
	tid := currentThreadID()
	if err := beginIsolateCreate(); err != nil {
		runtime.UnlockOSThread()
		return nil, err
	}
	var blobPtr, blobLen uintptr
	if blob != nil {
		b := blob.bytes
		blobPtr = uintptr(unsafe.Pointer(&b[0]))
		blobLen = uintptr(len(b))
	}
	var wrap, isoH uintptr
	r1, _, _ := proc("gov8_snapshot_creator_new").Call(
		blobPtr, blobLen,
		uintptr(unsafe.Pointer(&wrap)), uintptr(unsafe.Pointer(&isoH)))
	if int64(r1) < 0 {
		abandonIsolateCreate()
		runtime.UnlockOSThread()
		return nil, shimError("SnapshotCreator.New", r1)
	}
	iso := &Isolate{handle: isoH, tid: tid}
	finishIsolateCreate(iso)
	return &SnapshotCreator{iso: iso, wrap: wrap}, nil
}

// Isolate returns the creator's isolate. It is usable like any isolate on
// the owning thread until CreateBlob or Close consumes it.
func (sc *SnapshotCreator) Isolate() *Isolate { return sc.iso }

func (sc *SnapshotCreator) check() error {
	if sc.closed {
		return fmt.Errorf("gov8: snapshot creator used after CreateBlob or Close")
	}
	return sc.iso.check()
}

// SetDefaultContext sets the default context included in the snapshot. It
// must be called at most once per creator (engine CHECK).
func (sc *SnapshotCreator) SetDefaultContext(c *Context) error {
	if err := sc.check(); err != nil {
		return err
	}
	if err := c.check(); err != nil {
		return err
	}
	if c.iso != sc.iso {
		return foreignIsolate("context")
	}
	return callErr("SnapshotCreator.SetDefaultContext",
		proc("gov8_snapshot_set_default_context"), sc.iso.handle, sc.wrap, c.handle)
}

// AddContext adds an additional context (with its global proxy) to the
// snapshot and returns its index. Indices are assigned in insertion order
// starting at 0.
func (sc *SnapshotCreator) AddContext(c *Context) (int, error) {
	if err := sc.check(); err != nil {
		return 0, err
	}
	if err := c.check(); err != nil {
		return 0, err
	}
	if c.iso != sc.iso {
		return 0, foreignIsolate("context")
	}
	var idx int64
	if err := callErr("SnapshotCreator.AddContext",
		proc("gov8_snapshot_add_context"), sc.iso.handle, sc.wrap, c.handle,
		uintptr(unsafe.Pointer(&idx))); err != nil {
		return 0, err
	}
	return int(idx), nil
}

// AddIsolateData attaches a scope-local value (Integer, String, ...) to the
// isolate snapshot and returns its index. The value's scope must be open.
func (sc *SnapshotCreator) AddIsolateData(v Value) (int, error) {
	if err := sc.check(); err != nil {
		return 0, err
	}
	if err := v.check(); err != nil {
		return 0, err
	}
	if v.iso != sc.iso {
		return 0, foreignIsolate("value")
	}
	var idx int64
	if err := callErr("SnapshotCreator.AddIsolateData",
		proc("gov8_snapshot_add_isolate_data"), sc.iso.handle, v.sc.handle,
		sc.wrap, v.h, uintptr(unsafe.Pointer(&idx))); err != nil {
		return 0, err
	}
	return int(idx), nil
}

// AddContextData attaches a scope-local value to a context's snapshot and
// returns its index.
func (sc *SnapshotCreator) AddContextData(c *Context, v Value) (int, error) {
	if err := sc.check(); err != nil {
		return 0, err
	}
	if err := c.check(); err != nil {
		return 0, err
	}
	if c.iso != sc.iso {
		return 0, foreignIsolate("context")
	}
	if err := v.check(); err != nil {
		return 0, err
	}
	if v.iso != sc.iso {
		return 0, foreignIsolate("value")
	}
	var idx int64
	if err := callErr("SnapshotCreator.AddContextData",
		proc("gov8_snapshot_add_context_data"), sc.iso.handle, v.sc.handle,
		sc.wrap, c.handle, v.h, uintptr(unsafe.Pointer(&idx))); err != nil {
		return 0, err
	}
	return int(idx), nil
}

// CreateBlob serializes the creator into a startup data blob and consumes
// the creator isolate (like the pinned OwnedIsolate::create_blob, which
// takes the isolate by value): the creator cannot be used afterwards. It
// must not run inside a handle scope. An engine failure still consumes the
// creator and is reported as an error (the pinned crate returns None here
// and panics only later, on drop without a blob).
func (sc *SnapshotCreator) CreateBlob(policy FunctionCodeHandling) (*StartupData, error) {
	if err := sc.check(); err != nil {
		return nil, err
	}
	if policy != FunctionCodeClear && policy != FunctionCodeKeep {
		return nil, fmt.Errorf("gov8: invalid FunctionCodeHandling %d", int32(policy))
	}
	if contextHasUnclearedSlots(sc.iso) {
		return nil, fmt.Errorf("gov8: snapshot creator has uncleared Context slots; call Context.ClearAllSlots before CreateBlob")
	}
	var dataPtr uintptr
	var size int64
	r1, _, _ := proc("gov8_snapshot_create_blob").Call(
		sc.iso.handle, sc.wrap, uintptr(int32(policy)),
		uintptr(unsafe.Pointer(&dataPtr)), uintptr(unsafe.Pointer(&size)))
	// The engine isolate is gone either way; tear the Go wrappers down.
	sc.teardown()
	if int64(r1) < 0 {
		return nil, shimError("SnapshotCreator.CreateBlob", r1)
	}
	if dataPtr == 0 || size <= 0 {
		return nil, fmt.Errorf("gov8: SnapshotCreator.CreateBlob produced no data")
	}
	buf := make([]byte, size)
	if err := callErr("SnapshotCreator.CreateBlob.Read",
		proc("gov8_snapshot_blob_read_delete"), dataPtr, uintptr(size),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(size)); err != nil {
		return nil, err
	}
	return &StartupData{bytes: buf, requiresExternalReferences: sc.requiresExternalReferences,
		externalReferencesKnown: true}, nil
}

// Close releases the creator isolate without producing a blob. In the
// pinned crate dropping a creator without create_blob panics; Go has no
// destructors, so abandonment is this explicit, safe operation, and the
// negative tests document the deviation. After Close (or CreateBlob) the
// creator and its isolate are dead: further use returns errors.
func (sc *SnapshotCreator) Close() error {
	if err := sc.iso.check(); err != nil {
		return err
	}
	if sc.closed {
		return fmt.Errorf("gov8: snapshot creator already closed")
	}
	r1, _, _ := proc("gov8_snapshot_creator_dispose").Call(sc.wrap)
	sc.teardown()
	if int64(r1) < 0 {
		return shimError("SnapshotCreator.Close", r1)
	}
	return nil
}

// teardown marks the creator and its engine-owned isolate as consumed. It
// runs on the owning thread, after the engine teardown inside the shim call
// has released the thread's engine-enter state.
func (sc *SnapshotCreator) teardown() {
	sc.iso.mu.Lock()
	sc.iso.handle = 0
	sc.iso.advancedExternalReferences = false
	sc.iso.publishClosedLocked()
	sc.iso.mu.Unlock()
	// Creator disposal removes its native table before returning. A second
	// address-keyed cleanup here could delete a newer creator's table after V8
	// reuses the disposed isolate address.
	unregisterIsolate(sc.iso)
	runtime.UnlockOSThread()
	sc.closed = true
}

// NewIsolateFromSnapshot creates an isolate that instantiates its default
// context from the startup blob (CreateParams::snapshot_blob via
// Isolate::new). The blob is guarded before any engine call: empty blobs
// and blobs shorter than the snapshot version header would trip fatal V8
// CHECKs in the pinned engine, so they are rejected as errors here. The
// engine keeps reading the blob bytes for context creation until the
// isolate is closed; the wrapper tracks this and StartupData.Release frees
// the engine copy once every isolate created from the blob is closed.
func NewIsolateFromSnapshot(blob *StartupData) (*Isolate, error) {
	if err := requireInitialized(); err != nil {
		return nil, err
	}
	if err := blob.validForCreation(); err != nil {
		return nil, err
	}
	if !blob.externalReferencesKnown {
		return nil, fmt.Errorf("gov8: snapshot external-reference requirements are unknown; use NewIsolateFromSnapshotWithParams with an explicit table")
	}
	if blob.requiresExternalReferences {
		return nil, fmt.Errorf("gov8: snapshot requires external references; use NewIsolateFromSnapshotWithParams")
	}
	runtime.LockOSThread()
	tid := currentThreadID()
	if err := beginIsolateCreate(); err != nil {
		runtime.UnlockOSThread()
		return nil, err
	}
	b := blob.bytes
	var holder, isoH uintptr
	r1, _, _ := proc("gov8_isolate_new_snapshot").Call(
		uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)),
		uintptr(unsafe.Pointer(&holder)), uintptr(unsafe.Pointer(&isoH)))
	if int64(r1) < 0 {
		abandonIsolateCreate()
		runtime.UnlockOSThread()
		return nil, shimError("NewIsolateFromSnapshot", r1)
	}
	iso := &Isolate{handle: isoH, tid: tid}
	finishIsolateCreate(iso)
	blob.trackRef(iso, holder)
	return iso, nil
}

// SnapshotDataType selects the downcast applied to data retrieved from a
// snapshot. It carries the same observable information as the pinned
// crate's type parameter: which engine predicate decides Ok versus BadType.
type SnapshotDataType uint8

const (
	// SnapshotDataValue accepts any value-typed data (v8::Value).
	SnapshotDataValue SnapshotDataType = 0
	// SnapshotDataString accepts string values (v8::String).
	SnapshotDataString SnapshotDataType = 1
	// SnapshotDataPrivate accepts private symbols (v8::Private).
	SnapshotDataPrivate SnapshotDataType = 2
)

// DataErrorKind mirrors v8::DataError: NoData for consumed or out-of-range
// indices, BadType for a wrongly typed request.
type DataErrorKind uint8

const (
	DataErrorNoData DataErrorKind = iota + 1
	DataErrorBadType
)

// SnapshotDataError is the error outcome of exactly-once snapshot-data
// retrieval.
type SnapshotDataError struct {
	Kind DataErrorKind
}

func (e *SnapshotDataError) Error() string {
	if e.Kind == DataErrorBadType {
		return "gov8: snapshot data at this index has the wrong type (BadType)"
	}
	return "gov8: no snapshot data at this index (NoData)"
}

// ContextFromSnapshot recovers an added context (global proxy included)
// from the snapshot backing the isolate. ok is false for out-of-range or
// absent indices, matching the pinned Context::from_snapshot returning
// None.
func (s *Scope) ContextFromSnapshot(index int) (*Context, bool, error) {
	if err := s.check(); err != nil {
		return nil, false, err
	}
	ih, err := s.iso.handleChecked()
	if err != nil {
		return nil, false, err
	}
	// The shim returns null exactly when the index has no context (the
	// engine's empty MaybeLocal for out-of-range indices).
	ch, _, _ := proc("gov8_context_from_snapshot").Call(ih, uintptr(index))
	if ch == 0 {
		return nil, false, nil
	}
	return &Context{iso: s.iso, handle: ch}, true, nil
}

// GetIsolateDataFromSnapshotOnce returns data previously attached with
// SnapshotCreator.AddIsolateData and consumes the reference: a second
// request for the same index yields a *SnapshotDataError with
// DataErrorNoData, as does an out-of-range index. A wrongly typed request
// yields DataErrorBadType — and still consumes the slot (the engine fetches
// the raw data before the type check; upstream caveat, mirrored).
func (s *Scope) GetIsolateDataFromSnapshotOnce(index int, want SnapshotDataType) (Value, error) {
	if err := s.check(); err != nil {
		return Value{}, err
	}
	ih, err := s.iso.handleChecked()
	if err != nil {
		return Value{}, err
	}
	var out uintptr
	r1, _, _ := proc("gov8_isolate_data_from_snapshot_once").Call(
		ih, s.handle, uintptr(index), uintptr(uint8(want)),
		uintptr(unsafe.Pointer(&out)))
	switch {
	case int64(r1) < 0:
		return Value{}, shimError("GetIsolateDataFromSnapshotOnce", r1)
	case r1 == 1:
		return Value{}, &SnapshotDataError{Kind: DataErrorNoData}
	case r1 == 2:
		return Value{}, &SnapshotDataError{Kind: DataErrorBadType}
	}
	return Value{iso: s.iso, sc: s, h: out}, nil
}

// GetContextDataFromSnapshotOnce is the context-snapshot counterpart of
// GetIsolateDataFromSnapshotOnce, with the same exactly-once and BadType
// semantics. The context must belong to the same isolate as the scope.
func (s *Scope) GetContextDataFromSnapshotOnce(c *Context, index int, want SnapshotDataType) (Value, error) {
	if err := s.check(); err != nil {
		return Value{}, err
	}
	if err := c.check(); err != nil {
		return Value{}, err
	}
	if c.iso != s.iso {
		return Value{}, foreignIsolate("context")
	}
	ih, err := s.iso.handleChecked()
	if err != nil {
		return Value{}, err
	}
	var out uintptr
	r1, _, _ := proc("gov8_context_data_from_snapshot_once").Call(
		ih, c.handle, s.handle, uintptr(index), uintptr(uint8(want)),
		uintptr(unsafe.Pointer(&out)))
	switch {
	case int64(r1) < 0:
		return Value{}, shimError("GetContextDataFromSnapshotOnce", r1)
	case r1 == 1:
		return Value{}, &SnapshotDataError{Kind: DataErrorNoData}
	case r1 == 2:
		return Value{}, &SnapshotDataError{Kind: DataErrorBadType}
	}
	return Value{iso: s.iso, sc: s, h: out}, nil
}
