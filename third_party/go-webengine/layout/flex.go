// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package layout

import (
	"math"
	"sort"
	"strings"

	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/dom"
)

// flexItem is one participant in a flex container's layout.
type flexItem struct {
	node    *dom.Node
	st      *css.Style
	hEdges  float64 // border+padding left+right
	vEdges  float64 // border+padding top+bottom
	hMargin float64 // margin left+right
	vMargin float64 // margin top+bottom
	base    float64 // hypothetical main content size
	main    float64 // resolved main content size
	// minContent is the item's own min-content width (content-box, matching
	// base/main's own convention) — CSS's "automatic minimum size" for a
	// flex item whose min-width is auto (the default): the shrink phase must
	// never squeeze such an item below the widest unbreakable unit its own
	// content needs, exactly the floor min-width:auto silently disables when
	// an author sets it explicitly. See resolveMainRow's own doc comment for
	// the confirmed real need this closes.
	minContent float64
	box        *Box
	cross      float64 // outer cross size after layout
}

// flexLine is one flex line (a row of items in a row container) with its
// resolved cross size.
type flexLine struct {
	items []*flexItem
	cross float64
}

// flex lays out a flex container. It supports both directions, wrapping
// (flex-wrap), order, gaps, grow/shrink/basis with min/max clamping,
// justify-content on the main axis, align-items/align-self on the cross axis and
// align-content across the flex lines. Returns the content bottom y.
func (l *layouter) flex(box *Box, node *dom.Node, st *css.Style, cx, cw, top float64, b *bfc) float64 {
	items := l.flexItems(node)
	if len(items) == 0 || l.hasDirectText(node) {
		// flexItems only ever collects ELEMENT children, but a flex
		// container's bare text (no wrapping element) forms an anonymous
		// flex item per spec — confirmed live TWICE: react.dev's hero
		// heading (`<h1 style="display:flex">React</h1>`, zero element
		// children) and go.dev/blog's nav dropdown links (`<a
		// style="display:inline-flex">Why Go <i>arrow_drop_down</i></a>`,
		// ONE element child alongside the bare text "Why Go "). The first
		// case was fixed by falling back here when len(items)==0; the
		// second went unnoticed because flexItems (and preferredWidth's
		// flex-row branch) only ever check "are there zero elements", never
		// "is there ALSO meaningful bare text alongside the elements found"
		// — so a MIXED container skipped straight to flexRow/flexColumn
		// below, silently dropping every bare-text sibling entirely (the
		// icon rendered, "Why Go " never did). Falling back to the same
		// inline-formatting-context path contents() already uses for a
		// plain, non-flex element correctly renders both the text and the
		// element side by side for the common single-line case (there is no
		// attempt to model justify-content/align-items/gap once any bare
		// text is present — not confirmed as a live need for a container
		// that also has real flex items).
		b.commit()
		pre := st.WhiteSpace == css.WSPre
		inline := l.collectInline(node, st, pre, cw)
		if len(inline) == 0 {
			return top
		}
		// A mixed container's bare text can itself wrap an element whose OWN
		// display is block-level (e.g. a `<div style="width:...">` sibling
		// alongside the text, not just an inline icon like go.dev/blog's
		// case above) — collectInline promotes that via the ordinary
		// BlockBreak mechanism, which needs box.Children populated, not just
		// box.Lines (see contents()'s own identical hasBlockBreak check).
		// Skipping this and always taking the simple layoutInline path would
		// silently orphan such a block's box, the same "secondary box
		// nothing walks" defect class round 45 fixed for NestedBox.
		if hasBlockBreak(inline) {
			return l.placeInlineSegments(box, inline, st, cx, cw, b, pre)
		}
		lines, bottom := l.layoutInline(inline, st, cx, cw, top, pre)
		box.Lines = lines
		return bottom
	}
	if st.FlexDirection == css.FlexColumn {
		return l.flexColumn(box, items, st, cx, cw, top)
	}
	return l.flexRow(box, items, st, cx, cw, top)
}

