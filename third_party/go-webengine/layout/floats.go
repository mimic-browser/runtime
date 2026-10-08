// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package layout

import (
	"math"

	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/dom"
)

// floatRect is a placed float's margin box in absolute page coordinates.
type floatRect struct {
	x, y, w, h float64
}

func (r floatRect) right() float64  { return r.x + r.w }
func (r floatRect) bottom() float64 { return r.y + r.h }

// floatCtx tracks placed left/right floats so that line boxes and later floats
// avoid them. It is a single context in absolute coordinates for the whole
// document, which reproduces the common "content flows beside a floated box"
// layout without per-BFC bookkeeping.
type floatCtx struct {
	lefts  []floatRect
	rights []floatRect
}

// overlaps reports whether a float vertically intersects the band [yTop,yBot).
func overlaps(r floatRect, yTop, yBot float64) bool {
	return r.y < yBot && r.bottom() > yTop
}

// available returns the left and right content edges available in the vertical
// band [yTop,yBot) within the region [regionLeft,regionRight], after subtracting
// any floats that intrude on that band.
func (f *floatCtx) available(yTop, yBot, regionLeft, regionRight float64) (left, right float64) {
	left, right = regionLeft, regionRight
	for _, r := range f.lefts {
		if overlaps(r, yTop, yBot) && r.right() > left {
			left = r.right()
		}
	}
	for _, r := range f.rights {
		if overlaps(r, yTop, yBot) && r.x < right {
			right = r.x
		}
	}
	if right < left {
		right = left
	}
	return left, right
}

// nextEdge returns the smallest float bottom strictly below y (the next point
// at which more horizontal space frees up), or y if no float extends past y.
func (f *floatCtx) nextEdge(y, regionLeft, regionRight float64) float64 {
	best := math.Inf(1)
	consider := func(rs []floatRect) {
		for _, r := range rs {
			if r.bottom() > y && r.bottom() < best {
				best = r.bottom()
			}
		}
	}
	consider(f.lefts)
	consider(f.rights)
	if math.IsInf(best, 1) {
		return y
	}
	return best
}

// clearY returns the y below which no relevant float remains, for a clear value.
func (f *floatCtx) clearY(c css.Clear, y float64) float64 {
	out := y
	bump := func(rs []floatRect) {
		for _, r := range rs {
			if r.bottom() > out {
				out = r.bottom()
			}
		}
	}
	if c == css.ClearLeft || c == css.ClearBoth {
		bump(f.lefts)
	}
	if c == css.ClearRight || c == css.ClearBoth {
		bump(f.rights)
	}
	return out
}

// bottom returns the lowest bottom edge of any float (0 when there are none).
func (f *floatCtx) bottom() float64 {
	var b float64
	for _, r := range append(append([]floatRect{}, f.lefts...), f.rights...) {
		if r.bottom() > b {
			b = r.bottom()
		}
	}
	return b
}

// add records a placed float on the given side.
func (f *floatCtx) add(side css.Float, r floatRect) {
	if side == css.FloatRight {
		f.rights = append(f.rights, r)
	} else {
		f.lefts = append(f.lefts, r)
	}
}

// findSlot returns the (x,y) top-left of a float margin box of size outerW×outerH
// on the given side, at or below yTop, within [regionLeft,regionRight], that does
// not overlap existing floats.
func (f *floatCtx) findSlot(side css.Float, yTop, outerW, outerH, regionLeft, regionRight float64) (x, y float64) {
	// Scan downward from yTop, stepping to each float's bottom edge, until a band
	// is wide enough for the box.
	best := yTop
	for {
		left, right := f.available(best, best+outerH, regionLeft, regionRight)
		// Accept the band once it is wide enough for the box. A band that is too
		// narrow — including one fully blocked by earlier floats (right-left <= 0)
		// — is NOT a valid slot: per CSS a float that does not fit drops to a lower
		// band rather than being placed at the blocked edge (which, for a
		// full-width left float behind another, is the container's right edge and
		// pushes the box off-screen). The nextEdge fallback below still terminates
		// the scan for a box wider than the region (no float can ever free enough
		// room, so it lands at the current left/right edge below all floats).
		if right-left >= outerW {
			if side == css.FloatRight {
				return right - outerW, best
			}
			return left, best
		}
		// Move to the next float edge below the current band.
		ny := f.nextEdge(best, regionLeft, regionRight)
		if ny <= best {
			if side == css.FloatRight {
				return right - outerW, best
			}
			return left, best
		}
		best = ny
	}
}

