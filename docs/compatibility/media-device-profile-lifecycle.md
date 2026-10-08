# Media profile capture lifecycle

A media profile belongs to its BrowserContext. Its device catalog is replaceable
only when that Context has no capture opening, running, or closing. Reserving
capture before a provider's `Open` call and retaining ownership until `Close`
completes prevents a profile update from changing the identity or recipe of an
in-flight device.

A rejected replacement preserves the complete previous profile. Capture in one
Context does not prevent another Context from changing its own catalog. Once
capture cleanup releases its reservation, the original Context can replace the
catalog again.

## Focused regression coverage

`TestMediaProfileCaptureLifecycleBlocksReplacement` exercises camera and
microphone capture through the JavaScript entry points on Goja, V8 and QuickJS.
Synthetic providers deliberately block `Open` and `Close`, so the test verifies
ownership independently of scheduler timing. It covers replacement rejection
during each phase, preservation of the previous profile, independent Context
updates, and replacement after cleanup. It does not open physical devices.

Related media tests cover private source bindings, permissions, Context and
origin isolation, clone constraints, coherent pixels and PCM, revocation,
WebRTC output, and teardown. CDP profile commands observe the same Context-owned
state rather than maintain a separate device catalog.

## Evidence and limits

The [Chrome 152 physical sample](media-device-profile-webcamera-20261007.md) and
[native capture verification](media-device-profiles-native-20261007.md) retain
the provenance for measured physical-camera and OBS behavior. Synthetic lifecycle
coverage does not establish support for additional hardware or platforms.

The [media profile contract](../media-device-profiles-design.md) defines source
selection, public identity, and output recipes. Generic class presets do not
certify a specific camera model; native mode support is checked when capture
opens, and unmeasured hardware controls remain unsupported. The CDP semantic
support registry is authoritative for the exposed protocol surface.
