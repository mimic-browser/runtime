# Navigation, geometry and automation stall investigation

Baseline: `f44825bd9271fcbd031a9d88ccb406215af0a749`, freshly built before
production edits as `.build/mimic-stalls-before.exe`, SHA-256
`4ea8767cec0b5b7f6cb75c9a5a4e37c5b9c70b3c2b4517355cca6c0ab6454730`.
The earlier live comparison and its executable/raw receipts remain unchanged.

The measured optimization build is `.build/mimic-stalls-final.exe`, SHA-256
`34999b4ec79404ef9a712444e681208d39fc8158761c7b511d108be36565a874`.
Its [source manifest](../../.build/stalls-final-source-manifest.json) and
[source archive](../../.build/stalls-final-source.zip) preserve that checkpoint.
The main paired campaign uses diagnostic harness SHA-256
`74d5f8cc0f315830483fda15483052e291f72591b190430acadcf609e2d243d2`.
Later retention/cancellation safeguards, if present in the working tree, are
identified separately below; these measurements must not be silently attributed
to an executable with another hash.

## Paired live results

Five alternating before/after runs per site, fresh processes and Contexts,
preview off, no profilers. Times are median / nearest-rank p95 in seconds;
with five samples p95 is the maximum, not a stable tail estimate.
Generic content and DCL start at navigation; ChatGPT absolute times include
process startup and setup. [Campaign summary](../../.build/latency-final-main/summary.json)
links the independent per-trial receipts.

| Observation | Before | Optimization build | Valid before / after |
| --- | ---: | ---: | ---: |
| ChatGPT visible login button | 3.583 / 4.823 | 1.460 / 1.854 | 5 / 5 |
| ChatGPT click start to confirmed `:modal` | 1.339 / 1.411 | 0.778 / 0.831 | 5 / 5 |
| ChatGPT complete path to dialog | 4.946 / 6.162 | 2.234 / 2.679 | 5 / 5 |
| GitHub first content | 31.050 / 32.404 | 0.981 / 1.950 | 5 / 5 |
| GitHub DOMContentLoaded | 28.574 / 29.780 | 17.953 / 19.868 | 5 / 5 |
| SpigotMC DOMContentLoaded | No successful initialization | 11.074 / 11.288 | 0 / 5 |
| Modrinth first content | 28.689 / 29.094 | 1.102 / 1.864 | 5 / 5 |
| Modrinth DOMContentLoaded | 28.662 / 28.932 | 5.741 / 6.567 | 5 / 5 |
| React DOMContentLoaded | 3.323 / 3.483 | 3.134 / 3.266 | 5 / 5 |
| Wikipedia DOMContentLoaded | 1.261 / 1.362 | 1.118 / 1.141 | 5 / 4 |

SpigotMC baseline reached the 40-second execution deadline in all five trials;
the resulting text without a successful main DCL is a failed initialization.
All five optimized runs completed DCL without initialization errors. Background
JavaScript still delayed subsequent commands: total confirmation was
14.34–22.40 seconds, including the harness's required three-second confirmation
interval. This is not a claim that all Spigot tasks now finish immediately.

GitHub's first content is available before initialization completes. Both builds
retain existing bare-module/import-map compatibility errors (including asset
requests resolving `react` and related bare imports to unavailable URLs) and
three failed script assets. The content/DCL checks do not establish full GitHub
application functionality. Both ChatGPT builds retain an octane-shell
`TypeError: a is not iterable`; the measured real login-button click and modal
succeeded in every trial, but subsequent authentication was not tested.
Modrinth's five optimized trials had no initialization errors.

Wikipedia optimized trial 5 failed its main-document request after 10.001 seconds
with `Network.loadingFailed: context deadline exceeded`, before response headers
or commit. The page remained `about:blank`; 234 sequential CDP probes still
answered (median 2.825 ms). The 60-second trial duration was the harness waiting
after this network failure, not evidence of a locked Page. Available evidence
does not separate DNS, TLS and server waiting. The failed trial remains in the
denominator and is not replaced by a successful retry.

### Real Pyppeteer click phases

Medians in milliseconds, same five paired runs. The measured client workload is unchanged. `button.click()` uses the visible element, scrolling/visibility
observation, content quads and actual mouse input, followed by checking both
`dialog.open` and `:modal`.

