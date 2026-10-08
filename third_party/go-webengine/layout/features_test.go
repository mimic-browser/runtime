// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package layout

import (
	"testing"

	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/dom"
)

// ---- box model: margin:auto, max/min-width, box-sizing, borders ------------

func TestMarginAutoCentering(t *testing.T) {
	// width 600, margin:0 auto in a 1024 viewport (body margin 0).
	src := `<html><body style="margin:0"><div style="width:600px;margin:0 auto">x</div></body></html>`
	div := findBox(layoutHTML(t, src, 1024), "div")
	assertF(t, "centred.X", div.X, 212) // (1024-600)/2
	assertF(t, "centred.W", div.W, 600)
	assertF(t, "centred.ContentW", div.ContentW, 600)
}

func TestMarginLeftAutoPushesRight(t *testing.T) {
	src := `<html><body style="margin:0"><div style="width:200px;margin-left:auto">x</div></body></html>`
	div := findBox(layoutHTML(t, src, 500), "div")
	assertF(t, "pushed.X", div.X, 300) // 500-200
}

func TestMarginPercentShorthandNarrowsBoxSymmetrically(t *testing.T) {
	// The real shape found live on en.wikipedia.org: a width:auto box (no
	// explicit width, exactly like Wikipedia's own `.ambox` cleanup-notice
	// table) narrowed by a percentage margin shorthand, in a 1024 viewport
	// with body margin 0 so cw is exactly 1024.
	src := `<html><body style="margin:0"><div style="margin:0 10%">x</div></body></html>`
	div := findBox(layoutHTML(t, src, 1024), "div")
	assertF(t, "percent-margin.X", div.X, 102.4)      // 1024 * 0.10
	assertF(t, "percent-margin.ContentW", div.ContentW, 819.2) // 1024 - 2*102.4
}

func TestMarginPercentLonghandsIndependently(t *testing.T) {
	// margin-left/margin-right as separate percentage longhands, asymmetric,
	// confirming the longhand path (applyMarginSide) resolves the same way as
	// the shorthand (applyMarginShorthand).
	src := `<html><body style="margin:0"><div style="margin-left:25%;margin-right:5%">x</div></body></html>`
	div := findBox(layoutHTML(t, src, 1000), "div")
	assertF(t, "percent-longhand.X", div.X, 250)          // 1000 * 0.25
	assertF(t, "percent-longhand.ContentW", div.ContentW, 700) // 1000 - 250 - 50
}

func TestMarginPercentLeftAutoRightMixed(t *testing.T) {
	// One side a percentage, the other side auto — confirms resolveWidths
	// resolves the fixed percentage side first, then gives ALL the leftover
	// space to the auto side (not just the space beyond a zero-margin
	// assumption), and that the percent path didn't disturb auto's own
	// pre-existing precedence.
	src := `<html><body style="margin:0"><div style="width:200px;margin-left:10%;margin-right:auto">x</div></body></html>`
	div := findBox(layoutHTML(t, src, 1000), "div")
	assertF(t, "mixed.X", div.X, 100)          // 1000 * 0.10
	assertF(t, "mixed.ContentW", div.ContentW, 200) // explicit width, unaffected
}

func TestMaxWidthClampAndCentre(t *testing.T) {
	// width auto but max-width 600 with margin auto → clamps then centres.
	src := `<html><body style="margin:0"><div style="max-width:600px;margin:0 auto">x</div></body></html>`
	div := findBox(layoutHTML(t, src, 1000), "div")
	assertF(t, "maxw.ContentW", div.ContentW, 600)
	assertF(t, "maxw.X", div.X, 200) // (1000-600)/2
}

func TestMinWidthClamp(t *testing.T) {
	// width 50 but min-width 120 → clamps up to 120.
	src := `<html><body style="margin:0"><div style="width:50px;min-width:120px">x</div></body></html>`
	div := findBox(layoutHTML(t, src, 500), "div")
	assertF(t, "minw.ContentW", div.ContentW, 120)
}

