// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package layout

import "testing"

// firstLine returns the first line box holding any items, anywhere in the tree.
func firstLine(b *Box) *LineBox {
	if b == nil {
		return nil
	}
	for _, ln := range b.Lines {
		if len(ln.Items) > 0 {
			return ln
		}
	}
	for _, c := range b.Children {
		if ln := firstLine(c); ln != nil {
			return ln
		}
	}
	return nil
}

// allLines returns every non-empty line box in the tree, in document order.
func allLines(b *Box) []*LineBox {
	if b == nil {
		return nil
	}
	var out []*LineBox
	for _, ln := range b.Lines {
		if len(ln.Items) > 0 {
			out = append(out, ln)
		}
	}
	for _, c := range b.Children {
		out = append(out, allLines(c)...)
	}
	return out
}

// TestInlineDecorationReservesEdges: a styled inline <span> owns a box of its
// own — background, border and padding — and CSS reserves its horizontal
// border+padding in the line's advance. Before this, collectInline reserved
// nothing at all for it, so text after the span sat where the span's own
// padding should have been and any painted decoration would have overlapped
// its neighbours.
//
// fakeMeasurer gives every rune 10px, so "AB"/"CD"/"EF" are 20px each. The
// span's leading edge is border-left 2 + padding-left 5 = 7px and its trailing
// edge the same, so: AB at 0..20, the span's fragment opens at 20, CD's own
// text starts at 27, ends at 47, the trailing edge closes the fragment at 54,
// and EF resumes there.
func TestInlineDecorationReservesEdges(t *testing.T) {
	src := `<html><body style="margin:0"><div style="width:400px">` +
		`AB<span style="padding:0 5px;border:2px solid red;background:blue">CD</span>EF` +
		`</div></body></html>`
	line := firstLine(findBox(layoutHTML(t, src, 500), "div"))
	if line == nil || len(line.Items) != 3 {
		t.Fatalf("line items = %v, want 3", line)
	}
	assertF(t, "AB.X", line.Items[0].X, 0)
	assertF(t, "CD.X", line.Items[1].X, 27)
	assertF(t, "EF.X (pushed right by the span's own padding+border)", line.Items[2].X, 54)

	if len(line.Inlines) != 1 {
		t.Fatalf("line.Inlines = %d, want 1: %+v", len(line.Inlines), line.Inlines)
	}
	fr := line.Inlines[0]
	if fr.Node == nil || fr.Node.Tag != "span" {
		t.Errorf("fragment.Node = %v, want the <span>", fr.Node)
	}
	if fr.Style == nil || fr.Style.Background.B != 0xff {
		t.Errorf("fragment.Style = %+v, want the span's own computed style", fr.Style)
	}
	assertF(t, "fragment.X", fr.X, 20)
	assertF(t, "fragment.W", fr.W, 34) // 7 lead + 20 text + 7 trail
	// Vertical: fakeMeasurer's ascent 8 / line height 20 put the font box at
	// y 0..20; the 2px top and bottom borders grow the border box past it,
	// which is CSS — vertical inline padding/border overflows the line box
	// rather than growing it, so the line stays 20px tall.
	assertF(t, "fragment.Y", fr.Y, -2)
	assertF(t, "fragment.H", fr.H, 24)
	assertF(t, "line.H unchanged by vertical padding", line.H, 20)
	if !fr.First || !fr.Last {
		t.Errorf("fragment First/Last = %v/%v, want true/true (one fragment)", fr.First, fr.Last)
	}
}

// TestInlineDecorationFragmentsPerLineBox: an inline element that wraps gets
// ONE FRAGMENT PER LINE BOX, and box-decoration-break: slice (the CSS default)
// puts its leading edge on the first fragment only and its trailing edge on
// the last only. The continuation line must NOT re-reserve the left padding.
func TestInlineDecorationFragmentsPerLineBox(t *testing.T) {
	// Width 100 with 10px runes: "AAAA" and "BBBB" are 40px each and a
	// collapsible space is 10px, so the span's two words cannot share a line
	// once its 7px leading edge is reserved (7+40+10+40+7 = 104 > 100).
	src := `<html><body style="margin:0"><div style="width:100px">` +
		`<span style="padding:0 5px;border:2px solid red;background:blue">AAAA BBBB</span>` +
		`</div></body></html>`
	lines := allLines(findBox(layoutHTML(t, src, 300), "div"))
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
	if len(lines[0].Inlines) != 1 || len(lines[1].Inlines) != 1 {
		t.Fatalf("fragments per line = %d/%d, want 1/1", len(lines[0].Inlines), len(lines[1].Inlines))
	}
	first, second := lines[0].Inlines[0], lines[1].Inlines[0]
	if first.Node != second.Node {
		t.Errorf("the two fragments must belong to the SAME element")
	}
	if !first.First || first.Last {
		t.Errorf("first fragment First/Last = %v/%v, want true/false", first.First, first.Last)
	}
	if second.First || !second.Last {
		t.Errorf("second fragment First/Last = %v/%v, want false/true", second.First, second.Last)
	}
	// Line 1 carries the leading edge (7px) and nothing trailing: 7+40 = 47.
	assertF(t, "first.X", first.X, 0)
	assertF(t, "first.W", first.W, 47)
	assertF(t, "AAAA.X", lines[0].Items[0].X, 7)
	// Line 2 re-opens with NO leading edge and closes with the 7px trailing
	// one: the word starts at the line's own left edge.
	assertF(t, "BBBB.X (no phantom re-indent on the continuation line)", lines[1].Items[0].X, 0)
	assertF(t, "second.X", second.X, 0)
	assertF(t, "second.W", second.W, 47)
}