| Phase | Before | Optimization build |
| --- | ---: | ---: |
| Visible-button query | 49.76 | 24.43 |
| IntersectionObserver / scroll | 58.15 | 65.04 |
| Content quads | 73.11 | 52.40 |
| Mouse moved | 166.90 | 90.65 |
| Mouse pressed | 221.78 | 82.65 |
| Mouse released | 166.21 | 77.34 |
| Complete `button.click()` | 692.15 | 367.30 |
| Subsequent modal observation | 645.24 | 411.52 |

Subphases are nested in `button.click()` and must not be added again. Modal
observation includes scheduling and repeated observation, not just the site's
event handler CPU time. IO's frame-sensitive cost did not improve in this sample.

The separate [instrumented click](../../.build/latency-final-click-trace)
uses the optimization binary, despite the single-binary harness variant label
`before`. In its document capture/bubble listeners, click propagation spanned
3.6 ms. A MutationObserver observed `open` and `:modal` 181.4 ms after capture;
this is an observation boundary, not exact time spent in a site handler.
The dialog poll spent 8.97 ms waiting for Page ownership and 371.82 ms in the
remaining awaited work, with no command queue/session delay. It overlapped
223 scheduler tasks (160 network, 25 DOM, 19 timer and others), totaling
329.57 ms, with the largest network/DOM tasks 138.35/56.92 ms. Remaining modal
latency therefore includes page initialization/tasks and promise observation;
it is not all selector or input-handler cost. These instrumented numbers are
kept separate from the five-pair unprofiled table.

### Retained Context and preview

Six alternating trials per variant/preview mode retain one process and Context.
The seed (`reused_process_and_context=false`) is excluded, leaving five measured
reused Context trials per cell. All 20 reused trials confirmed DCL and `:modal`.
The [raw campaign](../../.build/latency-final-retained) retains the seeds; its
unfiltered `summary.json` must not be substituted for the filtered table below.

| Preview | Observation | Before median / p95 (s) | Optimization median / p95 (s) |
| --- | --- | ---: | ---: |
| Off | Button | 0.947 / 1.569 | 0.568 / 0.612 |
| Off | Click to modal | 0.925 / 0.952 | 0.693 / 0.722 |
| Off | Complete path | 1.888 / 2.454 | 1.258 / 1.263 |
| On | Button | 1.404 / 1.806 | 1.363 / 1.497 |
| On | Click to modal | 1.025 / 1.153 | 0.938 / 1.172 |
| On | Complete path | 2.422 / 2.831 | 2.376 / 2.668 |

Preview is an actual subscriber opened in pinned Chrome 152, with Mimic's
`-dev-preview` enabled. Its Chrome process is launched anew per trial. Absolute
button/total times include that launch (median 402.7 ms before versus 600.3 ms
after for these retained-preview trials), so the small total-time difference
with preview is not a clean measure of Mimic CPU improvement. The octane error
described above still occurs in retained Contexts.

The separate five-pair clean-Context preview campaign also opened the dialog
in all ten trials. Median / p95 seconds: button 4.109 / 4.264 before versus
1.830 / 1.976 optimized; click-to-modal 1.488 / 1.648 versus 0.987 / 0.997;
complete path 5.597 / 5.719 versus 2.827 / 2.973. DCL was 3.204 / 3.286 versus
0.992 / 1.137. [Clean preview receipts](../../.build/latency-final-preview/summary.json).

## Confirmed causes

1. Navigation held the Page command boundary while waiting for document,
   parser script, stylesheet and module network responses. Parser continuations
   now return to the Page event loop while waiting. Immutable network results
   commit on that loop only if the navigation/document still owns the work.
2. Static module linking and dynamic import previously fetched from inside V8
   callbacks. Graph preparation now discovers native module requests and fetches
   them before linking. Dynamic import settles after the evaluation promise,
   preserving top-level await, namespace identity and one evaluation per module.
3. Geometry repeatedly copied complete declaration arrays and re-read the same
   ancestors/attributes. Private read-only derived data is shared within one
   synchronous observation. Attribute-selector indexing excludes unrelated
   elements before the full matcher runs; it does not replace matching semantics.
