# Reusing style and geometry observations

Repeated geometry reads can spend most of their time rebuilding the same style
and geometry graph. Input commands may also wait for an observer task doing that
work on the same Page. A long Page-lock wait alone does not demonstrate shared
locking between independent Pages; attribute handler time, queued Page work,
native execution and allocation separately.

## Observation lifetime and invalidation

Eligible top-main-realm observations reuse the canonical style and geometry graph
across microtask checkpoints while its observation epoch remains unchanged.
DOM writes, CSSOM edits, viewport and media changes, element state, scrolling,
resources and shadow membership participate in invalidation. An epoch change
replaces the graph; a failed observation discards provisional results. Restore
and realm teardown release retained observations.

Child, isolated and foreign realms retain their conservative observation boundary.
Unchanged shadow trees can use the same IntersectionObserver no-change check;
shadow attachment and membership changes still invalidate the canonical epoch.
The cache retains a derived graph, not an unbounded history of document revisions.
New geometry inputs must participate in invalidation before reuse is enabled.

## Measurement method

Use paired fresh binaries with the same controlled local workload. Verify the
hit target and post-click state before counting a
latency sample. Separate warmup, unchanged reads, mutation/read cycles and input
commands. Profiling runs establish attribution; they do not replace unprofiled
latency measurements. Go profiles should be paired with native sampling when
most work occurs inside V8.

The retained controlled experiment measured three trials, each with one excluded
warmup and 30 measured rounds. These historical values describe this mechanism,
not a current release performance guarantee:

| Operation | Baseline median ms | Candidate median ms |
| --- | ---: | ---: |
| Local real click | 109.97 | 27.29 |
| Local geometry read | 29.79 | 2.00 |
| Local geometry mutation/read | 142.89 | 112.72 |

The aggregation is the median of the three trial medians. The baseline binary
SHA-256 is `8cfedb434de18a2db6466053060402aad90e75d1477047db53796bca6b255503`;
the measured candidate is
`c5527cdb4aeb73ad931fffb8085cff96a27284c87974a28b915d6754e2c203fa`.
That candidate predates the defensive discard-on-exception clause; these rows
cover successful observations only.

## Regression boundaries

`TestGeometryEpochSurvivesCheckpointAndInvalidatesMutations` checks unchanged
checkpoint reuse with deterministic host counters and verifies class, CSSOM and
visibility invalidation. `TestStyleObservationEpochCoversCSSOMShadowAndReentrantConversion`
covers shadow and reentrant conversion boundaries. Existing input, viewport,
font and animation regressions remain necessary when changing observation inputs.

Cold geometry and mutation-heavy workloads still rebuild derived state. Short
paired fast gates check semantic workloads, concurrent Page waves and memory
recovery; they do not establish a universal throughput improvement or a stable
latency percentile. Track current results in [the performance report](report.md)
and use the [follow-up analysis](click-followup-20260913.md) for mutation-heavy
observation costs.
