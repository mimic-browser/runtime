# Navigation and realm ownership block

This reference describes the navigation ownership foundation identified in
[the first report](report.md), including its remaining limits. It does not
establish complete support for all original 28 observation groups.

## Semantic changes

Window references identify a browsing context and follow its current Realm.
Document, node, object, function, constructor and Symbol references identify their
owning Realm. Navigation no longer redirects old object operations into the new
document or immediately destroys their runtime. Documents use the ordinary
canonical reference bridge, including their prototypes and constructors.
`frameElement` imports the parent's canonical node instead of creating an unrelated
wrapper. Symbol keys and arguments share their owner's canonical identity.

DOM traversal resolves a browsing Document through that same canonical bridge
before creating an inert-document wrapper. Document receiver checks use private
DOM slots rather than a caller-realm instanceof check. Every child navigation
joins its parent's node arena before publishing node IDs; a document lookup also
checks arena identity, so separately created document trees cannot alias by ID.
Existing cross-realm adoption/import observations remain part of validation.

The Page owns a registry of Realm runtimes. Imports create ownership edges; a
Window reference creates a browsing-context edge that follows the current Realm.
Collection traces from active contexts, outside JavaScript evaluation/cross-realm
call stacks, and disposes unreachable inactive realms, including cycles.
Navigation without imported references does not retain every visited runtime.
Page teardown disposes the complete registered graph. No global runtime lock or
cross-Page registry was introduced.

Document deactivation and runtime disposal are separate operations. Navigation
and detachment cancel resource work, stop workers, discard scheduler callbacks,
reject new scheduler work and suppress Promise checkpoints. Retained functions
remain callable synchronously. The measured old `Document.defaultView` is null.
The scheduler's permanent Close operation cannot be reversed by Resume.

Current cross-origin Window reads reject with a caller-realm DOMException while
old same-origin object capabilities remain usable. Saved eval has a separate
measured current-origin gate before argument conversion, including non-string
inputs. This direct bridge gate is not complete native eval security; see below.

## Evidence and tests

The [Chrome oracle documentation](../../../internal/browser/testdata/navigation_realm_oracle.md)
describes exact version, capture provenance, scripts and activity observations.
Captures preserve complete Chrome results. Focused tests cover supported fields,
retention across repeated navigation, release when an importing realm retires,
unreachable reference cycles, and complete Page teardown.

The unchanged corpus is 136 structured probes plus the same 100-probe surface
budget (3,533 discovered surface probes). A fixed-list comparison also replays
all 236 previous observations and all original 28 minimized representatives;
it compares every normalized leaf, not just each group's first signature.

The final build produced **236 checks, 135 matches, 101 divergent probes,
19 unique groups, 0 unstable observations and 0 harness errors**. See the
[official summary](navigation-fuzz-summary.json) and
[full-leaf regression comparison](navigation-regression.json).
Against block 1: **3 groups fixed, 19 remain, 0 previously matching probes
regressed, 0 new differing leaves, 0 Chrome oracle drift**. All 28 original
minimized representatives were replayed: 9 now match completely (6 from block 1).

| Original group | Shared semantic correction |
| --- | --- |
| `6dad37e5c7073bd2` | Stable browsing-context WindowProxy with per-document realm ownership across navigation. |
| `c035c36945679951` | Canonical parent-owned frameElement and realm/interface identity through the reference bridge. |
| `fb77ad41b9b7b4e0` | Caller-realm DOMException for current cross-origin access and access-dependent WindowProxy tag. |

The original minimized probes remain in `../compat-fuzz-slice/results/<id>/`;
their full replay outcomes, including all remaining leaf differences, are in
the regression comparison. The remaining 19 original groups are the block-1
table excluding those three IDs; no additional original group is claimed fixed.

The [supplemental comparison](navigation-supplemental.json) preserves full
results from the new navigation captures, including pending fields. Its new
diagnostic findings are distinct from regressions in the fixed corpus: native
global retargeting, delegated eval, cross-origin Window reflection, and the
pre-existing srcdoc setup boundary described below. The complete lifecycle
capture matches. The unsupported fields were not normalized away.

Validation commands (from this worktree):

```powershell
go test ./internal/browser ./internal/webapi ./internal/scheduler ./internal/dom -count=1 -timeout=15m
go test -race ./internal/browser ./internal/webapi ./internal/scheduler ./internal/dom -count=1 -timeout=20m
.build/fuzz-venv/Scripts/python.exe compatibility/compat_fuzz.py --chrome-cdp http://127.0.0.1:19333 --mimic-cdp http://127.0.0.1:19322 --fixture-port 19444 --timeout 30 --output navigation-results
```

- Final complete non-race browser/webapi/scheduler/DOM suites: **PASS** (browser
  335.273s). [Log](navigation-browser-tests.txt).
- Existing CDP package tests: **PASS**. [Log](navigation-cdp-tests.txt).
- Harness unit checks: **21 tests, OK, 6 skipped**; the live corpus is validated
  separately above. [Log](navigation-harness-tests.txt).
- Initial affected race selection under concurrent validation hit the existing
  20-second context limit in `TestFrameDocumentEntryFollowsSynchronousCalls/goja`.
  [Initial log](navigation-first-affected-race.txt). The unchanged test then
  **passed on the final source in isolation** (19.575s total), and a separate
  untouched `3faacc6` archive also passed (22.286s total). Those total durations
  include setup outside the context limit; they are not benchmark comparisons.
  [Current isolated log](navigation-isolated-entry-race.txt),
  [baseline isolated log](navigation-baseline-entry-race.txt).
