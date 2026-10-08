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

// solidPNGDataURI builds a data: URI for a w×h PNG filled with col, so a fixture
// can carry a decodable image with zero network I/O (the loader decodes data:
// URIs synchronously through the same path as a fetched image).
func solidPNGDataURI(t *testing.T, w, h int, col color.RGBA) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, col)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

// countColor returns how many pixels of img exactly match col.
func countColor(img *image.RGBA, col color.RGBA) int {
	n := 0
	for i := 0; i+3 < len(img.Pix); i += 4 {
		if img.Pix[i] == col.R && img.Pix[i+1] == col.G && img.Pix[i+2] == col.B && img.Pix[i+3] == col.A {
			n++
		}
	}
	return n
}

// docFromHTML parses an HTML string into a Document (fresh parse).
func docFromHTML(t *testing.T, html string) *Document {
	t.Helper()
	root, err := dom.Parse(html)
	if err != nil {
		t.Fatal(err)
	}
	return &Document{URL: "https://fixture.test/", Title: dom.Title(root), Root: root, HTML: html}
}

// TestProgressiveTextFirstReservesAndDefersImage proves the text-first win: the
// "initial" frame is a fully-styled paint emitted BEFORE the image is loaded (so
// the image pixels are absent from it), with the image box already reserved from
// its width/height attributes. The decoded pixels appear in the final frame. The
// image's intrinsic size matches its attrs, so the geometry never moves and the
// "images" refinement frame is correctly deduped (initial+final only).
func TestProgressiveTextFirstReservesAndDefersImage(t *testing.T) {
	red := color.RGBA{R: 0xFF, G: 0x00, B: 0x00, A: 0xFF}
	uri := solidPNGDataURI(t, 60, 40, red)
	html := `<html><body><h1>Headline</h1>` +
		`<img width="60" height="40" src="` + uri + `">` +
		`<p>Body text under the image.</p></body></html>`

	e := New()
	doc := docFromHTML(t, html)
	var frames []ProgressiveFrame
	_, err := e.RenderDocumentProgressive(context.Background(), doc, image.Rect(0, 0, 400, 120),
		func(f ProgressiveFrame) { frames = append(frames, f) })
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 2 || frames[0].Stage != "initial" || frames[1].Stage != "final" || !frames[1].Final {
		t.Fatalf("stages = %v, want initial,final(true) (image reserved from attrs → images frame deduped)", stagesOf(frames))
	}
	// The image is NOT painted in the text-first initial frame …
	if got := countColor(frames[0].Img, red); got != 0 {
		t.Errorf("initial frame already contains %d image pixels; the styled text must paint before the image loads", got)
	}
	// … but IS painted in the final frame.
	if got := countColor(frames[1].Img, red); got == 0 {
		t.Error("final frame is missing the decoded image pixels")
	}
	// The initial frame reserved the image box from its width/height attrs, so its
	// content height already covers the image (initial geometry is sane).
	if frames[0].Info.ContentHeight < 40 {
		t.Errorf("initial content height %d < reserved image height 40 (box not reserved from attrs)", frames[0].Info.ContentHeight)
	}
}

// TestProgressiveImagesFrameOnReflow covers the "images" refinement stage: an
// <img> with no width/height reserves nothing in the text-first frame, so
// loading its bitmap grows the geometry and a distinct "images" frame is emitted
// (not deduped) before the final one.
func TestProgressiveImagesFrameOnReflow(t *testing.T) {
	uri := solidPNGDataURI(t, 80, 50, color.RGBA{G: 0xFF, A: 0xFF})
	html := `<html><body><h1>Headline</h1><img src="` + uri + `"></body></html>`

	e := New()
	doc := docFromHTML(t, html)
	var frames []ProgressiveFrame
	_, err := e.RenderDocumentProgressive(context.Background(), doc, image.Rect(0, 0, 400, 120),
		func(f ProgressiveFrame) { frames = append(frames, f) })
	if err != nil {
		t.Fatal(err)
	}
	var sawImages bool
	for _, f := range frames {
		if f.Stage == "images" {
			sawImages = true
		}
	}
	if !sawImages {
		t.Fatalf("no \"images\" frame for an unsized image whose load reflows the page; stages = %v", stagesOf(frames))
	}
	// The reflow makes the final frame taller than the (image-collapsed) initial.
	if frames[len(frames)-1].Info.ContentHeight <= frames[0].Info.ContentHeight {
		t.Errorf("final height %d not greater than initial %d (image load did not reflow)",
			frames[len(frames)-1].Info.ContentHeight, frames[0].Info.ContentHeight)
	}
}

