// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package layout

import "testing"

func gridByText(g *Box) map[string]*Box {
	m := map[string]*Box{}
	for _, c := range g.Children {
		m[boxText(c)] = c
	}
	return m
}

// ---- flex edge cases -------------------------------------------------------

func TestFlexPercentGapIsZero(t *testing.T) {
	// A percentage gap resolves to 0 against an auto container size.
	src := `<html><body style="margin:0"><div style="display:flex;column-gap:10%;width:300px">` +
		`<div style="width:100px;height:20px">A</div>` +
		`<div style="width:100px;height:20px">B</div></div></body></html>`
	outer := findBox(layoutHTML(t, src, 400), "div")
	assertF(t, "pgap.B.X", outer.Children[1].X, 100) // gap 0
}

func TestFlexMinHeightRaisesCross(t *testing.T) {
	// min-height on a flex item lifts its cross size (and the line/container).
	src := `<html><body style="margin:0"><div style="display:flex">` +
		`<div style="width:50px;min-height:40px">A</div></div></body></html>`
	outer := findBox(layoutHTML(t, src, 300), "div")
	assertF(t, "minh.container.H", outer.H, 40)
}

func TestFlexPercentMinHeightNoContainer(t *testing.T) {
	// A percentage min-height with no definite container height is ignored.
	src := `<html><body style="margin:0"><div style="display:flex">` +
		`<div style="width:50px;min-height:50%">A</div></div></body></html>`
	outer := findBox(layoutHTML(t, src, 300), "div")
	assertF(t, "pminh.container.H", outer.H, 20) // just the text line
}

func TestFlexNegativeMinHeightIgnored(t *testing.T) {
	// A negative min-height is invalid and ignored (the natural line stands).
	src := `<html><body style="margin:0"><div style="display:flex">` +
		`<div style="width:50px;min-height:-5px">A</div></div></body></html>`
	outer := findBox(layoutHTML(t, src, 300), "div")
	assertF(t, "negminh.container.H", outer.H, 20)
}

func TestFlexBorderBoxMaxHeightClamps(t *testing.T) {
	// border-box max-height caps the (border-box) height minus padding.
	src := `<html><body style="margin:0"><div style="display:flex;align-items:stretch">` +
		`<div style="width:50px;height:80px">A</div>` +
		`<div style="width:50px;box-sizing:border-box;max-height:50px;padding:10px">B</div></div></body></html>`
	outer := findBox(layoutHTML(t, src, 300), "div")
	// B stretch toward 80; max-height 50 (border-box) -> content 30 + pad 20 = 50.
	assertF(t, "bbmaxh.B.H", outer.Children[1].H, 50)
}

func TestFlexAlignContentEnd(t *testing.T) {
	src := `<html><body style="margin:0"><div style="display:flex;flex-wrap:wrap;align-content:flex-end;width:250px;height:100px">` +
		`<div style="width:100px;height:20px">A</div>` +
		`<div style="width:100px;height:20px">B</div>` +
		`<div style="width:100px;height:20px">C</div></div></body></html>`
	outer := findBox(layoutHTML(t, src, 400), "div")
	// Two lines (40px) packed at the end of a 100px box: first line at y=60.
	assertF(t, "ace.A.Y", outer.Children[0].Y, 60)
	assertF(t, "ace.C.Y", outer.Children[2].Y, 80)
}

func TestFlexAlignContentSpaceAround(t *testing.T) {
	src := `<html><body style="margin:0"><div style="display:flex;flex-wrap:wrap;align-content:space-around;width:250px;height:100px">` +
		`<div style="width:100px;height:20px">A</div>` +
		`<div style="width:100px;height:20px">B</div>` +
		`<div style="width:100px;height:20px">C</div></div></body></html>`
	outer := findBox(layoutHTML(t, src, 400), "div")
	// free 60 over 2 lines: half-gap 15 leads, line2 at 15+20+30 = 65.
	assertF(t, "aca.A.Y", outer.Children[0].Y, 15)
	assertF(t, "aca.C.Y", outer.Children[2].Y, 65)
}

