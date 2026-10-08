# Mimic renderer fork

This directory is a source copy of `github.com/go-webengine/engine` v0.4.3
(BSD-3-Clause). The module path stays upstream's so imports remain ordinary Go
imports. The root module and renderer POC select this copy with `replace`.

Mimic keeps the fork on Go 1.26.4. The upstream v0.5.1 rotated-content
buffer fix is applied in `paint/paint.go`, with a focused regression test.
The `v0.5.0` dependency update to `klauspost/compress` v1.18.7 is included.

Additional local fixes address shared rendering causes observed on a live
Wikipedia page:

- `grid-template` shorthand sets rows, columns and named areas.
- `@supports` evaluates the grid and URL mask capabilities the engine actually
  implements, including `not`, `and` and `or` conditions. Unknown capabilities
  remain false.
- SVG images use the existing gfx rasterizer for compound paths, retaining the
  previous rasterizer as a fallback when gfx cannot parse a document.
- Isolated absolute elements retain their border-box height when layout changes
  their sizing mode internally. Fully transparent inline controls do not paint
  their native control underneath a styled replacement.
- Absolute CSS font-size keywords resolve independently of the parent size,
  and inline margins survive nested inline-block and inline-flex layout.

The screenshot adapter opts into literal text from CSS `::before` and `::after`
after script execution. These generated boxes participate in the final layout
and paint without entering the script-visible DOM. CSS counters, `attr()`, URLs,
and other nonliteral `content` values remain unsupported.

The screenshot adapter in `internal/renderer` also resolves asset URLs from
snapshot stylesheets against their `assets/` directory. The fork remains an
independent approximate painter and does not define Mimic's DOM or layout
semantics.
