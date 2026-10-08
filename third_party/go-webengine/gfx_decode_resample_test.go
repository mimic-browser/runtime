// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package engine

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"math"
	"testing"

	"github.com/go-gfx/gfx/raster"
	"github.com/go-gfx/gfx/resample"
	"golang.org/x/image/bmp"
	"golang.org/x/image/tiff"

	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/dom"
)

// encodeFormat encodes img into the named container so a test can build a
// data: URI the engine will sniff and decode. Every encoder is a pure-Go
// reference (stdlib or golang.org/x/image), matching codec's decode side.
func encodeFormat(t *testing.T, format string, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	var err error
	switch format {
	case "png":
		err = png.Encode(&buf, img)
	case "jpeg":
		err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95})
	case "gif":
		err = gif.Encode(&buf, img, nil)
	case "bmp":
		err = bmp.Encode(&buf, img)
	case "tiff":
		err = tiff.Encode(&buf, img, nil)
	default:
		t.Fatalf("unknown format %q", format)
	}
	if err != nil {
		t.Fatalf("encode %s: %v", format, err)
	}
	return buf.Bytes()
}

// TestImgDecodeRoundTripAllFormats proves the <img> pipeline now decodes every
// container codec.Decode supports — PNG, JPEG, GIF, BMP and TIFF — through the
// engine's real loadImages path. The pre-go-gfx pipeline (go-images.Decode over
// image.Decode) only registered PNG and JPEG, so GIF/BMP/TIFF are a capability
// the switch adds. Each fixture is a synthetic solid-red square generated in the
// test (no external assets, per the fixtures policy).
func TestImgDecodeRoundTripAllFormats(t *testing.T) {
	// A 16x16 solid red square. JPEG is lossy and GIF is palette-quantised, so a
	// pure primary colour survives both within a small tolerance.
	src := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for i := 0; i < len(src.Pix); i += 4 {
		src.Pix[i], src.Pix[i+1], src.Pix[i+2], src.Pix[i+3] = 220, 20, 20, 255
	}
	formats := []struct {
		name string
		mime string
	}{
		{"png", "image/png"},
		{"jpeg", "image/jpeg"},
		{"gif", "image/gif"},
		{"bmp", "image/bmp"},
		{"tiff", "image/tiff"},
	}
	for _, f := range formats {
		t.Run(f.name, func(t *testing.T) {
			data := encodeFormat(t, f.name, src)
			uri := "data:" + f.mime + ";base64," + base64.StdEncoding.EncodeToString(data)
			html := `<html><body style="margin:0"><img src="` + uri + `"></body></html>`
			root, _ := dom.Parse(html)
			doc := &Document{URL: "https://x.test/", Root: root}
			// Viewport wide enough that no downscale-to-fit alters the 16x16 image.
			sizes, bitmaps := New().loadImages(context.Background(), doc, css.Cascade(root), 200)
			if len(bitmaps) != 1 {
				t.Fatalf("%s: decoded %d bitmaps, want 1", f.name, len(bitmaps))
			}
			var bmpImg image.Image
			var size [2]float64
			for n := range bitmaps {
				bmpImg = bitmaps[n]
				size = sizes[n]
			}
			if bmpImg.Bounds().Dx() != 16 || bmpImg.Bounds().Dy() != 16 {
				t.Fatalf("%s: decoded size %v, want 16x16", f.name, bmpImg.Bounds())
			}
			if size != [2]float64{16, 16} {
				t.Fatalf("%s: intrinsic size %v, want 16x16", f.name, size)
			}
			r, g, b, a := bmpImg.At(8, 8).RGBA()
			r8, g8, b8, a8 := r>>8, g>>8, b>>8, a>>8
			if a8 != 255 {
				t.Fatalf("%s: centre alpha = %d, want opaque", f.name, a8)
			}
			// Red channel dominant, green/blue small — survives lossy/palette coding.
			if r8 < 150 || g8 > 90 || b8 > 90 {
				t.Errorf("%s: centre colour = (%d,%d,%d), want dominant red", f.name, r8, g8, b8)
			}
		})
	}
}

