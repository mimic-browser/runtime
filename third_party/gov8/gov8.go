//go:build (windows || linux) && amd64

package gov8

import (
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"unsafe"
)

// V8 version constants reported by the pinned engine build.
type Version struct {
	Major, Minor, Build, Patch int32
}

// Platform lifecycle states, mirroring the pinned crate's global state
// machine (Uninitialized -> Initialized -> Disposed -> PlatformShutdown).
// The crate panics on invalid transitions; this API returns errors instead —
// the only intentional deviation, because panics are not idiomatic for
// recoverable misuse in Go. Everything else matches observably.
type platformState int32

const (
	stateUninitialized platformState = iota
	stateInitialized
	stateDisposed
	statePlatformShutdown
)

var platform platformState

// lifecycleMu serializes the process-global transitions (Initialize,
// Dispose, DisposePlatform) against isolate creation and destruction. It
// guarantees that no isolate can be created across teardown: NewIsolate
// holds the lock while the engine allocates the isolate and registers it in
// liveIsolates, and Dispose refuses to run while liveIsolates is non-empty.
// The lock is deliberately NOT taken on hot per-value paths; those read the
// platform state through an atomic load only.
var lifecycleMu sync.Mutex

// liveIsolates tracks every isolate created but not yet closed. It is only
// accessed while holding lifecycleMu. The map (rather than a bare counter)
// keeps the accounting exact under Close error paths and makes the Dispose
// refusal message report the real number of leaked isolates.
var liveIsolates = make(map[*Isolate]struct{})

// ErrNotInitialized is returned when engine work is attempted before
// Initialize.
var ErrNotInitialized = errors.New("gov8: v8 platform is not initialized")

// loadPlatform reads the process-global platform state atomically so hot
// paths stay lock-free.
func loadPlatform() platformState {
	return platformState(atomic.LoadInt32((*int32)(&platform)))
}

// storePlatform writes the process-global platform state atomically. Callers
// mutate it only while holding lifecycleMu; the atomic store keeps
// concurrent lock-free readers well-defined.
func storePlatform(s platformState) {
	atomic.StoreInt32((*int32)(&platform), int32(s))
}

// Initialize installs the platform selected by ConfigurePlatform, calls
// V8::Initialize, and prepares the default ArrayBuffer allocator. With no
// explicit selection it preserves the original default configuration (worker
// count 0, idle tasks disabled). It must be called exactly once per process;
// invalid lifecycle transitions are returned as errors.
func Initialize() error {
	if err := loadShim(); err != nil {
		return err
	}
	lifecycleMu.Lock()
	defer lifecycleMu.Unlock()
	if loadPlatform() != stateUninitialized {
		return fmt.Errorf("gov8: invalid global state: Initialize called in state %d", loadPlatform())
	}
	if err := cppgcBeforeV8Initialize(); err != nil {
		return err
	}
	if err := initializeSelectedPlatform(); err != nil {
		return err
	}
	storePlatform(stateInitialized)
	return nil
}

// PlatformPresent reports whether a platform has been installed by this
// process (same observable behavior as the oracle's get_current_platform
// presence check).
func PlatformPresent() bool {
	if loadShim() != nil || loadPlatform() != stateInitialized {
		return false
	}
	r1, _, _ := proc("gov8_platform_present").Call()
	return r1 == 1
}

// Dispose calls V8::Dispose. Valid only in the Initialized state and only
// when no isolates are live (V8 requires all isolates to be destroyed
// before V8::Dispose, matching the oracle's dispose semantics); otherwise it
// returns an error without touching the engine. After a successful Dispose
// no isolates may be used (the Go wrapper enforces this) and DisposePlatform
// must follow.
//
// Dispose is synchronized against NewIsolate: a concurrent NewIsolate either
// registers its isolate first (Dispose then fails with a live-isolate error)
// or observes the state transition and fails — an isolate can never be
// created across teardown.
func Dispose() (bool, error) {
	lifecycleMu.Lock()
	defer lifecycleMu.Unlock()
	if loadPlatform() != stateInitialized {
		return false, fmt.Errorf("gov8: invalid global state: Dispose called in state %d", loadPlatform())
	}
	if n := len(liveIsolates); n > 0 {
		return false, fmt.Errorf("gov8: invalid global state: Dispose with %d live isolate(s); close all isolates first", n)
	}
	storePlatform(stateDisposed)
	r1, _, _ := proc("gov8_v8_dispose").Call()
	if int64(r1) < 0 {
		storePlatform(stateInitialized)
		return false, shimError("Dispose", r1)
	}
	return r1 == 1, nil
}

