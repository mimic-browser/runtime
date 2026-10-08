// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package layout

import (
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/dom"
)

// layouter carries the shared state for one layout pass.
type layouter struct {
	sm      css.StyleMap
	m       Measurer
	imgSize map[*dom.Node][2]float64 // intrinsic sizes for <img>, may be nil
	floats  *floatCtx
	// outOfFlow collects absolutely/fixed-positioned elements encountered during
	// the in-flow pass (and while laying out other positioned subtrees). They are
	// placed against their containing block after the in-flow layout.
	outOfFlow []outOfFlowItem

	// Inline whitespace-collapsing state, valid only while collecting one inline
	// formatting context (reset at each collectInline / collectInlineFrom entry).
	// wsPending records that the content emitted so far ends with collapsible
	// whitespace, so the next word takes a leading space; wsEmitted records that
	// at least one inline item has been emitted, so leading whitespace at the very
	// start of the context collapses away (no spurious indent). CSS collapses
	// whitespace ACROSS inline-element boundaries, which is why this is layouter
	// state threaded through the recursive collection rather than per text node.
	wsPending bool
	wsEmitted bool

	// preCol is the column the next preserved-whitespace character lands
	// in, counted from the start of the current line across every segment
	// that shares it, so a tab reaches its tab stop (see expandTabs). Reset
	// wherever a line starts: the inline context, a forced break, a
	// promoted block.
	preCol int

	// pendingMargin accumulates the margin-left of a plain inline element about
	// to be entered, plus the margin-right of one just left, until the next
	// InlineItem is created — margin-left applies as space before an inline
	// element's own content starts, margin-right as space after it ends.
	// Confirmed live on news.ycombinator.com: `<b class="hnname"
	// style="margin-right:5px">Hacker News</b>` immediately followed by
	// `<a>new</a>` with no source whitespace between them — the engine's own
	// InlineItem carries no margin field, so the gap a real browser renders
	// from margin-right alone was silently dropped, running the two together
	// as "Hacker Newsnew". Reset alongside the whitespace state at a genuine
	// break (a promoted block or forced <br>): unlike whitespace, which can
	// legitimately carry a value AT that point (deferred to whatever comes
	// after), a stale margin from before a hard break has nothing left on the
	// same line to apply to.
	//
	// Consumed into the SAME SpaceBefore field collapsible whitespace uses
	// (see takeMargin), not a dedicated one — a real, narrow consequence:
	// layoutInline/wrapOneLine/WrapItems deliberately ignore SpaceBefore for
	// a line's very FIRST item (so collapsible leading whitespace never
	// creates a phantom indent), so a margin lands on an element that
	// happens to start a line the SAME way — dropped, not applied. This
	// engine's real, confirmed use of inline margin-right is always between
	// two things already sharing a line (see the test above); a margin on a
	// genuinely line-initial element would need its own field to survive
	// line-breaking, not attempted here absent a confirmed live case.
	pendingMargin float64

	// decor is the chain of inline-level ancestors currently being collected
	// that generate a box of their own (background, border or padding),
	// outermost first. Every InlineItem created while it is in scope keeps a
	// reference to it (see InlineItem.decor), which is what lets layout, and
	// any downstream painter, know which piece of which <span> a word belongs
	// to. Nil for ordinary text, and reset at each inline formatting context.
	decor []inlineDecor
}

// beginInlineContext resets the whitespace-collapsing and pending-margin state
// at the top of an inline formatting context.
func (l *layouter) beginInlineContext() {
	l.wsPending, l.wsEmitted, l.pendingMargin = false, false, 0
	l.preCol = 0
	l.decor = nil
}

// takeMargin returns the accumulated pending inline margin and resets it —
// called exactly once, when the next InlineItem after it is created. Style's
// own Margin.Left/Right are already plain resolved pixel values by this
// point (a percentage margin collapses to 0 at cascade time — see
// applyMarginShorthand — and an auto one leaves Margin.Left/Right at its zero
// value too, tracked instead via the separate MarginLeftAuto/MarginRightAuto
// bools block layout consults), so callers add them into pendingMargin
// directly with no further resolution needed here.
func (l *layouter) takeMargin() float64 {
	m := l.pendingMargin
	l.pendingMargin = 0
	return m
}

// outOfFlowItem is a queued out-of-flow box plus the approximate static position
// (the normal-flow cursor at the point it was skipped) used to resolve the box's
// auto insets, matching CSS's static-position rule far better than dumping it at
// the containing block origin.
type outOfFlowItem struct {
	node             *dom.Node
	staticX, staticY float64
	hasStatic        bool
}

// bfc is the mutable cursor of a block formatting context: y is the committed
// bottom of the content laid out so far, and carry is the collapsible margin
// still pending below y (not yet materialised).
type bfc struct {
	y     float64
	carry float64
}

// commit materialises the pending margin, advancing y past it, returning new y.
func (b *bfc) commit() float64 {
	b.y += b.carry
	b.carry = 0
	return b.y
}

// collapse combines two adjacent margins per CSS: the larger positive plus the
// smaller (most negative) negative.
func collapse(a, c float64) float64 {
	return math.Max(math.Max(a, 0), math.Max(c, 0)) + math.Min(math.Min(a, 0), math.Min(c, 0))
}

// LayoutDocument lays out the element subtree at root within a viewport of
// width viewportW (CSS pixels), returning the root Box and the total content
// height. imgSize provides intrinsic image dimensions keyed by <img> node; it
// may be nil (images then fall back to width/height attributes).
//
// This is LayoutDocumentViewport with no real viewport height available (most
// existing callers, including nearly every test in this repo): a
// position:fixed element is then anchored against the full document height
// instead of a real viewport, the pre-existing approximation this package has
// always used — see LayoutDocumentViewport's own doc comment for why that is
// wrong for a full-page capture and when it matters.
func LayoutDocument(root *dom.Node, sm css.StyleMap, viewportW float64, m Measurer, imgSize map[*dom.Node][2]float64) (*Box, float64) {
	return LayoutDocumentViewport(root, sm, viewportW, 0, m, imgSize)
}

// LayoutDocumentViewport is LayoutDocument with an explicit viewport height,
// used to anchor position:fixed content against the ACTUAL requested
// viewport rather than the full document height. Real browsers keep a
// position:fixed element pinned to the ORIGINAL viewport bounds even when a
// full-page screenshot captures far more than one viewport's worth of
// content (confirmed against real headless Chrome's own captureBeyondViewport
// behaviour); this engine previously had no viewport height available at
// this entry point at all and used the eventual document height instead, so
// a `bottom:0` fixed element (e.g. a cookie-consent banner) landed at the
// very BOTTOM of the whole page rather than near the top where a real browser
// places it — confirmed live on pkg.go.dev/net/http's own cookie banner
// (round 84) and already flagged, but not fixed, as a separate architectural
// gap after round 74's react.dev investigation.
//
// viewportH<=0 falls back to the same full-document-height approximation
// LayoutDocument has always used (needed by callers, mostly tests, with no
// real viewport height to give — the fixed-position CONTAINING BLOCK is the
// only thing this affects; everything else about layout is unchanged).
func LayoutDocumentViewport(root *dom.Node, sm css.StyleMap, viewportW, viewportH float64, m Measurer, imgSize map[*dom.Node][2]float64) (*Box, float64) {
	l := &layouter{sm: sm, m: m, imgSize: imgSize, floats: &floatCtx{}}
	start := firstElement(root)
	if start == nil {
		return &Box{}, 0
	}
	b := &bfc{}
	box := l.place(start, l.sm[start], 0, viewportW, b)
	total := b.commit() // materialise any trailing margin into the page height
	// The page must also cover any float that extends past the flow content.
	if fb := l.floats.bottom(); fb > total {
		total = fb
	}
	// Positioned pass: apply relative offsets, then place out-of-flow
	// (absolute/fixed) boxes against their containing blocks. May grow the page
	// height to cover absolutely-positioned content.
	total = l.positioned(box, viewportW, viewportH, total)
	return box, total
}

func firstElement(n *dom.Node) *dom.Node {
	if n.Type == dom.Element {
		return n
	}
	for _, c := range n.Children {
		if e := firstElement(c); e != nil {
			return e
		}
	}
	return nil
}