// TestImgFetchFailSkipped covers the fetch-failure branch of loadOneImage: an
// <img> whose src cannot be fetched (a non-http, non-data scheme) is dropped.
func TestImgFetchFailSkipped(t *testing.T) {
	html := `<html><body><img src="ftp://x/i.png"></body></html>`
	root, _ := dom.Parse(html)
	doc := &Document{URL: "https://x.test/", Root: root}
	_, bitmaps := New().loadImages(context.Background(), doc, css.Cascade(root), 200)
	if len(bitmaps) != 0 {
		t.Errorf("unfetchable <img> should yield no bitmaps, got %d", len(bitmaps))
	}
}

// TestUnknownFormatSkipped confirms a payload matching no signature is skipped
// (codec.Decode returns ErrUnknownFormat, loadOneImage drops the node).
func TestUnknownFormatSkipped(t *testing.T) {
	uri := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("not an image"))
	html := `<html><body><img src="` + uri + `"></body></html>`
	root, _ := dom.Parse(html)
	doc := &Document{URL: "https://x.test/", Root: root}
	_, bitmaps := New().loadImages(context.Background(), doc, css.Cascade(root), 200)
	if len(bitmaps) != 0 {
		t.Errorf("undecodable payload should yield no bitmaps, got %d", len(bitmaps))
	}
}

// loadOneBitmap renders a single <img> with the given inline style through the
// real loadImages path and returns its decoded bitmap and intrinsic size.
func loadOneBitmap(t *testing.T, style string, viewportW int) (image.Image, [2]float64) {
	t.Helper()
	src := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for i := 0; i < len(src.Pix); i += 4 {
		src.Pix[i], src.Pix[i+1], src.Pix[i+2], src.Pix[i+3] = 40, 160, 220, 255
	}
	uri := "data:image/png;base64," + base64.StdEncoding.EncodeToString(encodeFormat(t, "png", src))
	html := `<html><body style="margin:0"><img style="` + style + `" src="` + uri + `"></body></html>`
	root, _ := dom.Parse(html)
	doc := &Document{URL: "https://x.test/", Root: root}
	sizes, bitmaps := New().loadImages(context.Background(), doc, css.Cascade(root), viewportW)
	if len(bitmaps) != 1 {
		t.Fatalf("style %q: got %d bitmaps, want 1", style, len(bitmaps))
	}
	for n := range bitmaps {
		return bitmaps[n], sizes[n]
	}
	return nil, [2]float64{}
}

// TestImgCSSResizePaths covers the CSS width/height and percentage-width resize
// branches of loadOneImage, which route through resizeRaster (bicubic). A 16x16
// source is enlarged to a definite 32x32 and, separately, to 50% of a 40px
// viewport (=20px).
func TestImgCSSResizePaths(t *testing.T) {
	// Definite width+height: exact 32x32.
	bmpImg, size := loadOneBitmap(t, "width:32px;height:32px", 200)
	if bmpImg.Bounds().Dx() != 32 || bmpImg.Bounds().Dy() != 32 {
		t.Errorf("css 32x32 gave %v", bmpImg.Bounds())
	}
	if size != [2]float64{32, 32} {
		t.Errorf("css size %v want 32x32", size)
	}
	// Percentage width against a 40px viewport → 20px wide (aspect-square → 20 tall).
	bmpImg, size = loadOneBitmap(t, "width:50%", 40)
	if bmpImg.Bounds().Dx() != 20 {
		t.Errorf("percent width gave %v want 20 wide", bmpImg.Bounds())
	}
	if size != [2]float64{20, 20} {
		t.Errorf("percent size %v want 20x20", size)
	}
	// The enlarged bitmap keeps the source's cyan-ish colour (bicubic preserves a
	// flat fill) and stays opaque.
	r, g, b, a := bmpImg.At(10, 10).RGBA()
	if a>>8 != 255 || r>>8 > 120 || g>>8 < 100 || b>>8 < 150 {
		t.Errorf("resized colour = (%d,%d,%d,%d)", r>>8, g>>8, b>>8, a>>8)
	}
}

