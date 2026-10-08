// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package css

import (
	"regexp"
	"strconv"
	"strings"
)

// CSS absolute font-size keywords use the browser's medium size as their
// reference, rather than the parent element's computed font size.
func absoluteFontSizeKeyword(v string) (float64, bool) {
	switch v {
	case "xx-small":
		return 9, true
	case "x-small":
		return 10, true
	case "small":
		return 13.333333333, true
	case "medium":
		return 16, true
	case "large":
		return 18, true
	case "x-large":
		return 24, true
	case "xx-large":
		return 32, true
	case "xxx-large":
		return 48, true
	default:
		return 0, false
	}
}

// mediaWidthRe captures min-width/max-width features in a media query, in
// either px or rem — Tailwind v4's default breakpoints (sm/md/lg/xl/2xl) are
// all expressed in rem ("min-width:80rem"), not px, and rejecting the unit
// entirely (rather than just failing to convert it) used to make mediaMatches
// silently ignore every one of them, treating every responsive breakpoint as
// permanently active — observed live as tailwindcss.com's hero headline
// rendering far larger than any real viewport width should select, because
// its "xl:text-8xl" breakpoint (min-width:80rem = 1280px) matched even at a
// 1024px viewport.
var mediaWidthRe = regexp.MustCompile(`(min|max)-width\s*:\s*([0-9.]+)(px|rem)`)

// mediaWidthCmpRe captures the CSS Media Queries Level 4 range-comparison
// syntax for width ("width<=48rem", "width>=48rem", or the value-first order
// "48rem<=width"), in px or rem. GitHub's Primer design system uses exactly
// this form for its responsive PageLayout breakpoints; it was as invisible to
// the old min-width:/max-width: colon-only matcher as the missing "rem" unit
// was, for the same reason — falling through to "unknown feature, assume it
// matches" made every one of these breakpoints permanently active regardless
// of viewport width.
var mediaWidthCmpRe = regexp.MustCompile(
	`width\s*(<=|>=|<|>)\s*([0-9.]+)(px|rem)|([0-9.]+)(px|rem)\s*(<=|>=|<|>)\s*width`)

// mediaHoverRe and mediaPointerRe capture the input-capability media
// features (Level 4's "interaction media features"), each with an optional
// "any-" prefix ("hover"/"any-hover" report the PRIMARY vs. ANY available
// input mechanism — this engine draws no distinction between the two, since
// it models exactly one, mouse-equipped input device either way). Unlike
// min-width/max-width and calc() above, these are NOT resolved via the
// generic "unknown feature: assume it matches" default (see
// mediaQueryMatches's own doc comment) — that default is right for a feature
// like colour-gamut, where optimistically matching still describes a
// plausible desktop browser, but wrong here: `(hover:none)`/`(pointer:
// coarse)` specifically ask "is the input a touchscreen", and this engine's
// one rendering context is a mouse-equipped desktop browser (matching the
// headless Chrome this project measures itself against), so those queries
// must evaluate false, not "match optimistically". Found live on
// github.com: `@media (hover:none){.markdown-body h1 .octicon-link,...{
// visibility:visible!important}}` — a touch-device fallback making a
// heading's hover-revealed permalink icon always visible, since a
// touchscreen user can never trigger `:hover` to reveal it another way —
// wrongly matched here and showed the icon on every heading unconditionally.
var (
	mediaHoverRe   = regexp.MustCompile(`(?:any-)?hover\s*:\s*(hover|none)`)
	mediaPointerRe = regexp.MustCompile(`(?:any-)?pointer\s*:\s*(fine|coarse|none)`)
)

// inputFeaturesHold reports whether every hover/pointer feature in cond holds
// for this engine's single assumed rendering context: a mouse-equipped
// desktop browser. A condition with no such feature holds (unaffected by this
// check either way).
func inputFeaturesHold(cond string) bool {
	for _, m := range mediaHoverRe.FindAllStringSubmatch(cond, -1) {
		if m[1] != "hover" {
			return false
		}
	}
	for _, m := range mediaPointerRe.FindAllStringSubmatch(cond, -1) {
		if m[1] != "fine" {
			return false
		}
	}
	return true
}

// evalMediaCalcs replaces every calc(...) call in cond with its evaluated
// pixel length (via the general calc() evaluator in calc.go — resolveCalc's
// own evalCalcExpr, already used for ordinary property values), so
// mediaWidthRe/mediaWidthCmpRe (which only ever match a bare
// "<number><unit>", never an expression) can then read it like any other
// length. A real breakpoint is rarely a single "calc(A ± B)" two-term
// expression this package's PREVIOUS media-only evaluator handled: MDN's own
// reference-article layout breakpoint is
// `calc(1rem * 2 + 15rem + 2rem + 31rem)` — a multiplication by a bare
// scalar plus a chain of four additive terms — which that narrower
// evaluator left as unparsed text, so mediaWidthCmpRe then found no length
// there and mediaMatches fell through to its "unknown feature: assume it
// matches" default, making a MOBILE-ONLY breakpoint (real width 800px)
// match at every viewport including a 1024px desktop one: MDN's article
// page then always applied the narrow-viewport `display:block` override to
// its CSS Grid sidebar layout, stacking the table-of-contents below the
// article body instead of beside it. A calc() outside what evalCalcExpr
// resolves (min()/max()/clamp(), a percentage, an em/vw/... term) is left as
// unparsed text, exactly as before.
func evalMediaCalcs(cond string) string {
	var b strings.Builder
	i := 0
	for {
		rel := strings.Index(cond[i:], "calc(")
		if rel < 0 {
			b.WriteString(cond[i:])
			break
		}
		start := i + rel
		b.WriteString(cond[i:start])
		open := start + len("calc")
		end, ok := matchParen(cond, open)
		if !ok {
			b.WriteString(cond[start:])
			break
		}
		if px, hasUnit, ok := evalCalcExpr(cond[open+1 : end]); ok && hasUnit {
			b.WriteString(strconv.FormatFloat(px, 'f', -1, 64))
			b.WriteString("px")
		} else {
			b.WriteString(cond[start : end+1])
		}
		i = end + 1
	}
	return b.String()
}

// Declaration is a single property: value pair. Important marks a trailing
// `!important` on the original declaration; it is consulted by the cascade,
// never by a property's value parser (Value never carries the marker).
type Declaration struct {
	Property  string
	Value     string
	Important bool
}

// Rule is a parsed style rule: a list of selectors sharing a declaration block.
type Rule struct {
	Selectors    []Selector
	Declarations []Declaration

	// Container is non-nil when this rule came from (directly, or via nested
	// @media/@layer) an `@container` at-rule: its selectors only take effect
	// for a matched element when the condition holds against that element's
	// nearest qualifying ancestor container, evaluated at cascade time (see
	// container.go — this is deferred, unlike @media, because it depends on
	// per-element ancestor geometry rather than a single known viewport
	// width).
	Container *ContainerCondition
}

// stripComments removes /* ... */ comment spans.
func stripComments(s string) string {
	var b strings.Builder
	for {
		i := strings.Index(s, "/*")
		if i < 0 {
			b.WriteString(s)
			break
		}
		b.WriteString(s[:i])
		j := strings.Index(s[i+2:], "*/")
		if j < 0 {
			break // unterminated comment: drop the rest
		}
		s = s[i+2+j+2:]
	}
	return b.String()
}

// DefaultViewportWidth is the viewport width (CSS px) used to evaluate @media
// width queries when no explicit width is supplied. It matches a desktop render.
const DefaultViewportWidth = 1024

// ParseStylesheet parses a full stylesheet into rules at the default viewport
// width. See ParseStylesheetVW to control @media evaluation.
func ParseStylesheet(src string) []Rule {
	return ParseStylesheetVW(src, DefaultViewportWidth)
}

// ParseStylesheetVW parses a full stylesheet into rules, evaluating @media
// blocks against viewport width vw: a matching @media block's inner rules are
// included, a non-matching one is skipped. @layer blocks are unwrapped (their
// rules included as if the layer boundary were not there — this engine does
// not model cross-layer cascade priority, but that is far less wrong than
// dropping the content: Tailwind v4's default output, among many other
// frameworks, puts nearly all of its CSS inside @layer). @container blocks are
// always included, with their condition attached to each inner rule for the
// cascade to evaluate per-element once real layout geometry is available (see
// container.go) — @container style(...) queries are not supported and are
// skipped wholesale, like any other unrecognised at-rule (@font-face,
// @keyframes, @import, ...). Recognised @supports conditions are evaluated;
// unrecognised conditions are false. Malformed rules are skipped
// defensively.
func ParseStylesheetVW(src string, vw float64) []Rule {
	return ParseStylesheetMedia(src, Media{Width: vw})
}

// ParseStylesheetMedia is ParseStylesheetVW evaluated against a Media — the
// entry point that makes "@media print" blocks apply.
func ParseStylesheetMedia(src string, m Media) []Rule {
	return parseRules(stripComments(src), m)
}

