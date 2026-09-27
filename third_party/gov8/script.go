//go:build (windows || linux) && amd64

package gov8

import (
	"fmt"
	syscall "github.com/maclof/gov8/internal/native"
	"runtime"
	"sync"
	"unsafe"
)

var (
	scriptHotProcsOnce sync.Once
	scriptCompileAddr  uintptr
	scriptRunAddr      uintptr
	scriptDisposeAddr  uintptr
)

func ensureScriptHotProcs() {
	scriptHotProcsOnce.Do(func() {
		scriptCompileAddr = proc("gov8_script_compile").Addr()
		scriptRunAddr = proc("gov8_script_run").Addr()
		scriptDisposeAddr = proc("gov8_script_dispose").Addr()
	})
}

//go:uintptrescapes
func scriptEscapingSyscall6(trap, nargs, a1, a2, a3, a4, a5, a6 uintptr) (uintptr, uintptr, syscall.Errno) {
	return syscall.Syscall6(trap, nargs, a1, a2, a3, a4, a5, a6)
}

//go:uintptrescapes
func scriptEscapingSyscall9(trap, nargs, a1, a2, a3, a4, a5, a6, a7, a8, a9 uintptr) (uintptr, uintptr, syscall.Errno) {
	return syscall.Syscall9(trap, nargs, a1, a2, a3, a4, a5, a6, a7, a8, a9)
}

// Script is a compiled script, rooted as a persistent v8::Global<Script> so
// it can be run repeatedly (e.g. benchmark workloads) and survives handle
// scope lifetimes. It is bound to the isolate and context it was compiled
// for.
type Script struct {
	iso    *Isolate
	ctx    *Context
	handle uintptr
	state  uintptr // 0 while open; 1 after Close; temporary Run output during synchronous dispatch
}

// Compile compiles source in the context. If tc is non-nil, compile failures
// leave the exception details in it (HasCaught etc.); if tc is nil, a
// shim-internal TryCatch observes the failure and only the error result is
// returned. The scope (and TryCatch, when given) must belong to the same
// isolate as the context.
func (c *Context) Compile(s *Scope, source string, tc *TryCatch) (*Script, error) {
	return c.compileScript(s, source, tc, false)
}

func (c *Context) compileScript(s *Scope, source string, tc *TryCatch, platform bool) (*Script, error) {
	if err := c.check(); err != nil {
		return nil, err
	}
	if s.iso != c.iso {
		return nil, foreignIsolate("scope")
	}
	if tc != nil {
		if tc.iso != c.iso {
			return nil, foreignIsolate("trycatch")
		}
		// Reject a closed TryCatch before its freed shim wrapper could be
		// dereferenced.
		if err := tc.check(); err != nil {
			return nil, err
		}
	}
	// c.check proved the isolate's state and affinity for this operation,
	// so the scope check below only inspects its own closed flag.
	sh, err := s.checkedHandleAssumingIsolate()
	if err != nil {
		return nil, err
	}
	var b []byte
	if len(source) > 0 {
		b = []byte(source)
	}
	var p uintptr
	if len(b) > 0 {
		p = uintptr(unsafe.Pointer(&b[0]))
	}
	compiled := &Script{iso: c.iso, ctx: c}
	var tcv uintptr
	if tc != nil {
		tcv = tc.handle
	}
	ensureScriptHotProcs()
	compileAddr := scriptCompileAddr
	if platform {
		compileAddr = proc("gov8_script_compile_platform").Addr()
	}
	r1, _, _ := scriptEscapingSyscall9(compileAddr, 7,
		c.iso.handleAssumingCheck(), c.handle, sh, tcv, p, uintptr(len(b)),
		uintptr(unsafe.Pointer(&compiled.handle)), 0, 0)
	runtime.KeepAlive(b)
	runtime.KeepAlive(compiled)
	if int64(r1) < 0 {
		return nil, shimError("Compile", r1)
	}
	return compiled, nil
}

// check validates the script's own state and its isolate's thread affinity
// first, so wrong-thread misuse returns before the script-local closed flag
// is read; the owning context's closed flag is validated afterwards (its
// isolate validation is subsumed by the affinity proof above).
func (sc *Script) check() error {
	if err := sc.iso.check(); err != nil {
		return err
	}
	if sc.state != 0 {
		return fmt.Errorf("gov8: script used after Close")
	}
	return sc.ctx.checkAssumingIsolate()
}

// ID returns the engine's script id. Compiling identical source twice in one
// isolate resolves through V8's compilation cache to the same id; distinct
// source yields strictly increasing ids (pinned oracle finding).
func (sc *Script) ID() (int32, error) {
	if err := sc.check(); err != nil {
		return 0, err
	}
	var out int32
	r1, _, _ := proc("gov8_script_id").Call(sc.handle, uintptr(unsafe.Pointer(&out)))
	if int64(r1) < 0 {
		return 0, shimError("Script.ID", r1)
	}
	return out, nil
}

// Run executes the script in its context. The completion value is a
// scope-local Value. If tc is non-nil, runtime exceptions are recorded there;
// otherwise the error alone is returned. The scope (and TryCatch, when
// given) must belong to the same isolate as the script.
func (sc *Script) Run(s *Scope, tc *TryCatch) (Value, error) {
	if err := sc.check(); err != nil {
		return Value{}, err
	}
	if s.iso != sc.iso {
		return Value{}, foreignIsolate("scope")
	}
	if tc != nil {
		if tc.iso != sc.iso {
			return Value{}, foreignIsolate("trycatch")
		}
		// Reject a closed TryCatch before its freed shim wrapper could be
		// dereferenced.
		if err := tc.check(); err != nil {
			return Value{}, err
		}
	}
	// sc.check proved the isolate's state and affinity for this operation,
	// so the scope check below only inspects its own closed flag.
	sh, err := s.checkedHandleAssumingIsolate()
	if err != nil {
		return Value{}, err
	}
	var tcv uintptr
	if tc != nil {
		tcv = tc.handle
	}
	ensureScriptHotProcs()
	// Script is heap-resident, so state is a stable output slot even when
	// JavaScript re-enters Go and grows its stack. The shim writes it only
	// after execution returns; a nested Run finishes and resets its result
	// before the outer shim writes here, preserving strict LIFO behavior.
	r1, _, _ := scriptEscapingSyscall6(scriptRunAddr, 6,
		sc.iso.handleAssumingCheck(), sc.ctx.handle, sh, sc.handle, tcv,
		uintptr(unsafe.Pointer(&sc.state)))
	out := sc.state
	sc.state = 0
	if int64(r1) < 0 {
		return Value{}, shimError("Run", r1)
	}
	return Value{iso: sc.iso, sc: s, h: out}, nil
}

// Close releases the persistent script handle.
func (sc *Script) Close() error {
	if err := sc.iso.check(); err != nil {
		return err
	}
	if sc.state != 0 {
		return fmt.Errorf("gov8: script already closed")
	}
	if err := requireInitialized(); err != nil {
		return err
	}
	ensureScriptHotProcs()
	r1, _, _ := syscall.Syscall(scriptDisposeAddr, 1, sc.handle, 0, 0)
	sc.state = 1
	if int64(r1) < 0 {
		return shimError("Script.Close", r1)
	}
	return nil
}
