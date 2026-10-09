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

## Validated optimization checkpoint

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
