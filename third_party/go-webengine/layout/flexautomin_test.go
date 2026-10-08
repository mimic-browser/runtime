// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package layout

import "testing"

// TestFlexShrinkAutomaticMinimumSize covers CSS's own "automatic minimum
// size": a flex item whose min-width is auto (the default) must never
// shrink below its own min-content width. "aa bb cc" can shrink down to its
// own min-content (the widest single word, "aa"=20px) but no further;
// "pricing" is ONE unbreakable word, so its min-content equals its
// max-content (70px) and it must not shrink at all. Confirmed live on
// github.com/golang/go's own marketing nav: without this floor, the shrink
// phase squeezed a single-word item ("Pricing") straight through zero,
// dropping it from the rendered page entirely.
func TestFlexShrinkAutomaticMinimumSize(t *testing.T) {
	src := `<html><body style="margin:0"><div style="display:flex;width:100px">` +
		`<div>aa bb cc</div><div>pricing</div></div></body></html>`
	outer := findBox(layoutHTML(t, src, 400), "div")
	assertF(t, "shrinkable.W", outer.Children[0].W, 30)
	assertF(t, "unbreakable.W", outer.Children[1].W, 70)
}

// TestFlexShrinkAutomaticMinimumSizeIgnoredWhenOverflowHidden confirms
// spec's own carve-out: an item with overflow:hidden (or scroll/auto) opts
// BACK into the old floor-at-0 behaviour, since clipped content has nothing
// left to protect — the automatic minimum only applies when overflow is
// visible (the default).
func TestFlexShrinkAutomaticMinimumSizeIgnoredWhenOverflowHidden(t *testing.T) {
	src := `<html><body style="margin:0"><div style="display:flex;width:100px">` +
		`<div>aa bb cc</div><div style="overflow:hidden">pricing</div></div></body></html>`
	outer := findBox(layoutHTML(t, src, 400), "div")
	// Both items now shrink freely: total 150 must fit 100, split evenly
	// (both single-word "run"s from a min-content perspective aren't at play
	// here — overflow:hidden simply removes the unbreakable item's own
	// floor, so it shrinks by its fair share like any other flexible item).
	if got := outer.Children[1].W; got >= 70 {
		t.Errorf("overflow:hidden item.W = %v, want less than 70 (its own floor must NOT apply)", got)
	}
}

// TestFlexShrinkAutomaticMinimumSizeIgnoredWhenMinWidthZero confirms an
// author can still opt back into the pre-fix floor-at-0 behaviour explicitly
// with min-width:0 — the automatic minimum only fills in for the auto
// default, it does not override an explicit author declaration.
func TestFlexShrinkAutomaticMinimumSizeIgnoredWhenMinWidthZero(t *testing.T) {
	src := `<html><body style="margin:0"><div style="display:flex;width:100px">` +
		`<div>aa bb cc</div><div style="min-width:0">pricing</div></div></body></html>`
	outer := findBox(layoutHTML(t, src, 400), "div")
	if got := outer.Children[1].W; got >= 70 {
		t.Errorf("min-width:0 item.W = %v, want less than 70 (explicit min-width:0 must win over the automatic floor)", got)
	}
}
