# Mimic V8 and Chrome 152: Windows x64 baseline

This is a measurement of the build identified below, using the frozen benchmark suite.

## Environment

| Parameter | Value |
|---|---|
| Start / end date | 2026-09-29T12:21:18.126083+04:00 / 2026-09-29T12:26:58.395024+04:00 |
| Chrome | {'protocolVersion': '1.3', 'product': 'Chrome/152.0.7977.82', 'revision': '@d04cdb24d67b081f6cf80200ffc5233f44b61109', 'userAgent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) HeadlessChrome/152.0.0.0 Safari/537.36', 'jsVersion': '15.2.124.21'} |
| Chromium | 1669021 / d04cdb24d67b081f6cf80200ffc5233f44b61109 |
| Mimic commit | d8acc2ca9cd509b5a034c465bd378170a95c7e28 |
| Mimic V8 | 15.2.124.1-rusty |
| OS | Windows-11-10.0.26200-SP0 |
| CPU | {"Name":"Intel(R) Core(TM) i7-14700KF","NumberOfCores":20,"NumberOfLogicalProcessors":28} |
| CPU physical / logical | 20 / 28 |
| RAM GiB | 31.83 |
| Power plan | Power Scheme GUID: 381b4222-f694-41f0-9685-ff5bb260df2e  (Balanced) |
| Power overlay | 0 00000000-0000-0000-0000-000000000000 |
| Antivirus | {"displayName":"Windows Defender","productState":397568} |
| Chrome SHA-256 | ea36dd818a90176f1a70616f0363d9be527229389a6c073a0b1688b9e73f67e9 |
| Mimic SHA-256 | db82f1c85faca672e7225260e4d0cc603d6a867effb7ca60a540e9e14eceabc4 |

The workstation was not dedicated exclusively to the test: background applications and antivirus were enabled. Process lists, tool versions, arguments, and fixture/binary SHA-256 hashes are in raw.json.

## Methodology and correctness

Both systems create a fresh page and a unique loopback origin. HTTP cache is disabled, responses use no-store, and cookies are not used. Contexts, transport, and the process persist in warm runs; page state is not reused. This provides isolation for this controlled corpus, not a test of tenant/security isolation. Cold runs create a new process and profile. Windows file, DLL, and OS DNS caches are not cleared; “cold” means a fresh process, not a cold disk. Warm HTTP-cache behavior was not measured.

Navigation completes only at the exact URL, with document.readyState === "complete" and __benchRun present. Both systems then explicitly invoke __benchRun; completion requires done and an exact match of the deterministic result. Settle = 0. Paint/networkidle are not awaited. Chrome uses headless=new; results do not automatically apply to headful mode.

External perf_counter/QPC clock; 5 ms polling plus CDP/OS scheduler latency. navigation_ms includes HTML parsing and script loading; execution_ms includes application startup/execution and marker detection. These are not isolated JIT or pure JavaScript timings. The page's js_ms is diagnostic: Mimic's virtual time is often zero.

The process starts suspended, is assigned to a Windows Job Object, and then resumes. process_start_ms measures the process-creation call; CDP readiness runs from the start of creation to the protocol response. runtime_initialization_ms is the remainder between them; internal V8 phases are not separately instrumented. Cold total includes page creation, execution, teardown, and process exit; temporary-profile removal and local-server maintenance are excluded.

CPU is user+kernel for the entire Job Object, including exited descendants. Working set/private bytes sum all current Job Object members every 50 ms and at checkpoints. Short memory peaks may be missed, and shared DLL pages may be counted multiple times. CPU % is relative to one logical core; 100% of the machine = 2800%. CPU peaks are sensitive to Windows counter granularity.

One correctness check and one initial warmup per series are explicitly excluded. Main series: 10 cold and 20 warm runs, without removing slow observations. p95 is published at n≥10 and p99 at n≥100; empirical quantiles use linear interpolation, and tails at n=10–20 are particularly unstable. SD, CV, and min/max for all series are available in summary.csv. Speedups are not aggregated into a single ratio.

| System / workload | Correctness gate |
|---|---|
| chrome/async | VALID |
| chrome/cpu | VALID |
| chrome/dom | VALID |
| chrome/react | VALID |
| chrome/static | VALID |
| chrome/wasm | VALID |
| mimic/async | VALID |
| mimic/cpu | VALID |
| mimic/dom | VALID |
| mimic/react | VALID |
| mimic/static | VALID |
| mimic/wasm | VALID |

