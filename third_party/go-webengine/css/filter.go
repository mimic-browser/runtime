// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package css

import (
	"math"
	"strconv"
	"strings"
)

// FilterKind identifies one CSS `filter` function.
type FilterKind int

const (
	// FilterBlur is blur(<length>): a Gaussian blur whose standard deviation is
	// Amount pixels.
	FilterBlur FilterKind = iota
	// FilterBrightness is brightness(<number|percentage>): a per-channel linear
	// multiply by Amount (Amount>=0, may exceed 1).
	FilterBrightness
	// FilterContrast is contrast(<number|percentage>): scales each channel about
	// the 0.5 mid-point by Amount.
	FilterContrast
	// FilterSaturate is saturate(<number|percentage>): a saturation matrix with
	// Amount as the saturation factor (may exceed 1).
	FilterSaturate
	// FilterGrayscale is grayscale(<number|percentage>): interpolates towards
	// luminance by Amount in [0,1].
	FilterGrayscale
	// FilterSepia is sepia(<number|percentage>): interpolates towards a sepia
	// tone by Amount in [0,1].
	FilterSepia
	// FilterHueRotate is hue-rotate(<angle>): rotates hue by Amount radians.
	FilterHueRotate
	// FilterInvert is invert(<number|percentage>): interpolates towards the
	// negative by Amount in [0,1].
	FilterInvert
	// FilterDropShadow is drop-shadow(<offset-x> <offset-y> <blur>? <color>?):
	// a blurred, offset, coloured copy of the element's alpha painted behind it.
	FilterDropShadow
)

// Filter is one parsed `filter` function.
type Filter struct {
	Kind   FilterKind
	Amount float64 // multiplier / fraction / blur sigma (px) / hue radians

	// Drop-shadow parameters (only meaningful when Kind == FilterDropShadow).
	OffsetX, OffsetY float64
	Blur             float64
	Color            Color
	UseCurrentColor  bool // resolve Color from the element's `color` at paint time
}

// parseFilterList parses a `filter` value: a whitespace-separated chain of
// filter functions applied in order. The keyword `none` yields a nil (cleared)
// list with ok=true. It reports false when no function is valid (leaving the
// property unset) so a malformed value does not silently drop filtering.
func parseFilterList(v string, emRef float64) ([]Filter, bool) {
	if strings.EqualFold(strings.TrimSpace(v), "none") {
		return nil, true
	}
	var out []Filter
	for _, tok := range tokenizeKeepingParens(v) {
		if f, ok := parseFilterFunc(tok, emRef); ok {
			out = append(out, f)
		} else {
			// A single unrecognised or malformed function invalidates the whole
			// declaration (CSS parses `filter` as a list; one bad component drops
			// the property), matching browser behaviour.
			return nil, false
		}
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

// parseFilterFunc parses one `name(args)` filter function.
func parseFilterFunc(tok string, emRef float64) (Filter, bool) {
	open := strings.IndexByte(tok, '(')
	if open < 0 || !strings.HasSuffix(tok, ")") {
		return Filter{}, false
	}
	name := strings.ToLower(strings.TrimSpace(tok[:open]))
	args := strings.TrimSpace(tok[open+1 : len(tok)-1])
	switch name {
	case "blur":
		return parseBlurFunc(args, emRef)
	case "brightness":
		return amountFilter(FilterBrightness, args, 1, false)
	case "contrast":
		return amountFilter(FilterContrast, args, 1, false)
	case "saturate":
		return amountFilter(FilterSaturate, args, 1, false)
	case "grayscale":
		return amountFilter(FilterGrayscale, args, 1, true)
	case "sepia":
		return amountFilter(FilterSepia, args, 1, true)
	case "invert":
		return amountFilter(FilterInvert, args, 1, true)
	case "hue-rotate":
		return parseHueRotateFunc(args)
	case "drop-shadow":
		return parseDropShadowFunc(args, emRef)
	}
	return Filter{}, false
}

// parseBlurFunc parses blur(<length>). An empty argument defaults to 0 (no
// blur). Percentages and `auto` are rejected; a negative radius is invalid.
func parseBlurFunc(args string, emRef float64) (Filter, bool) {
	if args == "" {
		return Filter{Kind: FilterBlur, Amount: 0}, true
	}
	l, ok := parseLength(args, emRef)
	if !ok || l.Auto || l.IsPercent || l.Px < 0 {
		return Filter{}, false
	}
	return Filter{Kind: FilterBlur, Amount: l.Px}, true
}

// amountFilter parses a number-or-percentage amount for the component-transfer
// filters. def is the default when the argument is empty. clampOne caps the
// amount at 1 (grayscale/sepia/invert saturate at fully-applied); all amounts
// clamp at 0 below.
func amountFilter(kind FilterKind, args string, def float64, clampOne bool) (Filter, bool) {
	amt := def
	if args != "" {
		v, ok := parseNumberOrPercent(args)
		if !ok {
			return Filter{}, false
		}
		amt = v
	}
	if amt < 0 {
		amt = 0
	}
	if clampOne && amt > 1 {
		amt = 1
	}
	return Filter{Kind: kind, Amount: amt}, true
}

// parseHueRotateFunc parses hue-rotate(<angle>) into radians. An empty argument
// defaults to 0deg. deg/grad/rad/turn units are accepted; a bare 0 is allowed.
func parseHueRotateFunc(args string) (Filter, bool) {
	if args == "" {
		return Filter{Kind: FilterHueRotate, Amount: 0}, true
	}
	// parseAngle (shared with gradients) returns degrees; convert to radians.
	deg, ok := parseAngle(args)
	if !ok {
		return Filter{}, false
	}
	return Filter{Kind: FilterHueRotate, Amount: deg * math.Pi / 180}, true
}

// parseDropShadowFunc parses drop-shadow(<offset-x> <offset-y> <blur>? <color>?).
// Unlike box-shadow there is no inset keyword and no spread. A missing colour
// resolves to the element's currentColor at paint time.
func parseDropShadowFunc(args string, emRef float64) (Filter, bool) {
	f := Filter{Kind: FilterDropShadow, UseCurrentColor: true}
	var lens []float64
	for _, t := range strings.Fields(args) {
		if strings.EqualFold(t, "currentcolor") {
			f.UseCurrentColor = true
			continue
		}
		if l, ok := parseLength(t, emRef); ok && !l.Auto && !l.IsPercent {
			lens = append(lens, l.Px)
			continue
		}
		if c, ok := parseColor(t); ok {
			f.Color, f.UseCurrentColor = c, false
			continue
		}
		return Filter{}, false
	}
	if len(lens) < 2 || len(lens) > 3 {
		return Filter{}, false
	}
	f.OffsetX, f.OffsetY = lens[0], lens[1]
	if len(lens) == 3 {
		if lens[2] < 0 {
			return Filter{}, false
		}
		f.Blur = lens[2]
	}
	return f, true
}

// parseNumberOrPercent parses a filter amount: a bare number (1.0 == unchanged)
// or a percentage (100% == unchanged, returned as the 0..n fraction).
func parseNumberOrPercent(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "%") {
		if p, err := strconv.ParseFloat(strings.TrimSpace(s[:len(s)-1]), 64); err == nil && isFinite(p) {
			return p / 100, true
		}
		return 0, false
	}
	if n, err := strconv.ParseFloat(s, 64); err == nil && isFinite(n) {
		return n, true
	}
	return 0, false
}

// isFinite reports whether v is neither NaN nor an infinity.
func isFinite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
