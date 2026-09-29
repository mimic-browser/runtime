# Mimic vs. Chrome: 2026-09-29

Fresh builds on one Windows workstation, using the unchanged frozen workloads. 12/12 correctness gates and 360/360 measured single-page attempts passed. 6 concurrency series stopped or contained a failure. All observations, including failed attempts and excluded warmups, remain in the data. These are controlled fixtures and do not establish general website compatibility.

The benchmark's conservative guard stopped the density schedule at 100 static
Pages with 4.54 GiB still available (guard threshold: 4.77 GiB). This is not
an out-of-memory event. CPU and React density series have no measured waves
in this run; their excluded diagnostic rows are not comparisons. The headline
below uses only the completed 50-Page static series. The measured Mimic executable is
SHA-256 `db82f1c85faca672e7225260e4d0cc603d6a867effb7ca60a540e9e14eceabc4`:
it was built from the architecture/font changes and the snapshot-selection fix
later committed as `04ac211`. Later unrelated `main` fixes were not in this
binary.

## Environment

| Item | Value |
|---|---|
| Platform | Windows-11-10.0.26200-SP0 |
| CPU | Intel(R) Core(TM) i7-14700KF |
| RAM | 31.83 GiB |
| Chrome | 152.0.7977.82 (headless=new) |
| Mimic / Chrome V8 | 15.2.124.1-rusty / 15.2.124.21 |
| Started / completed | 2026-09-29T12:21:18.126083+04:00 / 2026-09-29T12:26:58.395024+04:00 |

Executable hashes are checked before every launch. This is an interactive workstation with background applications and antivirus enabled; cache and scheduler variation remain possible.

## Startup and ready memory

Ten fresh processes per runtime, alternating order, after an excluded warmup. Both answer the same Target.getTargets readiness probe. Summed process-tree RSS includes the initial page and can count shared pages more than once; it is not marginal Page memory.

| Runtime | CDP ready p50, ms | p95, ms | Ready RSS, MiB | Ready private bytes, MiB |
|---|---|---|---|---|
| Mimic | 225.91 | 234.96 | 45.79 | 96.75 |
| Chrome | 318.97 | 361.23 | 379.54 | 174.33 |

## Warm execution and completion

Twenty retained samples per workload/runtime. Each iteration creates a new Page and origin in the warm process. Execution includes invoking the workload and detecting its validated result. Completion also includes navigation; Page creation and teardown are excluded. Lower is better.

| Workload | Mimic execution, ms | Chrome execution, ms | Mimic completion, ms | Chrome completion, ms |
|---|---|---|---|---|
| Static DOM | 3.87 | 3.99 | 34.95 | 27.18 |
| JavaScript / crypto | 34.29 | 30.16 | 65.62 | 53.78 |
| DOM mutations | 136.58 | 30.88 | 168.20 | 54.17 |
| Async / networking | 85.39 | 29.95 | 115.15 | 54.52 |
| React | 47.24 | 23.54 | 87.62 | 47.57 |
| WebAssembly | 5.41 | 4.84 | 35.86 | 29.10 |

## Cold end-to-end completion

Ten fresh-process samples per workload/runtime. Includes process startup, Page creation, navigation, execution, teardown and process exit. OS caches are not flushed. Server maintenance and temporary-profile removal are excluded. A failed series is withheld from comparisons.

| Workload | Mimic p50, ms | Chrome p50, ms |
|---|---|---|
| Static DOM | 496.80 | 483.12 |
| JavaScript / crypto | 520.96 | 547.30 |
| DOM mutations | 628.59 | 530.09 |
| Async / networking | 592.32 | 542.95 |
| React | 530.58 | 485.62 |
| WebAssembly | 505.18 | 474.24 |

## CPU and memory during warm work

Process-tree user plus kernel time per session; sampled peaks may miss short-lived allocations.

