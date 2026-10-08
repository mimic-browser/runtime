// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package css

import "testing"

func TestApplyBackgroundImageProperty(t *testing.T) {
	s := &Style{}
	s.apply(Declaration{Property: "background-image", Value: "linear-gradient(to right, red, blue)"}, 16, nil)
	if len(s.BackgroundImages) != 1 || s.BackgroundImages[0].Kind != BgGradient {
		t.Fatalf("background-image = %+v", s.BackgroundImages)
	}
	// `none` resets the image list.
	s.apply(Declaration{Property: "background-image", Value: "none"}, 16, nil)
	if s.BackgroundImages != nil {
		t.Errorf("background-image none did not reset: %+v", s.BackgroundImages)
	}
}

// TestApplyMaskImage covers a real regression: `mask-image` was entirely
// unimplemented, so an empty `<span>` icon cut into shape by a mask (rather
// than painted via background-image — confirmed load-bearing live: every
// toolbar icon on Wikipedia's Vector-2022 skin works this way) never got the
// URL it needs fetched, and rendered as a plain solid-coloured square.
func TestApplyMaskImage(t *testing.T) {
	s := &Style{}
	s.apply(Declaration{Property: "mask-image", Value: "url(icon.svg)"}, 16, nil)
	if s.MaskImage != "icon.svg" {
		t.Errorf("mask-image url = %q, want icon.svg", s.MaskImage)
	}
	// The -webkit- prefixed form (what real sites emit for wider browser
	// support) sets the SAME field.
	s = &Style{}
	s.apply(Declaration{Property: "-webkit-mask-image", Value: "url(icon2.svg)"}, 16, nil)
	if s.MaskImage != "icon2.svg" {
		t.Errorf("-webkit-mask-image url = %q, want icon2.svg", s.MaskImage)
	}
	// A quoted url() argument is unquoted, matching background-image's url().
	s = &Style{}
	s.apply(Declaration{Property: "mask-image", Value: `url("quoted.svg")`}, 16, nil)
	if s.MaskImage != "quoted.svg" {
		t.Errorf("quoted mask-image url = %q, want quoted.svg", s.MaskImage)
	}
	// none resets it.
	s = &Style{MaskImage: "icon.svg"}
	s.apply(Declaration{Property: "mask-image", Value: "none"}, 16, nil)
	if s.MaskImage != "" {
		t.Errorf("mask-image:none did not reset: %q", s.MaskImage)
	}
	// A gradient or any other non-url() mask value is not modelled — left
	// unchanged, same as every other unrecognised value in this switch.
	s = &Style{MaskImage: "icon.svg"}
	s.apply(Declaration{Property: "mask-image", Value: "linear-gradient(black, transparent)"}, 16, nil)
	if s.MaskImage != "icon.svg" {
		t.Errorf("gradient mask-image should be a no-op, got %q", s.MaskImage)
	}
}

func TestApplyBackgroundShorthandGradient(t *testing.T) {
	s := &Style{}
	s.apply(Declaration{Property: "background", Value: "#222 linear-gradient(to bottom, rgba(0,0,0,1), rgba(0,0,0,0))"}, 16, nil)
	if s.Background != (Color{0x22, 0x22, 0x22, 255}) {
		t.Errorf("shorthand colour = %+v", s.Background)
	}
	if len(s.BackgroundImages) != 1 || s.BackgroundImages[0].Kind != BgGradient {
		t.Errorf("shorthand image = %+v", s.BackgroundImages)
	}
}

// TestApplyBackgroundShorthandPositionSizeRepeat covers a real regression:
// the `background` shorthand parsed a colour and an image layer but silently
// dropped any repeat keyword and any "<position>/<size>" pair — left at
// their zero value (RepeatBoth/unset) regardless of what the author wrote.
// Confirmed load-bearing live on pkg.go.dev's own mobile-nav hamburger
// button: `background:no-repeat center/2rem url(/static/shared/icon/
// menu_gm_grey_24dp.svg)` on a 2.5rem button — with size/position dropped,
// the 24px icon stretched to fill the WHOLE button instead of sitting
// centred at its real 2rem size, visibly distorting its three bars.
func TestApplyBackgroundShorthandPositionSizeRepeat(t *testing.T) {
	s := &Style{}
	s.apply(Declaration{Property: "background", Value: "no-repeat center/2rem url(/static/shared/icon/menu_gm_grey_24dp.svg)"}, 16, nil)
	if len(s.BackgroundRepeat) != 1 || s.BackgroundRepeat[0] != NoRepeat {
		t.Errorf("shorthand repeat = %+v", s.BackgroundRepeat)
	}
	if len(s.BackgroundPosition) != 1 || s.BackgroundPosition[0].X.Percent != 0.5 {
		t.Errorf("shorthand position = %+v", s.BackgroundPosition)
	}
	if len(s.BackgroundSize) != 1 || s.BackgroundSize[0].W.Px != 32 { // 2rem @ 16px
		t.Errorf("shorthand size = %+v", s.BackgroundSize)
	}
	if len(s.BackgroundImages) != 1 || s.BackgroundImages[0].Kind != BgURL {
		t.Errorf("shorthand image = %+v", s.BackgroundImages)
	}
	// A '/' inside the url()'s own path must never be mistaken for the
	// shorthand's own position/size divider — this exact real value has FOUR
	// of them before the divider is even reached.
	s2 := &Style{}
	s2.apply(Declaration{Property: "background", Value: "url(/static/shared/icon/x.svg)"}, 16, nil)
	if s2.BackgroundPosition != nil || s2.BackgroundSize != nil {
		t.Errorf("a bare url() with no '/' divider set position/size: pos=%+v size=%+v", s2.BackgroundPosition, s2.BackgroundSize)
	}
	// A '/' divider with nothing (or only whitespace) on one side is malformed
	// and must not set position/size either.
	s3 := &Style{}
	s3.apply(Declaration{Property: "background", Value: "/2rem url(icon.svg)"}, 16, nil)
	if s3.BackgroundPosition != nil || s3.BackgroundSize != nil {
		t.Errorf("an empty position side set position/size: pos=%+v size=%+v", s3.BackgroundPosition, s3.BackgroundSize)
	}
}

