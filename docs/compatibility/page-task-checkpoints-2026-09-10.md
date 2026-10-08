# Page task selection and cross-realm checkpoints

This follows the [child task boundary investigation](child-task-boundary-2026-09-10.md).
The previous scheduler candidate failed because selecting tasks differently
exposed another compatibility defect: a synchronous call into an iframe could
enqueue native jobs without arranging a checkpoint for that realm. A leftover
native polling task sometimes masked the missing checkpoint.

## Measured before state

The shared `frame-task-interleaving` corpus records the child's counter when
each posted message reaches its parent. System Chrome 152.0.7977.83 returned
1, 2, 3, 4, 5; the running pre-fix Mimic returned 5, 5, 5, 5, 5. The valid
`frame-task-before-20260910` report contains four differences.

The new `frame-microtask-checkpoint` corpus allows setup work to finish, calls
code in a child, then schedules a parent timer. Chrome observes `parent-sync`,
`child-job`, `parent-timer`; pre-fix Mimic omits `child-job`. The expanded
version measures eval, getter, ordinary call, constructor and throwing call.
All five cases differ in the valid `frame-jobs-expanded-before-20260910`
report. `frame-jobs-before-20260910` retains the initial eval-only experiment.
All captures are private; no challenge code or values are embedded in either
fixture or implementation.

## Implementation

* Ready tasks are selected across Page realm queues using existing source
  priorities, due time and a Page-owned enqueue counter. Local task IDs remain
  local; they are no longer used to compare equal-time tasks from different
  realms. Independent Pages share no counter or runtime lock.
* Realm clocks observe elapsed time at task selection boundaries. The CDP pump
  advances all realm clocks before executing work and rebuilds its active queue
  snapshot after each task, so navigation cannot leave it driving retired queues.
* Cross-realm entry records a pending checkpoint, including accessors and Proxy
  reflection operations. It does not execute those jobs inside the synchronous
  caller's JS stack. The outer checkpoint services the touched realms and any
  additional realm entries produced by their jobs.
* Scheduler checkpoints use the same browser-level path as explicit script
  cleanup. This also preserves polling for native asynchronous work instead of
  relying on a single leftover control task.
* Pending checkpoints are Page-owned, skip closed realms and are released on
  Page teardown. Worker event loops remain separately owned.

This is not a replacement of the VM's per-isolate microtask queues and does not
claim complete global FIFO equivalence for arbitrarily interleaved Promise
jobs across multiple isolates. The explicit advancing `RunUntilIdle` helper is
not redesigned. No monitor threshold, server continuation, token or payload is
modified by this change.

## Engine boundary

Goja executes Promise jobs when a nested `RunScript` returns. The new probe
there observes `child-job`, `parent-sync`, `parent-timer`, a distinct existing
engine limitation. The native checkpoint integration assertion is explicitly
skipped for that backend; its Chrome expectation is not replaced with Goja's
incorrect order. Existing Goja fetch, cancellation, worker, cookie and frame
task tests retain their assertions. Deferring Goja's nested jobs requires engine
support beyond this change.

## Validation and protected control

The full `go test ./...` suite passes with this implementation (browser package
193.459 s), including both existing V8 child-fetch tests that failed for the
earlier candidate. Focused scheduler tests cover due-time ties, cross-realm
enqueue order, elapsed clock visibility and completing a microtask checkpoint
before yielding. Frame integration tests cover both awaited evaluation and the
`Page.AdvanceTime` path used by CDP.

`go test -race ./internal/scheduler ./internal/cdp` passes. The final focused
browser race run passes (48.843 s), covering frame task interleaving, native
frame checkpoints, delayed child fetch, cancellation/teardown, worker microtasks,
resource ownership, partition cookies and form submission. This is not a claim
that the entire browser package was run under race instrumentation.

`.build/mimic-scheduler.exe` was built from this candidate; its SHA-256 is
`5d2d1cee77a4d2600533dafc94e1bf530e7caced2649c5f6e7e7b15e3baed19e`.
It contains the production changes described here. Against the rebuilt CDP
instance, the shared harness completed both local probes successfully:
`frame-task-interleaving` and `frame-microtask-checkpoint` are valid, complete
and have zero differences against system Chrome 152.0.7977.83. The latter
matches all five entry modes (eval, getter, call, construct and throw).
The private report is
`compatibility/private-captures/page-checkpoints-after-20260910/report.json`.
This is a system Chrome comparison, not the frozen 152.0.7977.82 reference.

No new protected-site control was performed after this restart. Completion-path
effect remains unmeasured; this document does not claim that Mimic reaches the
target page or that either scheduler defect caused the previous rejection.
