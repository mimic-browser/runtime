// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package layout

import (
	"math"

	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/dom"
)

// gridItem is one placed participant of a grid container.
type gridItem struct {
	node    *dom.Node
	st      *css.Style
	hEdges  float64
	vEdges  float64
	hMargin float64
	vMargin float64
	r0, r1  int // half-open row track span [r0,r1)
	c0, c1  int // half-open column track span [c0,c1)
	box     *Box
}

// grid lays out a CSS grid container. It resolves grid-template-columns/rows
// (px/%/fr/auto/repeat/minmax), places items (explicit line numbers + span,
// named areas, and row-major auto-flow), applies column/row gaps and
// justify-items/align-items within each cell. Returns the content bottom y.
func (l *layouter) grid(box *Box, node *dom.Node, st *css.Style, cx, cw, top float64, b *bfc) float64 {
	items := l.gridItems(node)
	if len(items) == 0 {
		return top
	}

	// Column and row track counts: the template count, extended to cover named
	// areas and any explicitly-placed item that reaches past the template.
	nCols := len(st.GridTemplateColumns)
	nRowsExplicit := len(st.GridTemplateRows)
	if len(st.GridTemplateAreas) > 0 {
		if c := len(st.GridTemplateAreas[0]); c > nCols {
			nCols = c
		}
		if r := len(st.GridTemplateAreas); r > nRowsExplicit {
			nRowsExplicit = r
		}
	}
	if nCols < 1 {
		nCols = gridImpliedColumns(items)
	}

	l.placeItems(items, st, nCols, nRowsExplicit)

	nRows := nRowsExplicit
	for _, it := range items {
		if it.r1 > nRows {
			nRows = it.r1
		}
		if it.c1 > nCols {
			nCols = it.c1
		}
	}
	// nRows is >= 1 here: items is non-empty and every item spans >= 1 row.

	colGap := gapPxCSS(st.ColumnGap, cw)
	colW := l.sizeColumns(items, st, nCols, cw, colGap)
	colX := trackOffsets(cx, colW, colGap)

	// Lay each item out at its column span width to obtain its natural height.
	for _, it := range items {
		spanW := spanSize(colW, colGap, it.c0, it.c1)
		it.box = l.layoutIsolated(it.node, it.st, math.Max(spanW-it.hEdges-it.hMargin, 0))
	}

	rowGap := gapPxCSS(st.RowGap, 0)
	containerH, hDef := flexCrossHeight(st, cw)
	rowH := l.sizeRows(items, st, nRows, containerH, hDef, rowGap)
	rowY := trackOffsets(top, rowH, rowGap)

	// Justify the whole column band when it is narrower than the container.
	var usedW float64
	for _, w := range colW {
		usedW += w
	}
	usedW += colGap * float64(nCols-1)
	jOff, jGap := distribute(st.JustifyContent, math.Max(cw-usedW, 0), nCols)

	for _, it := range items {
		cellX := colX[it.c0] + jOff + jGap*float64(it.c0)
		cellY := rowY[it.r0]
		cellW := spanSize(colW, colGap, it.c0, it.c1)
		cellH := spanSize(rowH, rowGap, it.r0, it.r1)
		l.placeGridItem(box, it, st, cellX, cellY, cellW, cellH)
	}

	total := top
	for _, h := range rowH {
		total += h
	}
	total += rowGap * float64(nRows-1)
	return total
}

// gridItems collects the element children acting as grid items.
func (l *layouter) gridItems(node *dom.Node) []*gridItem {
	var out []*gridItem
	for _, c := range l.renderedChildren(node) {
		if c.Type != dom.Element {
			continue
		}
		cs := l.sm[c]
		if cs == nil || cs.Display == css.DisplayNone {
			continue
		}
		if cs.Position.OutOfFlow() {
			// Not a grid item; placed later against its containing block.
			l.outOfFlow = append(l.outOfFlow, outOfFlowItem{node: c})
			continue
		}
		bw := cs.Border.Widths()
		out = append(out, &gridItem{
			node:    c,
			st:      cs,
			hEdges:  bw.Left + bw.Right + cs.Padding.Left + cs.Padding.Right,
			vEdges:  bw.Top + bw.Bottom + cs.Padding.Top + cs.Padding.Bottom,
			hMargin: cs.Margin.Left + cs.Margin.Right,
			vMargin: cs.Margin.Top + cs.Margin.Bottom,
		})
	}
	return out
}

