// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package paint

import (
	"image"
	"image/color"
	"testing"

	"github.com/go-gfx/gfx/resample"

	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/dom"
	"github.com/go-webengine/engine/layout"
)

func stop(c css.Color) css.ColorStop { return css.ColorStop{Color: c} }

// linearGradStyle builds a Style whose background is a single linear gradient at
// the given CSS angle (0=up, 90=right) with the given stops.
func linearGradStyle(angle float64, stops ...css.ColorStop) *css.Style {
	return &css.Style{BackgroundImages: []css.BgImage{{
		Kind: css.BgGradient,
		Grad: &css.Gradient{AngleDeg: angle, Stops: stops},
	}}}
}

// TestPaintLinearGradientEndpoints fills a wide box with a black→white
// horizontal gradient and asserts the sampled endpoint/mid colours.
func TestPaintLinearGradientEndpoints(t *testing.T) {
	dst := white(100, 10)
	box := &layout.Box{
		Node:  &dom.Node{Type: dom.Element, Tag: "div"},
		Style: linearGradStyle(90, stop(css.Color{R: 0, G: 0, B: 0, A: 255}), stop(css.Color{R: 255, G: 255, B: 255, A: 255})),
		X:     0, Y: 0, W: 100, H: 10,
	}
	PaintFull(dst, box, NewFonts(), nil, nil)
	if c := dst.RGBAAt(0, 5); c.R > 8 {
		t.Errorf("left = %+v want ~black", c)
	}
	if c := dst.RGBAAt(50, 5); c.R < 118 || c.R > 138 {
		t.Errorf("mid = %+v want ~grey", c)
	}
	if c := dst.RGBAAt(99, 5); c.R < 247 {
		t.Errorf("right = %+v want ~white", c)
	}
}

// TestPaintGradientRoundedClip: a gradient in a rounded box leaves the extreme
// corner unpainted (still white).
func TestPaintGradientRoundedClip(t *testing.T) {
	dst := white(40, 40)
	st := linearGradStyle(90, stop(css.Color{R: 255, G: 0, B: 0, A: 255}), stop(css.Color{R: 255, G: 0, B: 0, A: 255}))
	st.BorderRadius = css.Length{Px: 12}
	box := &layout.Box{Node: &dom.Node{Type: dom.Element, Tag: "div"}, Style: st, X: 0, Y: 0, W: 40, H: 40}
	PaintFull(dst, box, NewFonts(), nil, nil)
	if c := dst.RGBAAt(0, 0); c.R != 255 || c.G != 255 || c.B != 255 {
		t.Errorf("rounded corner = %+v want white", c)
	}
	if c := dst.RGBAAt(20, 20); c.R < 250 || c.G > 5 {
		t.Errorf("centre = %+v want red", c)
	}
}

func TestPaintGradientNilGrad(t *testing.T) {
	dst := white(4, 4)
	paintGradient(dst, image.Rect(0, 0, 4, 4), 0, nil, dst.Rect) // no panic, no change
	if c := dst.RGBAAt(1, 1); c.R != 255 {
		t.Error("nil gradient changed pixels")
	}
}

// TestPaintGradientTransparentStopSkips: a fully-transparent stop region leaves
// the underlying pixel unchanged.
func TestPaintGradientTransparentStopSkips(t *testing.T) {
	dst := white(20, 4)
	box := &layout.Box{
		Node:  &dom.Node{Type: dom.Element, Tag: "div"},
		Style: linearGradStyle(90, stop(css.Color{R: 0, G: 0, B: 0, A: 0}), stop(css.Color{R: 0, G: 0, B: 0, A: 0})),
		X:     0, Y: 0, W: 20, H: 4,
	}
	PaintFull(dst, box, NewFonts(), nil, nil)
	if c := dst.RGBAAt(10, 2); c.R != 255 {
		t.Errorf("transparent gradient painted: %+v", c)
	}
}