func parseRules(src string, m Media) []Rule {
	var rules []Rule
	i := 0
	for i < len(src) {
		brace := strings.IndexByte(src[i:], '{')
		if brace < 0 {
			break
		}
		prelude := strings.TrimSpace(src[i : i+brace])
		blockStart := i + brace
		blockEnd, ok := matchBrace(src, blockStart)
		if !ok {
			break
		}
		body := src[blockStart+1 : blockEnd]
		i = blockEnd + 1

		// A bare at-rule statement ending in ';' (e.g. "@layer theme, base,
		// utilities;" declaring layer order, or "@import url(x);") has no '{' of
		// its own, so it rides along in the SAME prelude text as whatever
		// construct actually owns this brace. Only the text after the last such
		// ';' describes that construct.
		if semi := strings.LastIndexByte(prelude, ';'); semi >= 0 {
			prelude = strings.TrimSpace(prelude[semi+1:])
		}

		if strings.HasPrefix(prelude, "@") {
			lower := strings.ToLower(prelude)
			switch {
			case strings.HasPrefix(lower, "@media"):
				// Honour @media blocks whose width query matches the viewport; the
				// inner body is itself a list of rules.
				if mediaMatchesOn(lower[len("@media"):], m) {
					rules = append(rules, parseRules(body, m)...)
				}
			case strings.HasPrefix(lower, "@layer"):
				// A named layer's body is itself a list of rules; a bare "@layer
				// name{...}" (anonymous or named) always applies — there is no
				// width/media condition to test, only a cascade PRIORITY this
				// engine does not model (see the doc comment above).
				rules = append(rules, parseRules(body, m)...)
			case strings.HasPrefix(lower, "@container"):
				// Unlike @media/@layer, an @container condition cannot be resolved
				// here: it depends on an ANCESTOR ELEMENT's actual laid-out size,
				// which is per-matched-element and only known once layout has run
				// at least once. So the body is always parsed and included, with
				// the condition attached to every rule that comes out of it (see
				// container.go); the cascade evaluates it per-element, per-pass. A
				// condition this engine cannot represent at all (@container
				// style(...), a different, newer part of the spec) makes
				// parseContainerCondition report ok=false, and the body is then
				// dropped wholesale, like any other unrecognised at-rule.
				if cond, ok := parseContainerCondition(prelude); ok {
					inner := parseRules(body, m)
					for i := range inner {
						inner[i].Container = mergeContainerCondition(inner[i].Container, cond)
					}
					rules = append(rules, inner...)
				}
			case strings.HasPrefix(lower, "@supports"):
				// Include only feature queries backed by this renderer.
				if supportsConditionHolds(lower[len("@supports"):]) {
					rules = append(rules, parseRules(body, m)...)
				}
			}
			// Every other at-rule (@font-face, @keyframes, an @supports whose
			// condition supportsConditionHolds does not recognise, ...) is
			// skipped wholesale, as before.
			continue
		}
		sels := ParseSelectorList(prelude)
		if len(sels) > 0 {
			decls := ParseDeclarations(body)
			if len(decls) > 0 {
				rules = append(rules, Rule{Selectors: sels, Declarations: decls})
			}
		}
	}
	return rules
}

// notAllAndPrefix matches a leading "not all and" — CSS's own idiom for
// negating a single feature test, which "all" (a media type that always
// matches) reduces to "not (the feature)". Tailwind v4 compiles EVERY
// "max-*:" breakpoint variant (max-sm:, and the upper bound of a compound
// range like sm:max-md:) to exactly this shape — `@media not all and
// (min-width:40rem)` for max-sm:, confirmed live on tailwindcss.com's own
// homepage — rather than a direct max-width feature. Before this was
// recognised, mediaMatches evaluated the min-width feature INSIDE the
// parens completely normally and silently ignored the "not", so a
// narrow-viewport-only utility (e.g. a "text-4xl" label meant to show only
// below the sm breakpoint) matched at every viewport ABOVE it instead —
// the opposite of what the rule means — because the wrapped feature (here,
// "min-width:40rem") is exactly the condition that should be negated, and a
// match without negation gives precisely the inverted answer.
//
// The general rule — a leading "not" negates the whole query — now lives in
// mediaQueryMatches (media.go); this regexp is kept as the documented shape
// of the idiom that first exposed the bug.
var notAllAndPrefix = regexp.MustCompile(`(?i)^\s*not\s+all\s+and\s+`)

// mediaMatches evaluates a @media condition for screen at viewport width vw
// — see mediaMatchesOn (media.go) for the rules, and Media for print.
func mediaMatches(cond string, vw float64) bool {
	return mediaMatchesOn(cond, Media{Width: vw})
}

// widthFeaturesHold reports whether every min-width/max-width feature in cond
// (colon syntax and the Level 4 comparison syntax, calc() resolved first)
// holds at viewport width vw. A condition with no width feature holds.
func widthFeaturesHold(cond string, vw float64) bool {
	cond = evalMediaCalcs(cond)
	for _, m := range mediaWidthRe.FindAllStringSubmatch(cond, -1) {
		if _, err := strconv.ParseFloat(m[2], 64); err != nil {
			continue
		}
		n := lengthToPx(m[2], m[3])
		if m[1] == "min" && vw < n {
			return false
		}
		if m[1] == "max" && vw > n {
			return false
		}
	}
	for _, m := range mediaWidthCmpRe.FindAllStringSubmatch(cond, -1) {
		var op, numStr, unit string
		if m[1] != "" {
			op, numStr, unit = m[1], m[2], m[3]
		} else {
			numStr, unit, op = m[4], m[5], flipCmp(m[6])
		}
		if _, err := strconv.ParseFloat(numStr, 64); err != nil {
			continue
		}
		n := lengthToPx(numStr, unit)
		switch op {
		case "<":
			if vw >= n {
				return false
			}
		case "<=":
			if vw > n {
				return false
			}
		case ">":
			if vw <= n {
				return false
			}
		case ">=":
			if vw < n {
				return false
			}
		}
	}
	return true
}

// lengthToPx converts a numeric media-feature length to px, at the same 16px
// root font-size approximation length parsing uses elsewhere (see the "rem"
// case in parseLength) — kept consistent so a page's rem-based breakpoints and
// its rem-based element sizes agree. numStr is assumed already validated by
// the caller (via strconv.ParseFloat); a residual error here just yields 0,
// which cannot spuriously satisfy either a min or a max comparison in a way
// that hides content — it only ever narrows which viewports the condition
// covers, never widens it.
func lengthToPx(numStr, unit string) float64 {
	n, _ := strconv.ParseFloat(numStr, 64)
	if unit == "rem" {
		n *= 16
	}
	return n
}

// flipCmp reverses a comparison operator, for normalising the value-first
// media range form ("48rem<=width") to the width-first form mediaMatches
// evaluates ("width>=48rem" — the two say the same thing).
func flipCmp(op string) string {
	switch op {
	case "<":
		return ">"
	case "<=":
		return ">="
	case ">":
		return "<"
	case ">=":
		return "<="
	}
	return op
}

// matchBrace returns the index of the '}' matching the '{' at open.
func matchBrace(s string, open int) (int, bool) {
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return 0, false
}

// ParseDeclarations parses a declaration block body ("a: b; c: d") into
// declarations, lowercasing property names and trimming values.
func ParseDeclarations(body string) []Declaration {
	var out []Declaration
	for _, chunk := range splitDeclChunks(body) {
		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			continue
		}
		colon := strings.IndexByte(chunk, ':')
		if colon < 0 {
			continue
		}
		prop := strings.TrimSpace(chunk[:colon])
		// Custom-property names (--foo) are case-sensitive; normal property names
		// are case-insensitive and canonicalised to lower case.
		if !isCustomProperty(prop) {
			prop = strings.ToLower(prop)
		}
		val := strings.TrimSpace(chunk[colon+1:])
		// A trailing !important is not part of the value — strip it here, once,
		// so no property parser downstream ever has to know about it.
		important := false
		if idx := strings.LastIndex(strings.ToLower(val), "!important"); idx >= 0 {
			val = strings.TrimSpace(val[:idx])
			important = true
		}
		// An empty VALUE is meaningless for an ordinary property (there is
		// nothing to apply) but is a real, spec-valid, and load-bearing state
		// for a CUSTOM property: `--foo: ;` explicitly sets --foo to the empty
		// token stream, which is how the widespread "CSS toggle" pattern
		// (postcss-preset-env's light-dark() polyfill, seen live on
		// developer.mozilla.org) switches themes — one selector sets a guard
		// variable to `initial`, another (e.g. inside `@media
		// (prefers-color-scheme:dark)`) sets the SAME variable to empty to
		// flip every var() chain that reads it. Dropping the empty
		// declaration here left the guard stuck at its non-empty branch
		// forever, regardless of which media/attribute condition actually
		// applied — collapsing the page's entire colour system to its
		// pre-JS/light default.
		if prop == "" || (val == "" && !isCustomProperty(prop)) {
			continue
		}
		out = append(out, Declaration{Property: prop, Value: val, Important: important})
	}
	return out
}

// splitDeclChunks splits a declaration block on top-level semicolons, ignoring
// semicolons nested inside parentheses (e.g. a `url(data:…;base64,…)` value) or
// inside single/double quotes. This keeps a data-URI or function argument that
// contains ';' from being torn across two declarations.
func splitDeclChunks(body string) []string {
	var out []string
	depth := 0
	var quote byte
	start := 0
	for i := 0; i < len(body); i++ {
		c := body[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '(':
			depth++
		case c == ')':
			if depth > 0 {
				depth--
			}
		case c == ';' && depth == 0:
			out = append(out, body[start:i])
			start = i + 1
		}
	}
	out = append(out, body[start:])
	return out
}

