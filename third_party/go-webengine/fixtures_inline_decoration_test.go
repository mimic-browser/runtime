// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package engine

import "testing"

// TestInlineDecorationGolden verifies the FINAL rendered pixels of an
// inline-level element's own box decoration — background, padding and border
// painted per LINE BOX, which is what CSS calls the fragments of an inline box.
// Before this, layout modelled no box at all for an inline element: it reserved
// no space for its padding/border and paint drew none of it.
//
// `.pill` is a `<span>` with background #0000ff, a 6px #ff0000 border and
// `padding: 0 20px`, wrapping across two lines inside a 320px block; a
// `.pill b` with its own #00cc00 background sits on the second line; `.plain`
// is an undecorated `<span>` that must paint nothing at all.
//
// Vertical geometry (line-height 60px, #wrap padding-top 12px, so line 1 tops
// at y=12 and line 2 at y=72; the 6px border overflows each line box by 6px
// above and below, which is CSS — a line is NOT grown by an inline box's
// vertical border/padding):
//
//	y=  8  fragment 1's top border band      (12 − 6 .. 12)
//	y= 65  inside fragment 1, clear of glyphs
//	y= 90  inside fragment 2, clear of glyphs
//	y=135  fragment 2's bottom border band   (132 .. 72+60+6)
//
// Solid colour bands only → font-independent bytes.
func TestInlineDecorationGolden(t *testing.T) {
	img := renderFixture(t, "inline_decoration.html", 400, 200)

	const (
		redR, redG, redB    = 0xff, 0x00, 0x00
		blueR, blueG, blueB = 0x00, 0x00, 0xff
		grnR, grnG, grnB    = 0x00, 0xcc, 0x00
	)

	// --- Fragment 1 (line 1) -------------------------------------------------
	// Its top border paints across the whole fragment...
	assertPixel(t, img, 3, 8, redR, redG, redB, "fragment 1 top border (left end)")
	assertPixel(t, img, 290, 8, redR, redG, redB, "fragment 1 top border (right end)")
	// ...and the LEFT border paints, because this is the element's FIRST
	// fragment (box-decoration-break: slice).
	assertPixel(t, img, 2, 65, redR, redG, redB, "fragment 1 left border")
	// The background fills behind the text, including the 20px left padding.
	assertPixel(t, img, 10, 65, blueR, blueG, blueB, "fragment 1 background (left padding)")
	assertPixel(t, img, 290, 65, blueR, blueG, blueB, "fragment 1 background (right end)")
	// No RIGHT border here: the element continues onto the next line, so its
	// right edge belongs to the LAST fragment only. Nothing is painted past
	// the fragment's own end either.
	assertPixel(t, img, 299, 65, 255, 255, 255, "no right border on fragment 1 — it continues")
	assertPixel(t, img, 340, 65, 255, 255, 255, "nothing painted beyond fragment 1")

	// --- Fragment 2 (line 2) -------------------------------------------------
	// The background is present on the SECOND line too, starting flush at the
	// line's left edge: no left border and no re-applied left padding, because
	// this fragment is a continuation, not the element's first.
	assertPixel(t, img, 2, 90, blueR, blueG, blueB, "fragment 2 background, no left border")
	assertPixel(t, img, 200, 90, blueR, blueG, blueB, "fragment 2 background")
	// The nested <b>'s own background paints OVER its ancestor's, not under it.
	assertPixel(t, img, 150, 90, grnR, grnG, grnB, "nested <b> background over the span's")
	// The RIGHT border paints here, because this IS the element's last fragment.
	assertPixel(t, img, 246, 90, redR, redG, redB, "fragment 2 right border")
	assertPixel(t, img, 253, 90, 255, 255, 255, "nothing painted past fragment 2")
	// Its bottom border runs the fragment's whole width.
	assertPixel(t, img, 3, 135, redR, redG, redB, "fragment 2 bottom border (left end)")
	assertPixel(t, img, 246, 135, redR, redG, redB, "fragment 2 bottom border (right end)")

	// --- An undecorated inline element paints nothing ------------------------
	// `.plain` is a bare <span> (UA-default <code>/<a> are the same shape: no
	// background, no border, no padding), so its rows carry glyphs and white
	// only — none of the three decoration colours anywhere.
	for y := 145; y < 200; y++ {
		for x := 0; x < 400; x++ {
			c := img.RGBAAt(x, y)
			if (c.R == redR && c.G == redG && c.B == redB) ||
				(c.R == blueR && c.G == blueG && c.B == blueB) ||
				(c.R == grnR && c.G == grnG && c.B == grnB) {
				t.Fatalf("undecorated span painted decoration at (%d,%d): %+v", x, y, c)
			}
		}
	}

	checkGolden(t, img, "inline_decoration.png")
}
