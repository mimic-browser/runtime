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

func TestRotateOverhangingImageDoesNotCrash(t *testing.T) {
	dst := white(100, 80)
	imgNode := &dom.Node{Type: dom.Element, Tag: "img"}
	src := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			src.SetRGBA(x, y, color.RGBA{G: 180, A: 255})
		}
	}
	root := &layout.Box{
		Node: &dom.Node{Type: dom.Element, Tag: "div"}, Style: &css.Style{}, W: 100, H: 80,
		Children: []*layout.Box{{
			Node: &dom.Node{Type: dom.Element, Tag: "div"}, Style: &css.Style{RotateDeg: 10}, W: 60, H: 40,
			Children: []*layout.Box{{
				Node: &dom.Node{Type: dom.Element, Tag: "div"}, Style: &css.Style{}, X: 10, Y: 35, W: 20, H: 20,
				Lines: []*layout.LineBox{{X: 10, Y: 35, W: 20, H: 20, Items: []*layout.InlineItem{
					{Image: imgNode, X: 10, Y: 37, ImgW: 4, ImgH: 4, Width: 4, LineHeight: 4},
				}}},
			}},
		}},
	}
	PaintFull(dst, root, NewFonts(), map[*dom.Node]image.Image{imgNode: src}, nil)
}

// TestRotateNinetyDegreesIsClockwise covers the real confirmed trigger
// (round 145): tailwindcss.com's own P3-colours diagonal swatch labels,
// compiled by modern Tailwind to the standalone `rotate` property (not
// `transform: rotate()`). A 100×60 box split left-half red / right-half
// blue, rotated 90°, must come out 60×100 with red on TOP and blue on the
// BOTTOM — CSS's own clockwise-positive convention (left edge -> top edge),
// verified independently against go-images' own counter-clockwise-positive
// Rotate via an isolated four-colour-border repro before writing this test
// (see RotateDeg's own doc comment for why the angle is negated).
func TestRotateNinetyDegreesIsClockwise(t *testing.T) {
	dst := white(140, 140)
	root := &layout.Box{
		Node: &dom.Node{Type: dom.Element, Tag: "div"}, Style: &css.Style{},
		X: 0, Y: 0, W: 140, H: 140,
		Children: []*layout.Box{{
			Node: &dom.Node{Type: dom.Element, Tag: "div"}, Style: &css.Style{RotateDeg: 90},
			X: 20, Y: 20, W: 100, H: 60,
			Children: []*layout.Box{
				{Node: &dom.Node{Type: dom.Element, Tag: "div"}, Style: &css.Style{Background: css.Color{R: 255, A: 255}}, X: 20, Y: 20, W: 50, H: 60},
				{Node: &dom.Node{Type: dom.Element, Tag: "div"}, Style: &css.Style{Background: css.Color{B: 255, A: 255}}, X: 70, Y: 20, W: 50, H: 60},
			},
		}},
	}
	PaintFull(dst, root, NewFonts(), nil, nil)
	// Rotated bounds: 60 wide x 100 tall, centred on the original box's own
	// centre (70,50) -> spans roughly X:[40,100) Y:[0,100). Sampled well
	// inside the top and bottom quarters to stay clear of the bilinear-
	// interpolated seam at the exact centre.
	top := dst.RGBAAt(70, 15)
	bottom := dst.RGBAAt(70, 85)
	if top.R < 200 || top.B > 50 {
		t.Errorf("top of rotated box = %+v, want red (was the LEFT half before a clockwise 90°)", top)
	}
	if bottom.B < 200 || bottom.R > 50 {
		t.Errorf("bottom of rotated box = %+v, want blue (was the RIGHT half before a clockwise 90°)", bottom)
	}
}

// TestRotateZeroIsUnaffected confirms RotateDeg's own zero value (both the
// Go zero value and CSS's initial `rotate:none`/no rotate() at all) takes
// the ordinary, non-rotated paint path — no offscreen buffer, no visible
// difference from a plain box.
func TestRotateZeroIsUnaffected(t *testing.T) {
	dst := white(60, 60)
	root := &layout.Box{
		Node: &dom.Node{Type: dom.Element, Tag: "div"}, Style: &css.Style{},
		X: 0, Y: 0, W: 60, H: 60,
		Children: []*layout.Box{
			{Node: &dom.Node{Type: dom.Element, Tag: "div"}, Style: &css.Style{RotateDeg: 0, Background: css.Color{R: 255, A: 255}}, X: 10, Y: 10, W: 40, H: 40},
		},
	}
	PaintFull(dst, root, NewFonts(), nil, nil)
	if c := dst.RGBAAt(30, 30); c.R < 250 || c.G > 5 || c.B > 5 {
		t.Errorf("centre of an unrotated red box = %+v, want plain solid red", c)
	}
	if c := dst.RGBAAt(5, 5); c.R < 250 || c.G < 250 || c.B < 250 {
		t.Errorf("outside the box = %+v, want the white background untouched", c)
	}
}

