// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package paginate

import (
	"testing"
	"time"

	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/dom"
	"github.com/go-webengine/engine/layout"
	"github.com/go-webengine/engine/paint"
)

// box builds a synthetic block with n lines of height lh starting at y.
func box(st *css.Style, y, h float64, lines int, lh float64) *layout.Box {
	b := &layout.Box{Style: st, Y: y, H: h}
	for i := 0; i < lines; i++ {
		b.Lines = append(b.Lines, &layout.LineBox{Y: y + float64(i)*lh, H: lh})
	}
	return b
}

func root(children ...*layout.Box) *layout.Box {
	r := &layout.Box{Style: &css.Style{}}
	r.Children = children
	return r
}

func TestPlainFillCutsBetweenLines(t *testing.T) {
	// 10 lines of 10px, pages of 35px: 3 lines a page.
	r := root(box(&css.Style{Orphans: 1, Widows: 1}, 0, 100, 10, 10))
	got := Breaks(r, 35)
	want := []float64{30, 60, 90}
	if !equal(got, want) {
		t.Fatalf("breaks %v, want %v", got, want)
	}
}

func TestForcedBreakCutsAShortPage(t *testing.T) {
	a := box(&css.Style{}, 0, 10, 1, 10)
	b := box(&css.Style{BreakBefore: css.BreakPage}, 10, 10, 1, 10)
	c := box(&css.Style{BreakAfter: css.BreakPage}, 20, 10, 1, 10)
	d := box(&css.Style{}, 30, 10, 1, 10)
	got := Breaks(root(a, b, c, d), 1000)
	if want := []float64{10, 30}; !equal(got, want) {
		t.Fatalf("breaks %v, want %v (before b, after c)", got, want)
	}
}

func TestAvoidInsideMovesTheBoxWholeWhenItFits(t *testing.T) {
	// A 30px table starting at y=20 on 40px pages: it would straddle 40, so
	// the page ends before it.
	p := box(&css.Style{}, 0, 20, 2, 10)
	tbl := box(&css.Style{BreakInside: css.BreakInsideAvoid}, 20, 30, 3, 10)
	got := Breaks(root(p, tbl), 40)
	if want := []float64{20}; !equal(got, want) {
		t.Fatalf("breaks %v, want %v", got, want)
	}
}

func TestAvoidInsideTallerThanAPageIsCutAnyway(t *testing.T) {
	tbl := box(&css.Style{BreakInside: css.BreakInsideAvoid, Orphans: 1, Widows: 1}, 0, 100, 10, 10)
	got := Breaks(root(tbl), 40)
	if want := []float64{40, 80}; !equal(got, want) {
		t.Fatalf("breaks %v, want %v", got, want)
	}
}

func TestKeepWithNextCarriesTheHeadingOver(t *testing.T) {
	// Heading fits at the bottom of page 1, its paragraph does not: the cut
	// moves before the heading.
	filler := box(&css.Style{Orphans: 1, Widows: 1}, 0, 30, 3, 10)
	h := box(&css.Style{BreakAfter: css.BreakAvoid}, 30, 10, 1, 10)
	p := box(&css.Style{}, 40, 10, 1, 10)
	got := Breaks(root(filler, h, p), 40)
	if want := []float64{30}; !equal(got, want) {
		t.Fatalf("breaks %v, want %v", got, want)
	}
	// And break-before: avoid on the paragraph says the same thing.
	h2 := box(&css.Style{}, 30, 10, 1, 10)
	p2 := box(&css.Style{BreakBefore: css.BreakAvoid}, 40, 10, 1, 10)
	if got := Breaks(root(filler, h2, p2), 40); !equal(got, []float64{30}) {
		t.Fatalf("break-before: avoid: breaks %v, want [30]", got)
	}
}

func TestOrphansAndWidows(t *testing.T) {
	// 8 lines of 10px, pages of 55px: the plain cut would leave 5 + 3;
	// orphans/widows 3 allow that; orphans 4 widows 4 force 4 + 4; a block
	// that cannot satisfy both (orphans 5, widows 5) is cut anyway, at the
	// plain place, once the rule is relaxed.
	cases := []struct {
		orphans, widows int
		want            []float64
	}{
		{3, 3, []float64{50}},
		{4, 4, []float64{40}},
		{5, 5, []float64{50}},
	}
	for _, c := range cases {
		r := root(box(&css.Style{Orphans: c.orphans, Widows: c.widows}, 0, 80, 8, 10))
		if got := Breaks(r, 55); !equal(got, c.want) {
			t.Errorf("orphans %d widows %d: breaks %v, want %v", c.orphans, c.widows, got, c.want)
		}
	}
}

