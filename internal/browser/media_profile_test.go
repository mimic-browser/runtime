package browser

import (
	"context"
	"encoding/json"
	"image"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	chrome152 "github.com/moreveal/mimic/chrome/152"
	"github.com/moreveal/mimic/internal/camera"
	v8engine "github.com/moreveal/mimic/internal/engine/v8"
)

const mediaFixtureProfile = `{
 "seed":"persona-42",
 "devices":[
  {"key":"desk-camera","kind":"videoinput","source":{"label":"Fixture camera"},"label":"USB Camera","group":"desk",
   "modes":[{"width":64,"height":32,"frameRate":30},{"width":32,"height":16,"frameRate":15}],
   "defaultMode":{"width":64,"height":32,"frameRate":30},"processing":{"resize":"crop-and-scale","noise":2}},
  {"key":"desk-microphone","kind":"audioinput","source":{"label":"Fixture microphone"},"label":"Microphone (USB Camera)","group":"desk"}
 ]
}`

func TestMediaProfileIdentityConstraintsAndPixels(t *testing.T) {
	serialBrowserTest(t)
	cameraTestPages(t, func(t *testing.T, p *Page) {
		provider := &fixtureCameraProvider{}
		p.ctx.browser.cameraProvider = provider
		p.ctx.browser.microphoneProvider = &fixtureMicrophoneProvider{}
		if _, err := p.ctx.SetMediaProfileJSON([]byte(mediaFixtureProfile)); err != nil {
			t.Fatal(err)
		}
		if provider.opens.Load() != 0 {
			t.Fatal("configuration opened capture")
		}
		navigateCapabilityFixture(t, p)
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		value, err := p.Evaluate(ctx, `navigator.mediaDevices
  .enumerateDevices()
  .then((ds) => ds.length === 2 && ds.every((d) => !d.label && !d.deviceId && !d.groupId));`)
		if err != nil || value != true {
			t.Fatalf("redaction: %v %v", value, err)
		}
		for _, kind := range []string{"camera", "microphone"} {
			if err := p.ctx.SetPermission(originOf(p.URL()), kind, "granted"); err != nil {
				t.Fatal(err)
			}
		}
		value, err = p.Evaluate(ctx, `(async () => {
  const devices = await navigator.mediaDevices.enumerateDevices();
  const camera = devices.find((d) => d.kind === 'videoinput'),
    microphone = devices.find((d) => d.kind === 'audioinput');
  if (
    camera.label !== 'USB Camera' ||
    microphone.label !== 'Microphone (USB Camera)' ||
    camera.groupId !== microphone.groupId ||
    camera.deviceId === microphone.deviceId
  )
    throw new Error('catalog');
  globalThis.profileDeviceId = camera.deviceId;
  globalThis.profileStream = await navigator.mediaDevices.getUserMedia({
    audio: { deviceId: { exact: microphone.deviceId } },
    video: { deviceId: { exact: camera.deviceId } },
  });
  const audio = profileStream.getAudioTracks()[0];
  if (
    audio.label !== microphone.label ||
    audio.getSettings().deviceId !== microphone.deviceId ||
    audio.getSettings().groupId !== camera.groupId
  )
    throw new Error('paired microphone identity');
  const track = (globalThis.profileTrack = profileStream.getVideoTracks()[0]);
  const clone = (globalThis.profileClone = track.clone());
  if (
    track.label !== camera.label ||
    clone.label !== camera.label ||
    track.getSettings().deviceId !== camera.deviceId ||
    track.getCapabilities().deviceId !== camera.deviceId
  )
    throw new Error('identity');
  const capabilities = track.getCapabilities();
  if (
    capabilities.width.max !== 64 ||
    capabilities.height.max !== 32 ||
    capabilities.frameRate.max !== 30
  )
    throw new Error('capabilities');
  if (track.getSettings().width !== 64 || track.getSettings().height !== 32)
    throw new Error('default');
  const video = (globalThis.profileVideo = document.createElement('video'));
  video.srcObject = profileStream;
  await video.play();
  const canvas = document.createElement('canvas');
  canvas.width = 64;
  canvas.height = 32;
  const drawing = canvas.getContext('2d');
  drawing.drawImage(video, 0, 0);
  if (video.videoWidth !== 64 || video.videoHeight !== 32) throw new Error('dimensions');
  const left = drawing.getImageData(1, 1, 1, 1).data,
    right = drawing.getImageData(62, 1, 1, 1).data;
  if (left[0] < 245 || left[1] > 15 || right[1] < 245 || right[0] > 15)
    throw new Error('transformed pixels');
  const before = track.getSettings();
  try {
    await track.applyConstraints({ width: { exact: 65 } });
    throw new Error('accepted impossible width');
  } catch (e) {
    if (e.name !== 'OverconstrainedError' || e.constraint !== 'width') throw e;
  }
  const after = track.getSettings();
  if (Object.keys(before).some((key) => before[key] !== after[key]))
    throw new Error('partial mutation');
  await clone.applyConstraints({
    width: { exact: 32 },
    height: { exact: 16 },
    frameRate: { exact: 15 },
  });
  if (clone.getSettings().width !== 32 || track.getSettings().width !== 64)
    throw new Error('clone selection');
  const cloneVideo = document.createElement('video');
  cloneVideo.srcObject = new MediaStream([clone]);
  await cloneVideo.play();
  if (cloneVideo.videoWidth !== 32 || cloneVideo.videoHeight !== 16)
    throw new Error('clone output');
  await new Promise((resolve) =>
    cloneVideo.requestVideoFrameCallback((now, meta) => {
      if (meta.width !== 32 || meta.height !== 16) throw new Error('frame metadata');
      resolve();
    }),
  );
  if (
    JSON.stringify({
      devices,
      settings: track.getSettings(),
      capabilities: track.getCapabilities(),
      label: clone.label,
    }).includes('Fixture')
  )
    throw new Error('native identity leak');
  return true;
})();`)
		if err != nil || value != true {
			t.Fatalf("profile observations: %v %v", value, err)
		}
		before := p.ctx.MediaProfile()
		if _, err := p.ctx.SetMediaProfileJSON([]byte(`{"devices":[]}`)); err == nil {
			t.Fatal("changed catalog during live capture")
		}
		if !reflect.DeepEqual(before, p.ctx.MediaProfile()) {
			t.Fatal("rejected profile partially changed catalog")
		}
		value, err = p.Evaluate(ctx, `profileStream.getTracks().forEach(track => track.stop()); profileClone.stop(); true;`)
		if err != nil || value != true {
			t.Fatal(value, err)
		}
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			p.ctx.mu.RLock()
			active := p.ctx.mediaCaptures
			p.ctx.mu.RUnlock()
			if provider.closes.Load() == 1 && active == 0 {
				break
			}
			time.Sleep(time.Millisecond)
		}
		if provider.opens.Load() != 1 || provider.closes.Load() != 1 {
			t.Fatalf("capture lifetime: %d/%d", provider.opens.Load(), provider.closes.Load())
		}
		if _, err := p.ctx.SetMediaProfileJSON([]byte(`{"devices":[]}`)); err != nil {
			t.Fatal(err)
		}
		value, err = p.Evaluate(ctx, `navigator.mediaDevices.enumerateDevices().then((ds) => ds.length === 0);`)
		if err != nil || value != true {
			t.Fatalf("empty configured catalog: %v %v", value, err)
		}
	})
}