### CDP readiness: separate series with an identical probe

In the final series, both systems respond to Target.getTargets after the WebSocket handshake. Each uses 10 fresh processes, alternating system order; warmup is excluded. Date: 2026-09-29T12:26:41.641412+04:00. The main series uses the same shared probe.

| System | n | CDP p50 ms | p95 | Min | Max | SD | Ready RSS MiB |
|---|---|---|---|---|---|---|---|
| mimic | 10 | 225.91 | 234.96 | 216.30 | 237.02 | 6.60 | 45.79 |
| chrome | 10 | 318.97 | 361.23 | 283.35 | 373.23 | 28.30 | 379.54 |

## Cold startup (medians, ms)

| System | Workload | n | Process create | CDP ready | Runtime init | Cold total | Shutdown |
|---|---|---|---|---|---|---|---|
| chrome | static | 10 | 6.27 | 274.27 | 267.85 | 483.12 | 121.66 |
| mimic | static | 10 | 5.38 | 224.49 | 220.00 | 496.80 | 65.11 |
| mimic | cpu | 10 | 5.89 | 223.71 | 218.27 | 520.96 | 65.64 |
| chrome | cpu | 10 | 6.27 | 291.28 | 284.54 | 547.30 | 120.60 |
| chrome | dom | 10 | 5.86 | 277.27 | 271.35 | 530.09 | 119.88 |
| mimic | dom | 10 | 5.63 | 226.78 | 220.99 | 628.59 | 68.05 |
| mimic | async | 10 | 5.74 | 224.93 | 219.98 | 592.32 | 69.42 |
| chrome | async | 10 | 5.94 | 282.81 | 277.05 | 542.95 | 131.94 |
| chrome | react | 10 | 5.42 | 256.96 | 251.38 | 485.62 | 116.84 |
| mimic | react | 10 | 4.52 | 221.55 | 216.71 | 530.58 | 65.83 |
| mimic | wasm | 10 | 6.03 | 228.26 | 223.10 | 505.18 | 68.43 |
| chrome | wasm | 10 | 6.22 | 266.46 | 259.62 | 474.24 | 126.76 |

## Warm session startup / teardown (medians)

| System | Workload | n | Create ms | Teardown ms | RSS after teardown MiB |
|---|---|---|---|---|---|
| chrome | static | 20 | 37.51 | 14.40 | 1211.70 |
| mimic | static | 20 | 12.95 | 7.45 | 120.36 |
| mimic | cpu | 20 | 13.38 | 7.49 | 124.47 |
| chrome | cpu | 20 | 39.36 | 14.50 | 1403.35 |
| chrome | dom | 20 | 40.39 | 16.54 | 1415.14 |
| mimic | dom | 20 | 13.66 | 8.47 | 125.09 |
| mimic | async | 20 | 13.93 | 6.55 | 121.17 |
| chrome | async | 20 | 39.86 | 14.85 | 1251.74 |
| chrome | react | 20 | 36.70 | 13.99 | 1412.52 |
| mimic | react | 20 | 13.61 | 7.18 | 123.29 |
| mimic | wasm | 20 | 12.93 | 8.16 | 124.21 |
| chrome | wasm | 20 | 36.46 | 14.33 | 1291.15 |

## Single-session workload latency (ms)

