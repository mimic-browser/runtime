// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package layout

import (
	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/dom"
)

// layoutNestedInlineBlock lays out a display:inline-block element as a
// self-contained nested box for InlineItem.NestedBox (see
// appendElementInline) — the same mechanism layoutNestedInlineFlex already
// uses for display:inline-flex: an atomic inline-level item whose CONTENT is
// laid out as a genuine nested formatting context, since inline-vs-block only
// changes how the PARENT places this box, never how it lays out its OWN
// children. Sized at its explicit width if the author gave it one —
// preferredWidth's own definite-width branch already returns that outright,
// checked before any display-specific branching — or shrink-to-fit
// otherwise, exactly like layoutNestedInlineFlex's own sizing. The isolated
// clone forces plain DisplayBlock so contents()'s default (non-flex/grid/
// table) dispatch lays out its children: a NORMAL block formatting context
// is exactly what a real inline-block's content area is.
//
// Confirmed live on en.wikipedia.org: Vector-2022's own top-nav icons
// (`<span class="vector-icon ...">`, EMPTY — no children at all) are sized
// entirely by CSS (`display:inline-block;width:1rem;height:1rem`) and
// painted via `mask-image`+`background-color` — mask-image only ever applies
// through the ordinary Box-paint path (paintBox's own hasMask check reads
// box.Style, never an InlineItem directly), which a plain inline element
// (this engine's ENTIRE previous treatment of display:inline-block — the
// value parsed correctly into css.Style but was never referenced by
// isBlockLevel or appendElementInline, so it silently fell through to plain
// `display:inline` handling) never reaches: the search and language-switcher
// icons in Wikipedia's own header rendered as invisible empty space, sized
// to nothing since an empty plain-inline span has no content to measure
// either.
func (l *layouter) layoutNestedInlineBlock(el *dom.Node, st *css.Style) *Box {
	clone := *st
	clone.Display = css.DisplayBlock
	// preferredWidth returns an outer (border+padding-inclusive) measurement
	// — see its own doc comment — while layoutIsolated wants a pure content
	// width (it lays the node out at BoxSizing:ContentBox), so the edges it
	// added have to come back off here, the same adjustment
	// layoutNestedInlineFlex already makes.
	bw := clone.Border.Widths()
	edges := bw.Left + bw.Right + clone.Padding.Left + clone.Padding.Right
	w := l.preferredWidth(el, &clone) - edges
	if w < 0 {
		w = 0
	}
	return l.layoutIsolated(el, &clone, w)
}
