// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package layout

import "testing"

// boxText returns the first inline text of a box (used to identify which child
// a reordered flex/grid item corresponds to).
func boxText(b *Box) string {
	if b == nil {
		return ""
	}
	for _, ln := range b.Lines {
		for _, it := range ln.Items {
			if it.Text != "" {
				return it.Text
			}
		}
	}
	for _, c := range b.Children {
		if t := boxText(c); t != "" {
			return t
		}
	}
	return ""
}

// ---- flex-wrap -------------------------------------------------------------

func TestFlexWrapTwoLines(t *testing.T) {
	// Three 100px items in a 250px wrapping row: line 1 holds two, line 2 the
	// third. Cross size is one text line (20px) per line.
	src := `<html><body style="margin:0"><div style="display:flex;flex-wrap:wrap;width:250px">` +
		`<div style="width:100px;height:20px">A</div>` +
		`<div style="width:100px;height:20px">B</div>` +
		`<div style="width:100px;height:20px">C</div></div></body></html>`
	outer := findBox(layoutHTML(t, src, 400), "div")
	if len(outer.Children) != 3 {
		t.Fatalf("children = %d", len(outer.Children))
	}
	assertF(t, "wrap.A.X", outer.Children[0].X, 0)
	assertF(t, "wrap.A.Y", outer.Children[0].Y, 0)
	assertF(t, "wrap.B.X", outer.Children[1].X, 100)
	assertF(t, "wrap.B.Y", outer.Children[1].Y, 0)
	assertF(t, "wrap.C.X", outer.Children[2].X, 0)
	assertF(t, "wrap.C.Y", outer.Children[2].Y, 20)
	assertF(t, "wrap.container.H", outer.H, 40)
}

func TestFlexNoWrapSingleLine(t *testing.T) {
	// Without wrap (and shrink disabled) the three items stay on one overflowing
	// line at their full widths.
	src := `<html><body style="margin:0"><div style="display:flex;width:250px">` +
		`<div style="width:100px;height:20px;flex-shrink:0">A</div>` +
		`<div style="width:100px;height:20px;flex-shrink:0">B</div>` +
		`<div style="width:100px;height:20px;flex-shrink:0">C</div></div></body></html>`
	outer := findBox(layoutHTML(t, src, 400), "div")
	assertF(t, "nowrap.C.X", outer.Children[2].X, 200)
	assertF(t, "nowrap.C.Y", outer.Children[2].Y, 0)
	assertF(t, "nowrap.container.H", outer.H, 20)
}

func TestFlexWrapReverse(t *testing.T) {
	// wrap-reverse reverses the line order along the cross axis: the second line
	// (item C) is painted at the top, the first line below it.
	src := `<html><body style="margin:0"><div style="display:flex;flex-wrap:wrap-reverse;width:250px">` +
		`<div style="width:100px;height:20px">A</div>` +
		`<div style="width:100px;height:20px">B</div>` +
		`<div style="width:100px;height:20px">C</div></div></body></html>`
	outer := findBox(layoutHTML(t, src, 400), "div")
	// C is on its own (second) line, which reverse moves to y=0.
	byText := map[string]*Box{}
	for _, c := range outer.Children {
		byText[boxText(c)] = c
	}
	assertF(t, "wr.C.Y", byText["C"].Y, 0)
	assertF(t, "wr.A.Y", byText["A"].Y, 20)
	assertF(t, "wr.B.Y", byText["B"].Y, 20)
}

// ---- gap -------------------------------------------------------------------

func TestFlexColumnGap(t *testing.T) {
	// column-gap adds 20px between items on the main axis.
	src := `<html><body style="margin:0"><div style="display:flex;column-gap:20px;width:300px">` +
		`<div style="width:100px;height:20px">A</div>` +
		`<div style="width:100px;height:20px">B</div></div></body></html>`
	outer := findBox(layoutHTML(t, src, 400), "div")
	assertF(t, "cgap.A.X", outer.Children[0].X, 0)
	assertF(t, "cgap.B.X", outer.Children[1].X, 120)
}