// flexItems collects the element children that act as flex items, ordered by the
// CSS order property (stable within equal order, preserving document order).
func (l *layouter) flexItems(node *dom.Node) []*flexItem {
	var out []*flexItem
	for _, c := range l.renderedChildren(node) {
		if c.Type != dom.Element {
			continue
		}
		cs := l.sm[c]
		if cs == nil || cs.Display == css.DisplayNone {
			continue
		}
		if cs.Position.OutOfFlow() {
			// Not a flex item; placed later against its containing block.
			l.outOfFlow = append(l.outOfFlow, outOfFlowItem{node: c})
			continue
		}
		bw := cs.Border.Widths()
		hEdges := bw.Left + bw.Right + cs.Padding.Left + cs.Padding.Right
		// minContentWidth's own "a definite width is the contribution
		// outright" branch is right for ITS existing caller (a table
		// column's width floor, where an author's declared cell width
		// SHOULD act as a hard minimum) but wrong for a flex item's
		// automatic minimum size, which must reflect the item's own CONTENT
		// regardless of any width/flex-basis the author declared — shrinking
		// a definite-width item below its declared width is the entire
		// point of flex-shrink. Measuring with Width forced back to auto
		// (the identical pattern table.go's own column-minimum computation
		// already uses, for the identical reason) makes minContentWidth
		// fall through to the real content-based branches instead.
		content := *cs
		content.Width = css.Length{Auto: true}
		out = append(out, &flexItem{
			node:    c,
			st:      cs,
			hEdges:  hEdges,
			vEdges:  bw.Top + bw.Bottom + cs.Padding.Top + cs.Padding.Bottom,
			hMargin: cs.Margin.Left + cs.Margin.Right,
			vMargin: cs.Margin.Top + cs.Margin.Bottom,
			// minContentWidth already folds hEdges into its own result (see
			// its own doc comment), so subtract it back out here to keep
			// minContent in the same content-box convention as base/main.
			minContent: l.minContentWidth(c, &content) - hEdges,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].st.Order < out[j].st.Order })
	return out
}

// hasDirectText reports whether node has any direct child that is a
// non-blank text node (pure whitespace is not a real anonymous flex item,
// matching how CSS collapses it away). Used by flex() and preferredWidth's
// flex-row branch to detect a flex container mixing bare text with element
// children — neither routine models a text run as its own flex item, so both
// fall back to plain inline-formatting-context handling instead of silently
// dropping the text.
func (l *layouter) hasDirectText(node *dom.Node) bool {
	for _, c := range l.renderedChildren(node) {
		if c.Type == dom.Text && strings.TrimSpace(c.Text) != "" {
			return true
		}
	}
	return false
}

// mainBaseRow returns an item's hypothetical main content size along a row (cw is
// the container content width, used to resolve percentages).
func (it *flexItem) mainBaseRow(l *layouter, cw float64) float64 {
	switch {
	case !it.st.FlexBasis.Auto:
		v := it.st.FlexBasis.Resolve(cw)
		if it.st.BoxSizing == css.BorderBox {
			v -= it.hEdges
		}
		return math.Max(v, 0)
	case !it.st.Width.Auto:
		v := it.st.Width.Resolve(cw)
		if it.st.BoxSizing == css.BorderBox {
			v -= it.hEdges
		}
		return math.Max(v, 0)
	default:
		return math.Max(l.preferredWidth(it.node, it.st)-it.hEdges, 0)
	}
}

// clampMainRow clamps a row item's main (width) content size to its
// min-width/max-width, resolved against the container width cw.
func (it *flexItem) clampMainRow(v, cw float64) float64 {
	return clampWidth(v, it.st, cw, it.hEdges)
}

// clampCrossHeight clamps a row item's cross (height) border-box size to its
// min-height/max-height. outer is the item's outer cross size (border box +
// margin); containerH is the definite container height (or 0). At this fidelity
// min/max-height bound the border box directly (box-sizing is not distinguished
// on the cross axis, a documented simplification).
func (it *flexItem) clampCrossHeight(outer, containerH float64) float64 {
	inner := outer - it.vMargin
	if v, ok := heightBound(it.st.MaxHeight, containerH); ok && inner > v {
		inner = v
	}
	if v, ok := heightBound(it.st.MinHeight, containerH); ok && inner < v {
		inner = v
	}
	return inner + it.vMargin
}

// heightBound resolves a min/max-height length to a border-box height bound.
// Percentages resolve against containerH; auto, unset, an unresolved percentage
// or a negative value report false.
func heightBound(l css.Length, containerH float64) (float64, bool) {
	if l.Auto {
		return 0, false
	}
	if l.IsPercent && containerH <= 0 {
		return 0, false
	}
	v := l.Resolve(containerH)
	if v < 0 {
		return 0, false
	}
	return v, true
}

