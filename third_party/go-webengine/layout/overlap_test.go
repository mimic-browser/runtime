// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package layout

import (
	"testing"

	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/dom"
)

// sizeMeasurer is a font-size-aware fake: advances and vertical metrics scale
// with the requested size, so a line mixing font sizes exercises the real
// mixed-metric vertical-advance path (unlike fakeMeasurer, whose metrics are
// constant). Each rune advances 0.5×size; ascent is 0.8×size and the natural
// line height is 1.0×size.
type sizeMeasurer struct{}

func (sizeMeasurer) Measure(text string, _ css.FontFamily, sizePx float64, _ int, _ bool) float64 {
	return float64(len([]rune(text))) * 0.5 * sizePx
}
func (sizeMeasurer) Metrics(_ css.FontFamily, sizePx float64, _ int, _ bool) (float64, float64) {
	return 0.8 * sizePx, sizePx
}

func layoutHTMLWith(t *testing.T, m Measurer, src string, vpW float64) *Box {
	t.Helper()
	root, err := dom.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	sm := css.Cascade(root)
	box, _ := LayoutDocument(root, sm, vpW, m, nil)
	return box
}

// docLines returns every line box in the tree in document order (a box's own
// inline lines precede its block children's lines), each paired with its item
// glyph extent so callers can assert both line-box and per-item non-overlap.
func docLines(b *Box) []*LineBox {
	if b == nil {
		return nil
	}
	var out []*LineBox
	out = append(out, b.Lines...)
	for _, c := range b.Children {
		out = append(out, docLines(c)...)
	}
	return out
}

// assertNoLineOverlap asserts that consecutive line boxes never overlap
// vertically (each line's top is at or below the previous line's bottom) AND
// that every inline item stays within its own line box (its glyph extent, top
// to top+lineHeight, does not spill past the line-box bottom into the next
// line). This is the precise geometric statement of "text does not overlap".
func assertNoLineOverlap(t *testing.T, lines []*LineBox) {
	t.Helper()
	const eps = 1e-6
	prevBottom := -1e18
	for li, ln := range lines {
		if ln.Y < prevBottom-eps {
			t.Errorf("line %d top %.4f overlaps previous bottom %.4f (by %.4f)",
				li, ln.Y, prevBottom, prevBottom-ln.Y)
		}
		lineBottom := ln.Y + ln.H
		for _, it := range ln.Items {
			if bottom := it.Y + it.LineHeight; bottom > lineBottom+eps {
				t.Errorf("line %d item %q glyph bottom %.4f spills past line bottom %.4f",
					li, it.Text, bottom, lineBottom)
			}
			if it.Y < ln.Y-eps {
				t.Errorf("line %d item %q top %.4f is above line top %.4f",
					li, it.Text, it.Y, ln.Y)
			}
		}
		prevBottom = lineBottom
	}
}

// TestLineMetricsMixedIndependentMaxima is the tightest unit test of the fix:
// the tallest ascent and the deepest descent come from DIFFERENT items, so a
// naive max(item.LineHeight) would under-size the line box and let a glyph
// spill. The line height must be maxAscent + maxDescentBelow.
func TestLineMetricsMixedIndependentMaxima(t *testing.T) {
	// A: tall ascent, shallow descent. B: short ascent, deep descent.
	a := &InlineItem{Ascent: 30, LineHeight: 35, Width: 10} // below = 5
	b := &InlineItem{Ascent: 10, LineHeight: 40, Width: 10} // below = 30
	line := &LineBox{Items: []*InlineItem{a, b}}
	lineH, baseline, used := lineMetrics(line, 20, 8)
	if baseline != 30 {
		t.Errorf("baseline = %v, want 30 (max ascent)", baseline)
	}
	// max ascent 30 + max below 30 = 60; a naive max(LineHeight) would give 40.
	if lineH != 60 {
		t.Errorf("lineH = %v, want 60 (30 ascent + 30 below); naive max would be 40", lineH)
	}
	if used != 20 {
		t.Errorf("used = %v, want 20", used)
	}
	// Verify every item, positioned on the shared baseline, fits inside [0,lineH].
	for _, it := range line.Items {
		top := baseline - it.Ascent // item.Y offset within the line box
		if top < 0 {
			t.Errorf("item top offset %v < 0", top)
		}
		if bottom := top + it.LineHeight; bottom > lineH {
			t.Errorf("item bottom %v spills past lineH %v", bottom, lineH)
		}
	}
}

