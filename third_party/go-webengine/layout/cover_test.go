// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package layout

import (
	"testing"

	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/dom"
)

// ---- flex justify / align / basis / column branches ------------------------

func TestFlexJustifyEnd(t *testing.T) {
	src := `<html><body style="margin:0"><div style="display:flex;justify-content:flex-end">` +
		`<div style="width:100px">A</div></div></body></html>`
	outer := findBox(layoutHTML(t, src, 300), "div")
	assertF(t, "end.A.X", outer.Children[0].X, 200) // 300-100
}

func TestFlexSpaceAround(t *testing.T) {
	src := `<html><body style="margin:0"><div style="display:flex;justify-content:space-around">` +
		`<div style="width:100px">A</div><div style="width:100px">B</div></div></body></html>`
	outer := findBox(layoutHTML(t, src, 300), "div")
	// free=100, gap=50, offset=25 → A at 25, B at 25+100+50 = 175.
	assertF(t, "around.A.X", outer.Children[0].X, 25)
	assertF(t, "around.B.X", outer.Children[1].X, 175)
}

func TestFlexSpaceEvenly(t *testing.T) {
	src := `<html><body style="margin:0"><div style="display:flex;justify-content:space-evenly">` +
		`<div style="width:100px">A</div><div style="width:100px">B</div></div></body></html>`
	outer := findBox(layoutHTML(t, src, 300), "div")
	// free=100, gap=100/3, offset=100/3.
	got := outer.Children[0].X
	if got < 33.2 || got > 33.4 {
		t.Errorf("evenly.A.X = %v want ~33.33", got)
	}
}

func TestFlexSpaceBetweenSingle(t *testing.T) {
	src := `<html><body style="margin:0"><div style="display:flex;justify-content:space-between">` +
		`<div style="width:100px">A</div></div></body></html>`
	outer := findBox(layoutHTML(t, src, 300), "div")
	assertF(t, "sb1.A.X", outer.Children[0].X, 0) // single item: no gap
}

func TestFlexBasis(t *testing.T) {
	// flex-basis sets the main size regardless of width.
	src := `<html><body style="margin:0"><div style="display:flex">` +
		`<div style="width:20px;flex-basis:150px">A</div>` +
		`<div style="width:20px;flex-basis:50px">B</div></div></body></html>`
	outer := findBox(layoutHTML(t, src, 300), "div")
	assertF(t, "basisA.W", outer.Children[0].W, 150)
	assertF(t, "basisB.X", outer.Children[1].X, 150)
}

func TestFlexAlignFlexEnd(t *testing.T) {
	src := `<html><body style="margin:0"><div style="display:flex;align-items:flex-end">` +
		`<div style="width:50px;height:20px">A</div>` +
		`<div style="width:50px;height:60px">B</div></div></body></html>`
	outer := findBox(layoutHTML(t, src, 300), "div")
	// container cross 60; A (20) at bottom → offset 40.
	assertF(t, "aend.A.Y", outer.Children[0].Y, 40)
}

func TestFlexColumnAlignFlexStartWithWidth(t *testing.T) {
	// Column, align-items flex-start, an item with explicit width.
	src := `<html><body style="margin:0"><div style="display:flex;flex-direction:column;align-items:flex-start">` +
		`<div style="width:40px;height:10px">A</div>` +
		`<div style="height:15px">B</div></div></body></html>`
	outer := findBox(layoutHTML(t, src, 200), "div")
	a := outer.Children[0]
	assertF(t, "colfs.A.X", a.X, 0)
	assertF(t, "colfs.A.W", a.W, 40)
	assertF(t, "colfs.B.Y", outer.Children[1].Y, 10)
}

// ---- floats: right image beside text, clear both, stacking -----------------

func TestFloatRightImage(t *testing.T) {
	root, _ := dom.Parse(`<html><body style="margin:0">` +
		`<img id="i" style="float:right"><p style="margin:0">a b c d e f</p></body></html>`)
	sm := css.Cascade(root)
	img := dom.Find(root, "img")
	sizes := map[*dom.Node][2]float64{img: {60, 40}}
	box, _ := LayoutDocument(root, sm, 200, fakeMeasurer{}, sizes)
	f := findFloatBox(box)
	if f == nil {
		t.Fatal("no float box")
	}
	assertF(t, "fimg.X", f.X, 140) // 200 - 60, right-floated
	p := findBox(box, "p")
	assertF(t, "fimg.line0.W", p.Lines[0].W, 140) // text region shrinks by 60
}