// placeFloat lays out a floated element out of normal flow, positions it against
// existing floats, records it in the float context and appends it to parent.
func (l *layouter) placeFloat(parent *Box, node *dom.Node, st *css.Style, cx, cw float64, b *bfc) {
	bw := st.Border.Widths()
	hExtra := bw.Left + bw.Right + st.Padding.Left + st.Padding.Right

	var fcw float64
	if st.Width.Auto {
		avail := cw - st.Margin.Left - st.Margin.Right - hExtra
		if avail < 0 {
			avail = 0
		}
		fcw = math.Min(l.preferredWidth(node, st), avail)
	} else {
		fcw = st.Width.Resolve(cw)
		if st.BoxSizing == css.BorderBox {
			fcw -= hExtra
		}
	}
	fcw = clampWidth(fcw, st, cw, hExtra)
	if fcw < 0 {
		fcw = 0
	}

	// Lay the float out in isolation (its own float context, discarded) at a
	// local origin, with a fixed content width and no outer margins.
	saved := l.floats
	l.floats = &floatCtx{}
	clone := *st
	clone.Width = css.Length{Px: fcw}
	clone.MinWidth = css.Length{Auto: true}
	clone.MaxWidth = css.Length{Auto: true}
	clone.BoxSizing = css.ContentBox
	clone.Float = css.FloatNone
	clone.Margin = css.Edges{}
	clone.MarginLeftAuto, clone.MarginRightAuto = false, false
	fb := &bfc{}
	box := l.place(node, &clone, 0, fcw+hExtra, fb)
	fb.commit()
	l.floats = saved

	outerW := box.W + st.Margin.Left + st.Margin.Right
	outerH := box.H + st.Margin.Top + st.Margin.Bottom

	yTop := b.y + math.Max(b.carry, 0)
	x, y := l.floats.findSlot(st.Float, yTop, outerW, outerH, cx, cx+cw)

	translateBox(box, (x+st.Margin.Left)-box.X, (y+st.Margin.Top)-box.Y)
	l.floats.add(st.Float, floatRect{x: x, y: y, w: outerW, h: outerH})
	box.Float = st.Float
	parent.Children = append(parent.Children, box)
}

