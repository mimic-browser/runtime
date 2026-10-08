// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package layout

import "testing"

// TestInlineFlexFlowsInline covers the base bug: display:inline-flex used to
// parse down to the SAME css.DisplayFlex value as plain flex, losing the
// "inline" qualifier entirely and making the element block-level — so
// several inline-flex siblings with no whitespace between them each landed
// on their OWN line instead of flowing in a row. Confirmed live on
// pkg.go.dev: `.go-Breadcrumb li{display:inline-flex}` stacked every
// breadcrumb item vertically instead of reading as one line.
func TestInlineFlexFlowsInline(t *testing.T) {
	src := `<html><body style="margin:0">` +
		`<b style="display:inline-flex">AB</b><i style="display:inline-flex">CD</i>` +
		`</body></html>`
	items := firstLineItems(findBox(layoutHTML(t, src, 300), "body"))
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2 (both on one line): %v", len(items), items)
	}
	if items[0].NestedBox == nil || items[1].NestedBox == nil {
		t.Fatalf("expected both items to carry a NestedBox: %v", items)
	}
	// "AB" is 2 runes * 10px (fakeMeasurer) = 20px wide; "CD" starts right
	// after it (no source whitespace between the two elements).
	assertF(t, "AB.X", items[0].X, 0)
	assertF(t, "CD.X", items[1].X, 20)
}

// TestInlineFlexNestedBoxFollowsOuterTranslation covers a SEPARATE bug this
// session's own established methodology found while verifying the fix
// above live: translateBox — the general mechanism that repositions a
// subtree already laid out at a local origin (a flex/grid item, a float —
// see its own doc comment, which already documents an identical past bug
// for list-item markers) — walks box.Children and each InlineItem's scalar
// X/Y, but NestedBox is a SECOND, independent box tree hanging off an
// InlineItem that isn't reachable through either of those. An inline-flex
// element nested inside something that is ITSELF repositioned by an outer
// flex/grid ancestor (the common real case: pkg.go.dev's breadcrumb `<li>`
// sits inside a header section positioned by an outer flex layout) had its
// InlineItem's own X/Y scalar correctly updated by the outer translation,
// but its NestedBox's internally-stored coordinates were left stuck at
// their pre-translation position — painting the inline-flex content at the
// WRONG place while everything else on the line moved correctly.
func TestInlineFlexNestedBoxFollowsOuterTranslation(t *testing.T) {
	src := `<html><body style="margin:0"><div style="display:flex">` +
		`<div style="width:100px;height:10px"></div>` +
		`<span><b style="display:inline-flex"><i>XY</i></b></span>` +
		`</div></body></html>`
	root := layoutHTML(t, src, 300)
	outer := findBox(root, "div")
	if len(outer.Children) != 2 {
		t.Fatalf("flex children = %d, want 2", len(outer.Children))
	}
	span := outer.Children[1]
	// The 100px spacer is the first flex item, so the span starts at X=100.
	assertF(t, "span.X", span.X, 100)
	if len(span.Lines) == 0 || len(span.Lines[0].Items) == 0 {
		t.Fatal("span has no inline content")
	}
	it := span.Lines[0].Items[0]
	if it.NestedBox == nil {
		t.Fatal("expected the inline-flex <b> to carry a NestedBox")
	}
	// The NestedBox's OWN top-level position must have followed the span's
	// translation from its isolated local origin to X=100 — this is what
	// the bug broke (it stayed at its pre-translation position instead).
	assertF(t, "NestedBox.X", it.NestedBox.X, 100)
	// And so must everything INSIDE it: <i>XY</i> is <b>'s own (single)
	// flex item, one level deeper still.
	if len(it.NestedBox.Children) == 0 {
		t.Fatal("NestedBox has no flex children")
	}
	inner := it.NestedBox.Children[0]
	if len(inner.Lines) == 0 || len(inner.Lines[0].Items) == 0 {
		t.Fatal("inline-flex's own flex item has no inline content")
	}
	assertF(t, "XY.X", inner.Lines[0].Items[0].X, 100)
}

// TestInlineFlexAfterWhitespaceTakesASpace covers the same "boundary space
// before an atomic inline item" handling image/form-control items already
// have, applied to inline-flex: whitespace in the source between preceding
// text and the inline-flex element becomes exactly one space, the same
// whitespace-collapsing rule as everywhere else.
func TestInlineFlexAfterWhitespaceTakesASpace(t *testing.T) {
	src := `<html><body style="margin:0">x <b style="display:inline-flex">AB</b></body></html>`
	items := firstLineItems(findBox(layoutHTML(t, src, 300), "body"))
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2: %v", len(items), items)
	}
	if items[1].NestedBox == nil {
		t.Fatal("expected the second item to carry a NestedBox")
	}
	// "x" is 1 rune * 10px = 10px wide; the inline-flex starts 10px (one
	// space, fakeMeasurer) after it.
	assertF(t, "flex.SpaceBefore", items[1].SpaceBefore, 10)
	assertF(t, "flex.X", items[1].X, 20)
}

