# Mutation-aware style and geometry reuse

[Unchanged observation reuse](click-investigation-20260913.md) avoids rebuilding
style and geometry across checkpoints. Mutation-heavy input needs a narrower
reuse boundary: retain only derived data whose complete inputs are unchanged,
and rebuild observations affected by the mutation.

## Canonical inputs and bounded caches

A necessary-ancestor filter rejects impossible selectors before matching. Hash
collisions admit extra matching work, never an incorrect match; the pinned matcher
still decides each result. Static selector reuse includes exact attributes,
ancestor identities, stylesheet programs and supported environment inputs.
Sibling, structural and state-dependent selectors are re-evaluated. Dynamic
candidates retain executable selector programs rather than previous truth values.

Ordered matched rules, precise inline state and dialog defaults validate specified
declaration reuse. Style-only box data additionally validates ancestor declarations,
root font and relevant presentation attributes. Changes to sizes, child membership,
text flow, positions or availability require a fresh geometry graph. Inheritance
is not inferred from an inline-only copy of the DOM.

A lazy private projection reads parent IDs, attributes and inline state under the
canonical arena lock in one transfer. This is immutable derived data for an epoch,
not a second mutable DOM. Public observation boundaries validate that epoch;
internal hit-test batches share one observation.

Caches have bounded lifetime and size. Numeric basis caches retain at most 64
texts, four bases per text and four edge records; mutable margin records are not
shared with callers. Compact text metrics use a 1 MiB accounting budget and a
realm-owned font-collection revision. Font changes invalidate native and JavaScript
projections, and failed shaping results do not survive observations. Persistent
element/program caches use weak keys and reset on bootstrap restoration.

## Regression evidence

Focused checks cover the shared invalidation and identity boundaries:

- `TestStyleAncestorFilterMatchesCanonicalSelectors`.
- `TestStylesheetSelectorReusePreservesConditionsAndPseudoState`.
- `TestGeometryAncestorSnapshotsObserveFontAndFlatTreeChanges`.
- `TestFontCollectionChangesInvalidateGeometryWithinSameJob`.
- `TestForeignStyleProjectionInvalidatesCanonicalInputs` and
  `TestStyleProjectionCacheBounds`.
- `TestCompactTextMetricsMatchFullGlyphProjection`.

The retained stylesheet fixture was measured against frozen Chrome
152.0.7977.82, including 91 stylesheet comparisons and dynamic state/shadow cases.
Its SHA-256 is
`095b6d5c83481dd1acd3430279620edc36e13fb8c6c3f55cab7ff717d7822f93`.
The original Chrome receipt and native/Go profiles remain retained with the
measurement provenance. This reference is evidence for the fixture's observed
semantics, not general rendering or coordinate compatibility.

## Performance interpretation

A controlled local experiment measured mutation/read latency at 110.11 ms before
and 31.34 ms after, using the median of three trial medians with one excluded
warmup and 30 verified rounds per trial. Unchanged reads remained approximately
2 ms and local clicks approximately 27 ms. The compared binary hashes were
`035eff9e864902fed0653dde8bf74aefa239dd29aad3ca5305fe550633ec4ddf` and
`b5b473bf381f9c7523568c58c94038b4a414d50108dcda4c1fdc9fa6d3c16e1c`.
These are historical measurements of this mechanism, not current release claims.

The change removes repeated matching and transfer work; it does not provide
incremental layout for every mutation. Coordinate projection has its own
compatibility boundary. Network initialization, asynchronous readiness and handler
execution must be measured separately. Small sample counts do not establish
stable p95/p99 latency, and short concurrency/memory gates do not demonstrate
universal throughput gains. Use the current [performance report](report.md) and
canonical local harnesses when evaluating another change.
