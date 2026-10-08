// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package paint

import (
	"testing"

	"github.com/go-opentype/fonts/bitter"
	"github.com/go-opentype/fonts/cabin"
	"github.com/go-webengine/engine/css"
)

func named(names string, g css.Generic) css.FontFamily {
	return css.FontFamily{Names: names, Generic: g}
}

// A registered face is what the text is MEASURED with — the whole point, since
// a substitute at different metrics breaks the line somewhere else than its
// author saw. Cabin and Inter are both sans and measure a word differently.
func TestRegisteredFaceIsWhatMeasures(t *testing.T) {
	f := NewFonts()
	before := f.Measure("Partenaires", named("cabin", css.GenericSans), 40, 400, false)
	if err := f.Register("Cabin", 400, false, cabin.TTF); err != nil {
		t.Fatalf("Register: %v", err)
	}
	after := f.Measure("Partenaires", named("cabin", css.GenericSans), 40, 400, false)
	if before == after {
		t.Errorf("registering Cabin did not change the measurement (%v both times)", before)
	}
	// A family nothing registered still measures in the bundled bucket, so no
	// page that was working changes.
	if got := f.Measure("Partenaires", named("no such face", css.GenericSans), 40, 400, false); got != before {
		t.Errorf("an unregistered family measured %v, want the bundled %v", got, before)
	}
	if !f.Registered("cabin") || f.Registered("Spectral") {
		t.Error("Registered should see Cabin (case-insensitively) and not Spectral")
	}
}

// The declaration's ORDER decides: the first named family a face exists for
// wins, whatever follows it.
func TestNamedFamiliesAreTriedInOrder(t *testing.T) {
	f := NewFonts()
	if err := f.Register("bitter", 400, false, bitter.TTF); err != nil {
		t.Fatal(err)
	}
	bitterW := f.Measure("Ag", named("bitter", css.GenericSerif), 40, 400, false)
	firstWins := f.Measure("Ag", named("bitter,cabin", css.GenericSerif), 40, 400, false)
	if firstWins != bitterW {
		t.Errorf("first named family did not win: %v vs %v", firstWins, bitterW)
	}
	// And a name with no face is skipped for the next one that has.
	skipped := f.Measure("Ag", named("absent,bitter", css.GenericSerif), 40, 400, false)
	if skipped != bitterW {
		t.Errorf("an absent first name should fall through to bitter: %v vs %v", skipped, bitterW)
	}
}

// A slot the family does not ship falls back to that family's own regular,
// not to another typeface: the upright of the right face beats the italic of
// the wrong one, which is already how the bundled Go Mono behaves.
func TestNamedFamilyFallsBackToItsOwnRegular(t *testing.T) {
	f := NewFonts()
	if err := f.Register("cabin", 400, false, cabin.TTF); err != nil {
		t.Fatal(err)
	}
	reg := f.Measure("Ag", named("cabin", css.GenericSans), 40, 400, false)
	ital := f.Measure("Ag", named("cabin", css.GenericSans), 40, 400, true)
	if ital != reg {
		t.Errorf("italic request = %v, want the family's own regular %v", ital, reg)
	}
}

// Two rules competing for one slot: the bold slot's canonical weight is 700,
// so a 700 face displaces a 600 one and a second 700 does not displace it.
func TestRegisterKeepsTheAptestWeightForASlot(t *testing.T) {
	f := NewFonts()
	if err := f.Register("x", 600, false, cabin.TTF); err != nil {
		t.Fatal(err)
	}
	six := f.Measure("Ag", named("x", css.GenericSans), 40, 700, false)
	if err := f.Register("x", 700, false, bitter.TTF); err != nil {
		t.Fatal(err)
	}
	seven := f.Measure("Ag", named("x", css.GenericSans), 40, 700, false)
	if seven == six {
		t.Error("a 700 face should have displaced the 600 one in the bold slot")
	}
	// A further 600 must not take it back.
	if err := f.Register("x", 600, false, cabin.TTF); err != nil {
		t.Fatal(err)
	}
	if got := f.Measure("Ag", named("x", css.GenericSans), 40, 700, false); got != seven {
		t.Error("a 600 face displaced the 700 one in the bold slot")
	}
}

// Its refusals, and the weight default.
func TestRegisterRefusals(t *testing.T) {
	f := NewFonts()
	if err := f.Register("  ", 400, false, cabin.TTF); err == nil {
		t.Error("a blank family must be refused")
	}
	if err := f.Register("x", 400, false, []byte("not a font")); err == nil {
		t.Error("unparseable bytes must be refused")
	}
	// A rule with no usable weight lands in the regular slot.
	if err := f.Register("zero", 0, false, cabin.TTF); err != nil {
		t.Fatal(err)
	}
	if !f.Registered("zero") {
		t.Error("weight 0 should have registered as the regular")
	}
}

// FontFamilyOf names the key the bundled families live under.
func TestFontFamilyOf(t *testing.T) {
	for _, g := range []css.Generic{css.GenericSans, css.GenericSerif, css.GenericMono} {
		if got := FontFamilyOf(g); got.Generic != g || got.Names != "" {
			t.Errorf("FontFamilyOf(%v) = %+v", g, got)
		}
	}
}

func TestWeightDistance(t *testing.T) {
	for _, c := range []struct{ w, canon, want int }{{400, 400, 0}, {700, 400, 300}, {400, 700, 300}} {
		if got := weightDistance(c.w, c.canon); got != c.want {
			t.Errorf("weightDistance(%d,%d) = %d want %d", c.w, c.canon, got, c.want)
		}
	}
}
