//go:build (windows || linux) && amd64

package v8

import (
	"context"
	"errors"
	"os"
	"runtime"
	"sync"
	"time"

	gov8 "github.com/maclof/gov8"
	"github.com/moreveal/mimic/internal/engine"
)

func (Factory) BootstrapSnapshotsEnabled() bool {
	return os.Getenv("MIMIC_DIAGNOSTICS") != "1" || os.Getenv("MIMIC_PROFILE_HOSTS") == "1"
}

func (Factory) BootstrapSnapshotIdentity() (string, error) {
	build, err := gov8.VersionString()
	if err != nil {
		return "", err
	}
	runtimeVersion, err := gov8.RuntimeVersionString()
	if err != nil {
		return "", err
	}
	nativeIdentity, err := gov8.NativeLibraryIdentity()
	if err != nil {
		return "", err
	}
	return build + "; bootstrap-layout=bare-default+platform-context" + "\x00" + runtimeVersion + "\x00" + nativeIdentity, nil
}

func (Factory) LoadBootstrapSnapshot(data []byte) (engine.BootstrapSnapshot, error) {
	if len(data) == 0 {
		return nil, errors.New("empty bootstrap snapshot")
	}
	if _, err := initialize(); err != nil {
		return nil, err
	}
	blob := gov8.StartupDataFromBytes(data)
	// Validate through an actual consumer creation here, before the artifact is
	// admitted to the Browser cache. This turns corrupt cache files into a
	// recoverable rebuild instead of exposing them to a Page.
	consumer, err := blob.ShareImmutableBytes()
	if err != nil {
		_ = blob.Release()
		return nil, err
	}
	owner, err := newRuntime(consumer)
	if err != nil {
		_ = consumer.Release()
		_ = blob.Release()
		return nil, err
	}
	adapter, err := newAdapter(owner, nil)
	if err != nil {
		_ = owner.Dispose()
		_ = blob.Release()
		return nil, err
	}
	if err := adapter.Close(); err != nil {
		_ = blob.Release()
		return nil, err
	}
	return &bootstrapSnapshot{blob: blob, size: len(data)}, nil
}

type bootstrapSnapshot struct {
	mu   sync.Mutex
	blob *gov8.StartupData
	size int
}

// BuildBootstrapSnapshot serializes realm-owned initialization, including the
// portable property observer callbacks registered as external references.
// Go host bindings belong to each restored runtime and are installed later.
func (Factory) BuildBootstrapSnapshot(ctx context.Context, sources ...string) (engine.BootstrapSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := initialize(); err != nil {
		return nil, err
	}
	type result struct {
		blob *gov8.StartupData
		err  error
	}
	done := make(chan result, 1)
	// The creator owns/locks this goroutine's OS thread. The caller can be an
	// existing Page owner; creating the snapshot must not change its V8 stack.
	go func() { blob, err := buildBootstrapSnapshot(ctx, sources...); done <- result{blob, err} }()
	value := <-done
	if value.err != nil {
		return nil, value.err
	}
	return &bootstrapSnapshot{blob: value.blob, size: len(value.blob.Bytes())}, nil
}

