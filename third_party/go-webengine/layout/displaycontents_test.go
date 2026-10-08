// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package layout

import "testing"

// TestDisplayContentsPromotesGridChildren covers a real regression, found
// live on developer.mozilla.org: its reference-article layout wraps the
// actual header/body content in `<main class="layout__content">` with
// `display:contents`, solely to keep `<main>` out of the enclosing CSS
// Grid's item list while letting its OWN children still receive their own
// `grid-area` placement. Before display:contents was understood, `<main>`
// (which has no grid-area of its own) became an unnamed grid item, auto-
// placed into the wrong cell, while its real children's grid-area
// assignments were silently ignored (grid-area only matters on a direct
// grid item) — the DOM inspector showed the intended sidebar/content
// columns, but the rendered page stacked everything as if the grid weren't
// there at all.
func TestDisplayContentsPromotesGridChildren(t *testing.T) {
	src := `<html><body style="margin:0">` +
		`<div style="display:grid;grid-template-columns:100px 100px;grid-template-areas:'a b'">` +
		`<main style="display:contents"><header style="grid-area:a">H</header></main>` +
		`<footer style="grid-area:b">F</footer>` +
		`</div></body></html>`
	g := findBox(layoutHTML(t, src, 300), "div")
	header := findBox(g, "header")
	footer := findBox(g, "footer")
	if header == nil || footer == nil {
		t.Fatalf("expected both header and footer boxes, got header=%v footer=%v", header, footer)
	}
	// The display:contents <main> must not itself appear as a grid item —
	// its child <header> takes its place, landing in area "a" (column 1).
	assertF(t, "header.X (area a, column 1)", header.X, 0)
	assertF(t, "footer.X (area b, column 2)", footer.X, 100)
}

// TestDisplayContentsRecursesThroughNesting covers a display:contents
// element nested inside ANOTHER display:contents element — both must be
// skipped, promoting the innermost real element's box all the way up to the
// grid container's own item list.
func TestDisplayContentsRecursesThroughNesting(t *testing.T) {
	src := `<html><body style="margin:0">` +
		`<div style="display:grid;grid-template-columns:100px 100px;grid-template-areas:'a b'">` +
		`<main style="display:contents"><section style="display:contents">` +
		`<header style="grid-area:a">H</header></section></main>` +
		`<footer style="grid-area:b">F</footer>` +
		`</div></body></html>`
	g := findBox(layoutHTML(t, src, 300), "div")
	header := findBox(g, "header")
	if header == nil {
		t.Fatal("expected a header box even through two nested display:contents wrappers")
	}
	assertF(t, "header.X through nested display:contents", header.X, 0)
}

// TestDisplayContentsPlainBlockUnaffected covers the common case (no
// display:contents anywhere): renderedChildren/flattenContents must return
// children completely unchanged, byte-identical to before display:contents
// existed.
func TestDisplayContentsPlainBlockUnaffected(t *testing.T) {
	src := `<html><body style="margin:0"><div><p>a</p><p>b</p></div></body></html>`
	div := findBox(layoutHTML(t, src, 300), "div")
	if len(div.Children) != 2 {
		t.Fatalf("children = %d, want 2 (display:contents must not affect a page with none)", len(div.Children))
	}
}
