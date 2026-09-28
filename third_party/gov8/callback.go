//go:build (windows || linux) && amd64

package gov8

import (
	"fmt"
	"math"
	"os"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"unsafe"

	syscall "github.com/maclof/gov8/internal/native"
)

// Native callbacks.
//
// Rust (pinned crate) maps each stateless `fn` callback to a monomorphized
// `extern "C"` trampoline; Go cannot generate code at runtime, so every Go
// callback is dispatched through ONE shared engine-side trampoline pair
// (function callbacks and accessor callbacks; see
// internal/shim/features/templates_callbacks.inc). V8's per-callback `data`
// slot carries a shim-owned dispatch context (an External wrapping the Go
// registry handle), and the shim re-materializes the embedder's callback
// data from a Global so args.Data() observes it verbatim — the pinned oracle
// checks require both: dispatch must work and Data() must round-trip.
//
// Ownership rules:
//   - No Go pointer ever crosses into the engine. The engine only ever sees
//     an integer registry handle; the Go closure lives in a Go-side registry.
//   - Callback values (arguments, receiver, new.target, data) are wires in
//     the engine callback scope; the wrapper opens a Go Scope for the
//     callback and closes it before returning, so those values cannot
//     outlive the invocation.
//   - A panic inside a callback is recovered and deliberately translated
//     into a process abort (exit code 0xC0000409, fail-fast) after printing
//     the panic message — the observable equivalent of the pinned oracle's
//     panic boundary, where unwinding out of the `extern "C"` trampoline
//     aborts the process. Returning the panic into the engine instead would
//     unwind through C++ frames, which is unsupported and corrupts V8 state.

// FunctionCallback mirrors v8::FunctionCallback. cs carries the callback's
// scope, the engine's current context and native re-entry helpers; args are
// the JS call arguments; rv receives the return value (undefined when the
// callback sets nothing).
type FunctionCallback func(cs *CallbackScope, args FunctionCallbackArguments, rv ReturnValue)

// AccessorGetterCallback mirrors v8::AccessorNameGetterCallback: every read
// of the intercepted property invokes it; the read result goes into rv.
type AccessorGetterCallback func(cs *CallbackScope, args PropertyCallbackArguments, rv ReturnValue)

// AccessorSetterCallback mirrors v8::AccessorNameSetterCallback: every write
// to the intercepted property invokes it with the assigned value.
type AccessorSetterCallback func(cs *CallbackScope, args PropertyCallbackArguments, value Value)

// hostCallbackFrame mirrors the C++ CallbackFrame laid out by the shim
// trampolines (internal/shim/features/templates_callbacks.inc). It lives on
// the trampoline's C++ stack and is only valid for the duration of the
// synchronous dispatch call. The trailing fields belong to the
// template-advanced interceptor kinds; keep them field-for-field in sync with
// the C++ struct.
type hostCallbackFrame struct {
	kind          int32
	flags         int32
	handle        uint64
	argc          int64
	argv          unsafe.Pointer
	isolate       uintptr
	scopeWire     uintptr
	ctxWire       uintptr
	dataWire      uintptr
	thisWire      uintptr
	newTargetWire uintptr
	propertyWire  uintptr
	valueWire     uintptr
	rvWord        uintptr
	index         uint32
	// outIntercepted is written by Go for interceptor kinds: the Intercepted
	// verdict (0 = kYes, 1 = kNo) the trampoline returns to the engine.
	outIntercepted int32
	holderWire     uintptr
	outWire        uintptr
	shouldThrow    int32
	pdFlags        int32
	pdWritable     int32
	pdEnumerable   int32
	pdConfigurable int32
	pdPad          int32
	pdValueWire    uintptr
}

// hostCallbackInvocation keeps the two user-visible callback scope views in
// one uniquely-owned allocation. Either pointer may be retained by user code;
// both continue to address this invocation record after dispatch, with scope
// invalidation making every later engine-backed operation fail deterministically.
// FunctionCallbackArguments keeps immutable Length/IsConstructCall snapshots
// by value and routes all frame-backed access through that invalidation check.
type hostCallbackInvocation struct {
	scope    Scope
	callback CallbackScope
}

const (
	cbKindFunction    = 0
	cbKindAccessorGet = 1
	cbKindAccessorSet = 2
	// Interceptor kinds (template-advanced slice); order matches the C++
	// constants.
	cbKindNamedGet          = 3
	cbKindNamedSet          = 4
	cbKindNamedQuery        = 5
	cbKindNamedDelete       = 6
	cbKindNamedEnum         = 7
	cbKindNamedDefine       = 8
	cbKindNamedDescriptor   = 9
	cbKindIndexedGet        = 10
	cbKindIndexedSet        = 11
	cbKindIndexedQuery      = 12
	cbKindIndexedDelete     = 13
	cbKindIndexedEnum       = 14
	cbKindIndexedDefine     = 15
	cbKindIndexedDescriptor = 16
)

// PropertyDescriptor presence bits shared with the shim's pd_flags packing.
const (
	pdFlagHasValue        = 1
	pdFlagHasWritable     = 2
	pdFlagHasEnumerable   = 4
	pdFlagHasConfigurable = 8
)

// hostCallbackEntry is one registered native callback. Function callbacks
// use fn; accessor pairs use get/set (either side may be nil); property
// handlers use the n*/i* interceptor fields. ctx is the shim-owned dispatch
// context pointer released with the isolate's host state.
type hostCallbackEntry struct {
	iso *Isolate
	ctx uintptr
	fn  FunctionCallback
	get AccessorGetterCallback
	set AccessorSetterCallback

	nget    NamedPropertyGetterCallback
	nset    NamedPropertySetterCallback
	nquery  NamedPropertyQueryCallback
	ndel    NamedPropertyDeleterCallback
	nenum   NamedPropertyEnumeratorCallback
	ndefine NamedPropertyDefinerCallback
	ndesc   NamedPropertyDescriptorCallback
	iget    IndexedPropertyGetterCallback
	iset    IndexedPropertySetterCallback
	iquery  IndexedPropertyQueryCallback
	idel    IndexedPropertyDeleterCallback
	ienum   IndexedPropertyEnumeratorCallback
	idefine IndexedPropertyDefinerCallback
	idesc   IndexedPropertyDescriptorCallback
}

const hostCallbackFastChunkSize = 1024

type hostCallbackFastChunk [hostCallbackFastChunkSize]atomic.Pointer[hostCallbackEntry]

// hostCallbackFastTable is an immutable directory of stable chunks. The
// registry mutex publishes a new directory only when a monotonically assigned
// handle reaches a new chunk; individual entry publication/removal is atomic.
// Callback dispatch therefore avoids the global registry mutex without
// allocating on each registration.
type hostCallbackFastTable struct {
	chunks []*hostCallbackFastChunk
}

