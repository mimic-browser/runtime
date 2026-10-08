// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

// This file adds a hyperlink hit-map on top of the laid-out box tree: given a
// rendered page it reports the painted rectangle and resolved href of every
// <a href> anchor, so a consumer (e.g. the wasmdesk browserproxy) can turn a
// click at pixel (x, y) into a navigation target. It is kept deliberately in
// its own file — separate from engine.go and the js/ runtime work — so the
// hit-test can evolve without colliding with the rendering pipeline.
package engine

import (
	"context"
	"image"
	"net/url"
	"strings"

	"github.com/go-webengine/engine/dom"
	"github.com/go-webengine/engine/layout"
	"github.com/go-webengine/engine/paint"
)

// Link is one hyperlink's painted rectangle (in full-page image pixels, the
// same coordinate space as the image returned by Render) and its href resolved
// against the page URL to an absolute URL.
type Link struct {
	Rect image.Rectangle
	Href string
}

// LinksFromBox walks a laid-out box tree and returns, in document order, one
// Link per <a href> anchor that painted at least one inline atom (a word or an
// image). Each anchor's Rect is the union of the rectangles of all inline atoms
// it produced, so a multi-line link yields a single bounding box covering every
// line. baseURL is the page's own URL: each anchor's raw href is resolved
// against it, and links that resolve to a non-navigable scheme (javascript:,
// mailto:, an empty or pure-fragment href) are dropped.
//
// The rectangles are in the box tree's own coordinate space, which is the
// full-page image space (layout origin is 0,0 at the page top-left), so a
// caller can hit-test a full-page click directly against them.
func LinksFromBox(root *layout.Box, baseURL string) []Link {
	if root == nil {
		return nil
	}
	base, _ := url.Parse(baseURL)

	// Accumulate a bounding rect per anchor element, preserving first-seen order.
	rects := map[*dom.Node]image.Rectangle{}
	var order []*dom.Node
	var walk func(b *layout.Box)
	walk = func(b *layout.Box) {
		if b == nil {
			return
		}
		for _, ln := range b.Lines {
			for _, it := range ln.Items {
				a := anchorFor(it.Node)
				if a == nil {
					continue
				}
				r := itemRect(it)
				if r.Empty() {
					continue
				}
				if prev, ok := rects[a]; ok {
					rects[a] = prev.Union(r)
				} else {
					rects[a] = r
					order = append(order, a)
				}
			}
		}
		for _, c := range b.Children {
			walk(c)
		}
	}
	walk(root)

	out := make([]Link, 0, len(order))
	for _, a := range order {
		raw, _ := a.Attribute("href")
		href, ok := resolveHref(base, raw)
		if !ok {
			continue
		}
		out = append(out, Link{Rect: rects[a], Href: href})
	}
	return out
}

// LinkAt returns the href of the first link whose rectangle contains pt (a
// full-page pixel point) and whether one was found. Links are tested in
// document order, so an earlier anchor wins when rectangles overlap.
func LinkAt(links []Link, pt image.Point) (string, bool) {
	for _, l := range links {
		if pt.In(l.Rect) {
			return l.Href, true
		}
	}
	return "", false
}

// RenderWithLinks fetches url, renders it to a full-page image and, in the same
// layout pass, returns the anchor hit-map. It mirrors (*Engine).Render but also
// threads the laid-out box tree into LinksFromBox so the image and the link
// rectangles are guaranteed to describe the exact same layout. It is kept here
// (rather than folded into engine.go's RenderDocument) so the hit-test feature
// stays isolated from the core pipeline and the js/ runtime work.
func (e *Engine) RenderWithLinks(ctx context.Context, rawurl string, viewport image.Rectangle) (*image.RGBA, *RenderInfo, []Link, error) {
	doc, err := e.Fetch(ctx, rawurl)
	if err != nil {
		return nil, nil, nil, err
	}
	return e.RenderDocumentWithLinks(ctx, doc, viewport)
}

// RenderDocumentWithLinks renders an already-fetched Document and returns its
// image, render info and anchor hit-map from a single layout pass. It runs the
// same cascade → layout → paint pipeline as RenderDocument.
func (e *Engine) RenderDocumentWithLinks(ctx context.Context, doc *Document, viewport image.Rectangle) (*image.RGBA, *RenderInfo, []Link, error) {
	fonts := paint.NewFonts()
	vpW, vpH := viewportSize(viewport)
	// Share the exact cascade → JavaScript settle → layout pipeline that
	// RenderDocument uses, so the anchor hit-map reflects the JS-settled DOM (a
	// script that injects, moves or removes links is honoured) and the returned
	// image is identical to Render's for the same document.
	rp := e.renderCore(ctx, doc, vpW, vpH, fonts)
	img := e.newCanvas(doc, rp, viewport, vpW)
	paint.PaintFull(img, rp.box, fonts, rp.imgs, rp.bgImgs)

	links := LinksFromBox(rp.box, doc.URL)
	return img, renderInfo(doc, rp), links, nil
}

// itemRect returns an inline atom's painted rectangle in image pixels: itemBox
// (nav.go) rounded out, so a sub-pixel advance still yields a hit-testable
// width. For a word the height is the item's line height; for an image the
// image's height. Both use the atom's positioned top-left (X,Y) which layout
// fills in full-page space.
func itemRect(it *layout.InlineItem) image.Rectangle {
	x, y, w, h, ok := itemBox(it)
	if !ok {
		return image.Rectangle{}
	}
	x0, y0 := int(x), int(y)
	return image.Rect(x0, y0, x0+ceilPx(w), y0+ceilPx(h))
}

// anchorFor walks up from an inline atom's originating node to the nearest
// enclosing <a> element that carries a non-empty href, or nil.
func anchorFor(n *dom.Node) *dom.Node {
	for ; n != nil; n = n.Parent {
		if n.Type != dom.Element || n.Tag != "a" {
			continue
		}
		if href, ok := n.Attribute("href"); ok && strings.TrimSpace(href) != "" {
			return n
		}
	}
	return nil
}

// resolveHref resolves a raw href against the page base and reports whether the
// result is a navigable http(s) URL. Empty, pure-fragment and non-http(s)
// schemes (javascript:, mailto:, tel:, data:) are rejected.
func resolveHref(base *url.URL, raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "#") {
		return "", false
	}
	ref, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	var abs *url.URL
	if base != nil {
		abs = base.ResolveReference(ref)
	} else {
		abs = ref
	}
	if abs.Scheme != "http" && abs.Scheme != "https" {
		return "", false
	}
	return abs.String(), true
}

// ceilPx rounds a positive pixel extent up so a sub-pixel advance still yields a
// hit-testable width of at least one pixel.
func ceilPx(v float64) int {
	i := int(v)
	if float64(i) < v {
		i++
	}
	if i < 1 {
		i = 1
	}
	return i
}
