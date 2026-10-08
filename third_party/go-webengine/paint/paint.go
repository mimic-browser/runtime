// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package paint

import (
	"image"
	"image/draw"
	"math"
	"strings"

	"github.com/go-gfx/gfx/raster"
	"github.com/go-gfx/gfx/resample"
	"github.com/go-images/images"

	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/dom"
	"github.com/go-webengine/engine/layout"
	"github.com/go-widgets/painter"
)

// Paint draws the box tree onto dst. imgs supplies decoded <img> bitmaps keyed
// by node (already scaled to the layout size); a missing entry is skipped. The
// caller owns clearing dst to its base (e.g. white) beforehand. It is equivalent
// to PaintFull with no background-image bitmaps.
func Paint(dst *image.RGBA, box *layout.Box, f *Fonts, imgs map[*dom.Node]image.Image) {
	PaintFull(dst, box, f, imgs, nil)
}

// PaintFull draws the box tree onto dst, additionally painting CSS
// background-image layers (gradients and url() bitmaps). bgImgs supplies decoded
// background bitmaps keyed by their raw url() token; gradients need no bitmap.
func PaintFull(dst *image.RGBA, box *layout.Box, f *Fonts, imgs map[*dom.Node]image.Image, bgImgs map[string]image.Image) {
	pp := painter.NewPixelPainter(dst.Pix, dst.Rect.Dx(), dst.Rect.Dy())
	// The initial clip is the whole image; every draw already stays within it, so
	// pages with no overflow container render byte-identically to before.
	paintBox(dst, pp, box, f, imgs, bgImgs, dst.Rect)
}

// paintBox paints one box, wrapping the real work in a group-opacity pass when
// the box has 0 < opacity < 1 (opacity 0 skips the subtree entirely). clip is
// the pixel rectangle painting is confined to (an ancestor's overflow clip);
// it is always a sub-rectangle of the image bounds.
func paintBox(dst *image.RGBA, pp *painter.PixelPainter, box *layout.Box, f *Fonts, imgs map[*dom.Node]image.Image, bgImgs map[string]image.Image, clip image.Rectangle) {
	if box == nil || clip.Empty() {
		return
	}
	op := 1.0
	hasFilter := false
	hasMask := false
	hasRotate := false
	if box.Style != nil {
		if box.Style.HasOpacity {
			op = box.Style.Opacity
			if op <= 0 {
				return // fully transparent: paint nothing
			}
		}
		hasFilter = len(box.Style.Filters) > 0
		hasMask = box.Style.MaskImage != "" && bgImgs[box.Style.MaskImage] != nil
		hasRotate = box.Style.RotateDeg != 0
		if len(box.Style.BackdropFilters) > 0 {
			// Must run BEFORE any of this box's own content reaches dst (whether
			// painted directly below, or into the offscreen group buffer the
			// hasFilter/op<1/hasMask branch composites in afterwards) — backdrop-
			// filter's defining behaviour is filtering what is ALREADY there.
			applyBackdropFilter(dst, box, clip)
		}
	}
	// `transform:rotate()` (see css.Style.RotateDeg's own doc comment) takes
	// its own, separate offscreen-buffer path — paintRotated — rather than
	// joining the filter/opacity/mask group pass below: it rotates PIXELS,
	// not drawing coordinates, needs the box's own border-box rectangle
	// specifically (not the group pass's own full-canvas-width, ink-bounds-
	// expanded buffer), and grows its own buffer to fit the rotated
	// corners. Scoped to rotate ALONE — no confirmed real trigger combines
	// it with filter/opacity/mask-image on the same element, so this branch
	// is only taken when none of those apply; an element needing both would
	// silently keep its rotation unapplied rather than the two offscreen
	// mechanisms being unified for zero confirmed real need.
	if hasRotate && !hasFilter && !hasMask && op >= 1 {
		paintRotated(dst, box, f, imgs, bgImgs, clip)
		return
	}
	// A group pass (render to an offscreen buffer, then composite) is needed when
	// the box has a `filter` chain, a fractional opacity, and/or a `mask-image`.
	// Filters are applied to the group's rendered output before it is
	// composited, so blur/drop-shadow spread and colour transforms see the
	// whole subtree at once; a mask is applied the same way (see applyMask) so
	// it stencils everything the box paints — background, borders, text,
	// children — not just its own background layer.
	if hasFilter || op < 1 || hasMask {
		// The group buffer only needs to cover the rows this box's subtree can
		// actually paint into (its ink bounds, expanded for any blur/drop-shadow
		// spread), not the whole page: a page has a fixed, modest width but can be
		// tens of thousands of pixels tall, and a `filter`/fractional-opacity box is
		// typically a small icon or card. dst's PixelPainter still requires a
		// buffer starting at row 0 (it has no notion of a row origin), so the
		// PAINT step allocates [0, y1) — but the expensive per-pixel work (colour-
		// matrix filters, blur, composite) runs on a COPY of just [y0, y1),
		// re-anchored to row 0. The copy (not a zero-copy reslice) is deliberate:
		// go-images' AdjustContrast/GaussianBlur re-anchor any non-zero-origin
		// image.RGBA to (0,0) internally (see ToRGBA/newLike), which would silently
		// discard a shared reslice's row offset and paint the filtered result at
		// the top of the page. Re-anchoring ourselves, up front, and tracking the
		// offset separately through compositeGroupAt sidesteps that entirely.
		y0, y1 := groupRows(box, hasFilter, dst.Rect, clip)
		if y1 <= y0 {
			return // the box's ink bounds don't intersect the visible clip at all
		}
		tmp := image.NewRGBA(image.Rect(dst.Rect.Min.X, dst.Rect.Min.Y, dst.Rect.Max.X, y1))
		tpp := painter.NewPixelPainter(tmp.Pix, tmp.Rect.Dx(), tmp.Rect.Dy())
		paintBoxContent(tmp, tpp, box, f, imgs, bgImgs, clip)
		group := copyRows(tmp, y0, y1)
		if hasFilter {
			group = applyFilters(group, box.Style.Filters, box.Style.Color)
		}
		if hasMask {
			applyMask(group, box, bgImgs[box.Style.MaskImage], y0)
		}
		compositeGroupAt(dst, group, op, y0)
		return
	}
	paintBoxContent(dst, pp, box, f, imgs, bgImgs, clip)
}

// paintBoxContent paints a box's shadows, background, borders, inline content
// and children (recursing through paintBox so nested opacity groups compose).
// Everything this box paints is confined to clip; its own inline content and
// child boxes are additionally confined to descendantClip, which intersects clip
// with this box's padding box on any axis whose overflow is not visible.
func paintBoxContent(dst *image.RGBA, pp *painter.PixelPainter, box *layout.Box, f *Fonts, imgs map[*dom.Node]image.Image, bgImgs map[string]image.Image, clip image.Rectangle) {
	// The legacy `clip: rect(...)` property (see css.Style.HasClip) narrows
	// this box's OWN painting — background, border, text, AND everything it
	// passes down to its children — to a rectangle relative to its own
	// border-box corner, applying BEFORE any of the steps below so a
	// `clip:rect(0 0 0 0)` (the sr-only idiom this exists for) hides
	// everything, not just what a plain overflow clip on children would.
	// Spec-scoped to position:absolute/fixed, same as the property itself.
	if box.Style != nil && box.Style.HasClip && box.Position.OutOfFlow() {
		clip = intersectClipRect(clip, box)
	}
	// visibility:hidden paints nothing of this box's OWN background/border/
	// shadows/marker/inline content, but — unlike opacity<=0 or display:none —
	// still reserves its layout space, and a descendant box may re-show itself
	// with its own visibility:visible (visibility is inherited but overridable
	// per element). So children (step 7 below) are never skipped here; only
	// this box's own visual parts are.
	hidden := box.Style != nil && box.Style.Visibility != css.VisibilityVisible
	drawable := !hidden && box.Style != nil && box.W > 0 && box.H > 0
	rad := 0
	if drawable {
		rad = boxRadius(box)
	}
	// 1. Drop (outset) box-shadows paint behind the box.
	if drawable {
		for i := len(box.Style.BoxShadows) - 1; i >= 0; i-- {
			if sh := box.Style.BoxShadows[i]; !sh.Inset {
				paintDropShadow(dst, box, sh, rad, clip)
			}
		}
	}
	// 2. Solid background colour.
	if drawable && box.Style.Background.A > 0 {
		r := painter.Rect{X: int(box.X), Y: int(box.Y), W: int(box.W), H: int(box.H)}
		if rad > 0 {
			fillRoundRectClipped(dst, pp, r, rad, box.Style.Background, clip)
		} else {
			fillRectClipped(pp, r, box.Style.Background, clip)
		}
	}
	// 3. Background-image layers (last-listed painted first; first on top).
	if drawable && len(box.Style.BackgroundImages) > 0 {
		paintBackgroundLayers(dst, box.Style, rectOf(box), rad, bgImgs, clip)
	}
	// 4. Inset box-shadows paint over the background, under the border/content.
	if drawable {
		for i := len(box.Style.BoxShadows) - 1; i >= 0; i-- {
			if sh := box.Style.BoxShadows[i]; sh.Inset {
				paintInsetShadow(dst, box, sh, rad, clip)
			}
		}
	}
	// 5. Borders (real element boxes only; anonymous boxes carry no border).
	if !hidden && box.Style != nil && !box.Anonymous && box.W > 0 && box.H > 0 {
		paintBorders(pp, box, clip)
	}
	// A box's own inline content and children are clipped to its padding box when
	// it establishes an overflow clip (overflow != visible on either axis). This
	// is what confines the sr-only / visually-hidden pattern's overflowing text
	// to its 1×1 box instead of painting it at full size.
	inner := descendantClip(clip, box)
	// 5.5 List-item marker (bullet or ordinal) in the indent left of the content.
	if !hidden && box.Marker != nil {
		paintMarker(dst, pp, box.Marker, f, inner)
	}
	// 6. Inline content: every inline element's own box decoration on the line
	// first (outermost fragment first, so a nested background lands on top of
	// its ancestor's), then the words and atomic items over all of it.
	if !hidden {
		for _, line := range box.Lines {
			for i := range line.Inlines {
				paintInlineFragment(dst, pp, &line.Inlines[i], inner)
			}
			for _, it := range line.Items {
				paintItem(dst, pp, it, f, imgs, bgImgs, inner)
			}
		}
	}
	// 7. Children.
	for _, ch := range box.Children {
		paintBox(dst, pp, ch, f, imgs, bgImgs, inner)
	}
}

