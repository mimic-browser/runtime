// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package css

import (
	"math"
	"testing"
)

func TestParseFilterListNoneAndDispatch(t *testing.T) {
	// The `filter` shorthand dispatches through Style.apply.
	s := &Style{}
	s.apply(Declaration{Property: "filter", Value: "grayscale(50%) blur(3px)"}, 16, nil)
	if len(s.Filters) != 2 {
		t.Fatalf("filter chain = %+v", s.Filters)
	}
	if s.Filters[0].Kind != FilterGrayscale || s.Filters[0].Amount != 0.5 {
		t.Errorf("first = %+v", s.Filters[0])
	}
	if s.Filters[1].Kind != FilterBlur || s.Filters[1].Amount != 3 {
		t.Errorf("second = %+v", s.Filters[1])
	}
	// `none` clears the chain.
	s.apply(Declaration{Property: "filter", Value: "none"}, 16, nil)
	if s.Filters != nil {
		t.Errorf("none did not clear: %+v", s.Filters)
	}
	// A malformed chain leaves the property untouched (does not overwrite).
	s2 := &Style{Filters: []Filter{{Kind: FilterInvert, Amount: 1}}}
	s2.apply(Declaration{Property: "filter", Value: "blur(3px) bogus(2)"}, 16, nil)
	if len(s2.Filters) != 1 || s2.Filters[0].Kind != FilterInvert {
		t.Errorf("malformed chain overwrote: %+v", s2.Filters)
	}
}

// TestParseBackdropFilterDispatch covers backdrop-filter's own Style field
// (BackdropFilters, kept separate from Filters — see its own doc comment) and
// the -webkit-backdrop-filter alias real generated CSS (Tailwind) emits
// alongside the standard spelling.
func TestParseBackdropFilterDispatch(t *testing.T) {
	s := &Style{}
	s.apply(Declaration{Property: "backdrop-filter", Value: "blur(16px) saturate(2)"}, 16, nil)
	if len(s.BackdropFilters) != 2 || s.Filters != nil {
		t.Fatalf("backdrop-filter chain = %+v, Filters = %+v", s.BackdropFilters, s.Filters)
	}
	if s.BackdropFilters[0].Kind != FilterBlur || s.BackdropFilters[0].Amount != 16 {
		t.Errorf("first = %+v", s.BackdropFilters[0])
	}
	if s.BackdropFilters[1].Kind != FilterSaturate || s.BackdropFilters[1].Amount != 2 {
		t.Errorf("second = %+v", s.BackdropFilters[1])
	}
	// The -webkit- alias sets the SAME field, matching the real generated CSS
	// shape (-webkit- declared first, standard spelling immediately after —
	// ordinary cascade order then makes the standard one win, unchanged here
	// since it is the only declaration in this test).
	s2 := &Style{}
	s2.apply(Declaration{Property: "-webkit-backdrop-filter", Value: "blur(4px)"}, 16, nil)
	if len(s2.BackdropFilters) != 1 || s2.BackdropFilters[0].Kind != FilterBlur || s2.BackdropFilters[0].Amount != 4 {
		t.Errorf("-webkit-backdrop-filter = %+v", s2.BackdropFilters)
	}
}

func TestParseFilterListInvalid(t *testing.T) {
	for _, v := range []string{"", "bogus(1)", "blur", "blur(1px) unknown(2)", "brightness(x)"} {
		if fs, ok := parseFilterList(v, 16); ok {
			t.Errorf("parseFilterList(%q) unexpectedly ok: %+v", v, fs)
		}
	}
}

func TestParseFilterFuncKinds(t *testing.T) {
	cases := []struct {
		in   string
		kind FilterKind
		amt  float64
	}{
		{"blur(4px)", FilterBlur, 4},
		{"brightness(1.5)", FilterBrightness, 1.5},
		{"brightness(50%)", FilterBrightness, 0.5},
		{"contrast(2)", FilterContrast, 2},
		{"saturate(200%)", FilterSaturate, 2},
		{"grayscale(0.25)", FilterGrayscale, 0.25},
		{"sepia(100%)", FilterSepia, 1},
		{"invert(0.4)", FilterInvert, 0.4},
		{"hue-rotate(90deg)", FilterHueRotate, math.Pi / 2},
	}
	for _, c := range cases {
		f, ok := parseFilterFunc(c.in, 16)
		if !ok {
			t.Errorf("parseFilterFunc(%q) failed", c.in)
			continue
		}
		if f.Kind != c.kind || math.Abs(f.Amount-c.amt) > 1e-9 {
			t.Errorf("parseFilterFunc(%q) = kind %v amt %v, want %v %v", c.in, f.Kind, f.Amount, c.kind, c.amt)
		}
	}
}