func TestBoxSizingBorderBox(t *testing.T) {
	// border-box: width 200 includes 10px padding each side and 5px border each
	// side → content width 200 - 20 - 10 = 170; border box width stays 200.
	src := `<html><body style="margin:0"><div style="box-sizing:border-box;width:200px;padding:10px;border:5px solid black">x</div></body></html>`
	div := findBox(layoutHTML(t, src, 500), "div")
	assertF(t, "bb.ContentW", div.ContentW, 170)
	assertF(t, "bb.W", div.W, 200)
}

func TestBordersAddToBoxWidthHeight(t *testing.T) {
	// content-box: width 100 + padding 10 + border 4 on each side.
	src := `<html><body style="margin:0"><div style="width:100px;padding:10px;border:4px solid red">hi</div></body></html>`
	div := findBox(layoutHTML(t, src, 500), "div")
	assertF(t, "bx.W", div.W, 128)              // 4+10+100+10+4
	assertF(t, "bx.ContentX", div.ContentX, 14) // border 4 + padding 10
	// height: border 4 + padding 10 + one 20px line + padding 10 + border 4.
	assertF(t, "bx.H", div.H, 48)
	assertF(t, "bx.ContentY", div.ContentY, 14)
}

func TestBorderStyleNoneNoWidth(t *testing.T) {
	// border-style:none means the border reserves no space even with a width.
	src := `<html><body style="margin:0"><div style="width:100px;border-width:10px;border-style:none">x</div></body></html>`
	div := findBox(layoutHTML(t, src, 500), "div")
	assertF(t, "nonborder.W", div.W, 100)
}

// ---- margin collapsing -----------------------------------------------------

func TestAdjacentSiblingMarginsCollapse(t *testing.T) {
	// Two blocks with 20px bottom / 30px top margins collapse to max(20,30)=30.
	src := `<html><body style="margin:0;padding:0">` +
		`<div style="margin:0 0 20px 0;padding:0">a</div>` +
		`<div style="margin:30px 0 0 0;padding:0">b</div></body></html>`
	body := findBox(layoutHTML(t, src, 500), "body")
	a, b := body.Children[0], body.Children[1]
	assertF(t, "a.Y", a.Y, 0)
	assertF(t, "a.H", a.H, 20)
	// gap = max(20,30) = 30, so b.Y = 20 + 30 = 50.
	assertF(t, "b.Y", b.Y, 50)
}

func TestParentFirstChildMarginCollapse(t *testing.T) {
	// body margin 0 padding 0; first child p margin-top 40 collapses through the
	// parent, so the p (and the body) start at y=40.
	src := `<html><body style="margin:0;padding:0">` +
		`<p style="margin:40px 0 0 0">hi</p></body></html>`
	root := layoutHTML(t, src, 500)
	body := findBox(root, "body")
	p := findBox(root, "p")
	assertF(t, "collapse.p.Y", p.Y, 40)
	assertF(t, "collapse.body.Y", body.Y, 40) // border-top follows first child
}

func TestPaddingBlocksParentChildCollapse(t *testing.T) {
	// With padding-top on the parent, the child's top margin does NOT collapse
	// through: parent border-top at 0, content at 10, child at 10+40 = 50.
	src := `<html><body style="margin:0;padding:10px 0 0 0">` +
		`<p style="margin:40px 0 0 0">hi</p></body></html>`
	root := layoutHTML(t, src, 500)
	body := findBox(root, "body")
	p := findBox(root, "p")
	assertF(t, "noc.body.Y", body.Y, 0)
	assertF(t, "noc.p.Y", p.Y, 50)
}

func TestNegativeMarginCollapse(t *testing.T) {
	// collapse(20, -8) = 20 + (-8) = 12.
	if got := collapse(20, -8); got != 12 {
		t.Errorf("collapse(20,-8) = %v want 12", got)
	}
	if got := collapse(-5, -8); got != -8 {
		t.Errorf("collapse(-5,-8) = %v want -8", got)
	}
	if got := collapse(0, 0); got != 0 {
		t.Errorf("collapse(0,0) = %v", got)
	}
}

// ---- floats ----------------------------------------------------------------