// apply mutates s by applying one declaration. Unknown properties and
// unparseable values are ignored. emRef is the font-size used to resolve em
// lengths (the element's own computed font-size). parent is the element's
// already-computed parent style, needed only for the CSS-wide `inherit`
// keyword (see inheritProperty) — every other declaration value is
// self-contained.
func (s *Style) apply(d Declaration, emRef float64, parent *Style) {
	v := strings.TrimSpace(d.Value)
	lv := strings.ToLower(v)
	if lv == "inherit" {
		s.inheritProperty(d.Property, parent)
		return
	}
	switch d.Property {
	case "display":
		// A non-list display clears the list-item flag; `list-item` sets it (it is
		// a block-level box that additionally generates a marker).
		s.ListItem = false
		switch lv {
		case "list-item":
			s.Display, s.ListItem = DisplayBlock, true
		case "block", "flow-root":
			s.Display = DisplayBlock
		case "grid", "inline-grid":
			s.Display = DisplayGrid
		case "flex":
			s.Display = DisplayFlex
		case "inline-flex":
			s.Display = DisplayInlineFlex
		case "table", "inline-table":
			s.Display = DisplayTable
		case "table-row":
			s.Display = DisplayTableRow
		case "table-cell":
			s.Display = DisplayTableCell
		case "table-row-group", "table-header-group", "table-footer-group":
			s.Display = DisplayTableRowGroup
		case "inline-block":
			s.Display = DisplayInlineBlock
		case "inline":
			s.Display = DisplayInline
		case "none":
			s.Display = DisplayNone
		case "contents":
			s.Display = DisplayContents
		case "initial", "unset":
			// display is not an inherited property, so CSS-wide `unset` resolves
			// to `initial` here (same rule already applied to background/orphans/
			// widows below) — and display's own initial value is `inline`.
			// Confirmed load-bearing live: pkg.go.dev's own
			// `.UnitBuildContext-link{display:none} @media (width>=30rem){
			// .UnitBuildContext-link{display:initial}}` (its "Rendered for"
			// label, shown only above a width breakpoint) silently stayed at
			// `none` on any viewport — `initial` matched none of this switch's
			// cases, so the declaration was ignored entirely rather than
			// resetting the property, leaving the earlier, unconditional
			// `display:none` rule in effect no matter how wide the viewport was.
			s.Display = DisplayInline
		}
	case "visibility":
		switch lv {
		case "visible":
			s.Visibility = VisibilityVisible
		case "hidden":
			s.Visibility = VisibilityHidden
		case "collapse":
			s.Visibility = VisibilityCollapse
		}
	case "color":
		if c, ok := parseColor(v); ok {
			s.Color = c
		}
	case "fill":
		if lv == "none" {
			s.FillNone, s.FillSet = true, false
		} else if c, ok := parseColor(v); ok {
			s.Fill, s.FillSet, s.FillNone = c, true, false
		}
	case "stroke":
		if lv == "none" {
			s.StrokeNone, s.StrokeSet = true, false
		} else if c, ok := parseColor(v); ok {
			s.Stroke, s.StrokeSet, s.StrokeNone = c, true, false
		}
	case "background-color":
		// `initial`/`unset` (background-color is not an inherited property, so
		// `unset` behaves like `initial` here) reset to the property's spec
		// initial value, transparent — confirmed load-bearing live on MDN's
		// own `mdn-color-theme` web component: `.color-theme__button{
		// background-color:initial; border:none}` resets a real `<button>`'s
		// UA-default gray chrome to look like a plain icon-button, the same
		// idiom already handled for the `background:0 0`/`background:transparent`
		// shorthand forms. Left unrecognised before this, the declaration was
		// silently ignored and the UA default background survived untouched.
		if lv == "initial" || lv == "unset" {
			s.Background = Transparent
			break
		}
		// The whole value is the colour (may itself contain spaces, e.g. the
		// modern `rgb(22 24 29 / 1)` syntax), so parse it directly.
		if c, ok := parseColor(v); ok {
			s.Background = c
		}
	case "background":
		// A shorthand resets EVERY sub-property it represents, including ones a
		// given value doesn't mention — background-color's initial value is
		// transparent, so `background: 0 0` (position only) or `background:
		// url(x.png) no-repeat` (image only, no colour) must reset any
		// PREVIOUSLY-set background-colour to transparent, not leave it as
		// whatever it was before this declaration. Confirmed load-bearing live:
		// github.com's own nav <button>s reset `background:0 0;border:0` to
		// look like plain text links — before this reset, the UA-default
		// background-color this engine now gives <button> (css/ua.go) was never
		// overridden by that author declaration at all, since "0 0" carries no
		// colour token for the old code to find and apply, and the UA colour
		// simply survived untouched. Set to transparent FIRST, then overwrite
		// with a real colour token if the value has one.
		s.Background = Transparent
		if c, ok := parseColor(backgroundColorToken(v)); ok {
			s.Background = c
		}
		// Also pick up gradient / url() image layers from the shorthand so a
		// `background: linear-gradient(...)` (no separate background-image) paints.
		if imgs, ok := parseBackgroundImage(v, emRef); ok {
			s.BackgroundImages = imgs
		}
		// A repeat keyword and a "<position>/<size>" pair, if present, were
		// previously silently dropped by the shorthand entirely — left at
		// whatever they were before this declaration (or the zero value,
		// RepeatBoth/auto/0%,0%, on a fresh style) regardless of what the
		// author actually wrote. Confirmed load-bearing live: pkg.go.dev's own
		// mobile-nav hamburger button, `background:no-repeat center/2rem
		// url(/static/shared/icon/menu_gm_grey_24dp.svg)` on a 2.5rem button —
		// with the size/position dropped, the 24px icon stretched to fill the
		// WHOLE button instead of sitting centred at its real 2rem size,
		// visibly distorting its three bars.
		for _, f := range strings.Fields(v) {
			if r, ok := parseBackgroundRepeat(f); ok {
				s.BackgroundRepeat = []BgRepeat{r}
				break
			}
		}
		if pos, size, ok := backgroundPositionSizeTokens(v); ok {
			if p, ok := parseBackgroundPositionList(pos, emRef); ok {
				s.BackgroundPosition = p
			}
			if sz, ok := parseBackgroundSizeList(size, emRef); ok {
				s.BackgroundSize = sz
			}
		}
	case "background-image":
		if imgs, ok := parseBackgroundImage(v, emRef); ok {
			s.BackgroundImages = imgs
		} else if strings.EqualFold(lv, "none") {
			s.BackgroundImages = nil
		}
	case "mask-image", "-webkit-mask-image":
		// Only a single url() mask is modelled (see the field's doc comment on
		// Style.MaskImage) — a gradient or any other mask value is left as a
		// no-op, same as an unrecognised value everywhere else in this switch.
		if lv == "none" {
			s.MaskImage = ""
		} else if strings.HasPrefix(lv, "url(") {
			if u, ok := parseURLToken(v); ok {
				s.MaskImage = u
			}
		}
	case "background-size":
		if sz, ok := parseBackgroundSizeList(v, emRef); ok {
			s.BackgroundSize = sz
		}
	case "background-position":
		if p, ok := parseBackgroundPositionList(v, emRef); ok {
			s.BackgroundPosition = p
		}
	case "background-repeat":
		if r, ok := parseBackgroundRepeat(v); ok {
			s.BackgroundRepeat = []BgRepeat{r}
		}
	case "box-shadow":
		if sh, ok := parseBoxShadow(v, emRef); ok {
			s.BoxShadows = sh
		}
	case "opacity":
		if f, err := strconv.ParseFloat(lv, 64); err == nil {
			if f < 0 {
				f = 0
			} else if f > 1 {
				f = 1
			}
			s.Opacity, s.HasOpacity = f, true
		}
	case "filter":
		if fs, ok := parseFilterList(v, emRef); ok {
			s.Filters = fs
		}
	case "backdrop-filter", "-webkit-backdrop-filter":
		// Same function grammar as `filter` (see parseFilterList) — the only
		// difference is WHAT gets filtered: the content already painted behind
		// this box, not the box's own rendered subtree. See Style.BackdropFilters.
		// Tailwind (and other generated CSS) emits the -webkit- form immediately
		// before the standard one; aliasing both to the same field means the
		// standard spelling naturally wins via ordinary cascade order, matching
		// a real browser.
		if fs, ok := parseFilterList(v, emRef); ok {
			s.BackdropFilters = fs
		}
	case "font-size":
		if size, ok := absoluteFontSizeKeyword(lv); ok {
			s.FontSize = size
		} else if l, ok := parseLength(v, emRef); ok && !l.Auto {
			if l.IsPercent {
				s.FontSize = l.Percent * emRef
			} else {
				s.FontSize = l.Px
			}
		}
	case "font-weight":
		switch lv {
		case "bold", "bolder":
			s.FontWeight = 700
		case "normal", "lighter":
			s.FontWeight = 400
		default:
			if n, ok := atoiClamp(lv); ok {
				s.FontWeight = n
			}
		}
	case "font-style":
		// italic and oblique both select the slanted face; normal resets it.
		switch lv {
		case "italic", "oblique":
			s.Italic = true
		case "normal":
			s.Italic = false
		}
	case "text-decoration", "text-decoration-line":
		// The shorthand and the longhand are treated alike: only the
		// "underline" line is tracked (see Style.Underline's own doc
		// comment for the full scope and why). Any other explicit value —
		// "none", "line-through", … — clears it, matching the real,
		// non-additive replace-on-cascade semantics of this property.
		s.Underline = strings.Contains(lv, "underline")
	case "font-family":
		s.FontFamily = parseFontFamily(lv)
	case "text-align":
		switch lv {
		case "left", "start":
			s.TextAlign = AlignLeft
		case "center":
			s.TextAlign = AlignCenter
		case "right", "end":
			s.TextAlign = AlignRight
		case "-webkit-center", "-moz-center":
			// Legacy centre-including-blocks, emitted by the UA rule for <center>
			// and by the align="center" presentational hint.
			s.TextAlign = AlignCenterBlocks
		}
	case "white-space":
		switch lv {
		case "pre", "pre-wrap", "pre-line":
			s.WhiteSpace = WSPre
		case "nowrap":
			s.WhiteSpace = WSNoWrap
		case "normal":
			s.WhiteSpace = WSNormal
		}
	case "text-transform":
		switch lv {
		case "uppercase":
			s.TextTransform = TTUppercase
		case "lowercase":
			s.TextTransform = TTLowercase
		case "capitalize":
			s.TextTransform = TTCapitalize
		case "none":
			s.TextTransform = TTNone
		}
	case "image-rendering":
		switch lv {
		case "pixelated", "crisp-edges", "-webkit-optimize-contrast":
			s.ImageRendering = IRPixelated
		case "auto", "smooth", "high-quality", "optimizequality":
			s.ImageRendering = IRAuto
		}
	case "list-style-type":
		if t, ok := parseListStyleType(lv); ok {
			s.ListStyleType = t
		}
	case "list-style-position":
		switch lv {
		case "outside":
			s.ListStylePosition = ListOutside
		case "inside":
			s.ListStylePosition = ListInside
		}
	case "list-style":
		applyListStyle(s, lv)
	case "width":
		if l, ok := parseLength(v, emRef); ok {
			s.Width = l
		}
	case "min-width":
		if l, ok := parseLength(v, emRef); ok {
			s.MinWidth = l
		} else if lv == "none" {
			s.MinWidth = Length{Auto: true}
		}
	case "max-width":
		if l, ok := parseLength(v, emRef); ok {
			s.MaxWidth = l
		} else if lv == "none" {
			s.MaxWidth = Length{Auto: true}
		}
	case "height":
		if l, ok := parseLength(v, emRef); ok {
			s.Height = l
		}
	case "min-height":
		if l, ok := parseLength(v, emRef); ok {
			s.MinHeight = l
		} else if lv == "none" {
			s.MinHeight = Length{Auto: true}
		}
	case "max-height":
		if l, ok := parseLength(v, emRef); ok {
			s.MaxHeight = l
		} else if lv == "none" {
			s.MaxHeight = Length{Auto: true}
		}
	case "aspect-ratio":
		if r, ok := parseAspectRatio(lv); ok {
			s.AspectRatio = r
		} else {
			s.AspectRatio = 0 // "auto" or unparseable: no ratio constraint
		}
	case "object-fit":
		switch lv {
		case "cover":
			s.ObjectFit = ObjectFitCover
		case "contain":
			s.ObjectFit = ObjectFitContain
		default: // "fill" and every other/unrecognised keyword: this engine's own prior stretch behaviour
			s.ObjectFit = ObjectFitFill
		}
	case "box-sizing":
		switch lv {
		case "border-box":
			s.BoxSizing = BorderBox
		case "content-box":
			s.BoxSizing = ContentBox
		}
	case "overflow":
		// One value sets both axes; two values are `overflow-x overflow-y`.
		f := strings.Fields(lv)
		if len(f) == 1 {
			if o, ok := parseOverflowKeyword(f[0]); ok {
				s.OverflowX, s.OverflowY = o, o
			}
		} else if len(f) >= 2 {
			if o, ok := parseOverflowKeyword(f[0]); ok {
				s.OverflowX = o
			}
			if o, ok := parseOverflowKeyword(f[1]); ok {
				s.OverflowY = o
			}
		}
	case "overflow-x":
		if o, ok := parseOverflowKeyword(lv); ok {
			s.OverflowX = o
		}
	case "overflow-y":
		if o, ok := parseOverflowKeyword(lv); ok {
			s.OverflowY = o
		}
	case "clip":
		if r, ok := parseClipRect(lv, emRef); ok {
			s.HasClip, s.ClipRect = true, r
		}
	case "line-height":
		if lh, ok := parseLineHeight(v, emRef); ok {
			s.LineHeight = lh
		}
	case "letter-spacing":
		if lv == "normal" {
			s.LetterSpacing = 0
		} else if ln, ok := parseLength(v, emRef); ok && !ln.Auto {
			s.LetterSpacing = ln.Resolve(0)
		}
	case "border-spacing":
		fields := strings.Fields(v)
		if len(fields) == 1 {
			if ln, ok := parseLength(fields[0], emRef); ok && !ln.Auto {
				s.BorderSpacingH, s.BorderSpacingV = ln.Resolve(0), ln.Resolve(0)
			}
		} else if len(fields) == 2 {
			h, hok := parseLength(fields[0], emRef)
			vv, vok := parseLength(fields[1], emRef)
			if hok && vok && !h.Auto && !vv.Auto {
				s.BorderSpacingH, s.BorderSpacingV = h.Resolve(0), vv.Resolve(0)
			}
		}
	case "float":
		switch lv {
		case "left":
			s.Float = FloatLeft
		case "right":
			s.Float = FloatRight
		case "none":
			s.Float = FloatNone
		}
	case "clear":
		switch lv {
		case "left":
			s.Clear = ClearLeft
		case "right":
			s.Clear = ClearRight
		case "both":
			s.Clear = ClearBoth
		case "none":
			s.Clear = ClearNone
		}
	case "break-before", "page-break-before":
		// CSS Fragmentation 3; the legacy page-break-* properties are
		// aliases whose `always` means `page` (see parseBreakKeyword).
		if b, ok := parseBreakKeyword(lv); ok {
			s.BreakBefore = b
		}
	case "break-after", "page-break-after":
		if b, ok := parseBreakKeyword(lv); ok {
			s.BreakAfter = b
		}
	case "break-inside", "page-break-inside":
		if b, ok := parseBreakInsideKeyword(lv); ok {
			s.BreakInside = b
		}
	case "orphans":
		if lv == "unset" { // unset is inherit for an inherited property
			s.inheritProperty(d.Property, parent)
		} else if n, ok := parseLineCount(lv); ok {
			s.Orphans = n
		}
	case "widows":
		if lv == "unset" {
			s.inheritProperty(d.Property, parent)
		} else if n, ok := parseLineCount(lv); ok {
			s.Widows = n
		}
	case "tab-size":
		if lv == "unset" { // unset is inherit for an inherited property
			s.inheritProperty(d.Property, parent)
		} else if n, ok := parseTabSize(lv); ok {
			s.TabSize = n
		}
	case "text-wrap-style":
		if lv == "unset" { // unset is inherit for an inherited property
			s.inheritProperty(d.Property, parent)
		} else if b, ok := parseTextWrapStyle(lv); ok {
			s.TextWrapBalance = b
		}
	case "text-wrap-mode":
		if lv == "unset" { // unset is inherit for an inherited property
			s.inheritProperty(d.Property, parent)
		} else if n, ok := parseTextWrapMode(lv); ok {
			s.TextWrapNowrap = n
		}
	case "text-wrap":
		if lv == "unset" { // unset is inherit for an inherited property
			s.inheritProperty(d.Property, parent)
		} else if n, b, ok := parseTextWrap(lv); ok {
			s.TextWrapNowrap = n
			s.TextWrapBalance = b
		}
	case "word-break":
		if lv == "unset" { // unset is inherit for an inherited property
			s.inheritProperty(d.Property, parent)
		} else {
			switch lv {
			case "break-all":
				s.WordBreakAll = true
			case "break-word":
				// Deprecated alias for `normal` + `overflow-wrap:anywhere` —
				// see OverflowWrapAnywhere's own doc comment.
				s.WordBreakAll = false
				s.OverflowWrapAnywhere = true
			case "normal", "keep-all":
				s.WordBreakAll = false
			}
		}
	case "overflow-wrap", "word-wrap":
		if lv == "unset" { // unset is inherit for an inherited property
			s.inheritProperty(d.Property, parent)
		} else {
			switch lv {
			case "break-word", "anywhere":
				s.OverflowWrapAnywhere = true
			case "normal":
				s.OverflowWrapAnywhere = false
			}
		}
	case "-webkit-line-clamp", "line-clamp":
		// Not inherited, so "unset"/"initial"/"none" all just reset to 0 (no
		// clamp) — no inheritProperty special case needed, unlike the
		// text-wrap-* properties above. A bare positive integer is the only
		// real-world form (see Style.LineClamp's own doc comment); anything
		// else (a negative/zero/non-numeric value) is left as unset.
		if lv == "none" || lv == "unset" || lv == "initial" {
			s.LineClamp = 0
		} else if n, err := strconv.Atoi(lv); err == nil && n > 0 {
			s.LineClamp = n
		}
	case "vertical-align":
		switch lv {
		case "baseline", "initial", "unset":
			// Not inherited, so "unset" resolves to the initial value here too
			// (baseline), matching display's own initial/unset handling above.
			s.VerticalAlign = VAlignBaseline
		case "top":
			s.VerticalAlign = VAlignTop
		case "bottom":
			s.VerticalAlign = VAlignBottom
		case "text-top":
			s.VerticalAlign = VAlignTextTop
		case "text-bottom":
			s.VerticalAlign = VAlignTextBottom
		case "middle":
			s.VerticalAlign = VAlignMiddle
		case "sub":
			s.VerticalAlign = VAlignSub
		case "super":
			s.VerticalAlign = VAlignSuper
		}
	case "text-overflow":
		switch lv {
		case "ellipsis":
			s.TextOverflowEllipsis = true
		case "clip", "initial", "unset":
			// Not inherited, so "unset" resolves to the initial value here
			// too (clip — i.e. false), matching vertical-align's own
			// initial/unset handling above. Any other value (a custom
			// replacement string) is not modelled; left unchanged.
			s.TextOverflowEllipsis = false
		}
	case "position":
		switch lv {
		case "static":
			s.Position = PositionStatic
		case "relative":
			s.Position = PositionRelative
		case "absolute":
			s.Position = PositionAbsolute
		case "fixed":
			s.Position = PositionFixed
		case "sticky":
			s.Position = PositionSticky
		}
	case "top":
		if l, ok := parseLength(v, emRef); ok {
			s.Top = l
		}
	case "right":
		if l, ok := parseLength(v, emRef); ok {
			s.Right = l
		}
	case "bottom":
		if l, ok := parseLength(v, emRef); ok {
			s.Bottom = l
		}
	case "left":
		if l, ok := parseLength(v, emRef); ok {
			s.Left = l
		}
	case "inset":
		// The 1-to-4-value physical-offset shorthand (top/right/bottom/left, the
		// same expansion order as margin/padding). Confirmed live on
		// tailwindcss.com: `inset-0` (`inset:0`) positions a `size-82` image
		// meant to fill an ancestor `relative` card — with this case absent,
		// top/right/bottom/left all stayed at their initial `auto`, so the box
		// fell back to its (very wrong) static-flow position instead. Unlike
		// parseEdges (margin/padding), auto and percentages must be preserved,
		// not collapsed to 0 — placeAbsolute's static-position fallback and
		// percentage-of-containing-block resolution both depend on them.
		if ls, ok := parseLengthEdges(v, emRef); ok {
			s.Top, s.Right, s.Bottom, s.Left = ls[0], ls[1], ls[2], ls[3]
		}
	case "inset-inline":
		// Logical inline-axis shorthand (1 or 2 values: start[, end]); this
		// engine has no bidi/vertical writing-mode support anywhere, so
		// inline-start/end map directly to left/right as in every other
		// physical-only assumption it already makes.
		if start, end, ok := parseLengthPair(v, emRef); ok {
			s.Left, s.Right = start, end
		}
	case "inset-block":
		if start, end, ok := parseLengthPair(v, emRef); ok {
			s.Top, s.Bottom = start, end
		}
	case "z-index":
		if lv == "auto" {
			s.ZIndexAuto = true
		} else if n, err := strconv.Atoi(lv); err == nil {
			s.ZIndex, s.ZIndexAuto = n, false
		}
	case "transform":
		// This engine has no general CSS transform support (see FIDELITY.md's
		// Known gaps) — but `translate`/`translateX`/`translateY` and
		// `rotate`/`rotateZ`, EACH ONLY on its own, are the two subsets
		// pulled out (see TranslateX and RotateDeg's own doc comments for
		// why, and their real confirmed triggers). A value naming any OTHER
		// function, or mixing more than one of these two, is left
		// unsupported entirely rather than applying a partial, wrong
		// composition.
		//
		// A VALID value of either kind replaces the WHOLE previous computed
		// value — CSS's own cascade semantics, not just whichever field this
		// one rule happens to set — so each branch below clears the OTHER
		// kind's fields alongside setting its own. An INVALID/mixed value,
		// though, must leave every field UNTOUCHED, not reset to zero: an
		// invalid declaration is dropped by the cascade entirely, so a
		// valid `transform` from an earlier, lower-priority rule in the
		// same cascade pass must keep standing, not be silently cleared by
		// a later rule's own typo or unsupported function.
		if tx, ty, ok := parseTransformTranslate(v, emRef); ok {
			s.TranslateX, s.TranslateY, s.RotateDeg = tx, ty, 0
		} else if deg, ok := parseTransformRotate(v); ok {
			s.TranslateX, s.TranslateY, s.RotateDeg = Length{}, Length{}, deg
		}
	case "rotate":
		// The standalone CSS Transforms Level 2 property — a bare angle,
		// NOT a function call — confirmed as the actual real trigger (see
		// css.Style.RotateDeg's own doc comment): modern Tailwind compiles
		// its `rotate-*` utilities to THIS property, not `transform:
		// rotate()`. Writes the SAME field as the `transform` function of
		// the same name; see RotateDeg's own doc comment for why the two
		// are not composed.
		switch lv {
		case "none", "initial", "unset":
			s.RotateDeg = 0
		default:
			if deg, ok := parseAngle(lv); ok {
				s.RotateDeg = deg
			}
		}
	case "appearance", "-webkit-appearance":
		// Only "none" (suppress native chrome) is modelled — see
		// AppearanceNone's own doc comment. "auto"/"initial"/"unset" reset to
		// the initial value. Every other CSS Basic UI keyword (textfield,
		// menulist-button, button, searchfield, ...) is syntactically valid
		// but has no confirmed trigger and asks for nothing this engine paints
		// differently from auto — left untouched rather than guessed at,
		// same as an unsupported `transform` function is left untouched.
		switch lv {
		case "none":
			s.AppearanceNone = true
		case "auto", "initial", "unset":
			s.AppearanceNone = false
		}
	case "flex-direction":
		switch lv {
		case "row", "row-reverse":
			s.FlexDirection = FlexRow
		case "column", "column-reverse":
			s.FlexDirection = FlexColumn
		}
	case "justify-content":
		if j, ok := parseJustify(lv); ok {
			s.JustifyContent = j
		}
	case "align-items":
		if a, ok := parseAlignItems(lv); ok {
			s.AlignItems = a
		}
	case "flex-grow":
		if f, err := strconv.ParseFloat(lv, 64); err == nil && f >= 0 {
			s.FlexGrow = f
		}
	case "flex-shrink":
		if f, err := strconv.ParseFloat(lv, 64); err == nil && f >= 0 {
			s.FlexShrink = f
		}
	case "flex-basis":
		if l, ok := parseLength(v, emRef); ok {
			s.FlexBasis = l
		} else if lv == "content" {
			s.FlexBasis = Length{Auto: true}
		}
	case "flex":
		applyFlexShorthand(s, v, emRef)
	case "flex-wrap":
		if w, ok := parseFlexWrap(lv); ok {
			s.FlexWrap = w
		}
	case "flex-flow":
		applyFlexFlow(s, v)
	case "align-content":
		if a, ok := parseAlignContent(lv); ok {
			s.AlignContent = a
		}
	case "align-self":
		if a, ok := parseAlignSelf(lv); ok {
			s.AlignSelf = a
		}
	case "justify-self":
		if a, ok := parseAlignSelf(lv); ok {
			s.JustifySelf = a
		}
	case "justify-items":
		if a, ok := parseAlignItems(lv); ok {
			s.JustifyItems = a
		}
	case "order":
		if n, err := strconv.Atoi(lv); err == nil {
			s.Order = n
		}
	case "gap", "grid-gap":
		applyGap(s, v, emRef)
	case "row-gap", "grid-row-gap":
		if l, ok := parseLength(v, emRef); ok && !l.Auto {
			s.RowGap = l
		}
	case "column-gap", "grid-column-gap":
		if l, ok := parseLength(v, emRef); ok && !l.Auto {
			s.ColumnGap = l
		}
	case "column-count":
		// Not inherited (see ColumnCount's own doc comment), so unset/initial
		// both reset to 0 ("not set"), same convention as text-overflow above.
		switch lv {
		case "auto", "initial", "unset":
			s.ColumnCount = 0
		default:
			if n, err := strconv.Atoi(lv); err == nil && n > 0 {
				s.ColumnCount = n
			}
		}
	case "column-width":
		switch lv {
		case "auto", "initial", "unset":
			s.ColumnWidth = Length{Auto: true}
		default:
			if l, ok := parseLength(lv, emRef); ok && !l.Auto {
				s.ColumnWidth = l
			}
		}
	case "columns":
		switch lv {
		case "auto", "initial", "unset":
			s.ColumnCount, s.ColumnWidth = 0, Length{Auto: true}
		default:
			if count, width, ok := parseColumns(lv, emRef); ok {
				s.ColumnCount, s.ColumnWidth = count, width
			}
		}
	case "place-items":
		applyPlaceItems(s, v)
	case "place-content":
		applyPlaceContent(s, lv)
	case "place-self":
		applyPlaceSelf(s, v)
	case "grid-template-columns":
		if t, ok := parseTrackList(v, emRef); ok {
			s.GridTemplateColumns = t
		}
	case "grid-template":
		if rows, columns, areas, ok := parseGridTemplate(v, emRef); ok {
			s.GridTemplateRows = rows
			s.GridTemplateColumns = columns
			s.GridTemplateAreas = areas
		}
	case "grid-template-rows":
		if t, ok := parseTrackList(v, emRef); ok {
			s.GridTemplateRows = t
		}
	case "grid-auto-columns":
		if t, ok := parseTrackSize(v, emRef); ok {
			s.GridAutoColumns = t
		}
	case "grid-auto-rows":
		if t, ok := parseTrackSize(v, emRef); ok {
			s.GridAutoRows = t
		}
	case "grid-auto-flow":
		if strings.Contains(lv, "column") {
			s.GridAutoFlow = GridFlowColumn
		} else if strings.Contains(lv, "row") {
			s.GridAutoFlow = GridFlowRow
		}
		s.GridAutoFlowDense = strings.Contains(lv, "dense")
	case "grid-template-areas":
		if a, ok := parseGridTemplateAreas(v); ok {
			s.GridTemplateAreas = a
		}
	case "grid-column":
		s.GridColumnStart, s.GridColumnEnd = parseGridPlacement(v)
	case "grid-row":
		s.GridRowStart, s.GridRowEnd = parseGridPlacement(v)
	case "grid-column-start":
		s.GridColumnStart = parseGridLine(v)
	case "grid-column-end":
		s.GridColumnEnd = parseGridLine(v)
	case "grid-row-start":
		s.GridRowStart = parseGridLine(v)
	case "grid-row-end":
		s.GridRowEnd = parseGridLine(v)
	case "grid-area":
		applyGridArea(s, v)
	case "border-radius":
		if l, ok := parseBorderRadius(v, emRef); ok {
			s.BorderRadius = l
		}
	case "border-top-left-radius", "border-top-right-radius",
		"border-bottom-left-radius", "border-bottom-right-radius":
		// Per-corner radii collapse to the single uniform radius (last wins).
		if l, ok := parseBorderRadius(v, emRef); ok {
			s.BorderRadius = l
		}
	case "border":
		applyBorderShorthand(&s.Border, v, emRef, s.Color)
	case "border-top":
		applyBorderSideShorthand(&s.Border.Top, v, emRef, s.Color)
	case "border-right":
		applyBorderSideShorthand(&s.Border.Right, v, emRef, s.Color)
	case "border-bottom":
		applyBorderSideShorthand(&s.Border.Bottom, v, emRef, s.Color)
	case "border-left":
		applyBorderSideShorthand(&s.Border.Left, v, emRef, s.Color)
	case "border-width":
		applyBorderWidth(&s.Border, v, emRef)
	case "border-style":
		applyBorderStyle(&s.Border, v)
	case "border-color":
		applyBorderColor(&s.Border, v)
	case "border-top-width":
		applyBorderEdgeWidth(&s.Border.Top, v, emRef)
	case "border-right-width":
		applyBorderEdgeWidth(&s.Border.Right, v, emRef)
	case "border-bottom-width":
		applyBorderEdgeWidth(&s.Border.Bottom, v, emRef)
	case "border-left-width":
		applyBorderEdgeWidth(&s.Border.Left, v, emRef)
	case "margin":
		applyMarginShorthand(s, v, emRef)
	case "margin-top":
		applyEdge(&s.Margin.Top, v, emRef)
	case "margin-right":
		applyMarginSide(&s.Margin.Right, &s.MarginRightAuto, &s.MarginRightIsPercent, &s.MarginRightPercent, v, emRef)
	case "margin-bottom":
		applyEdge(&s.Margin.Bottom, v, emRef)
	case "margin-left":
		applyMarginSide(&s.Margin.Left, &s.MarginLeftAuto, &s.MarginLeftIsPercent, &s.MarginLeftPercent, v, emRef)
	case "padding":
		if e, ok := parseEdges(v, emRef); ok {
			s.Padding = e
		}
	case "padding-top":
		applyEdge(&s.Padding.Top, v, emRef)
	case "padding-right":
		applyEdge(&s.Padding.Right, v, emRef)
	case "padding-bottom":
		applyEdge(&s.Padding.Bottom, v, emRef)
	case "padding-left":
		applyEdge(&s.Padding.Left, v, emRef)
	case "margin-block-start":
		applyEdge(&s.Margin.Top, v, emRef)
	case "margin-block-end":
		applyEdge(&s.Margin.Bottom, v, emRef)
	case "margin-inline-start":
		applyMarginSide(&s.Margin.Left, &s.MarginLeftAuto, &s.MarginLeftIsPercent, &s.MarginLeftPercent, v, emRef)
	case "margin-inline-end":
		applyMarginSide(&s.Margin.Right, &s.MarginRightAuto, &s.MarginRightIsPercent, &s.MarginRightPercent, v, emRef)
	case "margin-block":
		applyMarginBlockShorthand(s, v, emRef)
	case "margin-inline":
		applyMarginInlineShorthand(s, v, emRef)
	case "padding-block-start":
		applyEdge(&s.Padding.Top, v, emRef)
	case "padding-block-end":
		applyEdge(&s.Padding.Bottom, v, emRef)
	case "padding-inline-start":
		applyEdge(&s.Padding.Left, v, emRef)
	case "padding-inline-end":
		applyEdge(&s.Padding.Right, v, emRef)
	case "padding-block":
		applyPaddingBlockShorthand(s, v, emRef)
	case "padding-inline":
		applyPaddingInlineShorthand(s, v, emRef)
	case "container-type":
		if ct, ok := containerTypeKeyword(lv); ok {
			s.ContainerType = ct
		}
	case "container-name":
		// container-name is a case-sensitive custom-ident (or a space-separated
		// list of them, though this engine only ever compares against a single
		// name — see ContainerCondition.Name); "none" clears it.
		if lv == "none" {
			s.ContainerName = ""
		} else {
			s.ContainerName = v
		}
	case "container":
		applyContainerShorthand(s, v)
	}
}

