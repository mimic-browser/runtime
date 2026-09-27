//go:build linux && amd64

// Package native supplies pointer-word calls using the host C ABI.
package native

import (
	"syscall"
	"unsafe"

	"github.com/ebitengine/purego"
)

type Errno = syscall.Errno
type DLL struct{ handle, wideCall uintptr }
type Proc struct{ address, wideCall uintptr }

func LoadDLL(path string) (*DLL, error) {
	h, err := purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil, err
	}
	wide, err := purego.Dlsym(h, "gov8_call_words")
	if err != nil {
		_ = purego.Dlclose(h)
		return nil, err
	}
	return &DLL{h, wide}, nil
}

func (d *DLL) Release() error { return purego.Dlclose(d.handle) }

func (d *DLL) FindProc(name string) (*Proc, error) {
	switch name {
	case "gov8_number_new", "gov8_rv_set_double", "gov8_rv_date_new", "gov8_platform_run_idle_tasks", "gov8_pc_idle_task_run_delete":
		name += "_words"
	}
	p, err := purego.Dlsym(d.handle, name)
	if err != nil {
		return nil, err
	}
	return &Proc{p, d.wideCall}, nil
}
func (p *Proc) Addr() uintptr { return p.address }

//go:uintptrescapes
func (p *Proc) Call(args ...uintptr) (uintptr, uintptr, error) {
	if len(args) > 15 {
		if len(args) > 42 {
			panic("gov8: native call exceeds 42 pointer words")
		}
		a, b, _ := purego.SyscallN(p.wideCall, p.address, uintptr(len(args)), uintptr(unsafe.Pointer(&args[0])))
		return a, b, Errno(0)
	}
	a, b, _ := purego.SyscallN(p.address, args...)
	return a, b, Errno(0)
}

var NewCallback = purego.NewCallback

//go:uintptrescapes
func Syscall(trap, nargs, a1, a2, a3 uintptr) (uintptr, uintptr, Errno) {
	args := [...]uintptr{a1, a2, a3}
	return SyscallN(trap, args[:nargs]...)
}

//go:uintptrescapes
func Syscall6(trap, nargs, a1, a2, a3, a4, a5, a6 uintptr) (uintptr, uintptr, Errno) {
	args := [...]uintptr{a1, a2, a3, a4, a5, a6}
	return SyscallN(trap, args[:nargs]...)
}

//go:uintptrescapes
func Syscall9(trap, nargs, a1, a2, a3, a4, a5, a6, a7, a8, a9 uintptr) (uintptr, uintptr, Errno) {
	args := [...]uintptr{a1, a2, a3, a4, a5, a6, a7, a8, a9}
	return SyscallN(trap, args[:nargs]...)
}

//go:uintptrescapes
func Syscall12(trap, nargs, a1, a2, a3, a4, a5, a6, a7, a8, a9, a10, a11, a12 uintptr) (uintptr, uintptr, Errno) {
	args := [...]uintptr{a1, a2, a3, a4, a5, a6, a7, a8, a9, a10, a11, a12}
	return SyscallN(trap, args[:nargs]...)
}

//go:uintptrescapes
func Syscall15(trap, nargs, a1, a2, a3, a4, a5, a6, a7, a8, a9, a10, a11, a12, a13, a14, a15 uintptr) (uintptr, uintptr, Errno) {
	args := [...]uintptr{a1, a2, a3, a4, a5, a6, a7, a8, a9, a10, a11, a12, a13, a14, a15}
	return SyscallN(trap, args[:nargs]...)
}

//go:uintptrescapes
func SyscallN(trap uintptr, args ...uintptr) (uintptr, uintptr, Errno) {
	a, b, e := purego.SyscallN(trap, args...)
	return a, b, Errno(e)
}
