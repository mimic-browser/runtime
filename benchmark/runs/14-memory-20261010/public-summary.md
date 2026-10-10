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
| rss MiB | 49.00 | 40.95 | 379.54 |
| private MiB | 104.36 | 96.11 | 174.33 |

## Warm single-Page sampled peaks

| Workload | Control RSS MiB | Final RSS MiB | September Chrome RSS MiB | Final private MiB | September Chrome private MiB |
|---|---:|---:|---:|---:|---:|
| static | 139.09 | 117.35 | 1211.70 | 150.07 | 599.05 |
| cpu | 135.39 | 119.19 | 1403.35 | 160.61 | 778.80 |
| dom | 155.68 | 138.74 | 1415.14 | 178.17 | 776.44 |
| async | 136.54 | 122.83 | 1251.86 | 163.87 | 638.58 |
| react | 138.81 | 120.45 | 1412.52 | 161.70 | 803.89 |
| wasm | 128.69 | 111.43 | 1291.15 | 152.46 | 630.81 |

## Active density memory

An absent historical Chrome comparison means its series was not completed successfully. Diagnostic and failed rows are not used as successful reference measurements.

| Workload | Pages | Control RSS MiB | Final RSS MiB | September Chrome RSS MiB | Final private MiB | Final recovery RSS MiB | Valid / attempts |
|---|---:|---:|---:|---:|---:|---:|---:|
| static | 1 | 122.03 | 106.74 | 1210.81 | 150.63 | 88.87 | 20 / 20 |
| static | 5 | 186.54 | 149.11 | 1373.74 | 208.24 | 85.27 | 25 / 25 |
| static | 10 | 254.73 | 221.79 | 1682.53 | 294.28 | 100.34 | 50 / 50 |
| static | 25 | 467.03 | 407.45 | 2571.97 | 521.16 | 113.32 | 125 / 125 |
| static | 50 | 823.99 | 719.06 | 4102.04 | 901.34 | 134.84 | 250 / 250 |
| static | 100 | 1513.99 | 1317.69 | — | 1633.38 | 169.59 | 500 / 500 |
| cpu | 1 | 139.41 | 129.53 | — | 170.66 | 101.63 | 20 / 20 |
| cpu | 5 | 239.70 | 202.09 | — | 262.07 | 105.28 | 25 / 25 |
| cpu | 10 | 359.96 | 327.49 | — | 405.82 | 108.64 | 50 / 50 |
| cpu | 25 | 723.24 | 661.75 | — | 793.89 | 124.31 | 125 / 125 |
| cpu | 50 | 1324.79 | 1206.30 | — | 1433.83 | 143.16 | 250 / 250 |
| cpu | 100 | 2519.11 | 2299.24 | — | 2716.16 | 178.47 | 500 / 500 |
| react | 1 | 136.44 | 121.50 | — | 161.81 | 98.68 | 20 / 20 |
| react | 5 | 214.79 | 195.18 | — | 253.54 | 109.38 | 25 / 25 |
| react | 10 | 319.49 | 282.80 | — | 356.23 | 121.47 | 50 / 50 |
| react | 25 | 610.68 | 565.27 | — | 692.27 | 160.99 | 125 / 125 |
| react | 50 | 1094.36 | 984.02 | — | 1187.68 | 207.49 | 250 / 250 |
| react | 100 | — | 1832.55 | — | 2193.56 | 297.55 | 500 / 500 |

## Validation and tradeoffs

Focused callable/native-source, snapshot equivalence/lifecycle, lazy-operation mutation/freeze, canvas/font oracle and fallback tests passed. Concurrent coverage initialization passed the focused race check. Frozen semantic expectations and fixtures were unchanged.

The snapshot-code policy separately reduced static 50-Page RSS from 771.51 to 741.19 MiB in paired diagnostics, with approximately 7% lower batch throughput. Two alternating Wikipedia pairs passed all four complete workflows; their median elapsed times were 9.698 and 9.478 s. This memory tradeoff is not a universal speed improvement.

For deferred font coverage, three clean alternating Wikipedia pairs passed all six complete workflows. Median elapsed times were 8.970 and 9.008 s. Median peak RSS was 352.14 and 356.73 MiB: no Wikipedia peak-memory reduction was established for that change. 

No forced Go/V8 collection was used in the memory matrix. Forced-GC heap snapshots are separate diagnostic artifacts. Recovery/cache retention does not prove leak absence. Background applications, OS caches and sampling variation remain factors.

## Provenance

Measured executable SHA-256: `27a564b0e5bd5087a838eb380ac1b6b7b64fde6bd975ae45f0ae750091a5b627`. Chrome reference: [September checkpoint](../13-rss-20260929/public-summary.md). This memory-only checkpoint passed 120/120 measured single-Page attempts and 2,910/2,910 density sessions. Numerical results: [public-results.json](public-results.json). Detailed diagnostic artifacts remain private.