func TestParseFilterFuncDefaultsAndClamps(t *testing.T) {
	// Empty-argument defaults.
	defs := map[string]float64{
		"blur()": 0, "brightness()": 1, "contrast()": 1, "saturate()": 1,
		"grayscale()": 1, "sepia()": 1, "invert()": 1, "hue-rotate()": 0,
	}
	for in, want := range defs {
		f, ok := parseFilterFunc(in, 16)
		if !ok || f.Amount != want {
			t.Errorf("parseFilterFunc(%q) = %v,%v want amount %v", in, f.Amount, ok, want)
		}
	}
	// grayscale/sepia/invert clamp above 1; all clamp below 0.
	if f, _ := parseFilterFunc("grayscale(3)", 16); f.Amount != 1 {
		t.Errorf("grayscale(3) = %v want 1", f.Amount)
	}
	if f, _ := parseFilterFunc("brightness(-1)", 16); f.Amount != 0 {
		t.Errorf("brightness(-1) = %v want 0", f.Amount)
	}
	// brightness/contrast/saturate are NOT clamped above 1.
	if f, _ := parseFilterFunc("brightness(4)", 16); f.Amount != 4 {
		t.Errorf("brightness(4) = %v want 4", f.Amount)
	}
}

func TestParseFilterFuncMalformed(t *testing.T) {
	for _, in := range []string{
		"blur", "blur(", "blur 4px)", "unknown(1)",
		"blur(-2px)", "blur(50%)", "blur(auto)",
		"brightness(nan%)", "hue-rotate(banana)", "contrast(x)",
	} {
		if f, ok := parseFilterFunc(in, 16); ok {
			t.Errorf("parseFilterFunc(%q) unexpectedly ok: %+v", in, f)
		}
	}
}

func TestParseDropShadowFunc(t *testing.T) {
	// Two lengths + explicit colour.
	f, ok := parseFilterFunc("drop-shadow(4px 6px red)", 16)
	if !ok || f.Kind != FilterDropShadow {
		t.Fatalf("drop-shadow parse = %+v ok=%v", f, ok)
	}
	if f.OffsetX != 4 || f.OffsetY != 6 || f.Blur != 0 {
		t.Errorf("offsets = %+v", f)
	}
	if f.UseCurrentColor || f.Color != (Color{255, 0, 0, 255}) {
		t.Errorf("colour = %+v useCurrent=%v", f.Color, f.UseCurrentColor)
	}
	// Three lengths (blur) + no colour → currentColor.
	f2, ok := parseFilterFunc("drop-shadow(2px 3px 5px)", 16)
	if !ok || f2.Blur != 5 || !f2.UseCurrentColor {
		t.Errorf("drop-shadow blur/current = %+v ok=%v", f2, ok)
	}
	// Explicit currentColor keyword.
	f3, _ := parseFilterFunc("drop-shadow(1px 1px currentColor)", 16)
	if !f3.UseCurrentColor {
		t.Errorf("currentColor keyword not honoured: %+v", f3)
	}
	// Malformed drop-shadows.
	for _, in := range []string{
		"drop-shadow(4px)",             // <2 lengths
		"drop-shadow(1px 2px 3px 4px)", // >3 lengths
		"drop-shadow(1px 2px -3px)",    // negative blur
		"drop-shadow(1px 2px bogus)",   // unknown token
	} {
		if f, ok := parseFilterFunc(in, 16); ok {
			t.Errorf("parseFilterFunc(%q) unexpectedly ok: %+v", in, f)
		}
	}
}

func TestParseNumberOrPercent(t *testing.T) {
	cases := map[string]struct {
		v  float64
		ok bool
	}{
		"50%":  {0.5, true},
		"1.5":  {1.5, true},
		"  2 ": {2, true},
		"x%":   {0, false},
		"abc":  {0, false},
	}
	for in, want := range cases {
		v, ok := parseNumberOrPercent(in)
		if ok != want.ok || (ok && v != want.v) {
			t.Errorf("parseNumberOrPercent(%q) = %v,%v want %v,%v", in, v, ok, want.v, want.ok)
		}
	}
}

func TestParseFilterListLeavesUnknownPropUntouched(t *testing.T) {
	// hue-rotate accepts turn/grad/rad units via the shared parseAngle.
	for _, c := range []struct {
		in  string
		rad float64
	}{
		{"hue-rotate(0.5turn)", math.Pi},
		{"hue-rotate(200grad)", math.Pi},
		{"hue-rotate(1rad)", 1},
		{"hue-rotate(0)", 0},
	} {
		f, ok := parseFilterFunc(c.in, 16)
		if !ok || math.Abs(f.Amount-c.rad) > 1e-9 {
			t.Errorf("%q = %v,%v want %v", c.in, f.Amount, ok, c.rad)
		}
	}
}
