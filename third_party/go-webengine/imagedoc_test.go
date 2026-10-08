// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package engine

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-webengine/engine/css"
)

// rasterPNG builds a w×h PNG: a solid red body with a 4px green border, so a
// test can measure exactly where the image lands in the rendered output.
func rasterPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.RGBA{220, 30, 30, 255}
			if x < 4 || y < 4 || x >= w-4 || y >= h-4 {
				c = color.RGBA{20, 160, 40, 255}
			}
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// solidJPEG builds a w×h solid-blue JPEG (a second raster format; JPEG of a
// flat colour is near-exact, so a "bluish" test is stable).
func solidJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i+0], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = 20, 40, 200, 255
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func px(img *image.RGBA, x, y int) color.RGBA {
	r, g, b, a := img.At(x, y).RGBA()
	return color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8)}
}

func nonWhite(c color.RGBA) bool { return c.R < 240 || c.G < 240 || c.B < 240 }

// contentBandExtent finds the row with the most pixels matching pred and returns
// the leftmost and rightmost matching x on that row (−1,−1 if none match).
func contentBandExtent(img *image.RGBA, pred func(color.RGBA) bool) (left, right int) {
	b := img.Bounds()
	bestRow, bestCount := -1, 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		count := 0
		for x := b.Min.X; x < b.Max.X; x++ {
			if pred(px(img, x, y)) {
				count++
			}
		}
		if count > bestCount {
			bestCount, bestRow = count, y
		}
	}
	if bestRow < 0 {
		return -1, -1
	}
	left, right = -1, -1
	for x := b.Min.X; x < b.Max.X; x++ {
		if pred(px(img, x, bestRow)) {
			if left < 0 {
				left = x
			}
			right = x
		}
	}
	return left, right
}

// assertSpansWidth asserts the content band reaches within tol px of both edges
// of a vpW-wide render (i.e. the image fills the viewport width).
func assertSpansWidth(t *testing.T, img *image.RGBA, vpW, tol int, pred func(color.RGBA) bool) {
	t.Helper()
	l, r := contentBandExtent(img, pred)
	if l < 0 {
		t.Fatalf("no content pixels found in %dpx render", vpW)
	}
	if l > tol {
		t.Errorf("content starts at x=%d, want <= %d (not flush to the left edge)", l, tol)
	}
	if r < vpW-1-tol {
		t.Errorf("content ends at x=%d, want >= %d (not spanning to the right edge of %dpx)", r, vpW-1-tol, vpW)
	}
}

// TestImageDocumentPNG: a standalone image/png renders as a document whose image
// spans the full viewport width, at two different widths (upscaled from its
// native 200×120 — proving it fills the width rather than sitting at native
// size).
func TestImageDocumentPNG(t *testing.T) {
	raw := rasterPNG(t, 200, 120)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(raw)
	}))
	defer srv.Close()

	for _, vpW := range []int{800, 400} {
		img, info, err := New().Render(context.Background(), srv.URL, image.Rect(0, 0, vpW, 600))
		if err != nil {
			t.Fatal(err)
		}
		assertSpansWidth(t, img, vpW, 2, nonWhite)
		// Native aspect ratio 200:120 → the upscaled height is vpW*120/200.
		wantH := vpW * 120 / 200
		if info.ContentHeight < wantH-2 || info.ContentHeight > wantH+2 {
			t.Errorf("vpW=%d: content height %d, want ~%d (aspect-preserved)", vpW, info.ContentHeight, wantH)
		}
	}
}

// TestImageDocumentJPEG: a second raster type also produces a full-width image
// document.
func TestImageDocumentJPEG(t *testing.T) {
	raw := solidJPEG(t, 160, 160)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(raw)
	}))
	defer srv.Close()

	bluish := func(c color.RGBA) bool { return c.B > 120 && c.R < 120 }
	for _, vpW := range []int{700, 350} {
		img, _, err := New().Render(context.Background(), srv.URL, image.Rect(0, 0, vpW, 500))
		if err != nil {
			t.Fatal(err)
		}
		assertSpansWidth(t, img, vpW, 2, bluish)
	}
}

// TestImageDocumentTitleAndURL: the synthesised document carries the URL's
// basename as its title and preserves the final URL and embeds the bytes as a
// data: URI (no re-fetch).
func TestImageDocumentTitleAndURL(t *testing.T) {
	raw := rasterPNG(t, 40, 40)
	mux := http.NewServeMux()
	mux.HandleFunc("/pics/cat.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(raw)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	doc, err := New().Fetch(context.Background(), srv.URL+"/pics/cat.png")
	if err != nil {
		t.Fatal(err)
	}
	if doc.Title != "cat.png" {
		t.Errorf("title = %q, want %q", doc.Title, "cat.png")
	}
	if doc.URL != srv.URL+"/pics/cat.png" {
		t.Errorf("url = %q, want the final image URL", doc.URL)
	}
	if !strings.Contains(doc.HTML, "data:image/png;base64,") {
		t.Errorf("expected an embedded data: URI in the synthesised document, got:\n%s", doc.HTML)
	}
	if !strings.Contains(doc.HTML, "width:100%") {
		t.Errorf("expected a full-width <img> in the synthesised document, got:\n%s", doc.HTML)
	}
}

// TestSVGResponseUnaffected: an image/svg+xml response is NOT wrapped in an
// image document — it flows through the existing inline-<svg> path unchanged, so
// its shape renders (non-white pixels) and the document HTML is the served SVG,
// not a synthesised <img> card.
func TestSVGResponseUnaffected(t *testing.T) {
	const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="100" height="100" viewBox="0 0 100 100"><rect x="0" y="0" width="100" height="100" fill="#c02020"/></svg>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		_, _ = w.Write([]byte(svg))
	}))
	defer srv.Close()

	doc, err := New().Fetch(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc.HTML, "data:image/svg") || strings.Contains(doc.HTML, "<!doctype html>") {
		t.Errorf("SVG response should NOT be wrapped in a synthesised image document; HTML:\n%s", doc.HTML)
	}
	img, _, err := New().Render(context.Background(), srv.URL, image.Rect(0, 0, 400, 300))
	if err != nil {
		t.Fatal(err)
	}
	if l, _ := contentBandExtent(img, nonWhite); l < 0 {
		t.Error("served SVG rendered blank; the inline-<svg> path should have drawn its rect")
	}
}

