# Performance status

Mimic's performance claims describe specific measured workloads. They do not
establish a universal speedup over Chrome or complete website compatibility.

## Published measurements

The [public benchmark checkpoint](../../benchmark/runs/13-rss-20260929/public-summary.md)
uses an unchanged frozen harness. All 12 correctness gates and 360 measured
single-Page attempts passed. The completed static 50-Page series measured:

| Metric | Mimic | Chrome |
| --- | ---: | ---: |
| Active process-tree RSS | 748.11 MiB | 4102.04 MiB |
| Throughput | 108.67 sessions/s | 18.09 sessions/s |
| Ready process-tree RSS | 45.79 MiB | 379.54 MiB |

The 100-Page schedule stopped at a conservative available-memory guard, not an
out-of-memory event. Its stopped rows are not successful measurements. Later
CPU and React density results are absent. The linked checkpoint retains the
methodology, source and binary identity, sample scope and limitations.

The [Wikipedia E2E reference](wikipedia-e2e-current.md) records the complete
unchanged Playwright workflow and its acceptance criteria. A faster isolated
CDP call, DOM operation or synthetic click is not an E2E result.

## Bootstrap optimization checkpoint

Bootstrap preparation now uses generated immutable metadata from the frozen
captures, bounded source and compiled-code caches, and direct admission of a
valid disk snapshot. The adapter used to validate a loaded snapshot becomes its
first consumer; later Pages still receive independent adapters. Snapshot seeding
reuses accepted platform code without changing classic-script lexical scope.
Workers reuse immutable surface composition and keep independent realm state.

Registered CSS property discovery is validated once per observation epoch and
invalidates on stylesheet and document changes, including snapshot restoration.
Custom-property inheritance walks ancestors iteratively. The pinned CSS value
lexer initializes lazily and includes only its required type tables. Its
validation results matched the previous bundle in 10,350 comparisons.

The current fresh binary passed 104 single-Page workload executions. Historical
first-Page navigation medians improved from approximately 194 to 50 ms with a
disk snapshot and from 630 to 526 ms with an empty cache (seven current samples
per mode). These historical comparisons are not matched host controls.
Fourteen alternating control/candidate pairs measured async navigation through
result at 125.18 versus 87.48 ms, with execution at 90.18 versus 53.51 ms.
DOM and CPU execution were effectively unchanged in those paired controls.
The paired candidate precedes the final narrow CSS discovery corrections.

Focused runtime, browser, metadata and platform tests passed, including snapshot
ownership, Page isolation, Worker crypto, style invalidation and teardown.
The iterative fast gate passed its six correctness workloads and density checks.
The final unchanged Wikipedia workflow passed all three cold and three warm
runs: cold median 26.761 s (26.331–29.400 s), warm median 14.396 s
(12.161–15.082 s). This remains slower than the earlier control's 20.016 s cold
and 8.357 s warm; an E2E improvement over that control is not established.

Memory remains a substantial constraint. At the checkpoint before the final
narrow CSS corrections, 50 warm static Pages retained approximately 793 MiB
process-tree RSS. Forced-GC attribution measured approximately 9.57 MiB of live
V8 heap per static Page; this is a diagnostic measurement, not ordinary RSS.
The changes do not establish a memory reduction. Concurrent host CPU load also
limits interpretation of unpaired absolute latency measurements.

## Resource discovery and relational styles

Background-image observations now travel in the existing packed native style
projection. Resource discovery checks ancestor visibility only for elements
whose computed background contains a URL, and shares ancestor results within
one validated style-read scope. It still observes the canonical computed style
and invalidates with the existing observation epoch.

The pinned native selector parser now enables `:has()`. Admitted relational
selectors rematch the subtree after a changed canonical epoch while retaining
nodes, parsed stylesheets and fonts. Clean observations retain their fast path.
Predicates not represented by the native state owner, including `:required`
and `:dir()`, retain the semantic fallback. This is a conservative admission
boundary; incremental reverse-dependency invalidation remains future work.

Two alternating fresh-process control/candidate pairs, each prepared by the
same validated static workload, passed the complete unchanged Wikipedia
workflow. Control durations were 39.307 and 40.446 s; candidate durations were
34.372 and 34.741 s. The medians differ by 5.320 s, or 13.3%. These matched
observations establish an improvement over the bootstrap checkpoint under
those host conditions. They do not establish parity with another browser.