// clipsContent reports whether a box should clip its descendants' painting.
// It requires a non-visible overflow AND a height this engine actually
// trusts: either an author-set definite height, or an auto height on a
// `white-space:nowrap` box (see below for why that specific auto-height case
// is safe).
//
// The height gate is a deliberate conservatism: the engine's block layout still
// under-sizes some auto-height containers (a collapsed float/flex row can come
// out 0-tall). Clipping to such a wrongly-tiny box would HIDE real article body
// text — a far worse defect than failing to hide some off-screen chrome. When
// the author set an explicit height, the box's size is author-determined (not an
// engine guess), so clipping to it is safe and matches the browser. The
// universal sr-only / visually-hidden pattern — the whole point of this clip —
// sets an explicit `height:1px`, so it is covered; an engine-collapsed
// auto-height container is not. Percentage heights are excluded because the
// engine resolves them as auto (no definite basis), so they are not trustworthy.
//
// A `white-space:nowrap` box's auto height is a SEPARATE, genuinely trustworthy
// case, not the collapsed-float/flex-row failure mode this gate otherwise
// guards against: forbidding wrapping means its content is exactly one line,
// so the auto height is a simple, direct line-height computation — never the
// result of the same float/flex collapse logic that can under-size a container.
// Confirmed load-bearing live (round 87): caniuse.com's own `.news` ticker
// (`overflow:hidden;white-space:nowrap;text-overflow:ellipsis`, sized only by
// its CSS Grid column, no explicit height at all) previously never clipped,
// so its own long single-line text overflowed straight through the adjacent
// nav items ("Compare browsers", "About") instead of being cut off at its own
// right edge. `text-overflow:ellipsis`'s own "…" glyph is a separate,
// narrower, still-open gap — this only restores the CLIP, matching a plain
// `overflow:hidden` (no ellipsis) result, which is what real Chrome falls
// back to for any browser without ellipsis support, so it's never a worse
// outcome than before, only a category of "worse" traded for "correctly clipped
// but missing a cosmetic '…'".
func clipsContent(box *layout.Box) bool {
	st := box.Style
	if st == nil {
		return false
	}
	if !st.OverflowX.Clips() && !st.OverflowY.Clips() {
		return false
	}
	if !st.Height.Auto && !st.Height.IsPercent {
		return true
	}
	return st.WhiteSpace == css.WSNoWrap
}

// descendantClip narrows clip to box's padding box on each axis whose overflow
// clips. A visible box (the overwhelmingly common case) returns clip unchanged,
// so non-overflow pages are unaffected.
func descendantClip(clip image.Rectangle, box *layout.Box) image.Rectangle {
	if !clipsContent(box) {
		return clip
	}
	cx, cy := box.Style.OverflowX.Clips(), box.Style.OverflowY.Clips()
	bw := box.Style.Border.Widths()
	pb := image.Rect(
		int(box.X+bw.Left), int(box.Y+bw.Top),
		int(box.X+box.W-bw.Right), int(box.Y+box.H-bw.Bottom),
	)
	out := clip
	if cx {
		if pb.Min.X > out.Min.X {
			out.Min.X = pb.Min.X
		}
		if pb.Max.X < out.Max.X {
			out.Max.X = pb.Max.X
		}
	}
	if cy {
		if pb.Min.Y > out.Min.Y {
			out.Min.Y = pb.Min.Y
		}
		if pb.Max.Y < out.Max.Y {
			out.Max.Y = pb.Max.Y
		}
	}
	if out.Max.X < out.Min.X {
		out.Max.X = out.Min.X
	}
	if out.Max.Y < out.Min.Y {
		out.Max.Y = out.Min.Y
	}
	return out
}

// intersectClipRect narrows clip to box's legacy `clip: rect(...)` region
// (see css.Style.HasClip/ClipRect): a rectangle whose four edges are
// distances from box's own border-box top-left corner — NOT the box's own
// padding box the way descendantClip's overflow clipping is. image.Rectangle.
// Intersect already returns the zero rectangle for two rects that do not
// overlap at all (e.g. rect(0 0 0 0), the sr-only idiom this exists for), so
// no extra empty-rect clamp is needed here the way descendantClip's per-axis
// intersection (which can legitimately end up inverted on just one axis)
// requires.
func intersectClipRect(clip image.Rectangle, box *layout.Box) image.Rectangle {
	r := box.Style.ClipRect
	cr := image.Rect(
		int(box.X+r.Left), int(box.Y+r.Top),
		int(box.X+r.Right), int(box.Y+r.Bottom),
	)
	out := clip.Intersect(cr)
	return out
}

// fillRectClipped fills r∩clip with a solid colour. Intersecting the rect (not
// the pixels) is exact for an axis-aligned fill and keeps the common
// clip==image-bounds case identical to an unclipped fill.
func fillRectClipped(pp *painter.PixelPainter, r painter.Rect, c css.Color, clip image.Rectangle) {
	if r.W <= 0 || r.H <= 0 || c.A == 0 {
		return
	}
	ir := image.Rect(r.X, r.Y, r.X+r.W, r.Y+r.H).Intersect(clip)
	if ir.Empty() {
		return
	}
	pp.FillRect(painter.Rect{X: ir.Min.X, Y: ir.Min.Y, W: ir.Dx(), H: ir.Dy()}, toPainter(c))
}

// fillRoundRectClipped fills a rounded rect confined to clip. When clip already
// contains the rect (the common case) the rounded fill is drawn as-is; when clip
// cuts it, the fill is masked per-pixel to clip so overflow content stays inside
// the clipping ancestor.
func fillRoundRectClipped(dst *image.RGBA, pp *painter.PixelPainter, r painter.Rect, rad int, c css.Color, clip image.Rectangle) {
	full := image.Rect(r.X, r.Y, r.X+r.W, r.Y+r.H)
	if clip.Intersect(full) == full {
		pp.FillRoundRect(r, rad, toPainter(c))
		return
	}
	ir := full.Intersect(clip)
	for y := ir.Min.Y; y < ir.Max.Y; y++ {
		for x := ir.Min.X; x < ir.Max.X; x++ {
			if insideRoundRect(x, y, full, rad) {
				blendPixel(dst, x, y, c, c.A)
			}
		}
	}
}

// boxRadius returns the used corner radius (in pixels) for a box, resolving a
// percentage against the box's smaller side. The painter clamps it to half the
// smaller side, so pill/circle radii (large px or 50%) render correctly.
func boxRadius(box *layout.Box) int { return styleRadius(box.Style, box.W, box.H) }

// styleRadius is boxRadius for anything that has a style and a size but is not
// a Box — an inline element's own fragment, which owns no block box.
func styleRadius(st *css.Style, w, h float64) int {
	if st == nil {
		return 0
	}
	l := st.BorderRadius
	if l.Auto {
		return 0
	}
	var r float64
	if l.IsPercent {
		r = l.Percent * math.Min(w, h)
	} else {
		r = l.Px
	}
	if r <= 0 {
		return 0
	}
	return int(r + 0.5)
}

// paintBackgroundLayers paints st's background-image layers into bx (a
// border box — the caller's, be it a real layout.Box's own rectOf(box) or a
// form control's own painter.Rect-derived bounds), clipped to the rounded-
// rect shape.
func paintBackgroundLayers(dst *image.RGBA, st *css.Style, bx image.Rectangle, rad int, bgImgs map[string]image.Image, clip image.Rectangle) {
	for i := len(st.BackgroundImages) - 1; i >= 0; i-- {
		layer := st.BackgroundImages[i]
		switch layer.Kind {
		case css.BgGradient:
			paintGradient(dst, bx, rad, layer.Grad, clip)
		case css.BgURL:
			src := bgImgs[layer.URL]
			if src == nil {
				continue
			}
			size := nthSize(st.BackgroundSize, i)
			pos := nthPosition(st.BackgroundPosition, i)
			rep := nthRepeat(st.BackgroundRepeat, i)
			paintBgBitmap(dst, bx, rad, src, size, pos, rep, clip, st)
		}
	}
}

// paintGradient fills the box rect with a gradient, clipped to the rounded rect
// and to clip (an ancestor's overflow clip).
func paintGradient(dst *image.RGBA, bx image.Rectangle, rad int, g *css.Gradient, clip image.Rectangle) {
	if g == nil {
		return
	}
	w, h := float64(bx.Dx()), float64(bx.Dy())
	s := g.Sampler(w, h)
	region := bx.Intersect(dst.Rect).Intersect(clip)
	for y := region.Min.Y; y < region.Max.Y; y++ {
		for x := region.Min.X; x < region.Max.X; x++ {
			if !insideRoundRect(x, y, bx, rad) {
				continue
			}
			c := s.At(float64(x-bx.Min.X)+0.5, float64(y-bx.Min.Y)+0.5)
			if c.A == 0 {
				continue
			}
			blendPixel(dst, x, y, c, 255)
		}
	}
}

