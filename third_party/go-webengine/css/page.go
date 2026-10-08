// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package css

import (
	"strings"

	"github.com/go-webengine/engine/dom"
)

// PageRule is one @page block as written (CSS Paged Media Level 3), with
// its selector — "" for the unqualified rule, ":first", ":left", ":right",
// ":blank", or a named page ("cover") — and its declarations in order. A
// selector list (`@page :left, :right { … }`) yields one PageRule per
// selector, sharing the declarations. Margin boxes nested inside
// (`@top-center { content: counter(page) }`) are skipped — they are not
// modelled yet — and the declarations around them are kept.
type PageRule struct {
	Selector     string
	Declarations []Declaration
}

// ParsePageRules returns the @page rules of a stylesheet, in order, honouring
// an enclosing @media (a @page inside `@media print` applies under Print
// only) and unwrapping @layer exactly as ParseStylesheetMedia does for style
// rules. Style rules and every other at-rule are passed over: the page
// context is read separately from the element cascade, which never sees a
// @page block (parseRules skips it as an unknown at-rule).
func ParsePageRules(src string, m Media) []PageRule {
	return parsePageRules(stripComments(src), m)
}

func parsePageRules(src string, m Media) []PageRule {
	var rules []PageRule
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
		// A bare at-rule statement ("@import url(x);", "@layer a, b;") has no
		// block of its own and rides along in the prelude of the next one —
		// see parseRules, whose walk this mirrors.
		if semi := strings.LastIndexByte(prelude, ';'); semi >= 0 {
			prelude = strings.TrimSpace(prelude[semi+1:])
		}
		lower := strings.ToLower(prelude)
		switch {
		case strings.HasPrefix(lower, "@media"):
			if mediaMatchesOn(lower[len("@media"):], m) {
				rules = append(rules, parsePageRules(body, m)...)
			}
		case strings.HasPrefix(lower, "@layer"):
			rules = append(rules, parsePageRules(body, m)...)
		case isPageRule(lower):
			decls := ParseDeclarations(stripNestedBlocks(body))
			for _, sel := range pageSelectors(prelude[len("@page"):]) {
				rules = append(rules, PageRule{Selector: sel, Declarations: decls})
			}
		}
	}
	return rules
}

// isPageRule reports whether a lower-cased, trimmed prelude is a @page rule:
// "@page" alone, or followed by whitespace or a ':' pseudo-class
// ("@page:first" is valid without a space) — never a longer at-rule name
// that merely starts with those letters.
func isPageRule(lower string) bool {
	const at = "@page"
	if !strings.HasPrefix(lower, at) {
		return false
	}
	return len(lower) == len(at) || isCSSSpace(lower[len(at)]) || lower[len(at)] == ':'
}

// pageSelectors splits what follows "@page" into its selectors: "" for the
// unqualified rule, else each comma-separated selector with its
// pseudo-classes lower-cased (they are case-insensitive; a page NAME is an
// identifier and is kept as written).
func pageSelectors(rest string) []string {
	var out []string
	for _, sel := range strings.Split(rest, ",") {
		sel = strings.TrimSpace(sel)
		if colon := strings.IndexByte(sel, ':'); colon >= 0 {
			sel = sel[:colon] + strings.ToLower(sel[colon:])
		}
		out = append(out, sel)
	}
	return out
}

// stripNestedBlocks removes every nested block — an @page margin box such
// as `@top-center { content: counter(page) }` — together with its prelude
// from a declaration-block body, keeping the declarations around it. A
// margin box is a nested AT-RULE: ParseDeclarations, which splits on
// top-level semicolons and knows nothing of braces, would otherwise fuse it
// with the neighbouring declarations into garbage on both sides (the
// `margin` before it would survive only by luck of the semicolon, the one
// after it never). An unterminated nested block drops the rest of the body.
func stripNestedBlocks(body string) string {
	var b strings.Builder
	start := 0 // start of the text (a declaration, or a block's prelude) since the last ';'
	for i := 0; i < len(body); i++ {
		switch body[i] {
		case ';':
			b.WriteString(body[start : i+1])
			start = i + 1
		case '{':
			end, ok := matchBrace(body, i)
			if !ok {
				return b.String()
			}
			i = end
			start = i + 1
		}
	}
	b.WriteString(body[start:])
	return b.String()
}

// PageSpec is a resolved page: the page box size and margins in CSS px. A
// zero Width/Height means size was not given (auto — the consumer's default
// page; `size: landscape` alone, an orientation for that default, also
// leaves them zero). MarginSet says which margins a rule set; an unset
// margin is the consumer's default too.
type PageSpec struct {
	Width, Height float64
	Margin        [4]float64 // top, right, bottom, left, CSS px
	MarginSet     [4]bool
}

// ResolvePage applies, in source order, every rule whose selector is "" and
// then every rule whose selector equals selector, so a ":first" page
// inherits the unqualified rule's values and overrides what it sets.
// Understood: size (auto | one or two lengths — one length is a square page
// | a named size and/or portrait|landscape, in either order) and margin /
// margin-top|right|bottom|left (absolute lengths, em/rem at 16px; a
// percentage or auto is ignored). Anything else — margin boxes, page,
// bleed, marks — is ignored.
func ResolvePage(rules []PageRule, selector string) PageSpec {
	var spec PageSpec
	for _, r := range rules {
		if r.Selector == "" {
			spec.applyRule(r)
		}
	}
	if selector != "" {
		for _, r := range rules {
			if r.Selector == selector {
				spec.applyRule(r)
			}
		}
	}
	return spec
}