| System | Workload | Mode | n | Nav p50 | Execution p50 | Completion p50 | p95 | Min | Max | SD | CV |
|---|---|---|---|---|---|---|---|---|---|---|---|
| chrome | static | cold | 10 | 25.05 | 2.62 | 27.90 | 126.64 | 24.64 | 196.66 | 53.10 | 1.15 |
| mimic | static | cold | 10 | 178.34 | 3.81 | 182.41 | 187.97 | 177.40 | 190.43 | 3.50 | 0.02 |
| chrome | static | warm | 20 | 23.03 | 3.99 | 27.18 | 31.61 | 19.85 | 32.55 | 4.29 | 0.16 |
| mimic | static | warm | 20 | 30.97 | 3.87 | 34.95 | 40.47 | 32.20 | 45.79 | 3.35 | 0.09 |
| mimic | cpu | cold | 10 | 174.45 | 33.72 | 209.05 | 216.69 | 203.93 | 217.26 | 4.10 | 0.02 |
| chrome | cpu | cold | 10 | 28.77 | 44.27 | 72.83 | 129.13 | 65.97 | 165.35 | 29.61 | 0.36 |
| mimic | cpu | warm | 20 | 30.62 | 34.29 | 65.62 | 70.73 | 61.25 | 71.38 | 2.92 | 0.04 |
| chrome | cpu | warm | 20 | 23.47 | 30.16 | 53.78 | 57.73 | 48.55 | 57.85 | 3.06 | 0.06 |
| chrome | dom | cold | 10 | 26.46 | 16.83 | 52.58 | 537.09 | 36.01 | 912.17 | 272.26 | 1.97 |
| mimic | dom | cold | 10 | 177.61 | 135.01 | 311.05 | 328.13 | 308.56 | 330.91 | 7.39 | 0.02 |
| chrome | dom | warm | 20 | 23.95 | 30.88 | 54.17 | 66.18 | 46.12 | 71.78 | 6.23 | 0.11 |
| mimic | dom | warm | 20 | 30.71 | 136.58 | 168.20 | 182.78 | 153.59 | 230.74 | 15.67 | 0.09 |
| mimic | async | cold | 10 | 186.35 | 83.97 | 268.91 | 284.45 | 257.18 | 286.67 | 9.88 | 0.04 |
| chrome | async | cold | 10 | 25.08 | 31.22 | 56.00 | 72.68 | 49.41 | 73.36 | 9.05 | 0.15 |
| mimic | async | warm | 20 | 30.63 | 85.39 | 115.15 | 123.49 | 105.17 | 126.03 | 5.76 | 0.05 |
| chrome | async | warm | 20 | 24.42 | 29.95 | 54.52 | 56.79 | 46.06 | 62.51 | 3.76 | 0.07 |
| chrome | react | cold | 10 | 25.42 | 17.64 | 48.01 | 72.31 | 37.91 | 80.89 | 13.45 | 0.27 |
| mimic | react | cold | 10 | 179.62 | 42.40 | 222.19 | 227.82 | 220.13 | 229.01 | 3.09 | 0.01 |
| chrome | react | warm | 20 | 24.07 | 23.54 | 47.57 | 57.80 | 37.98 | 58.49 | 5.03 | 0.11 |
| mimic | react | warm | 20 | 39.74 | 47.24 | 87.62 | 99.79 | 77.63 | 105.59 | 6.85 | 0.08 |
| mimic | wasm | cold | 10 | 179.43 | 5.09 | 184.62 | 188.31 | 179.40 | 188.99 | 2.79 | 0.02 |
| chrome | wasm | cold | 10 | 23.52 | 4.77 | 28.55 | 90.05 | 25.11 | 125.93 | 30.95 | 0.78 |
| mimic | wasm | warm | 20 | 30.30 | 5.41 | 35.86 | 43.48 | 31.92 | 46.03 | 3.71 | 0.10 |
| chrome | wasm | warm | 20 | 23.72 | 4.84 | 29.10 | 31.26 | 20.14 | 32.55 | 4.02 | 0.14 |

## Single-session memory / CPU (medians)

