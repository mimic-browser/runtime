# changedetection.io nonvisual monitoring evaluation

Measured on Windows on 2026-09-30 with Mimic runtime source at
`4043626c21a07cdafca8b98220e4e9fe12466c57` and frozen Chrome
`152.0.7977.82`. This is a representative standalone opt-in fetch harness
with actual upstream filtering, not a full application integration, a live
shop test, or a stock screenshot-fetcher compatibility claim.

## Upstream contract inspected

The inspected changedetection.io revision is
[`0e0566721b1c483dcf7ae548210ee10532d9b181`](https://github.com/dgtlmoon/changedetection.io/tree/0e0566721b1c483dcf7ae548210ee10532d9b181).
Its `content_fetchers/puppeteer.py` and `playwright.py` retrieve rendered
HTML but also capture screenshots unconditionally. Their default extra waits
are approximately 12 and 5 seconds respectively. They expose additional
browser-step, element-geometry and stock-observation behavior beyond this test.
Mimic cannot provide their screenshot contract. An opt-in nonvisual fetcher
would need to advertise the correct capabilities: `base.py` has screenshot
capability flags and `pluggy_interface.py` has fetcher registration hooks.
No such plugin was implemented here.

The harness executes unmodified upstream `html_tools.py` and `strtobool.py`
as a namespace package, without initializing the application. Each returned
`page.content()` snapshot runs through the actual `include_filters`,
`xpath_filter`, and `html_to_text` helpers. CSS and XPath results must both
equal independently declared expected strings. Notification comparison uses
the resulting text; the real app scheduler, datastore, processors and
notification delivery are outside this test.

The inspected [sockpuppetbrowser tuning documentation](https://github.com/dgtlmoon/sockpuppetbrowser/tree/d6c16815a231d0050eee22a8efc5c4afbbc987d9)
describes fresh Chrome per connection. We did not run that container or use
its defaults as a speed baseline. The primary comparison instead reuses a
browser process across six checks, creates and closes a Page per check, skips
screenshots in both backends, and uses the same explicit readiness selector.
It also excludes both stock fixed extra waits.

## Workload and controls

The localhost fixture asynchronously fetches product data, waits 20 ms,
constructs DOM nodes, and marks readiness after a microtask. Initial HTML has
no product price or availability values. A 100-item unwatched catalog adds
unselected HTML. Six checks exercise baseline, an unrelated counter change,
price reduction, restock, another unrelated change, and a product-name change.
Expected change flags are `[false, false, true, true, false, true]`.

CSS: `#product .name, #product .price, #product .stock`.
XPath: `//*[@id='product']/*[@class='name' or @class='price' or @class='stock']`.
Both browsers use a 1280 x 720 viewport and the same local server. Chrome
launches directly with a fresh dedicated profile and fixed CDP port; Playwright
only attaches. Saved headful controls have original `navigator.webdriver=false`.
Headless controls are explicitly labeled network-local headless experiments;
no navigator properties were overridden. Every snapshot, exact launch argument,
browser identity, binary hash and capture hash was retained locally under
`.build/changedetection-20260930/`. Headful successful captures were retained
before the headless experiment.

Three fresh-process pairs alternate backend order, with six checks per process:
18 correct snapshots per backend per mode, including 15 warm checks. One-pair
gates also passed. The first preliminary gate failed because this new harness
expected compact text instead of Inscriptis paragraph spacing; its Chrome
capture was retained and the harness expectation corrected. No Mimic runtime
defect or additional runtime fix was found in this evaluation.

## Measurements

The primary optimized headless comparison produced these medians:

| Measurement | Mimic | Chrome |
| --- | ---: | ---: |
| Backend launch to first HTML snapshot, ms | 1217.09 | 940.03 |
| Warm fetch, diagnostics and Page close, ms | 265.46 | 125.72 |
| Warm check including both upstream filter paths, ms | 294.34 | 153.76 |
| Peak sampled process-tree RSS per six-check run, MiB | 169.94 | 1044.68 |
| RSS after all check Pages close, MiB | 122.21 | 1042.86 |
| Sampled process-tree CPU per six-check run, seconds | 2.40625 | 4.03125 |

The prior headful comparison also passed all 18 snapshots per backend:
cold fetch 1162.52 / 910.37 ms, warm fetch 254.02 / 118.34 ms, warm check
276.22 / 151.18 ms, peak RSS 171.99 / 1031.25 MiB, and sampled CPU
2.40625 / 3.453125 seconds (Mimic / Chrome). The final headless runs additionally
assert that Page counts return to their initial baseline and sample retained
RSS after a 100 ms settling interval. All six assertions passed.

RSS sums include only the launched backend process and its descendants, sampled
every 25 ms with psutil. They exclude the Node client, Python filters and local
server. Shared Chrome pages can be counted multiple times; this is not unique
physical memory, PSS, or a complete deployment budget. Final headless runs
recorded 46-52 Chrome samples and 68-70 Mimic samples, with zero sampling errors.
CPU sums retain each observed process's maximum accumulated user/system time;
short-lived descendants may be missed. CPU values cover the six-check run,
not a per-check CPU estimate. No retained-memory plateau or leak claim follows
from six Pages. Browser cold timings include the Node startup and CDP attachment,
but Python filter dependencies were prewarmed equally and filtering is excluded
from that first-snapshot metric. Warm checks include both filter paths and
diagnostic reads, so they are not production single-filter latency.

The checked-in [measurement record](../performance/changedetection-nonvisual-20260930.json)
contains per-check results, identity, hashes, source provenance, dependency
versions and both experiment summaries. Raw HTML and launch records remain
local in the ignored build directory. Small samples, localhost networking,
one host, no concurrency, no screenshot work and no live anti-bot behavior
limit generalization. Different HTTP transfer-length accounting is recorded
as diagnostics, not claimed as bandwidth savings.

## Reproduce

Use the frozen Chrome bundle and repository Playwright-core installation
(measured Node 24.15.0, Playwright-core 1.63.0). Install the following Python
packages in an isolated virtual environment; measured Python was 3.14:

```powershell
python -m venv .build/changedetection-env
.build/changedetection-env/Scripts/python.exe -m pip install psutil==7.2.2 loguru==0.7.3 beautifulsoup4==4.14.3 inscriptis==2.7.5 lxml==6.1.3 elementpath==5.1.1 soupsieve==2.10
go build -o .build/mimic-changedetection-current.exe ./cmd/mimic
.build/changedetection-env/Scripts/python.exe tools/performance/changedetection_nonvisual.py --mimic .build/mimic-changedetection-current.exe --chrome compatibility/.chrome-for-testing/152.0.7977.82/chrome-win64/chrome.exe --output .build/changedetection-new-run --repeats 3 --chrome-mode headless
```

The output directory must be new; ports 9233, 9336 and 9240 must be available.
The script downloads only two pinned upstream helper files if `--source-cache`
is omitted. A source cache uses filenames with `/` replaced by `__`.
Use `--chrome-mode headful --repeats 1 --gate-only` for a normal headful local
control before recording headless performance. The driver terminates only
its launched backend process tree and client and closes its local server.

We can honestly approach a maintainer with evidence that an opt-in nonvisual
monitoring path can preserve these CSS/XPath text changes while reducing
sampled browser-tree RSS on this fixture. We cannot claim faster checks,
drop-in stock-fetcher compatibility, screenshot support, a deployed plugin,
or validated live-site monitoring.
