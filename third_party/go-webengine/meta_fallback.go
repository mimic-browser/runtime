// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

// This file adds a last-resort OpenGraph/meta fallback: when a page renders to
// nothing — an un-hydratable JavaScript SPA whose content lives only in a React
// initial-state blob (Mastodon, X, many app-shell sites) — the engine
// synthesises a clean, readable card from the page's og:/meta tags and renders
// that instead of a blank frame. It is gated by Engine.MetaFallback and only
// ever replaces a genuinely empty render, so pages that render real content are
// completely unaffected.
package engine

import (
	"context"
	"html"
	"strings"

	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/dom"
	"github.com/go-webengine/engine/layout"
	"github.com/go-webengine/engine/paint"
)

// Empty-render thresholds. A page counts as "empty" only when BOTH its in-flow
// content height is negligible AND it painted almost no text. A real content
// page has a height of hundreds-to-thousands of px and hundreds-to-thousands of
// characters, so both bounds sit far below anything that actually rendered — the
// fallback never fires on a page that laid out real content. (An SPA whose only
// output is a stray absolutely-positioned avatar contributes no in-flow height,
// which is why the height check is the primary signal rather than an image
// count.)
const (
	emptyRenderHeight = 64 // px of in-flow content
	emptyRenderChars  = 32 // visible characters
)

// renderedEmpty reports whether a laid-out tree shows essentially nothing: an
// in-flow content height below emptyRenderHeight and fewer than emptyRenderChars
// characters of visible text.
func renderedEmpty(box *layout.Box, height float64) bool {
	if height >= emptyRenderHeight {
		return false
	}
	chars := 0
	var walk func(b *layout.Box)
	walk = func(b *layout.Box) {
		if b == nil {
			return
		}
		for _, ln := range b.Lines {
			for _, it := range ln.Items {
				chars += len([]rune(strings.TrimSpace(it.Text)))
			}
		}
		for _, c := range b.Children {
			walk(c)
		}
	}
	walk(box)
	return chars < emptyRenderChars
}

// metaContent returns the content of the first <meta> whose property or name
// equals key (case-insensitive), trimmed, or "".
func metaContent(root *dom.Node, key string) string {
	var out string
	var walk func(n *dom.Node) bool
	walk = func(n *dom.Node) bool {
		if n.Type == dom.Element && n.Tag == "meta" {
			p, _ := n.Attribute("property")
			nm, _ := n.Attribute("name")
			if strings.EqualFold(p, key) || strings.EqualFold(nm, key) {
				if c, ok := n.Attribute("content"); ok {
					out = strings.TrimSpace(c)
					return true
				}
			}
		}
		for _, c := range n.Children {
			if walk(c) {
				return true
			}
		}
		return false
	}
	walk(root)
	return out
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

// buildMetaFallback synthesises a readable card from doc's OpenGraph/meta tags
// and lays it out into a fresh render pass, or returns nil when there is no
// usable title/description to show (leaving the blank render in place).
func (e *Engine) buildMetaFallback(ctx context.Context, doc *Document, vpW int, fonts *paint.Fonts) *renderPass {
	title := firstNonEmpty(metaContent(doc.Root, "og:title"), dom.Title(doc.Root))
	desc := firstNonEmpty(metaContent(doc.Root, "og:description"), metaContent(doc.Root, "description"),
		metaContent(doc.Root, "twitter:description"))
	if title == "" && desc == "" {
		return nil // nothing worth showing
	}
	img := firstNonEmpty(metaContent(doc.Root, "og:image"), metaContent(doc.Root, "twitter:image"))
	site := metaContent(doc.Root, "og:site_name")
	linkURL := firstNonEmpty(metaContent(doc.Root, "og:url"), doc.URL)

	src := metaCardHTML(site, title, desc, img, linkURL)
	root, err := dom.Parse(src)
	if err != nil {
		return nil
	}
	sd := &Document{URL: doc.URL, Root: root}

	rp := &renderPass{}
	rp.sm = css.CascadeVW(root, float64(vpW), nil)
	rp.imgSize, rp.imgs = e.loadImages(ctx, sd, rp.sm, vpW)
	rp.bgImgs = e.loadBackgroundImages(ctx, sd, rp.sm)
	rp.box, rp.height = layout.LayoutDocument(root, rp.sm, float64(vpW), fonts, rp.imgSize)
	return rp
}

// metaCardHTML builds the card markup. Text is HTML-escaped (meta content is
// untrusted and may contain <, &, quotes); the layout is a simple, theme-neutral
// centred column that wraps to the viewport. The title links to the source URL
// so a consumer's hit-map can open the real page.
func metaCardHTML(site, title, desc, img, linkURL string) string {
	var b strings.Builder
	b.WriteString(`<html><body style="margin:0;background:#ffffff;color:#111111;font-family:sans-serif">`)
	b.WriteString(`<div style="max-width:680px;margin:0 auto;padding:28px 24px">`)
	if img != "" {
		b.WriteString(`<img src="` + html.EscapeString(img) + `" style="width:100%;height:auto;margin-bottom:20px">`)
	}
	if site != "" {
		b.WriteString(`<div style="color:#666666;font-size:14px;margin-bottom:8px">` + html.EscapeString(site) + `</div>`)
	}
	if title != "" {
		b.WriteString(`<h1 style="font-size:26px;line-height:1.3;margin:0 0 14px 0;font-weight:bold">`)
		b.WriteString(`<a href="` + html.EscapeString(linkURL) + `" style="color:#111111">` + html.EscapeString(title) + `</a>`)
		b.WriteString(`</h1>`)
	}
	if desc != "" {
		b.WriteString(`<p style="font-size:18px;line-height:1.6;margin:0;color:#333333">` + html.EscapeString(desc) + `</p>`)
	}
	b.WriteString(`</div></body></html>`)
	return b.String()
}
