# Workload specialization PoC, 2026-10-05

The end-to-end offline workflow works, but these experiments do **not** establish
that automatic optimization beats a competent manual traffic policy. The real
SSR workload independently converged to document-only behavior. The existing
async and React workloads needed their acquired resources. The unchanged saved
Wikipedia workload demonstrated runtime savings, but its CDP fixture fulfillment
made it unsuitable as evidence of internet savings.

Recommendation: retain this as an experimental research tool. Do not promote
generated plans into the production live path or build universal deoptimization
machinery on the strength of these results. The next evidence gate is a set of
real dynamic applications with redundant bundles/requests and meaningful ordinary
assertions, each with several independently recorded states and a strong manual
baseline. The current infrastructure makes that experiment inexpensive offline;
it does not demonstrate generalization beyond its capture.

## Implemented boundary

The external process is the primary oracle: normal assertions, test-framework
success, nonzero exit, timeout and crashes. Existing optional JSON results can
be compared against the baseline. No new assertion API or exact CDP transcript
equivalence was introduced. Replay integrity and environment coverage are
independent checks; the report preserves `workloadStatus` when a passing workload
encounters an uncovered request.

`internal/workload/{transport,artifact}.go` owns the experiment, binary ZIP
capture, independent per-Context request cursors, streaming disk spool, lazy
body readers, counters and execution plans. Browser Context transport creation
in `internal/browser/browser.go` wraps the existing pinned transport. The normal
loader still owns redirects, compression, cookies, CORS, cache and consumers.
`internal/network/resource_observer.go` carries existing loader provenance into
the wrapper; `loader.go` observes normal deliveries after processing.

`internal/browser/realm.go` admits external classic author execution separately
from acquisition. This preserves normal acquisition/scheduling and deferred and
dynamic load events, without replaying script effects. It does not suppress
inline scripts, module execution, workers, init scripts or CDP evaluations.
The existing parser-blocking-script load-event limitation is inherited and
documented by the focused test.

`internal/mimicmain/run.go` connects these private hooks to an authenticated,
loopback-only experiment control listener and experimental flags. There are no
new CDP handlers. Generated resource actions compile into existing ResourcePolicy;
its public contract and manual control remain intact. `.mplan` artifacts require
the exact capture and executable hashes and reject live activation. These are
evidence guards for an offline experiment, not a production safe-deoptimization
model.

`tools/workload/optimize.py` owns fresh browser/client process trees, logs,
classification, baseline replay validation, bounded hierarchical elimination,
memoization and matched finalist measurements. `benchmark.py` reuses the frozen
fixture pages, vendor bundles and expected outputs without changing them.
Usage and limitations are in [the runner documentation](../../tools/workload/README.md).

## Workloads and manual baseline

* **Books SSR:** real `books.toscrape.com` response bodies, headers and compression
  captured once successfully. Ordinary Playwright extraction asserts 20 products,
  first title and price, valid prices and equality of the complete JSON product
  list. Manual uses document-only, not merely image/font blocking.
* **Existing async:** unchanged frozen page exercises promises, timers, a message
  channel, worker, fetch, XHR and posted messages. The ordinary external CDP
  process asserts the complete frozen expected result. Manual preserves document,
  scripts, workers and API traffic, blocks visual/speculative resources.
* **Existing React:** unchanged production React/ReactDOM bundles and frozen page
  update a dynamic list; the client asserts count, phase and first/last item.
  Manual uses the same strong interactive policy. These fixtures have little
  extraneous network work; that is a useful negative control, not evidence of
  general real-application coverage.
* **Saved Wikipedia:** the pre-existing client remains byte-for-byte unchanged.
  It asserts search, navigation, headings, paragraph content, link click and
  history return. Its private fixture is delivered through ordinary CDP Fetch
  fulfillment. Manual document-only passes this particular workload. It is a
  realistic compatibility/interaction control, not a transport traffic benchmark.

## Measurement protocol

The final build runs all variants against the same capture in fresh processes
and Contexts. Three baseline replays must pass and have stable encoded bytes and
acquired-response counts. Five finalist repetitions rotate variant order.
Detailed resource/script inventory is disabled for the matched matrix. CPU,
sampled RSS and the same small delivery/transport counters remain enabled for
all variants. No live-site latency or total physical-wire bandwidth is claimed.

