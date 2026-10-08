package browser

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/moreveal/mimic/internal/camera"
	"github.com/moreveal/mimic/internal/microphone"
)

// Hold native Open and Close separately so catalog replacement is tested at
// ownership boundaries, rather than relying on a fast fixture's timing.
type mediaCaptureGate struct {
	opening, allowOpen, closing, allowClose chan struct{}
	openOnce, closeOnce                     sync.Once
}

func newMediaCaptureGate(t *testing.T) *mediaCaptureGate {
	g := &mediaCaptureGate{
		opening: make(chan struct{}), allowOpen: make(chan struct{}),
		closing: make(chan struct{}), allowClose: make(chan struct{}),
	}
	t.Cleanup(func() { g.releaseOpen(); g.releaseClose() })
	return g
}

func (g *mediaCaptureGate) releaseOpen()  { g.openOnce.Do(func() { close(g.allowOpen) }) }
func (g *mediaCaptureGate) releaseClose() { g.closeOnce.Do(func() { close(g.allowClose) }) }
func (g *mediaCaptureGate) start(ctx context.Context) error {
	close(g.opening)
	select {
	case <-g.allowOpen:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type gatedProfileCamera struct {
	fixtureCameraProvider
	gate *mediaCaptureGate
}

func (p *gatedProfileCamera) Open(ctx context.Context, id string, choose func([]camera.Format) (camera.Format, error)) (camera.Capture, camera.Format, error) {
	if err := p.gate.start(ctx); err != nil {
		return nil, camera.Format{}, err
	}
	capture, format, err := p.fixtureCameraProvider.Open(ctx, id, choose)
	if err != nil {
		return nil, format, err
	}
	return &gatedProfileCameraCapture{Capture: capture, gate: p.gate}, format, nil
}

type gatedProfileCameraCapture struct {
	camera.Capture
	gate *mediaCaptureGate
}

func (c *gatedProfileCameraCapture) Close() error {
	close(c.gate.closing)
	<-c.gate.allowClose
	return c.Capture.Close()
}

type gatedProfileMicrophone struct {
	fixtureMicrophoneProvider
	gate *mediaCaptureGate
}

func (p *gatedProfileMicrophone) Open(ctx context.Context, id string, format microphone.Format) (microphone.Capture, error) {
	if err := p.gate.start(ctx); err != nil {
		return nil, err
	}
	capture, err := p.fixtureMicrophoneProvider.Open(ctx, id, format)
	if err != nil {
		return nil, err
	}
	return &gatedProfileMicrophoneCapture{Capture: capture, gate: p.gate}, nil
}

type gatedProfileMicrophoneCapture struct {
	microphone.Capture
	gate *mediaCaptureGate
}

func (c *gatedProfileMicrophoneCapture) Close() error {
	close(c.gate.closing)
	<-c.gate.allowClose
	return c.Capture.Close()
}

func TestMediaProfileCaptureLifecycleBlocksReplacement(t *testing.T) {
	serialBrowserTest(t)
	for _, kind := range []string{"camera", "microphone"} {
		t.Run(kind, func(t *testing.T) {
			cameraTestPages(t, func(t *testing.T, p *Page) {
				gate := newMediaCaptureGate(t)
				p.ctx.browser.cameraProvider = &fixtureCameraProvider{}
				p.ctx.browser.microphoneProvider = &fixtureMicrophoneProvider{}
				constraint := "video"
				if kind == "camera" {
					p.ctx.browser.cameraProvider = &gatedProfileCamera{gate: gate}
				} else {
					constraint = "audio"
					p.ctx.browser.microphoneProvider = &gatedProfileMicrophone{gate: gate}
				}
				if _, err := p.ctx.SetMediaProfileJSON([]byte(mediaFixtureProfile)); err != nil {
					t.Fatal(err)
				}
				before := p.ctx.MediaProfile()
				other := p.ctx.browser.NewContext()
				defer other.Close()
				navigateCapabilityFixture(t, p)
				if err := p.ctx.SetPermission(originOf(p.URL()), kind, "granted"); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
				defer cancel()
				wait := func(ch <-chan struct{}, phase string) {
					t.Helper()
					select {
					case <-ch:
					case <-ctx.Done():
						t.Fatalf("waiting for %s: %v", phase, ctx.Err())
					}
				}
				assertBlocked := func(phase string) {
					t.Helper()
					if _, err := p.ctx.SetMediaProfileJSON([]byte(`{"devices":[]}`)); err == nil {
						t.Fatalf("catalog replaced during %s", phase)
					}
					if !reflect.DeepEqual(before, p.ctx.MediaProfile()) {
						t.Fatalf("catalog partially mutated during %s", phase)
					}
					if _, err := other.SetMediaProfileJSON([]byte(`{"devices":[]}`)); err != nil {
						t.Fatalf("capture blocked another Context during %s: %v", phase, err)
					}
				}
				// Return immediately: awaiting this promise would hide the Open boundary.
				if _, err := p.Evaluate(ctx, `
globalThis.pendingProfileCapture = navigator.mediaDevices.getUserMedia({
  `+constraint+`: true,
});
true;
`); err != nil {
					t.Fatal(err)
				}
				wait(gate.opening, "Open")
				assertBlocked("Open")
				gate.releaseOpen()
				if _, err := p.Evaluate(ctx, `
pendingProfileCapture.then((stream) => {
  globalThis.profileCapture = stream;
  return true;
});
`); err != nil {
					t.Fatal(err)
				}
				assertBlocked("live capture")
				if _, err := p.Evaluate(ctx, `
profileCapture.getTracks().forEach((track) => track.stop());
true;
`); err != nil {
					t.Fatal(err)
				}
				wait(gate.closing, "Close")
				assertBlocked("Close")
				gate.releaseClose()
				for {
					p.ctx.mu.RLock()
					active := p.ctx.mediaCaptures
					p.ctx.mu.RUnlock()
					if active == 0 {
						break
					}
					select {
					case <-ctx.Done():
						t.Fatal("capture reservation leaked after Close")
					case <-time.After(time.Millisecond):
					}
				}
				if _, err := p.ctx.SetMediaProfileJSON([]byte(`{"devices":[]}`)); err != nil {
					t.Fatalf("catalog blocked after capture closed: %v", err)
				}
			})
		})
	}
}
