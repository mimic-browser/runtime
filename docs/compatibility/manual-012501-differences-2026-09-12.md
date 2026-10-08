# Offline execution comparison: input corpus and differences

This reference describes a captured baseline, not current runtime compatibility.
It compares computed results, geometry, serialization, observable internal
calls and incomplete operations beyond API presence. Subsequent corrections
are in [corrected observations](manual-012501-cleanup-2026-09-12.md).

## Corpus and provenance

The input is `compatibility/private-captures/manual-20260912-012501`, containing
39 saved responses. The original capture has no verifiable build identity and
cannot be assigned to a revision by inference.

Measured Mimic: clean revision `1e4ce25ff5c37294b94e4222792e195ff65c82ea`,
V8, Windows amd64, Go 1.26.4. Executable hashes and build information accompany
the results. Reference: headful frozen Chrome **152.0.7977.82**, Chromium
`d04cdb24d67b081f6cf80200ffc5233f44b61109`, V8 `15.2.124.21`, executable
SHA256 `ea36dd818a90176f1a70616f0363d9be527229389a6c073a0b1688b9e73f67e9`.

Private evidence under `.build/analysis-012501/` includes `receipt.json`,
`all-payload-diffs.json`, `stage-summary.json`, `probe.js` and
`focused/chrome-{a,b}.json` / `focused/mimic-before.json`. Older Chrome
captures contain different programs and are not controls for this corpus.

No requests reached the live service. Mimic used replacement transport; Chrome
used saved responses with an external-traffic-rejecting proxy and DNS/UDP
restrictions. ICE values from Mimic's Environment do not demonstrate STUN
traffic. Intercepted replay is distinct from the normal-browser controls in
[the oracle policy](../oracle-policy.md).

## Execution coverage

The source contains a 4522-byte VM candidate. The child interpreter resolves
4816 references across 1926 strings, retained as `child-readable.js`.
Continuation decoding uses the captured program's algorithm; original
responses remain unchanged.

| Zero-based response indices | Observed role | Decoded bytecode bytes |
| --- | --- | ---: |
| 3 / 20 / 35 | Parent continuation, cycles 1 / 2 / 3 | 63730 / 63728 / 63730 |
| 8 / 23 / 38 | Main child programs | 475733 / 462774 / 462727 |
| 13 / 28 | Child continuation after submission | 2879 / 2836 |
| 14 / 29 | Parent continuation | 1821 / 1821 |

This is observed-path decoding, not complete linear disassembly. Operands,
branches and rolling keys depend on execution; unvisited paths are unmeasured.
Six instrumented serializers produced ten complete pairs in each of two Chrome
and two Mimic controls. Every output exactly matched the following POST body;
there were no truncated snapshots or parse errors.

The main child payloads contain 39/38 result blocks; 23/39 and 22/38 differ even
without common start/end/duration fields. Counts mix environment, performance and
semantics, not independent defects. Block numbering changes between programs.

Both browsers reach two complete exchanges and stop at a missing captured GET
after fixture 38 in the third cycle. This is not completion of the third
program. Mimic makes 29 transport calls; favicon fixture 6 is unused due to
cache. Critical-CH, cache and synthetic resources prevent fixture counts from
representing VM steps.

## Independently reproduced differences

Two focused Chrome controls match. Eight independent checks yield 21 structural
differences against Mimic; expected exceptions are captured within each check.

| Mechanism | Chrome → measured Mimic | Payload relationship |
| --- | --- | --- |
| HTML quotes | Attribute `&quot;` → `&#34;` | OjmeV1[86]/[104], blocks 38/7. Equivalent DOM values do not imply equal serialization. |
| Selector reentry | Collection reads do not call overridden querySelectorAll → four option calls plus img/form/a[href],area[href] | Extra maNnU6 entries despite equal return values. |
| Coordinates | left=13, top=17, translate(7,9), size 80×20 gives (20,26,80,20) → (0,0,80,20) | A shared coordinate defect, not an explanation of every complex HPcn5 rectangle. |
| Nonce concealment | Measured insertion retains .nonce but omits the content attribute/outerHTML nonce → content value remains | Extra VHsEp9 script attributes, blocks 37/35. |
| Media capabilities | Three selected audio MIME checks return probably and two MSE checks true → undefined/null CDP array entries | Also 36 semantic-missing MediaSource.isTypeSupported observations. |
| RTP capabilities | Codec/header-extension structures → undefined and empty captured arrays | xkNI3 has 8 audio/23 video entries in Chrome; sets remain profile/operation dependent. |
| OPFS | Directory handle and worker sync-access write/flush/close → NotSupportedError, backend unavailable | DdIVt1, blocks 32/24; worker source retained. |

At this revision, form controls and document collections in
`internal/webapi/form_controls.js` and
`internal/webapi/document_compatibility.js` use public selectors, exposing
internal work to author overrides.

## Stable observations without complete causal localization

| Field / blocks | Chrome → measured Mimic | Boundary |
| --- | --- | --- |
| IGBuA2 / 10,33 | [159,163] → [16,17,21,22,84,159,163] | Not every generating operation is localized. |
| oHIQ6 / 10,33 | Empty → 69 indices | Not 69 independent defects. |
| SbVZ3 / 14,37 | 90 → 62 elements | Membership/order differ; not all sources established. |
| OjmeV1 / 38,7 | HTML, selected false values and strings → different HTML, true values and strings | HTML localized; other entries remain unresolved. |
| HPcn5 / 24,20 | Ten varied rectangles → many zero coordinates/heights and width 800 | Stable array; complex geometry incomplete. |
| Graphics / 11,34 | NVIDIA profile → Intel profile, with different readbacks/limits/extensions | Profile identity does not explain every shader/path/text difference. |
| RKUE0/JRzmw6/kRQwh3/oSIr8 / 15,8 | Four stable hashes → four different stable hashes | Inputs unlocalized in this baseline; later measurements are documented separately. |
| BMnw0/knVv1 / 39,26 | true,7 → false,0 | xWWV8 contains data versus null; Chrome data varies across controls. |
| rPXg2 | Two resource/timing entries in one initialization → three, including an extra document entry | Membership needs separate evidence; replay does not reproduce wire timing. |

Permissions, Notification, network information, storage estimates, timing, ICE
and other aggregates remain in the full diff. Raw counts 1042/1024 are inflated
by array shifts, bucket keys, timing and repetition; they are not defect totals.

Each main payload also contains 1666 named observations in `gsLi5`. Ten differ:
baseURI/referrer include a replay-replaced identifier; lastModified differs with
time; port-zero diagnostic Chrome exposes webdriver; four visibility projections
and outerWidth/outerHeight reflect window state versus the Mimic Environment.
URI values match after normalizing only the replaced segment. The remaining
eight do not alone establish new API defects, and this table excludes other
computed observations. The fixed-port follow-up uses a separate control.

## Failure and interpretation boundaries

Both browsers emit `NaCW0 = "600010"` after complete cycles. Saved server
responses do not reevaluate the payload and cannot identify a live rejection's
cause. Instrumented Chrome controls had no harness errors. A separate Debugger
control served source extraction, not transparent behavioral measurement.

Uninstrumented Chrome reached the same corpus boundary but recorded
`Fetch.failRequest: Session with given id not found` on teardown. The focused
HTTP fixture also recorded a teardown connection reset despite complete
results. Five Mimic imageDecode diagnostics remain without an established
relationship to server rejection.

Production changes, full regression testing and performance measurements were
not part of this baseline comparison. Graphics parity requires operation-level
controls, not substituted hashes or GPU names.