var hostCallbackRegistry = struct {
	mu          sync.Mutex
	next        uint64
	entries     map[uint64]*hostCallbackEntry
	lazyGetters map[lazyGetterCacheKey]uint64
	fast        atomic.Pointer[hostCallbackFastTable]
}{
	entries:     make(map[uint64]*hostCallbackEntry),
	lazyGetters: make(map[lazyGetterCacheKey]uint64),
}

// lazyGetterCacheKey identifies one exact Go function value within an
// isolate. A Go function value is a pointer-sized reference to an immutable
// funcval; copies of the same closure retain that reference, while separately
// created capturing closures have distinct references. The registry entry
// holds the function value itself, so its funcval cannot be collected or have
// its address reused while this key remains published.
//
// This representation-dependent optimization is confined to the supported
// Windows/amd64 build. Missing the cache is always safe; equality is used only
// to share an otherwise identical, immutable dispatch context.
type lazyGetterCacheKey struct {
	iso      *Isolate
	identity uintptr
}

func lazyGetterIdentity(getter AccessorGetterCallback) uintptr {
	return *(*uintptr)(unsafe.Pointer(&getter))
}

var (
	ensureDispatcherOnce sync.Once
	ensureDispatcherErr  error
)

// goHostDispatch is the single entry point handed to the shim; all engine
// callbacks funnel through it.
var goHostDispatch = syscall.NewCallback(hostCallbackDispatch)

func ensureDispatcher() error {
	ensureDispatcherOnce.Do(func() {
		ensureDispatcherErr = callErr("SetDispatcher",
			proc("gov8_host_set_dispatcher"), goHostDispatch)
	})
	return ensureDispatcherErr
}

// newHostContext allocates the shim-side dispatch context for a Go
// registration and pairs it with the registry entry.
func newHostContext(iso *Isolate, fn FunctionCallback, get AccessorGetterCallback, set AccessorSetterCallback, data Value) (uint64, error) {
	if fn == nil && get == nil && set == nil {
		return 0, fmt.Errorf("gov8: native callback requires a function")
	}
	return registerHostEntry(iso, &hostCallbackEntry{fn: fn, get: get, set: set}, data)
}

// registerHostEntry validates the registration preconditions, allocates the
// shim dispatch context and publishes the entry under a fresh integer
// handle. The entry's callback fields must already be populated.
func registerHostEntry(iso *Isolate, e *hostCallbackEntry, data Value) (uint64, error) {
	if err := iso.check(); err != nil {
		return 0, err
	}
	return registerHostEntryAssumingIsolate(iso, e, data)
}

// registerHostEntryAssumingIsolate registers after the caller has already
// proved the isolate lifecycle and owner thread in the same operation.
func registerHostEntryAssumingIsolate(iso *Isolate, e *hostCallbackEntry, data Value) (uint64, error) {
	if data.h != 0 {
		if err := data.check(); err != nil {
			return 0, err
		}
		if data.iso != iso {
			return 0, foreignIsolate("data")
		}
	}
	if err := ensureDispatcher(); err != nil {
		return 0, err
	}
	if err := requireInitialized(); err != nil {
		return 0, err
	}
	hostCallbackRegistry.mu.Lock()
	for {
		hostCallbackRegistry.next++
		if hostCallbackRegistry.next != 0 && hostCallbackRegistry.entries[hostCallbackRegistry.next] == nil {
			break
		}
	}
	h := hostCallbackRegistry.next
	hostCallbackRegistry.mu.Unlock()

	ctx, err := callHandle("HostContext.New", proc("gov8_host_context_new"),
		iso.handleAssumingCheck(), uintptr(h), data.h)
	if err != nil {
		return 0, err
	}
	e.iso = iso
	e.ctx = ctx
	hostCallbackRegistry.mu.Lock()
	hostCallbackRegistry.entries[h] = e
	publishFastHostCallbackLocked(h, e)
	hostCallbackRegistry.mu.Unlock()
	return h, nil
}

// registerFunctionCallback reserves a registry handle for a function
// callback created on the isolate.
func registerFunctionCallback(iso *Isolate, fn FunctionCallback, data Value) (uint64, error) {
	return newHostContext(iso, fn, nil, nil, data)
}

func registerFunctionCallbackAssumingIsolate(iso *Isolate, fn FunctionCallback, data Value) (uint64, *hostCallbackEntry, error) {
	if fn == nil {
		return 0, nil, fmt.Errorf("gov8: native callback requires a function")
	}
	entry := &hostCallbackEntry{fn: fn}
	handle, err := registerHostEntryAssumingIsolate(iso, entry, data)
	if err != nil {
		return 0, nil, err
	}
	return handle, entry, nil
}

// registerAccessorCallbacks reserves a registry handle for an accessor pair;
// exactly one of getter/setter may be nil.
func registerAccessorCallbacks(iso *Isolate, getter AccessorGetterCallback, setter AccessorSetterCallback, data Value) (uint64, error) {
	if getter == nil && setter == nil {
		return 0, fmt.Errorf("gov8: accessor requires a getter or a setter")
	}
	return newHostContext(iso, nil, getter, setter, data)
}

// registerLazyGetter reuses the dispatch entry and native context when the
// caller installs the same exact getter function value without callback data.
// That is the Go equivalent of rusty_v8 repeatedly passing the same static
// function pointer. Dynamic callback arguments (property, holder, receiver,
// context and ReturnValue) still come from each invocation's CallbackFrame.
//
// Isolate affinity serializes calls for one isolate, so a cache miss cannot
// race another publication for the same key. created tells the caller whether
// a failed V8 installation should remove this newly-created registration.
func registerLazyGetter(iso *Isolate, getter AccessorGetterCallback, data Value) (handle uint64, entry *hostCallbackEntry, key lazyGetterCacheKey, created bool, err error) {
	if getter == nil {
		return 0, nil, lazyGetterCacheKey{}, false, errNilLazyGetter
	}
	if data.h != 0 {
		handle, err = registerAccessorCallbacks(iso, getter, nil, data)
		if err != nil {
			return 0, nil, lazyGetterCacheKey{}, false, err
		}
		return handle, lookupHostCallback(handle), lazyGetterCacheKey{}, true, nil
	}

	key = lazyGetterCacheKey{iso: iso, identity: lazyGetterIdentity(getter)}
	hostCallbackRegistry.mu.Lock()
	if cached := hostCallbackRegistry.lazyGetters[key]; cached != 0 {
		entry = hostCallbackRegistry.entries[cached]
		hostCallbackRegistry.mu.Unlock()
		if entry == nil {
			return 0, nil, lazyGetterCacheKey{}, false, errLostCallbackRegistration
		}
		return cached, entry, key, false, nil
	}
	hostCallbackRegistry.mu.Unlock()

	handle, err = registerAccessorCallbacks(iso, getter, nil, data)
	if err != nil {
		return 0, nil, lazyGetterCacheKey{}, false, err
	}
	hostCallbackRegistry.mu.Lock()
	entry = hostCallbackRegistry.entries[handle]
	if entry != nil {
		hostCallbackRegistry.lazyGetters[key] = handle
	}
	hostCallbackRegistry.mu.Unlock()
	if entry == nil {
		dropHostCallback(handle)
		return 0, nil, lazyGetterCacheKey{}, false, errLostCallbackRegistration
	}
	return handle, entry, key, true, nil
}