// place lays out one block-level box within a containing block whose content
// origin x is cx and content width cw, advancing the block formatting context b.
func (l *layouter) place(node *dom.Node, st *css.Style, cx, cw float64, b *bfc) *Box {
	if st == nil {
		s := css.Style{Display: css.DisplayBlock, Width: css.Length{Auto: true},
			MinWidth: css.Length{Auto: true}, MaxWidth: css.Length{Auto: true},
			Height: css.Length{Auto: true}, ColumnWidth: css.Length{Auto: true}}
		st = &s
	}
	box := &Box{Node: node, Style: st}

	bw := st.Border.Widths()
	contentW, ml, mr := resolveWidths(st, cw)
	_ = mr
	boxLeft := cx + ml
	contentX := boxLeft + bw.Left + st.Padding.Left

	b.carry = collapse(b.carry, st.Margin.Top)

	topSep := bw.Top + st.Padding.Top
	botSep := bw.Bottom + st.Padding.Bottom
	establishes := st.Display == css.DisplayFlex || st.Display == css.DisplayTable ||
		st.Display == css.DisplayGrid

	var borderTopY, contentTopY float64
	sep := topSep > 0 || establishes
	if sep {
		borderTopY = b.commit()
		contentTopY = borderTopY + topSep
		b.y = contentTopY
	} else {
		contentTopY = b.y // provisional; carry still pending, corrected below
	}

	contentBottom := l.contents(box, node, st, contentX, contentW, contentTopY, b, sep)

	if h, ok := usedHeight(st, bw, cw, contentW); ok {
		// An explicit height fixes the content box height; taller content
		// overflows (overflow:visible) rather than growing the box.
		contentBottom = contentTopY + h
		b.y = contentBottom
	}

	if !sep {
		borderTopY = firstContentTop(box, contentTopY)
	}

	if botSep > 0 || establishes {
		b.commit()
		b.y += botSep
	}
	borderBottomY := b.y
	if !sep && borderBottomY < borderTopY {
		borderBottomY = borderTopY
	}

	b.carry = collapse(b.carry, st.Margin.Bottom)

	box.X = boxLeft
	box.Y = borderTopY
	box.W = bw.Left + st.Padding.Left + contentW + st.Padding.Right + bw.Right
	// borderBottomY >= borderTopY always (sep boxes grow downward; non-sep boxes
	// are clamped above), so the height is non-negative.
	box.H = borderBottomY - borderTopY
	box.ContentX = contentX
	box.ContentY = contentTopY
	box.ContentW = contentW
	box.ContentH = contentBottom - contentTopY
	if box.ContentH < 0 {
		box.ContentH = 0
	}
	box.Float = st.Float
	box.Position = st.Position
	return box
}

// firstContentTop returns the top y of the first placed content (the border-top
// of a box with no top border/padding), or the fallback when the box is empty.
func firstContentTop(box *Box, fallback float64) float64 {
	if len(box.Children) > 0 {
		return box.Children[0].Y
	}
	if len(box.Lines) > 0 {
		return box.Lines[0].Y
	}
	return fallback
}

// contents lays out a box's children, dispatching flex and table containers to
// their own algorithms; otherwise an inline formatting context (lines) when it
// has no block-level children, else a sequence of block boxes with anonymous
// inline boxes between runs of inline content. Returns the content bottom y.
func (l *layouter) contents(box *Box, node *dom.Node, st *css.Style, cx, cw, top float64, b *bfc, sep bool) float64 {
	// A replaced element (img / inline svg) renders at its intrinsic size as an
	// atomic box, whatever its `display` value — an <svg display:flex> is still an
	// image, not a flex container over its SVG primitives. This check therefore
	// precedes the flex/grid/table display dispatch.
	if node.Type == dom.Element && isReplacedTag(node.Tag) {
		if w, h := l.imageSize(node); w > 0 && h > 0 {
			dw, dh := resolvedReplacedSize(st, w, h, cw)
			b.commit()
			item := &InlineItem{Image: node, Style: st, ImgW: w, ImgH: h,
				Width: dw, Ascent: dh, LineHeight: dh, X: cx, Y: b.y}
			box.Lines = []*LineBox{{X: cx, Y: b.y, W: cw, H: dh, Items: []*InlineItem{item}}}
			b.y += dh
			return b.y
		}
	}

	// A form control (input/button/select/textarea) is likewise an atomic
	// box — real content, not a container laid out from its DOM children
	// (an <option>'s text is never itself rendered inline; a <button>'s
	// children become its LABEL, not child boxes) — sized explicitly since
	// unlike an image it has no intrinsic bitmap to measure. A hidden input
	// takes no box at all, matching real UA behavior.
	if node.Type == dom.Element && isFormControlTag(node.Tag) {
		if node.Tag == "input" && strings.EqualFold(node.Attr["type"], "hidden") {
			return b.y
		}
		w, h := l.formControlSize(node, st, cw)
		label := l.buttonLabel(node)
		var leadingIcon, icon *dom.Node
		if node.Tag == "button" {
			leadingIcon, icon, _, _, _, _ = l.buttonIcons(node)
			if label == "" && icon == nil && leadingIcon != nil {
				// An icon-only button's sole icon is found "before" the
				// (nonexistent) text by buttonIcons — there's no label for
				// it to be leading/trailing OF, so normalise it into Icon,
				// the field paint's own icon-only case actually reads.
				icon, leadingIcon = leadingIcon, nil
			}
		}
		b.commit()
		item := &InlineItem{Node: node, FormControl: node, Style: st,
			Width: w, Ascent: h, LineHeight: h, X: cx, Y: b.y, Label: label, Icon: icon, LeadingIcon: leadingIcon}
		box.Lines = []*LineBox{{X: cx, Y: b.y, W: cw, H: h, Items: []*InlineItem{item}}}
		b.y += h
		return b.y
	}

	// column-count/column-width/columns is an independent property, not a
	// display value — checked before the switch below, and only for a plain
	// block container (the confirmed real trigger, pkg.go.dev's own
	// `.UnitFiles-fileList{columns:...}`, is a plain `display:block` <ul>;
	// see layoutMultiCol's own doc comment for the full, deliberately
	// narrow scope this models).
	if st.Display == css.DisplayBlock && (st.ColumnCount > 0 || !st.ColumnWidth.Auto) {
		bottom := l.layoutMultiCol(box, node, st, cx, cw, top, b)
		b.y = bottom
		return bottom
	}

	switch st.Display {
	case css.DisplayFlex:
		bottom := l.flex(box, node, st, cx, cw, top, b)
		b.y = bottom // flex is out-of-band; advance the block cursor to its bottom
		return bottom
	case css.DisplayGrid:
		bottom := l.grid(box, node, st, cx, cw, top, b)
		b.y = bottom
		return bottom
	case css.DisplayTable:
		bottom := l.table(box, node, st, cx, cw, top, b)
		b.y = bottom
		return bottom
	}

	return l.blockOrInlineContents(box, node, st, cx, cw, top, b)
}

// blockOrInlineContents lays node's children out as ordinary block-in-flow
// content: a run of inline siblings collected between block-level children,
// each block-level child placed in turn, floats and out-of-flow items handled
// alongside. This is contents' own default (non-flex/grid/table) path, and is
// ALSO what table's own zero-rows fallback calls: CSS's anonymous-table-object
// synthesis (wrapping non-row/cell children of a display:table box in
// implicit rows/cells) isn't implemented, so a table box with no real
// table-row/table-cell descendant at all — e.g. MediaWiki's own thumbnail
// figure, which uses `figure{display:table}` purely as a shrink-to-fit sizing
// trick around a plain `display:block` image wrapper plus a `figcaption`, with
// no table-row or table-cell anywhere — falls back to this instead of table
// silently dropping the whole box. See table's own doc comment.
func (l *layouter) blockOrInlineContents(box *Box, node *dom.Node, st *css.Style, cx, cw, top float64, b *bfc) float64 {
	pre := st.WhiteSpace == css.WSPre
	// white-space: nowrap collects like normal (whitespace collapsed) but
	// places like pre (never wraps); pre implies both. text-wrap:nowrap
	// (round 95) contributes the SAME never-wraps placement behaviour but,
	// per spec, must never affect whitespace collapsing/preservation — it
	// only ever widens `nowrap` here, never `pre` above.
	nowrap := pre || st.WhiteSpace == css.WSNoWrap || st.TextWrapNowrap
	if !l.hasBlockLevelChild(node) {
		b.commit()
		items := l.collectInline(node, st, pre, cw)
		// hasBlockLevelChild only looks at node's DIRECT children — a
		// block-level element nested deeper, under an inline-context
		// ancestor (e.g. `<a><div>...</div></a>`), still shows up here as a
		// BlockBreak sentinel. The common case (no sentinel at all) keeps
		// the exact previous box shape (box.Lines set directly); only the
		// rarer mixed case pays for building box.Children instead.
		if !hasBlockBreak(items) {
			lines, bottom := l.layoutInline(items, st, cx, cw, b.y, nowrap)
			// -webkit-line-clamp/line-clamp: drop every line past the Nth,
			// shrinking the box to end where the Nth line does (see
			// css.Style.LineClamp's own doc comment for the confirmed real
			// need and the documented scope limit — no ellipsis glyph yet).
			if st.LineClamp > 0 && len(lines) > st.LineClamp {
				lines = lines[:st.LineClamp]
				last := lines[len(lines)-1]
				bottom = last.Y + last.H
			}
			box.Lines = lines
			b.y = bottom
			return bottom
		}
		return l.placeInlineSegments(box, items, st, cx, cw, b, nowrap)
	}

	// List-item counter for this block's direct list-item children. It seeds from
	// an <ol start> and honours a per-item <li value>; nested lists get a fresh
	// counter because each list container runs its own contents() pass.
	counter := listStart(node)

	var run []*dom.Node
	flush := func() {
		if len(run) == 0 {
			return
		}
		items := l.collectInlineFrom(run, st, pre, cw)
		run = nil
		if len(items) == 0 {
			return
		}
		// A run of top-level "inline" siblings can still contain a
		// block-level element nested under one of them (an inline wrapper) —
		// placeInlineSegments handles both the plain case (one anonymous
		// box, identical to what this function built directly before) and
		// the mixed one (splitting around each promoted block box).
		l.placeInlineSegments(box, items, st, cx, cw, b, nowrap)
	}

	for _, c := range l.renderedChildren(node) {
		switch {
		case c.Type == dom.Text:
			if strings.TrimSpace(c.Text) == "" {
				continue
			}
			run = append(run, c)
		case c.Type == dom.Element:
			cs := l.sm[c]
			if cs != nil && cs.Display == css.DisplayNone {
				continue
			}
			if cs != nil && cs.Position.OutOfFlow() {
				// Out of flow: reserve no space here, place later. Do not flush the
				// pending inline run — an out-of-flow box does not break the line.
				// Record the current flow cursor as the box's approximate static
				// position (used when its insets are auto).
				l.outOfFlow = append(l.outOfFlow, outOfFlowItem{
					node: c, staticX: cx, staticY: b.y + math.Max(b.carry, 0), hasStatic: true,
				})
				continue
			}
			if cs != nil && cs.Float != css.FloatNone {
				flush()
				l.placeFloat(box, c, cs, cx, cw, b)
				continue
			}
			if cs != nil && isBlockLevel(cs.Display) {
				flush()
				l.handleClear(cs, b)
				child := l.place(c, cs, cx, cw, b)
				box.Children = append(box.Children, child)
				if cs.ListItem {
					if val, ok := attrInt(c, "value"); ok {
						counter = val
					}
					l.attachMarker(child, cs, counter)
					counter++
				}
			} else {
				run = append(run, c)
			}
		}
	}
	flush()
	return b.y
}

