// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package layout

import (
	"math"

	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/dom"
)

// layoutMultiCol lays a `column-count`/`column-width`/`columns` block
// container's in-flow ELEMENT children out into N columns side by side — see
// css.Style.ColumnCount/ColumnWidth's own doc comment for the exact,
// deliberately narrow subset of the real spec this models (no column-span,
// no column-rule painting, no true balancing, no fragmenting a single
// child's own internal content across columns) and the one confirmed
// real-world trigger (pkg.go.dev's own `.UnitFiles-fileList`).
//
// Each child is laid out exactly ONCE, at the real column width, inside a
// throwaway single-column bfc — this gives every child its correct final
// internal layout (wrapping, nested boxes, everything) and, as a side
// effect, its real height. A single greedy pass then buckets children into
// columns by cumulative height (target = total÷count) and RE-POSITIONS each
// child's already-correct box subtree with translateBox — cheaper than a
// second full layout pass, since only X/Y need to change once the content
// itself was already measured at the right width the first time.
func (l *layouter) layoutMultiCol(box *Box, node *dom.Node, st *css.Style, cx, cw, top float64, b *bfc) float64 {
	count, colW, gap := resolveColumns(st, cw)
	if count <= 1 {
		return l.blockOrInlineContents(box, node, st, cx, cw, top, b)
	}

	var children []*dom.Node
	var styles []*css.Style
	for _, c := range l.renderedChildren(node) {
		// Only element children participate — a multicol container's own
		// bare text (an anonymous block per spec) has no confirmed trigger
		// mixing it into a columns container, unlike flex's own equivalent
		// case (see flex's own doc comment); left as ordinary single-column
		// content would require falling back entirely, so it is simply
		// skipped here rather than guessed at.
		if c.Type != dom.Element {
			continue
		}
		cs := l.sm[c]
		if cs == nil || cs.Display == css.DisplayNone {
			continue
		}
		if cs.Position.OutOfFlow() {
			l.outOfFlow = append(l.outOfFlow, outOfFlowItem{node: c})
			continue
		}
		children = append(children, c)
		styles = append(styles, cs)
	}
	if len(children) == 0 {
		return top
	}

	local := &bfc{}
	boxes := make([]*Box, len(children))
	for i, c := range children {
		boxes[i] = l.place(c, styles[i], 0, colW, local)
	}
	total := local.y + math.Max(local.carry, 0)
	target := total / float64(count)

	colIdx := 0
	colStartY := 0.0 // this child's Y in the ORIGINAL single-column stack, at which the current column began
	colX := cx
	maxBottom := top
	for _, cb := range boxes {
		heightSoFarInColumn := cb.Y - colStartY
		if colIdx < count-1 && heightSoFarInColumn > 0 && heightSoFarInColumn+cb.H > target {
			colIdx++
			colStartY = cb.Y
			colX = cx + float64(colIdx)*(colW+gap)
		}
		dx := colX - cb.X
		dy := top - colStartY
		translateBox(cb, dx, dy)
		box.Children = append(box.Children, cb)
		if bottom := cb.Y + cb.H; bottom > maxBottom {
			maxBottom = bottom
		}
	}
	return maxBottom
}

// resolveColumns determines the actual column count, per-column width and
// gap for a `column-count`/`column-width`/`columns` container of available
// width cw, per CSS Multi-column Layout Level 1 §2's own algorithm — with
// the count and width always resolved TOGETHER at the end (columns stretch
// to evenly fill cw, matching every real browser, rather than leaving
// leftover space unused), not separately as the spec's own prose describes
// the two individual longhands in isolation.
func resolveColumns(st *css.Style, cw float64) (count int, colW, gap float64) {
	gap = st.FontSize // column-gap:normal's own initial value, unlike flex/grid's — see ColumnGap's own doc comment
	if !st.ColumnGap.Auto {
		gap = math.Max(st.ColumnGap.Resolve(cw), 0)
	}
	switch {
	case st.ColumnCount > 0 && !st.ColumnWidth.Auto:
		// `columns: <width> <count>`: as many columns as column-count asks
		// for, UNLESS the container is too narrow to fit even the minimum
		// width for that many — then fewer, per the spec's own worked
		// example ("if the container is less than 103ems wide ... there
		// will be fewer than 12 columns").
		minW := math.Max(st.ColumnWidth.Resolve(cw), 1)
		fit := int(math.Floor((cw + gap) / (minW + gap)))
		count = st.ColumnCount
		if fit < count {
			count = fit
		}
	case st.ColumnCount > 0:
		count = st.ColumnCount
	case !st.ColumnWidth.Auto:
		minW := math.Max(st.ColumnWidth.Resolve(cw), 1)
		count = int(math.Floor((cw + gap) / (minW + gap)))
	}
	if count < 1 {
		count = 1
	}
	colW = (cw - float64(count-1)*gap) / float64(count)
	if colW < 0 {
		colW = 0
	}
	return count, colW, gap
}