func buildBootstrapSnapshot(ctx context.Context, sources ...string) (blob *gov8.StartupData, err error) {
	// SnapshotCreator releases its own thread pin when consuming the creator.
	// Keep an outer pin until the caller's thread policy has been restored: a
	// synchronously awaited builder is as latency-sensitive as the Page owner.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if restore := configurePageThreadPolicy(); restore != nil {
		defer func() {
			if restoreErr := restore(); restoreErr != nil {
				if blob != nil {
					_ = blob.Release()
					blob = nil
				}
				err = errors.Join(err, restoreErr)
			}
		}()
	}
	references, err := bootstrapNativeReferences()
	if err != nil {
		return nil, err
	}
	creator, err := gov8.NewSnapshotCreatorWithExternalReferences(references)
	if err != nil {
		return nil, err
	}
	consumed := false
	defer func() {
		if !consumed {
			_ = creator.Close()
		}
	}()
	iso := creator.Isolate()
	if err := iso.SetMicrotasksPolicy(gov8.PolicyExplicit); err != nil {
		return nil, err
	}
	realm, err := iso.NewContext()
	if err != nil {
		return nil, err
	}
	scope, err := iso.NewScope()
	if err != nil {
		_ = realm.Close()
		return nil, err
	}
	factory, err := iso.NewPropertyObservationFactory(scope, realm)
	if err == nil {
		var global *gov8.Object
		global, err = realm.GlobalObject(scope)
		if err == nil {
			_, err = global.SetByName(scope, realm, "__mimicPropertyObservationFactory", factory)
		}
	}
	if err != nil {
		_ = scope.Close()
		_ = realm.Close()
		return nil, err
	}
	dispatchFactory, err := iso.NewReceiverDispatchFactory(scope, realm)
	if err == nil {
		var global *gov8.Object
		global, err = realm.GlobalObject(scope)
		if err == nil {
			_, err = global.SetByName(scope, realm, "__mimicReceiverDispatchFactory", dispatchFactory)
		}
	}
	if err != nil {
		_ = scope.Close()
		_ = realm.Close()
		return nil, err
	}
	exceptionFactory, err := iso.NewExceptionStateFactory(scope, realm)
	if err == nil {
		var global *gov8.Object
		global, err = realm.GlobalObject(scope)
		if err == nil {
			_, err = global.SetByName(scope, realm, "__mimicExceptionStateFactory", exceptionFactory)
		}
	}
	if err != nil {
		_ = scope.Close()
		_ = realm.Close()
		return nil, err
	}
	for _, source := range sources {
		err = runSnapshotSeed(ctx, iso, realm, scope, source)
		if err != nil {
			break
		}
	}
	if err == nil {
		// Seed closures may retain the factories, but transport globals must not
		// become extra observable properties of an otherwise ordinary context.
		err = runSnapshotSeed(ctx, iso, realm, scope, `delete globalThis.__mimicPropertyObservationFactory; delete globalThis.__mimicReceiverDispatchFactory; delete globalThis.__mimicExceptionStateFactory;`)
	}
	if err == nil {
		// Fresh sibling realms must start with native intrinsics, rather than
		// inherit another document's serialized platform state. Store the seed
		// as the additional context and retain a bare default context.
		var bare *gov8.Context
		bare, err = iso.NewContext()
		if err == nil {
			err = creator.SetDefaultContext(bare)
			if err == nil {
				var index int
				index, err = creator.AddContext(realm)
				if err == nil && index != 0 {
					err = errors.New("unexpected platform context snapshot index")
				}
			}
			err = errors.Join(err, bare.Close())
		}
	}
	err = errors.Join(err, scope.Close(), realm.Close())
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	// Serialization is a bounded native phase. Cancellation is checked again
	// after it finishes; never terminate the serializer midway through cleanup.
	blob, err = creator.CreateBlob(gov8.FunctionCodeKeep)
	consumed = true // CreateBlob consumes its creator, including native failures.
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		_ = blob.Release()
		return nil, err
	}
	return blob, nil
}

func runSnapshotSeed(ctx context.Context, iso *gov8.Isolate, realm *gov8.Context, scope *gov8.Scope, source string) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	if ctx.Done() != nil {
		finished, joined := make(chan struct{}), make(chan struct{})
		handle := iso.ThreadSafeHandle()
		go func() {
			defer close(joined)
			select {
			case <-ctx.Done():
				handle.TerminateExecution()
			case <-finished:
			}
		}()
		defer func() {
			close(finished)
			<-joined
			if ctx.Err() != nil {
				_ = iso.CancelTerminateExecution()
				err = ctx.Err()
			}
		}()
	}
	catcher, err := iso.NewTryCatch()
	if err != nil {
		return err
	}
	defer catcher.Close()
	script, err := realm.CompilePlatformSeed(scope, source, catcher)
	if err != nil {
		return exceptionError(catcher, scope, realm, "bootstrap snapshot", err)
	}
	defer script.Close()
	if _, err = script.Run(scope, catcher); err != nil {
		return exceptionError(catcher, scope, realm, "bootstrap snapshot", err)
	}
	return nil
}

func (s *bootstrapSnapshot) NewRuntime() (engine.Runtime, error) {
	owner, profile, err := s.newRuntimeOwner()
	if err != nil {
		return nil, err
	}
	return newAdapter(owner, profile)
}

func (s *bootstrapSnapshot) NewBareRuntime() (engine.Runtime, error) {
	owner, profile, err := s.newRuntimeOwner()
	if err != nil {
		return nil, err
	}
	return newBareAdapterWithRelease(owner, profile, owner.Dispose)
}

