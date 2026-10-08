# Reproducible benchmark: Mimic V8 and Chrome 152

Latest measured checkpoint: [September 29, 2026 summary](runs/13-rss-20260929/public-summary.md)
and [full measured report](runs/13-rss-20260929/report.md). The headline uses
the completed static 50-Page series; the report shows the status of every
attempted level.
The original `results/` baseline and earlier runs remain historical records.

From the repository root on Windows x64:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File benchmark/run.ps1
```

The command creates a local Python environment, installs pinned dependencies,
builds the current Mimic, checks semantics, runs the measurement series, and generates
`benchmark/results/raw.json`, CSV, `summary.json`, `report.md`, and five PNG charts.
Python 3.14 x64, Go 1.26, CGO/GCC, and the project's pinned dependencies are required.
Initial Python package installation uses the internet; workloads are entirely local.
React 18.3.1 / ReactDOM 18.3.1 are included in `fixtures/vendor` under the MIT license.
Their sources are the npm packages `react@18.3.1` and `react-dom@18.3.1`, files
`umd/react.production.min.js` and `umd/react-dom.production.min.js`.

Chrome discovery checks the project's `compatibility/.chrome-for-testing/152.0.7977.82/`
and the adjacent `mimic-cleanup-private-archive-20260908` archive. To specify another path:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File benchmark/run.ps1 -Chrome 'D:\browsers\chrome-win64\chrome.exe'
```

The CDP version must be exactly `Chrome/152.0.7977.82`; substituting an installed
Chrome of another version is prohibited. Executable SHA-256 hashes, V8, Chromium revision,
Mimic commit, CPU/RAM/OS, power settings, and background processes are recorded in raw.json.
User-specific absolute paths are replaced with `<repo>`, `<workspace-parent>`, and `<user>`.
Launch arguments are defined in `Runtime` and remain identical across each system's iterations.
Chrome instances use fresh profiles, ports, and TEMP directories; extensions are disabled.

For a quick harness check, use `-Smoke`: this writes separate results to
`.build/benchmark-smoke`, which cannot support performance conclusions.
To run only the correctness gate:

```powershell
.build/benchmark-venv/Scripts/python.exe benchmark/run.py --gate-only --output .build/benchmark-gate
```

An interrupted saved run can be resumed with the same command and `-Resume`.
Both binary hashes and fixture hashes must match. Completed series are retained;
an incomplete series restarts in full with a new process. Interrupted observations
are marked and summarized separately in the report rather than deleted. New dates
and machine state on resumption are recorded in `resumptions`. A normal run without
`-Resume` refuses to overwrite a directory containing raw.json: specify a new `-Output`.

## Frozen baseline and future optimizations

`results/baseline.yaml` records the commit, harness commit/hash, cold/warm results,
levels 1/5/10/25/50/100, RSS, private bytes, CPU, latency, and throughput.
`manifest.json` contains SHA-256 hashes of source data and all final artifacts.
The harness hash includes measurement, reporting, comparison, the dependency lock, and fixtures.
It is checked on resumption and at the end of measurement: do not change the harness
during a run. The repository baseline is not overwritten.

Future optimizations are recorded in a separate commit such as `perf: reduce ...`.
Do not simultaneously change workloads, expected results, timings, flags,
stopping thresholds, statistics, or the sampler. Run **the same unchanged harness**:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File benchmark/run.ps1 -Output benchmark/runs/after-optimization
.build/benchmark-venv/Scripts/python.exe benchmark/compare.py benchmark/results benchmark/runs/after-optimization --output benchmark/runs/comparison.json
```

The comparator checks harness/fixture/Chrome hashes, CPU, OS, RAM, power settings,
and repetition policy; it compares only matching successful series and concurrency levels.
Percentages are calculated as `(after - before) / before * 100`, without manually
assigned gains. A negative RSS/CPU/latency change is an improvement; a negative
throughput change is a regression. Background applications and OS memory state can
still differ: high CV or paging requires interpretation, not a claim of universal speedup.

Generate an English report from saved data without browsers. Use a copied run
directory to preserve the original baseline artifacts:

```powershell
.build/benchmark-venv/Scripts/python.exe tools/report_benchmark.py <run-directory>/raw.json
```

Check Job Object counters, accounting for exited child processes, and statistics:

```powershell
.build/benchmark-venv/Scripts/python.exe benchmark/test_harness.py
```

The historical `benchmark/report.py` remains byte-for-byte frozen because it is
part of the measurement harness fingerprint. `tools/report_benchmark.py` provides
the English reporting variant with the same calculations.

Generate the README benchmark image from a completed, integrity-checked
checkpoint. The image follows the website palette; every displayed number is
read from `raw.json` and `summary.json`, whose hashes are validated against the
checkpoint manifest. The default output path stays the same for future
checkpoints, so the README always uses the newly generated image:

```powershell
.build/benchmark-venv/Scripts/python.exe tools/performance/benchmark_story.py `
  benchmark/runs/13-rss-20260929
```

The generator also writes `docs/assets/benchmark-story.receipt.json` with
checkpoint, source, generator and output hashes. Update the README benchmark
text and methodology link when publishing a new measured checkpoint.

## Measurement contract

