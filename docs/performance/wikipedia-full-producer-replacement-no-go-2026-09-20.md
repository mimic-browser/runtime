# Wikipedia full producer replacement: production no-go (2026-09-20)

## Decision

The attempted canonical style/geometry producer replacement is a production
**NO-GO**. It is behaviorally viable and replaces the intended internal paths,
but five alternating fresh-process runs show a stable full-E2E regression. The
experimental implementation remains isolated in
`.build/producer-replacement`; it is not merged into the production source.

This result is materially different from the earlier leaf-selector and narrow
retention experiments. The candidate implemented the complete proposed design:

- one canonical-DOM style program owned by the realm;
- compiled selector programs retained across rebuilds;
- whole-program static matching, cascade publication and exact context sharing;
- observation-scoped indexed style products consumed by computed style,
  visibility, geometry and isolated-world projections;
- a dependency-ordered normal-flow program publishing width, height, content
  height and child positions;
- one shared preorder for Flow and Taffy inputs;
- compact Taffy formatting-root ranges split at definite-width boundaries;
- bottom-up boundary extension using retained records;
- zero canonical fallback DOM walks from the Taffy compatibility encoder.

## Acceptance result

The unchanged frozen Wikipedia E2E was run with fixed binaries. Each workload
run used a fresh client process, and build order alternated by trial.

| mode | clean production control | full replacement candidate | median delta |
| --- | --- | --- | ---: |
| cold | 9543, 9408, 9776, 9573, 10050 ms | 10507, 10152, 10905, 10570, 9970 ms | 9573 -> 10507 ms (+934 ms, +9.8%) |
| warm | 6252, 6156, 6206, 6249, 6332 ms | 6506, 6552, 6443, 6641, 6615 ms | 6249 -> 6552 ms (+303 ms, +4.8%) |

Receipt: `.build/replacement-full-final5-paired-results.json`.

The candidate passed every Wikipedia run, so the regression is not a timeout,
fallback or displaced failure. It also persisted after an explicit candidate
warm-up and in multiple earlier controlled pairs.

## What the implementation proved

### Style

The native style program removed repeated stylesheet parsing on rebuild,
interned identical match sets, computed static cascade winners once, retained
compiled programs and shared exact selector contexts. The safe sharing key was
the canonical namespace/tag, selector-referenced exact attributes and interned
parent context. Sibling, structural and stateful selectors stayed on the
correct per-node path.

The mechanism worked internally:

- comparable native execution fell from about 817.4 ms to 749.9 ms;
- host style-program time fell from about 1095 ms to 930 ms;
- one 3663-node document reused roughly 994 exact contexts;
- rebuilds no longer resent 2-3 MiB stylesheet programs (`programBytes=0`).

It did not create a large E2E reduction. New Documents still require a real
static match for most nodes. Stateful/dynamic candidates still require realm
state, and the authoritative indexed style product still has to be created for
all geometry/visibility/input consumers. Publishing full cascade products
eagerly merely exchanged later JS allocation for earlier producer work.

### Geometry

The final candidate did not retain the old independent DOM walks. It built a
shared canonical preorder and projected only compact Taffy ranges at the same
formatting-root and definite-width boundaries used by observable geometry.
Nested boundaries used retained stubs/extensions and were scheduled bottom-up.

The final warm diagnostic profile recorded:

- Flow: 527 calls, 17,240 nodes, 31,173 base records, 151 ranges and 9,928
  range records;
- Taffy: 41 ranges/host calls, 9,504 records and five boundary extensions;
- `fallbackDOMWalks=0` for both Flow and Taffy;
- Flow preparation 1957.4 ms versus 106.8 ms native execution;
- Taffy preparation 573.2 ms versus 143.7 ms native execution.

Thus the proposed unified transaction successfully removed the second
canonical traversal, but not the expensive work. Creating a general shared
record and two solver projections cost more than the specialized preparations
it replaced. The dominant cost is normalization/materialization of style,
edges, text, intrinsic measurements and solver wire records, not the final
Flow/Taffy arithmetic or the number of host calls.

Profile: `.build/producer-replacement/.build/combined-plan9-profile-warm.json`.

## Why Part B's upper bound did not transfer

Part B replay restored already-built internal products. It therefore removed
both necessary producer computation and all of today's representation costs:
selector evaluation, cascade and computed-style resolution, geometry input
normalization, intrinsic/text preparation, wire-object creation, JSON transfer
and solver execution.

The production replacement could remove duplication, but it still had to
derive semantically complete products for each new Document. The measurements
show that the duplicated portion was much smaller than the replay upper bound:

- eliminating Flow's recursive legacy fallback changed `2800 -> 0`, but did
  not materially change E2E;
- document-wide batching changed roughly 500 Flow calls to 17, but increased
  visited nodes to 61k and made E2E dramatically worse;
- full native cascade publication was warm-neutral because eager publication
  replaced, rather than eliminated, declaration work;
- shared Flow/Taffy traversal saved the second DOM/style/text walk, but its
  common records and range projections added more preparation than they saved.

Therefore Part B was a valid upper bound on all current producer work, but not
evidence that the same budget was redundant or removable by retaining the
current semantic pipeline in a different graph.

## Correctness evidence

The candidate passed focused Goja and V8 coverage for selector ancestry,
sibling/structural/stateful selectors, CSSOM and mutation invalidation,
importance/`all`/pseudo cascade, SVG/MathML namespace and case handling,
computed style, isolated worlds, visibility, IntersectionObserver, scrolling,
input/hit testing, normal flow, tables, flex/grid, generated content,
percentage/absolute height dependencies, nested Taffy roots and geometry cycle
fallback. Native `dom` and `layoutflat` race tests also passed.

These gates establish that the E2E result was not obtained by stale geometry,
fake rectangles, skipped observers/input or a workload-specific shortcut.

## Consequence

Do not merge or polish this replacement as a performance feature. In
particular, do not repeat these variants under new names:

- whole-document Flow batching without selective dirty ranges;
- native selector matching layered under the existing style producer;
- eager full cascade publication;
- a shared Flow/Taffy input graph that still materializes complete JS/wire
  records;
- further safe-subset selector/context sharing as the main E2E strategy.

The experiment narrows the next architectural question. Chrome-like latency
will require avoiding a material part of the current rendering product set or
producing it in a representation whose construction does not involve millions
of JS property/function/object operations. That is the separate
automation-first/render-barrier design problem;
it should not be presented as another optimization of the current producer.
