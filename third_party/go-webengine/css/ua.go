// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package css

// uaDescendantRules are the user-agent stylesheet rules that need a descendant
// selector (which the per-tag uaDeclarations cannot express). They alternate the
// list marker glyph by nesting depth exactly like a browser's UA sheet: a `ul`
// at depth 1 is a disc (set by the tag rule), a `ul ul` at depth 2 a circle, and
// a `ul ul ul` at depth 3-or-deeper a square (the deepest matching, highest
// specificity, rule wins). They are matched at UA origin, so any author rule
// still overrides them.
var uaDescendantRules = ParseStylesheet(
	"ul ul { list-style-type: circle }\n" +
		"ul ul ul { list-style-type: square }\n" +
		// A closed <details> (no "open" attribute) shows only its <summary>;
		// every OTHER direct child is native browser behaviour, not a CSS
		// property an author sets — confirmed load-bearing live: pkg.go.dev's
		// help tooltips are <details class="go-Tooltip"><summary>...</summary>
		// <p role="tooltip">the tooltip text</p></details>, toggled open only
		// by a click (a static render never triggers one); without this rule
		// every such tooltip's real text rendered permanently visible,
		// overlapping the page. An `open`-attributed <details> (or any author
		// rule targeting its children) is unaffected — this selector simply
		// never matches one.
		"details:not([open]) > :not(summary) { display: none }\n" +
		// <input>'s tag-level default (uaDeclarations, above) is the
		// text-field look; these attribute-selector overrides narrow that
		// for the subtypes that need something else. A button-like input
		// gets the SAME grey button chrome a real <button>/<select> gets
		// (attribute selectors outrank the bare-tag rule they override,
		// same specificity ordering as any real stylesheet). Checkbox/
		// radio get no generic background/border at all: paint's own
		// checkbox-square renderer (paintCheckboxLike) only ever consults
		// Style.Background/Border/BackgroundImages when the author also
		// sets `appearance:none` (see AppearanceNone's own doc comment),
		// in which case this transparent/0 default is exactly a real
		// browser's own reset for a native-chrome-suppressed checkbox —
		// invisible until the author's OWN rule (like this one) paints
		// something, never a leftover white field-coloured square. In the
		// far more common case of no appearance:none at all, this value
		// is still never drawn (the generic square path ignores Style
		// entirely), so it remains only a trap-avoider for some future
		// consumer (e.g. a JS getComputedStyle) reading it.
		"input[type=button], input[type=submit], input[type=reset] { background-color: #efefef }\n" +
		"input[type=checkbox], input[type=radio] { background-color: transparent; border: 0 }\n")

