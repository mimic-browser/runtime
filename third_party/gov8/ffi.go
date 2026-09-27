//go:build (windows || linux) && amd64

package gov8

import (
	"fmt"
	syscall "github.com/maclof/gov8/internal/native"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/maclof/gov8/internal/prebuilt"
)

// shimABIVersion must match gov8_abi_version in internal/shim/shim.cc.
const shimABIVersion = 44

var (
	shimOnce sync.Once
	shimDLL  *syscall.DLL
	// procTable is an immutable name→proc map published through an atomic
	// pointer; resolution is copy-on-write under procMu. The hot read path
	// is one atomic load plus a plain map lookup, and every export resolves
	// through the host loader exactly once per process. The map stores
	// resolved *syscall.Proc values (not LazyProc) so per-call dispatch is
	// a direct SyscallN on the cached entry — no lazy-find check on the
	// hot path.
	procTable    atomic.Pointer[map[string]*syscall.Proc]
	procMu       sync.Mutex
	shimLoadErr  error
	shimIdentity string // SHA-256 of the artifact admitted by the one-time loader.
)

// shimDLLPath returns the native library path. GOV8_SHIM_LIBRARY (or the legacy
// GOV8_SHIM_DLL alias) overrides the verified embedded library in the user cache.
func shimDLLPath() (string, error) {
	p := os.Getenv("GOV8_SHIM_LIBRARY")
	if p == "" {
		p = os.Getenv("GOV8_SHIM_DLL")
	} // Legacy Windows override.
	if p != "" {
		absolute, err := filepath.Abs(p)
		if err != nil {
			return "", fmt.Errorf("gov8: resolve native shim override=%s: %w", p, err)
		}
		info, err := os.Stat(absolute)
		if err != nil {
			return "", fmt.Errorf("gov8: native shim override=%s: %w", p, err)
		}
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("gov8: native shim override=%s is not a regular file", p)
		}
		return absolute, nil
	}
	path, err := prebuilt.Path()
	if err != nil {
		return "", fmt.Errorf("gov8: prepare embedded native shim: %w (or set GOV8_SHIM_LIBRARY to a trusted library path)", err)
	}
	return path, nil
}

func loadShim() error {
	shimOnce.Do(func() {
		path, err := shimDLLPath()
		if err != nil {
			shimLoadErr = err
			return
		}
		identity, err := nativeLibraryDigest(path)
		if err != nil {
			shimLoadErr = err
			return
		}
		dll, err := loadShimDLL(path)
		if err != nil {
			shimLoadErr = fmt.Errorf("gov8: loading %s: %w", path, err)
			return
		}
		loadedIdentity, err := nativeLibraryDigest(path)
		if err != nil || identity != loadedIdentity {
			_ = dll.Release()
			shimLoadErr = fmt.Errorf("gov8: native shim changed during loading: %s: %v", path, err)
			return
		}
		abiProc, err := dll.FindProc("gov8_abi_version")
		if err != nil {
			shimLoadErr = fmt.Errorf("gov8: shim export gov8_abi_version missing: %w", err)
			return
		}
		abi, _, _ := abiProc.Call()
		if abi != shimABIVersion {
			shimLoadErr = fmt.Errorf("gov8: shim ABI mismatch: library reports %d, module expects %d; "+
				"remove GOV8_SHIM_LIBRARY/GOV8_SHIM_DLL or rebuild that override for this module version", abi, shimABIVersion)
			return
		}
		shimDLL = dll
		shimIdentity = identity
		m := map[string]*syscall.Proc{"gov8_abi_version": abiProc}
		procTable.Store(&m)
	})
	return shimLoadErr
}

func proc(name string) *syscall.Proc {
	if err := loadShim(); err != nil {
		panic("gov8: " + err.Error())
	}
	if m := procTable.Load(); m != nil {
		if p, ok := (*m)[name]; ok {
			return p
		}
	}
	return resolveProc(name)
}

// resolveProc looks up a missing export and publishes it copy-on-write. The
// mutex serializes publishers; the double-check after taking it keeps a
// concurrent resolution from publishing twice.
func resolveProc(name string) *syscall.Proc {
	procMu.Lock()
	defer procMu.Unlock()
	if m := procTable.Load(); m != nil {
		if p, ok := (*m)[name]; ok {
			return p
		}
	}
	p, err := shimDLL.FindProc(name)
	if err != nil {
		panic(fmt.Sprintf("gov8: shim export %s missing: %v; remove GOV8_SHIM_LIBRARY/GOV8_SHIM_DLL or rebuild that override for this module version", name, err))
	}
	old := *procTable.Load()
	m := make(map[string]*syscall.Proc, len(old)+1)
	for k, v := range old {
		m[k] = v
	}
	m[name] = p
	procTable.Store(&m)
	return p
}

// shimError builds an error for a negative shim status code, attaching the
// thread-local detail message from the shim. The status word is interpreted
// as a signed 64-bit value, which is exact for every negative status the
// shim emits.
func shimError(op string, status uintptr) error {
	return &ShimError{Op: op, Code: int64(status), Detail: lastShimError()}
}

// callErr invokes a shim function returning an int64 status and converts
// negative results into errors.
// uintptr arguments may be transient Go pointers forwarded to Proc.Call, so
// preserve its escape/liveness contract through this wrapper.
//
//go:uintptrescapes
func callErr(op string, p *syscall.Proc, args ...uintptr) error {
	r1, _, _ := p.Call(args...)
	if int64(r1) < 0 {
		return shimError(op, r1)
	}
	return nil
}

// callHandle invokes a shim function returning a pointer-sized handle
// (0 means failure with a thread-local error message).
func callHandle(op string, p *syscall.Proc, args ...uintptr) (uintptr, error) {
	r1, _, _ := p.Call(args...)
	if r1 == 0 {
		return 0, shimError(op, 0)
	}
	return r1, nil
}

const errBufSize = 1024

func lastShimError() string {
	var buf [errBufSize]byte
	r1, _, _ := proc("gov8_last_error").Call(
		uintptr(unsafe.Pointer(&buf[0])), errBufSize)
	n := int(r1)
	if n < 0 || n > errBufSize {
		n = 0
	}
	return string(buf[:n])
}

// ShimError is the error type for failures reported by the shim layer.
// Code is the negative status code from internal/shim/shim.cc.
type ShimError struct {
	Op     string
	Code   int64
	Detail string
}

func (e *ShimError) Error() string {
	if e.Detail == "" {
		return fmt.Sprintf("gov8: %s failed (status %d)", e.Op, e.Code)
	}
	return fmt.Sprintf("gov8: %s: %s (status %d)", e.Op, e.Detail, e.Code)
}

// IsException reports whether err is a JS-observable failure (a compile or
// run failure recorded by a TryCatch) as opposed to a wrapper misuse error.
func IsException(err error) bool {
	se, ok := err.(*ShimError)
	return ok && se.Code == errException
}

// Shim status codes; keep in sync with internal/shim/shim.cc.
const (
	errOK        = 0
	errGeneric   = -1
	errBadArg    = -2
	errState     = -3
	errNoMemory  = -4
	errException = -5
	errCpp       = -6
	errMagic     = -7
)