Encoded bytes are bodies actually read before decompression: they exclude HTTP
headers, TLS/framing, unread kernel buffers and synthetic CDP fulfillment. Search
blocks whole resources; it does not stop reading early to fabricate a smaller
wire figure. Transport attempts include pre-header cancellations; acquired
responses do not. Processing counts decoded loader deliveries, including cache
and synthetic bodies. Classic-script execution counts are narrower than total
JS execution, especially for the worker workload.

Time includes external client startup and assertions, excludes browser startup.
Browser CPU covers its owned Windows process tree during the command. Peak RSS
uses 20 ms sampling; retained RSS is the resident process after client completion,
not retained live heap. Normal downloaded-body caches are retained during the
run. Body-store retention returns to zero after Context disposal. Differences
in small CPU/RSS/timing samples are descriptive, not statistical proof of an
optimization when the same plan and acquisition counters are unchanged.

## Search findings and negative evidence

Search starts with non-document kinds, splits failing groups, then tries exact
URLs grouped by existing kind/owner/mechanism and ordered by encoded capture
cost. Passing removals accumulate; external classic-script suppression is a
separate final stage. Default and competent manual remain candidate finalists.
Selection rewards encoded acquisition and then acquired-response count, never
reduced cache retention, decoded processing or one noisy timing sample.

Books' successful coarse rule removes observed image, stylesheet and script
kinds. This is independently discovered, but not non-obvious compared with the
manual document-only policy. Suppressing all external classic scripts instead
triggers an inline jQuery fallback whose alternate URL was not requested by the
baseline. The original extraction still passes, but the capture cannot validate
that new request branch. It is reported as `UNSUPPORTED_REPLAY`, not semantic
failure, and no network fallback occurs.

Async rejects script removal/suppression because its runner is undefined, and
API/worker removal because asserted results fail or the normal wait expires.
React rejects removal of its runtime/vendor/application scripts because its
ordinary completion/result contract fails. In both cases the generated winner
is the default empty plan. Fine-grained search runs but discovers no additional
traffic savings. This is not an artificial win against a wasteful default.

The saved Wikipedia script-suppression ablation is an example of useful runtime
ownership, but equal zero transport acquisition gives it no advantage under the
traffic-only objective. Manual still wins runtime/time in this supplementary
control. It would be misleading to count its smaller fulfilled bodies as fewer
internet bytes. Existing coarse provenance is sufficient to partition candidates;
no instruction graph was built, and these results do not establish its causal
value or a memoization speedup.

## Replay evidence and limits

Original successful evidence is retained locally. Captures produced during the
first transport prototype were reindexed offline to add streaming body identity
metadata; original encoded payloads and response headers were preserved. No
successful website recording was discarded or repeatedly fetched for search.
Earlier async/React source-server receipts show exactly five live baseline
requests each and no extra requests across replay/search. Final offline trials
also run with their original source servers closed. Unit tests additionally use
a forbidden base transport to verify that replay cannot invoke live transport.

A previous Wikipedia matched run exposed one late unfulfilled favicon request
after the client's fixture route was being torn down. The workload passed, but
replay coverage did not: the trial was preserved as unsupported. Already-canceled
transport calls now return the normal cancellation before attempting acquisition
or matching. This prevents a false acquisition/miss on that ownership race; a
genuinely new uncanceled request still remains unsupported. This does not infer
that favicon changes are semantic failure, or guarantee all future teardown
orderings are deterministic.

Exact semantic query parameters, request bodies and headers are matched. Explicit
nonsemantic timestamp/cache-buster keys can normalize matching while preserving
the original request and browser observations. Changing paths/origins or new
response branches require new evidence. General live Mimic does not use replay
matching. Unsupported captures are distinguished from incorrect workload results.

Time/randomness are not virtualized. Different repeated identical responses,
concurrent identical requests, sockets/upgrades, incomplete response bodies,
arbitrary transport errors and persistent initial-state checkpoints remain
unsupported. No production URL guard lattice, automatic live fallback,
universal rollback, module pruning, cloud optimizer or public API migration was
built. The wrapper is not an OS sandbox for networking performed by the external
client itself. Frozen workload correctness does not establish unasserted
application requirements, and a passing frozen capture is not proof of safe live
specialization tomorrow.

