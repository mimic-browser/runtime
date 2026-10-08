# Wikipedia: semantics-preserving implementation plan

Status: **design proposal, 2026-09-19**. This document describes proposed production
changes, not a measured implementation. Experimental patches are retained under
`tools/performance/pocs` and are not part of production source.

## Target and evidence boundary

Target: approach Chrome on the complete unchanged workload, ideally 2 s warm
and 2 s cold, with warm the priority. Warm creates new Pages/documents: it is
not a second query against an already laid-out document. First construction
must therefore become cheaper; invalidation improvements alone cannot suffice.

The last destructive combination reached **2251 / 2147 ms cold / warm**, medians
of five pairs. Its paired destructive control was 2717 / 2664 ms. It suppressed
author scripts/styles, froze derived state, fabricated geometry, bypassed scroll
and correct hit testing, reused stale DOM reads and memoized Playwright queries.
All workload assertions passed; browser semantics did not. This is evidence of
sensitivity to combined work, not a proven attainable production bound.

Earlier 4608 / 4172 ms is a real intermediate PoC result, not replaced by the
6+ s production baseline. Compare each change with its own matched control.
Control timings drifted between later series; do not add their deltas or infer
a sub-2 s result by subtracting that drift. Frozen Chrome 152 must be used for
the eventual browser comparison; a separate Chrome 153 diagnostic is not that
control.

## Decision

Build a **demand-driven computation plan shared by style, geometry, scrolling
and input**, over the existing authoritative DOM. First remove unnecessary work
inside an observation, then reuse a bounded subset of valid results across
observations. Do not start with a new permanent layout tree, global mutation
journal, parallel DOM model, shared V8 owner or a collection of stale caches.

There is no established single cause accounting for the whole gap. The plan
addresses first-build cost, repeated dependent computations and author-triggered
work together, while preserving author execution and its observable ordering.

## Translating the PoCs into real changes

| Destructive shortcut | Production replacement | Evidence limitation |
| --- | --- | --- |
| Ignore author CSS | Parse stylesheet input once; property-specific cascade/computed reads; lazy CSSOM projection | Disabling CSS also changes fonts and layout demand; its full delta is not recoverable matching time |
| Ignore author scripts | Keep scripts/tasks; make the style/layout and observer reads they trigger share valid computations | Skipped top-level execution alone does not explain cold savings; no scheduler/task suppression is justified |
| Freeze geometry | Separate measurement, placement and viewport/paint dependencies; reuse only valid results | Freeze alone had a small warm gain; first construction remains essential |
| Fabricate rectangles, skip scrolling, trust target | One real geometry observation feeding scroll, clipping and hit testing; revalidate after event handlers | Joint delta includes required work, not only duplication |
| Keep stale parent/owner/attributes | Demand-loaded read records in mutation-safe observation scopes | Topology/attribute gains are secondary, not a multi-second solution |
| Memoize Playwright query results | Cheap native DOM reads and shared style/geometry facts underneath unmodified traversal | No demonstrated equivalent replacement for the entire memoization gain; do not cache Playwright callbacks or role results |

## Implementation sequence

### 1. Small style/read-plan changes, independently reviewable

Files: `css_computed_values.js`, `cssom_compatibility.js`, existing style-read
helpers in `surface.js`.

1. Resolve scalar properties without constructing box state or reading unrelated
   writing-mode/direction. Preserve cascade, inheritance, animation and existing
   serialization first. Start with the six properties exercised by the saved
   read-plan patch; it is a design input, not a validated patch to apply wholesale.
2. Cache the nearest specified inherited source within an observation, not the
   final computed value. Preserve `currentcolor`, caret `auto`, unitless
   line-height and animation rules. Bypass unsuitable dynamic observations and
   bound property names per node.
3. Remove the stylesheet parse -> CSSOM materialize -> serialize -> reparse path.
   Make the parsed stylesheet program authoritative for both matching and lazy
   CSSOM wrappers. Preserve rule/declaration identity and ordering, imports,
   media applicability and synchronous CSSOM mutation visibility. Do not replace
   parsed semantics with raw source passthrough.

Deliver separate diffs for scalar reads, inherited-source reuse and stylesheet
representation. The first two are small wins, not the promised route to 2 s.
The stylesheet change must reduce initial document work, including with author
scripts and styles fully enabled.

### 2. Main change: one geometry computation plan per observation

Files: `css_box_geometry.js`, `surface.js`; consumers in `scrolling.js` and
`input.js`. Keep the existing owner-realm and canonical DOM boundaries.

1. Represent intrinsic measurement, constrained size, placement and viewport
   projection as distinct results. Key measurements by actual constraints and
   shaping/style inputs, not just node identity.
2. Within the existing observation, share these results across legacy geometry,
   Taffy input construction, client rects, visibility, scroll metrics and clipping.
   Extend the existing observation mechanism rather than adding another cache
   layer or claiming owner checkpoints currently cannot share anything.
3. Make recursive construction explicit: uncomputed / computing / ready.
   Define the fallback for cyclic containing-block dependencies. The observed
   `append -> size -> containingWidth -> taffyBox -> append` cycle means that
   deleting legacy root layout is not a valid optimization by itself. Preserve
   its cycle-breaking behavior while removing only duplicated completed work.
4. Start with the demonstrated overlapping root/leaf computation path. Do not
   rewrite every formatting mode at once. Geometry results under different
   width/height constraints must remain distinct.
5. Scroll and hit testing consume this same calculation. Preserve nested scroll
   containers, clipping, transforms, stacking order and pointer eligibility.
   A measured target is only a candidate, never proof that it is unobstructed.

