# Mimic v0.2.4

Changes since v0.2.3:

- Keep internal snapshot preparation contexts and pages out of browser automation discovery. Creating and closing application contexts now produces a stable public context list while the runtime prepares its cache.
- Wait for internal snapshot preparation and page cleanup during shutdown, preventing overlapping preparation from retaining resources or using a closed realm.
- Complete accepted page, context and browser close commands even when the automation client immediately disconnects, while still cancelling ordinary session work on disconnect.
- Add PNG screenshots through the standard CDP `Page.captureScreenshot` command. They reflect the current DOM after script interactions, but visual accuracy is not guaranteed; the separate renderer does not represent Mimic's script-visible layout. See [approximate screenshots](docs/approximate-screenshots.md) for the supported scope.
- Improve approximate painting of SVG, CSS grid sidebars, styled controls and literal `::before`/`::after` text. The screenshot renderer now uses a maintained, static-only `go-webengine` fork without its browser, JavaScript or module-bundling packages.
- Preserve resource timing order when separate fetch starts resolve to the same exposed `startTime`.

Mimic remains a public beta for Windows and Linux amd64. Browser automation support follows the documented CDP scope. Full source changes: [v0.2.3...v0.2.4](https://github.com/mimic-browser/runtime/compare/v0.2.3...v0.2.4).
