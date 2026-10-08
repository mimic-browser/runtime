# Approximate screenshots

The standard CDP `Page.captureScreenshot` command returns a PNG in its usual
base64 `data` field, generated on demand from the Page's current static DOM
and resource snapshot. Ordinary CDP and Playwright clients can call it without
a Mimic-specific command or response parser. The renderer never runs page
scripts or fetches outside the snapshot bundle. No GPU is required.

**This is a visual preview, not a browser screenshot. Visual accuracy is not
guaranteed.** Mimic does not maintain
painted pixels. The independent renderer recalculates CSS and layout, so the
image can disagree with `getBoundingClientRect()`, computed styles, hit testing,
and Chrome 152. Text, fonts, Grid/Flex layout, iframe content, canvas/video
pixels, images and advanced effects can be missing or different. Do not use
these PNGs as pixel-accurate visual test expectations or as evidence of what a
website observed through Mimic's JavaScript APIs.

The current CDP scope supports PNG at one output pixel per CSS pixel, the
current viewport, and unscaled document-coordinate `clip` rectangles. Requests
for JPEG/WebP, a quality setting, scaled clips or view-surface capture return an
explicit error. Viewport dimensions and clip dimensions are limited to 4096
pixels per axis; clip origins are limited to 16384 CSS pixels. A clipped
screenshot can extend beyond the viewport when the
snapshot renderer has content there. The result may differ from Chrome even
when all resources are available. Snapshot resource warnings are recorded in
Mimic's diagnostic trace without changing the standard CDP response.

The screenshot is useful for human inspection, agent visual context and rough
reports. For a browser-rendered reference, use Chrome directly or the
[dev preview](dev-preview.md), which displays a DOM mirror in the viewer's own
browser and has its own documented limitations.

The painter uses the maintained [renderer fork](https://github.com/mimic-browser/renderer)
of `go-webengine`, based on v0.4.3 and retained on Go 1.26.4 with selected
fixes. Its visual rules are independent of Mimic's Page semantics.
