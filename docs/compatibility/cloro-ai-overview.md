# Cloro desktop AI Overview extraction checkpoint

2026-09-30, checkout `a5882f9d4e6b234e74f07ccf5e02860a268e862d` plus the local
innerText changes described below. This is **not a passed live Google scenario**.

Sources: [published extraction scenario](https://cloro.dev/blog/how-to-scrape-google-ai-overview/)
and [desktop browser fleet context](https://cloro.dev/blog/n100-google-update/).
The article supplies fragments, not a standalone executable: AI Mode source and
citation helper implementations are omitted. The probe reproduces its visible
layout wait, layout selection, section outerHTML/innerText, synthetic pill click,
100 ms wait, visible-link filtering, and href/aria-label extraction. It does not
claim to implement those omitted helpers, Markdown conversion, or cloro's fleet.

## Live result

Query: `why is the sky blue`.
URL: `https://www.google.com/search?q=why+is+the+sky+blue&hl=en&gl=us`.
One Chrome control and four bounded Mimic navigations used this same query;
there was no bulk crawling. The first comparison and subsequent Mimic reruns
all ended at `/sorry/` with Google's unusual-traffic message. The initial
navigation response reported 200, which is not proof of AIO availability.
Neither AIO layout appeared within the article's 10-second visible wait.
Live expansion and citation collection therefore remain untested.
No CAPTCHA was solved, proxy purchased, account used, or credentials supplied.
No change in network access was made to evade the rejection.

Windows x64, direct default network, signed out, `hl=en`, `gl=us`; these URL
parameters do not establish US egress. Both controls observed `ru-RU` languages,
Chrome 152 identity, `navigator.webdriver === false`, 1280x800 outer window and
1272x653 viewport. Frozen Chrome 152.0.7977.82 was launched directly with a
dedicated profile, fixed port, no automation/headless/feature flags, then CDP
attached to its persistent context. Windows startup requested a hidden window;
the attached page reported visible/focused. Restricted launch attempts first
failed; the successful launch reused the dedicated, never-navigated profile.
Runtime permission/cache failures and one Windows-reserved port were resolved
by approved local execution and selecting another loopback port.

Private evidence is in `.build/cloro-20260930/`: `chrome-launch.json` records
the executable hash, exact arguments and profile; `chrome-live/` and
`mimic-live/` retain identity, terminal DOM, network/exception events, response
bodies, and SHA-256 inventories. Initial bodies captured: Chrome 16, Mimic 11,
with no recorded body failures. Later `mimic-*-live/` directories preserve reruns.
Captures were not overwritten. Cookie jars and a full HAR were not separately
exported; these are bounded outcome captures, not complete transport equivalence
proofs. Machine clock output differed from shell session time; use the captured
ISO timestamps and server response times rather than inferred wall-clock offsets.

## Fixture and correction

`tools/runtimecheck/cloro_aio_fixture.html` is an authored local semantic fixture,
not Google content. Both layouts contain two pills, asynchronous delegated
handlers, mutually exclusive citation lists, and an inherited hidden link.
The probe checks exact citations, section text, final body text, and retained
HTML. `chrome-validated-main/` and `chrome-validated-alternative/` establish the
Chrome reference; `mimic-validated-main/` and `mimic-validated-alternative/`
contain the final runtime results.

The fixture exposed a general `innerText` defect: source HTML newlines between
inline controls survived as rendered newlines. Corrected text-node whitespace
handling in `internal/webapi/surface.js`, paragraph required breaks, maximum
coalescing of enclosing block boundaries, and outer collapsed whitespace.
Explicit BR breaks remain separate. Extended
`TestIsolatedInnerTextTracksOwnerMutations` with inline controls, paragraphs,
nested lists, repeated BR, and trailing whitespace next to hidden content.
There are no Google-specific selectors or results in the runtime correction.
Full CSS white-space, table, wrapping, and rendering fidelity are not established
by this focused correction.

Validation: final source build succeeded; the focused tests
`TestIsolatedInnerTextTracksOwnerMutations` (goja and V8),
`TestComputedStyleIncludesInheritedUAVisibility`,
`TestElementMatchesAndClosestUseGenericSelectorSemantics`, and
`TestEventDispatchUsesInternalEventSlots` passed. Pinned Prettier and
`git diff --check` passed. Full suite and benchmark matrix were deliberately
not run. CDP handlers were not changed. A transient sequential-client stall in
the initial harness did not reproduce with bounded client teardown; final
fixture cleanup is recorded separately and is not treated as a diagnosed
runtime defect.

Final binary: `.build/mimic-cloro-validated-20260930.exe`, SHA-256
`B7DB7FE64FE896FC3994DBFFCA3578260FC35D17FCB63B17C5777EE595238B05`.
The last live rerun used the immediately preceding build; the final additional
change only trims outer collapsed innerText whitespace. No additional live
traffic was necessary to revisit the preserved Google rejection.

## Reproduction

Start a fresh source build and the fixture server in separate PowerShell terminals:

```powershell
go build -o .build/mimic-cloro.exe ./cmd/mimic
./.build/mimic-cloro.exe -listen 127.0.0.1:9231 -navigation-timeout 25s
```

```powershell
python -m http.server 9238 --bind 127.0.0.1 --directory tools/runtimecheck
```

With existing `tools/runtimecheck/node_modules/playwright-core` available:

```powershell
$env:CLORO_FIXTURE_URL='http://127.0.0.1:9238/cloro_aio_fixture.html?layout=main'
node tools/runtimecheck/cloro_aio_probe.cjs http://127.0.0.1:9231 .build/cloro-new-main fixture
$env:CLORO_FIXTURE_URL='http://127.0.0.1:9238/cloro_aio_fixture.html?layout=alternative'
node tools/runtimecheck/cloro_aio_probe.cjs http://127.0.0.1:9231 .build/cloro-new-alternative fixture
# Optional single bounded live attempt; an absent AIO is recorded as blocked.
node tools/runtimecheck/cloro_aio_probe.cjs http://127.0.0.1:9231 .build/cloro-new-live
```

Each output directory must be new. Fixture assertion failures exit nonzero;
live blocked outcomes are captured in result.json, even when capture itself
exits zero. Chrome references must follow `docs/oracle-policy.md`, with a new
dedicated direct headful launch rather than a framework launch.

An honest outreach claim is that Mimic executed the article's interactive
extraction operations on controlled two-layout fixtures, with Chrome-matched
citations and focused text observations after a general correctness fix.
Live Google AIO acceptance, live citations, fleet throughput, geography, account
effects, and reliable scraping remain unproven. A permitted network/session
that serves a real AIO is required to finish that validation.

## Existing 9Proxy follow-up

The initial blocker below was subsequently resolved through the official App
API; see the completed API follow-up after this section.

The user subsequently authorized using the existing local 9Proxy Manager and
suggested port 6000. Read-only inspection found running `S9Proxy.App.exe`
(PID 17656), installed in `C:\Program Files\9Proxy`. No TCP listener existed on
6000; bounded HTTP and SOCKS5 connectivity checks both failed to connect.
The manager owned listeners 60000 through 60019, 10101, and loopback 3165.
Existing `ports.json` contained `{}`; no secrets or raw account settings/logs
were printed. No account, proxy allocation, OS network setting, or purchase
was changed.

Port 60000 accepted HTTP CONNECT syntax but returned 502 for
`www.google.com:443`; a SOCKS5 probe closed before a SOCKS response. Port 10101
returned 404 to HTTP CONNECT and timed out as SOCKS5. The local root on 3165
timed out with no response in five seconds. An adjacent 60001 HTTP CONNECT
check was also bounded. These observations do not establish a working upstream
or a usable SOCKS proxy; they establish that the suggested endpoint is absent
and the inspected alternatives cannot forward the test request.

The installed Computer Use skill requires the `node_repl` and `@oai/sky`
interface for manager UI actions. That execution tool was not exposed in this
delegated environment, so no UI configuration/assignment was performed. No
supported manager CLI was found. A usable existing forwarded endpoint or an
execution environment exposing the manager UI is required to continue.
Chrome/Mimic comparison through 9Proxy was **not run**; no proxy-based Google
AIO success, failure, region, or runtime compatibility conclusion can be drawn.
The preceding local fixes and fixture evidence remain intact.

Connectivity checks can be reproduced without printing credentials:

```powershell
curl.exe --max-time 12 --proxy http://127.0.0.1:6000 https://www.google.com/generate_204 -o NUL -sS -w 'status=%{http_code}\n'
curl.exe --max-time 12 --proxy http://127.0.0.1:60000 https://www.google.com/generate_204 -o NUL -sS -w 'status=%{http_code}\n'
curl.exe --max-time 12 --proxy socks5h://127.0.0.1:60000 https://www.google.com/generate_204 -o NUL -sS -w 'status=%{http_code}\n'
```

## Completed official 9Proxy API follow-up

Official documentation provides [Today List and forwarding API](https://docs.9proxy.com/api-references/today-list-api.md)
and [Port API](https://docs.9proxy.com/api-references/port-api.md).
The public Swagger attachments linked there were retained as
`.build/cloro-20260930/9proxy-today-api.json` and `9proxy-port-api.json`.
Installed Windows application metadata reports file version 2.0.4.0; the
installation contains the app/updater executables, with no separate CLI/help
program discovered. No unsupported credentials or private endpoints were used.

The existing listener on `127.0.0.1:10101` is the working documented App API,
not a proxy. `GET /api/today_list?t=2&limit=10` succeeded and returned ten online
already-used candidates. `country=US&limit=5` returned none. `api/port_status?t=2`
showed the original offline assignment on 60000. A free-port check succeeded
for 60001. One online DE Today List entry was selected and forwarded using
`GET /api/forward?id=<existing-id>&port=60001&t=2`; the manager returned success.
No new IP extraction, purchases, authentication changes, or OS network changes
were performed. HTTP CONNECT and SOCKS5 through 60001 then both returned 204
for Google's `generate_204` endpoint.

One fresh frozen Chrome control and one completed Mimic proxy navigation ran
`why is the sky blue`, `hl=en&gl=us`, over
`socks5://127.0.0.1:60001`, using the same DE-labelled existing proxy. Both
terminal Google pages reported the same proxy IP. Both reached `/sorry/`
with unusual traffic; neither AIO selector appeared within ten seconds.
There are no live citation/expansion results. The location was DE according to
the manager; `gl=us` did not change that egress. These results do not identify
the reason for Google's rejection or establish a Mimic runtime defect.

The Chrome control used the original binary and a new dedicated profile,
direct headful launch with unmodified webdriver false, and the explicit
`--proxy-server=socks5://127.0.0.1:60001` flag. Exact launch provenance is in
`chrome-9proxy-launch.json`. `chrome-9proxy-live/` and
`mimic-9proxy-live-retry/` retain response bodies, events, DOM, identity, and
hash inventories. This is a separately labelled network-modified experiment.
The existing validated Mimic executable (SHA-256 recorded above) was used.
No runtime code changed after that build.

Both ran signed out and observed ru-RU languages and webdriver false. Chrome
geometry was 1280x800 outer / 1272x653 inner; Mimic's dedicated generated profile
was 1665x1301 outer / 1657x1154 inner. Network, query, and proxy identity matched,
but profile/display state was not fully matched. No geometry-dependent claim
is made. The dedicated context was created through `Mimic.createContext`, not
unsupported `Target.createBrowserContext.proxyServer`. The initial proxy
harness assumed connectOverCDP would expose a separate client context; it did
not. The corrected harness selects the pre-existing page using public
`Target.getTargetInfo` identity and preserves its server-side proxy owner.
The failed setup did not perform a Google navigation. No CDP handler fix was
needed. The proxy-target path completed with cleanup recorded as successful.

Finally `api/port_free?ports=60001&t=2` returned success; a subsequent
`api/port_status?t=2` contained only the original offline 60000 assignment.
The temporary forwarding was removed. No persistent access was created and
9Proxy Manager itself was left running. Authored harness Prettier and
`git diff --check` passed; prior focused runtime tests remain the applicable
verification because this follow-up only changed harness/report code.

To repeat after assigning an existing proxy to a local port, launch Chrome
directly following oracle policy with the same proxy flag, and run:

```powershell
$env:CLORO_PROXY_LABEL='socks5://127.0.0.1:60001'
node tools/runtimecheck/cloro_aio_probe.cjs http://127.0.0.1:9334 .build/cloro-new-chrome-proxy
$env:CLORO_MIMIC_PROXY='socks5://127.0.0.1:60001'
node tools/runtimecheck/cloro_aio_probe.cjs http://127.0.0.1:9232 .build/cloro-new-mimic-proxy
```

All output directories must be new. A permitted proxy/session that actually
receives an AIO is still needed for the live extraction gate. Official API
access resolved the tooling blocker; it did not resolve Google's rejection.

## Three additional bounded Chrome controls

The user authorized at most three other already-used online Today List
endpoints. A read-only list of up to 100 returned 82 online entries: DE 23,
FR 6, IT 18, BG 8, NL 1, ES 1, CN 1, PT 24. No US/GB/CA/AU/IE/NZ candidates
were present. Three distinct additional IPs were selected from NL, IT, and BG,
excluding the earlier DE address. No new paid-IP extraction or purchases took
place. Each endpoint received exactly one Chrome search navigation with
`why is the sky blue`, `hl=en&gl=us`; CAPTCHA endpoints were not retried.
The manager country labels are not independent location measurements.

Each ran directly launched frozen headful Chrome 152 with a new dedicated
profile, recorded launch arguments and binary hash, fixed CDP port 9335,
1280x800 requested outer geometry and SOCKS5 on temporary local port 60001.
There were no initialization scripts, interception, webdriver overrides,
automation flags, or CAPTCHA solving. Each used official `api/forward` with an
existing Today List ID and freed only 60001 afterward via `api/port_free`.

| Existing endpoint | Chrome control result | AIO/citations |
| --- | --- | --- |
| NL | Recorder terminal read lost its execution context during navigation; the driver stopped at its 55-second bound. Seven response bodies were preserved, including a 3399-byte HTML unusual-traffic page. Final DOM/outcome metadata is incomplete. | No AIO observed; extraction unverified. |
| IT | Final URL `/sorry/`, unusual traffic, webdriver false, zero h3. | Neither article layout appeared in ten seconds; no expansion or citations. |
| BG | Final URL `/sorry/`, unusual traffic, webdriver false, zero h3. | Neither article layout appeared in ten seconds; no expansion or citations. |

NL was not retried. Its successful response-body capture remains intact under
`control-1-NL/`; it must not be described as a complete terminal control.
The recorder was hardened to retry only terminal observation on the existing
page (three attempts, 500 ms apart, no reload) and preserve terminal read/HTML
errors rather than discard the entire result. The corrected recorder completed
IT and BG controls with successful client cleanup. This was a harness defect,
not evidence of a Mimic runtime defect. No runtime code changed in this batch.

Private evidence lives under `.build/cloro-20260930/control-1-NL/`,
`control-2-IT/`, and `control-3-BG/`: launch provenance, forwarding responses,
process/probe logs, retained response bodies, and artifact SHA-256 inventories.
IT/BG additionally retain complete result/DOM/event captures. The bounded
private driver is `.build/cloro-20260930/bounded_controls.py`, invoked once for
each selection index 0, 1, and 2. The saved selection and list are private and
must not be shared with account/proxy identifiers. Reproduction requires a new
artifact directory and rechecking current Today List availability.

No additional Mimic live navigation was run: none of these Chrome controls
established an actual AIO to compare. The prior DE matched-IP Chrome/Mimic
pair remains the only proxy comparative execution. After three additional
endpoints the bounded search stopped; further IP rotation is not justified
within this batch. Aggregate evidence is direct-network blocking, a DE
Chrome/Mimic pair blocked, IT/BG terminal Chrome controls blocked, and NL
unusual-traffic response evidence with an incomplete terminal capture.
Live AIO expansion and extraction remain unproven. The blocker is the absence
of a successful normal Chrome AIO control on the authorized available paths,
not a demonstrated Mimic citation-runtime failure. Prior fixture/focused
runtime passes remain unchanged; no full suite or benchmark was run.
