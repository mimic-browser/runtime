# Consumed service implementation packet, capture 225523

This follows the historical [41-to-11 receipt](residual-225523-2026-09-11.md).
The original residual is now **41 → 2 observations: zero remaining fixable
semantic/capability differences in this consumed set**. The two retained
observations are `d.lastModified` (execution time) and `n.webdriver` (the explicit
Mimic invariant `false`, even when Chrome diagnostic instrumentation reports
`true`). Nine previously absent services now have implementations and focused
behavioral regressions; presence alone is not their implementation criterion.

## Implemented behavior and ownership

| Service | State and implemented behavior |
| --- | --- |
| caches | Context/origin cache storage; canonical Request/Response integration, matching/Vary, body consumption/cloning, atomic addAll/put/delete, retained handles after unlink. |
| cookieStore | Existing network cookie jar; selection, conversions, writes/deletes and change events including network/CDP changes. |
| indexedDB | Context/origin catalog, connection and transaction grants; stores, indexes, cursors, graph cloning, upgrade/blocked/versionchange, commit/rollback, concurrent Pages and teardown. |
| navigation | Canonical Frame history; entries/state/identity, same-document and cross-document navigation/traversal/reload, interception, Promise/event ordering and realm cloning. |
| scheduler | Page event-loop queue; priority, continuation ordering, abort and priority changes with Go-owned signal/task context. |
| crashReport | Go-owned initialization, ordered annotations and UTF-8 serialized buffer capacity; atomic validation and observable errors. |
| launchQueue | Authoritative Page launch queue; embedder `Page.QueueLaunch` feeds URL launches, delivery waits for the active consumer. |
| documentPictureInPicture | Auxiliary top-level context on the existing Page loop; independent document/viewport, opener, activation, close and isolated auxiliary history. |
| speechSynthesis | Go-owned utterances, voices, queue/run identity, cancellation and lifecycle; replaceable platform provider, real native notifications, no duration timers. |

Platform-specific types and lifecycle remain outside browser core. See
[service ownership and portability](../architecture.md#service-ownership-and-portability).
Speech is tested with a platform-independent controlled provider and, separately,
with installed Windows voices at zero volume. Native allocation/wait failures
remain observable; deactivated documents stop their provider; late completion
cannot finish a newer operation. Linux/non-cgo speech package compilation passes.

## Frozen oracle and replay

Only local frozen Chrome **152.0.7977.82** and the supplied saved capture were used.
No external target run or live-network fallback occurred. Focused services,
cache/cookie, scheduler, IndexedDB, Navigation, PiP and speech oracle packets pass.
The committed regression fixtures retain the measured Chrome expectations.

The diagnostic replay's normalized reference-A comparison is **47 → 8** on each
repeated consumed observation (1666 current/Chrome observations, no new differing
paths). Six of those eight are the previously proven Chrome-control variations:
`d.hidden`, `d.visibilityState`, their two WebKit aliases, `outerWidth`, and
`outerHeight`. Chrome A/B running the same saved programs disagree; the fixed
visible control matches Mimic. Their individual evidence remains in the previous
JSON receipt. Raw counts retain two additional substituted route identifiers;
only the explicitly substituted `/rch/` segment is normalized. Repeated cycles
are not summed as independent defects.

Final private evidence is under `.build/consumed-services`: `verified-replay`
contains the diagnostic comparison and serializer records, `verified-original`
the unmodified saved-response replay, and `merged-evidence` the retained
reference observations and logs. Replay completion at the first uncaptured boundary reports
`context canceled`; this is the bounded offline harness outcome, not a fresh
server verdict. The explicit missing-body fixture handling remains documented
in the prior packet; a missing CDP body is not represented as captured data.

## Scope and known boundaries

This closes the named consumed residual, not general Web Platform conformance.
IndexedDB is currently wired to Window, stores data for the Context lifetime in
memory, and has no disk crash durability or quota/eviction subsystem. Its graph
codec supports the tested graph/Blob/File brands, explicitly rejects
WebAssembly.Module, and does not claim arbitrary platform-object serialization
or all mixed-realm cyclic aliases. URL launch delivery does not claim a file
launch provider. Speech effects require a provider; other OSes may inject one,
otherwise speech reports `synthesis-unavailable`. None of these is a remaining
observed consumed difference in this capture.

The Goja engine cannot reproduce the native inter-listener microtask checkpoint
used by the corresponding consumed event/IndexedDB lifecycle tests. These cases
are explicitly skipped on Goja and exercised on V8; expectations were not
weakened. This engine boundary is distinct from the zero residual result on the
V8 replay runtime.

## Validation receipt

Final suite, race and replay results are recorded in the adjacent JSON receipt.
The ordinary browser suite passes in 377.548 seconds. The initial race browser
process was deliberately interrupted to split the remaining tests into three
bounded shards; its package failure is an interruption, not a detector report.
The coverage ledger accounts for every top-level browser test across the initial
completed tests and the three shards; no test is removed from the final gate.
The production package set is `./internal/... ./chrome/... ./cmd/... ./tools/...
./compatibility ./benchmark`. A blanket `go test ./...` also discovers historical
private-capture scratch Go files with duplicate `main` and undefined `New`, and
fails to build those scratch packages. They were not changed to hide the failure.
Earlier development failures/timeouts (owner reentry, optional Goja clone-brand
handling, and an incorrect test origin) were fixed and their logs preserved.

Skip review exposed an incorrect pre-initialization capability check in the IDB
lifecycle test: the full run had skipped V8 as well as Goja. The test now realizes
the runtime and permits this skip only on Goja. The corrected entire IDB oracle
package is rerun ordinarily and with race; the V8 lifecycle fixture executes.
This is a test-only correction; the production implementation did not change.
Native Windows speech notifications also pass a separate opt-in race run.

**Race is not fully green.** Three existing Goja tests hit short context deadlines
under parallel race instrumentation. `TestFrameInsertionAfterInnerHTMLAndFragmentMove`
passes a subsequent isolated run with its original 5-second limit. Two remain
reproducible with the original 10-second limit, including fresh-process retries:
`TestNavigationRemovesRetiredDescendantTasks` and
`TestChildNavigationRemovesRetiredGrandchildTasks`. Their ordinary and V8 runs pass;
no data-race report was emitted. Each initializes three fresh realms within a
shared deadline; Goja reparses/runs the bootstrap for each and cannot use the V8
snapshot path. Bootstrap setup uses its existing background context, so the
instrumented setup can exceed the guard before the next context-checked operation.
The observed failures are context deadlines, not failed retired-task assertions;
those subsequent assertions remain unverified in the two race runs.
No production optimization or test-timeout relaxation was added to conceal this
validation limitation. These two test failures remain explicitly open in the
race receipt and are not counted as consumed capability gaps.
