# Mimic memory comparison

Current source with four memory changes, compared with the unchanged October 10 control and saved September 29 Chrome 152 observations. No live Chrome run or full cold/latency benchmark was performed.

The Windows Job Object runner, frozen local fixtures and 50 ms sampler are reused. Each workload has 20 measured warm single-Page attempts after one excluded warmup. Static, CPU and React density use 1/5/10/25/50/100 Pages, one excluded warmup per process, and max(5, ceil(20/N)) measured waves. Active Pages remain alive until the wave ends. Recovery is 250 ms. RSS sums working sets and can count shared pages more than once.

The memory adapter has a 120 s wave guard rather than the full harness's 180 s guard; this guard was not reached. Per-Page CDP timeout remains 30 s. These memory observations do not replace the published full benchmark or establish general website compatibility.

## Retained-cost changes

- Native platform operations use one native gate without an extra private JS facade.
- Lazy generated operations are installed after reflective bootstrap finalizers, which otherwise materialize them eagerly.
- Bootstrap snapshots preserve function identity and lexical state without retaining compiled installation bytecode. Cache identity includes this policy.
- Installed-font names and order remain eager; immutable nominal coverage is decoded once per resource on demand. Shaping faces and author fonts remain Page-owned.

These experiments identify retained costs in the current implementation; they are not a complete historical commit bisect.

## Ready memory

| Metric | October control Mimic | Final Mimic | September Chrome |
|---|---:|---:|---:|
| rss MiB | 49.00 | 40.97 | 379.54 |
| private MiB | 104.36 | 95.99 | 174.33 |

## Warm single-Page sampled peaks

| Workload | Control RSS MiB | Final RSS MiB | September Chrome RSS MiB | Final private MiB | September Chrome private MiB |
|---|---:|---:|---:|---:|---:|
| static | 139.09 | 106.07 | 1211.70 | 149.92 | 599.05 |
| cpu | 135.39 | 119.97 | 1403.35 | 162.69 | 778.80 |
| dom | 155.68 | 140.14 | 1415.14 | 180.93 | 776.44 |
| async | 136.54 | 120.23 | 1251.86 | 160.26 | 638.58 |
| react | 138.81 | 121.67 | 1412.52 | 161.51 | 803.89 |
| wasm | 128.69 | 111.87 | 1291.15 | 152.17 | 630.81 |

## Active density memory

An absent historical Chrome comparison means its series was not completed successfully. Diagnostic and failed rows are not used as successful reference measurements.

| Workload | Pages | Control RSS MiB | Final RSS MiB | September Chrome RSS MiB | Final private MiB | Final recovery RSS MiB | Valid / attempts |
|---|---:|---:|---:|---:|---:|---:|---:|
| static | 1 | 122.03 | 108.12 | 1210.81 | 151.41 | 90.42 | 20 / 20 |
| static | 5 | 186.54 | 143.99 | 1373.74 | 201.27 | 79.10 | 25 / 25 |
| static | 10 | 254.73 | 208.90 | 1682.53 | 282.64 | 100.61 | 50 / 50 |
| static | 25 | 467.03 | 408.42 | 2571.97 | 519.20 | 110.09 | 125 / 125 |
| static | 50 | 823.99 | 714.91 | 4102.04 | 897.42 | 132.27 | 250 / 250 |
| static | 100 | 1513.99 | 1322.40 | — | 1637.14 | 168.25 | 500 / 500 |
| cpu | 1 | 139.41 | 128.00 | — | 170.80 | 100.56 | 20 / 20 |
| cpu | 5 | 239.70 | 204.97 | — | 263.97 | 93.03 | 25 / 25 |
| cpu | 10 | 359.96 | 313.02 | — | 392.32 | 106.30 | 50 / 50 |
| cpu | 25 | 723.24 | 661.27 | — | 794.28 | 123.53 | 125 / 125 |
| cpu | 50 | 1324.79 | 1209.24 | — | 1434.86 | 141.03 | 250 / 250 |
| cpu | 100 | 2519.11 | 2305.17 | — | 2709.32 | 181.36 | 500 / 500 |
| react | 1 | 136.44 | 117.47 | — | 157.59 | 95.07 | 20 / 20 |
| react | 5 | 214.79 | 179.85 | — | 236.29 | 95.45 | 25 / 25 |
| react | 10 | 319.49 | 290.53 | — | 363.49 | 128.07 | 50 / 50 |
| react | 25 | 610.68 | 559.96 | — | 681.43 | 162.70 | 125 / 125 |
| react | 50 | 1094.36 | 985.65 | — | 1185.25 | 210.69 | 250 / 250 |
| react | 100 | — | 1828.22 | — | 2183.93 | 294.54 | 500 / 500 |

## Validation and tradeoffs

Focused callable/native-source, snapshot equivalence/lifecycle, lazy-operation mutation/freeze, canvas/font oracle and fallback tests passed. Concurrent coverage initialization passed the focused race check. Frozen semantic expectations and fixtures were unchanged.

The snapshot-code policy separately reduced static 50-Page RSS from 771.51 to 741.19 MiB in paired diagnostics, with approximately 7% lower batch throughput. Two alternating Wikipedia pairs passed all four complete workflows; their median elapsed times were 9.698 and 9.478 s. This memory tradeoff is not a universal speed improvement.

For deferred font coverage, three clean alternating Wikipedia pairs passed all six complete workflows. Median elapsed times were 8.970 and 9.008 s. Median peak RSS was 352.14 and 356.73 MiB: no Wikipedia peak-memory reduction was established for that change. 

No forced Go/V8 collection was used in the memory matrix. Forced-GC heap snapshots are separate diagnostic artifacts. Recovery/cache retention does not prove leak absence. Background applications, OS caches and sampling variation remain factors.


One earlier remeasurement ended during the React 50-Page warmup after correct workload results but before teardown. Its rows are excluded from successful comparisons. The complete repeat reported here passed, including React at 50 and 100 Pages; this does not prove the intermittent process-exit cause is fixed.

## Provenance

Measured executable SHA-256: `d3dbba851da61ebda85619f5b44c8be60c80dcc66257cba02629820a6af833ee`. Production source matches `0fbe2dd11f8a0fc54127472177f49dd79fe0c43a`. Chrome reference: [September checkpoint](../13-rss-20260929/public-summary.md). This memory-only checkpoint passed 120/120 measured single-Page attempts and 2,910/2,910 density sessions. Numerical results: [public-results.json](public-results.json). Detailed diagnostic artifacts remain private.