func TestMediaProfileGenerationValidationAndIsolation(t *testing.T) {
	serialBrowserTest(t)
	cameraTestPages(t, func(t *testing.T, p *Page) {
		p.ctx.browser.cameraProvider = &fixtureCameraProvider{}
		p.ctx.browser.microphoneProvider = &fixtureMicrophoneProvider{}
		request := []byte(`{"seed":"same-persona","camera":{"source":{"label":"Fixture camera"},"profile":"auto"}}`)
		first, err := p.ctx.SetMediaProfileJSON(request)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(first.Devices[0].Label, "Fixture") || strings.Contains(first.Devices[0].Profile, "virtual") {
			t.Fatal("native/virtual identity generated")
		}
		again, err := p.ctx.SetMediaProfileJSON([]byte(`{"camera":{"source":{"label":"Fixture camera"},"profile":"auto"}}`))
		if err != nil {
			t.Fatal(err)
		}
		if first.Seed != again.Seed || first.Devices[0].Profile != again.Devices[0].Profile {
			t.Fatal("auto rerolled within context")
		}
		other := p.ctx.browser.NewContext()
		defer other.Close()
		second, err := other.SetMediaProfileJSON(request)
		if err != nil {
			t.Fatal(err)
		}
		if first.Devices[0].Profile != second.Devices[0].Profile || first.Devices[0].Label != second.Devices[0].Label {
			t.Fatal("same seed selected different complete profile")
		}
		if reflect.DeepEqual(first.Devices[0].Source, second.Devices[0].Source) {
			t.Fatal("native binding IDs not context scoped")
		}
		navigateCapabilityFixture(t, p)
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		readID := func(page *Page) string {
			t.Helper()
			if err := page.ctx.SetPermission(originOf(page.URL()), "camera", "granted"); err != nil {
				t.Fatal(err)
			}
			value, err := page.Evaluate(ctx, `navigator.mediaDevices
  .enumerateDevices()
  .then((devices) => devices.find((device) => device.kind === 'videoinput').deviceId);`)
			if err != nil {
				t.Fatal(err)
			}
			return value.(string)
		}
		id := readID(p)
		sibling, err := p.ctx.NewPage()
		if err != nil {
			t.Fatal(err)
		}
		defer sibling.Close()
		if err := sibling.Navigate(ctx, p.URL()); err != nil {
			t.Fatal(err)
		}
		if readID(sibling) != id {
			t.Fatal("same Context/origin pages have different logical identity")
		}
		isolated, err := other.NewPage()
		if err != nil {
			t.Fatal(err)
		}
		defer isolated.Close()
		if err := isolated.Navigate(ctx, p.URL()); err != nil {
			t.Fatal(err)
		}
		if readID(isolated) == id {
			t.Fatal("logical device identity leaked across Contexts")
		}
		navigateCapabilityFixture(t, sibling)
		if readID(sibling) == id {
			t.Fatal("logical device identity leaked across origins")
		}
		if err := p.Navigate(ctx, p.URL()); err != nil {
			t.Fatal(err)
		}
		if readID(p) != id || p.ctx.MediaProfile().Devices[0].Profile != first.Devices[0].Profile {
			t.Fatal("navigation rerolled identity or recipe")
		}
		for _, raw := range []string{
			`{"camera":{"source":{"label":"Fixture camera"},"profile":"obs"}}`,
			`{"camera":{"source":{"label":"Fixture camera"},"profile":"usb-webcam-hd","overrides":{"modes":[{"width":3840,"height":2160,"frameRate":120}],"defaultMode":{"width":3840,"height":2160,"frameRate":120}}}}`,
			`{"camera":{"source":{"label":"Fixture camera"},"profile":"usb-webcam","overrides":{"capabilities":{"width":{"max":9000}}}}}`,
			`{"camera":{"source":{"label":"missing"},"profile":"auto"}}`,
			`{"camera":{"source":{"label":"Fixture camera"},"profile":"auto"},"devices":[]}`,
			`{"camera":null}`,
			`{"camera":{"source":null}}`,
			`{"camera":{"overrides":{"label":null}}}`,
		} {
			if _, err := p.ctx.SetMediaProfileJSON([]byte(raw)); err == nil {
				t.Fatalf("accepted invalid profile: %s", raw)
			}
		}
		if !reflect.DeepEqual(&again, p.ctx.MediaProfile()) {
			t.Fatal("invalid override changed valid profile")
		}
		public := p.ctx.MediaProfile()
		public.Devices[0].Modes[0].Width = 9999
		if p.ctx.MediaProfile().Devices[0].Modes[0].Width == 9999 {
			t.Fatal("snapshot shares mutable profile state")
		}
		variants := map[string]bool{}
		for i := 0; i < 64; i++ {
			seed, _ := json.Marshal(i)
			profile, err := mediaPreset("auto", string(seed), "camera")
			if err != nil {
				t.Fatal(err)
			}
			variants[profile.Profile] = true
			if err := validateMediaCamera(&profile); err != nil {
				t.Fatal(err)
			}
		}
		if len(variants) != 4 {
			t.Fatalf("auto does not cover physical recipe catalog: %v", variants)
		}
	})
}

