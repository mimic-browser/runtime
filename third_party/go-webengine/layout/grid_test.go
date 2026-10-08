// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package layout

import (
	"testing"

	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/dom"
)

// ---- track sizing ----------------------------------------------------------

func TestGridThreeFrColumns(t *testing.T) {
	// Three equal fr columns split a 300px grid into 100px cells; items stretch.
	src := `<html><body style="margin:0"><div style="display:grid;grid-template-columns:1fr 1fr 1fr">` +
		`<div>A</div><div>B</div><div>C</div></div></body></html>`
	g := findBox(layoutHTML(t, src, 300), "div")
	if len(g.Children) != 3 {
		t.Fatalf("children = %d", len(g.Children))
	}
	assertF(t, "fr.A.X", g.Children[0].X, 0)
	assertF(t, "fr.A.W", g.Children[0].W, 100)
	assertF(t, "fr.B.X", g.Children[1].X, 100)
	assertF(t, "fr.C.X", g.Children[2].X, 200)
	assertF(t, "fr.row.Y", g.Children[2].Y, 0)
	assertF(t, "fr.container.H", g.H, 20)
}

func TestGridFixedColumnsWithColumnGap(t *testing.T) {
	// Fixed px tracks + a 20px column gap.
	src := `<html><body style="margin:0"><div style="display:grid;grid-template-columns:100px 200px;column-gap:20px">` +
		`<div>A</div><div>B</div></div></body></html>`
	g := findBox(layoutHTML(t, src, 500), "div")
	assertF(t, "fx.A.X", g.Children[0].X, 0)
	assertF(t, "fx.A.W", g.Children[0].W, 100)
	assertF(t, "fx.B.X", g.Children[1].X, 120)
	assertF(t, "fx.B.W", g.Children[1].W, 200)
}

func TestGridRepeatAutoFlowWraps(t *testing.T) {
	// repeat(2,100px): the third item flows onto row 2.
	src := `<html><body style="margin:0"><div style="display:grid;grid-template-columns:repeat(2, 100px)">` +
		`<div>A</div><div>B</div><div>C</div></div></body></html>`
	g := findBox(layoutHTML(t, src, 400), "div")
	assertF(t, "rep.C.X", g.Children[2].X, 0)
	assertF(t, "rep.C.Y", g.Children[2].Y, 20) // row 2 below the 20px first row
	assertF(t, "rep.container.H", g.H, 40)
}

func TestGridAutoFlowDenseBackfillsAGap(t *testing.T) {
	// Three 100px columns. A and B each span 2 columns (100px tall each, one
	// per row since neither fits beside the other): A occupies row 1's
	// columns 0-1, leaving column 2 free; B doesn't fit there (needs 2
	// columns) so it wraps to row 2's columns 0-1, leaving row 2's column 2
	// free too. C is a plain 1-column item placed after both in DOM order.
	//
	// Sparse (the default): C sees the auto-placement cursor sitting where B
	// left it (row 2, column 2) and is placed there directly — never looking
	// back at row 1's own leftover column 2.
	//
	// Dense: the cursor restarts from the very first cell for every item, so
	// C instead backfills row 1's column 2 — the gap A's own span left open
	// — landing ABOVE where sparse placed it, out of DOM order.
	src := `<html><body style="margin:0"><div style="display:grid;grid-template-columns:repeat(3,100px);grid-auto-flow:row">` +
		`<div style="grid-column:span 2">A</div><div style="grid-column:span 2">B</div><div>C</div></div></body></html>`
	sparse := findBox(layoutHTML(t, src, 400), "div")
	assertF(t, "dense.sparse.C.X", sparse.Children[2].X, 200)
	assertF(t, "dense.sparse.C.Y", sparse.Children[2].Y, 20) // row 2, alongside B

	denseSrc := `<html><body style="margin:0"><div style="display:grid;grid-template-columns:repeat(3,100px);grid-auto-flow:row dense">` +
		`<div style="grid-column:span 2">A</div><div style="grid-column:span 2">B</div><div>C</div></div></body></html>`
	dense := findBox(layoutHTML(t, denseSrc, 400), "div")
	assertF(t, "dense.dense.C.X", dense.Children[2].X, 200)
	assertF(t, "dense.dense.C.Y", dense.Children[2].Y, 0) // row 1, backfilling A's own leftover column
}

