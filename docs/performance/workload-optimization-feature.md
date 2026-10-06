# Workload optimization: dynamic acquisition challenge

Date: 2026-10-05. Windows amd64, V8, headless environment, Chrome 152 identity.
This evaluates the native feature, separately from the [original PoC](workload-optimization-poc.md).
The machine-readable [results](workload-optimization-feature-results.json) identify
the executable, clients, policies, captures, profiles and individual measurements.

## Finding

Automatic specialization found a real, narrow acquisition improvement beyond a
competent manual policy on React's documentation application: **30,060 encoded
body bytes (5.4%) and two acquired responses**. The actual installed profile passed
five matched local trials, and a separate live Manual/Auto pair reproduced the
same byte/response difference with both workloads passing.

Vue/VitePress and RealWorld/Angular tied their strong manual policies. Books tied
document-only. These ties do **not** establish a global optimum: remaining
implementation-coupled requests and static module dependencies exceed the
current intervention model. Conversely, the corpus does not justify a general
claim that Auto beats competent manual configuration on dynamic applications.

The feature is worth retaining as explicit, empirically validated specialization.
The evidence supports branch-aware acquisition search, not a broad production
generalization claim or a browser partial-evaluation project.

## Workloads and eligibility

- **React documentation:** actual `https://react.dev/learn`; substantive article
  content, theme change and restoration through author event handlers, in-page
  navigation and target existence. The original default live workload passed.
  The strong manual policy removes visual/speculative work, analytics, unused
  route data, Sandpack editor origins and legacy polyfills. It does not manually
  enumerate the two subsequently discovered first-party fallback chunks.
- **Vue guide:** actual `https://vuejs.org/guide/introduction.html`; introduction
  heading, theme round-trip, hydrated navigation to Quick Start and new article
  content. Fresh default live validation passed with 69 transport attempts.
  Manual permits first-party documents/modules, denies speculation and all
  other acquisition. An older capture contained 87 attempts, but its client used
  a nonexistent switch selector and failed. That is not eligible baseline evidence;
  the final measurements use the fresh, successful current workload capture.
- **RealWorld:** actual `https://demo.realworld.show/`; API-fed feed, matching
  article title/author, substantive API body content in the rendered article and
  return navigation preserving the first article. Fresh default live validation
  passed with 40 attempts. Manual keeps application modules and required article
  API routes, denies tags, speculation and other resources.
- **Books:** actual Books to Scrape capture and ordinary extraction assertions;
  SSR negative control. Manual is document-only. It is not counted as proof of a
  dynamic-site advantage.

The dynamic clients invoke DOM activation through ordinary CDP evaluations.
These tests do not establish Playwright pointer-actionability, rendering or
screenshot compatibility. No site-specific runtime branches were added.

An early RealWorld contract accepted nonempty loading/error text as article
content. Its apparent Markdown-module removal win was discarded. The final
contract verifies the API body prefix, waits for the same required asynchronous
rendering, and retains that assertion. Suppressing the Markdown module now fails.
No positive result from the weak contract is included here.

## Measurement protocol

All final offline cases run sequentially on one fresh executable, with no other
browser experiments, builds or site builds running concurrently. Three covered
baseline replays establish assertion stability; five matched measurements rotate
variant order and use fresh browser/process state. Search inventories are disabled
for measurements. The optimized variant installs the generated `.mprofile` through
the normal runtime entry point, rather than measuring only a raw training policy.

Encoded body bytes count bytes read from HTTP response bodies. They are not
physical wire traffic, TLS bytes or projected Resource Timing values. Requests
are transport attempts; acquired responses additionally require response headers.
CPU is browser process-tree CPU. Peak/retained RSS are samples, not exact owned
heap accounting. Processed body bytes include normal decoded/cache processing.
Classic executions do not count ES-module execution. Timing is local replay
timing and does not predict live server latency. Five samples are descriptive,
not precise statistical proof of small CPU/RSS/time improvements.

Default local replay remains uncovered on these dynamic states: changing
analytics request identities and ambiguous repeated visual responses are not
silently treated as recorded equivalents. Separate explicit exclusions must pass
new covered trials before search. **Default matched metrics are unavailable**;
they are not invented by relabeling an exclusion policy as Default. Original
manual policies required no coverage repairs in the final comparisons.

### React documentation

| Metric | Default | Manual | Auto |
| --- | ---: | ---: | ---: |
| Encoded body bytes | unavailable | 556,903 | 526,843 |
| Transport attempts / responses | unavailable | 20 / 20 | 18 / 18 |
| Workload time (median) | unavailable | 3.43 s | 3.30 s |
| Browser CPU (median) | unavailable | 4359 ms | 4016 ms |
| Sampled peak RSS | unavailable | 227.7 MiB | 222.6 MiB |
| Sampled retained RSS | unavailable | 133.8 MiB | 133.0 MiB |
| Processed body bytes | unavailable | 1,952,920 | 1,781,662 |
| Scripts acquired / classic executions | unavailable | 18 / 21 | 16 / 19 |
| Correctness | UNSUPPORTED | PASS 5/5 | PASS 5/5 |

