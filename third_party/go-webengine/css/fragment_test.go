// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package css

import (
	"math"
	"strings"
	"testing"

	"github.com/go-webengine/engine/dom"
)

// TestBreakBeforeAfterValues covers every break-before / break-after value
// of CSS Fragmentation 3 through both the modern property and its legacy
// page-break-* alias: the page-only mapping (column/region values say
// nothing about pages and read as auto), the legacy `always` → page, and
// the CSS-wide initial/unset keywords. Each property must touch only its
// own edge.
func TestBreakBeforeAfterValues(t *testing.T) {
	cases := []struct {
		value string
		want  Break
	}{
		{"auto", BreakAuto},
		{"avoid", BreakAvoid},
		{"avoid-page", BreakAvoid},
		{"page", BreakPage},
		{"always", BreakPage},
		{"all", BreakPage},
		{"recto", BreakPage},
		{"verso", BreakPage},
		{"left", BreakLeft},
		{"right", BreakRight},
		{"column", BreakAuto},
		{"avoid-column", BreakAuto},
		{"region", BreakAuto},
		{"avoid-region", BreakAuto},
		{"initial", BreakAuto},
		{"unset", BreakAuto},
		{"PAGE", BreakPage},   // keywords are case-insensitive
		{"bogus", BreakRight}, // unrecognised: the declaration is ignored
	}
	for _, prop := range []string{"break-before", "page-break-before", "break-after", "page-break-after"} {
		after := strings.HasSuffix(prop, "after")
		for _, c := range cases {
			s := initialStyle()
			// Start from a non-auto value on BOTH edges so a mapping to auto
			// is observable and so the other edge's survival is checked.
			s.BreakBefore, s.BreakAfter = BreakRight, BreakRight
			s.apply(Declaration{Property: prop, Value: c.value}, 16, nil)
			got, other := s.BreakBefore, s.BreakAfter
			if after {
				got, other = s.BreakAfter, s.BreakBefore
			}
			if got != c.want {
				t.Errorf("%s: %s = %v want %v", prop, c.value, got, c.want)
			}
			if other != BreakRight {
				t.Errorf("%s: %s changed the other edge to %v", prop, c.value, other)
			}
		}
	}
}

func TestBreakInsideValues(t *testing.T) {
	cases := []struct {
		value string
		want  BreakInside
	}{
		{"auto", BreakInsideAuto},
		{"avoid", BreakInsideAvoid},
		{"avoid-page", BreakInsideAvoid},
		{"avoid-column", BreakInsideAuto},
		{"avoid-region", BreakInsideAuto},
		{"initial", BreakInsideAuto},
		{"unset", BreakInsideAuto},
		{"AVOID", BreakInsideAvoid},
		{"page", BreakInsideAvoid}, // not a break-inside value: ignored, Avoid stands
	}
	for _, prop := range []string{"break-inside", "page-break-inside"} {
		for _, c := range cases {
			s := initialStyle()
			s.BreakInside = BreakInsideAvoid
			s.apply(Declaration{Property: prop, Value: c.value}, 16, nil)
			if s.BreakInside != c.want {
				t.Errorf("%s: %s = %v want %v", prop, c.value, s.BreakInside, c.want)
			}
		}
	}
}

// TestOrphansWidowsValues: a positive integer sets the value; zero, a
// negative number and a non-integer are invalid and leave the declaration
// ignored (CSS Fragmentation 3 §4); initial is 2.
func TestOrphansWidowsValues(t *testing.T) {
	cases := []struct {
		value string
		want  int
	}{
		{"3", 3},
		{"+4", 4},
		{"1", 1},
		{"initial", 2},
		{"0", 7},
		{"-1", 7},
		{"2.5", 7},
		{"abc", 7},
		{"1e1", 7},
	}
	for _, prop := range []string{"orphans", "widows"} {
		for _, c := range cases {
			s := initialStyle()
			s.Orphans, s.Widows = 7, 7
			s.apply(Declaration{Property: prop, Value: c.value}, 16, nil)
			got, other := s.Orphans, s.Widows
			if prop == "widows" {
				got, other = s.Widows, s.Orphans
			}
			if got != c.want {
				t.Errorf("%s: %s = %d want %d", prop, c.value, got, c.want)
			}
			if other != 7 {
				t.Errorf("%s: %s changed the other property to %d", prop, c.value, other)
			}
		}
	}
}