// gridImpliedColumns returns the number of columns when no template/areas are
// given: at least one, extended to cover any explicit column line or span.
func gridImpliedColumns(items []*gridItem) int {
	n := 1
	for _, it := range items {
		if l := it.st.GridColumnStart; !l.Auto && !l.Span && l.N > n {
			n = l.N
		}
		if l := it.st.GridColumnEnd; !l.Auto && !l.Span && l.N-1 > n {
			n = l.N - 1
		}
	}
	return n
}

// placeItems assigns each item its row/column track span: named areas first,
// then explicit line/span placement, then row-major auto-flow for the rest.
// nRowsExplicit is the grid-template-rows track count (0 if there is none),
// which lets a negative row line (e.g. `grid-row: 1 / -1`, Tailwind's
// `row-span-full`) resolve against the explicit grid instead of being left
// unresolved — an unresolved negative end previously collapsed such a span
// to zero rows, so the item reserved no occupancy at all and a later
// auto-placed item could slide into the cells it was meant to cover.
func (l *layouter) placeItems(items []*gridItem, st *css.Style, nCols, nRowsExplicit int) {
	occ := newOccupancy()
	rowTracks := -1
	if nRowsExplicit > 0 {
		rowTracks = nRowsExplicit
	}

	// Pass 1: items with a definite column AND row (named area or explicit lines).
	var autoItems []*gridItem
	for _, it := range items {
		c0, cSpan, cDef := resolveAxisPlacement(it.st.GridColumnStart, it.st.GridColumnEnd, nCols)
		rStart, rSpan, rDef := resolveAxisPlacement(it.st.GridRowStart, it.st.GridRowEnd, rowTracks)
		if area, ok := lookupArea(st, it.st.GridArea); ok {
			it.r0, it.r1, it.c0, it.c1 = area.r0, area.r1, area.c0, area.c1
			occ.fill(it.r0, it.r1, it.c0, it.c1)
			continue
		}
		it.c0, it.c1 = c0, c0+cSpan
		if cDef && rDef {
			it.r0, it.r1 = rStart, rStart+rSpan
			occ.fill(it.r0, it.r1, it.c0, it.c1)
			continue
		}
		autoItems = append(autoItems, it)
	}

	// Pass 2: auto-flow (row-major). Items keep an explicit column if they have
	// one; otherwise the cursor scans columns then rows for the next free slot.
	cursorR, cursorC := 0, 0
	for _, it := range autoItems {
		_, cSpan, cDef := resolveAxisPlacement(it.st.GridColumnStart, it.st.GridColumnEnd, nCols)
		rStart, rSpan, rDef := resolveAxisPlacement(it.st.GridRowStart, it.st.GridRowEnd, rowTracks)
		if cSpan > nCols {
			cSpan = nCols
		}
		if cDef {
			// Fixed column, auto row: find the first free row from the top.
			r := 0
			for occ.occupied(r, r+rSpan, it.c0, it.c1) {
				r++
			}
			it.r0, it.r1 = r, r+rSpan
			occ.fill(it.r0, it.r1, it.c0, it.c1)
			continue
		}
		if rDef {
			// Fixed row, auto column: find the first free column in that row.
			c := 0
			for c+cSpan <= nCols && occ.occupied(rStart, rStart+rSpan, c, c+cSpan) {
				c++
			}
			if c+cSpan > nCols {
				c = 0
			}
			it.r0, it.r1, it.c0, it.c1 = rStart, rStart+rSpan, c, c+cSpan
			occ.fill(it.r0, it.r1, it.c0, it.c1)
			continue
		}
		r, c := cursorR, cursorC
		if st.GridAutoFlowDense {
			// "dense" packing restarts the search from the very first cell for
			// EVERY item, backfilling any gap an earlier, bigger item left open
			// — as opposed to "sparse" (the default, above), which only ever
			// advances forward from wherever the previous item landed. This can
			// place a later item visually ahead of an earlier one in DOM order,
			// which is the whole point of the keyword.
			r, c = 0, 0
		}
		for {
			if c+cSpan > nCols {
				r++
				c = 0
				continue
			}
			if !occ.occupied(r, r+rSpan, c, c+cSpan) {
				break
			}
			c++
		}
		it.r0, it.r1, it.c0, it.c1 = r, r+rSpan, c, c+cSpan
		occ.fill(it.r0, it.r1, it.c0, it.c1)
		cursorR, cursorC = r, c+cSpan
	}
}