// paintBgBitmap paints a decoded background bitmap into the box rect honouring
// background-size, background-position and background-repeat, clipped to the
// rounded rect. The tile is resampled ONCE to its drawn pixel size with a
// high-quality filter (see scaleTile) and then stamped, so the image on screen
// is antialiased rather than the nearest-neighbour blocks a per-pixel sampler
// would produce.
func paintBgBitmap(dst *image.RGBA, bx image.Rectangle, rad int, src image.Image, size css.BgSize, pos css.BgPosition, rep css.BgRepeat, clip image.Rectangle, st *css.Style) {
	iw, ih := src.Bounds().Dx(), src.Bounds().Dy()
	if iw <= 0 || ih <= 0 {
		return
	}
	bw, bh := float64(bx.Dx()), float64(bx.Dy())
	dw, dh := bgTileSize(size, float64(iw), float64(ih), bw, bh)
	tw, th := int(math.Round(dw)), int(math.Round(dh))
	if tw < 1 {
		tw = 1
	}
	if th < 1 {
		th = 1
	}
	// Resample the source to the drawn tile size once, up front (bicubic by
	// default; nearest for image-rendering:pixelated). Every stamp below is then
	// a plain 1:1 copy — the resampling quality lives here, not in the blitter.
	tile := scaleTile(src, tw, th, bgMode(st))
	// Position: percentage aligns the image's p% point to the box's p% point.
	ox := resolvePos(pos.X, bw-float64(tw))
	oy := resolvePos(pos.Y, bh-float64(th))
	// Repeat: compute the tile index range covering the box.
	iMin, iMax := 0, 0
	jMin, jMax := 0, 0
	if rep == css.RepeatBoth || rep == css.RepeatX {
		iMin = int(math.Floor((0 - ox) / float64(tw)))
		iMax = int(math.Ceil((bw - ox) / float64(tw)))
	}
	if rep == css.RepeatBoth || rep == css.RepeatY {
		jMin = int(math.Floor((0 - oy) / float64(th)))
		jMax = int(math.Ceil((bh - oy) / float64(th)))
	}
	for j := jMin; j <= jMax; j++ {
		for i := iMin; i <= iMax; i++ {
			tileX := int(math.Round(float64(bx.Min.X) + ox + float64(i*tw)))
			tileY := int(math.Round(float64(bx.Min.Y) + oy + float64(j*th)))
			blitTileClipped(dst, tile, tileX, tileY, bx, rad, clip)
		}
	}
}

// bgMode maps a computed style's image-rendering to a resampling filter for
// background images: bicubic (smooth) by default, nearest for pixelated.
func bgMode(st *css.Style) resample.Mode {
	if st != nil && st.ImageRendering == css.IRPixelated {
		return resample.Nearest
	}
	return resample.Bicubic
}

// scaleTile returns src resampled to w×h using mode, in premultiplied-alpha
// space so a transparent pixel's colour does not bleed into a cut-out's edge.
// It returns src unchanged when it is already w×h (the common repeat/auto case),
// and falls back to src if go-gfx rejects the size (defensive: w,h are >= 1).
func scaleTile(src image.Image, w, h int, mode resample.Mode) image.Image {
	if src.Bounds().Dx() == w && src.Bounds().Dy() == h {
		return src
	}
	out, err := resample.ResizePremultiplied(raster.FromImage(src), w, h, mode)
	if err != nil {
		return src
	}
	return out.ToNRGBA()
}

// bgTileSize computes the drawn tile size for a background image.
func bgTileSize(size css.BgSize, iw, ih, bw, bh float64) (float64, float64) {
	switch size.Kind {
	case css.SizeCover:
		return coverContain(iw, ih, bw, bh, true)
	case css.SizeContain:
		return coverContain(iw, ih, bw, bh, false)
	case css.SizeExplicit:
		return explicitSize(size, iw, ih, bw, bh)
	default: // auto: intrinsic size
		return iw, ih
	}
}

// coverContain scales (iw,ih) to cover (true) or contain (false) the box.
func coverContain(iw, ih, bw, bh float64, cover bool) (float64, float64) {
	sx, sy := bw/iw, bh/ih
	var s float64
	if cover {
		s = math.Max(sx, sy)
	} else {
		s = math.Min(sx, sy)
	}
	return iw * s, ih * s
}

// explicitSize resolves an explicit background-size (lengths/percents, auto
// keeps the aspect ratio of the definite axis).
func explicitSize(size css.BgSize, iw, ih, bw, bh float64) (float64, float64) {
	wAuto, hAuto := size.W.Auto, size.H.Auto
	switch {
	case wAuto && hAuto:
		return iw, ih
	case hAuto:
		w := size.W.Resolve(bw)
		return w, ih * (w / iw)
	case wAuto:
		h := size.H.Resolve(bh)
		return iw * (h / ih), h
	default:
		return size.W.Resolve(bw), size.H.Resolve(bh)
	}
}

// resolvePos resolves a background-position component against the free space
// (box size minus tile size): a percentage p maps to p*free; px is literal.
func resolvePos(l css.Length, free float64) float64 {
	if l.IsPercent {
		return l.Percent * free
	}
	return l.Px
}

// blitTileClipped stamps an already-scaled tile at integer offset (dx,dy) with a
// 1:1 pixel copy, clipping each pixel to the rounded box rect (and to clip and
// the destination bounds). A source pixel with zero alpha is skipped; others are
// blended straight over the destination at their own alpha as coverage.
func blitTileClipped(dst *image.RGBA, tile image.Image, dx, dy int, bx image.Rectangle, rad int, clip image.Rectangle) {
	b := tile.Bounds()
	region := image.Rect(dx, dy, dx+b.Dx(), dy+b.Dy()).Intersect(bx).Intersect(dst.Rect).Intersect(clip)
	for y := region.Min.Y; y < region.Max.Y; y++ {
		for x := region.Min.X; x < region.Max.X; x++ {
			if !insideRoundRect(x, y, bx, rad) {
				continue
			}
			r, g, bl, a := tile.At(b.Min.X+(x-dx), b.Min.Y+(y-dy)).RGBA()
			cov := uint8(a >> 8)
			if cov == 0 {
				continue
			}
			blendPixel(dst, x, y, css.Color{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(bl >> 8), A: 255}, cov)
		}
	}
}

// ---- box-shadow ----

// paintDropShadow paints an outset box-shadow: a soft rect the size of the
// border box, expanded by spread and offset, Gaussian-blurred by the blur
// radius (approximated with an exact erf box; rounded corners are ignored for
// the blur shape, though the exclusion below still follows rad).
//
// A drop shadow never shows THROUGH the box's own original (un-offset)
// footprint, regardless of that box's own background being transparent —
// per spec, box-shadow is clipped to exclude the border box it belongs to,
// so only the part of the shifted/spread shadow that extends BEYOND the
// box's own edges is ever visible; the box's own content paints over
// whatever the shadow would have shown within its own footprint anyway.
// Before this exclusion existed, a small offset (common for a css-only
// "fake border" trick: box-shadow:-2px 0 0 <colour> in place of a real
// border-left, avoiding the layout shift a real border/padding change would
// cause) on a box with no background of its own left almost the WHOLE
// shadow rectangle visible — nearly indistinguishable from a solid fill —
// since nothing else ever painted over the overlap with the box's own
// (unshifted) area. Confirmed live and cross-checked against a real
// headless Chrome screenshot of the identical minimal markup: real Chrome
// shows only the thin sliver the -2px offset actually exposes; this engine
// showed what looked like the link's entire background filled solid grey
// (developer.mozilla.org's own "In this article" table-of-contents links,
// which use exactly this shape for their left-border indicator).
func paintDropShadow(dst *image.RGBA, box *layout.Box, sh css.BoxShadow, rad int, clip image.Rectangle) {
	if sh.Color.A == 0 {
		return
	}
	x0 := box.X + sh.OffsetX - sh.Spread
	y0 := box.Y + sh.OffsetY - sh.Spread
	x1 := box.X + box.W + sh.OffsetX + sh.Spread
	y1 := box.Y + box.H + sh.OffsetY + sh.Spread
	sigma := sh.Blur / 2
	pad := int(math.Ceil(sigma*3)) + 1
	area := image.Rect(int(x0)-pad, int(y0)-pad, int(x1)+pad+1, int(y1)+pad+1).Intersect(dst.Rect).Intersect(clip)
	own := rectOf(box)
	for y := area.Min.Y; y < area.Max.Y; y++ {
		for x := area.Min.X; x < area.Max.X; x++ {
			if insideRoundRect(x, y, own, rad) {
				continue
			}
			cov := erfBoxCoverage(float64(x)+0.5, float64(y)+0.5, x0, y0, x1, y1, sigma)
			if cov <= 0 {
				continue
			}
			blendPixel(dst, x, y, sh.Color, uint8(cov*float64(sh.Color.A)/255*255+0.5))
		}
	}
}

// paintInsetShadow paints an inset box-shadow: a soft dark band inside the box
// edges (the complement of a blurred inner rect), clipped to the box.
func paintInsetShadow(dst *image.RGBA, box *layout.Box, sh css.BoxShadow, rad int, clip image.Rectangle) {
	if sh.Color.A == 0 {
		return
	}
	bx := rectOf(box)
	x0 := box.X + sh.Spread + sh.OffsetX
	y0 := box.Y + sh.Spread + sh.OffsetY
	x1 := box.X + box.W - sh.Spread + sh.OffsetX
	y1 := box.Y + box.H - sh.Spread + sh.OffsetY
	sigma := sh.Blur / 2
	region := bx.Intersect(dst.Rect).Intersect(clip)
	for y := region.Min.Y; y < region.Max.Y; y++ {
		for x := region.Min.X; x < region.Max.X; x++ {
			if !insideRoundRect(x, y, bx, rad) {
				continue
			}
			inner := erfBoxCoverage(float64(x)+0.5, float64(y)+0.5, x0, y0, x1, y1, sigma)
			cov := 1 - inner
			if cov <= 0 {
				continue
			}
			blendPixel(dst, x, y, sh.Color, uint8(cov*float64(sh.Color.A)/255*255+0.5))
		}
	}
}

