//go:build windows && amd64

package v8

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

func allocateRevisionWord() (*uint64, func(), error) {
	address, err := windows.VirtualAlloc(0, 8, windows.MEM_COMMIT|windows.MEM_RESERVE, windows.PAGE_READWRITE)
	if err != nil {
		return nil, nil, err
	}
	// VirtualAlloc returns aligned native memory, outside the Go heap. The
	// counted V8 backing store's deleter owns the matching VirtualFree.
	return (*uint64)(unsafe.Pointer(address)), func() {
		if err := windows.VirtualFree(address, 0, windows.MEM_RELEASE); err != nil {
			panic(err)
		}
	}, nil
}