// TestNestedInlineDecorationOutermostFirst: a decorated <b> inside a decorated
// <span> yields a fragment each, ordered outermost first so a painter layers
// the nested background over its ancestor's rather than under it. Both
// elements' edges are reserved, and they nest.
func TestNestedInlineDecorationOutermostFirst(t *testing.T) {
	src := `<html><body style="margin:0"><div style="width:400px">` +
		`<span style="padding:0 5px;background:blue"><b style="padding:0 3px;background:lime">XY</b>Z</span>` +
		`</div></body></html>`
	line := firstLine(findBox(layoutHTML(t, src, 500), "div"))
	if line == nil || len(line.Inlines) != 2 {
		t.Fatalf("line.Inlines = %v, want 2", line)
	}
	outer, inner := line.Inlines[0], line.Inlines[1]
	if outer.Node.Tag != "span" || inner.Node.Tag != "b" {
		t.Fatalf("fragment order = %s,%s, want span,b (outermost first)", outer.Node.Tag, inner.Node.Tag)
	}
	// span lead 5 opens at 0; b lead 3 opens at 5; "XY" (20px) at 8..28;
	// b's trail 3 closes it at 31; "Z" (10px) at 31..41; span's trail 5
	// closes it at 46.
	assertF(t, "XY.X", line.Items[0].X, 8)
	assertF(t, "Z.X", line.Items[1].X, 31)
	assertF(t, "outer.X", outer.X, 0)
	assertF(t, "outer.W", outer.W, 46)
	assertF(t, "inner.X", inner.X, 5)
	assertF(t, "inner.W", inner.W, 26)
}

// TestAdjacentInlineDecorationsAreSeparateFragments: two sibling decorated
// spans diverge at depth 0 even though neither is nested in the other, so each
// gets its own complete (First AND Last) fragment and their edges add up.
func TestAdjacentInlineDecorationsAreSeparateFragments(t *testing.T) {
	src := `<html><body style="margin:0"><div style="width:400px">` +
		`<span style="padding:0 5px;background:blue">AB</span>` +
		`<span style="padding:0 2px;background:lime">CD</span>` +
		`</div></body></html>`
	line := firstLine(findBox(layoutHTML(t, src, 500), "div"))
	if line == nil || len(line.Inlines) != 2 {
		t.Fatalf("line.Inlines = %v, want 2", line)
	}
	a, b := line.Inlines[0], line.Inlines[1]
	if !a.First || !a.Last || !b.First || !b.Last {
		t.Errorf("both fragments must be complete: %v/%v and %v/%v", a.First, a.Last, b.First, b.Last)
	}
	assertF(t, "a.X", a.X, 0)
	assertF(t, "a.W", a.W, 30) // 5 + 20 + 5
	assertF(t, "b.X", b.X, 30)
	assertF(t, "b.W", b.W, 24) // 2 + 20 + 2
	assertF(t, "CD.X", line.Items[1].X, 32)
}

// TestUndecoratedInlineElementsMakeNoFragment: <a>, <b>, <em> and a UA-default
// <code> carry only typographic style — no background, no border, no padding —
// so they generate no box, reserve no space and produce no fragment at all.
// This is what keeps every existing page (and golden) byte-identical.
func TestUndecoratedInlineElementsMakeNoFragment(t *testing.T) {
	src := `<html><body style="margin:0"><div style="width:400px">` +
		`<a href="x">AB</a><code>CD</code><b>EF</b><em>GH</em>` +
		`</div></body></html>`
	line := firstLine(findBox(layoutHTML(t, src, 500), "div"))
	if line == nil || len(line.Items) != 4 {
		t.Fatalf("line items = %v, want 4", line)
	}
	if len(line.Inlines) != 0 {
		t.Fatalf("line.Inlines = %+v, want none for undecorated inline elements", line.Inlines)
	}
	for i, want := range []float64{0, 20, 40, 60} {
		assertF(t, "item.X", line.Items[i].X, want)
		if line.Items[i].padLead != 0 || line.Items[i].padTrail != 0 {
			t.Errorf("item %d reserved padding for an undecorated element", i)
		}
	}
}

