// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package css

import (
	"testing"
)

func TestApplyPositionValues(t *testing.T) {
	cases := map[string]Position{
		"static":   PositionStatic,
		"relative": PositionRelative,
		"absolute": PositionAbsolute,
		"fixed":    PositionFixed,
		"sticky":   PositionSticky,
	}
	for v, want := range cases {
		s := newStyle()
		applyOn(s, "position", v, 16)
		if s.Position != want {
			t.Errorf("position:%s = %v want %v", v, s.Position, want)
		}
	}
	// An unknown value leaves the initial (static).
	s := newStyle()
	applyOn(s, "position", "wat", 16)
	if s.Position != PositionStatic {
		t.Errorf("position:wat = %v want static", s.Position)
	}
}

func TestApplyOffsets(t *testing.T) {
	s := newStyle()
	// Initially every offset is auto.
	for _, l := range []Length{s.Top, s.Right, s.Bottom, s.Left} {
		if !l.Auto {
			t.Fatalf("initial offset should be auto, got %+v", l)
		}
	}
	applyOn(s, "top", "10px", 16)
	applyOn(s, "right", "2em", 16)
	applyOn(s, "bottom", "50%", 16)
	applyOn(s, "left", "auto", 16)
	if s.Top.Px != 10 {
		t.Errorf("top = %+v", s.Top)
	}
	if s.Right.Px != 32 { // 2em @ 16
		t.Errorf("right = %+v", s.Right)
	}
	if !s.Bottom.IsPercent || s.Bottom.Percent != 0.5 {
		t.Errorf("bottom = %+v", s.Bottom)
	}
	if !s.Left.Auto {
		t.Errorf("left = %+v", s.Left)
	}
	// An unparseable offset is ignored (keeps the prior value).
	applyOn(s, "top", "garbage", 16)
	if s.Top.Px != 10 {
		t.Errorf("top after garbage = %+v", s.Top)
	}
}

func TestApplyZIndex(t *testing.T) {
	s := newStyle()
	if !s.ZIndexAuto {
		t.Fatal("initial z-index should be auto")
	}
	applyOn(s, "z-index", "7", 16)
	if s.ZIndexAuto || s.ZIndex != 7 {
		t.Errorf("z-index:7 => auto=%v n=%d", s.ZIndexAuto, s.ZIndex)
	}
	applyOn(s, "z-index", "-3", 16)
	if s.ZIndexAuto || s.ZIndex != -3 {
		t.Errorf("z-index:-3 => auto=%v n=%d", s.ZIndexAuto, s.ZIndex)
	}
	applyOn(s, "z-index", "auto", 16)
	if !s.ZIndexAuto {
		t.Errorf("z-index:auto => auto=%v", s.ZIndexAuto)
	}
	// A non-integer value is ignored (keeps auto).
	applyOn(s, "z-index", "1.5", 16)
	if !s.ZIndexAuto {
		t.Errorf("z-index:1.5 should be ignored, auto=%v", s.ZIndexAuto)
	}
}

func TestPositionPredicates(t *testing.T) {
	for _, p := range []Position{PositionStatic, PositionRelative, PositionSticky} {
		if p.OutOfFlow() {
			t.Errorf("%v should be in flow", p)
		}
	}
	for _, p := range []Position{PositionAbsolute, PositionFixed} {
		if !p.OutOfFlow() {
			t.Errorf("%v should be out of flow", p)
		}
	}
	if PositionStatic.Positioned() {
		t.Error("static is not positioned")
	}
	for _, p := range []Position{PositionRelative, PositionAbsolute, PositionFixed, PositionSticky} {
		if !p.Positioned() {
			t.Errorf("%v should be positioned", p)
		}
	}
}

// --- dynamic pseudo-class suppression --------------------------------------
// (el and attach are shared test helpers defined in selector_test.go)

func TestDynamicPseudoNeverMatchesButBaseDoes(t *testing.T) {
	btn := el("button", "", "btn")

	// The base class rule matches.
	if sel, ok := parseComplex(".btn"); !ok || !sel.Matches(btn) {
		t.Error(".btn should match the button")
	}
	// The :hover-qualified rule never matches at static render time.
	hover, ok := parseComplex(".btn:hover")
	if !ok {
		t.Fatal(".btn:hover should parse")
	}
	if hover.Matches(btn) {
		t.Error(".btn:hover must not match in a static render")
	}
	// Every dynamic pseudo keyword suppresses the match.
	for _, p := range []string{":hover", ":active", ":focus", ":focus-within", ":focus-visible", ":target"} {
		sel, ok := parseComplex(".btn" + p)
		if !ok {
			t.Fatalf("parse .btn%s", p)
		}
		if sel.Matches(btn) {
			t.Errorf(".btn%s must not match", p)
		}
	}
	// A universal dynamic pseudo also never matches.
	if uni, ok := parseComplex("*:hover"); !ok || uni.Matches(btn) {
		t.Errorf("*:hover matched=%v ok=%v", uni.Matches(btn), ok)
	}
}

func TestDynamicPseudoInDescendantChainDoesNotApply(t *testing.T) {
	// .menu:hover .sub — the .sub rule must not apply because its ancestor
	// constraint is a dynamic pseudo that never matches.
	sub := el("div", "", "sub")
	menu := el("div", "", "menu")
	attach(menu, sub)

	sel, ok := parseComplex(".menu:hover .sub")
	if !ok {
		t.Fatal("parse .menu:hover .sub")
	}
	if sel.Matches(sub) {
		t.Error(".menu:hover .sub must not match under a static render")
	}
	// Without the dynamic pseudo it does match, proving the chain is otherwise
	// sound.
	plain, _ := parseComplex(".menu .sub")
	if !plain.Matches(sub) {
		t.Error(".menu .sub should match")
	}
}

func TestSelectorListKeepsNonDynamicMembers(t *testing.T) {
	// In a rule "a:hover, .base { … }" only the .base selector should apply.
	sels := ParseSelectorList("a:hover, .base")
	if len(sels) != 2 {
		t.Fatalf("want 2 selectors, got %d", len(sels))
	}
	base := el("p", "", "base")
	anchor := el("a", "", "")
	var baseMatched, anchorMatched bool
	for _, s := range sels {
		if s.Matches(base) {
			baseMatched = true
		}
		if s.Matches(anchor) {
			anchorMatched = true
		}
	}
	if !baseMatched {
		t.Error(".base member should still match its element")
	}
	if anchorMatched {
		t.Error("a:hover member must not match in a static render")
	}
}

func TestDynamicPseudoSpecificity(t *testing.T) {
	// A dynamic pseudo contributes class-level specificity, like :root.
	c, ok := parseSimple(".btn:hover")
	if !ok || !c.Dynamic {
		t.Fatalf("parse .btn:hover => %+v ok=%v", c, ok)
	}
	_, cl, _ := c.specificity()
	if cl != 2 { // one class (.btn) + one dynamic pseudo
		t.Errorf("class-level specificity = %d, want 2", cl)
	}
}
