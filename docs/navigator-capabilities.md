# Navigator capability domains — Chrome 152

Reference: `Chrome/152.0.7977.82`, Windows x64, headful, fresh controlled profile, no feature overrides; synthetic local pages. `Runtime.evaluate` uses `userGesture:false`. Network and OS-service availability reflect machine state, not constants of the Chrome version.

Contexts in the exposure columns: **secure / secure+COOP+COEP / opaque about:blank**. Additional insecure Window data was captured on `data:`. This report does not certify other profiles, origin trials, PWAs, managed apps, or iframe permissions.

Full shape matched in **108/108** observations; original HEAD `73d6b8b49d05d6c1cf27695e0c67560bac1ef0eb`: **0/108**. In the secure context, **50/54** operation results matched; in opaque, **53/54**. For isolated contexts, only shape was checked here, not operations. This is a limited set of observations, not a browser compatibility percentage.

Checks cover owners, descriptors, native Function#toString, name/length, function own keys, non-constructible methods/getters, readonly constructor.prototype, inheritance chains, constructor identity, same-object behavior, absence of leaked own fields, and illegal receivers.

| member / domain | Chrome exposed? | Mimic exposed? | descriptor parity | prototype parity | capability/value parity | semantics / capability-only / unsupported |
|---|---|---|---|---|---|---|
| `vendorSub` / Legacy identity | yes/yes/yes | yes/yes/yes | yes | yes | matched in probes | semantics implemented |
| `productSub` / Legacy identity | yes/yes/yes | yes/yes/yes | yes | yes | matched in probes | semantics implemented |
| `appCodeName` / Legacy identity | yes/yes/yes | yes/yes/yes | yes | yes | matched in probes | semantics implemented |
| `doNotTrack` / Legacy identity | yes/yes/yes | yes/yes/yes | yes | yes | matched in probes | semantics implemented |
| `userActivation` / Input | yes/yes/yes | yes/yes/yes | yes | yes | matched in probes | capability-only; input activation implemented |
| `scheduling` / Input | yes/yes/yes | yes/yes/yes | yes | yes | matched in probes | capability-only; input activation implemented |
| `keyboard` / Input | yes/yes/no | yes/yes/no | yes | yes | matched in probes | capability-only; input activation implemented |
| `virtualKeyboard` / Input | yes/yes/no | yes/yes/no | yes | yes | matched in probes | capability-only; input activation implemented |
| `ink` / Input | yes/yes/yes | yes/yes/yes | yes | yes | matched in probes | capability-only; input activation implemented |
| `permissions` / Permissions / credentials | yes/yes/yes | yes/yes/yes | yes | yes | matched in probes | capability-only; shared permission store implemented |
| `credentials` / Permissions / credentials | yes/yes/no | yes/yes/no | yes | yes | matched in probes | capability-only; shared permission store implemented |
| `clipboard` / Permissions / credentials | yes/yes/no | yes/yes/no | yes | yes | matched in probes | capability-only; shared permission store implemented |
| `geolocation` / Permissions / credentials | yes/yes/yes | yes/yes/yes | yes | yes | matched in probes | capability-only; shared permission store implemented |
| `connection` / Network / storage | yes/yes/yes | yes/yes/yes | yes | yes | differences: secure: connection, opaque: connection | semantics implemented for tested operations; see limits |
| `storage` / Network / storage | yes/yes/no | yes/yes/no | yes | yes | matched in probes | semantics implemented for tested operations; see limits |
| `storageBuckets` / Network / storage | yes/yes/no | yes/yes/no | yes | yes | matched in probes | semantics implemented for tested operations; see limits |
| `locks` / Network / storage | yes/yes/no | yes/yes/no | yes | yes | matched in probes | semantics implemented for tested operations; see limits |
| `webkitTemporaryStorage` / Network / storage | yes/yes/yes | yes/yes/yes | yes | yes | matched in probes | semantics implemented for tested operations; see limits |
| `webkitPersistentStorage` / Network / storage | yes/yes/yes | yes/yes/yes | yes | yes | matched in probes | semantics implemented for tested operations; see limits |
| `mediaDevices` / Media | yes/yes/no | yes/yes/no | yes | yes | capability probes plus native audio/video evidence | native camera and microphone capture; [scope and limits](camera.md) |
| `mediaCapabilities` / Media | yes/yes/yes | yes/yes/yes | yes | yes | matched in probes | capability-only |
| `mediaSession` / Media | yes/yes/yes | yes/yes/yes | yes | yes | matched in probes | capability-only |
| `presentation` / Media | yes/yes/no | yes/yes/no | yes | yes | matched in probes | capability-only |
| `bluetooth` / Devices | yes/yes/no | yes/yes/no | yes | yes | differences: secure: bluetooth.available | capability-only; transport unsupported by design |
| `hid` / Devices | yes/yes/no | yes/yes/no | yes | yes | matched in probes | capability-only; transport unsupported by design |
| `serial` / Devices | yes/yes/no | yes/yes/no | yes | yes | differences: secure: serial.ports | capability-only; transport unsupported by design |
| `usb` / Devices | yes/yes/no | yes/yes/no | yes | yes | matched in probes | capability-only; transport unsupported by design |
| `devicePosture` / Devices | yes/yes/no | yes/yes/no | yes | yes | matched in probes | capability-only; transport unsupported by design |
| `serviceWorker` / Browser services | yes/yes/no | yes/yes/no | yes | yes | matched in probes | capability-only; service backends unsupported by design |
| `wakeLock` / Browser services | yes/yes/no | yes/yes/no | yes | yes | differences: secure: wakeLock.request | capability-only; service backends unsupported by design |
| `windowControlsOverlay` / Browser services | yes/yes/yes | yes/yes/yes | yes | yes | matched in probes | capability-only; service backends unsupported by design |
| `xr` / Browser services | yes/yes/no | yes/yes/no | yes | yes | matched in probes | capability-only; service backends unsupported by design |
| `managed` / Browser services | yes/yes/no | yes/yes/no | yes | yes | matched in probes | capability-only; service backends unsupported by design |
| `login` / Browser services | yes/yes/no | yes/yes/no | yes | yes | matched in probes | capability-only; service backends unsupported by design |
| `protectedAudience` / Privacy | yes/yes/no | yes/yes/no | yes | yes | matched in probes | capability-only; auctions unsupported by design |
| `deprecatedRunAdAuctionEnforcesKAnonymity` / Privacy | yes/yes/no | yes/yes/no | yes | yes | matched in probes | capability-only; auctions unsupported by design |