func TestFloatRightTextFlowsLeft(t *testing.T) {
	src := `<html><body style="margin:0">` +
		`<div style="float:right;width:100px;height:50px">F</div>` +
		`<p style="margin:0">a b c d e f g h i j</p></body></html>`
	root := layoutHTML(t, src, 300)
	p := findBox(root, "p")
	// The paragraph block is full width, but its first line shrinks to avoid the
	// 100px-wide right float: available width 300 - 100 = 200, starting at x=0.
	assertF(t, "float.p.ContentW", p.ContentW, 300)
	assertF(t, "float.line0.W", p.Lines[0].W, 200)
	assertF(t, "float.line0.X", p.Lines[0].X, 0)
}

func TestFloatLeftPlacementAndTextOffset(t *testing.T) {
	src := `<html><body style="margin:0">` +
		`<div style="float:left;width:80px;height:40px">F</div>` +
		`<p style="margin:0">a b c d e</p></body></html>`
	root := layoutHTML(t, src, 400)
	f := findFloatBox(root)
	assertF(t, "floatL.X", f.X, 0)
	assertF(t, "floatL.Y", f.Y, 0)
	p := findBox(root, "p")
	// First line begins to the right of the 80px float.
	assertF(t, "floatL.line0.X", p.Lines[0].X, 80)
}

func findFloatBox(b *Box) *Box {
	if b == nil {
		return nil
	}
	if b.Float != css.FloatNone {
		return b
	}
	for _, c := range b.Children {
		if got := findFloatBox(c); got != nil {
			return got
		}
	}
	return nil
}

func TestFloatClear(t *testing.T) {
	// A cleared block drops below a preceding float.
	src := `<html><body style="margin:0">` +
		`<div style="float:left;width:50px;height:60px">F</div>` +
		`<div style="clear:left;margin:0;padding:0">x</div></body></html>`
	root := layoutHTML(t, src, 300)
	cleared := findBoxWithText(root, "x")
	assertF(t, "clear.Y", cleared.Y, 60) // below the 60px-tall float
}

func findBoxWithText(b *Box, want string) *Box {
	if b == nil {
		return nil
	}
	for _, line := range b.Lines {
		for _, it := range line.Items {
			if it.Text == want {
				return b
			}
		}
	}
	for _, c := range b.Children {
		if got := findBoxWithText(c, want); got != nil {
			return got
		}
	}
	return nil
}

func TestFloatStacksWhenNoRoom(t *testing.T) {
	// Two left floats each 200 wide in a 300 region: the second drops below the
	// first (no room beside it).
	src := `<html><body style="margin:0">` +
		`<div style="float:left;width:200px;height:30px" id="f1">A</div>` +
		`<div style="float:left;width:200px;height:30px" id="f2">B</div>` +
		`<p style="margin:0">t</p></body></html>`
	root := layoutHTML(t, src, 300)
	f1 := findBoxByID(root, "f1")
	f2 := findBoxByID(root, "f2")
	assertF(t, "f1.Y", f1.Y, 0)
	assertF(t, "f2.Y", f2.Y, 30) // dropped below f1
	assertF(t, "f2.X", f2.X, 0)
}

func findBoxByID(b *Box, id string) *Box {
	if b == nil {
		return nil
	}
	if b.Node != nil && b.Node.Type == dom.Element && b.Node.ID() == id {
		return b
	}
	for _, c := range b.Children {
		if got := findBoxByID(c, id); got != nil {
			return got
		}
	}
	return nil
}

// ---- flexbox ---------------------------------------------------------------

func TestFlexRowBasic(t *testing.T) {
	src := `<html><body style="margin:0"><div style="display:flex">` +
		`<div style="width:100px">A</div><div style="width:100px">B</div></div></body></html>`
	outer := findBox(layoutHTML(t, src, 300), "div")
	if len(outer.Children) != 2 {
		t.Fatalf("flex children = %d", len(outer.Children))
	}
	assertF(t, "flexA.X", outer.Children[0].X, 0)
	assertF(t, "flexB.X", outer.Children[1].X, 100)
	assertF(t, "flexA.Y", outer.Children[0].Y, 0)
	assertF(t, "flexB.Y", outer.Children[1].Y, 0)
}