// stagesOf lists frame stages for diagnostics.
func stagesOf(frames []ProgressiveFrame) []string {
	s := make([]string, len(frames))
	for i, f := range frames {
		s[i] = f.Stage
	}
	return s
}

// TestLoadImagesParallelDeterministicAndBudgeted covers the concurrent loader:
// the accepted set is charged in document order against MaxImages BEFORE any
// fetch, so exactly the first-N images load, and two runs over the same document
// return byte-identical results regardless of the concurrent scheduling.
func TestLoadImagesParallelDeterministicAndBudgeted(t *testing.T) {
	colors := []color.RGBA{
		{R: 0xFF, A: 0xFF}, {G: 0xFF, A: 0xFF}, {B: 0xFF, A: 0xFF},
		{R: 0xFF, G: 0xFF, A: 0xFF}, {R: 0xFF, B: 0xFF, A: 0xFF},
	}
	html := "<html><body>"
	nodes := make([]string, len(colors))
	for i, c := range colors {
		nodes[i] = solidPNGDataURI(t, 8+i, 8+i, c) // distinct sizes → distinct intrinsic dims
		html += `<img src="` + nodes[i] + `">`
	}
	html += "</body></html>"

	e := New()
	e.MaxImages = 3 // budget below the 5 images present
	doc := docFromHTML(t, html)
	sm := css.CascadeVW(doc.Root, 400, nil)

	sizes1, bmps1 := e.loadImages(context.Background(), doc, sm, 400)
	// Budget: exactly MaxImages raster images accepted, in document order.
	if len(sizes1) != 3 || len(bmps1) != 3 {
		t.Fatalf("budget MaxImages=3 → want 3 loaded, got sizes=%d bitmaps=%d", len(sizes1), len(bmps1))
	}

	// Determinism: a second run yields the identical size map (same nodes, same
	// intrinsic dims) — the concurrent scheduling must not change the result.
	sizes2, _ := e.loadImages(context.Background(), doc, sm, 400)
	if len(sizes2) != len(sizes1) {
		t.Fatalf("second run loaded %d, first %d", len(sizes2), len(sizes1))
	}
	for n, wh := range sizes1 {
		if sizes2[n] != wh {
			t.Errorf("node %p size differs between runs: %v vs %v", n, sizes2[n], wh)
		}
	}

	// The accepted nodes are the first three in document order (the last two are
	// over budget and never loaded).
	var imgs []*dom.Node
	var walk func(*dom.Node)
	walk = func(n *dom.Node) {
		if n.Type == dom.Element && n.Tag == "img" {
			imgs = append(imgs, n)
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(doc.Root)
	for i, n := range imgs {
		_, loaded := sizes1[n]
		if want := i < 3; loaded != want {
			t.Errorf("img[%d] loaded=%v, want %v (document-order budget)", i, loaded, want)
		}
	}
}

// TestLoadImagesCancelledContext covers the cancellation branch: a cancelled
// context loads nothing.
func TestLoadImagesCancelledContext(t *testing.T) {
	uri := solidPNGDataURI(t, 10, 10, color.RGBA{R: 0xFF, A: 0xFF})
	doc := docFromHTML(t, `<html><body><img src="`+uri+`"></body></html>`)
	sm := css.CascadeVW(doc.Root, 400, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sizes, bmps := New().loadImages(ctx, doc, sm, 400)
	if len(sizes) != 0 || len(bmps) != 0 {
		t.Errorf("cancelled context loaded %d/%d, want 0/0", len(sizes), len(bmps))
	}
}