// loadWideBitmap renders a very wide, 1px-tall <img> so a downscale drives the
// derived height below 1 and exercises the nh<1 clamps in loadOneImage.
func loadWideBitmap(t *testing.T, style string, viewportW int) image.Image {
	t.Helper()
	src := image.NewRGBA(image.Rect(0, 0, 400, 1))
	for i := 0; i < len(src.Pix); i += 4 {
		src.Pix[i], src.Pix[i+1], src.Pix[i+2], src.Pix[i+3] = 40, 160, 220, 255
	}
	uri := "data:image/png;base64," + base64.StdEncoding.EncodeToString(encodeFormat(t, "png", src))
	html := `<html><body style="margin:0"><img style="` + style + `" src="` + uri + `"></body></html>`
	root, _ := dom.Parse(html)
	doc := &Document{URL: "https://x.test/", Root: root}
	_, bitmaps := New().loadImages(context.Background(), doc, css.Cascade(root), viewportW)
	for n := range bitmaps {
		return bitmaps[n]
	}
	return nil
}

// TestImgExtremeAspectClampsHeight covers the derived-height < 1 clamps on both
// the percentage-width and viewport-fit resize paths: a 400x1 source shrunk far
// enough would compute a sub-pixel height, which is clamped to 1px.
func TestImgExtremeAspectClampsHeight(t *testing.T) {
	// Percentage width: 50% of a 40px viewport = 20px wide; height 1*20/400 < 1 → 1.
	if b := loadWideBitmap(t, "width:50%", 40); b == nil || b.Bounds().Dy() != 1 {
		t.Errorf("percent extreme aspect height = %v, want 1px", b.Bounds())
	}
	// Viewport-fit: no CSS size, viewport 40 < 400 → width 40, height 1*40/400 < 1 → 1.
	if b := loadWideBitmap(t, "", 40); b == nil || b.Bounds().Dy() != 1 || b.Bounds().Dx() != 40 {
		t.Errorf("viewport-fit extreme aspect = %v, want 40x1", b.Bounds())
	}
}

// TestLoadOneBackgroundGuards covers loadOneBackground's early-return guards: a
// cancelled context, and a src that fails to fetch — both yield nil.
func TestLoadOneBackgroundGuards(t *testing.T) {
	e := New()
	doc := &Document{URL: "https://x.test/"}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if img := e.loadOneBackground(cancelled, doc, "u.png"); img != nil {
		t.Error("cancelled context should yield nil background")
	}
	// A non-http, non-data src fails fetchImageBytes.
	if img := e.loadOneBackground(context.Background(), doc, "ftp://x/i.png"); img != nil {
		t.Error("unfetchable src should yield nil background")
	}
	// A data: URI carrying an undecodable payload reaches codec.Decode and errors.
	bad := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("nope"))
	if img := e.loadOneBackground(context.Background(), doc, bad); img != nil {
		t.Error("undecodable background payload should yield nil")
	}
	// A valid data: URI decodes to a bitmap (the success path + ToNRGBA).
	src := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for i := range src.Pix {
		src.Pix[i] = 255
	}
	good := "data:image/png;base64," + base64.StdEncoding.EncodeToString(encodeFormat(t, "png", src))
	if img := e.loadOneBackground(context.Background(), doc, good); img == nil {
		t.Error("valid background payload should decode")
	}
}

