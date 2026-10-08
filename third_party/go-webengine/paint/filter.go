// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package paint

import (
	"image"
	"math"

	gfxcolor "github.com/go-gfx/gfx/color"
	"github.com/go-images/images"

	"github.com/go-webengine/engine/css"
)

// applyFilters applies a CSS `filter` function chain to a group buffer (the
// element's rendered output on transparent, straight-alpha RGBA) in order,
// returning the filtered buffer. curColor is the element's used `color`,
// substituted for a drop-shadow whose colour is currentColor.
//
// The heavy primitives are REUSED, not reimplemented: the Gaussian blur is
// go-images' separable GaussianBlur (blur/drop-shadow), contrast is go-images'
// AdjustContrast, and premultiplied-alpha handling is go-gfx's colour helpers.
// The component-transfer / colour-matrix filters (brightness, saturate,
// grayscale, sepia, hue-rotate, invert) are the CSS Filter Effects spec
// matrices, whose colour-space construction lives in go-gfx's colour layer
// (gfxcolor.*Matrix); this package only iterates the pixel buffer through them.
func applyFilters(src *image.RGBA, filters []css.Filter, curColor css.Color) *image.RGBA {
	img := src
	for _, f := range filters {
		switch f.Kind {
		case css.FilterBlur:
			img = blurFilter(img, f.Amount)
		case css.FilterContrast:
			// go-images AdjustContrast scales each channel about the 128 mid-point
			// by the factor — exactly CSS contrast(<amount>).
			img = images.AdjustContrast(img, f.Amount)
		case css.FilterBrightness:
			img = applyMatrix(img, gfxcolor.BrightnessMatrix(f.Amount))
		case css.FilterSaturate:
			img = applyMatrix(img, gfxcolor.SaturateMatrix(f.Amount))
		case css.FilterGrayscale:
			img = applyMatrix(img, gfxcolor.GrayscaleMatrix(f.Amount))
		case css.FilterSepia:
			img = applyMatrix(img, gfxcolor.SepiaMatrix(f.Amount))
		case css.FilterHueRotate:
			img = applyMatrix(img, gfxcolor.HueRotateMatrix(f.Amount))
		case css.FilterInvert:
			img = applyMatrix(img, gfxcolor.InvertMatrix(f.Amount))
		case css.FilterDropShadow:
			img = dropShadowFilter(img, f, curColor)
		}
	}
	return img
}

