# Mimic workload benchmark

Current source with three memory changes, compared with the unchanged October 10 control and saved September 29 Chrome 152 observations. Timing and CPU are recovered from the final saved capture and the October 10 Chrome reference. No additional live Chrome run was performed.

The Windows Job Object runner, frozen local fixtures and 50 ms sampler are reused. Each workload has 20 measured warm single-Page attempts after one excluded warmup. Static, CPU and React density use 1/5/10/25/50/100 Pages, one excluded warmup per process, and max(5, ceil(20/N)) measured waves. Active Pages remain alive until the wave ends. Recovery is 250 ms. RSS sums working sets and can count shared pages more than once.

The memory adapter has a 120 s wave guard rather than the full harness's 180 s guard; this guard was not reached. Per-Page CDP timeout remains 30 s. This is the current public benchmark. It publishes active memory, warm latency/CPU and fixed-concurrency batch throughput; Optimize acquisition is a separate recorded workload. It does not establish general website compatibility.

## Retained-cost changes

- Native platform operations use one native gate without an extra private JS facade.
- Bootstrap snapshots preserve function identity and lexical state without retaining compiled installation bytecode. Cache identity includes this policy.
- Installed-font names and order remain eager; immutable nominal coverage is decoded once per resource on demand. Shaping faces and author fonts remain Page-owned.

These experiments identify retained costs in the current implementation; they are not a complete historical commit bisect.

## Warm single-Page sampled peaks

| Workload | Control RSS MiB | Final RSS MiB | Chrome 152 reference RSS MiB | Final private MiB | Chrome 152 reference private MiB |
|---|---:|---:|---:|---:|---:|
| static | 139.09 | 145.39 | 1211.70 | 172.38 | 599.05 |
| cpu | 135.39 | 121.07 | 1403.35 | 163.77 | 778.80 |
| dom | 155.68 | 139.14 | 1415.14 | 179.69 | 776.44 |
| async | 136.54 | 117.69 | 1251.86 | 157.69 | 638.58 |
| react | 138.81 | 119.12 | 1412.52 | 160.63 | 803.89 |
| wasm | 128.69 | 112.72 | 1291.15 | 152.18 | 630.81 |

## Active density memory

An absent Chrome reference comparison means its series was not completed successfully. Diagnostic and failed rows are not used as successful reference measurements.

| Workload | Pages | Control RSS MiB | Final RSS MiB | Chrome 152 reference RSS MiB | Final private MiB | Final recovery RSS MiB | Valid / attempts |
|---|---:|---:|---:|---:|---:|---:|---:|
| static | 1 | 122.03 | 106.44 | 1210.81 | 150.71 | 88.05 | 20 / 20 |
| static | 5 | 186.54 | 148.94 | 1373.74 | 206.35 | 85.48 | 25 / 25 |
| static | 10 | 254.73 | 213.13 | 1682.53 | 286.76 | 101.89 | 50 / 50 |
| static | 25 | 467.03 | 418.23 | 2571.97 | 532.48 | 117.70 | 125 / 125 |
| static | 50 | 823.99 | 727.78 | 4102.04 | 914.64 | 133.59 | 250 / 250 |
| static | 100 | 1513.99 | 1351.21 | — | 1683.12 | 169.20 | 500 / 500 |
| cpu | 1 | 139.41 | 124.77 | — | 168.04 | 96.77 | 20 / 20 |
| cpu | 5 | 239.70 | 206.46 | — | 266.06 | 92.16 | 25 / 25 |
| cpu | 10 | 359.96 | 316.48 | — | 397.09 | 112.44 | 50 / 50 |
| cpu | 25 | 723.24 | 671.74 | — | 806.52 | 122.14 | 125 / 125 |
| cpu | 50 | 1324.79 | 1225.69 | — | 1449.98 | 141.21 | 250 / 250 |
| cpu | 100 | 2519.11 | 2333.55 | — | 2741.97 | 184.64 | 500 / 500 |
| react | 1 | 136.44 | 119.95 | — | 160.53 | 97.69 | 20 / 20 |
| react | 5 | 214.79 | 180.53 | — | 239.64 | 94.63 | 25 / 25 |
| react | 10 | 319.49 | 288.28 | — | 362.72 | 123.61 | 50 / 50 |
| react | 25 | 610.68 | 569.34 | — | 689.66 | 159.75 | 125 / 125 |
| react | 50 | 1094.36 | 1013.87 | — | 1218.41 | 208.83 | 250 / 250 |
| react | 100 | — | — | — | — | — | 400 / 500 |

