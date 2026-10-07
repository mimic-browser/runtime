package videocodec

import (
	"github.com/ebitengine/purego"
	h264 "github.com/y9o/go-openh264"
	"unsafe"
)

// OpenH264 2.6 adds PSNR fields to the source picture and each encoded layer.
// Keep the pinned binary's exact ABI here: the dependency's 2.4 layouts must
// never be passed to these functions. The vtable and base parameters are stable.
// Reference: cisco/openh264 v2.6.0 codec/api/wels/codec_app_def.h.
type sourcePicture struct {
	h264.SSourcePicture
	psnrY, psnrU, psnrV bool
}
type layerInfo struct {
	h264.SLayerBSInfo
	psnr [3]float32
}
type frameInfo struct {
	ILayerNum         int32
	SLayerInfo        [128]layerInfo
	EFrameType        uint32
	IFrameSizeInBytes int32
	UiTimeStamp       int64
}

func encodeFrame(native *h264.ISVCEncoder, picture *sourcePicture, info *frameInfo) int {
	vtable := *(**h264.ISVCEncoderVtbl)(unsafe.Pointer(native))
	code, _, _ := purego.SyscallN(uintptr(unsafe.Pointer(vtable.EncodeFrame)), uintptr(unsafe.Pointer(native)), uintptr(unsafe.Pointer(picture)), uintptr(unsafe.Pointer(info)))
	return int(code)
}
func forceKeyFrame(native *h264.ISVCEncoder) {
	vtable := *(**h264.ISVCEncoderVtbl)(unsafe.Pointer(native))
	purego.SyscallN(uintptr(unsafe.Pointer(vtable.ForceIntraFrame)), uintptr(unsafe.Pointer(native)), 1, 0)
}
