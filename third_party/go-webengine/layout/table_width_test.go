// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package layout

import "testing"

// The automatic table layout of CSS 2.1 §17.5.2.2: every column keeps at
// least its minimum (longest unbreakable unit) width. Geometry uses the
// fakeMeasurer (10px per rune, 10px space).

// tableCells lays out src at viewport vpW and returns the first row's cells.
func tableCells(t *testing.T, src string, vpW float64) []*Box {
	t.Helper()
	tbl := findBox(layoutHTML(t, src, vpW), "table")
	if tbl == nil || len(tbl.Children) == 0 {
		t.Fatal("no table row laid out")
	}
	return tbl.Children[0].Children
}

// TestTableColumnNeverNarrowerThanLongestWord is the defect that motivated
// the algorithm: a column with one long word next to a column of many short
// ones. Scaling the max-content widths to fit gave the long word 41px for a
// 100px word, so it ran into the neighbouring column ("PuissanceIT seul" on
// a real operating-cost table). Between the minimums and the maximums each
// column grows from its minimum in proportion to its remaining room.
func TestTableColumnNeverNarrowerThanLongestWord(t *testing.T) {
	// col0: "aaaaaaaaaa" min = max = 100. col1: twenty "b" words, min 10,
	// max 20*10 + 19*10 = 390. summin 110, summax 490.
	bs := "b b b b b b b b b b b b b b b b b b b b"
	src := `<html><body style="margin:0"><table style="width:200px"><tr>` +
		`<td style="padding:0">aaaaaaaaaa</td><td style="padding:0">` + bs + `</td></tr></table></body></html>`
	c := tableCells(t, src, 400)
	// f = (200-110)/(490-110) = 90/380: col0 = 100 + 0, col1 = 10 + 380*f = 100.
	assertF(t, "word.c0.W", c[0].W, 100)
	assertF(t, "word.c1.W", c[1].W, 100)
	assertF(t, "word.c1.X", c[1].X, 100)

	// Narrower than the minimums: each column keeps its minimum and the
	// table overflows, as a browser's does — never a column below its word.
	src = `<html><body style="margin:0"><table style="width:90px"><tr>` +
		`<td style="padding:0">aaaaaaaaaa</td><td style="padding:0">` + bs + `</td></tr></table></body></html>`
	c = tableCells(t, src, 400)
	assertF(t, "overflow.c0.W", c[0].W, 100)
	assertF(t, "overflow.c1.W", c[1].W, 10)
}

// TestTablePercentColumn: a percentage cell width is that share of the
// table, never less than the column's minimum; the rest of the table goes
// to the other columns.
func TestTablePercentColumn(t *testing.T) {
	src := `<html><body style="margin:0"><table style="width:400px"><tr>` +
		`<td style="padding:0;width:25%">a</td><td style="padding:0">bbbbb</td></tr></table></body></html>`
	c := tableCells(t, src, 500)
	assertF(t, "pct.c0.W", c[0].W, 100)
	assertF(t, "pct.c1.W", c[1].W, 300)
	assertF(t, "pct.c1.X", c[1].X, 100)

	// 5% of 400 = 20 is less than the five-letter word: the word wins.
	src = `<html><body style="margin:0"><table style="width:400px"><tr>` +
		`<td style="padding:0;width:5%">aaaaa</td><td style="padding:0">b</td></tr></table></body></html>`
	c = tableCells(t, src, 500)
	assertF(t, "pctmin.c0.W", c[0].W, 50)
	assertF(t, "pctmin.c1.W", c[1].W, 350)

	// Every column a percentage: what they leave over is shared in
	// proportion, so the table still fills its width.
	src = `<html><body style="margin:0"><table style="width:400px"><tr>` +
		`<td style="padding:0;width:25%">a</td><td style="padding:0;width:25%">b</td></tr></table></body></html>`
	c = tableCells(t, src, 500)
	assertF(t, "allpct.c0.W", c[0].W, 200)
	assertF(t, "allpct.c1.W", c[1].W, 200)
}

