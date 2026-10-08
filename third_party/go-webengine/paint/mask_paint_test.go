// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package paint

import (
	"image"
	"image/color"
	"testing"

	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/dom"
	"github.com/go-webengine/engine/layout"
)

// halfMask returns a w×h alpha stencil: left half fully opaque, right half
// fully transparent — a stand-in for a real icon's shape.
func halfMask(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if x < w/2 {
				img.SetRGBA(x, y, color.RGBA{0, 0, 0, 255})
			} else {
				img.SetRGBA(x, y, color.RGBA{0, 0, 0, 0})
			}
		}
	}
	return img
}

func maskStyle(bg css.Color, maskURL string) *css.Style {
	return &css.Style{Background: bg, MaskImage: maskURL}
}

// TestPaintMaskImageStencilsBackground covers the real regression:
// `mask-image` was entirely unimplemented, so an empty <span> icon cut into
// shape by a mask (Wikipedia's Vector-2022 skin renders every toolbar icon
// this way) rendered as a plain solid-coloured square instead of the icon's
// real shape.
func TestPaintMaskImageStencilsBackground(t *testing.T) {
	dst := white(20, 20)
	mask := halfMask(20, 20)
	box := &layout.Box{
		Node:  &dom.Node{Type: dom.Element, Tag: "span"},
		Style: maskStyle(css.Color{R: 0, G: 100, B: 200, A: 255}, "m"),
		X:     0, Y: 0, W: 20, H: 20,
	}
	PaintFull(dst, box, NewFonts(), nil, map[string]image.Image{"m": mask})
	if c := dst.RGBAAt(5, 10); c.B < 150 {
		t.Errorf("left (masked-in) = %+v, want the background colour to show", c)
	}
	if c := dst.RGBAAt(15, 10); c.R != 255 || c.G != 255 || c.B != 255 {
		t.Errorf("right (masked-out) = %+v, want white (untouched)", c)
	}
}

// TestPaintMaskImageStretchesToFillBox covers this engine's one deliberate
// simplification of mask sizing (see css.Style.MaskImage's doc comment): the
// mask is stretched to fill the box's own border box, not sized/positioned
// per the real mask-size/mask-position grammar.
func TestPaintMaskImageStretchesToFillBox(t *testing.T) {
	dst := white(40, 10)
	mask := halfMask(4, 4) // much smaller than the box; must still stretch to fill it
	box := &layout.Box{
		Node:  &dom.Node{Type: dom.Element, Tag: "span"},
		Style: maskStyle(css.Color{R: 0, G: 200, B: 0, A: 255}, "m"),
		X:     0, Y: 0, W: 40, H: 10,
	}
	PaintFull(dst, box, NewFonts(), nil, map[string]image.Image{"m": mask})
	if c := dst.RGBAAt(5, 5); c.G < 150 {
		t.Errorf("left quarter (masked-in) = %+v, want the background colour", c)
	}
	if c := dst.RGBAAt(35, 5); c.R != 255 || c.G != 255 || c.B != 255 {
		t.Errorf("right half (masked-out) = %+v, want white (untouched)", c)
	}
}

// TestPaintMaskImageMissingFallsBackToPlainPaint covers hasMask's own guard:
// a mask-image URL not present in bgImgs (still fetching, or the fetch
// failed) must NOT block the element's normal paint — it should render as if
// mask-image were absent, not as fully masked-out.
func TestPaintMaskImageMissingFallsBackToPlainPaint(t *testing.T) {
	dst := white(10, 10)
	box := &layout.Box{
		Node:  &dom.Node{Type: dom.Element, Tag: "span"},
		Style: maskStyle(css.Color{R: 255, A: 255}, "missing"),
		X:     0, Y: 0, W: 10, H: 10,
	}
	PaintFull(dst, box, NewFonts(), nil, map[string]image.Image{})
	if c := dst.RGBAAt(5, 5); c.R != 255 || c.G != 0 || c.B != 0 {
		t.Errorf("missing mask image should not block the normal background paint, got %+v", c)
	}
}

// TestPaintMaskImageOutsideBorderBoxFullyMasked covers that a mask affects
// the WHOLE element — including anything a child paints outside the masked
// element's own border box — not just its own background layer.
func TestPaintMaskImageOutsideBorderBoxFullyMasked(t *testing.T) {
	dst := white(20, 20)
	mask := halfMask(10, 10)
	parent := &layout.Box{
		Node:  &dom.Node{Type: dom.Element, Tag: "span"},
		Style: maskStyle(css.Color{A: 0}, "m"), // transparent itself; only the child paints
		X:     5, Y: 5, W: 10, H: 10,
		Children: []*layout.Box{{
			Node:  &dom.Node{Type: dom.Element, Tag: "div"},
			Style: &css.Style{Background: css.Color{R: 0, G: 200, B: 0, A: 255}},
			X:     0, Y: 7, W: 4, H: 4, // outside the parent's border box (parent starts at X=5)
		}},
	}
	PaintFull(dst, parent, NewFonts(), nil, map[string]image.Image{"m": mask})
	if c := dst.RGBAAt(2, 9); c.R != 255 || c.G != 255 || c.B != 255 {
		t.Errorf("content painted outside the masked element's border box should be fully masked out, got %+v", c)
	}
}

// TestPaintMaskImageZeroSizedBox covers applyMask's degenerate-box guard: a
// zero-sized element has no border box to stretch a mask into, so it must be
// fully masked out rather than divide by zero or paint unmasked.
func TestPaintMaskImageZeroSizedBox(t *testing.T) {
	dst := white(10, 10)
	mask := halfMask(4, 4)
	box := &layout.Box{
		Node:  &dom.Node{Type: dom.Element, Tag: "span"},
		Style: maskStyle(css.Color{R: 255, A: 255}, "m"),
		X:     2, Y: 2, W: 0, H: 0,
	}
	PaintFull(dst, box, NewFonts(), nil, map[string]image.Image{"m": mask})
	if c := dst.RGBAAt(2, 2); c.R != 255 || c.G != 255 || c.B != 255 {
		t.Errorf("zero-sized masked box should paint nothing, got %+v", c)
	}
}

// TestPaintMaskImageZeroWidthBoxWithOverflowingChild covers the same
// zero-border-box guard as above, but where the group buffer is NOT itself
// empty (paintBox's own y1<=y0 early-out never fires) — a zero-WIDTH box
// whose child still overflows it and paints something. This is the only way
// to actually reach applyMask's clearAlpha path: the box above (0×0, no
// children) never even gets a group buffer, since its own ink bounds are
// empty.
func TestPaintMaskImageZeroWidthBoxWithOverflowingChild(t *testing.T) {
	dst := white(10, 10)
	mask := halfMask(4, 4)
	parent := &layout.Box{
		Node:  &dom.Node{Type: dom.Element, Tag: "span"},
		Style: maskStyle(css.Color{A: 0}, "m"),
		X:     5, Y: 0, W: 0, H: 10,
		Children: []*layout.Box{{
			Node:  &dom.Node{Type: dom.Element, Tag: "div"},
			Style: &css.Style{Background: css.Color{R: 255, A: 255}},
			X:     2, Y: 2, W: 6, H: 6,
		}},
	}
	PaintFull(dst, parent, NewFonts(), nil, map[string]image.Image{"m": mask})
	if c := dst.RGBAAt(5, 5); c.R != 255 || c.G != 255 || c.B != 255 {
		t.Errorf("a mask with no border box to stretch into should mask out the whole element, got %+v", c)
	}
}