func TestFloatClearBoth(t *testing.T) {
	src := `<html><body style="margin:0">` +
		`<div style="float:left;width:40px;height:30px" id="l">L</div>` +
		`<div style="float:right;width:40px;height:50px" id="r">R</div>` +
		`<div style="clear:both;margin:0;padding:0">x</div></body></html>`
	root := layoutHTML(t, src, 300)
	cleared := findBoxWithText(root, "x")
	assertF(t, "clearboth.Y", cleared.Y, 50) // below the taller (right) float
}

func TestFloatAutoWidthFromContent(t *testing.T) {
	// A float with no width shrinks to its content's max width ("xxxx" = 40).
	src := `<html><body style="margin:0">` +
		`<div style="float:left;padding:0" id="f">xxxx</div>` +
		`<p style="margin:0">a</p></body></html>`
	root := layoutHTML(t, src, 300)
	f := findBoxByID(root, "f")
	assertF(t, "autofloat.W", f.W, 40)
}

// ---- float context helpers -------------------------------------------------

func TestFloatCtxHelpers(t *testing.T) {
	fc := &floatCtx{}
	fc.add(css.FloatLeft, floatRect{x: 0, y: 0, w: 50, h: 100})
	fc.add(css.FloatRight, floatRect{x: 250, y: 0, w: 50, h: 40})
	left, right := fc.available(10, 20, 0, 300)
	assertF(t, "avail.left", left, 50)
	assertF(t, "avail.right", right, 250)
	// Below both floats: full region.
	left2, right2 := fc.available(120, 130, 0, 300)
	assertF(t, "avail2.left", left2, 0)
	assertF(t, "avail2.right", right2, 300)
	// clearY both.
	assertF(t, "clear.both", fc.clearY(css.ClearBoth, 0), 100)
	assertF(t, "clear.right", fc.clearY(css.ClearRight, 0), 40)
	// nextEdge finds the nearest bottom below y.
	assertF(t, "nextEdge", fc.nextEdge(0, 0, 300), 40)
	assertF(t, "nextEdge.none", fc.nextEdge(200, 0, 300), 200)
	assertF(t, "bottom", fc.bottom(), 100)
	// available where a right float would cross a left float clamps right>=left.
	fc2 := &floatCtx{}
	fc2.add(css.FloatLeft, floatRect{x: 0, y: 0, w: 200, h: 10})
	fc2.add(css.FloatRight, floatRect{x: 100, y: 0, w: 200, h: 10})
	l3, r3 := fc2.available(0, 5, 0, 300)
	if r3 < l3 {
		t.Errorf("right %v < left %v not clamped", r3, l3)
	}
}

// ---- preferredWidth branches ----------------------------------------------

func TestPreferredWidthPre(t *testing.T) {
	l := &layouter{m: fakeMeasurer{}, floats: &floatCtx{}}
	root, _ := dom.Parse(`<html><body><pre id="p">ab cd</pre></body></html>`)
	l.sm = css.Cascade(root)
	pre := dom.Find(root, "pre")
	// pre max-content keeps the whole line "ab cd" = 5 chars = 50.
	if got := l.preferredWidth(pre, l.sm[pre]); got != 50 {
		t.Errorf("pre preferred = %v want 50", got)
	}
}

func TestPreferredWidthNestedBlocks(t *testing.T) {
	l := &layouter{m: fakeMeasurer{}, floats: &floatCtx{}}
	root, _ := dom.Parse(`<html><body><div id="d">` +
		`<p style="margin:0">xx</p><p style="margin:0">xxxxx</p></div></body></html>`)
	l.sm = css.Cascade(root)
	d := dom.Find(root, "div")
	// max child preferred = "xxxxx" = 50.
	if got := l.preferredWidth(d, l.sm[d]); got != 50 {
		t.Errorf("nested preferred = %v want 50", got)
	}
}

// TestPreferredWidthFlexRowBareText covers a real regression: preferredWidth's
// flex-row branch only ever summed ELEMENT children, so a `display:flex`
// element whose content is bare text (no wrapping element at all — e.g.
// `<h1 style="display:flex">React</h1>`, confirmed live on react.dev's hero
// heading and its two CTA buttons) fell through the loop with zero element
// children counted and returned a bare 0, collapsing the whole element to
// zero width (and, via layoutIsolated laying it out at that zero content
// width, zero height too).
func TestPreferredWidthFlexRowBareText(t *testing.T) {
	l := &layouter{m: fakeMeasurer{}, floats: &floatCtx{}}
	root, _ := dom.Parse(`<html><body><h1 id="h" style="display:flex">React</h1></body></html>`)
	l.sm = css.Cascade(root)
	h1 := dom.Find(root, "h1")
	// "React" = 5 chars = 50 under fakeMeasurer, same convention as the pre
	// case above — NOT 0.
	if got := l.preferredWidth(h1, l.sm[h1]); got != 50 {
		t.Errorf("flex-row bare-text preferred = %v, want 50", got)
	}
}