// erfBoxCoverage returns the coverage in [0,1] at point (px,py) of a box
// [x0,x1]×[y0,y1] blurred by a Gaussian of standard deviation sigma. With
// sigma<=0 it is a hard inside test.
func erfBoxCoverage(px, py, x0, y0, x1, y1, sigma float64) float64 {
	if sigma <= 0 {
		if px >= x0 && px <= x1 && py >= y0 && py <= y1 {
			return 1
		}
		return 0
	}
	k := 1 / (sigma * math.Sqrt2)
	// cx,cy are each in [0,1] (erf is monotonic and x1>=x0, y1>=y0, and erf
	// saturates to ±1), so their product is already in [0,1].
	cx := 0.5 * (math.Erf((x1-px)*k) - math.Erf((x0-px)*k))
	cy := 0.5 * (math.Erf((y1-py)*k) - math.Erf((y0-py)*k))
	return cx * cy
}

// ---- geometry helpers ----

func rectOf(box *layout.Box) image.Rectangle {
	return image.Rect(int(box.X), int(box.Y), int(box.X)+int(box.W), int(box.Y)+int(box.H))
}

// insideRoundRect reports whether the pixel centre (x,y) lies within rect r with
// corner radius rad (rad<=0 is a plain rectangle test).
func insideRoundRect(x, y int, r image.Rectangle, rad int) bool {
	fx, fy := float64(x)+0.5, float64(y)+0.5
	if fx < float64(r.Min.X) || fx >= float64(r.Max.X) || fy < float64(r.Min.Y) || fy >= float64(r.Max.Y) {
		return false
	}
	if rad <= 0 {
		return true
	}
	rf := float64(rad)
	// Clamp radius to half the smaller side (matches the painter).
	half := math.Min(float64(r.Dx()), float64(r.Dy())) / 2
	if rf > half {
		rf = half
	}
	// Corner circle centres.
	lx := float64(r.Min.X) + rf
	rx := float64(r.Max.X) - rf
	ty := float64(r.Min.Y) + rf
	by := float64(r.Max.Y) - rf
	var dx, dy float64
	switch {
	case fx < lx:
		dx = lx - fx
	case fx > rx:
		dx = fx - rx
	}
	switch {
	case fy < ty:
		dy = ty - fy
	case fy > by:
		dy = fy - by
	}
	if dx == 0 || dy == 0 {
		return true
	}
	return dx*dx+dy*dy <= rf*rf
}

// paintBorders draws the four border edges of a box. When the box has a corner
// radius and a single uniform visible border (same width/style/colour on all
// four sides), it is stroked as one rounded rectangle; otherwise each edge is
// drawn as a straight solid rectangle.
func paintBorders(pp *painter.PixelPainter, box *layout.Box, clip image.Rectangle) {
	paintEdges(pp, box.Style.Border, int(box.X), int(box.Y), int(box.W), int(box.H),
		boxRadius(box), true, true, clip)
}

// paintEdges strokes the four border edges of a border box at (x,y,w,h).
//
// left and right select whether the LEADING and TRAILING edges paint. A block
// box passes true for both; an inline element split across lines passes them
// per fragment, which is box-decoration-break: slice (the CSS default) — the
// left border belongs to the element's first fragment only and the right
// border to its last, while top and bottom paint on every fragment. A
// fragment missing either edge also forgoes the rounded single-stroke path:
// a partial rounded rect is not what the stroke primitive draws.
func paintEdges(pp *painter.PixelPainter, bd css.Borders, x, y, w, h, rad int, left, right bool, clip image.Rectangle) {
	if rad > 0 && left && right && uniformBorder(bd) && paintsSide(bd.Top) {
		// A rounded uniform border is stroked as one rounded rect; skip it only
		// when it lies entirely outside the clip (thin strokes are not per-pixel
		// masked — an ancestor rarely clips a rounded-border box mid-edge).
		full := image.Rect(x, y, x+w, y+h)
		if !clip.Intersect(full).Empty() {
			pp.StrokeRoundRect(painter.Rect{X: x, Y: y, W: w, H: h}, rad,
				toPainter(bd.Top.Color), iround(bd.Top.Width))
		}
		return
	}
	fill := func(rx, ry, rw, rh int, c css.Color) {
		fillRectClipped(pp, painter.Rect{X: rx, Y: ry, W: rw, H: rh}, c, clip)
	}
	if paintsSide(bd.Top) {
		fill(x, y, w, iround(bd.Top.Width), bd.Top.Color)
	}
	if paintsSide(bd.Bottom) {
		bw := iround(bd.Bottom.Width)
		fill(x, y+h-bw, w, bw, bd.Bottom.Color)
	}
	if left && paintsSide(bd.Left) {
		fill(x, y, iround(bd.Left.Width), h, bd.Left.Color)
	}
	if right && paintsSide(bd.Right) {
		bw := iround(bd.Right.Width)
		fill(x+w-bw, y, bw, h, bd.Right.Color)
	}
}

func paintsSide(s css.BorderSide) bool {
	return s.Width > 0 && s.Style != css.BorderNone && s.Color.A > 0
}

// uniformBorder reports whether all four edges are identical (width, style and
// colour), so a rounded box can be stroked as a single rounded rectangle.
func uniformBorder(b css.Borders) bool {
	return b.Top == b.Right && b.Right == b.Bottom && b.Bottom == b.Left
}

func iround(f float64) int { return int(f + 0.5) }

// paintMarker draws a list-item marker. A decimal marker reuses the inline text
// painter (the ordinal string in the item's own face and colour); the bullet
// types map to painter primitives — a disc is a full-radius filled round rect, a
// hollow circle a stroked round rect, and a square a plain filled rect.
func paintMarker(dst *image.RGBA, pp *painter.PixelPainter, m *layout.Marker, f *Fonts, clip image.Rectangle) {
	if m.Type == css.ListDecimal {
		paintItem(dst, pp, &layout.InlineItem{Text: m.Text, Style: m.Style, X: m.X, Y: m.Y, Ascent: m.Ascent}, f, nil, nil, clip)
		return
	}
	r := markerRect(m)
	if image.Rect(r.X, r.Y, r.X+r.W, r.Y+r.H).Intersect(clip).Empty() {
		return
	}
	col := toPainter(m.Style.Color)
	switch m.Type {
	case css.ListSquare:
		fillRectClipped(pp, r, m.Style.Color, clip)
	case css.ListCircle:
		pp.StrokeRoundRect(r, r.W/2, col, 1)
	default: // css.ListDisc
		pp.FillRoundRect(r, r.W/2, col)
	}
}

// markerRect is a bullet marker's integer bounding rectangle in device pixels.
func markerRect(m *layout.Marker) painter.Rect {
	return painter.Rect{
		X: int(math.Round(m.X)),
		Y: int(math.Round(m.Y)),
		W: int(math.Round(m.W)),
		H: int(math.Round(m.H)),
	}
}

// paintInlineFragment paints one inline element's box decoration on one line —
// its background, then its border. A block box paints its own background and
// border in steps 2 and 5, but an inline element — a <span style="background:…">
// label, a bordered <code>, an <a> with a highlight — owns no block box at all,
// so without this its decoration never painted: any light text the author set
// against that background (the very common white-on-a-coloured-pill pattern)
// landed as light-on-white and vanished entirely, and a border simply never
// appeared.
//
// Layout supplies the geometry already fragmented per line (see
// layout.InlineFragment): X..X+W covers this line's run of the element plus its
// leading edge on the FIRST fragment and its trailing edge on the LAST, and
// Y..Y+H the items' font box grown by the element's vertical border+padding —
// which may overflow the line box, exactly as CSS specifies. First/Last are
// forwarded to paintEdges as the left/right selectors, which is
// box-decoration-break: slice: an element wrapped across two lines paints one
// left border, on the first fragment, and one right border, on the last, with
// top and bottom on both.
//
// Padding is spacing, not paint: it is already inside X/W and Y/H because
// layout reserved it, and nothing extra is drawn for it here.
func paintInlineFragment(dst *image.RGBA, pp *painter.PixelPainter, fr *layout.InlineFragment, clip image.Rectangle) {
	st := fr.Style
	w, h := int(fr.W), int(fr.H)
	if st == nil || w <= 0 || h <= 0 {
		return
	}
	x, y := int(fr.X), int(fr.Y)
	rad := styleRadius(st, fr.W, fr.H)
	if st.Background.A > 0 {
		r := painter.Rect{X: x, Y: y, W: w, H: h}
		if rad > 0 {
			fillRoundRectClipped(dst, pp, r, rad, st.Background, clip)
		} else {
			fillRectClipped(pp, r, st.Background, clip)
		}
	}
	paintEdges(pp, st.Border, x, y, w, h, rad, fr.First, fr.Last, clip)
}

