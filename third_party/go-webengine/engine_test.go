// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package engine

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/dom"
)

func TestResolveURL(t *testing.T) {
	cases := []struct {
		base, ref, want string
		ok              bool
	}{
		{"https://a.com/x/y", "z.png", "https://a.com/x/z.png", true},
		{"https://a.com/", "https://b.com/q", "https://b.com/q", true},
		{"https://a.com/", "//cdn.com/i.png", "https://cdn.com/i.png", true},
		{"https://a.com/", "  ", "", false},
		{"://bad", "x", "", false},
	}
	for _, c := range cases {
		got, ok := resolveURL(c.base, c.ref)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("resolveURL(%q,%q) = %q,%v want %q,%v", c.base, c.ref, got, ok, c.want, c.ok)
		}
	}
}

func TestDecodeDataURI(t *testing.T) {
	// base64 payload "Hi" = "SGk="
	if b, ok := decodeDataURI("data:text/plain;base64,SGk="); !ok || string(b) != "Hi" {
		t.Errorf("base64 data uri = %q %v", b, ok)
	}
	if b, ok := decodeDataURI("data:text/plain,Hi"); !ok || string(b) != "Hi" {
		t.Errorf("plain data uri = %q %v", b, ok)
	}
	if _, ok := decodeDataURI("data:base64,%%%"); ok {
		t.Error("bad base64 should fail")
	}
	if _, ok := decodeDataURI("data:nocomma"); ok {
		t.Error("no-comma should fail")
	}
}

func TestDecodeCharset(t *testing.T) {
	out, err := decodeCharset([]byte("hello"), "text/html; charset=utf-8")
	if err != nil || string(out) != "hello" {
		t.Errorf("utf8 = %q %v", out, err)
	}
	// Latin-1 0xE9 (é) declared via header is converted to UTF-8 (0xC3 0xA9).
	out, err = decodeCharset([]byte{0xE9}, "text/html; charset=iso-8859-1")
	if err != nil || !bytes.Equal(out, []byte{0xC3, 0xA9}) {
		t.Errorf("latin1 = %v %v", out, err)
	}
}

func TestFillHelpers(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	fillWhite(img)
	if c := img.RGBAAt(0, 0); c.R != 255 || c.A != 255 {
		t.Errorf("fillWhite = %+v", c)
	}
	fillColor(img, css.Color{R: 10, G: 20, B: 30, A: 255})
	if c := img.RGBAAt(1, 1); c.R != 10 || c.G != 20 || c.B != 30 || c.A != 255 {
		t.Errorf("fillColor = %+v", c)
	}
}

func TestPageBackground(t *testing.T) {
	root, _ := dom.Parse(`<html><body style="background-color:#eee">x</body></html>`)
	sm := css.Cascade(root)
	if c, ok := pageBackground(root, sm); !ok || c.R != 0xee {
		t.Errorf("body bg = %v %v", c, ok)
	}
	// html background used when body has none.
	root2, _ := dom.Parse(`<html style="background-color:navy"><body>x</body></html>`)
	sm2 := css.Cascade(root2)
	if c, ok := pageBackground(root2, sm2); !ok || c != (css.Color{R: 0, G: 0, B: 128, A: 255}) {
		t.Errorf("html bg = %v %v", c, ok)
	}
	// none set → not ok.
	root3, _ := dom.Parse(`<html><body>x</body></html>`)
	if _, ok := pageBackground(root3, css.Cascade(root3)); ok {
		t.Error("expected no background")
	}
}

func TestEncodePNG(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 3, 3))
	b, err := EncodePNG(img)
	if err != nil || len(b) == 0 {
		t.Fatalf("encode = %d %v", len(b), err)
	}
	if got, _, err := image.Decode(bytes.NewReader(b)); err != nil || got.Bounds().Dx() != 3 {
		t.Errorf("decode roundtrip failed: %v", err)
	}
}

const goldenFixture = `<html><head><title>Fix</title>` +
	`<style>body{background-color:#eeeeee} h1{color:#003366} a{color:#0000ee}</style>` +
	`</head><body style="margin:10px">` +
	`<h1>Hi</h1><p>Hello <strong>world</strong> and <a href="#">link</a>.</p>` +
	`</body></html>`