// breakLines partitions items into flex lines. With wrapping off, all items
// share one line. Otherwise a line is filled greedily until the next item's
// outer main size (plus the inter-item main gap) would overflow mainSize; an
// item that overflows an empty line still occupies it alone.
func breakLines(items []*flexItem, mainSize, mainGap float64, wrap bool) [][]*flexItem {
	if !wrap {
		return [][]*flexItem{items}
	}
	var lines [][]*flexItem
	var cur []*flexItem
	var used float64
	for _, it := range items {
		outer := it.base + it.hEdges + it.hMargin
		add := outer
		if len(cur) > 0 {
			add += mainGap
		}
		if len(cur) > 0 && used+add > mainSize+1e-6 {
			lines = append(lines, cur)
			cur = nil
			used = 0
			add = outer
		}
		cur = append(cur, it)
		used += add
	}
	if len(cur) > 0 {
		lines = append(lines, cur)
	}
	return lines
}

func (l *layouter) flexRow(box *Box, items []*flexItem, st *css.Style, cx, cw, top float64) float64 {
	mainGap := gapLen(st.ColumnGap)
	crossGap := gapLen(st.RowGap)
	containerH, crossDefinite := flexCrossHeight(st, cw)

	for _, it := range items {
		it.base = it.clampMainRow(it.mainBaseRow(l, cw), cw)
	}
	lines := breakLines(items, cw, mainGap, st.FlexWrap != css.FlexNoWrap)

	// Resolve flexible lengths per line, then lay each item out to get its cross.
	var totalCross float64
	fls := make([]*flexLine, 0, len(lines))
	for _, line := range lines {
		resolveMainRow(line, cw, mainGap)
		var lineCross float64
		for _, it := range line {
			it.box = l.layoutIsolated(it.node, it.st, it.main)
			it.cross = it.clampCrossHeight(it.box.H+it.vMargin, containerH)
			if it.cross > lineCross {
				lineCross = it.cross
			}
		}
		fls = append(fls, &flexLine{items: line, cross: lineCross})
		totalCross += lineCross
	}
	totalCross += crossGap * float64(len(fls)-1)

	// Cross container size: a definite height wins, else it fits the lines.
	containerCross := totalCross
	if crossDefinite && containerH > totalCross {
		containerCross = containerH
	}
	lineOff, lineGap := alignContentDistribute(st.AlignContent, containerCross-totalCross, len(fls))
	if st.FlexWrap == css.FlexWrapReverse {
		reverseLines(fls)
	}

	cyLine := top + lineOff
	for _, fl := range fls {
		// Main-axis positioning with justify-content over the leftover space.
		var mainUsed float64
		for _, it := range fl.items {
			mainUsed += it.main + it.hEdges + it.hMargin
		}
		mainUsed += mainGap * float64(len(fl.items)-1)
		leftover := math.Max(cw-mainUsed, 0)
		offset, jgap := distribute(st.JustifyContent, leftover, len(fl.items))

		x := cx + offset
		for _, it := range fl.items {
			ai := it.st.AlignSelf.Resolve(st.AlignItems)
			cyOff := crossOffset(ai, fl.cross, it.cross)
			if ai == css.AlignStretch && it.st.Height.Auto {
				stretched := it.clampCrossHeight(fl.cross, containerH)
				it.box.H = stretched - it.vMargin
				it.box.ContentH = it.box.H - it.vEdges
			}
			translateBox(it.box, (x+it.st.Margin.Left)-it.box.X, (cyLine+cyOff+it.st.Margin.Top)-it.box.Y)
			box.Children = append(box.Children, it.box)
			x += it.main + it.hEdges + it.hMargin + mainGap + jgap
		}
		cyLine += fl.cross + crossGap + lineGap
	}
	return top + math.Max(totalCross, containerCross)
}