// TestPreferredWidthFlexRowMixedTextAndElement covers that the fix's
// text-only fallback does not regress the ORIGINAL element-summing case this
// branch exists for (a flex row with real element children still sums their
// widths, not falls back to treating the whole node as one inline run).
func TestPreferredWidthFlexRowMixedTextAndElement(t *testing.T) {
	l := &layouter{m: fakeMeasurer{}, floats: &floatCtx{}}
	root, _ := dom.Parse(`<html><body><div id="d" style="display:flex">` +
		`<span style="margin:0">xx</span><span style="margin:0">xxxxx</span></div></body></html>`)
	l.sm = css.Cascade(root)
	d := dom.Find(root, "div")
	// Sum of the two spans' preferred widths = 20 + 50 = 70 (no gap set).
	if got := l.preferredWidth(d, l.sm[d]); got != 70 {
		t.Errorf("flex-row element-sum preferred = %v, want 70 (unchanged by the bare-text fallback)", got)
	}
}

// ---- table: caption skip, cellStyle fallback -------------------------------

func TestTableIgnoresNonRows(t *testing.T) {
	// A <caption> (display block) among rows is ignored by the row collector.
	src := `<html><body style="margin:0"><table style="width:100px">` +
		`<caption>Cap</caption><tr><td style="padding:0">A</td></tr></table></body></html>`
	tbl := findBox(layoutHTML(t, src, 300), "table")
	if len(tbl.Children) != 1 {
		t.Fatalf("rows (caption ignored) = %d", len(tbl.Children))
	}
}

func TestCellStyleFallback(t *testing.T) {
	l := &layouter{sm: css.StyleMap{}, m: fakeMeasurer{}, floats: &floatCtx{}}
	cell := &dom.Node{Type: dom.Element, Tag: "td"}
	st := l.cellStyle(cell)
	if st == nil || st.Display != css.DisplayTableCell {
		t.Errorf("fallback cell style = %+v", st)
	}
}

// ---- width/height edge branches --------------------------------------------

func TestPercentHeightIgnored(t *testing.T) {
	// A percentage height has no definite basis and is skipped (auto height).
	src := `<html><body style="margin:0"><div style="height:50%;margin:0;padding:0">x</div></body></html>`
	div := findBox(layoutHTML(t, src, 300), "div")
	assertF(t, "pcth.H", div.H, 20) // one line, height ignored
}

func TestExplicitHeightBorderBox(t *testing.T) {
	src := `<html><body style="margin:0"><div style="box-sizing:border-box;height:50px;padding:5px;border:5px solid black;margin:0">x</div></body></html>`
	div := findBox(layoutHTML(t, src, 300), "div")
	assertF(t, "ehbb.H", div.H, 50) // border box height fixed at 50
}

func TestMaxWidthPercentBound(t *testing.T) {
	// max-width:50% of a 400px container = 200; width auto fills then clamps.
	src := `<html><body style="margin:0"><div style="max-width:50%;margin:0 auto">x</div></body></html>`
	div := findBox(layoutHTML(t, src, 400), "div")
	assertF(t, "mwpct.ContentW", div.ContentW, 200)
	assertF(t, "mwpct.X", div.X, 100)
}

// ---- inline alignment inside pre; forceOne leading break -------------------

func TestPreTextAlignCenter(t *testing.T) {
	src := "<html><body style=\"margin:0\"><pre style=\"margin:0;padding:0;text-align:center;width:100px\">ab</pre></body></html>"
	pre := findBox(layoutHTML(t, src, 300), "pre")
	it := pre.Lines[0].Items[0]
	assertF(t, "prectr.offset", it.X-pre.ContentX, 40) // (100-20)/2
}

func TestForceOneWordTooWide(t *testing.T) {
	// A single word wider than the content box is forced onto its own line.
	src := `<html><body style="margin:0"><div style="width:15px;margin:0;padding:0">aaaaaa word</div></body></html>`
	div := findBox(layoutHTML(t, src, 300), "div")
	if len(div.Lines) < 2 {
		t.Fatalf("expected wrapping, got %d lines", len(div.Lines))
	}
	assertF(t, "force.line0.item0.W", div.Lines[0].Items[0].Width, 60)
}

func TestFloatTallerThanContentExtendsPage(t *testing.T) {
	// A float taller than the flow content still contributes to page height.
	src := `<html><body style="margin:0">` +
		`<div style="float:left;width:20px;height:500px">F</div>` +
		`<p style="margin:0">x</p></body></html>`
	root, _ := dom.Parse(src)
	sm := css.Cascade(root)
	_, h := LayoutDocument(root, sm, 300, fakeMeasurer{}, nil)
	if h < 500 {
		t.Errorf("page height = %v, want >= 500 (float extends it)", h)
	}
}
