package browser

import (
	"context"
	"testing"
	"time"
)

// Exercise the public bindings, ICE/DTLS/SRTP, native encoder and decoder, and
// the shared remote video/canvas frame boundary rather than accepting an SDP.
func TestCameraWebRTCVideoRoundTrip(t *testing.T) {
	serialBrowserTest(t)
	cameraTestPages(t, func(t *testing.T, p *Page) {
		provider := &fixtureCameraProvider{}
		p.ctx.browser.cameraProvider = provider
		navigateCapabilityFixture(t, p)
		if err := p.ctx.SetPermission(originOf(p.URL()), "camera", "granted"); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		result, err := p.Evaluate(ctx, cameraWebRTCRoundTripScript)
		if err != nil || result != true {
			for _, event := range p.trace.Events() {
				if event.Kind == "error" || event.Kind == "exception" {
					t.Logf("RTC task failure: %s %+v", event.Name, event.Data)
				}
			}
			diagnostic, _ := p.Evaluate(context.Background(), `JSON.stringify({ log: globalThis.rtcDebug, states: rtcCameraPeers.map((p) => p.connectionState) });`)
			t.Logf("RTC diagnostic: %v", diagnostic)
			t.Fatalf("real video: %v %v", result, err)
		}
	})
}

const cameraWebRTCRoundTripScript = `(async () => {
  const stream = await navigator.mediaDevices.getUserMedia({ video: true });
  globalThis.rtcCameraStream = stream;
  const a = new RTCPeerConnection(),
    b = new RTCPeerConnection();
  globalThis.rtcCameraPeers = [a, b];
  globalThis.rtcDebug = [];
  a.addEventListener('error', (e) => rtcDebug.push('a error ' + e.message));
  b.addEventListener('error', (e) => rtcDebug.push('b error ' + e.message));
  const data = a.createDataChannel('camera-control');
  const dataOpened = new Promise(
    (resolve) =>
      (data.onopen = () => {
        rtcDebug.push('opened');
        resolve();
      }),
  );
  const echoed = new Promise((resolve) => (data.onmessage = (event) => resolve(event.data)));
  b.ondatachannel = (event) => {
    rtcDebug.push('incoming channel');
    event.channel.onmessage = (message) => {
      rtcDebug.push('incoming message');
      event.channel.send(message.data);
    };
  };
  const placeholder = a.addTransceiver('video');
  const placeholderSender = placeholder.sender;
  const sender = a.addTrack(stream.getVideoTracks()[0], stream);
  if (a.getTransceivers()[0] !== placeholder || placeholderSender !== sender)
    throw new Error(
      'lazy transceiver identity: transceiver=' +
        (a.getTransceivers()[0] === placeholder) +
        ' sender=' +
        (placeholderSender === sender) +
        ' count=' +
        a.getTransceivers().length,
    );
  if (a.getSenders()[0] !== sender || a.getTransceivers()[0].sender !== sender)
    throw new Error('sender identity');
  const incoming = new Promise((resolve) => (b.ontrack = resolve));
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
  const event = await incoming;
  if (event.track.id !== stream.getVideoTracks()[0].id) throw new Error('wire track identity');
  if (
    !(event instanceof RTCTrackEvent) ||
    event.track !== event.receiver.track ||
    b.getReceivers()[0] !== event.receiver ||
    b.getTransceivers()[0] !== event.transceiver ||
    event.streams[0].getVideoTracks()[0] !== event.track
  )
    throw new Error('receiver identity');
  const video = document.createElement('video');
  video.srcObject = event.streams[0];
  await video.play();
  const canvas = document.createElement('canvas');
  canvas.width = video.videoWidth;
  canvas.height = video.videoHeight;
  const drawing = canvas.getContext('2d');
  drawing.drawImage(video, 0, 0);
  const left = drawing.getImageData(1, 1, 1, 1).data,
    right = drawing.getImageData(canvas.width - 2, 1, 1, 1).data;
  if (left[0] < 180 || left[1] > 75 || right[1] < 180 || right[0] > 75)
    throw new Error('decoded pixels: ' + left + ' / ' + right);
  if (a.connectionState !== 'connected' || b.connectionState !== 'connected')
    throw new Error('connection');
  const stats = await a.getStats();
  if (![...stats.values()].some((s) => s.type === 'outbound-rtp' && s.packetsSent > 0))
    throw new Error('send stats');
  rtcDebug.push('video checked');
  await dataOpened;
  data.send('video is live');
  rtcDebug.push('sent');
  if ((await echoed) !== 'video is live') throw new Error('data channel');
  if (!globalThis.rtcLeaveOpen) {
    await sender.replaceTrack(null);
    if (sender.track !== null) throw new Error('replace null');
    a.close();
    b.close();
    stream.getTracks().forEach((t) => t.stop());
  }
  return true;
})();`

// Leaving connected peers alive exercises document ownership of native codecs,
// transport readers, data queues and capture, rather than script cleanup.
func TestCameraWebRTCDocumentTeardown(t *testing.T) {
	serialBrowserTest(t)
	for _, action := range []string{"navigation", "close"} {
		t.Run(action, func(t *testing.T) {
			cameraTestPages(t, func(t *testing.T, p *Page) {
				provider := &fixtureCameraProvider{}
				p.ctx.browser.cameraProvider = provider
				navigateCapabilityFixture(t, p)
				if err := p.ctx.SetPermission(originOf(p.URL()), "camera", "granted"); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				result, err := p.Evaluate(ctx, "globalThis.rtcLeaveOpen = true;\n"+cameraWebRTCRoundTripScript)
				if err != nil || result != true {
					t.Fatalf("connected video: %v %v", result, err)
				}
				finished := make(chan error, 1)
				go func() {
					if action == "navigation" {
						finished <- p.Navigate(ctx, p.URL())
					} else {
						finished <- p.Close()
					}
				}()
				select {
				case err := <-finished:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("document teardown retained native media work")
				}
				if provider.opens.Load() != 1 || provider.closes.Load() != 1 {
					t.Fatalf("capture retained: opened=%d closed=%d", provider.opens.Load(), provider.closes.Load())
				}
			})
		})
	}
}
