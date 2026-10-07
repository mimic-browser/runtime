# Context-owned media device profiles

Mimic separates the capture source from the identity exposed to web pages.
A native input such as OBS Virtual Camera supplies frames; a Context-owned
profile supplies the logical device catalog, labels, groups and executable
output modes. **The user chooses identity; Mimic maintains invariants.**

Profiles affect enumeration, track labels, settings, capabilities, constraints,
clones, video/canvas/image bitmap observations and WebRTC input. Auto generation
contains physical-camera classes only. Native labels, paths and binding IDs do
not appear in a configured profile's web projections or backend error messages.

## Quick start

Configure the default Context before starting capture:

```js
await cdp.send("Mimic.setMediaProfile", {
  seed: "account-1842",
  camera: { source: "obs", profile: "auto" },
});
```

Use `profile: "usb-webcam"` or `"integrated-webcam"` to choose a class.
Omitting `profile` selects `auto`; omitting `source` selects the default native
camera. Add an actual microphone when needed:

```js
await cdp.send("Mimic.setMediaProfile", {
  seed: "account-1842",
  camera: { source: "obs", profile: "usb-webcam" },
  microphone: { source: "default", profile: "webcam" },
});
```

The microphone gets its own logical device ID and a shared group with the camera.
Its label derives from the selected camera label. This binds a real audio input;
it does not fabricate a microphone or implement DSP.

To install media before any Page exists, use the existing Context command:

```js
const { browserContextId, media } = await cdp.send("Mimic.createContext", {
  profile: { generate: { seed: "account-1842" } },
  media: {
    camera: { source: "obs", profile: "auto" },
    microphone: { source: "default" },
  },
});
```

Media configuration is a separate runtime option. Environment profile JSON
remains its existing CDP import/export format; it is not a CLI configuration
file or an arbitrary override document. There is one current media contract.

## CDP contract

| Command | Result and behavior |
| --- | --- |
| `Mimic.getMediaSources` | `{sources, diagnostics}`: native input discovery with opaque Context-bound `sourceId`, `kind`, native `label` and `default`. Does not open capture. |
| `Mimic.getMediaPresets` | `{presets}`: physical class IDs, labels and complete variant IDs. |
| `Mimic.validateMediaProfile` | `{profile, nativeModesVerified: false}`: compile and validate without changing state or opening capture. |
| `Mimic.setMediaProfile` | Same normalized result; atomically replaces the complete Context catalog. |
| `Mimic.getMediaProfile` | `{profile, diagnostics}`: resolved catalog or `null` when unconfigured, plus private last capture failures. |

All five accept optional `browserContextId`. Omission targets the server's
default Context, including on a Page session; supply the ID for other Contexts.
Set/validate accept `seed` and either `camera`/`microphone` shorthand or
`devices`, never both. Unknown fields, explicit nulls, invalid recipes and
missing/ambiguous source selectors reject with CDP error `-32602`.
These commands also appear in `Mimic.getCompatibilityMatrix().extensions`;
frozen Chrome wire schemas do not advertise Mimic extensions.

A source is `"default"`, `"obs"` (video only), `{sourceId: "..."}`, or
`{label: "exact native label"}`. Discovery and precise native diagnostics are
private automation information. Source selection must match exactly one input.
Source IDs are Context-bound and must be rebound when importing a resolved
catalog into another Context or host.

A configured catalog exposes only its listed inputs. `devices: []` exposes no
capture inputs. An unconfigured Context retains native enumeration. Unavailable
configured sources are omitted; capture cannot silently select an unlisted input.
Device order determines selection within each kind; web enumeration groups
audio inputs before video inputs, as in the retained Chrome capture.

## Generation and overrides

`auto` selects a whole recipe from four bounded class variants:

