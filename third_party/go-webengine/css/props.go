// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package css

import (
	"strconv"
	"strings"
)

// parseLineHeight parses a line-height value: the keyword normal, a unitless
// multiplier (relative to the font-size emRef), a px/em length, or a percentage.
func parseLineHeight(v string, emRef float64) (LineHeight, bool) {
	lv := strings.ToLower(strings.TrimSpace(v))
	if lv == "normal" {
		return LineHeight{Normal: true}, true
	}
	// A bare number (no unit) is a UNITLESS multiplier of the font-size. It is
	// kept as a factor (not collapsed to px here) so it inherits as the number
	// and each descendant re-multiplies by its own font-size (CSS 2.1 §10.8.1).
	if f, err := strconv.ParseFloat(lv, 64); err == nil {
		if f < 0 {
			return LineHeight{}, false
		}
		return LineHeight{Factor: f}, true
	}
	if l, ok := parseLength(v, emRef); ok && !l.Auto {
		if l.IsPercent {
			return LineHeight{Px: l.Percent * emRef}, true
		}
		return LineHeight{Px: l.Px}, true
	}
	return LineHeight{}, false
}

// parseListStyleType maps a list-style-type keyword to a ListStyleType.
func parseListStyleType(lv string) (ListStyleType, bool) {
	switch lv {
	case "disc":
		return ListDisc, true
	case "circle":
		return ListCircle, true
	case "square":
		return ListSquare, true
	case "decimal":
		return ListDecimal, true
	case "none":
		return ListNone, true
	}
	return 0, false
}

// applyListStyle parses the list-style shorthand, enough to set the marker type
// and position. A recognised type keyword sets ListStyleType; inside/outside set
// the position; a list-style-image (url()) or any other token is ignored at this
// fidelity (the shorthand's `none` is treated as type none).
func applyListStyle(s *Style, lv string) {
	for _, tok := range strings.Fields(lv) {
		switch tok {
		case "inside":
			s.ListStylePosition = ListInside
		case "outside":
			s.ListStylePosition = ListOutside
		default:
			if t, ok := parseListStyleType(tok); ok {
				s.ListStyleType = t
			}
		}
	}
}

func parseJustify(lv string) (Justify, bool) {
	switch lv {
	case "flex-start", "start", "left", "normal":
		return JustifyStart, true
	case "flex-end", "end", "right":
		return JustifyEnd, true
	case "center":
		return JustifyCenter, true
	case "space-between":
		return JustifySpaceBetween, true
	case "space-around":
		return JustifySpaceAround, true
	case "space-evenly":
		return JustifySpaceEvenly, true
	}
	return 0, false
}

func parseAlignItems(lv string) (AlignItems, bool) {
	switch lv {
	case "stretch", "normal":
		return AlignStretch, true
	case "flex-start", "start", "self-start", "baseline":
		return AlignFlexStart, true
	case "flex-end", "end", "self-end":
		return AlignFlexEnd, true
	case "center":
		return AlignCenterItems, true
	}
	return 0, false
}

func parseFlexWrap(lv string) (FlexWrap, bool) {
	switch lv {
	case "nowrap":
		return FlexNoWrap, true
	case "wrap":
		return FlexWrapOn, true
	case "wrap-reverse":
		return FlexWrapReverse, true
	}
	return 0, false
}

func parseAlignContent(lv string) (AlignContent, bool) {
	switch lv {
	case "stretch", "normal":
		return AlignContentStretch, true
	case "flex-start", "start":
		return AlignContentStart, true
	case "flex-end", "end":
		return AlignContentEnd, true
	case "center":
		return AlignContentCenter, true
	case "space-between":
		return AlignContentSpaceBetween, true
	case "space-around":
		return AlignContentSpaceAround, true
	case "space-evenly":
		return AlignContentSpaceEvenly, true
	}
	return 0, false
}

