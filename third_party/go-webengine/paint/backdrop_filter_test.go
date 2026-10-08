// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package paint

import (
	"testing"

	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/dom"
	"github.com/go-webengine/engine/layout"
)

// TestBackdropFilterBlursExistingContent covers the real shape found live on
// react.dev's own sticky nav bar: a translucent box with `backdrop-filter:
// blur(...)` sitting over already-painted content, blurring what is BEHIND it
// rather than its own (near-empty) content. Two solid-colour siblings meeting
// at a sharp seam are painted first; a wide, near-transparent overlay with a
// backdrop blur sits on top of both. Without backdrop-filter support the seam
// stays a hard edge; with it, the pixels straddling the seam mix both colours.
func TestBackdropFilterBlursExistingContent(t *testing.T) {
	dst := white(200, 100)
	root := &layout.Box{
		Node:  &dom.Node{Type: dom.Element, Tag: "div"},
		Style: &css.Style{},
		X:     0, Y: 0, W: 200, H: 100,
		Children: []*layout.Box{
			{Node: &dom.Node{Type: dom.Element, Tag: "div"}, Style: &css.Style{Background: css.Color{R: 255, A: 255}}, X: 0, Y: 0, W: 100, H: 100},
			{Node: &dom.Node{Type: dom.Element, Tag: "div"}, Style: &css.Style{Background: css.Color{B: 255, A: 255}}, X: 100, Y: 0, W: 100, H: 100},
			{Node: &dom.Node{Type: dom.Element, Tag: "div"}, Style: &css.Style{
				Background:      css.Color{R: 255, G: 255, B: 255, A: 3},
				BackdropFilters: []css.Filter{{Kind: css.FilterBlur, Amount: 20}},
			}, X: 0, Y: 0, W: 200, H: 100},
		},
	}
	PaintFull(dst, root, NewFonts(), nil, nil)
	c := dst.RGBAAt(100, 50)
	if c.R < 40 || c.B < 40 {
		t.Errorf("seam pixel = %+v, want both red and blue channels visibly mixed by the backdrop blur", c)
	}
}

// TestBackdropFilterOutsideClipIsANoOp covers a backdrop-filter box whose
// border box lies entirely outside the current clip rectangle (e.g. an
// ancestor's overflow:hidden region it has scrolled/positioned out of) — must
// return immediately without touching dst at all, not sample or blur
// anything.
func TestBackdropFilterOutsideClipIsANoOp(t *testing.T) {
	dst := solid(20, 20, css.Color{R: 255, A: 255})
	box := &layout.Box{
		Node: &dom.Node{Type: dom.Element, Tag: "div"},
		Style: &css.Style{
			BackdropFilters: []css.Filter{{Kind: css.FilterBlur, Amount: 5}},
		},
		X: 100, Y: 100, W: 20, H: 20, // entirely outside dst's 20x20 bounds
	}
	before := append([]byte(nil), dst.Pix...)
	applyBackdropFilter(dst, box, dst.Bounds())
	for i := range before {
		if dst.Pix[i] != before[i] {
			t.Fatalf("dst mutated for an off-canvas backdrop-filter box at byte %d", i)
		}
	}
}

// TestNoBackdropFilterLeavesSeamSharp is TestBackdropFilterBlursExistingContent's
// control: the SAME layout without backdrop-filter must still show a sharp
// red/blue seam — guards against a future change that blurs unconditionally.
func TestNoBackdropFilterLeavesSeamSharp(t *testing.T) {
	dst := white(200, 100)
	root := &layout.Box{
		Node:  &dom.Node{Type: dom.Element, Tag: "div"},
		Style: &css.Style{},
		X:     0, Y: 0, W: 200, H: 100,
		Children: []*layout.Box{
			{Node: &dom.Node{Type: dom.Element, Tag: "div"}, Style: &css.Style{Background: css.Color{R: 255, A: 255}}, X: 0, Y: 0, W: 100, H: 100},
			{Node: &dom.Node{Type: dom.Element, Tag: "div"}, Style: &css.Style{Background: css.Color{B: 255, A: 255}}, X: 100, Y: 0, W: 100, H: 100},
		},
	}
	PaintFull(dst, root, NewFonts(), nil, nil)
	left := dst.RGBAAt(99, 50)
	right := dst.RGBAAt(100, 50)
	if left.B != 0 || right.R != 0 {
		t.Errorf("seam should be sharp with no backdrop-filter: left=%+v right=%+v", left, right)
	}
}

// TestBackdropFilterFunctionThatResizesDoesNotPanicOrCorrupt covers
// drop-shadow() used as a backdrop-filter function — an exotic, never
// observed live for backdrop-filter specifically, but real per spec, and the
// one Filter kind whose own doc comment might suggest it resizes its buffer.
// It does not (see dropShadowFilter — always image.NewRGBA(src.Rect)), but
// this guards applyBackdropFilter's own assumption that it never needs to,
// rather than leaving that assumption unverified for the one filter kind a
// future reader would most suspect.
func TestBackdropFilterFunctionThatResizesDoesNotPanicOrCorrupt(t *testing.T) {
	dst := white(40, 40)
	root := &layout.Box{
		Node:  &dom.Node{Type: dom.Element, Tag: "div"},
		Style: &css.Style{},
		X:     0, Y: 0, W: 40, H: 40,
		Children: []*layout.Box{
			{Node: &dom.Node{Type: dom.Element, Tag: "div"}, Style: &css.Style{Background: css.Color{R: 255, A: 255}}, X: 0, Y: 0, W: 40, H: 40},
			{Node: &dom.Node{Type: dom.Element, Tag: "div"}, Style: &css.Style{
				Background:      css.Color{G: 255, A: 3},
				BackdropFilters: []css.Filter{{Kind: css.FilterDropShadow, OffsetX: 5, OffsetY: 5, Blur: 4, Color: css.Color{A: 255}}},
			}, X: 10, Y: 10, W: 20, H: 20},
		},
	}
	PaintFull(dst, root, NewFonts(), nil, nil) // must not panic or corrupt dst's bounds
	if dst.Bounds().Dx() != 40 || dst.Bounds().Dy() != 40 {
		t.Fatalf("dst bounds changed: %v", dst.Bounds())
	}
}

// TestBackdropFilterOwnBackgroundPaintsOnTop confirms compositing ORDER: the
// box's own background still paints on top of the now-blurred backdrop, not
// the other way around — an opaque backdrop-filter box's own colour must win
// outright, exactly as it would with no backdrop-filter at all.
func TestBackdropFilterOwnBackgroundPaintsOnTop(t *testing.T) {
	dst := white(20, 20)
	root := &layout.Box{
		Node:  &dom.Node{Type: dom.Element, Tag: "div"},
		Style: &css.Style{},
		X:     0, Y: 0, W: 20, H: 20,
		Children: []*layout.Box{
			{Node: &dom.Node{Type: dom.Element, Tag: "div"}, Style: &css.Style{Background: css.Color{R: 255, A: 255}}, X: 0, Y: 0, W: 20, H: 20},
			{Node: &dom.Node{Type: dom.Element, Tag: "div"}, Style: &css.Style{
				Background:      css.Color{G: 255, A: 255},
				BackdropFilters: []css.Filter{{Kind: css.FilterBlur, Amount: 5}},
			}, X: 0, Y: 0, W: 20, H: 20},
		},
	}
	PaintFull(dst, root, NewFonts(), nil, nil)
	c := dst.RGBAAt(10, 10)
	if c.R != 0 || c.G != 255 || c.B != 0 || c.A != 255 {
		t.Errorf("own opaque background should win over the blurred backdrop: %+v", c)
	}
}
