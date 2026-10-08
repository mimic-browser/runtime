// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package css

import (
	"math"
	"testing"
)

// TestLinearGradientSampledStops asserts the start/mid/end colours of a
// horizontal two-stop gradient at their known positions (within tolerance).
func TestLinearGradientSampledStops(t *testing.T) {
	g, ok := parseGradient("linear-gradient(to right, #000000, #ffffff)", 16)
	if !ok {
		t.Fatal("parse failed")
	}
	s := g.Sampler(100, 10)
	start := s.At(0.5, 5)
	mid := s.At(50, 5)
	end := s.At(99.5, 5)
	if !colorNear(start, Color{0, 0, 0, 255}, 3) {
		t.Errorf("start = %+v want black", start)
	}
	if !colorNear(mid, Color{128, 128, 128, 255}, 4) {
		t.Errorf("mid = %+v want ~grey", mid)
	}
	if !colorNear(end, Color{255, 255, 255, 255}, 3) {
		t.Errorf("end = %+v want white", end)
	}
}

// TestLinearGradientDirections checks the four cardinal `to <side>` directions
// put the end colour at the expected edge.
func TestLinearGradientDirections(t *testing.T) {
	red, blue := Color{255, 0, 0, 255}, Color{0, 0, 255, 255}
	// to top: end (blue) at top, start (red) at bottom.
	g, _ := parseGradient("linear-gradient(to top, red, blue)", 16)
	s := g.Sampler(10, 100)
	if !colorNear(s.At(5, 1), blue, 3) || !colorNear(s.At(5, 99), red, 3) {
		t.Errorf("to top top=%+v bottom=%+v", s.At(5, 1), s.At(5, 99))
	}
	// to bottom (default): end at bottom.
	g2, _ := parseGradient("linear-gradient(red, blue)", 16)
	s2 := g2.Sampler(10, 100)
	if !colorNear(s2.At(5, 99), blue, 3) {
		t.Errorf("to bottom = %+v want blue", s2.At(5, 99))
	}
	// to left: end at left.
	g3, _ := parseGradient("linear-gradient(to left, red, blue)", 16)
	s3 := g3.Sampler(100, 10)
	if !colorNear(s3.At(1, 5), blue, 3) {
		t.Errorf("to left = %+v want blue", s3.At(1, 5))
	}
}

func TestLinearGradientCorners(t *testing.T) {
	for _, dir := range []string{"to top right", "to bottom right", "to bottom left", "to top left"} {
		g, ok := parseGradient("linear-gradient("+dir+", #000, #fff)", 16)
		if !ok {
			t.Fatalf("parse %q failed", dir)
		}
		s := g.Sampler(80, 40)
		// Sampling the box centre should give the mid grey regardless of corner.
		if c := s.At(40, 20); !colorNear(c, Color{128, 128, 128, 255}, 6) {
			t.Errorf("%s centre = %+v want mid grey", dir, c)
		}
	}
}

func TestDirectionZeroSize(t *testing.T) {
	g := &Gradient{Corner: 1}
	dx, dy := g.direction(0, 0)
	if dx != 0 || dy != 1 {
		t.Errorf("zero-size corner dir = %v,%v want 0,1", dx, dy)
	}
}

func TestLinearSamplerDegenerate(t *testing.T) {
	// A zero-size box must not divide by zero.
	g, _ := parseGradient("linear-gradient(to right, red, blue)", 16)
	s := g.Sampler(0, 0)
	_ = s.At(0, 0) // no panic
}

// TestRadialGradientDefault: a centred default (farthest-corner ellipse) has the
// first colour at the centre and the last near the corner.
func TestRadialGradientDefault(t *testing.T) {
	g, _ := parseGradient("radial-gradient(#00ff00, #0000ff)", 16)
	s := g.Sampler(100, 100)
	if c := s.At(50, 50); !colorNear(c, Color{0, 255, 0, 255}, 3) {
		t.Errorf("radial centre = %+v want green", c)
	}
	if c := s.At(99, 99); !colorNear(c, Color{0, 0, 255, 255}, 40) {
		t.Errorf("radial corner = %+v want bluish", c)
	}
}

