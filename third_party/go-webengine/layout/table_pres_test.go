// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package layout

import "testing"

// TestQuirksTableCellTextNotCentered covers a real regression: a document
// with no <!DOCTYPE> (quirks mode — every layoutHTML test fixture in this
// package qualifies, matching news.ycombinator.com, which ships no doctype
// at all) wraps its whole page in `<center><table>...</table></center>` to
// centre the TABLE on the page — but that must not also inherit centered
// TEXT into every cell, exactly like a real browser's quirks-mode UA
// stylesheet (`table{text-align:initial}`). TestTableFixedWidthCentredInCenter
// (below) confirms the table itself STILL centres correctly even though its
// own text-align is reset — the two behaviours are meant to coexist.
func TestQuirksTableCellTextNotCentered(t *testing.T) {
	box := layoutHTML(t, `<html><body style="margin:0"><center>`+
		`<table width="200"><tr><td>x</td></tr></table></center></body></html>`, 500)
	td := findBox(box, "td")
	if td == nil {
		t.Fatal("no td box")
	}
	if len(td.Lines) == 0 || len(td.Lines[0].Items) == 0 {
		t.Fatal("td has no inline content")
	}
	// Left-aligned: the item starts at the cell's own content edge, not
	// centered somewhere in the middle of the (200px wide, mostly empty) cell.
	if got, want := td.Lines[0].Items[0].X, td.ContentX; got != want {
		t.Errorf("td text X = %v, want %v (left-aligned at the content edge, not centered)", got, want)
	}
}

// A fixed-width table is narrowed to its width and centred inside a <center>
// (legacy centre-including-blocks alignment).
func TestTableFixedWidthCentredInCenter(t *testing.T) {
	box := layoutHTML(t, `<html><body style="margin:0"><center>`+
		`<table width="200"><tr><td>x</td></tr></table></center></body></html>`, 500)
	tab := findBox(box, "table")
	if tab == nil {
		t.Fatal("no table box")
	}
	if tab.W != 200 {
		t.Errorf("table width = %v, want 200", tab.W)
	}
	if tab.X != 150 { // (500-200)/2
		t.Errorf("table x = %v, want 150 (centred)", tab.X)
	}
}

// align="center" centres a fixed-width table via auto margins.
func TestTableAlignCenterAutoMargins(t *testing.T) {
	box := layoutHTML(t, `<html><body style="margin:0">`+
		`<table align="center" width="200"><tr><td>x</td></tr></table></body></html>`, 500)
	tab := findBox(box, "table")
	if tab.X != 150 || tab.W != 200 {
		t.Errorf("align=center table = x%v w%v, want x150 w200", tab.X, tab.W)
	}
}

// margin-left:auto alone pushes a fixed-width table to the right edge;
// margin-right:auto alone keeps it at the left.
func TestTableSingleAutoMargin(t *testing.T) {
	right := findBox(layoutHTML(t, `<html><body style="margin:0">`+
		`<table style="margin-left:auto" width="200"><tr><td>x</td></tr></table></body></html>`, 500), "table")
	if right.X != 300 { // 0 + (500-200) - 0
		t.Errorf("margin-left:auto table x = %v, want 300", right.X)
	}
	left := findBox(layoutHTML(t, `<html><body style="margin:0">`+
		`<table style="margin-right:auto" width="200"><tr><td>x</td></tr></table></body></html>`, 500), "table")
	if left.X != 0 {
		t.Errorf("margin-right:auto table x = %v, want 0", left.X)
	}
}

// A plain fixed-width table (no centring) stays at the left; a width wider than
// the container falls back to the full container width.
func TestTableWidthFallbacks(t *testing.T) {
	leftTab := findBox(layoutHTML(t, `<html><body style="margin:0">`+
		`<table width="200"><tr><td>x</td></tr></table></body></html>`, 500), "table")
	if leftTab.X != 0 || leftTab.W != 200 {
		t.Errorf("plain fixed table = x%v w%v, want x0 w200", leftTab.X, leftTab.W)
	}
	// A width wider than the container overflows (as in browsers) — the pane's
	// horizontal scrollbar makes it reachable — rather than being clamped.
	wideTab := findBox(layoutHTML(t, `<html><body style="margin:0">`+
		`<table width="1000"><tr><td>x</td></tr></table></body></html>`, 500), "table")
	if wideTab.W != 1000 {
		t.Errorf("over-wide table W = %v, want 1000 (overflows, not clamped)", wideTab.W)
	}
	autoTab := findBox(layoutHTML(t, `<html><body style="margin:0">`+
		`<table><tr><td>x</td></tr></table></body></html>`, 500), "table")
	if autoTab.W != 500 {
		t.Errorf("auto-width table W = %v, want full container 500 (unchanged)", autoTab.W)
	}
}

// resolveWidths centres a definite-width block (not just a table) under the
// legacy centre-including-blocks alignment.
func TestBlockCentredInCenter(t *testing.T) {
	box := layoutHTML(t, `<html><body style="margin:0"><center>`+
		`<div style="width:200px">x</div></center></body></html>`, 500)
	div := findBox(box, "div")
	if div == nil {
		t.Fatal("no div box")
	}
	if div.X != 150 || div.W != 200 {
		t.Errorf("centred block = x%v w%v, want x150 w200", div.X, div.W)
	}
}