func TestMediaProfileFrameReuseNoiseAndRateReduction(t *testing.T) {
	t.Parallel()
	input := image.NewRGBA(image.Rect(0, 0, 8, 4))
	for i := range input.Pix {
		input.Pix[i] = 128
	}
	output := &cameraOutput{}
	stamp := time.Unix(10, 0)
	first := output.observe(input, stamp, 1, 4, 2, 15, 4, true)
	again := output.observe(input, stamp, 1, 4, 2, 15, 4, true)
	if first.image != again.image || first.sequence != again.sequence {
		t.Fatal("read count changed observed pixels")
	}
	dropped := output.observe(input, stamp.Add(time.Second/30), 2, 4, 2, 15, 4, true)
	if dropped.image != first.image || dropped.sequence != first.sequence {
		t.Fatal("output exceeded selected FPS")
	}
	next := output.observe(input, stamp.Add(time.Second/15), 3, 4, 2, 15, 4, true)
	if next.sequence != first.sequence+1 || reflect.DeepEqual(next.image.Pix, first.image.Pix) {
		t.Fatal("temporal noise did not change actual output frame")
	}
	clone := (&cameraOutput{}).observe(input, stamp.Add(time.Second/15), 3, 4, 2, 15, 4, true)
	if !reflect.DeepEqual(next.image.Pix, clone.image.Pix) {
		t.Fatal("same source/frame recipe produced different pixels")
	}
	black := output.observe(input, stamp.Add(time.Second/15), 3, 4, 2, 15, 4, false)
	for i, v := range black.image.Pix {
		if i%4 != 3 && v != 0 {
			t.Fatal("disabled track leaked source pixels")
		}
	}
	oldGeneration := output.currentGeneration()
	output.invalidate()
	if observation := output.observeAt(oldGeneration, input, stamp.Add(time.Second), 4, 4, 2, 15, 4, true); observation.image != nil || output.frame != nil {
		t.Fatal("stale sender snapshot republished an earlier output recipe")
	}
	output.close()
	if observation := output.observe(input, stamp.Add(time.Second), 4, 4, 2, 15, 4, true); observation.image != nil || output.frame != nil {
		t.Fatal("late worker observation resurrected a stopped output cache")
	}
}