func TestFlexRowGapWithWrap(t *testing.T) {
	// row-gap adds 10px between wrapped lines on the cross axis.
	src := `<html><body style="margin:0"><div style="display:flex;flex-wrap:wrap;row-gap:10px;width:250px">` +
		`<div style="width:100px;height:20px">A</div>` +
		`<div style="width:100px;height:20px">B</div>` +
		`<div style="width:100px;height:20px">C</div></div></body></html>`
	outer := findBox(layoutHTML(t, src, 400), "div")
	assertF(t, "rgap.C.Y", outer.Children[2].Y, 30) // 20 line + 10 gap
	assertF(t, "rgap.container.H", outer.H, 50)      // 20 + 10 + 20
}

func TestFlexGapShorthandTwoValues(t *testing.T) {
	// gap: <row> <column> — row-gap 10, column-gap 40.
	src := `<html><body style="margin:0"><div style="display:flex;flex-wrap:wrap;gap:10px 40px;width:240px">` +
		`<div style="width:100px;height:20px">A</div>` +
		`<div style="width:100px;height:20px">B</div>` +
		`<div style="width:100px;height:20px">C</div></div></body></html>`
	outer := findBox(layoutHTML(t, src, 400), "div")
	// A(0) + gap40 + B would be 100+40+100 = 240 <= 240, so A,B share line 1.
	assertF(t, "gap2.B.X", outer.Children[1].X, 140)
	assertF(t, "gap2.C.Y", outer.Children[2].Y, 30) // line 2 at 20 + rowgap 10
}

// ---- order -----------------------------------------------------------------

func TestFlexOrder(t *testing.T) {
	// order reorders items: C(order -1) first, then B(0), then A(order 2).
	src := `<html><body style="margin:0"><div style="display:flex">` +
		`<div style="width:50px;order:2">A</div>` +
		`<div style="width:50px">B</div>` +
		`<div style="width:50px;order:-1">C</div></div></body></html>`
	outer := findBox(layoutHTML(t, src, 300), "div")
	if got := boxText(outer.Children[0]); got != "C" {
		t.Fatalf("first item = %q want C", got)
	}
	assertF(t, "order.C.X", outer.Children[0].X, 0)
	assertF(t, "order.B.X", outer.Children[1].X, 50)
	assertF(t, "order.A.X", outer.Children[2].X, 100)
	if got := boxText(outer.Children[2]); got != "A" {
		t.Fatalf("last item = %q want A", got)
	}
}

// ---- align-content ---------------------------------------------------------

func TestFlexAlignContentCenter(t *testing.T) {
	// Two wrapped lines (40px total) centred in a 100px-tall container: 30px of
	// free space above, so line 1 starts at y=30, line 2 at y=50.
	src := `<html><body style="margin:0"><div style="display:flex;flex-wrap:wrap;align-content:center;width:250px;height:100px">` +
		`<div style="width:100px;height:20px">A</div>` +
		`<div style="width:100px;height:20px">B</div>` +
		`<div style="width:100px;height:20px">C</div></div></body></html>`
	outer := findBox(layoutHTML(t, src, 400), "div")
	assertF(t, "ac.A.Y", outer.Children[0].Y, 30)
	assertF(t, "ac.C.Y", outer.Children[2].Y, 50)
	assertF(t, "ac.container.H", outer.H, 100)
}

func TestFlexAlignContentSpaceBetween(t *testing.T) {
	// Two lines, space-between over 60px free → line1 at 0, line2 at 20+60=80.
	src := `<html><body style="margin:0"><div style="display:flex;flex-wrap:wrap;align-content:space-between;width:250px;height:100px">` +
		`<div style="width:100px;height:20px">A</div>` +
		`<div style="width:100px;height:20px">B</div>` +
		`<div style="width:100px;height:20px">C</div></div></body></html>`
	outer := findBox(layoutHTML(t, src, 400), "div")
	assertF(t, "acsb.A.Y", outer.Children[0].Y, 0)
	assertF(t, "acsb.C.Y", outer.Children[2].Y, 80)
}

// ---- align-self ------------------------------------------------------------

