// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package layout

import (
	"math"
	"sort"

	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/dom"
)

// cbRect is a containing block rectangle (the padding box of a positioned
// ancestor, or the initial containing block) in absolute document coordinates.
type cbRect struct{ x, y, w, h float64 }

// posBox pairs a placed out-of-flow box with its stacking key.
type posBox struct {
	box   *Box
	z     int
	order int
}

// positioned runs the position pass over an already-laid-out in-flow tree:
//  1. relative/sticky boxes are shifted by their top/left/right/bottom offset;
//  2. every collected out-of-flow (absolute/fixed) box is laid out and placed
//     against its containing block, then appended to root (so it paints after
//     in-flow content — an approximate but stable stacking order), sorted by
//     z-index.
//
// The initial containing block used for an ABSOLUTE box with no positioned
// ancestor is the viewport: width viewportW, and — lacking a separate
// viewport height in this entry point — height equal to the in-flow document
// height (a documented approximation, unchanged; absolute content scrolls
// with the page, so the document is the relevant canvas it can occupy). A
// FIXED box instead uses fixedH (viewportH if positive, else that same
// document-height approximation) — see LayoutDocumentViewport's own doc
// comment for why fixed specifically needs the real viewport, not the
// document, to match a real browser's full-page-screenshot behaviour. It
// returns the page height, grown to cover any absolutely-positioned content.
func (l *layouter) positioned(root *Box, viewportW, viewportH, total float64) float64 {
	nb := map[*dom.Node]*Box{}
	indexBoxes(root, nb)

	// Relative/sticky offsets are visual-only; apply them to the in-flow tree
	// first so a positioned ancestor's padding box (a containing block) reflects
	// the shift before any absolute descendant is placed against it.
	applyRelative(root, viewportW, total)

	icb := cbRect{x: 0, y: 0, w: viewportW, h: total}
	fixedH := viewportH
	if fixedH <= 0 {
		fixedH = total
	}
	fixedICB := cbRect{x: 0, y: 0, w: viewportW, h: fixedH}
	var placed []*posBox
	// A growing queue: laying out a positioned subtree may itself collect nested
	// out-of-flow boxes, which are appended and processed in turn.
	for i := 0; i < len(l.outOfFlow); i++ {
		item := l.outOfFlow[i]
		n := item.node
		// Every collection site appends only nodes with a non-nil out-of-flow
		// style, so l.sm[n] is guaranteed present here.
		st := l.sm[n]
		cb := l.resolveContainingBlock(n, st, nb, icb, fixedICB)
		box := l.placeAbsolute(n, st, cb, item)
		nb[n] = box // register so a nested absolute descendant can find it
		placed = append(placed, &posBox{box: box, z: zIndexValue(st), order: i})
	}

	// Stable stacking: lower z-index first, ties broken by collection order.
	sort.SliceStable(placed, func(i, j int) bool {
		if placed[i].z != placed[j].z {
			return placed[i].z < placed[j].z
		}
		return placed[i].order < placed[j].order
	})
	for _, p := range placed {
		root.Children = append(root.Children, p.box)
		// Absolutely-positioned content is part of the scrollable page; fixed
		// content is anchored to the viewport and never grows the page.
		if p.box.Position == css.PositionAbsolute {
			if bottom := p.box.Y + p.box.H; bottom > total {
				total = bottom
			}
		}
	}
	return total
}

// indexBoxes records every element box (those carrying a DOM node) by its node,
// so an absolute box can locate its positioned ancestor's box.
func indexBoxes(box *Box, nb map[*dom.Node]*Box) {
	if box.Node != nil {
		nb[box.Node] = box
	}
	for _, ch := range box.Children {
		indexBoxes(ch, nb)
	}
}

// applyRelative walks the in-flow tree and shifts each relative/sticky box (and
// its subtree) by its resolved offset, THEN applies any `transform: translate`
// shift (see applyTransformTranslate) — every box, regardless of position,
// since transform is independent of positioning. cbW/cbH are the containing
// block (the parent content box) dimensions used to resolve percentage
// offsets. Children are visited with this box's content box as their
// containing block.
func applyRelative(box *Box, cbW, cbH float64) {
	for _, ch := range box.Children {
		applyRelative(ch, box.ContentW, box.ContentH)
	}
	// A relative/sticky box always carries a non-nil Style (place set both), so
	// relativeOffset can read its offsets safely.
	if p := box.Position; p == css.PositionRelative || p == css.PositionSticky {
		dx, dy := relativeOffset(box.Style, cbW, cbH)
		translateBox(box, dx, dy)
	}
	applyTransformTranslate(box)
}

// applyTransformTranslate shifts box by its own `transform: translate(...)`
// offset (see css.Style.TranslateX/Y, parseTransformTranslate) — a percentage
// resolves against the box's OWN border-box size, per the CSS Transforms spec
// (the default reference box), unlike a position offset's containing-block
// percentage. A box built via the `&Box{}` empty-document fallback in
// layout.go carries no Style; skip it rather than dereference nil.
func applyTransformTranslate(box *Box) {
	if box.Style == nil {
		return
	}
	tx := box.Style.TranslateX.Resolve(box.W)
	ty := box.Style.TranslateY.Resolve(box.H)
	translateBox(box, tx, ty)
}

