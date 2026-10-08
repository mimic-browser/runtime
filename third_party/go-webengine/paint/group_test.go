// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package paint

import (
	"image"
	"image/color"
	"testing"

	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/layout"
)

// TestSubtreeExtentNil covers the defensive nil case (a marker/child slot that
// happens to be unset).
func TestSubtreeExtentNil(t *testing.T) {
	if got := subtreeExtent(nil); got != (image.Rectangle{}) {
		t.Errorf("subtreeExtent(nil) = %v, want zero rect", got)
	}
}

// TestSubtreeExtentOwnBox covers the base case: a leaf box's extent is exactly
// its own border box.
func TestSubtreeExtentOwnBox(t *testing.T) {
	box := &layout.Box{X: 10, Y: 20, W: 30, H: 40}
	want := image.Rect(10, 20, 40, 60)
	if got := subtreeExtent(box); got != want {
		t.Errorf("subtreeExtent = %v, want %v", got, want)
	}
}

// TestSubtreeExtentMarker covers a list-item marker sitting outside the box's
// own content box (in the indent to its left).
func TestSubtreeExtentMarker(t *testing.T) {
	box := &layout.Box{X: 20, Y: 20, W: 30, H: 20, Marker: &layout.Marker{X: 5, Y: 22, W: 10, H: 10}}
	got := subtreeExtent(box)
	want := image.Rect(5, 20, 50, 40) // union of box (20,20,50,40) and marker (5,22,15,32)
	if got != want {
		t.Errorf("subtreeExtent = %v, want %v", got, want)
	}
}

// TestSubtreeExtentLines covers a line box (e.g. text with tall glyphs) whose
// bounds exceed the containing box's own H (an under-sized auto-height guess).
func TestSubtreeExtentLines(t *testing.T) {
	box := &layout.Box{X: 0, Y: 0, W: 100, H: 10, Lines: []*layout.LineBox{
		{X: 0, Y: 0, W: 100, H: 25},
	}}
	got := subtreeExtent(box)
	want := image.Rect(0, 0, 100, 25)
	if got != want {
		t.Errorf("subtreeExtent = %v, want %v", got, want)
	}
}

// TestSubtreeExtentChildrenEscapeParent covers the case the doc comment calls
// out by name: a child (e.g. a shrink-wrapped float, or one shifted by a
// negative margin) that extends outside its parent's own reported W/H. The
// parent here is genuinely zero-tall (H:0, the collapsed-auto-height case),
// so it contributes no ink of its own — image.Rectangle.Union treats an empty
// operand as contributing nothing, which is exactly right: a zero-height box
// paints no background/border/shadow of its own (paintBoxContent's `drawable`
// gate requires W>0 && H>0), so only the children's extents should count.
func TestSubtreeExtentChildrenEscapeParent(t *testing.T) {
	parent := &layout.Box{X: 0, Y: 0, W: 50, H: 0, Children: []*layout.Box{
		{X: -10, Y: 5, W: 20, H: 20}, // escapes left of the parent
		{X: 40, Y: 5, W: 30, H: 20},  // escapes below/right of the parent
	}}
	got := subtreeExtent(parent)
	want := image.Rect(-10, 5, 70, 25) // union of the two children only
	if got != want {
		t.Errorf("subtreeExtent = %v, want %v", got, want)
	}
}

// TestShadowExpandedRectNoStyle covers a box with no Style at all (an
// anonymous box), which must not panic and must return its own border box.
func TestShadowExpandedRectNoStyle(t *testing.T) {
	box := &layout.Box{X: 1, Y: 2, W: 3, H: 4}
	if got, want := shadowExpandedRect(box), image.Rect(1, 2, 4, 6); got != want {
		t.Errorf("shadowExpandedRect = %v, want %v", got, want)
	}
}