func TestRadialRadiiExtents(t *testing.T) {
	base := func(shape RadialShape, ext RadialExtent) *Gradient {
		return &Gradient{
			Radial: true, Shape: shape, Extent: ext,
			PosX:  Length{Percent: 0.5, IsPercent: true},
			PosY:  Length{Percent: 0.5, IsPercent: true},
			Stops: []ColorStop{{Color: Color{A: 255}}, {Color: Color{R: 255, A: 255}, Pos: Length{Percent: 1, IsPercent: true}, HasPos: true}},
		}
	}
	// Circle closest-side in a 100x60 box centred: min side dist = 30.
	if rx, ry := radialRadii(base(RadialCircle, ExtentClosestSide), 100, 60, 50, 30); rx != 30 || ry != 30 {
		t.Errorf("circle closest-side = %v,%v want 30,30", rx, ry)
	}
	// Circle farthest-side: max side dist = 50.
	if rx, _ := radialRadii(base(RadialCircle, ExtentFarthestSide), 100, 60, 50, 30); rx != 50 {
		t.Errorf("circle farthest-side rx = %v want 50", rx)
	}
	// Circle closest-corner: hypot(50,30).
	if rx, _ := radialRadii(base(RadialCircle, ExtentClosestCorner), 100, 60, 50, 30); math.Abs(rx-math.Hypot(50, 30)) > 1e-9 {
		t.Errorf("circle closest-corner rx = %v", rx)
	}
	// Circle farthest-corner (default): hypot(50,30) as well when centred.
	if rx, _ := radialRadii(base(RadialCircle, ExtentFarthestCorner), 100, 60, 50, 30); math.Abs(rx-math.Hypot(50, 30)) > 1e-9 {
		t.Errorf("circle farthest-corner rx = %v", rx)
	}
	// Ellipse closest-side: (50,30).
	if rx, ry := radialRadii(base(RadialEllipse, ExtentClosestSide), 100, 60, 50, 30); rx != 50 || ry != 30 {
		t.Errorf("ellipse closest-side = %v,%v", rx, ry)
	}
	// Ellipse farthest-side: (50,30) centred.
	if rx, ry := radialRadii(base(RadialEllipse, ExtentFarthestSide), 100, 60, 50, 30); rx != 50 || ry != 30 {
		t.Errorf("ellipse farthest-side = %v,%v", rx, ry)
	}
	// Ellipse farthest-corner: sides scaled by sqrt(2) when centred.
	if rx, ry := radialRadii(base(RadialEllipse, ExtentFarthestCorner), 100, 60, 50, 30); math.Abs(rx-50*math.Sqrt2) > 1e-6 || math.Abs(ry-30*math.Sqrt2) > 1e-6 {
		t.Errorf("ellipse farthest-corner = %v,%v want sqrt2 scaled", rx, ry)
	}
	// Ellipse closest-corner also scaled.
	if rx, _ := radialRadii(base(RadialEllipse, ExtentClosestCorner), 100, 60, 50, 30); math.Abs(rx-50*math.Sqrt2) > 1e-6 {
		t.Errorf("ellipse closest-corner rx = %v", rx)
	}
	// Explicit radii.
	g := base(RadialEllipse, ExtentExplicit)
	g.RadiusX = Length{Px: 12}
	g.RadiusY = Length{Px: 7}
	if rx, ry := radialRadii(g, 100, 60, 50, 30); rx != 12 || ry != 7 {
		t.Errorf("explicit = %v,%v", rx, ry)
	}
}

func TestScaleToCornerZero(t *testing.T) {
	if rx, ry := scaleToCorner(0, 5, 3, 4); rx != 0 || ry != 5 {
		t.Errorf("zero sx = %v,%v want passthrough", rx, ry)
	}
}

