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
percentage-height chart bars and a table-like transaction list. On the tested
engine commit, the page renders to 1440 x 1080, but the intended sans-serif font
falls back to serif and the percentage-height bars are absent. The engine grows
the image to the full document height, so a viewport screenshot would need a
crop. These are concrete compatibility and integration limits, not Chrome 152
reference measurements.