| Workload | Runtime | CPU, ms/session | Peak RSS, MiB | Peak private bytes, MiB |
|---|---|---|---|---|
| Static DOM | Mimic | 39.06 | 138.11 | 169.38 |
| Static DOM | Chrome | 195.31 | 1211.70 | 599.05 |
| JavaScript / crypto | Mimic | 78.12 | 152.13 | 181.19 |
| JavaScript / crypto | Chrome | 257.81 | 1403.35 | 778.80 |
| DOM mutations | Mimic | 218.75 | 161.94 | 190.90 |
| DOM mutations | Chrome | 234.38 | 1415.14 | 776.44 |
| Async / networking | Mimic | 125.00 | 151.11 | 184.15 |
| Async / networking | Chrome | 250.00 | 1251.86 | 638.58 |
| React | Mimic | 117.19 | 146.23 | 176.15 |
| React | Chrome | 265.62 | 1412.52 | 803.89 |
| WebAssembly | Mimic | 46.88 | 142.42 | 170.17 |
| WebAssembly | Chrome | 187.50 | 1291.15 | 630.81 |

## Concurrent Pages

Fresh process per level, one excluded warmup, then max(5, ceil(20/N)) measured waves. Throughput includes setup and teardown but excludes the separate 250 ms recovery wait. Every attempted level is shown. A stopped series is not a stable successful result.

| Workload | Runtime | Pages | Waves | Success | Sessions/s | Active RSS, MiB | Recovered RSS, MiB | Status |
|---|---|---|---|---|---|---|---|---|
| Static DOM | Chrome | 1 | 20 | 100.0% | 8.62 | 1210.81 | 1152.31 | Completed |
| Static DOM | Mimic | 1 | 20 | 100.0% | 14.46 | 138.43 | 120.74 | Completed |
| Static DOM | Chrome | 5 | 5 | 100.0% | 25.58 | 1373.74 | 1087.43 | Completed |
| Static DOM | Mimic | 5 | 5 | 100.0% | 80.58 | 175.31 | 111.27 | Completed |
| Static DOM | Chrome | 10 | 5 | 100.0% | 27.86 | 1682.53 | 1138.27 | Completed |
| Static DOM | Mimic | 10 | 5 | 100.0% | 81.60 | 247.82 | 126.61 | Completed |
| Static DOM | Chrome | 25 | 5 | 100.0% | 25.57 | 2571.97 | 1148.70 | Completed |
| Static DOM | Mimic | 25 | 5 | 100.0% | 125.64 | 435.82 | 139.17 | Completed |
| Static DOM | Chrome | 50 | 5 | 100.0% | 18.09 | 4102.04 | 1209.67 | Completed |
| Static DOM | Mimic | 50 | 5 | 100.0% | 108.67 | 748.11 | 158.98 | Completed |
| Static DOM | Chrome | 100 | 0 | 0.0% | 0.00 | 5344.43 | 1052.40 | memory pressure (<15% or 2 GiB available) |
| Static DOM | Mimic | 100 | 0 | 100.0% | 95.83 | 845.35 | 152.32 | memory pressure (<15% or 2 GiB available) |
| JavaScript / crypto | Chrome | 1 | 0 | 0.0% | 0.00 | 392.61 | 515.27 | memory pressure (<15% or 2 GiB available) |
| JavaScript / crypto | Mimic | 1 | 0 | 100.0% | 4.26 | 80.24 | 109.63 | memory pressure (<15% or 2 GiB available) |
| React | Chrome | 1 | 0 | 0.0% | 0.00 | 380.30 | 515.18 | memory pressure (<15% or 2 GiB available) |
| React | Mimic | 1 | 0 | 100.0% | 3.77 | 78.39 | 110.15 | memory pressure (<15% or 2 GiB available) |

Recovery uses no forced collection. Allocator pools and shared runtime artifacts can remain resident; this table alone cannot prove leak absence. Private memory, marginal slopes, CPU and latency distributions are included in the numerical data.

## Measurement boundaries

Identical local fixtures, unique origins, HTTP cache disabled, full supported resource loading. The six fixtures cover static DOM, JavaScript/crypto, 3,000 DOM elements, asynchronous networking and Workers, React 18.3.1 and WebAssembly. Completion requires the exact expected result. No paint or network-idle delay is included. External high-resolution clocks, 5 ms result polling and 50 ms memory sampling are unchanged. Slow samples are retained; p95 from 10–20 samples is unstable. These measurements do not establish feature parity or costs on arbitrary sites.

## Data and provenance

[Numerical export](public-results.json) includes all summary metrics, numerical single-page and startup samples, concurrency outcomes and executable/harness hashes. [Full report](report.md) and [raw observations](raw.json) retain detailed evidence. [Optimization decisions](../../../docs/performance/optimization-campaign-20260914.md) distinguish these Windows observations from the primary paired Linux experiments.
