# Mimic v0.2.2

Changes since v0.2.1:

- Build release executables from the release tag so the startup banner and `Mimic.getVersion` report `v0.2.2`. Release packaging now rejects development versions, mismatched revisions, and dirty binaries, including reused CI artifacts and extracted packages.
- Preserve canonical DOM tree state when page scripts override public accessors, including iframe insertion, connectivity, viewport observations, mutation records, event paths, and removal. Resolve quirks-mode percentage heights through auto-height block containers. These fixes let the captured HUMAN widget complete its press-and-hold execution path without runtime errors; acceptance of a fresh server challenge remains unverified.
- Preserve CSS length comparison functions (`min`, `max`, `clamp`) in inline styles and stylesheet CSSOM, including mixed units, nested calculations and pending custom-property substitution. This fixes responsive dimensions and typography disappearing from the live developer preview, including a hero image whose container collapsed to zero height. Focused regression tests retain normal headful Chrome 152 observations and verify preview serialization.
- Queue beacon requests and load applied CSS background images through the document resource lifecycle.
- Avoid unused body streams for beacon uploads. Preserve binary snapshots, borrowed Navigator operations and keepalive quotas across engines. Transport regression tests check each accepted upload independently of page setup time.
- Admit CSS background discovery from canonical style inputs before entering JavaScript. Unstyled pages no longer run unrelated style callbacks during task checkpoints, fixing regressions when page code replaces JavaScript intrinsics. CSSOM changes and linked sheets continue to trigger resource discovery.
- Support XPath evaluator construction and compiled attribute predicates.
- Add the contributor Compatibility Doctor and field-evidence workflow. Keep Go metadata inspection offline and report inspection failures without making them fatal.

Mimic remains a renderer-free public beta for Windows and Linux amd64. The developer preview is drawn by the viewer's browser; this release does not add a pixel renderer or full responsive image candidate selection to Mimic. Full source changes: [v0.2.1...v0.2.2](https://github.com/mimic-browser/runtime/compare/v0.2.1...v0.2.2).
