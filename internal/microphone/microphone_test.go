package microphone

import (
	"context"
	"errors"
	"math"
	"os"
	"testing"
	"time"
)

func TestMicrophoneProviderCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := System().Devices(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("enumeration cancellation: %v", err)
	}
	if _, err := System().Open(ctx, "", Format{48000, 1}); !errors.Is(err, context.Canceled) {
		t.Fatalf("capture cancellation: %v", err)
	}
}

func TestSystemMicrophoneCapture(t *testing.T) {
	if os.Getenv("MIMIC_TEST_MICROPHONE") == "" {
		t.Skip("opt-in real microphone capture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	devices, err := System().Devices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) == 0 {
		t.Fatal("no native microphone")
	}
	selected := devices[0]
	for _, d := range devices {
		if d.Default {
			selected = d
			break
		}
	}
	capture, err := System().Open(ctx, selected.ID, Format{48000, 1})
	if err != nil {
		t.Fatal(err)
	}
	defer capture.Close()
	peak := float32(0)
	for block := 0; block < 8; block++ {
		samples, err := capture.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(samples) != 960 {
			t.Fatalf("unexpected block size: %d", len(samples))
		}
		for _, v := range samples {
			if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) || v < -1 || v > 1 {
				t.Fatal("invalid PCM sample")
			}
			peak = max(peak, float32(math.Abs(float64(v))))
		}
	}
	t.Logf("native microphone %q: 48000Hz mono, eight owned 20ms blocks; peak %.5f", selected.Label, peak)
	if err := capture.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := capture.Read(ctx); !errors.Is(err, context.Canceled) && err == nil {
		t.Fatal("read succeeded after close")
	}
}
