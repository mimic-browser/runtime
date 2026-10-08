# Observable subsystem compatibility package

This package repairs the generating browser operations behind the saved
`IGBuA2`, `oHIQ6`, `SbVZ3` and `OjmeV1` observations. Implementations and retained
synthetic tests contain no payload indices, site names or VM-specific branches.
Frozen headful Chrome 152.0.7977.82 remains the behavioral reference.

## Scope

- [Native callables](native-function-20260912.md): ordinary native prototype
  behavior, source metadata, receiver exceptions, cross-realm functions and
  workers. A real V8 callback replaces the `Function.prototype.toString` Proxy.
- [Computed styles](computed-style-lifecycle-20260912.md): live declarations
  follow canonical connectivity and active document ownership, including
  detached trees, fragments, shadow trees, adoption and removed frames.
  [Flat-tree participation](computed-style-flat-tree-20260912.md) handles
  unassigned light DOM, named/default slots, suppressed fallback and foreign
  shadow ownership; this is the exact cause of the captured index 82.
- [Console](console-family-2026-09-12.md): shared namespace methods, value
  description, coercion and Runtime preview across Window and worker realms.
- [AI availability](ai-availability-20260912.md): correct conditional-interface
  publication and unavailable-backend behavior for the four exposed interfaces,
  including dictionary conversion and policy boundaries.
- [Structured clone](structured-clone-20260912.md): the previously integrated
  shared codec repair covers boxed values and RegExp through the subsystem and
  its consumers, rather than treating the two captured values separately.

Performance API behavior and Trusted Types/CSP work are owned separately and
are not implemented by this package. Generic native getter source metadata also
applies to the existing performance getter; this does not change its API behavior.

## Interpretation

The [earlier field review](manual-045102-residual-review-2026-09-12.md) describes
the historical baseline. Its console caveat has since been reduced: independent
Chrome controls without remote-debugging flags confirm that console message
storage itself invokes custom conversion for some native value categories.
Runtime-domain preview is a separate source of observations. The captured
computed-style observation comes from light DOM excluded by a closed BODY shadow
root, not a disconnected document. `display:none` alone does not make a connected
declaration empty.

`OjmeV1[75]` is random-derived and is not an independent stable defect. The
graphics, CSS serialization, storage-backend and other residual fields in the
full registry remain separate investigations. Saved-response replay verifies
local execution observations, not the response a live server would produce.

## Final integrated replay

A fresh binary built from **62c84eb** replays the original saved programs through
ten complete, request-matched input/output pairs, with zero parser errors.
The complete arrays are compared, not just their lengths.

| Observation | Final result in both child executions |
| --- | --- |
| `IGBuA2` | Exact Chrome match; two common indices remain. |
| `oHIQ6` | Exact Chrome match; empty array. |
| `SbVZ3` | Exact Chrome match; all 90 entries. |
| `MlWrD5` | Exact Chrome match. |
| `OjmeV1` | All entries match except random-derived index 75 in the second execution. Index 82 is false; clone indices 118/119 agree. |

Comparison covers all **1,499 field rows**. No previously equal row whose Chrome
value agrees across A/B becomes different in the final run. This is bounded
regression evidence, not a claim that all historical residual fields now match.
Apparent timing matches are not counted as fixes. `KDQCx4` varied in an earlier
same-binary repeat, so its apparent improvements are also not claimed as fixes.

Initial integration caught two gaps in the first synthetic matrices: foreign
console arguments (62→76→90 entries) and the actual closed-shadow flat-tree
context. Both received general fixes and retained frozen regressions before the
final replay. Saved originals and historical binaries were preserved.

## Validation and boundaries

Integrated browser/CDP tests pass for AI conversion/policy, native functions,
computed styles/flat trees, console families and foreign arguments, debugger
console state, bootstrap observational equivalence, worker fetch, structured
clone, shadow DOM and cross-realm nodes. Full engine/v8 and webapi package tests
and the four generator unit tests also pass. The full generator manifest check
still encounters the documented pre-existing stale artifact manifest; frozen
inputs were not rewritten to conceal it.

Retained frozen coverage includes 30 native-function observations, 28 document
lifecycle observations, 23 flat-tree groups, 98 AI observations, console matrices
with and without CDP and foreign/inactive-realm controls, plus the prior clone
suite. Each subsystem report states its unsupported boundaries. No model backend,
full slot subsystem, profiler console instrumentation or general rendering
subsystem is claimed.

Private final evidence: `.build/observable-compat-package/package/`,
`package-replay.exe` and `comparison-package.json`. Baseline and intermediate
replays remain alongside them for attribution and repeat controls.
