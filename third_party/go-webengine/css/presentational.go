// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package css

import (
	"strconv"
	"strings"

	"github.com/go-webengine/engine/dom"
)

// presentationalHints maps an element's legacy HTML presentational attributes
// (width, height, bgcolor, align, <font> color/size/face, …) to CSS
// declarations. Browsers apply these as "presentation hints" at author origin
// with zero specificity, ordered before any author stylesheet — so a real CSS
// rule always wins, but a UA default does not. The caller adds them at that
// position in the cascade.
//
// Only the attributes that materially affect layout/paint of real pages are
// mapped; unknown attributes and unparseable values are skipped.
func presentationalHints(n *dom.Node) []Declaration {
	var d []Declaration
	tag := n.Tag
	attr := func(name string) (string, bool) {
		v, ok := n.Attribute(name)
		return strings.TrimSpace(v), ok
	}

	// width/height on the elements where the HTML attribute is a length hint.
	switch tag {
	case "table", "td", "th", "img", "col", "colgroup", "hr", "canvas",
		"video", "object", "iframe", "embed", "input":
		if v, ok := attr("width"); ok {
			if p := lengthHint(v); p != "" {
				d = append(d, Declaration{Property: "width", Value: p})
			}
		}
		if v, ok := attr("height"); ok {
			if p := lengthHint(v); p != "" {
				d = append(d, Declaration{Property: "height", Value: p})
			}
		}
	}

	// bgcolor → background-color (table, cells, rows, body, and legacy others).
	if v, ok := attr("bgcolor"); ok && v != "" {
		d = append(d, Declaration{Property: "background-color", Value: v})
	}

	// align: on replaced/table boxes it floats (or centres a table); on flow
	// containers it sets the inline text alignment (centre also centres blocks).
	if v, ok := attr("align"); ok {
		switch tag {
		case "img", "object", "iframe", "embed", "input":
			switch strings.ToLower(v) {
			case "left":
				d = append(d, Declaration{Property: "float", Value: "left"})
			case "right":
				d = append(d, Declaration{Property: "float", Value: "right"})
			}
		case "table":
			switch strings.ToLower(v) {
			case "left":
				d = append(d, Declaration{Property: "float", Value: "left"})
			case "right":
				d = append(d, Declaration{Property: "float", Value: "right"})
			case "center":
				d = append(d, Declaration{Property: "margin-left", Value: "auto"}, Declaration{Property: "margin-right", Value: "auto"})
			}
		default: // td, th, tr, div, p, thead, tbody, tfoot, col, caption, h1-6, …
			switch strings.ToLower(v) {
			case "left":
				d = append(d, Declaration{Property: "text-align", Value: "left"})
			case "right":
				d = append(d, Declaration{Property: "text-align", Value: "right"})
			case "center":
				// Legacy centre-including-blocks, like the <center> element.
				d = append(d, Declaration{Property: "text-align", Value: "-webkit-center"})
			}
		}
	}

	// valign: a table cell's legacy attribute counterpart to `vertical-align`
	// — confirmed load-bearing live (round 147, 2026-09-30): news.ycombinator.com's
	// own story rows set `valign="top"` on their rank-number and vote-arrow
	// `<td>`s, overriding the UA default `vertical-align:middle` (see ua.go's
	// own td/th entries) those same cells would otherwise get. Scoped to
	// td/th, the only tags real usage has confirmed need it — the spec also
	// allows it on tr/thead/tbody/tfoot/col/colgroup, not modelled here.
	if v, ok := attr("valign"); ok && (tag == "td" || tag == "th") {
		switch strings.ToLower(v) {
		case "top":
			d = append(d, Declaration{Property: "vertical-align", Value: "top"})
		case "middle":
			d = append(d, Declaration{Property: "vertical-align", Value: "middle"})
		case "bottom":
			d = append(d, Declaration{Property: "vertical-align", Value: "bottom"})
		case "baseline":
			d = append(d, Declaration{Property: "vertical-align", Value: "baseline"})
		}
	}

	// Table border attribute → a solid border of that width (border="0" is none).
	if tag == "table" {
		if v, ok := attr("border"); ok && v != "" && v != "0" {
			if p := lengthHint(v); p != "" {
				d = append(d, Declaration{Property: "border-width", Value: p}, Declaration{Property: "border-style", Value: "solid"})
			}
		}
	}

	// <font color=… size=… face=…>.
	if tag == "font" || tag == "basefont" {
		if v, ok := attr("color"); ok && v != "" {
			d = append(d, Declaration{Property: "color", Value: v})
		}
		if v, ok := attr("face"); ok && v != "" {
			d = append(d, Declaration{Property: "font-family", Value: v})
		}
		if v, ok := attr("size"); ok {
			if fs := fontSizeAttr(v); fs != "" {
				d = append(d, Declaration{Property: "font-size", Value: fs})
			}
		}
	}

	// <body text=… bgcolor=… link=…>: the text colour and page background.
	if tag == "body" {
		if v, ok := attr("text"); ok && v != "" {
			d = append(d, Declaration{Property: "color", Value: v})
		}
	}

	return d
}

// lengthHint turns an HTML length attribute value into a CSS length string: a
// bare number becomes px, an "N%" is kept, and anything else is dropped. A
// trailing "px" (non-standard but seen in the wild) is tolerated.
func lengthHint(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if strings.HasSuffix(v, "%") {
		if _, err := strconv.ParseFloat(strings.TrimSuffix(v, "%"), 64); err == nil {
			return v
		}
		return ""
	}
	v = strings.TrimSuffix(v, "px")
	if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 {
		return v + "px"
	}
	return ""
}

// fontSizeAttr maps a legacy <font size> value to a CSS font-size. Absolute
// sizes 1..7 map to the HTML size table; a leading + or - is a relative step
// from the size-3 baseline (approximated, since the real base is the inherited
// size). Out-of-range or unparseable values are dropped.
func fontSizeAttr(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	rel := 0
	switch v[0] {
	case '+':
		rel, v = 1, v[1:]
	case '-':
		rel, v = -1, v[1:]
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return ""
	}
	if rel != 0 {
		n = 3 + rel*n
	}
	// The HTML absolute size ladder (size 3 == medium/16px).
	ladder := map[int]string{1: "10px", 2: "13px", 3: "16px", 4: "18px", 5: "24px", 6: "32px", 7: "48px"}
	if n < 1 {
		n = 1
	}
	if n > 7 {
		n = 7
	}
	return ladder[n]
}
