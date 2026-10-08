# Local CSS mutation experiment, 2026-09-14

Decision: stop this prototype. Separating stylesheet-source discovery from the
canonical DOM observation epoch removes work, but does not materially accelerate
the integrated scenario. It adds an invalidation contract without a demonstrated
speed or lifetime benefit large enough to justify promotion.

## Scope and evidence

The isolated candidate adds a conservative stylesheet-source counter alongside
the existing arena revision. Every unknown write invalidates both counters.
Only class, id, style and data attributes on ordinary HTML nodes preserve the
source counter; style/link/base/meta, foreign namespaces, structural changes,
CSSOM, resource and environment changes remain conservative. Matching,
inheritance, canonical element snapshots and geometry still use the full epoch.
No browser tracing, observation, wrapper or canonical ownership is removed.

This is four production files, 42 inserted and seven removed lines, plus a
focused canonical-invalidation test. It is not subtree geometry invalidation.
The prototype is intentionally excluded from the selected production patch.

The unchanged diagnostic builds two absolute-positioned branches, each with
40 or 200 leaves, and either one or 16 stylesheets. It includes simple,
ancestor, sibling, :has and :is selectors, inherited custom properties and
fonts, class/token/attribute/inline-style mutations, held computed declarations
and reads from both the changed and untouched branch. Each process uses one
Page; 20 style-only and 20 style-plus-geometry mutation/read operations execute
through Page.Evaluate, including its checkpoint. Initialization and first
use are separate, followed by explicit diagnostic collection and Page teardown.

Linux binaries are built from f07bd2d through Go overlays: control contains only
the additive diagnostic, candidate contains only that diagnostic and the CSS
prototype. Bootstrap/snapshot experiments in the working directory are not in
either executable. Ordinary timings have diagnostic profiling disabled and
retain the normal tracing policy. Two sequential C-A-A-C blocks per configuration
give four processes per variant, with no concurrent heavy builds or tests. Values
below are medians of process medians; CPU includes process setup and collection.

## Result

| Leaves per branch / sheets | Style read ms, control → candidate | Style + geometry ms, control → candidate | Complete 40 mutation/read operations ms, control → candidate |
| --- | ---: | ---: | ---: |
| 40 / 1 | 1.975 → 1.852 | 4.816 → 5.216 | 139.973 → 148.477 |
| 40 / 16 | 2.187 → 2.060 | 5.455 → 5.278 | 158.290 → 150.766 |
| 200 / 1 | 3.802 → 3.651 | 26.639 → 26.166 | 597.102 → 600.021 |
| 200 / 16 | 3.751 → 3.763 | 28.312 → 27.260 | 645.376 → 632.405 |

Complete process medians change by approximately +4.3%, 0.0%, +0.1% and 0.0%
speedup respectively, with the first configuration dominated by initialization
variation rather than its slower mutation/read work. Process CPU changes from
about 1.8% worse to 2.3% better. No configuration approaches 2×. Small absolute
style-read differences and differing individual trials do not establish stable
tail-latency improvements; the 40/1 geometry result also exceeds the tolerated
5% regression in the combined medians.

After explicit collection, Go heap is only about 40–55 KiB lower per Page. Private
memory is 0.05–3.55 MiB higher in these samples, without a demonstrated retained
memory benefit. The source counter itself costs eight bytes per arena, and the
retained program cache uses weak root keys; this experiment does not fix a
lifetime leak. Full memory/allocation, setup, CPU and teardown samples are in
the archived summary and raw data.

A separate instrumented eight-mutation comparison demonstrates that the intended
work was removed: stylesheet queries 8 → 0, stylesheet-owner connectivity checks
128 → 0, getAttribute calls 279 → 23. Canonical styleObservationState calls remain
602 → 602 and observationVersion calls 896 → 896. Crossing reduction alone is
therefore not an acceptance criterion.

## Compatibility and remaining work

Every candidate output equals its control output. Frozen Linux Chrome
152.0.7977.82 agrees exactly on all sampled widths, heights, positions, border
widths, margins, inherited font size/family and the geometry resulting from
custom-property changes, across all four configurations.

The strict oracle also exposes an existing control limitation: computed color
and paddingLeft return literal `var(--ink)` and `var(--pad)`, while Chrome returns
their resolved values. The complete unmodified outputs, including these
differences, are retained. This is not a full Chrome-parity result. Chrome's
separate CDP round-trip geometry medians are 0.63, 0.62, 1.55 and 1.60 ms; they
have a different transport boundary from direct Page.Evaluate and must not be
presented as a precise paired engine-execution speed ratio.

Linux DOM tests and focused CSS rule-index, ancestor-filter, checked-selector,
CSSOM/shadow/reentrant-conversion and checkpoint-geometry regressions pass.
The rejected prototype was not advanced to race/full-browser promotion gates.

The whole-workload native profile of 80 geometry mutations shows about 1.70 s
inclusive under size/rect and 0.94 s under matchingStyles/styleContext in a
2.32 s sample window. These are nested costs and must not be added. GC accounts
for about 0.21 s of self samples. Native `toString` self samples are not enough
to attribute their cost to an individual conversion. The Go profile places
most samples at the native execution boundary; `cgocall` is not independent
overhead that can be added to the V8 stacks.

The next useful CSS slice must address rebuilding the graph and dynamic-selector
validation after a local mutation. Retaining an untouched branch safely requires
canonical dependency information for relational selectors, ancestor inheritance,
intrinsic descendant sizes, sibling flow and cross-root observations. A broad
dirty-bit cache without those dependencies would produce stale observations.
This source-only result does not authorize such a rewrite.

## Reproduction and archive

Evidence is in [data/css-source-epoch-20260914](data/css-source-epoch-20260914/):
compressed complete raw samples, summary, Chrome oracle, host counters, native
and Go CPU profiles, the exact oracle input and receipts. Gzip files
are ordinary JSON after decompression. Original binary and source hashes are
recorded; the source prototype and spike runners are discarded after this decision.

To repeat the bounded experiment, evaluate `oracle-input.js`, call
`cssSpikeSetup(nodesPerBranch, sheetCount)`, then `cssSpikeRound(0)` for first
use. Execute twenty `cssSpikeRound(index, false)` calls and then twenty
`cssSpikeRound(index, true)` calls in the same Page, retaining each complete
JSON result. Measure direct Page.Evaluate wall time separately from process
startup/teardown, and capture Go/native memory before, after, and after explicit
diagnostic collection. Run control/candidate/candidate/control twice for each
40/200 × 1/16 configuration, without concurrent heavy work. The archived
prototype hashes identify the historical candidate; its implementation is not
a maintained feature or benchmark mode.

Control binary SHA-256:
`bc202efb3c3e4c054d0b893fd48481852832355772a92fce0b760dd0e805ab28`.
Candidate binary SHA-256:
`61ee684a50d4e0a674952a592f5737d06eb722883baf536aa979f9d2f1d5b47c`.
Diagnostic fixture SHA-256:
`cc77eb6e79f333464bc21dad316214626b860be88b7b09fcbdeb345fd081d42b`.
