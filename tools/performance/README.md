# Fast performance iteration gate

Run with the frozen benchmark Python environment:

```
python tools/performance/fast_gate.py --output .build/fast-gate-UNIQUE
```

`--source` selects a source checkout for a before/after comparison. Each invocation builds its own executable into a new output directory. `build.json` records the build command, source revision/status, executable SHA-256 and frozen harness/workload hashes. Every process launch verifies and records the executable hash before invoking the frozen Runtime with that exact absolute path. A mismatch aborts immediately. Existing output directories are refused to preserve evidence.

Use `--concurrency 10` to limit the static concurrency waves to ten Pages, or
provide several counts such as `--concurrency 10 15`. The semantic checks and
ten-Page memory samples remain mandatory; the default counts are still 10 and 25.

The gate imports `benchmark/run.py`; it does not edit the frozen runner or workload fixtures. All six workload result checks are mandatory. Timings cover five measured warm iterations of DOM, static and React, then three measured static waves at N=10 and N=25. Each timing series also has one excluded warm-up, following the frozen suite. Memory is sampled with ten live static pages and ten live React pages in separate fresh processes, before teardown, immediately after teardown and after the same 250 ms recovery interval. No forced garbage collection is added. Concurrency retains the frozen memory-pressure and paging limits. Raw rows, failures and warm-ups are retained.

Run repository correctness regressions for each production change; this gate supplements those tests. Use the full frozen matrix only at a substantial improvement milestone. Fast gate timings cannot replace the original full-suite baseline or certify unmeasured N=50 targets.

Check executable mismatch handling with:

```
python -m unittest discover -s tools/performance -p test_fast_gate.py
```


At a substantial improvement milestone, use the full wrapper:

```
python tools/performance/full_gate.py --output benchmark/runs/UNIQUE-MILESTONE --chrome PATH-TO-PINNED-CHROME
```

It runs the unchanged full matrix. Its Runtime subclass verifies the just-built Mimic executable before every launch and also verifies the pinned Chrome executable against its initial hash; `launches.jsonl` records each check. Both wrappers reject a harness fingerprint that differs from the original baseline. The original benchmark files remain untouched.

After measurement, the full wrapper automatically generates `presentation/benchmark-startup.svg`
and `presentation/benchmark-scaling.svg`, plus `presentation/provenance.json` with
source, generator and chart hashes. The charts use the completed raw run and its
hash-verified summary, with no hand-entered results. An incomplete/smoke run or
failed static scaling series is rejected before rendering. Axes and footnotes
are checked for overlap at full and README-scale sizes.

To regenerate charts from a saved full run without rerunning browsers:

```
python tools/performance/readme_charts.py benchmark/runs/RUN/raw.json .build/RUN-charts --preview .build/RUN-chart-preview
```

The published `public-results.json` format is also accepted. Rendering creates
assets; updating a repository's README, numeric summaries and Git publication
remains a release step so that dates, claims and images stay on one checkpoint.
The original frozen `benchmark/run.py` and `run.ps1` remain unchanged; use
`full_gate.py` for the automatic presentation output.

`python tools/performance/publish_checkpoint.py benchmark/runs/RUN` exports the
full checkpoint's numerical data and a readable summary. It verifies raw and
summary hashes, retains failed/excluded samples, and withholds comparative
latency for incomplete or failed series. It does not modify the raw record,
frozen generator, original baseline or publish to an external repository.

## Matched Linux comparison

Run all engines, the fixture server and controller inside the same Linux
environment. `linux_compare.py` imports the unchanged fixture server, expected
results and harness fingerprint. It uses the same flattened CDP client for
Mimic, pinned Chrome 152 and Lightpanda. This is an additional adapter; its
timings and Linux USS/PSS must not be pooled with frozen Windows observations.

```
python tools/performance/linux_compare.py --output benchmark/runs/LINUX-RUN --engine control=/absolute/control --engine candidate=/absolute/candidate --engine chrome=/absolute/chrome --engine lightpanda=/absolute/lightpanda --rounds 4 --iterations 5
python tools/performance/linux_capacity.py --output benchmark/runs/LINUX-CAPACITY --engine control=/absolute/control --engine candidate=/absolute/candidate --engine chrome=/absolute/chrome --engine lightpanda=/absolute/lightpanda --rounds 2 --waves 3
```

Build control and candidate first, then stop other heavy jobs. The adapters
alternate engine order across rounds, check binary hashes before launch and
retain excluded warmups, semantic failures, resource requests, source/version
receipts, CPU and memory observations. Lightpanda loads all supported resource
types with CORS enabled; a missing or failed workload is never counted as fast
success. A single-page summary requires every planned sample and warmup to pass.
No forced collection is used. Capacity measures 1/5/10/25/50/100 retained Pages,
including creation and teardown in throughput and a separate 250 ms recovery
sample. Failures, memory pressure or sustained paging stop a series. Linux
process CPU includes sampled descendants; short-lived exited children can be
missed. Read the raw policy and failure records before making comparisons.

## CDP operation latency diagnostics