## Validation and tradeoffs

Focused callable/native-source, snapshot equivalence/lifecycle, lazy-operation mutation/freeze, canvas/font oracle and fallback tests passed. Concurrent coverage initialization passed the focused race check. Frozen semantic expectations and fixtures were unchanged.

The snapshot-code policy separately reduced static 50-Page RSS from 771.51 to 741.19 MiB in paired diagnostics, with approximately 7% lower batch throughput. Two alternating Wikipedia pairs passed all four complete workflows; their median elapsed times were 9.698 and 9.478 s. This memory tradeoff is not a universal speed improvement.

For deferred font coverage, three clean alternating Wikipedia pairs passed all six complete workflows. Median elapsed times were 8.970 and 9.008 s. Median peak RSS was 352.14 and 356.73 MiB: no Wikipedia peak-memory reduction was established for that change. 

No forced Go/V8 collection was used in the memory matrix. Forced-GC heap snapshots are separate diagnostic artifacts. Recovery/cache retention does not prove leak absence. Background applications, OS caches and sampling variation remain factors.


React at 100 Pages is not certified: four measured waves passed, then a native access violation caused all 100 attempts in the final wave to fail. The failed series is retained as diagnostic evidence and excluded from successful memory comparisons. Static and CPU completed 100 Pages; React completed 50 Pages. Recovery observations do not prove leak absence.

## Provenance

Measured executable SHA-256: `fd0fca18f8be20a64ce684b59d21a9b50479cf759bf1761cc148c7d17f298c00`. Production source matches `0480233219982ef79162b147565067216e7bef38`. Chrome reference captured September 29, 2026; its hashes and fixture identity are preserved in the numerical results provenance. This checkpoint passed 120/120 measured single-Page attempts and 2,810/2,910 density attempts; failed React-100 rows are excluded from successful comparisons. Numerical results: [public-results.json](public-results.json). Detailed diagnostic artifacts remain private.

Two subsequent alternating control/candidate pairs completed all ten measured React-100 waves for each binary. The native failure was not reproduced or attributed; these focused repeats do not replace the failed full memory-series record.

## Processing the same number of Pages

Throughput is total valid Pages divided by total measured wave time, from Page creation through final teardown. HTTP-server setup and 250 ms recovery are excluded. CPU uses Job Object user+kernel time including descendants; overlapping per-Page intervals are not summed. Warmups and failed series are excluded. The Chrome timing reference was captured October 10; memory retains the frozen September 29 reference. Exact capture hashes are in the JSON, with no startup metrics in this public projection.

Both runners use the same frozen execute, fixtures, navigation barrier and 50 ms completion polling. Candidate timing includes the initial accounting snapshot and excludes the final snapshot; reference timing does the reverse. Guards are 120 s and 180 s, neither reached. Runs are unpaired; background load and sampling variation remain factors. These are observed batch results, not universal speed or production capacity claims.

