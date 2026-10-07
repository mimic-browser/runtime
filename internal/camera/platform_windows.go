//go:build windows && cgo

package camera

/*
#cgo LDFLAGS: -lole32 -static
#include <windows.h>
#include <objbase.h>
static HRESULT camera_com_begin(void) { return CoInitializeEx(NULL, COINIT_MULTITHREADED); }
static void camera_com_end(void) { CoUninitialize(); }
*/
import "C"
import "fmt"

func beginPlatform() error {
	if hr := C.camera_com_begin(); hr < 0 {
		return fmt.Errorf("camera COM initialization: 0x%08x", uint32(hr))
	}
	return nil
}
func endPlatform() { C.camera_com_end() }
