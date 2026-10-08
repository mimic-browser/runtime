// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package engine

import "testing"

const rasterFixture = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 10" width="20" height="10">` +
	`<rect x="0" y="0" width="20" height="10" fill="#ff0000"/></svg>`

// RasterizeSVG renders at the size ASKED FOR, not the document's own, which is
// the point: an SVG has no pixels of its own, and a PDF exporter needs the same
// drawing at the paper's density rather than at the canvas's 96 dpi (#229).
func TestRasterizeSVGHonoursTheSizeAsked(t *testing.T) {
	for _, c := range []struct{ w, h int }{{20, 10}, {200, 100}, {1000, 500}} {
		img, ok := RasterizeSVG([]byte(rasterFixture), c.w, c.h, "")
		if !ok || img == nil {
			t.Fatalf("%dx%d: not ok", c.w, c.h)
		}
		if b := img.Bounds(); b.Dx() != c.w || b.Dy() != c.h {
			t.Errorf("asked %dx%d, got %v", c.w, c.h, b)
		}
		// The fill covers the whole viewBox, so the middle pixel is red at
		// every density — a blank bitmap of the right size would pass the
		// bounds check alone.
		r, g, _, a := img.At(c.w/2, c.h/2).RGBA()
		if a>>8 != 255 || r>>8 < 200 || g>>8 > 60 {
			t.Errorf("%dx%d: centre = (%d,%d,%d), want red", c.w, c.h, r>>8, g>>8, a>>8)
		}
	}
}

// Its clamp and its refusals: a non-positive size, source oksvg cannot parse,
// and a size past maxSVGDim.
func TestRasterizeSVGBoundsAndRefusals(t *testing.T) {
	if _, ok := RasterizeSVG([]byte(rasterFixture), 0, 10, ""); ok {
		t.Error("a zero width must not be ok")
	}
	if _, ok := RasterizeSVG([]byte("not an svg at all"), 20, 10, ""); ok {
		t.Error("unparseable source must not be ok")
	}
	img, ok := RasterizeSVG([]byte(rasterFixture), maxSVGDim+500, 10, "")
	if !ok || img == nil {
		t.Fatal("an oversized request should clamp, not fail")
	}
	if img.Bounds().Dx() != maxSVGDim {
		t.Errorf("width %d, want it clamped to %d", img.Bounds().Dx(), maxSVGDim)
	}
}

// currentColor resolves to what the caller names.
func TestRasterizeSVGCurrentColor(t *testing.T) {
	src := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10" width="10" height="10">` +
		`<rect width="10" height="10" fill="currentColor"/></svg>`
	img, ok := RasterizeSVG([]byte(src), 10, 10, "#0000ff")
	if !ok {
		t.Fatal("not ok")
	}
	r, _, b, _ := img.At(5, 5).RGBA()
	if b>>8 < 200 || r>>8 > 60 {
		t.Errorf("centre = (%d,_,%d), want blue", r>>8, b>>8)
	}
}
