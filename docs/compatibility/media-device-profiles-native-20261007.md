# Native media profile verification: Windows, 2026-10-07

The opt-in `TestSystemMediaProfileCamera` exercised the real connected
`WebCamera` input and an already running `OBS Virtual Camera` through the same
Context profile API and V8 bindings. These are runtime integration checks,
separate from the [frozen Chrome sample](media-device-profile-webcamera-20261007.md).
No camera images were retained, and neither device is a production dependency.

Both inputs used `usb-webcam-hd` with web-visible label `USB Camera`.
The requested output was 320×240 at 15 FPS; the track, video, eight subsequent
frame callbacks and image bitmap agreed on those dimensions. Capabilities were
derived from the HD recipe (maximum 1280×720 at 30 FPS). A clone independently
selected 640×480 at 30 FPS, while the original retained 320×240 at 15.
The tracks shared logical device/group IDs and released capture after stopping.

Retained logs:

- `.build/media-profile-obs-native-20261007.log`
- `.build/media-profile-browser-focused-20261007.log`

OBS callback media times spanned approximately 0.074–0.606 seconds across the
eight observations; physical-camera times spanned approximately 0.081–0.640.
These are actual admitted-frame times, not a guarantee of precise nominal FPS.

## Capture graph correction

Before the DirectShow correction, OBS supplied one frame and then the capture
reader timed out repeatedly: the track remained live, source sequence remained
1, and the next frame callback did not arrive within 15 seconds. The selected
native mode was 2560×1440 at approximately 60 FPS. Changing initial requested
output/native selection did not correct this symptom.

The capture-only DirectShow graph now sets its graph clock to null before
running, so the null renderer consumes arriving samples immediately. Hardware
and producer cadence remain source-owned. The same OBS continuity test then
passed, and the physical camera passed after the correction too.

This use follows Microsoft's
[SetSyncSource contract](https://learn.microsoft.com/en-us/windows/win32/api/strmif/nf-strmif-imediafilter-setsyncsource)
and [graph-clock guidance](https://learn.microsoft.com/en-us/windows/win32/directshow/setting-the-graph-clock).
A mismatch between producer and presentation epochs is a possible explanation
for the earlier stall; the underlying sample timestamps were not recorded, so
an exact timestamp cause is not claimed.

The fix is confined to the existing Windows capture adapter. Catalog validation,
generation, track processing and transport remain cross-platform; Linux/macOS
adapters retain their existing capture contracts. No dependency was added.

## Focused regression checks

The Browser checks cover all three engines, profile isolation/redaction,
paired microphone identity, cloning, successful/failing joint constraints,
actual pixels, noise/frame reuse, stale sender snapshots and teardown.
Real H264/ICE/DTLS/SRTP tests change output to 32×16 at 15 FPS during a connection
and verify decoded dimensions and measured sender/receiver statistics.
Existing microphone PCM, combined capture, revocation, Opus/video transport,
navigation and closure tests also pass.

CDP checks cover discovery, seeded class selection, validation atomicity,
private backend failure diagnostics, Context creation rollback, profile commands
and generated semantic coverage. Python checks verify deterministic projections;
the frozen Chrome schema is unchanged. These focused checks complement CI's
broader regression gates; they do not certify exact camera model controls or
complete conferencing application compatibility.