func parseAlignSelf(lv string) (AlignSelf, bool) {
	switch lv {
	case "auto":
		return AlignSelfAuto, true
	case "stretch", "normal":
		return AlignSelfStretch, true
	case "flex-start", "start", "self-start", "baseline":
		return AlignSelfStart, true
	case "flex-end", "end", "self-end":
		return AlignSelfEnd, true
	case "center":
		return AlignSelfCenter, true
	}
	return 0, false
}

// applyFlexFlow parses the flex-flow shorthand (flex-direction and/or
// flex-wrap, in any order).
func applyFlexFlow(s *Style, v string) {
	for _, f := range strings.Fields(strings.ToLower(v)) {
		switch f {
		case "row", "row-reverse":
			s.FlexDirection = FlexRow
		case "column", "column-reverse":
			s.FlexDirection = FlexColumn
		default:
			if w, ok := parseFlexWrap(f); ok {
				s.FlexWrap = w
			}
		}
	}
}

// applyFlexShorthand parses the flex shorthand. It supports the common forms:
// a single number (grow), "none" (0 0 auto), "auto" (1 1 auto), "initial"
// (0 1 auto), and "<grow> <shrink>? <basis>?".
func applyFlexShorthand(s *Style, v string, emRef float64) {
	fields := strings.Fields(strings.ToLower(v))
	switch {
	case len(fields) == 1 && fields[0] == "none":
		s.FlexGrow, s.FlexShrink, s.FlexBasis = 0, 0, Length{Auto: true}
		return
	case len(fields) == 1 && fields[0] == "auto":
		s.FlexGrow, s.FlexShrink, s.FlexBasis = 1, 1, Length{Auto: true}
		return
	case len(fields) == 1 && fields[0] == "initial":
		s.FlexGrow, s.FlexShrink, s.FlexBasis = 0, 1, Length{Auto: true}
		return
	}
	// Positional: grow [shrink] [basis]. A token that parses as a length (and is
	// not a bare number) is the basis; bare numbers fill grow then shrink.
	sawGrow, sawShrink := false, false
	// Default basis for the numeric form is 0 (per spec flex: 1 → 1 1 0).
	s.FlexGrow, s.FlexShrink, s.FlexBasis = 0, 1, Length{Px: 0}
	for _, f := range fields {
		if l, ok := parseLength(f, emRef); ok && (l.Auto || l.IsPercent || hasUnit(f)) {
			s.FlexBasis = l
			continue
		}
		if n, err := strconv.ParseFloat(f, 64); err == nil && n >= 0 {
			if !sawGrow {
				s.FlexGrow, sawGrow = n, true
			} else if !sawShrink {
				s.FlexShrink, sawShrink = n, true
			}
		}
	}
}

// hasUnit reports whether a token carries a length unit (so a bare number is
// treated as a flex factor rather than a basis): em/rem, %, auto, or any of
// the absolute units parseLength understands (px, in, cm, mm, Q, pt, pc).
func hasUnit(f string) bool {
	f = strings.ToLower(f)
	if strings.HasSuffix(f, "em") || strings.HasSuffix(f, "%") || f == "auto" {
		return true
	}
	_, unit := splitUnit(f)
	_, ok := absoluteUnitPx[unit]
	return ok
}

// parseBorderRadius parses a border-radius value into a single uniform radius.
// It takes the first horizontal radius: the elliptical `h / v` form keeps the
// horizontal component, and a multi-corner list (`8px 4px …`) keeps the first
// value. px/em lengths and percentages are accepted; auto and unparseable
// values are rejected. A negative radius is clamped to zero.
func parseBorderRadius(v string, emRef float64) (Length, bool) {
	// Drop any vertical-radius component after a slash (elliptical corners).
	if i := strings.IndexByte(v, '/'); i >= 0 {
		v = v[:i]
	}
	fields := strings.Fields(strings.TrimSpace(v))
	if len(fields) == 0 {
		return Length{}, false
	}
	l, ok := parseLength(fields[0], emRef)
	if !ok || l.Auto {
		return Length{}, false
	}
	if !l.IsPercent && l.Px < 0 {
		l.Px = 0
	}
	return l, true
}