The final binary includes a subsequent narrow correctness repair: form-state
and reflected attribute reads use captured DOM operations rather than author
overrides. Focused form, relational-selector, stylesheet, background-resource,
frame, isolated-world and snapshot checks passed. This binary also passed one
complete cold Wikipedia run at 20.213 s and its subsequent warm run at 11.439 s.
Those final absolute timings are unpaired and are not a further speedup claim.

The final iterative gate passed all six semantic workloads, four static
50-Page waves including the excluded warmup, and static/React 10-Page memory
checks. Five measured warm samples gave navigation-through-result medians of
32.284 ms for static, 158.849 ms for DOM and 78.447 ms for React. Three measured
50-Page waves gave median throughput of 192.28 Pages/s through the last result,
excluding teardown, and approximately 809.89 MiB active process-tree RSS.
Throughput including teardown was 150.84 sessions/s. These changes do not
establish a memory reduction.

An initial gate passed its 10- and 25-Page waves but failed its last 50-Page
wave with process-wide CDP disconnection. That wave is invalid and excluded
from successful results. The exact cause was not established; the subsequent
complete gate with preserved process logs passed. The unresolved failure
limits confidence in sustained density despite the successful checkpoint.

## Canonical reads and deferred font resolution

The style fallback previously resolved font sizes while constructing box state
for every visibility candidate. A computed-style document batch therefore paid
for font inheritance across thousands of nodes even when its consumers needed
only display or visibility. Font resolution is now deferred until a geometry
consumer needs it, with the existing recursive resolution boundary preserved.

Isolated-world pseudo content uses bounded projections from the canonical style
owner. Sparse candidate matching avoids resolving unrelated pseudo declarations;
shadow documents and oversized projections retain scalar owner reads. Exact
observation epochs reuse selector matches, and canonical availability reads no
longer discard an otherwise valid isolated style observation.

V8 realms receive a private native atomic DOM revision word after bootstrap
capture/restoration. Every canonical arena write publishes before unlocking;
live collections and bounded demand-loaded DOM readbacks validate that word
without a Go callback. Publishers unsubscribe before realm teardown. Engines
without the private word continue to read the canonical host directly.

Two alternating fresh-process pairs passed all eight unchanged Wikipedia
cold/warm executions. Control cold runs were 25.136 and 22.447 s; candidate
cold runs were 19.210 and 17.783 s. Warm control runs were 14.672 and 13.180 s;
candidate runs were 9.076 and 7.706 s. Cold medians improved from 23.792 to
18.497 s (22.3% less time, 1.29x speed); warm medians improved from 13.926 to
8.391 s (39.7% less time, 1.66x speed). This small paired series establishes
a complete-workflow improvement, not a universal speedup or competitor parity.

Focused checks passed for live collections, mutation/adoption, cross-world
readbacks, UTF-16 character data, shared-word lifetime, snapshot restoration,
teardown, pseudo content, recursive geometry, font invalidation, form state and
relational selectors. The fresh iterative gate passed all six semantic
workloads and all four 50-Page waves, including the excluded warmup.

Seven rotating control/candidate samples per workload found DOM execution at
170.559 versus 160.359 ms and React execution at 61.610 versus 49.143 ms;
static execution remained effectively unchanged at 4.322 versus 4.378 ms.
Three alternating measured 50-Page waves per build gave 163.06 versus
165.06 Pages/s through the last result, with active RSS at 821.71 versus
825.51 MiB. Recovery RSS was 145.38 versus 152.20 MiB. Scaling was effectively
unchanged; a memory reduction is not established.

The unpaired final gate measured warm navigation-through-result medians of
39.926 ms for static, 181.183 ms for DOM and 100.317 ms for React. Its
50-Page throughput through the last result was 127.13 Pages/s. Those absolute
numbers are slower than the previous unpaired gate; the contemporaneous paired
controls above are necessary to distinguish a code regression from differing
measurement conditions. Do not pool the two series or claim an overall win
against another runtime from them.

The unchanged live Wikipedia text-traversal probe, run only on Mimic, gave
control/candidate first-content medians of 1.834/1.822 s with three fresh
processes per build. This narrower workflow did not improve materially. Network
conditions, polling and extraction costs differ from the historical live-site
campaign; its older browser timings are not contemporaneous controls.

## Style projection allocation

