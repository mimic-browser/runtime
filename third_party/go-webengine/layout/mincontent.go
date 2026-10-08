// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package layout

import (
	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/dom"
)

// minContentWidth estimates the min-content width of a node's subtree: the
// widest unit that cannot be broken across lines — a single word, an image,
// a form control, a box with a definite width — plus the node's own
// horizontal edges. It is preferredWidth's counterpart (CSS 2.1 §17.5.2.2
// calls the pair a cell's "minimum" and "maximum" width) and mirrors its
// branch structure step for step, so the two always measure a node the same
// way and min-content never exceeds max-content.
//
// Its one consumer today is table layout, which needs a floor under each
// column: a column scaled below its longest word cannot lay that word out,
// and the word runs into the next column instead — observed on an
// operating-cost table whose header "Puissance IT" printed as
// "PuissanceIT seul" over the neighbouring column.
func (l *layouter) minContentWidth(node *dom.Node, st *css.Style) float64 {
	bw := st.Border.Widths()
	edges := bw.Left + bw.Right + st.Padding.Left + st.Padding.Right
	if node.Type == dom.Element && isReplacedTag(node.Tag) {
		w, _ := l.imageSize(node)
		return w + edges
	}
	if node.Type == dom.Element && isFormControlTag(node.Tag) {
		w, _ := l.formControlSize(node, st, 0)
		return w + edges
	}
	// A definite width is the contribution outright, exactly as in
	// preferredWidth: the author fixed the box, its content no longer sizes it.
	if !st.Width.Auto && !st.Width.IsPercent {
		if st.BoxSizing == css.BorderBox {
			return st.Width.Px
		}
		return st.Width.Px + edges
	}
	// Content that never wraps (white-space: pre / nowrap) has no unit
	// smaller than its longest line: min-content equals max-content.
	if st.WhiteSpace != css.WSNormal {
		return l.preferredWidth(node, st)
	}
	// A single-line flex row keeps its items side by side, so its min-content
	// is the SUM of theirs plus the gaps (css-flexbox-1 §9.9.1); a wrapping
	// row can put each item on its own line, so the widest item is enough.
	// Same guard as preferredWidth: bare text alongside the items falls
	// through to the inline measurement below, which sees both.
	if node.Type == dom.Element && st.Display == css.DisplayFlex && st.FlexDirection == css.FlexRow && !l.hasDirectText(node) {
		var sum, max float64
		n := 0
		for _, c := range l.renderedChildren(node) {
			if c.Type != dom.Element {
				continue
			}
			cs := l.sm[c]
			if cs == nil || cs.Display == css.DisplayNone || cs.Position.OutOfFlow() {
				continue
			}
			w := l.minContentWidth(c, cs) + cs.Margin.Left + cs.Margin.Right
			sum += w
			if w > max {
				max = w
			}
			n++
		}
		if n > 0 {
			if st.FlexWrap != css.FlexNoWrap {
				return max + edges
			}
			if n > 1 {
				sum += gapLen(st.ColumnGap) * float64(n-1)
			}
			return sum + edges
		}
	}
	// Block children stack, so the widest one sets the minimum.
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
			w := l.minContentWidth(c, cs) + cs.Margin.Left + cs.Margin.Right
			if w > max {
				max = w
			}
		}
		return max + edges
	}
	// Inline content: every item is a break opportunity from the next, so the
	// widest single item is the minimum — EXCEPT where an item's SpaceBefore
	// is 0 (appendWords/appendElementInline leave it there precisely when no
	// collapsible whitespace and no margin gap preceded it), which means
	// there is no CSS Text line-breaking opportunity at that boundary at
	// all: it is glued to the item before it into one unbreakable run, and
	// the minimum must count the whole run, not each piece separately.
	// Confirmed live: a table cell's own footnote marker `152,3<sup>†</sup>`
	// (engine#149) needs the width of "152,3†" together as its floor — the
	// SAME zero-SpaceBefore signal wrapOneLine/WrapItems now honour (see
	// glueRun in linebreak.go) for the actual line-breaking decision this
	// value exists to protect against ever being violated.
	// 0: an intrinsic-width computation (min-content), not real layout — see
	// collectInline's own doc comment on why a form control's percentage
	// width has no containing block to resolve against here.
	items := l.collectInline(node, st, false, 0)
	var widest, run float64
	hasRun := false
	flush := func() {
		if run > widest {
			widest = run
		}
		run, hasRun = 0, false
	}
	for _, it := range items {
		if it.LineBreak {
			flush()
			continue
		}
		if it.BlockBreak != nil {
			flush()
			w := l.minContentWidth(it.BlockBreak, it.Style) + it.Style.Margin.Left + it.Style.Margin.Right
			if w > widest {
				widest = w
			}
			continue
		}
		w := it.padLead + it.Width + it.padTrail
		if hasRun && it.SpaceBefore == 0 {
			run += w
		} else {
			flush()
			run, hasRun = w, true
		}
	}
	flush()
	return widest + edges
}