| Variant | Label | Base output modes | Default |
| --- | --- | --- | --- |
| `integrated-webcam-hd` | Integrated Camera | 640×480 and 1280×720, 30 FPS | 1280×720 @ 30 |
| `integrated-webcam-fhd` | Integrated Camera | HD modes plus 1920×1080 @ 30 | 1280×720 @ 30 |
| `usb-webcam-hd` | USB Camera | 640×480 and 1280×720, 30 FPS | 1280×720 @ 30 |
| `usb-webcam-fhd` | USB Camera | HD modes plus 1920×1080 @ 30 | 1280×720 @ 30 |

Class presets select their HD/FHD variant deterministically. Selection hashes
the seed and logical device key, never individual metadata fields.
Explicit seed wins. A successful set retains that seed for later configurations;
ordinary Contexts initially use their Context ID, environment-backed Contexts
use their environment identity, and `Mimic.createContext` supplies the environment
descriptor seed unless media specifies its own. Enumeration and navigation
never regenerate the catalog. Identical seeds select the same recipe, while web
IDs remain isolated by Context and origin.

These are physical **class recipes**, not certified copies of specific camera
models or drivers. No virtual identity is generated. Manufacturer specifications
alone cannot establish a model's Chrome capabilities, controls or lifecycle.

Shorthand camera `overrides` can contain `label`, `modes`, `defaultMode`
and `processing`. Changes are validated together. A preset's overridden modes
cannot exceed its dimensions or FPS. To author another bounded executable recipe,
use an advanced device without a preset:

```js
const { sources } = await cdp.send("Mimic.getMediaSources");
const source = sources.find(
  (input) => input.kind === "videoinput" && input.label === "OBS Virtual Camera",
);
if (!source) throw new Error("Capture source is unavailable");

await cdp.send("Mimic.setMediaProfile", {
  seed: "account-1842",
  devices: [{
    key: "desk-camera",
    kind: "videoinput",
    source: { sourceId: source.sourceId },
    label: "USB Camera",
    group: "desk",
    modes: [
      { width: 640, height: 480, frameRate: 30 },
      { width: 1280, height: 720, frameRate: 30 },
    ],
    defaultMode: { width: 1280, height: 720, frameRate: 30 },
    processing: { resize: "crop-and-scale", noise: 0 },
  }],
});
```

Advanced devices require a unique nonempty `key`, `kind` (`videoinput` or
`audioinput`), `source`, and nonempty `label`; `group` is optional and
defaults to the key. Video devices require complete modes and a default that is
one of them. Audio devices accept identity/grouping fields and no video
processing. At most 16 devices and 64 modes per camera are accepted. Width/height
are 1–8192, each frame is at most 16 megapixels, and mode FPS is 1–120.
An optional advanced video `profile` must name a resolved HD/FHD variant and
bounds its complete supplied recipe; use shorthand for `auto` or class generation.

## Output and constraints

Capabilities derive from executable recipes, never a separate injected settings
or capabilities bag. With `processing.resize: "crop-and-scale"`, the output
can be center-cropped/scaled to bounded integer dimensions within the recipe's
maximums, with aspect-ratio and joint constraints checked together. Lower FPS
discards input frames. Without processing (or with `resize: "none"`) a custom
recipe must have one mode matching an available native geometry.

Both initial capture and `applyConstraints` use the same selector. Required
values admit feasible outputs; ideals choose among them. Optional advanced sets
intersect the current feasible space as a whole and are skipped when impossible.
Base mode geometry reports `resizeMode: "none"`; adapted geometry reports
`"crop-and-scale"`. Required `none` limits selection to base geometries while
still permitting FPS reduction.

Impossible constraints reject with `OverconstrainedError` and retain the
previous settings, constraints and output selection. Clones share native input
but own independent output selections. Settings describe the selected logical
output; frame observations use the matching actual output pixels. Presentation
and callbacks still occur on later event-loop turns.