Native style batches reuse document-owned readback scratch, retaining at most
64 KiB and releasing it at owner teardown. Larger individual responses remain
temporary. Public CSSOM results still own their strings. Packed observations
intern borrowed UTF-8 values directly, avoiding a string array and duplicate
strings for every element. Canonical attribute observations use their existing
epoch-owned row; a second attribute map and an eagerly enumerated name array
are no longer retained for each observed node.

Three 100-iteration allocation samples measured style-batch readback at
5256 versus 1200 bytes per call. A packed snapshot of 1000 repeated styled
elements decreased from approximately 5.87 to 1.35 MB allocated per snapshot
(77%), with median construction time decreasing from 4.406 to 3.489 ms.
These are local producer measurements, not process-memory measurements.

Three alternating fresh-process pairs with separate empty bootstrap caches
passed all 12 complete unchanged Wikipedia executions:

| Phase | Control median (range) | Candidate median (range) |
| --- | ---: | ---: |
| Cold | 16.741 s (16.660–16.988) | 16.758 s (16.613–16.854) |
| Warm | 7.972 s (7.836–8.018) | 7.820 s (7.673–7.920) |

The measurements show no material complete-workflow latency regression. They
do not establish a substantial E2E speedup. Identical 50-ms process-tree
sampling found cold peak RSS medians of 467.64 versus 470.33 MiB and warm peak
RSS of 395.80 versus 405.16 MiB. A process RSS reduction is not established.
The control executable SHA-256 is
`29562f2ec2bfa19199d284b9ec690c5ba7d96adff15c4e3557641cd97e53bc15`;
the candidate is
`ae8406fd4058e6fb0a4ae82af46d587937e161147a0a855c131aec1b0bbd1bd9`.

One separate diagnostic pair collected heap statistics immediately before
leaving the first JavaScript article, after explicit V8 and Go collection.
Go cumulative allocation decreased from 1778.84 to 1547.30 MiB, and V8
cumulative allocation from 4914.66 to 4838.17 MiB. Live V8 heap decreased only
from 119.62 to 118.77 MiB, and live Go heap from 64.90 to 64.61 MiB. These
diagnostics establish lower allocation pressure and a modest live-heap saving;
forced collection is not a production policy or a latency measurement.

Three alternating measured static 50-Page waves per binary, after excluding
one warmup wave each, passed all 300 measured sessions. Throughput through the
last result was 185.73 versus 186.99 Pages/s. Active RSS was 829.50 versus
835.76 MiB, with recovery RSS at 149.14 versus 151.73 MiB. Scaling remains
effectively unchanged; these results also do not establish lower process RSS.
Focused checks passed for hidden and large style values, output ownership,
scratch teardown, canonical attribute invalidation and isolated-world reads.
The fresh fast gate passed all six semantic workloads, its four 50-Page waves
including warmup, and its static/React 10-Page memory checks.

## Architectural constraints

Independent Pages retain independent execution owners. Immutable platform data
can be shared; mutable browser state, realm identity and lifecycle cannot be
merged to obtain a favorable memory number. DOM, styles, geometry, navigation
and CDP observe canonical state. Derived observations must invalidate on all
relevant mutations and release retained resources at teardown.

Performance evaluation includes latency, throughput, live memory, allocation
pressure, concurrent execution and memory retained after teardown. Explicit GC,
instrumented profiles and destructive experiments are attribution tools; their
numbers do not replace ordinary workload measurements.

## Remaining limits

- Cold observation construction and mutation-driven style/geometry rebuilding
  can dominate complex automation workloads.
- Allocation and collection pressure remain relevant to Page density. Smaller
  heaps must be evaluated together with latency and throughput.
- Resource optimization profiles are empirical and workload-specific. Unknown
  request inputs use ordinary acquisition; network savings do not establish a
  CPU or latency improvement.
- Synthetic gates and live network scenarios have different sources of
  variance. Claims require matched binaries, workload, host and sample scope.

## Contributor validation

Use `tools/performance/fast_gate.py` for appropriate focused iteration. Reserve
the unchanged full benchmark matrix for substantial optimization batches and
reportable checkpoints. Profile a measured bottleneck before changing its
architecture. Preserve focused correctness regressions and compare fresh
control/candidate builds against the same observations.

This document contains current technical conclusions and supported measurements.
Private investigations and intermediate activity logs are not public project
documentation.