// preferredWidth estimates the max-content width of a node's subtree: the widest
// unwrapped inline run, or the widest block child, plus horizontal edges.
func (l *layouter) preferredWidth(node *dom.Node, st *css.Style) float64 {
	bw := st.Border.Widths()
	edges := bw.Left + bw.Right + st.Padding.Left + st.Padding.Right
	// A replaced element (img or inline svg) is sized from its own decoded/
	// intrinsic size, never by measuring children — checked via isReplacedTag
	// (not a bare "img" tag check) because an inline <svg> commonly computes
	// display:block itself (Tailwind's own preflight reset:
	// `img,svg,video,...{display:block}`), which used to route it through
	// hasBlockLevelChild's "widest block child" branch below instead of this
	// one. That branch recurses into preferredWidth for the child with no
	// further img/svg special case of its own, falling through all the way to
	// the generic inline-measurement fallback — which measures the SVG's own
	// <path> children (no text) as ~0, not the SVG's real size. Confirmed live
	// on tailwindcss.com: the nav's logo `<a><svg viewBox="..." class="h-5">`
	// (no width/height HTML attributes, sized by CSS alone) measured as a
	// zero-width flex item, so the version-badge sibling that should have sat
	// to its right rendered overlapping it at the container's left edge.
	if node.Type == dom.Element && isReplacedTag(node.Tag) {
		w, _ := l.imageSize(node)
		return w + edges
	}
	// A form control (input/button/select/textarea) is an atomic box sized by
	// formControlSize, never by measuring its children — it is a void or
	// content-driven-differently element (an <input> has no children at all
	// for collectInline's "Inline: max-content" fallback below to measure),
	// the same reasoning appendElementInline already applies when it meets
	// one as ordinary inline content. Without this check, preferredWidth
	// reaches this node via a DIFFERENT path — recursing directly into an
	// element child (the flex-row-sum and hasBlockLevelChild-max branches
	// below both do this for any element, form control or not) — that never
	// goes through appendElementInline at all, so the control's real size
	// was never applied: a text <input> with no explicit width reported 0,
	// not its ~170px UA-default. Confirmed live on github.com's "Go to file"
	// search box: its `<span style="display:flex">` wrapper (itself
	// block-level, so hasBlockLevelChild's max-of-children branch recurses
	// into it directly) collapsed to near-zero width, shrinking a
	// flex-shrink:0 container that should have kept room for the input down
	// to almost nothing and crowding it against the adjacent "Code" button.
	if node.Type == dom.Element && isFormControlTag(node.Tag) {
		w, _ := l.formControlSize(node, st, 0)
		return w + edges
	}
	// A definite (non-auto, non-percentage) width is the box's max-content
	// contribution outright — CSS doesn't fall back to measuring children once
	// the author has fixed the width, even when those children carry no text of
	// their own (a bar-chart fill, a spacer div). Skipping this check made such
	// a box report a 0 preferred width, which collapsed its table column to 0
	// and let the next column's cells overlap it. This takes priority over the
	// flex-row sum below: a flex row with its own explicit width doesn't need
	// its children measured either.
	if !st.Width.Auto && !st.Width.IsPercent {
		if st.BoxSizing == css.BorderBox {
			return st.Width.Px
		}
		return st.Width.Px + edges
	}
	// A flex ROW lays its items side by side, so its max-content main size is the
	// SUM of the items' outer main sizes plus the inter-item gaps — not the max of
	// them (the block / flex-column case handled below). Without this a
	// content-sized flex row (itself a flex item, float or inline-block) is
	// under-sized to its widest child, and the inner row then has negative free
	// space that flex-shrink collapses the items with, overlapping them
	// horizontally (the GitHub repo-nav "Code / Issues / Pull requests / …"
	// jumble).
	// This only covers a flex row whose content is ALL elements, no bare
	// text: a flex container's own bare text (no wrapping element — e.g.
	// `<h1 style="display:flex">React</h1>`, confirmed live on react.dev's
	// hero heading; or bare text ALONGSIDE a real element child, e.g.
	// go.dev/blog's `<a style="display:inline-flex">Why Go
	// <i>arrow_drop_down</i></a>`) forms its own anonymous flex item per
	// spec, which this sum-of-element-children loop cannot see at all (it
	// skips every non-Element child) — with zero element children this left
	// n at 0 and returned a bare `0 + edges`, collapsing the element to zero
	// width; with a mix, it summed only the elements' widths, silently
	// dropping the text's own contribution and under-sizing the container.
	// hasDirectText routes BOTH cases through the same fallback: the
	// inline-measurement path below (the same one a plain, non-flex element
	// already uses) measures text and elements together correctly.
	//
	// Both this loop and the block-children one just below it skip an
	// OutOfFlow (absolute/fixed) child — it is positioned against its own
	// containing block, not sized into this element's normal-flow content,
	// so it must never inflate this element's own intrinsic width any more
	// than a DisplayNone child does. Confirmed live on github.com/golang/go's
	// own marketing nav: each top-level item is a `position:relative`
	// wrapper around a short visible trigger label PLUS its own
	// `position:absolute` dropdown MENU panel (far wider than the label
	// alone, holding every submenu entry's full text) — without this check,
	// "Platform"'s own reported preferred width was its trigger label's real
	// ~90px PLUS a large share of the hidden panel's own content, reporting
	// ~320px instead. Summed across five such items the flex row's total
	// request badly overshot the 1024px viewport, and the shrink pass that
	// then had to claw back the difference had so little slack left on the
	// row's own shortest item ("Pricing", a single word with no dropdown of
	// its own) that it squeezed it to near zero width — this fix alone cuts
	// the inflated request roughly in half, not all the way down to each
	// trigger's true visible size (a SEPARATE gap: the shrink phase's own
	// missing "automatic minimum size" floor, flagged as a precise, distinct
	// follow-up rather than bundled in here, matching this codebase's own
	// established precedent for two independent bugs gating one symptom).
	if node.Type == dom.Element && st.Display == css.DisplayFlex && st.FlexDirection == css.FlexRow && !l.hasDirectText(node) {
		var sum float64
		n := 0
		for _, c := range l.renderedChildren(node) {
			if c.Type != dom.Element {
				continue
			}
			cs := l.sm[c]
			if cs == nil || cs.Display == css.DisplayNone || cs.Position.OutOfFlow() {
				continue
			}
			sum += l.preferredWidth(c, cs) + cs.Margin.Left + cs.Margin.Right
			n++
		}
		if n > 0 {
			if n > 1 {
				sum += gapLen(st.ColumnGap) * float64(n-1)
			}
			return sum + edges
		}
	}
	// Skipped, like the flex-row-sum branch above, when node ALSO has direct
	// text: this loop only ever visits Element children, so bare text mixed
	// alongside a block-level child (e.g. an inline-flex `<a>Become a
	// sponsor<svg style="display:block">`, confirmed live on tailwindcss.com
	// — Tailwind's own preflight reset gives every inline `<svg>`
	// `display:block`, which is enough to make THIS element's own child
	// count as block-level) would have its width silently dropped, same as
	// the flex-row-sum branch's own documented failure mode. Falling through
	// to the inline-measurement fallback below measures text and elements
	// together correctly, exactly as round 47 already established for the
	// flex-row-sum case.
	if l.hasBlockLevelChild(node) && !l.hasDirectText(node) {
		var max float64
		for _, c := range l.renderedChildren(node) {
			if c.Type != dom.Element {
				continue
			}
			cs := l.sm[c]
			if cs == nil || cs.Display == css.DisplayNone || cs.Position.OutOfFlow() {
				continue
			}
			w := l.preferredWidth(c, cs) + cs.Margin.Left + cs.Margin.Right
			if w > max {
				max = w
			}
		}
		return max + edges
	}
	// Inline: max-content is the WIDEST line, where a promoted block (see
	// InlineItem.BlockBreak) both ends the current line — CSS 2.1 §9.2.1.1
	// promotes it to a real sibling, so it never shares a line with
	// surrounding text — AND contributes its own preferred width as a
	// candidate line in its own right. A single running sum that simply
	// skipped every BlockBreak (this function's previous shape) was correct
	// only when a promoted block was a minor part of otherwise-substantial
	// surrounding text; when it is the ONLY content — e.g. an unstyled
	// `<span>` wrapping a single `display:flex` `<button>`, confirmed live on
	// developer.mozilla.org's header controls (the button computes
	// block-level via `display:flex`, so it promotes exactly like any other
	// block-level child found under an inline ancestor) — skipping it left
	// nothing else to sum, reporting a 0 max-content width and collapsing a
	// flex item that should have kept room for the button down to nothing.
	// A FLOATED promoted block is the one exception that stays on the
	// current line instead of starting a new one (see the BlockBreak
	// doc comment and round 40's fix): a float does not break flow the way a
	// genuine block does, so it keeps consuming space alongside its siblings.
	// 0: this is an intrinsic-width computation (max-content), not real
	// layout — no containing width exists yet for a form control's own
	// percentage to resolve against (see collectInline's own doc comment).
	items := l.collectInline(node, st, st.WhiteSpace == css.WSPre, 0)
	var line, maxLine float64
	flushLine := func() {
		if line > maxLine {
			maxLine = line
		}
		line = 0
	}
	for i, it := range items {
		if it.LineBreak {
			// A <br> forced break is deliberately NOT a line boundary for
			// this estimate (unlike a promoted block below) — matching this
			// function's pre-existing, unchanged convention that max-content
			// sums straight through a <br>, only BlockBreak starts a fresh
			// candidate line.
			continue
		}
		if it.BlockBreak != nil {
			// it.Style is always set alongside BlockBreak (see the promotion
			// site in appendElementInline), so no nil check is needed here.
			if it.Style.Float != css.FloatNone {
				line += l.preferredWidth(it.BlockBreak, it.Style) + it.Style.Margin.Left + it.Style.Margin.Right
				continue
			}
			flushLine()
			blockW := l.preferredWidth(it.BlockBreak, it.Style) + it.Style.Margin.Left + it.Style.Margin.Right
			if blockW > maxLine {
				maxLine = blockW
			}
			continue
		}
		if i > 0 {
			line += it.SpaceBefore
		}
		line += it.padLead + it.Width + it.padTrail
	}
	flushLine()
	return maxLine + edges
}