// resolveMainRow distributes free main space across a line via grow/shrink,
// clamping each item's resolved main size to its min/max-width.
//
// This is CSS Flexbox §9.7 "Resolving Flexible Lengths": distributing once and
// clamping each item independently is not enough. When an item's proportional
// share would push it past its min/max-width, clamping it there changes how
// much space the OTHER items must absorb — an item frozen at a bound no longer
// participates, so the space it couldn't take (grow) or couldn't give up
// (shrink) has to be redistributed among the remaining, still-flexible items,
// repeated until no further item clamps. A single pass instead leaves that
// leftover amount undistributed: a shrinking line whose first item clamps at
// its floor (0 or min-width) stays that much too WIDE overall, because the
// deficit it couldn't shrink by never gets passed on to its siblings.
// Confirmed live on github.com/golang/go: its marketing header lays out a
// logo (an auto-width flex item with no visible siblings once the mobile-only
// controls are hidden by their own media queries, so its content-based base
// size is small) beside a nav+search+sign-in/up group whose unwrapped content
// is wider than the 1024px viewport. The single-pass version clamped the logo
// item to 0 (it had nowhere to shrink to) and dropped the rest of the
// requested shrink instead of applying it to the nav group, leaving the group
// at ~1421px in a 1024px container — overflowing off the right edge and
// carrying "Sign in"/"Sign up" past the visible viewport with it.
func resolveMainRow(line []*flexItem, cw, mainGap float64) {
	for _, it := range line {
		it.main = it.base
	}
	n := len(line)
	frozen := make([]bool, n)
	sumOuter := func() float64 {
		var s float64
		for _, it := range line {
			s += it.main + it.hEdges + it.hMargin
		}
		return s
	}
	grow := cw-sumOuter()-mainGap*float64(n-1) > 0

	// At most n rounds: each round either freezes at least one more item or
	// stops, so the loop can run at most n times before every item is frozen.
	for range line {
		var sumFactor float64
		for i, it := range line {
			if frozen[i] {
				continue
			}
			if grow {
				sumFactor += it.st.FlexGrow
			} else {
				sumFactor += it.st.FlexShrink
			}
		}
		free := cw - sumOuter() - mainGap*float64(n-1)
		if sumFactor <= 0 || (grow && free <= 0) || (!grow && free >= 0) {
			break
		}
		frozeAny := false
		for i, it := range line {
			if frozen[i] {
				continue
			}
			factor := it.st.FlexShrink
			if grow {
				factor = it.st.FlexGrow
			}
			if factor <= 0 {
				continue // never participates; keeps its current main size
			}
			wanted := it.main + free*factor/sumFactor
			floor := math.Max(wanted, 0)
			// CSS's "automatic minimum size": an item whose min-width is
			// auto (the default — an author who wants the OLD, floor-at-0
			// behaviour sets min-width:0 explicitly) must never shrink below
			// its own min-content width, provided its overflow is visible in
			// this axis (spec's own carve-out: overflow:hidden/scroll/auto
			// opts back into floor-at-0, since clipped content has nothing
			// left to protect). Confirmed live on github.com/golang/go's own
			// marketing header (the SAME nav this function's own doc comment
			// above already documents once): six top-level nav items whose
			// combined content is wider than the 1024px viewport shrink
			// correctly down to each one's own longest-word floor — WITHOUT
			// this, the shrink deficit lands unevenly across rounds and can
			// squeeze the shortest, last-resolved item ("Pricing", a single
			// unbreakable word) straight through zero, vanishing entirely
			// rather than stopping at its own real minimum.
			if it.st.MinWidth.Auto && it.st.OverflowX == css.OverflowVisible && it.minContent > floor {
				floor = it.minContent
			}
			clamped := it.clampMainRow(floor, cw)
			it.main = clamped
			if clamped != wanted {
				frozen[i] = true
				frozeAny = true
			}
		}
		if !frozeAny {
			break
		}
	}
}

func gapLen(l css.Length) float64 {
	if l.Auto || l.IsPercent {
		return 0 // percentage gaps against an auto container size resolve to 0
	}
	return math.Max(l.Px, 0)
}

// flexCrossHeight returns the flex container's definite cross height (row) and
// whether it is definite. cw is used only for box-sizing. Passes 0 for
// usedHeight's own contentW (aspect-ratio) parameter: a flex container sized
// by its own aspect-ratio has no confirmed real caller, unlike the plain
// block-box case usedHeight's other call site (layout.go's place) resolves —
// see css.Style.AspectRatio's own doc comment.
func flexCrossHeight(st *css.Style, cw float64) (float64, bool) {
	if h, ok := usedHeight(st, st.Border.Widths(), cw, 0); ok {
		return h, true
	}
	return 0, false
}

