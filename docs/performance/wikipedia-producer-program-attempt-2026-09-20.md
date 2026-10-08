# Producer-program experiment: incomplete replacement

## Scope and status

This attempt changes observation-local product storage to shared node records,
compiles cascade ranking inputs, indexes immutable declaration arrays, enables
observation-local css-select subexpression caches, and publishes document style
projections as columns rather than per-node objects/Maps.

It preserves canonical DOM ownership, existing geometry algorithms, retained
geometry dirty generations, and recursive provisional-box publication. It does
not replace the recursive sizing/flow evaluator or implement a new selective
invalidation scheme. Consequently, touching all producer categories is **not**
equivalent to eliminating their measured work. This is not the full production
replacement and does not establish a no-go for Part B.

The implementation is unaccepted and uncommitted. Do not ship it on these results.

## Fixed-binary unchanged Wikipedia gate

Five alternating fresh-process cold/warm pairs, no workload edits:

| Mode | Control samples (ms) | Candidate samples (ms) | Median |
| --- | --- | --- | --- |
| cold | 10074, 10098, 9574, 9677, 9687 | 10025, 9691, 10233, 9974, 9792 | 9687 -> 9974 (+3.0%) |
| warm | 6137, 6574, 6227, 6159, 6090 | 6327, 6266, 6386, 6195, 6215 | 6159 -> 6266 (+1.7%) |

Warm stage medians:

| Stage | Control ms | Candidate ms |
| --- | ---: | ---: |
| JavaScript heading visible | 1330.06 | 1361.37 |
| ECMAScript scroll | 418.91 | 415.31 |
| ECMAScript innerText | 185.26 | 181.62 |
| ECMAScript href | 175.11 | 196.06 |
| ECMAScript click/navigation | 887.11 | 907.72 |
| ECMAScript heading visible | 803.50 | 822.28 |

There is no large saving to track into downstream consumers. These numbers do
not support continuing adjacent optimizations on this representation change.

Receipt: `.build/producer-program-paired-results.json`.
Control SHA256: `173f8229753e635b0784ff5863c827eaa02fa71a538ea6fa4743e06d308f227e`.
Candidate SHA256: `0efcd244d5a5e09909b532ddc47a1d82a08a49a30a77b3644f9aa0faaba0bc79`.

## Separate diagnostic accounting

One cold/warm profile per fixed binary; these are not acceptance timings.
Events are deduplicated by sequence and nested category times are exclusive.

| Category | Cold control/candidate ms | Warm control/candidate ms |
| --- | ---: | ---: |
| matching/cascade | 2818 / 2973 | 1823 / 2001 |
| size/flow | 1633 / 1635 | 813 / 946 |
| style context | 530 / 543 | 171 / 217 |
| intrinsic | 466 / 483 | 275 / 301 |
| computed | 415 / 398 | 243 / 244 |
| placement | 105 / 128 | 55 / 66 |
| Taffy bridge | 351 / 353 | 132 / 161 |
| Entire owner observation, including unattributed | 7165 / 7396 | 3997 / 4456 |

Cold counts are identical in every named category: 58,278 cascades, 58,253 style
contexts, 112,432 computed resolutions, 33,694 intrinsic builds, 36,904 size/flow
builds, 17,020 placements, 150 Taffy calls. This representation change did not
remove production. Warm candidate built more products (25,016 vs 21,495 cascades;
15,591 vs 13,506 size/flow builds); one instrumented pair does not establish the
cause of this lifecycle difference. No claim of measured displacement is made.

Part B skipped production of complete internal products. This attempt still
executes that production and adds record/column and compiler-cache lookups.
The missing upper bound was not recovered: it remains in matching and flow.
It would be incorrect to call this evidence that a full producer replacement
cannot realize the Part B budget.

Artifacts: `.build/producer-program-profile-{control,candidate}-{cold,warm}.json`
and corresponding logs. Runner: `tools/performance/pocs/producer-program-profile.cjs`.

## Correctness and incomplete gates

`go test ./internal/webapi` passed. Focused browser CSS, geometry, selectors,
computed style, isolated-owner/batching, IntersectionObserver, font and input
tests passed (130 seconds). New relational selector/CSSOM mutation regression
`TestProducerProgramsInvalidateRelationalMatches` passed.

Full race, controlled observation chain, memory/teardown and first-build/rebuild
accounting were not completed. No correctness or shipping acceptance is claimed.

## Concurrent source modification

The worktree already contained an in-progress product-consumption audit when the
control was built. During this attempt a separate writer removed that audit's
instrumentation from `surface.js`, `css_computed_values.js`,
`css_box_geometry.js` and `document_compatibility.go`, and added the audit report.
That report itself records independent concurrent edits during cleanup.

The fixed binary receipts remain valid descriptions of the tested binaries;
the mutable worktree is not their exact source snapshot, and the control is not
a clean post-audit production baseline. Further changes and any
rollback must first reconcile these overlapping edits. Original incoming edits
must not be restored wholesale over the other writer's cleanup. Work stopped
without attempting that destructive rollback.