func TestGridMinmaxWithFr(t *testing.T) {
	// minmax(100px,1fr) 1fr over 300px: col0 base 100 (+100 fr share) = 200,
	// col1 = 100.
	src := `<html><body style="margin:0"><div style="display:grid;grid-template-columns:minmax(100px,1fr) 1fr">` +
		`<div>A</div><div>B</div></div></body></html>`
	g := findBox(layoutHTML(t, src, 300), "div")
	assertF(t, "mm.A.W", g.Children[0].W, 200)
	assertF(t, "mm.B.X", g.Children[1].X, 200)
	assertF(t, "mm.B.W", g.Children[1].W, 100)
}

func TestGridMinmaxFixedMaxClamps(t *testing.T) {
	// minmax(auto,150px): the content column cannot exceed 150 even though the
	// content ("xxxxxxxxxxxxxxxxxxxx" = 200px) is wider.
	src := `<html><body style="margin:0"><div style="display:grid;grid-template-columns:minmax(auto,150px) 1fr;width:400px">` +
		`<div>xxxxxxxxxxxxxxxxxxxx</div><div>B</div></div></body></html>`
	g := findBox(layoutHTML(t, src, 500), "div")
	assertF(t, "mmc.A.W", g.Children[0].W, 150)
	assertF(t, "mmc.B.X", g.Children[1].X, 150)
}

func TestGridMinmaxNonFrGrowsToFillFreeSpace(t *testing.T) {
	// minmax(0,800px) (no fr unit) between two 40px fixed columns, over a
	// 1024px grid: the CSS "Maximize Tracks" step must grow it to absorb the
	// 944px of leftover space, same as a browser — this is the shape of a
	// typical sidebar/content/sidebar layout (e.g. tailwindcss.com's own page
	// shell), and a track with no fr unit was previously never grown past its
	// base size, leaving the whole grid stuck at its 80px minimum and centred
	// in the middle of the page.
	src := `<html><body style="margin:0"><div style="display:grid;grid-template-columns:40px minmax(0,1536px) 40px;width:1024px">` +
		`<div>A</div><div>B</div><div>C</div></div></body></html>`
	g := findBox(layoutHTML(t, src, 1024), "div")
	assertF(t, "mmg.A.W", g.Children[0].W, 40)
	assertF(t, "mmg.B.X", g.Children[1].X, 40)
	assertF(t, "mmg.B.W", g.Children[1].W, 944)
	assertF(t, "mmg.C.X", g.Children[2].X, 984)
	assertF(t, "mmg.C.W", g.Children[2].W, 40)
}

func TestGridMinmaxNonFrRespectsCap(t *testing.T) {
	// Same shape, but the cap (300px) is narrower than the free space (944px):
	// the track grows only up to its cap, and the remaining free space is
	// simply left over (no fr track to absorb it).
	src := `<html><body style="margin:0"><div style="display:grid;grid-template-columns:40px minmax(0,300px) 40px;width:1024px">` +
		`<div>A</div><div>B</div><div>C</div></div></body></html>`
	g := findBox(layoutHTML(t, src, 1024), "div")
	assertF(t, "mmcap.B.W", g.Children[1].W, 300)
}

// ---- explicit placement + span --------------------------------------------

func TestGridExplicitColumnSpan(t *testing.T) {
	// A spans columns 1..3 (span 2 → 200px); B and C auto-flow around it.
	src := `<html><body style="margin:0"><div style="display:grid;grid-template-columns:repeat(3, 100px)">` +
		`<div style="grid-column:1 / span 2">A</div>` +
		`<div>B</div><div>C</div></div></body></html>`
	g := findBox(layoutHTML(t, src, 400), "div")
	byText := map[string]*Box{}
	for _, c := range g.Children {
		byText[boxText(c)] = c
	}
	assertF(t, "span.A.X", byText["A"].X, 0)
	assertF(t, "span.A.W", byText["A"].W, 200)
	assertF(t, "span.B.X", byText["B"].X, 200) // column 3
	assertF(t, "span.B.Y", byText["B"].Y, 0)
	assertF(t, "span.C.X", byText["C"].X, 0) // wraps to row 2
	assertF(t, "span.C.Y", byText["C"].Y, 20)
}