// applyRule applies one @page rule's declarations to the spec, in order; a
// declaration whose value does not parse leaves the earlier value standing.
func (p *PageSpec) applyRule(r PageRule) {
	for _, d := range r.Declarations {
		v := strings.ToLower(strings.TrimSpace(d.Value))
		switch d.Property {
		case "size":
			if w, h, ok := parsePageSize(v); ok {
				p.Width, p.Height = w, h
			}
		case "margin":
			if e, ok := parsePageEdges(v); ok {
				p.Margin, p.MarginSet = e, [4]bool{true, true, true, true}
			}
		case "margin-top":
			p.setMargin(0, v)
		case "margin-right":
			p.setMargin(1, v)
		case "margin-bottom":
			p.setMargin(2, v)
		case "margin-left":
			p.setMargin(3, v)
		}
	}
}

func (p *PageSpec) setMargin(side int, v string) {
	if px, ok := pageLength(v); ok {
		p.Margin[side], p.MarginSet[side] = px, true
	}
}

// pageLength resolves a page-context length: any unit parseLength
// understands (the absolute units, em/rem at the 16px root default — a page
// box has no element font-size to be relative to); auto and a percentage
// are not lengths here.
func pageLength(v string) (float64, bool) {
	l, ok := parseLength(v, 16)
	if !ok || l.Auto || l.IsPercent {
		return 0, false
	}
	return l.Px, true
}

// parsePageEdges parses the 1-to-4 value margin shorthand into [top, right,
// bottom, left] px with CSS's usual expansion; any value that is not a
// length rejects the whole declaration.
func parsePageEdges(v string) ([4]float64, bool) {
	fields := strings.Fields(v)
	px := make([]float64, 0, len(fields))
	for _, f := range fields {
		n, ok := pageLength(f)
		if !ok {
			return [4]float64{}, false
		}
		px = append(px, n)
	}
	switch len(px) {
	case 1:
		return [4]float64{px[0], px[0], px[0], px[0]}, true
	case 2:
		return [4]float64{px[0], px[1], px[0], px[1]}, true
	case 3:
		return [4]float64{px[0], px[1], px[2], px[1]}, true
	case 4:
		return [4]float64{px[0], px[1], px[2], px[3]}, true
	}
	return [4]float64{}, false
}

// mmPx and inPx convert a millimetre / inch dimension to CSS px (96 per inch).
func mmPx(mm float64) float64 { return mm * 96 / 25.4 }
func inPx(in float64) float64 { return in * 96 }

// pageSizes are the named page sizes of CSS Paged Media 3 §7.2 as portrait
// width × height in CSS px: the ISO A and B series and the JIS B series in
// millimetres, the North American sizes in inches.
var pageSizes = map[string][2]float64{
	"a5":     {mmPx(148), mmPx(210)},
	"a4":     {mmPx(210), mmPx(297)},
	"a3":     {mmPx(297), mmPx(420)},
	"b5":     {mmPx(176), mmPx(250)},
	"b4":     {mmPx(250), mmPx(353)},
	"jis-b5": {mmPx(182), mmPx(257)},
	"jis-b4": {mmPx(257), mmPx(364)},
	"letter": {inPx(8.5), inPx(11)},
	"legal":  {inPx(8.5), inPx(14)},
	"ledger": {inPx(11), inPx(17)},
}

// parsePageSize parses a (lower-cased) `size` value: auto, one length (a
// square page) or two (width height), or a named size and/or an orientation
// in either order — `A4 landscape` and `landscape A4` both parse, landscape
// swapping the named size's width and height, portrait being the default.
// An orientation alone is the consumer's default page in that orientation,
// which PageSpec can only express as auto (zero width and height).
func parsePageSize(v string) (w, h float64, ok bool) {
	fields := strings.Fields(v)
	if len(fields) == 0 || len(fields) > 2 {
		return 0, 0, false
	}
	if fields[0] == "auto" {
		return 0, 0, len(fields) == 1
	}
	if w, ok := pageLength(fields[0]); ok {
		if len(fields) == 1 {
			return w, w, true
		}
		if h, ok := pageLength(fields[1]); ok {
			return w, h, true
		}
		return 0, 0, false
	}
	var size [2]float64
	var haveSize, haveOrient, landscape bool
	for _, f := range fields {
		switch {
		case f == "portrait" || f == "landscape":
			if haveOrient {
				return 0, 0, false
			}
			haveOrient, landscape = true, f == "landscape"
		default:
			sz, known := pageSizes[f]
			if !known || haveSize {
				return 0, 0, false
			}
			size, haveSize = sz, true
		}
	}
	if landscape {
		size[0], size[1] = size[1], size[0]
	}
	return size[0], size[1], true
}

// DocumentPage gathers the @page rules of every <style> in root (document
// order) and of externalSheets (external sheets first, as in the cascade: a
// <link> stylesheet loads in the head, before the document's own <style>),
// evaluated against m, and resolves selector — "" for the document's
// default page. externalSheets should come from the same Media-aware fetch
// the cascade uses (engine.LoadStylesheets), so a <link media="print">
// sheet's @page is seen exactly when its style rules are.
func DocumentPage(root *dom.Node, externalSheets []string, m Media, selector string) PageSpec {
	var rules []PageRule
	for _, sheet := range externalSheets {
		rules = append(rules, ParsePageRules(sheet, m)...)
	}
	rules = append(rules, ParsePageRules(styleElementText([]*dom.Node{root}), m)...)
	return ResolvePage(rules, selector)
}