// DisposePlatform calls V8::DisposePlatform and releases the platform
// created by Initialize. Valid only after Dispose.
func DisposePlatform() error {
	lifecycleMu.Lock()
	defer lifecycleMu.Unlock()
	if loadPlatform() != stateDisposed {
		return fmt.Errorf("gov8: invalid global state: DisposePlatform called in state %d", loadPlatform())
	}
	storePlatform(statePlatformShutdown)
	if err := callErr("DisposePlatform", proc("gov8_v8_dispose_platform")); err != nil {
		storePlatform(stateDisposed)
		return err
	}
	return nil
}

// Shutdown runs the full teardown in the pinned order: Dispose followed by
// DisposePlatform. All isolates must be closed beforehand.
func Shutdown() error {
	if _, err := Dispose(); err != nil {
		return err
	}
	return DisposePlatform()
}

// EngineVersion returns the engine version constants (15.2.124.1 for the
// pinned build).
func EngineVersion() (Version, error) {
	if err := loadShim(); err != nil {
		return Version{}, err
	}
	var out [4]int32
	r1, _, _ := proc("gov8_version").Call(uintptr(unsafe.Pointer(&out[0])))
	if int64(r1) < 0 {
		return Version{}, shimError("EngineVersion", r1)
	}
	return Version{Major: out[0], Minor: out[1], Build: out[2], Patch: out[3]}, nil
}

// VersionString returns the compile-time version string of the pinned
// engine, "15.2.124.1-rusty" (the -rusty suffix is the crate's embedder
// marker).
func VersionString() (string, error) {
	if err := loadShim(); err != nil {
		return "", err
	}
	return shimStringCall(func(p unsafe.Pointer, c uintptr) uintptr {
		r, _, _ := proc("gov8_version_string").Call(uintptr(p), c)
		return r
	})
}

// RuntimeVersionString returns V8::GetVersion() from the loaded engine.
func RuntimeVersionString() (string, error) {
	if err := loadShim(); err != nil {
		return "", err
	}
	return shimStringCall(func(p unsafe.Pointer, c uintptr) uintptr {
		r, _, _ := proc("gov8_runtime_version").Call(uintptr(p), c)
		return r
	})
}

func shimStringCall(fn func(p unsafe.Pointer, c uintptr) uintptr) (string, error) {
	var buf [128]byte
	// Keep the buffer as a pointer across the Go callback. Converting it to
	// uintptr before calling fn can leave native code with a stale stack address
	// if the callback grows the goroutine stack.
	r := fn(unsafe.Pointer(&buf[0]), uintptr(len(buf)))
	if int64(r) < 0 {
		return "", shimError("version string", r)
	}
	n := int(r)
	if n > len(buf) {
		n = len(buf)
	}
	return string(buf[:n]), nil
}

func requireInitialized() error {
	if loadShim() != nil {
		return shimLoadErr
	}
	if loadPlatform() != stateInitialized {
		return ErrNotInitialized
	}
	return nil
}

// beginIsolateCreate blocks teardown while an isolate is being created. It
// must be paired with exactly one of finishIsolateCreate (success) or
// abandonIsolateCreate (failure).
func beginIsolateCreate() error {
	lifecycleMu.Lock()
	if loadPlatform() != stateInitialized {
		lifecycleMu.Unlock()
		return ErrNotInitialized
	}
	return nil
}

// finishIsolateCreate registers the newly created isolate so Dispose cannot
// run while it is live.
func finishIsolateCreate(i *Isolate) {
	liveIsolates[i] = struct{}{}
	lifecycleMu.Unlock()
}

// abandonIsolateCreate releases the teardown lock after a failed creation.
func abandonIsolateCreate() {
	lifecycleMu.Unlock()
}

// unregisterIsolate removes a closed isolate from the live set so a later
// Dispose can proceed. It tolerates unknown isolates (double protection).
func unregisterIsolate(i *Isolate) {
	lifecycleMu.Lock()
	delete(liveIsolates, i)
	lifecycleMu.Unlock()
}

// foreignIsolate builds the uniform error for cross-isolate misuse detected
// in the wrapper before any shim call.
func foreignIsolate(what string) error {
	return fmt.Errorf("gov8: %s belongs to a different isolate", what)
}

// quoteThreadID formats a thread id for affinity error messages.
func quoteThreadID(tid uint32) string { return strconv.FormatUint(uint64(tid), 10) }

// keep runtime import meaningful even if future refactors drop explicit uses
var _ = runtime.LockOSThread
