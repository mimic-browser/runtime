# Document getter receiver checks, 2026-09-11

Baseline 7c1bbfe, following [owner dispatch](document-getter-ownership-2026-09-11.md)
and the separate [P0 serialization investigation](snapshot-serialization-2026-09-11.md).

| ID | Root cause | Repro | Chrome | Mimic before | Priority | Status | Fix | Limits |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| DOC-BRAND-GET | Getters omit a private Document receiver check | document_getter_brand_oracle.js; prior broad discovery | TypeError on invalid receiver, except three LegacyLenientThis attributes | Some return defaults or accept prototype forgeries | P1 | Corrected | Validate canonical document/private document slot before installed getter | Setters/methods remain separate |
| DOC-PLACEMENT | Semantic installers add properties to the wrong prototype | Broad discovery | No own firstChild/location/textContent on Document.prototype | Three extra descriptors | P1 | Open | Next package | Removing descriptors alone would break behavior |

A single check now guards the installed Document getters while preserving owner
reference dispatch. Prototype inheritance is not used as the brand: a genuine
inert Document keeps its brand after prototype mutation, while objects derived
from Document prototypes or from a real document, and a user Proxy around it,
are not valid receivers. The frozen Document WebIDL marks onreadystatechange,
onmouseenter and onmouseleave LegacyLenientThis; these three return undefined
for invalid receivers. No getters or other API capabilities were added.

Focused observations: 152 differing leaves before, zero after, zero control.
The unchanged broad getter probe improves 637 to 3, all remaining differences
being descriptor placement. Original brand corpus improves 609 to 583 records:
26 removed, no new records or changes in the remaining differences. These sets
overlap and are not summed; the fix is one common receiver-validation cause.
Original fixed corpus remains 205/236, representatives 19/28, with no new leaves,
regressions or Chrome drift. General corpus stays 40, relations 2. Their controls
and the brand control are zero. Fuzzer remains 31 divergent probes in 9
observational groups, 236 checks, no errors/instability, unchanged discovery
3,533 and budget 100. Sweep remains 114/130, 36 skipped, 11 candidates,
3 environment/timing reviews and 2 exception mismatches.

Navigation reflection/nested/lifecycle remain zero; native-global and saved-eval
limits remain 2/3 leaves with zero control diffs. The navigation driver emitted
one background pyppeteer Target.detachFromTarget/no-session warning after a
closed target. All recorded evaluations completed without harnessError; the
warning is not hidden or counted as a semantic mismatch.

Full browser PASS: 398.779 s output / 401.676 s completion. Full engine V8,
WebAPI and CDP pass; Goja reused its cached passing result. Focused ordinary and
restored oracle PASS (1.840 s). Targeted browser race PASS (11.762 s); filtered
WebAPI race has no matching tests. Full browser race is not claimed. Existing
opt-in skips remain in the test receipt.

Neither planned performance gate started. This receiver-only change does not
alter owner lifetimes or engine concurrency.
Prior owner-package gates remain evidence for that earlier revision, not a new
measurement here. No new performance claim is made. Already-completed controls
are retained with their original source and environment identities.

[Compact evidence](document-getter-brands-20260911/) records binary/revision/dirty
state, probe hashes, all-leaf transitions, failures/skips and validation scope.
Chrome stays 152.0.7977.82 headful with the dedicated reused profile; freshness
is not asserted. Only loopback fixtures ran. Remaining setter, Node inheritance,
Window named-access and architecture limits remain backlog; this is not the
completion of the whole compatibility cleanup.