func TestRenderDocumentGolden(t *testing.T) {
	root, err := dom.Parse(goldenFixture)
	if err != nil {
		t.Fatal(err)
	}
	doc := &Document{URL: "https://fixture.test/", Title: dom.Title(root), Root: root, HTML: goldenFixture}

	e := New()
	img, info, err := e.RenderDocument(context.Background(), doc, image.Rect(0, 0, 320, 160))
	if err != nil {
		t.Fatal(err)
	}
	// Precise, glyph-independent invariants.
	if img.Rect.Dx() != 320 {
		t.Errorf("width = %d", img.Rect.Dx())
	}
	if info.Title != "Fix" {
		t.Errorf("title = %q", info.Title)
	}
	if info.ContentHeight <= 0 {
		t.Errorf("content height = %d", info.ContentHeight)
	}
	// Backdrop (#eeeeee) fills the viewport, including the top-left corner.
	if c := img.RGBAAt(1, 1); c.R != 0xee || c.G != 0xee || c.B != 0xee {
		t.Errorf("backdrop pixel = %+v want #eeeeee", c)
	}

	png, err := EncodePNG(img)
	if err != nil {
		t.Fatal(err)
	}
	goldenPath := filepath.Join("testdata", "golden", "fixture.png")
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, png, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Log("golden updated")
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (run with UPDATE_GOLDEN=1 to create): %v", err)
	}
	if !bytes.Equal(png, want) {
		t.Errorf("render does not match golden fixture.png (%d vs %d bytes)", len(png), len(want))
	}
}

// TestLiveRender exercises the full network path when connectivity is
// available; it skips (not fails) when the network is unreachable.
func TestLiveRender(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	img, info, err := New().Render(ctx, "https://example.com/", image.Rect(0, 0, 1024, 768))
	if err != nil {
		t.Skipf("network unavailable: %v", err)
	}
	if img.Rect.Dx() != 1024 {
		t.Errorf("width = %d", img.Rect.Dx())
	}
	if info.Title == "" {
		t.Error("expected a non-empty title")
	}
	if b, err := New().Screenshot(ctx, "https://example.com/", image.Rect(0, 0, 400, 300)); err != nil || len(b) == 0 {
		t.Errorf("screenshot = %d %v", len(b), err)
	}
}

func TestRenderDocumentWithDataImage(t *testing.T) {
	// Build a data: URI PNG so the whole image pipeline runs offline: an 8x8
	// image at a 4px viewport also exercises the downscale-to-fit branch.
	src := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for i := 0; i < len(src.Pix); i += 4 {
		src.Pix[i], src.Pix[i+1], src.Pix[i+2], src.Pix[i+3] = 200, 0, 0, 255
	}
	png, err := EncodePNG(src)
	if err != nil {
		t.Fatal(err)
	}
	uri := "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
	html := `<html><body style="margin:0"><p><img src="` + uri + `"></p>` +
		`<img style="display:none" src="` + uri + `"><img></body></html>`
	root, _ := dom.Parse(html)
	doc := &Document{URL: "https://x.test/", Root: root}

	img, _, err := New().RenderDocument(context.Background(), doc, image.Rect(0, 0, 4, 30))
	if err != nil {
		t.Fatal(err)
	}
	// The 8x8 red image, scaled to fit width 4, drew reddish pixels somewhere
	// (its exact y depends on the <p> UA top margin).
	if !hasReddish(img) {
		t.Error("expected a reddish pixel from the scaled data-URI image")
	}
}

func hasReddish(img *image.RGBA) bool {
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if c := img.RGBAAt(x, y); c.R > 150 && c.G < 60 && c.B < 60 {
				return true
			}
		}
	}
	return false
}

func TestMaxImagesCap(t *testing.T) {
	e := New()
	e.MaxImages = 0 // no images loaded regardless of markup
	root, _ := dom.Parse(`<html><body><img src="data:image/png;base64,AAAA"></body></html>`)
	doc := &Document{URL: "https://x.test/", Root: root}
	sizes, bitmaps := e.loadImages(context.Background(), doc, css.Cascade(root), 100)
	if len(sizes) != 0 || len(bitmaps) != 0 {
		t.Errorf("MaxImages=0 should load nothing, got %d/%d", len(sizes), len(bitmaps))
	}
}

func TestFetchImageBytesSchemes(t *testing.T) {
	e := New()
	// data: URI path.
	if b, ok := e.fetchImageBytes(context.Background(), "https://a/", "data:text/plain,Hi"); !ok || string(b) != "Hi" {
		t.Errorf("data uri = %q %v", b, ok)
	}
	// Non-http scheme is rejected without a network call.
	if _, ok := e.fetchImageBytes(context.Background(), "https://a/", "ftp://x/i.png"); ok {
		t.Error("ftp scheme should be rejected")
	}
	// Empty/unresolvable src rejected.
	if _, ok := e.fetchImageBytes(context.Background(), "https://a/", "   "); ok {
		t.Error("empty src should be rejected")
	}
}

// mapImageCache is a tiny in-memory ImageCache for the hook test.
type mapImageCache struct {
	mu sync.Mutex
	m  map[string][]byte
}

func (c *mapImageCache) Get(url string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, ok := c.m[url]
	return b, ok
}
func (c *mapImageCache) Put(url string, data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[url] = data
}

