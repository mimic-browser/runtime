// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package engine

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/go-webengine/engine/dom"
	"github.com/go-webengine/engine/layout"
	"github.com/go-webengine/engine/paint"
)

// boxText concatenates every visible text run in a laid-out tree.
func allBoxText(b *layout.Box) string {
	var sb strings.Builder
	var walk func(x *layout.Box)
	walk = func(x *layout.Box) {
		if x == nil {
			return
		}
		for _, ln := range x.Lines {
			for _, it := range ln.Items {
				sb.WriteString(it.Text)
				sb.WriteByte(' ')
			}
		}
		for _, c := range x.Children {
			walk(c)
		}
	}
	walk(b)
	return sb.String()
}

func hasVisibleImage(b *layout.Box) bool {
	if b == nil {
		return false
	}
	for _, ln := range b.Lines {
		for _, it := range ln.Items {
			if it.Image != nil {
				return true
			}
		}
	}
	for _, c := range b.Children {
		if hasVisibleImage(c) {
			return true
		}
	}
	return false
}

func TestRenderedEmpty(t *testing.T) {
	// Truly empty: no content, zero height.
	if !renderedEmpty(&layout.Box{}, 0) {
		t.Error("empty box should be renderedEmpty")
	}
	// Tall content height defeats the trigger regardless of text.
	if renderedEmpty(&layout.Box{}, 100) {
		t.Error("height >= threshold must not be renderedEmpty")
	}
	// Enough visible text defeats the trigger even at zero height.
	textBox := &layout.Box{Lines: []*layout.LineBox{{Items: []*layout.InlineItem{
		{Text: "this is well over thirty two characters of real body text"},
	}}}}
	if renderedEmpty(textBox, 0) {
		t.Error("box with real text must not be renderedEmpty")
	}
}

// TestMetaFallbackRendersCard covers the SPA case: an empty body with OG tags
// (title + description + image) synthesises a readable card carrying that text
// and image, and a link to the source URL.
func TestMetaFallbackRendersCard(t *testing.T) {
	e := New()
	e.MetaFallback = true
	e.DisableJS = true
	ogImg := pngDataURI(t, 6, 6, color.RGBA{20, 120, 200, 255})
	src := `<html><head>` +
		`<meta property="og:site_name" content="Test Social">` +
		`<meta property="og:title" content="The Post Title Here">` +
		`<meta property="og:description" content="This is the post body from opengraph description tag.">` +
		`<meta property="og:image" content="` + ogImg + `">` +
		`<meta property="og:url" content="https://src.test/post/1">` +
		`<title>tab title</title>` +
		`</head><body><div id="app"></div></body></html>` // empty app shell

	root, err := dom.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	doc := &Document{URL: "https://src.test/post/1", Root: root}

	// The fallback pass carries the OG text + image.
	fb := e.buildMetaFallback(context.Background(), doc, 900, paint.NewFonts())
	if fb == nil {
		t.Fatal("buildMetaFallback returned nil for a page with OG tags")
	}
	txt := allBoxText(fb.box)
	if !strings.Contains(txt, "The Post Title Here") {
		t.Errorf("card missing og:title; text=%q", txt)
	}
	if !strings.Contains(txt, "opengraph description") {
		t.Errorf("card missing og:description; text=%q", txt)
	}
	if !strings.Contains(txt, "Test Social") {
		t.Errorf("card missing og:site_name; text=%q", txt)
	}
	if !hasVisibleImage(fb.box) {
		t.Error("card missing og:image")
	}

	// End to end: RenderDocumentWithLinks swaps in the card (non-empty height) and
	// exposes the source URL as the card's link.
	img, info, links, err := e.RenderDocumentWithLinks(context.Background(), doc, image.Rect(0, 0, 900, 120))
	if err != nil {
		t.Fatal(err)
	}
	if info.ContentHeight < emptyRenderHeight {
		t.Errorf("card content height %d too small (fallback did not render)", info.ContentHeight)
	}
	if img.Rect.Dy() < emptyRenderHeight {
		t.Errorf("card image too short: %d", img.Rect.Dy())
	}
	if len(links) != 1 || links[0].Href != "https://src.test/post/1" {
		t.Errorf("expected 1 link to the source URL, got %+v", links)
	}
}

// TestMetaFallbackNotTriggeredOnContent is the no-regression guard: a page that
// renders real content is byte-identical whether MetaFallback is on or off (the
// fallback must not trigger), even though the page also carries OG tags.
func TestMetaFallbackNotTriggeredOnContent(t *testing.T) {
	src := `<html><head>` +
		`<meta property="og:title" content="OG TITLE SHOULD NOT APPEAR">` +
		`<meta property="og:description" content="OG DESCRIPTION SHOULD NOT APPEAR">` +
		`</head><body style="margin:0"><h1>Real Heading</h1>` +
		`<p>Real article body paragraph with plenty of visible text content here.</p>` +
		`</body></html>`
	vp := image.Rect(0, 0, 900, 120)

	render := func(mfb bool) (*image.RGBA, int) {
		root, err := dom.Parse(src)
		if err != nil {
			t.Fatal(err)
		}
		e := New()
		e.DisableJS = true
		e.MetaFallback = mfb
		img, info, err := e.RenderDocument(context.Background(), &Document{URL: "https://x.test/", Root: root}, vp)
		if err != nil {
			t.Fatal(err)
		}
		return img, info.ContentHeight
	}
	off, hOff := render(false)
	on, hOn := render(true)

	if hOff != hOn {
		t.Errorf("content height changed with MetaFallback on: %d -> %d", hOff, hOn)
	}
	if off.Rect != on.Rect || !bytes.Equal(off.Pix, on.Pix) {
		t.Error("MetaFallback changed the render of a page with real content (must be byte-identical)")
	}
	// And the OG text must not have leaked into the real render.
	root, _ := dom.Parse(src)
	e := New()
	e.DisableJS = true
	e.MetaFallback = true
	rp := e.renderCore(context.Background(), &Document{URL: "https://x.test/", Root: root}, 900, 120, paint.NewFonts())
	if txt := allBoxText(rp.box); strings.Contains(txt, "SHOULD NOT APPEAR") {
		t.Errorf("OG fallback text leaked into a real-content render: %q", txt)
	}
}

// TestMetaFallbackNoMetadata: an empty page with no usable OG/meta stays blank
// (no card synthesised).
func TestMetaFallbackNoMetadata(t *testing.T) {
	e := New()
	root, _ := dom.Parse(`<html><head></head><body><div id="app"></div></body></html>`)
	fb := e.buildMetaFallback(context.Background(), &Document{URL: "https://x.test/", Root: root}, 900, paint.NewFonts())
	if fb != nil {
		t.Error("buildMetaFallback should return nil when there is no title/description")
	}
}