func TestTallAtomOverflowsItsOwnPage(t *testing.T) {
	// A 100px row on 40px pages: one break before it, one after it.
	p := box(&css.Style{}, 0, 10, 1, 10)
	row := &layout.Box{Node: &dom.Node{Type: dom.Element, Tag: "tr"}, Style: &css.Style{}, Y: 10, H: 100}
	q := box(&css.Style{}, 110, 10, 1, 10)
	got := Breaks(root(p, row, q), 40)
	if want := []float64{10, 110}; !equal(got, want) {
		t.Fatalf("breaks %v, want %v", got, want)
	}
}

func TestFirstPageHeight(t *testing.T) {
	r := root(box(&css.Style{Orphans: 1, Widows: 1}, 0, 100, 10, 10))
	got := Paginate(r, Options{PageHeight: 50, FirstPageHeight: 20})
	if want := []float64{20, 70}; !equal(got, want) {
		t.Fatalf("breaks %v, want %v", got, want)
	}
}

func TestNilAndEmpty(t *testing.T) {
	if Breaks(nil, 100) != nil || Breaks(root(), 100) != nil || Breaks(root(box(&css.Style{}, 0, 10, 1, 10)), 0) != nil {
		t.Fatal("nil root, empty root or no page height must give no breaks")
	}
	if got := Breaks(root(nil, box(&css.Style{}, 0, 10, 1, 10)), 100); got != nil {
		t.Fatalf("a nil child is skipped: breaks %v", got)
	}
}

// A chain of break-after: avoid longer than a page cannot be honoured: the
// avoid rules are dropped and the page is filled.
func TestAvoidChainLongerThanAPageIsCut(t *testing.T) {
	keep := &css.Style{BreakAfter: css.BreakAvoid}
	r := root(box(keep, 0, 10, 1, 10), box(keep, 10, 10, 1, 10), box(keep, 20, 10, 1, 10), box(keep, 30, 10, 1, 10), box(&css.Style{}, 40, 10, 1, 10))
	if got := Breaks(r, 25); !equal(got, []float64{20, 40}) {
		t.Fatalf("breaks %v, want [20 40]", got)
	}
}

// The last atom alone overflowing a page ends the document without a
// break after it.
func TestTallLastAtomEndsTheDocument(t *testing.T) {
	p := box(&css.Style{}, 0, 10, 1, 10)
	row := &layout.Box{Node: &dom.Node{Type: dom.Element, Tag: "tr"}, Style: &css.Style{}, Y: 10, H: 100}
	if got := Breaks(root(p, row), 40); !equal(got, []float64{10}) {
		t.Fatalf("breaks %v, want [10]", got)
	}
	// And break-after: left / right are forced breaks like page.
	a := box(&css.Style{BreakAfter: css.BreakLeft}, 0, 10, 1, 10)
	b := box(&css.Style{BreakBefore: css.BreakRight}, 10, 10, 1, 10)
	c := box(&css.Style{}, 20, 10, 1, 10)
	if got := Breaks(root(a, b, c), 1000); !equal(got, []float64{10}) {
		t.Fatalf("left/right: breaks %v, want [10]", got)
	}
}