// relativeOffset resolves the paint shift of a relatively positioned box. The
// left/right pair resolves against the containing block width, top/bottom
// against its height; left wins over right and top over bottom (per CSS for
// left-to-right, over-constrained cases).
func relativeOffset(st *css.Style, cbW, cbH float64) (dx, dy float64) {
	switch {
	case !st.Left.Auto:
		dx = st.Left.Resolve(cbW)
	case !st.Right.Auto:
		dx = -st.Right.Resolve(cbW)
	}
	switch {
	case !st.Top.Auto:
		dy = st.Top.Resolve(cbH)
	case !st.Bottom.Auto:
		dy = -st.Bottom.Resolve(cbH)
	}
	return dx, dy
}

// resolveContainingBlock returns the containing block rectangle for an
// out-of-flow box: for fixed, the viewport (fixedICB); for absolute, the
// padding box of the nearest positioned ancestor, else the initial containing
// block (icb).
func (l *layouter) resolveContainingBlock(n *dom.Node, st *css.Style, nb map[*dom.Node]*Box, icb, fixedICB cbRect) cbRect {
	if st.Position == css.PositionFixed {
		return fixedICB
	}
	for p := elementParentOf(n); p != nil; p = elementParentOf(p) {
		ps := l.sm[p]
		if ps == nil || !ps.Position.Positioned() {
			continue
		}
		if pb, ok := nb[p]; ok {
			return paddingBox(pb)
		}
	}
	return icb
}

// paddingBox returns a box's padding box (its border box inset by border
// widths) in absolute coordinates.
func paddingBox(box *Box) cbRect {
	bw := box.Style.Border.Widths()
	return cbRect{
		x: box.X + bw.Left,
		y: box.Y + bw.Top,
		w: math.Max(box.W-bw.Left-bw.Right, 0),
		h: math.Max(box.H-bw.Top-bw.Bottom, 0),
	}
}

// placeAbsolute lays out an absolutely/fixed-positioned box against containing
// block cb and returns it positioned in absolute document coordinates. Auto
// margins are treated as zero at this fidelity.
func (l *layouter) placeAbsolute(n *dom.Node, st *css.Style, cb cbRect, item outOfFlowItem) *Box {
	bw := st.Border.Widths()
	hExtra := bw.Left + bw.Right + st.Padding.Left + st.Padding.Right
	hMargin := st.Margin.Left + st.Margin.Right

	contentW := l.absoluteContentWidth(n, st, cb, hExtra, hMargin)

	box := l.layoutIsolated(n, st, contentW)
	// Resolve any relative descendants inside this out-of-flow subtree (its own
	// content box is their containing block) before translating it into place.
	applyRelative(box, contentW, box.ContentH)

	outerW := box.W + hMargin
	outerH := box.H + st.Margin.Top + st.Margin.Bottom

	// With both insets on an axis auto, CSS uses the box's static position; we
	// approximate it by the flow cursor captured at collection (else the
	// containing block origin).
	staticX, staticY := cb.x, cb.y
	if item.hasStatic {
		staticX, staticY = item.staticX, item.staticY
	}

	x := staticX + st.Margin.Left
	switch {
	case !st.Left.Auto:
		x = cb.x + st.Left.Resolve(cb.w) + st.Margin.Left
	case !st.Right.Auto:
		x = cb.x + cb.w - st.Right.Resolve(cb.w) - outerW + st.Margin.Left
	}
	y := staticY + st.Margin.Top
	switch {
	case !st.Top.Auto:
		y = cb.y + st.Top.Resolve(cb.h) + st.Margin.Top
	case !st.Bottom.Auto:
		y = cb.y + cb.h - st.Bottom.Resolve(cb.h) - outerH + st.Margin.Top
	}

	translateBox(box, x-box.X, y-box.Y)
	// The box's OWN transform must be applied AFTER this final placement, not
	// via the applyRelative call above: that call runs before x/y are known,
	// so any shift it applied there would be exactly cancelled by the
	// x-box.X/y-box.Y delta above (transform is meant to compose ON TOP of
	// the box's normal position, not be absorbed into computing it).
	applyTransformTranslate(box)
	return box
}

// absoluteContentWidth resolves the used content width of an absolutely
// positioned box: an explicit width wins; else a left+right pair fixes it to the
// containing block; else it shrinks to fit its content within the space left by
// any single offset.
func (l *layouter) absoluteContentWidth(n *dom.Node, st *css.Style, cb cbRect, hExtra, hMargin float64) float64 {
	var contentW float64
	switch {
	case !st.Width.Auto:
		contentW = st.Width.Resolve(cb.w)
		if st.BoxSizing == css.BorderBox {
			contentW -= hExtra
		}
	case !st.Left.Auto && !st.Right.Auto:
		contentW = cb.w - st.Left.Resolve(cb.w) - st.Right.Resolve(cb.w) - hMargin - hExtra
	default:
		avail := cb.w - hMargin - hExtra
		if !st.Left.Auto {
			avail -= st.Left.Resolve(cb.w)
		}
		if !st.Right.Auto {
			avail -= st.Right.Resolve(cb.w)
		}
		contentW = math.Min(l.preferredWidth(n, st)-hExtra, avail)
	}
	contentW = clampWidth(contentW, st, cb.w, hExtra)
	if contentW < 0 {
		contentW = 0
	}
	return contentW
}

// zIndexValue returns a style's effective z-index for stacking (auto == 0).
func zIndexValue(st *css.Style) int {
	if st.ZIndexAuto {
		return 0
	}
	return st.ZIndex
}

// elementParentOf returns the nearest element ancestor of a node.
func elementParentOf(n *dom.Node) *dom.Node {
	p := n.Parent
	for p != nil && p.Type != dom.Element {
		p = p.Parent
	}
	return p
}
