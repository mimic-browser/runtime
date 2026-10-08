// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package layout

import (
	"math"

	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/dom"
)

// newInlineDecor reports whether a plain inline-level element generates a box
// of its own and, if so, describes it. An element generates one when it has a
// background to paint or an edge (border or padding) to reserve and paint; a
// <b>, <em>, <a> or UA-default <code> carrying nothing but typographic style
// does not, returns false, and costs the line nothing.
//
// Border widths come from Borders.Widths(), so a `border-style: none` edge
// contributes no space even when a width is set — the same rule block layout
// already applies.
func newInlineDecor(el *dom.Node, st *css.Style) (inlineDecor, bool) {
	bw := st.Border.Widths()
	d := inlineDecor{
		node:   el,
		style:  st,
		lead:   st.Padding.Left + bw.Left,
		trail:  st.Padding.Right + bw.Right,
		top:    st.Padding.Top + bw.Top,
		bottom: st.Padding.Bottom + bw.Bottom,
	}
	if st.Background.A == 0 && d.lead == 0 && d.trail == 0 && d.top == 0 && d.bottom == 0 {
		return inlineDecor{}, false
	}
	return d, true
}

// push returns the decor chain for content inside el, which is l.decor plus el
// itself when el generates a box. The result is a fresh slice whenever it
// differs, never an append into the caller's backing array, because every item
// collected under this chain keeps a REFERENCE to it: sharing one growing array
// across sibling elements would rewrite already-collected items' ancestors.
func (l *layouter) pushDecor(el *dom.Node, st *css.Style) []inlineDecor {
	d, ok := newInlineDecor(el, st)
	if !ok {
		return l.decor
	}
	chain := make([]inlineDecor, len(l.decor), len(l.decor)+1)
	copy(chain, l.decor)
	return append(chain, d)
}

// sharedDecorDepth returns the length of the common ancestor prefix of two
// items' decor chains — the depth at which the second item's inline ancestry
// diverges from the first's. Entries are compared by element identity, so two
// sibling <span>s with identical styles still diverge at depth 0.
func sharedDecorDepth(prev, it *InlineItem) int {
	if prev == nil {
		return 0
	}
	n := len(prev.decor)
	if len(it.decor) < n {
		n = len(it.decor)
	}
	for i := 0; i < n; i++ {
		if prev.decor[i].node != it.decor[i].node {
			return i
		}
	}
	return n
}

// sumInlineEdges totals the leading and trailing horizontal edges of a decor
// chain slice.
func sumInlineEdges(ds []inlineDecor) (lead, trail float64) {
	for _, d := range ds {
		lead += d.lead
		trail += d.trail
	}
	return lead, trail
}

// resolveInlineEdges fills in decorFirst/decorLast and padLead/padTrail across
// one inline formatting context's collected items, in document order. An
// element's leading edge is reserved on its first item and its trailing edge on
// its last — box-decoration-break: slice, the CSS default — which is exactly
// where its ancestor chain diverges from the neighbouring item's.
//
// Sentinels that are never positioned (a <br> LineBreak, a promoted BlockBreak)
// are skipped rather than treated as content: an inline element's box simply
// spans them, so a <span> whose text is split by a <br> reserves its left edge
// once, before the first word, not again after the break.
func resolveInlineEdges(items []*InlineItem) {
	var prev *InlineItem
	for _, it := range items {
		if it.LineBreak || it.BlockBreak != nil {
			continue
		}
		d := sharedDecorDepth(prev, it)
		it.decorFirst = d
		it.padLead, _ = sumInlineEdges(it.decor[d:])
		// Provisional: with nothing after it yet, every ancestor ends here.
		it.decorLast = 0
		_, it.padTrail = sumInlineEdges(it.decor)
		if prev != nil {
			prev.decorLast = d
			_, prev.padTrail = sumInlineEdges(prev.decor[d:])
		}
		prev = it
	}
}

// openFrag is an inline element's fragment being built as a line is positioned:
// its decor, the x where the fragment's border box starts, whether it is the
// element's FIRST fragment, and the vertical span of the items seen so far.
type openFrag struct {
	decor  inlineDecor
	x0     float64
	first  bool
	y0, y1 float64
}

// grow extends the fragment's vertical span to cover one more item's font box.
func (f *openFrag) grow(it *InlineItem) {
	f.y0 = math.Min(f.y0, it.Y)
	f.y1 = math.Max(f.y1, it.Y+it.LineHeight)
}

// fragment closes the fragment at x, producing the border box: the vertical
// padding and border grow it beyond the items' font box (and so, per CSS,
// possibly beyond the line box — vertical inline padding overflows rather than
// growing the line).
func (f *openFrag) fragment(x float64, last bool) InlineFragment {
	return InlineFragment{
		Node:  f.decor.node,
		Style: f.decor.style,
		X:     f.x0,
		W:     x - f.x0,
		Y:     f.y0 - f.decor.top,
		H:     f.y1 - f.y0 + f.decor.top + f.decor.bottom,
		First: f.first,
		Last:  last,
	}
}

// placeLine positions one line's items left to right from x, at the line top
// and common baseline, and fills line.Inlines with the box fragments of the
// inline elements that generate one.
//
// It is the single positioning loop for both the wrapped and the pre
// (white-space:pre) paths. Each item's X is the pen position after any
// collapsible space and after the leading border+padding of every ancestor
// that STARTS at it; each ancestor's fragment closes as soon as the next item
// leaves it, taking its trailing border+padding only when the element genuinely
// ends there rather than merely continuing on the next line.
func placeLine(line *LineBox, x, top, baseline float64) {
	var open []openFrag
	// Fragments bucketed by nesting depth so the finished list is
	// outermost-first: closing pops innermost-first, which is the wrong order
	// for a painter (an enclosing background must go down before a nested one).
	var byDepth [][]InlineFragment
	closeTo := func(depth int, prev *InlineItem) {
		for len(open) > depth {
			f := open[len(open)-1]
			open = open[:len(open)-1]
			last := len(open) >= prev.decorLast
			if last {
				x += f.decor.trail
			}
			for len(byDepth) <= len(open) {
				byDepth = append(byDepth, nil)
			}
			byDepth[len(open)] = append(byDepth[len(open)], f.fragment(x, last))
		}
	}
	for i, it := range line.Items {
		it.Y = top + baseline - it.Ascent + it.BaselineShift
		if i > 0 {
			// Within a line the items are consecutive in document order, so the
			// previous item's decor chain IS the open stack and this item's
			// decorFirst is exactly where the two diverge.
			closeTo(it.decorFirst, line.Items[i-1])
			x += it.SpaceBefore
		}
		for len(open) < len(it.decor) {
			d := it.decor[len(open)]
			f := openFrag{decor: d, x0: x, first: len(open) >= it.decorFirst, y0: it.Y, y1: it.Y + it.LineHeight}
			if f.first {
				x += d.lead
			}
			open = append(open, f)
		}
		it.X = x
		for k := range open {
			open[k].grow(it)
		}
		if it.NestedBox != nil {
			translateBox(it.NestedBox, it.X-it.NestedBox.X, it.Y-it.NestedBox.Y)
		}
		x += it.Width
	}
	if n := len(line.Items); n > 0 {
		closeTo(0, line.Items[n-1])
	}
	for _, fs := range byDepth {
		line.Inlines = append(line.Inlines, fs...)
	}
}