First milestone: cheaper **initial** geometry construction with no persistent
cross-command reuse required. If only visibility becomes faster while scrolling
pays the saved work, this milestone has failed.

### 3. Bounded reuse with explicit invalidation

Only after step 2, retain selected expensive derived results across observations.
Begin with exact existing generation matching. Introduce narrower generations
only for the mutation classes needed by the retained results. Unknown changes
conservatively invalidate; do not infer safety from a passing Wikipedia run.

| Input change | Minimum invalidation obligation |
| --- | --- |
| Text, children, font/resource availability | Affected intrinsic measurements; dependent ancestor measurements and placements |
| Ancestor/class/attribute/CSSOM/style change | Selector/inheritance dependencies, then metric or paint results affected by changed values; conservative fallback where classification is incomplete |
| Width, viewport, containing block or preceding sibling | Constraint-dependent sizes and placement, including descendants and flow dependents |
| Scroll offset | Viewport projection, clipping and hit testing; also sticky/fixed/scroll-dependent observations as applicable, not unconditional intrinsic reuse |
| Transform, stacking, visibility, pointer-events | Their geometry/paint/hit-test dependents; transforms can change containing blocks |
| Reparent, adoption, shadow/slot reassignment | Structural/flat-tree reads, inherited style, ownership and affected layout dependencies |
| Navigation or Page teardown | All document-owned projections, retained values and references |

Do not recursively validate a whole subtree on every read; a previous exact
intrinsic-validation attempt already spent the prospective saving on validation.
Use mutation publication at existing authoritative gateways and input tokens.
No second mutation log or independently synchronized DOM is needed.

Every retained cache must document its input key, entry/byte limit, eviction and
teardown owner in the same change. Eviction may only cause recomputation. Keep
retention Page-local; test bounded size and independent Pages under concurrency.

Recheck generations after event handlers and nested owner execution. In
particular, `mousemove` can move a target or install an occluder before
`mousedown`; an actionability result must not override the new state.

### 4. Secondary DOM/accessibility read cost

Files: `surface.js`, `document_compatibility.js`, canonical mutation/read
gateways in `internal/browser`, using existing node wrapping and owner routing.

Unify requested parent, owner and attribute reads into demand-loaded records
inside existing pure observation scopes. Keep structural parentage separate
from flattened CSS parentage. Return canonical wrappers for IDs. Do not dump
the whole document or create a retained accessibility model.

Only extend this optimization to public getters once synchronous invalidation
is guaranteed: read -> author mutation -> read must see the change within the
same callback. Cover reentrant events/custom-element reactions, adoption,
detached nodes and return from foreign-owner calls. A CDP callback is not a
mutation-free transaction. Otherwise keep public getters uncached.

Playwright traversal remains unchanged. The native primitives beneath it become
cheaper; do not recognize InjectedScript source or replay selector results.
The earlier callback-scope cache gained only 2.7% overall and bulk snapshots
regressed: this package is subordinate to initial style/geometry construction.

## Execution and integration

Start with step 1 and the first-construction portion of step 2. They can be
developed independently with explicit ownership of shared `surface.js` edits.
Step 3 depends on step 2's result boundaries. Step 4 can be a separate small
change, but cannot justify expanding scope into a general DOM rewrite.

Integrate individually validated changes into one candidate and measure the
combination: compatible gains need not be additive. Keep author scripts, CSS,
observer callbacks, input dispatch and navigation enabled at every stage. Cold
savings must come from cheaper real work, not delayed callbacks, hidden prewarm,
changed idle waits or moving initialization outside the measured interval.

## Correctness and acceptance, during implementation

No tests or performance runs are part of this planning-only turn. For each
future implementation package:

- Add focused regression tests before relying on reuse: synchronous read/mutate/
  read; ancestor and sibling changes; CSSOM and font changes; tables/flex/grid
  cycles; percentages/logical properties; shadow/slot/adoption; cross-realm
  identity; overlay insertion between input events; nested scroll/clipping.
- Run the affected browser/CDP suites, race checks and `fast_gate.py`. For the
  substantive geometry/style batch, run the full frozen benchmark gate, memory
  after teardown, allocation and multi-Page throughput checks.
- Use explicit baseline/candidate commits and fresh binary hashes. Compare five
  paired cold/warm runs of the unchanged full workload with matching conditions.
  Record Page creation, three navigations, first-visible, article/role/link,
  scrolling and input stages, not just the fastest callback.
- No large stage may regress over 10% without resolving the regression. Reject
  work displacement even if a local microbenchmark improves. Confirm Chrome
  152 behavior for uncertain cases; never weaken frozen expectations.
- Small preparatory diffs need not each yield 20%; the substantive production
  candidate must meet the original >=20% median cold **and** warm gate. This is
  an intermediate acceptance threshold, not completion of the 2/2 s objective.

Report the actual combined production result and remaining gap. If 2 s is not
achieved, retain only correct measured improvements and name the residual cost;
do not claim the destructive 2.147 s result as achieved production performance.

## Explicitly out of scope

More destructive diagnosis in this phase; a broad maintained style/layout tree;
global mutation journals; shared V8 owners; unconditional prewarm; disabling
author/observer work; fixture replay; permanent selector/role-result memoization;
blind hit-target trust; stale wrapper attributes/topology; frozen geometry.

Evidence and receipts: [current record](wikipedia-e2e-current.md),
[experimental artifacts](../../tools/performance/pocs/README.md). These remain
separate from any eventual production implementation.