`python tools/performance/frame_bridge_probe.py --binary .build/mimic.exe --output .build/frame-bridge-UNIQUE`
measures warmed function calls, reads/writes, `in`, prototype, descriptor and
own-key reflection across a same-origin iframe. Local calls and a loop entirely
inside the iframe are controls. Every sample checks its result; outputs retain
raw durations plus executable/probe hashes. Use the same `--iterations` and
`--samples` for before/after binaries, and `--chrome --binary PATH` for frozen
Chrome. Run without concurrent builds/tests. Sub-resolution Chrome durations
must not be reported as zero execution cost. This is a local diagnostic and
does not replace the frozen correctness, concurrency or memory gates.
The probe requires `aiohttp` and `psutil` in the selected Python environment.

`python tools/performance/live_latency.py --before .build/before.exe --after .build/after.exe --output .build/live-latency-UNIQUE`
records five alternating clean-process trials of ChatGPT login, GitHub,
SpigotMC, Modrinth, React and Wikipedia. It retains binary/source hashes,
per-command client timing, resource/lifecycle events and validated DOM results.
Use `--mode both --preview both --sites chatgpt` to separate retained Contexts
and an actual preview subscriber. Startup discovery uses a bounded connection
probe; totals include process startup, unlike a script attaching to an already
running browser.

`--sites geometry` runs one warm-up and 30 local read/mutation/real-click
iterations on a 200-element DOM; reads sample four separated elements.
`--sites geometry-stress` reads all 200 once, kept separate because the original
runtime takes tens of seconds per operation. Do not pool these two workloads.

For diagnostic runs only, `--profile-cdp` enables `MIMIC_PROFILE_CDP=1` and
saves the Page trace. `commandTiming` records command/session IDs, queue delay,
session/Page lock wait, remaining handler work, JSON serialization and socket
write/wait durations, including the enclosing events for legacy nested sessions.
Parameters and results are not
retained in those timing events. Normal connections do not collect timings.
`workMs` is remaining wall time, including awaited execution or debugger pauses,
not a CPU measurement; use the CPU/block profiles to attribute it further.
Timeouts terminate a trial instead of leaving repeated evaluations queued.

`python tools/performance/runtime_latency.py --binary .build/mimic.exe --output .build/runtime-new.json`
uses a local generated DOM, validates each operation, and reports cold/warm Page,
selector, live-collection, traversal and mutation timings with recovery RSS.
It requires `pyppeteer` and `psutil`, records the executable hash, and does not
replace the frozen gate. Use a fresh output path for every comparison.

For an opt-in live native/Go profile, set `MIMIC_LATENCY_PROFILE_URL`,
`MIMIC_LATENCY_PROFILE_DIR`, `MIMIC_PROFILE_HOSTS=1`, and optionally
`MIMIC_LATENCY_PROFILE_EXPRESSION`, then run
`go test ./internal/browser -run '^TestRuntimeLatencyProfile$' -v -count=1`.
`MIMIC_LATENCY_PROFILE_KIND` selects `native`, `go-cpu`, `go-memory`,
`go-block`, or `go-mutex` for separate attribution runs; unset or `all` retains
the combined diagnostic. Only the selected profiler is started/exported
(`go-memory` writes sampled `allocs` and `heap`). Explicit Go modes suppress an
inherited navigation-native profiler. Host tracing remains independently
controlled by `MIMIC_PROFILE_HOSTS`. Every mode retains pump/selector work,
`hosts.json`, `diagnostics.json`, and records its mode in `profile-metadata.json`.
Use a fresh output directory per run so stale profiles cannot mix modes.
Use an absolute `MIMIC_LATENCY_PROFILE_DIR`: `go test` runs the test from the
package directory. Keep attribution runs separate from unprofiled latency runs.

`MIMIC_PROFILE_TEXT_CACHE=1` independently enables bounded cache-hit/miss,
eviction and successful-working-set counters. The latency profile test exports
them to `text-cache.json`. It records seeded key fingerprints rather than author
text; after the diagnostic key limit the distinct working-set estimate is marked
as a lower bound. This is disabled by default and is not a timing baseline.

`MIMIC_V8_CPU_PROFILE=1` plus `MIMIC_V8_CPU_PROFILE_FILTER` selects a script-name
substring for an additional native profile during navigation. Profiling changes
execution cost; use unprofiled binaries for latency claims.
The combined `all` mode also writes allocation, heap, mutex, block and goroutine
profiles without forcing GC. Set `MIMIC_LATENCY_PROFILE_PUMP_SECONDS=20` to
observe network continuations and subsequent tasks for a wall-clock window;
otherwise the diagnostic retains its original twenty-turn sample.

`MIMIC_LATENCY_PROFILE_ACTIONS` optionally names a JSON action array for the
same profiled Page, after the initial pump and expression probes. An action can
contain `expression` (state inspection), `method` and `params` (production
`Input.dispatchMouseEvent` / `Input.dispatchKeyEvent`), and `waitMs` (pump Page
tasks after the action). Keep coordinate preflight checks and state assertions
in the expressions. Input replay is diagnostic, not a replacement for a real
CDP client latency baseline. The enclosing 90-second deadline bounds pumping.


### Additional concurrency scaling

`concurrency_scaling.py` reuses the frozen runner and checks every Page result.
Pass fresh `--before`, `--after`, the pinned `--chrome` executable, and a new
`--output` directory. Defaults cover 10/25/50/100 static Pages and 50 React Pages.
Use `--cases static:100 --order before after` for a bounded repeated comparison.
The output records binary hashes, per-Page latency, wave throughput and RSS
before/while targets are retained/250 ms after teardown, without forced GC.