func TestFlexJustifyContentSpaceBetween(t *testing.T) {
	src := `<html><body style="margin:0"><div style="display:flex;justify-content:space-between">` +
		`<div style="width:100px">A</div><div style="width:100px">B</div></div></body></html>`
	outer := findBox(layoutHTML(t, src, 300), "div")
	// leftover = 300 - 200 = 100 between two items → gap 100.
	assertF(t, "sb.A.X", outer.Children[0].X, 0)
	assertF(t, "sb.B.X", outer.Children[1].X, 200)
}

func TestFlexJustifyCenter(t *testing.T) {
	src := `<html><body style="margin:0"><div style="display:flex;justify-content:center">` +
		`<div style="width:100px">A</div></div></body></html>`
	outer := findBox(layoutHTML(t, src, 300), "div")
	assertF(t, "center.A.X", outer.Children[0].X, 100) // (300-100)/2
}

func TestFlexGrow(t *testing.T) {
	// One item with flex-grow:1 absorbs all free space.
	src := `<html><body style="margin:0"><div style="display:flex">` +
		`<div style="width:100px">A</div>` +
		`<div style="width:100px;flex-grow:1">B</div></div></body></html>`
	outer := findBox(layoutHTML(t, src, 300), "div")
	a, b := outer.Children[0], outer.Children[1]
	assertF(t, "grow.A.W", a.W, 100)
	assertF(t, "grow.B.W", b.W, 200) // 100 base + 100 free
	assertF(t, "grow.B.X", b.X, 100)
}

func TestFlexShrink(t *testing.T) {
	// Two 200px items in a 300px container both with shrink 1 shrink equally.
	src := `<html><body style="margin:0"><div style="display:flex">` +
		`<div style="width:200px">A</div><div style="width:200px">B</div></div></body></html>`
	outer := findBox(layoutHTML(t, src, 300), "div")
	// free = 300 - 400 = -100, split by shrink equally → each 200 - 50 = 150.
	assertF(t, "shrink.A.W", outer.Children[0].W, 150)
	assertF(t, "shrink.B.W", outer.Children[1].W, 150)
	assertF(t, "shrink.B.X", outer.Children[1].X, 150)
}

// TestFlexShrinkCascadesPastAFrozenItem covers CSS Flexbox §9.7's iterative
// "freeze and redistribute" step, using github.com's own real shape: a small
// logo item beside a much wider nav+search+sign-in/up group, narrower than
// their combined content once the mobile-only controls are hidden. A single
// shrink pass (dividing the deficit equally, then clamping each item
// independently) clamps the small item at its own floor but drops the
// portion of ITS share that the floor refused — that portion needs to fall
// through to the still-flexible sibling instead. Without the redistribution,
// the two items' final widths sum to MORE than the container (an overflow),
// which on github.com/golang/go pushed the "Sign in"/"Sign up" buttons off
// the right edge of a 1024px viewport entirely.
//
// A's own floor is 10, not 0: its content ("A", one unbreakable character)
// has an automatic minimum size of 10px (round 111's own fix — a flex item
// whose min-width is auto never shrinks below its content's min-content),
// so this is the REAL floor a spec-compliant browser stops it at, not an
// arbitrary "nowhere left to shrink" 0 an earlier version of this test
// assumed before that gap was found and closed.
func TestFlexShrinkCascadesPastAFrozenItem(t *testing.T) {
	src := `<html><body style="margin:0"><div style="display:flex">` +
		`<div id="a" style="width:10px">A</div><div id="b" style="width:300px">B</div></div></body></html>`
	root := layoutHTML(t, src, 200)
	a, b := findBoxByID(root, "a"), findBoxByID(root, "b")
	// free = 200 - 310 = -110. A single, non-iterative pass would split this
	// -55/-55, clamp A's -45 up to its own automatic minimum (10, "A"'s own
	// content floor) and leave B at 300-55=245 — 55px past the 200px
	// container. Redistributing A's unabsorbed shrink to B instead brings
	// the total to exactly 200 (A=10, B=190).
	assertF(t, "cascade.A.W", a.W, 10)
	assertF(t, "cascade.B.W", b.W, 190)
	assertF(t, "cascade.B.X", b.X, 10) // right after A, which now keeps its own 10px floor
}

