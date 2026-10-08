// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package css

import "math"

// GradientSampler evaluates a gradient's colour at any point of a W×H box. It is
// built once per box (Gradient.Sampler) with its geometry and normalised stops
// precomputed, then queried per pixel with At.
type GradientSampler struct {
	radial bool

	// Linear: unit direction (dx,dy) toward the 100% end, start point (sx,sy) and
	// the gradient-line length in pixels.
	dx, dy, sx, sy, length float64

	// Radial: centre and radii.
	cx, cy, rx, ry float64

	stops []normStop
}

type normStop struct {
	pos   float64 // fraction 0..1 along the gradient line
	color Color
}

// Sampler builds a GradientSampler for a box of the given size (W×H, pixels).
func (g *Gradient) Sampler(w, h float64) GradientSampler {
	if g.Radial {
		return g.radialSampler(w, h)
	}
	return g.linearSampler(w, h)
}

func (g *Gradient) linearSampler(w, h float64) GradientSampler {
	dx, dy := g.direction(w, h)
	length := math.Abs(w*dx) + math.Abs(h*dy)
	if length <= 0 {
		length = 1
	}
	cx, cy := w/2, h/2
	// Start point is half the line length back from the centre along -dir.
	sx := cx - dx*length/2
	sy := cy - dy*length/2
	return GradientSampler{
		radial: false,
		dx:     dx, dy: dy, sx: sx, sy: sy, length: length,
		stops: normalizeStops(g.Stops, length),
	}
}

// direction returns the unit vector pointing toward the gradient's 100% end, in
// screen coordinates (y down). Angle form: 0deg = up, clockwise. Corner form:
// perpendicular to the box's opposite diagonal (the CSS "magic corners").
func (g *Gradient) direction(w, h float64) (float64, float64) {
	if g.Corner != 0 {
		d := math.Hypot(w, h)
		if d == 0 {
			return 0, 1
		}
		switch g.Corner {
		case 1: // to top right
			return h / d, -w / d
		case 2: // to bottom right
			return h / d, w / d
		case 3: // to bottom left
			return -h / d, w / d
		default: // 4: to top left
			return -h / d, -w / d
		}
	}
	rad := g.AngleDeg * math.Pi / 180
	return math.Sin(rad), -math.Cos(rad)
}

func (g *Gradient) radialSampler(w, h float64) GradientSampler {
	cx := g.PosX.Resolve(w)
	cy := g.PosY.Resolve(h)
	rx, ry := radialRadii(g, w, h, cx, cy)
	if rx <= 0 {
		rx = 1
	}
	if ry <= 0 {
		ry = 1
	}
	// For a radial gradient the "line length" for px-stop resolution is rx.
	return GradientSampler{
		radial: true,
		cx:     cx, cy: cy, rx: rx, ry: ry,
		stops: normalizeStops(g.Stops, rx),
	}
}

// radialRadii computes the ending shape's horizontal and vertical radii.
func radialRadii(g *Gradient, w, h, cx, cy float64) (float64, float64) {
	if g.Extent == ExtentExplicit {
		return g.RadiusX.Resolve(w), g.RadiusY.Resolve(h)
	}
	left, right := cx, w-cx
	top, bottom := cy, h-cy
	sideMin := func(a, b float64) float64 { return math.Min(math.Abs(a), math.Abs(b)) }
	sideMax := func(a, b float64) float64 { return math.Max(math.Abs(a), math.Abs(b)) }

	if g.Shape == RadialCircle {
		var r float64
		switch g.Extent {
		case ExtentClosestSide:
			r = math.Min(sideMin(left, right), sideMin(top, bottom))
		case ExtentFarthestSide:
			r = math.Max(sideMax(left, right), sideMax(top, bottom))
		case ExtentClosestCorner:
			r = math.Hypot(sideMin(left, right), sideMin(top, bottom))
		default: // farthest-corner
			r = math.Hypot(sideMax(left, right), sideMax(top, bottom))
		}
		return r, r
	}

	// Ellipse: side radii per the closest/farthest choice, scaled to a corner for
	// the corner variants (keeping the side aspect ratio).
	switch g.Extent {
	case ExtentClosestSide:
		return sideMin(left, right), sideMin(top, bottom)
	case ExtentFarthestSide:
		return sideMax(left, right), sideMax(top, bottom)
	case ExtentClosestCorner:
		sx, sy := sideMin(left, right), sideMin(top, bottom)
		return scaleToCorner(sx, sy, sideMin(left, right), sideMin(top, bottom))
	default: // farthest-corner
		sx, sy := sideMax(left, right), sideMax(top, bottom)
		return scaleToCorner(sx, sy, sideMax(left, right), sideMax(top, bottom))
	}
}

