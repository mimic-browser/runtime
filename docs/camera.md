# Camera, microphone and WebRTC capture

Mimic captures real cameras, including OBS Virtual Camera, through the operating
system. Windows uses DirectShow, Linux uses V4L2, and macOS uses AVFoundation.
Capture needs a native build with cgo. There is no FFmpeg executable, Chromium
process, GPU, display server, or graphics backend in the capture pipeline.

Microphones use miniaudio through malgo, compiled into the executable, with
WASAPI on Windows, CoreAudio on macOS, and PulseAudio or ALSA on Linux. Virtual
audio inputs exposed by these backends are selectable like physical inputs.
Native enumeration and capture start only when requested. No extra audio DLL,
FFmpeg executable, or runtime dependency download is required.

## Permissions and device selection

Live permission decisions belong to the BrowserContext's origin capability
store. `navigator.permissions.query({name: 'camera'})`, CDP commands, and
`getUserMedia` observe this same state. Contexts and origins remain independent;
device and group identifiers are salted by Context and origin. Before a camera
grant, enumeration exposes at most one anonymous video input without its label
or identifiers. Enumeration reflects currently attached devices rather than a
process-wide cached device list.

Microphone permission uses the same store under `microphone`. Before a grant,
enumeration exposes at most one anonymous audio input. Grant it through
`Browser.setPermission` with `{name: 'microphone'}`, or `audioCapture` through
`Browser.grantPermissions`. Revocation ends local audio tracks independently of
camera permission. OS microphone access settings still apply.

Mimic has no interactive permission UI. A `prompt` or `denied` request rejects
with `NotAllowedError`; changing the selected environment profile does not
introduce a waiting prompt. Automation grants access explicitly:

```js
const cdp = await context.newCDPSession(page);
await cdp.send('Browser.setPermission', {
  permission: { name: 'camera' },
  setting: 'granted',
  origin: new URL(page.url()).origin,
});

await page.evaluate(async () => {
  const devices = await navigator.mediaDevices.enumerateDevices();
  const camera = devices.find((device) => device.label === 'OBS Virtual Camera');
  if (!camera) throw new Error('Requested camera is unavailable');
  const stream = await navigator.mediaDevices.getUserMedia({
    video: { deviceId: { exact: camera.deviceId } },
  });
  const video = document.createElement('video');
  video.srcObject = stream;
  await video.play();
  // drawImage(video), createImageBitmap(video), and video frame callbacks
  // observe the same captured source. Keep stream for a subsequent addTrack.
  globalThis.cameraStream = stream;
});
```

For an incognito context, supply its `browserContextId` to Browser permission
commands. `Browser.grantPermissions` accepts `videoCapture` and sets an origin
allowlist. `Browser.resetPermissions` restores the context's initial defaults.
Revoking a camera grant ends local capture tracks and releases the device; it
does not revoke reception of a remote WebRTC track. OS camera access settings
still apply to the Mimic executable.

## Capture ownership and observations

`getUserMedia({video: ...})` starts enumeration and capture on demand. Native
width, height, frame rate, aspect ratio, device and group constraints select a supported
capture mode. Required constraints which cannot be satisfied reject with
`OverconstrainedError`. `getSettings`, `getConstraints`, and `getCapabilities`
project the source and track state. Capabilities currently describe the selected
native mode; `applyConstraints` validates against that mode without reopening a
shared source. Cropping, software resizing of capture observations, camera
controls, and pan/tilt/zoom are unsupported. Desktop adapters do not infer facing
direction; an exact `facingMode` requirement rejects. Advanced dictionaries
currently filter native numeric modes; advanced device/group selection is not
implemented. The separate `ImageCapture` photo/control API is not implemented.

Each source retains its latest immutable CPU frame. Script frame notifications
are coalesced, with at most one outstanding notification per source, and video
encoding does not accumulate a frame backlog. A clone owns independent track
identity, enabled and stopped state, while sharing the same native capture.
Disabling a track produces black observations for that track. Stopping the final
live clone releases the camera and its retained frame. Navigation and Page
teardown cancel capture and wait for native resources to finish closing.

`getUserMedia({audio: true})` captures 48 kHz PCM in 20 ms blocks. `channelCount`
selects mono or stereo; `deviceId` and `groupId` select an enumerated input.
`getUserMedia({audio: ..., video: ...})` returns one stream with both tracks and
releases an already opened source if the other capture fails. Audio clones
share capture while keeping independent enabled/stopped states. Disabling an
audio track produces silence, and stopping the final clone closes the input.
Audio settings remain available after stopping, as observed in Chrome 152.

Echo cancellation, noise suppression, automatic gain control, and voice
isolation are unavailable: settings report `false`, and required `exact: true`
constraints reject with `OverconstrainedError`. Capture format constraints
validate against the supported 48 kHz, 16-bit, mono/stereo output. Applying
constraints does not reopen a shared device or implement a DSP pipeline.

Captured and received audio support media-element playback readiness and
`AudioContext.createMediaStreamSource`, including actual analyser PCM readbacks.
Each source retains a bounded 160 ms PCM history so overlapping graph reads
observe the same samples. Received Opus is decoded into 48 kHz stereo PCM;
settings describe that output. The runtime does not play sound through a speaker
or implement audio output device selection. Media-stream destination tracks
remain outside the native WebRTC sending boundary.