func TestFlexAlignItemsCenter(t *testing.T) {
	// Items of different heights, align-items:center on the cross axis.
	src := `<html><body style="margin:0"><div style="display:flex;align-items:center">` +
		`<div style="width:50px;height:20px">A</div>` +
		`<div style="width:50px;height:60px">B</div></div></body></html>`
	outer := findBox(layoutHTML(t, src, 300), "div")
	a := outer.Children[0]
	// container cross = 60; A (20 tall) centred → offset (60-20)/2 = 20.
	assertF(t, "aic.A.Y", a.Y, 20)
	assertF(t, "aic.B.Y", outer.Children[1].Y, 0)
}

func TestFlexColumn(t *testing.T) {
	src := `<html><body style="margin:0"><div style="display:flex;flex-direction:column">` +
		`<div style="height:30px">A</div><div style="height:40px">B</div></div></body></html>`
	outer := findBox(layoutHTML(t, src, 300), "div")
	assertF(t, "col.A.Y", outer.Children[0].Y, 0)
	assertF(t, "col.B.Y", outer.Children[1].Y, 30)
}

func TestFlexEmpty(t *testing.T) {
	src := `<html><body style="margin:0"><div style="display:flex"></div></body></html>`
	outer := findBox(layoutHTML(t, src, 300), "div")
	if len(outer.Children) != 0 {
		t.Errorf("empty flex children = %d", len(outer.Children))
	}
}

// ---- tables ----------------------------------------------------------------

func TestTableTwoCells(t *testing.T) {
	src := `<html><body style="margin:0"><table style="width:200px"><tr>` +
		`<td style="padding:0">A</td><td style="padding:0">B</td></tr></table></body></html>`
	tbl := findBox(layoutHTML(t, src, 300), "table")
	if len(tbl.Children) != 1 {
		t.Fatalf("rows = %d", len(tbl.Children))
	}
	row := tbl.Children[0]
	if len(row.Children) != 2 {
		t.Fatalf("cells = %d", len(row.Children))
	}
	c0, c1 := row.Children[0], row.Children[1]
	// Equal natural widths → each column 100 of the 200 table width.
	assertF(t, "cell0.X", c0.X, 0)
	assertF(t, "cell0.W", c0.W, 100)
	assertF(t, "cell1.X", c1.X, 100)
	assertF(t, "cell1.W", c1.W, 100)
	assertF(t, "row.H", row.H, 20) // one 20px line tall
}

func TestTableColumnWidthsFromContent(t *testing.T) {
	// Cell A has one word (10px), cell B has "xx xx" content. Column widths
	// scale from natural max-content widths.
	src := `<html><body style="margin:0"><table style="width:300px"><tr>` +
		`<td style="padding:0">x</td><td style="padding:0">xxxxx</td></tr></table></body></html>`
	tbl := findBox(layoutHTML(t, src, 400), "table")
	row := tbl.Children[0]
	c0, c1 := row.Children[0], row.Children[1]
	// natural: col0 = 10, col1 = 50; sum 60; scale 300/60 = 5 → 50 and 250.
	assertF(t, "tc.c0.W", c0.W, 50)
	assertF(t, "tc.c1.W", c1.W, 250)
	assertF(t, "tc.c1.X", c1.X, 50)
}

func TestTableRowGroup(t *testing.T) {
	src := `<html><body style="margin:0"><table style="width:200px"><tbody>` +
		`<tr><td style="padding:0">A</td></tr>` +
		`<tr><td style="padding:0">B</td></tr></tbody></table></body></html>`
	tbl := findBox(layoutHTML(t, src, 300), "table")
	if len(tbl.Children) != 2 {
		t.Fatalf("tbody rows = %d", len(tbl.Children))
	}
	assertF(t, "rg.row0.Y", tbl.Children[0].Y, 0)
	assertF(t, "rg.row1.Y", tbl.Children[1].Y, 20)
}

