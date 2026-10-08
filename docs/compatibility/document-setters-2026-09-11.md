# Document setter receiver and conversion semantics

Baseline 26f3e12, following [Document descriptors](document-descriptors-2026-09-11.md).
This package changes existing bindings and three frozen LegacyLenientSetter descriptors.

| ID | Root cause | Repro | Chrome | Mimic before | Priority | Status | Fix | Limits |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| DOC-SET-BRAND | Setters omitted a common private receiver check | document_setter_oracle.js | Reject invalid receivers before value access; three LegacyLenientThis handlers ignore them | Invalid receivers accepted or value accessed first | P1 | Corrected | Validate private Document brand before original setter | Valid foreign setter owner dispatch is not newly claimed |
| DOC-LENIENT-SET | Readonly legacy attributes lacked setters | Same oracle | Branded no-op setter, no value conversion | Setter absent | P1 | Corrected | Install fullscreen/fullscreenElement/fullscreenEnabled LegacyLenientSetter bindings | Does not implement fullscreen capability |
| DOC-SET-STRING | Existing string setters accepted Symbol or threw a later operation exception | Same oracle | TypeError during conversion | Accepted Symbol or xmlVersion NotSupportedError | P1 | Corrected | Shared string conversion with nullable/legacy null handling | Complete domain/USVString semantics not claimed |
| DOC-BODY-SET | Missing body setter | Complete discovery probe | HTMLElement setter exists | Setter absent | P2 | Incomplete implementation, open | None | Requires real tree mutation semantics, not a stub |

The focused frozen oracle improves **100 differing normalized leaves to zero**,
with zero Chrome-to-Chrome control differences. It covers eight invalid receivers,
conversion ordering, valid handlers, private branding after prototype replacement,
legacy readonly setters and Symbol conversion for eleven string attributes.
The broad discovery revisions have 536, 542 and 553 baseline leaves respectively;
they are overlapping revisions, not additive bug counts. The final complete probe
improves 553 to one (missing body setter), with zero control differences.
[Compact evidence](document-setters-20260911/) records probe/build hashes, Chrome
mode/profile metadata, observations and fixed corpus transitions.

The original fixed set remains 205/236 matches and 19/28 repaired representatives,
with no regressions, new differing leaves or oracle drift. Brand/general/relations
remain 578/40/2 differing records: no added, removed or changed records. These sets
overlap. No new discovery-group count is claimed; the last nine observational
groups remain a historical measurement, not nine proven root causes.

Only focused Chrome controls were repeated. Full unchanged Chrome controls,
fuzzer discovery, sweep and performance gates were not run. No performance improvement or complete
ownership/lifetime reclamation is claimed. All probes use local fixtures and
frozen Chrome 152.0.7977.82; no target site ran.

P0 remains tracked separately in [snapshot serialization](snapshot-serialization-2026-09-11.md).
The historical SizeFromMap signature is still unreduced; passing semantic tests
and gates do not close it.

Full browser suite PASS (475.388 s output / 482.658 s completion); V8, Goja,
CDP and WebAPI package results pass. Focused regression PASS (3.017 s).
Targeted browser race PASS (6.664 s); filtered WebAPI race has no matching tests.
No test failures were recorded; existing opt-in skips are preserved in the receipt.
Full browser race was not run.
