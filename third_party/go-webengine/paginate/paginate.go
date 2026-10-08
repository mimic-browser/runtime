// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

// Package paginate cuts a laid-out document into pages: it returns the
// document y at which each page after the first starts, honouring CSS
// Fragmentation Level 3 as far as a page medium needs it — forced breaks
// (break-before/after: page, left, right and the page-break-* aliases),
// break-inside: avoid, break-before/after: avoid (keep with the previous or
// the next content), orphans and widows — over the atoms a page can cut
// between.
//
// # Atoms
//
// A page can only be cut between atoms: a text line, a whole table row
// (never split mid-row — unless the row is a layout-table wrapper whose
// cell holds a nested table, in which case its real rows are the atoms), or
// a leaf box with height and no lines (a rule, a spacer). This is the rule
// the PDF exporter go-pdfkit/html2pdf paginated with before the CSS
// properties were read at all, and it is what keeps a line from being
// sliced across two pages.
//
// # Constraints, and the order they yield in
//
// A forced break is always taken, even on a page that is nearly empty.
// Between forced breaks a page is filled as far as it goes, and the cut is
// the last boundary that respects, in the spec's order of precedence: the
// break-before/after: avoid of the atoms on either side and every
// break-inside: avoid box that could fit on a page (rules 1, 2 and 4), then
// orphans and widows (rule 3). When no boundary respects everything, the
// orphans/widows rule is dropped first, then the avoid rules, exactly as
// css-break-3 §5.4 prescribes; a box taller than a page is never kept
// together, since it cannot be. An atom taller than a page overflows its
// own page's bottom rather than blocking pagination.
//
// Margins adjoining an unforced break are not truncated (css-break-3 §5.5):
// the atoms' positions are the layout's, and a block's top margin before a
// cut reappears as a blank at the next page's top. A consumer that paints
// pages by shifting the document accepts that today.
package paginate

import (
	"sort"

	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/dom"
	"github.com/go-webengine/engine/layout"
)

// Options tunes Paginate.
type Options struct {
	// PageHeight is the usable height of a page, in the layout's CSS px —
	// the page area, margins already taken out.
	PageHeight float64
	// FirstPageHeight, when non-zero, is the first page's usable height (an
	// @page :first with different margins); every later page uses PageHeight.
	FirstPageHeight float64
}

// Breaks returns the document y at which each page after the first starts,
// for pages of usable height pageH — Paginate with only PageHeight set.
func Breaks(root *layout.Box, pageH float64) []float64 {
	return Paginate(root, Options{PageHeight: pageH})
}

// Paginate returns the document y at which each page after the first
// starts. A nil root or a non-positive page height gives no breaks.
func Paginate(root *layout.Box, o Options) []float64 {
	if root == nil || o.PageHeight <= 0 {
		return nil
	}
	atoms, groups := collect(root)
	if len(atoms) == 0 {
		return nil
	}
	blocked := blockedBoundaries(atoms, groups, o.PageHeight)
	var tops []float64
	pageTop := 0.0
	pageH := o.PageHeight
	if o.FirstPageHeight > 0 {
		pageH = o.FirstPageHeight
	}
	i := 0 // first atom of the current page
	for {
		// The page's last fitting atom, stopping short of a forced break.
		j := i - 1
		forcedAt := -1
		for k := i; k < len(atoms); k++ {
			if k > i && atoms[k].forcedBefore {
				forcedAt = k
				break
			}
			if atoms[k].bottom-pageTop > pageH+epsilon {
				break
			}
			j = k
		}
		var cut int
		switch {
		case forcedAt >= 0 && forcedAt <= j+1:
			cut = forcedAt // everything before the forced break fits: the page is simply short
		case j+1 >= len(atoms) && forcedAt < 0:
			return tops // the rest fits on this page
		case j < i:
			cut = i + 1 // the first atom alone overflows: let it, and cut after it
		default:
			cut = choose(atoms, blocked, i, j+1)
		}
		if cut >= len(atoms) {
			return tops
		}
		tops = append(tops, atoms[cut].top)
		pageTop = atoms[cut].top
		pageH = o.PageHeight
		i = cut
	}
}

// epsilon absorbs the layout's floating-point residue at a page edge.
const epsilon = 1e-6

// atom is one indivisible slice of content.
type atom struct {
	top, bottom float64
	// block is the block container whose line this atom is, with the line's
	// position among its lines — what orphans and widows are counted on; nil
	// for a row or a leaf.
	block              *layout.Box
	lineIdx, lineCount int
	forcedBefore       bool // a forced break falls before this atom
	avoidBefore        bool // break-before: avoid on a box starting with this atom
	avoidAfter         bool // break-after: avoid on a box ending with this atom
}

// group is a break-inside: avoid box, as the document span it covers: a
// boundary strictly inside it is one the box asks not to be cut at.
type group struct{ top, bottom float64 }

