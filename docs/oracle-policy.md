# Chrome 152 oracle policy

Mimic targets observable behavior of normal desktop Chrome, not incidental
automation or headless behavior. The primary compatibility oracle is:

```text
Chrome 152.0.7977.82
Chromium r1669021 / d04cdb24d67b081f6cf80200ffc5233f44b61109
V8 15.2.124.21
Windows x64
headful
fresh controlled profile
chrome-152-windows-x64-headful-controlled-v1
1280x800 outer window (observed viewport is recorded)
no command-line feature overrides
```

## Authority and provenance

The semantic reference is Chrome without an attached debugger or automation
client. Preserve its observable browser semantics and bugs. CDP/DevTools
artifacts are evidence about tooling, not browser behavior to reproduce.
Mimic can execute without a display while exposing the coherent headful
Environment profile; implementation mode does not change that public contract.

Passive attachment, Runtime enable/disable, remote-object descriptions, previews,
property inspection and protocol serialization must not execute author code or
change author-visible state. This includes getters, Proxy traps, coercions,
lazy Error.stack materialization, Error.prepareStackTrace, exception formatting
and task/microtask ordering. Unsafe inspection must reject explicitly. Explicit
evaluation, function calls, installed bindings and requested browser mutations
have their requested effects; their transport must add no implicit author calls.

For tooling-sensitive probes, retain both unattached and attached controls.
Synthetic fixtures can report through local HTTP without a debugger. Regressions
must cover classes of effects, cold/restored realms and multiple sessions rather
than assert agreement with an instrumented Chrome side effect. Existing attached
captures remain valid evidence with their original provenance; they must not be
relabeled as unattached browser expectations.

Headful Chrome is authoritative whenever an observation can depend on window or
display state, focus, visibility, geometry, graphics, media/devices, activation,
permissions, rendering, browser UI integration, automation exposure, or
Navigator/platform capabilities. Headless Chrome is a non-authoritative, explicit
regression source. It may be used only after the exact probe is shown invariant,
or when the expected result comes from standards/IDL and cannot depend on
presentation mode. When uncertain, use `compatibility/oracle_differential.py`.

Every retained Chrome observation has `captureMetadata` containing the exact
Chrome, Chromium and V8 versions, platform, browser mode, feature overrides,
observed viewport/window, secure/isolation state and Environment profile ID.
Origins are never erased when observations are compared. Historical captures
whose full launch configuration cannot be reconstructed use an explicit
`historical-...-uncontrolled` profile ID and cannot become generic expectations.

## Differential classification

Before changing compatibility behavior, classify a Chrome ↔ Mimic difference:

1. Chrome-version semantic;
2. selected Environment-profile state;
3. headful/headless-mode state;
4. automation/tooling artifact;
5. nondeterministic machine/session state.

Only (1), and deliberately selected (2), may feed generic compatibility behavior.
A value differing between headful and headless must remain attached to its mode.
The three-way tool emits `invariant`, `headless-specific`,
`environment-specific`, or `Mimic divergence`; its rows retain all three values
and both Chrome capture metadata blocks.

## Capture workflow

### Normal browser launch and recording

For a normal desktop browser control, start the headful executable directly,
then attach the recorder to its existing CDP endpoint. Do not use Playwright,
Puppeteer, Selenium, or another framework's default browser launch: its flags
and profile changes are part of the observed environment. Connecting through
CDP does not require launching Chrome with `--enable-automation`.

Use a new, dedicated user data directory outside tracked project files and a
fixed, nonzero debugging port. A minimal argument set is:

```text
--remote-debugging-port=<fixed nonzero port>
--user-data-dir=<new private absolute directory>
--no-first-run
--no-default-browser-check
about:blank
```

For the canonical geometry above, also use `--window-size=1280,800` and record
the actual outer window, inner viewport, device scale, focus, and visibility.
Headful mode and a window hidden by the operating system are different facts;
record both. Do not infer presentation state from the absence of a headless flag.
Check the executable path and `/json/version` before navigating. Frozen Chrome
152 and the user's system Chrome are separate references, even when both work.

