// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package layout

import (
	"strings"
	"testing"
)

// word builds an item with a fixed width and a 1-unit space before.
func word(text string, w float64) *InlineItem {
	return &InlineItem{Text: text, Width: w, SpaceBefore: 1}
}

// glued builds an item with a fixed width and NO space before — the same
// zero SpaceBefore appendWords/appendElementInline leave when no collapsible
// whitespace and no margin gap preceded it, e.g. `<sup>` immediately after
// text with no space in the source. Unlike word's 1-unit space, it has no
// CSS Text line-breaking opportunity before it (see glueRun).
func glued(text string, w float64) *InlineItem {
	return &InlineItem{Text: text, Width: w, SpaceBefore: 0}
}

// lineText joins the words of a line for compact assertions.
func lineText(l *LineBox) string {
	var parts []string
	for _, it := range l.Items {
		parts = append(parts, it.Text)
	}
	return strings.Join(parts, " ")
}

// linesText is lineText over a whole line list, for compact failure messages.
func linesText(lines []*LineBox) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = lineText(l)
	}
	return out
}

func TestWrapItemsEmpty(t *testing.T) {
	if got := WrapItems(nil, 100); got != nil {
		t.Errorf("empty = %v", got)
	}
}

func TestWrapItemsGreedy(t *testing.T) {
	// widths: a=10 b=10 c=10 d=10, space=1. maxW=22.
	// line1: a(10) + " "+b(1+10)=21 <=22; +" "+c(1+10)=32 >22 → break.
	// line2: c(10) + " "+d(11)=21 <=22.
	items := []*InlineItem{word("a", 10), word("b", 10), word("c", 10), word("d", 10)}
	lines := WrapItems(items, 22)
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	if lineText(lines[0]) != "a b" {
		t.Errorf("line0 = %q", lineText(lines[0]))
	}
	if lineText(lines[1]) != "c d" {
		t.Errorf("line1 = %q", lineText(lines[1]))
	}
}

func TestWrapItemsExactBoundary(t *testing.T) {
	// Exactly filling maxW must NOT overflow to a new line.
	items := []*InlineItem{word("a", 10), word("b", 11)} // 10 + 1 + 11 = 22
	lines := WrapItems(items, 22)
	if len(lines) != 1 {
		t.Fatalf("expected 1 line at exact fit, got %d", len(lines))
	}
}

func TestWrapItemsOverflowWord(t *testing.T) {
	// A single word wider than maxW sits alone; the next word starts a new line.
	items := []*InlineItem{word("huge", 100), word("x", 5)}
	lines := WrapItems(items, 20)
	if len(lines) != 2 || lineText(lines[0]) != "huge" || lineText(lines[1]) != "x" {
		t.Fatalf("overflow lines = %d %q/%q", len(lines), lineText(lines[0]), lineText(lines[1]))
	}
}

// TestWrapItemsGluedRunMovesTogether covers engine#149: a footnote-marker
// shape like `152,3<sup>†</sup>` is TWO InlineItems ("152,3" then a glued "†")
// but ONE unbreakable run — it must never split at the zero-SpaceBefore seam
// between them, moving to the next line together instead.
func TestWrapItemsGluedRunMovesTogether(t *testing.T) {
	// widths: AA=10, BB=10, glued CC=10 (glued to BB, SpaceBefore=0). maxW=21.
	// AA(10) placed first (no SpaceBefore counted). The run "BB CC" is
	// measured as ONE unit (20) before deciding whether it joins line 1:
	// 10 + SpaceBefore(1) + 20 = 31 > 21, so the WHOLE run moves to line 2 —
	// never "AA BB" on line 1 with "CC" split off alone.
	items := []*InlineItem{word("AA", 10), word("BB", 10), glued("CC", 10)}
	lines := WrapItems(items, 21)
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %v", len(lines), lines)
	}
	if lineText(lines[0]) != "AA" {
		t.Errorf("line0 = %q, want just \"AA\"", lineText(lines[0]))
	}
	if lineText(lines[1]) != "BB CC" {
		t.Errorf("line1 = %q, want \"BB CC\" together (not split at the glued seam)", lineText(lines[1]))
	}
}

// TestWrapItemsGluedRunFitsWithPriorContent covers the companion case: when
// the WHOLE run does fit alongside what's already on the line, it stays
// there — the fix must not force an unnecessary break either.
func TestWrapItemsGluedRunFitsWithPriorContent(t *testing.T) {
	// AA(10) + SpaceBefore(1) + run "BB CC"(20) = 31, fits exactly at maxW=31.
	items := []*InlineItem{word("AA", 10), word("BB", 10), glued("CC", 10)}
	lines := WrapItems(items, 31)
	if len(lines) != 1 || lineText(lines[0]) != "AA BB CC" {
		t.Fatalf("lines = %d %q, want one line \"AA BB CC\"", len(lines), lineText(lines[0]))
	}
}