4. Every box length eagerly resolved the root font, even for absolute pixel
   lengths. Resolving that root through the public `documentElement` getter
   repeatedly enumerated live child collections. Unit bases are now lazy and
   the required root is obtained from canonical DOM membership. Changes to root
   font size remain visible to the next observation in the same JavaScript task.
5. Cloning an image restarted fetch/decode even when that document already had
   the successful image. Chrome 152 reuses it even with HTTP `no-store`.
   Successful document-owned images now share decoded data by request/CORS key.
   Detached unloaded lazy images defer fetching; ordinary detached `new Image`
   loading remains supported.
6. Repeated identical text measurements re-entered the native shaping engine
   and serialized identical glyph arrays. A realm-owned cache now retains only
   successful immutable results, keyed by all eight shaping inputs. Its FIFO
   bounds are 1,024 entries and approximately 1 MiB including keys/results.
   Changes to the actual font collection invalidate it; retired realms release it.
7. Reading an element's width or height reconstructed ancestor coordinates and
   unrelated sibling flow. Dimension-only observations now use the existing
   width/size calculations. Coupled table, shadow, replaced/control and foreign
   realm cases retain the complete path. Actual bounding rectangles still
   calculate positions.
8. CSS text measurement transferred and parsed full glyph arrays although it
   only consumes advance/ascent/descent/lineGap. A private compact projection
   keeps authoritative shaping and the same font invalidations, but serializes
   and retains only those four aggregates. Canvas/SVG full results are unchanged.
9. Ready network continuations only woke the CDP pump on its 10 ms ticker.
   A coalesced Page-owned notification now wakes the same bounded pump. Future
   timers keep the ticker and canonical clock accounting; no extra event loop
   or mid-JavaScript unlock is introduced. Pause/resume preserves notifications.

Final review added two safeguards after the main measured optimization build:

- Child DOMContentLoaded's microtask checkpoint now uses the active event task
  context. A Promise reaction previously inherited the document lifetime, so
  an infinite reaction could ignore a snapshot interrupt. This preserves the
  existing single-loop ownership and does not unlock JavaScript mid-execution.