// handleClear moves the cursor below the relevant floats for a clear value.
func (l *layouter) handleClear(st *css.Style, b *bfc) {
	if st.Clear == css.ClearNone {
		return
	}
	y := b.y + b.carry
	if cy := l.floats.clearY(st.Clear, y); cy > y {
		b.commit()
		b.y = cy
	}
}

// isBlockLevel reports whether a display value generates a block-level box in
// the parent's flow (breaking the line).
func isBlockLevel(d css.Display) bool {
	switch d {
	case css.DisplayBlock, css.DisplayFlex, css.DisplayGrid, css.DisplayTable,
		css.DisplayTableRowGroup, css.DisplayTableRow, css.DisplayTableCell:
		return true
	}
	return false
}

func (l *layouter) hasBlockLevelChild(node *dom.Node) bool {
	for _, c := range l.renderedChildren(node) {
		if c.Type != dom.Element {
			continue
		}
		cs := l.sm[c]
		if cs == nil {
			continue
		}
		if cs.Position.OutOfFlow() {
			continue // out-of-flow children do not establish a block context
		}
		if cs.Float != css.FloatNone || isBlockLevel(cs.Display) {
			return true
		}
	}
	return false
}

// hasBlockBreak reports whether items contains a BlockBreak sentinel — a
// block-level element found nested under an inline-context ancestor while
// collecting inline content (see InlineItem.BlockBreak).
func hasBlockBreak(items []*InlineItem) bool {
	for _, it := range items {
		if it.BlockBreak != nil {
			return true
		}
	}
	return false
}

// placeInlineSegments consumes items that may contain BlockBreak sentinels,
// appending the result to box.Children and advancing b.y, returning the new
// bottom. Each run of ordinary inline items between sentinels becomes its
// own anonymous block box — the SAME "wrap the inline run, promote the
// block sibling" treatment contents() already applies when a block-level
// element is a DIRECT child of a block container (CSS 2.1 §9.2.1.1) — and
// each sentinel's node is placed as a real box via the normal place()
// dispatch, so it gets real margins and its own block/flex/grid/table/atomic
// layout instead of having its content silently flattened into surrounding
// text. With no sentinels at all, this produces exactly one anonymous box —
// byte-identical to the plain (pre-BlockBreak) code this replaced.
func (l *layouter) placeInlineSegments(box *Box, items []*InlineItem, st *css.Style, cx, cw float64, b *bfc, nowrap bool) float64 {
	var run []*InlineItem
	flushRun := func() {
		if len(run) == 0 {
			return
		}
		b.commit()
		anonTop := b.y
		lines, bottom := l.layoutInline(run, st, cx, cw, anonTop, nowrap)
		run = nil
		anon := &Box{Anonymous: true, Style: st, ContentX: cx, ContentY: anonTop, ContentW: cw}
		anon.Lines = lines
		anon.X, anon.Y, anon.W, anon.H = cx, anonTop, cw, bottom-anonTop
		box.Children = append(box.Children, anon)
		b.y = bottom
	}
	for _, it := range items {
		if it.BlockBreak == nil {
			run = append(run, it)
			continue
		}
		// A FLOATED promoted element (e.g. a classic `<li style="float:left">`
		// button under a `display:inline` `<ul>`, confirmed live on
		// github.com's repo-header action row) does not break the
		// surrounding inline run the way a genuine block does — it is taken
		// out of flow into the float context instead, matching the float
		// check contents() already applies before its own generic block
		// dispatch. Routing it through the plain block l.place() here (as a
		// non-floated BlockBreak correctly is) gave it a full-width,
		// in-flow block box instead of a shrink-to-fit float — and, upstream
		// in preferredWidth, meant its width never counted toward its
		// ancestor's max-content estimate at all, collapsing a
		// flex-shrink:0 container that should have kept its natural width
		// down to zero.
		if it.Style != nil && it.Style.Float != css.FloatNone {
			l.placeFloat(box, it.BlockBreak, it.Style, cx, cw, b)
			continue
		}
		flushRun()
		child := l.place(it.BlockBreak, it.Style, cx, cw, b)
		box.Children = append(box.Children, child)
	}
	flushRun()
	return b.y
}

// resolveWidths computes the used content width and left/right margins of a
// block box in its containing block of content width cw, honouring width,
// min/max-width, box-sizing, auto-margin centring (CSS 10.3.3) and a
// percentage margin-left/margin-right (resolved against cw here — the one
// place this value is known — since Style keeps it unresolved in
// MarginLeftPercent/MarginRightPercent; see their own doc comment). Found
// live on en.wikipedia.org: `.ambox{margin:0 10%}` (gated by
// `@media(min-width:720px)`) never narrowed the box at all, since a
// percentage margin was silently treated as 0 everywhere before this.
func resolveWidths(st *css.Style, cw float64) (contentW, ml, mr float64) {
	bw := st.Border.Widths()
	extra := bw.Left + bw.Right + st.Padding.Left + st.Padding.Right
	mlFixed, mrFixed := st.Margin.Left, st.Margin.Right
	if st.MarginLeftIsPercent {
		mlFixed = st.MarginLeftPercent * cw
	}
	if st.MarginRightIsPercent {
		mrFixed = st.MarginRightPercent * cw
	}
	if st.MarginLeftAuto {
		mlFixed = 0
	}
	if st.MarginRightAuto {
		mrFixed = 0
	}

	widthAuto := st.Width.Auto
	if widthAuto {
		contentW = cw - mlFixed - mrFixed - extra
	} else {
		contentW = st.Width.Resolve(cw)
		if st.BoxSizing == css.BorderBox {
			contentW -= extra
		}
	}

	clamped := clampWidth(contentW, st, cw, extra)
	fixed := !widthAuto || clamped != contentW
	contentW = clamped
	if contentW < 0 {
		contentW = 0
	}

	if !fixed {
		return contentW, mlFixed, mrFixed
	}
	leftover := cw - contentW - extra - mlFixed - mrFixed
	switch {
	case st.MarginLeftAuto && st.MarginRightAuto:
		if leftover > 0 {
			ml = leftover / 2
			mr = leftover - ml
		}
	case st.MarginLeftAuto:
		ml, mr = leftover, mrFixed
	case st.MarginRightAuto:
		ml, mr = mlFixed, leftover
	case st.CenterAsBlock && mlFixed == 0 && mrFixed == 0:
		// Legacy <center> / align="center": centre a definite-width block within
		// its container when no explicit margins constrain it. CenterAsBlock,
		// not a direct TextAlign check, so a quirks-mode <table> (whose OWN
		// TextAlign is separately reset to AlignLeft — see css/ua.go) still
		// centres correctly when ITS parent is the <center>.
		if leftover > 0 {
			ml = leftover / 2
			mr = leftover - ml
		}
	default:
		ml, mr = mlFixed, mrFixed+leftover
	}
	return contentW, ml, mr
}

// clampWidth applies min-width/max-width (box-sizing aware) to a content width.
func clampWidth(contentW float64, st *css.Style, cw, extra float64) float64 {
	if v, ok := widthBound(st.MaxWidth, cw, extra, st.BoxSizing); ok && contentW > v {
		contentW = v
	}
	if v, ok := widthBound(st.MinWidth, cw, extra, st.BoxSizing); ok && contentW < v {
		contentW = v
	}
	return contentW
}

func widthBound(l css.Length, cw, extra float64, bs css.BoxSizing) (float64, bool) {
	if l.Auto {
		return 0, false
	}
	v := l.Resolve(cw)
	if bs == css.BorderBox {
		v -= extra
	}
	if v < 0 {
		v = 0
	}
	return v, true
}

// usedHeight returns an explicit content height when height is set (box-sizing
// aware), else — when height is auto/unresolvable but aspect-ratio is set and
// this box's own resolved content width (contentW) is known — the height that
// ratio implies, else (0,false). Percentage heights are skipped (no definite
// basis); contentW is already in content-box terms (resolveWidths' own
// box-sizing adjustment already applied), so no further box-sizing correction
// is needed for the aspect-ratio branch. Only width-known/height-auto is
// resolved — see css.Style.AspectRatio's own doc comment for why the reverse
// direction isn't attempted.
func usedHeight(st *css.Style, bw css.Edges, cw, contentW float64) (float64, bool) {
	if st.Height.Auto || st.Height.IsPercent {
		if st.AspectRatio > 0 && contentW > 0 {
			return contentW / st.AspectRatio, true
		}
		return 0, false
	}
	h := st.Height.Px
	if st.BoxSizing == css.BorderBox {
		h -= bw.Top + bw.Bottom + st.Padding.Top + st.Padding.Bottom
	}
	if h < 0 {
		h = 0
	}
	return h, true
}

