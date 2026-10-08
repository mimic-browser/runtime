// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package css

import "strings"

// Media is what a stylesheet is evaluated against: the media TYPE — screen
// (the zero value) or print — that "@media print", "@media screen" and a
// <link media="print"> select on, and the viewport width that min-width /
// max-width features compare with. A browser's screen render and its print
// preview of the same page differ by exactly this value: sites hide their
// navigation, sidebars and footers under @media print, and a print-only
// stylesheet is skipped on screen. Every Cascade*/ParseStylesheet* entry
// point that takes only a viewport width evaluates for screen.
type Media struct {
	Type  string  // Screen ("" or "screen") or Print; compared case-insensitively
	Width float64 // viewport width, CSS px
}

// Media types a Media.Type can name. Any other type (speech, tv, …) never
// matches.
const (
	Screen = "screen"
	Print  = "print"
)

// isPrint reports whether m is the print medium; anything else is screen.
func (m Media) isPrint() bool { return strings.EqualFold(strings.TrimSpace(m.Type), Print) }

// mediaTypes are the CSS media types recognised at the head of a query. Only
// all/screen/print can ever match; the deprecated ones are listed so that a
// query naming one is understood as a type (and rejected) rather than passed
// through to the feature evaluation as an unknown word.
var mediaTypes = map[string]bool{
	"all": true, "screen": true, "print": true,
	"speech": true, "aural": true, "braille": true, "embossed": true,
	"handheld": true, "projection": true, "tty": true, "tv": true,
}

// mediaMatchesOn evaluates a media condition — everything after "@media", or a
// <link media> / @import query — against m. A comma-separated list matches
// if any of its queries does (an empty condition always matches). Within one
// query, in order: a leading "only" is ignored (it exists to hide the query
// from ancient browsers); a leading "not" negates the rest — which is how
// Tailwind's "not all and (min-width:…)" idiom for a max-width breakpoint
// reads (see notAllAndPrefix for the history); a leading media type selects
// on m.Type — "print" matches only print, "screen" only screen, "all"
// always, any other type never; then every min-width/max-width feature (colon
// and Level 4 comparison syntax) must hold against m.Width, and every
// hover/pointer feature (see inputFeaturesHold) must hold for this engine's
// one assumed mouse-equipped desktop rendering context; anything else
// (colour, resolution, truly unknown features) matches optimistically so
// desktop layout rules are applied.
func mediaMatchesOn(cond string, m Media) bool {
	for _, q := range splitMediaList(cond) {
		if mediaQueryMatches(q, m) {
			return true
		}
	}
	return false
}

// mediaQueryMatches evaluates one query (no commas) against m.
func mediaQueryMatches(q string, m Media) bool {
	q = strings.TrimSpace(strings.ToLower(q))
	if rest, ok := cutLeadingWord(q, "only"); ok {
		q = rest
	}
	if rest, ok := cutLeadingWord(q, "not"); ok {
		return !mediaQueryMatches(rest, m)
	}
	if typ, rest, ok := leadingWord(q); ok && mediaTypes[typ] {
		switch typ {
		case "all":
		case "print":
			if !m.isPrint() {
				return false
			}
		case "screen":
			if m.isPrint() {
				return false
			}
		default:
			return false
		}
		q = rest
	}
	return widthFeaturesHold(q, m.Width) && inputFeaturesHold(q)
}

// leadingWord splits a leading identifier ([a-z-]+) off q when one is there
// and is followed by whitespace or the end of q — "screen and (…)" → "screen",
// " and (…)"; "(min-width:…)" → not ok.
func leadingWord(q string) (word, rest string, ok bool) {
	i := 0
	for i < len(q) && (q[i] >= 'a' && q[i] <= 'z' || q[i] == '-') {
		i++
	}
	if i == 0 || (i < len(q) && q[i] != ' ' && q[i] != '\t' && q[i] != '\n') {
		return "", q, false
	}
	return q[:i], strings.TrimSpace(q[i:]), true
}

// cutLeadingWord strips word from the head of q when q begins with exactly
// that identifier.
func cutLeadingWord(q, word string) (string, bool) {
	if w, rest, ok := leadingWord(q); ok && w == word {
		return rest, true
	}
	return q, false
}

// splitMediaList splits a media list on the commas outside parentheses —
// never the comma inside a calc() or a min()/max() feature value.
func splitMediaList(s string) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}