func TestFlexAlignContentSpaceEvenly(t *testing.T) {
	src := `<html><body style="margin:0"><div style="display:flex;flex-wrap:wrap;align-content:space-evenly;width:250px;height:100px">` +
		`<div style="width:100px;height:20px">A</div>` +
		`<div style="width:100px;height:20px">B</div>` +
		`<div style="width:100px;height:20px">C</div></div></body></html>`
	outer := findBox(layoutHTML(t, src, 400), "div")
	// free 60 over 3 gaps (evenly) = 20 each: line1 at 20, line2 at 20+20+20=60.
	assertF(t, "ace2.A.Y", outer.Children[0].Y, 20)
	assertF(t, "ace2.C.Y", outer.Children[2].Y, 60)
}

// ---- grid edge cases -------------------------------------------------------

func TestGridSkipsTextAndHiddenChildren(t *testing.T) {
	// Whitespace/text nodes and display:none children are not grid items.
	src := `<html><body style="margin:0"><div style="display:grid;grid-template-columns:100px 100px">` +
		"text\n" + `<div>A</div><div style="display:none">X</div><div>B</div></div></body></html>`
	g := findBox(layoutHTML(t, src, 400), "div")
	if len(g.Children) != 2 {
		t.Fatalf("grid items = %d want 2", len(g.Children))
	}
}

func TestGridImpliedColumnsFromLines(t *testing.T) {
	// No template: implied column count comes from an item's explicit lines.
	src := `<html><body style="margin:0"><div style="display:grid;width:300px">` +
		`<div style="grid-column:2 / 4">A</div></div></body></html>`
	g := findBox(layoutHTML(t, src, 400), "div")
	// 3 implied auto columns stretch to 100 each; A spans columns 2..3.
	assertF(t, "impl.A.X", g.Children[0].X, 100)
	assertF(t, "impl.A.W", g.Children[0].W, 200)
}

func TestGridAutoColumnsBeyondTemplate(t *testing.T) {
	// An item placed past the template uses grid-auto-columns for the new track.
	src := `<html><body style="margin:0"><div style="display:grid;grid-template-columns:100px;grid-auto-columns:60px">` +
		`<div style="grid-column:2">A</div></div></body></html>`
	g := findBox(layoutHTML(t, src, 400), "div")
	assertF(t, "auto.A.X", g.Children[0].X, 100) // column 2 begins after the 100px col
	assertF(t, "auto.A.W", g.Children[0].W, 60)
}

func TestGridItemExtendsColumnCount(t *testing.T) {
	// grid-column:1/4 forces a third (auto) column past the 2-track template.
	src := `<html><body style="margin:0"><div style="display:grid;grid-template-columns:100px 100px;width:300px">` +
		`<div style="grid-column:1 / 4">A</div></div></body></html>`
	g := findBox(layoutHTML(t, src, 400), "div")
	assertF(t, "ext.A.W", g.Children[0].W, 300) // spans all three columns
}

func TestGridNegativeAndReversedLines(t *testing.T) {
	cases := []struct {
		decl    string
		wantX   float64
		wantW   float64
		comment string
	}{
		{"grid-column:2 / -1", 50, 150, "negative end line"},
		{"grid-column:4 / 2", 50, 100, "reversed lines swap"},
		{"grid-column:2 / 2", 50, 50, "same line -> span 1"},
		{"grid-column:span 2 / 4", 50, 100, "span start, line end"},
	}
	for _, c := range cases {
		src := `<html><body style="margin:0"><div style="display:grid;grid-template-columns:repeat(4, 50px)">` +
			`<div style="` + c.decl + `">A</div></div></body></html>`
		g := findBox(layoutHTML(t, src, 400), "div")
		assertF(t, c.comment+".X", g.Children[0].X, c.wantX)
		assertF(t, c.comment+".W", g.Children[0].W, c.wantW)
	}
}

