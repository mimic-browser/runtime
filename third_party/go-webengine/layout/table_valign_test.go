// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package layout

import "testing"

// TestTableCellVerticalAlignMiddle/Bottom/Top cover round 147's fix: a table
// cell shorter than its own row (set here via an explicit height on a
// sibling cell, the simplest way to force a tall row deterministically)
// shifts its CONTENT down per `vertical-align`, while the cell's own box
// still spans the full row height — confirmed load-bearing live on
// news.ycombinator.com's own top-nav table (an 18px logo centred against a
// shorter line of nav text, see css/ua.go's own td/th UA-default comment).
func TestTableCellVerticalAlignMiddle(t *testing.T) {
	box := layoutHTML(t, `<html><body style="margin:0"><table><tr>`+
		`<td style="height:100px">tall</td>`+
		`<td id="c" style="vertical-align:middle">x</td>`+
		`</tr></table></body></html>`, 500)
	td := findBoxByID(box, "c")
	if td == nil {
		t.Fatal("no td#c")
	}
	if td.H < 99 {
		t.Fatalf("td#c.H = %v, want stretched to ~the tall sibling's row height", td.H)
	}
	if len(td.Lines) == 0 {
		t.Fatal("td#c has no lines")
	}
	// The cell's own box starts at the row top; its content line must sit
	// LOWER than that top edge, roughly centred in the available slack.
	if td.Lines[0].Y <= td.Y {
		t.Errorf("td#c line Y = %v, want > box top Y = %v (content shifted down for middle)", td.Lines[0].Y, td.Y)
	}
	// naturalH: fakeMeasurer's 20px line height + the UA-default 1px top/
	// bottom cell padding (css/ua.go's td rule) = 22.
	slack := td.H - 22
	wantY := td.Y + 1 + slack/2
	if got := td.Lines[0].Y; got != wantY {
		t.Errorf("td#c line Y = %v, want %v (centred in the row's slack)", got, wantY)
	}
}

func TestTableCellVerticalAlignBottom(t *testing.T) {
	box := layoutHTML(t, `<html><body style="margin:0"><table><tr>`+
		`<td style="height:100px">tall</td>`+
		`<td id="c" style="vertical-align:bottom">x</td>`+
		`</tr></table></body></html>`, 500)
	td := findBoxByID(box, "c")
	if td == nil {
		t.Fatal("no td#c")
	}
	if len(td.Lines) == 0 {
		t.Fatal("td#c has no lines")
	}
	// Bottom-aligned content sits at the very bottom of the stretched box —
	// strictly lower than middle would place it.
	slack := td.H - 22 // see TestTableCellVerticalAlignMiddle's own comment
	wantY := td.Y + 1 + slack
	if got := td.Lines[0].Y; got != wantY {
		t.Errorf("td#c line Y = %v, want %v (flush with the row's own bottom)", got, wantY)
	}
}

// TestTableCellVerticalAlignTopIsUnaffected covers the pre-existing,
// unchanged default: top (and the UA-default-overriding case where a
// shorter sibling doesn't force any slack at all) leaves content flush with
// the cell's own top edge — the behaviour every table layout had before
// this round, which this fix must not disturb.
func TestTableCellVerticalAlignTopIsUnaffected(t *testing.T) {
	box := layoutHTML(t, `<html><body style="margin:0"><table><tr>`+
		`<td style="height:100px">tall</td>`+
		`<td id="c" style="vertical-align:top">x</td>`+
		`</tr></table></body></html>`, 500)
	td := findBoxByID(box, "c")
	if td == nil {
		t.Fatal("no td#c")
	}
	if len(td.Lines) == 0 {
		t.Fatal("td#c has no lines")
	}
	// +1 for the UA-default 1px cell padding (css/ua.go's td rule) between
	// the box's own top edge and where its content actually starts.
	if got, want := td.Lines[0].Y, td.Y+1; got != want {
		t.Errorf("td#c (vertical-align:top) line Y = %v, want %v (flush with box top, plus the 1px UA padding)", got, want)
	}
}

// TestTableCellsSameHeightNoShift covers the slack<=0 path: when every cell
// in a row naturally has the SAME height (the common case — no explicit
// height difference), vertical-align:middle/bottom on a cell must be a
// complete no-op, not accidentally shift content off its own box.
func TestTableCellsSameHeightNoShift(t *testing.T) {
	box := layoutHTML(t, `<html><body style="margin:0"><table><tr>`+
		`<td>a</td><td id="c" style="vertical-align:bottom">x</td>`+
		`</tr></table></body></html>`, 500)
	td := findBoxByID(box, "c")
	if td == nil {
		t.Fatal("no td#c")
	}
	if len(td.Lines) == 0 {
		t.Fatal("td#c has no lines")
	}
	if got, want := td.Lines[0].Y, td.Y+1; got != want { // +1: the UA-default 1px cell padding
		t.Errorf("td#c line Y = %v, want %v (no slack to distribute, no shift)", got, want)
	}
}
