// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package css

import (
	"math"
	"strconv"
	"strings"
)

// BgImageKind identifies the kind of a single background-image layer.
type BgImageKind uint8

const (
	// BgNone is an empty layer (the keyword `none`).
	BgNone BgImageKind = iota
	// BgURL is a url(...) bitmap layer.
	BgURL
	// BgGradient is a linear/radial gradient layer.
	BgGradient
)

// BgImage is one background-image layer: a url() bitmap or a gradient. Layers
// are stored first-listed-first; the first layer paints on top (CSS order).
type BgImage struct {
	Kind BgImageKind
	URL  string    // raw url() argument (BgURL); resolved by the engine
	Grad *Gradient // gradient spec (BgGradient)
}

// BgSizeKind selects how a background image is scaled.
type BgSizeKind uint8

const (
	// SizeAuto uses the image's intrinsic size (gradients fill the box).
	SizeAuto BgSizeKind = iota
	// SizeCover scales to cover the whole box (may crop).
	SizeCover
	// SizeContain scales to fit inside the box (may letterbox).
	SizeContain
	// SizeExplicit uses the given width/height lengths (a length or auto each).
	SizeExplicit
)

// BgSize is a resolved background-size value.
type BgSize struct {
	Kind BgSizeKind
	W    Length // meaningful when SizeExplicit
	H    Length // meaningful when SizeExplicit; Auto keeps aspect ratio
}

// ObjectFit selects how a replaced element's (img/video) own content is
// scaled to its box, when that box's size (from width/height/aspect-ratio)
// doesn't match the content's own intrinsic aspect ratio — the equivalent of
// background-size for a foreground element rather than a background layer.
// Confirmed live (round 89) as `object-fit:cover`, the standard Tailwind
// `object-cover` utility: react.dev's own avatar/video-thumbnail images and
// tailwindcss.com's own gallery images. `object-position` is not modelled —
// no confirmed non-centered real use — so cover/contain always centre.
type ObjectFit uint8

const (
	// ObjectFitFill stretches the content to exactly fill the box, distorting
	// its aspect ratio if the box's doesn't match — the initial value, and
	// this engine's own prior behaviour for every image before this existed.
	ObjectFitFill ObjectFit = iota
	// ObjectFitCover scales the content, preserving its aspect ratio, to
	// cover the whole box, cropping any overflow — same mechanism as
	// background-size:cover, applied to a foreground element.
	ObjectFitCover
	// ObjectFitContain scales the content, preserving its aspect ratio, to
	// fit entirely inside the box, letterboxing any remainder.
	ObjectFitContain
)

// BgPosition is a resolved background-position (percent/px per axis). The
// initial value is the top-left corner (0%, 0%).
type BgPosition struct {
	X, Y Length
}

// BgRepeat is the background-repeat value.
type BgRepeat uint8

const (
	// RepeatBoth tiles on both axes (initial value).
	RepeatBoth BgRepeat = iota
	// RepeatX tiles horizontally only.
	RepeatX
	// RepeatY tiles vertically only.
	RepeatY
	// NoRepeat paints the image once.
	NoRepeat
)

// RadialShape selects a circular or elliptical radial gradient.
type RadialShape uint8

const (
	// RadialEllipse is the default radial shape.
	RadialEllipse RadialShape = iota
	// RadialCircle forces equal radii.
	RadialCircle
)

// RadialExtent is the sizing keyword of a radial gradient's ending shape.
type RadialExtent uint8

const (
	// ExtentFarthestCorner is the default extent.
	ExtentFarthestCorner RadialExtent = iota
	// ExtentClosestSide sizes to the nearest edge.
	ExtentClosestSide
	// ExtentClosestCorner sizes to the nearest corner.
	ExtentClosestCorner
	// ExtentFarthestSide sizes to the farthest edge.
	ExtentFarthestSide
	// ExtentExplicit uses explicit RadiusX/RadiusY lengths.
	ExtentExplicit
)

// ColorStop is one gradient colour stop. Pos is the parsed position (a percent
// or px length); HasPos is false when the stop had no explicit position and its
// fraction is interpolated during normalisation.
type ColorStop struct {
	Color  Color
	Pos    Length
	HasPos bool
}

