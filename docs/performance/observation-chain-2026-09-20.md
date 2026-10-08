# Controlled observation/input chain — 2026-09-20

## Scope and reproducibility

This is a whole-chain comparison, not another isolated optimization or a
replacement for the frozen Wikipedia workload. No production fix is retained.
The incoming uncommitted implementation is preserved in
`tools/performance/pocs/chromium-starting-worktree.patch`. Control executable
SHA-256: `966091BED8ED5417296F1474CE626C1E67AE53BAC9BC9E4BC6963E96D99CA34D`.

Run `node tools/runtimecheck/chromium_observation_chain.cjs` from repository
root. Defaults: pinned Chrome 152.0.7977.82, current control Mimic, headless,
1280x800, both connected over CDP, 100 and 10000 divs, three trials, four
first-consumer orders (rect/style/visibility/click). Browser order alternates;
case order rotates. The target precedes fixed-height rows. There are no external
resources. A handler only increments a JavaScript counter; it does not mutate DOM.

Each fresh Page builds the fixture, performs the chosen first operation, then
runs direct geometry/style, visibility, role lookup, scroll, DOM click, mouse
click and locator click. Subsequent phases repeat after input, after an unused
data attribute changes, after ancestor width changes, and after unrelated row
text changes. Separate-command direct reads check genuinely unchanged state.
All 48 cases passed count, visibility, geometry-response and click assertions.

Main measurements: `.build/observation-chain-results.json`. Use
`node tools/runtimecheck/summarize_observation_chain.cjs` to summarize.
`EXTRA_REPEATS=1` adds interleaved clean role/visibility/scroll reads and five rAF
callbacks; three further pairs are in `.build/observation-chain-clean-repeats.json`.
`DIAGNOSTICS=1` is a separate instrumented mode, not a latency benchmark.

Important distinctions:

- First rect/style executes in the same callback as DOM construction, so Chrome
  cannot hide that first synchronous flush in an earlier rendering opportunity.
  First Playwright visibility/click necessarily executes in a later command.
- Page-local direct-read time excludes protocol round trips. Action timings are
  controller wall time. Do not mix them into an exclusive CPU breakdown.
- Phase totals overlap their component stages. Full-chain totals are measured
  separately, include Page creation/navigation/setup, and exclude teardown.
- Text mutation is unrelated to the target's intrinsic size. It is not a test
  of correctly updating a target whose own text changes.
- `after-input` is not a clean-read baseline: preceding input can change hover
  and focus. Only separate repeats and the added clean sequence test reuse.
- Process-first Page is labelled explicitly; it is not Wikipedia's cold/warm
  definition. The synthetic chain contains many more actions than Wikipedia.

## Results

Milliseconds, medians. The 10000-element stage rows combine the four first-order
variants where the stage is common; first-consumer rows have three samples.

| Observation | Mimic | Chrome 152 |
| --- | ---: | ---: |
| Complete chain, 100 divs | 697.05 | 228.49 |
| Complete chain, 10000 divs | 2760.11 | 304.82 |
| First synchronous rectangle, 10000 divs, page-local | 349.30 | 11.40 |
| First scalar display read, 10000 divs, page-local | 7.40 | 4.90 |
| First Playwright visibility, 10000 divs | 522.89 | 23.95 |
| First Playwright click, 10000 divs | 777.77 | 39.26 |
| Clean repeated rectangle, page-local | ~0–0.1 | ~0–0.1 |
| Rectangle after unrelated data attribute, page-local | 142.50 | ~0 |
| Rectangle after ancestor width change, page-local | 241.35 | 4.60 |
| Rectangle after unrelated text change, page-local | 142.90 | 1.75 |
| Mouse click after initial input, controller wall | 4.41 | 0.85 |
| Locator click after initial input, controller wall | 56.60 | 13.50 |

Changing first-consumer order does not eliminate the full-chain work: Mimic
10000-div medians were 2721/2753/2765/2766 ms for rect/style/visible/click first;
Chrome was 305/312/303/308 ms. Earlier observation mostly changes who pays.

The hypothesis that Mimic rebuilds geometry on *every* unchanged getter is
disproved for this fixture. Its retained geometry works. The hypothesis that
Chrome's first calculation is similarly expensive is also disproved here.

## Verified chain mechanisms

### Broad reconstruction after irrelevant mutation

