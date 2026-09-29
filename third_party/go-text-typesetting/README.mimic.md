# Mimic's go-text/typesetting fork

This directory is a source fork of `github.com/go-text/typesetting` v0.3.4.
The upstream license is in `LICENSE`. Tests and sample assets were omitted
except for the focused metrics-only tests and their variable-font fixture.

Mimic's change adds `font.NewFontMetricsOnly`. It uses the normal font-table
parser and validates every TrueType glyph, but drops simple-glyph contours
after parsing when `fvar` and `gvar` are both absent. Glyph headers and
composite component metadata remain. Full `NewFont` behavior is unchanged;
variable fonts always keep their complete glyph data. The compact API is
intended for horizontal text shaping and glyph extents. It does not provide
TrueType outlines, and vertical origins for point-matched composites are not
part of its supported contract.

`font/testdata/Selawik-VF-Subset.ttf` is an unmodified test fixture from
upstream v0.3.4 (SHA-256
`720afadd389c79e39a02502e952fecb6f5d78eeac5f095c14b9963ead45a8f1c`).
Selawik's license is in `font/testdata/Selawik-LICENSE.txt`.
