// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package engine

import "testing"

// TestGradientsGolden verifies gradients, box-shadow, border-radius clipping,
// opacity and a url() background-image with exact painted-pixel invariants on a
// text-free fixture (its bytes are deterministic and byte-golden-safe).
func TestGradientsGolden(t *testing.T) {
	img := renderFixture(t, "gradients_golden.html", 200, 260)

	d := func(a, b uint8) int {
		if a > b {
			return int(a - b)
		}
		return int(b - a)
	}
	near := func(x, y int, r, g, b uint8, tol int, what string) {
		c := img.RGBAAt(x, y)
		if d(c.R, r) > tol || d(c.G, g) > tol || d(c.B, b) > tol {
			t.Errorf("%s: (%d,%d) = #%02x%02x%02x want ~#%02x%02x%02x (tol %d)", what, x, y, c.R, c.G, c.B, r, g, b, tol)
		}
	}

	// 1. Linear gradient to right, black→white: start black, mid grey, end white.
	near(1, 30, 0, 0, 0, 6, "linear start black")
	near(100, 30, 128, 128, 128, 8, "linear mid grey")
	near(198, 30, 255, 255, 255, 6, "linear end white")

	// 2. Radial circle green→blue: centre green, edge blue.
	near(100, 90, 0, 255, 0, 24, "radial centre green")
	near(2, 90, 0, 0, 255, 40, "radial edge blue")

	// 3. Sharp box-shadow (0 0 0 4px red): a red halo hugs the white box; the box
	//    interior stays white and a point beyond the 4px spread is clean white.
	near(17, 160, 255, 0, 0, 6, "box-shadow red halo")
	near(50, 160, 255, 255, 255, 6, "box interior white")
	near(10, 160, 255, 255, 255, 6, "beyond shadow spread white")

	// 4. Rounded background (radius 20 on a 60x40 box): extreme corner clipped
	//    (white), centre filled with #3366cc.
	near(121, 141, 255, 255, 255, 4, "rounded corner clipped")
	near(150, 160, 0x33, 0x66, 0xcc, 6, "rounded centre fill")

	// 5. Opacity 0.5 black over white → mid grey.
	near(50, 220, 128, 128, 128, 6, "opacity 0.5 grey")

	// 6. url() background-image (magenta 2x2) covering the box.
	near(150, 220, 0xcc, 0x22, 0x99, 6, "background-image magenta")

	checkGolden(t, img, "gradients_golden.png")
}
