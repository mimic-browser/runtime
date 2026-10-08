// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package paint

import (
	"image"
	"os"
	"testing"

	"github.com/go-webengine/engine/css"
)

// materialIconsFamily registers the real Material Icons font (Google Fonts,
// Apache License 2.0 — see testdata/materialicons-LICENSE.txt) fetched live
// from fonts.gstatic.com, the exact file go.dev/blog itself loads for its
// nav-menu dropdown carets. It is the real-world case that exposed this
// engine painting nothing at all for a whole class of @font-face fonts: one
// whose cmap maps ordinary ASCII letters to individual (non-.notdef, so not
// skipped) glyphs, and whose GSUB "rlig" (Required Ligatures — see
// requiredLigatureFeature) feature substitutes a whole word, such as
// "arrow_drop_down", into one pictogram glyph.
func materialIconsFamily(t *testing.T) (*Fonts, css.FontFamily) {
	t.Helper()
	data, err := os.ReadFile("testdata/materialicons-arrow_drop_down.woff2")
	if err != nil {
		t.Skip(err)
	}
	f := NewFonts()
	if err := f.Register("Material Icons Test", 400, false, data); err != nil {
		t.Fatalf("Register: %v", err)
	}
	// Register lowercases the family it stores under (it matches a
	// font-family declaration case-insensitively); NamedFamilies does not
	// lowercase what it returns, so a FontFamily built by hand — unlike one
	// css/parse.go produces from a real declaration — must already be
	// lowercase to resolve to what Register just stored, or font() silently
	// falls through to the Generic bucket (Inter) instead.
	return f, css.FontFamily{Names: "material icons test", Generic: css.GenericSans}
}

// TestRequiredLigatureCollapsesWordToOneGlyph is the Measure side of the
// go.dev/blog Material Icons fix: before requiredLigatureFeature shaping,
// Measure summed one glyph advance per ASCII letter of "arrow_drop_down" (15
// of them, each a real, non-.notdef glyph in this font's cmap — see
// requiredLigatureFeature's own doc comment for why that is not a skippable
// case), instead of the one substituted icon glyph a real browser measures.
func TestRequiredLigatureCollapsesWordToOneGlyph(t *testing.T) {
	f, fam := materialIconsFamily(t)
	one := f.Measure("a", fam, 48, 400, false) // one glyph, no ligature involved
	word := f.Measure("arrow_drop_down", fam, 48, 400, false)
	naive := 15 * one // what the old per-rune loop would have measured
	if word != one {
		t.Errorf(`Measure("arrow_drop_down") = %v, want exactly one glyph's advance (%v, matching Measure("a")) — the rlig substitution collapsed 15 runes to 1 glyph`, word, one)
	}
	if word >= naive-1e-9 {
		t.Errorf(`Measure("arrow_drop_down") = %v did not shrink from the unshaped 15-glyph width %v`, word, naive)
	}
}

// TestRequiredLigaturePaintsAVisibleGlyph is the drawText side: before this
// fix, go.dev/blog's own live nav rendered this exact word as nothing at all
// (see FIDELITY.md) — every one of the 15 ASCII-letter glyphs the old
// per-rune loop drew was real but blank, a deliberate authoring convention
// so a renderer that skips rlig shows nothing rather than mangled tofu-like
// letter shapes. Shaping first and painting by glyph index (GlyphMaskIndex)
// draws the one substituted icon glyph instead.
func TestRequiredLigaturePaintsAVisibleGlyph(t *testing.T) {
	f, fam := materialIconsFamily(t)
	dst := white(80, 80)
	pp := newTestPainter(dst)
	st := &css.Style{FontFamily: fam, FontSize: 48, FontWeight: 400, Color: css.Color{A: 255}}
	drawText(dst, pp, f, st, "arrow_drop_down", 4, 60, st.Color, dst.Bounds())
	if !hasDarkInk(dst, image.Rect(0, 0, 80, 80)) {
		t.Error("drawText painted no ink at all for a required-ligature icon glyph")
	}
}

// TestRequiredLigatureSkipsNotdefLikeAnUnshapedUnmappedRune covers the
// gid == 0 guard drawText and Measure both added: Face.Shape maps a rune
// neither the primary nor the fallback face's cmap covers to glyph 0
// (.notdef) rather than dropping it, so shaping must not start painting
// tofu boxes (or measuring their width) for exactly the runes this engine
// has always rendered as nothing — see fallback_test.go's
// TestMeasureIsExactAndLinear, which covers the same contract on the Measure
// side with the same rune.
func TestRequiredLigatureSkipsNotdefLikeAnUnshapedUnmappedRune(t *testing.T) {
	f := NewFonts()
	dst := white(40, 40)
	pp := newTestPainter(dst)
	st := &css.Style{FontFamily: css.Sans, FontSize: 20, FontWeight: 400, Color: css.Color{A: 255}}
	end := drawText(dst, pp, f, st, "中", 2, 22, st.Color, dst.Bounds())
	if end != 2 {
		t.Errorf("drawText advanced the pen by %d for an uncovered rune, want 0 (pen stays at x=2)", end-2)
	}
	if hasDarkInk(dst, image.Rect(0, 0, 40, 40)) {
		t.Error("drawText painted ink for a rune neither face covers")
	}
}