// scaleToCorner scales side radii (sx,sy) so the ellipse passes through the
// corner offset (dx,dy) while keeping the sx:sy aspect ratio.
func scaleToCorner(sx, sy, dx, dy float64) (float64, float64) {
	if sx == 0 || sy == 0 {
		return sx, sy
	}
	k := math.Sqrt((dx/sx)*(dx/sx) + (dy/sy)*(dy/sy))
	return sx * k, sy * k
}

// At returns the gradient colour at box-local point (px, py).
func (s GradientSampler) At(px, py float64) Color {
	var t float64
	if s.radial {
		nx := (px - s.cx) / s.rx
		ny := (py - s.cy) / s.ry
		t = math.Sqrt(nx*nx + ny*ny)
	} else {
		t = ((px-s.sx)*s.dx + (py-s.sy)*s.dy) / s.length
	}
	return sampleStops(s.stops, t)
}

// normalizeStops converts parsed colour stops to monotone fractions in [0,1].
// Px positions resolve against lineLen; percents map directly; unset positions
// are distributed evenly between their bounding neighbours (CSS stop rules).
func normalizeStops(in []ColorStop, lineLen float64) []normStop {
	n := len(in)
	out := make([]normStop, n)
	has := make([]bool, n)
	for i, s := range in {
		out[i].color = s.Color
		if s.HasPos {
			if s.Pos.IsPercent {
				out[i].pos = s.Pos.Percent
			} else if lineLen > 0 {
				out[i].pos = s.Pos.Px / lineLen
			}
			has[i] = true
		}
	}
	// First/last default to 0 and 1.
	if !has[0] {
		out[0].pos, has[0] = 0, true
	}
	if !has[n-1] {
		out[n-1].pos, has[n-1] = 1, true
	}
	// Enforce monotonic non-decreasing positions.
	for i := 1; i < n; i++ {
		if has[i] && out[i].pos < out[i-1].pos {
			out[i].pos = out[i-1].pos
		}
	}
	// Distribute runs of unset positions between bounding set stops.
	i := 0
	for i < n {
		if has[i] {
			i++
			continue
		}
		j := i
		for j < n && !has[j] {
			j++
		}
		prev := out[i-1].pos
		next := out[j].pos
		count := j - i + 1
		for k := i; k < j; k++ {
			out[k].pos = prev + (next-prev)*float64(k-i+1)/float64(count)
			has[k] = true
		}
		i = j
	}
	return out
}

// sampleStops returns the interpolated colour at fraction t (clamped to the end
// stops), interpolating in premultiplied-alpha space between adjacent stops.
func sampleStops(stops []normStop, t float64) Color {
	if t <= stops[0].pos {
		return stops[0].color
	}
	last := len(stops) - 1
	if t >= stops[last].pos {
		return stops[last].color
	}
	for i := 0; i < last; i++ {
		a, b := stops[i], stops[i+1]
		if t >= a.pos && t <= b.pos {
			span := b.pos - a.pos
			if span <= 0 {
				return b.color
			}
			return lerpColorPremul(a.color, b.color, (t-a.pos)/span)
		}
	}
	return stops[last].color
}

// lerpColorPremul linearly interpolates two colours in premultiplied-alpha
// space (so a transparent stop does not bleed its RGB), returning an
// unpremultiplied Color.
func lerpColorPremul(a, b Color, f float64) Color {
	aa := float64(a.A) / 255
	ba := float64(b.A) / 255
	ar, ag, ab := float64(a.R)*aa, float64(a.G)*aa, float64(a.B)*aa
	br, bg, bb := float64(b.R)*ba, float64(b.G)*ba, float64(b.B)*ba
	oa := aa + (ba-aa)*f
	or := ar + (br-ar)*f
	og := ag + (bg-ag)*f
	ob := ab + (bb-ab)*f
	if oa <= 0 {
		return Color{0, 0, 0, 0}
	}
	return Color{
		R: clampByte(or / oa),
		G: clampByte(og / oa),
		B: clampByte(ob / oa),
		A: clampByte(oa * 255),
	}
}
