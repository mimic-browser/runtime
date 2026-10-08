// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package layout

import "testing"

// TestLineClampTruncatesLines covers the real shape found live on
// tailwindcss.com's own `<p class="line-clamp-2">` room-description text: an
// element whose inline content would naturally wrap to more lines than
// -webkit-line-clamp allows must be cut off at exactly N lines, shrinking the
// box to end where the Nth line does — not left at its full, unclamped
// height. fakeMeasurer gives every rune 10px and each line 20px height, so
// with a 100px-wide box, "aa aa aa aa aa aa aa aa" (8 two-letter words, each
// "aa "=30px incl. trailing space except the last) wraps to a known, fixed
// line count with no clamp, confirmed below before clamping it.
func TestLineClampTruncatesLines(t *testing.T) {
	text := "aa aa aa aa aa aa aa aa"
	unclamped := findBoxByID(layoutHTML(t, `<html><body style="margin:0">`+
		`<p id="x" style="width:100px">`+text+`</p>`+
		`</body></html>`, 500), "x")
	if unclamped == nil {
		t.Fatal("unclamped box not found")
	}
	naturalLines := len(unclamped.Lines)
	if naturalLines < 3 {
		t.Fatalf("test text wraps to only %d lines, want >=3 to make clamping meaningful", naturalLines)
	}

	clamped := findBoxByID(layoutHTML(t, `<html><body style="margin:0">`+
		`<p id="x" style="width:100px;-webkit-line-clamp:2;display:-webkit-box;-webkit-box-orient:vertical;overflow:hidden">`+text+`</p>`+
		`</body></html>`, 500), "x")
	if clamped == nil {
		t.Fatal("clamped box not found")
	}
	if len(clamped.Lines) != 2 {
		t.Fatalf("clamped Lines = %d, want exactly 2 (natural was %d)", len(clamped.Lines), naturalLines)
	}
	// The box's own height must end exactly where the 2nd line does, not the
	// unclamped content's full height.
	wantH := clamped.Lines[1].Y + clamped.Lines[1].H - clamped.ContentY
	assertF(t, "clamped.ContentH", clamped.ContentH, wantH)
	if clamped.ContentH >= unclamped.ContentH {
		t.Errorf("clamped height (%v) should be less than the unclamped height (%v)", clamped.ContentH, unclamped.ContentH)
	}
}

// TestLineClampNoOpWhenContentFitsWithinLimit confirms clamping a value
// GREATER than the content's own natural line count changes nothing — the
// truncation only ever removes lines that would otherwise overflow it.
func TestLineClampNoOpWhenContentFitsWithinLimit(t *testing.T) {
	text := "aa aa"
	unclamped := findBoxByID(layoutHTML(t, `<html><body style="margin:0">`+
		`<p id="x" style="width:500px">`+text+`</p>`+
		`</body></html>`, 500), "x")
	clamped := findBoxByID(layoutHTML(t, `<html><body style="margin:0">`+
		`<p id="x" style="width:500px;-webkit-line-clamp:5;display:-webkit-box;-webkit-box-orient:vertical;overflow:hidden">`+text+`</p>`+
		`</body></html>`, 500), "x")
	if len(unclamped.Lines) != len(clamped.Lines) {
		t.Fatalf("line-clamp with a limit above the natural line count changed the line count: %d vs %d", len(unclamped.Lines), len(clamped.Lines))
	}
	assertF(t, "unaffected ContentH", clamped.ContentH, unclamped.ContentH)
}