// TestInlineFlexBorderBoxNarrowerThanItsOwnPadding covers
// layoutNestedInlineFlex's width floor: preferredWidth normally returns a
// content+edges total, but an EXPLICIT box-sizing:border-box width smaller
// than the element's own padding returns just that (smaller) border-box
// value directly — subtracting edges from it goes negative. Clamped to 0
// rather than passed to layoutIsolated as a negative content width.
func TestInlineFlexBorderBoxNarrowerThanItsOwnPadding(t *testing.T) {
	src := `<html><body style="margin:0">` +
		`<b style="display:inline-flex;box-sizing:border-box;width:2px;padding:10px"><i>Z</i></b>` +
		`</body></html>`
	items := firstLineItems(findBox(layoutHTML(t, src, 300), "body"))
	if len(items) != 1 || items[0].NestedBox == nil {
		t.Fatalf("items = %v, want one item carrying a NestedBox", items)
	}
	// Content width clamped to 0; the box's own outer width is still just
	// its padding (border-box width itself is smaller than that padding,
	// so the used content width can't go negative).
	if items[0].NestedBox.W < 0 {
		t.Errorf("NestedBox.W = %v, want >= 0", items[0].NestedBox.W)
	}
}

// TestInlineFlexInsidePreStillTranslates covers the white-space:pre variant
// of layoutInline's positioning loop — a separate code path from the
// wrapping one above, with its own translateBox call site — to make sure a
// NestedBox item reached that way is translated to its final position too.
func TestInlineFlexInsidePreStillTranslates(t *testing.T) {
	src := `<pre><b style="display:inline-flex">AB</b></pre>`
	items := firstLineItems(findBox(layoutHTML(t, src, 300), "pre"))
	if len(items) != 1 || items[0].NestedBox == nil {
		t.Fatalf("items = %v, want one item carrying a NestedBox", items)
	}
	assertF(t, "NestedBox.X", items[0].NestedBox.X, items[0].X)
	assertF(t, "NestedBox.Y", items[0].NestedBox.Y, items[0].Y)
}

// TestInlineFlexPreferredWidthIncludesTextAlongsideBlockChild covers a
// SEPARATE preferredWidth sizing gap from TestFlexContainerMixedTextAndElement
// (cover2_test.go, round 47): that fix only guarded the flex-row-SUM branch's
// "bare text mixed with a real element child" case. Its sibling branch below
// it — hasBlockLevelChild's own "max of ELEMENT children" loop, which ALSO
// only ever visits Element children — has the identical blind spot, reached
// instead of the sum branch whenever the element child computes display:block
// specifically (not just any element) — e.g. an inline <svg>, which
// Tailwind's own preflight reset (`img,svg,video,...{display:block}`) gives
// display:block unconditionally. Before this fix, preferredWidth measured
// ONLY the block child (silently dropping the text's own width entirely),
// sizing the whole inline-flex container down to that child's tiny width —
// forcing the bare text to wrap ONE WORD PER LINE inside it. Confirmed live
// on tailwindcss.com's own "Become a sponsor →" button: an inline-flex <a>
// whose content is bare text plus exactly this shape of icon.
func TestInlineFlexPreferredWidthIncludesTextAlongsideBlockChild(t *testing.T) {
	src := `<html><body style="margin:0">` +
		`<b style="display:inline-flex">Become a sponsor<i style="display:block;width:10px;height:10px"></i></b>` +
		`</body></html>`
	items := firstLineItems(findBox(layoutHTML(t, src, 1024), "body"))
	if len(items) != 1 || items[0].NestedBox == nil {
		t.Fatalf("items = %v, want one item carrying a NestedBox", items)
	}
	nb := items[0].NestedBox
	// "Become a sponsor" is 16 chars (fakeMeasurer: 10px/char) = 160px, so the
	// container's shrink-to-fit width must reflect that, not just the 10px
	// block child's own width.
	if nb.W < 160 {
		t.Errorf("NestedBox.W = %.0f, want >= ~160 (text width included, not just the 10px block child)", nb.W)
	}
	textItems := firstLineItems(nb)
	if len(textItems) == 0 || textItems[0].Text == "" {
		t.Fatalf("NestedBox's own text content missing: %v", textItems)
	}
	// The bug's visible symptom: the container was sized so narrow that the
	// text wrapped after almost every word, landing "Become"/"a"/"sponsor"
	// each on their own line instead of flowing as ONE run.
	if len(textItems) < 3 || textItems[0].Text != "Become" || textItems[1].Text != "a" || textItems[2].Text != "sponsor" {
		t.Fatalf("expected \"Become\"/\"a\"/\"sponsor\" as consecutive items on one line, got %v", texts(textItems))
	}
	for i := 1; i < len(textItems); i++ {
		if textItems[i].Y != textItems[0].Y {
			t.Fatalf("item %d (%q) is on a different line (Y=%.0f vs Y=%.0f) — text wrapped word-by-word instead of flowing on one line",
				i, textItems[i].Text, textItems[i].Y, textItems[0].Y)
		}
	}
}
