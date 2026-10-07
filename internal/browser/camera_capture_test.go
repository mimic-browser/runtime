package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"os"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	chrome152 "github.com/moreveal/mimic/chrome/152"
	"github.com/moreveal/mimic/internal/camera"
	quickjsengine "github.com/moreveal/mimic/internal/engine/quickjs"
	v8engine "github.com/moreveal/mimic/internal/engine/v8"
)

func cameraTestPages(t *testing.T, run func(*testing.T, *Page)) {
	t.Helper()
	historyTestPages(t, func(t *testing.T, p *Page) {
		p.ctx.browser.microphoneProvider = emptyMicrophoneProvider{}
		run(t, p)
	})
	t.Run("quickjs", func(t *testing.T) {
		b, err := New(quickjsengine.Factory{}, chrome152.New())
		if err != nil {
			t.Fatal(err)
		}
		c := b.NewContext()
		t.Cleanup(func() { _ = c.Close() })
		p, err := c.NewPage()
		if err != nil {
			t.Fatal(err)
		}
		p.Trace().Start()
		b.microphoneProvider = emptyMicrophoneProvider{}
		run(t, p)
	})
}

type fixtureCameraProvider struct{ opens, closes atomic.Int32 }

func (*fixtureCameraProvider) Devices(context.Context) ([]camera.Device, error) {
	return []camera.Device{{ID: "fixture-camera", Label: "Fixture camera"}}, nil
}
func (p *fixtureCameraProvider) Open(ctx context.Context, id string, choose func([]camera.Format) (camera.Format, error)) (camera.Capture, camera.Format, error) {
	f, err := choose([]camera.Format{{Width: 2, Height: 1, FrameRate: 30}})
	if err != nil {
		return nil, f, err
	}
	p.opens.Add(1)
	return &fixtureCameraCapture{provider: p}, f, nil
}

type fixtureCameraCapture struct {
	provider *fixtureCameraProvider
	closed   bool
}

func (c *fixtureCameraCapture) Read() (image.Image, func(), error) {
	time.Sleep(5 * time.Millisecond)
	return &image.RGBA{Pix: []byte{255, 0, 0, 255, 0, 255, 0, 255}, Stride: 8, Rect: image.Rect(0, 0, 2, 1)}, func() {}, nil
}
func (c *fixtureCameraCapture) Close() error {
	if !c.closed {
		c.closed = true
		c.provider.closes.Add(1)
	}
	return nil
}

func TestCameraStreamPixelsCloneAndTeardown(t *testing.T) {
	serialBrowserTest(t)
	cameraTestPages(t, func(t *testing.T, p *Page) {
		provider := &fixtureCameraProvider{}
		p.ctx.browser.cameraProvider = provider
		navigateCapabilityFixture(t, p)
		if err := p.ctx.SetPermission(originOf(p.URL()), "camera", "granted"); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		result, err := p.Evaluate(ctx, `(async () => {
  const devices = await navigator.mediaDevices.enumerateDevices();
  if (devices.length !== 1 || devices[0].label !== 'Fixture camera' || !devices[0].deviceId)
    throw new Error('devices');
  globalThis.stream = await navigator.mediaDevices.getUserMedia({
    video: { deviceId: { exact: devices[0].deviceId } },
  });
  globalThis.track = stream.getVideoTracks()[0];
  globalThis.clone = track.clone();
  if (
    !(stream instanceof MediaStream) ||
    !(track instanceof MediaStreamTrack) ||
    track === clone ||
    track.id === clone.id ||
    clone.getSettings().deviceId !== devices[0].deviceId
  )
    throw new Error('identity');
  const v = document.createElement('video');
  v.srcObject = stream;
  await v.play();
  if (
    v.srcObject !== stream ||
    v.videoWidth !== 2 ||
    v.videoHeight !== 1 ||
    v.readyState !== 4 ||
    v.paused
  )
    throw new Error('video');
  const canvas = document.createElement('canvas');
  canvas.width = 2;
  canvas.height = 1;
  const c = canvas.getContext('2d');
  c.drawImage(v, 0, 0);
  if ([...c.getImageData(0, 0, 2, 1).data].join() !== '255,0,0,255,0,255,0,255')
    throw new Error('pixels');
  const bitmap = await createImageBitmap(v);
  if (bitmap.width !== 2 || bitmap.height !== 1) throw new Error('bitmap');
  bitmap.close();
  track.enabled = false;
  c.drawImage(v, 0, 0);
  if ([...c.getImageData(0, 0, 2, 1).data].join() !== '0,0,0,255,0,0,0,255')
    throw new Error('disabled pixels');
  if (!clone.enabled) throw new Error('clone enabled');
  let ended = 0;
  track.onended = () => ended++;
  track.stop();
  if (track.readyState !== 'ended' || stream.active || clone.readyState !== 'live' || ended)
    throw new Error('stop');
  globalThis.cloneStream = new MediaStream([clone]);
  v.srcObject = cloneStream;
  await v.play();
  await new Promise((resolve) =>
    v.requestVideoFrameCallback((now, m) => {
      if (m.width !== 2 || m.height !== 1) throw new Error('frame callback');
      resolve();
    }),
  );
  clone.stop();
  return true;
})();`)
		if err != nil || result != true {
			t.Fatalf("camera: %v %v", result, err)
		}
		deadline := time.Now().Add(time.Second)
		for provider.closes.Load() == 0 && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if provider.opens.Load() != 1 || provider.closes.Load() != 1 {
			t.Fatalf("capture ownership: opened=%d closed=%d", provider.opens.Load(), provider.closes.Load())
		}
	})
}