## State and boundaries

- Legacy: fixed Blink rules and the DoNotTrack preference; these are not values taken from a site.
- Input: trusted `Page.DispatchInput`, the UserInteraction queue, and Scheduler time; synthetic dispatchEvent does not activate the page. Supports mousedown/keydown/touchend, expiry, and sticky activation. This is not complete CDP Input, hit-testing, or the full activation propagation/consumption algorithm. KeyboardLayoutMap reads the layout from Environment. No actual keyboard lock, virtual keyboard, or ink renderer is provided.
- Permissions: one origin store in Context, shared by query, location, clipboard, media, and keyboard; PermissionStatus changes are delivered as tasks. The full set of specialized descriptor algorithms (MIDI sysex, requestedOrigin, fullscreen, and others) is not implemented. Credential transport, WebAuthn/FedCM, and ClipboardItem transport are absent. Location does not fabricate a position without a backend.
- Network/storage: connection reads Environment.Network. Quota uses a shared profile; as in Chrome, Web Storage is excluded from StorageManager usage. Bucket metadata/lifecycle, expiry, and persistence are supported. Quota-consuming IDB/Cache/OPFS clients are absent: estimate is not real disk usage. Locks supports queuing, shared/exclusive modes, callback lifetime, ifAvailable, abort, and release on realm closure; steal is explicitly unsupported. The two legacy quota getters return the same object without a public constructor.
- Media: native camera/microphone enumeration, permission redaction, and shared sources support camera streams, microphone PCM, H264 video and Opus audio over WebRTC. See [camera, microphone and WebRTC capture](camera.md) for tested configurations and explicit unsupported boundaries. This is not a complete decoder/encoder/EME pipeline or a performance test of arbitrary resolutions/bitrates. MediaSession stores playback/action/position state; MediaMetadata/PresentationRequest constructors and Presentation transport are not implemented.
- Devices: one empty transport registry, not independent successful devices. HID/USB/Serial return empty lists; a chooser without activation is rejected. Bluetooth getDevices is absent in the selected Chrome profile. On this machine, Bluetooth availability sometimes does not settle within 1500 ms and sometimes returns false; Mimic reports the absence of an adapter immediately.
- Browser services: no service-worker execution, XR session backend, presentation receiver, managed configuration, or system power backend. Controller/registration queries describe an empty state; ready remains pending. Unsupported operations explicitly reject instead of resolving to null through semanticMissing. Chrome with a power backend returns WakeLockSentinel; Mimic rejects the request. This is an intentional difference, not demonstrated Chrome parity. Window controls overlay is disabled for a regular window. Login status is stored per origin.
- Privacy: observed capability responses are supported; the auctions/interest-group backend is not implemented. Related Navigator operations from the same IDL source explicitly reject their Promise instead of resolving to null through semanticMissing. deprecatedRunAdAuctionEnforcesKAnonymity is false in the selected profile.

Exposure is selected from the pinned snapshot and may then be restricted by explicit Environment.Features. An unknown or enabled override does not add APIs beyond the snapshot. Sources and conditions for partial/mixin declarations and conditional Exposed(Window Feature) are preserved; provenance is not replaced with merged Navigator conditions.

## Verifiable artifacts

- [Headful Chrome capture](../compatibility/captures/navigator-chrome152.json), [mode-specific headless capture](../compatibility/captures/navigator-chrome152-headless.json), [original HEAD](../compatibility/captures/navigator-mimic-baseline.json), [Mimic after changes](../compatibility/captures/navigator-mimic-current.json).
- [Oracle policy](oracle-policy.md) and [headless expectations audit](oracle-headless-audit.md).
- [Exact operation differences](navigator-capability-differences.json), [provenance and conditions for each member](navigator-capability-provenance.json).
- [Shared capture scenario](../compatibility/navigator_capabilities.py), [probes](../compatibility/probes-navigator-capabilities.json), [regression tests](../internal/browser/capabilities_test.go).
- [109 pinned IDL sources](../chrome/152/generated/navigator-idl-sources.json), [reproducible reconstruction](../tools/refresh_navigator_idl.py), [parser tests](../tools/test_generate_compat.py).

The matrix does not imply complete implementation of every method on these interfaces. `capability-only` and `unsupported by design` remain explicit boundaries even when shape matches completely.

Camera and microphone operations now use real device capture and explicit Context/origin grants; interactive prompt waiting is not part of the automation runtime. See [camera, microphone and WebRTC capture](camera.md) for lazy ownership, bundled codecs, current limits, and retained hardware evidence. The historical operation totals above describe the original capability probes.

The operation regression preserves and asserts the original Chrome prompt and
device observations before checking the requested automation policy separately:
ungranted clipboard, keyboard-lock and capture requests reject with
`NotAllowedError`. Input enumeration uses deterministic provider fixtures,
retains Chrome's audio-input-before-video-input ordering and permission
redaction, and omits unsupported speaker outputs. The frozen capture and shared
capture probes remain unchanged; these differences are not Chrome parity claims.