func TestGridExplicitLineNumbers(t *testing.T) {
	// grid-column:2 / 4 places the item in columns 2..3 (span 2).
	src := `<html><body style="margin:0"><div style="display:grid;grid-template-columns:repeat(4, 50px)">` +
		`<div style="grid-column:2 / 4;grid-row:1">A</div></div></body></html>`
	g := findBox(layoutHTML(t, src, 400), "div")
	assertF(t, "lines.A.X", g.Children[0].X, 50)  // column 2 starts at 50
	assertF(t, "lines.A.W", g.Children[0].W, 100) // columns 2+3
}

func TestGridRowSpan(t *testing.T) {
	// A spans two rows; B and C occupy the remaining cells of a 2-col grid.
	src := `<html><body style="margin:0"><div style="display:grid;grid-template-columns:repeat(2,100px);grid-template-rows:30px 40px">` +
		`<div style="grid-row:1 / span 2">A</div>` +
		`<div>B</div><div>C</div></div></body></html>`
	g := findBox(layoutHTML(t, src, 400), "div")
	byText := map[string]*Box{}
	for _, c := range g.Children {
		byText[boxText(c)] = c
	}
	assertF(t, "rspan.A.X", byText["A"].X, 0)
	assertF(t, "rspan.A.Y", byText["A"].Y, 0)
	assertF(t, "rspan.B.X", byText["B"].X, 100)
	assertF(t, "rspan.B.Y", byText["B"].Y, 0)
	assertF(t, "rspan.C.X", byText["C"].X, 100)
	assertF(t, "rspan.C.Y", byText["C"].Y, 30) // second row
}

func TestGridRowSpanFullNegativeLineReservesOccupancy(t *testing.T) {
	// `grid-row: 1 / -1` (Tailwind's row-span-full) in a 3-row explicit grid
	// must span all 3 rows and reserve column 0 in every row, exactly like a
	// browser. The row axis previously resolved a negative end line against
	// an unknown track count, silently collapsing the span to zero rows: the
	// item reserved no occupancy, so the next auto-placed item slid into
	// column 0 instead of column 1 (this is what left tailwindcss.com's main
	// content column stuck in its 40px decorative gutter track).
	src := `<html><body style="margin:0"><div style="display:grid;grid-template-columns:40px 100px;grid-template-rows:20px 20px 20px">` +
		`<div style="grid-row:1 / -1;grid-column:1">A</div>` +
		`<div>B</div></div></body></html>`
	g := findBox(layoutHTML(t, src, 400), "div")
	byText := map[string]*Box{}
	for _, c := range g.Children {
		byText[boxText(c)] = c
	}
	assertF(t, "rsfull.A.X", byText["A"].X, 0)
	assertF(t, "rsfull.A.W", byText["A"].W, 40)
	assertF(t, "rsfull.A.H", byText["A"].H, 60) // spans all 3 rows: 3*20px
	// B has no explicit placement; column 0 is occupied for every row by A,
	// so B must land in column 1, not overlap A in column 0.
	assertF(t, "rsfull.B.X", byText["B"].X, 40)
	assertF(t, "rsfull.B.Y", byText["B"].Y, 0)
}

// ---- row sizing ------------------------------------------------------------

func TestGridExplicitRowHeights(t *testing.T) {
	// grid-template-rows with fixed px heights positions row 2 below row 1.
	src := `<html><body style="margin:0"><div style="display:grid;grid-template-columns:100px;grid-template-rows:50px 30px">` +
		`<div>A</div><div>B</div></div></body></html>`
	g := findBox(layoutHTML(t, src, 300), "div")
	assertF(t, "rows.A.Y", g.Children[0].Y, 0)
	assertF(t, "rows.B.Y", g.Children[1].Y, 50)
	assertF(t, "rows.container.H", g.H, 80)
}

func TestGridRowGap(t *testing.T) {
	// row-gap separates the two auto rows by 10px.
	src := `<html><body style="margin:0"><div style="display:grid;grid-template-columns:100px;row-gap:10px">` +
		`<div>A</div><div>B</div></div></body></html>`
	g := findBox(layoutHTML(t, src, 300), "div")
	assertF(t, "rg.B.Y", g.Children[1].Y, 30) // 20 row + 10 gap
	assertF(t, "rg.container.H", g.H, 50)
}

