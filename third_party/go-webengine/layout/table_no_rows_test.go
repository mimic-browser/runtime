// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package layout

import "testing"

// TestTableWithNoRowsFallsBackToBlockContent covers CSS's "shrink-to-fit"
// display:table idiom applied to markup that never uses table-row/table-cell
// at all — MediaWiki's own thumbnail-figure markup does exactly this:
// `figure{display:table}` around a plain `display:block` image wrapper and a
// `figcaption`, confirmed live on en.wikipedia.org's "Go (programming
// language)" article, whose "Branding and styling" section lost its
// gopher-mascot drawing (image AND caption) entirely. This engine doesn't
// implement CSS's anonymous-table-object synthesis (wrapping non-row/cell
// children of a display:table box in implicit rows/cells), so table's own
// zero-rows case used to return the content bottom UNCHANGED — silently
// dropping the whole box, as if it had no content at all — instead of
// falling back to ordinary block-in-flow layout of the children.
func TestTableWithNoRowsFallsBackToBlockContent(t *testing.T) {
	src := `<html><body style="margin:0">` +
		`<div style="display:table">` +
		`<a style="display:block">IMG</a><span style="display:block">CAPTION</span>` +
		`</div></body></html>`
	div := findBox(layoutHTML(t, src, 300), "div")
	if div == nil {
		t.Fatal("no box generated for the display:table element at all")
	}
	a := findBox(div, "a")
	span := findBox(div, "span")
	if a == nil || span == nil {
		t.Fatalf("expected both block children to have their own box: a=%v span=%v", a, span)
	}
	aItems := firstLineItems(a)
	spanItems := firstLineItems(span)
	if len(aItems) == 0 || aItems[0].Text != "IMG" {
		t.Fatalf("expected the first block child's own text, got %v", aItems)
	}
	if len(spanItems) == 0 || spanItems[0].Text != "CAPTION" {
		t.Fatalf("expected the second block child's own text, got %v", spanItems)
	}
	// Document order: the image wrapper comes first, the caption below it —
	// the same order MediaWiki's own markup and a real browser both use.
	if span.Y <= a.Y {
		t.Fatalf("caption Y=%.0f should be BELOW the image block's Y=%.0f", span.Y, a.Y)
	}
}