func solidImage(w, h int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func urlStyle(url string, size css.BgSize, pos css.BgPosition, rep css.BgRepeat) *css.Style {
	return &css.Style{
		BackgroundImages:   []css.BgImage{{Kind: css.BgURL, URL: url}},
		BackgroundSize:     []css.BgSize{size},
		BackgroundPosition: []css.BgPosition{pos},
		BackgroundRepeat:   []css.BgRepeat{rep},
	}
}

func TestPaintBgBitmapCover(t *testing.T) {
	dst := white(20, 20)
	src := solidImage(2, 2, color.RGBA{0, 100, 200, 255})
	box := &layout.Box{
		Node:  &dom.Node{Type: dom.Element, Tag: "div"},
		Style: urlStyle("x", css.BgSize{Kind: css.SizeCover}, css.BgPosition{}, css.NoRepeat),
		X:     0, Y: 0, W: 20, H: 20,
	}
	PaintFull(dst, box, NewFonts(), nil, map[string]image.Image{"x": src})
	if c := dst.RGBAAt(10, 10); c.B < 150 {
		t.Errorf("cover fill = %+v want blue", c)
	}
}

func TestPaintBgBitmapMissingSkipped(t *testing.T) {
	dst := white(10, 10)
	box := &layout.Box{
		Node:  &dom.Node{Type: dom.Element, Tag: "div"},
		Style: urlStyle("missing", css.BgSize{Kind: css.SizeAuto}, css.BgPosition{}, css.NoRepeat),
		X:     0, Y: 0, W: 10, H: 10,
	}
	PaintFull(dst, box, NewFonts(), nil, map[string]image.Image{}) // no entry
	if c := dst.RGBAAt(5, 5); c.R != 255 {
		t.Error("missing bg bitmap painted something")
	}
}

func TestPaintBgBitmapNoRepeatPosition(t *testing.T) {
	dst := white(40, 40)
	src := solidImage(4, 4, color.RGBA{200, 0, 0, 255})
	// no-repeat, size 4x4, position centre => tile at (18,18)..(22,22).
	pos := css.BgPosition{X: css.Length{Percent: 0.5, IsPercent: true}, Y: css.Length{Percent: 0.5, IsPercent: true}}
	box := &layout.Box{
		Node:  &dom.Node{Type: dom.Element, Tag: "div"},
		Style: urlStyle("x", css.BgSize{Kind: css.SizeExplicit, W: css.Length{Px: 4}, H: css.Length{Px: 4}}, pos, css.NoRepeat),
		X:     0, Y: 0, W: 40, H: 40,
	}
	PaintFull(dst, box, NewFonts(), nil, map[string]image.Image{"x": src})
	if c := dst.RGBAAt(20, 20); c.R < 150 {
		t.Errorf("centre tile = %+v want red", c)
	}
	if c := dst.RGBAAt(2, 2); c.R != 255 {
		t.Errorf("corner should be empty (no-repeat) = %+v", c)
	}
}

func TestPaintBgBitmapRepeatModes(t *testing.T) {
	src := solidImage(4, 4, color.RGBA{0, 150, 0, 255})
	for _, rep := range []css.BgRepeat{css.RepeatBoth, css.RepeatX, css.RepeatY} {
		dst := white(20, 20)
		box := &layout.Box{
			Node:  &dom.Node{Type: dom.Element, Tag: "div"},
			Style: urlStyle("x", css.BgSize{Kind: css.SizeAuto}, css.BgPosition{}, rep),
			X:     0, Y: 0, W: 20, H: 20,
		}
		PaintFull(dst, box, NewFonts(), nil, map[string]image.Image{"x": src})
		// The top-left tile always paints.
		if c := dst.RGBAAt(1, 1); c.G < 100 {
			t.Errorf("repeat %d top-left = %+v", rep, c)
		}
	}
}

func TestBgTileSizeVariants(t *testing.T) {
	// cover: scale to max so the 2x1 image covers a 10x10 box → 20x10.
	if w, h := bgTileSize(css.BgSize{Kind: css.SizeCover}, 2, 1, 10, 10); w != 20 || h != 10 {
		t.Errorf("cover = %v,%v", w, h)
	}
	// contain: scale to min → 10x5.
	if w, h := bgTileSize(css.BgSize{Kind: css.SizeContain}, 2, 1, 10, 10); w != 10 || h != 5 {
		t.Errorf("contain = %v,%v", w, h)
	}
	// auto: intrinsic.
	if w, h := bgTileSize(css.BgSize{Kind: css.SizeAuto}, 3, 4, 10, 10); w != 3 || h != 4 {
		t.Errorf("auto = %v,%v", w, h)
	}
	// explicit both.
	if w, h := bgTileSize(css.BgSize{Kind: css.SizeExplicit, W: css.Length{Px: 8}, H: css.Length{Px: 6}}, 3, 4, 10, 10); w != 8 || h != 6 {
		t.Errorf("explicit both = %v,%v", w, h)
	}
	// explicit width, auto height keeps aspect (image 4x2, W=8 => H=4).
	if w, h := bgTileSize(css.BgSize{Kind: css.SizeExplicit, W: css.Length{Px: 8}, H: css.Length{Auto: true}}, 4, 2, 10, 10); w != 8 || h != 4 {
		t.Errorf("explicit w+auto = %v,%v", w, h)
	}
	// explicit height, auto width keeps aspect (image 4x2, H=4 => W=8).
	if w, h := bgTileSize(css.BgSize{Kind: css.SizeExplicit, W: css.Length{Auto: true}, H: css.Length{Px: 4}}, 4, 2, 10, 10); w != 8 || h != 4 {
		t.Errorf("explicit h+auto = %v,%v", w, h)
	}
	// explicit both auto => intrinsic.
	if w, h := bgTileSize(css.BgSize{Kind: css.SizeExplicit, W: css.Length{Auto: true}, H: css.Length{Auto: true}}, 5, 6, 10, 10); w != 5 || h != 6 {
		t.Errorf("explicit both auto = %v,%v", w, h)
	}
}

func TestResolvePos(t *testing.T) {
	if got := resolvePos(css.Length{Percent: 0.5, IsPercent: true}, 40); got != 20 {
		t.Errorf("percent pos = %v want 20", got)
	}
	if got := resolvePos(css.Length{Px: 7}, 40); got != 7 {
		t.Errorf("px pos = %v want 7", got)
	}
}

func TestPaintBgBitmapDegenerateSizes(t *testing.T) {
	dst := white(10, 10)
	src := solidImage(4, 4, color.RGBA{1, 2, 3, 255})
	// Explicit 0-size clamps to 1px each (no panic, minimal paint).
	box := &layout.Box{
		Node:  &dom.Node{Type: dom.Element, Tag: "div"},
		Style: urlStyle("x", css.BgSize{Kind: css.SizeExplicit, W: css.Length{Px: 0}, H: css.Length{Px: 0}}, css.BgPosition{}, css.NoRepeat),
		X:     0, Y: 0, W: 10, H: 10,
	}
	PaintFull(dst, box, NewFonts(), nil, map[string]image.Image{"x": src})
	// Zero-dimension source is a no-op.
	empty := image.NewRGBA(image.Rect(0, 0, 0, 0))
	paintBgBitmap(dst, image.Rect(0, 0, 10, 10), 0, empty, css.BgSize{Kind: css.SizeAuto}, css.BgPosition{}, css.NoRepeat, dst.Rect, nil)
}

// TestPaintDropShadow: a soft drop shadow darkens pixels below/right of the box
// but leaves distant pixels clean; the extent is bounded by offset+blur.
func TestPaintDropShadow(t *testing.T) {
	dst := white(60, 60)
	st := &css.Style{
		Background: css.Color{R: 255, G: 255, B: 255, A: 255},
		BoxShadows: []css.BoxShadow{{OffsetX: 0, OffsetY: 4, Blur: 6, Color: css.Color{A: 255}}},
	}
	box := &layout.Box{Node: &dom.Node{Type: dom.Element, Tag: "div"}, Style: st, X: 20, Y: 20, W: 20, H: 20}
	PaintFull(dst, box, NewFonts(), nil, nil)
	// Just below the box a shadow band darkens the white.
	darkened := false
	for y := 40; y < 50; y++ {
		if dst.RGBAAt(30, y).R < 230 {
			darkened = true
			break
		}
	}
	if !darkened {
		t.Error("expected a shadow band below the box")
	}
	// Far top-left corner is untouched (outside the shadow extent).
	if c := dst.RGBAAt(0, 0); c.R != 255 {
		t.Errorf("distant pixel darkened = %+v", c)
	}
}

func TestPaintDropShadowTransparentSkipped(t *testing.T) {
	dst := white(30, 30)
	st := &css.Style{BoxShadows: []css.BoxShadow{{OffsetY: 2, Blur: 2, Color: css.Color{}}}}
	box := &layout.Box{Node: &dom.Node{Type: dom.Element, Tag: "div"}, Style: st, X: 10, Y: 10, W: 8, H: 8}
	PaintFull(dst, box, NewFonts(), nil, nil)
	if c := dst.RGBAAt(14, 20); c.R != 255 {
		t.Errorf("transparent shadow painted: %+v", c)
	}
}

func TestPaintInsetShadow(t *testing.T) {
	dst := white(40, 40)
	st := &css.Style{
		Background: css.Color{R: 255, G: 255, B: 255, A: 255},
		BoxShadows: []css.BoxShadow{{OffsetX: 0, OffsetY: 0, Blur: 6, Inset: true, Color: css.Color{A: 255}}},
	}
	box := &layout.Box{Node: &dom.Node{Type: dom.Element, Tag: "div"}, Style: st, X: 5, Y: 5, W: 30, H: 30}
	PaintFull(dst, box, NewFonts(), nil, nil)
	// Interior centre is lighter than the shadowed inner edge.
	edge := dst.RGBAAt(6, 20).R
	centre := dst.RGBAAt(20, 20).R
	if edge >= centre {
		t.Errorf("inset edge (%d) should be darker than centre (%d)", edge, centre)
	}
}

func TestPaintInsetShadowTransparentSkipped(t *testing.T) {
	dst := white(20, 20)
	st := &css.Style{BoxShadows: []css.BoxShadow{{Blur: 2, Inset: true, Color: css.Color{}}}}
	box := &layout.Box{Node: &dom.Node{Type: dom.Element, Tag: "div"}, Style: st, X: 2, Y: 2, W: 16, H: 16}
	PaintFull(dst, box, NewFonts(), nil, nil)
	if c := dst.RGBAAt(3, 3); c.R != 255 {
		t.Errorf("transparent inset painted: %+v", c)
	}
}

func TestErfBoxCoverage(t *testing.T) {
	// sigma<=0: hard test.
	if erfBoxCoverage(5, 5, 0, 0, 10, 10, 0) != 1 {
		t.Error("hard inside should be 1")
	}
	if erfBoxCoverage(-1, 5, 0, 0, 10, 10, 0) != 0 {
		t.Error("hard outside should be 0")
	}
	// sigma>0: centre near 1, far outside near 0.
	if c := erfBoxCoverage(5, 5, 0, 0, 10, 10, 1); c < 0.9 {
		t.Errorf("blurred centre = %v want ~1", c)
	}
	if c := erfBoxCoverage(50, 50, 0, 0, 10, 10, 1); c > 0.01 {
		t.Errorf("blurred far = %v want ~0", c)
	}
}

// TestPaintOpacityGroup: an opacity-0.5 black box over white yields ~grey; an
// opacity-0 box paints nothing.
func TestPaintOpacityGroup(t *testing.T) {
	dst := white(20, 20)
	st := &css.Style{Background: css.Color{A: 255}, Opacity: 0.5, HasOpacity: true}
	box := &layout.Box{Node: &dom.Node{Type: dom.Element, Tag: "div"}, Style: st, X: 0, Y: 0, W: 20, H: 20}
	PaintFull(dst, box, NewFonts(), nil, nil)
	if c := dst.RGBAAt(10, 10); c.R < 118 || c.R > 138 {
		t.Errorf("opacity 0.5 black over white = %+v want ~grey", c)
	}

	dst2 := white(20, 20)
	st2 := &css.Style{Background: css.Color{A: 255}, Opacity: 0, HasOpacity: true}
	box2 := &layout.Box{Node: &dom.Node{Type: dom.Element, Tag: "div"}, Style: st2, X: 0, Y: 0, W: 20, H: 20}
	PaintFull(dst2, box2, NewFonts(), nil, nil)
	if c := dst2.RGBAAt(10, 10); c.R != 255 {
		t.Errorf("opacity 0 painted: %+v", c)
	}

	// opacity exactly 1 (HasOpacity) paints normally.
	dst3 := white(20, 20)
	st3 := &css.Style{Background: css.Color{A: 255}, Opacity: 1, HasOpacity: true}
	box3 := &layout.Box{Node: &dom.Node{Type: dom.Element, Tag: "div"}, Style: st3, X: 0, Y: 0, W: 20, H: 20}
	PaintFull(dst3, box3, NewFonts(), nil, nil)
	if c := dst3.RGBAAt(10, 10); c.R != 0 {
		t.Errorf("opacity 1 = %+v want black", c)
	}
}

func TestInsideRoundRect(t *testing.T) {
	r := image.Rect(0, 0, 40, 40)
	// Outside bounds.
	if insideRoundRect(-1, 5, r, 10) {
		t.Error("outside bounds should be false")
	}
	// rad<=0: plain rect.
	if !insideRoundRect(0, 0, r, 0) {
		t.Error("plain rect corner should be inside")
	}
	// Corner outside the radius arc.
	if insideRoundRect(0, 0, r, 12) {
		t.Error("extreme corner should be outside the arc")
	}
	// Straight-edge pixel (dx or dy == 0) inside.
	if !insideRoundRect(20, 1, r, 12) {
		t.Error("mid top edge should be inside")
	}
	// Huge radius clamps to half the smaller side (disc): centre inside.
	if !insideRoundRect(20, 20, r, 1000) {
		t.Error("centre should be inside a disc")
	}
}

func TestBlendPixelTransparentBuffer(t *testing.T) {
	// On a fully-transparent buffer, blending a colour yields that colour with the
	// coverage alpha (correct group-buffer behaviour).
	buf := image.NewRGBA(image.Rect(0, 0, 1, 1)) // transparent
	blendPixel(buf, 0, 0, css.Color{R: 200, G: 100, B: 50, A: 255}, 128)
	c := buf.RGBAAt(0, 0)
	if c.R < 190 || c.A < 120 || c.A > 135 {
		t.Errorf("transparent-buffer blend = %+v", c)
	}
	// Zero coverage is a no-op.
	blendPixel(buf, 0, 0, css.Color{A: 255}, 0)
	// Compositing onto a zero-alpha result with zero source keeps zero.
	buf2 := image.NewRGBA(image.Rect(0, 0, 1, 1))
	blendPixel(buf2, 0, 0, css.Color{A: 0}, 255) // sa==0 -> early return
	if buf2.RGBAAt(0, 0).A != 0 {
		t.Error("zero-alpha source changed transparent pixel")
	}
}

func TestCompositeGroupSkipsTransparent(t *testing.T) {
	dst := white(2, 1)
	src := image.NewRGBA(image.Rect(0, 0, 2, 1)) // all transparent
	src.SetRGBA(0, 0, color.RGBA{0, 0, 0, 255})  // one opaque black
	compositeGroupAt(dst, src, 1, 0)
	if c := dst.RGBAAt(0, 0); c.R != 0 {
		t.Errorf("opaque group pixel = %+v want black", c)
	}
	if c := dst.RGBAAt(1, 0); c.R != 255 {
		t.Errorf("transparent group pixel changed dst = %+v", c)
	}
}

func TestNthHelpers(t *testing.T) {
	if s := nthSize(nil, 3); s.Kind != css.SizeAuto {
		t.Errorf("nthSize empty = %+v", s)
	}
	if s := nthSize([]css.BgSize{{Kind: css.SizeCover}}, 5); s.Kind != css.SizeCover {
		t.Errorf("nthSize overflow = %+v", s)
	}
	if p := nthPosition(nil, 0); !p.X.IsPercent || p.X.Percent != 0 {
		t.Errorf("nthPosition empty = %+v", p)
	}
	if p := nthPosition([]css.BgPosition{{X: css.Length{Px: 5}}}, 9); p.X.Px != 5 {
		t.Errorf("nthPosition overflow = %+v", p)
	}
	if r := nthRepeat(nil, 0); r != css.RepeatBoth {
		t.Errorf("nthRepeat empty = %v", r)
	}
	if r := nthRepeat([]css.BgRepeat{css.NoRepeat}, 4); r != css.NoRepeat {
		t.Errorf("nthRepeat overflow = %v", r)
	}
}

func TestPaintFullNonDrawable(t *testing.T) {
	// nil style and zero-size boxes are non-drawable but must not panic.
	dst := white(4, 4)
	PaintFull(dst, &layout.Box{X: 0, Y: 0, W: 0, H: 0}, NewFonts(), nil, nil)
	PaintFull(dst, &layout.Box{Style: &css.Style{Background: css.Color{A: 255}}, W: 0, H: 0}, NewFonts(), nil, nil)
}

func TestBlitTileClippedBounds(t *testing.T) {
	// A tile stamped partly off the box is clipped to the region; the pixels that
	// land inside paint, those outside are left alone.
	dst := white(10, 10)
	src := solidImage(4, 4, color.RGBA{0, 0, 200, 255})
	blitTileClipped(dst, src, 8, 8, image.Rect(0, 0, 10, 10), 0, dst.Rect)
	if c := dst.RGBAAt(9, 9); c.B < 150 {
		t.Errorf("clipped tile pixel = %+v want blue", c)
	}
	// Stamping fully outside the destination paints nothing and does not panic.
	blitTileClipped(dst, src, 20, 20, image.Rect(0, 0, 10, 10), 0, dst.Rect)
}

func TestBlitTileClippedTransparentPixel(t *testing.T) {
	// A source pixel with alpha 0 is skipped (cov==0 branch).
	dst := white(4, 4)
	src := image.NewRGBA(image.Rect(0, 0, 2, 2)) // all transparent
	blitTileClipped(dst, src, 0, 0, image.Rect(0, 0, 4, 4), 0, dst.Rect)
	if c := dst.RGBAAt(0, 0); c.R != 255 {
		t.Errorf("transparent source pixel painted: %+v", c)
	}
}

func TestBgMode(t *testing.T) {
	if bgMode(nil) != resample.Bicubic {
		t.Error("nil style should default to bicubic")
	}
	if bgMode(&css.Style{ImageRendering: css.IRAuto}) != resample.Bicubic {
		t.Error("IRAuto should be bicubic")
	}
	if bgMode(&css.Style{ImageRendering: css.IRPixelated}) != resample.Nearest {
		t.Error("IRPixelated should be nearest")
	}
}

// TestPaintBgBitmapPixelatedScales exercises the nearest (pixelated) path
// through paintBgBitmap: a 2x2 checker background scaled up to cover a 20x20 box
// must reproduce the source's two distinct colours as solid quadrant blocks.
func TestPaintBgBitmapPixelatedScales(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 2, 2))
	src.Set(0, 0, color.RGBA{200, 0, 0, 255})
	src.Set(1, 0, color.RGBA{0, 0, 200, 255})
	src.Set(0, 1, color.RGBA{0, 0, 200, 255})
	src.Set(1, 1, color.RGBA{200, 0, 0, 255})
	dst := white(20, 20)
	st := urlStyle("x", css.BgSize{Kind: css.SizeCover}, css.BgPosition{}, css.NoRepeat)
	st.ImageRendering = css.IRPixelated
	box := &layout.Box{Node: &dom.Node{Type: dom.Element, Tag: "div"}, Style: st, X: 0, Y: 0, W: 20, H: 20}
	PaintFull(dst, box, NewFonts(), nil, map[string]image.Image{"x": src})
	// Top-left quadrant is the red source pixel, top-right the blue one — nearest
	// keeps them as hard blocks (no blend between them).
	if c := dst.RGBAAt(4, 4); c.R < 150 || c.B > 60 {
		t.Errorf("pixelated top-left = %+v want red", c)
	}
	if c := dst.RGBAAt(15, 4); c.B < 150 || c.R > 60 {
		t.Errorf("pixelated top-right = %+v want blue", c)
	}
}