// refNearestResize reproduces, verbatim, the nearest-neighbour sampling the paint
// layer used to do (the replaced blitScaledClipped mapping with dx=0, dw=w): each
// destination pixel takes the single source pixel its centre maps back onto. It
// is the "before" the bicubic switch replaces, kept here so the quality
// comparison is against the real old code, not a variant of the new one.
func refNearestResize(src *image.RGBA, w, h int) *image.RGBA {
	b := src.Bounds()
	iw, ih := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			u := (float64(x) + 0.5) / float64(w) * float64(iw)
			v := (float64(y) + 0.5) / float64(h) * float64(ih)
			sx := b.Min.X + int(u)
			sy := b.Min.Y + int(v)
			if sx >= b.Max.X {
				sx = b.Max.X - 1
			}
			if sy >= b.Max.Y {
				sy = b.Max.Y - 1
			}
			dst.SetRGBA(x, y, src.RGBAAt(sx, sy))
		}
	}
	return dst
}

// boxAverageDownscale computes the exact area-average downscale, used as the
// antialiased ground truth. It requires an integer reduction ratio (iw%w==0),
// which the caller guarantees, so each destination pixel is the mean of an exact
// block of source pixels — the ideal a good resampler approximates.
func boxAverageDownscale(src *image.RGBA, w, h int) *image.RGBA {
	b := src.Bounds()
	iw, ih := b.Dx(), b.Dy()
	bx, by := iw/w, ih/h
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var rs, gs, bs, as int
			for dy := 0; dy < by; dy++ {
				for dx := 0; dx < bx; dx++ {
					c := src.RGBAAt(b.Min.X+x*bx+dx, b.Min.Y+y*by+dy)
					rs += int(c.R)
					gs += int(c.G)
					bs += int(c.B)
					as += int(c.A)
				}
			}
			n := bx * by
			dst.SetRGBA(x, y, color.RGBA{uint8(rs / n), uint8(gs / n), uint8(bs / n), uint8(as / n)})
		}
	}
	return dst
}

// mse returns the mean squared error over the RGB channels of two equally-sized
// images.
func mse(a, b *image.RGBA) float64 {
	var sum float64
	n := 0
	for i := 0; i+3 < len(a.Pix) && i+3 < len(b.Pix); i += 4 {
		for c := 0; c < 3; c++ {
			d := float64(a.Pix[i+c]) - float64(b.Pix[i+c])
			sum += d * d
			n++
		}
	}
	return sum / float64(n)
}

// syntheticDisk draws an antialiasing-sensitive test image: a hard-edged filled
// red disk on white. Its edge is exactly what nearest sampling turns to jaggies
// and a good low-pass filter antialiases.
func syntheticDisk(size int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	cx, cy := float64(size)/2, float64(size)/2
	rad := float64(size) * 0.42
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx, dy := float64(x)+0.5-cx, float64(y)+0.5-cy
			if dx*dx+dy*dy <= rad*rad {
				img.SetRGBA(x, y, color.RGBA{220, 20, 20, 255})
			} else {
				img.SetRGBA(x, y, color.RGBA{255, 255, 255, 255})
			}
		}
	}
	return img
}

// TestResizeQualityBicubicBeatsNearest is the quality proof: downscaling a
// hard-edged disk 4x, the bicubic resample (the new default) is markedly closer
// to the exact area-averaged ground truth than the nearest sampler it replaces.
// This is a measured improvement, not merely "it runs".
func TestResizeQualityBicubicBeatsNearest(t *testing.T) {
	const srcN, dstN = 240, 60 // exact 4x reduction
	src := syntheticDisk(srcN)
	ideal := boxAverageDownscale(src, dstN, dstN)

	near := refNearestResize(src, dstN, dstN)

	rs, err := resample.Resize(raster.FromImage(src), dstN, dstN, resample.Bicubic)
	if err != nil {
		t.Fatal(err)
	}
	bicubic := rasterToRGBA(rs)

	mseNear := mse(near, ideal)
	mseBicubic := mse(bicubic, ideal)
	psnr := func(m float64) float64 {
		if m == 0 {
			return math.Inf(1)
		}
		return 10 * math.Log10(255*255/m)
	}
	t.Logf("downscale 240->60 vs area-average ground truth:")
	t.Logf("  nearest  MSE=%.2f  PSNR=%.2f dB", mseNear, psnr(mseNear))
	t.Logf("  bicubic  MSE=%.2f  PSNR=%.2f dB", mseBicubic, psnr(mseBicubic))
	t.Logf("  bicubic error is %.1f%% of nearest's", 100*mseBicubic/mseNear)

	if mseBicubic >= mseNear {
		t.Fatalf("bicubic MSE %.2f not better than nearest %.2f", mseBicubic, mseNear)
	}
	// The improvement is substantial, not marginal: at a 4x reduction of a hard
	// edge the bicubic low-pass should more than halve the error.
	if mseBicubic > 0.5*mseNear {
		t.Errorf("bicubic MSE %.2f should be < half of nearest %.2f", mseBicubic, mseNear)
	}
}

