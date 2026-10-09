# CDP compatibility

For context creation and dynamic environment overrides, see the
[versioned `Mimic.*` profile contract](environment-profiles.md). Custom commands
have their own strict parameter validation; the frozen Chrome schema is unchanged.

The pinned reference is Chrome **152.0.7977.82**, Windows x64, Chromium
r1669021 / d04cdb24d67b081f6cf80200ffc5233f44b61109. The current automation
checkpoint passes **34/34 Pyppeteer 2.0.0 checks and 34/34 Puppeteer 25.10.0
checks**, matching the same 68 checks on pinned Chrome. A separate raw-protocol
interception suite passes **16/16**. These are measured workflow results, not a
claim of compatibility with every option of the methods involved.

The [retained final receipt](compatibility/cdp-automation-20260912/final.json)
records binary, source, oracle and report hashes. The
[runner documentation](../compatibility/cdp_automation.md) explains the local
fixture, fixed client versions and reproduction commands. The retained
[client reference](../compatibility/cdp_automation_reference.json) and
[interception reference](../compatibility/cdp_automation_interception_reference.json)
contain exact Chrome provenance. The earlier expanded baseline passed 13 of 66
checks; the current suite additionally exercises uncaught page errors in both clients.

## Playwright CLI workflow checkpoint

Puppeteer `browser.target().createCDPSession()` is also supported through the
browser target's discovery and explicit attachment paths. A browser session
survives closing the initial page. See the [Chrome observations and focused
checks](compatibility/browser-target-20260913/README.md) for the measured scope.

`compatibility/playwright_cli.cjs` exercises the actual CLI daemon, including
the snapshot performed after attach. With `@playwright/cli` 0.1.19 and its
Playwright 1.63.0-alpha-2026-08-31 dependency, the complete workflow succeeds
against Chrome 152.0.7977.82 and Mimic: attach, HTTP navigation, evaluation,
role/name-based fill and click, changed-content snapshot, reload, tab creation,
listing and closure, detach, reattach and another snapshot.

Run with Node, the installed `@playwright/cli/playwright-cli.js` path, a CDP
endpoint and a unique session name. The runner serves its own local fixture.
Use a dedicated browser instance: navigation and tab operations modify it.

The page target and top frame share their canonical ID. Isolated-world CSS/input
observations initialize the deferred main-world owner before using its bridge.
Label associations are live DOM queries; point queries share the existing input
stacking/box model rather than introducing another geometry model.

This checkpoint does not claim every CLI command or complete Chrome layout.
Point queries currently cover the document's modeled boxes, not shadow-root
retargeting or arbitrary visual clipping. Visibility supports box presence,
ancestor display/content-visibility hiding and optional visibility/opacity checks;
offscreen `content-visibility:auto` skipping is not modeled. File chooser
interception remains explicitly unsupported (this CLI tolerates that error).
Approximate PNG screenshots are available through the standard CDP
`Page.captureScreenshot` command; see [their scope and limitations](approximate-screenshots.md).
Video and rendered PDFs remain unsupported.

For interactive Elements/Styles editing, Console and Network inspection, see
[Chrome DevTools](devtools.md). Its optional external Blink page view starts only
when requested and releases its process/profile when stopped or disconnected.

## Generated schema and semantic scope

The complete runtime `/json/protocol` schema has **58 domains, 665 commands,
234 events and 611 named types**. Go wire types, validation descriptors and the
complete status inventory are generated offline from a retained exact-Chrome
schema. The older frozen bundle remains unchanged; its legacy stable protocol
and incomplete V8 PDL projection are not used as the new wire authority.

See the [generated domain and command matrix](cdp-coverage-generated.md) and
[complete machine-readable inventory](../internal/cdp/protocol_inventory_generated.json).
The inventory covers every schema command and event, including unsupported ones.
Current support counts are maintained in the generated matrix. Schema presence
does not imply semantic support.

`Mimic.getCompatibilityMatrix` returns the same generated statuses, scope notes
and evidence references. `wireSchemaGenerated` and `surfaceRegistered` describe
schema availability only. The legacy `semanticsImplemented`/`semanticsVerified`
booleans remain false for partial support. A test reference identifies a focused
regression, not exhaustive equivalence with Chrome. Missing declarations default
to unsupported; adding a handler requires a reviewed scope declaration.

Diagnostic trace capture is disabled by default. Send `Mimic.startTrace` on a
Page session before the work to capture, then read `Mimic.getTrace`. Capture
retains the most recent 8192 events and the first 128 IDs of large selector
results together with their full count. `Mimic.stopTrace` stops capture while
keeping the recorded events available for reading; `Mimic.clearTrace` clears
them. These Mimic extensions are outside the pinned Chrome protocol schema.
Live CDP events and resource Performance entries continue independently of
diagnostic capture.