func TestGridEndSpanOnly(t *testing.T) {
	// grid-column-end: span 2 with an auto start auto-places a 2-column item.
	src := `<html><body style="margin:0"><div style="display:grid;grid-template-columns:repeat(4, 50px)">` +
		`<div style="grid-column-end:span 2">A</div></div></body></html>`
	g := findBox(layoutHTML(t, src, 400), "div")
	assertF(t, "endspan.A.X", g.Children[0].X, 0)
	assertF(t, "endspan.A.W", g.Children[0].W, 100)
}

func TestGridEndLineOnly(t *testing.T) {
	// grid-column: auto / 3 (start auto) auto-places a single-column item.
	src := `<html><body style="margin:0"><div style="display:grid;grid-template-columns:repeat(4, 50px)">` +
		`<div style="grid-column:auto / 3">A</div></div></body></html>`
	g := findBox(layoutHTML(t, src, 400), "div")
	assertF(t, "endline.A.X", g.Children[0].X, 0)
	assertF(t, "endline.A.W", g.Children[0].W, 50)
}

func TestGridFixedRowAutoColumn(t *testing.T) {
	// An item with a definite row but auto column flows into that row's first
	// free column.
	src := `<html><body style="margin:0"><div style="display:grid;grid-template-columns:repeat(2, 50px)">` +
		`<div>A</div><div style="grid-row:2">B</div></div></body></html>`
	g := findBox(layoutHTML(t, src, 400), "div")
	m := gridByText(g)
	assertF(t, "frac.A.Y", m["A"].Y, 0)
	assertF(t, "frac.B.X", m["B"].X, 0)  // first free column in row 2
	assertF(t, "frac.B.Y", m["B"].Y, 20) // second row
}

func TestGridAreasWiderThanTemplate(t *testing.T) {
	// grid-template-areas declares more columns/rows than the column template;
	// the grid grows to the areas' extent.
	src := `<html><body style="margin:0"><div style="display:grid;width:300px;` +
		`grid-template-columns:100px;grid-template-areas:'a a a' 'b b b'">` +
		`<div style="grid-area:a">A</div><div style="grid-area:b">B</div></div></body></html>`
	g := findBox(layoutHTML(t, src, 400), "div")
	m := gridByText(g)
	assertF(t, "wide.A.W", m["A"].W, 300) // spans all three columns
	assertF(t, "wide.B.Y", m["B"].Y, 20)  // second row
}

// ---- grid track sizing edges ----------------------------------------------

func TestGridPercentColumns(t *testing.T) {
	src := `<html><body style="margin:0"><div style="display:grid;grid-template-columns:50% 50%;width:200px">` +
		`<div>A</div><div>B</div></div></body></html>`
	g := findBox(layoutHTML(t, src, 400), "div")
	assertF(t, "pc.A.W", g.Children[0].W, 100)
	assertF(t, "pc.B.X", g.Children[1].X, 100)
}

func TestGridPercentColumnGap(t *testing.T) {
	// A percentage column-gap resolves against the grid content width.
	src := `<html><body style="margin:0"><div style="display:grid;grid-template-columns:100px 100px;column-gap:10%;width:300px">` +
		`<div>A</div><div>B</div></div></body></html>`
	g := findBox(layoutHTML(t, src, 400), "div")
	assertF(t, "pcg.B.X", g.Children[1].X, 130) // 100 + 30 gap
}

func TestGridFrRowsWithHeight(t *testing.T) {
	// fr rows distribute a definite container height.
	src := `<html><body style="margin:0"><div style="display:grid;grid-template-columns:100px;grid-template-rows:1fr 1fr;height:100px">` +
		`<div>A</div><div>B</div></div></body></html>`
	g := findBox(layoutHTML(t, src, 400), "div")
	assertF(t, "frrow.A.Y", g.Children[0].Y, 0)
	assertF(t, "frrow.B.Y", g.Children[1].Y, 50)
}