func (s *bootstrapSnapshot) newRuntimeOwner() (*Runtime, *diagnosticState, error) {
	s.mu.Lock()
	if s.blob == nil {
		s.mu.Unlock()
		return nil, nil, errors.New("bootstrap snapshot is closed")
	}
	// Every runtime owns its native consumer records, so teardown releases its
	// native blob even while other Pages live. The serialized Go bytes are
	// immutable and can share their backing storage with the cache and siblings.
	consumer, err := s.blob.ShareImmutableBytes()
	s.mu.Unlock()
	if err != nil {
		return nil, nil, err
	}
	profile := newDiagnostics()
	started := time.Now()
	owner, err := newRuntime(consumer)
	if err != nil {
		_ = consumer.Release()
		return nil, nil, err
	}
	if profile != nil {
		profile.Costs["factory:isolate"] = diagnosticCost{Count: 1, Nanoseconds: time.Since(started).Nanoseconds()}
	}
	return owner, profile, nil
}

type bootstrapRuntimeLane struct {
	owner  *Runtime
	active int
}

type bootstrapRuntimePool struct {
	mu       sync.Mutex
	snapshot *bootstrapSnapshot
	max      int
	lanes    []*bootstrapRuntimeLane
	closed   bool
}

func (s *bootstrapSnapshot) NewRuntimePool(maxRealmsPerIsolate int) engine.RuntimePool {
	if maxRealmsPerIsolate < 1 {
		maxRealmsPerIsolate = 1
	}
	return &bootstrapRuntimePool{snapshot: s, max: maxRealmsPerIsolate}
}

func (p *bootstrapRuntimePool) NewRuntime() (engine.Runtime, error) {
	return p.newRuntime(false)
}

func (p *bootstrapRuntimePool) NewBareRuntime() (engine.Runtime, error) {
	return p.newRuntime(true)
}

func (p *bootstrapRuntimePool) newRuntime(bare bool) (engine.Runtime, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, errors.New("bootstrap runtime pool is closed")
	}
	var lane *bootstrapRuntimeLane
	for _, candidate := range p.lanes {
		if candidate.active < p.max {
			lane = candidate
			break
		}
	}
	var profile *diagnosticState
	if lane == nil {
		owner, diagnostics, err := p.snapshot.newRuntimeOwner()
		if err != nil {
			p.mu.Unlock()
			return nil, err
		}
		lane = &bootstrapRuntimeLane{owner: owner}
		profile = diagnostics
		p.lanes = append(p.lanes, lane)
	} else {
		profile = newDiagnostics()
	}
	lane.active++
	p.mu.Unlock()

	release := func() error {
		return p.release(lane)
	}
	if bare {
		return newBareAdapterWithRelease(lane.owner, profile, release)
	}
	return newAdapterWithRelease(lane.owner, profile, release)
}

func (p *bootstrapRuntimePool) release(lane *bootstrapRuntimeLane) error {
	p.mu.Lock()
	if lane.active > 0 {
		lane.active--
	}
	// The snapshot remains cached, but an idle isolate retains Page memory.
	// Release the owner as soon as its last realm closes.
	dispose := lane.active == 0
	if dispose {
		for i, candidate := range p.lanes {
			if candidate == lane {
				p.lanes = append(p.lanes[:i], p.lanes[i+1:]...)
				break
			}
		}
	}
	p.mu.Unlock()
	if dispose {
		return lane.owner.Dispose()
	}
	return nil
}

func (p *bootstrapRuntimePool) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	var idle []*Runtime
	kept := p.lanes[:0]
	for _, lane := range p.lanes {
		if lane.active == 0 {
			idle = append(idle, lane.owner)
		} else {
			kept = append(kept, lane)
		}
	}
	p.lanes = kept
	p.mu.Unlock()
	var err error
	for _, owner := range idle {
		err = errors.Join(err, owner.Dispose())
	}
	return err
}

func (s *bootstrapSnapshot) SizeBytes() int { return s.size }
func (s *bootstrapSnapshot) BootstrapSnapshotBytes() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.blob == nil {
		return nil
	}
	data := s.blob.Bytes()
	return append([]byte(nil), data...)
}
func (s *bootstrapSnapshot) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.blob == nil {
		return nil
	}
	err := s.blob.Release()
	if err == nil {
		s.blob = nil
	}
	return err
}

var _ engine.BootstrapSnapshotFactory = Factory{}
var _ engine.PersistentBootstrapSnapshotFactory = Factory{}
var _ engine.BootstrapSnapshot = (*bootstrapSnapshot)(nil)
var _ engine.PersistentBootstrapSnapshot = (*bootstrapSnapshot)(nil)
var _ engine.RuntimePoolSnapshot = (*bootstrapSnapshot)(nil)
var _ engine.RuntimePool = (*bootstrapRuntimePool)(nil)