func TestTableColumnWidthFromChildlessExplicitWidthBox(t *testing.T) {
	// A cell with no text of its own — just a div sized by an explicit width,
	// as a bar-chart fill or a spacer — must still contribute that width to its
	// column's natural (max-content) width. Before the preferredWidth fix, a
	// childless box's own explicit width was never consulted (only its
	// children's), so this column collapsed to 0 and the next column's cells
	// were positioned on top of it instead of after it.
	src := `<html><body style="margin:0"><table style="width:400px"><tr>` +
		`<td style="padding:0">label</td>` + // natural 50 (5 chars × 10)
		`<td style="padding:0"><div style="width:200px;height:15px"></div></td>` + // natural 200
		`<td style="padding:0">9,4k</td>` + // natural 40
		`</tr></table></body></html>`
	tbl := findBox(layoutHTML(t, src, 500), "table")
	row := tbl.Children[0]
	c0, c1, c2 := row.Children[0], row.Children[1], row.Children[2]
	// natural: 50 + 200 + 40 = 290; scale = 400/290.
	scale := 400.0 / 290.0
	assertF(t, "bar.c0.W", c0.W, 50*scale)
	assertF(t, "bar.c1.W", c1.W, 200*scale)
	assertF(t, "bar.c2.W", c2.W, 40*scale)
	assertF(t, "bar.c2.X", c2.X, (50+200)*scale)
	if got := c1.Children[0].W; got != 200 {
		t.Errorf("bar.c1 child div.W = %v, want 200 (own explicit width, not clamped by the column)", got)
	}
}

// TestTableColspanAlignsFollowingCellWithLaterRow covers a real regression,
// found live on news.ycombinator.com: its own markup shapes a story as a
// title row of 3 plain cells, then a subtext row of
// `<td colspan=2></td><td class=subtext>...</td>`. colStart/colSpan were
// previously unmodelled — a cell's column index was just its position in
// the row's own cell list — so the subtext row's SECOND cell (raw position
// 1) landed in the vote-arrow's narrow column instead of the title's wide
// one, splitting every story into two misaligned side-by-side blocks
// instead of one flowing title+subtext column.
func TestTableColspanAlignsFollowingCellWithLaterRow(t *testing.T) {
	src := `<html><body style="margin:0"><table style="width:140px">` +
		`<tr><td style="padding:0">A</td><td style="padding:0">B</td><td style="padding:0">CCCCC</td></tr>` +
		`<tr><td colspan="2" style="padding:0"></td><td style="padding:0">DDDDD</td></tr>` +
		`</table></body></html>`
	tbl := findBox(layoutHTML(t, src, 300), "table")
	if len(tbl.Children) != 2 {
		t.Fatalf("rows = %d", len(tbl.Children))
	}
	titleRow, subtextRow := tbl.Children[0], tbl.Children[1]
	if len(titleRow.Children) != 3 || len(subtextRow.Children) != 2 {
		t.Fatalf("cells = %d, %d", len(titleRow.Children), len(subtextRow.Children))
	}
	// natural: col0=10 (A), col1=10 (B), col2=50 (CCCCC, DDDDD); sum=70;
	// scale=140/70=2 → colW=[20,20,100], colX=[0,20,40].
	titleCell := titleRow.Children[2]     // "CCCCC"
	spanCell := subtextRow.Children[0]    // <td colspan=2>
	subtextCell := subtextRow.Children[1] // "DDDDD" — must align with titleCell, not colX[1]
	assertF(t, "title cell X", titleCell.X, 40)
	assertF(t, "title cell W", titleCell.W, 100)
	assertF(t, "colspan=2 cell X", spanCell.X, 0)
	assertF(t, "colspan=2 cell W", spanCell.W, 40)
	assertF(t, "subtext cell X", subtextCell.X, 40)
	assertF(t, "subtext cell W", subtextCell.W, 100)
}

