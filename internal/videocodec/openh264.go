// Package videocodec provides a bundled CPU video encoder without external tools.
package videocodec

import (
	"bytes"
	"compress/bzip2"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"unsafe"

	h264 "github.com/y9o/go-openh264"
)

var library struct {
	once sync.Once
	err  error
}

// The loaded code is immutable process-wide. Each encoder owns independent
// native state; no capture or Page work runs under a global codec lock.
func load() error {
	library.once.Do(func() {
		packed, err := bundled.ReadFile("bundled/" + runtime.GOOS + "_" + runtime.GOARCH + ".bz2")
		if err != nil {
			library.err = fmt.Errorf("no bundled H264 codec for %s/%s", runtime.GOOS, runtime.GOARCH)
			return
		}
		provenance, _ := bundled.ReadFile("bundled/provenance.json")
		var manifest struct {
			Artifacts map[string]struct {
				SHA256 string `json:"sha256"`
			} `json:"artifacts"`
		}
		if err = json.Unmarshal(provenance, &manifest); err != nil {
			library.err = err
			return
		}
		if fmt.Sprintf("%x", sha256.Sum256(packed)) != manifest.Artifacts[runtime.GOOS+"_"+runtime.GOARCH].SHA256 {
			library.err = fmt.Errorf("bundled H264 checksum mismatch")
			return
		}
		data, err := io.ReadAll(bzip2.NewReader(bytes.NewReader(packed)))
		if err != nil {
			library.err = err
			return
		}
		hash := sha256.Sum256(data)
		suffix := ".so"
		if runtime.GOOS == "windows" {
			suffix = ".dll"
		}
		if runtime.GOOS == "darwin" {
			suffix = ".dylib"
		}
		directory := filepath.Join(os.TempDir(), fmt.Sprintf("mimic-openh264-%x", hash[:12]))
		if err = os.MkdirAll(directory, 0700); err != nil {
			library.err = err
			return
		}
		path := filepath.Join(directory, "openh264"+suffix)
		existing, err := os.ReadFile(path)
		if err != nil || sha256.Sum256(existing) != hash {
			temporary, err := os.CreateTemp(directory, "codec-*")
			if err != nil {
				library.err = err
				return
			}
			name := temporary.Name()
			defer os.Remove(name)
			if _, err = temporary.Write(data); err == nil {
				err = temporary.Close()
			} else {
				temporary.Close()
			}
			if err == nil {
				err = os.Chmod(name, 0700)
			}
			if err == nil {
				err = os.Rename(name, path)
			}
			if err != nil {
				library.err = err
				return
			}
		}
		library.err = h264.Open(path)
		if library.err == nil {
			v := h264.WelsGetCodecVersion()
			if v.UMajor != 2 || v.UMinor != 6 || v.URevision != 0 {
				library.err = fmt.Errorf("unexpected H264 codec ABI %d.%d.%d", v.UMajor, v.UMinor, v.URevision)
			}
		}
	})
	return library.err
}

type Encoder struct {
	native        *h264.ISVCEncoder
	width, height int
	frames        int64
	yuv           *image.YCbCr
}

func New(width, height int, fps float64) (*Encoder, error) {
	if width <= 0 || height <= 0 || width%2 != 0 || height%2 != 0 {
		return nil, fmt.Errorf("H264 requires positive even frame dimensions")
	}
	if err := load(); err != nil {
		return nil, err
	}
	var native *h264.ISVCEncoder
	if code := h264.WelsCreateSVCEncoder(&native); code != 0 {
		return nil, fmt.Errorf("create H264 encoder: %d", code)
	}
	params := h264.SEncParamBase{IUsageType: h264.CAMERA_VIDEO_REAL_TIME, IPicWidth: int32(width), IPicHeight: int32(height), ITargetBitrate: 2_000_000, FMaxFrameRate: float32(fps)}
	if code := native.Initialize(&params); code != 0 {
		h264.WelsDestroySVCEncoder(native)
		return nil, fmt.Errorf("initialize H264 encoder: %d", code)
	}
	return &Encoder{native: native, width: width, height: height, yuv: image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)}, nil
}
func (e *Encoder) Close() {
	if e.native != nil {
		e.native.Uninitialize()
		h264.WelsDestroySVCEncoder(e.native)
		e.native = nil
	}
}
func (e *Encoder) Encode(src image.Image, keyFrame bool) ([]byte, error) {
	if e.native == nil {
		return nil, fmt.Errorf("encoder closed")
	}
	yuv := e.yuv
	for y := 0; y < e.height; y++ {
		for x := 0; x < e.width; x++ {
			r, g, b, _ := src.At(x, y).RGBA()
			l, cb, cr := color.RGBToYCbCr(uint8(r>>8), uint8(g>>8), uint8(b>>8))
			yuv.Y[yuv.YOffset(x, y)] = l
			if x%2 == 0 && y%2 == 0 {
				offset := yuv.COffset(x, y)
				yuv.Cb[offset] = cb
				yuv.Cr[offset] = cr
			}
		}
	}
	picture := sourcePicture{SSourcePicture: h264.SSourcePicture{IColorFormat: h264.VideoFormatI420, IStride: [4]int32{int32(yuv.YStride), int32(yuv.CStride), int32(yuv.CStride)}, PData: [4]*byte{&yuv.Y[0], &yuv.Cb[0], &yuv.Cr[0]}, IPicWidth: int32(e.width), IPicHeight: int32(e.height), UiTimeStamp: e.frames * 33}}
	if keyFrame {
		forceKeyFrame(e.native)
	}
	var info frameInfo
	code := encodeFrame(e.native, &picture, &info)
	runtime.KeepAlive(yuv)
	if code != 0 {
		return nil, fmt.Errorf("encode H264 frame: %d", code)
	}
	e.frames++
	if info.ILayerNum < 0 || info.ILayerNum > 128 {
		return nil, fmt.Errorf("invalid encoded layer count")
	}
	var data []byte
	for i := 0; i < int(info.ILayerNum); i++ {
		layer := info.SLayerInfo[i]
		size := 0
		if layer.INalCount < 0 || layer.INalCount > 65536 {
			return nil, fmt.Errorf("invalid encoded NAL count")
		}
		for _, n := range unsafe.Slice(layer.PNalLengthInByte, int(layer.INalCount)) {
			if n < 0 || n > 32<<20 {
				return nil, fmt.Errorf("invalid NAL size")
			}
			size += int(n)
		}
		if size > 32<<20 {
			return nil, fmt.Errorf("encoded frame too large")
		}
		data = append(data, unsafe.Slice(layer.PBsBuf, size)...)
	}
	return data, nil
}