func TestFlexAlignSelf(t *testing.T) {
	// align-items:flex-start; one item overrides with align-self:flex-end.
	src := `<html><body style="margin:0"><div style="display:flex;align-items:flex-start">` +
		`<div style="width:50px;height:20px">A</div>` +
		`<div style="width:50px;height:60px">B</div>` +
		`<div style="width:50px;height:20px;align-self:flex-end">C</div></div></body></html>`
	outer := findBox(layoutHTML(t, src, 300), "div")
	assertF(t, "as.A.Y", outer.Children[0].Y, 0)  // flex-start
	assertF(t, "as.B.Y", outer.Children[1].Y, 0)  // tallest
	assertF(t, "as.C.Y", outer.Children[2].Y, 40) // flex-end: 60-20
}

// ---- min/max constraints in flex ------------------------------------------

func TestFlexGrowClampedByMaxWidth(t *testing.T) {
	// A single grow item would take the whole 300px but max-width caps it at 100.
	src := `<html><body style="margin:0"><div style="display:flex;width:300px">` +
		`<div style="width:50px;flex-grow:1;max-width:100px">A</div></div></body></html>`
	outer := findBox(layoutHTML(t, src, 400), "div")
	assertF(t, "maxw.A.W", outer.Children[0].W, 100)
}

func TestFlexShrinkClampedByMinWidth(t *testing.T) {
	// Two 50px items in a 60px container want to shrink to 30 each, but
	// min-width:40 stops them at 40 (overflowing the container).
	src := `<html><body style="margin:0"><div style="display:flex;width:60px">` +
		`<div style="width:50px;min-width:40px">A</div>` +
		`<div style="width:50px;min-width:40px">B</div></div></body></html>`
	outer := findBox(layoutHTML(t, src, 400), "div")
	assertF(t, "minw.A.W", outer.Children[0].W, 40)
	assertF(t, "minw.B.W", outer.Children[1].W, 40)
	assertF(t, "minw.B.X", outer.Children[1].X, 40)
}

func TestFlexStretchClampedByMaxHeight(t *testing.T) {
	// align-items:stretch stretches B toward the 60px line, but max-height:40
	// clamps B's height to 40.
	src := `<html><body style="margin:0"><div style="display:flex;align-items:stretch">` +
		`<div style="width:50px;height:60px">A</div>` +
		`<div style="width:50px;max-height:40px">B</div></div></body></html>`
	outer := findBox(layoutHTML(t, src, 300), "div")
	assertF(t, "maxh.B.H", outer.Children[1].H, 40)
}

// ---- flex column gap + align-self -----------------------------------------

func TestFlexColumnRowGap(t *testing.T) {
	// In a column container row-gap separates items along the vertical main axis.
	src := `<html><body style="margin:0"><div style="display:flex;flex-direction:column;row-gap:10px">` +
		`<div style="height:30px">A</div><div style="height:40px">B</div></div></body></html>`
	outer := findBox(layoutHTML(t, src, 300), "div")
	assertF(t, "colgap.A.Y", outer.Children[0].Y, 0)
	assertF(t, "colgap.B.Y", outer.Children[1].Y, 40) // 30 + 10 gap
}

func TestFlexColumnAlignSelfCenter(t *testing.T) {
	// Column cross axis is horizontal; align-self:center centres one item.
	src := `<html><body style="margin:0"><div style="display:flex;flex-direction:column;width:200px">` +
		`<div style="width:50px;height:20px;align-self:center">A</div></div></body></html>`
	outer := findBox(layoutHTML(t, src, 300), "div")
	assertF(t, "colas.A.X", outer.Children[0].X, 75) // (200-50)/2
}

// ---- flex-flow shorthand ---------------------------------------------------

func TestFlexFlowShorthand(t *testing.T) {
	// flex-flow sets direction + wrap together.
	src := `<html><body style="margin:0"><div style="display:flex;flex-flow:row wrap;width:150px">` +
		`<div style="width:100px;height:20px">A</div>` +
		`<div style="width:100px;height:20px">B</div></div></body></html>`
	outer := findBox(layoutHTML(t, src, 400), "div")
	assertF(t, "flow.B.Y", outer.Children[1].Y, 20) // wrapped to line 2
}
