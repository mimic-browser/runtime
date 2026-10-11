# Sequential Runtime.evaluate diagnostics

`evaluate_series.py` launches an identified executable directly and saves the
complete CDP exchange, individual round-trip durations, launch arguments,
executable hash, source identity, environment observations, and artifact hashes.
Chrome uses a fresh headful profile and a fixed debugging port. Follow
`docs/oracle-policy.md`; do not replace this launch with an automation launcher.
Install Python's `websocket-client` dependency before running.

Build a fresh Mimic executable, then run the same probe against both binaries:

```powershell
go build -o .build/evaluate-series.exe ./cmd/mimic
python tools/performance/evaluate_series.py --binary .build/evaluate-series.exe --kind mimic --port 19368 --output .build/evaluate-series/mimic
python tools/performance/evaluate_series.py --binary compatibility/.chrome-for-testing/152.0.7977.82/chrome-win64/chrome.exe --kind chrome --port 19369 --output .build/evaluate-series/chrome
```

Each series contains 64 sequential evaluations. Cases cover small objects,
bounded object traversal returning JSON, 4096-element objects, by-value and
handle results, both awaitPromise settings, and a single evaluation returning
64 equivalent results. Handles are released after each series. The probe also
tests first evaluation, a new browser context/target, and recovery after local
receive deadlines for both pending Promises and synchronous execution.
Round trips include client JSON encoding/decoding and event handling. Large
payload cases therefore require attribution before claiming server overhead.
Saving captures occurs outside the timed interval but changes inter-command
spacing; use matched probe versions and retain host interference limitations.

`--local-navigation` adds a local HTTP document with a delayed external script,
Fetch interception and request continuation. It waits for terminal fixture
execution after the measured series. `--navigation-url` can instead select an
existing controlled fixture. These are network-intercepted experiments.

`--trace --small-only` enables Mimic diagnostics without large-payload capture
cost. This is a separate instrumented control, not passive latency evidence.
The trace has command IDs and session IDs for wire timings and correlated
`evaluate:<session>:<command>` spans for execution:

| Observation | Boundary |
| --- | --- |
| commandTiming.queueMs | Admission through ordered command dispatch |
| receivedAt / queuedAt / startedAt | Complete wire-message receipt / command admission / ordered worker entry |
| executionStartedAt / executionEndedAt | Handler dispatch after ownership acquisition / handler return after ownership unwinds |
| replyQueuedAt / writeStartedAt / sentAt | Reply admission to outbound FIFO / writer start / successful socket completion |
| sessionWaitMs / pageWaitMs | Session mutex / Page command mutex |
| evaluatePhase.state | Realm lookup, initial runtime admission and debugger bridge creation |
| runtimeInitialization / stateValue | Lazy runtime materialization / bridge prefix value creation |
| shadowSnapshots | Isolated-world shadow synchronization |
| scheduler.inlineWait | Scheduler execution mutex |
| runtime.Eval.entry | Engine dispatch through owner entry and scope setup |
| runtime.Eval.execution | Compilation, execution and result persistence inside V8 owner |
| scheduler.inlineCheckpoint | Microtask checkpoint and associated resource synchronization |
| evaluatePhase.remoteObject | RemoteObject description/by-value serialization and Go JSON decoding |
| serializeMs | CDP response envelope JSON encoding |
| writeWaitMs / writeMs | Outbound FIFO and writer wait / WebSocket write |

`inline`, `execution`, `finish`, and `workMs` include nested phases; do not sum
overlapping categories. Promise waiting belongs to `finish`, outside the
RemoteObject phase. Timings are wall time, not CPU. Chrome's public CDP endpoint
does not expose equivalent private queue/lock phases: compare round trips and
use a separate Chrome tracing experiment when deeper attribution is needed.
`totalMs` ends at the command's own socket completion, even if its worker joins
later packets for lifecycle cleanup. Early control commands can omit the Page
dispatch phase; zero timestamps indicate a boundary that was not reached.

For a loopback-only busy-task/body/input comparison, install `websockets` and run:

```powershell
python tools/performance/cdp_queue_latency.py --binary .build/evaluate-series.exe --output .build/cdp-queue/mimic.json
python tools/performance/cdp_queue_latency.py --binary compatibility/.chrome-for-testing/152.0.7977.82/chrome-win64/chrome.exe --output .build/cdp-queue/chrome.json
```

The probe directly launches Chrome headful with a dedicated fresh profile and
records its unmodified webdriver descriptor, window, visibility, focus, binary
hash and launch arguments. It asserts pointer-event/microtask ordering and exact
retained body bytes. `--profile` adds Mimic's command-boundary trace; compare
uninstrumented runs separately. All HTTP workload requests use loopback. The
probe is a scripted diagnostic, not a passive live-site acceptance capture.
Preserve each Chrome result and profile before trying another hypothesis.
`TestBlockedResponseWriterDoesNotOwnSessionOrder` supplies the complementary
deterministic writer-stall regression without depending on TCP buffer sizes.

A client receive timeout sends no cancellation to either browser. An awaited
Promise can yield the Page boundary while remaining pending; synchronous JS
continues until completion, an explicit supported evaluation deadline, or
session disconnection. A timeout must not be interpreted as proof that the
server stopped executing. Focused transport EOF cancellation tests verify that
disconnecting an executing client leaves the Page usable.