// ---- inline collection -----------------------------------------------------

// cw, on every function in this section, is the containing block width a
// form control's own percentage width/height resolves against — see
// appendElementInline's isFormControlTag branch, the one place it's actually
// read. Pass the block's real content width from a genuine in-flow layout
// call site (contents/blockOrInlineContents, flex's mixed-text fallback);
// pass 0 from an INTRINSIC-sizing call site (preferredWidth's/
// minContentWidth's own inline fallbacks in floats.go/mincontent.go), where
// a percentage is meaningless — CSS itself treats a percentage against an
// indeterminate (max-content/min-content) containing block as auto, which a
// 0 here already degrades to (formControlSize's own UA-default fallback),
// so passing 0 in those two places is correct behaviour, not a shortcut.
func (l *layouter) collectInline(node *dom.Node, st *css.Style, pre bool, cw float64) []*InlineItem {
	var items []*InlineItem
	l.beginInlineContext()
	l.appendInline(node, st, &items, pre, cw)
	resolveInlineEdges(items)
	return items
}

func (l *layouter) collectInlineFrom(nodes []*dom.Node, st *css.Style, pre bool, cw float64) []*InlineItem {
	var items []*InlineItem
	l.beginInlineContext()
	for _, n := range nodes {
		if n.Type == dom.Text {
			l.appendWords(n.Text, st, &items, pre, n.Parent)
		} else {
			cs := l.sm[n]
			if cs == nil {
				cs = st
			}
			l.appendElementInline(n, cs, &items, pre, cw)
		}
	}
	resolveInlineEdges(items)
	return items
}

func (l *layouter) appendInline(node *dom.Node, st *css.Style, items *[]*InlineItem, pre bool, cw float64) {
	for _, c := range l.renderedChildren(node) {
		if c.Type == dom.Text {
			l.appendWords(c.Text, st, items, pre, node)
			continue
		}
		cs := l.sm[c]
		if cs == nil {
			cs = st
		}
		l.appendElementInline(c, cs, items, pre, cw)
	}
}

func (l *layouter) appendElementInline(el *dom.Node, cs *css.Style, items *[]*InlineItem, pre bool, cw float64) {
	if cs.Display == css.DisplayNone {
		return
	}
	if es := l.sm[el]; es != nil && es.Position.OutOfFlow() {
		// Out-of-flow inline-level box: contributes no inline item, placed later.
		// No simple flow cursor here, so its static position falls back to the
		// containing block origin.
		l.outOfFlow = append(l.outOfFlow, outOfFlowItem{node: el})
		return
	}
	switch el.Tag {
	case "br":
		*items = append(*items, &InlineItem{LineBreak: true, Style: cs, Node: el})
		// A forced break starts a new line: leading whitespace after it
		// collapses, and any pending margin has nothing left on this line to
		// apply to (same reasoning as the BlockBreak case below).
		l.wsPending, l.wsEmitted, l.pendingMargin = false, false, 0
		l.preCol = 0
	case "img", "svg":
		w, h := l.imageSize(el)
		if w > 0 && h > 0 {
			dw, dh := resolvedReplacedSize(cs, w, h, cw)
			sb := 0.0
			if l.wsEmitted && l.wsPending {
				sb = l.m.Measure(" ", cs.FontFamily, cs.FontSize, cs.FontWeight, cs.Italic)
			}
			sb += l.takeMargin() + cs.Margin.Left
			*items = append(*items, &InlineItem{
				Style: cs, Image: el, Node: el, ImgW: w, ImgH: h,
				Width: dw, Ascent: dh, LineHeight: dh,
				BaselineShift: l.baselineShiftFor(cs),
				SpaceBefore:   sb, decor: l.decor,
			})
			l.wsEmitted, l.wsPending = true, false
			l.pendingMargin += cs.Margin.Right
		}
	default:
		// display:inline-flex is an INLINE-level box (unlike plain flex,
		// which is block-level and goes through the BlockBreak promotion
		// below) that lays out its own content with the flex algorithm — an
		// atomic item like an image or form control, except its "bitmap" is
		// a real nested Box tree rather than opaque pixels or a drawn
		// control. Checked against the raw style map entry for the same
		// nil-is-not-this-display reason as the isBlockLevel check below.
		// Confirmed live on pkg.go.dev: `.go-Breadcrumb li{display:inline-
		// flex}` stacked each breadcrumb item onto its own line (treated as
		// block-level) instead of flowing in a row, because inline-flex and
		// flex previously parsed down to the same Display value, losing the
		// "inline" qualifier entirely.
		if es := l.sm[el]; es != nil && es.Display == css.DisplayInlineFlex {
			// layoutNestedInlineFlex lays out el's OWN content, which reuses
			// this SAME layouter's wsPending/wsEmitted fields for ITS
			// internal text — capture the OUTER context's values (from
			// whatever preceded this element) before that call clobbers
			// them, or a real leading space here was silently lost whenever
			// el's own content ended in a state that reset them.
			wasWsEmitted, wasWsPending := l.wsEmitted, l.wsPending
			box := l.layoutNestedInlineFlex(el, es)
			sb := 0.0
			if wasWsEmitted && wasWsPending {
				sb = l.m.Measure(" ", cs.FontFamily, cs.FontSize, cs.FontWeight, cs.Italic)
			}
			sb += l.takeMargin() + es.Margin.Left
			*items = append(*items, &InlineItem{
				Style: es, NestedBox: box, Node: el,
				Width: box.W, Ascent: box.H, LineHeight: box.H,
				SpaceBefore: sb, decor: l.decor,
			})
			l.wsEmitted, l.wsPending = true, false
			l.pendingMargin += es.Margin.Right
			return
		}
		// display:inline-block is likewise an INLINE-level atomic box whose
		// content is a genuine nested formatting context — see
		// layoutNestedInlineBlock's own doc comment (found live on
		// en.wikipedia.org: an EMPTY `<span style="display:inline-block;
		// width:1rem;height:1rem">` styled entirely via mask-image, which
		// only ever paints through the real Box path this engine previously
		// never gave a plain display:inline-block element at all).
		if es := l.sm[el]; es != nil && es.Display == css.DisplayInlineBlock {
			wasWsEmitted, wasWsPending := l.wsEmitted, l.wsPending
			box := l.layoutNestedInlineBlock(el, es)
			sb := 0.0
			if wasWsEmitted && wasWsPending {
				sb = l.m.Measure(" ", cs.FontFamily, cs.FontSize, cs.FontWeight, cs.Italic)
			}
			sb += l.takeMargin() + es.Margin.Left
			*items = append(*items, &InlineItem{
				Style: es, NestedBox: box, Node: el,
				Width: box.W, Ascent: box.H, LineHeight: box.H,
				SpaceBefore: sb, decor: l.decor,
			})
			l.wsEmitted, l.wsPending = true, false
			l.pendingMargin += es.Margin.Right
			return
		}
		// A genuinely block-level element (display:block/flex/grid/table, or
		// a form control explicitly given one of those) found while
		// collecting INLINE content must be promoted to a real sibling box,
		// not flattened into the surrounding text — see BlockBreak's own doc
		// comment for the CSS 2.1 §9.2.1.1 rule and the real pages this was
		// found on. Recursing into its children here (the old behaviour)
		// skipped its own box entirely: no margins, no background, no
		// flex/grid layout for whatever is inside it. placeInlineSegments
		// (called by every consumer of the items this function fills)
		// strips this sentinel out and places the real node via the normal
		// place()/contents() dispatch, which already handles a block-level
		// form control as an atomic box correctly when reached this way
		// (the same path a form control given display:block as a DIRECT
		// child of a block container already went through).
		//
		// Checked against the RAW style map entry (l.sm[el]), not the cs
		// parameter — cs is the caller's already-substituted "use the
		// parent's style when this element has none" fallback (see
		// appendInline/collectInlineFrom), matching the SAME nil-is-not-
		// block-level convention hasBlockLevelChild and contents()'s own
		// per-child dispatch already use, and the out-of-flow check just
		// above in this very function. Using cs.Display directly would
		// treat "no style resolved for this specific element" as "inherit
		// whatever block-level-ness some ancestor happened to have",
		// corrupting layout for a node with a genuinely unresolved style.
		if es := l.sm[el]; es != nil && isBlockLevel(es.Display) {
			*items = append(*items, &InlineItem{BlockBreak: el, Style: cs})
			// The promoted block starts its own anonymous box/line (see
			// placeInlineSegments), so any whitespace pending before it must
			// not carry across as a phantom leading space on the FIRST word
			// of the run that resumes after it — the same reset the "br"
			// case above already applies for the identical reason. Any
			// pending margin (from a preceding sibling's margin-right) is
			// dropped the same way: nothing remains on this line to apply it
			// to once a hard block break ends it.
			l.wsPending, l.wsEmitted, l.pendingMargin = false, false, 0
			l.preCol = 0
			return
		}
		// A form control defaults to display:inline (see css/ua.go) and so is
		// laid out HERE, as a child of whatever block contains it — the
		// common case (an <input> inside a <div>/<label>/<form>). It is
		// still an atomic box like img/svg, just sized differently (no
		// intrinsic bitmap — formControlSize resolves explicit CSS or a
		// UA-shaped default). cw is this function's own parameter (see the
		// doc comment above collectInline), threaded down from whatever real
		// containing width the caller's OWN layout pass has resolved — a
		// percentage width/height on a form control reached through ordinary
		// inline collection (e.g. caniuse.com's own `.ciu-search__input
		// {width:40%}`, previously always resolving to the UA-shaped default
		// regardless of the input's real container, effectively invisible at
		// a few pixels wide) now resolves against it exactly like the
		// display:block-routed form-control branch above already could.
		if isFormControlTag(el.Tag) {
			if el.Tag == "input" && strings.EqualFold(el.Attr["type"], "hidden") {
				return
			}
			w, h := l.formControlSize(el, cs, cw)
			sb := 0.0
			if l.wsEmitted && l.wsPending {
				sb = l.m.Measure(" ", cs.FontFamily, cs.FontSize, cs.FontWeight, cs.Italic)
			}
			sb += l.takeMargin() + cs.Margin.Left
			label := l.buttonLabel(el)
			var leadingIcon, icon *dom.Node
			if el.Tag == "button" {
				leadingIcon, icon, _, _, _, _ = l.buttonIcons(el)
				if label == "" && icon == nil && leadingIcon != nil {
					// See the mirror comment at contents()'s own identical
					// normalisation above.
					icon, leadingIcon = leadingIcon, nil
				}
			}
			*items = append(*items, &InlineItem{
				Style: cs, FormControl: el, Node: el,
				Width: w, Ascent: h, LineHeight: h,
				SpaceBefore: sb, Label: label, Icon: icon, LeadingIcon: leadingIcon, decor: l.decor,
			})
			l.wsEmitted, l.wsPending = true, false
			l.pendingMargin += cs.Margin.Right
			return
		}
		// A plain inline element contributes no box of its own — its margin
		// is instead space around wherever its content ends up: margin-left
		// as leading space before its first descendant InlineItem,
		// margin-right as trailing space before whatever InlineItem follows
		// it (see pendingMargin's own doc comment). Adjacent inline margins
		// simply add up rather than collapsing, matching real CSS and
		// falling out naturally here since pendingMargin only ever
		// accumulates between takeMargin() calls.
		//
		// A plain inline element DOES, however, generate a box for its own
		// background, border and padding — one fragment per line box it
		// spans. pushDecor records it for every item collected inside it; the
		// horizontal edges it reserves in the line, and the fragment geometry
		// a painter needs, both fall out of that chain (resolveInlineEdges,
		// placeLine).
		l.pendingMargin += cs.Margin.Left
		saved := l.decor
		l.decor = l.pushDecor(el, cs)
		l.appendInline(el, cs, items, pre || cs.WhiteSpace == css.WSPre, cw)
		l.decor = saved
		l.pendingMargin += cs.Margin.Right
	}
}