| System | Workload | Mode | Before page MiB | After create MiB | Peak RSS MiB | Peak private MiB | CPU/session ms | CPU/workload ms |
|---|---|---|---|---|---|---|---|---|
| chrome | static | cold | 374.99 | 426.17 | 485.34 | 255.61 | 335.94 | 125.00 |
| mimic | static | cold | 45.68 | 45.79 | 122.05 | 153.95 | 242.19 | 242.19 |
| chrome | static | warm | 1152.09 | 1200.27 | 1211.70 | 599.05 | 195.31 | 62.50 |
| mimic | static | warm | 120.36 | 120.36 | 138.11 | 169.38 | 39.06 | 31.25 |
| mimic | cpu | cold | 45.58 | 45.67 | 136.90 | 166.85 | 273.44 | 257.81 |
| chrome | cpu | cold | 373.22 | 429.94 | 530.11 | 285.96 | 531.25 | 335.94 |
| mimic | cpu | warm | 124.45 | 124.45 | 152.13 | 181.19 | 78.12 | 62.50 |
| chrome | cpu | warm | 1324.34 | 1372.79 | 1403.35 | 778.80 | 257.81 | 132.81 |
| chrome | dom | cold | 382.94 | 437.87 | 549.55 | 298.65 | 554.69 | 242.19 |
| mimic | dom | cold | 45.56 | 45.69 | 147.77 | 176.27 | 382.81 | 367.19 |
| chrome | dom | warm | 1330.34 | 1378.48 | 1415.14 | 776.44 | 234.38 | 109.38 |
| mimic | dom | warm | 124.56 | 124.56 | 161.94 | 190.90 | 218.75 | 210.94 |
| mimic | async | cold | 45.64 | 45.71 | 134.87 | 169.09 | 382.81 | 343.75 |
| chrome | async | cold | 383.93 | 444.37 | 529.78 | 287.96 | 437.50 | 218.75 |
| mimic | async | warm | 121.04 | 121.04 | 151.11 | 184.15 | 125.00 | 117.19 |
| chrome | async | warm | 1187.90 | 1232.73 | 1251.86 | 638.58 | 250.00 | 125.00 |
| chrome | react | cold | 381.73 | 433.62 | 530.19 | 284.42 | 351.56 | 156.25 |
| mimic | react | cold | 45.65 | 45.79 | 131.59 | 160.06 | 312.50 | 312.50 |
| chrome | react | warm | 1329.65 | 1378.69 | 1412.52 | 803.89 | 265.62 | 125.00 |
| mimic | react | warm | 123.43 | 123.43 | 146.23 | 176.15 | 117.19 | 109.38 |
| mimic | wasm | cold | 45.62 | 45.75 | 126.92 | 155.75 | 242.19 | 234.38 |
| chrome | wasm | cold | 380.92 | 433.38 | 498.69 | 260.42 | 312.50 | 132.81 |
| mimic | wasm | warm | 124.03 | 124.03 | 142.42 | 170.17 | 46.88 | 31.25 |
| chrome | wasm | warm | 1222.40 | 1271.88 | 1291.15 | 630.81 | 187.50 | 62.50 |

## Concurrency / density

Each level uses a separate process, one excluded warmup, and max(5, ceil(20/N)) measured waves. Pages are created concurrently and all begin navigation after a barrier. Completed pages are held until the wave ends to measure simultaneous RSS. Throughput = successful sessions / time from create to final teardown, including the barrier and measurements but excluding HTTP-server setup/cleanup. Latency = create→completion, including barrier wait. This is batch throughput, not an optimized continuous request stream. Holding pages adds to throughput time but not latency. CPU per session = total wave CPU / successes; overlapping per-page CPU intervals are not summed.

Stopping limits: any error, <15% or <2 GiB available RAM, >1024 pages input/s for 3 s, or a 180 s timeout. These protect the workstation; the highest passing level is a lower bound on capacity supported here, not proof of an absolute maximum.

Mimic serializes CDP commands with a shared mutex. The measurement includes this behavior; its cost was not profiled. In stopped rows, RSS may have been sampled before all pages completed, and 0 waves means stopping during the excluded warmup. Such rows are diagnostic and excluded from stable-level fits/charts. Between waves, an additional 250 ms recovery, server cleanup, and checkpoint writing are outside batch throughput.

