# Performance status

Mimic's performance claims describe specific measured workloads. They do not
establish a universal speedup over Chrome or complete website compatibility.

## Published measurements

The [current public memory checkpoint](../../benchmark/runs/14-memory-20261010/public-summary.md) measured the following against frozen Chrome 152 reference observations:

| Metric | Mimic | Chrome |
| --- | ---: | ---: |
| Active process-tree RSS, 50 static Pages | 727.78 MiB | 4102.04 MiB |
| Ready process-tree RSS | 40.80 MiB | 379.54 MiB |

All 120 measured single-Page attempts passed. Static and CPU completed 100 Pages; React completed 50 Pages. React-100 failed in the final wave and is excluded from successful memory comparisons (2,810/2,910 density attempts valid). This memory-only checkpoint does not certify throughput or cold latency. The methodology retains exact binary and reference provenance.

The [Wikipedia E2E reference](wikipedia-e2e-current.md) records the complete
unchanged Playwright workflow and its acceptance criteria. A faster isolated
CDP call, DOM operation or synthetic click is not an E2E result.

## Bootstrap and font memory retention

Native platform operations retain one native gate without an intermediate
JavaScript facade. Snapshots preserve function identity and lexical state without
keeping compiled installation bytecode; artifact identity includes this policy.
Installed-font names and candidate order remain eager, while immutable nominal
coverage is decoded once per resource on demand. Shaping faces and author fonts
remain Page-owned. Generated lazy operations retain the established publication
order: semantic installers require callable descriptors before finalization.

A Windows memory-only checkpoint reused frozen local fixtures, Job Object
accounting and the 50 ms sampler. All 120 warm single-Page attempts passed.
Density measured 1/5/10/25/50/100 Pages with an excluded warmup and
`max(5, ceil(20/N))` waves per fresh process, then 250 ms recovery without forced
collection. Static and CPU completed 100 Pages; React completed 50 Pages.
React-100 failed in its final wave with a native access violation; 2,810/2,910
density attempts were valid. Failed series do not supply successful comparisons.
No cold latency matrix or live Chrome comparison was run.

| Process-tree RSS | October 10 control | Current source |
| --- | ---: | ---: |
| Ready, ten fresh processes | 49.00 MiB | 40.80 MiB |
| Static, 50 active Pages | 823.99 MiB | 727.78 MiB |
| CPU, 50 active Pages | 1324.79 MiB | 1225.69 MiB |
| React, 50 active Pages | 1094.36 MiB | 1013.87 MiB |

Measured executable SHA-256: `fd0fca18f8be20a64ce684b59d21a9b50479cf759bf1761cc148c7d17f298c00`.
Saved September Chrome observations give 9.30 times lower ready RSS and 5.64
times lower static 50-Page RSS. CPU/React density and static 100-Page historical
Chrome diagnostics are not successful reference series.

Focused callable/source, snapshot/lifecycle, lazy mutation/freeze, Navigator,
SVG, camera/WebRTC and frozen font checks passed. Concurrent font coverage
initialization passed the focused race check. Removing the intermediate facade
reduced diagnostic live V8 heap by approximately 0.51 MiB per Page; forced-GC
heap observations are distinct from ordinary RSS.

Snapshot bytecode clearing reduced paired static 50-Page RSS from 771.51 to
741.19 MiB, with approximately 7% lower batch throughput. Two alternating
Wikipedia pairs passed all four complete workflows at control/candidate medians
of 9.698/9.478 s. Three clean font-coverage pairs passed all six workflows at
8.970/9.008 s; no Wikipedia peak-RSS reduction was established (352.14/356.73
MiB). These memory changes are not a universal speed improvement. RSS can count
shared pages repeatedly, sampling can miss short peaks, and recovery/cache
retention does not prove leak absence. See the [memory checkpoint](../../benchmark/runs/14-memory-20261010/public-summary.md)
for the full result scope and limitations.

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

## Interactive runtime and inspector overhead

Post-load CSS image discovery uses a resource-only native projection rather than
serializing every computed property. Initial discovery shares the ordinary bulk
projection while parsing, stylesheets and load state are changing. Hidden
branches do not require computed-style readbacks. Unknown computed property
names return the empty string without constructing layout. Scalar serialization
resolves box state and logical axes only when its property requires them. Exact
inline syntax admission has bounded retention. Canonical DOM, style mutation and
Page ownership remain authoritative.

Immutable stylesheet inputs use a validated acknowledgement. Changes to source,
order, base URL or CSSOM declarations invalidate it. Replacing a derived native
owner replays acknowledged programs. Navigation lookahead overlaps at most 64
classic script fetches from an already received HTML response. Script execution
remains on the parser's event loop. Templates, noscript, security, credentials
and document cancellation still apply. Base, policy and foreign-content
transitions stop the conservative scan. It does not scan streaming network bytes
or speculate module dependencies.

The Page pump subtracts time already accounted for by external execution from
its wall-clock idle advance. A regression reproduces the former double count
when a paused pump resumes after a long synchronous command. Both clock samples
are taken inside the command boundary, preserving one canonical clock per Page.

Three alternating fresh-process pairs on an interactive network workload used
separate empty bootstrap caches. Control/candidate navigation medians were
6.255/4.724 s (ranges 5.838-7.017/4.532-4.950 s), form detection medians were
2.032/1.501 s (ranges 2.016-2.296/1.495-1.556 s), and the medians of per-run
character insertion medians were 114.1/13.9 ms. Form detection includes the
trusted click and polling until the form is observable; it is not a visual paint
timestamp. A separate normal frozen Chrome 152 capture measured 3.35 s
navigation, 284 ms form detection and approximately 1 ms per character. Live
network conditions differ, so these samples establish neither a universal load
speedup nor Chrome parity.

The unchanged Wikipedia workflow passed all 12 executions in these three pairs.
Control/candidate cold medians were 10.340/10.129 s (ranges
10.019-10.450/9.528-10.361 s); warm medians were 7.104/7.069 s (ranges
7.102-7.453/6.551-7.146 s). This establishes no material regression in the
complete workflow, not a substantial Wikipedia speedup. More aggressive retained
state and per-node projection experiments were rejected after full workflow
regressions. These results must not be pooled with earlier checkpoints.
The control executable SHA-256 is
`66738652e8246db13f4d53f9adf67a5b03196ca8d5fd15a35ecef2f1655c07b3`;
the candidate is
`e8a78e59ec4e508c871962015bbcb005cf2c1bbeaf346d54f286973d990433a4`.

On a 1000-row synthetic inspector fixture, median character insertion with DOM
inspection enabled fell from 1599 to 5.0 ms in the final candidate after replacing
repeated child-order searches with a linear diff. External Blink presentation
and screencast delivery have been removed from the current runtime: their
reconstruction and frame delivery costs do not meet lightweight browser
requirements. Inspection continues directly against canonical state.

Focused parser, computed-style catalog, fallback geometry, image-resource,
stylesheet replay, clock, inspector checks passed. Clock and
inspector checks passed under the Go race detector, as did parser lookahead
checks. The fresh-build fast gate passed all 24 semantic runs, four 10-Page
concurrency waves and static/React teardown checks. No memory reduction is claimed.

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

Two subsequent alternating control/candidate pairs completed all ten measured React-100 waves for each binary. The native failure was not reproduced or attributed; these focused repeats do not replace the failed full memory-series record.
