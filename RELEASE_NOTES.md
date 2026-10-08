# Mimic v0.2.4

Changes since v0.2.3:

- Keep internal snapshot preparation contexts and pages out of browser automation discovery. Creating and closing application contexts now produces a stable public context list while the runtime prepares its cache.
- Wait for internal snapshot preparation and page cleanup during shutdown, preventing overlapping preparation from retaining resources or using a closed realm.
- Complete accepted page, context and browser close commands even when the automation client immediately disconnects, while still cancelling ordinary session work on disconnect.

Mimic remains a renderer-free public beta for Windows and Linux amd64. Browser automation support follows the documented CDP scope. Full source changes: [v0.2.3...v0.2.4](https://github.com/mimic-browser/runtime/compare/v0.2.3...v0.2.4).
