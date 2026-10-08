// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package paint

import (
	"image"
	"math"
	"testing"

	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/dom"
	"github.com/go-webengine/engine/layout"
)

// solid returns a w×h straight-alpha RGBA filled with c.
func solid(w, h int, c css.Color) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, c.A
	}
	return img
}

func px(img *image.RGBA, x, y int) (uint8, uint8, uint8, uint8) {
	i := img.PixOffset(x, y)
	return img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]
}

// assertPixel checks a pixel against expected values within tol per channel.
func assertPixel(t *testing.T, img *image.RGBA, x, y int, r, g, b, a uint8, tol int) {
	t.Helper()
	gr, gg, gb, ga := px(img, x, y)
	near := func(got, want uint8) bool { return abs(int(got)-int(want)) <= tol }
	if !near(gr, r) || !near(gg, g) || !near(gb, b) || !near(ga, a) {
		t.Errorf("pixel(%d,%d) = (%d,%d,%d,%d), want (%d,%d,%d,%d) ±%d", x, y, gr, gg, gb, ga, r, g, b, a, tol)
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// one wraps a single filter as a chain.
func one(f css.Filter) []css.Filter { return []css.Filter{f} }

// --- Component-transfer / colour-matrix filters vs analytic ground truth ---

func TestBrightnessFilterAnalytic(t *testing.T) {
	in := solid(3, 3, css.Color{R: 200, G: 100, B: 50, A: 255})
	// brightness(0.5): each channel halved.
	out := applyFilters(in, one(css.Filter{Kind: css.FilterBrightness, Amount: 0.5}), css.Color{})
	assertPixel(t, out, 1, 1, 100, 50, 25, 255, 0)
	// brightness(2): 200*2 clamps to 255; 100*2=200; 50*2=100.
	out2 := applyFilters(in, one(css.Filter{Kind: css.FilterBrightness, Amount: 2}), css.Color{})
	assertPixel(t, out2, 1, 1, 255, 200, 100, 255, 0)
}

func TestContrastFilterAnalytic(t *testing.T) {
	in := solid(3, 3, css.Color{R: 200, G: 100, B: 50, A: 255})
	// contrast(2): (v-128)*2+128 → 200→255, 100→72, 50→-28→0. (reuses go-images)
	out := applyFilters(in, one(css.Filter{Kind: css.FilterContrast, Amount: 2}), css.Color{})
	assertPixel(t, out, 1, 1, 255, 72, 0, 255, 0)
	// contrast(1) is identity.
	id := applyFilters(in, one(css.Filter{Kind: css.FilterContrast, Amount: 1}), css.Color{})
	assertPixel(t, id, 1, 1, 200, 100, 50, 255, 0)
}

func TestInvertFilterAnalytic(t *testing.T) {
	in := solid(3, 3, css.Color{R: 10, G: 20, B: 30, A: 255})
	// invert(1): 255-v.
	out := applyFilters(in, one(css.Filter{Kind: css.FilterInvert, Amount: 1}), css.Color{})
	assertPixel(t, out, 1, 1, 245, 235, 225, 255, 0)
	// invert(0.5): every channel maps to the mid-point 127.5→128.
	half := applyFilters(in, one(css.Filter{Kind: css.FilterInvert, Amount: 0.5}), css.Color{})
	assertPixel(t, half, 1, 1, 128, 128, 128, 255, 0)
}

func TestGrayscaleFilterAnalytic(t *testing.T) {
	in := solid(3, 3, css.Color{R: 100, G: 150, B: 200, A: 255})
	// grayscale(1): luminance 0.2126*100+0.7152*150+0.0722*200 = 142.98 → 143.
	lum := uint8(math.Round(0.2126*100 + 0.7152*150 + 0.0722*200))
	out := applyFilters(in, one(css.Filter{Kind: css.FilterGrayscale, Amount: 1}), css.Color{})
	assertPixel(t, out, 1, 1, lum, lum, lum, 255, 1)
	// grayscale(0) is identity.
	id := applyFilters(in, one(css.Filter{Kind: css.FilterGrayscale, Amount: 0}), css.Color{})
	assertPixel(t, id, 1, 1, 100, 150, 200, 255, 0)
}

func TestSaturateFilterAnalytic(t *testing.T) {
	in := solid(3, 3, css.Color{R: 100, G: 150, B: 200, A: 255})
	// saturate(1) is identity.
	id := applyFilters(in, one(css.Filter{Kind: css.FilterSaturate, Amount: 1}), css.Color{})
	assertPixel(t, id, 1, 1, 100, 150, 200, 255, 0)
	// saturate(0) collapses to the 0.213/0.715/0.072 luminance (grey).
	gray := uint8(math.Round(0.213*100 + 0.715*150 + 0.072*200))
	out := applyFilters(in, one(css.Filter{Kind: css.FilterSaturate, Amount: 0}), css.Color{})
	assertPixel(t, out, 1, 1, gray, gray, gray, 255, 1)
}

func TestSepiaFilterAnalytic(t *testing.T) {
	in := solid(3, 3, css.Color{R: 100, G: 100, B: 100, A: 255})
	// sepia(1) on grey 100: rows sum to 1.351/1.203/0.937 → 135/120/94.
	out := applyFilters(in, one(css.Filter{Kind: css.FilterSepia, Amount: 1}), css.Color{})
	r := uint8(math.Round((0.393 + 0.769 + 0.189) * 100))
	g := uint8(math.Round((0.349 + 0.686 + 0.168) * 100))
	b := uint8(math.Round((0.272 + 0.534 + 0.131) * 100))
	assertPixel(t, out, 1, 1, r, g, b, 255, 1)
	// sepia(0) is identity.
	id := applyFilters(in, one(css.Filter{Kind: css.FilterSepia, Amount: 0}), css.Color{})
	assertPixel(t, id, 1, 1, 100, 100, 100, 255, 0)
}

func TestHueRotateFilterAnalytic(t *testing.T) {
	// hue-rotate(0) is identity.
	in := solid(3, 3, css.Color{R: 100, G: 150, B: 200, A: 255})
	id := applyFilters(in, one(css.Filter{Kind: css.FilterHueRotate, Amount: 0}), css.Color{})
	assertPixel(t, id, 1, 1, 100, 150, 200, 255, 0)
	// A neutral grey is invariant under any hue rotation (matrix rows sum to 1).
	grey := solid(3, 3, css.Color{R: 120, G: 120, B: 120, A: 255})
	out := applyFilters(grey, one(css.Filter{Kind: css.FilterHueRotate, Amount: math.Pi / 3}), css.Color{})
	assertPixel(t, out, 1, 1, 120, 120, 120, 255, 1)
}

// --- Blur: premultiplied correctness vs a naive straight blur ---

func TestBlurZeroIsNoOp(t *testing.T) {
	in := solid(4, 4, css.Color{R: 10, G: 20, B: 30, A: 255})
	out := applyFilters(in, one(css.Filter{Kind: css.FilterBlur, Amount: 0}), css.Color{})
	if out != in {
		t.Error("blur(0) should return the input buffer unchanged")
	}
}

func TestBlurUniformOpaqueUnchanged(t *testing.T) {
	// Blurring a solid opaque field leaves every pixel unchanged (clamp-to-edge).
	in := solid(8, 8, css.Color{R: 60, G: 120, B: 180, A: 255})
	out := applyFilters(in, one(css.Filter{Kind: css.FilterBlur, Amount: 2}), css.Color{})
	assertPixel(t, out, 4, 4, 60, 120, 180, 255, 1)
	assertPixel(t, out, 0, 0, 60, 120, 180, 255, 1)
}

func TestBlurPremultipliedNoDarkFringe(t *testing.T) {
	// An opaque red block on the left half, transparent on the right. A
	// premultiplied blur keeps the recovered colour red where alpha survives;
	// a naive straight-alpha blur would drag it toward black (the transparent
	// pixels' 0,0,0 colour leaking in).
	w, h := 16, 8
	in := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := in.PixOffset(x, y)
			if x < w/2 {
				in.Pix[i], in.Pix[i+1], in.Pix[i+2], in.Pix[i+3] = 255, 0, 0, 255
			}
			// right half left transparent (all zero)
		}
	}
	out := applyFilters(in, one(css.Filter{Kind: css.FilterBlur, Amount: 2}), css.Color{})
	// A pixel just across the edge (x=8) now has partial alpha; its recovered
	// colour must still be essentially pure red, not a dark muddied red.
	r, g, b, a := px(out, 8, 4)
	if a == 0 {
		t.Fatal("expected partial alpha across the blurred edge")
	}
	if r < 240 || g > 15 || b > 15 {
		t.Errorf("edge colour = (%d,%d,%d,%d): premultiplied blur should keep it red", r, g, b, a)
	}
}

