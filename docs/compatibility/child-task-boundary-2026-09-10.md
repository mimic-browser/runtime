# Child task boundary investigation, 2026-09-10

Follow-up implementation and validation:
[Page task selection and cross-realm checkpoints](page-task-checkpoints-2026-09-10.md).
The rejected candidate and evidence below are retained as the investigation record.

The new site-independent reproducer finds a real scheduling difference before
completion: Mimic drains a child's ready queue before delivering its messages
to the parent. A scheduler candidate was investigated but **withdrawn after
regression failures**. This batch retains the differential fixture and evidence,
with no production-runtime change. Scheduling alone has not been shown to
explain the protected scenario's failure.

## Evidence and limits

The measured reference is system Chrome 152.0.7977.83,
not frozen 152.0.7977.82. Mimic's running executable contains the production
changes committed in 1851d61, including Resource Timing ownership and JSON
serialization. No timing, API result, continuation or request body was replaced.

Private evidence is under `compatibility/private-captures/`:

* `chrome-extra-postfix-20260910`: fresh disposable Chrome context, message
  listener in pages/frames, normal navigation and revisit.
* `mimic-extra-postfix-20260910`: cookies and HTTP cache cleared, the same
  listener, existing CDP instance. Observed `overrunBegin`,
  `overrunEnd`, then `fail` 600010; no successful document.
* `mimic-overrun-tasks-20260910`: additional run with the runtime trace retained.
  This run reaches the CDP pump's context deadline and is **incomplete**, not
  an ordinary terminal-verdict comparison.
* `overrun-control-20260910`: source provenance, restricted static analysis,
  structural comparison, and local reproducer outputs. Raw scripts and message
  contents remain private.

The message listener adds logging and descriptor enumeration. A passing Chrome
control supports its usefulness, not its transparency. Long string contents
were truncated by that earlier listener: equal string length is **unknown**,
not equality. Objects are limited to 40 descriptors and depth three. Missing
fields must be interpreted with those bounds. Neither complete value-flow
tracking nor an equivalence proof of the executed programs is available.

## Origin of `overrunBegin`

The statically examined Chrome script exactly matches the scriptSource in
`chrome-message-all-frames-v2-20260910` (SHA-256
`6528e9fbd530d17ba274e1aea85c27c18d7b69d431f2ff70e469e834e8a44e77`).
The Mimic script is an exact substring of the child document response in
`mimic-resource-json-normal-20260910` (script SHA-256
`884cb542c04b9fa4b2b1458eeb6061b2486f6b31f95507c39817cf7f50108875`).
These are different attempts/programs. Their entire behavior is not assumed
equivalent. The analysis parses source and resolves literal string lookups;
it does not execute or patch downloaded programs.

Both examined emitters have the same recognizable structure: feature guard,
already-active latch, counter initialization/increment, setting the latch,
feedback UI helper, and parent message. The direct caller is a one-second
interval. It checks completion/state flags and whether `Date.now()` minus
a progress timestamp exceeds a configured threshold, defaulting to 10,000 ms.
The timestamp reset helper assigns `Date.now()`. Thus elapsed-time monitoring
is established from control flow, rather than inferred from the event name.
This does **not** identify error 600010 as a timeout or prove that the monitor
determines the server verdict.

The fresh message pair takes approximately 4.82 seconds from extraParams to
complete in Chrome. Mimic takes 10.25 seconds to the *received* overrunBegin
message and 28.22 seconds to fail. Chrome continues exchanging messages during
execution; Mimic's first such pair is followed by a long delivery gap.
These receive timestamps alone cannot locate the time the sender emitted an
event.

## Task trace narrows that ambiguity

In the additional Mimic trace:

| Sequence | Task / observation | Wall-clock evidence |
| --- | --- | --- |
| 666–668 | Parent queue task 46 delivers extraParams into child context 6 | 11:19:58.991 UTC |
| 933–1895 | Child DOM task 21 | 11,026.75 ms |
| 1900–1906 | Child timer task 2 posts a 75-byte message, queuing parent task 54 | 11:20:10.693 UTC |
| 1942–2008 | Child DOM task 31 | 13,178.49 ms |
| 2045–2241 | Child timer task 45 | 5,785.41 ms |
| 2243 | CDP event-loop pump context deadline | Incomplete execution |
| 2244–2250 | Parent task 54 finally delivers overrunBegin to context 2 | 11:20:29.684 UTC |

The sender-to-delivery association follows the frame-message enqueue and
parent task sequence; the trace does not directly store a callback source ID
or primitive payload in `frameMessagePosted`. This is stronger than matching
wall timestamps, but is not general taint tracing. The message waits about
19 seconds after enqueue while the child queue continues running.

