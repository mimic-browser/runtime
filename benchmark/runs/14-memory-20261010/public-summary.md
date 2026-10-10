# Mimic memory comparison

Current source with three memory changes, compared with the unchanged October 10 control and saved September 29 Chrome 152 observations. No live Chrome run or full cold/latency benchmark was performed.

The Windows Job Object runner, frozen local fixtures and 50 ms sampler are reused. Each workload has 20 measured warm single-Page attempts after one excluded warmup. Static, CPU and React density use 1/5/10/25/50/100 Pages, one excluded warmup per process, and max(5, ceil(20/N)) measured waves. Active Pages remain alive until the wave ends. Recovery is 250 ms. RSS sums working sets and can count shared pages more than once.

The memory adapter has a 120 s wave guard rather than the full harness's 180 s guard; this guard was not reached. Per-Page CDP timeout remains 30 s. These memory observations do not replace the published full benchmark or establish general website compatibility.

## Retained-cost changes

- Native platform operations use one native gate without an extra private JS facade.
- Bootstrap snapshots preserve function identity and lexical state without retaining compiled installation bytecode. Cache identity includes this policy.
- Installed-font names and order remain eager; immutable nominal coverage is decoded once per resource on demand. Shaping faces and author fonts remain Page-owned.

These experiments identify retained costs in the current implementation; they are not a complete historical commit bisect.

## Ready memory

| Metric | October control Mimic | Final Mimic | September Chrome |
|---|---:|---:|---:|
| rss MiB | 49.00 | 40.80 | 379.54 |
| private MiB | 104.36 | 95.95 | 174.33 |

## Warm single-Page sampled peaks

| Workload | Control RSS MiB | Final RSS MiB | September Chrome RSS MiB | Final private MiB | September Chrome private MiB |
|---|---:|---:|---:|---:|---:|
| static | 139.09 | 145.39 | 1211.70 | 172.38 | 599.05 |
| cpu | 135.39 | 121.07 | 1403.35 | 163.77 | 778.80 |
| dom | 155.68 | 139.14 | 1415.14 | 179.69 | 776.44 |
| async | 136.54 | 117.69 | 1251.86 | 157.69 | 638.58 |
| react | 138.81 | 119.12 | 1412.52 | 160.63 | 803.89 |
| wasm | 128.69 | 112.72 | 1291.15 | 152.18 | 630.81 |

## Active density memory

An absent historical Chrome comparison means its series was not completed successfully. Diagnostic and failed rows are not used as successful reference measurements.

| Workload | Pages | Control RSS MiB | Final RSS MiB | September Chrome RSS MiB | Final private MiB | Final recovery RSS MiB | Valid / attempts |
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

Measured executable SHA-256: `fd0fca18f8be20a64ce684b59d21a9b50479cf759bf1761cc148c7d17f298c00`. Production source matches `0480233219982ef79162b147565067216e7bef38`. Chrome reference: [September checkpoint](../13-rss-20260929/public-summary.md). This memory-only checkpoint passed 120/120 measured single-Page attempts and 2,810/2,910 density attempts; failed React-100 rows are excluded from successful comparisons. Numerical results: [public-results.json](public-results.json). Detailed diagnostic artifacts remain private.