// --- Drop shadow ---

func TestDropShadowHardOffset(t *testing.T) {
	// A 4×4 opaque black square centred in a 16×16 buffer, shadow offset (5,5)
	// with no blur and a fully opaque red colour.
	w := 16
	in := image.NewRGBA(image.Rect(0, 0, w, w))
	for y := 6; y < 10; y++ {
		for x := 6; x < 10; x++ {
			i := in.PixOffset(x, y)
			in.Pix[i], in.Pix[i+1], in.Pix[i+2], in.Pix[i+3] = 0, 0, 0, 255
		}
	}
	f := css.Filter{Kind: css.FilterDropShadow, OffsetX: 5, OffsetY: 5, Color: css.Color{R: 255, G: 0, B: 0, A: 255}}
	out := applyFilters(in, one(f), css.Color{})
	// The element itself stays black on top.
	assertPixel(t, out, 7, 7, 0, 0, 0, 255, 0)
	// The shadow appears at the element position + offset, where the element is
	// absent (bottom-right), as opaque red.
	assertPixel(t, out, 12, 12, 255, 0, 0, 255, 0)
	// A pixel neither in the element nor its shadow stays transparent.
	assertPixel(t, out, 0, 0, 0, 0, 0, 0, 0)
}

func TestDropShadowCurrentColor(t *testing.T) {
	// UseCurrentColor resolves the shadow colour from the element's `color`.
	w := 12
	in := image.NewRGBA(image.Rect(0, 0, w, w))
	for y := 4; y < 7; y++ {
		for x := 4; x < 7; x++ {
			i := in.PixOffset(x, y)
			in.Pix[i], in.Pix[i+1], in.Pix[i+2], in.Pix[i+3] = 0, 0, 255, 255
		}
	}
	f := css.Filter{Kind: css.FilterDropShadow, OffsetX: 3, OffsetY: 3, UseCurrentColor: true}
	out := applyFilters(in, one(f), css.Color{R: 0, G: 200, B: 0, A: 255})
	// Shadow region (offset, outside element) is the current colour green.
	assertPixel(t, out, 8, 8, 0, 200, 0, 255, 0)
}