// TestOrphansWidowsUnsetAndInherit: unset is inherit for these inherited
// properties (the parent's value); with no parent style known (a bare
// apply, as the property tests do) neither unset nor inherit can resolve
// and the value stands.
func TestOrphansWidowsUnsetAndInherit(t *testing.T) {
	parent := initialStyle()
	parent.Orphans, parent.Widows = 5, 6
	s := initialStyle()
	s.apply(Declaration{Property: "orphans", Value: "unset"}, 16, &parent)
	s.apply(Declaration{Property: "widows", Value: "UNSET"}, 16, &parent)
	if s.Orphans != 5 || s.Widows != 6 {
		t.Errorf("unset with parent: orphans %d widows %d, want 5 6", s.Orphans, s.Widows)
	}
	s = initialStyle()
	s.Orphans, s.Widows = 7, 7
	s.apply(Declaration{Property: "orphans", Value: "unset"}, 16, nil)
	s.apply(Declaration{Property: "widows", Value: "unset"}, 16, nil)
	s.apply(Declaration{Property: "orphans", Value: "inherit"}, 16, nil)
	if s.Orphans != 7 || s.Widows != 7 {
		t.Errorf("unset/inherit without parent: orphans %d widows %d, want 7 7", s.Orphans, s.Widows)
	}
}

// TestFragmentationInheritance runs the real cascade: orphans/widows start
// at 2 on every element and are inherited by a child block; the three break
// properties are not inherited and reset to auto on the child.
func TestFragmentationInheritance(t *testing.T) {
	root, err := dom.Parse(`<html><body>` +
		`<div id="d" style="orphans:3;widows:4;break-before:page;break-after:left;break-inside:avoid">` +
		`<p id="p">x<span id="s">y</span></p></div></body></html>`)
	if err != nil {
		t.Fatal(err)
	}
	sm := Cascade(root)
	body := sm[dom.Find(root, "body")]
	if body.Orphans != 2 || body.Widows != 2 {
		t.Errorf("body orphans/widows = %d/%d, want the initial 2/2", body.Orphans, body.Widows)
	}
	if body.BreakBefore != BreakAuto || body.BreakAfter != BreakAuto || body.BreakInside != BreakInsideAuto {
		t.Errorf("body break-* = %v/%v/%v, want auto", body.BreakBefore, body.BreakAfter, body.BreakInside)
	}
	d := sm[dom.Find(root, "div")]
	if d.Orphans != 3 || d.Widows != 4 {
		t.Errorf("div orphans/widows = %d/%d, want 3/4", d.Orphans, d.Widows)
	}
	if d.BreakBefore != BreakPage || d.BreakAfter != BreakLeft || d.BreakInside != BreakInsideAvoid {
		t.Errorf("div break-* = %v/%v/%v, want page/left/avoid", d.BreakBefore, d.BreakAfter, d.BreakInside)
	}
	for _, tag := range []string{"p", "span"} {
		c := sm[dom.Find(root, tag)]
		if c.Orphans != 3 || c.Widows != 4 {
			t.Errorf("%s orphans/widows = %d/%d, want the inherited 3/4", tag, c.Orphans, c.Widows)
		}
		if c.BreakBefore != BreakAuto || c.BreakAfter != BreakAuto || c.BreakInside != BreakInsideAuto {
			t.Errorf("%s break-* = %v/%v/%v, want auto (not inherited)", tag, c.BreakBefore, c.BreakAfter, c.BreakInside)
		}
	}
}