- The second affected race selection passed the entry-document test but hit the
  existing 10-second context limit in
  `TestChildNavigationRemovesRetiredGrandchildTasks/goja`.
  [Second selection](navigation-second-affected-race.txt). This unchanged test
  then **passed in isolation** on the final source (11.555s total) and untouched
  `3faacc6` (11.535s total).
  [Current](navigation-isolated-grandchild-race.txt),
  [baseline](navigation-baseline-grandchild-race.txt).
  Neither aggregate affected selection is represented as a clean pass.
- New ownership, navigation-node identity, nested WindowProxy, origin-tag,
  unreachable-cycle and scheduler-close tests under race: **PASS** (browser
  41.286s). [Log](navigation-new-tests-race.txt).
- Complete runtime race suite: **NOT PASSING**. Browser reached the 20-minute
  suite deadline (1201.470s), while webapi, scheduler and DOM passed. The run
  reported goja context deadlines in cookie transport, entry-document calls,
  child-load observation, child Location navigation, frame insertion, and
  retired descendant/grandchild task tests. It timed out during
  `TestConnectedShadowTreeIframeLoadsItsDocument`. See the
  [complete log](navigation-full-race.txt). The earlier block already reproduced
  the cookie-transport deadline on untouched `3faacc6`; other aggregate deadline
  failures are not all classified as baseline defects. A final focused rerun
  passed child-load observation, child Location and retired-descendant tests,
  but frame insertion still hit its five-second goja context limit.
  [Focused log](navigation-remaining-deadlines-race.txt). That same frame-insertion
  deadline also **reproduced on untouched `3faacc6`**.
  [Baseline insertion log](navigation-baseline-insertion-race.txt).
- That full run also reported two data-race warnings from the same existing
  test-fixture cause: `TestNavigatorPermissionAndNetworkState` changed `p.env`
  without its mutex while the resource loader copied the environment under
  `p.mu.RLock`. The race **reproduced on untouched `3faacc6`** in an isolated
  five-run test. [Baseline race log](navigation-baseline-network-race.txt).
  The fixture now takes `p.mu` around those same three assignments. No assertion,
  expected value, runtime behavior or timeout was changed. After that fixture-only
  correction, **10 consecutive isolated race runs passed** (23.382s total).
  [Fixed-fixture log](navigation-locked-network-race.txt). The full 20-minute run
  predates it and must not be called a clean final race pass.

During implementation, full browser tests caught node-owner identity and eval
native-text regressions absent from the small fuzz corpus. Both were repaired
before the final full-suite pass. No existing expectation or timeout was changed.

## Explicit unfinished work

1. **Native global-proxy retargeting.** In Chrome, a retained closure reading
   `globalThis.savedMarker` observes the replacement Window after same-origin
   navigation and throws SecurityError after cross-origin navigation. Mimic's
   separate per-realm isolates still expose the old native global. The full
   ownership capture keeps `oldFunctionGlobal`; the focused test explicitly
   excludes that single field from its supported comparison. It is not full
   Chrome equivalence. A JavaScript replacement for globalThis would break
   identity, sloppy `this` and direct-eval semantics; an engine-level design is
   needed.
2. **Native delegated eval gate.** Chrome's saved eval returns undefined after
   cross-origin navigation through direct calls, call, apply, bind and Reflect.apply.
   The direct bridge call and local Reflect.apply path match; calling the remote
   native call/apply/bind can recover the original intrinsic inside its owner
   isolate and bypass the gate. This needs an engine-level eval invocation/access
   policy. Per-method wrappers or replacing intrinsic eval with a Proxy would
   damage direct-eval semantics and were not used.
3. **GC precision.** Existing bridge caches hold strong wrappers. An imported
   object conservatively pins its owner until the importing realm retires, even
   if script drops its final reference. The graph releases unreachable cycles
   and ordinary unobserved navigation, but is not connected to JavaScript weak
   reachability. Long-lived importers can retain many old runtimes.
   Separately, the shared DOM arena retains node entries from replaced child
   documents even after their Realm runtime is collected. Realm-count tests do
   not prove DOM-memory reclamation. Joining navigated documents to that arena
   preserves node identity but extends its existing retention to navigation
   history; ownership-aware DOM arena pruning remains necessary.
4. **Broader inactive-document capabilities.** The lifecycle oracle covers
   queueMicrotask, Promise reactions, zero-delay timers and synchronous calls.
   It does not establish complete lifecycle parity for locks, permissions,
   networking, storage or all task sources. Retained top-document URL/security
   projections and about:blank creator base-URL snapshots also need dedicated
   measurements; some existing projections still consult current Page/parent
   state. Do not infer these are fixed from child-object ownership.
   Inert Documents created inside another realm also need broader canonical
   ownership coverage: the registry resolves each realm's browsing Document,
   while inert-document wrappers remain in the existing local DOM cache.
5. **Other compatibility classes.** The prior report's remaining WebIDL binding
   signatures, constructor inheritance, receiver/argument conversions, Location
   descriptors, Window named properties and interface member order are outside
   this authorized foundation block. Collection duplicate numeric own-key parity
   still requires a native legacy-object interceptor.
6. **Additional measured probes.** Cross-origin Window reflection still differs
   in `then`, Symbol.hasInstance, Symbol.isConcatSpreadable, the toStringTag
   descriptor and the full own-key inventory. These are retained in the
   supplemental capture, outside the unchanged 236-probe corpus. The exact
   nested-window srcdoc capture also remains unsupported: its script never runs
   because existing iframe navigation reads only src. The passing HTTP-based
   two-evaluation test checks nested WindowProxy retention independently and
   asserts the window exists before navigating. Neither pending capture is
   claimed as a full match.

These boundaries are intentionally visible. No corpus probe, Chrome expectation
or runtime behavior was hardcoded to a probe ID.
