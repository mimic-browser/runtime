// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package paint

import (
	"testing"

	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/dom"
	"github.com/go-webengine/engine/layout"
)

// TestPaintNestedBoxRecursesIntoPaintBox covers InlineItem.NestedBox's own
// paint path — an inline-flex element (see layout.appendElementInline),
// distinct from Image/FormControl in that it carries a REAL Box tree rather
// than opaque pixels or a drawn control. paintItem must recurse back into
// paintBox for it, painting its own background, borders and children exactly
// like any other box (including going through the SAME filter/opacity
// group-buffer handling a normal box gets, not a special case here).
func TestPaintNestedBoxRecursesIntoPaintBox(t *testing.T) {
	dst := white(30, 30)
	nestedStyle := &css.Style{Background: css.Color{R: 0, G: 0x80, B: 0, A: 0xff}}
	nested := &layout.Box{
		Node: &dom.Node{Type: dom.Element, Tag: "b"}, Style: nestedStyle,
		X: 5, Y: 5, W: 10, H: 10,
	}
	it := &layout.InlineItem{NestedBox: nested, X: 5, Y: 5, Width: 10, Ascent: 10, LineHeight: 10}
	line := &layout.LineBox{X: 0, Y: 0, W: 30, H: 10, Items: []*layout.InlineItem{it}}
	pStyle := &css.Style{}
	box := &layout.Box{Node: &dom.Node{Type: dom.Element, Tag: "p"}, Style: pStyle,
		X: 0, Y: 0, W: 30, H: 30, Lines: []*layout.LineBox{line}}
	PaintFull(dst, box, NewFonts(), nil, nil)

	if c := dst.RGBAAt(10, 10); c.G < 100 || c.R > 80 {
		t.Errorf("nested box background = %+v, want green", c)
	}
	// Outside the nested box's bounds stays white.
	if c := dst.RGBAAt(20, 20); c.R != 255 || c.G != 255 || c.B != 255 {
		t.Errorf("outside nested box = %+v, want white", c)
	}
}