func TestScaleTileIdentityAndError(t *testing.T) {
	src := solidImage(4, 4, color.RGBA{10, 20, 30, 255})
	// Same size: returned unchanged (the common repeat/auto case).
	if got := scaleTile(src, 4, 4, resample.Bicubic); got != image.Image(src) {
		t.Error("scaleTile at identical size should return src unchanged")
	}
	// A non-positive target makes go-gfx error; scaleTile falls back to src.
	if got := scaleTile(src, 0, 4, resample.Bicubic); got != image.Image(src) {
		t.Error("scaleTile with bad size should fall back to src")
	}
}

// TestPaintDropShadowSharp covers a hard-edged (blur 0) offset shadow: it is
// visible ONLY in the sliver the offset exposes beyond the box's own edge,
// never inside the box's own (un-shifted) footprint — a drop shadow never
// shows through a box's own interior, transparent background or not, per
// spec; the box's own content is what would normally cover it there, but
// that must hold even for a box with no background of its own (this
// engine's earlier behaviour, painting the shadow unconditionally, made a
// small-offset "fake border" box-shadow on a transparent box look like a
// solid fill — confirmed live on developer.mozilla.org's own "In this
// article" table-of-contents links, which use exactly this idiom, and
// cross-checked against a real headless Chrome screenshot of the identical
// markup).
func TestPaintDropShadowSharp(t *testing.T) {
	dst := white(30, 30)
	st := &css.Style{BoxShadows: []css.BoxShadow{{OffsetX: 4, OffsetY: 0, Blur: 0, Color: css.Color{A: 255}}}}
	box := &layout.Box{Node: &dom.Node{Type: dom.Element, Tag: "div"}, Style: st, X: 10, Y: 10, W: 8, H: 8}
	PaintFull(dst, box, NewFonts(), nil, nil)
	if c := dst.RGBAAt(16, 13); c.R != 255 {
		t.Errorf("inside the box's own footprint (also inside the shifted shadow rect) = %+v want white, excluded", c)
	}
	if c := dst.RGBAAt(20, 13); c.R != 0 {
		t.Errorf("sliver the +4 offset exposes beyond the box's own right edge = %+v want black", c)
	}
	if c := dst.RGBAAt(5, 5); c.R != 255 {
		t.Errorf("just outside sharp shadow = %+v want white", c)
	}
}