func (l *layouter) appendWords(text string, st *css.Style, items *[]*InlineItem, pre bool, origin *dom.Node) {
	// Every text node reaches an item through here, which is why the
	// rendering-time case change belongs here: measurement below and paint
	// downstream then see one and the same string.
	text = applyTextTransform(text, st)
	asc, lh := l.lineMetricsFor(st)
	if pre {
		for i, seg := range strings.Split(text, "\n") {
			if i > 0 {
				*items = append(*items, &InlineItem{LineBreak: true, Style: st, Node: origin})
				l.preCol = 0
			}
			ts := 8
			if st != nil {
				ts = st.TabSize
			}
			seg = l.expandTabs(seg, ts)
			if seg == "" {
				continue
			}
			*items = append(*items, &InlineItem{
				Text:        seg,
				Style:       st,
				Node:        origin,
				Width:       l.m.Measure(seg, st.FontFamily, st.FontSize, st.FontWeight, st.Italic) + letterSpacingWidth(seg, st),
				SpaceBefore: l.takeMargin(),
				Ascent:      asc,
				LineHeight:  lh,
				decor:       l.decor,
			})
		}
		return
	}
	space := l.m.Measure(" ", st.FontFamily, st.FontSize, st.FontWeight, st.Italic)
	// Words split on collapsible whitespace ONLY (isSpace) — never on
	// strings.Fields' unicode.IsSpace, which also splits on the no-break
	// space U+00A0. A no-break space is part of its word: it is drawn at its
	// own width and never breaks (a French "428 630,96 €", a "15.2.1.  100"
	// table-of-contents entry, confirmed live on rfc-editor.org where the
	// dropped spaces glued "." to "100" in the PDF export).
	words := strings.FieldsFunc(text, isSpace)
	if len(words) == 0 {
		// A whitespace-only (or empty) text node between inline content carries a
		// single collapsible space to the next word — unless nothing has been
		// emitted yet, in which case leading whitespace collapses to nothing.
		if text != "" {
			l.wsPending = true
		}
		return
	}
	// Leading whitespace of this run is a collapsible space before its first word.
	if isSpace(rune(text[0])) {
		l.wsPending = true
	}
	for i, w := range words {
		sb := 0.0
		if i > 0 {
			sb = space // whitespace between words within a run collapses to one space
		} else if l.wsEmitted && l.wsPending {
			sb = space // a boundary space — but never a leading indent on the first item
		}
		if i == 0 {
			sb += l.takeMargin()
		}
		*items = append(*items, &InlineItem{
			Text:        w,
			Style:       st,
			Node:        origin,
			Width:       l.m.Measure(w, st.FontFamily, st.FontSize, st.FontWeight, st.Italic) + letterSpacingWidth(w, st),
			SpaceBefore: sb,
			Ascent:      asc,
			LineHeight:  lh,
			decor:       l.decor,
		})
		l.wsEmitted = true
		l.wsPending = false
	}
	// Trailing whitespace defers a space to whatever inline content comes next.
	if isSpace(rune(text[len(text)-1])) {
		l.wsPending = true
	}
}

// isSpace reports whether r is ASCII whitespace subject to CSS whitespace
// collapsing (space, tab, newline, carriage return, form feed).
func isSpace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f'
}

// expandTabs replaces each tab of a preserved-whitespace segment with the
// spaces that reach the next tab stop, counting columns from the start of
// the line (preCol persists across the segments that share a line — a
// `<span>` in the middle of a `<pre>` line — and resets at each line
// break). A tab left in the text has no glyph: the measurer gave it no
// width while a PDF exporter drew it as the font's .notdef box, so the
// text after it overran its neighbour by one box (confirmed live on
// pkg.go.dev's source listings: "= 100 // RFC" printed as "= 100// RFC"
// behind a tofu box).
//
// tabSize is the element's own CSS `tab-size` (INHERITED; 8 is the CSS
// initial value, not a hardcoded assumption — see css.Style.TabSize's own
// doc comment). Confirmed live (round 91): pkg.go.dev's own
// `pre,textarea.code{tab-size:4}` on its real, tab-indented Go source
// samples — this function previously ignored the property entirely and
// always expanded to 8, doubling the real indentation on every such page.
func (l *layouter) expandTabs(seg string, tabSize int) string {
	if !strings.ContainsRune(seg, '\t') {
		l.preCol += utf8.RuneCountInString(seg)
		return seg
	}
	var b strings.Builder
	for _, r := range seg {
		if r != '\t' {
			b.WriteRune(r)
			l.preCol++
			continue
		}
		if tabSize <= 0 {
			continue // CSS Text 3: tab-size:0 (or an unresolved property) renders no tab at all
		}
		n := tabSize - l.preCol%tabSize
		b.WriteString(strings.Repeat(" ", n))
		l.preCol += n
	}
	return b.String()
}

// lineMetricsFor returns the ascent and line height for a style, honouring an
// explicit line-height (which sets the line box height, centring the font's
// natural height within it).
func (l *layouter) lineMetricsFor(st *css.Style) (ascent, lineHeight float64) {
	asc, fh := l.m.Metrics(st.FontFamily, st.FontSize, st.FontWeight, st.Italic)
	lh, ok := st.LineHeight.Resolve(st.FontSize)
	if !ok {
		return asc, fh
	}
	// Distribute the extra (or negative) leading equally above and below the
	// font's natural box (CSS half-leading), so the baseline stays centred.
	return asc + (lh-fh)/2, lh
}

// baselineShiftFor returns how far DOWN a replaced item's default
// baseline-aligned position (its bottom margin edge on the line's baseline)
// must move to honour st's `vertical-align`. Only VAlignTextBottom currently
// has an effect (see css.Style.VerticalAlign's own doc comment for scope):
// "text-bottom" aligns the item's bottom edge with the BOTTOM OF THE FONT's
// own em-box, i.e. one descent below the baseline — the raw font metrics
// (not lineMetricsFor's half-leading-adjusted ones, which describe the
// USED line box, not the font itself) give exactly that descent as fh-asc.
func (l *layouter) baselineShiftFor(st *css.Style) float64 {
	if st.VerticalAlign != css.VAlignTextBottom {
		return 0
	}
	asc, fh := l.m.Metrics(st.FontFamily, st.FontSize, st.FontWeight, st.Italic)
	return fh - asc
}

// letterSpacingWidth returns how much wider text is than the Measurer's own
// (letter-spacing-unaware) advance reports, for st.LetterSpacing added after
// EVERY character of text — including the last, matching real browsers (see
// css.Style.LetterSpacing's own doc comment for the confirmed real need and
// its documented scope: only this, the line-wrapping width, and paint.
// drawText's own per-glyph loop apply it).
func letterSpacingWidth(text string, st *css.Style) float64 {
	if st.LetterSpacing == 0 {
		return 0
	}
	return float64(utf8.RuneCountInString(text)) * st.LetterSpacing
}