Search: 14 candidates, 180.0 s. Training/validation: 238.7 s. Recorded states: 1.

### Vue guide

| Metric | Default | Manual | Auto |
| --- | ---: | ---: | ---: |
| Encoded body bytes | unavailable | 88,870 | 88,870 |
| Transport attempts / responses | unavailable | 13 / 13 | 13 / 13 |
| Workload time (median) | unavailable | 1.80 s | 1.81 s |
| Browser CPU (median) | unavailable | 1969 ms | 1875 ms |
| Sampled peak RSS | unavailable | 184.8 MiB | 184.4 MiB |
| Sampled retained RSS | unavailable | 122.1 MiB | 121.5 MiB |
| Processed body bytes | unavailable | 336,175 | 336,175 |
| Scripts acquired / classic executions | unavailable | 12 / 5 | 12 / 5 |
| Correctness | UNSUPPORTED | PASS 5/5 | PASS 5/5 |

Search: 11 candidates, 180.0 s. Training/validation: 211.0 s. Recorded states: 1.

### RealWorld

| Metric | Default | Manual | Auto |
| --- | ---: | ---: | ---: |
| Encoded body bytes | unavailable | 159,719 | 159,719 |
| Transport attempts / responses | unavailable | 21 / 21 | 21 / 21 |
| Workload time (median) | unavailable | 1.56 s | 1.60 s |
| Browser CPU (median) | unavailable | 1719 ms | 1734 ms |
| Sampled peak RSS | unavailable | 165.8 MiB | 165.1 MiB |
| Sampled retained RSS | unavailable | 111.5 MiB | 111.6 MiB |
| Processed body bytes | unavailable | 446,058 | 446,058 |
| Scripts acquired / classic executions | unavailable | 16 / 0 | 16 / 0 |
| Correctness | UNSUPPORTED | PASS 5/5 | PASS 5/5 |

Search: 9 candidates, 180.0 s. Training/validation: 207.5 s. Recorded states: 1.

### Books SSR

| Metric | Default | Manual | Auto |
| --- | ---: | ---: | ---: |
| Encoded body bytes | 284,591 | 5,276 | 5,276 |
| Transport attempts / responses | 30 / 29 | 1 / 1 | 1 / 1 |
| Workload time (median) | 1.38 s | 1.17 s | 1.17 s |
| Browser CPU (median) | 1469 ms | 1203 ms | 1172 ms |
| Sampled peak RSS | 185.4 MiB | 157.0 MiB | 153.3 MiB |
| Sampled retained RSS | 120.6 MiB | 109.2 MiB | 107.8 MiB |
| Processed body bytes | 682,045 | 51,294 | 51,294 |
| Scripts acquired / classic executions | 5 / 7 | 0 / 2 | 0 / 2 |
| Correctness | PASS 5/5 | PASS 5/5 | PASS 5/5 |

Search: 0 candidates, 0.0 s. Training/validation: 27.1 s. Recorded states: 1.

All matched encoded-byte ranges equal their medians; browser response-body retention returns to zero after Context disposal. The JSON includes individual samples and candidate statuses.


## Separate live observations

These are single eligibility/activation observations, not a matched latency
benchmark or a substitute for missing Default replay measurements.

| Workload / variant | Encoded body bytes | Attempts / responses | Oracle |
| --- | ---: | ---: | --- |
| React original Default | 1,370,768 | 60 / 60 | PASS |
| Vue current Default | 597,236 | 69 / 68 | PASS |
| RealWorld current Default | 312,790 | 40 / 40 | PASS |
| React current Manual | 556,903 | 20 / 20 | PASS |
| React installed Auto | 526,843 | 18 / 18 | PASS |

RealWorld's default capture retains ambiguous repeated avatar evidence; recording
coverage is UNSUPPORTED even though the external assertions pass. Its exclusion
is separately validated locally. React's live Auto took 4.67 s versus Manual's
4.36 s in the single live pair: **no live latency win is claimed**. Unknown-world
fallback was not exercised by that pair; generalization remains unproven.

## Remaining acquisition after strong Manual

The categories below distinguish verified behavior, current removable branches
and limits of the model. A failed removal alone does not prove every byte needed.

| Workload | Required by tested behavior | Removable with supported search | Apparently outside business contract, but implementation-coupled |
| --- | --- | --- | --- |
| React | Article document, hydration/framework/router/theme code and supporting resources; broad bundle removal or classic suppression fails author interaction | First-party chunks `921.2a9510deb51459cd.js` (4,776 encoded bytes) and `834.fdb5399aa041241a.js` (25,284), activated by the editor fallback branch | Unused editor/features can share modules or initializers with needed application behavior. Whole acquisition/execution removal cannot isolate arbitrary functions or bundled exports |
| Vue | Guide document, Vue runtime, application/router/theme components and Quick Start route module | No additional passing acquisition reduction found | `SponsorsGroup` is statically imported by the application; its individual block prevents theme hydration. The sponsor feature is outside the assertions, but the current module-linking graph requires its module. `PreferenceSwitch` is also coupled, and may serve observable theme/preferences behavior |
| RealWorld | Feed/article API data, Angular/router/view modules, actual Markdown rendering and return feed | No additional passing acquisition reduction found | The comments request is outside the extracted data contract, but the route resolver waits for it; blocking it prevents the article heading from appearing. Arbitrary initializer/continuation pruning or fabricated API JSON would be required to bypass that coupling |
| Books | The catalog document used by extraction | Manual already removes all other acquisitions | No material unexplored network mechanism identified for this contract |