// TestRotateOutsideClipIsANoOp mirrors TestBackdropFilterOutsideClipIsANoOp's
// identical guard for a different offscreen-buffer feature: a rotated box
// entirely outside the current clip must return immediately without
// touching dst at all.
func TestRotateOutsideClipIsANoOp(t *testing.T) {
	dst := solid(20, 20, css.Color{R: 255, A: 255})
	box := &layout.Box{
		Node:  &dom.Node{Type: dom.Element, Tag: "div"},
		Style: &css.Style{RotateDeg: 45, Background: css.Color{B: 255, A: 255}},
		X:     100, Y: 100, W: 50, H: 50,
	}
	paintBox(dst, newTestPainter(dst), box, NewFonts(), nil, nil, dst.Rect)
	if c := dst.RGBAAt(10, 10); c.R != 255 || c.G != 0 || c.B != 0 {
		t.Errorf("dst = %+v, want untouched solid red (box entirely outside the canvas)", c)
	}
}

// TestRotateDoesNotCombineWithOpacity confirms the disclosed scope limit in
// css.Style.RotateDeg's own doc comment: rotate takes its own offscreen path
// ONLY when no filter/opacity/mask-image also applies to the same box: an
// element with BOTH a fractional opacity AND a rotation still gets the
// opacity's own group pass (so it is NOT fully opaque), but the rotation
// itself is silently not applied — no confirmed real trigger combines them,
// so this is disclosed rather than engineered around.
func TestRotateDoesNotCombineWithOpacity(t *testing.T) {
	dst := white(100, 100)
	root := &layout.Box{
		Node: &dom.Node{Type: dom.Element, Tag: "div"}, Style: &css.Style{},
		X: 0, Y: 0, W: 100, H: 100,
		Children: []*layout.Box{{
			Node:  &dom.Node{Type: dom.Element, Tag: "div"},
			Style: &css.Style{RotateDeg: 45, HasOpacity: true, Opacity: 0.5, Background: css.Color{R: 255, A: 255}},
			X:     20, Y: 20, W: 60, H: 60,
		}},
	}
	PaintFull(dst, root, NewFonts(), nil, nil)
	// The box's own centre: red-over-white at 50% opacity. Red's own R
	// channel is already 255 and white's is too, so R alone says nothing —
	// G and B are the real discriminator: full-strength red would leave
	// them at 0, a 50% blend toward white's 255 lands around 127.
	c := dst.RGBAAt(50, 50)
	if c.G < 90 || c.G > 165 || c.B < 90 || c.B > 165 {
		t.Errorf("centre = %+v, want G/B around 127 (a real ~50%% opacity blend, proving the opacity group pass ran)", c)
	}
	// A corner of the box's own UNROTATED square (e.g. (22,22), just inside
	// the top-left corner) stays covered — proving no rotation was applied;
	// a genuinely-rotated 45° box would have clipped its own corners away.
	corner := dst.RGBAAt(22, 22)
	if corner.R < 150 {
		t.Errorf("box corner = %+v, want still covered by the (unrotated) box's own square shape", corner)
	}
}

// TestRotateNegativeAngle confirms a negative RotateDeg (CSS's own
// counter-clockwise direction) produces the mirror-image mapping of
// TestRotateNinetyDegreesIsClockwise: the RIGHT edge moves to the TOP
// instead of the LEFT edge.
func TestRotateNegativeAngle(t *testing.T) {
	dst := white(140, 140)
	root := &layout.Box{
		Node: &dom.Node{Type: dom.Element, Tag: "div"}, Style: &css.Style{},
		X: 0, Y: 0, W: 140, H: 140,
		Children: []*layout.Box{{
			Node: &dom.Node{Type: dom.Element, Tag: "div"}, Style: &css.Style{RotateDeg: -90},
			X: 20, Y: 20, W: 100, H: 60,
			Children: []*layout.Box{
				{Node: &dom.Node{Type: dom.Element, Tag: "div"}, Style: &css.Style{Background: css.Color{R: 255, A: 255}}, X: 20, Y: 20, W: 50, H: 60},
				{Node: &dom.Node{Type: dom.Element, Tag: "div"}, Style: &css.Style{Background: css.Color{B: 255, A: 255}}, X: 70, Y: 20, W: 50, H: 60},
			},
		}},
	}
	PaintFull(dst, root, NewFonts(), nil, nil)
	top := dst.RGBAAt(70, 15)
	bottom := dst.RGBAAt(70, 85)
	if top.B < 200 || top.R > 50 {
		t.Errorf("top of -90°-rotated box = %+v, want blue (was the RIGHT half before a counter-clockwise 90°)", top)
	}
	if bottom.R < 200 || bottom.B > 50 {
		t.Errorf("bottom of -90°-rotated box = %+v, want red (was the LEFT half before a counter-clockwise 90°)", bottom)
	}
}