// isReplacedTag reports whether an element is a replaced box laid out at an
// intrinsic size (a raster/SVG <img>, or an inline <svg> rasterised upstream).
func isReplacedTag(tag string) bool {
	return tag == "img" || tag == "svg"
}

// isFormControlTag reports whether an element is a form control laid out as
// its own atomic box (see the contents() branch above) rather than through
// its DOM children.
func isFormControlTag(tag string) bool {
	switch tag {
	case "input", "button", "select", "textarea":
		return true
	}
	return false
}

// formControlSize resolves a form control's used box size: an explicit
// non-auto CSS width/height first (percentages resolved against cw, the
// same containing-block basis an ordinary block uses), else a UA default
// sized to its kind — close enough to a real browser's own defaults for the
// controls a login-shaped form actually uses. A button-like control (an
// <input type=button/submit/reset>, or a <button>) sizes to fit its own
// label text plus padding, the same way a real browser's default
// (intrinsic, content-sized) button does.
func (l *layouter) formControlSize(node *dom.Node, st *css.Style, cw float64) (w, h float64) {
	if !st.Width.Auto {
		w = st.Width.Resolve(cw)
	}
	if !st.Height.Auto {
		h = st.Height.Resolve(cw)
	}
	if w > 0 && h > 0 {
		return w, h
	}
	dw, dh := l.formControlDefaultSize(node, st)
	if w <= 0 {
		w = dw
	}
	if h <= 0 {
		h = dh
	}
	return w, h
}

// formControlPadX/Y are the button-like controls' label padding (matching
// typical UA default button padding closely enough to look intentional).
const formControlPadX, formControlPadY = 12.0, 6.0

// buttonIconGap is the horizontal gap between a button's text label and its
// trailing icon (see buttonIcon), approximating github.com's own real
// margin-left:4px between a nav dropdown's label and its caret. Shared by
// name only (paint has its own identical constant, paintFormControl's
// buttonIconGap) — the two packages don't share layout constants directly,
// so both must be kept in sync by hand if this value ever changes.
const buttonIconGap = 4.0

// formControlDefaultSize is called only for a tag isFormControlTag already
// accepted (input/button/select/textarea), so its outer switch's every real
// case is covered by construction; w/h are named returns assigned by
// whichever case matches and returned once at the bottom, rather than each
// case returning directly, so that one line — not an unreachable trailing
// fallback — is what a coverage tool sees every call flow through.
func (l *layouter) formControlDefaultSize(node *dom.Node, st *css.Style) (w, h float64) {
	textHeight := st.FontSize + 10 // ~ real UA text-input default height at common font sizes
	switch node.Tag {
	case "input":
		switch strings.ToLower(node.Attr["type"]) {
		case "checkbox", "radio":
			w, h = 13, 13
		case "button", "submit", "reset":
			w, h = l.buttonSize(controlLabel(node), st)
		default: // text, email, password, search, tel, url, number, date, …
			w, h = 170, textHeight
		}
	case "button":
		// Unlike <input type=button/submit> (a real UA default label, see
		// controlLabel), a <button> tag with no visible text renders with
		// NO label at all in every major browser — an icon-only button
		// (e.g. pkg.go.dev's search-submit button, an <img>/<svg> child with
		// no text) sizes to its icon's own intrinsic size plus padding, the
		// same box a real browser gives it, rather than the padding-only
		// floor a fabricated empty label would leave (see buttonIcon).
		label := l.buttonLabel(node)
		leading, trailing, lw, lh, tw, th := l.buttonIcons(node)
		switch {
		case label == "" && leading != nil:
			// buttonIcons only ever populates "before" when no visible text
			// preceded it (see its seenText walk) — so an empty label (which
			// requires every text node in the same subtree to be whitespace-
			// only) can NEVER leave trailing populated instead: label=="" and
			// trailing!=nil is not a reachable combination, only this one is.
			w, h = lw+2*formControlPadX, lh+2*formControlPadY
		case label != "" && (leading != nil || trailing != nil):
			// Text label plus a leading and/or trailing icon — github.com's
			// own Primer "<> Code ▾" button has BOTH at once (round 92); its
			// nav dropdown triggers (round 85, "Platform▾") have only a
			// trailing one. Width is the label's own measured width plus a
			// fixed gap (buttonIconGap, shared with paint's own drawing of
			// the same layout) on each side that has an icon, plus that
			// icon's own width; height is the tallest of the text's own
			// line height and either icon's height.
			labelW := l.m.Measure(label, st.FontFamily, st.FontSize, st.FontWeight, st.Italic)
			w = labelW + 2*formControlPadX
			lineH := st.FontSize
			if leading != nil {
				w += lw + buttonIconGap
				if lh > lineH {
					lineH = lh
				}
			}
			if trailing != nil {
				w += tw + buttonIconGap
				if th > lineH {
					lineH = th
				}
			}
			h = lineH + 2*formControlPadY
		default:
			// No icon at all, or an ambiguous icon-only case (both sides
			// present with no text — no confirmed real need): the plain
			// padding-only sizing, same as a labelless, icon-less button.
			w, h = l.buttonSize(label, st)
		}
	case "select":
		// A real <select> sizes itself to its WIDEST option's label, not a
		// flat default — matching the common cross-engine pattern (e.g.
		// WebKit's RenderMenuList::updateOptionsWidth), read from that
		// engine's own source rather than assumed, so the control does not
		// visually resize as a different option becomes selected. The flat
		// text-input default (170) is unrelated to any option's own size —
		// it applies ONLY to a <select> with no options to measure at all,
		// never as a floor that a real, narrower set of options gets
		// clamped up to.
		h = textHeight
		labels := selectOptionLabels(node)
		if len(labels) == 0 {
			w = 170
			break
		}
		for _, label := range labels {
			if lw := l.m.Measure(label, st.FontFamily, st.FontSize, st.FontWeight, st.Italic) + 2*formControlPadX; lw > w {
				w = lw
			}
		}
	case "textarea":
		w, h = 200, 60
	}
	return w, h
}

// buttonSize sizes a button-like control to fit label at st's font, plus
// padding — an intrinsic, content-sized box, matching how a real browser's
// unstyled <button>/submit input sizes itself (unlike a plain text input,
// which gets a fixed UA default width regardless of content).
func (l *layouter) buttonSize(label string, st *css.Style) (float64, float64) {
	tw := l.m.Measure(label, st.FontFamily, st.FontSize, st.FontWeight, st.Italic)
	return tw + 2*formControlPadX, st.FontSize + 2*formControlPadY
}

// controlLabel returns an <input type=button/submit/reset>'s visible label:
// its value attribute if set, else the type-appropriate UA default text.
func controlLabel(n *dom.Node) string {
	if v, ok := n.Attribute("value"); ok && v != "" {
		return v
	}
	switch strings.ToLower(n.Attr["type"]) {
	case "reset":
		return "Reset"
	default: // submit and button both default to "Submit" in every major UA
		return "Submit"
	}
}

// buttonLabel returns a <button>'s rendered label: the concatenation of its
// VISIBLE (non-display:none) descendant text, in document order. A <button>
// is laid out as an atomic box in this engine (see the isFormControlTag
// branch above) rather than as a real container of child boxes, so its
// label has always come from dom.TextContent — but that walk has no notion
// of computed style and includes text under a display:none descendant too.
// That is not hypothetical: GitHub's site-header search trigger nests a
// responsive text label next to a keyboard-shortcut hint ("/"), each shown
// only at a different breakpoint via `display:none`/`display:block` on
// nested spans/kbd — dom.TextContent concatenated both into a single
// "Search/" label at every width, real browsers show only whichever one (if
// either) is not display:none. This mirrors dom.TextContent's own recursive
// walk (see dom/mutate.go), just pruning a display:none element's entire
// subtree instead of recursing into it.
func (l *layouter) buttonLabel(n *dom.Node) string {
	var b strings.Builder
	l.appendVisibleText(n, &b)
	return strings.TrimSpace(b.String())
}

func (l *layouter) appendVisibleText(n *dom.Node, b *strings.Builder) {
	for _, c := range n.Children {
		switch c.Type {
		case dom.Text:
			b.WriteString(c.Text)
		case dom.Element:
			if cs := l.sm[c]; cs != nil && cs.Display == css.DisplayNone {
				continue
			}
			l.appendVisibleText(c, b)
		}
	}
}