// TestCellColSpanInvalidValueDefault covers cellColSpan's "invalid value
// default" (a non-numeric or non-positive colspan is treated as absent, per
// the HTML spec), via its effect on column alignment: an invalid colspan
// cell must NOT shift a later plain cell out of its column, same as a
// colspan="1" cell wouldn't.
func TestCellColSpanInvalidValueDefault(t *testing.T) {
	src := `<html><body style="margin:0"><table style="width:100px">` +
		`<tr><td colspan="bogus" style="padding:0">A</td><td style="padding:0">BB</td></tr>` +
		`<tr><td colspan="0" style="padding:0">C</td><td style="padding:0">DD</td></tr>` +
		`</table></body></html>`
	tbl := findBox(layoutHTML(t, src, 300), "table")
	// natural: col0=max(10,10)=10, col1=max(20,20)=20; sum=30; scale=100/30.
	want := 10 * (100.0 / 30.0)
	if got := tbl.Children[0].Children[1].X; got != want {
		t.Errorf("row 0 second cell X = %v, want %v (colspan=\"bogus\" must not shift it)", got, want)
	}
	if got := tbl.Children[1].Children[1].X; got != want {
		t.Errorf("row 1 second cell X = %v, want %v (colspan=\"0\" must not shift it)", got, want)
	}
}

func TestTableEmpty(t *testing.T) {
	// A table with no rows lays out to nothing (no panic).
	src := `<html><body style="margin:0"><table style="width:100px"></table></body></html>`
	tbl := findBox(layoutHTML(t, src, 300), "table")
	if len(tbl.Children) != 0 {
		t.Errorf("empty table children = %d", len(tbl.Children))
	}
}

// ---- line-height -----------------------------------------------------------

func TestLineHeightExplicit(t *testing.T) {
	// line-height 40 makes each line 40px tall (fake font natural height 20).
	src := `<html><body style="margin:0"><div style="line-height:40px;width:35px">a b c d</div></body></html>`
	div := findBox(layoutHTML(t, src, 300), "div")
	if len(div.Lines) != 2 {
		t.Fatalf("lines = %d", len(div.Lines))
	}
	assertF(t, "lh.line0.H", div.Lines[0].H, 40)
	assertF(t, "lh.div.H", div.H, 80)
}

func TestLineHeightUnitlessMultiplier(t *testing.T) {
	// line-height 2 with font-size 16 → 32px lines (fake metrics ignore size but
	// the multiplier resolves against the em, giving 32).
	src := `<html><body style="margin:0"><div style="font-size:16px;line-height:2">hi</div></body></html>`
	div := findBox(layoutHTML(t, src, 300), "div")
	assertF(t, "lhm.line0.H", div.Lines[0].H, 32)
}

// ---- inline-block and display fallbacks ------------------------------------

func TestInlineBlockTreatedInline(t *testing.T) {
	// inline-block participates in the inline run (does not break the line).
	src := `<html><body style="margin:0"><div style="margin:0;padding:0">` +
		`x<span style="display:inline-block">y</span>z</div></body></html>`
	items := firstLineItems(findBox(layoutHTML(t, src, 300), "div"))
	if len(items) != 3 {
		t.Fatalf("inline-block items = %v", texts(items))
	}
}

func TestFlexContainerHeight(t *testing.T) {
	// The flex container's height reflects its tallest item (cross size).
	src := `<html><body style="margin:0"><div style="display:flex;margin:0;padding:0">` +
		`<div style="width:50px;height:60px">A</div>` +
		`<div style="width:50px;height:30px">B</div></div></body></html>`
	outer := findBox(layoutHTML(t, src, 300), "div")
	assertF(t, "flexh.H", outer.H, 60)
}

func TestTableContainerHeight(t *testing.T) {
	// The table box height is the sum of its row heights.
	src := `<html><body style="margin:0"><table style="width:200px;margin:0;padding:0"><tbody>` +
		`<tr><td style="padding:0">A</td></tr>` +
		`<tr><td style="padding:0">B</td></tr></tbody></table></body></html>`
	tbl := findBox(layoutHTML(t, src, 300), "table")
	assertF(t, "tableh.H", tbl.H, 40) // two 20px rows
}