`HTMLVideoElement.srcObject`, playback readiness, frame callbacks, canvas
readbacks, and image bitmaps use this shared frame boundary. These observations
do not require a rendered window. Native modes are bounded to 8192 pixels per
dimension and 16 megapixels per frame to keep source allocations bounded.

## WebRTC transport

Attach camera and microphone tracks using `peer.addTrack(track, stream)`, exchange SDP and ICE
through the application's signaling service, and attach `track` event streams
to a video element. Connected state comes from Pion's actual ICE, DTLS and SRTP
transport. ICE server configuration supports STUN and TURN. RTP counters come
from actual packet reads and writes. Text and binary RTC data channels use SCTP
and bounded send queues. Packet loss feedback requests new H264 keyframes.

The transport and its certificate state are allocated when media is attached or
a remote session is negotiated. An unattached peer's offline capability offer
does not allocate a camera or encoder. Native H264 encoding starts only after
the connection is established, and decoding starts when remote media arrives.
Each encoder and decoder owns independent native state; Pages share only the
immutable loaded codec library. Closing a peer releases transport and codecs
without stopping the caller's independently owned camera stream.

Attach media before applying an offline offer. Migrating an already applied
offline session into a live transport rejects with `NotSupportedError` because
its session certificate and transport state cannot be reused.

Connected video negotiates H264 with packetization mode 1 and constrained
baseline compatibility. Transmission is capped at 1280×720 and 30 fps; local
capture observations retain their native dimensions. Audio negotiates Opus at
48 kHz with mono/stereo microphone encoding and remote audio decoding. Each
audio sender has a bounded two-block queue; encoding starts after connection,
and decoding starts on incoming RTP. Pion Opus is pure Go and allocates no native
codec library. Mixed audio/video events share a canonical remote MediaStream.

Other video codecs, simulcast, insertable streams, sender parameter changes,
and changing an existing transceiver's direction are explicit unsupported
boundaries. A grant never implies an implemented processing capability.

Do not infer full conferencing application compatibility from the camera API or
from Chrome's offline codec capability catalog. Applications requiring other
codecs, audio processing or unsupported sender controls need those capabilities
implemented separately.

## Distribution and evidence

Only the selected platform's compressed Cisco OpenH264 2.6.0 library is embedded
in the executable: Windows amd64, Linux amd64/arm64, or macOS amd64/arm64. The
Windows payload is 452,053 bytes compressed. The first codec use verifies its
recorded checksum, extracts it into a content-addressed temporary directory,
and loads the bundled library. Existing matching files are reused. No runtime
download or system codec installation is required. Binary provenance and full
terms are in `internal/videocodec/bundled`; release third-party notices include
the binary distribution license. Bundling does not assert Cisco's conditional
patent coverage for separately downloaded binaries.

Focused tests cover synthetic capture, origin/context isolation, prompt
completion, constraints, clones, disabled pixels, navigation teardown, and a
real H264/ICE/DTLS/SRTP encode/decode round trip through the public JS bindings.
Hardware tests are opt-in through `MIMIC_TEST_CAMERA=OBS Virtual Camera`.

Microphone tests cover actual PCM observations, disabled silence, selected
devices, required constraints, combined-capture rollback, permission revocation,
clones, and an Opus audio/video round trip with navigation and Page teardown in
Goja, V8 and QuickJS. Native microphone capture is opt-in through
`MIMIC_TEST_MICROPHONE=1`. Release notices include malgo, miniaudio and Pion Opus.
The pinned Opus source commit provides the encoder; the older tagged release
only provided decoding. Both are compiled into the executable through Go/cgo.

Realtime audio graph advancement retains one owned PCM window before processing
its quanta. This prevents a slow JavaScript engine from losing those samples to
the bounded native history while processing the same interval. Binary buffers
avoid base64 conversion on this path. Samples retain their original positions;
expired or unavailable samples remain silence. Source attachment synchronizes
the running context clock, and resuming a suspended context reconnects its graph
timeline to the current capture block. Regression coverage includes delayed
source attachment and suspension longer than the retained PCM history.

The retained Chrome 152 capture is
`internal/browser/testdata/camera_capture_chrome152.json`. Its launch metadata
records the direct headful launch, unmodified `navigator.webdriver === false`,
CDP permission grant, executable hash and probe hash. Reuse this capture for
track, stream, video and stopped-track observations.

A separate retained bilateral transport diagnostic under
`internal/browser/testdata/camera_webrtc_chrome152/` establishes actual OBS video from Mimic
to frozen Chrome 152 at 1280×720 and from Chrome to Mimic at 640×360, including
decoded canvas observations and packet counters. Windows native capture and
Windows/Linux codec round trips were tested locally. The macOS driver and
bundled codec use the same provider contract; macOS hardware capture has not
been verified on this host.

`internal/browser/testdata/microphone_webrtc_chrome152/` retains a bilateral
native USB microphone diagnostic against frozen Chrome 152, including decoded
audio levels, packet counts, settings, original webdriver state, exact binary
and probe hashes, trusted input activation, and document/network recording.
No microphone waveform is retained. The saved Chrome audio observation also
establishes that stopped local audio tracks retain their format settings.
Windows native capture and Windows/Linux component tests passed locally; macOS
hardware capture remains unverified on this host.
