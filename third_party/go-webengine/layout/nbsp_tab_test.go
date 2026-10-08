// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package layout

import "testing"

// A no-break space is part of its word, and a tab in preserved text reaches
// the next tab stop. Geometry uses the fakeMeasurer (10px per rune).

// TestNoBreakSpaceStaysInItsWord: "a b" is one item, drawn at the
// width of its three characters, never split — strings.Fields would have
// split it (unicode.IsSpace holds for U+00A0) and dropped the space.
func TestNoBreakSpaceStaysInItsWord(t *testing.T) {
	div := findBox(layoutHTML(t, `<html><body style="margin:0"><div>a&nbsp;b c</div></body></html>`, 400), "div")
	items := div.Lines[0].Items
	if len(items) != 2 || items[0].Text != "a b" {
		t.Fatalf("items = %d, first %q; want 2 with the no-break space inside the first word", len(items), items[0].Text)
	}
	assertF(t, "nbsp.word.W", items[0].Width, 30)
	assertF(t, "nbsp.c.X", items[1].X, 40)
}

// TestNoBreakSpacesBetweenInlineElements is rfc-editor.org's table of
// contents: `<a>15.2.1</a>.&nbsp;&nbsp;<a>100 Continue</a>`. The dot and
// its two no-break spaces are one word right after the number, and "100"
// follows at the width of those spaces — no collapsible space anywhere.
func TestNoBreakSpacesBetweenInlineElements(t *testing.T) {
	src := `<html><body style="margin:0"><p><a href="#a">15.2.1</a>.&nbsp;&nbsp;<a href="#b">100 Continue</a></p></body></html>`
	p := findBox(layoutHTML(t, src, 400), "p")
	items := p.Lines[0].Items
	if len(items) != 4 {
		t.Fatalf("items = %d, want 4 (15.2.1 / .nbsp nbsp / 100 / Continue)", len(items))
	}
	if items[1].Text != ".  " {
		t.Fatalf("second item = %q, want the dot with its two no-break spaces", items[1].Text)
	}
	assertF(t, "toc.dot.X", items[1].X, 60)
	assertF(t, "toc.dot.SpaceBefore", items[1].SpaceBefore, 0)
	assertF(t, "toc.100.X", items[2].X, 90)
	assertF(t, "toc.100.SpaceBefore", items[2].SpaceBefore, 0)
}

// TestNoBreakSpaceOnlyTextNodeIsAWord: a text node made of no-break
// spaces alone is drawn at their width, not collapsed to one space.
func TestNoBreakSpaceOnlyTextNodeIsAWord(t *testing.T) {
	src := `<html><body style="margin:0"><div><b>a</b>&nbsp;&nbsp;<b>b</b></div></body></html>`
	div := findBox(layoutHTML(t, src, 400), "div")
	items := div.Lines[0].Items
	if len(items) != 3 {
		t.Fatalf("items = %d, want 3", len(items))
	}
	assertF(t, "nbsponly.b.X", items[2].X, 30)
	assertF(t, "nbsponly.b.SpaceBefore", items[2].SpaceBefore, 0)
}

// TestPreTabsReachTabStops: in preserved text a tab advances to the next
// multiple of 8 columns, counted from the start of the line and across the
// segments that share it; a line break resets the count.
func TestPreTabsReachTabStops(t *testing.T) {
	src := "<html><body style=\"margin:0\"><pre>\ta\nab\tc<span>\td</span>\ne</pre></body></html>"
	pre := findBox(layoutHTML(t, src, 800), "pre")
	if len(pre.Lines) != 3 {
		t.Fatalf("lines = %d, want 3", len(pre.Lines))
	}
	l0 := pre.Lines[0].Items
	if len(l0) != 1 || l0[0].Text != "        a" {
		t.Fatalf("line 0 = %q, want a leading tab expanded to 8 spaces", l0[0].Text)
	}
	assertF(t, "tab.line0.W", l0[0].Width, 90)
	l1 := pre.Lines[1].Items
	if len(l1) != 2 || l1[0].Text != "ab      c" || l1[1].Text != "       d" {
		t.Fatalf("line 1 = %q + %q, want \"ab      c\" then \"       d\" (the span's tab counts the columns before it)", l1[0].Text, l1[1].Text)
	}
	assertF(t, "tab.line1.d.X", l1[1].X, 90)
	if got := pre.Lines[2].Items[0].Text; got != "e" {
		t.Fatalf("line 2 = %q, want e", got)
	}
}

// TestPreBrResetsTabColumn: a forced <br> inside preserved text starts a
// new line for the column count too.
func TestPreBrResetsTabColumn(t *testing.T) {
	src := "<html><body style=\"margin:0\"><pre>abc<br>\tx</pre></body></html>"
	pre := findBox(layoutHTML(t, src, 800), "pre")
	if len(pre.Lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(pre.Lines))
	}
	if got := pre.Lines[1].Items[0].Text; got != "        x" {
		t.Fatalf("after <br> = %q, want the tab expanded from column 0", got)
	}
}

// TestPreTabSizeHonoursCSSProperty is the confirmed real-world regression
// (round 91): pkg.go.dev's own `pre,textarea.code{tab-size:4}` on its real,
// tab-indented Go source samples — expandTabs previously ignored the
// property entirely and always expanded to 8 (CSS's own initial value, but
// not necessarily the author's), doubling the real indentation.
func TestPreTabSizeHonoursCSSProperty(t *testing.T) {
	src := "<html><body style=\"margin:0\"><pre style=\"tab-size:4\">a\tb\naa\tb</pre></body></html>"
	pre := findBox(layoutHTML(t, src, 800), "pre")
	if len(pre.Lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(pre.Lines))
	}
	if got := pre.Lines[0].Items[0].Text; got != "a   b" {
		t.Fatalf("line 0 = %q, want \"a   b\" (tab to column 4, not the default 8)", got)
	}
	if got := pre.Lines[1].Items[0].Text; got != "aa  b" {
		t.Fatalf("line 1 = %q, want \"aa  b\" (same tab stop reached from column 2)", got)
	}
}

// TestPreTabSizeZeroRendersNoTab covers the CSS Text 3 spec's own explicit
// zero case ("preserved tabs are not rendered"): the tab contributes no
// glyph and no column advance at all, rather than crashing on modulo-by-zero.
func TestPreTabSizeZeroRendersNoTab(t *testing.T) {
	src := "<html><body style=\"margin:0\"><pre style=\"tab-size:0\">a\tb</pre></body></html>"
	pre := findBox(layoutHTML(t, src, 800), "pre")
	if got := pre.Lines[0].Items[0].Text; got != "ab" {
		t.Fatalf("tab-size:0 = %q, want \"ab\" (tab renders nothing)", got)
	}
}