The diagnostic first rectangle read requested 10005 `styleObservationState`
records and materialized 10005 `nodeData` records. After changing a data attribute
not referenced by any stylesheet, the next rectangle again requested 10005
style-observation records, although the target rectangle did not change. Clean
repeats requested neither. This establishes over-broad reconstruction, not the
absence of caching. The connected-mutation invalidation is in
`internal/webapi/surface.js:1177`.

### Consumers request different document-wide projections

The removable Go trace patch records actual projection arguments, epoch and
cache hit/miss. `.build/observation-chain-projection-trace.json` confirms:

1. Visibility requests `[cursor,display,visibility]`: document projection miss.
2. Role/accessibility requests `[content,display,visibility]`: another miss in
   the same document epoch, with 10007 owner `isConnected` calls.
3. Scroll requests cursor again: owner-cache hit.
4. Initial input changes document epoch; subsequent projections miss again.
5. An unrelated data mutation also invalidates these projections.
6. With no further mutations, alternating role/visibility still requests the two
   document projections, now as hits: per-element property maps are replaced,
   not merged (`css_computed_values.js:201`). The caller still parses/rebuilds the
   returned document-sized maps. Clean role is ~23–24 ms and visibility ~10–15 ms
   on Mimic, versus ~3–4 ms and ~1.5–1.8 ms on Chrome.

This distinguishes owner recomputation from the cost of consuming a cache hit.
It is not evidence that raw Go/V8 transport alone explains the Wikipedia gap.

### Input contains legitimate stability waits as well as computation

Raw mouse input is cheap once settled; locator input is not the same operation.
Playwright waits for stable rectangles through rAF before scrolling/clicking.
Five callbacks in this environment took about 100 ms on Mimic versus 18–20 ms
on pinned Chrome: ~20 ms versus ~4.2 ms intervals. Mimic uses a 16 ms rendering
timer and a 10 ms CDP pump ticker. Chrome's measured cadence is environment-
specific, not a universal promised frame interval. Frame waits explain part of
the controller latency, but cannot explain seconds of first layout work.

## Reconnection to full Wikipedia

Fresh, unchanged Chrome 152 E2E launches: 1507/1250/1395 ms. The quiet paired
Mimic control from the PoC series: 10071 ms cold / 6062 ms warm.

`node tools/runtimecheck/run_wikipedia_chain_diagnostic.cjs` runs the existing
diagnostic Wikipedia companion twice in one instrumented process. Both passed.
Raw receipts: `.build/wiki-chain-{cold,warm}.json`, their workload logs, and
`.build/wiki-chain-server.log`. Diagnostic times are NOT acceptance timings.

| Diagnostic observation | cold | warm |
| --- | ---: | ---: |
| Total elapsed | 11191 ms | 6994 ms |
| Union of scheduler task intervals | 5420 ms | 1560 ms |
| Sum of foreground callFunctionOn intervals | 4640 ms | 4429 ms |
| Tasks with repeated stylesheet + >=8 table-row query signature | 10 | 4 |
| Time inside those signature tasks | 2954 ms | 572 ms |

Scheduler keys must include realm + taskId; taskId alone collides across realms.
Scheduler and foreground intervals overlap slightly (~47/7 ms); these columns
must not be added as exclusive CPU categories. The cold/warm difference is
predominantly additional page-task work, not a several-second slowdown of the
same Playwright callbacks. The repeated style/table signature is consistent
with the known observer/geometry path; it is not exclusive selector CPU time.

The Wikipedia projection log independently reproduces cursor/content batches,
cache hits on a return to a previous property set, and fresh batches after
document epochs change. Cold JavaScript-article epochs progress during the
workflow; warm holds one epoch across more consecutive observations. This
connects the controlled lifecycle effects to the real workload without claiming
that one synthetic optimization accounts for the entire gap.

## Decision

Do not revive “one missing cache” or claim that a reordered first read fixes E2E.
The measured requirements are: cheaper first construction; reuse across the
actual dirty dependency scope; and inexpensive consumption of the same derived
state across CSSOM/geometry/isolated-world consumers. Cold additionally requires
preserving those results across the observed sequence of page tasks. Scheduling
cadence is a separate, smaller contribution.

No semantics-preserving 4 s Wikipedia result has been established. The four
earlier small PoCs combined improved only about 5% and were removed. Fixture tape
artifacts remain unvalidated and were not executed in this whole-chain comparison.