| System | Workload | N | Waves | Success % | RSS MiB | RSS/N MiB | Peak MiB | CPU/session ms | Sessions/s | p50 ms | p95 ms | p99 ms | Stop |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| chrome | static | 1 | 20 | 100.00 | 1210.81 | 1210.81 | 1216.25 | 252.34 | 8.62 | 65.29 | 72.32 | — |  |
| mimic | static | 1 | 20 | 100.00 | 138.43 | 138.43 | 139.27 | 40.62 | 14.46 | 46.51 | 55.34 | — |  |
| chrome | static | 5 | 5 | 100.00 | 1373.74 | 274.75 | 1423.04 | 123.12 | 25.58 | 145.08 | 157.55 | — |  |
| mimic | static | 5 | 5 | 100.00 | 175.31 | 35.06 | 185.30 | 51.88 | 80.58 | 39.54 | 41.05 | — |  |
| chrome | static | 10 | 5 | 100.00 | 1682.53 | 168.25 | 1692.98 | 113.44 | 27.86 | 270.37 | 298.78 | — |  |
| mimic | static | 10 | 5 | 100.00 | 247.82 | 24.78 | 252.90 | 65.31 | 81.60 | 61.96 | 74.88 | — |  |
| chrome | static | 25 | 5 | 100.00 | 2571.97 | 102.88 | 2583.28 | 116.75 | 25.57 | 820.26 | 919.82 | 921.04 |  |
| mimic | static | 25 | 5 | 100.00 | 435.82 | 17.43 | 454.43 | 67.25 | 125.64 | 106.27 | 121.52 | 125.50 |  |
| chrome | static | 50 | 5 | 100.00 | 4102.04 | 82.04 | 4125.46 | 149.94 | 18.09 | 2409.10 | 2514.27 | 2521.35 |  |
| mimic | static | 50 | 5 | 100.00 | 748.11 | 14.96 | 757.17 | 87.38 | 108.67 | 272.14 | 361.23 | 376.30 |  |
| chrome | static | 100 | 0 | 0.00 | 5344.43 | 53.44 | 5462.97 | — | 0.00 | 2720.14 | 2746.82 | 2760.82 | memory pressure (<15% or 2 GiB available) |
| mimic | static | 100 | 0 | 100.00 | 845.35 | 8.45 | 1020.75 | 100.16 | 95.83 | 710.54 | 920.94 | 959.23 | memory pressure (<15% or 2 GiB available) |
| chrome | cpu | 1 | 0 | 0.00 | 392.61 | 392.61 | 475.13 | — | 0.00 | 31.51 | — | — | memory pressure (<15% or 2 GiB available) |
| mimic | cpu | 1 | 0 | 100.00 | 80.24 | 80.24 | 124.79 | 312.50 | 4.26 | 227.50 | — | — | memory pressure (<15% or 2 GiB available) |
| chrome | react | 1 | 0 | 0.00 | 380.30 | 380.30 | 451.50 | — | 0.00 | 30.17 | — | — | memory pressure (<15% or 2 GiB available) |
| mimic | react | 1 | 0 | 100.00 | 78.39 | 78.39 | 132.93 | 250.00 | 3.77 | 256.58 | — | — | memory pressure (<15% or 2 GiB available) |

## Marginal RAM/session

The finite difference between adjacent tested N values is measured and divided by ΔN; this is not a direct measurement of every N→N+1 step. The linear model is a descriptive OLS fit to medians of successful levels only. The intercept is an extrapolation; actual startup overhead includes the initial blank page. Total RSS includes processes and caches retained from earlier waves, and the number of waves depends on N. The fit therefore combines active-page costs with process history. RSS growth relative to the start of the same wave is also shown; it can include background activity and GC.

| System | Workload | N | (Active RSS − before wave RSS)/N MiB |
|---|---|---|---|
| chrome | static | 1 | 59.17 |
| mimic | static | 1 | 17.82 |
| chrome | static | 5 | 58.45 |
| mimic | static | 5 | 12.95 |
| chrome | static | 10 | 55.44 |
| mimic | static | 10 | 11.99 |
| chrome | static | 25 | 57.47 |
| mimic | static | 25 | 11.84 |
| chrome | static | 50 | 58.20 |
| mimic | static | 50 | 11.83 |
| chrome | static | 100 | 48.90 |
| mimic | static | 100 | 7.99 |
| chrome | cpu | 1 | 1.65 |
| mimic | cpu | 1 | 34.98 |
| chrome | react | 1 | -5.68 |
| mimic | react | 1 | 33.18 |

| System | Workload | N low→high | ΔRSS MiB | ΔRSS/ΔN MiB |
|---|---|---|---|---|
| chrome | static | 1→5 | 162.93 | 40.73 |
| chrome | static | 5→10 | 308.79 | 61.76 |
| chrome | static | 10→25 | 889.44 | 59.30 |
| chrome | static | 25→50 | 1530.07 | 61.20 |
| mimic | static | 1→5 | 36.88 | 9.22 |
| mimic | static | 5→10 | 72.50 | 14.50 |
| mimic | static | 10→25 | 188.01 | 12.53 |
| mimic | static | 25→50 | 312.29 | 12.49 |