func TestGridPercentRowsWithHeight(t *testing.T) {
	src := `<html><body style="margin:0"><div style="display:grid;grid-template-columns:100px;grid-template-rows:50% 50%;height:200px">` +
		`<div>A</div><div>B</div></div></body></html>`
	g := findBox(layoutHTML(t, src, 400), "div")
	assertF(t, "prow.B.Y", g.Children[1].Y, 100)
}

func TestGridMinmaxRowWithHeight(t *testing.T) {
	// minmax(40px,1fr) rows: base 40, grown by leftover height.
	src := `<html><body style="margin:0"><div style="display:grid;grid-template-columns:100px;grid-template-rows:minmax(40px,1fr) minmax(40px,1fr);height:120px">` +
		`<div>A</div><div>B</div></div></body></html>`
	g := findBox(layoutHTML(t, src, 400), "div")
	assertF(t, "mmrow.B.Y", g.Children[1].Y, 60) // each row 60
}

func TestGridRowSpanGrowsAutoRow(t *testing.T) {
	// A tall item spanning two auto rows grows the last spanned row to fit.
	src := `<html><body style="margin:0"><div style="display:grid;grid-template-columns:100px">` +
		`<div style="grid-row:1 / span 2;height:100px">A</div>` +
		`<div>B</div></div></body></html>`
	g := findBox(layoutHTML(t, src, 400), "div")
	m := gridByText(g)
	assertF(t, "rsg.A.Y", m["A"].Y, 0)
	assertF(t, "rsg.A.H", m["A"].H, 100)
	assertF(t, "rsg.B.Y", m["B"].Y, 100) // below the grown span
}

// ---- grid item alignment/width edges --------------------------------------

func TestGridJustifyStartContentWidth(t *testing.T) {
	// justify-items:start with an auto-width item uses its content max-width.
	src := `<html><body style="margin:0"><div style="display:grid;grid-template-columns:100px;justify-items:start">` +
		`<div>AB</div></div></body></html>`
	g := findBox(layoutHTML(t, src, 400), "div")
	assertF(t, "jsw.A.W", g.Children[0].W, 20) // "AB" = 2 runes
	assertF(t, "jsw.A.X", g.Children[0].X, 0)
}

func TestGridJustifyStartBorderBoxWidth(t *testing.T) {
	// A non-stretched item keeps its border-box width inside the cell.
	src := `<html><body style="margin:0"><div style="display:grid;grid-template-columns:100px;justify-items:end">` +
		`<div style="width:60px;box-sizing:border-box;padding:0 10px">A</div></div></body></html>`
	g := findBox(layoutHTML(t, src, 400), "div")
	a := g.Children[0]
	assertF(t, "jbb.A.W", a.W, 60)  // border-box width preserved
	assertF(t, "jbb.A.X", a.X, 40)  // right-aligned: 100-60
}

func TestFlexAlignContentFlexStart(t *testing.T) {
	// Default-like start packing leaves the lines at the top with free space
	// below.
	src := `<html><body style="margin:0"><div style="display:flex;flex-wrap:wrap;align-content:flex-start;width:250px;height:100px">` +
		`<div style="width:100px;height:20px">A</div>` +
		`<div style="width:100px;height:20px">B</div>` +
		`<div style="width:100px;height:20px">C</div></div></body></html>`
	outer := findBox(layoutHTML(t, src, 400), "div")
	assertF(t, "acs.A.Y", outer.Children[0].Y, 0)
	assertF(t, "acs.C.Y", outer.Children[2].Y, 20)
}

func TestFlexAlignContentSingleLine(t *testing.T) {
	// A single line with space-between and spare cross space stays at the top.
	src := `<html><body style="margin:0"><div style="display:flex;flex-wrap:wrap;align-content:space-between;width:300px;height:100px">` +
		`<div style="width:100px;height:20px">A</div></div></body></html>`
	outer := findBox(layoutHTML(t, src, 400), "div")
	assertF(t, "acsingle.A.Y", outer.Children[0].Y, 0)
}

