// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package paint

import (
	"image"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/dom"
	"github.com/go-webengine/engine/layout"
	"github.com/go-widgets/painter"
)

func newTestPainter(dst *image.RGBA) *painter.PixelPainter {
	return painter.NewPixelPainter(dst.Pix, dst.Rect.Dx(), dst.Rect.Dy())
}

func ptRect(x, y, w, h int) painter.Rect { return painter.Rect{X: x, Y: y, W: w, H: h} }

// renderFixture lays out and paints an offline HTML fixture onto a white canvas,
// returning the image for pixel inspection.
func renderFixture(t *testing.T, name string, w, h int) *image.RGBA {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	root, err := dom.Parse(string(data))
	if err != nil {
		t.Fatal(err)
	}
	sm := css.Cascade(root)
	fonts := NewFonts()
	box, _ := layout.LayoutDocument(root, sm, float64(w), fonts, nil)
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range dst.Pix {
		dst.Pix[i] = 255 // white
	}
	PaintFull(dst, box, fonts, nil, nil)
	return dst
}

// hasInk reports whether any pixel in [x0,x1)×[y0,y1) is markedly darker than the
// white background (i.e. text was painted there).
func hasInk(img *image.RGBA, x0, y0, x1, y1 int) bool {
	b := img.Rect
	if x1 > b.Max.X {
		x1 = b.Max.X
	}
	if y1 > b.Max.Y {
		y1 = b.Max.Y
	}
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			if img.RGBAAt(x, y).R < 128 {
				return true
			}
		}
	}
	return false
}

// TestOverflowClipsSrOnlyText is the core regression: the 1×1 overflow:hidden
// sr-only box must confine its overflowing text to its box, not paint it at full
// size. This is the fix for the GitHub / Wikipedia top-of-page jumble.
func TestOverflowClipsSrOnlyText(t *testing.T) {
	img := renderFixture(t, "overflow_sronly.html", 300, 200)
	// The sr-only box sits at y≈100 (after the 100px spacer). Its text, if
	// unclipped, would paint a wide dark band across x≈0..130 at y≈100..120.
	// Assert that band (clear of the 1×1 box's own column) stays blank.
	if hasInk(img, 10, 96, 260, 130) {
		t.Error("sr-only overflowing text painted outside its 1x1 box (not clipped)")
	}
}

// TestClipRectHidesContent is a real regression, found live on pkg.go.dev:
// its own "skip to main content" link uses the LEGACY clip:rect(0 0 0 0)
// sr-only idiom (position:absolute, no explicit small width/height — unlike
// TestOverflowClipsSrOnlyText's overflow:hidden variant, so that mechanism
// never engaged here at all) rather than the modern width:1px/height:1px one.
// Before `clip` was parsed, the link's real text rendered at full size,
// overlapping the page's real search box.
func TestClipRectHidesContent(t *testing.T) {
	img := renderFixture(t, "clip_rect_sronly.html", 300, 200)
	// The clipped link sits at y≈100 (after the 100px spacer). Its text, if
	// not clipped away, would paint a wide dark band there.
	if hasInk(img, 0, 96, 260, 130) {
		t.Error("clip:rect(0 0 0 0) content painted instead of being clipped to nothing")
	}
}

// TestClipRectIgnoredWithoutPositioning guards the spec scoping: clip:rect()
// applies only to position:absolute/fixed, so on a statically-positioned
// element (the common case a bare "clip" declaration on the wrong element
// should never accidentally erase) it must have no effect at all.
func TestClipRectIgnoredWithoutPositioning(t *testing.T) {
	img := renderFixture(t, "clip_rect_static_ignored.html", 300, 200)
	if !hasInk(img, 0, 96, 260, 130) {
		t.Error("clip:rect() on a statically-positioned element wrongly hid its content")
	}
}

// TestOverflowVisiblePaintsOverflow guards the opposite: with the default
// overflow:visible, content past the container's own box still paints.
func TestOverflowVisiblePaintsOverflow(t *testing.T) {
	img := renderFixture(t, "overflow_visible.html", 300, 200)
	// LINETWO (y≈24..48) and LINETHREE (y≈48..72) fall past the 24px container
	// but must still be painted.
	if !hasInk(img, 0, 0, 300, 24) {
		t.Error("first line not painted")
	}
	if !hasInk(img, 0, 28, 300, 72) {
		t.Error("overflow:visible must still paint content past the container box")
	}
}

// TestOverflowHiddenFixedHeightClips confirms an author-set fixed-height
// overflow:hidden box clips the lines below its height.
func TestOverflowHiddenFixedHeightClips(t *testing.T) {
	img := renderFixture(t, "overflow_fixed_height.html", 300, 200)
	if !hasInk(img, 0, 0, 300, 24) {
		t.Error("first (visible) line not painted")
	}
	// LINETWO / LINETHREE lie below the 24px clip and must be gone.
	if hasInk(img, 0, 30, 300, 120) {
		t.Error("content below a fixed-height overflow:hidden box was not clipped")
	}
}

// TestOverflowAutoHeightNotClipped is the regression guard: overflow:hidden on an
// auto-height container the engine collapses (a float container without BFC
// containment) must NOT clip — clipping to the wrongly-tiny box would hide real
// body text. The floated text must still paint.
func TestOverflowAutoHeightNotClipped(t *testing.T) {
	img := renderFixture(t, "overflow_autoheight_guard.html", 300, 200)
	if !hasInk(img, 0, 0, 300, 40) {
		t.Error("auto-height overflow:hidden container wrongly clipped its (collapsed) content")
	}
}

