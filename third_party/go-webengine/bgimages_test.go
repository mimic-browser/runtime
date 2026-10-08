// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package engine

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/dom"
)

// pngDataURI encodes a solid w×h PNG as a base64 data: URI.
func pngDataURI(t *testing.T, w, h int, c color.RGBA) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

func TestLoadBackgroundImagesDataURI(t *testing.T) {
	uri := pngDataURI(t, 3, 3, color.RGBA{10, 20, 30, 255})
	html := `<html><body><div style="background-image: url('` + uri + `')">x</div></body></html>`
	root, err := dom.Parse(html)
	if err != nil {
		t.Fatal(err)
	}
	doc := &Document{URL: "https://example.com/", Root: root}
	sm := css.CascadeVW(root, 1024, nil)

	e := New()
	got := e.loadBackgroundImages(context.Background(), doc, sm)
	if got == nil {
		t.Fatal("expected a background bitmap, got nil")
	}
	img, ok := got[uri]
	if !ok {
		t.Fatalf("no entry for the data URI; keys=%v", keys(got))
	}
	if img.Bounds().Dx() != 3 || img.Bounds().Dy() != 3 {
		t.Errorf("decoded size = %v want 3x3", img.Bounds())
	}
}

func TestLoadBackgroundImagesNoneAndGradient(t *testing.T) {
	// A gradient-only background needs no bitmap; the result is nil.
	html := `<html><body><div style="background-image: linear-gradient(red, blue)">x</div></body></html>`
	root, _ := dom.Parse(html)
	doc := &Document{URL: "https://example.com/", Root: root}
	sm := css.CascadeVW(root, 1024, nil)
	if got := New().loadBackgroundImages(context.Background(), doc, sm); got != nil {
		t.Errorf("gradient-only should need no bitmaps, got %v", keys(got))
	}
	// A document with no background-image at all also returns nil.
	root2, _ := dom.Parse(`<html><body><div>x</div></body></html>`)
	doc2 := &Document{URL: "https://example.com/", Root: root2}
	sm2 := css.CascadeVW(root2, 1024, nil)
	if got := New().loadBackgroundImages(context.Background(), doc2, sm2); got != nil {
		t.Errorf("no bg-image should return nil, got %v", keys(got))
	}
}

// TestLoadBackgroundImagesSVG covers a real regression: background-image
// (and mask-image, which shares this loader) never actually decoded an SVG
// source — codec.Decode is raster-only — so any SVG background silently
// rendered as nothing, regardless of whether the URL was even correctly
// classified as vector. This case is the easy one: the data URI's own
// "image/svg" media type identifies it without needing to look at the bytes.
func TestLoadBackgroundImagesSVG(t *testing.T) {
	svg := `<svg xmlns="http://www.w3.org/2000/svg" width="4" height="4" viewBox="0 0 4 4"><rect width="4" height="4" fill="#ff0000"/></svg>`
	uri := "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(svg))
	html := `<html><body><div style="background-image: url('` + uri + `')">x</div></body></html>`
	root, err := dom.Parse(html)
	if err != nil {
		t.Fatal(err)
	}
	doc := &Document{URL: "https://example.com/", Root: root}
	sm := css.CascadeVW(root, 1024, nil)
	got := New().loadBackgroundImages(context.Background(), doc, sm)
	img, ok := got[uri]
	if !ok {
		t.Fatalf("no entry for the SVG data URI; keys=%v", keys(got))
	}
	if img.Bounds().Dx() <= 0 || img.Bounds().Dy() <= 0 {
		t.Errorf("SVG decoded to an empty image: %v", img.Bounds())
	}
}

