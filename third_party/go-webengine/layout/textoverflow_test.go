// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package layout

import "testing"

// TestTextOverflowEllipsisTruncatesOverflowingText covers the real shape
// found live on tailwindcss.com's own `.truncate` team-member role labels:
// overflow:hidden;text-overflow:ellipsis;white-space:nowrap on a plain-text
// element whose content is wider than its box. fakeMeasurer gives every rune
// 10px, so "aaaaaaaaaa" (10 runes = 100px) at a 50px box must truncate to fit
// within 50px including the "…" glyph.
func TestTextOverflowEllipsisTruncatesOverflowingText(t *testing.T) {
	p := findBoxByID(layoutHTML(t, `<html><body style="margin:0">`+
		`<p id="p" style="width:50px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">aaaaaaaaaa</p>`+
		`</body></html>`, 500), "p")
	if p == nil || len(p.Lines) != 1 || len(p.Lines[0].Items) != 1 {
		t.Fatalf("expected exactly one line with one item, got %+v", p)
	}
	it := p.Lines[0].Items[0]
	if it.Text == "aaaaaaaaaa" {
		t.Fatal("text was not truncated at all")
	}
	if len(it.Text) == 0 || it.Text[len(it.Text)-len("…"):] != "…" {
		t.Errorf("truncated text %q does not end in the ellipsis glyph", it.Text)
	}
	if it.Width > 50 {
		t.Errorf("truncated item width = %v, want <= 50 (the box width)", it.Width)
	}
}

// TestTextOverflowEllipsisJoinsMultipleWordsBeforeTruncating covers the more
// common real shape (a label of several words, not one long token): the
// words must be rejoined with their real spaces before truncation, not
// concatenated together, and the final result still ends in "…".
func TestTextOverflowEllipsisJoinsMultipleWordsBeforeTruncating(t *testing.T) {
	p := findBoxByID(layoutHTML(t, `<html><body style="margin:0">`+
		`<p id="p" style="width:50px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">aa bb cc dd ee</p>`+
		`</body></html>`, 500), "p")
	if p == nil || len(p.Lines) != 1 || len(p.Lines[0].Items) != 1 {
		t.Fatalf("expected exactly one line with one item, got %+v", p)
	}
	it := p.Lines[0].Items[0]
	if it.Text == "aa bb cc dd ee" {
		t.Fatal("text was not truncated at all")
	}
	if it.Text[len(it.Text)-len("…"):] != "…" {
		t.Errorf("truncated text %q does not end in the ellipsis glyph", it.Text)
	}
	if it.Width > 50 {
		t.Errorf("truncated item width = %v, want <= 50 (the box width)", it.Width)
	}
}

// TestTextOverflowEllipsisNoOpWhenContentFits confirms short content that
// already fits is left completely unchanged — truncation only ever kicks in
// on genuine overflow.
func TestTextOverflowEllipsisNoOpWhenContentFits(t *testing.T) {
	p := findBoxByID(layoutHTML(t, `<html><body style="margin:0">`+
		`<p id="p" style="width:200px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">short</p>`+
		`</body></html>`, 500), "p")
	if p == nil || len(p.Lines) != 1 || len(p.Lines[0].Items) != 1 {
		t.Fatalf("expected exactly one line with one item, got %+v", p)
	}
	if got := p.Lines[0].Items[0].Text; got != "short" {
		t.Errorf("text = %q, want unchanged %q", got, "short")
	}
}

// TestTextOverflowEllipsisSkipsLineWithImage confirms the documented, narrower
// scope: a nowrap line mixing in a non-text item (here an inline image) is
// left as plain overflow (no ellipsis), even though it also overflows and
// carries the same CSS.
func TestTextOverflowEllipsisSkipsLineWithImage(t *testing.T) {
	p := findBoxByID(layoutHTML(t, `<html><body style="margin:0">`+
		`<p id="p" style="width:50px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">`+
		`aaaaaaaaaa<img width="10" height="10"></p>`+
		`</body></html>`, 500), "p")
	if p == nil || len(p.Lines) != 1 {
		t.Fatalf("expected exactly one line, got %+v", p)
	}
	items := p.Lines[0].Items
	if len(items) != 2 {
		t.Fatalf("line was truncated to %d item(s), want the original 2 untouched (image present)", len(items))
	}
	if items[0].Text != "aaaaaaaaaa" {
		t.Errorf("text item changed to %q, want unchanged %q", items[0].Text, "aaaaaaaaaa")
	}
}

// TestTextOverflowEllipsisNarrowerThanGlyphItself confirms a box too narrow
// for even the ellipsis glyph alone still ends up with just "…" (matching a
// real browser: the trim loop has nothing left to give up but keeps the
// glyph regardless of how far over cw it sits) rather than an empty string
// or a panic.
func TestTextOverflowEllipsisNarrowerThanGlyphItself(t *testing.T) {
	p := findBoxByID(layoutHTML(t, `<html><body style="margin:0">`+
		`<p id="p" style="width:5px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">aaaaaaaaaa</p>`+
		`</body></html>`, 500), "p")
	if p == nil || len(p.Lines) != 1 || len(p.Lines[0].Items) != 1 {
		t.Fatalf("expected exactly one line with one item, got %+v", p)
	}
	if got := p.Lines[0].Items[0].Text; got != "…" {
		t.Errorf("text = %q, want the bare ellipsis glyph", got)
	}
}

// TestTextOverflowEllipsisSkipsEmptyLine confirms a bare `<br>` (which
// WrapItems renders as one real, deliberately empty line — round 97) does
// not panic or otherwise misbehave when this CSS is also present: there is
// no text to measure or truncate, so the line is left as-is.
func TestTextOverflowEllipsisSkipsEmptyLine(t *testing.T) {
	p := findBoxByID(layoutHTML(t, `<html><body style="margin:0">`+
		`<p id="p" style="width:50px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis"><br></p>`+
		`</body></html>`, 500), "p")
	if p == nil || len(p.Lines) != 1 || len(p.Lines[0].Items) != 0 {
		t.Fatalf("expected exactly one empty line, got %+v", p)
	}
}

// TestTextOverflowEllipsisRequiresClippingOverflow confirms `text-overflow:
// ellipsis` alone, without a clipping `overflow-x`, has no effect — per spec
// the property only applies when overflow is not visible on that axis.
func TestTextOverflowEllipsisRequiresClippingOverflow(t *testing.T) {
	p := findBoxByID(layoutHTML(t, `<html><body style="margin:0">`+
		`<p id="p" style="width:50px;white-space:nowrap;text-overflow:ellipsis">aaaaaaaaaa</p>`+
		`</body></html>`, 500), "p")
	if p == nil || len(p.Lines) != 1 || len(p.Lines[0].Items) != 1 {
		t.Fatalf("expected exactly one line with one item, got %+v", p)
	}
	if got := p.Lines[0].Items[0].Text; got != "aaaaaaaaaa" {
		t.Errorf("text = %q, want unchanged %q (no overflow:hidden, so no clip, so no ellipsis)", got, "aaaaaaaaaa")
	}
}
