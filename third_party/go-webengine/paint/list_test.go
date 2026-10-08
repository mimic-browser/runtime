// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package paint

import (
	"image"
	"testing"

	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/layout"
)

var markerBlack = css.Color{R: 0, G: 0, B: 0, A: 255}

// paintMarkerOnly renders a bare box carrying just a marker onto a white canvas.
func paintMarkerOnly(w, h int, m *layout.Marker) *image.RGBA {
	dst := white(w, h)
	box := &layout.Box{Style: &css.Style{}, W: float64(w), H: float64(h), Marker: m}
	PaintFull(dst, box, NewFonts(), nil, nil)
	return dst
}

// TestPaintDiscMarker: a disc is a filled circle — solid at the centre, empty at
// the corner (the rounding).
func TestPaintDiscMarker(t *testing.T) {
	img := paintMarkerOnly(40, 40, &layout.Marker{
		Type: css.ListDisc, Style: &css.Style{Color: markerBlack},
		X: 10, Y: 10, W: 6, H: 6,
	})
	if c := img.RGBAAt(12, 12); c.R > 128 {
		t.Errorf("disc centre not filled: %+v", c)
	}
	if c := img.RGBAAt(10, 10); c.R != 255 {
		t.Errorf("disc corner should be empty (rounded), got %+v", c)
	}
	// Nothing paints left of the glyph box.
	if c := img.RGBAAt(5, 12); c.R != 255 {
		t.Errorf("pixel left of disc not white: %+v", c)
	}
}

// TestPaintSquareMarker: a square fills its whole box, corners included.
func TestPaintSquareMarker(t *testing.T) {
	img := paintMarkerOnly(40, 40, &layout.Marker{
		Type: css.ListSquare, Style: &css.Style{Color: markerBlack},
		X: 10, Y: 10, W: 6, H: 6,
	})
	if c := img.RGBAAt(10, 10); c.R > 128 {
		t.Errorf("square corner not filled: %+v", c)
	}
	if c := img.RGBAAt(14, 14); c.R > 128 {
		t.Errorf("square interior not filled: %+v", c)
	}
	if c := img.RGBAAt(16, 16); c.R != 255 {
		t.Errorf("outside square should be white: %+v", c)
	}
}

// TestPaintCircleMarker: a hollow circle is empty at the centre, stroked on the
// ring.
func TestPaintCircleMarker(t *testing.T) {
	img := paintMarkerOnly(40, 40, &layout.Marker{
		Type: css.ListCircle, Style: &css.Style{Color: markerBlack},
		X: 10, Y: 10, W: 10, H: 10,
	})
	if c := img.RGBAAt(15, 15); c.R != 255 {
		t.Errorf("circle centre should be hollow (white), got %+v", c)
	}
	if c := img.RGBAAt(10, 15); c.R > 200 {
		t.Errorf("circle ring not stroked at left edge: %+v", c)
	}
}

// TestPaintDecimalMarker: a decimal marker paints its ordinal text; ink appears
// only within the marker's pen box, never to its left.
func TestPaintDecimalMarker(t *testing.T) {
	img := paintMarkerOnly(48, 24, &layout.Marker{
		Type: css.ListDecimal, Text: "1.",
		Style:  &css.Style{FontFamily: css.Sans, FontSize: 16, FontWeight: 400, Color: markerBlack},
		X:      12, Y: 2, Ascent: 12,
	})
	// Some ink lands in the glyph region.
	ink := 0
	for y := 0; y < 24; y++ {
		for x := 12; x < 48; x++ {
			if img.RGBAAt(x, y).R < 128 {
				ink++
			}
		}
	}
	if ink == 0 {
		t.Error("decimal marker painted no glyph pixels")
	}
	// Nothing paints left of the pen origin (x < 12).
	for y := 0; y < 24; y++ {
		for x := 0; x < 12; x++ {
			if c := img.RGBAAt(x, y); c.R != 255 {
				t.Fatalf("ink left of the pen at (%d,%d): %+v", x, y, c)
			}
		}
	}
}