| Stage | Start → end |
|---|---|
| process_start_ms | Popen → suspended process created |
| http_ready_ms | start of Popen → /json/version available |
| cdp_ready_ms | start of Popen → WebSocket connected and CDP response received |
| runtime_initialization_ms | cdp_ready_ms − process_start_ms; includes harness/protocol |
| session_create_ms | control CDP connection → new page, page CDP connection, HTTP cache disabled |
| navigate_ack_ms | Page.navigate → ACK, diagnostic only |
| navigation_ms | Page.navigate → exact URL + readyState complete + workload function |
| execution_ms | explicit workload start → done detected and result read |
| completion_ms | Page.navigate → done; navigation_ms + execution_ms with a small harness gap |
| teardown_ms | page CDP close → Target.closeTarget and target absent from Target.getTargets |
| total_cold_ms | Popen → runtime and its Job Object exit |

Workload settle = 0: no waiting for paint, visual rendering, or networkidle.
CDP readiness is confirmed by the same `Target.getTargets` call in both systems.
At the end, 10 interleaved cold runs use this probe with a separate warmup.
The original readiness probes differed in the first saved baseline; its report bases
startup conclusions only on the corrected separate series while retaining the original observations.
Each controlled workload has 10 cold and 20 warm measurements, plus an excluded warmup.
All slow iterations are retained. A correctness-gate failure excludes the workload from
timed comparisons for both systems; a timed-iteration failure is retained and precludes
claims of an advantage for that series.

Each page receives a unique local-server port that is not reused within the run.
Servers use only ports 49152–65534: this machine's system ephemeral range starts at
1024 and can allocate ports prohibited by Chrome. URLs and full Page.navigate ACKs
are saved for diagnostics. `localStorage` is also checked for traces of the previous
iteration. Pages do not write cookies, register service workers, or use external sites.
Mimic does not support creating CDP browser contexts: isolation is by page/origin,
not independent security tenants. The process, shared transport, and context persist
in warm runs. HTTP cache is disabled on both sides with `Cache-Control: no-store`;
no warm HTTP-cache scenario is claimed. DNS, OS DLL/file caches, and machine state are not reset.

| Workload | Independent expected value |
|---|---|
| static | baseline text, one root |
| cpu | sum 59614380 and SHA-256 of its decimal string; objects/arrays/Map/JSON/regex/Promise/crypto |
| dom | 3000 elements, 3000 active, specific final text, child-node count, and total text length |
| async | exact Promise/microtask/timer/MessageChannel/Worker/window.postMessage/fetch/XHR results |
| react | React 18.3.1: fetch, 200 cards, three state transitions, effect marker, first/last text |
| wasm | async instantiate and 100000 i32 add calls; sum 704982704 |

The React corpus is a reproducible synthetic client application, not a complete
Next.js site or a claim of compatibility with arbitrary React applications.
Fixtures and expected results are identical for both engines; there are no UA branches.

## Memory, CPU, and concurrency

A Windows Job Object is assigned before the process resumes and is inherited by descendants.
Job CPU accounting retains user/kernel CPU for processes that have already exited.
Working sets and private bytes are summed across the Job Object, including browser,
renderer, GPU, network, utility, and helper processes created by that instance.
The controlling Python process and HTTP servers are excluded from SUT metrics.
Their system load remains a measurement factor. Shared DLL pages can be counted more than once.
This is a sum of working sets, not unique resident RAM or available OS memory.
The sampler period is 50 ms; actual timestamps are saved, and short peaks can be missed.

Static, CPU, and React workloads are tested at 1, 5, 10, 25, 50, and 100 concurrent pages.
Each level uses a fresh process, one excluded warmup wave, and
`max(5, ceil(20/N))` measured waves. Pages are created concurrently, then a barrier
starts navigation. Completed pages are held until the wave ends to measure simultaneous RSS.
CPU is counted once per wave, not by summing overlapping page intervals. Throughput
includes creation, execution, the barrier, and teardown; HTTP-server setup/cleanup
is outside the measured interval. This is batch throughput, not an optimized steady-state pool.

Each warm iteration and density wave has a separate 250 ms recovery period,
identical for both systems and excluded from latency/batch throughput. Memory is
recorded before the next wave, with active pages, immediately after teardown, and after recovery.
Memory retained in engine processes/caches is not subtracted from total RSS.
Additional gaps between waves include server cleanup and checkpoint JSON writes.
They are outside throughput; this is therefore not a continuous production-pool measurement.
When a level stops during warmup, its row is diagnostic: RSS may have been sampled
before all pages completed and is not used in the memory fit. OLS fits and finite
differences reflect this particular lifecycle and wave history, not a universal
cost per new page or a security-isolation cost.

Increasing N stops on an error, available RAM below max(15%, 2 GiB), sustained
paging (`Memory\\Pages Input/sec` >1024 for 3 s), or a 180 s wave timeout.
A failed warmup marks the level unsuccessful and prevents higher N levels from running.
Reaching 100 sessions means “100 tested,” not the engine's limit.

Processes are closed through Browser.close / Ctrl+C in a dedicated hidden Mimic console.
Remaining processes are terminated only through the test-owned Job Object.
The Job also closes descendants if the harness crashes. Unrelated Chrome processes
are neither enumerated for termination nor terminated. Only the test's own TEMP is deleted.

## Runtime changes

Semantic errors found by the original gate were fixed; see
`docs/benchmark-semantics.md` and the separate commit.
`results/pre-fix/raw.json` contains only the initial correctness check, not a performance baseline.
Final numbers correspond to the Mimic hash in the final raw.json metadata.
The runtime contains no branches or optimizations specific to the benchmark corpus.