// Gradient is a parsed linear or radial gradient.
type Gradient struct {
	Radial bool

	// Linear direction. Corner is 0 for an explicit angle (AngleDeg), else a
	// corner code 1..4 (top-right/bottom-right/bottom-left/top-left) whose angle
	// depends on the box size and is resolved in the sampler.
	AngleDeg float64
	Corner   uint8

	// Radial geometry.
	Shape            RadialShape
	Extent           RadialExtent
	RadiusX, RadiusY Length // meaningful when Extent==ExtentExplicit
	PosX, PosY       Length // centre; default 50% 50%

	Stops []ColorStop
}

// parseBackgroundImage parses a background-image value (a comma-separated list
// of layers) into BgImage layers. It reports false when the whole value fails to
// yield any usable layer (so the property is left unset).
func parseBackgroundImage(v string, emRef float64) ([]BgImage, bool) {
	v = strings.TrimSpace(v)
	if strings.EqualFold(v, "none") || v == "" {
		return nil, false
	}
	var out []BgImage
	for _, part := range splitTopLevelSep(v, ',') {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if img, ok := parseBgImageLayer(part, emRef); ok {
			out = append(out, img)
		}
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

// parseBgImageLayer parses a single background-image layer, which may (in a
// `background` shorthand) also carry a colour and position/size/repeat tokens
// around the image function. It scans the layer's top-level tokens for a url()
// or gradient() function and returns that as the layer's image.
func parseBgImageLayer(part string, emRef float64) (BgImage, bool) {
	for _, tok := range splitTopLevel(part) {
		lt := strings.ToLower(tok)
		if strings.HasPrefix(lt, "url(") {
			if u, ok := parseURLToken(tok); ok {
				return BgImage{Kind: BgURL, URL: u}, true
			}
			return BgImage{}, false
		}
		if isGradientToken(lt) {
			if g, ok := parseGradient(tok, emRef); ok {
				return BgImage{Kind: BgGradient, Grad: g}, true
			}
			return BgImage{}, false
		}
	}
	return BgImage{}, false
}

// isGradientToken reports whether a lowercase token is a gradient function name
// (possibly vendor-prefixed / repeating-).
func isGradientToken(lt string) bool {
	for _, p := range []string{"-webkit-", "-moz-", "-o-", "-ms-"} {
		lt = strings.TrimPrefix(lt, p)
	}
	lt = strings.TrimPrefix(lt, "repeating-")
	return strings.HasPrefix(lt, "linear-gradient(") ||
		strings.HasPrefix(lt, "radial-gradient(") ||
		strings.HasPrefix(lt, "conic-gradient(")
}

// parseURLToken extracts the target of a url(...) token, stripping optional
// quotes. A `-webkit-image-set`/`image-set` or bare token is not handled here.
func parseURLToken(tok string) (string, bool) {
	i := strings.IndexByte(tok, '(')
	j := strings.LastIndexByte(tok, ')')
	if i < 0 || j <= i {
		return "", false
	}
	inner := strings.TrimSpace(tok[i+1 : j])
	inner = strings.Trim(inner, `"'`)
	inner = strings.TrimSpace(inner)
	if inner == "" {
		return "", false
	}
	return inner, true
}

// parseGradient parses a linear-gradient()/radial-gradient() function (including
// the repeating-* and -webkit-/-moz- prefixed forms). conic-gradient is not
// modelled and reports false (the layer is dropped, falling back to the colour).
func parseGradient(fn string, emRef float64) (*Gradient, bool) {
	lf := strings.ToLower(strings.TrimSpace(fn))
	// Strip vendor prefixes so -webkit-linear-gradient parses like the standard.
	for _, p := range []string{"-webkit-", "-moz-", "-o-", "-ms-"} {
		lf = strings.TrimPrefix(lf, p)
	}
	repeating := strings.HasPrefix(lf, "repeating-")
	lf = strings.TrimPrefix(lf, "repeating-")

	open := strings.IndexByte(fn, '(')
	closeIdx := strings.LastIndexByte(fn, ')')
	if open < 0 || closeIdx <= open {
		return nil, false
	}
	inner := fn[open+1 : closeIdx]
	args := splitTopLevelSep(inner, ',')
	if len(args) < 2 {
		return nil, false
	}

	switch {
	case strings.HasPrefix(lf, "linear-gradient"):
		return parseLinearGradient(args, emRef, repeating)
	case strings.HasPrefix(lf, "radial-gradient"):
		return parseRadialGradient(args, emRef, repeating)
	}
	return nil, false
}

// parseLinearGradient parses the argument list of a linear-gradient().
func parseLinearGradient(args []string, emRef float64, repeating bool) (*Gradient, bool) {
	g := &Gradient{AngleDeg: 180, Radial: false} // default direction: to bottom
	_ = repeating
	first := strings.TrimSpace(args[0])
	lfirst := strings.ToLower(first)
	stopArgs := args
	if deg, corner, ok := parseLinearDirection(lfirst); ok {
		g.AngleDeg, g.Corner = deg, corner
		stopArgs = args[1:]
	}
	stops, ok := parseColorStops(stopArgs, emRef)
	if !ok {
		return nil, false
	}
	g.Stops = stops
	return g, true
}

// parseLinearDirection parses a linear-gradient's optional first argument (an
// angle or a `to <side(s)>`), returning the CSS angle and/or a corner code.
func parseLinearDirection(s string) (deg float64, corner uint8, ok bool) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "to ") {
		sides := strings.Fields(s[len("to "):])
		return sideDirection(sides)
	}
	if d, ok := parseAngle(s); ok {
		return d, 0, true
	}
	return 0, 0, false
}