The recorder timestamps are host wall time, not CPU samples. The DOM tasks
contain numerous interface reads, temporary frames and worker messages. The
last timer task includes 34 approximate SVG text-bound observations. The
largest gaps occur between remote DOM/Screen-related trace entries, but a gap
after a property trace is **not** proof that this property consumed the time.
A minimal local remote-property probe did not reproduce those multi-second
pauses. Its CDP-evaluated performance clock did not advance in Mimic, so its
reported zero durations cannot be used as per-operation measurements.

Also, scheduler `start` is currently recorded before checking cancellation.
A started task without an end at the pump deadline must not be mistaken for
a callback still executing alongside the parent. The initial offline active
task inventory needs this caveat; it is not evidence of concurrent JS turns.

## Independent compatibility defect and rejected candidate

`compatibility/corpus/frame-task-interleaving.js` creates a same-origin iframe.
Five chained zero-delay timers increment a counter and post its value to the
parent. Each parent handler records both the sent value and the child's current
counter. Chrome returned `(1,1), (2,2), (3,3), (4,4), (5,5)`. The previous Mimic
returned `(1,5), (2,5), (3,5), (4,5), (5,5)`.

`Realm.RunReady` drains up to 10,000 tasks from each realm before returning to
its parent. The CDP `Page.AdvanceTime` path also drives realms separately.
An experimental candidate selected the next ready task across active realm
queues using due time, existing source priority and Page-owned enqueue order.
It preserved task-local microtask checkpoints and refreshed active queues at
navigation boundaries. It was not committed as a fix.

An initial simple round-robin candidate passed the ordinary suite but failed
the frame test under race instrumentation. Comparing per-realm task IDs at equal
due times also failed. Sharing a Page enqueue counter plus synchronizing realm
clocks made the focused frame race test pass, but the broader suite reproduced
failures in existing V8 tests `TestAwaitParentPromiseFromDelayedChildFetch` and
`TestAwaitChildWorkCancellationAndPageTeardown`. Removing clock synchronization
restored those tests while breaking frame ordering again. Assertions and
deadlines in existing tests were not weakened.

The failed fetch trace ends after child `frameEvalResult`; no child request is
started. A previously pending child control/poll task has run before that eval.
This points to a dependency on how cross-realm Promise/native jobs receive
checkpoints; it does not yet prove the complete mechanism. Task ordering cannot
be repaired safely by changing queue selection alone and assuming those
checkpoints remain equivalent.

The candidate diff, new integration test and failed trace remain private in
`overrun-control-20260910/scheduler-candidate.patch`,
`frame_task_interleaving_test.go.txt`, `await-regression.log` and
`frame-task-failure.json` in the same private directory. Static-analysis and
comparison helpers are archived there too. These are investigation artifacts, not a
validated patch to apply. The experimental binary was renamed
`.build/mimic-fair-unvalidated.exe`; it is not a release or control binary.

The tracked corpus intentionally continues reporting four differences against
the current runtime. A known failing integration assertion was not added to the
normal Go suite. The normal runtime and its tests were restored to 1851d61.

## extraParams after the previous fixes

`extra-postfix-diff.json` uses no automatic random/session normalization.
It preserves unequal primitives and missing versus undefined. Differences
include elapsed setup times, viewport/visibility configuration, undefined
properties absent in Mimic (`scs`, and nested `pac`/`pad`), and remaining
Resource Timing fields. Long session strings remain unknown even when their
lengths match. Content type also differs; attributing that to runtime semantics
requires checking the actual response headers. None of these differences is
declared causal merely because it is present in extraParams. Resource Timing
was not changed again.

## Validation and next boundary

The ordinary suite passed for the initial round-robin candidate (browser
192.766 s), but that candidate failed focused race ordering. A subsequent
candidate passed focused frame/worker/cookie/resource/form race checks
(31.985 s), then failed the full suite on the two existing child-fetch tests
(browser 193.440 s). This is why neither candidate is considered validated.

The shared harness's `frame-task-before-20260910` report is valid, with four
differences and no incomplete probes. The passing Chrome protected control
reached target 404 and revisit 404. The post-1851d61 Mimic message control failed
600010. No protected run used the withdrawn candidate, so its completion-path
effect is **unmeasured**. Reaching the target in Mimic is **not achieved**.

After withdrawing the candidate, `go test ./...` passes on the restored runtime
(cached package results). This does not turn the rejected candidate's full-suite
or race failures into passes.

The next implementation must jointly cover ready-task selection, Page enqueue
ordering, realm clock coherence and cross-realm/native microtask checkpoints.
First reproduce both the five-message corpus and delayed-child-fetch tests,
including cancellation and Page teardown. Once they pass together, run equal
cold ordinary protected controls. If interleaving changes but completion does
not, retain the defect as having no observed verdict effect and return to the
long child tasks before the monitor. Do not force completion or increase the
monitor threshold.
