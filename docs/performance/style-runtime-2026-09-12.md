# Style runtime and Page scaling — 2026-09-12

Baseline: `366346f99ebaba8d57a7001e581a58fbdb12b7bc`. Production changes were made directly on `main`. Fresh baseline and candidate builds were verified by executable hashes; build receipts and compact measurements are in [measurements.json](style-runtime-20260912/measurements.json). Frozen workloads and original baseline data were not modified.

## Measured cause and changes

The live page had a timer callback lasting 2637 ms under profiling. It repeatedly copied stylesheet responses across Go/V8, rebuilt ordered rule arrays, matched irrelevant selectors, and allocated DOM projections. The final profiled callback was 409 ms. This is remaining work on the Page event loop, not CDP connection latency.

- CSS owner revalidation omits a response body when the existing sheet has the same URL identity. Availability is still checked.
- Stylesheet owner membership is derived from the canonical DOM revision. Ordered rule programs are reused only while the canonical source texts and their order agree. No computed element match is cached across author code.
- An index selects candidate rules by a necessary rightmost tag/class/ID; the complete existing selector matcher still decides applicability. Unindexable selectors retain a fallback path. Predicates and declaration lists are compiled only when selected.
- Base URL lookups cache only the parsed reference under the canonical DOM revision; history and inherited fallback URLs are resolved anew.
- Attribute enumeration transfers only ordered names. Traversal reuses canonical Document identity instead of repeatedly fetching its node data; bootstrap restoration refreshes the host root ID.
- HTTP freshness now supports Last-Modified heuristics and accounts for Date/Age. Explicit zero freshness, no-cache and no-store never fall back to heuristics. Chrome oracle requests verified hit/miss behavior.

The changes preserve one event loop per Page. They do not move author JavaScript off that loop or add global shared Page state.

## Live client workload

The client workload was unchanged for the final measurement, with its SHA recorded. A wrapper changed only the endpoint and timed its calls. Identical 1280×720 viewports were prepared outside the timed runs. Three measured alternating runs per browser follow one excluded warm-up; browser profiles/caches are reused. Chrome was 152.0.7977.83. All 12 runs returned 5 categories, 27 forums and 39 subforums.

| Median, ms | Baseline Mimic | Updated Mimic | Chrome |
|---|---:|---:|---:|
| Navigation to DOMContentLoaded | 3613.54 | 2208.97 | 1637.02 |
| Browser-info evaluation, including queue wait | 2534.78 | 394.63 | 10.28 |
| Forum parsing | 17.47 | 16.81 | 11.48 |
| Document statistics | 27.78 | 25.91 | 5.81 |
| Total script | 6231.86 | 2687.24 | 1702.90 |

Total median improves from 6.23 s to 2.69 s (57% less time, 2.32× faster). The queued info call falls from 2535 to 395 ms. Chrome remains faster for this single live page at 1.70 s. Network variability and reused profiles prevent a controlled cold-start claim. A first candidate load was still approximately 7.8 s with a cold resource cache in an earlier trial.

A candidate comparison accidentally reached an unrelated listener on the intended baseline port and was discarded. Final endpoint ownership was checked against the newly launched process IDs before accepting these measurements.

## Parallel Pages

The independent scaling tool imports the unchanged frozen runner and uses its exact expected outputs. Chrome for this matrix was the pinned 152.0.7977.82. Each workload has a unique local origin, disabled HTTP cache and a barrier after all targets are created. A wave retains targets until every result is checked, then closes them. Throughput includes target creation and teardown; p95 covers navigation plus workload execution after the barrier. Thus these results include the process/target overhead that particularly affects Chrome; they are not live-site throughput.

One warm-up plus three measured waves per case; all **3620 Page executions** passed across the matrix and the repeat. No memory-pressure stop was triggered. Other applications remained open on this machine.

| Workload / Pages | Baseline Pages/s | Updated Pages/s | Chrome Pages/s | Updated active RSS MiB | Chrome active RSS MiB |
|---|---:|---:|---:|---:|---:|
| static / 10 | 77.00 | 74.20 | 25.04 | 460 | 1670 |
| static / 25 | 89.42 | 90.29 | 23.61 | 865 | 2586 |
| static / 50 | 83.47 | 83.39 | 25.12 | 1714 | 4118 |
| static / 100 | 92.64 | 84.60 | 18.69 | 3367 | 7098 |
| react / 50 | 58.06 | 55.76 | 16.75 | 2128 | 4797 |

At 50 static Pages the candidate is 3.32× Chrome throughput; at 100 it is 4.51×. This advantage already existed before this patch. The patch does **not** establish higher parallel throughput: the first 100-Page median declined 92.6 → 84.6 Pages/s; an opposite-order repeat was 87.8 → 85.2. Individual repeated waves span approximately 44–108 Pages/s. Treat the 100-Page change as unresolved performance variance, not proven neutrality or improvement.

RSS is measured with all targets retained. Recovery measurements are 250 ms after teardown without forced GC, not a proof of zero retained memory. On the 50-Page React case candidate recovery RSS ranged 306–1061 MiB versus baseline 314–921 MiB; subsequent process shutdown reclaimed the owned runtime.

## Frozen fast gates and correctness

Both complete fast gates passed six mandatory workloads, 10/25-Page waves and static/React recovery checks. Median DOM execution was 158.78 → 151.97 ms; React completion 112.75 → 86.16 ms. Separate fast-gate 10/25-Page throughput was 74.76/83.26 → 77.07/90.00 Pages/s; the larger matrix above demonstrates why this one pair should not be generalized.

- Chrome 152.0.7977.83 oracle agrees with the mutation sequence for indexed rules, CSSOM edits, classes/IDs, owner removal, media attributes, sheet disabling, adopted sheets and shadow roots.
- Focused regression tests pass, including canonical base/history changes and ordered attribute snapshots. Focused browser/DOM/network race tests pass.
- The full Go run completed the pre-existing browser tests and CDP suite. Its draft new CSS test used an unsupported media property setter; the finalized canonical-attribute version matches Chrome and passes, including under race detection.
- The full run encountered an IPv6 QUIC timeout; a complete isolated network rerun passed.
- `go test ./...` also includes local ignored `compatibility/private-captures` utilities which fail compilation (duplicate main functions and an undefined New). These files were not changed or deleted. Therefore this is not a claim that the literal whole-workspace command is green.

## Reproduction

Run `tools/performance/fast_gate.py` against a clean baseline checkout and the candidate; each invocation builds its own executable. Then use the extra scaling runner:

```powershell
.build/benchmark-venv/Scripts/python.exe tools/performance/concurrency_scaling.py --before .build/style-gate-before/mimic.exe --after .build/style-gate-after/mimic.exe --chrome compatibility/.chrome-for-testing/152.0.7977.82/chrome-win64/chrome.exe --output .build/new-scaling
```

Use `--cases static:100 --order before after` to repeat just the high-concurrency Mimic comparison. The frozen harness remains untouched.

Remaining bottlenecks: CSSOM parsing/serialization at first style access, roughly 0.4 s of post-DOMContentLoaded initialization on this live page, and sequential parser-blocking resource loads on a cold network cache. Single-page Chrome latency has not been beaten.