// sideDirection maps a `to <side(s)>` keyword set to an angle or corner code.
func sideDirection(sides []string) (deg float64, corner uint8, ok bool) {
	set := map[string]bool{}
	for _, s := range sides {
		set[s] = true
	}
	top, bottom, left, right := set["top"], set["bottom"], set["left"], set["right"]
	switch {
	case top && right:
		return 0, 1, true
	case bottom && right:
		return 0, 2, true
	case bottom && left:
		return 0, 3, true
	case top && left:
		return 0, 4, true
	case top:
		return 0, 0, true
	case right:
		return 90, 0, true
	case bottom:
		return 180, 0, true
	case left:
		return 270, 0, true
	}
	return 0, 0, false
}

// parseAngle parses a CSS angle (deg/grad/rad/turn; a bare number is degrees).
func parseAngle(s string) (float64, bool) {
	s = strings.TrimSpace(strings.ToLower(s))
	switch {
	case strings.HasSuffix(s, "deg"):
		return parseFloatUnit(s, "deg", 1)
	case strings.HasSuffix(s, "grad"):
		return parseFloatUnit(s, "grad", 0.9)
	case strings.HasSuffix(s, "rad"):
		return parseFloatUnit(s, "rad", 180/math.Pi)
	case strings.HasSuffix(s, "turn"):
		return parseFloatUnit(s, "turn", 360)
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f, true
	}
	return 0, false
}

func parseFloatUnit(s, unit string, scale float64) (float64, bool) {
	f, err := strconv.ParseFloat(strings.TrimSpace(s[:len(s)-len(unit)]), 64)
	if err != nil {
		return 0, false
	}
	return f * scale, true
}

// parseRadialGradient parses the argument list of a radial-gradient().
func parseRadialGradient(args []string, emRef float64, repeating bool) (*Gradient, bool) {
	g := &Gradient{
		Radial: true,
		Shape:  RadialEllipse,
		Extent: ExtentFarthestCorner,
		PosX:   Length{Percent: 0.5, IsPercent: true},
		PosY:   Length{Percent: 0.5, IsPercent: true},
	}
	_ = repeating
	first := strings.TrimSpace(args[0])
	stopArgs := args
	// The first argument is a shape/size/position prefix only when it is not a
	// colour stop (radial-gradient(red, blue) has no prefix).
	if !looksLikeStop(first) {
		parseRadialPrefix(g, first, emRef)
		stopArgs = args[1:]
	}
	stops, ok := parseColorStops(stopArgs, emRef)
	if !ok {
		return nil, false
	}
	g.Stops = stops
	return g, true
}