// TestOverflowNowrapAutoHeightClips is a real regression, found live on
// caniuse.com: its own `.news` ticker (overflow:hidden;white-space:nowrap;
// text-overflow:ellipsis) is sized only by a CSS Grid column, with no
// explicit height at all. Unlike TestOverflowAutoHeightNotClipped's collapsed
// float container, a nowrap box's auto height is a trustworthy one-line
// computation — it must still clip the overflowing text at its own right
// edge instead of overlapping the adjacent sibling.
func TestOverflowNowrapAutoHeightClips(t *testing.T) {
	img := renderFixture(t, "overflow_nowrap_autoheight_clips.html", 300, 60)
	// The narrow (150px) nowrap box sits at x=0..150, followed by a 40px gap
	// (the sibling's own margin-left) before "NEXT" starts at x=190. Its long
	// single line, if not clipped, overflows straight through that gap.
	// Assert the gap itself — clear of both the narrow box's own width and
	// the sibling's real text — stays blank.
	if hasInk(img, 155, 0, 185, 40) {
		t.Error("nowrap auto-height overflow:hidden box was not clipped, spilled into the gap before its sibling")
	}
}

// TestClipHelperUnits covers the clip-arithmetic branches directly and
// deterministically (per-axis clamps, empty fills, rounded-rect masking).
func TestClipHelperUnits(t *testing.T) {
	hidden := css.OverflowHidden
	fixedH := css.Length{Px: 1}     // definite height => clipsContent true
	autoH := css.Length{Auto: true} // auto height => clipsContent false
	full := image.Rect(0, 0, 100, 100)

	// clipsContent gate: overflow set but auto/percent height => no clip.
	if clipsContent(&layout.Box{Style: &css.Style{OverflowX: hidden, Height: autoH}}) {
		t.Error("auto-height overflow box must not clip")
	}
	// ...unless the box also forbids wrapping: a nowrap auto height is a
	// trustworthy single-line computation, not a collapsed float/flex row.
	if !clipsContent(&layout.Box{Style: &css.Style{OverflowX: hidden, Height: autoH, WhiteSpace: css.WSNoWrap}}) {
		t.Error("nowrap auto-height overflow box must clip")
	}
	if clipsContent(&layout.Box{Style: &css.Style{OverflowX: hidden, Height: css.Length{IsPercent: true, Percent: 0.5}}}) {
		t.Error("percent-height overflow box must not clip")
	}
	if clipsContent(&layout.Box{Style: nil}) {
		t.Error("nil style must not clip")
	}
	if clipsContent(&layout.Box{Style: &css.Style{Height: fixedH}}) {
		t.Error("visible overflow must not clip")
	}

	// descendantClip: X-axis both edges narrow (box inset from clip on both sides).
	bx := &layout.Box{Style: &css.Style{OverflowX: hidden, Height: fixedH}, X: 20, Y: 0, W: 40, H: 1}
	got := descendantClip(full, bx)
	if got.Min.X != 20 || got.Max.X != 60 || got.Min.Y != 0 || got.Max.Y != 100 {
		t.Errorf("X-clip = %v, want (20,0)-(60,100)", got)
	}
	// Y-axis both edges narrow.
	by := &layout.Box{Style: &css.Style{OverflowY: hidden, Height: fixedH}, X: 0, Y: 30, W: 1, H: 25}
	got = descendantClip(full, by)
	if got.Min.Y != 30 || got.Max.Y != 55 || got.Min.X != 0 || got.Max.X != 100 {
		t.Errorf("Y-clip = %v, want (0,30)-(100,55)", got)
	}
	// Degenerate: padding box entirely left of clip => empty (Max clamped to Min).
	deg := &layout.Box{Style: &css.Style{OverflowX: hidden, OverflowY: hidden, Height: fixedH},
		X: -50, Y: -50, W: 10, H: 10}
	got = descendantClip(image.Rect(0, 0, 100, 100), deg)
	if !got.Empty() {
		t.Errorf("fully-outside clip = %v, want empty", got)
	}

	// fillRectClipped: rect fully outside clip => nothing drawn (empty branch).
	dst := white(10, 10)
	pp := newTestPainter(dst)
	fillRectClipped(pp, ptRect(0, 0, 4, 4), css.Color{R: 1, A: 255}, image.Rect(20, 20, 30, 30))
	if hasInk(dst, 0, 0, 10, 10) {
		t.Error("fillRectClipped drew outside its clip")
	}
	// A==0 / zero-size guards.
	fillRectClipped(pp, ptRect(0, 0, 4, 4), css.Color{A: 0}, dst.Rect)
	fillRectClipped(pp, ptRect(0, 0, 0, 4), css.Color{A: 255}, dst.Rect)

	// fillRoundRectClipped cut path: clip cuts the rounded rect => per-pixel mask.
	dst2 := white(20, 20)
	pp2 := newTestPainter(dst2)
	fillRoundRectClipped(dst2, pp2, ptRect(0, 0, 20, 20), 4, css.Color{R: 0, G: 0, B: 0, A: 255},
		image.Rect(0, 0, 10, 20)) // clip cuts the right half
	if !hasInk(dst2, 4, 8, 6, 12) { // left half painted
		t.Error("rounded fill under clip did not paint the visible half")
	}
	if hasInk(dst2, 12, 8, 20, 12) { // right half clipped away
		t.Error("rounded fill leaked past its clip")
	}

	// paintMarker: a bullet fully outside the clip draws nothing (early return).
	dst3 := white(10, 10)
	pp3 := newTestPainter(dst3)
	m := &layout.Marker{Type: css.ListDisc, Style: &css.Style{Color: css.Color{A: 255}},
		X: 50, Y: 50, W: 5, H: 5}
	paintMarker(dst3, pp3, m, NewFonts(), image.Rect(0, 0, 10, 10))
	if hasInk(dst3, 0, 0, 10, 10) {
		t.Error("marker outside clip was painted")
	}
}