// inheritProperty implements the CSS-wide `inherit` keyword for the
// properties this engine gives explicit inheritance semantics to in
// inheritFrom. It exists because a property already inherited by default
// (color, for instance) can still have been overridden by an EARLIER,
// lower-precedence declaration in the very same cascade — most commonly this
// engine's own user-agent default `a{color:#0000ee}` — before an author rule
// explicitly asks to inherit again. Confirmed live: Tailwind's (and most
// modern frameworks') preflight reset ships exactly `a{color:inherit}` to
// cancel that default, which a plain no-op for "inherit" would not undo,
// leaving every reset anchor (and, transitively, every `fill="currentColor"`
// SVG icon inside one — a very common pattern for a linked logo/icon) stuck
// in browser-default link blue regardless of the page's real design.
// Properties with no inheritance behaviour to restore here (background-color
// included — it resets to transparent, not the parent's background, per
// spec) leave the field exactly as prior declarations left it, same as
// before this keyword was understood at all.
func (s *Style) inheritProperty(prop string, parent *Style) {
	if parent == nil {
		return // no parent style known (a bare apply): nothing to inherit from
	}
	switch prop {
	case "color":
		s.Color = parent.Color
	case "visibility":
		s.Visibility = parent.Visibility
	case "font-weight":
		s.FontWeight = parent.FontWeight
	case "text-align":
		s.TextAlign = parent.TextAlign
	case "white-space":
		s.WhiteSpace = parent.WhiteSpace
	case "text-transform":
		s.TextTransform = parent.TextTransform
	case "line-height":
		s.LineHeight = parent.LineHeight
	case "letter-spacing":
		s.LetterSpacing = parent.LetterSpacing
	case "border-spacing":
		s.BorderSpacingH, s.BorderSpacingV = parent.BorderSpacingH, parent.BorderSpacingV
	case "list-style-type":
		s.ListStyleType = parent.ListStyleType
	case "list-style-position":
		s.ListStylePosition = parent.ListStylePosition
	case "orphans":
		s.Orphans = parent.Orphans
	case "widows":
		s.Widows = parent.Widows
	case "tab-size":
		s.TabSize = parent.TabSize
	case "text-wrap-style":
		s.TextWrapBalance = parent.TextWrapBalance
	case "text-wrap-mode":
		s.TextWrapNowrap = parent.TextWrapNowrap
	case "text-wrap":
		s.TextWrapBalance = parent.TextWrapBalance
		s.TextWrapNowrap = parent.TextWrapNowrap
	case "word-break":
		s.WordBreakAll = parent.WordBreakAll
	case "overflow-wrap", "word-wrap":
		s.OverflowWrapAnywhere = parent.OverflowWrapAnywhere
	// The break properties are not inherited by default, but an explicit
	// `inherit` still copies the parent's computed value, per CSS Cascade.
	case "break-before", "page-break-before":
		s.BreakBefore = parent.BreakBefore
	case "break-after", "page-break-after":
		s.BreakAfter = parent.BreakAfter
	case "break-inside", "page-break-inside":
		s.BreakInside = parent.BreakInside
	// column-count/column-width are not inherited by default either, but the
	// explicit `inherit` keyword still copies the parent's computed value.
	case "column-count":
		s.ColumnCount = parent.ColumnCount
	case "column-width":
		s.ColumnWidth = parent.ColumnWidth
	case "columns":
		s.ColumnCount, s.ColumnWidth = parent.ColumnCount, parent.ColumnWidth
	case "rotate":
		s.RotateDeg = parent.RotateDeg
	case "appearance", "-webkit-appearance":
		s.AppearanceNone = parent.AppearanceNone
	}
}

