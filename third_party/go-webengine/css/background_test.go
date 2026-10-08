// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package css

import (
	"math"
	"testing"
)

func TestParseBackgroundImageLayers(t *testing.T) {
	imgs, ok := parseBackgroundImage("linear-gradient(to right, red, blue), url('a.png'), none, ", 16)
	if !ok {
		t.Fatal("expected ok")
	}
	if len(imgs) != 2 {
		t.Fatalf("layers = %d want 2 (gradient + url; none/empty dropped)", len(imgs))
	}
	if imgs[0].Kind != BgGradient || imgs[0].Grad == nil {
		t.Errorf("layer0 = %+v want gradient", imgs[0])
	}
	if imgs[1].Kind != BgURL || imgs[1].URL != "a.png" {
		t.Errorf("layer1 = %+v want url a.png", imgs[1])
	}
}

func TestParseBackgroundImageNone(t *testing.T) {
	if _, ok := parseBackgroundImage("none", 16); ok {
		t.Error("none should report false")
	}
	if _, ok := parseBackgroundImage("", 16); ok {
		t.Error("empty should report false")
	}
	if _, ok := parseBackgroundImage("nonsense(1)", 16); ok {
		t.Error("unparseable should report false")
	}
}

func TestParseURLToken(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{`url("x.png")`, "x.png", true},
		{`url('y.jpg')`, "y.jpg", true},
		{`url( z.gif )`, "z.gif", true},
		{`url()`, "", false},
		{`url(   )`, "", false},
		{`url`, "", false},
		{`urlx`, "", false},
	}
	for _, c := range cases {
		got, ok := parseURLToken(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("parseURLToken(%q) = %q,%v want %q,%v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestParseGradientKinds(t *testing.T) {
	if g, ok := parseGradient("-webkit-linear-gradient(to top, #000, #fff)", 16); !ok || g.Radial {
		t.Errorf("vendor linear = %+v,%v", g, ok)
	}
	if g, ok := parseGradient("repeating-linear-gradient(45deg, red, blue)", 16); !ok || g.AngleDeg != 45 {
		t.Errorf("repeating linear = %+v,%v", g, ok)
	}
	if g, ok := parseGradient("radial-gradient(circle, red, blue)", 16); !ok || !g.Radial {
		t.Errorf("radial = %+v,%v", g, ok)
	}
	if _, ok := parseGradient("conic-gradient(red, blue)", 16); ok {
		t.Error("conic should be unsupported (false)")
	}
	if _, ok := parseGradient("linear-gradient()", 16); ok {
		t.Error("empty args should be false")
	}
	if _, ok := parseGradient("linear-gradient", 16); ok {
		t.Error("no parens should be false")
	}
	if _, ok := parseGradient("linear-gradient(red)", 16); ok {
		t.Error("single arg should be false (needs 2)")
	}
}

func TestParseLinearDirection(t *testing.T) {
	cases := []struct {
		in     string
		deg    float64
		corner uint8
		ok     bool
	}{
		{"to top", 0, 0, true},
		{"to right", 90, 0, true},
		{"to bottom", 180, 0, true},
		{"to left", 270, 0, true},
		{"to top right", 0, 1, true},
		{"to bottom right", 0, 2, true},
		{"to bottom left", 0, 3, true},
		{"to top left", 0, 4, true},
		{"45deg", 45, 0, true},
		{"to nowhere", 0, 0, false},
		{"redish", 0, 0, false},
	}
	for _, c := range cases {
		deg, corner, ok := parseLinearDirection(c.in)
		if ok != c.ok || deg != c.deg || corner != c.corner {
			t.Errorf("parseLinearDirection(%q) = %v,%d,%v want %v,%d,%v", c.in, deg, corner, ok, c.deg, c.corner, c.ok)
		}
	}
}

func TestParseAngle(t *testing.T) {
	cases := []struct {
		in   string
		want float64
		ok   bool
	}{
		{"90deg", 90, true},
		{"100grad", 90, true},
		{"0.5turn", 180, true},
		{"3.14159265rad", 180, true},
		{"42", 42, true},
		{"abc", 0, false},
		{"xdeg", 0, false},
	}
	for _, c := range cases {
		got, ok := parseAngle(c.in)
		if ok != c.ok || (ok && math.Abs(got-c.want) > 0.01) {
			t.Errorf("parseAngle(%q) = %v,%v want %v,%v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestParseRadialPrefixVariants(t *testing.T) {
	// Explicit two-length ellipse at a position.
	g, ok := parseGradient("radial-gradient(80px 40px at left top, red, blue)", 16)
	if !ok {
		t.Fatal("parse failed")
	}
	if g.Extent != ExtentExplicit || g.RadiusX.Px != 80 || g.RadiusY.Px != 40 {
		t.Errorf("explicit radii = %+v", g)
	}
	if g.PosX.Percent != 0 || g.PosY.Percent != 0 {
		t.Errorf("position = %v %v want top-left", g.PosX, g.PosY)
	}
	// One length => circle.
	g2, _ := parseGradient("radial-gradient(50px, red, blue)", 16)
	if g2.Shape != RadialCircle || g2.RadiusX.Px != 50 || g2.RadiusY.Px != 50 {
		t.Errorf("one-length circle = %+v", g2)
	}
	// Extent keywords + shape.
	for _, kw := range []struct {
		s string
		e RadialExtent
	}{
		{"closest-side", ExtentClosestSide},
		{"farthest-side", ExtentFarthestSide},
		{"closest-corner", ExtentClosestCorner},
		{"farthest-corner", ExtentFarthestCorner},
	} {
		g3, _ := parseGradient("radial-gradient(ellipse "+kw.s+" at center, red, blue)", 16)
		if g3.Extent != kw.e || g3.Shape != RadialEllipse {
			t.Errorf("%s => %+v", kw.s, g3)
		}
	}
	// "at" with no size part.
	g4, _ := parseGradient("radial-gradient(at 25% 75%, red, blue)", 16)
	if math.Abs(g4.PosX.Percent-0.25) > 1e-9 || math.Abs(g4.PosY.Percent-0.75) > 1e-9 {
		t.Errorf("at position = %+v", g4)
	}
}

func TestLooksLikeStop(t *testing.T) {
	if !looksLikeStop("red 10%") {
		t.Error("colour prefix should look like a stop")
	}
	if looksLikeStop("circle") {
		t.Error("shape keyword is not a stop")
	}
	if looksLikeStop("") {
		t.Error("empty is not a stop")
	}
}

func TestParseColorStops(t *testing.T) {
	// A two-position stop expands to two stops of the same colour.
	g, ok := parseGradient("linear-gradient(red 0% 40%, blue)", 16)
	if !ok {
		t.Fatal("parse failed")
	}
	if len(g.Stops) != 3 {
		t.Fatalf("stops = %d want 3 (red@0, red@40, blue)", len(g.Stops))
	}
	if !g.Stops[0].HasPos || g.Stops[0].Pos.Percent != 0 {
		t.Errorf("stop0 = %+v", g.Stops[0])
	}
	if g.Stops[1].Pos.Percent != 0.4 {
		t.Errorf("stop1 = %+v", g.Stops[1])
	}
	// A bare position (transition hint) with no colour is skipped.
	g2, ok := parseGradient("linear-gradient(red, 30%, blue)", 16)
	if !ok || len(g2.Stops) != 2 {
		t.Errorf("hint handling = %+v,%v", g2, ok)
	}
	// Fewer than two stops fails.
	if _, ok := parseGradient("linear-gradient(45deg, notacolor, alsobad)", 16); ok {
		t.Error("no valid stops should fail")
	}
}

func TestSplitTopLevelSep(t *testing.T) {
	got := splitTopLevelSep("a, rgb(1, 2, 3), b", ',')
	if len(got) != 3 || got[1] != " rgb(1, 2, 3)" {
		t.Errorf("split = %q", got)
	}
}

func TestParseBackgroundSize(t *testing.T) {
	cases := []struct {
		in   string
		kind BgSizeKind
	}{
		{"cover", SizeCover},
		{"contain", SizeContain},
		{"auto", SizeAuto},
		{"50px", SizeExplicit},
		{"50px 20px", SizeExplicit},
		{"50% auto", SizeExplicit},
	}
	for _, c := range cases {
		list, ok := parseBackgroundSizeList(c.in, 16)
		if !ok || list[0].Kind != c.kind {
			t.Errorf("size %q = %+v,%v", c.in, list, ok)
		}
	}
	// One explicit length => H auto.
	l, _ := parseBackgroundSizeList("30px", 16)
	if !l[0].H.Auto || l[0].W.Px != 30 {
		t.Errorf("single length = %+v", l[0])
	}
	// Bad token fails.
	if _, ok := parseBackgroundSizeList("bogus", 16); ok {
		t.Error("bogus size should fail")
	}
	// Three lengths (invalid) fails.
	if _, ok := parseBackgroundSizeList("1px 2px 3px", 16); ok {
		t.Error("3-length size should fail")
	}
}

func TestParseBackgroundPosition(t *testing.T) {
	single := map[string][2]float64{
		"left":   {0, 0.5},
		"right":  {1, 0.5},
		"top":    {0.5, 0},
		"bottom": {0.5, 1},
		"center": {0.5, 0.5},
	}
	for k, want := range single {
		list, ok := parseBackgroundPositionList(k, 16)
		if !ok || list[0].X.Percent != want[0] || list[0].Y.Percent != want[1] {
			t.Errorf("pos %q = %+v,%v want %v", k, list, ok, want)
		}
	}
	// Single length: X literal, Y centre.
	l, _ := parseBackgroundPositionList("10px", 16)
	if l[0].X.Px != 10 || l[0].Y.Percent != 0.5 {
		t.Errorf("single length pos = %+v", l[0])
	}
	// Two-component with keywords.
	l2, _ := parseBackgroundPositionList("right bottom", 16)
	if l2[0].X.Percent != 1 || l2[0].Y.Percent != 1 {
		t.Errorf("right bottom = %+v", l2[0])
	}
	// Two-component with lengths/percents.
	l3, _ := parseBackgroundPositionList("25% 40px", 16)
	if l3[0].X.Percent != 0.25 || l3[0].Y.Px != 40 {
		t.Errorf("25%% 40px = %+v", l3[0])
	}
	// Failures.
	if _, ok := parseBackgroundPositionList("", 16); ok {
		t.Error("empty pos should fail")
	}
	if _, ok := parseBackgroundPositionList("bogus", 16); ok {
		t.Error("bogus single should fail")
	}
	if _, ok := parseBackgroundPositionList("left bogus", 16); ok {
		t.Error("bogus y should fail")
	}
	if _, ok := parseBackgroundPositionList("bogus top", 16); ok {
		t.Error("bogus x should fail")
	}
}

func TestAxisLengthKeywordAxes(t *testing.T) {
	// left/right only valid horizontally; top/bottom only vertically.
	if _, ok := axisLength("left", false, 16); ok {
		t.Error("left is not a vertical keyword")
	}
	if _, ok := axisLength("top", true, 16); ok {
		t.Error("top is not a horizontal keyword")
	}
	if l, ok := axisLength("center", true, 16); !ok || l.Percent != 0.5 {
		t.Errorf("center = %+v,%v", l, ok)
	}
	if l, ok := axisLength("bottom", false, 16); !ok || l.Percent != 1 {
		t.Errorf("bottom = %+v,%v", l, ok)
	}
	if l, ok := axisLength("right", true, 16); !ok || l.Percent != 1 {
		t.Errorf("right = %+v,%v", l, ok)
	}
}

func TestParseBackgroundRepeat(t *testing.T) {
	cases := map[string]BgRepeat{
		"no-repeat": NoRepeat,
		"repeat":    RepeatBoth,
		"repeat-x":  RepeatX,
		"repeat-y":  RepeatY,
		"space":     RepeatBoth,
		"round":     RepeatBoth,
	}
	for k, want := range cases {
		got, ok := parseBackgroundRepeat(k)
		if !ok || got != want {
			t.Errorf("repeat %q = %v,%v want %v", k, got, ok, want)
		}
	}
	if _, ok := parseBackgroundRepeat("bogus"); ok {
		t.Error("bogus repeat should fail")
	}
}
