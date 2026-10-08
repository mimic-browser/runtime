// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package layout

import "testing"

// TestInlineGluedRunDoesNotSplitAtLineWrap is the end-to-end version of
// TestWrapItemsGluedRunMovesTogether (linebreak_test.go), through the real
// layoutInline/wrapOneLine/forceOne path a page actually takes — engine#149,
// found on an html2pdf cost table cell rendering `101,5 / 152,3<sup>†</sup>`:
// the line broke between "152,3" and the dagger, since every InlineItem
// boundary was treated as a break opportunity regardless of whether real
// whitespace separated the two. `<sup>` immediately after text with no
// source whitespace leaves the sup's own item at SpaceBefore==0, gluing it
// to "152,3" into one unbreakable run.
func TestInlineGluedRunDoesNotSplitAtLineWrap(t *testing.T) {
	src := `<html><body style="margin:0">AA BB<sup>CC</sup></body></html>`
	// fakeMeasurer: 10px/rune, 10px space. "AA"=20, "BB"=20, glued "CC"=20 —
	// the run "BB CC" is 40 wide. maxW=50: "AA"(20) + space(10) + run(40) =
	// 70 > 50, so "AA" alone on line 1, "BB CC" together on line 2 — never
	// "AA BB" on line 1 with "CC" split off alone.
	body := findBox(layoutHTML(t, src, 50), "body")
	if body == nil || len(body.Lines) < 2 {
		t.Fatalf("expected 2 lines, got %v", body)
	}
	if len(body.Lines[0].Items) != 1 || body.Lines[0].Items[0].Text != "AA" {
		t.Fatalf("line0 = %v, want just \"AA\"", texts(body.Lines[0].Items))
	}
	line1 := body.Lines[1].Items
	if len(line1) != 2 || line1[0].Text != "BB" || line1[1].Text != "CC" {
		t.Fatalf("line1 = %v, want \"BB\" \"CC\" together", texts(line1))
	}
}

// TestInlineGluedRunSplitsOnlyAsLastResort covers forceOne's own fallback:
// when the WHOLE glued run still doesn't fit even alone on a fresh line, it
// is split there — but only as an overflow-of-last-resort, confirming the
// fix doesn't regress into an infinite loop or a permanently-stuck layout.
func TestInlineGluedRunSplitsOnlyAsLastResort(t *testing.T) {
	src := `<html><body style="margin:0">BB<sup>CC</sup></body></html>`
	// "BB"=20, glued "CC"=20; maxW=15 fits neither alone nor together.
	body := findBox(layoutHTML(t, src, 15), "body")
	if body == nil || len(body.Lines) != 2 {
		t.Fatalf("expected 2 lines (forced split), got %d: %v", len(body.Lines), body.Lines)
	}
	if len(body.Lines[0].Items) != 1 || body.Lines[0].Items[0].Text != "BB" {
		t.Errorf("line0 = %v, want just \"BB\"", texts(body.Lines[0].Items))
	}
	if len(body.Lines[1].Items) != 1 || body.Lines[1].Items[0].Text != "CC" {
		t.Errorf("line1 = %v, want just \"CC\"", texts(body.Lines[1].Items))
	}
}

// TestMinContentWidthCountsGluedRunAsOneWord is minContentWidth's own half of
// engine#149 — the issue's explicit ask: "the table minimum floor (#147)
// counts 152,3† as one word". Without it, a column could be sized between
// the glued run's two pieces' individual widths and its true combined
// width, reproducing the same mid-run split one level up, inside a table
// cell whose column the min-content floor is supposed to protect exactly
// against this.
func TestMinContentWidthCountsGluedRunAsOneWord(t *testing.T) {
	// col0 is a single long word forcing col1 down to its own minimum.
	// col1's content is "AB" (20) + glued "C" (10, SpaceBefore 0) = an
	// unbreakable run of 30 — NOT 20, the widest individual item.
	src := `<html><body style="margin:0"><table style="width:90px"><tr>` +
		`<td style="padding:0">aaaaaaaaaa</td><td style="padding:0">AB<sup>C</sup></td></tr></table></body></html>`
	c := tableCells(t, src, 400)
	assertF(t, "col1.W (glued run as one word)", c[1].W, 30)
}