func dropNewLazyGetter(handle uint64, key lazyGetterCacheKey) {
	if key.identity != 0 {
		hostCallbackRegistry.mu.Lock()
		if hostCallbackRegistry.lazyGetters[key] == handle {
			delete(hostCallbackRegistry.lazyGetters, key)
		}
		hostCallbackRegistry.mu.Unlock()
	}
	dropHostCallback(handle)
}

func lookupHostCallback(handle uint64) *hostCallbackEntry {
	if handle == 0 {
		return nil
	}
	index := handle - 1
	table := hostCallbackRegistry.fast.Load()
	chunkIndex := index / hostCallbackFastChunkSize
	if table == nil || chunkIndex >= uint64(len(table.chunks)) {
		return nil
	}
	return table.chunks[chunkIndex][index%hostCallbackFastChunkSize].Load()
}

func publishFastHostCallbackLocked(handle uint64, entry *hostCallbackEntry) {
	index := handle - 1
	chunkIndex := int(index / hostCallbackFastChunkSize)
	table := hostCallbackRegistry.fast.Load()
	if table == nil || chunkIndex >= len(table.chunks) {
		chunks := make([]*hostCallbackFastChunk, chunkIndex+1)
		if table != nil {
			copy(chunks, table.chunks)
		}
		chunks[chunkIndex] = &hostCallbackFastChunk{}
		table = &hostCallbackFastTable{chunks: chunks}
		hostCallbackRegistry.fast.Store(table)
	}
	table.chunks[chunkIndex][index%hostCallbackFastChunkSize].Store(entry)
}

func dropFastHostCallback(handle uint64) {
	if handle == 0 {
		return
	}
	index := handle - 1
	table := hostCallbackRegistry.fast.Load()
	chunkIndex := index / hostCallbackFastChunkSize
	if table != nil && chunkIndex < uint64(len(table.chunks)) {
		table.chunks[chunkIndex][index%hostCallbackFastChunkSize].Store(nil)
	}
}

func dropHostCallback(handle uint64) {
	dropFastHostCallback(handle)
	hostCallbackRegistry.mu.Lock()
	entry := hostCallbackRegistry.entries[handle]
	delete(hostCallbackRegistry.entries, handle)
	hostCallbackRegistry.mu.Unlock()
	if entry != nil && entry.ctx != 0 {
		_ = callErr("HostContext.Free", proc("gov8_host_context_free"),
			entry.iso.handle, entry.ctx)
	}
}

// hostCallbackDispatch is the Go half of the native-callback boundary. It
// runs synchronously inside the engine trampoline, on the isolate's owning
// thread (V8 invokes API callbacks during engine execution).
//
// The frame arrives as a raw pointer argument: syscall callbacks pass
// pointer-sized values and the runtime delivers each argument verbatim, so
// the parameter is typed as the frame pointer itself and no
// uintptr→unsafe.Pointer conversion exists on this path.
func hostCallbackDispatch(frame *hostCallbackFrame) uintptr {
	if frame == nil {
		fatalNilHostCallbackFrame()
		return 1
	}
	entry := lookupHostCallback(frame.handle)
	if entry == nil {
		fatalUnknownHostCallback(frame.handle)
		return 1
	}
	iso := entry.iso
	// Enforce the isolate's thread affinity before any engine work: a
	// callback running on a foreign thread means the engine contract was
	// already violated at a higher level.
	// Check the immutable owner thread before reading closed. A foreign-thread
	// callback must not touch lock-protected isolate state even on the fatal
	// path; the owner-thread invariant then makes the closed read stable.
	if currentThreadID() != iso.tid || iso.closed {
		fatalWrongThreadHostCallback(iso)
		return 1
	}
	if frame.isolate != iso.handleAssumingCheck() || frame.scopeWire == 0 {
		fatalInvalidHostCallbackFrame()
		return 1
	}
	// Every native trampoline owns a HandleScope around this synchronous
	// dispatch and exposes a stack GoScope token in scopeWire. Borrow it rather
	// than entering a second native HandleScope. The C++ token and this Go view
	// are both invalidated as dispatch unwinds; retained callback values remain
	// deterministically unusable after the callback.
	invocation := &hostCallbackInvocation{}
	invocation.scope = Scope{iso: iso, handle: frame.scopeWire, borrowed: true}
	invocation.callback = CallbackScope{iso: iso, sc: &invocation.scope, ctxWire: frame.ctxWire, frame: frame}
	scope := &invocation.scope
	// Deferred functions run LIFO: the borrowed Go view is invalidated first,
	// then the panic handler converts a recovered panic into the process abort
	// documented on FunctionCallback. C++ closes the actual HandleScope after
	// this dispatcher returns (or while the abort boundary unwinds).
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "gov8: panic in native callback: %v\n", r)
			debug.PrintStack()
			proc("gov8_host_panic_abort").Call()
			// abort() does not return; unreachable.
			panic(r)
		}
	}()
	defer func() {
		scope.closed = true
		scope.handle = 0
	}()

	cs := &invocation.callback
	switch frame.kind {
	case cbKindFunction:
		entry.fn(cs, FunctionCallbackArguments{
			cs: cs, frame: frame, shape: packFunctionCallbackArgumentShape(frame.argc, frame.flags&1 != 0),
		},
			ReturnValue{cs: cs, frame: frame})
	case cbKindAccessorGet:
		entry.get(cs, PropertyCallbackArguments{cs: cs, frame: frame},
			ReturnValue{cs: cs, frame: frame})
	case cbKindAccessorSet:
		entry.set(cs, PropertyCallbackArguments{cs: cs, frame: frame},
			cs.wrap(frame.valueWire))
	case cbKindNamedGet:
		frame.outIntercepted = int32(entry.nget(cs, cs.wrap(frame.propertyWire),
			PropertyCallbackArguments{cs: cs, frame: frame},
			ReturnValue{cs: cs, frame: frame}))
	case cbKindNamedSet:
		frame.outIntercepted = int32(entry.nset(cs, cs.wrap(frame.propertyWire),
			cs.wrap(frame.valueWire),
			PropertyCallbackArguments{cs: cs, frame: frame},
			ReturnValue{cs: cs, frame: frame}))
	case cbKindNamedQuery:
		frame.outIntercepted = int32(entry.nquery(cs, cs.wrap(frame.propertyWire),
			PropertyCallbackArguments{cs: cs, frame: frame},
			ReturnValue{cs: cs, frame: frame}))
	case cbKindNamedDelete:
		frame.outIntercepted = int32(entry.ndel(cs, cs.wrap(frame.propertyWire),
			PropertyCallbackArguments{cs: cs, frame: frame},
			ReturnValue{cs: cs, frame: frame}))
	case cbKindNamedEnum:
		entry.nenum(cs, PropertyCallbackArguments{cs: cs, frame: frame},
			ReturnValue{cs: cs, frame: frame})
	case cbKindNamedDefine:
		frame.outIntercepted = int32(entry.ndefine(cs, cs.wrap(frame.propertyWire),
			propertyDescriptorFromFrame(cs, frame),
			PropertyCallbackArguments{cs: cs, frame: frame},
			ReturnValue{cs: cs, frame: frame}))
	case cbKindNamedDescriptor:
		frame.outIntercepted = int32(entry.ndesc(cs, cs.wrap(frame.propertyWire),
			PropertyCallbackArguments{cs: cs, frame: frame},
			ReturnValue{cs: cs, frame: frame}))
	case cbKindIndexedGet:
		frame.outIntercepted = int32(entry.iget(cs, frame.index,
			PropertyCallbackArguments{cs: cs, frame: frame},
			ReturnValue{cs: cs, frame: frame}))
	case cbKindIndexedSet:
		frame.outIntercepted = int32(entry.iset(cs, frame.index,
			cs.wrap(frame.valueWire),
			PropertyCallbackArguments{cs: cs, frame: frame},
			ReturnValue{cs: cs, frame: frame}))
	case cbKindIndexedQuery:
		frame.outIntercepted = int32(entry.iquery(cs, frame.index,
			PropertyCallbackArguments{cs: cs, frame: frame},
			ReturnValue{cs: cs, frame: frame}))
	case cbKindIndexedDelete:
		frame.outIntercepted = int32(entry.idel(cs, frame.index,
			PropertyCallbackArguments{cs: cs, frame: frame},
			ReturnValue{cs: cs, frame: frame}))
	case cbKindIndexedEnum:
		entry.ienum(cs, PropertyCallbackArguments{cs: cs, frame: frame},
			ReturnValue{cs: cs, frame: frame})
	case cbKindIndexedDefine:
		frame.outIntercepted = int32(entry.idefine(cs, frame.index,
			propertyDescriptorFromFrame(cs, frame),
			PropertyCallbackArguments{cs: cs, frame: frame},
			ReturnValue{cs: cs, frame: frame}))
	case cbKindIndexedDescriptor:
		frame.outIntercepted = int32(entry.idesc(cs, frame.index,
			PropertyCallbackArguments{cs: cs, frame: frame},
			ReturnValue{cs: cs, frame: frame}))
	default:
		fatalUnknownHostCallbackKind(frame.kind)
		return 1
	}
	return 0
}