// buttonIcons returns a "button"-tag node's own leading and/or trailing
// icon (an img/svg found respectively before/after the button's own visible
// text, in document order) and their used sizes. Searches ALL descendants,
// not just direct children — real markup commonly wraps an icon in a
// non-replaced wrapper element (confirmed live, round 92: github.com's own
// Primer `<button><span data-component="leadingVisual"><svg .../></span>
// <span>Code</span><span data-component="trailingVisual"><svg .../></span>
// </button>` — a WYSIWYG-generated wrapper span this engine has no reason to
// special-case by class name, so the split is purely "before the text" vs
// "after the text", the same signal a real browser's own layout would use
// were it asked the same question). This is the same real "Code" button
// that motivated the two-icon case: github.com's nav dropdown triggers
// (round 85, "Platform▾" — a single TRAILING icon) established the
// label+icon shape; the Code button additionally has a LEADING icon,
// alongside the pkg.go.dev-style icon-ONLY button (no text at all, so
// every icon found counts as "leading" by this same rule, with no trailing
// side — callers needing the icon-only shape read whichever of the two
// returned nodes is non-nil). Fires regardless of whether the button also
// has visible text — see InlineItem.Icon/LeadingIcon's own doc comment for
// how callers use the label they already have to pick a drawing shape. A
// side reports a nil node and zero size when it has no img/svg at all, MORE
// than one on that SAME side (ambiguous — no confirmed real case mixes two
// icons on one side), or its lone candidate's size never resolved (e.g. its
// fetch failed or budget was exceeded); the OTHER side is resolved
// independently and unaffected by that.
func (l *layouter) buttonIcons(node *dom.Node) (leading, trailing *dom.Node, lw, lh, tw, th float64) {
	seenText := false
	var before, after []*dom.Node
	var walk func(n *dom.Node)
	walk = func(n *dom.Node) {
		for _, c := range n.Children {
			if cs := l.sm[c]; cs != nil && cs.Display == css.DisplayNone {
				continue
			}
			switch c.Type {
			case dom.Text:
				if strings.TrimSpace(c.Text) != "" {
					seenText = true
				}
			case dom.Element:
				if isReplacedTag(c.Tag) {
					if seenText {
						after = append(after, c)
					} else {
						before = append(before, c)
					}
					continue // an <img>/<svg> is atomic; never descend into its own children
				}
				walk(c)
			}
		}
	}
	walk(node)
	if len(before) == 1 {
		if w, h := l.imageSize(before[0]); w > 0 && h > 0 {
			leading, lw, lh = before[0], w, h
		}
	}
	if len(after) == 1 {
		if w, h := l.imageSize(after[0]); w > 0 && h > 0 {
			trailing, tw, th = after[0], w, h
		}
	}
	return
}

func (l *layouter) imageSize(el *dom.Node) (float64, float64) {
	if l.imgSize != nil {
		if wh, ok := l.imgSize[el]; ok {
			return wh[0], wh[1]
		}
	}
	return attrFloat(el, "width"), attrFloat(el, "height")
}

// resolvedReplacedSize computes a replaced element's (img/svg) DISPLAY size
// from its own CSS width/height/max-width, resolved against cw — the REAL
// available containing width at this exact point in layout. This is
// something the pre-layout image-loading step (images.go) cannot do: it only
// knows the page's viewport width, not any nested container's narrower one,
// so it sizes a percentage width against the viewport and otherwise leaves
// the image at its full loaded resolution. Confirmed live on
// github.com/golang/go's own README: `<img style="max-width:100%">` sits in
// a ~646px-wide article column on a 1024px-wide page — the loader's own
// viewport-relative sizing left the image at its full ~1262px intrinsic
// width, overflowing the article and misaligning everything below it.
// iw,ih is the element's own loaded/intrinsic size; returns it UNCHANGED
// when nothing constrains it, matching prior behaviour exactly for the —
// overwhelmingly common — no-CSS-size case.
//
// Implements CSS 2.1 §10.3.2's own numbered rules for an inline replaced
// element's used width, read verbatim rather than assumed symmetric: rule 2
// covers width:auto with an explicit height ("used width = used height *
// intrinsic ratio") — the case this function got wrong before this fix,
// always returning the UNSCALED intrinsic width paired with the explicit
// height instead of deriving width from the ratio (found via issue #226,
// reported against a real A0 poster export through go-pdfkit/html2pdf).
// §10.6.2's mirror-image rule (height:auto, explicit width → derive height
// from width via the ratio) was already correct — that's the pre-existing
// "found missing (round 90)" fix below, for tailwindcss.com's gallery
// `<img>`s setting BOTH `w-full` AND `h-40` independently, which needs BOTH
// dimensions to win outright with no ratio involved at all once both are
// explicit — this function must tell "width explicit, height explicit"
// (return both, no ratio) apart from "width auto, height explicit" (derive
// width), which the pre-fix code conflated into a single check on height
// alone. A percentage height is NOT resolved (deliberately, matching
// `max-width`'s own containing-block requirement above) — no containing-block
// HEIGHT is available/meaningful at this call site, unlike width's own `cw`
// parameter — and `max-height` has no confirmed real caller yet either.
func resolvedReplacedSize(st *css.Style, iw, ih, cw float64) (float64, float64) {
	if st == nil || iw <= 0 || ih <= 0 {
		return iw, ih
	}
	heightExplicit := !st.Height.Auto && !st.Height.IsPercent && st.Height.Px > 0
	w := iw
	switch {
	case !st.Width.Auto && (!st.Width.IsPercent || cw > 0):
		if width := st.Width.Resolve(cw); width > 0 {
			w = width
		}
	case st.Width.Auto && heightExplicit:
		// §10.3.2 rule 2: width auto, height not auto, intrinsic ratio present.
		w = st.Height.Px * iw / ih
	}
	if !st.MaxWidth.Auto && (!st.MaxWidth.IsPercent || cw > 0) {
		if maxW := st.MaxWidth.Resolve(cw); maxW > 0 && maxW < w {
			w = maxW
		}
	}
	if heightExplicit {
		return w, st.Height.Px
	}
	if w == iw {
		return iw, ih
	}
	return w, ih * w / iw
}

func attrFloat(el *dom.Node, name string) float64 {
	v, ok := el.Attribute(name)
	if !ok {
		return 0
	}
	v = strings.TrimSuffix(strings.TrimSpace(v), "px")
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0
	}
	return f
}

// layoutInline breaks items into lines and positions them, honouring text-align
// and any floats intruding into each line's vertical band. cx/cw are the content
// origin x and width; y is the top of the first line. Returns lines and the
// bottom y after the last line.
func (l *layouter) layoutInline(items []*InlineItem, st *css.Style, cx, cw, y float64, nowrap bool) ([]*LineBox, float64) {
	fbAsc, fbH := l.lineMetricsFor(st)
	if nowrap {
		lines := WrapItems(items, math.MaxFloat32)
		if st.TextOverflowEllipsis && st.OverflowX.Clips() {
			for _, line := range lines {
				l.truncateLineWithEllipsis(line, st, cw)
			}
		}
		return lines, placeSimpleLines(lines, cx, cw, y, st, fbH, fbAsc)
	}

	// text-wrap:balance re-breaks a short, float-free run at the narrowest
	// width that still uses the SAME number of lines as an ordinary greedy
	// wrap (see balanceWidth's own doc comment) — scoped to no floats at all
	// anywhere in the document yet, since a per-line float-narrowed width
	// (the general case just below) has no single "cw" this fast path could
	// binary-search against. Confirmed live (round 93): tailwindcss.com's own
	// hero `<h1 class="text-balance">` heading, never inside a floated
	// column in this corpus.
	if st.TextWrapBalance && len(l.floats.lefts) == 0 && len(l.floats.rights) == 0 {
		if n := len(WrapItems(items, cw)); n > 1 && n <= 10 {
			lines := WrapItems(items, balanceWidth(items, cw, n))
			if len(lines) == n { // defensive: binary search must preserve n
				return lines, placeSimpleLines(lines, cx, cw, y, st, fbH, fbAsc)
			}
		}
	}

	var lines []*LineBox
	cursor := y
	rest := items
	guardH := math.Max(fbH, 1)
	for len(rest) > 0 {
		left, right := l.floats.available(cursor, cursor+guardH, cx, cx+cw)
		avail := right - left // available() guarantees right >= left
		line, consumed := wrapOneLine(rest, avail)
		if consumed == 0 {
			if ny := l.floats.nextEdge(cursor, cx, cx+cw); ny > cursor {
				cursor = ny
				continue
			}
			if head, tail, ok := l.splitOverlongWord(rest, avail); ok {
				// rest[0] is guaranteed to be the non-fitting item here: a
				// leading LineBreak would already have made wrapOneLine
				// return consumed==1 above, never 0.
				rest = append([]*InlineItem{head, tail}, rest[1:]...)
				continue
			}
			line, consumed = forceOne(rest)
		}
		rest = rest[consumed:]
		lineH, baseline, used := lineMetrics(line, fbH, fbAsc)
		placeLine(line, alignOffsetIn(st.TextAlign, left, right, used), cursor, baseline)
		line.X, line.Y, line.W, line.H = left, cursor, right-left, lineH
		lines = append(lines, line)
		cursor += lineH
		// A `<br>` with nothing after it (brokeAtEnd, rest now empty) does NOT
		// open a further, visibly-empty line of its own — confirmed live
		// (round 97): go.dev/blog's own `<span class="author">…<br></span>`
		// markup ends every post title with a trailing break, and real Chrome
		// gives it no extra height at all. Each EARLIER break in a run still
		// gets its own real empty line exactly as before (wrapOneLine already
		// returned an empty `line` for it, appended above on ITS OWN loop
		// iteration) — only the break that is genuinely the LAST thing in the
		// whole run, with no further content to end up on a following line,
		// is suppressed here.
	}
	// A truly empty inline box (no items) yields no lines and stays zero-height;
	// any items always produce at least one line above.
	return lines, cursor
}

