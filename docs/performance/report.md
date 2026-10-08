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
