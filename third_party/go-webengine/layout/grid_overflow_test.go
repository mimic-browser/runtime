// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package layout

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/dom"
)

// TestGridAutoTrackNoOverflowFixture is the regression for the CSS-Grid
// auto-track blowout: an `auto` column sized to its max-content (the whole
// unwrapped text) must be shrunk back to the grid's definite width so the text
// wraps, instead of overflowing the grid to the right and painting off-screen.
func TestGridAutoTrackNoOverflowFixture(t *testing.T) {
	const vpW = 900
	data, err := os.ReadFile(filepath.Join("..", "testdata", "grid_auto_track_overflow.html"))
	if err != nil {
		t.Fatal(err)
	}
	root, err := dom.Parse(string(data))
	if err != nil {
		t.Fatal(err)
	}
	sm := css.Cascade(root)
	box, _ := LayoutDocument(root, sm, vpW, fakeMeasurer{}, nil)

	grid := findBox(box, "div")
	if grid == nil {
		t.Fatal("no grid box")
	}
	// The grid has an explicit 400px width; its auto column must not exceed it.
	if grid.W > 400.5 {
		t.Fatalf("grid width %.1f, want 400", grid.W)
	}
	p := findBox(box, "p")
	if p == nil {
		t.Fatal("no paragraph box")
	}
	// The paragraph (the grid item) must fit inside the grid, not blow out to
	// max-content (~2500px for this text under the fake measurer).
	if p.W > grid.ContentW+0.5 {
		t.Errorf("grid item width %.1f exceeds grid content width %.1f (auto track overflow)",
			p.W, grid.ContentW)
	}
	// It must therefore have wrapped to several lines...
	if len(p.Lines) < 2 {
		t.Errorf("expected the paragraph to wrap (auto track shrank to fit), got %d line(s)", len(p.Lines))
	}
	// ...and every glyph must stay within the grid's content box (on-screen).
	right := grid.ContentX + grid.ContentW
	for _, ln := range p.Lines {
		for _, it := range ln.Items {
			if it.X+it.Width > right+0.5 {
				t.Errorf("text %q at x=%.1f-%.1f spills past grid right edge %.1f",
					it.Text, it.X, it.X+it.Width, right)
			}
		}
	}
}

// TestShrinkAutoTracksUnit exercises shrinkAutoTracks directly: an overflowing
// pair of auto tracks shrinks proportionally to fit; fixed tracks are untouched;
// a non-overflowing set is left exactly as-is.
func TestShrinkAutoTracksUnit(t *testing.T) {
	// Two auto tracks (300+500=800) plus a 100 fixed track, 20 gap, axis 400.
	base := []float64{300, 500, 100}
	auto := []bool{true, true, false}
	shrinkAutoTracks(base, auto, 400, 20, 3)
	// total was 900+40 gap = 940; excess 540; autoSum 800; scale=(800-540)/800.
	if base[2] != 100 {
		t.Errorf("fixed track changed: %.2f", base[2])
	}
	total := base[0] + base[1] + base[2] + 40
	if total > 400.5 || total < 399.5 {
		t.Errorf("tracks did not shrink to fit axis: total=%.2f want 400", total)
	}
	if !(base[0] < base[1]) { // proportional: smaller stays smaller
		t.Errorf("shrink not proportional: %.2f %.2f", base[0], base[1])
	}

	// No overflow → untouched.
	b2 := []float64{100, 100}
	shrinkAutoTracks(b2, []bool{true, true}, 400, 0, 2)
	if b2[0] != 100 || b2[1] != 100 {
		t.Errorf("non-overflow tracks changed: %v", b2)
	}

	// Fixed tracks alone exceed the axis → nothing shrinkable, left overflowing.
	b3 := []float64{500}
	shrinkAutoTracks(b3, []bool{false}, 400, 0, 1)
	if b3[0] != 500 {
		t.Errorf("fixed overflow track changed: %.2f", b3[0])
	}

	// Overflow larger than the whole auto sum (a fixed track dwarfs the axis):
	// the auto tracks floor at 0 rather than going negative.
	b4 := []float64{100, 100, 900}
	shrinkAutoTracks(b4, []bool{true, true, false}, 400, 0, 3)
	if b4[0] != 0 || b4[1] != 0 {
		t.Errorf("auto tracks should floor at 0 when excess exceeds auto sum: %v", b4)
	}
	if b4[2] != 900 {
		t.Errorf("fixed track changed: %.2f", b4[2])
	}
}