// collect walks the box tree and returns the atoms in document order (by
// top) and the avoid groups.
func collect(root *layout.Box) ([]*atom, []group) {
	var atoms []*atom
	var groups []group
	pendingForced := false // a forced break is owed before the next atom
	pendingAvoidBefore := false
	var walk func(b *layout.Box)
	walk = func(b *layout.Box) {
		if b == nil {
			return
		}
		st := b.Style
		if st != nil {
			if st.BreakBefore == css.BreakPage || st.BreakBefore == css.BreakLeft || st.BreakBefore == css.BreakRight {
				pendingForced = true
			}
			if st.BreakBefore == css.BreakAvoid {
				pendingAvoidBefore = true
			}
			if st.BreakInside == css.BreakInsideAvoid && b.H > 0 {
				groups = append(groups, group{b.Y, b.Y + b.H})
			}
		}
		first := len(atoms)
		add := func(a *atom) {
			a.forcedBefore = a.forcedBefore || pendingForced
			a.avoidBefore = a.avoidBefore || pendingAvoidBefore
			pendingForced, pendingAvoidBefore = false, false
			atoms = append(atoms, a)
		}
		switch {
		case isRow(b) && !hasDescendantTr(b):
			add(&atom{top: b.Y, bottom: b.Y + b.H})
		default:
			for i, ln := range b.Lines {
				add(&atom{top: ln.Y, bottom: ln.Y + ln.H, block: b, lineIdx: i, lineCount: len(b.Lines)})
			}
			if len(b.Children) == 0 && len(b.Lines) == 0 && b.H > 0 {
				add(&atom{top: b.Y, bottom: b.Y + b.H})
			}
			for _, c := range b.Children {
				walk(c)
			}
		}
		if st != nil && len(atoms) > first {
			last := atoms[len(atoms)-1]
			switch st.BreakAfter {
			case css.BreakPage, css.BreakLeft, css.BreakRight:
				pendingForced = true
			case css.BreakAvoid:
				last.avoidAfter = true
			}
		}
	}
	walk(root)
	sort.SliceStable(atoms, func(i, j int) bool { return atoms[i].top < atoms[j].top })
	return atoms, groups
}

// blockedBoundaries marks, once for the document, every boundary b (a cut
// before atoms[b]) that falls strictly inside a break-inside: avoid box
// small enough to be kept on a page. One sweep over the groups with a
// binary search each, so a table of ten thousand rows each asking not to
// be cut costs a log factor rather than a scan per boundary per page — the
// per-boundary scan made a Wikipedia table page take thirteen seconds.
func blockedBoundaries(atoms []*atom, groups []group, pageH float64) []bool {
	blocked := make([]bool, len(atoms)+1)
	diff := make([]int, len(atoms)+2)
	for _, g := range groups {
		if g.bottom-g.top > pageH+epsilon {
			continue // cannot be kept whole: its boundaries are fair game
		}
		// atoms whose top is strictly inside (g.top, g.bottom)
		lo := sort.Search(len(atoms), func(i int) bool { return atoms[i].top > g.top+epsilon })
		hi := sort.Search(len(atoms), func(i int) bool { return atoms[i].top >= g.bottom-epsilon })
		if lo < hi {
			diff[lo]++
			diff[hi]--
		}
	}
	depth := 0
	for b := range blocked {
		if b < len(diff) {
			depth += diff[b]
		}
		blocked[b] = depth > 0
	}
	return blocked
}

// choose picks the boundary to cut at on a page holding atoms[i:max]
// (max is the first atom that does not fit, always a real atom here): the
// largest b in (i, max] allowed by the constraints; when none is, the
// orphans/widows rule is dropped and the search repeated; when still none
// is, the avoid rules are dropped too and the page is simply filled — the
// spec's own order of relaxation.
func choose(atoms []*atom, blocked []bool, i, max int) int {
	for tier := 0; tier < 2; tier++ {
		for b := max; b > i; b-- {
			if allowed(atoms, blocked, b, tier) {
				return b
			}
		}
	}
	return max
}

// allowed reports whether cutting before atoms[b] respects the constraints
// still in force at tier: 0 all of them, 1 without orphans/widows.
func allowed(atoms []*atom, blocked []bool, b int, tier int) bool {
	prev, next := atoms[b-1], atoms[b]
	if prev.avoidAfter || next.avoidBefore || blocked[b] {
		return false
	}
	if tier < 1 && prev.block != nil && prev.block == next.block {
		st := prev.block.Style
		orphans, widows := 2, 2
		if st != nil {
			if st.Orphans > 0 {
				orphans = st.Orphans
			}
			if st.Widows > 0 {
				widows = st.Widows
			}
		}
		if prev.lineIdx+1 < orphans || next.lineCount-next.lineIdx < widows {
			return false
		}
	}
	return true
}

// isRow reports whether b is a <tr> box.
func isRow(b *layout.Box) bool {
	return b.Node != nil && b.Node.Type == dom.Element && b.Node.Tag == "tr"
}

// hasDescendantTr reports whether b's subtree contains another <tr> — the
// signature of a layout-table trick (a row whose cell holds a nested table)
// rather than a plain data row.
func hasDescendantTr(b *layout.Box) bool {
	for _, c := range b.Children {
		if isRow(c) || hasDescendantTr(c) {
			return true
		}
	}
	return false
}
