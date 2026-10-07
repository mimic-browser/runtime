# Physical camera profile sample: frozen Chrome 152

This sample informs the
[media device profile mechanism](../media-device-profiles-design.md). It does
not certify other camera models or make this device
a production dependency.

## Provenance

The native input was `WebCamera (1bcf:e307)`, independently identified by the
Windows device catalog as the connected physical `WebCamera` input. Meta Quest
capture inputs and OBS Virtual Camera were also enumerated; none was selected
for this measurement.

The reference was directly launched Chrome 152.0.7977.82, Chromium
`d04cdb24d67b081f6cf80200ffc5233f44b61109`, in normal headful mode with a fresh
dedicated profile, a fixed nonzero CDP port and the standard oracle launch
arguments. The window was hidden by Windows. Its original `navigator.webdriver`
was `false`; no browser APIs or properties were overridden.

This was a diagnostic experiment: CDP explicitly granted camera/microphone
permissions, evaluations used user gestures, constraints changed the selected
track, and a same-browser WebRTC loopback exercised real encoding and decoding.
It is not a passive production-site capture. Microphone enumeration was observed;
microphone PCM was not captured. Only frame hashes and numeric summaries were
retained, without camera images.

The recorder is
[`capture_media_device_profile.py`](../../tools/compatibility/capture_media_device_profile.py);
the exact probe is
[`media_device_profile_probe.js`](../../tools/compatibility/media_device_profile_probe.js).
Every completed phase was saved before the next measurement.

Private evidence is retained at
`.build/physical-camera-profile-20261007-webcamera`, including the exact launch
record, original identity, complete CDP transcript, device list, constraints,
frame observations, transport statistics and a SHA-256 inventory. The complete
`capture.json` SHA-256 is:

```text
7fc2fda9296b4fec748d848b6ebf1ffe7416104302f0bb361f6e12b7f25e6525
```

The earlier selection-only capture is preserved separately at
`.build/physical-camera-profile-20261007`. It could not automatically choose
among multiple video inputs. The subsequent capture explicitly selected the
physical device after the saved list and Windows inventory resolved that
ambiguity. No successful physical observation was overwritten.

## Observations

The webcam microphone had exactly the same web-visible `groupId` as the video
input. This relation was observed, not inferred from its device name.

Initial settings were 1280x720 at 30 FPS, `resizeMode: 'none'`.

| Exact requested output | Observed result                                                                                             |
| ---------------------- | ----------------------------------------------------------------------------------------------------------- |
| 640x480 at 30 FPS      | Accepted; matching settings and presented dimensions.                                                       |
| 1280x720 at 30 FPS     | Accepted; matching settings and presented dimensions.                                                       |
| 1920x1080 at 30 FPS    | Accepted; matching settings and presented dimensions.                                                       |
| 1280x720 at 60 FPS     | `OverconstrainedError`, constraint `frameRate`; previous 1920x1080 at 30 settings and constraints retained. |
| 320x240 at 15 FPS      | Accepted; matching settings, `resizeMode: 'crop-and-scale'`.                                                |
| 10000x10000 at 30 FPS  | `OverconstrainedError`, constraint `width`; previous 320x240 at 15 settings and constraints retained.       |

Capabilities reported width 1..2560, height 1..1440, FPS 0..30, and resize modes
`none` and `crop-and-scale`. Those independent range extrema are not a measured
list of native modes or proof that every combination is satisfiable. The probe
did not enumerate the entire feasible dimension/FPS space.

Additional capabilities included brightness, contrast, color temperature,
exposure mode/time, focus mode/distance, saturation, sharpness and white-balance
mode. The presence and defaults were observed; changes to those controls and
their image response were not exercised. No pan, tilt, zoom or torch capability
appeared in this sample. Their absence is valid physical-camera behavior.

A cloned track had independent identity, the same label, source identifiers and
capabilities. Immediately after its 640x480 constraint application resolved,
its settings still reported 1280x720 while resize mode changed to
`crop-and-scale`. The original remained 1280x720. The clone was then stopped;
this capture does not establish its subsequent frame/settings transition.
Do not turn the immediate read into a permanent constraint-selection rule.

At restored 1280x720 at 30 FPS, 90 local frame observations had:

- Media-time intervals: minimum 31.940 ms, median 33.334 ms, maximum 79.999 ms.
- 90 distinct hashes of the 64x36 downsampled readbacks.
- Identical repeated reads of each already-drawn canvas buffer.

These values describe one scene, light condition and diagnostic run. Hash
changes do not independently prove sensor noise, exposure behavior or physical
camera identity. Presented-frame counts do not expose every hardware frame.

The actual same-browser WebRTC sender adapted to 480x270 despite 1280x720 track
settings. Sender statistics reported 25 FPS, 61 encoded/sent frames, 299 packets
and 213116 payload bytes at the observation boundary. Receiver statistics
reported the same dimensions, FPS, packets/bytes and 61 decoded frames, with zero
dropped frames at that boundary. The 60 sampled decoded frames had median media
interval 37 ms, with a 28..52 ms observed range.

Therefore, differing source settings and encoded output dimensions can be
consistent Chrome behavior. A profile must keep its capture state coherent while
transport statistics reflect the actual encoder adaptation. Copying capture
settings into invented WebRTC statistics would misrepresent this observation.

## Targeted clone transition diagnostic

The saved immediate clone result could not answer whether adaptation occurs
before a sink consumes frames. A separate capture at
`.build/physical-camera-clone-transition-20261007-webcamera` retained the same
normal launch conditions, original webdriver state and exact recorder/probe
copies before changing instrumentation. Its `capture.json` SHA-256 is:

```text
2bd8f5e33cbb2fb31d900c1b6ccb7a813f95222ae433e4f4b22593b1013d10a8
```

Immediately after applying 640x480 to the clone, and after 150 ms without a sink,
its settings remained 1280x720 with `crop-and-scale`. After attaching a video
element and observing three callbacks, clone settings became 640x480 while the
original remained 1280x720. Callback metadata reported 960x720 in those three
observations. This establishes sink-dependent adaptation and distinct settings
and callback boundaries for this driver/run; it does not establish every later
presentation/readback dimension. Do not turn the first immediate settings into
a permanent rejection or force raw callback dimensions to equal every setting.
The generic class recipes do not claim to reproduce this specific driver.

Both successful captures' SHA-256 inventories were verified offline. Reuse their
phase files for iterative analysis; neither successful capture was overwritten.

## Remaining evidence needed

Before certifying this exact model/driver or changing related generic semantics,
measure later clone presentation/readback transitions, capabilities after stop,
control changes and their response, and multi-track source adaptation. Other
camera classes and platforms require their own evidence. Reuse this saved
capture for every question it already answers.