// applyMatrix applies the go-gfx colour matrix m to every pixel of src,
// returning a new buffer. The transform is evaluated in 0..255 byte space (the
// matrix offset, spec-defined in 0..1, is scaled by 255) and clamped to
// [0,255]; alpha is preserved. Keeping the arithmetic in byte space — rather
// than normalising to gfxcolor.Apply's 0..1 range and back — makes the result
// bit-identical to the previous in-package implementation.
func applyMatrix(src *image.RGBA, m gfxcolor.ColorMatrix) *image.RGBA {
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

// blurFilter applies a Gaussian blur of standard deviation sigma to a
// straight-alpha buffer, blurring in PREMULTIPLIED space so transparent edges do
// not leak dark fringes. The premultiplied colour and the alpha are each blurred
// with the same separable Gaussian (go-images GaussianBlur carries alpha through
// unchanged, so the alpha plane is blurred as a separate grayscale pass), then
// the result is divided back out. sigma <= 0 is a no-op.
func blurFilter(src *image.RGBA, sigma float64) *image.RGBA {
	if sigma <= 0 {
		return src
	}
	// Premultiplied colour, blurred (alpha rides along unchanged, discarded).
	pm := image.NewRGBA(src.Rect)
	copy(pm.Pix, gfxcolor.Premultiply(src.Pix))
	bColor, _ := images.GaussianBlur(pm, sigma) // err only for sigma<=0, guarded above
	// Alpha plane as grayscale, blurred with the same kernel.
	alpha := image.NewRGBA(src.Rect)
	for i := 0; i < len(src.Pix); i += 4 {
		a := src.Pix[i+3]
		alpha.Pix[i], alpha.Pix[i+1], alpha.Pix[i+2], alpha.Pix[i+3] = a, a, a, 255
	}
	bAlpha, _ := images.GaussianBlur(alpha, sigma)
	// Recombine premultiplied colour with blurred alpha, then unpremultiply.
	out := image.NewRGBA(src.Rect)
	for i := 0; i < len(out.Pix); i += 4 {
		out.Pix[i] = bColor.Pix[i]
		out.Pix[i+1] = bColor.Pix[i+1]
		out.Pix[i+2] = bColor.Pix[i+2]
		out.Pix[i+3] = bAlpha.Pix[i]
	}
	gfxcolor.UnpremultiplyInPlace(out.Pix)
	return out
}

// dropShadowFilter paints a blurred, offset, coloured copy of the element's
// alpha behind the element. The shadow's shape is the element's alpha channel
// blurred by a Gaussian (standard deviation = blur/2, matching the box-shadow
// convention), tinted by the shadow colour, offset by (OffsetX,OffsetY); the
// original element is composited on top (source-over).
func dropShadowFilter(src *image.RGBA, f css.Filter, curColor css.Color) *image.RGBA {
	col := f.Color
	if f.UseCurrentColor {
		col = curColor
	}
	// Blurred alpha mask of the element.
	mask := image.NewRGBA(src.Rect)
	for i := 0; i < len(src.Pix); i += 4 {
		a := src.Pix[i+3]
		mask.Pix[i], mask.Pix[i+1], mask.Pix[i+2], mask.Pix[i+3] = a, a, a, 255
	}
	if sigma := f.Blur / 2; sigma > 0 {
		mask, _ = images.GaussianBlur(mask, sigma)
	}
	ox := int(math.Round(f.OffsetX))
	oy := int(math.Round(f.OffsetY))
	w, h := src.Rect.Dx(), src.Rect.Dy()
	out := image.NewRGBA(src.Rect)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			di := out.PixOffset(src.Rect.Min.X+x, src.Rect.Min.Y+y)
			// Shadow contribution: the mask sampled at the pre-offset position.
			var sr, sg, sb, sa float64
			sx, sy := x-ox, y-oy
			if sx >= 0 && sx < w && sy >= 0 && sy < h {
				mi := mask.PixOffset(src.Rect.Min.X+sx, src.Rect.Min.Y+sy)
				ma := float64(mask.Pix[mi]) / 255 * float64(col.A) / 255
				sr, sg, sb, sa = float64(col.R)/255, float64(col.G)/255, float64(col.B)/255, ma
			}
			// Element (source) over the shadow.
			er := float64(src.Pix[di]) / 255
			eg := float64(src.Pix[di+1]) / 255
			eb := float64(src.Pix[di+2]) / 255
			ea := float64(src.Pix[di+3]) / 255
			or, og, ob, oa := srcOver(er, eg, eb, ea, sr, sg, sb, sa)
			out.Pix[di] = clamp8(or * 255)
			out.Pix[di+1] = clamp8(og * 255)
			out.Pix[di+2] = clamp8(ob * 255)
			out.Pix[di+3] = clamp8(oa * 255)
		}
	}
	return out
}

// srcOver composites straight-alpha colour (fr,fg,fb,fa) over (br,bg,bb,ba),
// returning straight-alpha out. All components are in [0,1].
func srcOver(fr, fg, fb, fa, br, bg, bb, ba float64) (r, g, b, a float64) {
	oa := fa + ba*(1-fa)
	if oa <= 0 {
		return 0, 0, 0, 0
	}
	r = (fr*fa + br*ba*(1-fa)) / oa
	g = (fg*fa + bg*ba*(1-fa)) / oa
	b = (fb*fa + bb*ba*(1-fa)) / oa
	return r, g, b, oa
}

// clamp8 rounds v to the nearest byte, clamping to [0,255].
func clamp8(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(v + 0.5)
}