func TestCameraPermissionConstraintsAndOriginIsolation(t *testing.T) {
	serialBrowserTest(t)
	cameraTestPages(t, func(t *testing.T, p *Page) {
		provider := &fixtureCameraProvider{}
		p.ctx.browser.cameraProvider = provider
		navigateCapabilityFixture(t, p)
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		result, err := p.Evaluate(ctx, `(async () => {
  const d = await navigator.mediaDevices.enumerateDevices();
  if (d.length !== 1 || d[0].label || d[0].deviceId) throw new Error('privacy');
  try {
    await navigator.mediaDevices.getUserMedia({ video: true });
    return false;
  } catch (e) {
    return e.name === 'NotAllowedError';
  }
})();`)
		if err != nil || result != true {
			if result == false {
				diagnostic, _ := p.Evaluate(ctx, `navigator.mediaDevices.getUserMedia({ video: true }).then(
  () => ({ success: true }),
  (e) => ({ name: e.name, message: e.message, text: String(e) }),
);`)
				t.Logf("permission exception: %v", diagnostic)
			}
			t.Fatalf("prompt completion: %v %v", result, err)
		}
		if provider.opens.Load() != 0 {
			t.Fatal("ungranted camera opened")
		}
		if err := p.ctx.SetPermission(originOf(p.URL()), "camera", "granted"); err != nil {
			t.Fatal(err)
		}
		result, err = p.Evaluate(ctx, `(async () => {
  for (const [constraints, expected] of [
    [{ width: { exact: 9000 } }, 'width'],
    [{ deviceId: { exact: 'missing' } }, 'deviceId'],
    [{ groupId: { exact: 'missing' } }, 'groupId'],
    [{ facingMode: { exact: 'environment' } }, 'facingMode'],
  ]) {
    try {
      await navigator.mediaDevices.getUserMedia({ video: constraints });
      return false;
    } catch (e) {
      if (e.name !== 'OverconstrainedError' || e.constraint !== expected)
        throw new Error('constraint ' + expected + ': ' + e.name + ' ' + e.constraint);
    }
  }
  globalThis.stream = await navigator.mediaDevices.getUserMedia({ video: true });
  const t = stream.getVideoTracks()[0];
  const before = JSON.stringify(t.getSettings());
  try {
    await t.applyConstraints({ height: { exact: 3 } });
    return false;
  } catch (e) {
    if (e.name !== 'OverconstrainedError' || e.constraint !== 'height')
      throw new Error('apply ' + e.name + ' ' + e.constraint);
  }
  if (t.getSettings().width !== 2 || t.getSettings().height !== 1)
    throw new Error('settings changed after failed apply');
  return true;
})();`)
		if err != nil || result != true {
			t.Fatalf("constraints: %v %v", result, err)
		}
		other := p.ctx.browser.NewContext()
		defer other.Close()
		p2, err := other.NewPage()
		if err != nil {
			t.Fatal(err)
		}
		defer p2.Close()
		if err = p2.Navigate(ctx, p.URL()); err != nil {
			t.Fatal(err)
		}
		state, err := p2.Evaluate(ctx, `navigator.permissions.query({ name: 'camera' }).then((p) => p.state);`)
		if err != nil || state != "prompt" {
			t.Fatalf("context isolation: %v %v", state, err)
		}
		if err = p.Navigate(ctx, p.URL()); err != nil {
			t.Fatal(err)
		}
		if provider.closes.Load() != 1 {
			t.Fatalf("navigation retained capture: %d", provider.closes.Load())
		}
	})
}