func TestFetchImageBytesUsesCache(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Write([]byte("PNGDATA"))
	}))
	defer srv.Close()

	e := New()
	e.Client = srv.Client()
	e.ImageCache = &mapImageCache{m: map[string][]byte{}}
	ctx := context.Background()

	// Miss → downloads and populates the cache.
	if b, ok := e.fetchImageBytes(ctx, srv.URL+"/", srv.URL+"/a.png"); !ok || string(b) != "PNGDATA" {
		t.Fatalf("first fetch = %q %v", b, ok)
	}
	if atomic.LoadInt32(&hits) != 1 {
		t.Fatalf("first fetch hits = %d, want 1", hits)
	}
	// Hit → served from the cache with no network.
	if b, ok := e.fetchImageBytes(ctx, srv.URL+"/", srv.URL+"/a.png"); !ok || string(b) != "PNGDATA" {
		t.Fatalf("cached fetch = %q %v", b, ok)
	}
	if n := atomic.LoadInt32(&hits); n != 1 {
		t.Fatalf("second fetch hit the network (%d requests); cache should have served it", n)
	}
}

func TestPackageWrappersBadURL(t *testing.T) {
	// Package-level wrappers propagate errors from an unresolvable URL.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, _, err := Render(ctx, "http://nonexistent.invalid.", image.Rect(0, 0, 10, 10)); err == nil {
		t.Skip("resolver unexpectedly succeeded")
	}
	if _, err := Screenshot(ctx, "://bad-url", image.Rect(0, 0, 10, 10)); err == nil {
		t.Error("expected error for bad url")
	}
}

func TestRenderHTMLOffline(t *testing.T) {
	e := New()
	src := `<html><head><title>Offline</title></head><body style="margin:0">` +
		`<div style="display:flex"><div style="width:100px">A</div>` +
		`<div style="width:100px">B</div></div></body></html>`
	img, info, err := e.RenderHTML(context.Background(), src, "https://demo.test/", image.Rect(0, 0, 400, 200))
	if err != nil {
		t.Fatal(err)
	}
	if info.Title != "Offline" {
		t.Errorf("title = %q", info.Title)
	}
	if img.Rect.Dx() != 400 {
		t.Errorf("width = %d", img.Rect.Dx())
	}
}

func TestRenderHTMLParseError(t *testing.T) {
	// html.Parse tolerates almost anything, so verify a normal string succeeds
	// and the entry point is exercised end-to-end without a network fetch.
	if _, _, err := New().RenderHTML(context.Background(), "<p>hi</p>", "", image.Rect(0, 0, 50, 50)); err != nil {
		t.Fatalf("RenderHTML minimal doc: %v", err)
	}
}

// TestTemplateContentNotRendered proves a <template>'s children never lay out
// or paint, end to end. Per the HTML spec they live in the element's own
// .content DocumentFragment, never the normal document tree — no browser
// renders them. This engine has no separate content-fragment concept and no
// Shadow DOM, so a real page's declarative-shadow-DOM markup
// (<template shadowrootmode="open">, used throughout developer.mozilla.org's
// header/menu web components) would otherwise be parsed straight into the
// light-DOM tree and painted inline: live on MDN this showed up as 23 unstyled
// nav trees stacked on top of the article text. A huge font-size inside the
// template makes the failure mode unmissable: if it ever laid out, the page
// would be thousands of pixels taller than the two real paragraphs around it.
func TestTemplateContentNotRendered(t *testing.T) {
	src := `<html><body><p>before</p>` +
		`<template><p style="font-size:400px">SHOULD-NOT-RENDER</p></template>` +
		`<p>after</p></body></html>`
	_, info, err := New().RenderHTML(context.Background(), src, "https://demo.test/", image.Rect(0, 0, 400, 100))
	if err != nil {
		t.Fatal(err)
	}
	if info.ContentHeight > 100 {
		t.Fatalf("template content appears to have been laid out: contentHeight=%d, want roughly two paragraphs' worth", info.ContentHeight)
	}
}