func TestMediaProfileJointConstraintSelection(t *testing.T) {
	t.Parallel()
	d, err := mediaPreset("usb-webcam-fhd", "fixture", "camera")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name        string
		constraints map[string]any
		want        camera.Format
	}{
		{"default", map[string]any{}, camera.Format{Width: 1280, Height: 720, FrameRate: 30}},
		{"clamp ideal to bounds", map[string]any{"width": map[string]any{"min": float64(600), "max": float64(800), "ideal": float64(1200)}, "height": float64(480)}, camera.Format{Width: 800, Height: 480, FrameRate: 30}},
		{"portrait", map[string]any{"width": map[string]any{"exact": float64(360)}, "aspectRatio": map[string]any{"exact": float64(0.5)}}, camera.Format{Width: 360, Height: 720, FrameRate: 30}},
		{"joint ratio", map[string]any{"width": map[string]any{"min": float64(300), "max": float64(400)}, "height": map[string]any{"exact": float64(600)}, "aspectRatio": map[string]any{"exact": float64(0.5)}}, camera.Format{Width: 300, Height: 600, FrameRate: 30}},
		{"rate reduction without resize", map[string]any{"width": map[string]any{"exact": float64(640)}, "height": map[string]any{"exact": float64(480)}, "frameRate": map[string]any{"exact": float64(15)}, "resizeMode": map[string]any{"exact": "none"}}, camera.Format{Width: 640, Height: 480, FrameRate: 15}},
		{"advanced whole sets", map[string]any{"advanced": []any{map[string]any{"width": float64(320), "height": float64(240), "frameRate": float64(15)}, map[string]any{"width": float64(9999)}, map[string]any{"width": float64(640)}}}, camera.Format{Width: 320, Height: 240, FrameRate: 15}},
	} {
		t.Run(test.name, func(t *testing.T) {
			actual, err := selectMediaCameraFormat(&d, test.constraints)
			if err != nil || actual.Format != test.want {
				t.Fatalf("selection: %+v %v, want %+v", actual, err, test.want)
			}
		})
	}
	for _, constraints := range []map[string]any{
		{"width": map[string]any{"min": float64(800), "max": float64(600)}},
		{"width": map[string]any{"exact": float64(100)}, "height": map[string]any{"exact": float64(100)}, "aspectRatio": map[string]any{"exact": float64(0.5)}},
		{"width": map[string]any{"exact": float64(1921)}},
		{"resizeMode": map[string]any{"exact": "unsupported"}},
	} {
		if _, err := selectMediaCameraFormat(&d, constraints); err == nil {
			t.Fatalf("accepted impossible constraints: %v", constraints)
		}
	}
	advancedCrop, err := selectMediaCameraFormat(&d, map[string]any{"advanced": []any{map[string]any{"width": float64(640), "height": float64(480), "resizeMode": "crop-and-scale"}}})
	if err != nil || advancedCrop.Width != 640 || advancedCrop.ResizeMode != "crop-and-scale" {
		t.Fatalf("advanced resize projection: %+v %v", advancedCrop, err)
	}
	conflict, err := selectMediaCameraFormat(&d, map[string]any{"resizeMode": map[string]any{"exact": "crop-and-scale"}, "advanced": []any{map[string]any{"width": float64(640), "resizeMode": "none"}}})
	if err != nil || conflict.Width != 1280 || conflict.ResizeMode != "crop-and-scale" {
		t.Fatalf("conflicting advanced set was not skipped atomically: %+v %v", conflict, err)
	}
}

