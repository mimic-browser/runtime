//go:build cgo && (windows || linux || darwin)

package camera

import (
	"context"
	"errors"
	"fmt"
	"image"
	"runtime"
	"sort"

	"github.com/pion/mediadevices/pkg/driver"
	native "github.com/pion/mediadevices/pkg/driver/camera"
	"github.com/pion/mediadevices/pkg/io/video"
	"github.com/pion/mediadevices/pkg/prop"
)

type systemProvider struct{}

func System() Provider { return systemProvider{} }

func (systemProvider) Devices(ctx context.Context) ([]Device, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := beginPlatform(); err != nil {
		return nil, err
	}
	defer endPlatform()
	devices, err := native.Discover()
	if err != nil {
		return nil, err
	}
	result := make([]Device, 0, len(devices))
	for _, d := range devices {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		label := d.Info.Name
		if label == "" {
			label = d.Info.Label
		}
		result = append(result, Device{ID: d.Info.Label, Label: label})
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

// Open/Read/Close execute on one OS thread. The browser's resource goroutine
// locks that thread for the capture lifetime, preserving COM apartment ownership.
func (systemProvider) Open(ctx context.Context, id string, selectFormat func([]Format) (Format, error)) (Capture, Format, error) {
	if err := ctx.Err(); err != nil {
		return nil, Format{}, err
	}
	if err := beginPlatform(); err != nil {
		return nil, Format{}, err
	}
	devices, err := native.Discover()
	if err != nil {
		endPlatform()
		return nil, Format{}, err
	}
	for _, d := range devices {
		if d.Info.Label != id {
			continue
		}
		a := d.Adapter
		if err = a.Open(); err != nil {
			endPlatform()
			return nil, Format{}, err
		}
		formats := []Format{}
		for _, p := range a.Properties() {
			if p.Width > 0 && p.Height > 0 && p.Width <= 8192 && p.Height <= 8192 && p.Width*p.Height <= 16<<20 {
				formats = append(formats, Format{p.Width, p.Height, float64(p.FrameRate), p})
			}
		}
		var f Format
		f, err = selectFormat(formats)
		if err == nil {
			err = ctx.Err()
		}
		if err != nil {
			a.Close()
			endPlatform()
			return nil, Format{}, err
		}
		recorder, ok := a.(driver.VideoRecorder)
		if !ok {
			a.Close()
			endPlatform()
			return nil, Format{}, fmt.Errorf("camera has no video recorder")
		}
		p := f.Native.(prop.Media)
		reader, err := recorder.VideoRecord(p)
		if err != nil {
			a.Close()
			endPlatform()
			return nil, Format{}, err
		}
		return &systemCapture{adapter: a, reader: reader}, f, nil
	}
	endPlatform()
	return nil, Format{}, fmt.Errorf("camera disconnected")
}

type systemCapture struct {
	closed  bool
	adapter driver.Adapter
	reader  video.Reader
}

func (c *systemCapture) Read() (image.Image, func(), error) {
	img, release, err := c.reader.Read()
	if errors.Is(err, native.ErrReadTimeout) {
		err = ErrFrameTimeout
	}
	return img, release, err
}
func (c *systemCapture) Close() error {
	if c.closed {
		return nil
	}
	c.closed = true
	err := c.adapter.Close()
	endPlatform()
	return err
}
