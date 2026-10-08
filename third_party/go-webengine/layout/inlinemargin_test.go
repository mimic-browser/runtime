// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package layout

import "testing"

// TestInlineElementMarginRightAddsGap covers news.ycombinator.com's own real
// markup: `<b class="hnname" style="margin-right:5px">Hacker News</b><a
// href="newest">new</a>` — no source whitespace between the two elements, so
// the only gap between "Hacker News" and "new" is the <b>'s own margin-right.
// InlineItem carries no margin field of its own; before this fix that margin
// was silently dropped, running the words together as "Hacker Newsnew".
func TestInlineElementMarginRightAddsGap(t *testing.T) {
	src := `<html><body style="margin:0"><span>` +
		`<b style="margin-right:5px">AB</b><a href="x">CD</a>` +
		`</span></body></html>`
	items := firstLineItems(findBox(layoutHTML(t, src, 300), "body"))
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2: %v", len(items), items)
	}
	assertF(t, "AB.X", items[0].X, 0)
	assertF(t, "CD.SpaceBefore", items[1].SpaceBefore, 5)
	// "AB" is 2 runes * 10px (fakeMeasurer) = 20px wide; "CD" starts 5px after.
	assertF(t, "CD.X", items[1].X, 25)
}

// TestInlineElementMarginLeftAddsGap covers margin-left applying as leading
// space before an inline element's own content, mirroring the margin-right
// case above. Deliberately NOT the very first item of its line: pendingMargin
// is carried in SpaceBefore, the same field collapsible whitespace uses, and
// layoutInline/wrapOneLine's line-breaking deliberately ignore SpaceBefore
// for a line's first item (so collapsible whitespace never creates a phantom
// indent) — a real margin on the line-INITIAL element would need a separate
// field to survive that, unconfirmed as a live bug on any of this project's
// corpus pages and NOT fixed here; this test covers the confirmed, common
// case (an inline margin between two things already on the same line).
func TestInlineElementMarginLeftAddsGap(t *testing.T) {
	src := `<html><body style="margin:0"><span>` +
		`<a href="x">AB</a><b style="margin-left:5px">CD</b>` +
		`</span></body></html>`
	items := firstLineItems(findBox(layoutHTML(t, src, 300), "body"))
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2: %v", len(items), items)
	}
	assertF(t, "CD.SpaceBefore", items[1].SpaceBefore, 5)
	assertF(t, "CD.X", items[1].X, 25)
}

// TestAdjacentInlineMarginsAdd covers two adjacent inline elements' margins
// stacking rather than collapsing — unlike BLOCK margins, adjacent inline
// margins are independent horizontal space that simply adds up.
func TestAdjacentInlineMarginsAdd(t *testing.T) {
	src := `<html><body style="margin:0"><span>` +
		`<b style="margin-right:5px">AB</b><i style="margin-left:3px">CD</i>` +
		`</span></body></html>`
	items := firstLineItems(findBox(layoutHTML(t, src, 300), "body"))
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2: %v", len(items), items)
	}
	assertF(t, "CD.SpaceBefore", items[1].SpaceBefore, 8)
	assertF(t, "CD.X", items[1].X, 28)
}

// TestBlockBreakDropsPendingMargin covers the boundary case explicitly: a
// pending margin-right with nothing left on the same line to apply to (a
// promoted block, see BlockBreak, immediately follows) is dropped rather than
// leaking into whatever comes after the block in the SAME parent scope.
func TestBlockBreakDropsPendingMargin(t *testing.T) {
	src := `<html><body style="margin:0"><span>` +
		`<b style="margin-right:5px">AB</b>` +
		`<div style="display:block">block</div>CD` +
		`</span></body></html>`
	body := findBox(layoutHTML(t, src, 300), "body")
	if body == nil || len(body.Children) < 3 {
		n := -1
		if body != nil {
			n = len(body.Children)
		}
		t.Fatalf("body.Children = %d, want >= 3 (anon run, div, anon run)", n)
	}
	lastAnon := body.Children[len(body.Children)-1]
	items := firstLineItems(lastAnon)
	if len(items) != 1 || items[0].Text != "CD" {
		t.Fatalf("items after the block break = %v, want just \"CD\"", items)
	}
	if items[0].SpaceBefore != 0 {
		t.Errorf("CD.SpaceBefore = %v, want 0 (pending margin dropped at the block break)", items[0].SpaceBefore)
	}
}
