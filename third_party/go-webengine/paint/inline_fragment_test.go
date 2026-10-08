// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package paint

import (
	"image"
	"testing"

	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/dom"
	"github.com/go-webengine/engine/layout"
)

// frag builds a line carrying one inline fragment over one word, the shape
// layout produces for `<span style="…">AA</span>` inside a <p>.
func frag(st *css.Style, x, y, w, h float64, first, last bool) *layout.LineBox {
	span := &dom.Node{Type: dom.Element, Tag: "span"}
	it := &layout.InlineItem{Text: "AA", Style: st, Node: span, X: x + 2, Y: y + 2, Width: 20, Ascent: 12, LineHeight: 20}
	return &layout.LineBox{
		X: 0, Y: y, W: 200, H: 20,
		Items:   []*layout.InlineItem{it},
		Inlines: []layout.InlineFragment{{Node: span, Style: st, X: x, Y: y, W: w, H: h, First: first, Last: last}},
	}
}

func paintLine(dst *image.RGBA, line *layout.LineBox) {
	p := &dom.Node{Type: dom.Element, Tag: "p"}
	box := &layout.Box{Node: p, Style: &css.Style{}, X: 0, Y: 0, W: 200, H: 60, Lines: []*layout.LineBox{line}}
	PaintFull(dst, box, NewFonts(), nil, nil)
}

// TestPaintInlineFragmentBackground: an inline-level element owns no block box,
// so its background must be painted from its line fragment. Without it, the
// white-on-a-coloured-label pattern lands as white-on-white and the words
// vanish entirely. The fill covers the WHOLE fragment — the internal space
// between two words of the same element included — because layout already
// merged them into one fragment.
func TestPaintInlineFragmentBackground(t *testing.T) {
	dst := white(120, 40)
	blue := css.Color{R: 0x15, G: 0x65, B: 0xc0, A: 0xff}
	st := &css.Style{Background: blue, Color: css.Color{R: 0xff, G: 0xff, B: 0xff, A: 0xff}}
	paintLine(dst, frag(st, 10, 5, 46, 20, true, true))

	for _, x := range []int{11, 33, 54} {
		if c := dst.RGBAAt(x, 12); c.B < 150 || c.R > 80 {
			t.Errorf("inline bg at x=%d = %+v, want blue", x, c)
		}
	}
	// Bounded left and above: nothing painted outside the fragment box.
	if c := dst.RGBAAt(2, 12); c.R != 255 || c.G != 255 || c.B != 255 {
		t.Errorf("left of the fragment = %+v, want white", c)
	}
	if c := dst.RGBAAt(20, 2); c.R != 255 || c.G != 255 || c.B != 255 {
		t.Errorf("above the fragment = %+v, want white", c)
	}
}

// TestPaintInlineFragmentBorderSlice: box-decoration-break: slice, the CSS
// default. The LEFT border paints only on the element's first fragment and the
// RIGHT border only on its last; top and bottom paint on every one. A fragment
// that is neither (a middle line of a three-line span) gets no vertical edge at
// all, which is what stops a wrapped span from looking like a row of separate
// boxes.
func TestPaintInlineFragmentBorderSlice(t *testing.T) {
	red := css.Color{R: 0xff, A: 0xff}
	side := css.BorderSide{Width: 2, Style: css.BorderSolid, Color: red}
	st := &css.Style{Border: css.Borders{Top: side, Right: side, Bottom: side, Left: side}}
	isRed := func(dst *image.RGBA, x, y int) bool {
		c := dst.RGBAAt(x, y)
		return c.R > 200 && c.G < 80 && c.B < 80
	}

	// First fragment: left edge painted, right edge NOT (it continues).
	dst := white(120, 40)
	paintLine(dst, frag(st, 10, 5, 40, 20, true, false))
	if !isRed(dst, 10, 15) {
		t.Errorf("first fragment: left border missing")
	}
	if isRed(dst, 49, 15) {
		t.Errorf("first fragment: right border painted, but the element continues")
	}
	if !isRed(dst, 30, 5) || !isRed(dst, 30, 24) {
		t.Errorf("first fragment: top/bottom border missing")
	}

	// Last fragment: right edge painted, left edge NOT (it continued into here).
	dst = white(120, 40)
	paintLine(dst, frag(st, 10, 5, 40, 20, false, true))
	if isRed(dst, 10, 15) {
		t.Errorf("last fragment: left border painted, but the element continued into it")
	}
	if !isRed(dst, 49, 15) {
		t.Errorf("last fragment: right border missing")
	}

	// Middle fragment: neither vertical edge, both horizontal ones.
	dst = white(120, 40)
	paintLine(dst, frag(st, 10, 5, 40, 20, false, false))
	if isRed(dst, 10, 15) || isRed(dst, 49, 15) {
		t.Errorf("middle fragment: a vertical border painted")
	}
	if !isRed(dst, 30, 5) {
		t.Errorf("middle fragment: top border missing")
	}
}

// TestPaintInlineFragmentRounded: a complete (First AND Last) fragment with a
// uniform border and a border-radius is the "pill" pattern; it takes the same
// single rounded-rect stroke a block box does, and its background is filled to
// the rounded shape rather than square.
func TestPaintInlineFragmentRounded(t *testing.T) {
	dst := white(120, 40)
	green := css.Color{G: 0x80, A: 0xff}
	side := css.BorderSide{Width: 2, Style: css.BorderSolid, Color: css.Color{R: 0xff, A: 0xff}}
	st := &css.Style{
		Background:   green,
		Border:       css.Borders{Top: side, Right: side, Bottom: side, Left: side},
		BorderRadius: css.Length{Px: 8},
	}
	paintLine(dst, frag(st, 10, 5, 46, 20, true, true))
	// Centre of the pill is the background colour.
	if c := dst.RGBAAt(33, 15); c.G < 100 || c.R > 90 {
		t.Errorf("rounded fragment centre = %+v, want green", c)
	}
	// The top-left CORNER is outside the rounded shape, so it stays white —
	// the proof the fill followed the radius instead of squaring it off.
	if c := dst.RGBAAt(10, 5); c.R != 255 || c.G != 255 || c.B != 255 {
		t.Errorf("rounded fragment corner = %+v, want untouched white", c)
	}
}

// TestPaintInlineFragmentDegenerate: a fragment with no style, or one with no
// area (an empty run, or a zero-height line), paints nothing and must not panic.
func TestPaintInlineFragmentDegenerate(t *testing.T) {
	dst := white(40, 20)
	span := &dom.Node{Type: dom.Element, Tag: "span"}
	blue := css.Color{B: 0xff, A: 0xff}
	line := &layout.LineBox{X: 0, Y: 0, W: 40, H: 10, Inlines: []layout.InlineFragment{
		{Node: span, Style: nil, X: 0, Y: 0, W: 10, H: 10, First: true, Last: true},
		{Node: span, Style: &css.Style{Background: blue}, X: 10, Y: 0, W: 0, H: 10, First: true, Last: true},
		{Node: span, Style: &css.Style{Background: blue}, X: 20, Y: 0, W: 10, H: 0, First: true, Last: true},
	}}
	paintLine(dst, line)
	for x := 0; x < 40; x++ {
		if c := dst.RGBAAt(x, 5); c.R != 255 || c.G != 255 || c.B != 255 {
			t.Fatalf("degenerate fragment painted at x=%d: %+v", x, c)
		}
	}
}
