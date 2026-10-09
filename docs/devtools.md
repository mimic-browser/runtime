# Chrome DevTools

Chrome's ordinary DevTools frontend can attach to Mimic's page CDP endpoint.
Elements, Styles, Console and Network read the same state used by scripts and
automation clients. Live edits change Mimic's canonical DOM and CSSOM.

## Connect

Start Mimic with its usual CDP listener:

```sh
mimic -listen 127.0.0.1:9222
```

In Chrome, open `chrome://inspect/#devices`, configure `127.0.0.1:9222`, and
inspect the desired page. Discovery responses include `devtoolsFrontendUrl`.
If discovery does not show the target, obtain its exact bundled frontend URL:

```powershell
(Invoke-RestMethod http://127.0.0.1:9222/json/list)[0].devtoolsFrontendUrl
```

Paste that URL into Chrome's address bar. The endpoint also works with ordinary
raw CDP clients; no Dev Preview server or WebDriver is required.

## Optional page view

The DevTools page view uses an external Chrome/Chromium process to render an
inert presentation of Mimic's current DOM. Configure an executable explicitly
when it is not discoverable:

```powershell
.\mimic.exe -listen 127.0.0.1:9222 -devtools-chrome "C:\Program Files\Google\Chrome\Application\chrome.exe"
```

`MIMIC_DEVTOOLS_CHROME` is the equivalent environment variable. Without either
setting, Mimic checks the executable search path and common Windows Chrome/Edge
locations when a view is requested.

Enabling DOM, CSS, Network, Runtime or Overlay does not launch Chrome. Only an
explicit `Page.startScreencast` request creates the presentation process; the
DevTools frontend sends this request when its page view is opened. Each active
view owns its process and fresh temporary profile. A missing renderer produces
an explicit screencast error; DOM, Console and Network inspection still work.

The presentation receives sanitized DOM, current canonical CSSOM, shadow/form
state and a resource bundle obtained through Mimic's loader. Source scripts and
inline event handlers are removed, CSP blocks source execution, and renderer
page requests outside that bundle are rejected. Renderer DOM identifiers never
become public Mimic node identifiers. View clicks are hit-tested in Blink, then
dispatched to the selected canonical Mimic frame and node. Inspect mode selects
that node without activating its author click handler.

The renderer has no GPU requirement. It is a presentation consumer, not another
execution of the website. The view rebuilds an inert document for changed
snapshots, so it is not a compositor mirror: canvas/WebGL readbacks, video frames,
animation progress, renderer focus and selection are not copied. Blink view
geometry can differ from Mimic's script-visible geometry. Ordinary protocol
input outside an active view continues to use Mimic's geometry.

## Inspection and editing

- Elements exposes document/frame ownership and open or closed shadow roots.
  Attribute, text, HTML and removal edits affect canonical nodes. Search supports
  selectors and tag/attribute/text matches, with explicit bounded result storage.
  Navigation and frame removal invalidate old handles.
- Styles exposes author/adopted stylesheet matches, inherited styles, inline and
  computed values, and before/after rules. Declaration and selector edits
  preserve canonical CSSRule and CSSStyleDeclaration identity. Text/ranges refer
  to normalized inspector CSSOM source, rather than original authored formatting.
- Console evaluates in the selected realm, expands property descriptors without
  invoking getters, and supports object groups, Promise waits and exceptions.
  With V8, command-line evaluation uses its native temporary inspector scope:
  `$0` through `$4` refer to recent selections, and `$_` retains the last result
  in the console object group. Global lexical declarations and author property
  precedence are preserved. Foreign-frame selections use the canonical cross-realm
  reference bridge; retired realm selections are pruned.
- Network exposes requests, redirect chains, response headers and completed
  bodies from Mimic's loader. POST history is retained only for enabled sessions,
  with limits of 128 entries, 4 MiB total and 1 MiB per body. Resource content is
  scoped to its current frame/document and the existing bounded response cache.
- The active view supports node picking and basic node highlighting. Advanced
  grid/flex/container overlays are not implemented.

Breakpoints, pause/step, source debugging and profiler controls are unsupported.
Other material boundaries include undo/redo, XPath inspector search, UA stylesheet
and shadow-tree inventory, forced CSS pseudo states, native font/animation
attribution, Blink-specific Console DOM helpers, complete network extra-info
events and durable/streaming body storage. Multi-edit CSS batches apply
sequentially without rollback. Unsupported backend requests return protocol
errors; frontend panels may show reduced functionality.

The [generated CDP matrix](cdp-coverage-generated.md) is the authoritative
per-command support registry, including limitations and focused test evidence.

## Resource ownership and validation

Inspector projections, stylesheet text handles, search results and watches are
allocated on demand. Unchanged Page turns do not scan DOM or style trees.
Screencasts keep one unacknowledged frame and coalesce newer changes; a slow
viewer does not hold the Page command boundary or accumulate frame queues.
There is no periodic rendering timer while idle. `everyNthFrame` values other
than `1` are explicitly rejected for this change-driven view.

DOM/CSS disable and disconnect release their inspector state. Stopping the view,
disabling Page, detaching or disconnecting joins renderer teardown and deletes
its dedicated profile. Pages retain independent execution and ownership.

The view follows canonical window and element scrolling and captures the current
viewport, rather than the document origin. Requested image dimensions select a
direct raster scale up to 2x; JPEG/PNG format and JPEG quality are honored. This
avoids enlarging a low-resolution bitmap on high-density viewers. JPEG remains
lossy. Closed shadow-root scroll offsets are not restored by the presentation
script; canonical shadow DOM inspection remains available.

Run the focused regression group:

```sh
go test ./internal/cdp -run '^TestInspector' -count=1
python tools/generate_cdp.py --check
go test ./internal/cdp -run '^(TestProtocolSupportManifestHasLiveEvidenceAndNoLostHandlers|TestProtocolCoverageUsesCompleteGeneratedInventory)$' -count=1
```

Renderer tests require a local Chrome/Chromium executable and otherwise report
a skip. Set `MIMIC_DEVTOOLS_CHROME` to make the tested executable explicit.
The tests cover wire commands/events, canonical edits and identities, frame/shadow
ownership, navigation, Console helpers, Network bodies, rendered pixels, picking,
ACK backpressure, unused/idle allocation cost and teardown. Presentation checks
use the external renderer as an integration dependency, not as a behavioral oracle.