// areaRect is the bounding track rectangle of a named grid area.
type areaRect struct{ r0, r1, c0, c1 int }

// lookupArea returns the bounding rectangle of a named grid area, if defined.
func lookupArea(st *css.Style, name string) (areaRect, bool) {
	if name == "" || len(st.GridTemplateAreas) == 0 {
		return areaRect{}, false
	}
	r0, c0 := math.MaxInt32, math.MaxInt32
	r1, c1 := -1, -1
	for r, row := range st.GridTemplateAreas {
		for c, n := range row {
			if n != name {
				continue
			}
			if r < r0 {
				r0 = r
			}
			if c < c0 {
				c0 = c
			}
			if r+1 > r1 {
				r1 = r + 1
			}
			if c+1 > c1 {
				c1 = c + 1
			}
		}
	}
	if r1 < 0 {
		return areaRect{}, false
	}
	return areaRect{r0: r0, r1: r1, c0: c0, c1: c1}, true
}

// resolveAxisPlacement resolves a pair of grid lines into a 0-based start track
// and span, reporting whether the start is definite. nTracks is the track count
// on that axis (-1 when unknown, e.g. rows, so negative lines are not resolved).
func resolveAxisPlacement(start, end css.GridLine, nTracks int) (start0, span int, definite bool) {
	norm := func(n int) int { // 1-based line -> 0-based start track
		if n < 0 && nTracks >= 0 {
			return nTracks + 1 + n // -1 == last line
		}
		return n - 1
	}
	switch {
	case !start.Auto && !start.Span && !end.Auto && !end.Span:
		s, e := norm(start.N), norm(end.N)
		if e < s {
			s, e = e, s
		}
		if e == s {
			e = s + 1
		}
		return clampNonNeg(s), e - clampNonNeg(s), true
	case !start.Auto && !start.Span && end.Span:
		s := clampNonNeg(norm(start.N))
		return s, maxInt(end.N, 1), true
	case start.Span && !end.Auto && !end.Span:
		e := norm(end.N)
		sp := maxInt(start.N, 1)
		s := e - sp
		return clampNonNeg(s), sp, true
	case !start.Auto && !start.Span:
		return clampNonNeg(norm(start.N)), 1, true
	case start.Span:
		return 0, maxInt(start.N, 1), false
	case end.Span:
		return 0, maxInt(end.N, 1), false
	case !end.Auto:
		return 0, 1, false
	default:
		return 0, 1, false
	}
}

