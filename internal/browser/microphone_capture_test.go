package browser

import (
	"context"
	"fmt"
	"github.com/moreveal/mimic/internal/microphone"
	"math"
	"sync/atomic"
	"testing"
	"time"
)

type emptyMicrophoneProvider struct{}

func (emptyMicrophoneProvider) Devices(context.Context) ([]microphone.Device, error) { return nil, nil }
func (emptyMicrophoneProvider) Open(context.Context, string, microphone.Format) (microphone.Capture, error) {
	return nil, fmt.Errorf("no fixture microphone")
}

type fixtureMicrophoneProvider struct{ opens, closes atomic.Int32 }

func (*fixtureMicrophoneProvider) Devices(context.Context) ([]microphone.Device, error) {
	return []microphone.Device{{ID: "fixture-microphone", Label: "Fixture microphone", Default: true}}, nil
}
func (p *fixtureMicrophoneProvider) Open(_ context.Context, _ string, f microphone.Format) (microphone.Capture, error) {
	p.opens.Add(1)
	return &fixtureMicrophoneCapture{provider: p, format: f}, nil
}

type fixtureMicrophoneCapture struct {
	provider *fixtureMicrophoneProvider
	format   microphone.Format
	frame    int
	closed   bool
}

func (c *fixtureMicrophoneCapture) Read(ctx context.Context) ([]float32, error) {
	timer := time.NewTimer(20 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
	}
	samples := make([]float32, 960*c.format.Channels)
	for frame := 0; frame < 960; frame++ {
		for channel := 0; channel < c.format.Channels; channel++ {
			samples[frame*c.format.Channels+channel] = float32(.2 * math.Sin(2*math.Pi*float64(440+220*channel)*float64(c.frame)/48000))
		}
		c.frame++
	}
	return samples, nil
}
func (c *fixtureMicrophoneCapture) Close() error {
	if !c.closed {
		c.closed = true
		c.provider.closes.Add(1)
	}
	return nil
}

func microphoneTestPage(t *testing.T, p *Page) *fixtureMicrophoneProvider {
	t.Helper()
	provider := &fixtureMicrophoneProvider{}
	p.ctx.browser.microphoneProvider = provider
	p.ctx.browser.cameraProvider = &fixtureCameraProvider{}
	navigateCapabilityFixture(t, p)
	if err := p.ctx.SetPermission(originOf(p.URL()), "microphone", "granted"); err != nil {
		t.Fatal(err)
	}
	return provider
}

