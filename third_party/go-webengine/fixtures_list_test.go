// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package engine

import (
	"context"
	"image"
	"testing"
)

// gutterInk counts dark pixels in the marker gutter — the column to the left of
// the list content box (which starts at the 40px padding, so the gutter is
// roughly x in [20,39]) — over the whole height.
func gutterInk(img *image.RGBA) int {
	b := img.Bounds()
	n := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := 20; x < 40 && x < b.Max.X; x++ {
			if img.RGBAAt(x, y).R < 128 {
				n++
			}
		}
	}
	return n
}

// TestListMarkersGolden renders a <ul> (discs) and an <ol> (decimals) and proves
// markers paint in the gutter left of the content, then pins the exact bytes.
func TestListMarkersGolden(t *testing.T) {
	img := renderFixture(t, "list_markers.html", 200, 120)
	// The content column starts at the 40px list indent; the list text itself
	// begins at x>=40, so any dark ink in x<40 is a painted marker.
	if ink := gutterInk(img); ink == 0 {
		t.Error("no marker ink painted in the list gutter")
	}
	// Sanity: the very first column (x=0) is white — markers sit in the indent,
	// not at the page edge.
	if c := img.RGBAAt(0, 0); c.R != 255 {
		t.Errorf("page top-left not white: %+v", c)
	}
	checkGolden(t, img, "list_markers.png")
}

// TestListStyleNonePaintsNoMarker proves list-style-type:none paints nothing in
// the gutter, while an otherwise-identical disc list does — same layout, so the
// only pixel difference is the marker.
func TestListStyleNonePaintsNoMarker(t *testing.T) {
	const disc = `<!DOCTYPE html><html><head><style>
	  body{margin:0;font-family:sans-serif;font-size:16px} ul{margin:0}
	</style></head><body><ul><li>Alpha</li><li>Beta</li></ul></body></html>`
	const none = `<!DOCTYPE html><html><head><style>
	  body{margin:0;font-family:sans-serif;font-size:16px} ul{margin:0;list-style-type:none}
	</style></head><body><ul><li>Alpha</li><li>Beta</li></ul></body></html>`

	discImg := renderString(t, disc, 200, 60)
	noneImg := renderString(t, none, 200, 60)

	if gutterInk(discImg) == 0 {
		t.Error("disc list painted no gutter marker")
	}
	if ink := gutterInk(noneImg); ink != 0 {
		t.Errorf("list-style-type:none painted %d gutter pixels, want 0", ink)
	}
}

// renderString renders an inline HTML string offline at the given viewport.
func renderString(t *testing.T, html string, w, h int) *image.RGBA {
	t.Helper()
	img, _, err := New().RenderHTML(context.Background(), html, "https://demo.test/", image.Rect(0, 0, w, h))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	return img
}
