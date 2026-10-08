// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package layout

import "testing"

// TestLetterSpacingWidensWordItem covers the real shape found live on
// tailwindcss.com's own hero heading (`tracking-tighter`, negative
// letter-spacing) and sidebar labels (`tracking-widest`, positive): a word's
// measured Width must grow (or shrink) by letter-spacing times its own
// character count, added after EVERY character including the last —
// fakeMeasurer gives every rune 10px, so "abc" (3 runes) at
// letter-spacing:2px must measure 30+3*2=36px.
func TestLetterSpacingWidensWordItem(t *testing.T) {
	p := findBoxByID(layoutHTML(t, `<html><body style="margin:0">`+
		`<p id="p" style="letter-spacing:2px">abc</p>`+
		`</body></html>`, 500), "p")
	if p == nil || len(p.Lines) != 1 || len(p.Lines[0].Items) != 1 {
		t.Fatalf("expected exactly one line with one item, got %+v", p)
	}
	if got, want := p.Lines[0].Items[0].Width, 36.0; got != want {
		t.Errorf("Width = %v, want %v (10*3 + 2*3)", got, want)
	}
}

// TestLetterSpacingNegativeNarrowsWordItem confirms a negative value (the
// `tracking-tighter`/`tracking-tight` shape) correctly SHRINKS the measured
// width, not just widens it.
func TestLetterSpacingNegativeNarrowsWordItem(t *testing.T) {
	p := findBoxByID(layoutHTML(t, `<html><body style="margin:0">`+
		`<p id="p" style="letter-spacing:-1px">abcd</p>`+
		`</body></html>`, 500), "p")
	if p == nil || len(p.Lines) != 1 || len(p.Lines[0].Items) != 1 {
		t.Fatalf("expected exactly one line with one item, got %+v", p)
	}
	if got, want := p.Lines[0].Items[0].Width, 36.0; got != want {
		t.Errorf("Width = %v, want %v (10*4 - 1*4)", got, want)
	}
}

// TestLetterSpacingZeroIsNoOp confirms the default (normal, 0) leaves a
// word's width exactly as the unmodified Measure result — no accidental
// per-character cost when the property is not in use at all.
func TestLetterSpacingZeroIsNoOp(t *testing.T) {
	p := findBoxByID(layoutHTML(t, `<html><body style="margin:0">`+
		`<p id="p">abc</p>`+
		`</body></html>`, 500), "p")
	if p == nil || len(p.Lines) != 1 || len(p.Lines[0].Items) != 1 {
		t.Fatalf("expected exactly one line with one item, got %+v", p)
	}
	if got, want := p.Lines[0].Items[0].Width, 30.0; got != want {
		t.Errorf("Width = %v, want %v (plain 10*3, no letter-spacing)", got, want)
	}
}

// TestLetterSpacingInheritsToChildText confirms the property is genuinely
// INHERITED (unlike vertical-align/text-overflow/line-clamp): a child span
// with no own letter-spacing declaration still picks it up from its parent.
func TestLetterSpacingInheritsToChildText(t *testing.T) {
	p := findBoxByID(layoutHTML(t, `<html><body style="margin:0">`+
		`<p id="p" style="letter-spacing:2px"><span id="s">abc</span></p>`+
		`</body></html>`, 500), "p")
	if p == nil || len(p.Lines) != 1 || len(p.Lines[0].Items) != 1 {
		t.Fatalf("expected exactly one line with one item, got %+v", p)
	}
	if got, want := p.Lines[0].Items[0].Width, 36.0; got != want {
		t.Errorf("child span's Width = %v, want %v (inherited letter-spacing:2px)", got, want)
	}
}

// TestLetterSpacingAppliesInPreformattedText confirms the white-space:pre
// path (a separate InlineItem-construction branch in appendWords) also
// applies letter-spacing, not just the ordinary word-splitting path.
func TestLetterSpacingAppliesInPreformattedText(t *testing.T) {
	p := findBoxByID(layoutHTML(t, `<html><body style="margin:0">`+
		`<p id="p" style="letter-spacing:2px;white-space:pre">abc</p>`+
		`</body></html>`, 500), "p")
	if p == nil || len(p.Lines) != 1 || len(p.Lines[0].Items) != 1 {
		t.Fatalf("expected exactly one line with one item, got %+v", p)
	}
	if got, want := p.Lines[0].Items[0].Width, 36.0; got != want {
		t.Errorf("pre-formatted Width = %v, want %v (10*3 + 2*3)", got, want)
	}
}