func paintItem(dst *image.RGBA, pp *painter.PixelPainter, it *layout.InlineItem, f *Fonts, imgs map[*dom.Node]image.Image, bgImgs map[string]image.Image, clip image.Rectangle) {
	if it.NestedBox != nil {
		// An inline-flex item (see InlineItem.NestedBox's own doc comment)
		// carries a real, already-positioned Box tree — paint it exactly
		// like any other box, including its own filter/opacity group-buffer
		// handling, backgrounds, borders and children, by recursing back
		// into paintBox rather than duplicating any of that here.
		paintBox(dst, pp, it.NestedBox, f, imgs, bgImgs, clip)
		return
	}
	// visibility:hidden on the item's OWN element paints nothing here — the
	// same "reserve the space, paint none of it" rule paintBoxContent already
	// applies to a Box (see its own Visibility doc comment above); an
	// InlineItem is the equivalent leaf for an image/form-control/text run
	// that never gets a real Box at all. This function never consulted the
	// item's own Visibility at all before this and always blitted it
	// regardless — found while investigating github.com's Markdown-heading
	// permalink icons (though those turned out to be gated by `opacity`, via
	// a real layout.Box, not this path — see the hover/pointer media-feature
	// fix in css/parse.go for the fix that actually closes that thread). A
	// minimal `<h1>text<a><svg style="visibility:hidden">…</a></h1>` repro
	// confirms this is a real, independent defect on its own: any inline
	// image/icon with an explicit `visibility:hidden` painted anyway.
	if it.Style != nil && it.Style.Visibility != css.VisibilityVisible {
		return
	}
	if it.Image != nil {
		if src, ok := imgs[it.Image]; ok {
			// Every <img> — block or inline — is represented as an InlineItem,
			// never a layout.Box (see contents()'s isReplacedTag branch), so it
			// never goes through paintBox's own hasFilter group-buffer wrapping
			// above. Without this, `filter` on an <img> had no effect at all:
			// confirmed live on pkg.go.dev, whose Details-panel icons are a
			// single grey SVG asset recoloured per-context via exactly this
			// `filter: brightness(0) invert(...) sepia(...) saturate(...)
			// hue-rotate(...)` idiom (the standard "SVG-to-CSS-filter" trick,
			// since an <img src> — unlike an inline <svg> — cannot be recoloured
			// with `fill`/`currentColor`) — every one of them rendered in the
			// SVG's own flat grey, never the intended accent colour.
			if it.Style != nil && len(it.Style.Filters) > 0 {
				src = applyFilters(toRGBA(src), it.Style.Filters, it.Style.Color)
			}
			// layout resolves an explicit width/max-width (see
			// resolvedReplacedSize) against the item's REAL containing width,
			// which can be narrower than the loaded bitmap's own pixel size —
			// the loader only ever sizes against the page's viewport, not any
			// nested container's narrower one. Scale here (reusing the same
			// resample path background-image tiles already go through) rather
			// than in the loader, so the loaded bitmap's own resolution stays
			// an upper bound and this stays a pure display-size concern.
			// object-fit:cover/contain (round 89) preserve the bitmap's OWN
			// aspect ratio instead of independently stretching each axis to
			// the box -- the exact mechanism background-size:cover/contain
			// already implements for a background layer, reused here via the
			// same coverContain/blitTileClipped helpers for a foreground
			// element. object-position is not modelled (no confirmed non-
			// centred real use), so the scaled content is always centred.
			fit := css.ObjectFitFill
			if it.Style != nil {
				fit = it.Style.ObjectFit
			}
			if bw, bh := it.Width, it.LineHeight; fit != css.ObjectFitFill && bw > 0 && bh > 0 {
				iw, ih := float64(src.Bounds().Dx()), float64(src.Bounds().Dy())
				tw, th := coverContain(iw, ih, bw, bh, fit == css.ObjectFitCover)
				tile := scaleTile(src, int(math.Round(tw)), int(math.Round(th)), bgMode(it.Style))
				bx := image.Rect(int(it.X), int(it.Y), int(it.X+bw), int(it.Y+bh))
				ox := int(it.X) + int(math.Round((bw-tw)/2))
				oy := int(it.Y) + int(math.Round((bh-th)/2))
				blitTileClipped(dst, tile, ox, oy, bx, 0, clip)
				return
			}
			if tw, th := int(math.Round(it.Width)), int(math.Round(it.LineHeight)); tw > 0 && th > 0 &&
				(tw != src.Bounds().Dx() || th != src.Bounds().Dy()) {
				src = scaleTile(src, tw, th, bgMode(it.Style))
			}
			blitImage(dst, src, int(it.X), int(it.Y), clip)
		}
		return
	}
	if it.FormControl != nil {
		paintFormControl(dst, pp, it, f, imgs, bgImgs, clip)
		return
	}
	if it.Text == "" || it.Style == nil {
		return
	}
	drawText(dst, pp, f, it.Style, it.Text, int(it.X), int(it.Y+it.Ascent), it.Style.Color, clip)
}

// drawText draws s left-aligned with its baseline at (x, baseline) in col, at
// st's font, clipped to clip. Shared by the plain text-run path above and
// form-control label/value painting (paintFormControl) so both letter glyphs
// identically. Returns the pen position after the last glyph.
func drawText(dst *image.RGBA, pp *painter.PixelPainter, f *Fonts, st *css.Style, s string, x, baseline int, col css.Color, clip image.Rectangle) int {
	penX := x
	for _, run := range f.Runs(s, st.FontFamily, st.FontWeight, st.Italic) {
		fc := f.runFace(run, st.FontFamily, st.FontSize, st.FontWeight, st.Italic)
		// Shape (see requiredLigatureFeature on Measure) before painting, not
		// per rune: a required ligature (Arabic lam-alef, an icon web font's
		// word-to-pictogram substitution) draws as the one glyph it shaped to,
		// by index (GlyphMaskIndex), not by re-looking the run's own runes up
		// in the cmap one at a time — which is exactly what a per-rune loop
		// did before this and could never produce a ligature glyph at all.
		for _, gid := range fc.Shape(run.Text, requiredLigatureFeature) {
			if gid == 0 {
				// See Measure's identical skip: a rune neither face's cmap
				// covers shapes to .notdef rather than being dropped, and
				// must still draw (and advance) nothing, as it did when this
				// loop looked runes up in the cmap directly.
				continue
			}
			bounds, mask, maskp, advance, ok := fc.GlyphMaskIndex(gid, penX, baseline)
			if ok && mask != nil {
				blitMask(dst, bounds, mask, maskp, col, clip)
			}
			penX += advance + int(st.LetterSpacing)
		}
	}
	if st.Underline && penX > x {
		// A single solid line the full advance width, in the text's own
		// colour (text-decoration-color is not modelled — see Style.
		// Underline's own doc comment). Thickness and offset are a fixed,
		// reasonable approximation scaled off font size rather than a real
		// font's own underline-position/-thickness metrics, which this
		// engine's font faces do not expose.
		thick := int(st.FontSize / 14)
		if thick < 1 {
			thick = 1
		}
		y := baseline + thick + 1
		fillRectClipped(pp, painter.Rect{X: x, Y: y, W: penX - x, H: thick}, col, clip)
	}
	return penX
}

// Form-control palette: close enough to a real browser's own UA defaults
// (Chrome/Safari light theme) to read as intentional, not exact — the point
// of this rendering is that a control is visible and clickable where it
// belongs, not pixel-perfect chrome fidelity.
var (
	formFieldBg     = css.Color{R: 255, G: 255, B: 255, A: 255}
	formButtonBg    = css.Color{R: 239, G: 239, B: 239, A: 255}
	formBorder      = css.Color{R: 118, G: 118, B: 118, A: 255}
	formAccent      = css.Color{R: 26, G: 115, B: 232, A: 255}
	formMutedText   = css.Color{R: 150, G: 150, B: 150, A: 255}
	formControlPadX = 6
)

// buttonIconGap is the horizontal gap between a button's text label and its
// trailing icon — must match layout's own buttonIconGap (layout/layout.go),
// which already reserved this same gap when sizing the control's box; the
// two packages don't share layout constants directly, so both must be kept
// in sync by hand if this value ever changes.
const buttonIconGap = 4

