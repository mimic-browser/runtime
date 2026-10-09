# Wikipedia Playwright E2E performance

This is the acceptance reference for performance changes affecting
`tools/runtimecheck/playwright_wikipedia_local.js`. Keep that workload and its
assertions unchanged. Compare the complete cold and warm workflow, not only a
microbenchmark or a selected action.

## Established production measurement

The [native producer measurement](blitz-production-final-2026-09-20.md) uses five
alternating fresh-process pairs:

| Phase | Baseline median | Native producer median | Paired median improvement |
| --- | ---: | ---: | ---: |
| Cold | 9648 ms | 6144 ms | 3489 ms (36.2%) |
| Warm | 5879 ms | 3977 ms | 1854 ms (31.7%) |

All 20 cold/warm executions completed. The linked measurement retains exact
binary/workload hashes, ranges, stage timings and correctness evidence. These
are measurements of the identified builds, not a promise for every release or
host. The result does not establish Chrome parity. Scroll remains a measured
regression; timings from different or instrumented controls are not additive.

The native style/geometry producer handles admitted Documents. The JavaScript
producer is an explicit semantic fallback/oracle rather than a user-selectable
alternative performance mode. Canonical Go DOM state and realm ownership remain
authoritative.

## Current canonical-read checkpoint

The [current performance summary](report.md#canonical-reads-and-deferred-font-resolution)
records two alternating fresh-process pairs with the unchanged workload.
All eight cold/warm executions passed. Cold medians were 23.792 s for the
control and 18.497 s for the candidate; warm medians were 13.926 and 8.391 s.
The candidate defers unused box-state font resolution and reuses canonical
DOM/style observations. These matched results show a 22.3% cold and 39.7% warm
latency reduction for the identified builds. They are a separate checkpoint
from the earlier native producer series and do not establish competitor parity.

## Attribution and rejected approaches

The [style projection allocation checkpoint](report.md#style-projection-allocation)
removes temporary native readback buffers and duplicate attribute projections.
Three alternating fresh-process pairs passed all 12 complete executions:
cold medians were 16.741/16.758 s and warm medians 7.972/7.820 s for the
control/candidate. Allocation pressure decreased, while a process RSS reduction
was not established. These matched results show no material E2E regression;
they must not be pooled with the earlier 18.497-second checkpoint.

- Warm utility callback work can dominate when Page owner wait and CDP
  serialization are small. Aggregate Go/V8 crossing counts alone do not identify
  the cause.
- First style/geometry observations have a substantial construction cost.
  Moving that work into navigation or prewarming is not an E2E improvement unless
  the complete workflow improves.
- Broad Document-owned retained-state experiments improved isolated reads but
  regressed the complete workflow. Retaining more state needs new E2E evidence.
- Shared V8 execution ownership across lifecycle boundaries produced navigation
  and teardown problems. Independent Pages must stay isolated.
- Faster individual `nodeData`, `getAttribute`, wrapper, JSON or selector
  operations do not establish a complete workflow improvement.
- Persisting existing geometry caches more aggressively did not by itself
  improve the target workflow.
- Definite-position geometry, conservative hit-test hints and no-op scrolling
  improved a 13k-node actionability fixture, but did not establish an E2E win.
- Fabricated geometry, skipped scroll/input work, stale DOM snapshots and
  memoized positive selector results change browser semantics. Passing the
  workload's assertions does not make those shortcuts valid production fixes.
- Destructive interventions interact. Their timing deltas are causal probes,
  not additive savings or guaranteed compatible upper bounds.
- Direct stylesheet projection must preserve declaration normalization,
  specificity, pseudo state and mutation behavior. Faster projection alone
  cannot justify an observable change.

## Acceptance and method

1. Use fresh control and candidate binaries with explicit source identities.
   Keep the host conditions matched and unrelated runtime processes out of the
   measurement.
2. Run the complete unchanged workflow early. A material local delta must also
   be checked against cold/warm E2E before expanding the optimization.
3. Record successful and failed attempts, sample counts, ranges, binary/workload
   hashes and instrumentation conditions. Compare only matched controls.
4. Separate cold navigation/queue costs, warm execution costs and retained
   memory. Do not sum overlapping profile categories.
5. Preserve navigation, task/microtask ordering, canonical state, cross-realm
   identity, occlusion checks, teardown and concurrent Page isolation.
6. Validate focused semantic regressions, then the appropriate performance gate.
   A slower complete workflow rejects a locally faster implementation.

## Reproduction

Start a fresh source build on a loopback CDP endpoint:

```sh
go run ./tools/runmimic -listen 127.0.0.1:9222
```

Run the unchanged workload with `PW_MIMIC_ENDPOINT=http://127.0.0.1:9222`:

```sh
node tools/runtimecheck/playwright_wikipedia_local.js
```

`tools/runtimecheck/playwright_wikipedia_profile.js` supplies attributed
diagnostics. Its tracing traffic makes it unsuitable for latency claims.
Chrome comparisons follow `docs/oracle-policy.md`; retain successful captures
and exact launch/instrumentation provenance before further experiments.
