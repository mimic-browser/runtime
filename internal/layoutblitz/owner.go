// Package layoutblitz owns the native derived document used during migration.
// Calls must run on the owning Page's event loop. Separate Pages share no lock.
package layoutblitz

/*
#cgo windows,amd64 LDFLAGS: ${SRCDIR}/native/target/x86_64-pc-windows-gnu/release/libmimic_layout_blitz.a -lws2_32 -luserenv -lbcrypt -lntdll
#cgo linux,amd64 LDFLAGS: ${SRCDIR}/native/target/x86_64-unknown-linux-gnu/release/libmimic_layout_blitz.a -lfontconfig -ldl -lpthread -lm
#include <stdint.h>
#include <stddef.h>
#include "native_build_stamp.h"
static const char* mimic_native_archive_stamp(void) { return MIMIC_BLITZ_NATIVE_ARCHIVE_SHA256; }
typedef struct MimicBlitzHandle MimicBlitzHandle;
typedef struct { float x,y,width,height,client_width,client_height,content_width,content_height; uint32_t flags; } MimicBlitzRect;
MimicBlitzHandle* mimic_blitz_new(uint64_t, uint32_t, uint32_t);
void mimic_blitz_drop(MimicBlitzHandle*);
int32_t mimic_blitz_element(MimicBlitzHandle*, uint64_t, const char*, size_t, const char*, size_t);
int32_t mimic_blitz_attribute(MimicBlitzHandle*, uint64_t, const char*, size_t, const char*, size_t, const char*, size_t);
int32_t mimic_blitz_append(MimicBlitzHandle*, uint64_t, uint64_t);
int32_t mimic_blitz_state(MimicBlitzHandle*, uint64_t, uint32_t, uint32_t);
int32_t mimic_blitz_restyle_subtree(MimicBlitzHandle*);
int32_t mimic_blitz_color_scheme(MimicBlitzHandle*, uint32_t);
int32_t mimic_blitz_text(MimicBlitzHandle*, uint64_t, const char*, size_t, uint32_t);
int32_t mimic_blitz_resolve(MimicBlitzHandle*, double, uint64_t*);
int32_t mimic_blitz_rect(MimicBlitzHandle*, uint64_t, MimicBlitzRect*);
int32_t mimic_blitz_detach(MimicBlitzHandle*, uint64_t);
int32_t mimic_blitz_has_computed_style(MimicBlitzHandle*, uint64_t, uint32_t*);
int32_t mimic_blitz_viewport(MimicBlitzHandle*, uint32_t, uint32_t);
int32_t mimic_blitz_base_url(MimicBlitzHandle*, const char*, size_t);
int32_t mimic_blitz_clear_attribute(MimicBlitzHandle*, uint64_t, const char*, size_t, const char*, size_t);
int32_t mimic_blitz_stylesheet(MimicBlitzHandle*, uint64_t, const char*, size_t);
int32_t mimic_blitz_stylesheet_url(MimicBlitzHandle*, uint64_t, const char*, size_t, const char*, size_t);
int32_t mimic_blitz_style(MimicBlitzHandle*, uint64_t, const char*, size_t, char*, size_t, size_t*);
*/
import "C"

import (
	"fmt"
	"runtime"
	"unsafe"
)

type Owner struct {
	handle *C.MimicBlitzHandle
	// Like the native handle, scratch belongs to the document's serialized
	// execution owner. It is never shared between Pages or exposed to callers.
	styleBatchScratch []byte
}

// Refer to the generated archive digest from Go so a rebuilt static library
// invalidates cgo's build cache and the executable links the new archive.
var nativeArchiveStamp = C.GoString(C.mimic_native_archive_stamp())

type Rect struct {
	X, Y, Width, Height, ClientWidth, ClientHeight, ContentWidth, ContentHeight float64
	Flags                                                                       uint32
}

func New(root uint64, width, height uint32) (*Owner, error) {
	if nativeArchiveStamp == "" {
		return nil, fmt.Errorf("blitz: missing native archive stamp")
	}
	p := C.mimic_blitz_new(C.uint64_t(root), C.uint32_t(width), C.uint32_t(height))
	if p == nil {
		return nil, fmt.Errorf("blitz: document creation failed")
	}
	return &Owner{handle: p}, nil
}

// Close must be called when the canonical Document is disposed, including on
// failed navigation. It is idempotent; native state never outlives that owner.
func (o *Owner) Close() {
	if o.handle != nil {
		C.mimic_blitz_drop(o.handle)
		o.handle = nil
	}
	o.styleBatchScratch = nil
}

func check(status C.int32_t) error {
	if status != 0 {
		return fmt.Errorf("blitz: native transaction failed (%d); resynchronization required", int32(status))
	}
	return nil
}
func chars(s string) *C.char { return (*C.char)(unsafe.Pointer(unsafe.StringData(s))) }

