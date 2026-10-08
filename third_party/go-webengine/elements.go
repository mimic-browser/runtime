// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

// This file generalizes linkmap.go's anchor-only hit-map to any element, so a
// host can turn a click at pixel (x, y) into a specific button/input/etc, not
// only a link. Kept in its own file for the same reason linkmap.go is: the
// hit-test feature stays isolated from the core render pipeline.
package engine

import (
	"image"

	"github.com/go-webengine/engine/dom"
	"github.com/go-webengine/engine/layout"
)

// ElementsFromBox walks a laid-out box tree and returns every element's used
// border-box rectangle in full-page image pixels — the same coordinate space
// LinksFromBox's Link.Rect uses. It is a thin wrapper over
// layout.BuildIndex(root), exported at this package's level (like
// LinksFromBox wraps the box-walk for anchors) so a host does not need to
// import the layout package just to hit-test.
func ElementsFromBox(root *layout.Box) map[*dom.Node]layout.Rect {
	return layout.BuildIndex(root)
}

// ElementAt returns the most specific element at pt: the smallest-area
// rectangle in index that contains it, which — since a descendant's box is
// always contained within its ancestors' — is the deepest element under the
// point for ordinary (non-overlapping-sibling) layouts. It does not model
// paint/z-order for OVERLAPPING siblings (e.g. one absolutely positioned
// over another): LinkAt's equally simple first-match has been sufficient for
// that case in practice, and a login-form-shaped page (the driving use case)
// does not stack interactive controls. ok is false when pt hits no element.
func ElementAt(index map[*dom.Node]layout.Rect, pt image.Point) (*dom.Node, bool) {
	var best *dom.Node
	var bestArea float64
	for n, r := range index {
		if !rectContains(r, pt) {
			continue
		}
		area := r.W * r.H
		if best == nil || area < bestArea {
			best, bestArea = n, area
		}
	}
	return best, best != nil
}

// rectContains reports whether pt (full-page image pixel coords) falls
// within r (document/CSS-pixel coords) — the two coordinate spaces coincide
// at the engine's default 1 image-pixel per CSS-pixel scale, matching how
// LinkAt compares image.Point against Link.Rect (also document coords).
func rectContains(r layout.Rect, pt image.Point) bool {
	x, y := float64(pt.X), float64(pt.Y)
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}