// parseColumns parses the `columns` shorthand: `<'column-width'> || <'column-
// count'>` — one or both of a length and a positive integer, in EITHER
// order, space-separated (`columns:12.5rem 5` and `columns:5 12.5rem` are
// identical, matching real CSS's `||` combinator). A bare integer is always
// column-count and a length is always column-width, so the two tokens are
// never ambiguous with each other; "auto" in either position just leaves
// that sub-property at its own already-auto/zero default. Reports false —
// leaving both sub-properties unchanged — for more than two tokens, a
// second token of a kind already seen (two integers, or two lengths), or any
// token that is neither "auto", a positive integer, nor a valid length.
func parseColumns(v string, emRef float64) (count int, width Length, ok bool) {
	fields := strings.Fields(v)
	if len(fields) == 0 || len(fields) > 2 {
		return 0, Length{}, false
	}
	width = Length{Auto: true}
	var sawCount, sawWidth bool
	for _, f := range fields {
		if f == "auto" {
			continue
		}
		if n, err := strconv.Atoi(f); err == nil && n > 0 {
			if sawCount {
				return 0, Length{}, false
			}
			count, sawCount = n, true
			continue
		}
		if l, lok := parseLength(f, emRef); lok && !l.Auto {
			if sawWidth {
				return 0, Length{}, false
			}
			width, sawWidth = l, true
			continue
		}
		return 0, Length{}, false
	}
	return count, width, true
}