// Keep fail-fast formatting out of hostCallbackDispatch's successful path.
// Passing values through the generic variadic formatter there made the Go
// compiler conservatively heap-box them on every callback, even though these
// branches terminate the process and are never taken during valid dispatch.
//
//go:noinline
func fatalNilHostCallbackFrame() {
	fatalHostMisuse("gov8: native callback dispatch received a nil frame")
}

//go:noinline
func fatalUnknownHostCallback(handle uint64) {
	fatalHostMisuse("gov8: native callback dispatch for unknown handle %d", handle)
}

//go:noinline
func fatalWrongThreadHostCallback(iso *Isolate) {
	fatalHostMisuse("gov8: native callback invoked off the owning thread: isolate owner %s, callback thread %s",
		quoteThreadID(iso.tid), quoteThreadID(currentThreadID()))
}

//go:noinline
func fatalInvalidHostCallbackFrame() {
	fatalHostMisuse("gov8: native callback supplied an invalid isolate or scope")
}

//go:noinline
func fatalUnknownHostCallbackKind(kind int32) {
	fatalHostMisuse("gov8: native callback dispatch for unknown kind %d", kind)
}

// fatalHostMisuse reports an unrecoverable dispatch-time misuse. Like the
// pinned oracle's fail-fast boundary, this does not return into the engine.
func fatalHostMisuse(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "gov8: fatal: "+format+"\n", args...)
	proc("gov8_host_panic_abort").Call()
}

// CallbackScope is the execution scope handed to native callbacks. It pairs
// the callback's own Scope (value construction) with the engine's current
// context, captured by the trampoline as a scope-local wire: context-bound
// conversions inside callbacks run through that wire instead of a persistent
// context wrapper (the engine materializes no extra Global per invocation).
type CallbackScope struct {
	iso     *Isolate
	sc      *Scope
	ctxWire uintptr
	frame   *hostCallbackFrame
}

// Isolate returns the isolate the callback runs on.
func (cs *CallbackScope) Isolate() *Isolate { return cs.iso }

// Scope returns the callback's value-construction scope. Values created
// through it are only valid inside the callback.
func (cs *CallbackScope) Scope() *Scope { return cs.sc }

func (cs *CallbackScope) wrap(wire uintptr) Value {
	return Value{iso: cs.iso, sc: cs.sc, h: wire}
}

func (cs *CallbackScope) check() error {
	if cs == nil || cs.iso == nil || cs.sc == nil {
		return fmt.Errorf("gov8: invalid callback scope")
	}
	if cs.ctxWire == 0 {
		return fmt.Errorf("gov8: callback has no current context")
	}
	return cs.sc.check()
}

// checkValue validates a callback value with one owner-thread check on the
// overwhelmingly common same-isolate path. Value.check and CallbackScope.check
// would otherwise each query the OS thread independently. Unrelated or
// malformed values retain the original validation order and errors.
func (cs *CallbackScope) checkValue(v Value) error {
	if v.h == 0 {
		return fmt.Errorf("gov8: zero value handle")
	}
	if cs != nil && cs.iso != nil && cs.sc != nil && v.iso == cs.iso && v.sc != nil {
		if err := cs.check(); err != nil {
			return err
		}
		if v.sc.closed {
			return fmt.Errorf("gov8: scope used after Close")
		}
		return nil
	}
	if err := v.check(); err != nil {
		return err
	}
	return cs.check()
}

// NewString creates a JS string in the callback scope.
func (cs *CallbackScope) NewString(str string) (Value, error) {
	return cs.sc.NewString(str)
}

// ToString returns the ECMAScript ToString of the value (lossy UTF-8),
// evaluated in the callback's current context.
func (cs *CallbackScope) ToString(v Value) (string, error) {
	if err := v.check(); err != nil {
		return "", err
	}
	if err := cs.check(); err != nil {
		return "", err
	}
	return callTextFn("CallbackScope.ToString", func(buf *byte, cap int, outLen *int64) uintptr {
		r, _, _ := proc("gov8_wctx_to_string_utf8").Call(
			cs.iso.handle, cs.ctxWire, cs.sc.handle, v.h,
			uintptr(unsafe.Pointer(buf)), uintptr(cap), uintptr(unsafe.Pointer(outLen)))
		return r
	})
}