func TestMicrophoneCapturePermissionsClonesAndPCM(t *testing.T) {
	serialBrowserTest(t)
	cameraTestPages(t, func(t *testing.T, p *Page) {
		provider := microphoneTestPage(t, p)
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		// Trusted protocol input activates the existing AudioContext policy.
		if err := p.DispatchProtocolInput(ctx, "Input.dispatchKeyEvent", map[string]any{"type": "keyDown", "key": "a", "code": "KeyA"}); err != nil {
			t.Fatal(err)
		}
		result, err := p.Evaluate(ctx, `(async () => {
  const devices = await navigator.mediaDevices.enumerateDevices();
  const mic = devices.find((d) => d.kind === 'audioinput');
  if (mic.label !== 'Fixture microphone' || !mic.deviceId) throw Error('microphone enumeration');
  globalThis.micStream = await navigator.mediaDevices.getUserMedia({
    audio: { deviceId: { exact: mic.deviceId }, channelCount: { exact: 2 } },
  });
  globalThis.micTrack = micStream.getAudioTracks()[0];
  globalThis.micClone = micTrack.clone();
  const s = micTrack.getSettings();
  if (
    s.sampleRate !== 48000 ||
    s.channelCount !== 2 ||
    s.echoCancellation !== false ||
    micTrack.kind !== 'audio'
  )
    throw Error('PCM settings');
  const audio = document.createElement('audio');
  audio.srcObject = micStream;
  await audio.play();
  if (audio.readyState !== 4 || audio.paused) throw Error('audio readiness');
  globalThis.micContext = new AudioContext({ sampleRate: 48000 });
  const source = micContext.createMediaStreamSource(micStream),
    analyser = micContext.createAnalyser();
  analyser.fftSize = 256;
  source.connect(analyser);
  await micContext.resume();
  await new Promise((r) => setTimeout(r, 120));
  const waveform = new Float32Array(256);
  analyser.getFloatTimeDomainData(waveform);
  if (!waveform.some((v) => Math.abs(v) > 0.04))
    throw Error('captured waveform is silent: ' + Array.from(waveform.slice(0, 8)));
  micTrack.enabled = false;
  await new Promise((r) => setTimeout(r, 100));
  analyser.getFloatTimeDomainData(waveform);
  if (waveform.some((v) => v !== 0) || !micClone.enabled)
    throw Error('disabled silence or clone independence');
  micTrack.stop();
  if (
    micTrack.readyState !== 'ended' ||
    micClone.readyState !== 'live' ||
    micTrack.getSettings().sampleRate !== 48000 ||
    micTrack.getSettings().channelCount !== 2
  )
    throw Error('clone ownership or stopped settings');
  await micContext.close();
  return true;
})();`)
		if err != nil || result != true {
			t.Fatalf("microphone observations: %v %v", result, err)
		}
		if provider.opens.Load() != 1 || provider.closes.Load() != 0 {
			t.Fatal("clone prematurely released capture")
		}
		if err := p.ctx.SetPermission(originOf(p.URL()), "microphone", "denied"); err != nil {
			t.Fatal(err)
		}
		result, err = p.Evaluate(ctx, `(async () => {
  await new Promise((r) => setTimeout(r, 30));
  if (micClone.readyState !== 'ended') throw Error('revocation');
  try {
    await navigator.mediaDevices.getUserMedia({ audio: true });
    return false;
  } catch (e) {
    return e.name === 'NotAllowedError';
  }
})();`)
		if err != nil || result != true {
			t.Fatalf("microphone revocation: %v %v", result, err)
		}
		if provider.closes.Load() != 1 {
			t.Fatalf("capture not released: %d", provider.closes.Load())
		}
	})
}

func TestMicrophoneConstraintsAndCombinedCaptureRollback(t *testing.T) {
	serialBrowserTest(t)
	cameraTestPages(t, func(t *testing.T, p *Page) {
		provider := microphoneTestPage(t, p)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := p.ctx.SetPermission(originOf(p.URL()), "camera", "granted"); err != nil {
			t.Fatal(err)
		}
		result, err := p.Evaluate(ctx, `(async () => {
  for (const [property, value] of [
    ['echoCancellation', true],
    ['sampleRate', 44100],
    ['channelCount', 3],
  ]) {
    try {
      await navigator.mediaDevices.getUserMedia({ audio: { [property]: { exact: value } } });
      throw Error('accepted ' + property);
    } catch (e) {
      if (e.name !== 'OverconstrainedError' || e.constraint !== property) throw e;
    }
  }
  try {
    await navigator.mediaDevices.getUserMedia({ audio: true, video: { width: { exact: 999 } } });
    return false;
  } catch (e) {
    return e.name === 'OverconstrainedError' && e.constraint === 'width';
  }
})();`)
		if err != nil || result != true {
			t.Fatalf("constraints and rollback: %v %v", result, err)
		}
		deadline := time.Now().Add(time.Second)
		for provider.closes.Load() < 1 && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if provider.opens.Load() != 1 || provider.closes.Load() != 1 {
			t.Fatalf("failed combined request leaked microphone: %d/%d", provider.opens.Load(), provider.closes.Load())
		}
	})
}