| System | Workload | Fixed fit MiB | Marginal fit MiB | R² | Max stable N |
|---|---|---|---|---|---|
| chrome | static | 1102.04 | 59.68 | 1.00 | 50 |
| mimic | static | 120.56 | 12.56 | 1.00 | 50 |

## Teardown / recovery

| System | Workload | N | Ready RSS MiB | RSS after waves MiB | Whole-series CPU s | CPU % |
|---|---|---|---|---|---|---|
| chrome | static | 1 | 383.01 | 1152.31 | 5.05 | 217.46 |
| mimic | static | 1 | 45.90 | 120.74 | 0.81 | 58.74 |
| chrome | static | 5 | 385.69 | 1087.43 | 3.08 | 314.97 |
| mimic | static | 5 | 45.91 | 111.27 | 1.30 | 418.02 |
| chrome | static | 10 | 367.91 | 1138.27 | 5.67 | 316.07 |
| mimic | static | 10 | 45.20 | 126.61 | 3.27 | 532.96 |
| chrome | static | 25 | 370.50 | 1148.70 | 14.59 | 298.57 |
| mimic | static | 25 | 45.29 | 139.17 | 8.41 | 844.93 |
| chrome | static | 50 | 380.77 | 1209.67 | 37.48 | 271.20 |
| mimic | static | 50 | 46.05 | 158.98 | 21.84 | 949.54 |
| chrome | static | 100 | 369.16 | 1052.40 | 12.16 | 353.33 |
| mimic | static | 100 | 45.88 | 152.32 | 10.02 | 959.76 |
| chrome | cpu | 1 | 388.47 | 515.27 | 0.22 | 460.74 |
| mimic | cpu | 1 | 45.27 | 109.63 | 0.31 | 132.98 |
| chrome | react | 1 | 381.60 | 515.18 | 0.25 | 522.28 |
| mimic | react | 1 | 45.20 | 110.15 | 0.25 | 94.29 |

## Local server (measured independently)

HTTP handler time runs from the handler receiving a request to completion of response writing; it excludes TCP/server scheduler queues and network roundtrip. Before the main workload, the server is created outside the measured interval. Requests and bytes for each resource are recorded in raw.json.

| System | Workload | Mode | Requests | Server p50 ms | Server p95 ms | Server max ms |
|---|---|---|---|---|---|---|
| chrome | static | cold | 20 | 0.31 | 0.49 | 0.57 |
| mimic | static | cold | 20 | 0.42 | 1.89 | 2.23 |
| chrome | static | warm | 40 | 0.33 | 0.46 | 1.22 |
| mimic | static | warm | 40 | 0.27 | 0.77 | 1.09 |
| mimic | cpu | cold | 20 | 0.30 | 0.61 | 0.93 |
| chrome | cpu | cold | 20 | 0.33 | 0.50 | 0.50 |
| mimic | cpu | warm | 40 | 0.30 | 0.59 | 0.93 |
| chrome | cpu | warm | 40 | 0.33 | 0.46 | 0.54 |
| chrome | dom | cold | 20 | 0.32 | 0.51 | 0.59 |
| mimic | dom | cold | 20 | 0.27 | 0.66 | 1.44 |
| chrome | dom | warm | 40 | 0.35 | 0.56 | 1.15 |
| mimic | dom | warm | 40 | 0.26 | 0.56 | 1.21 |
| mimic | async | cold | 50 | 0.18 | 1.05 | 2.12 |
| chrome | async | cold | 50 | 0.18 | 0.55 | 1.20 |
| mimic | async | warm | 100 | 0.15 | 0.41 | 0.57 |
| chrome | async | warm | 100 | 0.16 | 0.43 | 0.74 |
| chrome | react | cold | 50 | 0.46 | 1.11 | 1.58 |
| mimic | react | cold | 50 | 0.38 | 1.57 | 2.29 |
| chrome | react | warm | 100 | 0.35 | 0.92 | 1.14 |
| mimic | react | warm | 100 | 0.34 | 1.19 | 1.88 |
| mimic | wasm | cold | 20 | 0.23 | 0.43 | 0.49 |
| chrome | wasm | cold | 20 | 0.33 | 0.51 | 0.54 |
| mimic | wasm | warm | 40 | 0.22 | 0.61 | 1.39 |
| chrome | wasm | warm | 40 | 0.31 | 0.52 | 1.21 |