// TestHTMLResponseUnaffected: a normal text/html response still parses as HTML
// (not wrapped), so its content is preserved.
func TestHTMLResponseUnaffected(t *testing.T) {
	const page = `<html><head><title>Hello</title></head><body><h1>A heading here</h1><p>Body text content.</p></body></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(page))
	}))
	defer srv.Close()

	doc, err := New().Fetch(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Title != "Hello" {
		t.Errorf("title = %q, want %q", doc.Title, "Hello")
	}
	if !strings.Contains(doc.HTML, "A heading here") {
		t.Errorf("HTML response was altered; got:\n%s", doc.HTML)
	}
	if strings.Contains(doc.HTML, "data:image/") {
		t.Errorf("HTML response should not be wrapped in an image document")
	}
}

// TestImageDocumentProgressive: the progressive path (used by the reader) also
// produces the full-width image document, and the final frame spans the width.
func TestImageDocumentProgressive(t *testing.T) {
	raw := rasterPNG(t, 200, 120)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(raw)
	}))
	defer srv.Close()

	var final *image.RGBA
	_, err := New().RenderProgressive(context.Background(), srv.URL, image.Rect(0, 0, 800, 600), func(f ProgressiveFrame) {
		if f.Final {
			final = f.Img
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if final == nil {
		t.Fatal("no final frame emitted")
	}
	assertSpansWidth(t, final, 800, 2, nonWhite)
}

// TestImageContentType covers the content-type classifier's branches.
func TestImageContentType(t *testing.T) {
	cases := []struct {
		ct      string
		wantMT  string
		wantOK  bool
		wantSVG bool
	}{
		{"image/png", "image/png", true, false},
		{"image/jpeg; charset=binary", "image/jpeg", true, false},
		{"IMAGE/WEBP", "image/webp", true, false},
		{"image/gif", "image/gif", true, false},
		{"image/bmp", "image/bmp", true, false},
		{"image/svg+xml", "image/svg+xml", false, true},
		{"text/html; charset=utf-8", "", false, false},
		{"application/json", "", false, false},
		{"", "", false, false},
		{"image/png ;;bogus", "image/png", true, false}, // strict parse fails → fallback trims to bare type
	}
	for _, c := range cases {
		mt, ok, svg := imageContentType(c.ct)
		if mt != c.wantMT || ok != c.wantOK || svg != c.wantSVG {
			t.Errorf("imageContentType(%q) = (%q,%v,%v), want (%q,%v,%v)",
				c.ct, mt, ok, svg, c.wantMT, c.wantOK, c.wantSVG)
		}
	}
}

// TestImageDocTitle covers title derivation from a URL.
func TestImageDocTitle(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://i.redd.it/abc123.jpg", "abc123.jpg"},
		{"https://example.com/a/b/photo.png?x=1#frag", "photo.png"},
		{"https://example.com/", "example.com"},
		{"https://example.com", "example.com"},
		{"://bad::url", "://bad::url"},
	}
	for _, c := range cases {
		if got := imageDocTitle(c.in); got != c.want {
			t.Errorf("imageDocTitle(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestImageDocumentHTMLFallsBackToURL: bytes over the data-URI budget point the
// <img> src at the final URL instead of inlining a huge base64 string.
func TestImageDocumentHTMLFallsBackToURL(t *testing.T) {
	big := bytes.Repeat([]byte{0xAB}, maxImageDocDataURI+1)
	html := imageDocumentHTML("image/png", big, "https://example.com/big.png", "big.png")
	if strings.Contains(html, "base64,") {
		t.Error("oversized image should NOT be inlined as a data: URI")
	}
	if !strings.Contains(html, `src="https://example.com/big.png"`) {
		t.Errorf("oversized image should use the final URL as src; got:\n%s", html)
	}
	small := imageDocumentHTML("image/png", []byte{1, 2, 3}, "https://example.com/s.png", "s.png")
	if !strings.Contains(small, "data:image/png;base64,") {
		t.Errorf("small image should inline a data: URI; got:\n%s", small)
	}
}

// TestPercentImageWidth covers the percentage-width resolver.
func TestPercentImageWidth(t *testing.T) {
	full := &css.Style{}
	full.Width = css.Length{IsPercent: true, Percent: 1.0}
	if w, ok := percentImageWidth(full, 800); !ok || w != 800 {
		t.Errorf("100%% of 800 = (%d,%v), want (800,true)", w, ok)
	}
	half := &css.Style{}
	half.Width = css.Length{IsPercent: true, Percent: 0.5}
	if w, ok := percentImageWidth(half, 800); !ok || w != 400 {
		t.Errorf("50%% of 800 = (%d,%v), want (400,true)", w, ok)
	}
	if _, ok := percentImageWidth(full, 0); ok {
		t.Error("zero viewport should report false")
	}
	if _, ok := percentImageWidth(nil, 800); ok {
		t.Error("nil style should report false")
	}
	pxOnly := &css.Style{}
	pxOnly.Width = css.Length{Px: 120}
	if _, ok := percentImageWidth(pxOnly, 800); ok {
		t.Error("a definite px width is not a percentage; should report false")
	}
}