// IntegerValue is Value::IntegerValue in the callback's current context;
// ok is false when the conversion failed.
func (cs *CallbackScope) IntegerValue(v Value) (int64, bool, error) {
	if err := cs.checkValue(v); err != nil {
		return 0, false, err
	}
	if value, ok := cs.cachedInt32Argument(v); ok {
		return value, true, nil
	}
	callbackScalarProcsOnce.Do(resolveCallbackScalarProcs)
	result, _, _ := syscall.Syscall(callbackIntegerValueAddr, 3,
		cs.iso.handle, cs.ctxWire, v.h)
	if int64(result) < 0 {
		return 0, false, shimError("CallbackScope.IntegerValue", result)
	}
	if result == 0 {
		return 0, false, nil
	}
	// result addresses shim-owned thread-local storage, not Go memory. The
	// conversion stores it only after nested JS/Go re-entry has completed, and
	// callback execution remains pinned to the isolate's owning OS thread.
	return callbackNativeInt64(result), true, nil
}

func callbackNativeInt64(address uintptr) int64 {
	// address is allocated by the shim and remains valid for the lifetime of
	// the DLL. Bit-copy the native address into pointer form so vet does not
	// mistake it for a Go pointer that escaped through uintptr arithmetic.
	pointer := *(*unsafe.Pointer)(unsafe.Pointer(&address))
	return *(*int64)(pointer)
}

// NumberValue is Value::NumberValue in the callback's current context.
func (cs *CallbackScope) NumberValue(v Value) (float64, bool, error) {
	if err := v.check(); err != nil {
		return 0, false, err
	}
	if err := cs.check(); err != nil {
		return 0, false, err
	}
	var out float64
	var okv int32
	r1, _, _ := proc("gov8_wctx_number_value").Call(
		cs.iso.handle, cs.ctxWire, v.h,
		uintptr(unsafe.Pointer(&out)), uintptr(unsafe.Pointer(&okv)))
	if int64(r1) < 0 {
		return 0, false, shimError("CallbackScope.NumberValue", r1)
	}
	return out, okv == 1, nil
}

// ObjectGet reads a named property in the callback's current context; ok is
// false when the read threw.
func (cs *CallbackScope) ObjectGet(obj Value, key string) (Value, bool, error) {
	if err := obj.check(); err != nil {
		return Value{}, false, err
	}
	if err := cs.check(); err != nil {
		return Value{}, false, err
	}
	k, err := cs.sc.NewString(key)
	if err != nil {
		return Value{}, false, err
	}
	var out uintptr
	var okv int32
	r1, _, _ := proc("gov8_wctx_object_get").Call(
		cs.iso.handle, cs.ctxWire, cs.sc.handle, obj.h, k.h,
		uintptr(unsafe.Pointer(&out)), uintptr(unsafe.Pointer(&okv)))
	if int64(r1) < 0 {
		return Value{}, false, shimError("CallbackScope.ObjectGet", r1)
	}
	return cs.wrap(out), okv == 1, nil
}

// ObjectSet writes a named property in the callback's current context; ok is
// false when the write threw or was rejected.
func (cs *CallbackScope) ObjectSet(obj Value, key string, v Value) (bool, error) {
	if err := obj.check(); err != nil {
		return false, err
	}
	if err := v.check(); err != nil {
		return false, err
	}
	if err := cs.check(); err != nil {
		return false, err
	}
	k, err := cs.sc.NewString(key)
	if err != nil {
		return false, err
	}
	var okv int32
	r1, _, _ := proc("gov8_wctx_object_set").Call(
		cs.iso.handle, cs.ctxWire, cs.sc.handle, obj.h, k.h, v.h,
		uintptr(unsafe.Pointer(&okv)))
	if int64(r1) < 0 {
		return false, shimError("CallbackScope.ObjectSet", r1)
	}
	return okv == 1, nil
}

// CallFunction re-enters JavaScript from inside the callback: it invokes fn
// (a function value) with recv and args. ok is false when the call threw.
func (cs *CallbackScope) CallFunction(fn Value, recv Value, args []Value) (Value, bool, error) {
	if err := fn.check(); err != nil {
		return Value{}, false, err
	}
	if err := recv.check(); err != nil {
		return Value{}, false, err
	}
	if err := cs.check(); err != nil {
		return Value{}, false, err
	}
	wires := valueWires(args)
	var argv uintptr
	if len(wires) > 0 {
		argv = uintptr(unsafe.Pointer(&wires[0]))
	}
	var out uintptr
	r1, _, _ := proc("gov8_function_call_wctx").Call(
		cs.iso.handle, cs.ctxWire, cs.sc.handle, fn.h, recv.h,
		uintptr(len(wires)), argv, uintptr(unsafe.Pointer(&out)))
	if int64(r1) < 0 {
		return Value{}, false, shimError("CallbackScope.CallFunction", r1)
	}
	return cs.wrap(out), true, nil
}

// NewObject creates a plain JS object in the callback's current context
// (v8::Object::New). Used by descriptor handlers and enumerator writers.
func (cs *CallbackScope) NewObject() (Value, error) {
	if err := cs.check(); err != nil {
		return Value{}, err
	}
	h, err := callHandle("CallbackScope.NewObject", proc("gov8_wctx_object_new"),
		cs.iso.handle, cs.ctxWire, cs.sc.handle)
	if err != nil {
		return Value{}, err
	}
	return cs.wrap(h), nil
}

// NewArrayWithElements creates a JS array from the given elements in the
// callback's current context (v8::Array::NewWithElements). Elements must
// belong to the callback's isolate.
func (cs *CallbackScope) NewArrayWithElements(elements []Value) (Value, error) {
	if err := cs.check(); err != nil {
		return Value{}, err
	}
	for _, e := range elements {
		if err := e.check(); err != nil {
			return Value{}, err
		}
		if e.iso != cs.iso {
			return Value{}, foreignIsolate("element")
		}
	}
	wires := valueWires(elements)
	var argv uintptr
	if len(wires) > 0 {
		argv = uintptr(unsafe.Pointer(&wires[0]))
	}
	var out uintptr
	r1, _, _ := proc("gov8_wctx_array_new_with_elements").Call(
		cs.iso.handle, cs.ctxWire, cs.sc.handle, argv, uintptr(len(wires)),
		uintptr(unsafe.Pointer(&out)))
	if int64(r1) < 0 {
		return Value{}, shimError("CallbackScope.NewArrayWithElements", r1)
	}
	return cs.wrap(out), nil
}

// ThrowException schedules v to propagate to the JS caller once the native
// callback returns (Scope::throw_exception in the oracle). The callback's
// return value is ignored by the engine when an exception is scheduled.
func (cs *CallbackScope) ThrowException(v Value) error {
	if err := cs.check(); err != nil {
		return err
	}
	if err := v.check(); err != nil {
		return err
	}
	if v.iso != cs.iso {
		return foreignIsolate("exception")
	}
	return callErr("ThrowException", proc("gov8_isolate_throw_exception"),
		cs.iso.handle, v.h)
}

// NewError builds a JS Error object with the given message in the callback
// scope (v8::Exception::Error).
func (cs *CallbackScope) NewError(message string) (Value, error) {
	if err := cs.sc.check(); err != nil {
		return Value{}, err
	}
	msg, err := cs.sc.NewString(message)
	if err != nil {
		return Value{}, err
	}
	h, err := callHandle("NewError", proc("gov8_exception_error"),
		cs.iso.handle, cs.sc.handle, msg.h)
	if err != nil {
		return Value{}, err
	}
	return cs.wrap(h), nil
}