func TestPaintBgBitmapRoundedClip(t *testing.T) {
	// A url() background in a rounded box is clipped: the extreme corner stays
	// white (exercises blitScaledClipped's rounded-rect test).
	dst := white(40, 40)
	src := solidImage(4, 4, color.RGBA{0, 0, 200, 255})
	st := urlStyle("x", css.BgSize{Kind: css.SizeCover}, css.BgPosition{}, css.RepeatBoth)
	st.BorderRadius = css.Length{Px: 14}
	box := &layout.Box{Node: &dom.Node{Type: dom.Element, Tag: "div"}, Style: st, X: 0, Y: 0, W: 40, H: 40}
	PaintFull(dst, box, NewFonts(), nil, map[string]image.Image{"x": src})
	if c := dst.RGBAAt(0, 0); c.R != 255 || c.G != 255 || c.B != 255 {
		t.Errorf("rounded bg corner = %+v want white", c)
	}
	if c := dst.RGBAAt(20, 20); c.B < 150 {
		t.Errorf("rounded bg centre = %+v want blue", c)
	}
}

func TestPaintInsetShadowRoundedClip(t *testing.T) {
	// An inset shadow in a rounded box: extreme corner is outside the shape.
	dst := white(40, 40)
	st := &css.Style{
		Background:   css.Color{R: 255, G: 255, B: 255, A: 255},
		BorderRadius: css.Length{Px: 12},
		BoxShadows:   []css.BoxShadow{{Blur: 4, Inset: true, Color: css.Color{A: 255}}},
	}
	box := &layout.Box{Node: &dom.Node{Type: dom.Element, Tag: "div"}, Style: st, X: 0, Y: 0, W: 40, H: 40}
	PaintFull(dst, box, NewFonts(), nil, nil)
	if c := dst.RGBAAt(0, 0); c.R != 255 {
		t.Errorf("rounded inset corner = %+v want white", c)
	}
}

func TestPaintInsetShadowClampsCentre(t *testing.T) {
	// A large box with a small blur: the centre's inner coverage saturates to 1
	// (erf clamp), so the inset shadow coverage there is zero and the centre stays
	// the background colour.
	dst := white(80, 80)
	st := &css.Style{
		Background: css.Color{R: 255, G: 255, B: 255, A: 255},
		BoxShadows: []css.BoxShadow{{Blur: 2, Inset: true, Color: css.Color{A: 255}}},
	}
	box := &layout.Box{Node: &dom.Node{Type: dom.Element, Tag: "div"}, Style: st, X: 5, Y: 5, W: 70, H: 70}
	PaintFull(dst, box, NewFonts(), nil, nil)
	if c := dst.RGBAAt(40, 40); c.R != 255 {
		t.Errorf("inset centre = %+v want unshadowed white", c)
	}
}