// TestWrapItemsGluedRunOverflowsAsOneUnit covers a run wider than maxW even
// starting a fresh line: it is placed whole (overflowing), never split mid-run
// — the same "unbreakable unit overflows rather than splits" treatment a
// single too-wide word already gets (TestWrapItemsOverflowWord above).
func TestWrapItemsGluedRunOverflowsAsOneUnit(t *testing.T) {
	items := []*InlineItem{glued("BB", 10), glued("CC", 10), word("next", 5)}
	// The first item passed to WrapItems is never treated as having a
	// preceding item to glue TO, but glueRun still folds a FOLLOWING
	// zero-SpaceBefore item into its run regardless of position.
	lines := WrapItems(items, 15) // run "BB CC" = 20 > 15
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %v", len(lines), lines)
	}
	if lineText(lines[0]) != "BB CC" {
		t.Errorf("line0 = %q, want \"BB CC\" placed together despite overflowing maxW", lineText(lines[0]))
	}
	if lineText(lines[1]) != "next" {
		t.Errorf("line1 = %q, want \"next\"", lineText(lines[1]))
	}
}

func TestWrapItemsLineBreak(t *testing.T) {
	// A LineBreak ends the current line even when space remains, and an empty
	// line is produced for a double break.
	items := []*InlineItem{
		word("a", 5),
		{LineBreak: true},
		{LineBreak: true},
		word("b", 5),
	}
	lines := WrapItems(items, 1000)
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	if lineText(lines[0]) != "a" || len(lines[1].Items) != 0 || lineText(lines[2]) != "b" {
		t.Errorf("break lines = %q / %d / %q", lineText(lines[0]), len(lines[1].Items), lineText(lines[2]))
	}
}

// TestBalanceWidthKeepsLineCountButRedistributes guards balanceWidth's own
// core property (see its doc comment): re-wrapping at the width it returns
// must use the SAME line count as the caller's own already-computed n, but
// distribute content more evenly than the plain greedy wrap does. Widths:
// a-e each 20px, SpaceBefore 1 (word's own fixed value) → cumulative content
// widths after 1/2/3/4/5 words are 20/41/62/83/104. At maxW=90 greedy fits
// four words (83) then strands the fifth alone (20) — the classic "orphan"
// split; the narrowest width that STILL wraps to 2 lines is exactly 62 (the
// three-word cumulative), giving a much more even 62/41 split instead.
func TestBalanceWidthKeepsLineCountButRedistributes(t *testing.T) {
	items := []*InlineItem{word("a", 20), word("b", 20), word("c", 20), word("d", 20), word("e", 20)}
	greedy := WrapItems(items, 90)
	if len(greedy) != 2 {
		t.Fatalf("greedy setup: expected 2 lines, got %d", len(greedy))
	}
	if lineText(greedy[0]) != "a b c d" || lineText(greedy[1]) != "e" {
		t.Fatalf("greedy setup: unexpected split %q / %q", lineText(greedy[0]), lineText(greedy[1]))
	}

	w := balanceWidth(items, 90, 2)
	balanced := WrapItems(items, w)
	if len(balanced) != 2 {
		t.Fatalf("balanceWidth(...)=%v re-wraps to %d lines, want 2 (must preserve the greedy line count)", w, len(balanced))
	}
	if lineText(balanced[0]) != "a b c" || lineText(balanced[1]) != "d e" {
		t.Errorf("balanced split = %q / %q, want %q / %q", lineText(balanced[0]), lineText(balanced[1]), "a b c", "d e")
	}
}

// TestWrapItemsTrailingLineBreakGivesNoExtraLine guards WrapItems' own half
// of round 97's fix (the nowrap/text-wrap:balance fast path — see
// layoutInline's identical fix, and its own doc comment, for the confirmed
// live case and the general in-flow path's equivalent): a LineBreak with
// nothing after it must not open a further, empty line of its own, but an
// EARLIER break in the same trailing run still does (matching the real,
// visible blank line "a<br><br>b" already gets mid-content).
func TestWrapItemsTrailingLineBreakGivesNoExtraLine(t *testing.T) {
	one := WrapItems([]*InlineItem{word("a", 10), {LineBreak: true}}, 1000)
	if len(one) != 1 || lineText(one[0]) != "a" {
		t.Fatalf("a<br>: expected 1 line %q, got %d lines: %v", "a", len(one), one)
	}
	two := WrapItems([]*InlineItem{word("a", 10), {LineBreak: true}, {LineBreak: true}}, 1000)
	if len(two) != 2 || lineText(two[0]) != "a" || len(two[1].Items) != 0 {
		t.Fatalf("a<br><br>: expected [%q, <empty>], got %d lines: %v", "a", len(two), two)
	}
	// A LONE trailing break (no preceding content at all) still renders as
	// one real empty line — round 97's fix must not suppress this case too.
	lone := WrapItems([]*InlineItem{{LineBreak: true}}, 1000)
	if len(lone) != 1 || len(lone[0].Items) != 0 {
		t.Fatalf("<br> alone: expected 1 empty line, got %d lines: %v", len(lone), lone)
	}
}