// wrapOneLine greedily fills one line from items within maxW, stopping at a
// forced break. Returns the line and how many items it consumed (incl. a
// consumed LineBreak). A leading item — or an unbreakable glued run (see
// glueRun in linebreak.go) — wider than maxW is not taken (consumed==0) so the
// caller can try to drop past a float first; if that doesn't help either, the
// caller's forceOne places just the run's first item, splitting the run only
// as an overflow-of-last-resort, never as an ordinary wrap point.
func wrapOneLine(items []*InlineItem, maxW float64) (line *LineBox, consumed int) {
	line = &LineBox{}
	w := 0.0
	i := 0
	for i < len(items) {
		it := items[i]
		if it.LineBreak {
			i++
			return line, i
		}
		// An inline element's own leading/trailing border+padding is part of
		// what the item occupies on the line (see InlineItem.padLead), so it
		// counts toward the break decision exactly like the word's own width —
		// glueRun folds every item's own padLead/Width/padTrail into runW.
		j, runW := glueRun(items, i)
		add := runW
		if len(line.Items) > 0 {
			add += it.SpaceBefore
		}
		if len(line.Items) > 0 && w+add > maxW {
			return line, i
		}
		if len(line.Items) == 0 && runW > maxW {
			return line, i
		}
		line.Items = append(line.Items, items[i:j]...)
		w += add
		i = j
	}
	return line, i
}

// forceOne places exactly the first non-break item (overflowing) on a line.
func forceOne(items []*InlineItem) (*LineBox, int) {
	line := &LineBox{}
	i := 0
	for i < len(items) && items[i].LineBreak {
		i++
	}
	if i < len(items) {
		line.Items = append(line.Items, items[i])
		i++
	}
	return line, i
}

// splitOverlongWord attempts to split items[0] — always a non-break item that
// does not fit maxW even alone, the same one forceOne would otherwise place
// whole; layoutInline only ever calls this right before falling through to
// forceOne, after wrapOneLine has already returned consumed==0, which only
// happens once it has confirmed items[0] itself is not a LineBreak and does
// not fit (see wrapOneLine's own "len(line.Items) == 0 && runW > maxW"
// check) — into a prefix that fits within maxW and a suffix that continues
// as its own item, when that item's own style requests it
// (css.Style.BreaksOverlongWords — word-break:break-all, or overflow-wrap/
// word-wrap:break-word/anywhere).
//
// It returns ok=false — leaving the caller to fall through to forceOne's
// ordinary overflow — when items[0] is not a splittable text item at all (an
// image, a form control, a nested box), when it is part of a glued run of
// more than one item (see glueRun's own doc comment: splitting WITHIN a run
// like `152,3<sup>†</sup>` has no confirmed real-world trigger, unlike a
// single long "word" — the evidenced case this models), when its style does
// not request this, or when it has fewer than two runes (nothing to split
// off).
//
// The returned head keeps at least one rune even when maxW is smaller than
// that rune's own width, guaranteeing forward progress: the caller's loop
// would otherwise retry the same unsplit item forever. head keeps the
// original item's own leading edge (SpaceBefore, and any padLead from an
// enclosing decorated inline ancestor's own left border/padding — see
// InlineItem.padLead); tail is a fresh continuation with no leading space of
// its own (SpaceBefore 0, matching glueRun's convention that zero means "no
// break opportunity before this item") and takes the original item's
// TRAILING edge (padTrail) instead, since the synthetic split point itself is
// not a real decorated-ancestor boundary and reserves no edge space at all.
// Both share the original item's decor chain, style, node and vertical
// metrics unchanged — only Text/Width/padLead/padTrail differ between them.
func (l *layouter) splitOverlongWord(items []*InlineItem, maxW float64) (head, tail *InlineItem, ok bool) {
	it := items[0]
	if it.Text == "" || it.Style == nil || !it.Style.BreaksOverlongWords() {
		return nil, nil, false
	}
	if j, _ := glueRun(items, 0); j != 1 {
		return nil, nil, false
	}
	runes := []rune(it.Text)
	if len(runes) < 2 {
		return nil, nil, false
	}
	st := it.Style
	measure := func(s string) float64 {
		return l.m.Measure(s, st.FontFamily, st.FontSize, st.FontWeight, st.Italic) + letterSpacingWidth(s, st)
	}
	n := len(runes)
	for n > 1 && measure(string(runes[:n])) > maxW {
		n--
	}
	headText, tailText := string(runes[:n]), string(runes[n:])
	head = &InlineItem{
		Text: headText, Style: st, Node: it.Node,
		Width: measure(headText), SpaceBefore: it.SpaceBefore,
		Ascent: it.Ascent, LineHeight: it.LineHeight, BaselineShift: it.BaselineShift,
		decor: it.decor, padLead: it.padLead,
	}
	tail = &InlineItem{
		Text: tailText, Style: st, Node: it.Node,
		Width: measure(tailText), SpaceBefore: 0,
		Ascent: it.Ascent, LineHeight: it.LineHeight, BaselineShift: it.BaselineShift,
		decor: it.decor, padTrail: it.padTrail,
	}
	return head, tail, true
}

// truncateLineWithEllipsis replaces line's items with a single text item
// ending in "…", trimmed to fit cw, when the line's total width overflows it
// — the real-world effect of `text-overflow:ellipsis` combined with
// `white-space:nowrap` and a clipping `overflow-x` (see
// css.Style.TextOverflowEllipsis's own doc comment for why `overflow:hidden`
// alone already clips the raw text with no code here at all: this only
// swaps that clip for a "…"-terminated one). Scoped to a line made entirely
// of plain text items — one holding an image, forced break or nested box
// (Text == "") is left untouched, plain overflow, the documented narrower
// scope. resolveInlineEdges re-derives the new single item's decor bookkeeping
// exactly as it would for any other one-item line, rather than hand-deriving
// it here.
func (l *layouter) truncateLineWithEllipsis(line *LineBox, st *css.Style, cw float64) {
	items := line.Items
	if len(items) == 0 {
		return
	}
	total := 0.0
	for i, it := range items {
		if it.Text == "" {
			return
		}
		if i > 0 {
			total += it.SpaceBefore
		}
		total += it.Width
	}
	if total <= cw {
		return
	}
	var b strings.Builder
	for i, it := range items {
		if i > 0 && it.SpaceBefore > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(it.Text)
	}
	runes := []rune(b.String())
	measure := func(s string) float64 {
		return l.m.Measure(s, st.FontFamily, st.FontSize, st.FontWeight, st.Italic)
	}
	for len(runes) > 0 && measure(string(runes)+"…") > cw {
		runes = runes[:len(runes)-1]
	}
	truncated := string(runes) + "…"
	first := items[0]
	line.Items = []*InlineItem{{
		Text:       truncated,
		Style:      first.Style,
		Node:       first.Node,
		Width:      measure(truncated),
		Ascent:     first.Ascent,
		LineHeight: first.LineHeight,
		decor:      first.decor,
	}}
	resolveInlineEdges(line.Items)
}

// placeSimpleLines positions a pre-wrapped sequence of lines that all share
// the SAME available width cw at a fixed cx (no floats narrowing any of
// them individually) — the shape both layoutInline's `nowrap` case and its
// `text-wrap:balance` fast path produce. Returns the cursor y just past the
// last line.
func placeSimpleLines(lines []*LineBox, cx, cw, y float64, st *css.Style, fbH, fbAsc float64) float64 {
	cursor := y
	for _, line := range lines {
		lineH, baseline, used := lineMetrics(line, fbH, fbAsc)
		placeLine(line, cx+alignOffset(st.TextAlign, cw, used), cursor, baseline)
		line.X, line.Y, line.W, line.H = cx, cursor, cw, lineH
		cursor += lineH
	}
	return cursor
}

// lineMetrics computes a line box's height, common baseline offset and used
// inline width. All items share one baseline at the tallest ascent; the line
// box must then be tall enough to hold BOTH the tallest ascent above the
// baseline AND the deepest descent below it. Taking these two maxima
// independently (rather than max(item.LineHeight)) is what keeps a line with
// mixed font sizes / line-heights from letting a tall inline's glyphs spill
// into the next line: each item spans [baseline-ascent, baseline+(lineHeight-
// ascent)], so the line height baseline+maxBelow bounds every item exactly and
// successive lines never overlap.
func lineMetrics(line *LineBox, fbH, fbAsc float64) (lineH, baseline, used float64) {
	if len(line.Items) == 0 {
		return fbH, fbAsc, 0
	}
	var maxBelow float64
	for i, it := range line.Items {
		// A shifted item (BaselineShift, see the field's own doc comment)
		// needs less room ABOVE the shared baseline and more room BELOW it
		// than its own Ascent/LineHeight alone would suggest, exactly the
		// amount it moved by — folding the shift in here keeps the line box
		// tall enough that placeLine's shifted position never spills into
		// the next line, the same guarantee this function already gives an
		// ordinary (unshifted) tall inline.
		if asc := it.Ascent - it.BaselineShift; asc > baseline {
			baseline = asc
		}
		if below := it.LineHeight - it.Ascent + it.BaselineShift; below > maxBelow {
			maxBelow = below
		}
		if i > 0 {
			used += it.SpaceBefore
		}
		used += it.padLead + it.Width + it.padTrail
	}
	return baseline + maxBelow, baseline, used
}

func alignOffset(a css.TextAlign, cw, used float64) float64 {
	switch a {
	case css.AlignCenter, css.AlignCenterBlocks:
		if cw > used {
			return (cw - used) / 2
		}
	case css.AlignRight:
		if cw > used {
			return cw - used
		}
	}
	return 0
}

// alignOffsetIn returns the absolute x where a line of width used starts within
// [left,right] for a given alignment.
func alignOffsetIn(a css.TextAlign, left, right, used float64) float64 {
	switch a {
	case css.AlignCenter, css.AlignCenterBlocks:
		if right-left > used {
			return left + (right-left-used)/2
		}
	case css.AlignRight:
		if right-left > used {
			return right - used
		}
	}
	return left
}