// valueWires materializes values' wires into a fresh slice for a shim call;
// the caller takes the address of element 0 inline in the call expression.
func valueWires(values []Value) []uintptr {
	wires := make([]uintptr, len(values))
	for i, v := range values {
		wires[i] = v.h
	}
	return wires
}

// FunctionCallbackArguments mirrors v8::FunctionCallbackArguments. Length and
// IsConstructCall are immutable Go-owned snapshots and remain safe to inspect
// on a copied wrapper. Get, This, NewTarget and Data expose engine locals and
// are valid only while the callback is running; they validate the callback's
// borrowed Scope before reading the trampoline-owned native frame. Get returns
// undefined for out-of-bounds indices (matching the crate's bounds handling),
// This is the receiver, NewTarget is the constructor function for construct
// calls and undefined otherwise, and Data is the callback data attached at
// creation time.
type FunctionCallbackArguments struct {
	cs    *CallbackScope
	frame *hostCallbackFrame
	shape uintptr
}

// The native argc source is a non-negative C++ int. Preserve its exact
// 32-bit representation in the low word and reserve bit 32 for construct
// status. Length narrows through uint32 before converting to Go int, so the
// high shape bits can never contaminate or overflow length extraction.
const functionCallbackConstructShapeBit uintptr = 1 << 32

func packFunctionCallbackArgumentShape(argc int64, construct bool) uintptr {
	shape := uintptr(uint32(argc))
	if construct {
		shape |= functionCallbackConstructShapeBit
	}
	return shape
}

// checkedFrame validates callback lifetime and owner-thread affinity before
// dereferencing the trampoline-owned frame. A retained argument wrapper keeps
// its Go callback invocation alive, but the native frame was stack-owned and
// is already gone; checking the borrowed Scope first makes that misuse fail
// deterministically without reading stale native memory.
func (a FunctionCallbackArguments) checkedFrame() (*hostCallbackFrame, error) {
	if a.cs == nil {
		return nil, fmt.Errorf("gov8: invalid callback arguments")
	}
	if err := a.cs.check(); err != nil {
		return nil, err
	}
	if a.frame == nil {
		return nil, fmt.Errorf("gov8: invalid callback arguments")
	}
	return a.frame, nil
}

// Length returns the number of actually passed arguments.
func (a FunctionCallbackArguments) Length() int { return int(uint32(a.shape)) }

// IsConstructCall reports whether this is a `new F(..)` construct call.
func (a FunctionCallbackArguments) IsConstructCall() bool {
	return a.shape&functionCallbackConstructShapeBit != 0
}

// Get returns the argument at index i, or undefined when out of bounds.
func (a FunctionCallbackArguments) Get(i int) (Value, error) {
	frame, err := a.checkedFrame()
	if err != nil {
		return Value{}, err
	}
	if i < 0 || i >= int(frame.argc) {
		return a.cs.sc.Undefined()
	}
	// argv points to a C array of wires laid out by the trampoline; the
	// slice header re-types the trampoline-owned pointer for this read.
	args := unsafe.Slice((*uintptr)(frame.argv), int(frame.argc))
	wire := args[i]
	if wire == 0 {
		return a.cs.sc.Undefined()
	}
	return a.cs.wrap(wire), nil
}

// This returns the call receiver (the created instance for construct calls).
func (a FunctionCallbackArguments) This() (*Object, error) {
	value, err := a.ThisValue()
	if err != nil {
		return nil, err
	}
	return &Object{value}, nil
}

// ThisValue returns the same receiver without allocating an Object wrapper.
// It has the same borrowed callback-scope lifetime as This.
func (a FunctionCallbackArguments) ThisValue() (Value, error) {
	frame, err := a.checkedFrame()
	if err != nil {
		return Value{}, err
	}
	if frame.thisWire == 0 {
		return Value{}, fmt.Errorf("gov8: callback has no receiver")
	}
	return a.cs.wrap(frame.thisWire), nil
}

// NewTarget returns new.target: the constructor function for construct
// calls, undefined for plain calls.
func (a FunctionCallbackArguments) NewTarget() (Value, error) {
	frame, err := a.checkedFrame()
	if err != nil {
		return Value{}, err
	}
	if frame.newTargetWire == 0 {
		return a.cs.sc.Undefined()
	}
	return a.cs.wrap(frame.newTargetWire), nil
}

// Data returns the callback data attached when the function (template) was
// created; undefined when none was attached.
func (a FunctionCallbackArguments) Data() (Value, error) {
	frame, err := a.checkedFrame()
	if err != nil {
		return Value{}, err
	}
	if frame.dataWire == 0 {
		return a.cs.sc.Undefined()
	}
	return a.cs.wrap(frame.dataWire), nil
}

// PropertyCallbackArguments mirrors v8::PropertyCallbackArguments for
// accessor callbacks and property interceptors: Holder is the object the
// property lives on, This is the receiver the operation was invoked on, Data
// is the callback data, and ShouldThrowOnError reports the strict-mode
// verdict (always false on kinds that do not carry it).
type PropertyCallbackArguments struct {
	cs    *CallbackScope
	frame *hostCallbackFrame
}

// Property returns the Name supplied to a named accessor callback. It is not
// available for indexed interceptor callbacks.
func (a PropertyCallbackArguments) Property() (Value, error) {
	if a.frame.propertyWire == 0 {
		return Value{}, fmt.Errorf("gov8: property callback has no named property")
	}
	return a.cs.wrap(a.frame.propertyWire), nil
}

// holderWire returns the wire captured for Holder(): interceptor trampolines
// record it in holder_wire, the older accessor trampolines in this_wire.
func (a PropertyCallbackArguments) holderWire() uintptr {
	if a.frame.holderWire != 0 {
		return a.frame.holderWire
	}
	return a.frame.thisWire
}

// This returns the receiver the property operation was invoked on. v8 152's
// PropertyCallbackInfo exposes only Holder() (there is no separate This()
// accessor), so the shim captures the holder in both frame slots and this
// observation equals Holder() for property callbacks.
func (a PropertyCallbackArguments) This() (*Object, error) {
	if a.frame.thisWire == 0 {
		return nil, fmt.Errorf("gov8: property callback has no receiver")
	}
	return &Object{a.cs.wrap(a.frame.thisWire)}, nil
}

// Holder returns the object holding the intercepted property.
func (a PropertyCallbackArguments) Holder() (*Object, error) {
	wire := a.holderWire()
	if wire == 0 {
		return nil, fmt.Errorf("gov8: accessor callback has no holder")
	}
	return &Object{a.cs.wrap(wire)}, nil
}