// uaDeclarations returns the user-agent default declarations for a tag, as
// property:value pairs. These mirror a browser's default stylesheet for the
// common structural and text tags Phase 0 supports. Values in px are chosen to
// match typical browser defaults at a 16px root font-size. quirks is the
// document's quirks-mode flag (see dom.Node.Quirks) — consulted only by the
// handful of tags whose UA default genuinely differs in quirks mode.
func uaDeclarations(tag string, quirks bool) []Declaration {
	switch tag {
	case "html", "body", "div", "section", "article", "header", "footer",
		"nav", "main", "aside", "figure", "form", "hr",
		"blockquote", "pre", "address", "fieldset", "figcaption",
		"dl", "dd", "dt":
		d := []Declaration{{Property: "display", Value: "block"}}
		if tag == "body" {
			d = append(d, Declaration{Property: "margin", Value: "8px"})
		}
		if tag == "blockquote" || tag == "figure" {
			d = append(d, Declaration{Property: "margin", Value: "16px 40px"})
		}
		if tag == "dd" {
			d = append(d, Declaration{Property: "margin-left", Value: "40px"})
		}
		if tag == "pre" {
			d = append(d, Declaration{Property: "font-family", Value: "monospace"},
				Declaration{Property: "white-space", Value: "pre"}, Declaration{Property: "margin", Value: "13px 0"})
		}
		return d
	case "table":
		d := []Declaration{{Property: "display", Value: "table"}, {Property: "border-color", Value: "gray"}}
		if quirks {
			// The HTML spec's quirks-mode UA stylesheet resets several
			// inherited properties on <table> to their initial value —
			// text-align is the one confirmed load-bearing live:
			// news.ycombinator.com has no <!DOCTYPE> at all (quirks mode)
			// and wraps its whole page in `<center><table>...</table>
			// </center>` — real browsers use `<center>`'s block-centering
			// effect to centre the TABLE on the page without also
			// inheriting centered TEXT into every cell, which this reset is
			// what actually prevents. font-weight/font-style/font-variant/
			// font-size/line-height/white-space are also reset in the spec's
			// quirks-mode table rule but are not added here — no live page
			// has shown them load-bearing yet, and adding them speculatively
			// would be guessing rather than fixing a confirmed defect.
			d = append(d, Declaration{Property: "text-align", Value: "left"})
		}
		return d
	case "thead", "tbody", "tfoot":
		return []Declaration{{Property: "display", Value: "table-row-group"}}
	case "tr":
		return []Declaration{{Property: "display", Value: "table-row"}}
	case "td":
		// vertical-align:middle is a real browser's own UA default for a
		// table cell (distinct from every other box, whose default is the
		// property's OWN initial value, baseline) — confirmed load-bearing
		// live: news.ycombinator.com's own top-nav table (round 147, 2026-
		// 09-30) sets no vertical-align/valign anywhere on its three cells
		// (an 18px logo image, a line of nav text, a "login" link), which a
		// real browser centres against each other; without this default they
		// were all top-aligned instead, a ~4px visible misalignment in the
		// very first row of the page.
		return []Declaration{{Property: "display", Value: "table-cell"}, {Property: "padding", Value: "1px"},
			{Property: "vertical-align", Value: "middle"}}
	case "th":
		return []Declaration{{Property: "display", Value: "table-cell"}, {Property: "padding", Value: "1px"},
			{Property: "font-weight", Value: "bold"}, {Property: "text-align", Value: "center"},
			{Property: "vertical-align", Value: "middle"}}
	case "center":
		// The legacy <center> element: a block that centres its inline content and
		// its definite-width block/table children (see AlignCenterBlocks).
		return []Declaration{{Property: "display", Value: "block"}, {Property: "text-align", Value: "-webkit-center"}}
	case "font", "big", "nobr", "wbr", "strike":
		d := []Declaration{{Property: "display", Value: "inline"}}
		if tag == "big" {
			// `larger` ≈ 1.2× the parent size; expressed in em since the engine
			// resolves font-size against the parent.
			d = append(d, Declaration{Property: "font-size", Value: "1.2em"})
		}
		if tag == "strike" {
			d = append(d, Declaration{Property: "text-decoration", Value: "line-through"})
		}
		return d
	case "caption":
		return []Declaration{{Property: "display", Value: "block"}, {Property: "text-align", Value: "center"}}
	case "p":
		return []Declaration{{Property: "display", Value: "block"}, {Property: "margin", Value: "16px 0"}}
	case "h1":
		return []Declaration{{Property: "display", Value: "block"}, {Property: "font-size", Value: "32px"}, {Property: "font-weight", Value: "bold"}, {Property: "margin", Value: "21px 0"}}
	case "h2":
		return []Declaration{{Property: "display", Value: "block"}, {Property: "font-size", Value: "24px"}, {Property: "font-weight", Value: "bold"}, {Property: "margin", Value: "20px 0"}}
	case "h3":
		return []Declaration{{Property: "display", Value: "block"}, {Property: "font-size", Value: "19px"}, {Property: "font-weight", Value: "bold"}, {Property: "margin", Value: "18px 0"}}
	case "h4":
		return []Declaration{{Property: "display", Value: "block"}, {Property: "font-size", Value: "16px"}, {Property: "font-weight", Value: "bold"}, {Property: "margin", Value: "21px 0"}}
	case "h5":
		return []Declaration{{Property: "display", Value: "block"}, {Property: "font-size", Value: "13px"}, {Property: "font-weight", Value: "bold"}, {Property: "margin", Value: "22px 0"}}
	case "h6":
		return []Declaration{{Property: "display", Value: "block"}, {Property: "font-size", Value: "11px"}, {Property: "font-weight", Value: "bold"}, {Property: "margin", Value: "24px 0"}}
	case "ul":
		return []Declaration{{Property: "display", Value: "block"}, {Property: "margin", Value: "16px 0"}, {Property: "padding-left", Value: "40px"},
			{Property: "list-style-type", Value: "disc"}}
	case "ol":
		return []Declaration{{Property: "display", Value: "block"}, {Property: "margin", Value: "16px 0"}, {Property: "padding-left", Value: "40px"},
			{Property: "list-style-type", Value: "decimal"}}
	case "li":
		return []Declaration{{Property: "display", Value: "list-item"}}
	case "a":
		return []Declaration{{Property: "color", Value: "#0000ee"}}
	case "strong", "b":
		return []Declaration{{Property: "font-weight", Value: "bold"}}
	case "em", "i", "cite", "var", "dfn":
		return []Declaration{{Property: "font-style", Value: "italic"}}
	case "small":
		return []Declaration{{Property: "font-size", Value: "13px"}}
	case "code", "kbd", "samp", "tt":
		return []Declaration{{Property: "font-family", Value: "monospace"}}
	case "span", "label", "abbr", "sup", "sub", "mark", "u", "s", "del", "ins",
		"time", "q", "img":
		return []Declaration{{Property: "display", Value: "inline"}}
	case "button", "select":
		// A real button/select has a visible UA-default background and border
		// that author CSS very commonly resets (e.g. a nav bar's `<button>`
		// styled to look like a plain text link) — this MUST be a real,
		// author-overridable cascade value, not a hardcoded colour paint
		// picks regardless of styling, or every such reset renders with a
		// fake generic gray box instead of the invisible chrome the author
		// asked for. Confirmed load-bearing live: github.com's top nav items
		// are literal `<button>` elements with `background:0 0;border:0`
		// (its own CSS, read directly) rendering as solid gray pills without
		// this. Colours match paint's PRE-EXISTING hardcoded fallback
		// (`formButtonBg`/`formBorder` in paint/paint.go) exactly, so an
		// actually-unstyled control's appearance is unchanged — only a
		// STYLED one now differs, correctly.
		return []Declaration{{Property: "display", Value: "inline"},
			{Property: "background-color", Value: "#efefef"},
			{Property: "border", Value: "1px solid #767676"}}
	case "input", "textarea":
		// Same reasoning as button/select above; the default (white-ish
		// field) UA look for a text-like control. <input type=checkbox/
		// radio/button/submit/reset> are narrowed by the attribute-selector
		// rules in uaDescendantRules below — checkbox/radio lose this
		// background/border entirely (paint draws their own checked-state
		// square and never consults Style.Background/Border for them), and
		// button-like input types get the SAME grey button look as a real
		// <button>.
		return []Declaration{{Property: "display", Value: "inline"},
			{Property: "background-color", Value: "#ffffff"},
			{Property: "border", Value: "1px solid #767676"}}
	case "head", "title", "style", "script", "meta", "link", "base", "noscript":
		return []Declaration{{Property: "display", Value: "none"}}
	case "option":
		// A real <select> is a replaced, OS-native control: it shows only the
		// currently selected option's text, on one line, entirely opaque to CSS
		// box layout — <option>/<optgroup> children never participate in normal
		// document flow the way this engine's generic "select" (line 114,
		// display:inline) fallback implies. Observed live on pkg.go.dev/net/http:
		// its version/tab-switcher <select> (dozens of <option>s, one holding the
		// page's ENTIRE alphabetical symbol index as option text) rendered every
		// option's text as ordinary inline content, wrapping across 455px/~19
		// lines of concatenated identifiers stacked at the select's DOM position.
		// This engine has no native form-control rendering at all (an <input>'s
		// value is equally never shown — an existing, accepted simplification),
		// so hiding <option> content is the same honest-about-the-gap choice
		// already used for <template>'s subtree above, not a new kind of gap.
		return []Declaration{{Property: "display", Value: "none"}}
	case "template":
		// A <template>'s children are inert: per the HTML spec they live in the
		// element's .content DocumentFragment, never in the normal document tree,
		// so no browser lays them out — CSS cannot make them visible (unlike the
		// other display:none defaults above, this one is not really author-
		// overridable, it just happens to be modelled the same way). This engine
		// has no separate content-fragment concept and no Shadow DOM, so a real
		// page's <template shadowrootmode="open"> (declarative shadow DOM) would
		// otherwise have its shadow markup parsed straight into the light-DOM
		// tree and painted inline — observed live on developer.mozilla.org: 23
		// declarative-shadow templates rendered as unstyled, unscoped nav trees
		// stacked on top of the article text. Hiding the subtree is honest about
		// the gap (no content) rather than rendering something broken.
		return []Declaration{{Property: "display", Value: "none"}}
	default:
		return nil
	}
}
