// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package css

import (
	"testing"

	gfxcolor "github.com/go-gfx/gfx/color"
)

// oldHSLToRGB is the pre-migration, in-package HSL->8-bit-RGB conversion,
// reproduced verbatim (including its private absF/mathFloor helpers, inlined
// here) so the go-gfx-backed replacement can be proven byte-identical rather
// than compared against a variant of the new code.
func oldHSLToRGB(h, s, l float64) (uint8, uint8, uint8) {
	absF := func(f float64) float64 {
		if f < 0 {
			return -f
		}
		return f
	}
	mathFloor := func(f float64) float64 {
		i := float64(int64(f))
		if f < 0 && i != f {
			i--
		}
		return i
	}
	h = h - 360*mathFloor(h/360) // normalise into [0,360)
	c := (1 - absF(2*l-1)) * s
	hp := h / 60
	x := c * (1 - absF(hp-2*mathFloor(hp/2)-1))
	var r1, g1, b1 float64
	switch {
	case hp < 1:
		r1, g1, b1 = c, x, 0
	case hp < 2:
		r1, g1, b1 = x, c, 0
	case hp < 3:
		r1, g1, b1 = 0, c, x
	case hp < 4:
		r1, g1, b1 = 0, x, c
	case hp < 5:
		r1, g1, b1 = x, 0, c
	default:
		r1, g1, b1 = c, 0, x
	}
	m := l - c/2
	return clampByte((r1 + m) * 255), clampByte((g1 + m) * 255), clampByte((b1 + m) * 255)
}

// newHSLToRGB is the exact production expression now used by parseHSLChannels.
func newHSLToRGB(h, s, l float64) (uint8, uint8, uint8) {
	rf, gf, bf := gfxcolor.HSLToSRGB(gfxcolor.HSL{H: h, S: s, L: l})
	return clampByte(rf * 255), clampByte(gf * 255), clampByte(bf * 255)
}

// TestHSLToSRGBParityWithReplacedCode sweeps HSL space and asserts the go-gfx
// path yields byte-for-byte the same 8-bit RGB the replaced local code did — the
// invariance proof for routing HSL conversion through the shared colour layer.
func TestHSLToSRGBParityWithReplacedCode(t *testing.T) {
	// Boundary hues that stress hue-sextant selection and the normalisation of
	// out-of-range angles (negative, >=360, and multiples of 360).
	edgeHues := []float64{-360, -180, -30, -0.001, 0, 0.001, 59.999, 60, 120, 180,
		240, 300, 359.999, 360, 360.001, 420, 720}
	for _, h := range edgeHues {
		for si := 0; si <= 20; si++ {
			s := float64(si) / 20
			for li := 0; li <= 20; li++ {
				l := float64(li) / 20
				or, og, ob := oldHSLToRGB(h, s, l)
				nr, ng, nb := newHSLToRGB(h, s, l)
				if or != nr || og != ng || ob != nb {
					t.Fatalf("hsl(%g,%g,%g): old=(%d,%d,%d) new=(%d,%d,%d)",
						h, s, l, or, og, ob, nr, ng, nb)
				}
			}
		}
	}
	// A dense in-range sweep over every degree.
	for h := 0.0; h < 360; h += 1 {
		for si := 0; si <= 10; si++ {
			s := float64(si) / 10
			for li := 0; li <= 10; li++ {
				l := float64(li) / 10
				or, og, ob := oldHSLToRGB(h, s, l)
				nr, ng, nb := newHSLToRGB(h, s, l)
				if or != nr || og != ng || ob != nb {
					t.Fatalf("hsl(%g,%g,%g): old=(%d,%d,%d) new=(%d,%d,%d)",
						h, s, l, or, og, ob, nr, ng, nb)
				}
			}
		}
	}
}

// TestParseHSLStillParses guards that the parsing path (which stays local) keeps
// producing the same colours end-to-end through the new conversion.
func TestParseHSLStillParses(t *testing.T) {
	cases := []struct {
		in   string
		want Color
	}{
		{"hsl(0, 100%, 50%)", Color{255, 0, 0, 255}},
		{"hsl(120, 100%, 50%)", Color{0, 255, 0, 255}},
		{"hsl(240 100% 50%)", Color{0, 0, 255, 255}},
		{"hsl(0, 0%, 100%)", Color{255, 255, 255, 255}},
		{"hsla(0, 0%, 0%, 0.5)", Color{0, 0, 0, 128}},
	}
	for _, c := range cases {
		got, ok := parseColor(c.in)
		if !ok || got != c.want {
			t.Errorf("parseColor(%q) = %v,%v want %v", c.in, got, ok, c.want)
		}
	}
}
