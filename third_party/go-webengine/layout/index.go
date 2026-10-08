// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package layout

import "github.com/go-webengine/engine/dom"

// Rect is a used-geometry rectangle in document (CSS pixel) coordinates: X,Y is
// the top-left of the border box, W,H its size.
type Rect struct{ X, Y, W, H float64 }

// union returns the smallest rect covering both a and b. A zero-size rect with
// no position (the map's zero value) is treated as "unset" so the first real
// fragment seeds the union rather than anchoring it at the origin.
func (a Rect) union(b Rect) Rect {
	if a == (Rect{}) {
		return b
	}
	if b == (Rect{}) {
		return a
	}
	left := min2(a.X, b.X)
	top := min2(a.Y, b.Y)
	right := max2(a.X+a.W, b.X+b.W)
	bottom := max2(a.Y+a.H, b.Y+b.H)
	return Rect{X: left, Y: top, W: right - left, H: bottom - top}
}

func min2(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func max2(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// BuildIndex walks a laid-out box tree and returns, for every element node, its
// used border-box rectangle in document coordinates. Block-level elements take
// their box rect directly (authoritative); inline-level elements — which produce
// InlineItems inside line boxes rather than their own Box — take the union of
// their fragments' rects (and of any inline descendants). This is the geometry
// the JS DOM binding reads back through getBoundingClientRect / offset* /
// getComputedStyle, so scripts that decide layout from measured metrics
// (responsive nav, MediaWiki's mw.loader sidebar collapse) see real numbers.
func BuildIndex(root *Box) map[*dom.Node]Rect {
	idx := map[*dom.Node]Rect{}
	if root == nil {
		return idx
	}
	// block records the nodes that own a real block box; an inline fragment never
	// overwrites (or is unioned into) such a node's authoritative rect.
	block := map[*dom.Node]bool{}

	var visit func(b *Box)
	visit = func(b *Box) {
		if b.Node != nil && b.Node.Type == dom.Element {
			idx[b.Node] = Rect{X: b.X, Y: b.Y, W: b.W, H: b.H}
			block[b.Node] = true
		}
		for _, ln := range b.Lines {
			for _, it := range ln.Items {
				r := itemRect(it)
				// Attribute the fragment to its originating element and every inline
				// ancestor up to (but not into) the nearest element that owns a block
				// box — that block box already carries its own authoritative rect.
				for e := it.Node; e != nil && e.Type == dom.Element; e = elementParent(e) {
					if block[e] {
						break
					}
					idx[e] = idx[e].union(r)
				}
			}
		}
		for _, c := range b.Children {
			visit(c)
		}
	}
	visit(root)
	return idx
}

// itemRect is the border-box rect of one inline item (a word, image or break).
func itemRect(it *InlineItem) Rect {
	h := it.LineHeight
	if it.Image != nil && it.ImgH > h {
		h = it.ImgH
	}
	return Rect{X: it.X, Y: it.Y, W: it.Width, H: h}
}

// elementParent returns the nearest element ancestor of n (skipping text /
// document nodes), or nil.
//
// A node at the top of an attached shadow tree has Parent == nil (see
// attachDeclarativeShadowRoots: it is the root of a separate tree, not a
// light-DOM descendant) — this bridges to its ShadowHost instead of stopping
// there, so BuildIndex's own attribution walk (the only caller) can continue
// up the HOST's real light-DOM parent chain. Without this, a shadow host that
// itself generates no Box (a plain display:inline custom element — the spec
// default for one with no author styling, e.g. an unrecognised
// `<mdn-sidebar-filter>`) never received ANY rect entry at all: its own inline
// fragments live only in the shadow tree, whose parent chain dead-ends at nil
// before ever reaching back to the host. getComputedStyle/getBoundingClientRect
// then reported the host as if it were display:none, even though its shadow
// content renders and paints normally — confirmed via a minimal repro
// (a bare custom element wrapping a declarative shadow root with no CSS at
// all) before any block/inline-block styling ever entered the picture, and
// live on developer.mozilla.org's own `<mdn-sidebar-filter>` search widget.
func elementParent(n *dom.Node) *dom.Node {
	if n.Parent == nil && n.ShadowHost != nil {
		return n.ShadowHost
	}
	p := n.Parent
	for p != nil && p.Type != dom.Element {
		p = p.Parent
	}
	return p
}