// ---- alignment -------------------------------------------------------------

func TestGridJustifyAndAlignItems(t *testing.T) {
	// A 100x60 cell; a 40x20 item centred on both axes sits at (30,20).
	src := `<html><body style="margin:0"><div style="display:grid;grid-template-columns:100px;grid-template-rows:60px;justify-items:center;align-items:center">` +
		`<div style="width:40px;height:20px">A</div></div></body></html>`
	g := findBox(layoutHTML(t, src, 300), "div")
	a := g.Children[0]
	assertF(t, "ji.A.X", a.X, 30) // (100-40)/2
	assertF(t, "ji.A.Y", a.Y, 20) // (60-20)/2
	assertF(t, "ji.A.W", a.W, 40)
	assertF(t, "ji.A.H", a.H, 20)
}

func TestGridStretchDefault(t *testing.T) {
	// Default justify/align is stretch: the item fills the whole 100x50 cell.
	src := `<html><body style="margin:0"><div style="display:grid;grid-template-columns:100px;grid-template-rows:50px">` +
		`<div>A</div></div></body></html>`
	g := findBox(layoutHTML(t, src, 300), "div")
	a := g.Children[0]
	assertF(t, "st.A.W", a.W, 100)
	assertF(t, "st.A.H", a.H, 50)
}

func TestGridAlignSelfOverridesItems(t *testing.T) {
	// align-self:end on one item overrides the container's align-items:start.
	src := `<html><body style="margin:0"><div style="display:grid;grid-template-columns:100px;grid-template-rows:60px;align-items:start">` +
		`<div style="height:20px;align-self:end">A</div></div></body></html>`
	g := findBox(layoutHTML(t, src, 300), "div")
	assertF(t, "gas.A.Y", g.Children[0].Y, 40) // 60-20
}

// ---- template areas --------------------------------------------------------

func TestGridTemplateAreas(t *testing.T) {
	// A classic header/sidebar/main areas grid.
	src := `<html><body style="margin:0"><div style="display:grid;` +
		`grid-template-columns:100px 100px;` +
		`grid-template-areas:'h h' 's m'">` +
		`<div style="grid-area:h">H</div>` +
		`<div style="grid-area:s">S</div>` +
		`<div style="grid-area:m">M</div></div></body></html>`
	g := findBox(layoutHTML(t, src, 400), "div")
	byText := map[string]*Box{}
	for _, c := range g.Children {
		byText[boxText(c)] = c
	}
	assertF(t, "area.H.X", byText["H"].X, 0)
	assertF(t, "area.H.W", byText["H"].W, 200) // spans both columns
	assertF(t, "area.H.Y", byText["H"].Y, 0)
	assertF(t, "area.S.X", byText["S"].X, 0)
	assertF(t, "area.S.Y", byText["S"].Y, 20) // second row
	assertF(t, "area.M.X", byText["M"].X, 100)
	assertF(t, "area.M.Y", byText["M"].Y, 20)
}

// ---- implicit single column + auto rows -----------------------------------

func TestGridImplicitSingleColumn(t *testing.T) {
	// display:grid with no template columns → one column, items stacked in rows.
	src := `<html><body style="margin:0"><div style="display:grid;width:200px">` +
		`<div>A</div><div>B</div></div></body></html>`
	g := findBox(layoutHTML(t, src, 300), "div")
	assertF(t, "imp.A.Y", g.Children[0].Y, 0)
	assertF(t, "imp.B.Y", g.Children[1].Y, 20)
	assertF(t, "imp.A.W", g.Children[0].W, 200) // fills the single column
}

func TestGridEmpty(t *testing.T) {
	src := `<html><body style="margin:0"><div style="display:grid;grid-template-columns:1fr 1fr"></div></body></html>`
	g := findBox(layoutHTML(t, src, 300), "div")
	if len(g.Children) != 0 {
		t.Errorf("empty grid children = %d", len(g.Children))
	}
}

func TestGridJustifyContentCenter(t *testing.T) {
	// Two 80px fixed columns in a 300px grid, justify-content:center → the whole
	// track band (160px) is centred, leaving 70px on each side.
	src := `<html><body style="margin:0"><div style="display:grid;grid-template-columns:80px 80px;justify-content:center;width:300px">` +
		`<div>A</div><div>B</div></div></body></html>`
	g := findBox(layoutHTML(t, src, 400), "div")
	assertF(t, "jc.A.X", g.Children[0].X, 70)
	assertF(t, "jc.B.X", g.Children[1].X, 150)
}

