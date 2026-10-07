# Mimic v0.2.1

Changes since v0.2.0:

## Camera, microphone and WebRTC

- Capture real cameras, including OBS Virtual Camera, and native microphone inputs. `enumerateDevices` and `getUserMedia` support device selection, permission redaction, required constraints, shared-source clones and combined audio/video streams.
- Send and receive H264 video and Opus audio over real ICE/DTLS/SRTP connections. Mixed audio/video tracks share a canonical remote stream; text and binary data channels use SCTP. Camera frames feed video, canvas and bitmap observations, and microphone/received audio feed Web Audio analyser readbacks.
- Allocate capture and transport on demand, start encoders after connection, and bound frame/audio queues. Navigation and Page teardown release devices, transport and codecs. Capture uses native platform adapters without an FFmpeg process; the selected OpenH264 binary and audio dependencies are bundled, with no runtime download.
- Project camera and microphone permissions from the existing BrowserContext/origin store through JavaScript and CDP. Automation grants access explicitly; ungranted requests reject immediately without an interactive permission wait. Revocation ends the corresponding local capture tracks.
- Correct QuickJS error construction so it does not invoke subclass accessors before initialization, and return numeric Window/Worker timer handles across engines.

## Developer preview

- Add light, dark and system themes, fit/zoom controls, and a passive viewport that preserves target dimensions and keeps viewer input isolated from the automated Page.
- Show bounded live activity for commands, lifecycle, network, console and exceptions, with filters, search, pause/resume and reconnect support. Slow viewers do not stall Page execution; queue overflow is reported explicitly.

## Verification and limits

Retained frozen Chrome 152 diagnostics verify OBS video and USB microphone audio in both directions, with decoded observations and real packet counts. Focused tests cover capture, permissions, constraints, clones, disabled tracks, mixed audio/video transport and teardown in Goja, V8 and QuickJS. Release packaging verifies extracted Windows/Linux archives, all three engines, public clients and checksums.

Video transmission currently supports H264 up to 1280×720 at 30 fps. Microphones deliver 48 kHz mono/stereo PCM and Opus audio. Echo cancellation, noise suppression, automatic gain control, voice isolation, speaker output, simulcast and sender parameter changes remain unsupported. Required unavailable processing constraints reject rather than simulate success. macOS hardware capture has not been tested locally, and no macOS release archive is provided. See the [capture contract and limitations](https://github.com/mimic-browser/runtime/blob/v0.2.1/docs/camera.md).

Mimic remains a renderer-free public beta for Windows and Linux amd64. Full source changes: [v0.2.0...v0.2.1](https://github.com/mimic-browser/runtime/compare/v0.2.0...v0.2.1).
