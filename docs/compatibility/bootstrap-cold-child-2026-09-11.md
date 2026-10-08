# Cold child bootstrap capture and offline VM follow-up

This follows [the manual capture fixes](manual-capture-fixes-2026-09-11.md).
Baseline is `37ba978`, on main. The earlier fix preserved live calls when seed
recording failed; it did not make that seed reusable. This package addresses the
creation of native WindowProxy objects inside the captured bootstrap itself.

Follow-up: [manual capture 211032 and the replay scope correction](manual-replay-scope-2026-09-11.md)
confirm zero snapshot-unavailable diagnostics in the new manual capture, but
supersede this report's claim of reaching the end of the saved exchange. The
offline driver reused the first document on reload and stopped before later
recorded cycles.

## Cause and implementation

The first realm for a profile can be a child. Bootstrap eagerly created native
proxies for its parent/top; the JSON host recorder then inspected the returned
object and could trigger cross-origin access or traversal of the parent's state.
The cache retained this error and reported it on subsequent uses. Multiple
`bootstrapSnapshotUnavailable` records are not independent capture attempts.

Window relations now retain only frame IDs during bootstrap. The existing native
proxy and canonical cache materialize a relation on its first actual getter read.
Restore refreshes the IDs and clears the lazy values before running page code.
The native global object and cross-origin access checks are unchanged.

Deferring proxy creation alone was insufficient: two constructor-discovery loops
read every global property value and thereby invoked the top/parent getters.
Both now inspect own data-property descriptors, as the other native-binding
reflection passes already do. They do not invoke Window accessors to discover
interface constructors.

The added `TestBootstrapSnapshotColdChildRelations` covers same-origin and
cross-origin children as the first captured realm, a parent toJSON getter that
must not be read, child seed admission/restoration, canonical parent/top identity,
access denial, and reuse of a child-created seed in a top Page. It was compiled
but **not executed** for this verification.

## Bounded offline replay

The local VM replay has no live-site connection. The replay driver uses only the 39 responses from `manual-20260911-202858` through
a replacement Page transport. There is no live fallback. Unknown GETs return a
transport error; the first uncaptured non-GET request cancels the replay. Neither
stored nor newly computed request bodies were sent to an external server.

The same driver and fixtures were used for the unmodified baseline surface
(build overlay) and final surface:

| Observation | Baseline | Final |
| --- | ---: | ---: |
| Cached bootstrap-unavailable records | 7 | 0 |
| Scheduler error records | 0 | 0 |
| Propagated script-error records | 0 | 0 |
| Image decode error records | 3 | 3 |
| Responses supplied: GET / POST | 10 / 4 | 10 / 4 |
| Requests blocked: GET / POST | 1 / 1 | 1 / 1 |

Cancellation is the expected offline transport boundary, not a semantic failure.
The intermediate lazy-only and one-loop variants still failed; diagnostic stacks
located both constructor-discovery reads. All intermediate receipts remain in
the ignored directory, rather than being counted as successful fixes.

This is controlled execution of saved programs, not a deterministic HTTP oracle:
request bodies are not validated against historical submissions; GETs may be
reused; the historical helper's widget-ID mapping is retained. Recorded server
responses cannot reveal how the server would evaluate the changed observations.
There is no performance or successful-challenge claim. Absence of propagated
errors does not establish absence of caught exceptions inside the programs.

## VM inspection

The existing private VM lab and replay instructions were located and reviewed.
The current parent's embedded 4,522-byte program executes 359 traced steps through
two instrumented dispatchers, returns undefined and reports no blocked side
effect or exception. This is the initialization program, not the full decision
path. The child's 5,208-byte candidate stops while loading in the small VM lab
because that lab lacks URLSearchParams; the resulting missing runProgram export
is a harness limitation, not a newly diagnosed Mimic defect.

The current Mimic offline replay was used for subsequent execution instead of
adding browser stubs to the VM lab. It reaches the end of the saved exchange and
the next uncaptured POST. The exact input responsible for the historical failed
decision remains unknown. Complete register/value provenance and a paired native
execution are outside this package.

Private source/fixture/binary hashes, before/after traces, opcode traces, blocked
request hashes and commands are retained in `.build/vm-offline-20260911/`, with
the compact receipt in `summary.json`. Raw scripts, cookies and request bodies
are not included in Git.

## Validation scope

Production and offline-helper builds succeeded. The browser test binary compiles.
No browser suite, targeted test execution, race, general differential, new Chrome
oracle, native stress or performance gate was run for this package. Historical
passing results do not qualify this unexecuted validation scope.
The [native stability debt](snapshot-stability-debt-2026-09-11.md) remains open
under its existing immediate-reopen condition.

Suggested focused check, followed by the normal package validation:

```powershell
go test ./internal/browser -run 'TestBootstrapSnapshotColdChildRelations|TestBootstrapSnapshotNavigationRebindsDocumentAndOrigin|TestWindowReflectionMatchesFrozenChrome|TestWindowGlobalPublicationOrder|TestWindowFramesReflectsDirectChildBrowsingContexts' -count=1 -timeout=5m
```