// paintFormControl draws a form control's box (background + 1px border) and,
// for anything but a checkbox/radio, its label or current value/placeholder
// text — the visible, clickable rendering isReplacedTag-style items never
// got before (see layout.go's isFormControlTag branch, which is what gives
// it.FormControl a non-nil node and a real box size to paint here).
func paintFormControl(dst *image.RGBA, pp *painter.PixelPainter, it *layout.InlineItem, f *Fonts, imgs map[*dom.Node]image.Image, bgImgs map[string]image.Image, clip image.Rectangle) {
	n := it.FormControl
	r := painter.Rect{X: int(it.X), Y: int(it.Y), W: int(it.Width), H: int(it.LineHeight)}
	kind := formControlKind(n)

	if kind == controlCheckbox || kind == controlRadio {
		paintCheckboxLike(dst, pp, r, n, it.Style, bgImgs, clip)
		return
	}

	// The background/border PAINTED here comes from the element's own cascaded
	// style (css/ua.go gives button/select/input/textarea a real UA-default
	// background-color+border, exactly like a real browser) rather than a
	// hardcoded colour — so an author reset (`background:0 0;border:0`, the
	// exact rule github.com's own top-nav <button>s use to look like plain
	// text links) is honoured instead of overridden by fake generic chrome.
	// formFieldBg/formBorder below are only the defensive fallback for the
	// pathological case of a nil Style (never real for an actual page
	// element, but paintControl's own unit-test helper can construct one).
	bg := formFieldBg
	if kind == controlButtonLike || kind == controlSelect {
		bg = formButtonBg
	}
	if it.Style != nil {
		bg = it.Style.Background
	}
	if bg.A > 0 {
		fillRectClipped(pp, r, bg, clip)
	}
	if it.Style != nil {
		// Per-side border painting, via the SAME paintEdges helper a real
		// layout.Box's own border uses (paintBorders) — previously this
		// control's own check inspected ONLY Border.Top's width/style/colour
		// and, if it painted, stroked one uniform-colour rectangle for all
		// four sides. An author style setting just ONE side (`border:0;
		// border-bottom:1px solid #fff`, confirmed live on caniuse.com's own
		// search input, round 65's own flagged follow-up) left Border.Top at
		// its zeroed `border:0` value regardless of what border-bottom set,
		// so drawBorder was false and NOTHING painted at all despite the
		// author clearly intending a visible bottom rule.
		paintEdges(pp, it.Style.Border, r.X, r.Y, r.W, r.H, 0, true, true, clip)
	} else {
		strokeRect1px(pp, r, formBorder, clip)
	}

	text, muted := formControlDisplayText(n, it.Label)

	// An icon-only <button> (Label == "", see layout.InlineItem.Icon's doc
	// comment) draws its img/svg child's own bitmap centred in the control's
	// box instead of any text — real browsers give it no fabricated label
	// either, and this is the actual visible content that box exists for.
	if it.Icon != nil && text == "" {
		if src, ok := imgs[it.Icon]; ok {
			b := src.Bounds()
			ix := r.X + (r.W-b.Dx())/2
			iy := r.Y + (r.H-b.Dy())/2
			blitImage(dst, src, ix, iy, clip)
		}
		return
	}

	if text == "" || it.Style == nil {
		return
	}
	st := it.Style
	col := st.Color
	if muted {
		col = formMutedText
	}
	baseline := r.Y + (r.H+int(st.FontSize))/2 - 2 // roughly centers the cap-height in the box
	x := r.X + formControlPadX
	tw := f.Measure(text, st.FontFamily, st.FontSize, st.FontWeight, st.Italic)

	// A text label with a leading and/or trailing icon — github.com's own
	// Primer "<> Code ▾" button has BOTH at once (round 92); its nav
	// dropdown triggers (round 85, "Platform▾" and friends) have only a
	// trailing one. The whole group (icon+gap+label+gap+icon, omitting
	// whichever side has no icon) centres as a unit — layout already
	// reserved exactly this width (buttonIconGap per side present) when
	// sizing the control's box, so no further layout decision is made
	// here, only painting each present piece in document order.
	leadSrc, hasLead := imgs[it.LeadingIcon]
	trailSrc, hasTrail := imgs[it.Icon]
	if hasLead || hasTrail {
		total := tw
		if hasLead {
			total += float64(leadSrc.Bounds().Dx()) + buttonIconGap
		}
		if hasTrail {
			total += float64(trailSrc.Bounds().Dx()) + buttonIconGap
		}
		x = r.X + int((float64(r.W)-total)/2)
		if hasLead {
			b := leadSrc.Bounds()
			iy := r.Y + (r.H-b.Dy())/2
			blitImage(dst, leadSrc, x, iy, clip)
			x += b.Dx() + buttonIconGap
		}
		drawText(dst, pp, f, st, text, x, baseline, col, clip)
		if hasTrail {
			b := trailSrc.Bounds()
			ix := x + int(tw) + buttonIconGap
			iy := r.Y + (r.H-b.Dy())/2
			blitImage(dst, trailSrc, ix, iy, clip)
		}
		return
	}

	if kind == controlButtonLike {
		// A button's label centers horizontally in its own (content-sized) box.
		x = r.X + int((float64(r.W)-tw)/2)
	}
	drawText(dst, pp, f, st, text, x, baseline, col, clip)
}

// strokeRect1px draws a plain (non-rounded) 1px border around r — the simple
// case paintBorders (paint.go) already handles for a real element box, but
// this needs no Style/border-side plumbing, just a flat outline.
func strokeRect1px(pp *painter.PixelPainter, r painter.Rect, c css.Color, clip image.Rectangle) {
	fillRectClipped(pp, painter.Rect{X: r.X, Y: r.Y, W: r.W, H: 1}, c, clip)
	fillRectClipped(pp, painter.Rect{X: r.X, Y: r.Y + r.H - 1, W: r.W, H: 1}, c, clip)
	fillRectClipped(pp, painter.Rect{X: r.X, Y: r.Y, W: 1, H: r.H}, c, clip)
	fillRectClipped(pp, painter.Rect{X: r.X + r.W - 1, Y: r.Y, W: 1, H: r.H}, c, clip)
}

// paintCheckboxLike draws a checkbox/radio. By default (no `appearance:none`)
// it draws this engine's own generic small square — filled accent when
// checked, outlined otherwise. Checkbox vs. radio (square vs. circular) is
// not distinguished, and neither is this engine's own real native OS chrome
// (never modelled at all) — clickability and checked-state visibility are
// what matter for this engine's driving use case (a login form), not exact
// native fidelity.
//
// But when the author has set `appearance:none` (st.AppearanceNone — see its
// own doc comment for the confirmed developer.mozilla.org <mdn-switch>
// trigger, a custom toggle-switch built from a plain checkbox), the element
// is no longer a native-look control at all: it becomes a plain styled box
// like any other, and the SAME author style paintFormControl's own
// non-checkbox path already honours (round 65's caniuse.com precedent) must
// paint here too — background colour, background-image layers (gradients
// and url() bitmaps, reusing paintBackgroundLayers exactly as a real
// layout.Box's own background does), border-radius, and the per-side border.
// A checked-state colour swap some custom-checkbox patterns apply via a
// `:checked` rule (MDN's own switch does, for its knob's position and
// accent colour) is not specially modelled here: it works automatically,
// the same as any other `:checked`-matched rule already recascades this
// element's Style before paint ever sees it.
func paintCheckboxLike(dst *image.RGBA, pp *painter.PixelPainter, r painter.Rect, n *dom.Node, st *css.Style, bgImgs map[string]image.Image, clip image.Rectangle) {
	if st != nil && st.AppearanceNone {
		rad := styleRadius(st, float64(r.W), float64(r.H))
		if st.Background.A > 0 {
			fillRoundRectClipped(dst, pp, r, rad, st.Background, clip)
		}
		if len(st.BackgroundImages) > 0 {
			bx := image.Rect(r.X, r.Y, r.X+r.W, r.Y+r.H)
			paintBackgroundLayers(dst, st, bx, rad, bgImgs, clip)
		}
		paintEdges(pp, st.Border, r.X, r.Y, r.W, r.H, rad, true, true, clip)
		return
	}
	_, checked := n.Attribute("checked")
	bg, border := formFieldBg, formBorder
	if checked {
		bg, border = formAccent, formAccent
	}
	fillRectClipped(pp, r, bg, clip)
	strokeRect1px(pp, r, border, clip)
}

type controlKind int

const (
	controlText controlKind = iota
	controlCheckbox
	controlRadio
	controlButtonLike // <input type=button/submit/reset>, <button>
	controlSelect
	controlTextarea
)

func formControlKind(n *dom.Node) controlKind {
	switch n.Tag {
	case "input":
		switch strings.ToLower(n.Attr["type"]) {
		case "checkbox":
			return controlCheckbox
		case "radio":
			return controlRadio
		case "button", "submit", "reset":
			return controlButtonLike
		}
		return controlText
	case "button":
		return controlButtonLike
	case "select":
		return controlSelect
	case "textarea":
		return controlTextarea
	}
	return controlText
}

// formControlDisplayText returns the text a form control shows and whether
// it is placeholder text (drawn muted, matching a real browser) rather than
// a real value. label is the item's precomputed InlineItem.Label — only
// meaningful (and only ever non-empty) for a "button", whose label is
// resolved at layout time from its VISIBLE descendant text (see
// layout.buttonLabel); paint has no style map of its own to redo that
// display:none-aware walk from n alone.
func formControlDisplayText(n *dom.Node, label string) (text string, muted bool) {
	switch n.Tag {
	case "input":
		v := n.Attr["value"]
		if v != "" {
			if strings.EqualFold(n.Attr["type"], "password") {
				return strings.Repeat("•", len([]rune(v))), false
			}
			return v, false
		}
		if strings.EqualFold(n.Attr["type"], "button") ||
			strings.EqualFold(n.Attr["type"], "submit") ||
			strings.EqualFold(n.Attr["type"], "reset") {
			return controlLabel(n), false
		}
		if ph, ok := n.Attribute("placeholder"); ok && ph != "" {
			return ph, true
		}
		return "", false
	case "button":
		// label is layout's precomputed visible-text-content label; unlike
		// <input type=button/submit>, a <button> tag with no visible text
		// (an icon-only button) has NO fabricated default in any real UA.
		return label, false
	case "textarea":
		if v, ok := n.Attribute("value"); ok && v != "" {
			return v, false
		}
		if v := dom.TextContent(n); v != "" {
			return v, false
		}
		if ph, ok := n.Attribute("placeholder"); ok && ph != "" {
			return ph, true
		}
		return "", false
	case "select":
		if label, ok := selectedOptionLabel(n); ok {
			return label, false
		}
		return "", false
	}
	return "", false
}

// controlLabel returns an <input type=button/submit/reset>'s visible label —
// duplicated (deliberately, not shared) from layout's own copy: this is a
// PAINT-time text-content decision (what to draw) using only the DOM
// attribute already available here, not a layout-time sizing input, and the
// two packages do not otherwise depend on each other's internals.
func controlLabel(n *dom.Node) string {
	if v, ok := n.Attribute("value"); ok && v != "" {
		return v
	}
	if strings.EqualFold(n.Attr["type"], "reset") {
		return "Reset"
	}
	return "Submit"
}

