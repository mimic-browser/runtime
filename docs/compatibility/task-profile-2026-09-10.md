# Cross-realm task profiles, 2026-09-10

The measured slowdown has a concrete browser-runtime contributor: repeated
cross-realm reference description and reflection through the V8 actor bridge.
Identical, site-independent JavaScript reproduces it. The evidence does **not**
prove that the complete downloaded programs follow identical control flow,
nor identify their first diverging branch. CPU sampling cannot establish that
equivalence, especially across different server-generated programs.

## Captures and measurement limits

Private artifacts live under `compatibility/private-captures/task-profile-20260910`.
Chrome is system **152.0.7977.83**, V8 **15.2.124.21**; frozen .82 was not
available on its previous debugging endpoint. The earlier frozen capture
remains the historical reference, not the source of these new CPU samples.

Chrome was recorded in a disposable browser context with recursively attached
targets. Sampling starts before resuming each target and stops when that
target sends its second `/fo/` request. The principal page and iframe profiles
were retrieved successfully. Several already detached worker/frame sessions
could not return profiles or final snapshots; their errors remain in the
manifest. The Chrome run reached the application document.

Mimic ran in separate fresh diagnostic processes. Each isolate was sampled
from creation to teardown, with Go CPU sampling and host-call counters. An
additional message listener recorded bounded descriptor-based summaries.
Instrumentation changes timing. Chrome and the after-candidate sample at
1,000 microseconds; the initial Mimic profile used the existing profiler's
100-microsecond setting. Therefore live before/after timings are observations,
not a controlled speedup measurement. The uninstrumented local probe below
provides the implementation comparison. Diagnostic source changes were saved
as a private patch and removed from production sources.

## Network versus execution

In the original retained trace, request 872 receives its response at sequence
900 in **299.272 ms**. The following flow request is sequence 2756, another
**10,967.117 ms** later. The bulk of this interval is not waiting for that
network response.

In the new Chrome capture, the corresponding child response-to-next-request
window is **4.089355 seconds**. The CPU sample accounting for that interval is
approximately 2.041 s in page JavaScript, 1.719 s idle, 0.224 s garbage
collection and 0.072 s program/native work. Builtin DOM entries account for
small remaining samples. Native/inlined work is not necessarily attributed
to a separately named builtin.

The new pre-candidate Mimic trace has a long child task at sequences
**2320–2655**, lasting **10.941190 s**. Mapping its wall-clock bounds to the
owning isolate profile gives about:

| Leaf attribution | Seconds |
| --- | ---: |
| Mimic surface wrappers | 10.175 |
| Page script | 0.117 |
| Program/native | 0.588 |
| Garbage collector | 0.034 |

The largest wrapper leaves are `apply` (4.153 s), `getPrototypeOf` (3.539 s)
and `get` (2.003 s). A wrapper may include time waiting for another isolate;
these are sampled elapsed-time attributions, not exclusive CPU costs of the
named primitive. Do not sum parent and child isolate profiles as exclusive
wall time. Wall-clock/profile alignment uses timestamps immediately before
starting the inspector, so boundary samples have initialization uncertainty.

Host counters independently identify `framePrototype`, `frameCall` and
`frameGet` as dominant. Whole-process Go samples include 5.47 s in
`runtime.semawakeup` and 8.36 s flat in `runtime.cgocall`. The implementation
previously described one remote value with many separate actor round trips:
type checks, special-object identities, reference lookup, and object shape.

## Controlled reproduction and implementation

`frameValueEncoderProbe` creates an iframe, then executes exactly 100 iterations
of walking a three-object prototype chain, reading a property and calling a
function. Both engines return checksum **6050**. No server code, randomness,
network request or variable branch is involved.

| Run | First execution, ms | Subsequent executions, ms |
| --- | ---: | --- |
| Chrome .83 | 47.617 | 5.108, 4.508 |
| Mimic before | 726.493 | 535.147, 491.911 |
| Mimic candidate | 378.468 | 191.970, 197.205 |

Mimic measurements wrap Page.Evaluate; Chrome measurements include CDP
round-trip overhead. Both include iframe creation and teardown. This small
sample demonstrates the cost and a roughly 2.6x improvement in warm Mimic
executions; it is not a statistically rigorous benchmark or parity claim.

Reference descriptions now execute in one private host invocation on the
object's owning runtime. The same identity, shape and prototype logic runs;
no remote result is cached and no browser-observable operation is skipped.
V8 global lookup gained the missing callback-local path so this invocation
does not dispatch a request to its own blocked actor. Regression tests cover
repeated operations, changed prototypes and reentrant global getters.

The post-candidate live capture still returns a repeated challenge. Its first
flow interval is 11.299 s, versus 16.489 s in the more heavily sampled initial
profile. The longest recorded task is 4.665 s, versus 11.088 s previously.
These different attempts and sampling settings do not establish equal page
control flow or a controlled end-to-end improvement.

Two subsequent sequential controls use fresh dedicated Mimic processes and
the same message listener, with **no CPU profiler or host counters**. The
first flow interval is **15,453.551 ms before** and **6,961.995 ms after**.
Before, first-cycle tasks include 6,142.696 ms, 4,116.604 ms and 4,077.684 ms;
after, the first cycle includes 3,888.478 ms and 2,172.834 ms tasks. These
observations support the local probe's improvement without the sampling-rate
confound, but still involve different server programs. Both controls fail to
reach the application. The before control observes `overrunBegin` followed
by `overrunEnd`/`fail`; the after control observes `fail` without any recorded
`overrunBegin` across its three captured attempts. This does not prove that
overrun determines the server's outcome. Raw evidence is in `control-before`,
`control-after`, and `control-summary.json`.

## overrunBegin correlation

The original 6.521386-second task is **2080–2497** in the retained trace. Message
values were not retained there, only their field types and encoded sizes.
Consequently an `overrunBegin` label cannot be assigned to a particular
original message with confidence.

In the new pre-candidate recording, correlation is explicit:

* Long task 59 ends at **13:08:46.7503217 UTC**, sequence 2655.
* Child timer task 58 starts at **13:08:46.7672902**, sequence 2719.
* It posts the message at **13:08:46.7694189**, sequence 2721, and queues
  parent posted-message task 75 at sequence 2722.
* Task 75 delivers the observed `overrunBegin` at **13:08:46.7841896**,
  sequence 2759.

Thus the new recording places emission about **19.1 ms after**, and reception
about **33.9 ms after**, the long task. It is not emitted during that task.
The receive association follows the queued parent task, not message size
alone. This is temporal correlation; it does not establish a timeout verdict
or that the same ordering held in the original capture.

## Remaining question

Slow execution of identical browser operations is demonstrated and partially
improved. A separate additional page branch remains possible. No first
branch/condition difference has been proven, and the protected scenario is
not fixed. A complete control-flow claim would need aligned source identity
and branch/operation observations, not renamed sampled call stacks.

## Validation

All packages passed: the initial `go test ./...` invocation reached its
four-minute overall browser-package deadline; its other packages passed.
The complete browser package then passed with a ten-minute limit in
**283.993 s**. The SVG test active at the deadline also passed independently.
Focused frame/cross-realm/nested/cancellation tests passed before that run.
The new encoder, changed-prototype, handle-identity and reentrant-Get tests
passed under `-race` (browser 11.183 s, V8 1.158 s).
`tools/performance/fast_gate.py` also completed successfully with a freshly
built, hash-verified binary: all six frozen correctness workloads, warm
measurements, concurrent waves and the prescribed teardown/memory checks.
Its raw output is retained in the private `fast-gate` directory; no full
benchmark matrix or frozen baseline was modified.
