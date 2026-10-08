# Deferred renderer experiment

This isolated Go module tests `go-webengine/engine` as a possible on-demand
screenshot backend. It reads HTML from a file, disables the engine's separate
JavaScript execution, and renders the document to PNG. It does not connect to
Mimic's canonical Page state or CDP.

From this directory:

```sh
go run . -input fixture.html -output screenshot.png -width 1440 -height 900
```

The fixture exercises Grid, Flexbox, text, gradients, shadows, rounded corners,
chart bars and a table-like transaction list. The engine does not interpret the
fixture's original `font` shorthand or percentage-height bars inside the flex
chart. This experiment expands the font declaration and uses explicit bar heights
as input workarounds. It also crops the engine's full-document output to the
requested viewport. These changes do not fix the engine's CSS implementation.

The screenshot is compared locally against frozen Chrome 152 in explicitly
headless mode. The engine still lays out the lower two-column Grid differently,
uses a different font face, and paints text with different metrics. Headless
Chrome is a visual diagnostic here, not Mimic's authoritative headful oracle.
