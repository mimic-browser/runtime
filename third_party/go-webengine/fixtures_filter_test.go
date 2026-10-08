// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package engine

import "testing"

// TestFilterGolden verifies the CSS `filter` chain end-to-end (parse → cascade →
// paint) with exact painted-pixel invariants on a text-free, solid-colour
// fixture (its bytes are font-independent and byte-golden-safe). Each expected
// value is the analytic CSS Filter Effects result for the tile's source colour.
func TestFilterGolden(t *testing.T) {
	img := renderFixture(t, "filter_golden.html", 320, 160)

	d := func(a, b uint8) int {
		if a > b {
			return int(a - b)
		}
		return int(b - a)
	}
	near := func(x, y int, r, g, b uint8, what string) {
		c := img.RGBAAt(x, y)
		if d(c.R, r) > 1 || d(c.G, g) > 1 || d(c.B, b) > 1 {
			t.Errorf("%s: (%d,%d) = #%02x%02x%02x want ~#%02x%02x%02x", what, x, y, c.R, c.G, c.B, r, g, b)
		}
	}

	// Reference tile: source colour rgb(200,100,50) unfiltered.
	near(30, 30, 200, 100, 50, "reference")
	// brightness(0.5): each channel halved.
	near(80, 30, 100, 50, 25, "brightness 0.5")
	// grayscale(1): luminance 0.2126*200+0.7152*100+0.0722*50 = 117.65 → 118.
	near(130, 30, 118, 118, 118, "grayscale 1")
	// invert(1): 255-channel.
	near(180, 30, 55, 155, 205, "invert 1")
	// contrast(2): (v-128)*2+128 → 200→255, 100→72, 50→0.
	near(230, 30, 255, 72, 0, "contrast 2")
	// sepia(1) on grey 100 → (135,120,94).
	near(280, 30, 135, 120, 94, "sepia 1")

	// Drop-shadow: the black square is on top at its own position.
	near(60, 110, 0, 0, 0, "shadow element black")
	// The hard red shadow shows where the element is offset to but absent
	// (bottom-right of the square: element occupies x[40,80),y[90,130);
	// shadow occupies x[52,92),y[102,142); (86,136) is shadow-only).
	near(86, 136, 255, 0, 0, "drop-shadow red")
	// A point in neither the element nor its shadow stays white.
	near(200, 136, 255, 255, 255, "outside shadow white")

	checkGolden(t, img, "filter_golden.png")
}