func TestApplyBackgroundSizePositionRepeat(t *testing.T) {
	s := &Style{}
	s.apply(Declaration{Property: "background-size", Value: "cover"}, 16, nil)
	if len(s.BackgroundSize) != 1 || s.BackgroundSize[0].Kind != SizeCover {
		t.Errorf("size = %+v", s.BackgroundSize)
	}
	s.apply(Declaration{Property: "background-position", Value: "center top"}, 16, nil)
	if len(s.BackgroundPosition) != 1 || s.BackgroundPosition[0].Y.Percent != 0 {
		t.Errorf("position = %+v", s.BackgroundPosition)
	}
	s.apply(Declaration{Property: "background-repeat", Value: "no-repeat"}, 16, nil)
	if len(s.BackgroundRepeat) != 1 || s.BackgroundRepeat[0] != NoRepeat {
		t.Errorf("repeat = %+v", s.BackgroundRepeat)
	}
	// Invalid values leave the property unset.
	s2 := &Style{}
	s2.apply(Declaration{Property: "background-size", Value: "bogus"}, 16, nil)
	s2.apply(Declaration{Property: "background-position", Value: ""}, 16, nil)
	s2.apply(Declaration{Property: "background-repeat", Value: "bogus"}, 16, nil)
	if s2.BackgroundSize != nil || s2.BackgroundPosition != nil || s2.BackgroundRepeat != nil {
		t.Errorf("invalid values set something: %+v", s2)
	}
}

func TestApplyBoxShadowAndOpacity(t *testing.T) {
	s := &Style{}
	s.apply(Declaration{Property: "box-shadow", Value: "0 4px 8px rgba(0,0,0,0.3)"}, 16, nil)
	if len(s.BoxShadows) != 1 || s.BoxShadows[0].Blur != 8 {
		t.Errorf("box-shadow = %+v", s.BoxShadows)
	}
	s.apply(Declaration{Property: "opacity", Value: "0.5"}, 16, nil)
	if !s.HasOpacity || s.Opacity != 0.5 {
		t.Errorf("opacity = %v,%v", s.Opacity, s.HasOpacity)
	}
	// Clamping below 0 and above 1.
	lo := &Style{}
	lo.apply(Declaration{Property: "opacity", Value: "-2"}, 16, nil)
	if lo.Opacity != 0 {
		t.Errorf("opacity -2 = %v want 0", lo.Opacity)
	}
	hi := &Style{}
	hi.apply(Declaration{Property: "opacity", Value: "3"}, 16, nil)
	if hi.Opacity != 1 {
		t.Errorf("opacity 3 = %v want 1", hi.Opacity)
	}
	// A non-numeric opacity is ignored.
	bad := &Style{}
	bad.apply(Declaration{Property: "opacity", Value: "half"}, 16, nil)
	if bad.HasOpacity {
		t.Error("non-numeric opacity should be ignored")
	}
}

func TestSplitDeclChunksNestedSemicolons(t *testing.T) {
	// Semicolons inside url()/parens and inside quotes must not split declarations.
	decls := ParseDeclarations(`background-image: url(data:image/png;base64,AAAA); color: red; content: 'a;b'; x: )`)
	got := map[string]string{}
	for _, d := range decls {
		got[d.Property] = d.Value
	}
	if got["background-image"] != "url(data:image/png;base64,AAAA)" {
		t.Errorf("bg-image = %q", got["background-image"])
	}
	if got["color"] != "red" {
		t.Errorf("color = %q", got["color"])
	}
	if got["content"] != "'a;b'" {
		t.Errorf("content = %q", got["content"])
	}
	if got["x"] != ")" {
		t.Errorf("x = %q (stray paren should not underflow)", got["x"])
	}
}

func TestParseBgImageLayerURLFail(t *testing.T) {
	// A url() with an empty target fails the layer (and the whole value).
	if _, ok := parseBackgroundImage("url()", 16); ok {
		t.Error("empty url() should not yield a layer")
	}
}

func TestParseRadialGradientBadStops(t *testing.T) {
	if _, ok := parseGradient("radial-gradient(circle at center, bogus, alsobad)", 16); ok {
		t.Error("radial with no valid stops should fail")
	}
}

func TestParseColorStopsEmptyEntry(t *testing.T) {
	// An empty stop entry (double comma) is skipped; the rest still parse.
	g, ok := parseGradient("linear-gradient(45deg, red, , blue)", 16)
	if !ok || len(g.Stops) != 2 {
		t.Errorf("empty stop entry handling = %+v,%v", g, ok)
	}
}