// parseLineCount parses an orphans / widows value (CSS Fragmentation 3 §4):
// a positive integer, or the CSS-wide initial (2). Zero, a negative number
// and a non-integer are invalid and report false, so the declaration is
// ignored and the inherited value stands.
func parseLineCount(lv string) (int, bool) {
	if lv == "initial" {
		return 2, true
	}
	n, err := strconv.Atoi(lv)
	return n, err == nil && n > 0
}

// parseTabSize parses a `tab-size` value (CSS Text 3 §4.3): a bare,
// non-negative integer (0 is valid — it means "don't render tabs"), or the
// CSS-wide initial (8). The sibling `<length>` form (spec-flagged "at risk")
// is not parsed — no confirmed real use — so it reports false and the
// declaration is ignored, same as a negative or non-integer value.
func parseTabSize(lv string) (int, bool) {
	if lv == "initial" {
		return 8, true
	}
	n, err := strconv.Atoi(lv)
	return n, err == nil && n >= 0
}

// parseTextWrapStyle parses a `text-wrap-style` value (or one token of the
// `text-wrap` shorthand): only `balance` has a distinguishing implementation
// (see TextWrapBalance's own doc comment) — the other real keywords (auto,
// stable, pretty, avoid-short-last-line) are recognised, just resolve to
// false, identical to the property's own initial value. An unrecognised
// keyword reports false,false so the caller leaves the inherited value in
// place, matching every other property here that distinguishes "invalid,
// ignore" from "valid, but not `balance`".
func parseTextWrapStyle(tok string) (balance, ok bool) {
	switch tok {
	case "balance":
		return true, true
	case "auto", "stable", "pretty", "avoid-short-last-line", "initial":
		return false, true
	default:
		return false, false
	}
}