// selectedOptionLabel returns the visible text of sel's selected <option>,
// per the HTML standard's option selectedness algorithm
// (https://html.spec.whatwg.org/multipage/form-elements.html#concept-option-selectedness):
// for a non-`multiple` select, the LAST <option> with a `selected` attribute
// wins in tree order; with none marked selected, the FIRST option that is
// not itself `disabled` and whose ancestor <optgroup> (if any) is not
// disabled wins instead. Returns false only when sel has no eligible option
// at all (no options, or every option disabled).
func selectedOptionLabel(sel *dom.Node) (string, bool) {
	var lastSelected, firstEnabled *dom.Node
	var walk func(n *dom.Node, groupDisabled bool)
	walk = func(n *dom.Node, groupDisabled bool) {
		for _, c := range n.Children {
			if c.Type != dom.Element {
				continue
			}
			switch c.Tag {
			case "option":
				if _, ok := c.Attribute("selected"); ok {
					lastSelected = c
				}
				if firstEnabled == nil && !groupDisabled {
					if _, disabled := c.Attribute("disabled"); !disabled {
						firstEnabled = c
					}
				}
			case "optgroup":
				_, groupDisabledHere := c.Attribute("disabled")
				walk(c, groupDisabled || groupDisabledHere)
			}
		}
	}
	walk(sel, false)
	chosen := lastSelected
	if chosen == nil {
		chosen = firstEnabled
	}
	if chosen == nil {
		return "", false
	}
	return optionLabel(chosen), true
}

// optionLabel is an <option>'s displayed text: its `label` attribute if
// present and non-empty, else its text content
// (https://html.spec.whatwg.org/multipage/form-elements.html#the-option-element).
// Duplicated (deliberately, not shared) from layout's own copy — see
// controlLabel's doc comment above for why.
func optionLabel(opt *dom.Node) string {
	if v, ok := opt.Attribute("label"); ok && v != "" {
		return v
	}
	return strings.TrimSpace(dom.TextContent(opt))
}

// blitMask composites an 8-bit coverage mask in colour col onto dst, confined to
// clip (an ancestor's overflow clip).
func blitMask(dst *image.RGBA, bounds image.Rectangle, mask *image.Alpha, maskp image.Point, col css.Color, clip image.Rectangle) {
	region := bounds.Intersect(dst.Rect).Intersect(clip)
	for y := region.Min.Y; y < region.Max.Y; y++ {
		my := maskp.Y + (y - bounds.Min.Y)
		for x := region.Min.X; x < region.Max.X; x++ {
			mx := maskp.X + (x - bounds.Min.X)
			a := mask.AlphaAt(mx, my).A
			if a == 0 {
				continue
			}
			blendPixel(dst, x, y, col, a)
		}
	}
}

// blendPixel does src-over compositing of col at coverage cov (0..255) over the
// existing pixel, preserving/accumulating alpha so it is correct on both an
// opaque canvas (alpha stays 255) and a transparent group buffer.
func blendPixel(dst *image.RGBA, x, y int, col css.Color, cov uint8) {
	sa := float64(cov) / 255 * float64(col.A) / 255
	if sa <= 0 {
		return
	}
	i := dst.PixOffset(x, y)
	da := float64(dst.Pix[i+3]) / 255
	// sa>0 here (guarded above), so outA = sa + da*(1-sa) > 0 always.
	outA := sa + da*(1-sa)
	inv := da * (1 - sa)
	dst.Pix[i+0] = uint8((float64(col.R)*sa+float64(dst.Pix[i+0])*inv)/outA + 0.5)
	dst.Pix[i+1] = uint8((float64(col.G)*sa+float64(dst.Pix[i+1])*inv)/outA + 0.5)
	dst.Pix[i+2] = uint8((float64(col.B)*sa+float64(dst.Pix[i+2])*inv)/outA + 0.5)
	dst.Pix[i+3] = uint8(outA*255 + 0.5)
}

// compositeGroupAt composites a transparent, zero-origin group buffer src over
// dst at row offset yOffset (src's row 0 lands on dst row yOffset), scaling the
// group's alpha by the group opacity (0..1). The offset is threaded through
// explicitly rather than carried in src.Rect.Min because src has already been
// re-anchored to (0,0) by copyRows (see the comment on that function).
func compositeGroupAt(dst, src *image.RGBA, opacity float64, yOffset int) {
	cov := uint8(opacity*255 + 0.5)
	for y := src.Rect.Min.Y; y < src.Rect.Max.Y; y++ {
		for x := src.Rect.Min.X; x < src.Rect.Max.X; x++ {
			i := src.PixOffset(x, y)
			a := src.Pix[i+3]
			if a == 0 {
				continue
			}
			blendPixel(dst, x, y+yOffset, css.Color{R: src.Pix[i+0], G: src.Pix[i+1], B: src.Pix[i+2], A: a}, cov)
		}
	}
}

// applyMask stencils group (a zero-origin buffer whose row 0 is canvas row
// yOffset, X aligned 1:1 with the canvas — see copyRows/compositeGroupAt) by
// mask, an alpha template for box's `mask-image`. Everything OUTSIDE box's
// own border box is fully masked out (mask-image affects the whole element,
// including any overflowing shadow/child content) — a page with such an
// element would need real mask-position/mask-repeat/mask-origin support to
// render correctly, which this engine does not model, so cutting it entirely
// is the honest choice over guessing. Inside the border box, mask is
// stretched to fill it (this engine's one deliberate simplification of the
// mask-size/mask-position grammar — see the doc comment on
// css.Style.MaskImage) and each pixel's alpha is scaled by the mask's alpha
// there. group's Pix is alpha-premultiplied (matching image.RGBA and every
// other group-buffer consumer in this file), so R/G/B must be scaled down by
// the SAME fraction as A to keep representing the same underlying colour at
// lower opacity, not to darken it.
func applyMask(group *image.RGBA, box *layout.Box, mask image.Image, yOffset int) {
	bx := rectOf(box)
	bw, bh := bx.Dx(), bx.Dy()
	if bw <= 0 || bh <= 0 {
		clearAlpha(group)
		return
	}
	scaled := scaleTile(mask, bw, bh, bgMode(box.Style))
	for y := group.Rect.Min.Y; y < group.Rect.Max.Y; y++ {
		canvasY := y + yOffset
		inRow := canvasY >= bx.Min.Y && canvasY < bx.Max.Y
		for x := group.Rect.Min.X; x < group.Rect.Max.X; x++ {
			i := group.PixOffset(x, y)
			if group.Pix[i+3] == 0 {
				continue
			}
			if !inRow || x < bx.Min.X || x >= bx.Max.X {
				group.Pix[i+0], group.Pix[i+1], group.Pix[i+2], group.Pix[i+3] = 0, 0, 0, 0
				continue
			}
			_, _, _, ma := scaled.At(x-bx.Min.X, canvasY-bx.Min.Y).RGBA()
			f := ma >> 8 // 0..255
			group.Pix[i+0] = uint8(uint32(group.Pix[i+0]) * f / 255)
			group.Pix[i+1] = uint8(uint32(group.Pix[i+1]) * f / 255)
			group.Pix[i+2] = uint8(uint32(group.Pix[i+2]) * f / 255)
			group.Pix[i+3] = uint8(uint32(group.Pix[i+3]) * f / 255)
		}
	}
}

// clearAlpha fully masks a group buffer (used when box has no border box to
// mask into — a degenerate 0-sized element).
func clearAlpha(group *image.RGBA) {
	for i := 3; i < len(group.Pix); i += 4 {
		group.Pix[i] = 0
	}
}

// groupRows returns the row range [y0, y1) a filter/opacity group buffer for
// box actually needs to cover: box's own ink bounds (its subtree's painted
// extent, which can exceed its border box — a shrink-wrapped float, a negative
// margin, an outset box-shadow), expanded for any blur/drop-shadow spread in
// its filter chain, intersected with the ancestor clip and the canvas. Only
// rows are bounded (not columns): a page is typically a fixed, modest width but
// can be tens of thousands of pixels tall, so that is where the win is: X is
// left at the canvas's full width since narrowing it would buy little for the
// extra bookkeeping.
func groupRows(box *layout.Box, hasFilter bool, canvas, clip image.Rectangle) (y0, y1 int) {
	ext := subtreeExtent(box)
	if hasFilter {
		if m := filterMargin(box.Style.Filters); m > 0 {
			ext.Min.Y -= int(m)
			ext.Max.Y += int(m)
		}
	}
	rows := image.Rect(canvas.Min.X, ext.Min.Y, canvas.Max.X, ext.Max.Y).Intersect(clip).Intersect(canvas)
	return rows.Min.Y, rows.Max.Y
}

// subtreeExtent returns the smallest axis-aligned rectangle (in absolute
// document pixels) covering everything box's paintBoxContent recursion can
// draw: the box's own border box plus any outset box-shadow spread, its list
// marker, its line boxes, and — recursively — every child. A block box's own
// W/H is not always a safe upper bound on its own (a shrink-wrapped
// float/negative margin can place a descendant outside it — see the comment on
// clipsContent), so this walks the real subtree rather than trusting box.H.
func subtreeExtent(box *layout.Box) image.Rectangle {
	if box == nil {
		return image.Rectangle{}
	}
	r := shadowExpandedRect(box)
	if box.Marker != nil {
		m := box.Marker
		r = r.Union(pixelRect(m.X, m.Y, m.X+m.W, m.Y+m.H))
	}
	for _, line := range box.Lines {
		r = r.Union(pixelRect(line.X, line.Y, line.X+line.W, line.Y+line.H))
	}
	for _, ch := range box.Children {
		r = r.Union(subtreeExtent(ch))
	}
	return r
}

// shadowExpandedRect is box's own border box, expanded by the spread of any
// outset (non-inset) box-shadow it carries — the same geometry paintDropShadow
// paints into.
func shadowExpandedRect(box *layout.Box) image.Rectangle {
	x0, y0 := box.X, box.Y
	x1, y1 := box.X+box.W, box.Y+box.H
	if box.Style != nil {
		for _, sh := range box.Style.BoxShadows {
			if sh.Inset {
				continue
			}
			sigma := sh.Blur / 2
			pad := math.Ceil(sigma*3) + 1
			x0 = math.Min(x0, box.X+sh.OffsetX-sh.Spread-pad)
			y0 = math.Min(y0, box.Y+sh.OffsetY-sh.Spread-pad)
			x1 = math.Max(x1, box.X+box.W+sh.OffsetX+sh.Spread+pad)
			y1 = math.Max(y1, box.Y+box.H+sh.OffsetY+sh.Spread+pad)
		}
	}
	return pixelRect(x0, y0, x1, y1)
}