// TestTableFixedColumn: a definite cell width is handed to its column
// exactly, surplus going to the auto columns; a word longer than the
// declaration still wins (§17.5.2.2: the specified width raises the
// minimum, it never lowers it); and when every column is fixed they share
// the surplus in proportion.
func TestTableFixedColumn(t *testing.T) {
	src := `<html><body style="margin:0"><table style="width:400px"><tr>` +
		`<td style="padding:0;width:50px">a</td><td style="padding:0">bb</td></tr></table></body></html>`
	c := tableCells(t, src, 500)
	assertF(t, "fixed.c0.W", c[0].W, 50)
	assertF(t, "fixed.c1.W", c[1].W, 350)

	src = `<html><body style="margin:0"><table style="width:400px"><tr>` +
		`<td style="padding:0;width:10px">aaaaa</td><td style="padding:0">bb</td></tr></table></body></html>`
	c = tableCells(t, src, 500)
	assertF(t, "fixedword.c0.W", c[0].W, 50)
	assertF(t, "fixedword.c1.W", c[1].W, 350)

	src = `<html><body style="margin:0"><table style="width:300px"><tr>` +
		`<td style="padding:0;width:50px">a</td><td style="padding:0;width:100px">b</td></tr></table></body></html>`
	c = tableCells(t, src, 500)
	assertF(t, "allfixed.c0.W", c[0].W, 100)
	assertF(t, "allfixed.c1.W", c[1].W, 200)
}

// TestTableMinimumOfNonWrappingCell: white-space: nowrap (and pre) content
// has no unit smaller than its longest line, so its column's minimum is its
// maximum.
func TestTableMinimumOfNonWrappingCell(t *testing.T) {
	// col0 nowrap "a a a a a" = 90 both ways; col1 min 10, max 190;
	// summin 100 = table width: both columns sit at their minimum.
	src := `<html><body style="margin:0"><table style="width:100px"><tr>` +
		`<td style="padding:0;white-space:nowrap">a a a a a</td>` +
		`<td style="padding:0">b b b b b b b b b b</td></tr></table></body></html>`
	c := tableCells(t, src, 400)
	assertF(t, "nowrap.c0.W", c[0].W, 90)
	assertF(t, "nowrap.c1.W", c[1].W, 10)
	if got := len(c[0].Lines); got != 1 {
		t.Errorf("nowrap cell laid out on %d lines, want 1", got)
	}
}

// TestTableMinimumOfReplacedAndControlCells: an image and a form control
// are atomic — the column's minimum is their own width.
func TestTableMinimumOfReplacedAndControlCells(t *testing.T) {
	src := `<html><body style="margin:0"><table style="width:100px"><tr>` +
		`<td style="padding:0"><img width="30" height="10"></td>` +
		`<td style="padding:0">b b b b b b b b b b b b b b b b b b b</td></tr></table></body></html>`
	c := tableCells(t, src, 400)
	// col0 min = max = 30; col1 min 10, max 19*10 + 18*10 = 370;
	// f = (100-40)/(400-40) = 1/6 → col1 = 10 + 360/6 = 70.
	assertF(t, "img.c0.W", c[0].W, 30)
	assertF(t, "img.c1.W", c[1].W, 70)

	// The control's own size, taken from the same measurement an inline
	// <input> gets, is the minimum of a 10px-wide table's first column.
	alone := layoutHTML(t, `<html><body style="margin:0"><input></body></html>`, 400)
	body := findBox(alone, "body")
	if body == nil || len(body.Lines) == 0 || len(body.Lines[0].Items) == 0 {
		t.Fatal("standalone <input> produced no inline item")
	}
	want := body.Lines[0].Items[0].Width
	src = `<html><body style="margin:0"><table style="width:10px"><tr>` +
		`<td style="padding:0"><input></td><td style="padding:0">b</td></tr></table></body></html>`
	c = tableCells(t, src, 400)
	assertF(t, "input.c0.W", c[0].W, want)
	assertF(t, "input.c1.W", c[1].W, 10)
}

