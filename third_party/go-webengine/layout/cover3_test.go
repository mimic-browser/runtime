// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package layout

import (
	"testing"

	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/dom"
)

func TestPreferredWidthBlockSkips(t *testing.T) {
	// Block children with whitespace text between and a display:none block are
	// both skipped by the max-content computation.
	l := &layouter{m: fakeMeasurer{}, floats: &floatCtx{}}
	root, _ := dom.Parse("<html><body><div id=\"d\">\n" +
		"  <p style=\"margin:0\">xx</p>\n" +
		"  <p style=\"margin:0;display:none\">xxxxxxxx</p>\n" +
		"  <p style=\"margin:0\">xxxx</p>\n</div></body></html>")
	l.sm = css.Cascade(root)
	d := dom.Find(root, "div")
	// Hidden 8-char paragraph ignored; widest visible = "xxxx" = 40.
	if got := l.preferredWidth(d, l.sm[d]); got != 40 {
		t.Errorf("block preferred = %v want 40", got)
	}
}

func TestPreferredWidthInlineWithBreak(t *testing.T) {
	// max-content ignores <br> (a forced break) and sums the whole run.
	l := &layouter{m: fakeMeasurer{}, floats: &floatCtx{}}
	root, _ := dom.Parse(`<html><body><span id="s">ab<br>cd</span></body></html>`)
	l.sm = css.Cascade(root)
	s := dom.Find(root, "span")
	// Source "ab<br>cd" has NO whitespace around the break, so the runs join with
	// no space (CSS collapsing): "ab"(20) + "cd"(20) = 40. (A phantom space across a
	// spaceless boundary was the pre-fix inline-whitespace bug.)
	if got := l.preferredWidth(s, l.sm[s]); got != 40 {
		t.Errorf("inline preferred = %v want 40", got)
	}
}

func TestFlexColumnBorderBoxWidth(t *testing.T) {
	// Column, non-stretch, item with border-box width → cross width honours it.
	src := `<html><body style="margin:0"><div style="display:flex;flex-direction:column;align-items:flex-start">` +
		`<div style="box-sizing:border-box;width:80px;padding:10px;height:20px">A</div></div></body></html>`
	outer := findBox(layoutHTML(t, src, 300), "div")
	assertF(t, "colbb.A.W", outer.Children[0].W, 80)
}

func TestTableWhitespaceAndHiddenRows(t *testing.T) {
	// Whitespace text between rows and a display:none row are skipped; a row
	// group with whitespace is descended into.
	src := "<html><body style=\"margin:0\"><table style=\"width:200px\">\n" +
		"  <tr><td style=\"padding:0\">A</td></tr>\n" +
		"  <tr style=\"display:none\"><td style=\"padding:0\">H</td></tr>\n" +
		"  <tbody>\n    <tr><td style=\"padding:0\">B</td></tr>\n  </tbody>\n" +
		"</table></body></html>"
	tbl := findBox(layoutHTML(t, src, 300), "table")
	if len(tbl.Children) != 2 {
		t.Fatalf("visible rows = %d want 2", len(tbl.Children))
	}
	assertF(t, "wsrow.row1.Y", tbl.Children[1].Y, 20)
}

func TestRowWithWhitespaceCells(t *testing.T) {
	// Whitespace between <td>s (a non-element child of the row) is skipped.
	src := "<html><body style=\"margin:0\"><table style=\"width:200px\"><tr>\n" +
		"  <td style=\"padding:0\">A</td>\n  <td style=\"padding:0\">B</td>\n" +
		"</tr></table></body></html>"
	tbl := findBox(layoutHTML(t, src, 300), "table")
	row := tbl.Children[0]
	if len(row.Children) != 2 {
		t.Fatalf("cells = %d want 2", len(row.Children))
	}
}

func TestRightFloatsStack(t *testing.T) {
	// Two right floats too wide to share a line: the second drops below.
	src := `<html><body style="margin:0">` +
		`<div style="float:right;width:200px;height:30px" id="r1">A</div>` +
		`<div style="float:right;width:200px;height:30px" id="r2">B</div>` +
		`<p style="margin:0">t</p></body></html>`
	root := layoutHTML(t, src, 300)
	r1 := findBoxByID(root, "r1")
	r2 := findBoxByID(root, "r2")
	assertF(t, "r1.X", r1.X, 100) // 300-200
	assertF(t, "r2.Y", r2.Y, 30)  // dropped below r1
	assertF(t, "r2.X", r2.X, 100)
}

func TestFindSlotDirect(t *testing.T) {
	fc := &floatCtx{}
	// First left float: empty context, placed at (0,0).
	x, y := fc.findSlot(css.FloatLeft, 0, 100, 40, 0, 300)
	assertF(t, "slot1.x", x, 0)
	assertF(t, "slot1.y", y, 0)
	fc.add(css.FloatLeft, floatRect{x: 0, y: 0, w: 250, h: 40})
	// A 100-wide left float cannot fit beside the 250-wide one → drops to y=40.
	x2, y2 := fc.findSlot(css.FloatLeft, 0, 100, 40, 0, 300)
	assertF(t, "slot2.x", x2, 0)
	assertF(t, "slot2.y", y2, 40)
	// A wide right float leaves no room for a 150-wide right float at y=0, which
	// therefore drops below it.
	rc := &floatCtx{}
	rc.add(css.FloatRight, floatRect{x: 50, y: 0, w: 250, h: 40})
	xr, yr := rc.findSlot(css.FloatRight, 0, 150, 40, 0, 300)
	assertF(t, "slotR.y", yr, 40)
	assertF(t, "slotR.x", xr, 150) // 300-150 in the freed band
}