// parseTextWrapMode parses a `text-wrap-mode` value (or one token of the
// `text-wrap` shorthand): `nowrap` has a distinguishing implementation (see
// TextWrapNowrap's own doc comment); `wrap` is the property's own initial
// value. An unrecognised keyword reports false,false.
func parseTextWrapMode(tok string) (nowrap, ok bool) {
	switch tok {
	case "nowrap":
		return true, true
	case "wrap", "initial":
		return false, true
	default:
		return false, false
	}
}

// parseTextWrap parses the `text-wrap` shorthand's `<text-wrap-mode> ||
// <text-wrap-style>` grammar (either order, either omitted) by checking each
// whitespace-separated token against both longhands in turn. Per ordinary
// CSS shorthand semantics, an axis absent from the value resets to ITS OWN
// initial value (false for both fields here) rather than staying unchanged —
// exactly what the zero-value nowrap/balance returned for a token that never
// arrives already gives, so no extra reset step is needed. A token matching
// neither longhand's keywords makes the whole shorthand invalid.
func parseTextWrap(lv string) (nowrap, balance, ok bool) {
	ok = true
	for _, tok := range strings.Fields(lv) {
		if n, k := parseTextWrapMode(tok); k {
			nowrap = n
			continue
		}
		if b, k := parseTextWrapStyle(tok); k {
			balance = b
			continue
		}
		return false, false, false
	}
	return nowrap, balance, ok
}

func applyEdge(dst *float64, v string, emRef float64) {
	if l, ok := parseLength(v, emRef); ok && !l.Auto && !l.IsPercent {
		*dst = l.Px
	}
}

// applyMarginBlockShorthand parses the 1-or-2-value margin-block shorthand
// (<start> [<end>]) onto the top/bottom physical edges. This engine has no
// bidi/vertical writing-mode support anywhere, so block-start/end always map
// directly to top/bottom, matching the inset-block precedent.
func applyMarginBlockShorthand(s *Style, v string, emRef float64) {
	fields := strings.Fields(v)
	if len(fields) == 0 || len(fields) > 2 {
		return
	}
	applyEdge(&s.Margin.Top, fields[0], emRef)
	applyEdge(&s.Margin.Bottom, fields[len(fields)-1], emRef)
}

// applyMarginInlineShorthand parses the 1-or-2-value margin-inline shorthand
// onto the left/right physical edges, honouring `auto` as margin-left/right
// already do — this engine's inline axis always maps to left/right.
func applyMarginInlineShorthand(s *Style, v string, emRef float64) {
	fields := strings.Fields(v)
	if len(fields) == 0 || len(fields) > 2 {
		return
	}
	applyMarginSide(&s.Margin.Left, &s.MarginLeftAuto, &s.MarginLeftIsPercent, &s.MarginLeftPercent, fields[0], emRef)
	applyMarginSide(&s.Margin.Right, &s.MarginRightAuto, &s.MarginRightIsPercent, &s.MarginRightPercent, fields[len(fields)-1], emRef)
}

// applyPaddingBlockShorthand parses the 1-or-2-value padding-block shorthand
// onto the top/bottom physical edges.
func applyPaddingBlockShorthand(s *Style, v string, emRef float64) {
	fields := strings.Fields(v)
	if len(fields) == 0 || len(fields) > 2 {
		return
	}
	applyEdge(&s.Padding.Top, fields[0], emRef)
	applyEdge(&s.Padding.Bottom, fields[len(fields)-1], emRef)
}

// applyPaddingInlineShorthand parses the 1-or-2-value padding-inline
// shorthand onto the left/right physical edges.
func applyPaddingInlineShorthand(s *Style, v string, emRef float64) {
	fields := strings.Fields(v)
	if len(fields) == 0 || len(fields) > 2 {
		return
	}
	applyEdge(&s.Padding.Left, fields[0], emRef)
	applyEdge(&s.Padding.Right, fields[len(fields)-1], emRef)
}

// parseEdges parses the 1-to-4 value shorthand for margin/padding. Percentages
// and auto collapse to 0 (Phase 0 does not resolve them for the shorthand).
func parseEdges(v string, emRef float64) (Edges, bool) {
	fields := strings.Fields(v)
	px := make([]float64, 0, len(fields))
	for _, f := range fields {
		l, ok := parseLength(f, emRef)
		if !ok {
			return Edges{}, false
		}
		if l.Auto || l.IsPercent {
			px = append(px, 0)
		} else {
			px = append(px, l.Px)
		}
	}
	switch len(px) {
	case 1:
		return Edges{px[0], px[0], px[0], px[0]}, true
	case 2:
		return Edges{px[0], px[1], px[0], px[1]}, true
	case 3:
		return Edges{px[0], px[1], px[2], px[1]}, true
	case 4:
		return Edges{px[0], px[1], px[2], px[3]}, true
	}
	return Edges{}, false
}

// parseLengthEdges parses the 1-to-4 value box-edge shorthand (the same
// expansion order as margin/padding) into [top, right, bottom, left],
// preserving each Length in full — unlike parseEdges (used for margin/
// padding, which collapse auto/percentage to a flat 0), a position-offset
// shorthand like `inset` must keep auto and percentage intact: placeAbsolute's
// static-position fallback and containing-block-relative percentage
// resolution both depend on the distinction.
func parseLengthEdges(v string, emRef float64) ([4]Length, bool) {
	fields := strings.Fields(v)
	ls := make([]Length, 0, len(fields))
	for _, f := range fields {
		l, ok := parseLength(f, emRef)
		if !ok {
			return [4]Length{}, false
		}
		ls = append(ls, l)
	}
	switch len(ls) {
	case 1:
		return [4]Length{ls[0], ls[0], ls[0], ls[0]}, true
	case 2:
		return [4]Length{ls[0], ls[1], ls[0], ls[1]}, true
	case 3:
		return [4]Length{ls[0], ls[1], ls[2], ls[1]}, true
	case 4:
		return [4]Length{ls[0], ls[1], ls[2], ls[3]}, true
	}
	return [4]Length{}, false
}