// TestTableMinimumOfSizedBox: a box with a definite width is that width in
// both box-sizing models; a flex row's minimum is the sum of its items (plus
// gaps) when it cannot wrap and the widest item when it can; an empty flex
// row contributes nothing.
func TestTableMinimumOfSizedBox(t *testing.T) {
	src := `<html><body style="margin:0"><table style="width:10px"><tr>` +
		`<td style="padding:0"><div style="width:100px;padding:0 10px;box-sizing:border-box"></div></td>` +
		`<td style="padding:0">b</td></tr></table></body></html>`
	c := tableCells(t, src, 400)
	assertF(t, "borderbox.c0.W", c[0].W, 100)

	// Whitespace and a display:none item between the flex items are not
	// items: neither counts, and neither takes a gap.
	src = `<html><body style="margin:0"><table style="width:10px"><tr>` +
		`<td style="padding:0"><div style="display:flex;column-gap:5px"><span>aaa</span> ` +
		`<span style="display:none">zzzzzzzz</span><span>bbbb</span></div></td>` +
		`<td style="padding:0">b</td></tr></table></body></html>`
	c = tableCells(t, src, 400)
	assertF(t, "flexrow.c0.W", c[0].W, 75)

	// Flex items that are an image and a form control are measured as the
	// atomic boxes they are — the control with its own border, exactly as a
	// shrink-to-fit float around a block-level <input> measures it.
	alone := layoutHTML(t, `<html><body style="margin:0"><div style="float:left"><input style="display:block"></div></body></html>`, 400)
	input := findFloatBox(alone).W
	src = `<html><body style="margin:0"><table style="width:10px"><tr>` +
		`<td style="padding:0"><div style="display:flex"><img width="30" height="10"><input></div></td>` +
		`<td style="padding:0">b</td></tr></table></body></html>`
	c = tableCells(t, src, 400)
	assertF(t, "flexatoms.c0.W", c[0].W, 30+input)

	// Block children: the widest sets the minimum; whitespace and a
	// display:none child are skipped.
	src = `<html><body style="margin:0"><table style="width:10px"><tr>` +
		`<td style="padding:0"><div>aa bb</div> <div style="display:none">zzzzzzzz</div><div>cccc d</div></td>` +
		`<td style="padding:0">b</td></tr></table></body></html>`
	c = tableCells(t, src, 400)
	assertF(t, "blocks.c0.W", c[0].W, 40)

	src = `<html><body style="margin:0"><table style="width:10px"><tr>` +
		`<td style="padding:0"><div style="display:flex;flex-wrap:wrap"><span>aaa</span><span>bbbb</span></div></td>` +
		`<td style="padding:0">b</td></tr></table></body></html>`
	c = tableCells(t, src, 400)
	assertF(t, "flexwrap.c0.W", c[0].W, 40)

	src = `<html><body style="margin:0"><table style="width:10px"><tr>` +
		`<td style="padding:0"><div style="display:flex"></div></td>` +
		`<td style="padding:0">b</td></tr></table></body></html>`
	c = tableCells(t, src, 400)
	assertF(t, "flexempty.c0.W", c[0].W, 0)
	assertF(t, "flexempty.c1.W", c[1].W, 10)
}

// TestTableMinimumThroughInlineWrapper: inline content's minimum is its
// widest single item; a <br> is not an item, and a block promoted out of an
// inline wrapper (see InlineItem.BlockBreak) is measured by its own minimum.
func TestTableMinimumThroughInlineWrapper(t *testing.T) {
	src := `<html><body style="margin:0"><table style="width:10px"><tr>` +
		`<td style="padding:0">a<br><span><div>aaaa bb</div></span> cc</td>` +
		`<td style="padding:0">b</td></tr></table></body></html>`
	c := tableCells(t, src, 400)
	assertF(t, "wrapper.c0.W", c[0].W, 40)
	assertF(t, "wrapper.c1.W", c[1].W, 10)
}

// TestWhiteSpaceNoWrapKeepsOneLine: white-space: nowrap collapses whitespace
// like normal but never wraps, however narrow the box.
func TestWhiteSpaceNoWrapKeepsOneLine(t *testing.T) {
	src := `<html><body style="margin:0"><div style="width:50px;white-space:nowrap">aa    bb cc</div></body></html>`
	div := findBox(layoutHTML(t, src, 400), "div")
	if got := len(div.Lines); got != 1 {
		t.Fatalf("nowrap div laid out on %d lines, want 1", got)
	}
	items := div.Lines[0].Items
	if len(items) != 3 {
		t.Fatalf("nowrap items = %d, want 3 (whitespace collapsed to one space)", len(items))
	}
	assertF(t, "nowrap.bb.X", items[1].X, 30)
	assertF(t, "nowrap.cc.X", items[2].X, 60)
}