// TestShadowExpandedRectInsetIgnored covers that an inset box-shadow (painted
// INSIDE the box) contributes no outward spread.
func TestShadowExpandedRectInsetIgnored(t *testing.T) {
	box := &layout.Box{X: 0, Y: 0, W: 10, H: 10, Style: &css.Style{
		BoxShadows: []css.BoxShadow{{OffsetX: 50, OffsetY: 50, Blur: 20, Spread: 20, Inset: true, Color: css.Color{A: 255}}},
	}}
	if got, want := shadowExpandedRect(box), image.Rect(0, 0, 10, 10); got != want {
		t.Errorf("shadowExpandedRect = %v, want %v (inset shadow must not expand)", got, want)
	}
}

// TestShadowExpandedRectOutsetSpread covers an outset box-shadow's offset,
// spread and blur all pushing the ink bounds outward, matching the geometry
// paintDropShadow itself paints into.
func TestShadowExpandedRectOutsetSpread(t *testing.T) {
	box := &layout.Box{X: 100, Y: 100, W: 40, H: 40, Style: &css.Style{
		BoxShadows: []css.BoxShadow{{OffsetX: 10, OffsetY: -5, Blur: 8, Spread: 2, Color: css.Color{A: 255}}},
	}}
	// sigma = 4, pad = ceil(12)+1 = 13.
	// x0 = min(100, 100+10-2-13) = 95; y0 = min(100, 100-5-2-13) = 80
	// x1 = max(140, 100+40+10+2+13) = 165; y1 = max(140, 100+40-5+2+13) = 150
	want := image.Rect(95, 80, 165, 150)
	if got := shadowExpandedRect(box); got != want {
		t.Errorf("shadowExpandedRect = %v, want %v", got, want)
	}
}

// TestFilterMarginKinds covers each filter kind's contribution: pointwise
// filters contribute nothing, blur and drop-shadow contribute their spread,
// and a chain takes the largest.
func TestFilterMarginKinds(t *testing.T) {
	if got := filterMargin(nil); got != 0 {
		t.Errorf("no filters: margin = %v, want 0", got)
	}
	if got := filterMargin([]css.Filter{{Kind: css.FilterBrightness, Amount: 0.5}}); got != 0 {
		t.Errorf("pointwise filter: margin = %v, want 0", got)
	}
	if got, want := filterMargin([]css.Filter{{Kind: css.FilterBlur, Amount: 4}}), 13.0; got != want { // ceil(12)+1
		t.Errorf("blur(4): margin = %v, want %v", got, want)
	}
	if got, want := filterMargin([]css.Filter{{Kind: css.FilterDropShadow, OffsetX: 5, OffsetY: -6, Blur: 8}}), 24.0; got != want { // |5|+|-6|+ceil(12)+1
		t.Errorf("drop-shadow: margin = %v, want %v", got, want)
	}
	chain := []css.Filter{
		{Kind: css.FilterBlur, Amount: 1},                              // ceil(3)+1 = 4
		{Kind: css.FilterDropShadow, OffsetX: 20, OffsetY: 0, Blur: 0}, // 20+0+1 = 21
	}
	if got, want := filterMargin(chain), 21.0; got != want {
		t.Errorf("chain: margin = %v, want the largest contribution %v", got, want)
	}
}

// TestGroupRowsFilterExpandsRows covers groupRows expanding the row range for a
// filter's spread, then clamping to the clip and canvas.
func TestGroupRowsFilterExpandsRows(t *testing.T) {
	box := &layout.Box{X: 10, Y: 100, W: 20, H: 20, Style: &css.Style{
		Filters: []css.Filter{{Kind: css.FilterDropShadow, OffsetX: 0, OffsetY: 0, Blur: 0}}, // margin = 1
	}}
	canvas := image.Rect(0, 0, 200, 1000)
	clip := canvas
	y0, y1 := groupRows(box, true, canvas, clip)
	if wantY0, wantY1 := 99, 121; y0 != wantY0 || y1 != wantY1 {
		t.Errorf("groupRows = [%d,%d), want [%d,%d)", y0, y1, wantY0, wantY1)
	}

	// A tighter clip cuts the range down further.
	tight := image.Rect(0, 0, 200, 110)
	y0, y1 = groupRows(box, true, canvas, tight)
	if y1 != 110 {
		t.Errorf("groupRows clip clamp: y1 = %d, want 110", y1)
	}

	// No filter: no margin, exactly the box's own extent.
	y0, y1 = groupRows(box, false, canvas, canvas)
	if y0 != 100 || y1 != 120 {
		t.Errorf("groupRows (no filter) = [%d,%d), want [100,120)", y0, y1)
	}
}

