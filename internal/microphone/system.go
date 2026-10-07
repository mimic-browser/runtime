//go:build cgo && (windows || linux || darwin)

package microphone

// #include <stdlib.h>
import "C"

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"sync"

	"github.com/gen2brain/malgo"
)

type systemProvider struct{}

func System() Provider { return systemProvider{} }

func audioContext() (*malgo.AllocatedContext, error) {
	// Never select the null backend as a substitute for a real microphone.
	return malgo.InitContext([]malgo.Backend{malgo.BackendWasapi, malgo.BackendCoreaudio, malgo.BackendPulseaudio, malgo.BackendAlsa}, malgo.ContextConfig{}, nil)
}

func (systemProvider) Devices(ctx context.Context) ([]Device, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	native, err := audioContext()
	if err != nil {
		return nil, err
	}
	defer native.Free()
	defer native.Uninit()
	devices, err := native.Devices(malgo.Capture)
	if err != nil {
		return nil, err
	}
	result := make([]Device, 0, len(devices))
	for _, d := range devices {
		result = append(result, Device{ID: d.ID.String(), Label: d.Name(), Default: d.IsDefault != 0})
	}
	return result, ctx.Err()
}

func (systemProvider) Open(ctx context.Context, id string, format Format) (Capture, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if format.SampleRate != 48000 || format.Channels < 1 || format.Channels > 2 {
		return nil, fmt.Errorf("unsupported microphone PCM format")
	}
	native, err := audioContext()
	if err != nil {
		return nil, err
	}
	fail := func(err error) (Capture, error) { native.Uninit(); native.Free(); return nil, err }
	devices, err := native.Devices(malgo.Capture)
	if err != nil {
		return fail(err)
	}
	var selected *malgo.DeviceID
	for _, device := range devices {
		if device.ID.String() == id {
			value := device.ID
			selected = &value
			break
		}
	}
	if selected == nil {
		return fail(fmt.Errorf("microphone disconnected"))
	}
	capture := &systemCapture{native: native, blocks: make(chan []float32, 2), done: make(chan struct{})}
	config := malgo.DefaultDeviceConfig(malgo.Capture)
	// InitDevice copies this identifier during the synchronous native call.
	identifier := selected.Pointer()
	defer C.free(identifier)
	config.Capture.DeviceID = identifier
	config.Capture.Format = malgo.FormatS16
	config.Capture.Channels = uint32(format.Channels)
	config.SampleRate = uint32(format.SampleRate)
	config.PeriodSizeInMilliseconds = 20
	blockSize := format.SampleRate / 50 * format.Channels
	partial := make([]float32, 0, blockSize)
	device, err := malgo.InitDevice(native.Context, config, malgo.DeviceCallbacks{
		Data: func(_ []byte, input []byte, _ uint32) {
			// Native bytes cannot escape the callback. Reblock variable callback
			// periods into 20ms packets, retaining at most two owned PCM blocks.
			for offset := 0; offset+2 <= len(input); offset += 2 {
				partial = append(partial, float32(int16(binary.LittleEndian.Uint16(input[offset:])))/32768)
				if len(partial) == blockSize {
					pcm := append([]float32(nil), partial...)
					partial = partial[:0]
					select {
					case capture.blocks <- pcm:
					default:
						select {
						case <-capture.blocks:
						default:
						}
						select {
						case capture.blocks <- pcm:
						default:
						}
					}
				}
			}
		},
		Stop: func() { capture.stopped.Do(func() { close(capture.done) }) },
	})
	if err != nil {
		return fail(err)
	}
	capture.device = device
	if err = device.Start(); err != nil {
		device.Uninit()
		return fail(err)
	}
	return capture, nil
}

type systemCapture struct {
	native          *malgo.AllocatedContext
	device          *malgo.Device
	blocks          chan []float32
	done            chan struct{}
	stopped, closed sync.Once
}

func (c *systemCapture) Read(ctx context.Context) ([]float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case <-c.done:
		return nil, io.EOF
	default:
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.done:
		return nil, io.EOF
	case block := <-c.blocks:
		return block, nil
	}
}
func (c *systemCapture) Close() error {
	c.closed.Do(func() { c.device.Uninit(); c.stopped.Do(func() { close(c.done) }); c.native.Uninit(); c.native.Free() })
	return nil
}