// parseRadialPrefix parses the `[shape || size] [at position]` prefix.
func parseRadialPrefix(g *Gradient, prefix string, emRef float64) {
	lp := strings.ToLower(prefix)
	sizePart := lp
	if i := strings.Index(lp, " at "); i >= 0 {
		sizePart = strings.TrimSpace(lp[:i])
		parseRadialPosition(g, strings.TrimSpace(lp[i+len(" at "):]), emRef)
	} else if strings.HasPrefix(lp, "at ") {
		parseRadialPosition(g, strings.TrimSpace(lp[len("at "):]), emRef)
		sizePart = ""
	}
	var lens []Length
	for _, tok := range strings.Fields(sizePart) {
		switch tok {
		case "circle":
			g.Shape = RadialCircle
		case "ellipse":
			g.Shape = RadialEllipse
		case "closest-side":
			g.Extent = ExtentClosestSide
		case "farthest-side":
			g.Extent = ExtentFarthestSide
		case "closest-corner":
			g.Extent = ExtentClosestCorner
		case "farthest-corner":
			g.Extent = ExtentFarthestCorner
		default:
			if l, ok := parseLength(tok, emRef); ok && !l.Auto {
				lens = append(lens, l)
			}
		}
	}
	if len(lens) >= 1 {
		g.Extent = ExtentExplicit
		g.RadiusX = lens[0]
		if len(lens) >= 2 {
			g.RadiusY = lens[1]
		} else {
			g.RadiusY = lens[0] // one length => circle radius
			g.Shape = RadialCircle
		}
	}
}

// parseRadialPosition parses the `at <position>` clause into PosX/PosY.
func parseRadialPosition(g *Gradient, pos string, emRef float64) {
	if p, ok := parseBgPositionValue(pos, emRef); ok {
		g.PosX, g.PosY = p.X, p.Y
	}
}

// looksLikeStop reports whether a gradient argument is a colour stop (starts
// with a colour) rather than a direction/shape prefix.
func looksLikeStop(arg string) bool {
	fields := strings.Fields(arg)
	if len(fields) == 0 {
		return false
	}
	_, ok := parseColor(fields[0])
	return ok
}

// parseColorStops parses the colour-stop list of a gradient. Each entry is a
// colour with an optional position; a single entry may also carry a second
// position (a stop with two positions expands to two stops of the same colour).
func parseColorStops(args []string, emRef float64) ([]ColorStop, bool) {
	var stops []ColorStop
	for _, a := range args {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		fields := strings.Fields(a)
		c, ok := parseColor(fields[0])
		if !ok {
			// A bare position with no colour is a transition hint; skip it (the
			// surrounding stops still interpolate correctly at this fidelity).
			continue
		}
		if len(fields) == 1 {
			stops = append(stops, ColorStop{Color: c})
			continue
		}
		for _, ptok := range fields[1:] {
			if l, ok := parseLength(ptok, emRef); ok && !l.Auto {
				stops = append(stops, ColorStop{Color: c, Pos: l, HasPos: true})
			}
		}
	}
	if len(stops) < 2 {
		return nil, false
	}
	return stops, true
}

// splitTopLevel splits s on sep, ignoring separators nested inside parentheses.
func splitTopLevelSep(s string, sep byte) []string {
	var out []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case sep:
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	out = append(out, s[start:])
	return out
}

// backgroundPositionSizeTokens extracts the "<position> / <size>" portion of
// a `background` shorthand value, if present: the single sub-value most real
// stylesheets use on each side of the '/' (a single keyword or length, e.g.
// "center/2rem") — not a multi-token position or size (e.g. "left 10px top /
// cover contain"), which no confirmed real usage needs. Splitting on '/'
// respects parentheses (via splitTopLevelSep) so a url()'s own path
// separators — a real, common shape: `background:no-repeat center/2rem
// url(/static/shared/icon/menu_gm_grey_24dp.svg)`, pkg.go.dev's own header
// hamburger-menu icon — are never mistaken for the shorthand's own
// position/size divider.
func backgroundPositionSizeTokens(v string) (pos, size string, ok bool) {
	parts := splitTopLevelSep(v, '/')
	if len(parts) != 2 {
		return "", "", false
	}
	before := strings.Fields(parts[0])
	after := strings.Fields(parts[1])
	if len(before) == 0 || len(after) == 0 {
		return "", "", false
	}
	return before[len(before)-1], after[0], true
}

// ---- background-size / position / repeat parsing ----

