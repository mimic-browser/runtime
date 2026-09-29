# Mimic v0.1.9

Changes since v0.1.8:

## Browser compatibility

- Improved frame lifecycle ordering and shared observations across connected realms. Restored contexts now retain the correct Page and document state through navigation.
- Implemented `Element.insertAdjacentElement` and corrected state snapshot attributes and PNG resource classification.
- Preserved the Chrome 152 Web API surface and source-site diagnostics during lazy binding and snapshot restoration.

## Runtime and performance

- Fixed an address reuse race in V8 snapshot cleanup that could remove a live callback table while independent Pages started concurrently.
- Reduced memory retained by V8 isolates, bootstrap snapshots, DOM mutation journals, and font data. Connected Page worlds now share a runtime owner without sharing mutable Page state.
- Removed measured host-call and geometry overhead, including unnecessary argument boxing and repeated DOM/style reads.
- Restored ready bootstrap snapshots for the first wave of Pages. A focused paired 50-Page check measured lower first-wave RSS, CPU, and elapsed time; the [performance report](https://github.com/moreveal/mimic/blob/v0.1.9/docs/performance/report.md) records the method and limitations.
- Published a dated [Mimic vs. Chrome checkpoint](https://github.com/moreveal/mimic/blob/v0.1.9/benchmark/runs/13-rss-20260929/public-summary.md). Its completed static 50-Page series measured 748.11 MiB active RSS and 108.67 sessions/s for Mimic, versus 4102.04 MiB and 18.09 sessions/s for Chrome. The checkpoint used a separately identified development binary; these figures are not measurements of the v0.1.9 download. Later density series stopped at the benchmark's memory guard and are not successful comparisons.

## Verification and provenance

- The Windows and Linux executables come from the same successful CI source revision. Release packaging separately verifies both extracted archives, three JavaScript engines, public examples, and archive checksums.
- The release manifest records the binary source revision, packaging revision, CI run, checks, and hashes. The executable banner identifies its source revision; the archive and release tag carry `v0.1.9`.

Mimic remains a renderer-free public beta for Windows and Linux amd64. See the [compatibility boundaries](https://github.com/moreveal/mimic/blob/v0.1.9/docs/compatibility.md). Full source changes: [v0.1.8...v0.1.9](https://github.com/moreveal/mimic/compare/v0.1.8...v0.1.9).