// The answer key: the fixture go-pdfkit/html2pdf prints with Chrome
// (corpus/fixtures/breaks.html, Chrome 141 headless, A5, 15 mm margins —
// breaks.expected.tsv). Sections force pages; a table and a figure with
// break-inside: avoid straddling a page end move whole; an h2 with
// break-after: avoid follows its paragraph; an 8-line paragraph with
// orphans/widows 3 is cut with at least three lines on each side (Chrome
// cuts it 4 + 4; the exact split depends on the font, the constraint does
// not). Laid out at the A5 page area, 118 × 180 mm at 96 dpi.
const answerKey = `<!doctype html><html><head><style>
  body { font-family: sans-serif; font-size: 12pt; line-height: 1.4; margin: 0; }
  h1 { break-before: page; font-size: 20pt; margin: 0 0 8pt; }
  h1.first { break-before: auto; }
  h2 { break-after: avoid; font-size: 14pt; margin: 14pt 0 4pt; }
  table { border-collapse: collapse; break-inside: avoid; width: 100%; }
  td { border: 1px solid #888; padding: 3pt 6pt; }
  figure { break-inside: avoid; margin: 8pt 0; padding: 6pt; border: 1px solid #333; }
  p { margin: 0 0 8pt; }
  p.ow { orphans: 3; widows: 3; }
  .spacer { height: 40mm; background: #eee; }
</style></head><body>
<h1 class="first">Section 1: forced breaks</h1>
<p>P1-A. Each h1 below has break-before: page, so every section starts a new page.</p>
<p id="p1b">P1-B. This paragraph stays on page 1.</p>
<h1 id="s2">Section 2: keep a table whole</h1>
<p>P2-A. A spacer pushes the table so that it would straddle the page end; break-inside: avoid must move the whole table to the next page.</p>
<div class="spacer"></div><div class="spacer"></div><div class="spacer"></div>
<table>
<tr id="t1"><td>T-row-1</td><td>one</td></tr><tr><td>T-row-2</td><td>two</td></tr><tr><td>T-row-3</td><td>three</td></tr><tr><td>T-row-4</td><td>four</td></tr>
<tr><td>T-row-5</td><td>five</td></tr><tr><td>T-row-6</td><td>six</td></tr><tr><td>T-row-7</td><td>seven</td></tr><tr id="t8"><td>T-row-8</td><td>eight</td></tr>
</table>
<p id="p2b">P2-B. After the table.</p>
<h1 id="s3">Section 3: heading kept with its paragraph</h1>
<p>P3-A. Spacers leave room for exactly the heading at the page bottom; break-after: avoid must carry it to the next page with its paragraph.</p>
<div class="spacer"></div><div class="spacer"></div><div class="spacer"></div><div class="spacer"></div>
<h2 id="h2">H2-keep-with-next</h2>
<p id="p3b">P3-B. The paragraph that follows the heading.</p>
<h1 id="s4">Section 4: orphans and widows</h1>
<p id="p4a">P4-A. Three spacers leave room for about seven of the eight lines below; with orphans and widows at 3 the paragraph must split with at least three lines on each side, or move whole.</p>
<div class="spacer"></div><div class="spacer"></div><div class="spacer"></div>
<p class="ow"><span id="l1">L1 line one</span><br><span id="l2">L2 line two</span><br><span id="l3">L3 line three</span><br><span id="l4">L4 line four</span><br><span id="l5">L5 line five</span><br><span id="l6">L6 line six</span><br><span id="l7">L7 line seven</span><br><span id="l8">L8 line eight</span></p>
<p id="p4b">P4-B. After the eight-line paragraph.</p>
<h1 id="s5">Section 5: figure kept whole</h1>
<div class="spacer"></div><div class="spacer"></div><div class="spacer"></div><div class="spacer"></div>
<figure><span id="f1">F-1 figure line one</span><br>F-2 figure line two<br>F-3 figure line three<br>F-4 figure line four<br><span id="f5">F-5 figure line five</span></figure>
<p id="p5b">P5-B. After the figure.</p>
</body></html>`

func TestAnswerKeyFromChrome(t *testing.T) {
	const mm = 96.0 / 25.4
	rootNode, err := dom.Parse(answerKey)
	if err != nil {
		t.Fatal(err)
	}
	sm := css.CascadeMedia(rootNode, css.Media{Type: css.Print, Width: 118 * mm}, nil)
	box, _ := layout.LayoutDocument(rootNode, sm, 118*mm, paint.NewFonts(), nil)
	tops := Breaks(box, 180*mm)
	pageOf := func(id string) int {
		y, ok := topOf(box, id)
		if !ok {
			t.Fatalf("no element with id %q", id)
		}
		p := 1
		for _, top := range tops {
			if y >= top-epsilon {
				p++
			}
		}
		return p
	}
	want := map[string]int{
		"p1b": 1, "s2": 2, "t1": 3, "t8": 3, "p2b": 3, "s3": 4, "h2": 5, "p3b": 5,
		"s4": 6, "p4a": 6, "l1": 6, "l2": 6, "l3": 6, "l6": 7, "l7": 7, "l8": 7, "p4b": 7,
		"s5": 8, "f1": 9, "f5": 9, "p5b": 9,
	}
	for id, p := range want {
		if got := pageOf(id); got != p {
			t.Errorf("%s: page %d, want %d (Chrome)", id, got, p)
		}
	}
	if len(tops)+1 != 9 {
		t.Errorf("pages: %d, want 9 (Chrome)", len(tops)+1)
	}
	// The split of the orphans paragraph: three lines at least on each side.
	if pageOf("l4") == pageOf("l5") && pageOf("l4") != 6 && pageOf("l4") != 7 {
		t.Errorf("l4/l5 on page %d", pageOf("l4"))
	}
}