Native mode checks are deferred to capture startup. A source must support the
recipe's maximum nominal FPS, including for subsequent constraint changes.
With crop/scale enabled, native geometry targets the recipe's default mode,
independently of the first track's output request. Clones adapt that stable input.
Validation explicitly returns `nativeModesVerified: false`. Upscaling creates
larger output pixels, not additional source detail. Frames are never duplicated
to pretend that a slower native source supplies higher FPS.

Facing direction is not inferred; required `facingMode` rejects. Zoom, focus,
exposure, white balance, torch, PTZ and ImageCapture controls are unsupported.
Profile validation rejects unknown control/capability bags. The captured physical
sample has some such controls, but their responses have not been measured and
they are not claimed by these generic presets.

## Pixels, timing and transport

```text
native input + timestamp
  -> per-track output recipe
  -> immutable output frame + timestamp + sequence
  -> video / canvas / image bitmap / WebRTC encoder
```

Each track retains at most one processed output frame. Processing is lazy and
repeated reads of an admitted frame reuse identical pixels and metadata.
Unchanged output reuses the immutable native frame. CPU crop/scale runs when
demanded; WebRTC processes its snapshot outside the Page JS owner.
Stopping a track closes its cache, including against late sender work.

Optional `processing.noise` (0–8 RGB levels, default 0) modifies actual pixels.
It is deterministic per source frame and consistent across readbacks and encoding;
it does not change with read count. It is simulated noise, not a physical sensor.
No artificial motion, exposure/white-balance simulation or timing jitter is added.

Timing retains native timestamps and requested rate reduction. Busy consumers
coalesce frames rather than building a backlog. WebRTC uses the track output,
its requested FPS and admitted timestamps, including capture gaps. The existing
H264 encoder adapts dimensions to at most 1280×720, with even dimensions and
a minimum 16 pixels; this can differ from local track settings. RTP and receiver
observations describe the actual transmitted result.

RTP packet/byte counters are measured at transport boundaries. Video statistics
measure encoded/sent and decoded frames, actual dimensions, and FPS over a bounded
recent window; measured fields merge into the existing RTP report for each
direction/SSRC. Decode failures are counted; loss of incomplete frames before
decoding is not fully accounted by `framesDropped`. This is not full Chrome
statistics coverage or configurable network jitter/loss.

## Permissions, ownership and evidence

Configuration does not grant permissions. BrowserContext/origin permissions
remain authoritative, and pre-grant anonymous-device redaction still applies.
Unset/denied camera or microphone permission rejects immediately in automation.
Logical IDs are stable within their Context/origin and shared group IDs can link
a camera to its configured microphone. Unrelated origins and Contexts remain
isolated. Permission revocation ends affected clones and releases capture.

Catalog replacement rejects atomically while any Context capture is starting,
live or finishing closure. After workers close, it can be replaced. Stopped
tracks retain historical identity; video/audio stopped settings follow their
existing Chrome observations. Navigation retains the catalog and closes that
document's capture, transport, codecs and caches.

Creation, validation and discovery open no capture, filters, timers, codecs or
transport. Profiles add no native library or mandatory runtime dependency.
Existing Windows/Linux/macOS adapters and bundled codecs remain authoritative
for platform availability; macOS hardware capture was not verified on this host.

The [physical sample report](compatibility/media-device-profile-webcamera-20261007.md)
retains Chrome 152 provenance, grouping, successful/failing constraints, coherent
canvas reads, timing and sender adaptation. It also records source-specific clone
transition behavior; these class recipes do not certify that driver's lifecycle.
The sample webcam is evidence, not a production requirement.
The [native verification report](compatibility/media-device-profiles-native-20261007.md)
records continuous capture from both OBS and the physical camera.

Focused tests cover all three JS engines: private source failures, seeded recipes,
Context/origin/page isolation, redaction, clone constraints and pixels, atomic
validation, frame reuse/rate reduction, late teardown, and live WebRTC output
changes with actual RTP/frame statistics.
