// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

// This file reports the navigation structure of a laid-out document: where
// every <a href> is clickable, line by line; where every id'd element sits;
// and the heading outline. These are properties of the layout, not of any
// output format — a PDF exporter turns them into link annotations, named
// destinations and bookmarks; an outline panel lists the headings; an EPUB
// export writes them as its navigation document — so they live here beside
// the box tree instead of being re-derived, slightly differently, by each
// consumer. linkmap.go's LinksFromBox answers a narrower question (one
// bounding box per anchor, in image pixels, to hit-test a click) and is left
// exactly as it is; the two share anchorFor and itemBox.
package engine

import (
	"net/url"
	"strings"

	"github.com/go-webengine/engine/dom"
	"github.com/go-webengine/engine/layout"
)

// LinkRun is one clickable rectangle in document CSS px: the atoms of one
// <a href> anchor on one line. A link wrapped over three lines is three runs,
// so the clickable area follows the text rather than covering the block
// between the lines the way a single bounding box would — a PDF link
// annotation or a hover highlight drawn from a run lands on the words and
// nowhere else. Exactly one of URI and Fragment is set: URI for an external
// http(s) target, Fragment for an in-document jump to an element's id.
type LinkRun struct {
	X, Y, W, H float64
	Node       *dom.Node // the <a> element the run belongs to
	URI        string    // absolute http(s) target; empty for an in-document link
	Fragment   string    // in-document target: the id of the element it points to
}

// linkTarget is where an <a href> resolves to, or nothing worth linking.
type linkTarget struct {
	uri, fragment string
	ok            bool
}

// resolveAnchor turns a raw href into a target. A fragment ("#intro"), or a
// URL that is the document's own with a fragment, becomes an in-document
// jump when an element with that id exists; an http(s) URL becomes a URI
// link, resolved against base; anything else (javascript:, mailto:, tel:,
// data:, a fragment nobody anchors) is dropped — a link that goes nowhere is
// worse than plain text. It is deliberately not resolveHref, which serves
// the click hit-map and has no notion of an in-document destination.
func resolveAnchor(base *url.URL, raw string, ids map[string]struct{}) linkTarget {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return linkTarget{}
	}
	if strings.HasPrefix(raw, "#") {
		if _, ok := ids[raw[1:]]; ok {
			return linkTarget{fragment: raw[1:], ok: true}
		}
		return linkTarget{}
	}
	ref, err := url.Parse(raw)
	if err != nil {
		return linkTarget{}
	}
	abs := ref
	if base != nil {
		abs = base.ResolveReference(ref)
	}
	if abs.Fragment != "" && base != nil {
		self := *abs
		self.Fragment = ""
		here := *base
		here.Fragment = ""
		if self.String() == here.String() {
			if _, ok := ids[abs.Fragment]; ok {
				return linkTarget{fragment: abs.Fragment, ok: true}
			}
			return linkTarget{}
		}
	}
	if abs.Scheme != "http" && abs.Scheme != "https" {
		return linkTarget{}
	}
	return linkTarget{uri: abs.String(), ok: true}
}

// itemBox returns an inline atom's painted rectangle in document CSS px: a
// word is its advance by its line height, an image its bitmap; a <br> or an
// empty atom is nothing. itemRect is this rounded out to image pixels.
func itemBox(it *layout.InlineItem) (x, y, w, h float64, ok bool) {
	if it.LineBreak {
		return 0, 0, 0, 0, false
	}
	w, h = it.Width, it.LineHeight
	if it.Image != nil {
		h = it.ImgH
	}
	if w <= 0 || h <= 0 {
		return 0, 0, 0, 0, false
	}
	return it.X, it.Y, w, h, true
}

// LinkRuns returns every anchor's clickable runs in document order, one per
// line the anchor's atoms appear on, in document CSS px (the box tree's own
// coordinate space). base is the document's URL: relative hrefs resolve
// against it, and it decides whether an absolute URL with a fragment is the
// document itself. ids is the set of element ids a fragment may jump to —
// the keys of DocumentIDs — so a fragment nobody anchors yields no run, as
// does a javascript:, mailto:, tel: or data: href.
//
// An anchor that lays out no atom at all — a link whose only content is an
// empty styled block, Hacker News's vote arrows being the canonical case (30
// of its 228 links) — gets the rectangle of the outermost box laid out under
// it instead, once, as a browser's print does. A consumer that paginates is
// left to clip a run straddling a page break to the page it starts on.
//
// LinksFromBox is the other reading of the same tree: one rectangle per
// anchor in image pixels, for finding the link under a click. A run is for
// drawing the link — a PDF annotation, an EPUB export's hit area — not for
// finding it.
func LinkRuns(root *layout.Box, base *url.URL, ids map[string]struct{}) []LinkRun {
	var out []LinkRun
	covered := map[*dom.Node]bool{} // anchors that produced at least one run
	targets := map[*dom.Node]linkTarget{}
	target := func(a *dom.Node) linkTarget {
		if t, ok := targets[a]; ok {
			return t
		}
		raw, _ := a.Attribute("href")
		t := resolveAnchor(base, raw, ids)
		targets[a] = t
		return t
	}
	var walk func(b *layout.Box)
	walk = func(b *layout.Box) {
		if b == nil {
			return
		}
		for _, line := range b.Lines {
			var cur *dom.Node
			var run LinkRun
			flush := func() {
				if cur != nil {
					if t := target(cur); t.ok {
						run.Node, run.URI, run.Fragment = cur, t.uri, t.fragment
						out = append(out, run)
					}
					covered[cur] = true
				}
				cur = nil
			}
			for _, it := range line.Items {
				x, y, w, h, ok := itemBox(it)
				if !ok {
					continue
				}
				a := anchorFor(it.Node)
				if a != cur {
					flush()
					if a != nil {
						cur, run = a, LinkRun{X: x, Y: y, W: w, H: h}
					}
					continue
				}
				if a == nil {
					continue
				}
				// Same anchor, same line: grow the run to cover this atom too.
				x1, y1 := run.X+run.W, run.Y+run.H
				if x < run.X {
					run.X = x
				}
				if y < run.Y {
					run.Y = y
				}
				if x+w > x1 {
					x1 = x + w
				}
				if y+h > y1 {
					y1 = y + h
				}
				run.W, run.H = x1-run.X, y1-run.Y
			}
			flush()
		}
		for _, c := range b.Children {
			walk(c)
		}
	}
	walk(root)

	// Fallback for the atom-less anchors: pre-order, so the outermost box
	// under an anchor is the one taken and its descendants are then skipped.
	var boxes func(b *layout.Box)
	boxes = func(b *layout.Box) {
		if b == nil {
			return
		}
		if b.Node != nil && b.W > 0 && b.H > 0 {
			if a := anchorFor(b.Node); a != nil && !covered[a] {
				covered[a] = true
				if t := target(a); t.ok {
					out = append(out, LinkRun{X: b.X, Y: b.Y, W: b.W, H: b.H, Node: a, URI: t.uri, Fragment: t.fragment})
				}
			}
		}
		for _, c := range b.Children {
			boxes(c)
		}
	}
	boxes(root)
	return out
}