func TestMediaProfileWebRTCOutputSwitchAndStats(t *testing.T) {
	serialBrowserTest(t)
	cameraTestPages(t, func(t *testing.T, p *Page) {
		p.ctx.browser.cameraProvider = &fixtureCameraProvider{}
		p.ctx.browser.microphoneProvider = &fixtureMicrophoneProvider{}
		if _, err := p.ctx.SetMediaProfileJSON([]byte(mediaFixtureProfile)); err != nil {
			t.Fatal(err)
		}
		navigateCapabilityFixture(t, p)
		if err := p.ctx.SetPermission(originOf(p.URL()), "camera", "granted"); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		value, err := p.Evaluate(ctx, "globalThis.rtcLeaveOpen = true;\n"+cameraWebRTCRoundTripScript)
		if err != nil || value != true {
			t.Fatal(value, err)
		}
		value, err = p.Evaluate(ctx, `(async () => {
  const [sender, receiver] = rtcCameraPeers;
  const track = rtcCameraStream.getVideoTracks()[0];
  await track.applyConstraints({
    width: { exact: 32 },
    height: { exact: 16 },
    frameRate: { exact: 15 },
  });
  const video = document.createElement('video');
  video.srcObject = new MediaStream([receiver.getReceivers()[0].track]);
  await video.play();
  for (let i = 0; i < 30; i++)
    await new Promise((resolve) => video.requestVideoFrameCallback(resolve));
  if (
    video.videoWidth !== 32 ||
    video.videoHeight !== 16 ||
    track.getSettings().width !== 32 ||
    track.getSettings().frameRate !== 15
  )
    throw new Error('output switch');
  const outgoing = [...(await sender.getStats()).values()].filter(
    (s) => s.type === 'outbound-rtp' && s.kind === 'video',
  );
  const incoming = [...(await receiver.getStats()).values()].filter(
    (s) => s.type === 'inbound-rtp' && s.kind === 'video',
  );
  if (outgoing.length !== 1 || incoming.length !== 1)
    throw new Error('duplicate stats ' + outgoing.length + '/' + incoming.length);
  const sent = outgoing[0],
    received = incoming[0];
  if (
    sent.frameWidth !== 32 ||
    sent.frameHeight !== 16 ||
    sent.framesEncoded <= 0 ||
    sent.framesSent !== sent.framesEncoded ||
    sent.framesPerSecond <= 0 ||
    sent.framesPerSecond > 17 ||
    received.frameWidth !== 32 ||
    received.frameHeight !== 16 ||
    received.framesDecoded <= 0
  )
    throw new Error('measured stats ' + JSON.stringify({ sent, received }));
  sender.close();
  receiver.close();
  rtcCameraStream.getTracks().forEach((t) => t.stop());
  return true;
})();`)
		if err != nil || value != true {
			t.Fatal(value, err)
		}
	})
}

func TestMediaProfileStatsMergePreservesLinksAndDirection(t *testing.T) {
	t.Parallel()
	s := &rtcStatsInterceptor{streams: []*rtcPacketStats{{ssrc: 7, typeName: "outbound-rtp", kind: "video"}, {ssrc: 7, typeName: "inbound-rtp", kind: "video"}}}
	s.encodedFrame(7, 640, 480, time.Now())
	s.decodedFrame(7, 320, 240, false, time.Now())
	rows := map[string]any{"native-out": map[string]any{"id": "native-out", "type": "outbound-rtp", "ssrc": float64(7), "codecId": "codec", "framesEncoded": float64(0)}}
	s.mergeInto(rows)
	row := rows["native-out"].(map[string]any)
	if len(rows) != 2 || row["id"] != "native-out" || row["codecId"] != "codec" || row["frameWidth"] != 640 || row["framesEncoded"] != uint64(1) || rows["inbound-rtp-7"].(map[string]any)["frameWidth"] != 320 {
		t.Fatal(rows)
	}
}

