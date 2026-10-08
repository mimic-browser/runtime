# Controlled differential investigation, 2026-09-10

Follow-up: [continuation state investigation](continuation-state-2026-09-10.md)
records the later SameSite/worker/form fixes, consumed Resource Timing defects,
fresh verification and the widget's `fail: 600010` before the final top-level /fo.
The remaining-defects list below describes the earlier dfc383d checkpoint.

## Outcome and reference

The general cookie partition defect is fixed. It did **not** resolve the protected
test page: a fresh ordinary Mimic run still returned challenge 403 after clearance.
Fresh system Chrome navigated by POST to application 404 and its same-context GET
revisit also returned application 404. No POST was forced and no proof was copied.
The earliest **causal** divergence before the completion decision remains unknown.

Measured reference: Google Chrome **152.0.7977.83**, not the frozen Chromium
**152.0.7977.82** profile. Reports explicitly mark this alternate reference. Product
brands and language preferences differ; they are not silently normalized away or
classified as implementation defects. No full frozen-reference validation is claimed.

## Reusable local corpus

`tools/compatibility/differential.py` runs the same JS corpus in both runtimes,
captures independent loopback server requests and retains raw CDP events. It covers
serial redirects, headers, connection reuse, cookie partitions/attributes, SameSite,
window/worker/iframe ordering, selected resource timing relations, prototype
descriptors, SVG value semantics, WebGPU lifecycle, fonts, canvas readback relations,
media capability and RTC state. This is a reproducible seed corpus, not exhaustive
browser coverage or a complete scheduling oracle.

Results for the eight shared browser cases (counts are JSON diff leaves, **not**
independent defects):

| Case | Before | After | Interpretation |
| --- | ---: | ---: | --- |
| network | 24 | 24 | 12 locale/product-profile leaves; 12 HTTP/1.1 header leaves |
| temporal | 2 | 2 | Worker queueMicrotask unavailable; other measured relations agree |
| surface | 8 | 8 | Selected descriptor/interface gaps, listed below |
| SameSite | 7 | 5 | Rejected insecure SameSite=None; remaining cross-site rules differ |
| partition | 11 | 0 | Document, transport and effective CDP cookie metadata agree |
| SVG values | 0 | 0 | Existing value oracle; not all SVG/rendering semantics |
| observations | 22 | 22 | Font metrics, media methods and canvas exception text |
| WebGPU lifecycle | 0 | 0 | Existing lifecycle oracle; not graphics rendering |

Total: **74 -> 61** leaves on shared cases. A later ninth harness self-test verifies
undefined/NaN/infinity/-0/bigint/array-hole normalization and passes both current
runtimes. The initial comparison used ordinary JSON observations; the eight shared
fixtures were unchanged when the tagged normalizer was added and their after-counts
remained identical. A rebuilt baseline rerun with the new normalizer did not run.
Do not present the ninth case as a newly measured baseline result.

Private evidence: `corpus-before-final`, `corpus-after-final`,
`corpus-after-normalized`, `corpus-controlled-after`. All are under
`compatibility/private-captures/`. The checked-in summary retains counts, hashes and
local-corpus differences without promoting private network captures.

## General fixes

* Cookie identity now includes a schemeful top-level site and cross-site ancestor
  bit in addition to domain/path/name. A -> B -> A is distinct from first-party A.
  Ports and same-site subdomains do not invent new partitions; public suffixes,
  private registries and IP literals are handled by the shared site function.
* The loader uses that context for selection and Set-Cookie storage. Main-frame
  navigation uses its destination partition; subresources preserve their top-level
  context through redirects. Document cookie access uses security-origin ancestry.
* Dedicated workers inherit a value snapshot from their creator. Script loads and
  worker fetches use the same partition, including nested frame ancestry. No global
  runtime lock or duplicate cookie jar was added.
* Partitioned cookies require Secure. Missing/opaque top-level sites are rejected,
  not collapsed into a first-party partition. HttpOnly overwrites and deletion stay
  inside the selected partition; same-name unpartitioned cookies can coexist.
* CDP reads canonical cookie snapshots with effective flags and partition keys.
  SetCookie/SetCookies preserve the supplied attributes and explicit partition key;
  DeleteCookies selects the specified partition, with omission selecting only
  unpartitioned cookies. A separate disposable Chrome CDP probe verified this
  deletion behavior. Opaque/non-HTTP partition keys are rejected explicitly.
* SameSite=None without the Secure attribute is rejected for HTTP response and
  document writes, including trustworthy loopback URLs, as measured in Chrome.

## Remaining measured divergences and boundaries