// borderKeywordWidth maps the thin/medium/thick keywords to pixel widths.
func borderKeywordWidth(tok string) (float64, bool) {
	switch tok {
	case "thin":
		return 1, true
	case "medium":
		return 3, true
	case "thick":
		return 5, true
	}
	return 0, false
}

// borderStyleKeyword maps a border-style token to our BorderStyle, reporting
// whether the token was a recognised style keyword.
func borderStyleKeyword(tok string) (BorderStyle, bool) {
	switch tok {
	case "none", "hidden":
		return BorderNone, true
	case "solid", "dashed", "dotted", "double", "groove", "ridge", "inset", "outset":
		return BorderSolid, true
	}
	return BorderNone, false
}

// tokenizeKeepingParens splits v on whitespace but keeps a functional token and
// its parenthesised argument list together (so `rgb(230 120 40 / 1)` — the
// modern space-separated colour syntax — survives as one token rather than being
// shredded into "rgb(230", "120", …). Nested parens are balanced.
func tokenizeKeepingParens(v string) []string {
	var toks []string
	var cur strings.Builder
	depth := 0
	flush := func() {
		if cur.Len() > 0 {
			toks = append(toks, cur.String())
			cur.Reset()
		}
	}
	for i := 0; i < len(v); i++ {
		ch := v[i]
		switch {
		case ch == '(':
			depth++
			cur.WriteByte(ch)
		case ch == ')':
			if depth > 0 {
				depth--
			}
			cur.WriteByte(ch)
		case (ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' || ch == '\f') && depth == 0:
			flush()
		default:
			cur.WriteByte(ch)
		}
	}
	flush()
	return toks
}

// applyBorderSideShorthand parses "<width> <style> <color>" (any order) into one
// side. Missing colour defaults to the element's current colour.
func applyBorderSideShorthand(side *BorderSide, v string, emRef float64, cur Color) {
	var out BorderSide
	out.Color = cur
	sawColor := false
	for _, tok := range tokenizeKeepingParens(strings.ToLower(v)) {
		if w, ok := borderKeywordWidth(tok); ok {
			out.Width = w
			continue
		}
		if st, ok := borderStyleKeyword(tok); ok {
			out.Style = st
			continue
		}
		if l, ok := parseLength(tok, emRef); ok && !l.Auto && !l.IsPercent {
			out.Width = l.Px
			continue
		}
		if c, ok := parseColor(tok); ok {
			out.Color, sawColor = c, true
		}
	}
	_ = sawColor
	*side = out
}

func applyBorderShorthand(b *Borders, v string, emRef float64, cur Color) {
	var side BorderSide
	side.Color = cur
	applyBorderSideShorthand(&side, v, emRef, cur)
	b.Top, b.Right, b.Bottom, b.Left = side, side, side, side
}

// fourValues returns the top,right,bottom,left expansion of a 1-to-4 token list.
func fourValues[T any](vals []T) (t, r, bo, l T, ok bool) {
	switch len(vals) {
	case 1:
		return vals[0], vals[0], vals[0], vals[0], true
	case 2:
		return vals[0], vals[1], vals[0], vals[1], true
	case 3:
		return vals[0], vals[1], vals[2], vals[1], true
	case 4:
		return vals[0], vals[1], vals[2], vals[3], true
	}
	var z T
	return z, z, z, z, false
}

func applyBorderWidth(b *Borders, v string, emRef float64) {
	fields := strings.Fields(strings.ToLower(v))
	widths := make([]float64, 0, len(fields))
	for _, f := range fields {
		if w, ok := borderKeywordWidth(f); ok {
			widths = append(widths, w)
		} else if l, ok := parseLength(f, emRef); ok && !l.Auto && !l.IsPercent {
			widths = append(widths, l.Px)
		} else {
			return
		}
	}
	if t, r, bo, l, ok := fourValues(widths); ok {
		b.Top.Width, b.Right.Width, b.Bottom.Width, b.Left.Width = t, r, bo, l
	}
}

func applyBorderStyle(b *Borders, v string) {
	fields := strings.Fields(strings.ToLower(v))
	styles := make([]BorderStyle, 0, len(fields))
	for _, f := range fields {
		st, ok := borderStyleKeyword(f)
		if !ok {
			return
		}
		styles = append(styles, st)
	}
	if t, r, bo, l, ok := fourValues(styles); ok {
		b.Top.Style, b.Right.Style, b.Bottom.Style, b.Left.Style = t, r, bo, l
	}
}

func applyBorderColor(b *Borders, v string) {
	fields := tokenizeKeepingParens(v)
	cols := make([]Color, 0, len(fields))
	for _, f := range fields {
		c, ok := parseColor(f)
		if !ok {
			return
		}
		cols = append(cols, c)
	}
	if t, r, bo, l, ok := fourValues(cols); ok {
		b.Top.Color, b.Right.Color, b.Bottom.Color, b.Left.Color = t, r, bo, l
	}
}

func applyBorderEdgeWidth(side *BorderSide, v string, emRef float64) {
	lv := strings.ToLower(strings.TrimSpace(v))
	if w, ok := borderKeywordWidth(lv); ok {
		side.Width = w
		return
	}
	if l, ok := parseLength(v, emRef); ok && !l.Auto && !l.IsPercent {
		side.Width = l.Px
	}
}

// applyMarginShorthand parses the 1-to-4 margin shorthand, honouring `auto` on
// the left and right sides (used for horizontal centring) and a percentage on
// the left and right sides (see Style.MarginLeftIsPercent's own doc comment
// for why only left/right, not top/bottom).
func applyMarginShorthand(s *Style, v string, emRef float64) {
	fields := strings.Fields(strings.ToLower(v))
	type mv struct {
		px        float64
		auto      bool
		isPercent bool
		percent   float64
	}
	vals := make([]mv, 0, len(fields))
	for _, f := range fields {
		if f == "auto" {
			vals = append(vals, mv{auto: true})
			continue
		}
		l, ok := parseLength(f, emRef)
		if !ok {
			return
		}
		if l.IsPercent {
			vals = append(vals, mv{isPercent: true, percent: l.Percent})
		} else {
			vals = append(vals, mv{px: l.Px})
		}
	}
	t, r, bo, l, ok := fourValues(vals)
	if !ok {
		return
	}
	// top/bottom auto and top/bottom percentages both collapse to 0 in block
	// flow — see Style.MarginLeftIsPercent's doc comment for the latter.
	s.Margin.Top, s.Margin.Bottom = t.px, bo.px
	s.Margin.Left, s.MarginLeftAuto = l.px, l.auto
	s.MarginLeftIsPercent, s.MarginLeftPercent = l.isPercent, l.percent
	s.Margin.Right, s.MarginRightAuto = r.px, r.auto
	s.MarginRightIsPercent, s.MarginRightPercent = r.isPercent, r.percent
}

// applyMarginSide sets one horizontal margin, recording an `auto` or
// percentage value (percentFlag/percentDst — see Style.MarginLeftIsPercent's
// own doc comment).
func applyMarginSide(dst *float64, autoFlag *bool, percentFlag *bool, percentDst *float64, v string, emRef float64) {
	lv := strings.ToLower(strings.TrimSpace(v))
	if lv == "auto" {
		*autoFlag = true
		*percentFlag = false
		*dst = 0
		return
	}
	l, ok := parseLength(v, emRef)
	if !ok || l.Auto {
		return
	}
	*autoFlag = false
	if l.IsPercent {
		*percentFlag = true
		*percentDst = l.Percent
	} else {
		*percentFlag = false
		*dst = l.Px
	}
}