// TestLoadBackgroundImagesSVGSniffedFromBytes covers the harder, real-world
// case that motivated this fix: a `mask-image`/background-image URL that
// looks nothing like SVG at all — no ".svg" path, no "image/svg" anywhere in
// the URL string — because the server decides the content type dynamically.
// Confirmed live: EVERY icon in Wikipedia's Vector-2022 skin is served this
// way (an extensionless MediaWiki `load.php?...&image=menu&format=original`
// URL, `Content-Type: image/svg+xml` only in the HTTP response). Only
// sniffing the fetched BYTES (looksLikeSVG's fallback) can identify this.
func TestLoadBackgroundImagesSVGSniffedFromBytes(t *testing.T) {
	svg := `<svg xmlns="http://www.w3.org/2000/svg" width="4" height="4" viewBox="0 0 4 4"><rect width="4" height="4" fill="#00ff00"/></svg>`
	// A generic media type carries no "svg" hint at all; only content-sniffing
	// the decoded bytes (not the data: URI string) can identify this as SVG.
	uri := "data:application/octet-stream;base64," + base64.StdEncoding.EncodeToString([]byte(svg))
	html := `<html><body><div style="mask-image: url('` + uri + `')">x</div></body></html>`
	root, err := dom.Parse(html)
	if err != nil {
		t.Fatal(err)
	}
	doc := &Document{URL: "https://example.com/", Root: root}
	sm := css.CascadeVW(root, 1024, nil)
	got := New().loadBackgroundImages(context.Background(), doc, sm)
	img, ok := got[uri]
	if !ok {
		t.Fatalf("no entry for the sniffed SVG mask; keys=%v", keys(got))
	}
	if img.Bounds().Dx() <= 0 || img.Bounds().Dy() <= 0 {
		t.Errorf("sniffed SVG decoded to an empty image: %v", img.Bounds())
	}
}

// TestLoadBackgroundImagesMaskImageURL covers that mask-image shares the
// SAME fetch/decode/dedup path as background-image (see loadBackgroundImages
// and loadOneBackground's doc comments) — a real regression, since
// `mask-image` URLs were never collected here at all before this fix.
func TestLoadBackgroundImagesMaskImageURL(t *testing.T) {
	uri := pngDataURI(t, 3, 3, color.RGBA{4, 5, 6, 255})
	html := `<html><body><div style="mask-image: url('` + uri + `')">x</div></body></html>`
	root, err := dom.Parse(html)
	if err != nil {
		t.Fatal(err)
	}
	doc := &Document{URL: "https://example.com/", Root: root}
	sm := css.CascadeVW(root, 1024, nil)
	got := New().loadBackgroundImages(context.Background(), doc, sm)
	if _, ok := got[uri]; !ok {
		t.Fatalf("no entry for the mask-image url; keys=%v", keys(got))
	}
}

// TestLoadBackgroundImagesBackgroundAndMaskDedup covers that a background-
// image and a mask-image sharing the exact same URL fetch/decode it only
// once — they share one cache, keyed by URL alone.
func TestLoadBackgroundImagesBackgroundAndMaskDedup(t *testing.T) {
	uri := pngDataURI(t, 2, 2, color.RGBA{7, 8, 9, 255})
	html := `<html><body>` +
		`<div style="background-image: url('` + uri + `')">a</div>` +
		`<div style="mask-image: url('` + uri + `')">b</div>` +
		`</body></html>`
	root, err := dom.Parse(html)
	if err != nil {
		t.Fatal(err)
	}
	doc := &Document{URL: "https://example.com/", Root: root}
	sm := css.CascadeVW(root, 1024, nil)
	got := New().loadBackgroundImages(context.Background(), doc, sm)
	if len(got) != 1 {
		t.Errorf("dedup across background-image/mask-image failed: %d entries, keys=%v", len(got), keys(got))
	}
}

func TestLoadBackgroundImagesUndecodable(t *testing.T) {
	// A data URI whose payload is not a valid image fails to decode and is skipped.
	bad := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("not a png"))
	html := `<html><body><div style="background-image: url('` + bad + `')">x</div></body></html>`
	root, _ := dom.Parse(html)
	doc := &Document{URL: "https://example.com/", Root: root}
	sm := css.CascadeVW(root, 1024, nil)
	if got := New().loadBackgroundImages(context.Background(), doc, sm); got != nil {
		t.Errorf("undecodable image should be skipped (nil), got %v", keys(got))
	}
}

func TestLoadBackgroundImagesDedup(t *testing.T) {
	uri := pngDataURI(t, 2, 2, color.RGBA{1, 2, 3, 255})
	// Two elements referencing the same URL fetch/decode it once.
	html := `<html><body>` +
		`<div style="background-image: url('` + uri + `')">a</div>` +
		`<div style="background-image: url('` + uri + `')">b</div>` +
		`</body></html>`
	root, _ := dom.Parse(html)
	doc := &Document{URL: "https://example.com/", Root: root}
	sm := css.CascadeVW(root, 1024, nil)
	got := New().loadBackgroundImages(context.Background(), doc, sm)
	if len(got) != 1 {
		t.Errorf("dedup failed: %d entries, keys=%v", len(got), keys(got))
	}
}

func keys(m map[string]image.Image) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		if len(k) > 40 {
			k = k[:40] + "..."
		}
		out = append(out, k)
	}
	return out
}