One necessary shared runtime fix was found: nested `document.write` paused at a
denied external script could double-resume the tokenizer, hang navigation or
lose the outer insertion tail. `internal/htmlstream/stream.go` now preserves
both continuations; DOM and browser tests check both tails and complete lifecycle.
This fixes the shared parser boundary rather than adding a Books-specific path.

## Validation

Focused tests cover capture cursors/Context isolation, redirects/cookies/gzip,
immediate disk writing/lazy replay/cleanup, partial/concurrent/ambiguous capture
rejection, cancellation ownership, explicit volatile keys, disk-failure isolation
from live bodies, classic suppression lifecycle, nested parser continuation,
existing resource-policy/CDP control and external runner process/result contracts.
All nine Python runner/search tests passed against the final executable, including
a fresh actual async recording followed by replay after the source HTTP server
was closed, and cleanup of descendants after both success and timeout. Focused
Go workload, browser, DOM stream, network policy/WebSocket/certificate and CDP
resource-policy tests passed; `go vet ./internal/workload` and diff whitespace
checks also passed.
The full local suite was deliberately not run. No frozen harness, original
baseline data, reference expectation or primary checkout was modified.

The measured tables and machine-readable receipts follow below.

## Final matched results

All figures below are medians of five fresh-process, rotated-order trials; correctness is all five trials, not a median. RSS is MiB. Raw metric receipts are in [the JSON evidence](workload-optimization-poc-results.json).


### Real Books SSR

| Metric | Default | Manual | Auto |
| --- | ---: | ---: | ---: |
| Encoded acquired body bytes | 284591 | 5276 | 5276 |
| Transport attempts | 30 | 1 | 2 |
| Acquired HTTP responses | 29 | 1 | 1 |
| Processed body bytes | 682045 | 51294 | 51294 |
| Workload time, ms | 1399.0 | 1181.4 | 1164.7 |
| Browser CPU, ms | 1546.9 | 1140.6 | 1156.2 |
| Peak sampled RSS, MiB | 194.6 | 163.2 | 163.2 |
| Retained RSS, MiB | 130.4 | 112.3 | 115.3 |
| Peak retained body-store bytes | 682045 | 51294 | 51294 |
| Retained body-store bytes after teardown | 0 | 0 | 0 |
| Transport scripts acquired (including workers) | 5 | 0 | 0 |
| Classic author scripts executed | 7 | 2 | 2 |
| Correctness | PASS 5/5 | PASS 5/5 | PASS 5/5 |

Baseline replay: 3/3 PASS with stable acquisition cost. Search: 1 trials, 1 passing, 0 rejected; 1.60 s search wall time; 0 memo hits. Selected origin: **automatic**. Training/validation trial wall sum (baseline, manual, ablation, search): 10.59 s; matched matrix excluded.

Auto independently removed image/script/stylesheet kinds: encoded body acquisition fell by **98.15%** versus default and **0%** versus competent manual. Its extra transport attempt is a pre-header favicon cancellation, not another acquired response. Both smaller plans still retain the decoded document normally.


### Existing async

| Metric | Default | Manual | Auto |
| --- | ---: | ---: | ---: |
| Encoded acquired body bytes | 5480 | 5480 | 5480 |
| Transport attempts | 5 | 5 | 5 |
| Acquired HTTP responses | 5 | 5 | 5 |
| Processed body bytes | 5480 | 5480 | 5480 |
| Workload time, ms | 1405.6 | 1405.7 | 1396.7 |
| Browser CPU, ms | 1203.1 | 1093.8 | 1156.2 |
| Peak sampled RSS, MiB | 172.9 | 171.2 | 170.6 |
| Retained RSS, MiB | 118.0 | 117.3 | 115.1 |
| Peak retained body-store bytes | 5480 | 5480 | 5480 |
| Retained body-store bytes after teardown | 0 | 0 | 0 |
| Transport scripts acquired (including workers) | 2 | 2 | 2 |
| Classic author scripts executed | 1 | 1 | 1 |
| Correctness | PASS 5/5 | PASS 5/5 | PASS 5/5 |