func TestCameraRevocationEndsClonesAndReleasesDevice(t *testing.T) {
	serialBrowserTest(t)
	cameraTestPages(t, func(t *testing.T, p *Page) {
		provider := &fixtureCameraProvider{}
		p.ctx.browser.cameraProvider = provider
		navigateCapabilityFixture(t, p)
		if err := p.ctx.SetPermission(originOf(p.URL()), "camera", "granted"); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		_, err := p.Evaluate(ctx, `(async () => {
  const s = await navigator.mediaDevices.getUserMedia({ video: true });
  globalThis.revokedTracks = [s.getVideoTracks()[0], s.getVideoTracks()[0].clone()];
  globalThis.revocationEvents = 0;
  globalThis.revocationComplete = new Promise((resolve) => {
    for (const t of revokedTracks)
      t.onended = () => {
        revocationEvents++;
        if (revocationEvents === 2) resolve();
      };
  });
})();`)
		if err != nil {
			t.Fatal(err)
		}
		if err := p.ctx.SetPermission(originOf(p.URL()), "camera", "denied"); err != nil {
			t.Fatal(err)
		}
		result, err := p.Evaluate(ctx, `revocationComplete.then(
  () => revokedTracks.every((t) => t.readyState === 'ended') && revocationEvents === 2,
);`)
		if err != nil || result != true {
			t.Fatalf("revocation: %v %v", result, err)
		}
		deadline := time.Now().Add(time.Second)
		for provider.closes.Load() != 1 && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if provider.closes.Load() != 1 {
			t.Fatal("revocation retained device")
		}
	})
}

// Replay the retained probe against Mimic, never recapturing Chrome during tests.
// This opt-in evidence is tied to the same OBS device configuration as the
// saved reference; CI's synthetic tests exercise lifecycle without hardware.
func TestSystemCameraMatchesSavedChrome152(t *testing.T) {
	serialBrowserTest(t)
	if os.Getenv("MIMIC_TEST_CAMERA") != "OBS Virtual Camera" {
		t.Skip("requires opted-in OBS Virtual Camera")
	}
	data, err := os.ReadFile("testdata/camera_capture_chrome152.json")
	if err != nil {
		t.Fatal(err)
	}
	var capture struct {
		Observation any `json:"observation"`
	}
	if err = json.Unmarshal(data, &capture); err != nil {
		t.Fatal(err)
	}
	probe, err := os.ReadFile("testdata/camera_capture_oracle.js")
	if err != nil {
		t.Fatal(err)
	}
	b, err := New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	c := b.NewContext()
	defer c.Close()
	p, err := c.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	navigateCapabilityFixture(t, p)
	if err = c.SetPermission(originOf(p.URL()), "camera", "granted"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	result, err := p.Evaluate(ctx, string(probe))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var observation any
	if err = json.Unmarshal(encoded, &observation); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(observation, capture.Observation) {
		t.Fatalf("camera Chrome152 divergence: got %s\nwant %s", encoded, data)
	}
}

func TestCameraFormatSelection(t *testing.T) {
	parallelBrowserTest(t)
	formats := []camera.Format{{Width: 640, Height: 480, FrameRate: 30}, {Width: 1280, Height: 720, FrameRate: 60}}
	f, err := selectCameraFormat(formats, map[string]any{"width": map[string]any{"ideal": float64(1280)}})
	if err != nil || f.Width != 1280 {
		t.Fatalf("ideal selection: %v %v", f, err)
	}
	_, err = selectCameraFormat(formats, map[string]any{"frameRate": map[string]any{"min": float64(120)}})
	if fmt.Sprint(err) != "Cannot satisfy camera constraint frameRate" {
		t.Fatalf("required constraint: %v", err)
	}
	f, err = selectCameraFormat(formats, map[string]any{"advanced": []any{map[string]any{"width": float64(1280)}, map[string]any{"width": float64(9999)}}})
	if err != nil || f.Width != 1280 {
		t.Fatalf("advanced requirements and skipped unsatisfied dictionary: %v %v", f, err)
	}
}
