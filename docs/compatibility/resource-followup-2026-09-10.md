# Resource and document compatibility follow-up — 2026-09-10

Reference: frozen Chrome 152.0.7977.82. This follows `svg-audit-2026-09-10.md`
and the retained `manual-20260910-051107` capture. It is a general compatibility
batch, not evidence of a successful Cloudflare challenge. The manual capture
contained no SVG unsupported exceptions; its SVG text ink notices were still
approximation notices. A 403 and DNS failures do not identify a server rejection
cause.

## Implemented and measured

| Area | Coverage |
| --- | --- |
| Document | Active/inert HTML and XML state; designMode and XML setters; legacy body colors; root/scrolling element defaults; canonical custom-element registry; stable document-owned object identities; fullscreen/PiP/pointer-lock idle state; Last-Modified response timestamp |
| Owned CSS sheets | HTML/SVG style and loaded link sheets; live document/shadow lists; rule mutation affects computed style; disabled/media state; owner identity; text replacement; cross-origin rule-access boundary |
| Colors | Standard named colors shared by CSS and Canvas; hex, numeric RGB/HSL parsing; inherited computed color including borrowed cross-frame getComputedStyle |
| Fonts | Local full/PostScript names; binary and URL resources; SFNT/WOFF/WOFF2 decoding; collection ownership; promises/status/events; active document feeds the existing CPU shaping engine; unicode-range selection; resource deduplication and engine isolation |
| Images | PNG/JPEG/GIF/WebP resource decoding; SVG intrinsic metadata without SVG rendering; complete/currentSrc/natural dimensions; load/error/decode; decoded raster copy into Canvas and ImageBitmap; origin-clean propagation and dimension-reset behavior |
| Local resources | data URLs use the common loader and trace path, without HTTP transport; percent and forgiving base64 decoding; default media type; Fetch headers project identically into Goja/V8 |
| Screen/focus | Single-screen profile origin/extension/orientation observations; stable orientation object; HTMLElement tabIndex parsing for basic elements; clientInformation identity; Notification.permission reads configured permission state |

New differential fixtures: `platform_state`, `document_state_mutations`,
`owned_stylesheets`, `css_named_colors`, `font_loading` (window and worker),
`font_metrics_loading`, `font_events`, `image_resources`, `image_intrinsic`,
`data_resources`, and `screen_focus`. Native results and fixture hashes are in
`compatibility/captures/semantic-checkpoints`. The browser tests exercise both
Goja and V8. Font-container tests cover the same upstream fixture in all three
containers, registration identity and cross-engine isolation. Image tests cover
pixel bytes, rejected input, CORS readback, bitmap propagation, resets, request
replacement and load ordering.

The font converter is pinned to github.com/tdewolff/font at
v0.0.0-20241125190050-d899fdc808fc. Only its CPU container conversion is used;
there is no native rendering dependency. Compressed font tables are bounded
before conversion, decoded resource storage retains existing per-engine limits,
and independent Pages/Workers do not share mutable font engines.

## Remaining boundaries — not closed by this batch

This is not complete browser compatibility. In particular, introducing a getter
must not be mistaken for implementing all operations of its interface.

* SVG text ink and curved path approximations, complex text layout, SMIL,
  filters, scene intersection/selection, complex screen transforms and other
  renderer-dependent limits from the SVG audit remain. SVG image pixels throw
  NotSupportedError and emit `Canvas2D.vectorImageReadback` with a reason.
* Complex font descriptor effects (variation/features, stretch, metric overrides,
  size adjustment) are not silently used as ordinary fonts: selecting such a
  registered face fails CPU measurement with the descriptor names. Font CSS
  `@font-face` discovery, comprehensive shorthand/source grammar, variable-font
  selection and exact font-loading/layout task ordering need separate coverage.
  Loaded worker fonts do not yet feed OffscreenCanvas text observations.
* Image animation timelines, EXIF orientation, AVIF/ICO, color-management details,
  srcset/sizes selection, font-relative SVG intrinsic dimensions, all CSS layout
  interactions, and decode cancellation on arbitrary source mutation remain.
  Per-image budgets do not constitute a total Page image-memory budget.
* Canvas scaling/smoothing and path/text raster observations, WebGL shader
  observations and WebGPU execution remain bounded by the existing observation
  model. WebGPU texture support, WGSL feature enumeration and the newly observed
  supported-limit fields were not implemented in this batch.
* Owned-sheet detach/reattach with no intervening sheet observation, same-text
  replacement, link CORS/redirect details, @import and complete CSS cascade/media
  semantics need further lifecycle tests. The current implementation is not a
  full layout or style engine.
* DocumentTimeline/ModelContext operations, fullscreen/PiP transitions, quirks-mode
  potentially-scrollable-body rules, multiple displays/orientation events and
  orientation lock were not implemented. The new getters cover measured idle
  state and identities; existing unsupported operations remain explicit.
* MediaSource and RTP capability queries, Navigator.cpuPerformance, per-isolate
  Performance.memory, structuredClone unsupported types, CDP debugger resume and
  the HTMLDDA behavior of Document.all remain independent work. Missing properties
  that Chrome itself lacks (for example Document.host) must not be added simply
  to erase access diagnostics.

None of these remaining items is evidence by itself of why clearance was
rejected. A future trace should be compared against this inventory and the
recorded native fixtures, not patched for a challenge's particular values.

## Reproduction and validation

Native capture example (existing frozen CDP endpoint on port 9343):

```powershell
python -X utf8 tools/compatibility/capture_query_oracle.py internal/browser/testdata/image_intrinsic_oracle.js compatibility/captures/semantic-checkpoints/image-intrinsic-chrome152.json --secure-context
go test ./... -count=1
go test -race ./internal/browser ./internal/network ./internal/textmetrics ./internal/imageresource -run 'Test(DocumentCompatibilityOracle|Font|WebFont|Image|DetachedImage|ConstructedStylesheet|SVG|DataResources)' -count=1
go build -o .build/mimic.exe ./cmd/mimic
```

Validation on 2026-09-10:

* Full `go test ./... -count=1`: PASS (browser package 198.394 s).
* Native differential fixtures run on both Goja and V8: PASS; all 12 new
  capture versions and LF-normalized source hashes verified. The existing
  provenance checker also passes its 23 retained captures.
* After that suite started, native-function metadata and image credentials/blob
  origin handling received final corrections. Focused font/image tests passed;
  the image load/readback/cancellation race check passed (4.415 s).
* Extended SVG/document/font/image/network race run: PASS (browser 179.631 s;
  network 2.370 s; textmetrics 1.572 s; imageresource 1.212 s).
* Fresh `.build/mimic.exe` built successfully. Private captures, local
  verification logs and the executable are excluded from Git.