| Workload | Pages | Mimic Pages/s | Chrome Pages/s | Mimic CPU ms/Page | Chrome CPU ms/Page | Mimic p50 / p95 ms | Chrome p50 / p95 ms |
|---|---:|---:|---:|---:|---:|---|---|
| static | 1 | 16.87 | 8.63 | 38.28 | 222.66 | 41.88 / 45.04 | 63.11 / 81.43 |
| static | 5 | 80.55 | 22.15 | 51.88 | 130.62 | 46.98 / 49.02 | 185.69 / 192.27 |
| static | 10 | 79.79 | 24.19 | 61.88 | 126.56 | 53.77 / 57.72 | 348.31 / 369.21 |
| static | 25 | 164.27 | 25.03 | 59.12 | 123.25 | 94.66 / 106.78 | 860.14 / 898.87 |
| static | 50 | 164.62 | 22.11 | 69.94 | 133.38 | 161.33 / 197.39 | 1853.23 / 2382.80 |
| static | 100 | 170.15 | 21.67 | 75.62 | 136.41 | 345.41 / 401.80 | 3983.93 / 4150.74 |
| cpu | 1 | 9.19 | 8.66 | 81.25 | 234.38 | 69.87 / 73.04 | 70.44 / 83.92 |
| cpu | 5 | 43.22 | 20.89 | 87.50 | 161.88 | 72.17 / 87.64 | 176.72 / 181.68 |
| cpu | 10 | 74.82 | 26.85 | 97.19 | 170.31 | 88.30 / 101.78 | 296.49 / 325.91 |
| cpu | 25 | 95.82 | 29.18 | 142.88 | 159.50 | 158.06 / 173.37 | 709.28 / 797.10 |
| cpu | 50 | 111.25 | 29.50 | 147.38 | 154.50 | 295.56 / 338.13 | 1448.40 / 1509.62 |
| cpu | 100 | 108.20 | 19.14 | 157.28 | 194.75 | 601.86 / 707.72 | 4558.45 / 5248.12 |
| react | 1 | 9.22 | 6.87 | 103.91 | 319.53 | 74.78 / 89.41 | 102.38 / 120.67 |
| react | 5 | 43.64 | 5.89 | 121.25 | 230.62 | 89.65 / 97.70 | 211.86 / 1187.50 |
| react | 10 | 56.41 | 8.65 | 143.12 | 186.88 | 121.63 / 129.34 | 1064.29 / 1086.55 |
| react | 25 | 75.96 | 24.76 | 174.50 | 157.25 | 225.50 / 242.37 | 874.72 / 891.93 |
| react | 50 | 83.26 | 17.08 | 180.81 | 194.62 | 450.47 / 483.54 | 2503.58 / 3199.40 |
| react | 100 | — | 16.57 | — | 212.44 | — / — | 4911.93 / 5679.77 |

## Warm single-Page latency and CPU

20 valid measured samples per workload and system; one warmup excluded. Completion is navigation through the exact workload result. CPU spans Page create through teardown. p95 uses linear interpolation. Lower batch memory/CPU does not imply lower single-Page latency.

| Workload | Mimic completion p50 / p95 ms | Chrome completion p50 / p95 ms | Mimic CPU ms/Page | Chrome CPU ms/Page |
|---|---|---|---:|---:|
| static | 32.05 / 35.02 | 18.44 / 22.07 | 31.25 | 140.62 |
| cpu | 59.47 / 65.83 | 45.59 / 52.14 | 62.50 | 234.38 |
| dom | 155.73 / 163.30 | 48.28 / 66.57 | 187.50 | 210.94 |
| async | 71.39 / 82.74 | 50.98 / 55.15 | 85.94 | 203.12 |
| react | 66.33 / 69.23 | 44.95 / 51.90 | 93.75 | 250.00 |
| wasm | 30.96 / 37.61 | 23.13 / 30.59 | 46.88 | 179.69 |

## Optimize acquisition

Books SSR extraction, recorded October 5: **284,591 → 5,276 encoded HTTP body bytes (98.1% less)** with installed Auto versus Mimic Default without Optimize. Acquired responses: **29 → 1**. Both variants passed **5/5** matched local trials. Manual tied Auto. One recorded state; training/validation took 27.1 seconds. Encoded acquired bodies are not physical wire traffic. See [Optimize methodology](../../../docs/performance/workload-optimization-feature.md#books-ssr).