1. SameSite Lax/Strict/default filtering and cross-site writes still disagree in
   the A -> B -> A probe. The current change implements partition isolation, not a
   full SameSite navigation/redirect policy. Next expand this local corpus with safe
   and unsafe top-level methods, redirect taint and default-Lax age before changing
   that policy. Cookie sourceScheme/sourcePort/priority, nonce-backed opaque CHIPS,
   CDP URL filtering and exhaustive attribute validation also remain outside coverage.
2. WorkerGlobalScope.queueMicrotask currently resolves to undefined. The window
   probe agrees for nested Promise/microtask/timer order, monotonic clock samples,
   iframe load/readyState and sampled resource timing. This does not prove complete
   scheduler equivalence. A worker fix needs callback validation and exception/error
   reporting tests as well as ordering; copying a Promise shim would be insufficient.
3. Independent HTTP/1.1 server observations show Chrome sends Connection: keep-alive
   and omits Priority, while Mimic's cleartext fallback omits Connection and sends
   Priority. Redirect method/body semantics and observed connection reuse agree.
   These header differences are not evidence about the h3 challenge connection.
4. Measured surface gaps: Canvas createConicGradient/createPattern/drawFocusIfNeeded/
   lang; FontFaceSet.forEach length; GPUAdapter.isFallbackAdapter exposure;
   HTMLIFrameElement.sharedStorageWritable; HTMLMediaElement.controlsList setter.
   No API was added solely to eliminate these descriptor counts.
5. Canvas text metrics differ for the installed Arial reference and getImageData's
   zero-width exception message differs. Media elements lack canPlayType. The chosen
   RTC observation and canvas repeated/local-change relations agree. The original
   default canvas context changed pixels across Chrome readbacks; the explicit
   willReadFrequently context is stable in repeated runs. Do not turn that backend
   variation into independent random buffers in Mimic.

## Fresh completion comparison

Private ordinary captures: `mimic-partition-normal-20260910` and
`chrome-system-partition-normal-20260910`. Chrome's recorder reported two late
snapshot/evaluation errors for a detached frame; document response, POST and revisit
evidence is present. Mimic's nested CDP event envelopes must be decoded; reading
only top-level Network events incorrectly produces an empty response list.

Both runs show initial document activity, orchestrate, first same-origin /fo,
two challenge-frame /fo requests and final same-origin /fo in the same order.
Chrome then posts to the application (404) and its later GET is 404. Mimic performs
another GET and receives 403. Its jar now separately retains the cross-site frame
and first-party partition keys. Clearance receipt remains insufficient evidence
that the completion verdict is successful.

The inspected CDP request projections differ immediately in configured language
and Google Chrome/Chromium brands. Stable inspected Origin, Fetch Metadata and
other selected headers on the final same-origin /fo agree apart from product/
language/version fields. Attempt-specific frame Referer paths naturally differ.
This is **not** an independently captured transport equivalence result, nor proof
that profile differences caused rejection.

Critical-CH produces duplicate native request events with one request ID and only
one ExtraInfo record in this capture. The restarted document's actual high-entropy
headers cannot be reconstructed by blindly joining on request ID. Treat that part
as unresolved; do not call the projected missing headers a new compatibility bug.

Next discriminating experiment: first obtain independent HTTP/2/3 serialization
evidence on a controlled endpoint and an unambiguous request-attempt association.
Measure an identical profile/reference and record protocol, ordered headers, body
hash/length, cookies, partition/site/initiator and connection reuse at each causal
step. Then reduce locally reproduced SameSite and worker scheduling divergences.
Do not infer a particular telemetry verdict from encrypted completion programs or
repair unsupported APIs solely because they appear in the challenge trace.

## Validation

* Cookie store, site derivation, partitioned deletion, HttpOnly protection and
  immutable snapshot tests pass.
* Browser regression verifies A -> B -> A document reads, actual received fetch
  cookies, worker script request and worker fetch in both V8 and Goja.
* CDP partition/attribute/deletion regression passes.
* Seven Python differential helper tests pass (16 compatibility helper tests in
  total); the normalization corpus self-test
  passes in system Chrome and Mimic.
* `go test ./...` passed on repeat (browser: 186.306 s). The first full run hit the
  existing native asynchronous WASM test's five-second timeout; three isolated
  repetitions then passed. No timeout or assertion was weakened.
* Focused race checks for cookie/network/CDP/frame-worker integration passed
  (network 2.461 s, CDP 1.169 s, browser 26.478 s). This is not the full race suite.
* `.build/mimic.exe` built and used for the final local corpus and ordinary page
  test. The protected application target is **not reached by Mimic** in this batch.
