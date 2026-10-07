# Mimic v0.2.2

Changes since v0.2.1:

- Preserve CSS length comparison functions (`min`, `max`, `clamp`) in inline styles and stylesheet CSSOM, including mixed units, nested calculations and pending custom-property substitution. This fixes responsive dimensions and typography disappearing from the live developer preview, including a hero image whose container collapsed to zero height. Focused regression tests retain normal headful Chrome 152 observations and verify preview serialization.
- Queue beacon requests and load applied CSS background images through the document resource lifecycle.
- Support XPath evaluator construction and compiled attribute predicates.
- Add the contributor Compatibility Doctor and field-evidence workflow. Keep Go metadata inspection offline and report inspection failures without making them fatal.

Mimic remains a renderer-free public beta for Windows and Linux amd64. The developer preview is drawn by the viewer's browser; this release does not add a pixel renderer or full responsive image candidate selection to Mimic. Full source changes: [v0.2.1...v0.2.2](https://github.com/mimic-browser/runtime/compare/v0.2.1...v0.2.2).