// TestGroupRowsOutsideCanvas covers a box entirely below the canvas (e.g. an
// abandoned off-screen node): the row range collapses to empty rather than
// going negative-width.
func TestGroupRowsOutsideCanvas(t *testing.T) {
	box := &layout.Box{X: 0, Y: 5000, W: 10, H: 10}
	canvas := image.Rect(0, 0, 100, 100)
	y0, y1 := groupRows(box, false, canvas, canvas)
	if y1 > y0 {
		t.Errorf("groupRows for an off-canvas box = [%d,%d), want empty", y0, y1)
	}
}

// TestCopyRows verifies the row range is copied byte-for-byte into a fresh
// zero-origin buffer, independent of the source's own Rect.Min.
func TestCopyRows(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 2, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 2; x++ {
			src.SetRGBA(x, y, color.RGBA{R: uint8(y), G: uint8(x), B: 0, A: 255})
		}
	}
	out := copyRows(src, 1, 3)
	if got, want := out.Bounds(), image.Rect(0, 0, 2, 2); got != want {
		t.Fatalf("copyRows bounds = %v, want %v", got, want)
	}
	if c := out.RGBAAt(0, 0); c.R != 1 {
		t.Errorf("row 0 of copy should be src row 1: got R=%d", c.R)
	}
	if c := out.RGBAAt(1, 1); c.R != 2 || c.G != 1 {
		t.Errorf("row 1 of copy should be src row 2: got %+v", c)
	}

	// Clamped to the source's own bounds.
	if got := copyRows(src, -5, 2).Bounds().Dy(); got != 2 {
		t.Errorf("copyRows clamps y0 to source bounds: height = %d, want 2", got)
	}
	if got := copyRows(src, 2, 100).Bounds().Dy(); got != 2 {
		t.Errorf("copyRows clamps y1 to source bounds: height = %d, want 2", got)
	}

	// An empty range yields a zero-height buffer, not a panic.
	if got := copyRows(src, 3, 1).Bounds().Dy(); got != 0 {
		t.Errorf("copyRows(y1<=y0) height = %d, want 0", got)
	}
}

// TestPaintBoxGroupOutsideClipIsNoop covers paintBox's early return when a
// filter/opacity box's ink bounds don't intersect the visible clip at all
// (e.g. a translated-off-canvas node): it must do nothing rather than
// allocate a degenerate buffer or panic.
func TestPaintBoxGroupOutsideClipIsNoop(t *testing.T) {
	dst := white(10, 10)
	box := &layout.Box{X: 0, Y: 5000, W: 10, H: 10, Style: &css.Style{HasOpacity: true, Opacity: 0.5}}
	paintBox(dst, nil, box, nil, nil, nil, dst.Bounds())
	if c := dst.RGBAAt(0, 0); c.R != 255 || c.G != 255 || c.B != 255 {
		t.Errorf("dst should be untouched: got %+v", c)
	}
}

// TestCompositeGroupAtOffset covers that a non-zero yOffset lands a zero-origin
// group buffer at the right absolute row on dst — the case that matters once
// copyRows has re-anchored the group to (0,0) (see its doc comment).
func TestCompositeGroupAtOffset(t *testing.T) {
	dst := white(2, 5)
	src := image.NewRGBA(image.Rect(0, 0, 2, 1))
	src.SetRGBA(0, 0, color.RGBA{R: 0, G: 0, B: 0, A: 255})
	compositeGroupAt(dst, src, 1, 3)
	if c := dst.RGBAAt(0, 3); c.R != 0 {
		t.Errorf("opaque pixel should land at dst row yOffset=3: got %+v", c)
	}
	if c := dst.RGBAAt(0, 0); c.R != 255 {
		t.Errorf("row 0 of dst must be untouched: got %+v", c)
	}
}