func TestDropShadowBlurSoftens(t *testing.T) {
	// With a blur the shadow edge is soft: a pixel just outside the hard shadow
	// footprint still receives partial alpha.
	w := 24
	in := image.NewRGBA(image.Rect(0, 0, w, w))
	for y := 8; y < 12; y++ {
		for x := 8; x < 12; x++ {
			i := in.PixOffset(x, y)
			in.Pix[i], in.Pix[i+1], in.Pix[i+2], in.Pix[i+3] = 0, 0, 0, 255
		}
	}
	f := css.Filter{Kind: css.FilterDropShadow, OffsetX: 6, OffsetY: 6, Blur: 6, Color: css.Color{R: 255, G: 0, B: 0, A: 255}}
	out := applyFilters(in, one(f), css.Color{})
	// Centre of the shadow footprint is strongly red.
	_, _, _, ca := px(out, 16, 16)
	// Just outside the 4×4 hard footprint the blur bleeds some partial alpha.
	_, _, _, ea := px(out, 20, 20)
	if ca == 0 {
		t.Fatal("shadow centre should have alpha")
	}
	if ea == 0 || ea >= ca {
		t.Errorf("blur edge alpha = %d, centre = %d: expected 0 < edge < centre", ea, ca)
	}
}

// --- Chaining: filters apply left to right ---