func TestSystemMediaProfileCamera(t *testing.T) {
	serialBrowserTest(t)
	label := os.Getenv("MIMIC_TEST_MEDIA_PROFILE_CAMERA")
	if label == "" {
		t.Skip("requires an opted-in native camera")
	}
	b, err := New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	c := b.NewContext()
	defer c.Close()
	request, err := json.Marshal(map[string]any{"camera": map[string]any{"source": map[string]any{"label": label}, "profile": "usb-webcam-hd", "overrides": map[string]any{"label": "USB Camera"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.SetMediaProfileJSON(request); err != nil {
		t.Fatal(err)
	}
	p, err := c.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	navigateCapabilityFixture(t, p)
	if err := c.SetPermission(originOf(p.URL()), "camera", "granted"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	value, err := p.Evaluate(ctx, `(async () => {
  globalThis.mediaProfileHardwarePhase = 'enumeration';
  const devices = await navigator.mediaDevices.enumerateDevices();
  if (devices.length !== 1 || devices[0].label !== 'USB Camera')
    throw new Error('private native source');
  globalThis.mediaProfileHardwarePhase = 'capture';
  const stream = await navigator.mediaDevices.getUserMedia({
    video: { width: { exact: 320 }, height: { exact: 240 }, frameRate: { exact: 15 } },
  });
  const track = stream.getVideoTracks()[0];
  globalThis.mediaProfileHardwareTrack = track;
  const video = document.createElement('video');
  globalThis.mediaProfileHardwareVideo = video;
  video.srcObject = stream;
  globalThis.mediaProfileHardwarePhase = 'video play';
  await video.play();
  globalThis.mediaProfileHardwarePhase = 'next frame';
  globalThis.mediaProfileHardwareFrames = [];
  for (let index = 0; index < 8; index++)
    await new Promise((resolve) =>
      video.requestVideoFrameCallback((now, metadata) => {
        mediaProfileHardwareFrames.push({
          width: metadata.width,
          height: metadata.height,
          mediaTime: metadata.mediaTime,
        });
        resolve();
      }),
    );
  const canvas = document.createElement('canvas');
  canvas.width = 320;
  canvas.height = 240;
  canvas.getContext('2d').drawImage(video, 0, 0);
  globalThis.mediaProfileHardwarePhase = 'bitmap';
  const bitmap = await createImageBitmap(video);
  if (
    video.videoWidth !== 320 ||
    video.videoHeight !== 240 ||
    bitmap.width !== 320 ||
    bitmap.height !== 240 ||
    track.getSettings().frameRate !== 15 ||
    track.getCapabilities().width.max !== 1280 ||
    track.label !== 'USB Camera'
  )
    throw new Error('native profile observations');
  const clone = track.clone();
  await clone.applyConstraints({
    width: { exact: 640 },
    height: { exact: 480 },
    frameRate: { exact: 30 },
  });
  const cloneVideo = document.createElement('video');
  cloneVideo.srcObject = new MediaStream([clone]);
  globalThis.mediaProfileHardwarePhase = 'clone play';
  await cloneVideo.play();
  if (
    cloneVideo.videoWidth !== 640 ||
    cloneVideo.videoHeight !== 480 ||
    track.getSettings().width !== 320
  )
    throw new Error('native clone independence');
  globalThis.mediaProfileHardwareSummary = {
    label: track.label,
    settings: track.getSettings(),
    capabilities: track.getCapabilities(),
    frames: mediaProfileHardwareFrames,
    clone: clone.getSettings(),
    bitmap: [bitmap.width, bitmap.height],
  };
  bitmap.close();
  track.stop();
  clone.stop();
  return true;
})();`)
	if err != nil || value != true {
		diagnosticContext, diagnosticCancel := context.WithTimeout(context.Background(), time.Second)
		defer diagnosticCancel()
		phase, _ := p.Evaluate(diagnosticContext, "({phase: globalThis.mediaProfileHardwarePhase, track: globalThis.mediaProfileHardwareTrack?.readyState, time: globalThis.mediaProfileHardwareVideo?.currentTime, width: globalThis.mediaProfileHardwareVideo?.videoWidth})")
		p.commandMu.Lock()
		for _, track := range p.Top.Realm.cameraTracks {
			track.source.mu.RLock()
			t.Logf("native input: format=%+v sequence=%d latest=%s", track.source.format, track.source.sequence, track.source.stamp)
			track.source.mu.RUnlock()
		}
		p.commandMu.Unlock()
		t.Fatal(value, err, phase, c.MediaDiagnostics())
	}
	summary, err := p.Evaluate(ctx, "JSON.stringify(globalThis.mediaProfileHardwareSummary)")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("native profile observations: %v", summary)
}