// Anchor is where an id'd element sits, in document CSS px: the top-left a
// viewer scrolls to for an in-document link — a PDF named destination, an
// EPUB fragment target, a scroll-to-hash in a browser shell.
type Anchor struct{ X, Y float64 }

// DocumentIDs maps every element id (and legacy <a name>) that has a
// position to that position: a block element's box top-left, or for an
// inline element — which has no box of its own — the first atom it
// produced. Walked in document order, so the first occurrence of a
// duplicated id wins, as in a browser. An id whose element laid out nothing
// (display:none, an inline with no text) is absent, so LinkRuns drops a
// link to it rather than pointing at nowhere.
func DocumentIDs(root *layout.Box) map[string]Anchor {
	ids := map[string]Anchor{}
	record := func(n *dom.Node, x, y float64) {
		if n == nil || n.Type != dom.Element {
			return
		}
		if id := n.ID(); id != "" {
			if _, seen := ids[id]; !seen {
				ids[id] = Anchor{x, y}
			}
		}
		if n.Tag == "a" {
			if name, ok := n.Attribute("name"); ok && name != "" {
				if _, seen := ids[name]; !seen {
					ids[name] = Anchor{x, y}
				}
			}
		}
	}
	var walk func(b *layout.Box)
	walk = func(b *layout.Box) {
		if b == nil {
			return
		}
		record(b.Node, b.X, b.Y)
		for _, line := range b.Lines {
			for _, it := range line.Items {
				x, y, _, _, ok := itemBox(it)
				if !ok {
					continue
				}
				for n := it.Node; n != nil; n = n.Parent {
					record(n, x, y)
				}
			}
		}
		for _, c := range b.Children {
			walk(c)
		}
	}
	walk(root)
	return ids
}

// Heading is one <h1>…<h6> with its text and top, for the document outline:
// a PDF's bookmark tree, an outline panel, an EPUB's table of contents.
type Heading struct {
	Level int       // 1 for <h1> … 6 for <h6>
	Title string    // the heading's atoms' texts, joined by single spaces
	Y     float64   // top of the heading's box, document CSS px
	Node  *dom.Node // the heading element
}

// Headings returns the headings in document order. A heading's title is the
// text of every atom inside its box, words joined by single spaces — so
// <h2>Part <b>Alpha</b></h2> is "Part Alpha" — and one with no text (an
// icon-only heading) is skipped, since a bookmark with no label helps
// nobody. Only Level is reported; whether an <h3> after an <h1> nests under
// it or stands beside it is the consumer's outline policy, not the layout's.
func Headings(root *layout.Box) []Heading {
	var out []Heading
	var walk func(b *layout.Box)
	walk = func(b *layout.Box) {
		if b == nil {
			return
		}
		if b.Node != nil && b.Node.Type == dom.Element && len(b.Node.Tag) == 2 && b.Node.Tag[0] == 'h' &&
			b.Node.Tag[1] >= '1' && b.Node.Tag[1] <= '6' {
			if title := strings.Join(boxWords(b), " "); title != "" {
				out = append(out, Heading{Level: int(b.Node.Tag[1] - '0'), Title: title, Y: b.Y, Node: b.Node})
			}
			return
		}
		for _, c := range b.Children {
			walk(c)
		}
	}
	walk(root)
	return out
}

// boxWords returns the non-empty atom texts under a box, in order.
func boxWords(b *layout.Box) []string {
	var words []string
	var walk func(b *layout.Box)
	walk = func(b *layout.Box) {
		if b == nil {
			return
		}
		for _, line := range b.Lines {
			for _, it := range line.Items {
				if t := strings.TrimSpace(it.Text); t != "" {
					words = append(words, t)
				}
			}
		}
		for _, c := range b.Children {
			walk(c)
		}
	}
	walk(b)
	return words
}
