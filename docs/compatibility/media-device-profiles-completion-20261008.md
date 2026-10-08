# Media device profiles: completion verification, 2026-10-08

MIM-15's implementation was already committed in `1563c24`
(`feat(media): add coherent context-owned camera profiles`). The issue's earlier
description of an uncommitted proposal was stale. This verification checks the
implemented contract and adds coverage for its capture ownership boundaries.

## Capture ownership regression

`TestMediaProfileCaptureLifecycleBlocksReplacement` runs camera and microphone
cases on Goja, V8 and QuickJS. Synthetic providers deliberately block native
`Open` and `Close`, independently of scheduler timing. For each input it verifies:

- Catalog replacement fails during opening, live capture and closing.
- Every rejection preserves the complete previous profile.
- A separate Context can update its catalog during each blocked phase.
- Capture closure releases its reservation and permits catalog replacement.

This exercises the actual JavaScript capture entry points and worker lifecycle;
it does not simulate ownership by manually incrementing the reservation counter.
No production behavior change was needed for these cases.

## Local verification

The following focused checks passed on Windows. Browser media tests use synthetic
providers and retained reference evidence; they do not open native devices or
launch Chrome.

```powershell
go test ./internal/browser -run '^TestMediaProfile' -count=1 -timeout=90s
go test ./internal/browser -run '^TestCamera(StreamPixelsCloneAndTeardown|PermissionConstraintsAndOriginIsolation|RevocationEndsClonesAndReleasesDevice|FormatSelection)$|^TestMicrophone(CapturePermissionsClonesAndPCM|ConstraintsAndCombinedCaptureRollback|WebRTCAudioVideoRoundTripAndTeardown|Chrome152Evidence)$' -count=1 -timeout=90s
go test ./internal/cdp -run '^TestMimicMediaProfileCommands$|^TestProtocolSupportManifestHasLiveEvidenceAndNoLostHandlers$|^TestProtocolCoverageUsesCompleteGeneratedInventory$' -count=1 -timeout=60s
python tools/generate_cdp.py --check
python -m unittest discover -s tools -p test_generate_cdp.py
```

Together these checks cover profile validation, seeded generation, private source
bindings, permissions, Context/origin isolation, clone constraints, coherent
pixels, frame reuse and rate reduction, measured WebRTC output, PCM capture,
revocation, teardown, CDP commands and deterministic support projections.
The new lifecycle regression additionally passed as a focused standalone test.
The full local suite was not run, and completion does not assert a CI result.

## Evidence and boundaries

The [Chrome 152 physical sample](media-device-profile-webcamera-20261007.md) and
[Windows native capture verification](media-device-profiles-native-20261007.md)
remain the evidence for physical-camera and OBS behavior. Those experiments
were not repeated for this completion check.

The [current contract](../media-device-profiles-design.md) retains its explicit
limits: generic class presets do not certify a camera model, native mode support
is checked when capture opens, and unmeasured hardware controls are unsupported.
This check adds no macOS hardware claim or broader conferencing compatibility
claim. The semantic support registry remains authoritative for the CDP surface.
