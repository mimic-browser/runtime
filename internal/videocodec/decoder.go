package videocodec

import (
	"fmt"
	"image"
	"image/draw"
	"runtime"

	h264 "github.com/y9o/go-openh264"
)

// Decoder owns its native state. Decode returns an immutable Go frame so that
// source readers never retain a buffer which the next native call will reuse.
type Decoder struct{ native *h264.ISVCDecoder }

func NewDecoder() (*Decoder, error) {
	if err := load(); err != nil {
		return nil, err
	}
	var native *h264.ISVCDecoder
	if code := h264.WelsCreateDecoder(&native); code != 0 {
		return nil, fmt.Errorf("create H264 decoder: %d", code)
	}
	params := h264.SDecodingParam{EEcActiveIdc: h264.ERROR_CON_SLICE_MV_COPY_CROSS_IDR_FREEZE_RES_CHANGE}
	if code := native.Initialize(&params); code != 0 {
		h264.WelsDestroyDecoder(native)
		return nil, fmt.Errorf("initialize H264 decoder: %d", code)
	}
	return &Decoder{native: native}, nil
}
func (d *Decoder) Close() {
	if d.native != nil {
		d.native.Uninitialize()
		h264.WelsDestroyDecoder(d.native)
		d.native = nil
	}
}
func (d *Decoder) Decode(data []byte) (*image.RGBA, error) {
	if d.native == nil {
		return nil, fmt.Errorf("decoder closed")
	}
	if len(data) == 0 {
		return nil, nil
	}
	var planes [3][]byte
	var info h264.SBufferInfo
	code := d.native.DecodeFrameNoDelay(data, len(data), &planes, &info)
	runtime.KeepAlive(data)
	if code != 0 {
		return nil, fmt.Errorf("decode H264 frame: %d", code)
	}
	if info.IBufferStatus != 1 {
		return nil, nil
	}
	sys := info.UsrData_sSystemBuffer()
	w, h := int(sys.IWidth), int(sys.IHeight)
	if w <= 0 || h <= 0 || w > 4096 || h > 4096 {
		return nil, fmt.Errorf("decoded dimensions exceed supported bounds")
	}
	src := &image.YCbCr{Y: planes[0], Cb: planes[1], Cr: planes[2], YStride: int(sys.IStride[0]), CStride: int(sys.IStride[1]), SubsampleRatio: image.YCbCrSubsampleRatio420, Rect: image.Rect(0, 0, w, h)}
	frame := image.NewRGBA(src.Rect)
	draw.Draw(frame, frame.Bounds(), src, image.Point{}, draw.Src)
	return frame, nil
}