// TestLineMetricsUniformUnchanged guards fidelity: for a single-font line the
// new formula must give exactly the old max(LineHeight) result (no regression
// in the overwhelmingly common case).
func TestLineMetricsUniformUnchanged(t *testing.T) {
	items := []*InlineItem{
		{Ascent: 8, LineHeight: 20, Width: 10},
		{Ascent: 8, LineHeight: 20, Width: 10, SpaceBefore: 5},
	}
	lineH, baseline, used := lineMetrics(&LineBox{Items: items}, 20, 8)
	if lineH != 20 || baseline != 8 || used != 25 {
		t.Errorf("uniform lineMetrics = %v,%v,%v want 20,8,25", lineH, baseline, used)
	}
}

// TestInlineMixedFontSizesNonOverlap lays out a paragraph mixing a large inline
// span with normal body text under a unitless line-height, and asserts no line
// or glyph overlaps — the end-to-end regression for the reported bug.
func TestInlineMixedFontSizesNonOverlap(t *testing.T) {
	src := `<html><body style="margin:0;font-size:16px;line-height:1.5">` +
		`<p style="margin:0">normal words here and a ` +
		`<span style="font-size:40px">HUGE inline span</span> then more normal words ` +
		`that wrap onto several lines at this narrow width to be sure</p></body></html>`
	root := layoutHTMLWith(t, sizeMeasurer{}, src, 240)
	lines := docLines(findBox(root, "p"))
	if len(lines) < 3 {
		t.Fatalf("expected the paragraph to wrap to several lines, got %d", len(lines))
	}
	assertNoLineOverlap(t, lines)

	// The line carrying the 40px span must be tall enough for it: line-height
	// 1.5×40 = 60. Find the line that contains a 40px item.
	found := false
	for _, ln := range lines {
		for _, it := range ln.Items {
			if it.Style.FontSize == 40 {
				found = true
				if ln.H < 60-1e-6 {
					t.Errorf("line with 40px span has H=%.4f, want >= 60 (1.5×40)", ln.H)
				}
			}
		}
	}
	if !found {
		t.Fatal("did not find the 40px span item on any line")
	}
}

// TestMultiParagraphAndHeadingsMonotonic asserts block-level vertical flow stays
// monotonic across headings and paragraphs of different sizes, and that every
// line across the whole document is non-overlapping in document order.
func TestMultiParagraphAndHeadingsMonotonic(t *testing.T) {
	src := `<html><body style="margin:0;line-height:1.5">` +
		`<h1 style="margin:0;font-size:40px">Big heading that wraps across the narrow width here</h1>` +
		`<p style="margin:0;font-size:16px">First paragraph body text that also wraps several times across the width</p>` +
		`<h2 style="margin:0;font-size:26px">Medium heading also wrapping over width</h2>` +
		`<p style="margin:0;font-size:16px">Second paragraph with more wrapping body text to fill out lines nicely</p>` +
		`</body></html>`
	root := layoutHTMLWith(t, sizeMeasurer{}, src, 200)
	lines := docLines(findBox(root, "body"))
	if len(lines) < 6 {
		t.Fatalf("expected many wrapped lines, got %d", len(lines))
	}
	assertNoLineOverlap(t, lines)

	// Block boxes themselves stack monotonically (each child top >= previous
	// child bottom).
	body := findBox(root, "body")
	prevBottom := body.ContentY
	for i, c := range body.Children {
		if c.Y < prevBottom-1e-6 {
			t.Errorf("block child %d top %.4f overlaps previous bottom %.4f", i, c.Y, prevBottom)
		}
		prevBottom = c.Y + c.H
	}
}

// TestTightLineHeightHalfLeading confirms a sub-natural (tight) unitless
// line-height still resolves per-element and produces a line box smaller than
// the font's natural height (matching browsers) without the layout silently
// forcing it back up — i.e. we honour CSS rather than papering over overlap.
func TestTightLineHeightHalfLeading(t *testing.T) {
	src := `<html><body style="margin:0;font-size:20px;line-height:0.8">` +
		`<p style="margin:0">tight leading text</p></body></html>`
	root := layoutHTMLWith(t, sizeMeasurer{}, src, 400)
	p := findBox(root, "p")
	if len(p.Lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(p.Lines))
	}
	// natural height is 1.0×20 = 20; tight line-height 0.8×20 = 16 < 20.
	if got := p.Lines[0].H; got != 16 {
		t.Errorf("tight line height = %.4f, want 16 (0.8×20)", got)
	}
}