// topOf finds the y of the box or first line of the element with id.
func topOf(b *layout.Box, id string) (float64, bool) {
	if b == nil {
		return 0, false
	}
	if b.Node != nil && b.Node.ID() == id {
		return b.Y, true
	}
	for _, ln := range b.Lines {
		for _, it := range ln.Items {
			for n := it.Node; n != nil; n = n.Parent {
				if n.ID() == id {
					return ln.Y, true
				}
			}
		}
	}
	for _, c := range b.Children {
		if y, ok := topOf(c, id); ok {
			return y, true
		}
	}
	return 0, false
}

func equal(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i]-b[i] > epsilon || b[i]-a[i] > epsilon {
			return false
		}
	}
	return true
}

// A layout-table wrapper row (a <tr> whose cell holds a nested table) is
// descended into so the nested rows are the atoms; a plain row is one atom
// however many lines its cells wrap to.
func TestWrapperRowIsDescendedIntoPlainRowIsOneAtom(t *testing.T) {
	tr := func(y, h float64, kids ...*layout.Box) *layout.Box {
		return &layout.Box{Node: &dom.Node{Type: dom.Element, Tag: "tr"}, Style: &css.Style{}, Y: y, H: h, Children: kids}
	}
	td := func(y, h float64, kids ...*layout.Box) *layout.Box {
		return &layout.Box{Node: &dom.Node{Type: dom.Element, Tag: "td"}, Style: &css.Style{}, Y: y, H: h, Children: kids}
	}
	// wrapper row holding three 10px rows
	inner := []*layout.Box{tr(0, 10), tr(10, 10), tr(20, 10)}
	wrapper := tr(0, 30, td(0, 30, &layout.Box{Style: &css.Style{}, Y: 0, H: 30, Children: inner}))
	if got := Breaks(root(wrapper), 15); !equal(got, []float64{10, 20}) {
		t.Errorf("wrapper row: breaks %v, want [10 20] (cut between the nested rows)", got)
	}
	// plain row with two wrapped lines in a cell: one atom, never cut inside
	plain := tr(0, 20, td(0, 20, box(&css.Style{}, 0, 20, 2, 10)))
	next := box(&css.Style{}, 20, 10, 1, 10)
	if got := Breaks(root(plain, next), 15); !equal(got, []float64{20}) {
		t.Errorf("plain row: breaks %v, want [20] (the row overflows whole, the cut is after it)", got)
	}
}

// Ten thousand rows each asking not to be cut inside (a large table whose
// print stylesheet sets break-inside: avoid on tr, as Wikipedia's does) must
// paginate in well under a second: the blocked boundaries are computed once.
func TestManyAvoidGroupsPaginateFast(t *testing.T) {
	const rows = 10000
	kids := make([]*layout.Box, rows)
	for i := range kids {
		kids[i] = &layout.Box{Node: &dom.Node{Type: dom.Element, Tag: "tr"}, Style: &css.Style{BreakInside: css.BreakInsideAvoid}, Y: float64(i) * 20, H: 20}
	}
	r := root(&layout.Box{Style: &css.Style{}, Y: 0, H: rows * 20, Children: kids})
	start := time.Now()
	got := Breaks(r, 1000)
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("pagination took %v", d)
	}
	if len(got) != rows*20/1000-1 {
		t.Fatalf("breaks: %d, want %d", len(got), rows*20/1000-1)
	}
	// A group that straddles the page end with room to move: the cut lands
	// before it, and a group taller than a page is cut through.
	tall := &layout.Box{Style: &css.Style{BreakInside: css.BreakInsideAvoid, Orphans: 1, Widows: 1}, Y: 0, H: 3000}
	for i := 0; i < 150; i++ {
		tall.Lines = append(tall.Lines, &layout.LineBox{Y: float64(i) * 20, H: 20})
	}
	if got := Breaks(root(tall), 1000); len(got) != 2 {
		t.Fatalf("tall avoid box: breaks %v, want two plain cuts", got)
	}
}