Current-build focused residual probes independently reject Vue's sponsor module,
RealWorld comments and RealWorld Markdown removal. They use the same final
assertions and explicit captures, without live acquisition. Sponsor removal times
out waiting for the theme transition; comments removal times out before article
navigation completes; Markdown removal times out waiting for verified API content.
These failures remain in the evidence, not classified as proof of global minima.

The small sponsor module and comments payload expose a real limitation, but do
not justify instruction-level taint, arbitrary module export stubs, recorded JS
effects or API-result fabrication. Most remaining acquisition is compressed
application/runtime code required by the supported execution model. A general
lazy/module intervention should be pursued only on a measured corpus where that
coupling carries substantial removable downstream acquisition.

## What changed in the optimizer and runtime

- Search starts with the passing strong reference and inventories what remains.
  Whole-request elimination precedes headers-only and classic author actions.
  Grouping, adaptive splitting, bounded nonmonotonic pairs, memoization and
  separate fallback-branch probes keep the request set dynamic.
- Uncovered trials remain invalid. A subsequent plan can explicitly block newly
  activated fallback requests, including bounded combinations originating from
  an uncovered failing oracle. Only a fresh, fully covered PASS can be accepted.
- One successful manual branch recording exposed React's two missing immutable
  chunks. Extension preserves original response bodies and parent provenance;
  mutable new responses or changed existing state are not merged. Explicit
  capture inputs never cause live recording. Search itself is always offline.
- Branch provenance matters here: without its priority, an exploratory 300-second
  search found only chunk 834. With it, both chunks were found by candidate 3;
  both final sequential experiments found them by candidate 2. This is useful
  ordering evidence, not a controlled claim of universal heuristic superiority.
- Denied speculative loads no longer poison the demanded module/classic cache.
  This shared lifecycle correction also strengthens the Manual baseline; its
  benefits are not credited exclusively to Auto.
- A Fetch response can retain status/headers/cookies while avoiding body reads.
  Body consumers/clones receive an errored stream. It does not return invented
  empty JSON. Focused tests establish this action, but no real-corpus win selected
  it. Classic suppression remains separate from network permissions; none of
  the final dynamic winners selected it beyond whole-resource removal.
- SPA initiating-source/history evidence distinguishes the two RealWorld feed
  acquisitions. Identical concurrent responses retain separate occurrence quotas;
  ambiguous response selection still fails closed.
- Browser-owned cancellation now records a received body prefix and replays it
  without successful EOF, waiting for actual owner cancellation. Unexpected
  truncation stays unsupported. Disk spooling/lazy replay avoid loading captures
  wholesale into RAM; normal acquired browser bodies remain on normal cache paths.

## Cost, safety and decision

The React search took 180 seconds and 14 candidates; training/local validation
took about 239 seconds. The original default plus required reference-branch
captures acquired 1,927,671 encoded bytes. At 30,060 bytes saved per run versus
Manual, that preparation has an **encoded-body break-even of approximately 65
runs**. Local replay search consumes CPU/disk/time separately. Reuse of existing
captures makes the final operation's live acquisition zero; it does not erase
the cost of preparing that evidence. The separate live validation pair is
research cost, not part of ordinary training.

No reliable time break-even versus Manual is established. The roughly 0.13 s
median local React difference is too small and environment-dependent to turn
into a production lifetime claim. Vue/RealWorld/Books provide no incremental
network savings versus Manual, so their training cost has no demonstrated
network break-even against that reference.

MDN/commerce exploration is retained as unsupported/failed evidence: MDN exposed
a shared isolated-world shadow-host geometry ownership bug (fixed with focused
regression evidence), then changing telemetry coverage; the storefront workload
still failed required interaction before optimization. Neither is a feature win
or a meaningful specialization negative control. The original async/React PoC
fixtures and intercepted Wikipedia are regression controls, not the dynamic
product evaluation; zero acquired HTTP bytes on intercepted Wikipedia are never
reported as traffic savings.

Profiles bind to the validating build and are admitted per Page by known document
and request inputs. Unknown inputs take general Mimic. This is not rollback:
late divergence cannot restore already skipped work, and unchanged HTML can
still hide changed API/script behavior. One recorded state per workload is
insufficient evidence of production generalization. Assertions remain required
on live use. Windows was evaluated here; Unix orchestration is implemented but
not measured on this host.

Recommendation: keep Optimize as an explicitly applied empirical feature and
continue a broader, multi-state dynamic corpus. The branch discovery win answers
the original hypothesis positively in one real workload, while the other cases
limit the claim. Prioritize high-byte optional request subtrees and robust
recorded branch/state coverage; do not expand into generic CPU pruning or
substitute results merely to manufacture wins. Documentation/site preparation
remains local and unpublished.
