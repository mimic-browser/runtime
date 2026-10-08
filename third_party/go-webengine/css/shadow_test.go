// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package css

import "testing"

func TestParseBoxShadowLayers(t *testing.T) {
	sh, ok := parseBoxShadow("0 2px 4px rgba(0,0,0,0.5), inset 1px 1px 0 0 red", 16)
	if !ok || len(sh) != 2 {
		t.Fatalf("shadows = %+v,%v want 2", sh, ok)
	}
	if sh[0].OffsetX != 0 || sh[0].OffsetY != 2 || sh[0].Blur != 4 || sh[0].Inset {
		t.Errorf("layer0 = %+v", sh[0])
	}
	if sh[0].Color.A != 127 && sh[0].Color.A != 128 {
		t.Errorf("layer0 alpha = %d want ~128", sh[0].Color.A)
	}
	if !sh[1].Inset || sh[1].OffsetX != 1 || sh[1].OffsetY != 1 || sh[1].Spread != 0 {
		t.Errorf("layer1 = %+v", sh[1])
	}
	if sh[1].Color != (Color{255, 0, 0, 255}) {
		t.Errorf("layer1 colour = %+v want red", sh[1].Color)
	}
}

func TestParseBoxShadowSpread(t *testing.T) {
	sh, ok := parseBoxShadow("3px 4px 5px 6px #123456", 16)
	if !ok || len(sh) != 1 {
		t.Fatalf("shadow = %+v,%v", sh, ok)
	}
	s := sh[0]
	if s.OffsetX != 3 || s.OffsetY != 4 || s.Blur != 5 || s.Spread != 6 {
		t.Errorf("offsets = %+v", s)
	}
}

func TestParseBoxShadowNone(t *testing.T) {
	sh, ok := parseBoxShadow("none", 16)
	if !ok || sh != nil {
		t.Errorf("none = %+v,%v want nil,true", sh, ok)
	}
}

func TestParseBoxShadowDefaultColor(t *testing.T) {
	sh, ok := parseBoxShadow("2px 2px", 16)
	if !ok || sh[0].Color.A != 255 {
		t.Errorf("default colour shadow = %+v,%v", sh, ok)
	}
}

func TestParseBoxShadowInvalid(t *testing.T) {
	// A layer with an unknown token is rejected; with no valid layer -> false.
	if _, ok := parseBoxShadow("2px 2px wat", 16); ok {
		t.Error("unknown token should fail the layer")
	}
	// Only one offset -> invalid.
	if _, ok := parseBoxShadow("2px", 16); ok {
		t.Error("single offset should fail")
	}
	// Inset keyword alone -> invalid (needs offsets).
	if _, ok := parseBoxShadow("inset", 16); ok {
		t.Error("inset only should fail")
	}
}