Baseline replay: 3/3 PASS with stable acquisition cost. Search: 12 trials, 0 passing, 12 rejected; 136.85 s search wall time; 0 memo hits. Selected origin: **default**. Training/validation trial wall sum (baseline, manual, ablation, search): 165.06 s; matched matrix excluded.


### Existing React

| Metric | Default | Manual | Auto |
| --- | ---: | ---: | ---: |
| Encoded acquired body bytes | 148134 | 148134 | 148134 |
| Transport attempts | 5 | 5 | 5 |
| Acquired HTTP responses | 5 | 5 | 5 |
| Processed body bytes | 148134 | 148134 | 148134 |
| Workload time, ms | 1405.8 | 1392.1 | 1382.9 |
| Browser CPU, ms | 1062.5 | 1140.6 | 1078.1 |
| Peak sampled RSS, MiB | 161.1 | 164.0 | 160.2 |
| Retained RSS, MiB | 118.7 | 116.6 | 117.7 |
| Peak retained body-store bytes | 148134 | 148134 | 148134 |
| Retained body-store bytes after teardown | 0 | 0 | 0 |
| Transport scripts acquired (including workers) | 3 | 3 | 3 |
| Classic author scripts executed | 3 | 3 | 3 |
| Correctness | PASS 5/5 | PASS 5/5 | PASS 5/5 |

Baseline replay: 3/3 PASS with stable acquisition cost. Search: 15 trials, 0 passing, 15 rejected; 200.09 s search wall time; 0 memo hits. Selected origin: **default**. Training/validation trial wall sum (baseline, manual, ablation, search): 227.65 s; matched matrix excluded.


### Saved Wikipedia (supplementary)

| Metric | Default | Manual | Auto |
| --- | ---: | ---: | ---: |
| Encoded acquired body bytes | 0 | 0 | 0 |
| Transport attempts | 0 | 0 | 0 |
| Acquired HTTP responses | 0 | 0 | 0 |
| Processed body bytes | 7024306 | 2812481 | 7024306 |
| Workload time, ms | 10909.2 | 6428.5 | 10860.3 |
| Browser CPU, ms | 17781.2 | 8515.6 | 16953.1 |
| Peak sampled RSS, MiB | 427.0 | 314.4 | 446.1 |
| Retained RSS, MiB | 207.9 | 169.2 | 212.8 |
| Peak retained body-store bytes | 7024306 | 2812481 | 7024306 |
| Retained body-store bytes after teardown | 0 | 0 | 0 |
| Transport scripts acquired (including workers) | 0 | 0 | 0 |
| Classic author scripts executed | 30 | 12 | 30 |
| Correctness | PASS 5/5 | PASS 5/5 | PASS 5/5 |

Baseline replay: 3/3 PASS with stable acquisition cost. Search: 1 trials, 1 passing, 0 rejected; 6.80 s search wall time; 0 memo hits. Selected origin: **default**. Training/validation trial wall sum (baseline, manual, ablation, search): 55.61 s; matched matrix excluded.

The separate execution ablation passed: 12 classic scripts executed, 4 directly suppressed, 7333.0 ms, 11296.9 browser CPU ms. It indirectly prevents additional script creation; these figures are one exploratory trial, not a matched five-run performance claim. Encoded transport acquisition remains zero. The passing coarse-removal trial also has zero acquisition; the traffic objective therefore keeps default, even though manual saves runtime work.


### Provenance

Base commit: `244be86d8d991a41eda470a4be0ac6878bd4c91e`. Final V8 executable SHA-256: `95de8c94593c957e60f8578d4483687fd58e643aca3960970ace937c77192ee8`. Go: go1.26.4, Windows amd64. Bootstrap disk compilation is disabled equally for all trials; these are fresh-process results, not the historical Wikipedia cold/warm gate.

Unchanged Wikipedia client SHA-256: `5b340d5c51d1ce7f291c1fb9f635df7e5a7d29cd12255ee2e09e59918325e444`. Frozen benchmark fingerprint: `ce1fce42fa9b9e03f105900601db4d6d7fa9b0d9cda51d5096322357277673e7`. Individual capture SHA-256 identities and all raw per-trial metric/status receipts are in the JSON evidence. Full logs and binary artifacts remain in the separate workspace under `.build/workload-final-measured/`.