// gridImgHTML parses src and returns a size map keyed by every <img> in
// document order, all sharing the one iw,ih intrinsic size — the multi-image
// sibling of layout_test.go's own single-image imgSizeHTML.
func gridImgHTML(t *testing.T, src string, iw, ih float64) (*dom.Node, css.StyleMap, map[*dom.Node][2]float64) {
	t.Helper()
	root, err := dom.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	sm := css.Cascade(root)
	sizes := map[*dom.Node][2]float64{}
	var walk func(n *dom.Node)
	walk = func(n *dom.Node) {
		if n.Type == dom.Element && n.Tag == "img" {
			sizes[n] = [2]float64{iw, ih}
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	return root, sm, sizes
}

// TestGridBareReplacedItemHeightOnlyPreservesAspectRatio is the confirmed
// real-world regression from issue #226 (a real A0 poster export through
// go-pdfkit/html2pdf): a bare `<img>` used DIRECTLY as a grid item (no
// wrapping block) with only `height` set stretched to fill its whole column
// at the source's raw intrinsic width, instead of deriving width from the
// intrinsic ratio per CSS 2.1 §10.3.2 rule 2 — because grid's own
// `stretchW` branch (placeGridItem) never calls layoutIsolated a second
// time for an auto-width, stretch-aligned item, and the FIRST (track-sizing)
// call to layoutIsolated unconditionally forced Width to the raw available
// column width regardless of the element's own (auto) width, discarding the
// "auto" signal resolvedReplacedSize needs to see. A 320×160 source in a
// 200px column with `height:40px` must come out 80×40, matching a real
// browser, not 200×40 (stretched) or 320×40 (raw intrinsic).
func TestGridBareReplacedItemHeightOnlyPreservesAspectRatio(t *testing.T) {
	root, sm, sizes := gridImgHTML(t, `<html><body style="margin:0">`+
		`<div style="display:grid;grid-template-columns:200px"><img style="height:40px"></div>`+
		`</body></html>`, 320, 160)
	box, _ := LayoutDocument(root, sm, 300, fakeMeasurer{}, sizes)
	img := findBox(box, "img")
	if img == nil || len(img.Lines) != 1 || len(img.Lines[0].Items) != 1 {
		t.Fatalf("expected one img box with one line item, got %v", img)
	}
	item := img.Lines[0].Items[0]
	assertF(t, "grid bare img height:40px item.Width (80*320/... derived)", item.Width, 80)
	assertF(t, "grid bare img height:40px item.LineHeight (explicit)", item.LineHeight, 40)
}

// TestGridBareReplacedItemPercentWidthNotDoubleResolved guards a regression
// introduced and caught WITHIN the same fix as the test above: a grid item
// with an explicit, non-auto percentage width (e.g. `width:50%`) is resolved
// against the column width ONCE already, by grid's own itemNaturalWidth,
// before layoutIsolated is called again to finalise it — layoutIsolated must
// NOT re-resolve that percentage a second time against the already-resolved
// target width it receives (50% of 200 = 100, then wrongly 50% of 100 = 50).
// A 320×160 source in a 200px column with `width:50%` must come out 100×50.
func TestGridBareReplacedItemPercentWidthNotDoubleResolved(t *testing.T) {
	root, sm, sizes := gridImgHTML(t, `<html><body style="margin:0">`+
		`<div style="display:grid;grid-template-columns:200px"><img style="width:50%"></div>`+
		`</body></html>`, 320, 160)
	box, _ := LayoutDocument(root, sm, 300, fakeMeasurer{}, sizes)
	img := findBox(box, "img")
	if img == nil || len(img.Lines) != 1 || len(img.Lines[0].Items) != 1 {
		t.Fatalf("expected one img box with one line item, got %v", img)
	}
	item := img.Lines[0].Items[0]
	assertF(t, "grid bare img width:50% item.Width (not double-resolved)", item.Width, 100)
	assertF(t, "grid bare img width:50% item.LineHeight (ratio-derived)", item.LineHeight, 50)
}