// TestFragmentationInheritKeyword: an explicit `inherit` copies the parent's
// computed value for every one of the properties, through both spellings.
func TestFragmentationInheritKeyword(t *testing.T) {
	root, err := dom.Parse(`<html><body>` +
		`<div style="break-before:page;break-after:left;break-inside:avoid;orphans:3;widows:4">` +
		`<p id="a" style="orphans:9;widows:9;break-before:inherit;break-after:inherit;break-inside:inherit;orphans:inherit;widows:inherit"></p>` +
		`<p id="b" style="page-break-before:inherit;page-break-after:inherit;page-break-inside:inherit"></p>` +
		`</div></body></html>`)
	if err != nil {
		t.Fatal(err)
	}
	sm := Cascade(root)
	for _, id := range []string{"a", "b"} {
		s := sm[findByID(root, id)]
		if s.BreakBefore != BreakPage || s.BreakAfter != BreakLeft || s.BreakInside != BreakInsideAvoid {
			t.Errorf("#%s break-* = %v/%v/%v, want the inherited page/left/avoid", id, s.BreakBefore, s.BreakAfter, s.BreakInside)
		}
		if s.Orphans != 3 || s.Widows != 4 {
			t.Errorf("#%s orphans/widows = %d/%d, want the inherited 3/4", id, s.Orphans, s.Widows)
		}
	}
}

// TestParseLengthAbsoluteUnits is the CSS Values 4 §6.2 table: 1in = 96px =
// 2.54cm = 25.4mm = 72pt = 6pc, 1Q = ¼mm. Before, every unit but px fell
// through unrecognised.
func TestParseLengthAbsoluteUnits(t *testing.T) {
	cases := []struct {
		in   string
		want float64
	}{
		{"40mm", 151.1811},
		{"4cm", 151.1811},
		{"1.5in", 144},
		{"100pt", 133.3333},
		{"10pc", 160},
		{"40q", 37.7953},
		{"40Q", 37.7953},
		{"151px", 151},
		{"12pt", 16},
		{"1E1PX", 10},
		{"1e3px", 1000},
		{"10 mm", 37.7953},
		{"-5mm", -18.8976},
		{"0", 0},
	}
	for _, c := range cases {
		l, ok := parseLength(c.in, 16)
		if !ok || l.Auto || l.IsPercent || math.Abs(l.Px-c.want) > 0.001 {
			t.Errorf("parseLength(%q) = %+v, %v; want Px %.4f", c.in, l, ok, c.want)
		}
	}
	// Not lengths: a keyword whose tail happens to spell a unit, a bare
	// number, an unknown unit, and a known unit on a malformed number.
	for _, in := range []string{"thin", "min", "10", "10xx", "1..2mm", "mm", "in"} {
		if l, ok := parseLength(in, 16); ok {
			t.Errorf("parseLength(%q) = %+v, want not a length", in, l)
		}
	}
}

// TestAbsoluteUnitsThroughProperties: the units reach every property that
// resolves a length through parseLength — font-size (12pt is exactly 16px),
// a calc() term, and the flex shorthand's basis (hasUnit must recognise
// them so `10mm` is a basis, not a flex factor).
func TestAbsoluteUnitsThroughProperties(t *testing.T) {
	root, err := dom.Parse(`<html><body><p style="font-size:12pt;width:calc(10mm + 1cm);margin-top:1in">x</p></body></html>`)
	if err != nil {
		t.Fatal(err)
	}
	p := Cascade(root)[dom.Find(root, "p")]
	if p.FontSize != 16 {
		t.Errorf("font-size:12pt = %v px, want 16", p.FontSize)
	}
	if math.Abs(p.Width.Px-mmPx(20)) > 0.001 {
		t.Errorf("width:calc(10mm + 1cm) = %v px, want %.4f", p.Width.Px, mmPx(20))
	}
	if p.Margin.Top != 96 {
		t.Errorf("margin-top:1in = %v px, want 96", p.Margin.Top)
	}
	var s Style
	s.apply(Declaration{Property: "flex", Value: "2 1 10mm"}, 16, nil)
	if s.FlexGrow != 2 || s.FlexShrink != 1 || math.Abs(s.FlexBasis.Px-mmPx(10)) > 0.001 {
		t.Errorf("flex: 2 1 10mm = grow %v shrink %v basis %+v", s.FlexGrow, s.FlexShrink, s.FlexBasis)
	}
	for _, f := range []string{"10mm", "2in", "3q", "auto", "5%", "1.5em"} {
		if !hasUnit(f) {
			t.Errorf("hasUnit(%q) = false", f)
		}
	}
	for _, f := range []string{"0", "2", "thin", "1x"} {
		if hasUnit(f) {
			t.Errorf("hasUnit(%q) = true", f)
		}
	}
}