func TestCSSDrivenTable(t *testing.T) {
	// display:table/table-row/table-cell on divs exercises the direct table-row
	// branch (HTML wraps real <tr> in an implicit <tbody>).
	src := `<html><body style="margin:0">` +
		`<div style="display:table;width:200px">` +
		`<div style="display:table-row">` +
		`<div style="display:table-cell;padding:0">A</div>` +
		`<div style="display:table-cell;padding:0">B</div>` +
		`</div></div></body></html>`
	tbl := findBoxByDisplay(layoutHTML(t, src, 300), css.DisplayTable)
	if tbl == nil || len(tbl.Children) != 1 {
		t.Fatalf("css table rows = %v", tbl)
	}
	row := tbl.Children[0]
	if len(row.Children) != 2 {
		t.Fatalf("css table cells = %d", len(row.Children))
	}
	assertF(t, "csstable.c1.X", row.Children[1].X, 100)
}

func findBoxByDisplay(b *Box, d css.Display) *Box {
	if b == nil {
		return nil
	}
	if b.Style != nil && b.Style.Display == d {
		return b
	}
	for _, c := range b.Children {
		if got := findBoxByDisplay(c, d); got != nil {
			return got
		}
	}
	return nil
}

func TestContentsEmptyInlineRunBetweenBlocks(t *testing.T) {
	// A run consisting only of an empty inline element (no text) yields no items
	// and generates no anonymous box.
	src := `<html><body style="margin:0"><div style="margin:0;padding:0">` +
		`<p style="margin:0">a</p><span></span></div></body></html>`
	div := findBox(layoutHTML(t, src, 300), "div")
	if len(div.Children) != 1 {
		t.Fatalf("children = %d want 1 (empty span run drops)", len(div.Children))
	}
}

func TestPreLineMultipleItems(t *testing.T) {
	// A pre line with an inline element yields multiple items on one line.
	src := "<html><body style=\"margin:0\"><pre style=\"margin:0;padding:0\">a<span>bc</span>d</pre></body></html>"
	pre := findBox(layoutHTML(t, src, 300), "pre")
	if len(pre.Lines) != 1 {
		t.Fatalf("pre lines = %d", len(pre.Lines))
	}
	items := pre.Lines[0].Items
	if len(items) != 3 {
		t.Fatalf("pre items = %v", texts(items))
	}
	// Items lay out contiguously (no space between): a(10), bc(20), d(10).
	assertF(t, "pre.item1.X", items[1].X, 10)
	assertF(t, "pre.item2.X", items[2].X, 30)
}

func TestMarginRightAutoOnly(t *testing.T) {
	// margin-right:auto only keeps the box at the left edge (leftover to right).
	src := `<html><body style="margin:0"><div style="width:200px;margin-right:auto">x</div></body></html>`
	div := findBox(layoutHTML(t, src, 500), "div")
	assertF(t, "mrauto.X", div.X, 0)
	assertF(t, "mrauto.W", div.W, 200)
}

func TestTableDisplayNoneDirectChild(t *testing.T) {
	// A display:none direct child of a table is skipped by the row collector.
	src := `<html><body style="margin:0"><div style="display:table;width:200px">` +
		`<div style="display:none">gone</div>` +
		`<div style="display:table-row"><div style="display:table-cell;padding:0">A</div></div>` +
		`</div></body></html>`
	tbl := findBoxByDisplay(layoutHTML(t, src, 300), css.DisplayTable)
	if tbl == nil || len(tbl.Children) != 1 {
		t.Fatalf("table rows (none skipped) = %v", tbl)
	}
}

func TestContentsWhitespaceBetweenBlocks(t *testing.T) {
	src := "<html><body style=\"margin:0\"><div style=\"margin:0;padding:0\">\n" +
		"  <p style=\"margin:0\">a</p>\n  <p style=\"margin:0\">b</p>\n</div></body></html>"
	div := findBox(layoutHTML(t, src, 300), "div")
	if len(div.Children) != 2 {
		t.Fatalf("blocks = %d want 2 (whitespace skipped)", len(div.Children))
	}
}

func TestFloatBorderBoxWidth(t *testing.T) {
	// A float with box-sizing:border-box: width 100 includes 10px padding sides.
	src := `<html><body style="margin:0">` +
		`<div style="float:left;box-sizing:border-box;width:100px;padding:10px;height:20px" id="f">x</div>` +
		`<p style="margin:0">a</p></body></html>`
	root := layoutHTML(t, src, 300)
	f := findBoxByID(root, "f")
	assertF(t, "fbb.W", f.W, 100)              // border box width stays 100
	assertF(t, "fbb.ContentW", f.ContentW, 80) // content shrinks by padding
}

func TestFloatWithBlockChildTranslated(t *testing.T) {
	// A float containing a block child exercises the recursive translate path.
	src := `<html><body style="margin:0">` +
		`<div style="float:left;width:100px" id="f"><p style="margin:0">inner</p></div>` +
		`<p style="margin:0">a</p></body></html>`
	root := layoutHTML(t, src, 300)
	f := findBoxByID(root, "f")
	if len(f.Children) == 0 {
		t.Fatal("float should have a child block")
	}
	// The inner block was translated to sit within the float at x=0.
	assertF(t, "innerblock.X", f.Children[0].X, 0)
}