Unknown methods return `-32601`; invalid wire parameters return `-32602`.
Known methods without a semantic handler return an explicit unsupported error.
A few client subscription/activation acknowledgments have no matching browser
subsystem and are explicitly marked unsupported in the inventory even though
the command acknowledges. In particular, Audits and WebMCP subscription calls do
not imply an issue detector or model-context backend.

## Automation behavior covered

| Area | Current behavior | Remaining boundary |
|---|---|---|
| Target/session transport | Independent Pages and BrowserContexts, page/tab hierarchy, nested and flattened sessions, separate command IDs, discovery, auto-attach and disposal | No worker/service-worker/OOPIF/prerender target graph or native window management |
| Runtime | Live handles and groups, object identity, descriptors without getter invocation, unserializable values, promises, exceptions, console arguments and exposed bindings | Previews, V8 private/internal slots, deep serialization, side-effect checks and full debugger options |
| Isolated worlds | Independent globals and wrappers over one canonical DOM, form/focus state and Page event loop; navigation and snapshot restoration preserve ownership | Universal access grants and complete cross-world listener registration ordering |
| Page/navigation | Navigation, load/lifecycle events, reload, history, init scripts/removal, document content, child frames and 500ms shared-loader network-idle windows | Full navigation option/error vocabulary, BFCache event fidelity and every lifecycle timestamp |
| DOM/input | Selector evaluation, node/handle conversion, attributes/mutations, modeled boxes, trusted keyboard/text/mouse input, selection and ordinary form defaults | Full layout, multiple fragments, SVG/transform quads, touch/pen/wheel/drag/IME/contenteditable and rich editing |
| Network | URL/stage/type interception, continue/fulfill/abort, response bytes, headers, offline/cache controls, cookies and scoped BrowserContext storage | Auth challenges, stream handles, complete redirects/extra-info events, latency/throughput emulation and every cookie metadata field |
| Emulation | Canonical viewport, scale, screen orientation, user agent, languages/platform, Client Hints and selected media preferences | Full mobile layout, touch, display posture/features, all metadata/query-change behavior |
| TLS policy | Measured root-versus-page certificate-ignore scope, separate verified/unverified connection pools, reset when subscriptions detach | No complete Security event/inspection domain |

All observations use the same underlying Page/DOM/loader state. In particular,
protocol handles do not serialize an object into a second mutable copy, a
fulfilled response remains the loader response, and cookies/headers/emulation
seen by page JavaScript agree with the corresponding network/control state.

## Trusted input and geometry

The input dispatcher runs on the Page event loop. Dirty form values and focus
are owned by the document main world; isolated-world getters/setters and native
input operate on those same slots. Bootstrap snapshots rebind world ownership
before page scripts run. Trusted events are projected into realm-local wrappers,
with the default action applied once; cancellation is observed before editing.
Cross-world listeners are currently delivered in groups by world, so global
registration ordering across worlds is not claimed.

Twelve retained Chrome keyboard/text cases cover selection replacement,
keydown/keypress/keyup, raw-key/char dispatch, cancellation, blur/change,
select-all, beforeinput mutations, empty insertion and UTF-16 maxlength behavior.
Focused regressions cover isolated-world typing/select/click, restored snapshots
and persistent input suppression. Synthetic `HTMLElement.click()` remains
untrusted, and inline event handlers pass through the canonical CSP policy.

Default Windows form-control dimensions were measured against the same Chrome.
They use the existing text shaper and the same modeled rectangle used by
`getBoundingClientRect`, iterable `getClientRects`, CDP quads and hit testing.
This adds consistent automation observations without a full layout engine;
arbitrary CSS flow, occlusion, fragments and rich editing remain outside the
current claim. `Page.captureScreenshot` returns an approximate PNG from the
current Page snapshot, independent of the modeled geometry. PDF rendering,
screencast and video output remain unsupported.

## Updating and checking coverage

[Generation instructions](cdp-protocol-generation.md) describe explicit oracle
refresh, source hashes and parameter-decoder rules. Ordinary regeneration and
checks are offline:

```powershell
python tools/generate_cdp.py
python tools/generate_cdp.py --check
python -m unittest discover -s tools -p test_generate_cdp.py
go test ./internal/cdp -run 'TestProtocol|TestGeneratedProtocol' -count=1
```

Edit `internal/cdp/protocol_support.json` for semantic claims, then regenerate.
The generator requires scope notes for supported entries and regression evidence
for implemented entries. Integrity tests check evidence references, handler
inventory, schema completeness, deterministic artifacts and immutable runtime
matrix projections. Real-client and Chrome comparisons remain necessary when
changing behavioral support.