The normal control must not add `--enable-automation`, `--headless`, or
`--remote-debugging-port=0`. These launch conditions can expose automation
through `navigator.webdriver`. Do not inject a replacement getter, patch
prototypes, or use `--disable-blink-features=AutomationControlled` to conceal
that exposure. An apparently normal property value after a patch does not make
the rest of the environment equivalent to an ordinary browser. Verify the
unmodified `navigator.webdriver` value and retain its descriptor, user agent,
languages, exact process command line, executable hash, and profile provenance.
If it is unexpectedly true, classify the run as an automation control and
correct the launch conditions before collecting the normal reference.

Attach with a method such as `chromium.connectOverCDP(endpoint)`, rather than
`chromium.launch(...)`. Record whether the page belongs to the persistent
profile or a newly created browser context. An incognito context does not reset
process flags or the process's network state. A fresh incognito session requires
closing all previous incognito windows; it is not equivalent to a fresh process
and profile. When the user requests their Chrome through the Computer Use
extension, use that browser rather than substituting a separately launched one.
Record the extension's observation limits; a page view does not supply HAR or
Debugger events.

Enable recording domains before the first navigation and collect the complete
initial document, redirect chain, subsequent requests, cookies and client hint
state, exceptions, and terminal page state. Finish on an observable outcome with
a bounded timeout, not merely the first `load` event or an immediately empty
token field. Retain response bodies as requests complete. If a fast navigation
can discard the bootstrap body before `Network.getResponseBody` succeeds, use
`Network.streamResourceContent` where supported and retain both its initial
buffer and subsequent data events; record the collection method.

Keep the primary outcome run passive: no initialization scripts, deterministic
randomness, API wrappers, request fulfillment, or injected browser observations.
Use separate, explicitly labelled diagnostic runs for those operations. Record
all changes and their hashes. Local replay can establish executed behavior but
cannot establish acceptance by a live server. When forwarding or fulfilling
responses, verify cookies, response headers, navigation timing, and lifecycle
state in each runtime before treating the comparison as matched; the interception
mechanism can itself change these observations.

Preserve default networking in the normal control. Flags such as
`--disable-quic`, proxy settings, and framework routing can change protocols,
connection reuse, and server outcomes. `--log-net-log` is an additional diagnostic
condition and can show an unsupported command-line warning; record it and use a
separate run when needed. Do not interpret that warning alone as proof of the
server's rejection cause. Keep protocol, egress, profile, launch, and execution
differences separate when drawing conclusions.

The 2026-09-26 Search investigation demonstrated why these rules matter: a
fresh system Chrome 154 normal launch reached results, adding only
`--enable-automation` produced a rejection, and another fresh normal launch
reached results again. The successful and rejected runs both used HTTP/3.
A separate normal launch with QUIC disabled was rejected over HTTP/2. A directly
launched frozen Chrome 152 with unmodified `navigator.webdriver === false` also
reached results. These observations establish launch and networking as material
experimental conditions; they do not prove that a single JavaScript property or
protocol determines the server's decision. Raw captures remain private under
`.build/google-serp-success-20260926/`.

Immediately preserve each successful capture, its complete launch provenance,
and an artifact hash inventory before changing instrumentation. Reuse that
capture for offline analysis. Launch another live reference only for a specific
question the saved evidence cannot answer, and preserve the new run separately.

### Mode comparison for local semantic probes

Start the exact Chrome binary twice with separate fresh profiles and pinned launch
configuration when a local probe requires a headful/headless comparison, then run:

```powershell
python compatibility/oracle_differential.py `
  --headful http://127.0.0.1:9333 `
  --headless http://127.0.0.1:9444 `
  --mimic http://127.0.0.1:19222 `
  --probes compatibility/probes.json
```

Exposure and Navigator tools default to `--browser-mode headful`; headless must be
requested explicitly. Navigator operations run in separate pages with permissions
reset between probes, so permission UI, focus and previous operations cannot
contaminate later observations. Clipboard probes never persist clipboard content.

Use `python tools/check_oracle_captures.py` to reject retained Chrome captures
without the required provenance. Tests select the authoritative headful source by
default; `chrome152.NewForMode(state.BrowserModeHeadless)` and the explicit
headless capture exist only for mode-scoped regression work.

The environment model selects a coherent presentation profile. It does not apply
property-by-property “not headless” patches: Product/UA, display/window, graphics,
permissions and capability behavior all derive from the selected headful or
headless Environment.
