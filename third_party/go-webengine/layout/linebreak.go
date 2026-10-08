// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package layout

// glueRun returns the extent [i,j) of the maximal run of items starting at i
// that are joined with NO CSS Text line-breaking opportunity between them —
// item i itself, plus every immediately-following item whose SpaceBefore is
// exactly 0. appendWords/appendElementInline leave SpaceBefore at 0 precisely
// when an item was appended with no collapsible whitespace and no margin gap
// before it, so a zero there means the boundary is an InlineItem seam, not a
// real break opportunity — e.g. `152,3<sup>†</sup>` (a table cell's own
// footnote marker, engine#149): "152,3" and "†" are two separate InlineItems
// but ONE unbreakable run, and must be measured and placed as a single unit,
// never split at that seam as an ordinary wrap point. runW is the combined
// width the run occupies once it's the LEADING content on a line (i's own
// SpaceBefore, which is what a real preceding break would have consumed, is
// deliberately excluded — the caller adds it separately when there is
// preceding content on the line, exactly as a single item's own width would
// be).
func glueRun(items []*InlineItem, i int) (j int, runW float64) {
	j = i + 1
	runW = items[i].padLead + items[i].Width + items[i].padTrail
	for j < len(items) && !items[j].LineBreak && items[j].SpaceBefore == 0 {
		runW += items[j].padLead + items[j].Width + items[j].padTrail
		j++
	}
	return j, runW
}

// WrapItems greedily breaks a sequence of inline items into lines so that each
// line's used width does not exceed maxW, inserting a per-item SpaceBefore
// between adjacent items on the same line. A single item — or an unbreakable
// glued run (see glueRun) — wider than maxW is placed alone on its own line
// (overflow), never split mid-run. A LineBreak item ends the current line (and
// is not itself placed). This is the pure, exactly-testable core of inline
// layout; positioning and heights are applied by the caller.
func WrapItems(items []*InlineItem, maxW float64) []*LineBox {
	if len(items) == 0 {
		return nil
	}
	var lines []*LineBox
	cur := &LineBox{}
	curW := 0.0
	i := 0
	for i < len(items) {
		it := items[i]
		if it.LineBreak {
			lines = append(lines, cur)
			cur = &LineBox{}
			curW = 0
			i++
			continue
		}
		j, runW := glueRun(items, i)
		add := runW
		if len(cur.Items) > 0 {
			add += it.SpaceBefore
		}
		if len(cur.Items) > 0 && curW+add > maxW {
			lines = append(lines, cur)
			cur = &LineBox{}
			curW = 0
			add = runW
		}
		cur.Items = append(cur.Items, items[i:j]...)
		curW += add
		i = j
	}
	// The final `cur` is appended unconditionally UNLESS it is both empty and
	// there is already at least one earlier line: that shape only arises when
	// the very last item was a LineBreak with nothing after it, which must
	// NOT open a further, visibly-empty line of its own (round 97 — see
	// layoutInline's own identical fix and its doc comment for the confirmed
	// live case). An EARLIER break in the same trailing run already got its
	// own real empty line on ITS OWN loop iteration above; only the break
	// that is genuinely the last thing in the whole run is suppressed here.
	// A lone leading break (lines still empty) is unaffected: cur is still
	// appended, giving the one real empty line a bare "<br>" alone renders.
	if len(cur.Items) > 0 || len(lines) == 0 {
		lines = append(lines, cur)
	}
	return lines
}

// balanceWidth finds the narrowest width in [0,cw] at which WrapItems(items,
// ·) still produces exactly n lines — the width `text-wrap:balance` should
// actually wrap at (see TextWrapBalance's own doc comment for why this
// approximates the spec's UA-defined algorithm: since WrapItems's own line
// count is monotonically non-increasing as its width grows, for any width in
// [balanceWidth,cw] the count is already exactly n — cw itself witnesses
// that — and narrower than balanceWidth it's provably more than n, so this
// is the widest content-column that still forces every line's own leftover
// space to shrink as much as possible without adding a line). n is assumed
// to already be len(WrapItems(items,cw)) — the caller's own already-computed
// baseline — so this never needs to invoke WrapItems at cw itself.
func balanceWidth(items []*InlineItem, cw float64, n int) float64 {
	lo, hi := 0.0, cw
	for i := 0; i < 30 && hi-lo > 0.25; i++ {
		mid := (lo + hi) / 2
		if len(WrapItems(items, mid)) <= n {
			hi = mid
		} else {
			lo = mid
		}
	}
	return hi
}
