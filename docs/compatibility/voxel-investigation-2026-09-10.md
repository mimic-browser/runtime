# Captured workload exceptions and SVG geometry

This reference separates locally reproduced browser defects from remote-server
outcomes. A retained diagnostic capture returned HTTP 403 for its main document.
Successful live acceptance was **not established**; eliminating JavaScript
exceptions is insufficient evidence.

Measurements belong to the retained captures and binaries, not every later
revision. Current boundaries are in [SVG geometry](svg-geometry.md),
[offline audio](offline-audio.md) and [architecture](../architecture.md).

## SVG availability and error attribution

In the retained `voxel-20260910-payload-fixed2/trace.json`, events 2875/2876
record unsupported Element<g>.getBBox immediately before
`Cannot read properties of undefined (reading 'call')`.

The corresponding VM frame, line 4, column 125708, reads a method then calls its
.call property. Receiver/property values were not retained. Another trace also
records two unsupported getBBox reads. Order alone is not a complete causal
observation.

An independent reproduction creates SVG, a group and a rectangle at x=10,
y=20, width=30, height=40:

```javascript
const fn = g.getBBox;
const box = fn.call(g);
```

| Observation | Chrome 152.0.7977.82 | Measured Mimic V8/Goja baseline |
| --- | --- | --- |
| typeof g.getBBox | function | undefined |
| Group brand | [object SVGGElement] | [object SVGElement] |
| Result | {x:10,y:20,width:30,height:40} | TypeError |
| V8 exception | None | Cannot read properties of undefined (reading 'call') |

The missing method and incorrect interface family are confirmed locally and
reproduce the exact exception. Missing VM locals prevent definitive attribution
of the captured report. Neither finding establishes the cause of HTTP 403.

The retained reproduction includes `svg_bbox_repro.js`,
`svg-bbox-chrome152.json`, `svg-bbox-mimic.txt` and
`local_svg_diagnostic_test.go.txt`. The printing test's PASS means successful
observation collection, not compatibility.

## Geometry evidence and text boundary

The measured SVGGraphicsElement/SVGGElement implementation was checked against
55 native reference groups on V8/Goja: primitives, paths, group unions, nested
transforms/viewBox, detached/display states and cross-realm receivers.
Geometry observations require no graphics backend.

A subsequent 5965-event capture no longer contains the two unsupported group
getBBox observations. Supported calls occur at 2491/4979; both reach the explicit
SVG.getBBox.text boundary at 2504/4992. No new unsupported/semantic-missing names
appear in the comparison. This establishes method availability, not text-bound
compatibility.

Text and use remain explicit limits at that captured revision. Text bounds need
font, baseline, text-anchor, x/y/dx/dy and tspan references; approximate Canvas
metrics alone do not establish SVG parity.

The capture still has three main-document 403 responses and two child-document
200 responses. Two challenge-resource DNS failures have no proven causal role.
No decoded caught-error payloads were present, so all error reports are not
proven absent. A successful live acceptance control was not retained.

## Independently reproduced mechanisms

| Observation | Established mechanism | Measured correction |
| --- | --- | --- |
| null read through then | getBattery reached a generic null-returning stub. | Stable Navigator Promise/BatteryManager identity, no-hardware state, receiver checks and empty gamepad state. |
| Expected a Node / Expected Nodes | Foreign nodes failed local instanceof checks; documents used separate ID spaces. | Page-owned node arena, private genuine-node recognition, insertion/replacement/moves/fragments/traversal and Attr identity. |
| Obfuscated not-a-function report | Captured locals showed an earlier nodeId failure in borrowed getComputedStyle for a foreign node. | Node identity crosses the frame bridge; styles come from the owner document. |
| Unsupported createOscillator | Signal generation unavailable. | OscillatorNode, PeriodicWave, DynamicsCompressor, parameter/lifetime behavior and CPU DSP. |
| Battery event order | Four property handlers differed from addEventListener ordering. | Shared DOM assignment/replacement/removal/reattachment semantics. |

Related frozen references cover Window/Worker base64, dirName/maxLength,
Document/HTMLElement handlers and WebGL color spaces. Arenas remain Page-isolated.
At the measured revision, individual unreachable nodes are not collected before
Page closure. Audio has an explicit tolerance, not bit-identical native FFT.

## Evidence and binary identity

Raw responses, instrumented sources, payloads/events/traces, binaries and
integrity manifests remain in ignored `compatibility/private-captures/`.
They can contain cookies and challenge tokens. Private target URLs and session
correlation identifiers are omitted from this public reference.

- A capture with an undersized Node pipe string limit is invalid evidence.
  Complete captures match 20/20, 6/6 and 3/3 serializer pairs to POST bodies.
  The fixed2 capture matches 3/3 pairs across four POST requests; not every POST necessarily
  uses the instrumented serializer.
- Instrumentation changes source, timing and observable globals. Snapshots skip
  accessor getters and mark traversal limits. These observations cannot alone
  explain an uninstrumented server outcome.
- The fixed2 capture predates the final battery-event correction. Its integrity manifest
  identifies that binary, not a final-build claim.
- A separate 530-CDP-event/8493-runtime-event capture reports missing battery,
  gamepad and oscillator semantics but has no executable hash/launch metadata.
  It cannot establish a corrected-build regression. No pre-pack or
  Runtime.exceptionThrown event is present; caught exceptions can still exist.
- No successful Chrome network control was captured by this diagnostic method.
  Local native API oracles are not successful-server-response references.
- A clearance cookie is present in recordings that still return 403. Its presence
  does not prove acceptance; no cookie decryption/server-key recovery was shown.

## Diagnostic tooling boundaries

The ignored `compatibility/private-captures/payload-diagnostic/` tools are
separate from the runtime. Capture needs a large enough Node pipe limit.
The analyzer uses version-specific field names and historically copies a binary
from a fixed path; its executable identity must match the capture before use.
Otherwise it can overwrite the wrong retained binary.

The exception overlay generator emits diagnostic Go source that must remain
outside ordinary package discovery. A diagnostic printing test and a frozen
compatibility expectation have different meanings.

## Recorded validation

These statuses apply to the retained source/binary evidence:

- DOM/browser suites passed; browser took 214.215 seconds.
- Event/document-reference tests passed after the battery-handler correction.
- Focused race checks for cross-realm DOM, Navigator, identity, parallel Pages
  and teardown passed in 25.378 seconds.
- Other production packages, commands, Chrome data and compatibility checks
  passed in the recorded validation.
- Six audio references passed on Goja/V8. Exact PCM tests were retained; new
  comparisons used tolerance 1e-5, with measured composite error at most 2.39e-7.
- Source-fixture hashes for 14 captures and whitespace checks passed.

No successful live-server result after the SVG change is established.
Remaining caught-exception attribution requires a minimal causal reproduction
or retained VM locals, not the last API name in a trace.