// TestInlineDecorationSpansAForcedBreak: a <br> inside a decorated span is not
// content — it is never positioned — so the element's leading edge is reserved
// once, before its first word, and its trailing edge once, after its last, with
// one fragment per resulting line box.
func TestInlineDecorationSpansAForcedBreak(t *testing.T) {
	src := `<html><body style="margin:0"><div style="width:400px">` +
		`<span style="padding:0 5px;background:blue">AB<br>CD</span>` +
		`</div></body></html>`
	lines := allLines(findBox(layoutHTML(t, src, 500), "div"))
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
	if len(lines[0].Inlines) != 1 || len(lines[1].Inlines) != 1 {
		t.Fatalf("fragments per line = %d/%d, want 1/1", len(lines[0].Inlines), len(lines[1].Inlines))
	}
	assertF(t, "AB.X", lines[0].Items[0].X, 5)
	assertF(t, "line1 fragment.W", lines[0].Inlines[0].W, 25) // 5 lead + 20
	assertF(t, "CD.X (no re-indent after the break)", lines[1].Items[0].X, 0)
	assertF(t, "line2 fragment.W", lines[1].Inlines[0].W, 25) // 20 + 5 trail
	if !lines[0].Inlines[0].First || lines[0].Inlines[0].Last {
		t.Errorf("the pre-break fragment must be First and not Last")
	}
	if lines[1].Inlines[0].First || !lines[1].Inlines[0].Last {
		t.Errorf("the post-break fragment must be Last and not First")
	}
}

// TestInlineDecorationCountsTowardMaxContentWidth: a shrink-to-fit container's
// max-content estimate must include the inline element's reserved edges, or a
// float/flex item sized to its text alone would clip the padding away.
func TestInlineDecorationCountsTowardMaxContentWidth(t *testing.T) {
	src := `<html><body style="margin:0"><div style="float:left">` +
		`<span style="padding:0 5px;border:2px solid red">AB</span>` +
		`</div></body></html>`
	div := findBox(layoutHTML(t, src, 500), "div")
	if div == nil {
		t.Fatal("no float box")
	}
	assertF(t, "float width = text + both reserved edges", div.W, 34)
}

// TestInlineDecorationInPreformattedText: white-space:pre takes a different
// line-building path (WrapItems, no wrapping) that must reserve and fragment
// identically.
func TestInlineDecorationInPreformattedText(t *testing.T) {
	src := `<html><body style="margin:0"><pre style="margin:0;width:400px">` +
		`<span style="padding:0 5px;background:blue">AB</span>CD` +
		`</pre></body></html>`
	line := firstLine(findBox(layoutHTML(t, src, 500), "pre"))
	if line == nil || len(line.Inlines) != 1 {
		t.Fatalf("line.Inlines = %v, want 1", line)
	}
	assertF(t, "AB.X", line.Items[0].X, 5)
	assertF(t, "CD.X", line.Items[1].X, 30)
	assertF(t, "fragment.W", line.Inlines[0].W, 30)
}

// TestBorderStyleNoneReservesNothing: Borders.Widths() zeroes a
// `border-style:none` edge, so a width set alongside it neither reserves space
// nor makes the element generate a box at all — the same rule block layout
// already applies.
func TestBorderStyleNoneReservesNothing(t *testing.T) {
	src := `<html><body style="margin:0"><div style="width:400px">` +
		`<span style="border:4px none red">AB</span>CD` +
		`</div></body></html>`
	line := firstLine(findBox(layoutHTML(t, src, 500), "div"))
	if line == nil {
		t.Fatal("no line")
	}
	if len(line.Inlines) != 0 {
		t.Fatalf("line.Inlines = %+v, want none", line.Inlines)
	}
	assertF(t, "CD.X", line.Items[1].X, 20)
}

// TestInlineDecorationTranslatesWithItsBox: fragments are absolute document
// geometry, so a box repositioned after layout (a flex item, a grid item, a
// float) must carry them along with its words.
func TestInlineDecorationTranslatesWithItsBox(t *testing.T) {
	src := `<html><body style="margin:0"><div style="display:flex">` +
		`<div style="width:50px">P</div>` +
		`<div><span style="padding:0 5px;background:blue">AB</span></div>` +
		`</div></body></html>`
	lines := allLines(layoutHTML(t, src, 500))
	var frag *InlineFragment
	var word *InlineItem
	for _, ln := range lines {
		if len(ln.Inlines) == 1 {
			frag = &ln.Inlines[0]
			word = ln.Items[0]
		}
	}
	if frag == nil {
		t.Fatal("no inline fragment found")
	}
	if frag.X != word.X-5 {
		t.Errorf("fragment.X = %v, word.X = %v: the fragment did not follow its box", frag.X, word.X)
	}
	if frag.X == 0 {
		t.Errorf("fragment.X = 0: the flex translation was not applied to it")
	}
}