// pixelRect converts a document-space rect to an integer pixel rect, rounding
// outward so a fractional edge is never clipped.
func pixelRect(x0, y0, x1, y1 float64) image.Rectangle {
	return image.Rect(int(math.Floor(x0)), int(math.Floor(y0)), int(math.Ceil(x1)), int(math.Ceil(y1)))
}

// filterMargin returns the pixel margin a filter chain's spatial effects
// (blur, drop-shadow) can spread beyond the element's own bounds — the same
// 3-sigma-plus-one convention paintDropShadow uses for CSS box-shadow. Purely
// pointwise filters (brightness, contrast, …) contribute nothing.
func filterMargin(filters []css.Filter) float64 {
	var m float64
	for _, f := range filters {
		var v float64
		switch f.Kind {
		case css.FilterBlur:
			v = math.Ceil(f.Amount*3) + 1
		case css.FilterDropShadow:
			sigma := f.Blur / 2
			v = math.Abs(f.OffsetX) + math.Abs(f.OffsetY) + math.Ceil(sigma*3) + 1
		}
		if v > m {
			m = v
		}
	}
	return m
}

// applyBackdropFilter blurs (or otherwise filters — see css.Filter) whatever
// has already been painted behind box's own border box, replacing those
// pixels in dst in place, before box's own background/border/content paint on
// top of them: backdrop-filter's defining "frosted glass" behaviour. clip is
// the same ancestor overflow-clip rectangle paintBox already carries.
//
// The band sampled for the blur spans the FULL canvas width (via copyRows,
// the same helper the foreground `filter` group path uses), not box's own X
// range hard-cropped first: a blur legitimately draws on content just outside
// box's own edges (what is actually behind a translucent bar's rounded
// corner, say), and cropping first would manufacture a wrong dark/transparent
// fringe at the boundary instead of a smooth falloff. Only the pixels inside
// box's own (clipped) rect are written back — the wider band exists solely to
// give the blur real neighbouring pixels to sample from. applyFilters never
// changes its buffer's size (every Filter kind, including drop-shadow, paints
// into a same-Rect output — see filter.go), so the write-back loop below can
// safely assume filtered shares band's re-anchored coordinate space.
func applyBackdropFilter(dst *image.RGBA, box *layout.Box, clip image.Rectangle) {
	r := rectOf(box).Intersect(clip).Intersect(dst.Rect)
	if r.Empty() {
		return
	}
	m := int(filterMargin(box.Style.BackdropFilters))
	y0, y1 := r.Min.Y-m, r.Max.Y+m
	band := copyRows(dst, y0, y1) // zero-origin, full canvas width
	bandY0 := y0
	if bandY0 < dst.Rect.Min.Y {
		bandY0 = dst.Rect.Min.Y
	}
	filtered := applyFilters(band, box.Style.BackdropFilters, box.Style.Color)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		by := y - bandY0
		for x := r.Min.X; x < r.Max.X; x++ {
			si := filtered.PixOffset(x, by)
			di := dst.PixOffset(x, y)
			copy(dst.Pix[di:di+4], filtered.Pix[si:si+4])
		}
	}
}

// copyRows returns a fresh, zero-origin image.RGBA holding a COPY of img's row
// range [y0, y1) (full width). It deliberately copies rather than reslicing
// img's backing array in place: a reslice would keep img's non-zero Rect.Min,
// and go-images' AdjustContrast/GaussianBlur (used by the CSS contrast and
// blur/drop-shadow filters) re-anchor any non-zero-origin image.RGBA to (0,0)
// internally (ToRGBA/newLike) before processing it — silently discarding a
// reslice's row offset and making the filtered result land at the top of the
// page instead of at the box's real position. Returning an explicitly
// re-anchored copy up front, with the caller tracking y0 separately (see
// compositeGroupAt), avoids relying on every current and future pixel
// primitive to preserve a rect it has no reason to.
func copyRows(img *image.RGBA, y0, y1 int) *image.RGBA {
	if y0 < img.Rect.Min.Y {
		y0 = img.Rect.Min.Y
	}
	if y1 > img.Rect.Max.Y {
		y1 = img.Rect.Max.Y
	}
	w := img.Rect.Dx()
	if y1 <= y0 {
		return image.NewRGBA(image.Rect(0, 0, w, 0))
	}
	out := image.NewRGBA(image.Rect(0, 0, w, y1-y0))
	lo := (y0 - img.Rect.Min.Y) * img.Stride
	hi := (y1 - img.Rect.Min.Y) * img.Stride
	copy(out.Pix, img.Pix[lo:hi])
	return out
}

// toRGBA returns src as an *image.RGBA, unchanged if it already is one (the
// common case for a decoded PNG icon) — applyFilters' per-pixel colour-matrix
// math needs direct Pix access, which a decoded JPEG (image.YCbCr) or other
// image.Image implementation does not offer.
func toRGBA(src image.Image) *image.RGBA {
	if rgba, ok := src.(*image.RGBA); ok {
		return rgba
	}
	out := image.NewRGBA(src.Bounds())
	draw.Draw(out, out.Bounds(), src, src.Bounds().Min, draw.Src)
	return out
}

// blitImage draws src onto dst with its top-left at (dx, dy), confined to clip
// (an ancestor's overflow clip; always within the image bounds).
func blitImage(dst *image.RGBA, src image.Image, dx, dy int, clip image.Rectangle) {
	b := src.Bounds()
	for sy := b.Min.Y; sy < b.Max.Y; sy++ {
		ty := dy + (sy - b.Min.Y)
		if ty < clip.Min.Y || ty >= clip.Max.Y {
			continue
		}
		for sx := b.Min.X; sx < b.Max.X; sx++ {
			tx := dx + (sx - b.Min.X)
			if tx < clip.Min.X || tx >= clip.Max.X {
				continue
			}
			r, g, bl, a := src.At(sx, sy).RGBA()
			cov := uint8(a >> 8)
			blendPixel(dst, tx, ty, css.Color{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(bl >> 8), A: 255}, cov)
		}
	}
}

// paintRotated renders box's own border-box rectangle (rectOf) to an
// offscreen buffer, rotates the PIXELS about the box's own centre — the
// default, and only modelled, transform-origin — by RotateDeg degrees with
// go-images' Rotate (bilinear, growing the output to fit the rotated
// corners, matching a real browser's own visible-overflow-during-rotation
// behaviour), and composites the grown result back centred on the box's
// original centre point via blitImage. See css.Style.RotateDeg's own doc
// comment for the negated sign (CSS is clockwise-positive; go-images'
// Rotate is counter-clockwise-positive, matching scikit-image) and for why
// this never runs alongside filter/opacity/mask-image (paintBox's own
// dispatch only takes this path when none of those apply).
//
// Content outside the box's own border-box rectangle — an overflowing
// child of an `overflow:visible` box — is not captured or rotated with it,
// the same border-box-only scope applyMask's own doc comment already
// discloses for `mask-image`; no confirmed real trigger needs more.
func paintRotated(dst *image.RGBA, box *layout.Box, f *Fonts, imgs map[*dom.Node]image.Image, bgImgs map[string]image.Image, clip image.Rectangle) {
	bx := rectOf(box).Intersect(dst.Rect)
	if bx.Empty() {
		return
	}
	// Descendants can paint below the rotated box's border box. Account for
	// their extent before painting into the temporary image (upstream v0.5.1).
	tmpH := subtreeExtent(box).Intersect(dst.Rect).Max.Y
	tmp := image.NewRGBA(image.Rect(0, 0, dst.Rect.Dx(), tmpH))
	tpp := painter.NewPixelPainter(tmp.Pix, tmp.Rect.Dx(), tmp.Rect.Dy())
	paintBoxContent(tmp, tpp, box, f, imgs, bgImgs, clip)
	src := image.NewRGBA(image.Rect(0, 0, bx.Dx(), bx.Dy()))
	draw.Draw(src, src.Bounds(), tmp, bx.Min, draw.Src)
	rotated := images.Rotate(src, -box.Style.RotateDeg, true)
	cx, cy := box.X+box.W/2, box.Y+box.H/2
	ox := int(math.Round(cx - float64(rotated.Rect.Dx())/2))
	oy := int(math.Round(cy - float64(rotated.Rect.Dy())/2))
	blitImage(dst, rotated, ox, oy, clip)
}

func toPainter(c css.Color) painter.RGBA {
	return painter.RGBA{R: c.R, G: c.G, B: c.B, A: c.A}
}

// nthSize returns the size for layer i (repeating the last, default auto).
func nthSize(list []css.BgSize, i int) css.BgSize {
	if len(list) == 0 {
		return css.BgSize{Kind: css.SizeAuto}
	}
	if i >= len(list) {
		i = len(list) - 1
	}
	return list[i]
}

// nthPosition returns the position for layer i (default top-left 0%,0%).
func nthPosition(list []css.BgPosition, i int) css.BgPosition {
	if len(list) == 0 {
		return css.BgPosition{X: css.Length{Percent: 0, IsPercent: true}, Y: css.Length{Percent: 0, IsPercent: true}}
	}
	if i >= len(list) {
		i = len(list) - 1
	}
	return list[i]
}

// nthRepeat returns the repeat for layer i (default repeat).
func nthRepeat(list []css.BgRepeat, i int) css.BgRepeat {
	if len(list) == 0 {
		return css.RepeatBoth
	}
	if i >= len(list) {
		i = len(list) - 1
	}
	return list[i]
}
