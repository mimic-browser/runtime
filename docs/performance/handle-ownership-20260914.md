# Handle ownership experiment, 2026-09-14

Selected production code and focused regression tests are retained. Temporary
implementation patches and diagnostic executables are discarded after the
decision. [Raw observations and executable hashes](data/optimization-20260914/handles/)
include both the rejected first candidate and accepted second candidate.
The [Worker follow-up](worker-handle-ownership-20260914.md) extends this ownership
model. Incoming cached module-graph errors remain borrowed; only locally created
evaluation errors may be consumed by a dynamic-import completion.

Candidate source: detached worktree `spike-handles-20260914`, based on `f07bd2d643eebbadc28d582ef93f01c832a50106`. Production patch: `handles-v2.patch` (SHA256 `02a2e8e36b1b53b3a714db8e18ab6e30f5ea6735e085ae8e79052d27eb93cfb6`). The patch includes focused correctness tests; the opt-in diagnostic is separate, `lifetime_profile_test.go` (SHA256 `eec1f42c91ad6561b7af9b4905646a635b52914d45110b111f2d32db8b488a7f`).

Linux WSL Ubuntu 24.04. Each process runs one Page, same loopback workload server and native V8 backend. Measurements were serialized with no concurrent builds or tests. The order was control, candidate, candidate, control. Each raw directory contains its binary hash and test log. The diagnostics were built as browser test executables; these are not production CDP harness timings and do not establish any Chrome speed ratio. Tracing stayed enabled. Native `ProfileCollect` and Go GC were explicit diagnostic interventions after each workload; their duration is recorded separately as `reclaim_ms`. No automatic GC policy was changed.

Control binary SHA256: `3b744676c44e28997a9320cbccdd977ea1295798269ffdbc4f01b622f37e50bb`.

Rejected first candidate SHA256: `a7103ee326b5244dde7be13061de6d6cafd338498b8a18263947107f64ca0f0a`. It eliminated Eval roots, but dispatching each release separately slowed scalar evaluations by 15.6% and Promise evaluations by 27.2%. Raw: `control-1`, `candidate-1`, `candidate-2`, `control-2`. This variant must not be promoted.

Selected second candidate SHA256: `6d573ad8a39ed0996068995fe152f61804bce90d3b5f90c1188df90d4c6d6420`. It groups Await, export and releases on the existing owning thread, closes Globals without allocating a local scope, retains only the callback needed by a timer and releases it on completion/cancel. It also uses borrowed arguments for synchronous URL/baseURI, clock, checkpoint and Fetch hosts. Native host Promise values are borrowed for the current callback; only pending resolvers remain rooted until settlement. Ordinary Runtime.NewPromise preserves caller ownership of Value.

Raw second series: `control-v2-1`, `candidate-v2-1`, `candidate-v2-2`, `control-v2-2`. Means of the two executions per variant:

| Scenario | Operations | Control ms | Candidate ms | Native roots retained: control -> candidate |
| --- | ---: | ---: | ---: | ---: |
| Eval returning scalar from an object expression | 256 | 61.97 | 51.44 | 256 -> 0 |
| Eval returning a fulfilled Promise | 256 | 67.52 | 54.75 | 512 -> 0 |
| Actual Fetch/arrayBuffer, 64 KiB each | 16 | 266.23 | 261.30 | 368 -> 0 |
| EventTarget listener registration/dispatch/removal | 1024 | 6.44 | 6.58 | 1028 -> 0 |
| Timers run, capturing 4 KiB each | 1024 | 248.02 | 204.52 | 6152 -> 0 |
| Timers canceled, capturing 4 KiB each | 1024 | 11.15 | 8.48 | 6148 -> 0 |
| Page.Close after the sequence | 1 | 5.50 | 1.86 | isolate disposed |

After the entire sequence and recovery, V8 used heap was 27.07 -> 17.05 MiB, external memory 8.25 -> 0.25 MiB. The remaining 0.25 MiB is also present at the initial baseline. All 8 MiB of timer-captured payloads recovered before Page.Close. Go heap was approximately 19.51 -> 18.52 MiB before close; after close and recovery both variants were approximately 18.07 MiB. These include live BrowserContext/trace and test state and are not process-private RSS claims. The Fetch Go allocation volume remains approximately 78.6 MiB for 16 bodies in this isolated handles patch; the independent binary-transfer candidate removes that separate cost.

Listener wall time differs by +2.1% in this tiny series, below the 5% rejection threshold and too small a sample for a throughput claim. All other sampled operation medians improve. The 2.95x close result concerns this retained-state sequence only, not ordinary cold Page creation or application execution.

Correctness: Windows focused V8 and browser tests passed, including repeated Page evaluations across three create/close cycles; independent retained values; original JS exception identity through host rethrow; reentrant Promise settlement; JS-held Promises/results surviving native cleanup; completed/canceled/self-cleared timer registrations; interval continuation after a handled exception; Fetch binary/clone/abort/origin/redirect behavior; Window/Worker Fetch; and same-realm/cross-realm borrowed-call behavior. A separate Linux focused browser run of the first candidate passed. Linux v2 diagnostic verifies correct results in every scenario; final integrated Linux, race and fast-gate checks remain required before promotion.

Limits: Worker Fetch/GPU Promise ownership is covered, but Worker timer callbacks retain the older lifecycle and require a separate bounded follow-up. Errors returned to a native caller still transfer a live ThrownValue root to that caller; only errors handled by a host rethrow or finished dynamic import are released here. Cross-realm owners, listener data actually retained by JavaScript, debugger object groups, cache/history retention and arbitrary service callbacks are not globally rewritten. This patch proves elimination of temporary roots in the listed long-lived Page scenarios, not universal zero retention for every Web API.