// translateBox shifts a box subtree (and its lines/items) by (dx,dy).
func translateBox(box *Box, dx, dy float64) {
	if box == nil || (dx == 0 && dy == 0) {
		return
	}
	box.X += dx
	box.Y += dy
	box.ContentX += dx
	box.ContentY += dy
	// A list-item's marker is computed once, in attachMarker, from the box's
	// ContentX/first-line Y at the time layout ran — absolute coordinates, like
	// everything else in this package. Any box translated AFTER that (a flex
	// row/column item repositioned from its temporary local-origin layout,
	// flex.go:251/359; a grid item, grid.go; a table cell, table.go; a float,
	// placeFloat above) left its marker's X/Y stuck at the PRE-translation
	// position — observed live on caniuse.com: a `display:flex` 3-column
	// section's numbered list (`<ol style="list-style:decimal">`, a flex
	// child) had every marker's absolute X computed against its local,
	// near-zero pre-flex-placement ContentX, so after the real ~370px flex
	// shift the "1."/"2."/… markers rendered off in the gutter of column one
	// instead of beside their own list items — invisible in this specific
	// page's cramped indent, but wrong regardless of what happens to sit
	// there.
	if box.Marker != nil {
		box.Marker.X += dx
		box.Marker.Y += dy
	}
	for _, line := range box.Lines {
		line.X += dx
		line.Y += dy
		// Inline fragments are absolute document geometry like everything else
		// here (see InlineFragment), so a translated box must carry them along
		// or an inline element's background/border stays behind at the
		// pre-translation position while its own words move.
		for i := range line.Inlines {
			line.Inlines[i].X += dx
			line.Inlines[i].Y += dy
		}
		for _, it := range line.Items {
			it.X += dx
			it.Y += dy
			// An inline-flex item's NestedBox (see InlineItem's own doc
			// comment) is a SECOND, independent box tree hanging off an
			// InlineItem rather than living in box.Children — the exact same
			// blind spot the marker fix above already found and fixed for a
			// DIFFERENT kind of attached data. it.X/it.Y (just updated above)
			// are scalars the rest of this package treats as the item's
			// authoritative position; NestedBox's own internally-stored
			// coordinates need the identical shift to stay in sync, or its
			// painted content is left stuck at wherever it was BEFORE an
			// outer flex/grid ancestor's later re-translation moved the
			// InlineItem itself — confirmed live on pkg.go.dev: an
			// inline-flex breadcrumb `<li>` nested inside a flex-positioned
			// header section painted at its pre-translation position,
			// overlapping the page's own top navigation bar.
			if it.NestedBox != nil {
				translateBox(it.NestedBox, dx, dy)
			}
		}
	}
	for _, ch := range box.Children {
		translateBox(ch, dx, dy)
	}
}
