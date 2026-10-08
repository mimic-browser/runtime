// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package layout

import "testing"

// TestInlineBlockExplicitSizeGetsNestedBox covers the bug this fixed: an
// EMPTY display:inline-block element (no children, no text — the common
// real shape for a CSS-only icon) was previously treated as plain
// display:inline, which contributes no box of its own at all — an empty
// inline element has nothing to measure, so it painted as invisible empty
// space regardless of its own explicit width/height. Confirmed live on
// en.wikipedia.org's own top-nav search icon: `<span class="vector-icon"
// style="display:inline-block;width:1rem;height:1rem">` (empty, styled
// entirely via mask-image+background-color) rendered as nothing, since
// mask-image only ever applies through the real Box-paint path a plain
// inline element never reaches.
func TestInlineBlockExplicitSizeGetsNestedBox(t *testing.T) {
	src := `<html><body style="margin:0">x <span style="display:inline-block;width:20px;height:20px"></span> y</body></html>`
	items := firstLineItems(layoutHTML(t, src, 300))
	if len(items) != 3 {
		t.Fatalf("items = %d, want 3 (x, the inline-block, y): %v", len(items), texts(items))
	}
	if items[1].NestedBox == nil {
		t.Fatal("expected the inline-block span to carry a NestedBox")
	}
	assertF(t, "NestedBox.W", items[1].NestedBox.W, 20)
	assertF(t, "NestedBox.H", items[1].NestedBox.H, 20)
	// "x" is 1 rune * 10px (fakeMeasurer) = 10px; one space (10px) before the
	// inline-block, matching ordinary inline whitespace-collapsing.
	assertF(t, "inlineblock.SpaceBefore", items[1].SpaceBefore, 10)
	assertF(t, "inlineblock.X", items[1].X, 20)
}

// TestInlineBlockAutoWidthShrinksToFit covers the width:auto case: with no
// explicit width, preferredWidth's own definite-width branch doesn't apply,
// so the element shrink-to-fits its own content — exactly like
// layoutNestedInlineFlex's identical sizing rule for display:inline-flex.
func TestInlineBlockAutoWidthShrinksToFit(t *testing.T) {
	src := `<html><body style="margin:0"><span style="display:inline-block">AB</span></body></html>`
	items := firstLineItems(layoutHTML(t, src, 300))
	if len(items) != 1 || items[0].NestedBox == nil {
		t.Fatalf("items = %v, want one item carrying a NestedBox", texts(items))
	}
	// "AB" is 2 runes * 10px (fakeMeasurer) = 20px content width.
	assertF(t, "NestedBox.W", items[0].NestedBox.W, 20)
}

// TestInlineBlockBorderBoxNarrowerThanItsOwnPadding covers
// layoutNestedInlineBlock's width floor — the same clamp
// layoutNestedInlineFlex's own identically-named test already covers for
// display:inline-flex: preferredWidth's definite-width branch returns an
// explicit box-sizing:border-box width outright even when it's smaller than
// the element's own padding, so subtracting the edges to get a content
// width would go negative; clamped to 0 instead of reaching layoutIsolated
// with a negative content width.
func TestInlineBlockBorderBoxNarrowerThanItsOwnPadding(t *testing.T) {
	src := `<html><body style="margin:0">` +
		`<span style="display:inline-block;box-sizing:border-box;width:2px;padding:10px"></span>` +
		`</body></html>`
	items := firstLineItems(layoutHTML(t, src, 300))
	if len(items) != 1 || items[0].NestedBox == nil {
		t.Fatalf("items = %v, want one item carrying a NestedBox", texts(items))
	}
	if items[0].NestedBox.W < 0 {
		t.Errorf("NestedBox.W = %v, want >= 0", items[0].NestedBox.W)
	}
}

// TestInlineBlockNotTreatedAsBlockLevelChild guards the OTHER half of the
// fix: an inline-block child must NOT make hasBlockLevelChild report true
// for its parent (it stays inline-level to whatever contains it — only its
// OWN content gets a nested block formatting context), or every paragraph
// containing one would wrongly switch from the fast pure-inline path to the
// block/inline-mixed path.
func TestInlineBlockNotTreatedAsBlockLevelChild(t *testing.T) {
	src := `<html><body style="margin:0"><span style="display:inline-block;width:10px;height:10px"></span></body></html>`
	body := findBox(layoutHTML(t, src, 300), "body")
	if body == nil {
		t.Fatal("no body box")
	}
	if len(body.Lines) == 0 {
		t.Fatal("expected body to take the pure-inline path (Lines set directly), not the block/inline mixed path")
	}
}