func clampNonNeg(n int) int {
	if n < 0 {
		return 0
	}
	return n
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// occupancy tracks which grid cells are taken during placement.
type occupancy struct{ set map[[2]int]bool }

func newOccupancy() *occupancy { return &occupancy{set: map[[2]int]bool{}} }

func (o *occupancy) fill(r0, r1, c0, c1 int) {
	for r := r0; r < r1; r++ {
		for c := c0; c < c1; c++ {
			o.set[[2]int{r, c}] = true
		}
	}
}

func (o *occupancy) occupied(r0, r1, c0, c1 int) bool {
	for r := r0; r < r1; r++ {
		for c := c0; c < c1; c++ {
			if o.set[[2]int{r, c}] {
				return true
			}
		}
	}
	return false
}

// trackSizeAt returns the sizing function of track i on an axis whose explicit
// template is tmpl, falling back to auto (the implicit-track sizing) past it.
func trackSizeAt(tmpl []css.TrackSize, autoTrack css.TrackSize, i int) css.TrackSize {
	if i < len(tmpl) {
		return tmpl[i]
	}
	if autoTrack.Kind != css.TrackAuto || autoTrack.Px != 0 {
		return autoTrack
	}
	return css.TrackSize{Kind: css.TrackAuto}
}

// sizeColumns resolves the pixel width of every column track.
func (l *layouter) sizeColumns(items []*gridItem, st *css.Style, nCols int, cw, colGap float64) []float64 {
	base := make([]float64, nCols)
	fr := make([]float64, nCols)
	flexible := make([]bool, nCols)
	maxCap := make([]float64, nCols)
	autoTrack := make([]bool, nCols)
	for i := 0; i < nCols; i++ {
		ts := trackSizeAt(st.GridTemplateColumns, st.GridAutoColumns, i)
		autoTrack[i] = ts.Kind == css.TrackAuto
		base[i], fr[i], flexible[i], maxCap[i] = l.baseTrack(ts, cw, func() float64 {
			return l.columnContent(items, i)
		})
	}
	distributeFr(base, fr, flexible, maxCap, cw, colGap, nCols)
	// With no flexible tracks and content-distribution stretch (the default),
	// auto tracks grow equally to absorb the leftover width (so a lone auto
	// column fills its container, matching browser grid behaviour).
	if st.JustifyContent == css.JustifyStart {
		stretchAutoTracks(base, autoTrack, cw, colGap, nCols)
	}
	// Auto tracks are sized to their max-content in baseTrack, which for a column
	// of long wrappable text is the whole unwrapped run — far wider than the grid.
	// A browser lets such a track SHRINK to the available space so its content
	// wraps; without a matching step the track (and every item in it) overflows
	// the grid to the right and paints off-screen. Shrink the auto tracks back to
	// fit the grid's definite width when their combined base overflows it.
	shrinkAutoTracks(base, autoTrack, cw, colGap, nCols)
	return base
}

// shrinkAutoTracks reduces auto tracks proportionally when the total track size
// overflows the axis, so an auto column of long text wraps to the grid width
// instead of overflowing to the right. Only auto (intrinsically sized) tracks
// shrink — fixed px/percent and resolved fr tracks keep their size — and only
// when the combined base actually exceeds the axis (the common non-overflow case
// is untouched). If the fixed tracks alone already exceed the axis there is
// nothing shrinkable and the grid overflows, matching a browser.
func shrinkAutoTracks(base []float64, autoTrack []bool, axisSize, gap float64, n int) {
	var total, autoSum float64
	for i := 0; i < n; i++ {
		total += base[i]
		if autoTrack[i] {
			autoSum += base[i]
		}
	}
	total += gap * float64(n-1)
	excess := total - axisSize
	if excess <= 0 || autoSum <= 0 {
		return
	}
	// Remove the overflow from the auto tracks, proportionally to their size,
	// flooring at 0 (a track narrower than a word then wraps/overflows word by
	// word, exactly as the inline layout already handles an over-wide word).
	scale := (autoSum - excess) / autoSum
	if scale < 0 {
		scale = 0
	}
	for i := 0; i < n; i++ {
		if autoTrack[i] {
			base[i] *= scale
		}
	}
}

// stretchAutoTracks grows auto tracks equally to fill any positive leftover
// space when no flexible track already consumed it.
func stretchAutoTracks(base []float64, autoTrack []bool, axisSize, gap float64, n int) {
	var sum float64
	nAuto := 0
	for i := 0; i < n; i++ {
		sum += base[i]
		if autoTrack[i] {
			nAuto++
		}
	}
	free := axisSize - sum - gap*float64(n-1)
	if free <= 0 || nAuto == 0 {
		return
	}
	add := free / float64(nAuto)
	for i := 0; i < n; i++ {
		if autoTrack[i] {
			base[i] += add
		}
	}
}

// baseTrack resolves a single track's base size and flex factor. content() is
// called lazily to compute the max-content size for auto/min-content tracks.
func (l *layouter) baseTrack(ts css.TrackSize, axisSize float64, content func() float64) (base, fr float64, flexible bool, maxCap float64) {
	switch ts.Kind {
	case css.TrackPx:
		return ts.Px, 0, false, 0
	case css.TrackPercent:
		return ts.Percent * axisSize, 0, false, 0
	case css.TrackFr:
		return 0, ts.Fr, true, math.Inf(1)
	case css.TrackMinMax:
		mn, _, _, _ := l.baseTrack(*ts.Min, axisSize, content)
		if ts.Max.Kind == css.TrackFr {
			return mn, ts.Max.Fr, true, math.Inf(1)
		}
		mx, _, _, _ := l.baseTrack(*ts.Max, axisSize, content)
		return mn, 0, false, mx
	default: // TrackAuto / min-content / max-content
		return content(), 0, false, 0
	}
}

// columnContent returns the max-content width contribution of the items that lie
// solely within column i (multi-column spanners do not contribute, keeping the
// intrinsic sizing simple and predictable).
func (l *layouter) columnContent(items []*gridItem, i int) float64 {
	var w float64
	for _, it := range items {
		if it.c0 == i && it.c1 == i+1 {
			pw := l.preferredWidth(it.node, it.st) + it.hMargin
			if pw > w {
				w = pw
			}
		}
	}
	return w
}

// distributeFr grows tracks to fill the leftover axis space, mirroring the
// CSS Grid track-sizing algorithm's two growth steps in order: "Maximize
// Tracks" first grows any non-flexible track with room below its max cap
// (e.g. minmax(0,960px) — a fixed track never has a cap above its own base,
// so it is untouched), splitting the free space among such tracks; then
// "Expand Flexible Tracks" hands whatever is left to the fr tracks,
// proportionally to their flex factor.
func distributeFr(base, fr []float64, flexible []bool, maxCap []float64, axisSize, gap float64, n int) {
	var sumBase, sumFr float64
	for i := 0; i < n; i++ {
		sumBase += base[i]
		sumFr += fr[i]
	}
	// Clamp non-flexible minmax tracks to their max cap up front.
	for i := 0; i < n; i++ {
		if !flexible[i] && maxCap[i] > 0 && base[i] > maxCap[i] {
			sumBase -= base[i] - maxCap[i]
			base[i] = maxCap[i]
		}
	}
	free := axisSize - sumBase - gap*float64(n-1)
	if free <= 0 {
		return
	}
	free = growCappedTracks(base, flexible, maxCap, free, n)
	if free > 0 && sumFr > 0 {
		for i := 0; i < n; i++ {
			if flexible[i] {
				base[i] += free * fr[i] / sumFr
			}
		}
	}
}

// growCappedTracks implements "Maximize Tracks": free space is split equally
// among every non-flexible track that still has room below its max cap (a
// plain fixed or auto track's cap is 0, i.e. no room, so it never grows
// here). A track that saturates its cap frees its unused share for another
// pass over the rest, so tracks with different caps still end up correct.
// A non-flexible track's cap is always finite — an infinite cap only arises
// from an fr max, which is marked flexible and so never reaches this
// function. Returns the free space left over for the flexible (fr) tracks.
func growCappedTracks(base []float64, flexible []bool, maxCap []float64, free float64, n int) float64 {
	for free > 0 {
		var room []int
		for i := 0; i < n; i++ {
			if !flexible[i] && maxCap[i] > base[i] {
				room = append(room, i)
			}
		}
		if len(room) == 0 {
			break
		}
		// share is positive (free > 0) and every room entry has avail > 0 (by
		// the filter above), so add is always positive: this loop always makes
		// progress and free strictly decreases, guaranteeing termination.
		share := free / float64(len(room))
		for _, i := range room {
			add := share
			if avail := maxCap[i] - base[i]; add > avail {
				add = avail
			}
			base[i] += add
			free -= add
		}
	}
	return free
}

// sizeRows resolves the pixel height of every row track from the template and
// the natural heights of the laid-out items.
func (l *layouter) sizeRows(items []*gridItem, st *css.Style, nRows int, containerH float64, hDef bool, rowGap float64) []float64 {
	base := make([]float64, nRows)
	explicit := make([]bool, nRows)
	fr := make([]float64, nRows)
	flexible := make([]bool, nRows)
	maxCap := make([]float64, nRows)
	axisH := 0.0
	if hDef {
		axisH = containerH
	}
	for i := 0; i < nRows; i++ {
		ts := trackSizeAt(st.GridTemplateRows, st.GridAutoRows, i)
		switch ts.Kind {
		case css.TrackPx:
			base[i], explicit[i] = ts.Px, true
		case css.TrackPercent:
			base[i], explicit[i] = ts.Percent*axisH, true
		case css.TrackFr:
			fr[i], flexible[i], maxCap[i] = ts.Fr, true, math.Inf(1)
		case css.TrackMinMax:
			mn, _, _, _ := l.baseTrack(*ts.Min, axisH, func() float64 { return 0 })
			base[i] = mn
			if ts.Max.Kind == css.TrackFr {
				fr[i], flexible[i], maxCap[i] = ts.Max.Fr, true, math.Inf(1)
			}
		}
	}
	// Content sizing for auto/flex rows: single-row items set their row's height.
	for _, it := range items {
		if it.r1-it.r0 != 1 {
			continue
		}
		if explicit[it.r0] {
			continue
		}
		h := it.box.H + it.vMargin
		if h > base[it.r0] {
			base[it.r0] = h
		}
	}
	// Row-spanning items: grow the last spanned (non-explicit) row if too short.
	for _, it := range items {
		if it.r1-it.r0 <= 1 {
			continue
		}
		have := spanSize(base, rowGap, it.r0, it.r1)
		need := it.box.H + it.vMargin
		if need > have && !explicit[it.r1-1] {
			base[it.r1-1] += need - have
		}
	}
	if hDef {
		distributeFr(base, fr, flexible, maxCap, containerH, rowGap, nRows)
	}
	return base
}

// trackOffsets returns the start coordinate of every track given a start origin,
// per-track sizes and the inter-track gap.
func trackOffsets(origin float64, sizes []float64, gap float64) []float64 {
	out := make([]float64, len(sizes))
	acc := origin
	for i, s := range sizes {
		out[i] = acc
		acc += s + gap
	}
	return out
}

// spanSize returns the total size of tracks [a,b) plus the interior gaps. The
// caller guarantees 0 <= a <= b <= len(sizes) (placement clamps every span into
// the resolved track range before sizing).
func spanSize(sizes []float64, gap float64, a, b int) float64 {
	var s float64
	for i := a; i < b; i++ {
		s += sizes[i]
	}
	if b-a > 1 {
		s += gap * float64(b-a-1)
	}
	return s
}

// placeGridItem positions one item within its cell rectangle, honouring
// justify-items/justify-self (inline axis) and align-items/align-self (block
// axis); stretch fills the cell, otherwise the item keeps its natural size.
func (l *layouter) placeGridItem(box *Box, it *gridItem, st *css.Style, cellX, cellY, cellW, cellH float64) {
	justify := it.st.JustifySelf.Resolve(st.JustifyItems)
	align := it.st.AlignSelf.Resolve(st.AlignItems)

	// Inline axis: width. Stretch fills the cell content width; otherwise the
	// item takes its specified width (or content max-content), positioned in it.
	stretchW := justify == css.AlignStretch && it.st.Width.Auto
	if !stretchW {
		natural := l.itemNaturalWidth(it, cellW)
		it.box = l.layoutIsolated(it.node, it.st, natural)
	}
	xOff := crossOffset(justify, cellW, it.box.W+it.hMargin)

	// Block axis: height.
	if align == css.AlignStretch && it.st.Height.Auto {
		it.box.H = math.Max(cellH-it.vMargin, 0)
		it.box.ContentH = math.Max(it.box.H-it.vEdges, 0)
	}
	yOff := crossOffset(align, cellH, it.box.H+it.vMargin)

	translateBox(it.box,
		(cellX+xOff+it.st.Margin.Left)-it.box.X,
		(cellY+yOff+it.st.Margin.Top)-it.box.Y)
	box.Children = append(box.Children, it.box)
}

// itemNaturalWidth returns the content width a non-stretched grid item uses: its
// specified width if set (box-sizing aware), else its max-content width, both
// clamped to the cell content width.
func (l *layouter) itemNaturalWidth(it *gridItem, cellW float64) float64 {
	avail := math.Max(cellW-it.hMargin-it.hEdges, 0)
	var w float64
	if it.st.Width.Auto {
		w = l.preferredWidth(it.node, it.st) - it.hEdges
	} else {
		w = it.st.Width.Resolve(cellW)
		if it.st.BoxSizing == css.BorderBox {
			w -= it.hEdges
		}
	}
	w = clampWidth(w, it.st, cellW, it.hEdges)
	return math.Min(math.Max(w, 0), avail)
}

// gapPxCSS resolves a grid gap length against the axis size (px, or percentage
// of the axis). An unset gap is a zero Length and resolves to 0.
func gapPxCSS(l css.Length, axis float64) float64 {
	return math.Max(l.Resolve(axis), 0)
}