func TestMicrophoneWebRTCAudioVideoRoundTripAndTeardown(t *testing.T) {
	serialBrowserTest(t)
	for _, action := range []string{"close", "navigation"} {
		t.Run(action, func(t *testing.T) {
			cameraTestPages(t, func(t *testing.T, p *Page) {
				provider := microphoneTestPage(t, p)
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				if err := p.ctx.SetPermission(originOf(p.URL()), "camera", "granted"); err != nil {
					t.Fatal(err)
				}
				if err := p.DispatchProtocolInput(ctx, "Input.dispatchKeyEvent", map[string]any{"type": "keyDown", "key": "a", "code": "KeyA"}); err != nil {
					t.Fatal(err)
				}
				channels := 1
				if action == "close" {
					channels = 2
				}
				result, err := p.Evaluate(ctx, fmt.Sprintf("globalThis.rtcAudioChannels = %d;\n", channels)+`(async () => {
  globalThis.avStream = await navigator.mediaDevices.getUserMedia({
    audio: { channelCount: { exact: rtcAudioChannels } },
    video: true,
  });
  const a = new RTCPeerConnection(),
    b = new RTCPeerConnection();
  globalThis.audioPeers = [a, b];
  globalThis.audioErrors = [];
  a.addEventListener('error', (e) => audioErrors.push(e.message));
  b.addEventListener('error', (e) => audioErrors.push(e.message));
  const placeholder = a.addTransceiver('audio');
  const sender = a.addTrack(avStream.getAudioTracks()[0], avStream);
  a.addTrack(avStream.getVideoTracks()[0], avStream);
  if (sender !== placeholder.sender || a.getTransceivers()[0] !== placeholder)
    throw Error('audio placeholder identity');
  try {
    await sender.replaceTrack(avStream.getVideoTracks()[0]);
    throw Error('accepted wrong kind');
  } catch (e) {
    if (!(e instanceof TypeError)) throw e;
  }
  const events = [];
  const incoming = new Promise(
    (resolve) =>
      (b.ontrack = (e) => {
        events.push(e);
        if (events.length === 2) resolve();
      }),
  );
  const gathered = async (peer) => {
    if (peer.iceGatheringState === 'complete') return;
    await new Promise((resolve) =>
      peer.addEventListener('icegatheringstatechange', function changed() {
        if (peer.iceGatheringState === 'complete') {
          peer.removeEventListener('icegatheringstatechange', changed);
          resolve();
        }
      }),
    );
  };
  await a.setLocalDescription(await a.createOffer());
  await gathered(a);
  await b.setRemoteDescription(a.localDescription);
  await b.setLocalDescription(await b.createAnswer());
  await gathered(b);
  await a.setRemoteDescription(b.localDescription);
  await incoming;
  if (
    events[0].streams[0] !== events[1].streams[0] ||
    events[0].streams[0].getTracks().length !== 2
  )
    throw Error('mixed stream identity');
  const audio = events.find((e) => e.track.kind === 'audio');
  if (audio.track !== audio.receiver.track || !b.getReceivers().includes(audio.receiver))
    throw Error('audio receiver identity');
  const element = document.createElement('audio');
  element.srcObject = audio.streams[0];
  await element.play();
  globalThis.audioContext = new AudioContext({ sampleRate: 48000 });
  const source = audioContext.createMediaStreamSource(audio.streams[0]),
    analyser = audioContext.createAnalyser();
  analyser.fftSize = 512;
  source.connect(analyser);
  await audioContext.resume();
  let peak = 0;
  for (let attempt = 0; attempt < 25 && peak < 0.04; attempt++) {
    await new Promise((r) => setTimeout(r, 40));
    const data = new Float32Array(512);
    analyser.getFloatTimeDomainData(data);
    peak = Math.max(...data.map(Math.abs));
  }
  if (peak < 0.04) throw Error('decoded Opus waveform silent: ' + peak + ' / ' + audioErrors);
  const stats = await a.getStats();
  if (
    ![...stats.values()].some(
      (s) => s.type === 'outbound-rtp' && s.kind === 'audio' && s.packetsSent > 0,
    )
  )
    throw Error('audio packet stats');
  if (audioErrors.length) throw Error(audioErrors.join(';'));
  return true;
})();`)
				if err != nil || result != true {
					t.Fatalf("audio/video transport: %v %v", result, err)
				}
				if action == "close" {
					err = p.Close()
				} else {
					err = p.Navigate(ctx, p.URL())
				}
				if err != nil {
					t.Fatal(err)
				}
				if provider.opens.Load() != 1 || provider.closes.Load() != 1 {
					t.Fatalf("document retained audio capture: %d/%d", provider.opens.Load(), provider.closes.Load())
				}
			})
		})
	}
}