func TestGridSameColumnStacksRows(t *testing.T) {
	// Two items pinned to column 1 stack into successive rows.
	src := `<html><body style="margin:0"><div style="display:grid;grid-template-columns:repeat(2, 50px)">` +
		`<div style="grid-column:1">A</div><div style="grid-column:1">B</div></div></body></html>`
	g := findBox(layoutHTML(t, src, 400), "div")
	m := gridByText(g)
	assertF(t, "samecol.A.Y", m["A"].Y, 0)
	assertF(t, "samecol.B.Y", m["B"].Y, 20) // second row
	assertF(t, "samecol.B.X", m["B"].X, 0)
}

func TestGridFixedRowColumnScan(t *testing.T) {
	// Several items pinned to row 2 fill its columns then wrap back over it.
	src := `<html><body style="margin:0"><div style="display:grid;grid-template-columns:repeat(2, 50px)">` +
		`<div>A</div>` + // (0,0)
		`<div style="grid-row:2">B</div>` + // (1,0)
		`<div style="grid-row:2">C</div>` + // (1,1)
		`<div style="grid-row:2">D</div></div></body></html>` // wraps to (1,0)
	g := findBox(layoutHTML(t, src, 400), "div")
	m := gridByText(g)
	assertF(t, "frcs.B.X", m["B"].X, 0)
	assertF(t, "frcs.C.X", m["C"].X, 50)
	assertF(t, "frcs.D.X", m["D"].X, 0) // reset to column 1
}

func TestGridAreaNameNotFound(t *testing.T) {
	// grid-area referencing a name absent from the areas grid falls back to
	// auto-placement.
	src := `<html><body style="margin:0"><div style="display:grid;grid-template-columns:100px 100px;grid-template-areas:'a a'">` +
		`<div style="grid-area:a">A</div><div style="grid-area:zzz">Z</div></div></body></html>`
	g := findBox(layoutHTML(t, src, 400), "div")
	m := gridByText(g)
	assertF(t, "nf.A.W", m["A"].W, 200) // named area a spans both columns
	// Z (unknown area) auto-places into the next free cell (row 2, column 1).
	assertF(t, "nf.Z.Y", m["Z"].Y, 20)
}

func TestGridSpanStartVariants(t *testing.T) {
	cases := []struct {
		decl        string
		wantX, wantW float64
	}{
		{"grid-column:span 2", 0, 100},       // span-only start, auto-placed
		{"grid-column:span 1 / 3", 50, 50},   // span 1 + end line
		{"grid-column:span 3 / 2", 0, 150},   // span clamped to start 0
		{"grid-column-end:span 1", 0, 50},    // end span of 1
	}
	for _, c := range cases {
		src := `<html><body style="margin:0"><div style="display:grid;grid-template-columns:repeat(4, 50px)">` +
			`<div style="` + c.decl + `">A</div></div></body></html>`
		g := findBox(layoutHTML(t, src, 400), "div")
		assertF(t, c.decl+".X", g.Children[0].X, c.wantX)
		assertF(t, c.decl+".W", g.Children[0].W, c.wantW)
	}
}

func TestGridMinmaxAutoFrRow(t *testing.T) {
	// minmax(auto,1fr) row: auto min (content 0) + fr max distributed over the
	// definite height.
	src := `<html><body style="margin:0"><div style="display:grid;grid-template-columns:100px;grid-template-rows:minmax(auto,1fr);height:80px">` +
		`<div>A</div></div></body></html>`
	g := findBox(layoutHTML(t, src, 400), "div")
	assertF(t, "mmafr.A.H", g.Children[0].H, 80)
}

func TestGridItemMaxWidthClamp(t *testing.T) {
	// max-width clamps a non-stretched item's width within its cell.
	src := `<html><body style="margin:0"><div style="display:grid;grid-template-columns:200px;justify-items:start">` +
		`<div style="width:150px;max-width:80px">A</div></div></body></html>`
	g := findBox(layoutHTML(t, src, 400), "div")
	assertF(t, "gmw.A.W", g.Children[0].W, 80)
}