// alignContentDistribute returns the leading cross offset and extra inter-line
// gap for an align-content value over free cross space (added on top of the base
// row-gap already applied between lines).
func alignContentDistribute(a css.AlignContent, free float64, n int) (offset, extraGap float64) {
	if n <= 0 || free <= 0 {
		return 0, 0
	}
	switch a {
	case css.AlignContentEnd:
		return free, 0
	case css.AlignContentCenter:
		return free / 2, 0
	case css.AlignContentSpaceBetween:
		if n > 1 {
			return 0, free / float64(n-1)
		}
		return 0, 0
	case css.AlignContentSpaceAround:
		g := free / float64(n)
		return g / 2, g
	case css.AlignContentSpaceEvenly:
		g := free / float64(n+1)
		return g, g
	default: // start and stretch pack at the cross-start edge
		return 0, 0
	}
}

func reverseLines(fls []*flexLine) {
	for i, j := 0, len(fls)-1; i < j; i, j = i+1, j-1 {
		fls[i], fls[j] = fls[j], fls[i]
	}
}

func (l *layouter) flexColumn(box *Box, items []*flexItem, st *css.Style, cx, cw, top float64) float64 {
	rowGap := gapLen(st.RowGap) // main-axis (vertical) gap in a column container
	// Cross axis is horizontal. Determine each item's cross (width) and lay it
	// out to get its main (height); stack along the vertical main axis.
	y := top
	for i, it := range items {
		if i > 0 {
			y += rowGap
		}
		// layoutIsolated takes a CONTENT width, so the item's own padding and
		// border come off the line it is handed: what stretch makes fill the
		// line is the item's MARGIN box, not its content box. Leaving them on
		// made every padded item overflow its container by exactly its own
		// edges — a 600 px column gave a 20 px-padded, 5 px-bordered item a
		// 650 px border box, where an ordinary block parent gives the same
		// element 600 (#228). The non-stretch branch just below was already
		// careful about it, which is why only stretched items were wrong.
		avail := cw - it.hMargin - it.hEdges
		crossW := avail
		ai := it.st.AlignSelf.Resolve(st.AlignItems)
		stretch := ai == css.AlignStretch && it.st.Width.Auto
		if !stretch {
			natural := l.preferredWidth(it.node, it.st) - it.hEdges
			if !it.st.Width.Auto {
				natural = it.st.Width.Resolve(cw)
				if it.st.BoxSizing == css.BorderBox {
					natural -= it.hEdges
				}
			}
			natural = it.clampMainRow(natural, cw)
			crossW = math.Min(math.Max(natural, 0), avail)
		}
		it.box = l.layoutIsolated(it.node, it.st, crossW)
		outerCrossW := it.box.W + it.hMargin
		cxOff := crossOffset(ai, cw, outerCrossW)
		translateBox(it.box, (cx+cxOff+it.st.Margin.Left)-it.box.X, (y+it.st.Margin.Top)-it.box.Y)
		box.Children = append(box.Children, it.box)
		y += it.box.H + it.vMargin
	}
	return y
}