- The document available-image list now has FIFO bounds of 32 MiB (pixel backing
  capacity plus accounted key/entry overhead) and 1,024 entries. Eviction releases
  only its extra reference; active IMG nodes keep their decoded result.
  Oversized images still load normally but receive no additional reuse-cache
  entry. HTML explicitly permits available-image eviction to save memory;
  these limits are Mimic's policy, not a claimed Chrome eviction threshold.
  [HTML available-image list](https://html.spec.whatwg.org/multipage/images.html#the-list-of-available-images).

Persistent DOM/geometry caching was deliberately not introduced: form values,
synthetic shadow state, fonts and author-overridden getters make a DOM revision
alone insufficient to establish that an observation is unchanged.

## Diagnostic evidence (not unprofiled speed claims)

The original GitHub profile measured 30.19 s navigation and recurring 2.88–3.02 s
background turns, while the selector itself took 1–2 ms. It recorded 9,689,476
`parentNode` and 3,284,791 `getAttribute` host calls. The first transient-read
change reduced those to 2,412,366 and 1,121,156, respectively; recurring turns
were 1.61–1.67 s. These runs include profiling overhead and different live
responses and are not the final before/after benchmark.

The SpigotMC profile identified an 80.26 s DOMContentLoaded callback: 253,658
text-shaping calls, 2,517,560 `documentRootID`, 5,033,930 `nodeChildAt` and
7,550,886 `nodeChildCount` calls. An intermediate CDP diagnostic showed a DOM
read waiting 34.96 s for the Page, then taking 19 ms of remaining handler time.
The callback was terminated at the configured navigation deadline. Receiving
content after that deadline is not evidence that initialization succeeded.

Profiles and intermediate receipts are retained under `.build/`:
`stall-profile-github-before`, `stall-profile-github-geometry`,
`stall-profile-spigot-current`, and `stalls-candidate-spigot-diagnostic`.
CPU, allocation, heap, block, mutex and native JavaScript profiles distinguish
computation from network/lock waits; profiling is opt-in.

Direct evidence: [GitHub baseline profile](../../.build/stall-profile-github-before),
[GitHub transient-read profile](../../.build/stall-profile-github-geometry),
[Spigot expensive callback profile](../../.build/stall-profile-spigot-current),
[Spigot queued-command diagnostic](../../.build/stalls-candidate-spigot-diagnostic).

The width/text-cache checkpoint (`6f2a09fbada363d7c85e3d61730fd71d7035365a3b3e2d29fc0ef09d6b4fa8ba`,
preserved as `mimic-stalls-width-checkpoint.exe`) completed Spigot initialization:
DCL 20.415 s, no execution deadline. Its separate native-only profile still
identified a 17.06 s callback. `offsetHeight` accounted for approximately 8,700
of 38,092 samples; compact text and height-only changes follow this checkpoint.
This single diagnostic is not the final paired live comparison.

The compact projection control uses 128 different strings, two passes, one
excluded warm-up and 30 alternating rounds. Full/compact median is 27.873/7.085 ms,
p95 30.584/11.747 ms. Transfer volume is 3,969,492/14,744 bytes per 256
observations; retained entries are 66/128 versus 128/128 under the same cache
limit. The full glyph working set exceeded the byte limit and evicted entries.
Receipt: `.build/text-metrics-projection-measurement.json`.

## Local geometry control

One excluded warm-up and 30 measured operations per build, same generated
200-element DOM with 80 attribute rules. Read operations sample four separated
elements; mutations change geometry and validate the changed result; the click
uses real Pyppeteer input. Values below use `geometry_summary`, which excludes
warm-up, rather than the all-phase diagnostic medians.

| Operation | Before median / p95 (ms) | Optimization median / p95 (ms) |
| --- | ---: | ---: |
| Read four elements | 637.75 / 702.13 | 176.72 / 206.47 |
| Mutate and observe | 808.89 / 898.82 | 219.21 / 243.59 |
| Real click | 495.09 / 555.45 | 156.91 / 182.76 |

[Before receipt](../../.build/latency-final-geometry/geometry-before-clean-preview0-r1/result.json),
[optimization receipt](../../.build/latency-final-geometry/geometry-after-clean-preview0-r1/result.json).
The separate all-200 cold rectangle read took 30.444 seconds before and
8.052 seconds after in one run each. This proves the local 30-second case is
reduced without waiting for a timeout; it does not establish a 30-sample tail
distribution for the full stress operation. [Stress receipts](../../.build/latency-final-geometry-stress/summary.json).

## Fast-workload regression investigation

Two independent fast gates confirmed a small-page latency regression after
asynchronous navigation: static completion 38.28/41.22 ms before versus
60.19/60.51 ms after; React 107.23/98.00 versus 127.21/125.54 ms. Ready-task wake
restored React completion to 107.18 ms, but static remained 60.22 ms.

The remaining static cost is additional observable work: the frozen runner's
first readiness evaluation now executes in the still-current initial empty
document, materializing its deferred runtime. Commit then creates the destination
runtime. Before this change the Page lock delayed that evaluation until commit,
so the initial runtime was never constructed. Repeated warm static rows show
document-to-script request intervals increasing from 20–24 to 42–44 ms and CPU
from 31.25 to 46.875–62.5 ms. The instrumented local replay shows the first
evaluation consuming old-document execution time, followed by a second one
waiting for destination initialization. It does not justify skipping arbitrary
JavaScript or recognizing the benchmark's readiness expression specially.

DOM execution also measured approximately 158–159 versus 167–169 ms in the
repeated fast gates. A separate event-readiness control retained the unchanged
DOM workload, context policy and unique origin per Page, but waited for the
matching frame/loader `load` event before issuing any JavaScript. With one
excluded warm-up and 30 alternating measured pairs, execution median was
169.06/170.31 ms (+0.74%), p95 186.07/192.74 ms; completion median
205.00/209.86 ms (+2.37%). Execution CPU median was 0.3203/0.3125 seconds,
including GC/native threads, and peak private memory medians were identical.
This did not reproduce the >5% execution regression when early empty-document
evaluation was removed. It supports a pipeline/setup interaction rather than
a general slowdown of the DOM workload, but does not identify every individual
CPU sample in the frozen gate. The original frozen regression remains recorded;
this distinct diagnostic does not replace or alter it.

Control: [raw results](../../.build/dom-event-readiness-results/raw.json),
[runner](../../.build/dom-event-readiness-control.py), SHA-256
`70c02c1e7f2f0bf9c84668a28896ef1a477b4dabb1d848cb1f82cc46a119dd57`.

Receipts: `.build/stalls-fast-before`, `stalls-fast-before-recheck`,
`stalls-fast-final`, `stalls-fast-final-recheck`, `stalls-fast-wake-final`, and
`stalls-static-diagnostic`. The first two build an isolated clean checkout of
the baseline revision (binary `161b79b45192c42ed8d8980a9a89656510d2f6706049490c60d95f17c79ff5e5`).
The live campaign uses the original freshly built baseline recorded above.

Throughput and memory costs also remain material. The repeated baseline gate
measured median static throughput 68.92/65.71 sessions per second at concurrency
10/25, versus 39.89/43.40 with the ready-wake optimization build. Recovered RSS
for those waves was 221.6/417.2 MiB before versus 328.7/706.5 MiB after. The
single 10-Page static memory wave was 482 MiB active, 129 MiB after teardown and
recovery before; 522.5 MiB active, 158.1 MiB after teardown and 149 MiB after
recovery in the optimization build. React was 511.6→142.2 MiB versus
551.9→171.1 MiB. These snapshots include allocator reservations and execution
history; they do not alone prove a leaked Page. Extra runtime materialization
and retirement is confirmed, while exact attribution of all retained RSS is not.

## Measurement protocol

`tools/performance/live_latency.py` uses an unchanged client workload, a US locale
profile and a visible button handle with real Pyppeteer click.
It separates connection/context setup, navigation, selector, IntersectionObserver,
content quads, input events and modal observation. Generic content probes are
read-only; at most one DOM probe is outstanding and a timeout ends the trial.

Startup totals include launching a fresh browser. The attach-only client workload
uses an existing process. Initial exploratory startup detection incurred
an approximately 500 ms Windows connection-refusal delay; bounded probes removed
that measurement artifact. Those exploratory totals must not be pooled with
the final paired campaign.

The local geometry control contains 200 elements and 80 attribute-selector
rules. The bounded workload samples four separated elements and runs one warm-up
plus 30 measured read/mutation/real-click iterations. The separate stress case
reads all 200 once: the baseline took approximately 32 s per operation, so
repeating that full stress 30 times would spend about 16 minutes on one case.
These are distinct workloads and their timings are not pooled.

## Network-wait command responsiveness

[Final local receipt](../../.build/stalls-network-final.log) runs seven held
resource cases: document, location navigation, form navigation, parser script,
stylesheet, module entry and iframe stylesheet. Each excludes one warm-up and
measures 30 sequential commands, all completing before the fixture releases
the resource. Six medians were approximately 0.51 ms; the document case rounded
to zero at the test clock's resolution. Observed p95 was at most 1.443 ms,
below the 100 ms target. This is a loopback-server command check on
this machine, not a promise about a Page running long JavaScript.

The image replay reduced seven sequential no-store clone requests to one,
matching the pinned reference while preserving eager detached `new Image()`.
[Chrome image oracle](../../internal/browser/testdata/image_reuse_chrome152.json),
[before replay](../../.build/image-reuse-before.json),
[after replay](../../.build/image-reuse-after2.json).

## Additional live compatibility observations

One paired run per URL, preserved in the [additional campaign](../../.build/latency-final-extra/summary.json).
These are smoke checks with live-network variance, not a performance distribution.

| URL/scenario | Before / optimized first content (s) | Observed result |
| --- | ---: | --- |
| Amiibo demo | 0.851 / 0.606 | HTTP 200, same extracted text and 12 elements |
| TodoMVC | 1.110 / 1.536 | HTTP 200; CDP text/Enter creates exactly one matching item |
| ScrapingCourse Cloudflare laboratory | 14.346 / 11.671 | Both transition from challenge 403 to success 200 |
| Iroshop `/mimic-e2e` | No content success | Both clear challenge to application HTTP 404 |
| Iroshop `/` | 1.067 / 5.819 | Both HTTP 200, no observed challenge |

The laboratory response initially carried `cf-mitigated: challenge`, then the
explicit success page. This preserves the previously observed challenge result
for this one site/IP/session combination, not universal Cloudflare clearance.
Iroshop's obsolete route remains a failed content scenario despite challenge
clearance. Its home page's request-to-response interval was 226 ms before and
5,402 ms after, accounting for most of the apparent slowdown; a single pair
does not establish a runtime regression. Amiibo's live smoke requested one
image per build; repeated-image correctness comes from the separate local
clone/image oracle, not from this smoke.

## Correctness coverage and test receipts

The broad `go test ./... -count=1` invocation is retained in
[the original test log](../../.build/stalls-final-correctness.log). It did **not**
exit successfully: the browser package exhausted its default ten-minute package
budget upon reaching `TestWebGLQueryExtraFrozenChrome`, with no preceding
assertion failure. The remaining 41 browser tests were then run with a larger
package budget and passed in 56.48 seconds; the already completed tests were
not needlessly repeated. [Remaining-browser log](../../.build/stalls-browser-remaining.log).
Other packages passed except one CDP detach test whose old implicit readiness
assumption was corrected after a [pinned Chrome observation](../../.build/stalls-detach-oracle.json).
The test now waits for the exact document URL and complete state, preserving
and extending its original content assertions. It does not weaken reference
expectations or wait by sleeping an arbitrary duration.

Subsequent complete CDP runs passed (31.34 and 31.36 seconds), as did focused
late-change tests for ready-work wake/pause/resume, clocks, CSS computed values,
full box graph, dimension projections, mutations, font invalidation, module
identity/TLA/cancellation, and image reuse. Late safeguards receive their own
final tests; a split coverage result is not represented as a successful exit
of the earlier timed-out command.

Regression tests cover static module dependencies, dynamic imports and top-level
await, module rejection/cycles/identity, parent-child realm identity, replacement
navigation, interrupted main/child scripts, snapshots and close, Page isolation,
timer clock accounting, cache invalidations, same-task DOM changes, and image
reuse/lazy detached behavior. The immutable frozen harness/reference files and
original baseline datasets are verified by the gate wrappers before launches.

## Compatibility boundaries

Focused Chrome 152 observations cover image reuse, lazy detached images,
geometry mutation/unit bases and parser resource barriers. During a held main
document response, Chrome did not answer the same-session evaluation in the
two-second oracle window. The first checkpoint acknowledged navigation early;
the completed candidate acknowledges commit while retaining explicitly concurrent
old-document responsiveness. Do not claim identical unscoped evaluation timing. Direct Go navigation/evaluation retains its synchronous embedding
contract; CDP and browser-scheduled work use continuations.

Classic script `async`/`defer` ordering and stylesheet-only DOMContentLoaded
accounting remain pre-existing compatibility boundaries; this investigation
does not certify them as implemented. Pending identical image requests are
not yet coalesced; completed images are reused. Connected lazy-image viewport
heuristics are outside the detached-image fix.

The dimension oracles preserve known existing limitations rather than silently
changing unrelated behavior: fractional `offsetWidth` rounding, computed width
of an empty inline element, and the default UA button box-sizing differ from
Chrome in some cases. The explicit supported content-box/control cases remain
covered; this optimization does not claim those older differences are fixed.

Cancellation checks include infinite inline/external child-frame scripts,
interrupting snapshots and target closure. Child parser continuations inherit
the active Page task context, in addition to their navigation lifetime.

The diagnostic harness rejects content that appears after an execution deadline
or without a confirmed main-document DOMContentLoaded. Such rows retain their
content observation time and failure details but are excluded from successful
latency summaries. Click subphases are nested inside the complete click phase;
they must not be added to that phase a second time.

## Continued optimization after the first checkpoint

The executable called `mimic-stalls-final.exe` above is the first measured
checkpoint, identified by SHA-256 `34999b4ec79404ef9a712444e681208d39fc8158761c7b511d108be36565a874`.
Work continued after it; its measurements must not be attributed to later
sources or binaries with similar filenames.

The next profiles identified redundant persistent V8 invocation handles and
repeated full text shaping. Read-only document/viewport/text hosts now use
transient invocations. Aggregate CSS metrics use the same font selection and
HarfBuzz shaping while omitting unobserved glyph extents and glyph objects.
A Page-owned, bounded shaping scratch buffer avoids rebuilding its immutable
plans for every short text run. The separate 30-iteration, 128-string shaping
benchmark measured full fresh / aggregate fresh / aggregate reused at
4.377 / 3.995 / 3.067 ms, with 7.147 / 4.696 / 0.598 MB allocated per batch
(`.build/stalls-round2-text-benchmark.log`). This microbenchmark is not a live
navigation speed claim.

The opt-in text-cache diagnostic then proved a count-limit problem: 1,694
successful keys occupied about 490 KB, within the existing 1 MiB byte budget,
but the separate 1,024-entry cap caused 35,586 evictions. The entry ceiling is
now derived from the same byte budget and fixed entry-overhead allowance;
the budget is unchanged. A subsequent profile recorded 35,090 compact hits
out of 36,783 calls, 1,693 distinct compact keys and no eviction. The profile
wall time did not improve proportionally; these counters establish removal of
redundant work, not an end-to-end speedup. Evidence:
`.build/stalls-profile-round3-spigot/text-cache.json` and
`.build/stalls-profile-round4-spigot/text-cache.json`.

An additional selector-result/program cache was tested and removed. Its
three-pair Spigot DCL medians were 7.712 versus 7.751 seconds, while the local
30-repeat geometry control did not improve. Another task was running related
profiles during this checkpoint, so these measurements are exploratory rather
than a clean final performance comparison. The simpler implementation remains.
The shared canonical ancestor/children snapshots from the preceding change
are retained.

The static-workload regression was traced to an eagerly acknowledged navigation:
a subsequent readiness evaluation could instantiate the old empty document's
V8 runtime before the destination runtime. `Page.navigate` now waits for
document commit, outside Page and session locks. An explicit old-document read
can still run concurrently while the main response is held. Pinned Chrome 152
confirms the commit acknowledgment boundary and `net::ERR_ABORTED` on stop or
replacement (`.build/stalls-commit-ack-oracle.json`). Chrome's unscoped parallel
evaluation waited for the destination in that oracle; identical unscoped CDP
evaluation timing is not claimed. Separate tests verify observed old-realm
identity and that an unobserved initial runtime stays deferred through commit.

Concurrent Playwright work in the same checkout contributed the initial
revision-based observation cache. This batch completed its invalidation and
restricted retention to the top main realm, through the next microtask only.
DOM/resource/viewport/media/scroll and selector-state changes revalidate the
cache; CSSOM, synthetic shadow membership, dirty form state and font collection
changes invalidate the canonical document epoch. Child, isolated and mixed
foreign projections keep synchronous reuse. Canonical form reads also fix the
underlying `:checked` alias bug without consulting author-overridden getters.
Two new same-job scenarios match pinned Chrome 152 exactly; related geometry,
foreign-realm, CSS catalog, viewport and trusted-input tests passed. Evidence:
`.build/style-observation-epoch-chrome152.json`,
`internal/browser/style_observation_epoch_test.go`, and
`internal/browser/font_geometry_epoch_test.go`.

The height projection now skips descendant text/flow when a non-percentage,
definite height supplies the answer. It does not insert an incomplete box into
the full-flow cache. Percentage heights, tables, replaced elements and shadow
cases keep their full dependency path. The pinned height oracle and a regression
that reads height before child rectangles and then mutates both passed in goja
and V8 (`.build/stalls-height-font-final.log`).

Stop-loading now cancels active logical document requests through the Page-owned
network loader, without replacing realm contexts. The pinned oracle verifies
transport cancellation of stylesheet, static-module dependency, fetch and XHR;
worker requests survive, and later fetch/import still work. A stopped module
keeps its failure in the module map. Parser continuations cannot restart after
stop; no synthetic DCL/load is emitted. XHR observes `abort`, and CDP reports
`canceled: true` with `net::ERR_ABORTED`. A separate regression covers cancellation
immediately before parser-handle handoff and immediately after commit acknowledgment.
Evidence: `.build/stalls-stop-resources-oracle.json`,
`.build/stalls-stop-resources-tests.log`, and
`internal/browser/navigation_cancel_handoff_test.go`.

The separate title correction was committed by the concurrent task as
`2c11d04`; its protocol support declarations are in `4309e7b`. The original
performance baseline remains unchanged.

### Last measured fast-gate checkpoint

The checkpoint executable `mimic-stalls-release-20260913.exe` has SHA-256
`9aed84f1aa9826b2c2123ffbb6611f0116c8974e27ef1a3d9d374ce9c2e65d54`, built on
HEAD `4309e7b6abc4a987d51ded709c2694191c42216c` plus the recorded working-tree
changes. This predates the final shadow-stylesheet and isolated-owner corrections;
its filename does not identify the final committed source. Its fast gate is
`.build/stalls-fast-release-final/`. A contemporaneous
unchanged baseline gate is `.build/stalls-fast-before-late/` (clean original
revision, binary `33b693bf15790659ed677040f598aa963d22c6738486a1b44721bac700145788`).

| Frozen fast observation | Fresh baseline | Release candidate |
| --- | ---: | ---: |
| Static completion median, 5 measured | 37.691 ms | 38.905 ms |
| DOM completion median, 5 measured | 218.412 ms | 199.611 ms |
| React completion median, 5 measured | 99.440 ms | 89.367 ms |
| Static throughput, 10 Pages, median of 3 waves | 52.651/s | 70.254/s |
| Static throughput, 25 Pages, median of 3 waves | 69.977/s | 69.346/s |
| Static recovered RSS, 25 Pages, median | 423.60 MiB | 421.71 MiB |

The intermediate 17.46% throughput loss at 25 Pages did not persist in the
completed candidate. The historical DOM execution comparison was affected by
machine conditions: the later unchanged baseline measured 179.23 ms instead
of the earlier 158 ms, while the adjacent commit-ACK checkpoint measured
181.81 ms (+1.44%). Five-observation fast gates are regression controls, not
stable p95 estimates; earlier frozen/live distributions are recorded separately
with their own checkpoint hashes.

### Final corrections and verification boundary

The broad `go test ./... -count=1 -timeout=25m -v` run recorded 871 passing
top-level tests and two failures in the browser package. All other 19 tested
packages passed, including CDP (34.601 s). The failures were shadow stylesheet
visibility in `TestComputedStyleUsesContainingShadowRootStyleSheets` and
`TestStyleRuleIndexRevalidatesCanonicalState`: checking native connectivity
alone omitted canonical synthetic shadow ancestry. The correction preserves
the native-document fast path and avoids author-overridden getters. Both failing
tests and the related catalog, epoch, flat-tree, lifecycle and foreign-realm
checks then passed (15.727 s). Evidence:
`.build/stalls-release-all-correctness.log`,
`.build/stalls-release-correctness-summary.json`, and
`.build/stalls-shadow-fix-validation.json`. The latter records focused tool
results; separate raw logs for those focused commands were not retained.

A final real Playwright GitHub navigation plus accessibility snapshot exposed
a 30-second timeout in the isolated utility world. Commit `57944ec` routes its
style, geometry and private visibility observations through the canonical main
owner, initializing the deferred owner when needed and releasing temporary
engine values. Child-world caches remain synchronous. The focused isolated,
restored-realm, input and visibility suite passed (14.144 s). Two exact CLI
navigation/snapshot runs then passed in approximately 11.9 and 11.4 seconds on
`mimic-playwright-owner-final.exe`, SHA-256
`23774c4aab322e4770a8f06f16049dc2a8b00b8fc56c50446eaf0ef87e9a490b`.
These two observations establish the reproduced timeout fix, not a new
five-pair live distribution or a complete GitHub application compatibility claim.

No full correctness suite, frozen matrix or live campaign was repeated after
the last corrections. The broad run therefore must not be reported as an exit-zero
final-source run. The final committed executable is `.build/mimic-optimized.exe`;
its commit and SHA-256 receipt is `.build/stalls-committed-release.json`.
Earlier measurements remain attached to their actual binaries. The unchanged
client workload has SHA-256
`cb6734186d1922edd609af0a87ef224dbb14a54ab070ae672f8cd200a4b9d08b`.
