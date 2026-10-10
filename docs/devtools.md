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

## Presentation boundary

DevTools inspection does not launch an external browser or render page images.
Screencasts, visual node picking and highlighting are unsupported. Elements,
Styles, Console and Network operate directly on Mimic's canonical state.
External rendering and frame delivery do not meet the lightweight runtime's
resource and interaction requirements.

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
DOM/CSS disable and disconnect release their inspector state. Pages retain
independent execution and ownership.

Run focused inspection and protocol coverage tests:

```sh
go test ./internal/cdp -run '^TestInspector' -count=1
python tools/generate_cdp.py --check
go test ./internal/cdp -run '^(TestProtocolSupportManifestHasLiveEvidenceAndNoLostHandlers|TestProtocolCoverageUsesCompleteGeneratedInventory)$' -count=1
```