// parseBackgroundSizeList parses a (comma-separated) background-size value.
func parseBackgroundSizeList(v string, emRef float64) ([]BgSize, bool) {
	var out []BgSize
	for _, part := range splitTopLevelSep(v, ',') {
		if s, ok := parseBgSizeValue(strings.TrimSpace(part), emRef); ok {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

func parseBgSizeValue(part string, emRef float64) (BgSize, bool) {
	lp := strings.ToLower(part)
	switch lp {
	case "cover":
		return BgSize{Kind: SizeCover}, true
	case "contain":
		return BgSize{Kind: SizeContain}, true
	case "auto", "":
		return BgSize{Kind: SizeAuto}, true
	}
	fields := strings.Fields(lp)
	dims := make([]Length, 0, 2)
	for _, f := range fields {
		if f == "auto" {
			dims = append(dims, Length{Auto: true})
			continue
		}
		l, ok := parseLength(f, emRef)
		if !ok {
			return BgSize{}, false
		}
		dims = append(dims, l)
	}
	switch len(dims) {
	case 1:
		return BgSize{Kind: SizeExplicit, W: dims[0], H: Length{Auto: true}}, true
	case 2:
		return BgSize{Kind: SizeExplicit, W: dims[0], H: dims[1]}, true
	}
	return BgSize{}, false
}

// parseBackgroundPositionList parses a (comma-separated) background-position.
func parseBackgroundPositionList(v string, emRef float64) ([]BgPosition, bool) {
	var out []BgPosition
	for _, part := range splitTopLevelSep(v, ',') {
		if p, ok := parseBgPositionValue(strings.TrimSpace(part), emRef); ok {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

// parseBgPositionValue parses a one-or-two-component background-position.
func parseBgPositionValue(part string, emRef float64) (BgPosition, bool) {
	fields := strings.Fields(strings.ToLower(part))
	if len(fields) == 0 {
		return BgPosition{}, false
	}
	pct := func(f float64) Length { return Length{Percent: f, IsPercent: true} }
	// Single keyword/length: the other axis is centre.
	if len(fields) == 1 {
		switch fields[0] {
		case "left":
			return BgPosition{X: pct(0), Y: pct(0.5)}, true
		case "right":
			return BgPosition{X: pct(1), Y: pct(0.5)}, true
		case "top":
			return BgPosition{X: pct(0.5), Y: pct(0)}, true
		case "bottom":
			return BgPosition{X: pct(0.5), Y: pct(1)}, true
		case "center":
			return BgPosition{X: pct(0.5), Y: pct(0.5)}, true
		}
		if l, ok := parseLength(fields[0], emRef); ok && !l.Auto {
			return BgPosition{X: l, Y: pct(0.5)}, true
		}
		return BgPosition{}, false
	}
	x, okx := axisLength(fields[0], true, emRef)
	y, oky := axisLength(fields[1], false, emRef)
	if !okx || !oky {
		return BgPosition{}, false
	}
	return BgPosition{X: x, Y: y}, true
}

// axisLength resolves one background-position component. horizontal selects
// whether left/right keywords apply (else top/bottom).
func axisLength(tok string, horizontal bool, emRef float64) (Length, bool) {
	pct := func(f float64) Length { return Length{Percent: f, IsPercent: true} }
	switch tok {
	case "center":
		return pct(0.5), true
	case "left":
		if horizontal {
			return pct(0), true
		}
	case "right":
		if horizontal {
			return pct(1), true
		}
	case "top":
		if !horizontal {
			return pct(0), true
		}
	case "bottom":
		if !horizontal {
			return pct(1), true
		}
	}
	if l, ok := parseLength(tok, emRef); ok && !l.Auto {
		return l, true
	}
	return Length{}, false
}

// parseBackgroundRepeat parses a background-repeat value (first layer only, as
// all bench uses are single-layer).
func parseBackgroundRepeat(v string) (BgRepeat, bool) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "no-repeat":
		return NoRepeat, true
	case "repeat":
		return RepeatBoth, true
	case "repeat-x":
		return RepeatX, true
	case "repeat-y":
		return RepeatY, true
	case "space", "round":
		return RepeatBoth, true
	}
	return RepeatBoth, false
}
