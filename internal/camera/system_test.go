package camera

import (
	"context"
	"os"
	"runtime"
	"testing"
	"time"
)

// Opt-in hardware evidence: CI never opens an operator's physical webcam.
func TestSystemCameraCapture(t *testing.T) {
	label := os.Getenv("MIMIC_TEST_CAMERA")
	if label == "" {
		t.Skip("set MIMIC_TEST_CAMERA to the exact device label")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	provider := System()
	devices, err := provider.Devices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var id string
	for _, device := range devices {
		if device.Label == label {
			id = device.ID
			break
		}
	}
	if id == "" {
		t.Fatalf("camera %q not found: %v", label, devices)
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	capture, format, err := provider.Open(ctx, id, func(formats []Format) (Format, error) {
		if len(formats) == 0 {
			t.Fatal("no supported formats")
		}
		t.Logf("camera formats: %v", formats)
		best := formats[0]
		for _, f := range formats {
			if f.Width == 640 && f.Height == 480 {
				best = f
				break
			}
		}
		return best, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer capture.Close()
	for ctx.Err() == nil {
		frame, release, err := capture.Read()
		if err != nil {
			continue
		}
		if frame.Bounds().Dx() != format.Width || frame.Bounds().Dy() != format.Height {
			t.Fatalf("frame %v, format %v", frame.Bounds(), format)
		}
		release()
		t.Logf("captured %dx%d from %s", format.Width, format.Height, label)
		return
	}
	t.Fatal("no camera frame delivered")
}
