// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package layout

import (
	"math"
	"strconv"
	"strings"

	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/dom"
)

// tableRow is one row and its cells (both with resolved styles), plus each
// cell's starting column index and column span (colspan="N", default 1).
// These can diverge from a cell's raw position in cells: a row mixing a
// spanning cell with plain ones — e.g. news.ycombinator.com's own real
// markup, a title row of 3 plain cells followed by a subtext row of
// `<td colspan=2></td><td class=subtext>...</td>` — must still align its
// non-spanning cells with the SAME columns every other row uses.
type tableRow struct {
	node     *dom.Node
	style    *css.Style
	cells    []*dom.Node
	colStart []int
	colSpan  []int
}

// cellColSpan returns a table cell's colspan attribute, defaulting to 1 for a
// missing, non-numeric, or non-positive value (the HTML spec's own "invalid
// value default" for this attribute).
func cellColSpan(cell *dom.Node) int {
	v, ok := cell.Attribute("colspan")
	if !ok {
		return 1
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 1 {
		return 1
	}
	return n
}

// column is one table column's width constraints, gathered from its
// single-span cells (CSS 2.1 §17.5.2.2).
type column struct {
	// min is the widest unbreakable unit in the column, max the widest line;
	// a cell's declared definite width counts toward both, so a fixed column
	// has min == max unless a word is longer than the declaration.
	min, max float64
	// pct is the largest percentage width declared on a cell, 0..1 (0: none).
	pct float64
	// fixed marks a column some cell sized with a definite width: it is
	// handed exactly that width while the table can afford it, and only
	// shares in surplus space when no auto column is left to take it.
	fixed bool
}

// table lays out a table box with the automatic table layout of CSS 2.1
// §17.5.2.2: each column collects the minimum and maximum widths of its
// cells (see column), and the table's content width is distributed so that
// no column is ever narrower than its minimum — a column that cannot hold
// its longest word would let that word run into the next column. Surplus
// beyond every maximum goes to the auto columns in proportion to their
// maximums, which is also what the earlier "scale the max-content widths to
// fill the table" did whenever the table was wide enough for all of them, so
// that case is unchanged. Each row's height is the tallest cell. Returns the
// content bottom y.
//
// A display:table box with no real table-row/table-cell descendant at all —
// collectRows returns none — falls back to plain block-in-flow layout of its
// children (blockOrInlineContents) rather than this function dropping the
// whole box (returning top unchanged, as if it had no content whatsoever).
// CSS's own anonymous-table-object generation would wrap such children in
// implicit rows/cells instead; this engine doesn't implement that synthesis,
// but MediaWiki's own thumbnail-figure markup relies on exactly this gap
// being handled somehow: `figure[typeof~='mw:File/Thumb']{display:table}`
// wraps a plain `display:block` `.mw-file-description` anchor (the image) and
// a `figcaption` (itself a `display:table-caption` this engine doesn't
// recognise either, so it stays the block its UA default already set it to)
// — NEITHER child is ever a table-row or table-cell. Before this fallback,
// the whole figure (image AND caption) rendered as empty space: confirmed
// live on en.wikipedia.org's "Go (programming language)" article, whose
// "Branding and styling" section lost its gopher-mascot drawing entirely.
func (l *layouter) table(box *Box, node *dom.Node, st *css.Style, cx, cw, top float64, b *bfc) float64 {
	rows := l.collectRows(node)
	if len(rows) == 0 {
		return l.blockOrInlineContents(box, node, st, cx, cw, top, b)
	}
	ncols := 0
	for _, r := range rows {
		for j := range r.cells {
			if end := r.colStart[j] + r.colSpan[j]; end > ncols {
				ncols = end
			}
		}
	}
	if ncols == 0 {
		return top
	}

	// Width constraints per column. A colspan>1 cell is excluded —
	// distributing its content need across the columns it spans is the full
	// spec algorithm's job, past this table layout's scope — but it still
	// OCCUPIES those columns (see the row-layout loop below), so it does not
	// shift a later plain cell in the same row out of alignment with the
	// other rows' columns.
	cols := make([]column, ncols)
	for _, r := range rows {
		for j, cell := range r.cells {
			if r.colSpan[j] != 1 {
				continue
			}
			cs := l.cellStyle(cell)
			m := cs.Margin.Left + cs.Margin.Right
			col := &cols[r.colStart[j]]
			max := l.preferredWidth(cell, cs) + m
			// The cell's own declared width is a floor under both widths, not
			// a ceiling on the minimum: "if the specified width of the cell
			// is greater than the minimum content width, it becomes the
			// minimum cell width" (§17.5.2.2) — a word longer than the
			// declaration still wins, so it is measured with the width
			// treated as auto and the declaration applied afterwards.
			content := *cs
			content.Width = css.Length{Auto: true}
			min := l.minContentWidth(cell, &content) + m
			switch {
			case cs.Width.IsPercent:
				if cs.Width.Percent > col.pct {
					col.pct = cs.Width.Percent
				}
			case !cs.Width.Auto:
				col.fixed = true
				if max > min {
					min = max
				}
			}
			if max < min {
				max = min
			}
			if min > col.min {
				col.min = min
			}
			if max > col.max {
				col.max = max
			}
		}
	}
	// border-spacing (see css.Style.BorderSpacingH/V's own doc comment for
	// scope): a gap of BorderSpacingH BEFORE the first column, BETWEEN every
	// pair of columns, and AFTER the last — ncols+1 gaps total, taken out of
	// the width columns are distributed over, matching the separate-borders
	// model's own box accounting. BorderSpacingV works identically down the
	// rows. Zero for a table that never sets border-spacing, so this is a
	// no-op reproducing the exact previous flush-column/row layout.
	hs, vs := st.BorderSpacingH, st.BorderSpacingV
	avail := cw - hs*float64(ncols+1)
	if avail < 0 {
		avail = 0
	}
	colW := distributeColumns(cols, avail)
	colX := make([]float64, ncols)
	acc := cx + hs
	for j := range colW {
		colX[j] = acc
		acc += colW[j] + hs
	}

	y := top + vs
	for _, r := range rows {
		rowBox := &Box{Node: r.node, Style: r.style, X: cx, Y: y, W: cw, ContentX: cx, ContentY: y, ContentW: cw}
		var cellBoxes []*Box
		var rowH float64
		for j, cell := range r.cells {
			cs := l.cellStyle(cell)
			bw := cs.Border.Widths()
			hEdges := bw.Left + bw.Right + cs.Padding.Left + cs.Padding.Right
			var spanW float64
			for k := 0; k < r.colSpan[j]; k++ {
				spanW += colW[r.colStart[j]+k]
			}
			if r.colSpan[j] > 1 {
				// The span's own internal gaps belong to it, same as a
				// plain cell's single column already has none to add.
				spanW += hs * float64(r.colSpan[j]-1)
			}
			contentW := spanW - hEdges - cs.Margin.Left - cs.Margin.Right
			cbox := l.layoutIsolated(cell, cs, contentW)
			cellBoxes = append(cellBoxes, cbox)
			if h := cbox.H + cs.Margin.Top + cs.Margin.Bottom; h > rowH {
				rowH = h
			}
		}
		// A row's own CSS height is a minimum (CSS 2.1 §17.5.3), not just its
		// cells' content. Confirmed live: news.ycombinator.com's spacer rows are
		// `<tr class="spacer" style="height:5px">`, which laid out at 0px.
		if rs := r.style; rs != nil && !rs.Height.Auto && !rs.Height.IsPercent && rs.Height.Px > rowH {
			rowH = rs.Height.Px
		}
		for j, cbox := range cellBoxes {
			cs := l.cellStyle(r.cells[j])
			naturalH := cbox.H
			translateBox(cbox, (colX[r.colStart[j]]+cs.Margin.Left)-cbox.X, (y+cs.Margin.Top)-cbox.Y)
			// vertical-align:middle/bottom shifts the cell's own CONTENT down
			// within the row-height slot — the box's own outer rect (its
			// background/border, painted from cbox.X/Y/W/H directly) still
			// spans the FULL row height regardless, matching a real
			// browser's own table box model, where vertical-align never
			// affects a cell's own background fill, only where its content
			// sits inside it. Only top/middle/bottom move anything; every
			// other keyword (baseline and its own row-wide first-line
			// alignment, text-top/text-bottom/sub/super) has no confirmed
			// real trigger on a table cell and keeps the pre-existing
			// top-aligned behaviour, same as this field's own general
			// doc comment already discloses for non-table contexts.
			if slack := rowH - cs.Margin.Top - cs.Margin.Bottom - naturalH; slack > 0 {
				var offset float64
				switch cs.VerticalAlign {
				case css.VAlignMiddle:
					offset = slack / 2
				case css.VAlignBottom:
					offset = slack
				}
				if offset > 0 {
					translateBox(cbox, 0, offset)
					cbox.Y -= offset
					cbox.ContentY -= offset
				}
			}
			// Stretch the cell to the row height (content stays aligned per
			// the offset just applied above, top by default).
			cbox.H = rowH - cs.Margin.Top - cs.Margin.Bottom
			rowBox.Children = append(rowBox.Children, cbox)
		}
		rowBox.H = rowH
		rowBox.ContentH = rowH
		box.Children = append(box.Children, rowBox)
		y += rowH + vs
	}
	return y
}

// collectRows gathers the table's rows, descending into row groups.
func (l *layouter) collectRows(node *dom.Node) []tableRow {
	var rows []tableRow
	for _, c := range l.renderedChildren(node) {
		if c.Type != dom.Element {
			continue
		}
		cs := l.sm[c]
		if cs == nil || cs.Display == css.DisplayNone {
			continue
		}
		switch cs.Display {
		case css.DisplayTableRow:
			rows = append(rows, l.makeRow(c, cs))
		case css.DisplayTableRowGroup:
			for _, r := range l.renderedChildren(c) {
				if r.Type != dom.Element {
					continue
				}
				rs := l.sm[r]
				if rs != nil && rs.Display == css.DisplayTableRow {
					rows = append(rows, l.makeRow(r, rs))
				}
			}
		}
	}
	return rows
}

// makeRow collects the table-cell children of a row, tracking each one's
// starting column index by advancing a running cursor by the PRECEDING
// cells' own colspan — not by cell position — so a colspan>1 cell shifts
// every cell after it into the correct later column.
func (l *layouter) makeRow(node *dom.Node, st *css.Style) tableRow {
	row := tableRow{node: node, style: st}
	col := 0
	for _, c := range l.renderedChildren(node) {
		if c.Type != dom.Element {
			continue
		}
		cs := l.sm[c]
		if cs != nil && cs.Display == css.DisplayTableCell {
			span := cellColSpan(c)
			row.cells = append(row.cells, c)
			row.colStart = append(row.colStart, col)
			row.colSpan = append(row.colSpan, span)
			col += span
		}
	}
	return row
}

// cellStyle returns a cell node's style, falling back to a default table-cell
// style when absent.
func (l *layouter) cellStyle(cell *dom.Node) *css.Style {
	if cs := l.sm[cell]; cs != nil {
		return cs
	}
	return &css.Style{Display: css.DisplayTableCell, Width: css.Length{Auto: true},
		MinWidth: css.Length{Auto: true}, MaxWidth: css.Length{Auto: true},
		Height: css.Length{Auto: true}, ColumnWidth: css.Length{Auto: true}}
}

// distributeColumns turns the columns' constraints into widths summing to W
// (CSS 2.1 §17.5.2.2, refined the way browsers do). Percentage columns are
// served first — their share of W, never less than their minimum — and the
// rest of the width goes to the remaining columns: each gets its minimum when
// the table cannot afford more (the table then overflows, as a browser's
// does); between the minimums and the maximums every column grows from its
// minimum in proportion to how much it can still grow; beyond every maximum
// the surplus goes to the auto columns in proportion to their maximums, to
// the fixed ones only when no auto column is left to take it, and in equal
// shares when no column has any content to be proportional to.
func distributeColumns(cols []column, W float64) []float64 {
	w := make([]float64, len(cols))
	rest := W
	var free []int // the non-percentage columns
	for j, c := range cols {
		if c.pct > 0 {
			w[j] = math.Max(c.pct*W, c.min)
			rest -= w[j]
			continue
		}
		free = append(free, j)
	}
	if len(free) == 0 {
		// Every column is a percentage: what they leave over is shared in
		// proportion to their widths, so the table still fills W. (Each
		// width is at least its share of a positive W, so their sum is
		// positive whenever anything is left over.)
		if rest > 0 {
			var sum float64
			for _, v := range w {
				sum += v
			}
			for j := range w {
				w[j] += rest * w[j] / sum
			}
		}
		return w
	}
	var summin, summax float64
	for _, j := range free {
		summin += cols[j].min
		summax += cols[j].max
	}
	switch {
	case rest <= summin:
		for _, j := range free {
			w[j] = cols[j].min
		}
	case rest < summax:
		f := (rest - summin) / (summax - summin)
		for _, j := range free {
			w[j] = cols[j].min + (cols[j].max-cols[j].min)*f
		}
	default:
		extra := rest - summax
		var recv []int
		var base float64
		for _, j := range free {
			w[j] = cols[j].max
			if !cols[j].fixed && cols[j].max > 0 {
				recv = append(recv, j)
				base += cols[j].max
			}
		}
		if len(recv) == 0 {
			for _, j := range free {
				if cols[j].max > 0 {
					recv = append(recv, j)
					base += cols[j].max
				}
			}
		}
		if len(recv) == 0 {
			for _, j := range free {
				w[j] += extra / float64(len(free))
			}
			return w
		}
		if base == summax {
			// Every content column receives: scaling is the same arithmetic
			// the pre-§17.5.2.2 layout used, kept so identical inputs give
			// bit-identical widths.
			scale := rest / summax
			for _, j := range recv {
				w[j] = cols[j].max * scale
			}
			return w
		}
		for _, j := range recv {
			w[j] += extra * cols[j].max / base
		}
	}
	return w
}