func TestFilterChainOrder(t *testing.T) {
	in := solid(3, 3, css.Color{R: 200, G: 100, B: 50, A: 255})
	// invert(1) then grayscale(1): first → (55,155,205), then luminance.
	chain := []css.Filter{
		{Kind: css.FilterInvert, Amount: 1},
		{Kind: css.FilterGrayscale, Amount: 1},
	}
	out := applyFilters(in, chain, css.Color{})
	lum := uint8(math.Round(0.2126*55 + 0.7152*155 + 0.0722*205))
	assertPixel(t, out, 1, 1, lum, lum, lum, 255, 1)
}

func TestSrcOverFullyTransparent(t *testing.T) {
	// Compositing transparent over transparent yields transparent (covers the
	// oa<=0 branch of srcOver).
	r, g, b, a := srcOver(0, 0, 0, 0, 0, 0, 0, 0)
	if r != 0 || g != 0 || b != 0 || a != 0 {
		t.Errorf("srcOver of two transparent = (%v,%v,%v,%v)", r, g, b, a)
	}
}

func TestClamp8Bounds(t *testing.T) {
	if clamp8(-5) != 0 || clamp8(300) != 255 || clamp8(127.5) != 128 {
		t.Errorf("clamp8 bounds wrong: %d %d %d", clamp8(-5), clamp8(300), clamp8(127.5))
	}
}

// TestPaintBoxFilterGroup exercises the paintBox group pass: a filtered element
// is rendered offscreen, the filter chain applied, then composited. A grayscale
// filter on a solid blue box turns it grey through the public Paint entry point.
func TestPaintBoxFilterGroup(t *testing.T) {
	dst := white(20, 20)
	st := &css.Style{
		Background: css.Color{R: 0, G: 0, B: 255, A: 255},
		Filters:    []css.Filter{{Kind: css.FilterGrayscale, Amount: 1}},
	}
	box := &layout.Box{Node: &dom.Node{Type: dom.Element, Tag: "div"}, Style: st, X: 0, Y: 0, W: 20, H: 20}
	PaintFull(dst, box, NewFonts(), nil, nil)
	c := dst.RGBAAt(10, 10)
	// Pure blue (0,0,255) → luminance 0.0722*255 ≈ 18, grey (R==G==B).
	if c.R != c.G || c.G != c.B {
		t.Errorf("grayscale filter did not neutralise colour: %+v", c)
	}
	if c.R < 14 || c.R > 22 {
		t.Errorf("grey level = %d, want ~18", c.R)
	}
}