func (o *Owner) Element(id uint64, namespace, name string) error {
	status := C.mimic_blitz_element(o.handle, C.uint64_t(id), chars(namespace), C.size_t(len(namespace)), chars(name), C.size_t(len(name)))
	runtime.KeepAlive(namespace)
	runtime.KeepAlive(name)
	return check(status)
}
func (o *Owner) Attribute(id uint64, namespace, name, value string) error {
	status := C.mimic_blitz_attribute(o.handle, C.uint64_t(id), chars(namespace), C.size_t(len(namespace)), chars(name), C.size_t(len(name)), chars(value), C.size_t(len(value)))
	runtime.KeepAlive(namespace)
	runtime.KeepAlive(name)
	runtime.KeepAlive(value)
	return check(status)
}
func (o *Owner) Append(parent, child uint64) error {
	return check(C.mimic_blitz_append(o.handle, C.uint64_t(parent), C.uint64_t(child)))
}

func (o *Owner) State(id uint64, mask, flags uint32) error {
	return check(C.mimic_blitz_state(o.handle, C.uint64_t(id), C.uint32_t(mask), C.uint32_t(flags)))
}

func (o *Owner) RestyleSubtree() error {
	return check(C.mimic_blitz_restyle_subtree(o.handle))
}

func (o *Owner) ColorScheme(dark bool) error {
	var flag C.uint32_t
	if dark {
		flag = 1
	}
	return check(C.mimic_blitz_color_scheme(o.handle, flag))
}
func (o *Owner) Text(id uint64, value string, create bool) error {
	flag := C.uint32_t(0)
	if create {
		flag = 1
	}
	status := C.mimic_blitz_text(o.handle, C.uint64_t(id), chars(value), C.size_t(len(value)), flag)
	runtime.KeepAlive(value)
	return check(status)
}
func (o *Owner) Resolve(time float64) (uint64, error) {
	var generation C.uint64_t
	err := check(C.mimic_blitz_resolve(o.handle, C.double(time), &generation))
	return uint64(generation), err
}
func (o *Owner) Rect(id uint64) (Rect, error) {
	var rect C.MimicBlitzRect
	err := check(C.mimic_blitz_rect(o.handle, C.uint64_t(id), &rect))
	return Rect{float64(rect.x), float64(rect.y), float64(rect.width), float64(rect.height), float64(rect.client_width), float64(rect.client_height), float64(rect.content_width), float64(rect.content_height), uint32(rect.flags)}, err
}

func (o *Owner) Comment(id uint64, value string) error {
	status := C.mimic_blitz_text(o.handle, C.uint64_t(id), chars(value), C.size_t(len(value)), 2)
	runtime.KeepAlive(value)
	return check(status)
}
func (o *Owner) BaseURL(value string) error {
	status := C.mimic_blitz_base_url(o.handle, chars(value), C.size_t(len(value)))
	runtime.KeepAlive(value)
	return check(status)
}
func (o *Owner) Detach(id uint64) error { return check(C.mimic_blitz_detach(o.handle, C.uint64_t(id))) }
func (o *Owner) Viewport(width, height uint32) error {
	return check(C.mimic_blitz_viewport(o.handle, C.uint32_t(width), C.uint32_t(height)))
}
func (o *Owner) ClearAttribute(id uint64, namespace, name string) error {
	status := C.mimic_blitz_clear_attribute(o.handle, C.uint64_t(id), chars(namespace), C.size_t(len(namespace)), chars(name), C.size_t(len(name)))
	runtime.KeepAlive(namespace)
	runtime.KeepAlive(name)
	return check(status)
}
func (o *Owner) Stylesheet(id uint64, css string) error {
	status := C.mimic_blitz_stylesheet(o.handle, C.uint64_t(id), chars(css), C.size_t(len(css)))
	runtime.KeepAlive(css)
	return check(status)
}

func (o *Owner) StylesheetAtURL(id uint64, css, baseURL string) error {
	status := C.mimic_blitz_stylesheet_url(o.handle, C.uint64_t(id), chars(css), C.size_t(len(css)), chars(baseURL), C.size_t(len(baseURL)))
	runtime.KeepAlive(css)
	runtime.KeepAlive(baseURL)
	return check(status)
}
func (o *Owner) Style(id uint64, name string) (string, error) {
	buffer := make([]byte, 128)
	for {
		var written C.size_t
		status := C.mimic_blitz_style(o.handle, C.uint64_t(id), chars(name), C.size_t(len(name)), (*C.char)(unsafe.Pointer(&buffer[0])), C.size_t(len(buffer)), &written)
		runtime.KeepAlive(name)
		if status == -3 {
			if uint64(written) > 64*1024*1024 {
				return "", fmt.Errorf("blitz: style readback exceeds allocation limit")
			}
			buffer = make([]byte, int(written))
			continue
		}
		if err := check(status); err != nil {
			return "", err
		}
		return string(buffer[:int(written)]), nil
	}
}
func (o *Owner) HasComputedStyle(id uint64) (bool, error) {
	var value C.uint32_t
	err := check(C.mimic_blitz_has_computed_style(o.handle, C.uint64_t(id), &value))
	return value != 0, err
}
