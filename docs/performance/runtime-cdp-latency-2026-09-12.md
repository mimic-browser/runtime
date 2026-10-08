# Runtime and CDP traversal costs

Connection discovery, DOM traversal and style initialization contribute different
costs to browser automation. Measure them independently before changing runtime
execution or Page locking.

## Mechanisms

- Localhost discovery can wait on an unavailable address family before connecting
  to the other. The snapshot client races localhost address families and uses the
  address that accepted the connection. HTTPS and remote endpoints retain their
  hostname. This reduces connection delay; it is not a JavaScript-engine speedup.
- A necessary leaf in a selector's final compound selects candidate node IDs in
  canonical DOM. The validated selector library still determines the complete
  match, including ordering, scope, pseudo-classes and errors. Query results do
  not survive a query.
- Live class collections cache member IDs against the canonical arena revision.
  Parser, host, cross-realm, attribute and tree writes invalidate membership.
  Existing wrapper and collection identity remains intact.
- Parent, child-at-index, element-child and selector traversal exchange canonical
  IDs. Existing wrappers are reused; missing wrappers load their record once,
  avoiding repeated serialization of attributes, text and child arrays.

These mechanisms preserve one event loop per Page, task and microtask ordering,
Page isolation and the GPU-free observation boundary. Native profiling can select
script names with `MIMIC_V8_CPU_PROFILE_FILTER`; disabled profiling adds no
per-operation environment lookup.

## Controlled measurement

`tools/performance/runtime_latency.py` serves the same 600-section local document
to paired binaries. It verifies results and mutations, measures three Pages with
one excluded warmup and five operations per Page, and samples RSS after Page
closure and 250 ms recovery without forced GC.

The retained measurements against baseline
`9c82cddc6a564de1a38c285c566fc30107f21064` show the operation-specific effect:

| Operation | Baseline median ms | Candidate median ms |
| --- | ---: | ---: |
| New Page | 2.07 | 1.63 |
| Local navigation / DOMContentLoaded | 104.04 | 91.31 |
| Structural selector | 24.98 | 0.89 |
| Iterate live class collection | 2217.38 | 0.48 |
| Parent/child traversal | 211.37 | 14.46 |
| Mutate and reread live collection | 106.59 | 0.74 |

The [retained measurement artifact](runtime-cdp-20260912/measurements.json)
identifies exact binaries and harness hashes. These historical results do not
represent a multiplier for all JavaScript or a current release performance claim.

## Limits and focused checks

CSSOM construction, style matching and geometry initialization can remain
synchronous costs after traversal is improved. Network latency and transport
failures must be separated from runtime work. Shared-host measurements showed
substantial throughput variance, and React completion regressed in one paired
gate; the evidence does not establish broad latency neutrality or stable overall
throughput improvement. See [the performance report](report.md) for current gates.

Focused semantic coverage includes:

- `TestSelectorCandidatesPreserveOrderScopeAndFallback`.
- `TestClassCollectionsAreLiveAndScoped`, `TestClassCollectionQuirksCaseMatching`
  and `TestClassCollectionRevisionIncludesHostWrites`.
- `TestNodeTraversalAndLiveChildList`.
- `tools/test_mimic_snapshot.py`, including IPv4-only and IPv6-only localhost
  listeners and redirect/lifecycle observation.

Use fresh paired binaries with the unchanged local harness, then the focused fast
gate when needed. Preserve binary and harness identity, failures, Page-concurrency
results and recovery memory together; a successful microbenchmark alone does not
qualify an end-to-end workload.