## Validity of measured series

| System | Workload | Mode | Successes / attempts | Comparison use |
|---|---|---|---|---|
| chrome | static | cold | 10/10 | VALID |
| mimic | static | cold | 10/10 | VALID |
| chrome | static | warm | 20/20 | VALID |
| mimic | static | warm | 20/20 | VALID |
| mimic | cpu | cold | 10/10 | VALID |
| chrome | cpu | cold | 10/10 | VALID |
| mimic | cpu | warm | 20/20 | VALID |
| chrome | cpu | warm | 20/20 | VALID |
| chrome | dom | cold | 10/10 | VALID |
| mimic | dom | cold | 10/10 | VALID |
| chrome | dom | warm | 20/20 | VALID |
| mimic | dom | warm | 20/20 | VALID |
| mimic | async | cold | 10/10 | VALID |
| chrome | async | cold | 10/10 | VALID |
| mimic | async | warm | 20/20 | VALID |
| chrome | async | warm | 20/20 | VALID |
| chrome | react | cold | 10/10 | VALID |
| mimic | react | cold | 10/10 | VALID |
| chrome | react | warm | 20/20 | VALID |
| mimic | react | warm | 20/20 | VALID |
| mimic | wasm | cold | 10/10 | VALID |
| chrome | wasm | cold | 10/10 | VALID |
| mimic | wasm | warm | 20/20 | VALID |
| chrome | wasm | warm | 20/20 | VALID |

![Total working set (MiB)](total-rss.png)

![Marginal working set (MiB/session)](marginal-rss.png)

![Successful sessions / second](throughput.png)

![Session latency (ms)](latency.png)

![CPU (% of one logical core)](cpu.png)

## Engineering conclusions

1. By median warm navigation→completion, Mimic is faster on: none of the measured workloads. CDP readiness (shared probe, separate cold runs): mimic 225.91 ms; chrome 318.97 ms

2. Chrome is faster by the same metric on: async (115.15 / 54.52 ms Mimic/Chrome); cpu (65.62 / 53.78 ms Mimic/Chrome); dom (168.20 / 54.17 ms Mimic/Chrome); react (87.62 / 47.57 ms Mimic/Chrome); static (34.95 / 27.18 ms Mimic/Chrome); wasm (35.86 / 29.10 ms Mimic/Chrome).

3. Fixed process overhead (CDP ready, including the initial page): mimic 45.61 MiB; chrome 379.82 MiB. Startup latency and the OLS intercept are reported separately above.

4. Estimated marginal RAM/session: chrome/static 59.68 MiB; mimic/static 12.56 MiB.

5. Maximum stable N by workload: mimic/cpu 0; chrome/cpu 0; mimic/react 0; chrome/react 0; mimic/static 50; chrome/static 50.

6. Throughput at the highest common stable level:

static, N=50: Mimic 108.67 and Chrome 18.09 successful sessions/s; RSS 748.11 and 4102.04 MiB; CPU/session 87.38 and 149.94 ms.

7. Invalid comparisons in this run: none at the correctness gate; individual iteration errors remain in raw.json.

8. Limitations: one busy workstation; headless Chrome; different V8 builds; HTTP cache disabled; limited corpus sizes and semantic checks; a synthetic React fixture, not a Next.js/production application. Polling and sampling add system load. RSS sums working sets, not unique physical RAM; no financial model of session cost is provided. Paging is a system-wide counter, not attribution of hard faults to a specific process. Mimic virtual-clock values are not used for comparisons.

9. Supported scope: “On Windows x64, local controlled workloads passed result validation in Mimic V8 and Chrome 152.0.7977.82; a reproducible harness, raw observations, and separate latency, CPU, and memory metrics are published. static, N=50: Mimic 108.67 and Chrome 18.09 successful sessions/s; RSS 748.11 and 4102.04 MiB; CPU/session 87.38 and 149.94 ms.”

10. The data do NOT support claims that “Mimic is X times faster than Chrome in general,” full browser compatibility, tenant-isolation security, an advantage in pure V8/JIT execution, gains on arbitrary sites, or monetary savings without an operating model.
