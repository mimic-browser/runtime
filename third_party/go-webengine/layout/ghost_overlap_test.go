// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package layout

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/dom"
)

// blockRect is one in-flow block box's vertical extent, tagged for diagnostics.
type blockRect struct {
	tag        string
	y0, y1     float64
	x0, x1     float64
	firstChild bool
}

// inFlowSiblingRects returns, for every element box, its in-flow block children
// in document order (skipping floats and out-of-flow boxes, which legitimately
// share coordinates with siblings). Consecutive entries within one slice must
// stack — the assertion the Ghost bug violated.
func inFlowSiblingGroups(b *Box, out *[][]blockRect) {
	if b == nil {
		return
	}
	var grp []blockRect
	for _, c := range b.Children {
		if c.Float != css.FloatNone || c.Position == css.PositionAbsolute ||
			c.Position == css.PositionFixed {
			continue
		}
		if c.H <= 0 {
			continue
		}
		tag := ""
		if c.Node != nil {
			tag = c.Node.Tag
		}
		grp = append(grp, blockRect{tag: tag, y0: c.Y, y1: c.Y + c.H, x0: c.X, x1: c.X + c.W})
	}
	if len(grp) > 1 {
		*out = append(*out, grp)
	}
	for _, c := range b.Children {
		inFlowSiblingGroups(c, out)
	}
}

// allTextItems collects every positioned text run in the tree.
func allTextItems(b *Box, out *[]*InlineItem) {
	if b == nil {
		return
	}
	for _, ln := range b.Lines {
		for _, it := range ln.Items {
			if it.Text != "" {
				*out = append(*out, it)
			}
		}
	}
	for _, c := range b.Children {
		allTextItems(c, out)
	}
}

// TestGhostBlockOverlapFixture is the end-to-end regression for the go-news-reader
// Ghost-blog block-overlap bug. It lays out a trimmed offline fixture (no network)
// that reproduces BOTH root causes — a clearfix `height:0` on ::before/::after
// pseudo-elements wrongly collapsing the real .container/.wrap/.post boxes, and a
// full-width `float:left` mobile layout whose stacked floats must not be pushed
// off-screen. It asserts the article, author-bio, subscribe box and footer stack
// vertically without overlap and that all text stays on-screen.
func TestGhostBlockOverlapFixture(t *testing.T) {
	const vpW = 900
	data, err := os.ReadFile(filepath.Join("..", "testdata", "ghost_block_overlap.html"))
	if err != nil {
		t.Fatal(err)
	}
	root, err := dom.Parse(string(data))
	if err != nil {
		t.Fatal(err)
	}
	sm := css.Cascade(root)
	box, height := LayoutDocument(root, sm, vpW, fakeMeasurer{}, nil)

	// (1) The document must not have collapsed. With the deterministic fake
	// measurer every line is 20px tall; the six stacked text lines (title, three
	// body paragraphs, author bio, subscribe, footer) lay out to ~140px. The bug
	// collapsed them onto one band (~20-40px). 120px cleanly separates the two.
	if height < 120 {
		t.Fatalf("document collapsed: content height %.0f (want >= 120)", height)
	}

	// (2) Consecutive in-flow block siblings must stack (no vertical overlap).
	var groups [][]blockRect
	inFlowSiblingGroups(box, &groups)
	const eps = 0.5
	for _, grp := range groups {
		for i := 1; i < len(grp); i++ {
			prev, cur := grp[i-1], grp[i]
			if cur.y0 < prev.y1-eps {
				t.Errorf("block <%s> [y %.0f-%.0f] overlaps previous <%s> [y %.0f-%.0f] by %.0fpx",
					cur.tag, cur.y0, cur.y1, prev.tag, prev.y0, prev.y1, prev.y1-cur.y0)
			}
		}
	}

	// (3) All text must stay on-screen: the float bug pushed the body column to
	// x = viewport width, off the right edge. Every text run must start left of
	// the viewport's right edge.
	var items []*InlineItem
	allTextItems(box, &items)
	if len(items) == 0 {
		t.Fatal("no text laid out")
	}
	for _, it := range items {
		if it.X >= vpW-eps {
			t.Errorf("text %q pushed off-screen at x=%.0f (viewport width %d)", it.Text, it.X, vpW)
		}
	}

	// (4) Spot-check the body paragraph and the footer are vertically separated
	// (the two ends of the collapsed band in the bug).
	var bodyY, footerY float64 = -1, -1
	for _, it := range items {
		if bodyY < 0 && len(it.Text) >= 5 && it.Text[:5] == "Today" {
			bodyY = it.Y
		}
		if footerY < 0 && len(it.Text) >= 9 && it.Text[:9] == "Copyright" {
			footerY = it.Y
		}
	}
	if bodyY < 0 || footerY < 0 {
		t.Fatalf("could not locate body (%.0f) and footer (%.0f) text", bodyY, footerY)
	}
	if footerY <= bodyY {
		t.Errorf("footer text (y=%.0f) must sit below body text (y=%.0f)", footerY, bodyY)
	}
}
