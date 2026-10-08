// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package paint

import (
	"image"
	"math"
	"testing"

	gfxcolor "github.com/go-gfx/gfx/color"
)

// The colour-matrix builders below are the pre-migration, in-package
// implementations, reproduced verbatim so the go-gfx-backed replacement is
// proven byte-identical against the code it replaced (not a variant of the new
// code). oldApplyMatrix is likewise the previous pixel loop.

func oldBrightnessMatrix(a float64) [3][4]float64 {
	return [3][4]float64{{a, 0, 0, 0}, {0, a, 0, 0}, {0, 0, a, 0}}
}

func oldInvertMatrix(a float64) [3][4]float64 {
	d := 1 - 2*a
	return [3][4]float64{{d, 0, 0, a}, {0, d, 0, a}, {0, 0, d, a}}
}

func oldSaturateMatrix(s, lr, lg, lb float64) [3][4]float64 {
	return [3][4]float64{
		{lr + s*(1-lr), lg - s*lg, lb - s*lb, 0},
		{lr - s*lr, lg + s*(1-lg), lb - s*lb, 0},
		{lr - s*lr, lg - s*lg, lb + s*(1-lb), 0},
	}
}

func oldSepiaMatrix(a float64) [3][4]float64 {
	t := 1 - a
	return [3][4]float64{
		{0.393 + 0.607*t, 0.769 - 0.769*t, 0.189 - 0.189*t, 0},
		{0.349 - 0.349*t, 0.686 + 0.314*t, 0.168 - 0.168*t, 0},
		{0.272 - 0.272*t, 0.534 - 0.534*t, 0.131 + 0.869*t, 0},
	}
}

func oldHueRotateMatrix(rad float64) [3][4]float64 {
	c := math.Cos(rad)
	s := math.Sin(rad)
	return [3][4]float64{
		{0.213 + c*0.787 - s*0.213, 0.715 - c*0.715 - s*0.715, 0.072 - c*0.072 + s*0.928, 0},
		{0.213 - c*0.213 + s*0.143, 0.715 + c*0.285 + s*0.140, 0.072 - c*0.072 - s*0.283, 0},
		{0.213 - c*0.213 - s*0.787, 0.715 - c*0.715 + s*0.715, 0.072 + c*0.928 + s*0.072, 0},
	}
}

func oldApplyMatrix(src *image.RGBA, m [3][4]float64) *image.RGBA {
	dst := image.NewRGBA(src.Rect)
	for i := 0; i < len(src.Pix); i += 4 {
		r := float64(src.Pix[i])
		g := float64(src.Pix[i+1])
		b := float64(src.Pix[i+2])
		dst.Pix[i] = clamp8(m[0][0]*r + m[0][1]*g + m[0][2]*b + m[0][3]*255)
		dst.Pix[i+1] = clamp8(m[1][0]*r + m[1][1]*g + m[1][2]*b + m[1][3]*255)
		dst.Pix[i+2] = clamp8(m[2][0]*r + m[2][1]*g + m[2][2]*b + m[2][3]*255)
		dst.Pix[i+3] = src.Pix[i+3]
	}
	return dst
}

// syntheticImage builds an RGBA buffer whose colour and alpha channels vary
// independently, covering the whole 0..255 range and translucency.
func syntheticImage() *image.RGBA {
	const w, h = 32, 32
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := img.PixOffset(x, y)
			img.Pix[i] = uint8((x * 255) / (w - 1))
			img.Pix[i+1] = uint8((y * 255) / (h - 1))
			img.Pix[i+2] = uint8(((x + y) * 255) / (w + h - 2))
			img.Pix[i+3] = uint8(((x*y)*255)/((w-1)*(h-1)) ^ 0x55)
		}
	}
	return img
}

func mustEqual(t *testing.T, name string, a, b *image.RGBA) {
	t.Helper()
	if len(a.Pix) != len(b.Pix) {
		t.Fatalf("%s: length %d != %d", name, len(a.Pix), len(b.Pix))
	}
	for i := range a.Pix {
		if a.Pix[i] != b.Pix[i] {
			t.Fatalf("%s: byte %d old=%d new=%d", name, i, a.Pix[i], b.Pix[i])
		}
	}
}

// TestFilterMatricesByteIdenticalToReplacedCode proves that every colour-matrix
// filter, computed via the go-gfx shared matrices, produces byte-for-byte the
// same output as the replaced local matrices over a synthetic image.
func TestFilterMatricesByteIdenticalToReplacedCode(t *testing.T) {
	img := syntheticImage()
	amounts := []float64{0, 0.25, 0.5, 0.75, 1, 1.5, 2, 3}
	for _, a := range amounts {
		mustEqual(t, "brightness",
			oldApplyMatrix(img, oldBrightnessMatrix(a)),
			applyMatrix(img, gfxcolor.BrightnessMatrix(a)))
		mustEqual(t, "invert",
			oldApplyMatrix(img, oldInvertMatrix(a)),
			applyMatrix(img, gfxcolor.InvertMatrix(a)))
		mustEqual(t, "saturate",
			oldApplyMatrix(img, oldSaturateMatrix(a, 0.213, 0.715, 0.072)),
			applyMatrix(img, gfxcolor.SaturateMatrix(a)))
		mustEqual(t, "grayscale",
			oldApplyMatrix(img, oldSaturateMatrix(1-a, 0.2126, 0.7152, 0.0722)),
			applyMatrix(img, gfxcolor.GrayscaleMatrix(a)))
		mustEqual(t, "sepia",
			oldApplyMatrix(img, oldSepiaMatrix(a)),
			applyMatrix(img, gfxcolor.SepiaMatrix(a)))
	}
	// Hue-rotate over a full sweep of angles (radians), including negatives and
	// beyond a full turn.
	for deg := -360.0; deg <= 720; deg += 15 {
		rad := deg * math.Pi / 180
		mustEqual(t, "hue-rotate",
			oldApplyMatrix(img, oldHueRotateMatrix(rad)),
			applyMatrix(img, gfxcolor.HueRotateMatrix(rad)))
	}
}