// TestUpscaleBicubicIsSmoother proves the enlargement side: upscaling a 2-colour
// step, nearest keeps a single hard jump while bicubic spreads the transition
// over several pixels, so bicubic's largest single-pixel step is much smaller —
// the smoothness a browser gives a scaled-up image.
func TestUpscaleBicubicIsSmoother(t *testing.T) {
	// A 4x1 horizontal ramp 0,85,170,255 upscaled to 64 wide.
	src := image.NewRGBA(image.Rect(0, 0, 4, 1))
	for x, v := range []uint8{0, 85, 170, 255} {
		src.SetRGBA(x, 0, color.RGBA{v, v, v, 255})
	}
	const w = 64
	near := refNearestResize(src, w, 1)
	rs, err := resample.Resize(raster.FromImage(src), w, 1, resample.Bicubic)
	if err != nil {
		t.Fatal(err)
	}
	bic := rasterToRGBA(rs)
	maxStep := func(img *image.RGBA) int {
		mx := 0
		for x := 1; x < w; x++ {
			d := int(img.RGBAAt(x, 0).R) - int(img.RGBAAt(x-1, 0).R)
			if d < 0 {
				d = -d
			}
			if d > mx {
				mx = d
			}
		}
		return mx
	}
	nStep, bStep := maxStep(near), maxStep(bic)
	t.Logf("upscale 4->64 max single-pixel step: nearest=%d bicubic=%d", nStep, bStep)
	if bStep >= nStep {
		t.Fatalf("bicubic max step %d not smoother than nearest %d", bStep, nStep)
	}
}

// rasterToRGBA converts a go-gfx raster.Image (straight alpha) to *image.RGBA
// (premultiplied) for the metric helpers, which read premultiplied bytes.
func rasterToRGBA(p *raster.Image) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, p.W, p.H))
	nr := p.ToNRGBA()
	for y := 0; y < p.H; y++ {
		for x := 0; x < p.W; x++ {
			dst.Set(x, y, nr.At(x, y))
		}
	}
	return dst
}

// TestResampleMode covers the image-rendering -> filter mapping used for <img>.
func TestResampleMode(t *testing.T) {
	if resampleMode(nil) != resample.Bicubic {
		t.Error("nil style should be bicubic")
	}
	if resampleMode(&css.Style{ImageRendering: css.IRAuto}) != resample.Bicubic {
		t.Error("IRAuto should be bicubic")
	}
	if resampleMode(&css.Style{ImageRendering: css.IRPixelated}) != resample.Nearest {
		t.Error("IRPixelated should be nearest")
	}
}

// TestResizeRasterFallback covers resizeRaster's success and defensive-error
// branches: a valid size resizes; a non-positive size makes go-gfx error and the
// helper returns the source unchanged.
func TestResizeRasterFallback(t *testing.T) {
	src := raster.New(8, 8)
	for i := range src.Pix {
		src.Pix[i] = 200
	}
	if got := resizeRaster(src, 4, 4, resample.Bicubic); got.W != 4 || got.H != 4 {
		t.Errorf("resize to 4x4 gave %dx%d", got.W, got.H)
	}
	if got := resizeRaster(src, 0, 4, resample.Bicubic); got != src {
		t.Error("bad size should fall back to the source image")
	}
}