// layoutIsolated lays out a node as a fixed-content-width block in its own
// (discarded) float context, returning a box positioned at a local origin with
// zero outer margins. Used by flex/grid/table to size and then translate
// children.
//
// Forcing Width to the full contentW is correct for an ordinary block (an
// auto width block DOES fill its container), but wrong for a replaced element
// (img/svg): CSS 2.1 §10.3.2's own rules give width:auto on a replaced
// element a completely different meaning (the intrinsic size, or a size
// derived from an explicit height via the intrinsic ratio) that has nothing
// to do with the available container width. Forcing it here overwrote that
// meaning before resolvedReplacedSize (contents()'s own isReplacedTag
// branch) ever got a chance to see the real "auto" — so a bare replaced
// element used as a flex/grid/table-cell item (no wrapping block) always
// stretched to fill its item box, distorting its aspect ratio, regardless of
// this round's own resolvedReplacedSize fix. Found via issue #226 (a real A0
// poster export where a height-only-styled image, used directly as a grid
// item, overflowed its 200px track at the source's full 320px width instead
// of the 80px CSS 2.1 §10.3.2 calls for). Resolving the used size HERE with
// the ORIGINAL (unmodified) style, then feeding it back in as an explicit
// px width, keeps every other case (plain blocks, and a replaced element
// with its own explicit width) byte-identical: resolvedReplacedSize runs
// twice for the second case (once here, once again inside contents()) but
// idempotently, since it's a pure function of style + intrinsic size + cw.
func (l *layouter) layoutIsolated(node *dom.Node, st *css.Style, contentW float64) *Box {
	if contentW < 0 {
		contentW = 0
	}
	saved := l.floats
	l.floats = &floatCtx{}
	clone := *st
	clone.Width = css.Length{Px: contentW}
	// Only when the ORIGINAL style's width is genuinely auto: some callers
	// (grid's own itemNaturalWidth, for a non-stretched item) already resolve
	// an explicit width/percentage against the real containing width THEMSELVES
	// before calling here, passing the ALREADY-RESOLVED target width as
	// contentW — for those, contentW is not a containing width to resolve
	// against a second time, and the original force-to-contentW behaviour
	// below is exactly right. Only a still-auto width reaches here needing
	// resolvedReplacedSize's own derivation, and contentW in that case is
	// genuinely the raw available space (e.g. grid's own spanW passed straight
	// through for a stretched item, never pre-resolved for one).
	if node.Type == dom.Element && isReplacedTag(node.Tag) && st.Width.Auto {
		if iw, ih := l.imageSize(node); iw > 0 && ih > 0 {
			if dw, _ := resolvedReplacedSize(st, iw, ih, contentW); dw > 0 {
				clone.Width = css.Length{Px: dw}
			}
		}
	}
	clone.MinWidth = css.Length{Auto: true}
	clone.MaxWidth = css.Length{Auto: true}
	clone.BoxSizing = css.ContentBox
	clone.Float = css.FloatNone
	clone.Margin = css.Edges{}
	clone.MarginLeftAuto, clone.MarginRightAuto = false, false
	fb := &bfc{}
	box := l.place(node, &clone, 0, contentW, fb)
	fb.commit()
	l.floats = saved
	return box
}

// layoutNestedInlineFlex lays out a display:inline-flex element as a
// self-contained nested box for InlineItem.NestedBox (see appendElementInline):
// shrink-to-fit at its own preferred width, exactly like a width:auto flex
// item would size against an unconstrained container, since at inline-
// collection time no surrounding line width is known yet (the two-phase
// collect-then-wrap design fixes every atomic inline item's size before line
// breaking, the same reason Image/FormControl are sized up front). Internally
// this behaves exactly like a block-level flex container — inline-vs-block
// only matters for how the PARENT places this box, not how it lays out its
// OWN children — so the isolated clone forces plain DisplayFlex, the value
// contents()'s own dispatch and preferredWidth's flex-row branch already
// know how to handle.
func (l *layouter) layoutNestedInlineFlex(el *dom.Node, st *css.Style) *Box {
	clone := *st
	clone.Display = css.DisplayFlex
	// preferredWidth returns an outer (border+padding-inclusive) measurement
	// — see its own doc comment — while layoutIsolated wants a pure content
	// width (it lays the node out at BoxSizing:ContentBox), so the edges it
	// added have to come back off here.
	bw := clone.Border.Widths()
	edges := bw.Left + bw.Right + clone.Padding.Left + clone.Padding.Right
	w := l.preferredWidth(el, &clone) - edges
	if w < 0 {
		w = 0
	}
	return l.layoutIsolated(el, &clone, w)
}

// distribute returns the leading offset and inter-item gap for a justify-content
// value given free space and item count.
func distribute(j css.Justify, free float64, n int) (offset, gap float64) {
	if n <= 0 {
		return 0, 0
	}
	switch j {
	case css.JustifyEnd:
		return free, 0
	case css.JustifyCenter:
		return free / 2, 0
	case css.JustifySpaceBetween:
		if n > 1 {
			return 0, free / float64(n-1)
		}
		return 0, 0
	case css.JustifySpaceAround:
		g := free / float64(n)
		return g / 2, g
	case css.JustifySpaceEvenly:
		g := free / float64(n+1)
		return g, g
	default: // JustifyStart
		return 0, 0
	}
}

// crossOffset returns the cross-axis offset of an item's outer box within the
// container's cross size for an align-items value.
func crossOffset(a css.AlignItems, containerCross, outerCross float64) float64 {
	switch a {
	case css.AlignFlexEnd:
		return math.Max(containerCross-outerCross, 0)
	case css.AlignCenterItems:
		return math.Max(containerCross-outerCross, 0) / 2
	default: // stretch or flex-start start at the cross-start edge
		return 0
	}
}