// parseLengthPair parses a 1-or-2-value axis shorthand (inset-inline,
// inset-block: `<start> [<end>]`) into (start, end). A single value applies
// to both ends, matching the CSS shorthand's own 1-value rule (distinct from
// the 4-side box-edge expansion parseLengthEdges implements — an axis only
// has two ends, not four).
func parseLengthPair(v string, emRef float64) (start, end Length, ok bool) {
	fields := strings.Fields(v)
	switch len(fields) {
	case 1:
		l, ok := parseLength(fields[0], emRef)
		return l, l, ok
	case 2:
		l0, ok0 := parseLength(fields[0], emRef)
		l1, ok1 := parseLength(fields[1], emRef)
		return l0, l1, ok0 && ok1
	}
	return Length{}, Length{}, false
}

// parseTransformTranslate parses a `transform` value that consists ENTIRELY
// of translate/translateX/translateY function calls (whitespace-separated,
// per the CSS transform-list grammar), summing their contributions onto the
// X/Y axes — translations commute, so composing several this way is exact.
// `none` resolves to (0,0). Any OTHER function name (rotate, scale, skew,
// matrix, translateZ, perspective, …), or a malformed call, reports ok=false
// and the caller must leave the style's translate fields untouched — this
// engine has no general transform support, so a value mixing an
// unsupported function among translate calls must not apply a partial,
// wrong composition.
func parseTransformTranslate(v string, emRef float64) (tx, ty Length, ok bool) {
	v = strings.TrimSpace(v)
	if strings.EqualFold(v, "none") || v == "" {
		return Length{}, Length{}, true
	}
	i := 0
	for i < len(v) {
		// The leading TrimSpace guarantees v's last character is non-space, so
		// this can never run past len(v): the loop condition above already
		// ensures i < len(v) on entry, and skipping only whitespace can reach
		// len(v) only if everything from here to the end were whitespace,
		// which the trim rules out everywhere in v, not just at its end.
		for i < len(v) && isCSSSpace(v[i]) {
			i++
		}
		nameStart := i
		for i < len(v) && v[i] != '(' {
			i++
		}
		if i >= len(v) {
			return Length{}, Length{}, false // a function name with no '('
		}
		name := strings.ToLower(strings.TrimSpace(v[nameStart:i]))
		closeIdx, matched := matchParen(v, i)
		if !matched {
			return Length{}, Length{}, false
		}
		args := strings.Split(v[i+1:closeIdx], ",")
		i = closeIdx + 1

		switch name {
		case "translate":
			x, okx := parseLength(strings.TrimSpace(args[0]), emRef)
			if !okx {
				return Length{}, Length{}, false
			}
			tx = addLength(tx, x)
			if len(args) > 1 {
				y, oky := parseLength(strings.TrimSpace(args[1]), emRef)
				if !oky {
					return Length{}, Length{}, false
				}
				ty = addLength(ty, y)
			}
		case "translatex":
			x, okx := parseLength(strings.TrimSpace(args[0]), emRef)
			if !okx {
				return Length{}, Length{}, false
			}
			tx = addLength(tx, x)
		case "translatey":
			y, oky := parseLength(strings.TrimSpace(args[0]), emRef)
			if !oky {
				return Length{}, Length{}, false
			}
			ty = addLength(ty, y)
		default:
			return Length{}, Length{}, false
		}
	}
	return tx, ty, true
}

// addLength sums two same-axis translate contributions. When both are the
// same kind (both px-resolved or both percentages) the sum is exact; a page
// mixing units across multiple translate calls on the SAME axis (e.g.
// `translateX(10px) translateX(5%)`) cannot be represented by this engine's
// Length type (either an absolute px value or a percentage, never both) —
// the later call wins outright rather than silently dropping one
// contribution. This is a rare combination in practice; the common
// single-function case (the only shape this engine's own live regression
// needed) is always exact.
func addLength(a, b Length) Length {
	switch {
	case a.IsPercent && b.IsPercent:
		return Length{IsPercent: true, Percent: a.Percent + b.Percent}
	case !a.IsPercent && !b.IsPercent:
		return Length{Px: a.Px + b.Px}
	default:
		return b
	}
}

// parseTransformRotate parses a `transform` value that is ONLY a single
// `rotate()`/`rotateZ()` function — see css.Style.RotateDeg's own doc
// comment for why this engine models only this one function on its own,
// the same "no mixing" scope parseTransformTranslate already has. Returns
// the angle in CSS's own degrees-clockwise-positive convention (see
// parseAngle in background.go, already shared with linear-gradient's own
// angle argument) and ok.
func parseTransformRotate(v string) (deg float64, ok bool) {
	v = strings.TrimSpace(v)
	if strings.EqualFold(v, "none") || v == "" {
		return 0, true
	}
	i := 0
	for i < len(v) && isCSSSpace(v[i]) {
		i++
	}
	nameStart := i
	for i < len(v) && v[i] != '(' {
		i++
	}
	if i >= len(v) {
		return 0, false // a function name with no '('
	}
	name := strings.ToLower(strings.TrimSpace(v[nameStart:i]))
	if name != "rotate" && name != "rotatez" {
		return 0, false
	}
	closeIdx, matched := matchParen(v, i)
	if !matched {
		return 0, false
	}
	if rest := strings.TrimSpace(v[closeIdx+1:]); rest != "" {
		return 0, false // a second function chained after rotate: mixing not supported
	}
	return parseAngle(strings.TrimSpace(v[i+1 : closeIdx]))
}

// isCSSSpace reports whether c is CSS whitespace, for scanning a
// space-separated transform-function list.
func isCSSSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

// parseFontFamily resolves a `font-family` declaration into the families it
// named and the generic bucket to fall back on.
//
// The bucket is decided exactly as it was before named families existed — the
// first entry the heuristic below recognises wins, so no page changes the
// typeface it was already getting. What is new is that the names are kept, so
// an @font-face rule can be matched against them.
func parseFontFamily(lv string) FontFamily {
	var names []string
	generic, found := GenericSans, false
	for _, fam := range splitTopLevelCommas(strings.ToLower(lv)) {
		fam = strings.TrimSpace(strings.Trim(strings.TrimSpace(fam), `"'`))
		if fam == "" {
			continue
		}
		if g, ok := genericBucketFor(fam); ok && !found {
			generic, found = g, true
		}
		// A generic keyword names no face. An alias like "helvetica" does
		// both — it names a face this engine has not got, and says which
		// bucket that face belongs to — so it stays in the list, where an
		// @font-face rule for it would be found.
		if !isGenericKeyword(fam) {
			names = append(names, fam)
		}
	}
	return FontFamily{Names: strings.Join(names, ","), Generic: generic}
}

// isGenericKeyword reports whether a family entry is one of CSS's generic
// keywords rather than the name of a typeface.
func isGenericKeyword(fam string) bool {
	switch fam {
	case "serif", "sans-serif", "monospace", "cursive", "fantasy",
		"system-ui", "ui-serif", "ui-sans-serif", "ui-monospace", "ui-rounded":
		return true
	}
	return false
}

// genericBucketFor maps one family entry to a generic bucket, by keyword or by
// the name of a face whose shape is known. This is the pre-existing heuristic,
// moved here unchanged so the bucket a page resolves to does not shift.
func genericBucketFor(fam string) (Generic, bool) {
	switch {
	case fam == "monospace" || strings.Contains(fam, "mono") ||
		strings.Contains(fam, "courier") || strings.Contains(fam, "consolas"):
		return GenericMono, true
	case fam == "serif" || strings.Contains(fam, "times") ||
		strings.Contains(fam, "georgia") || strings.Contains(fam, "lora"):
		return GenericSerif, true
	case fam == "sans-serif" || strings.Contains(fam, "sans") ||
		strings.Contains(fam, "arial") || strings.Contains(fam, "helvetica") ||
		strings.Contains(fam, "inter") || strings.Contains(fam, "roboto"):
		return GenericSans, true
	}
	return GenericSans, false
}

// backgroundColorToken extracts a leading colour token from a `background`
// shorthand value. A functional colour (rgb()/rgba()/hsl()/hsla()) is returned
// whole (including internal spaces and its parenthesised argument list);
// otherwise the first whitespace-delimited token is returned. Gradient and
// url() image layers are left for the (unimplemented) background-image path and
// simply fail to parse as a colour.
func backgroundColorToken(v string) string {
	v = strings.TrimSpace(v)
	lv := strings.ToLower(v)
	for _, fn := range []string{"rgb(", "rgba(", "hsl(", "hsla("} {
		if strings.HasPrefix(lv, fn) {
			if close := strings.IndexByte(v, ')'); close >= 0 {
				return v[:close+1]
			}
		}
	}
	return firstToken(v)
}

func firstToken(v string) string {
	f := strings.Fields(v)
	if len(f) == 0 {
		return ""
	}
	return f[0]
}

func atoiClamp(s string) (int, bool) {
	n := 0
	if s == "" {
		return 0, false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
}