// ShouldThrowOnError mirrors the pinned crate's
// PropertyCallbackArguments::should_throw_on_error. The pinned binding
// (src/binding.cc, v8 =152.2.0) instantiates
// v8::PropertyCallbackInfo<v8::Value>::ShouldThrowOnError(), whose
// `if constexpr (!HasShouldThrowOnError()) return false;` path makes the
// observation compile-time false on this build — including strict-mode
// stores (characterized by the tpladv fixture's "strict=false" entries).
// The raw engine bit is still captured in the dispatch frame; the Go
// surface intentionally reports the pinned crate's observable verdict.
func (a PropertyCallbackArguments) ShouldThrowOnError() bool {
	return false
}

// Data returns the accessor's/handler's callback data; undefined when none.
func (a PropertyCallbackArguments) Data() (Value, error) {
	if a.frame.dataWire == 0 {
		return a.cs.sc.Undefined()
	}
	return a.cs.wrap(a.frame.dataWire), nil
}

// propertyDescriptorFromFrame builds the Go view of the v8::PropertyDescriptor
// snapshot the trampoline captured for definer callbacks.
func propertyDescriptorFromFrame(cs *CallbackScope, frame *hostCallbackFrame) CallbackPropertyDescriptor {
	d := CallbackPropertyDescriptor{flags: frame.pdFlags}
	if frame.pdFlags&16 != 0 {
		d.getter = cs.wrap(frame.newTargetWire)
	}
	if frame.pdFlags&32 != 0 {
		d.setter = cs.wrap(frame.valueWire)
	}
	if frame.pdFlags&pdFlagHasValue != 0 {
		d.hasValue = true
		d.value = cs.wrap(frame.pdValueWire)
	}
	d.writable = frame.pdWritable != 0
	d.enumerable = frame.pdEnumerable != 0
	d.configurable = frame.pdConfigurable != 0
	return d
}

// ReturnValue receives the callback's JS return value. It is bound to the
// running callback; the engine pre-seeds it with undefined, so a callback
// that sets nothing returns undefined.
type ReturnValue struct {
	cs    *CallbackScope
	frame *hostCallbackFrame
}

var (
	callbackScalarProcsOnce   sync.Once
	callbackIntegerValueAddr  uintptr
	returnValueInt32ProcAddr  uintptr
	returnValueUint32ProcAddr uintptr
	returnValueSetterAddrs    [returnValueEmptyString + 1]uintptr
)

type returnValueSetter uint8

const (
	returnValueArbitrary returnValueSetter = iota
	returnValueDouble
	returnValueBool
	returnValueNull
	returnValueUndefined
	returnValueEmptyString
)

const (
	callbackDeferredRVInt32Magic int32 = 0x47565231 // "GVR1"
	callbackDeferredRVNone       int32 = 0
	callbackDeferredRVInt32      int32 = 1
	callbackInt32ArgsMagic       int32 = 0x47564131 // "GVA1"
	callbackInt32Arg0Valid       int32 = 1 << 0
	callbackInt32Arg1Valid       int32 = 1 << 1
)

// cachedInt32Argument returns a native positive IsInt32 snapshot only when v
// is the exact local-handle wire captured for one of the first two function
// arguments. Equal values in other local slots do not match. Negative type
// results are never cached, so every conversion of a string, fractional
// number, object, proxy, or other non-Int32 value keeps taking V8's coercive
// path and preserves repeated conversion side effects.
//
// The caller must validate callback lifetime and thread affinity first: frame
// and argv are owned by the synchronous native trampoline's stack.
func (cs *CallbackScope) cachedInt32Argument(v Value) (int64, bool) {
	frame := cs.frame
	if frame == nil || frame.kind != cbKindFunction ||
		frame.pdConfigurable != callbackInt32ArgsMagic || frame.argv == nil {
		return 0, false
	}
	argc := int(frame.argc)
	if argc <= 0 {
		return 0, false
	}
	if argc > 2 {
		argc = 2
	}
	argv := unsafe.Slice((*uintptr)(frame.argv), argc)
	if frame.pdFlags&callbackInt32Arg0Valid != 0 && argv[0] == v.h {
		return int64(frame.pdWritable), true
	}
	if argc > 1 && frame.pdFlags&callbackInt32Arg1Valid != 0 && argv[1] == v.h {
		return int64(frame.pdEnumerable), true
	}
	return 0, false
}

func resolveCallbackScalarProcs() {
	callbackIntegerValueAddr = proc("gov8_wctx_integer_value_direct").Addr()
	returnValueInt32ProcAddr = proc("gov8_rv_set_int32").Addr()
	returnValueUint32ProcAddr = proc("gov8_rv_set_uint32").Addr()
	names := [...]string{
		"gov8_rv_set",
		"gov8_rv_set_double",
		"gov8_rv_set_bool",
		"gov8_rv_set_null",
		"gov8_rv_set_undefined",
		"gov8_rv_set_empty_string",
	}
	for index, name := range names {
		returnValueSetterAddrs[index] = proc(name).Addr()
	}
}

// checkedFrame proves callback lifetime and owner-thread affinity before a
// ReturnValue operation dereferences the native callback frame. The frame is
// stack-owned by the C++ trampoline and may remain referenced by a retained
// ReturnValue after dispatch; the borrowed Scope is invalidated first so such
// a retained value fails here without reading stale native memory.
func (rv ReturnValue) checkedFrame() (*hostCallbackFrame, error) {
	if err := rv.cs.sc.check(); err != nil {
		return nil, err
	}
	if rv.frame == nil {
		return nil, fmt.Errorf("gov8: invalid callback return value")
	}
	return rv.frame, nil
}

func deferredInt32Capable(frame *hostCallbackFrame) bool {
	return frame.kind == cbKindFunction && frame.pdPad == callbackDeferredRVInt32Magic
}

func (rv ReturnValue) setInt32Native(frame *hostCallbackFrame, value int32) error {
	callbackScalarProcsOnce.Do(resolveCallbackScalarProcs)
	r1, _, _ := syscall.Syscall(returnValueInt32ProcAddr, 2,
		frame.rvWord, uintptr(value), 0)
	if int64(r1) < 0 {
		return shimError("gov8_rv_set_int32", r1)
	}
	return nil
}

// materializeDeferredInt32 applies a pending function-callback integer before
// an operation that must observe or supersede it. Non-function callbacks and
// older pre-GVR1 DLLs carry no capability marker and stay on the legacy path.
func (rv ReturnValue) materializeDeferredInt32(frame *hostCallbackFrame) error {
	if !deferredInt32Capable(frame) {
		return nil
	}
	switch frame.outIntercepted {
	case callbackDeferredRVNone:
		return nil
	case callbackDeferredRVInt32:
		if err := rv.setInt32Native(frame, int32(frame.index)); err != nil {
			// The previously accepted SetInt32 remains pending and is still the
			// last successful write if this defensive native error is surfaced.
			return err
		}
		frame.outIntercepted = callbackDeferredRVNone
		return nil
	default:
		return fmt.Errorf("gov8: invalid deferred callback return state")
	}
}