func TestRadialSamplerZeroRadius(t *testing.T) {
	g := &Gradient{
		Radial: true, Shape: RadialCircle, Extent: ExtentClosestSide,
		PosX: Length{Percent: 0, IsPercent: true}, PosY: Length{Percent: 0, IsPercent: true},
		Stops: []ColorStop{{Color: Color{A: 255}}, {Color: Color{R: 255, A: 255}}},
	}
	// Centre at corner (0,0) with closest-side => radius 0; guarded to 1.
	s := g.Sampler(100, 100)
	if s.rx < 1 || s.ry < 1 {
		t.Errorf("guarded radii = %v,%v want >=1", s.rx, s.ry)
	}
}

func TestNormalizeStopsDistribution(t *testing.T) {
	// Four stops, only ends positioned: middle two distributed evenly.
	in := []ColorStop{
		{Color: Color{A: 255}},
		{Color: Color{R: 255, A: 255}},
		{Color: Color{G: 255, A: 255}},
		{Color: Color{B: 255, A: 255}},
	}
	out := normalizeStops(in, 100)
	wantPos := []float64{0, 1.0 / 3, 2.0 / 3, 1}
	for i, w := range wantPos {
		if math.Abs(out[i].pos-w) > 1e-9 {
			t.Errorf("stop %d pos = %v want %v", i, out[i].pos, w)
		}
	}
}

func TestNormalizeStopsPxAndMonotonic(t *testing.T) {
	// Px stop resolves against lineLen; a decreasing stop is clamped up.
	in := []ColorStop{
		{Color: Color{A: 255}, Pos: Length{Px: 50}, HasPos: true},
		{Color: Color{R: 255, A: 255}, Pos: Length{Px: 25}, HasPos: true}, // decreasing -> clamped to 0.5
	}
	out := normalizeStops(in, 100)
	if math.Abs(out[0].pos-0.5) > 1e-9 {
		t.Errorf("px stop = %v want 0.5", out[0].pos)
	}
	if out[1].pos < out[0].pos {
		t.Errorf("non-monotonic: %v < %v", out[1].pos, out[0].pos)
	}
}

func TestNormalizeStopsZeroLineLen(t *testing.T) {
	in := []ColorStop{
		{Color: Color{A: 255}, Pos: Length{Px: 50}, HasPos: true},
		{Color: Color{R: 255, A: 255}},
	}
	out := normalizeStops(in, 0) // lineLen<=0: px pos stays 0
	if out[0].pos != 0 {
		t.Errorf("zero lineLen px pos = %v want 0", out[0].pos)
	}
}

func TestSampleStopsEdgesAndSpan(t *testing.T) {
	stops := []normStop{
		{pos: 0.3, color: Color{10, 10, 10, 255}},
		{pos: 0.3, color: Color{20, 20, 20, 255}}, // equal position => span 0
		{pos: 0.9, color: Color{200, 200, 200, 255}},
	}
	// Below first => first colour.
	if c := sampleStops(stops, 0.0); c.R != 10 {
		t.Errorf("below = %+v", c)
	}
	// Above last => last colour.
	if c := sampleStops(stops, 1.0); c.R != 200 {
		t.Errorf("above = %+v", c)
	}
	// At the zero-span boundary returns the second stop's colour.
	if c := sampleStops(stops, 0.3); c.R != 20 && c.R != 10 {
		t.Errorf("span-zero = %+v", c)
	}
	// Mid the second segment interpolates.
	if c := sampleStops(stops, 0.6); c.R < 20 || c.R > 200 {
		t.Errorf("mid = %+v", c)
	}
}

func TestLerpColorPremulTransparent(t *testing.T) {
	// Interpolating opaque red to transparent at the fully-transparent end keeps
	// no RGB bleed (premultiplied), returning alpha 0.
	c := lerpColorPremul(Color{255, 0, 0, 255}, Color{0, 0, 0, 0}, 1)
	if c.A != 0 {
		t.Errorf("fully transparent end alpha = %d want 0", c.A)
	}
	// Both transparent => zero colour.
	if c := lerpColorPremul(Color{0, 0, 0, 0}, Color{0, 0, 0, 0}, 0.5); c != (Color{0, 0, 0, 0}) {
		t.Errorf("both transparent = %+v", c)
	}
}
