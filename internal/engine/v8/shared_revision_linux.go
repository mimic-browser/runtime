//go:build linux && amd64

package v8

import (
	"unsafe"

	"golang.org/x/sys/unix"
)

func allocateRevisionWord() (*uint64, func(), error) {
	memory, err := unix.Mmap(-1, 0, 8, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_PRIVATE|unix.MAP_ANON)
	if err != nil {
		return nil, nil, err
	}
	return (*uint64)(unsafe.Pointer(&memory[0])), func() {
		if err := unix.Munmap(memory); err != nil {
			panic(err)
		}
	}, nil
}
