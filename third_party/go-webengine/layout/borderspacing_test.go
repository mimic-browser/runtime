// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package layout

import "testing"

// TestBorderSpacingGapsColumnsAndRows covers the real shape found live on
// en.wikipedia.org's own infobox (`.infobox{border-spacing:3px}`, cells
// styled with only a `border-bottom` — this property's gap between rows IS
// the infobox's visible row-to-row breathing room): a table's own
// border-spacing must open a gap of exactly that size BEFORE the first
// column/row, BETWEEN every pair, and AFTER the last.
func TestBorderSpacingGapsColumnsAndRows(t *testing.T) {
	src := `<html><body style="margin:0"><table style="border-spacing:5px;width:200px">` +
		`<tr><td style="padding:0;width:50px">a</td><td style="padding:0;width:50px">b</td></tr>` +
		`<tr><td style="padding:0;width:50px">c</td><td style="padding:0;width:50px">d</td></tr>` +
		`</table></body></html>`
	tbl := findBox(layoutHTML(t, src, 400), "table")
	if tbl == nil || len(tbl.Children) != 2 {
		t.Fatalf("expected 2 rows, got %+v", tbl)
	}
	row0, row1 := tbl.Children[0].Children, tbl.Children[1].Children
	if len(row0) != 2 || len(row1) != 2 {
		t.Fatalf("expected 2 cells per row, got %d and %d", len(row0), len(row1))
	}

	// A gap of 5px before the first column, from the table's own content edge.
	assertF(t, "row0.c0.X", row0[0].X, tbl.ContentX+5)
	// A gap of 5px between the two columns.
	assertF(t, "row0.c1.X", row0[1].X, row0[0].X+row0[0].W+5)
	// A gap of 5px between the two rows.
	assertF(t, "row1.c0.Y", row1[0].Y, row0[0].Y+row0[0].H+5)
	// The table's own reported bottom includes the trailing gap after the
	// last row too, matching the leading gap before the first.
	wantBottom := row1[0].Y + row1[0].H + 5
	assertF(t, "table.bottom", tbl.ContentY+tbl.ContentH, wantBottom)
}

// TestBorderSpacingZeroIsNoOp confirms the default (no border-spacing set)
// leaves cells exactly flush, the pre-existing behaviour, unchanged.
func TestBorderSpacingZeroIsNoOp(t *testing.T) {
	src := `<html><body style="margin:0"><table style="width:200px">` +
		`<tr><td style="padding:0;width:50px">a</td><td style="padding:0;width:50px">b</td></tr>` +
		`</table></body></html>`
	tbl := findBox(layoutHTML(t, src, 400), "table")
	row0 := tbl.Children[0].Children
	assertF(t, "row0.c0.X", row0[0].X, tbl.ContentX)
	assertF(t, "row0.c1.X", row0[1].X, row0[0].X+row0[0].W)
}

// TestBorderSpacingInheritsToNestedTable confirms the property is genuinely
// INHERITED, per spec: a nested table with no own declaration still picks up
// its ancestor's border-spacing through the cascade's default inheritance.
func TestBorderSpacingInheritsToNestedTable(t *testing.T) {
	src := `<html><body style="margin:0;border-spacing:5px"><table style="width:100px">` +
		`<tr><td style="padding:0;width:50px">a</td><td style="padding:0;width:50px">b</td></tr>` +
		`</table></body></html>`
	tbl := findBox(layoutHTML(t, src, 400), "table")
	row0 := tbl.Children[0].Children
	assertF(t, "inherited row0.c0.X", row0[0].X, tbl.ContentX+5)
}

// TestBorderSpacingWithColspanIncludesInternalGap confirms a colspan cell's
// own width includes the border-spacing gaps INTERNAL to its span (it
// covers two columns and the one gap between them), not just the two
// columns' plain widths added together.
func TestBorderSpacingWithColspanIncludesInternalGap(t *testing.T) {
	src := `<html><body style="margin:0"><table style="border-spacing:5px;width:200px">` +
		`<tr><td style="padding:0;width:50px">a</td><td style="padding:0;width:50px">b</td></tr>` +
		`<tr><td style="padding:0" colspan="2">c</td></tr>` +
		`</table></body></html>`
	tbl := findBox(layoutHTML(t, src, 400), "table")
	if tbl == nil || len(tbl.Children) != 2 {
		t.Fatalf("expected 2 rows, got %+v", tbl)
	}
	row0, row1 := tbl.Children[0].Children, tbl.Children[1].Children
	if len(row1) != 1 {
		t.Fatalf("expected 1 spanned cell in row1, got %d", len(row1))
	}
	// The spanned cell's width = col0.W + the 5px internal gap + col1.W.
	want := row0[0].W + 5 + row0[1].W
	assertF(t, "spanned.W", row1[0].W, want)
}