func (rv ReturnValue) setFixed(op string, kind returnValueSetter, value uintptr) error {
	frame, err := rv.checkedFrame()
	if err != nil {
		return err
	}
	if err := rv.materializeDeferredInt32(frame); err != nil {
		return err
	}
	callbackScalarProcsOnce.Do(resolveCallbackScalarProcs)
	address := returnValueSetterAddrs[kind]
	var r1 uintptr
	if kind >= returnValueNull {
		r1, _, _ = syscall.Syscall(address, 1, frame.rvWord, 0, 0)
	} else {
		r1, _, _ = syscall.Syscall(address, 2, frame.rvWord, value, 0)
	}
	if int64(r1) < 0 {
		return shimError(op, r1)
	}
	return nil
}

// Set stores an arbitrary value as the callback result.
func (rv ReturnValue) Set(v Value) error {
	if err := v.check(); err != nil {
		return err
	}
	return rv.setFixed("gov8_rv_set", returnValueArbitrary, v.h)
}

// SetInt32 stores an int32 (surfacing as a JS number).
func (rv ReturnValue) SetInt32(v int32) error {
	frame, err := rv.checkedFrame()
	if err != nil {
		return err
	}
	if deferredInt32Capable(frame) {
		frame.index = uint32(v)
		frame.outIntercepted = callbackDeferredRVInt32
		return nil
	}
	return rv.setInt32Native(frame, v)
}

// SetUint32 stores a uint32 (surfacing as a JS number).
func (rv ReturnValue) SetUint32(v uint32) error {
	frame, err := rv.checkedFrame()
	if err != nil {
		return err
	}
	if err := rv.materializeDeferredInt32(frame); err != nil {
		return err
	}
	callbackScalarProcsOnce.Do(resolveCallbackScalarProcs)
	r1, _, _ := syscall.Syscall(returnValueUint32ProcAddr, 2,
		frame.rvWord, uintptr(v), 0)
	if int64(r1) < 0 {
		return shimError("gov8_rv_set_uint32", r1)
	}
	return nil
}

// SetFloat64 stores a float64 (surfacing as a JS number).
func (rv ReturnValue) SetFloat64(v float64) error {
	return rv.setFixed("gov8_rv_set_double", returnValueDouble, uintptr(math.Float64bits(v)))
}

// SetBool stores a boolean.
func (rv ReturnValue) SetBool(v bool) error {
	u := uintptr(0)
	if v {
		u = 1
	}
	return rv.setFixed("gov8_rv_set_bool", returnValueBool, u)
}

// SetNull stores null.
func (rv ReturnValue) SetNull() error {
	return rv.setFixed("gov8_rv_set_null", returnValueNull, 0)
}

// SetUndefined stores undefined.
func (rv ReturnValue) SetUndefined() error {
	return rv.setFixed("gov8_rv_set_undefined", returnValueUndefined, 0)
}

// SetEmptyString stores the empty string.
func (rv ReturnValue) SetEmptyString() error {
	return rv.setFixed("gov8_rv_set_empty_string", returnValueEmptyString, 0)
}

// Get reads back the value currently held by the return value: undefined
// when nothing was set, otherwise the last value stored by a setter
// (v8 ReturnValue::Get). The result is bound to the running callback's
// scope and must not outlive it.
func (rv ReturnValue) Get() (Value, error) {
	frame, err := rv.checkedFrame()
	if err != nil {
		return Value{}, err
	}
	if err := rv.materializeDeferredInt32(frame); err != nil {
		return Value{}, err
	}
	h, err := callHandle("ReturnValue.Get", proc("gov8_rv_get"), frame.rvWord)
	if err != nil {
		return Value{}, err
	}
	if h == 0 {
		// The engine pre-seeds the slot with undefined; an empty local would
		// only appear for a mis-typed word, and undefined is the documented
		// unset observation.
		return rv.cs.sc.Undefined()
	}
	return rv.cs.wrap(h), nil
}

// ReleaseIsolateHostState releases host state attached to the isolate by
// host features: native-callback registrations (Go registry and the shim's
// per-isolate callback contexts, including their Global embedder-data handles),
// Wasm streaming bindings, and isolate slot values.
//
// This is the explicit Go equivalent of the Rust destructors that run when
// the isolate is dropped: Go has no destructors, and a finalizer would run
// after engine teardown where calling it would be unsafe. Call it on the
// owning thread after all engine work is done and before Isolate.Close.
// It is safe to call twice.
func ReleaseIsolateHostState(i *Isolate) error {
	if err := i.check(); err != nil {
		return err
	}
	if err := requireInitialized(); err != nil {
		return err
	}
	// Inspector teardown invokes V8 and therefore cannot be synthesized by
	// this host-registry cleanup. Refuse before mutating any registry so the
	// caller can close sessions, unregister contexts, and close inspectors in
	// that order while the isolate is still alive.
	if err := inspectorIsolateCloseError(i); err != nil {
		return err
	}
	if err := releaseInspectorInspectableHostState(i); err != nil {
		return err
	}
	// Wasm owns callbacks that may still target this isolate. Refuse release
	// before mutating the other host registries when a stream or asynchronous
	// compilation is still active; otherwise clear the native binding while
	// the isolate is alive.
	if err := releaseWasmStreamingHostState(i); err != nil {
		return err
	}
	if err := releaseModuleAdvancedHostState(i); err != nil {
		return err
	}
	// Drop the Go-side registrations and slot values first (the registries are
	// pure Go), then free the shim-side dispatch contexts (which resets
	// their Global embedder-data handles while the isolate still lives).
	releaseCHIsolateEntries(i)
	hostCallbackRegistry.mu.Lock()
	var handles []uint64
	var contexts []uintptr
	var isoHandle uintptr
	for h, e := range hostCallbackRegistry.entries {
		if e.iso == i {
			handles = append(handles, h)
			if e.ctx != 0 {
				contexts = append(contexts, e.ctx)
				isoHandle = e.iso.handle
			}
		}
	}
	for _, h := range handles {
		delete(hostCallbackRegistry.entries, h)
		dropFastHostCallback(h)
	}
	for key := range hostCallbackRegistry.lazyGetters {
		if key.iso == i {
			delete(hostCallbackRegistry.lazyGetters, key)
		}
	}
	hostCallbackRegistry.mu.Unlock()

	if len(contexts) > 0 {
		argv := make([]uintptr, len(contexts))
		copy(argv, contexts)
		if err := callErr("HostContexts.Release",
			proc("gov8_host_contexts_release"), isoHandle,
			uintptr(unsafe.Pointer(&argv[0])), uintptr(len(argv))); err != nil {
			return err
		}
	}
	// Once every native dispatch context has been released, no stale native
	// handle can address a registry entry. Reuse the empty fast table from its
	// first slot instead of growing its immutable chunk directory forever
	// across explicit release/rebuild cycles.
	hostCallbackRegistry.mu.Lock()
	if len(hostCallbackRegistry.entries) == 0 {
		hostCallbackRegistry.next = 0
	}
	hostCallbackRegistry.mu.Unlock()

	releaseSlots(i)

	return nil
}