// TestMaxExternalSheetsCoversAModernComponentStyledSite is the regression
// guard for a live bug: github.com/golang/go ships 38 separate
// <link rel=stylesheet> tags (per-component CSS modules plus several
// mutually-exclusive colour-scheme variants), and the one sheet holding its
// header's hide/sr-only rules happened to be 38th — past the old cap of 20,
// so it was silently dropped and the header's mega-menu markup rendered
// fully unstyled and visible. This serves exactly that many stylesheets (one
// past the OLD cap, several past it) and asserts a rule from the LAST one is
// still applied.
func TestMaxExternalSheetsCoversAModernComponentStyledSite(t *testing.T) {
	const totalSheets = 38 // github.com/golang/go's real count, the live repro
	if totalSheets > maxExternalSheets {
		t.Fatalf("test setup: totalSheets=%d exceeds maxExternalSheets=%d — the regression this test guards would no longer be exercised", totalSheets, maxExternalSheets)
	}
	if totalSheets <= 20 {
		t.Fatalf("test setup: totalSheets=%d must exceed the OLD cap of 20 to actually exercise the fix", totalSheets)
	}

	mux := http.NewServeMux()
	var links strings.Builder
	for i := 0; i < totalSheets; i++ {
		i := i
		path := fmt.Sprintf("/sheet%d.css", i)
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/css")
			fmt.Fprintf(w, ".mark%d{color:rgb(%d,0,0)}", i, i%256)
		})
		fmt.Fprintf(&links, `<link rel="stylesheet" href="%s">`, path)
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<html><head>%s</head><body><div class="mark%d">last-sheet-rule</div></body></html>`, links.String(), totalSheets-1)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	e := New()
	e.Client = srv.Client()
	doc, err := e.Fetch(context.Background(), srv.URL+"/")
	if err != nil {
		t.Fatal(err)
	}
	sheets := e.fetchExternalSheets(context.Background(), doc, 1024)
	if len(sheets) != totalSheets {
		t.Fatalf("fetched %d sheets, want all %d", len(sheets), totalSheets)
	}
	lastRule := fmt.Sprintf(".mark%d{color:rgb(%d,0,0)}", totalSheets-1, (totalSheets-1)%256)
	found := false
	for _, s := range sheets {
		if strings.Contains(s, lastRule) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("the last (38th) stylesheet's rule was not fetched/applied — the exact shape of the live github.com bug")
	}
}

// TestMaxExternalSheetsStillBoundsAPathologicalPage proves the cap still
// exists: a page with far more stylesheets than maxExternalSheets does not
// fetch them all (this engine bounds per-render work; a browser has no such
// limit, but this one deliberately does).
func TestMaxExternalSheetsStillBoundsAPathologicalPage(t *testing.T) {
	total := maxExternalSheets + 20
	mux := http.NewServeMux()
	var links strings.Builder
	for i := 0; i < total; i++ {
		path := fmt.Sprintf("/sheet%d.css", i)
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/css")
			w.Write([]byte("body{}"))
		})
		fmt.Fprintf(&links, `<link rel="stylesheet" href="%s">`, path)
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<html><head>%s</head><body>x</body></html>`, links.String())
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	e := New()
	e.Client = srv.Client()
	doc, err := e.Fetch(context.Background(), srv.URL+"/")
	if err != nil {
		t.Fatal(err)
	}
	sheets := e.fetchExternalSheets(context.Background(), doc, 1024)
	if len(sheets) != maxExternalSheets {
		t.Fatalf("fetched %d sheets, want exactly the cap (%d)", len(sheets), maxExternalSheets)
	}
}

func TestLoadImagesExported(t *testing.T) {
	// The exported entry point must hand back the same two maps the internal
	// pipeline feeds to layout and paint — sizes for LayoutDocument, bitmaps
	// for a painter — keyed by the <img> element, offline via a data: URI, and
	// must leave a src-less / display:none <img> out of both.
	src := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for i := 0; i < len(src.Pix); i += 4 {
		src.Pix[i], src.Pix[i+1], src.Pix[i+2], src.Pix[i+3] = 0, 0, 200, 255
	}
	png, err := EncodePNG(src)
	if err != nil {
		t.Fatal(err)
	}
	uri := "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
	html := `<html><body style="margin:0"><img id="a" src="` + uri + `">` +
		`<img id="hidden" style="display:none" src="` + uri + `"><img id="nosrc"></body></html>`
	root, _ := dom.Parse(html)
	doc := &Document{URL: "https://x.test/", Root: root}
	sm := css.Cascade(root)

	sizes, bmps := New().LoadImages(context.Background(), doc, sm, 1024)

	var a *dom.Node
	var walk func(n *dom.Node)
	walk = func(n *dom.Node) {
		if n.Type == dom.Element && n.Tag == "img" {
			if id, _ := n.Attribute("id"); id == "a" {
				a = n
			}
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	if a == nil {
		t.Fatal("test <img id=a> not found in parsed DOM")
	}
	if got := sizes[a]; got != [2]float64{8, 8} {
		t.Errorf("sizes[a] = %v, want [8 8]", got)
	}
	bmp, ok := bmps[a]
	if !ok || bmp == nil {
		t.Fatalf("bitmaps[a] missing (ok=%v)", ok)
	}
	if b := bmp.Bounds(); b.Dx() != 8 || b.Dy() != 8 {
		t.Errorf("bitmaps[a] bounds = %v, want 8x8", b)
	}
	// One accepted image only: display:none and src-less are both excluded.
	if len(sizes) != 1 || len(bmps) != 1 {
		t.Errorf("len(sizes)=%d len(bitmaps)=%d, want 1 and 1", len(sizes), len(bmps))
	}
}