// TestInlineImageFilterApplied covers a THIRD entry point into filter
// handling, distinct from the two paintBox-based tests above: an <img>,
// which is always represented as an InlineItem (see contents()'s
// isReplacedTag branch), never a layout.Box, and so is painted via paintItem
// rather than paintBox — a different code path that never saw `filter` at
// all before this fix. Confirmed live on pkg.go.dev, whose Details-panel
// icons are a single grey SVG asset recoloured per-context via exactly this
// filter chain (the "SVG-to-CSS-filter" idiom, since an <img src> cannot be
// recoloured with `fill`/`currentColor` the way an inline <svg> can).
func TestInlineImageFilterApplied(t *testing.T) {
	dst := white(20, 20)
	node := &dom.Node{Type: dom.Element, Tag: "img"}
	src := solid(10, 10, css.Color{R: 0, G: 0, B: 255, A: 255})
	imgs := map[*dom.Node]image.Image{node: src}
	st := &css.Style{Filters: []css.Filter{{Kind: css.FilterGrayscale, Amount: 1}}}
	it := &layout.InlineItem{Image: node, Style: st, X: 0, Y: 0, ImgW: 10, ImgH: 10}
	line := &layout.LineBox{X: 0, Y: 0, W: 10, H: 10, Items: []*layout.InlineItem{it}}
	box := &layout.Box{Node: &dom.Node{Type: dom.Element, Tag: "p"}, Style: &css.Style{},
		X: 0, Y: 0, W: 20, H: 20, Lines: []*layout.LineBox{line}}
	PaintFull(dst, box, NewFonts(), imgs, nil)
	c := dst.RGBAAt(5, 5)
	if c.R != c.G || c.G != c.B {
		t.Errorf("grayscale filter did not neutralise inline image colour: %+v", c)
	}
}

// TestInlineImageFilterConvertsNonRGBASource covers toRGBA's conversion
// branch: a decoded image whose concrete type is not already *image.RGBA
// (an *image.NRGBA here, matching what a decoded PNG with a straight alpha
// channel commonly is) must still have its filter applied correctly, not
// silently skipped or panic on a failed type assertion.
func TestInlineImageFilterConvertsNonRGBASource(t *testing.T) {
	dst := white(20, 20)
	node := &dom.Node{Type: dom.Element, Tag: "img"}
	nrgba := image.NewNRGBA(image.Rect(0, 0, 10, 10))
	for i := 0; i < len(nrgba.Pix); i += 4 {
		nrgba.Pix[i], nrgba.Pix[i+1], nrgba.Pix[i+2], nrgba.Pix[i+3] = 0, 0, 255, 255
	}
	imgs := map[*dom.Node]image.Image{node: nrgba}
	st := &css.Style{Filters: []css.Filter{{Kind: css.FilterGrayscale, Amount: 1}}}
	it := &layout.InlineItem{Image: node, Style: st, X: 0, Y: 0, ImgW: 10, ImgH: 10}
	line := &layout.LineBox{X: 0, Y: 0, W: 10, H: 10, Items: []*layout.InlineItem{it}}
	box := &layout.Box{Node: &dom.Node{Type: dom.Element, Tag: "p"}, Style: &css.Style{},
		X: 0, Y: 0, W: 20, H: 20, Lines: []*layout.LineBox{line}}
	PaintFull(dst, box, NewFonts(), imgs, nil)
	c := dst.RGBAAt(5, 5)
	if c.R != c.G || c.G != c.B {
		t.Errorf("grayscale filter on a non-RGBA source = %+v, want neutralised", c)
	}
}

// TestPaintBoxFilterWithOpacity covers a box carrying BOTH a filter and a
// fractional opacity: filter first, then the opacity composites the result.
func TestPaintBoxFilterWithOpacity(t *testing.T) {
	dst := white(20, 20)
	st := &css.Style{
		Background: css.Color{R: 0, G: 0, B: 0, A: 255},
		Filters:    []css.Filter{{Kind: css.FilterInvert, Amount: 1}}, // black → white
		Opacity:    0.5,
		HasOpacity: true,
	}
	box := &layout.Box{Node: &dom.Node{Type: dom.Element, Tag: "div"}, Style: st, X: 0, Y: 0, W: 20, H: 20}
	PaintFull(dst, box, NewFonts(), nil, nil)
	// Inverted black = white; at 0.5 opacity over white → still white.
	if c := dst.RGBAAt(10, 10); c.R != 255 || c.G != 255 || c.B != 255 {
		t.Errorf("filter+opacity = %+v want white", c)
	}
}
