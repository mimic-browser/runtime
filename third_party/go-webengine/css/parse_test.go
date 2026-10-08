// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package css

import "testing"

func TestStripComments(t *testing.T) {
	if got := stripComments("a/*x*/b/* y */c"); got != "abc" {
		t.Errorf("stripComments = %q", got)
	}
	if got := stripComments("a/*unterminated"); got != "a" {
		t.Errorf("unterminated = %q", got)
	}
	if got := stripComments("plain"); got != "plain" {
		t.Errorf("plain = %q", got)
	}
}

func TestParseDeclarations(t *testing.T) {
	d := ParseDeclarations("color: red; font-size:16px ; bad ; :novalue; prop: ; x:1 !important")
	// Expect color, font-size, x (bad, empty-prop, empty-value skipped).
	want := map[string]string{"color": "red", "font-size": "16px", "x": "1"}
	if len(d) != len(want) {
		t.Fatalf("got %d decls: %v", len(d), d)
	}
	for _, decl := range d {
		if want[decl.Property] != decl.Value {
			t.Errorf("decl %q = %q want %q", decl.Property, decl.Value, want[decl.Property])
		}
		// Only x carries !important; the marker must never leak into Value, and
		// no other declaration should pick it up.
		wantImportant := decl.Property == "x"
		if decl.Important != wantImportant {
			t.Errorf("decl %q Important = %v want %v", decl.Property, decl.Important, wantImportant)
		}
	}
}

// TestParseDeclarationsEmptyCustomPropertyKept covers a real regression: an
// ORDINARY property with an empty value ("prop: ;") is correctly meaningless
// and skipped (see TestParseDeclarations above), but a CUSTOM property set to
// empty ("--foo: ;") is a real, spec-valid, load-bearing CSS construct — the
// widely-used "CSS toggle" pattern (postcss-preset-env's light-dark()
// polyfill, seen live on developer.mozilla.org) sets a guard variable to
// `initial` for one theme and to EMPTY for the other, so var() fallback
// chains flip between them. Dropping the empty declaration here left every
// such guard stuck at its non-empty branch forever.
func TestParseDeclarationsEmptyCustomPropertyKept(t *testing.T) {
	d := ParseDeclarations("--set: red; --empty: ; prop: ")
	if len(d) != 2 {
		t.Fatalf("got %d decls: %v, want 2 (--set, --empty)", len(d), d)
	}
	want := map[string]string{"--set": "red", "--empty": ""}
	for _, decl := range d {
		v, ok := want[decl.Property]
		if !ok {
			t.Errorf("unexpected decl %q", decl.Property)
			continue
		}
		if decl.Value != v {
			t.Errorf("decl %q = %q want %q", decl.Property, decl.Value, v)
		}
	}
}

// TestParseDeclarationsImportantCaseAndSpacing covers case-insensitivity and
// whitespace around the !important marker, and that a plain declaration is
// unaffected.
func TestParseDeclarationsImportantCaseAndSpacing(t *testing.T) {
	d := ParseDeclarations("a: 1 !IMPORTANT; b: 2  !important  ; c: 3")
	want := map[string]struct {
		val       string
		important bool
	}{
		"a": {"1", true},
		"b": {"2", true},
		"c": {"3", false},
	}
	if len(d) != len(want) {
		t.Fatalf("got %d decls: %v", len(d), d)
	}
	for _, decl := range d {
		w := want[decl.Property]
		if decl.Value != w.val || decl.Important != w.important {
			t.Errorf("decl %q = %q,important=%v want %q,important=%v",
				decl.Property, decl.Value, decl.Important, w.val, w.important)
		}
	}
}

func TestParseStylesheet(t *testing.T) {
	css := `
	/* a comment */
	h1, .big { font-size: 30px; color: blue }
	@font-face { src: url(x) }
	broken {
	`
	rules := ParseStylesheet(css)
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule (at-rules + unterminated skipped), got %d: %+v", len(rules), rules)
	}
	r := rules[0]
	if len(r.Selectors) != 2 {
		t.Errorf("selectors = %v", r.Selectors)
	}
	if len(r.Declarations) != 2 {
		t.Errorf("declarations = %v", r.Declarations)
	}
}

func TestParseStylesheetMediaQueries(t *testing.T) {
	css := `
	@media (min-width: 640px) { .infobox { float: right; width: 22em } }
	@media (max-width: 639px) { .infobox { float: none } }
	@media print { p { color: red } }
	@media screen { a { color: blue } }
	@supports (display:grid) { div { display: grid } }
	`
	// At vw=1024: min-width:640 matches, max-width:639 does not, print never,
	// Screen and the supported grid feature query both match.
	rules := ParseStylesheetVW(css, 1024)
	got := map[string]string{}
	for _, r := range rules {
		for _, d := range r.Declarations {
			got[d.Property] = d.Value
		}
	}
	if got["float"] != "right" {
		t.Errorf("min-width infobox float should apply, rules=%+v", rules)
	}
	if got["color"] != "blue" {
		t.Error("screen media rule should apply")
	}
	if got["display"] != "grid" {
		t.Error("supported grid @supports should apply")
	}
	// The max-width:639 (mobile) and print rules must be excluded: exactly the
	// two matching blocks (infobox + a) remain.
	if len(rules) != 3 {
		t.Errorf("expected 3 matching rules, got %d: %+v", len(rules), rules)
	}

	// At a narrow viewport the mobile rule wins instead.
	mobile := ParseStylesheetVW(css, 480)
	var floatVal string
	for _, r := range mobile {
		for _, d := range r.Declarations {
			if d.Property == "float" {
				floatVal = d.Value
			}
		}
	}
	if floatVal != "none" {
		t.Errorf("at 480px the mobile float:none should apply, got %q", floatVal)
	}
}

// TestParseStylesheetNestedNotAllAnd covers Tailwind v4's real compiled shape
// for a compound range variant like "sm:max-md:inline" (min 40rem, max under
// 48rem): an OUTER "@media (min-width:40rem)" (already handled) wrapping an
// INNER "@media not all and (min-width:48rem)" for the upper bound —
// confirmed live on tailwindcss.com. Both must hold for the inner rule to
// apply: at 1024px the outer matches (>=40rem) but the inner must NOT
// (1024px is not < 48rem=768px), so the declaration is excluded; at 700px
// both hold and it is included.
func TestParseStylesheetNestedNotAllAnd(t *testing.T) {
	css := `@media (min-width:40rem) { @media not all and (min-width:48rem) { .sm\:max-md\:inline { display: inline } } }`
	wide := declValues(ParseStylesheetVW(css, 1024))
	if _, ok := wide["display"]; ok {
		t.Errorf("sm:max-md:inline must NOT apply at 1024px (above the max-md upper bound), got %+v", wide)
	}
	narrow := declValues(ParseStylesheetVW(css, 700))
	if narrow["display"] != "inline" {
		t.Errorf("sm:max-md:inline must apply at 700px (within [40rem,48rem)), got %+v", narrow)
	}
	tooNarrow := declValues(ParseStylesheetVW(css, 500))
	if _, ok := tooNarrow["display"]; ok {
		t.Errorf("sm:max-md:inline must NOT apply at 500px (below the outer min-width:40rem), got %+v", tooNarrow)
	}
}

// declValues returns a property->value map flattening every declaration of
// every rule, for the common "did this apply" style of assertion below.
func declValues(rules []Rule) map[string]string {
	got := map[string]string{}
	for _, r := range rules {
		for _, d := range r.Declarations {
			got[d.Property] = d.Value
		}
	}
	return got
}

// TestParseStylesheetLayerBasic covers the core fix: a named @layer's rules
// must be included, not dropped — Tailwind v4's default output (and many
// other frameworks) puts nearly all of its CSS inside @layer utilities, and
// this engine silently discarding it meant almost the whole stylesheet never
// reached the cascade (observed live: tailwindcss.com had 690KB of CSS but
// only 181 rules parsed out of it before this fix — 5334 after).
func TestParseStylesheetLayerBasic(t *testing.T) {
	rules := ParseStylesheetVW(`@layer utilities { .flex { display: flex } }`, 1024)
	if got := declValues(rules); got["display"] != "flex" {
		t.Errorf("@layer utilities content should be included, got %+v", got)
	}
}

// TestParseStylesheetLayerAnonymousAndNested covers an anonymous @layer (no
// name) and @media nested inside @layer (the shape Tailwind actually emits
// for responsive/dark-mode variants defined as utilities).
func TestParseStylesheetLayerAnonymousAndNested(t *testing.T) {
	css := `
	@layer { .anon { color: red } }
	@layer utilities {
		@media (min-width: 640px) { .sm\:block { display: block } }
	}
	`
	got := declValues(ParseStylesheetVW(css, 1024))
	if got["color"] != "red" {
		t.Errorf("anonymous @layer content should be included, got %+v", got)
	}
	if got["display"] != "block" {
		t.Errorf("@media nested inside @layer should still be evaluated, got %+v", got)
	}
}

// TestParseStylesheetLayerBareDeclaration covers the bare "@layer a, b, c;"
// order-declaration form (no body of its own — just establishes priority,
// which this engine does not model). It must not itself contribute rules,
// and — critically — must not swallow whatever real construct follows it,
// since a bare declaration has no '{' and so shares its textual prelude with
// the next brace found (the exact shape Tailwind emits:
// "@layer theme, base, components, utilities;" followed immediately by
// "@layer properties{...}").
func TestParseStylesheetLayerBareDeclaration(t *testing.T) {
	css := `
	@layer theme, base, utilities;
	@layer utilities { .grid { display: grid } }
	`
	got := declValues(ParseStylesheetVW(css, 1024))
	if got["display"] != "grid" {
		t.Errorf("layer after a bare order-declaration should still parse, got %+v", got)
	}
}

// TestParseStylesheetBareDeclarationBeforeNormalRule covers the same bare-
// declaration hazard when what follows is an ordinary rule, not another
// at-rule — the case a purely prefix-based check (without splitting at the
// last ';') would get wrong: it would see a prelude like
// "@layer a, b;\n.foo" and skip it wholesale, silently dropping ".foo".
func TestParseStylesheetBareDeclarationBeforeNormalRule(t *testing.T) {
	css := `
	@layer a, b;
	.foo { color: green }
	`
	got := declValues(ParseStylesheetVW(css, 1024))
	if got["color"] != "green" {
		t.Errorf("a normal rule after a bare @layer declaration must still parse, got %+v", got)
	}
}

// TestParseStylesheetOtherAtRulesStillSkipped is the regression guard: the
// @layer fix must not accidentally start recursing into unrelated at-rules
// that were correctly skipped before.
func TestParseStylesheetOtherAtRulesStillSkipped(t *testing.T) {
	css := `
	@font-face { font-family: X; src: url(x) }
	@keyframes spin { from { transform: none } to { transform: none } }
	@supports (display: subgrid) { .g { display: subgrid } }
	`
	rules := ParseStylesheetVW(css, 1024)
	if len(rules) != 0 {
		t.Errorf("expected @font-face/@keyframes/@supports to still be skipped wholesale, got %+v", rules)
	}
}

// TestParseStylesheetSupportsLightDark covers supportsConditionHolds: the one
// @supports feature test this engine answers honestly (light-dark(), which it
// genuinely supports — see lightdark.go) — reproducing postcss-preset-env's
// own light-dark() polyfill shape (confirmed live on developer.mozilla.org): a
// POSITIVE `@supports (color: light-dark(...))` block using the native
// function, and its NEGATIVE `@supports not (...)` counterpart, of which only
// one should ever apply.
func TestParseStylesheetSupportsLightDark(t *testing.T) {
	css := `
	@supports (color: light-dark(red, red)) { .a { color: red } }
	@supports not (color: light-dark(tan, tan)) { .b { background: blue } }
		@supports (display: grid) { .c { font-weight: bold } }
		@supports not (display: grid) { .d { text-align: center } }
	`
	got := declValues(ParseStylesheetVW(css, 1024))
	if got["color"] != "red" {
		t.Errorf("positive light-dark() @supports should be included, got %+v", got)
	}
	if v, ok := got["background"]; ok {
		t.Errorf("negative light-dark() @supports should be excluded, got background=%q", v)
	}
	if got["font-weight"] != "bold" {
		t.Errorf("supported grid @supports should apply, got %+v", got)
	}
	if _, ok := got["text-align"]; ok {
		t.Errorf("negated supported grid @supports should be excluded, got %+v", got)
	}
}

func TestSupportsGridAndMasks(t *testing.T) {
	for condition, want := range map[string]bool{
		"(display:grid)":     true,
		"not (display:grid)": false,
		"(display:subgrid)":  false,
		"((-webkit-mask-image:none) or (mask-image:none))":     true,
		"not ((-webkit-mask-image:none) or (mask-image:none))": false,
	} {
		if got := supportsConditionHolds(condition); got != want {
			t.Errorf("%q = %v, want %v", condition, got, want)
		}
	}
}

func TestMediaMatches(t *testing.T) {
	if mediaMatches("print", 1024) {
		t.Error("print should not match")
	}
	if !mediaMatches("screen and (min-width: 640px)", 1024) {
		t.Error("min-width 640 should match at 1024")
	}
	if mediaMatches("(min-width: 1200px)", 1024) {
		t.Error("min-width 1200 should not match at 1024")
	}
	if !mediaMatches("(max-width: 1200px)", 1024) {
		t.Error("max-width 1200 should match at 1024")
	}
	if mediaMatches("(max-width: 800px)", 1024) {
		t.Error("max-width 800 should not match at 1024")
	}
	if !mediaMatches("all", 1024) {
		t.Error("all should match")
	}
	if !mediaMatches("(min-width: abc)", 1024) {
		t.Error("unparseable width feature is ignored (matches)")
	}
	// mediaWidthRe's [0-9.]+ character class accepts a value with more than
	// one '.' (e.g. multiple decimal points), which strconv.ParseFloat then
	// rejects — must not panic, just skip this feature like any other
	// unparseable one.
	if !mediaMatches("(min-width: 1.2.3px)", 1024) {
		t.Error("a malformed numeric width (multiple dots) should be skipped, matching")
	}
}

// TestMediaMatchesRemUnits covers Tailwind v4's default breakpoints, which are
// expressed in rem, not px ("min-width:80rem" for its "xl" variant). Before
// this, an unrecognised unit was silently ignored entirely rather than just
// failing to convert — every rem-based breakpoint matched unconditionally
// regardless of viewport width, so ALL of a page's responsive font-size/
// spacing steps applied at once and the cascade fell back to picking
// whichever was declared last (usually the largest). 1rem == 16px, matching
// parseLength's own rem conversion.
func TestMediaMatchesRemUnits(t *testing.T) {
	// 1024px viewport == 64rem: exactly the Tailwind "lg" breakpoint.
	if !mediaMatches("(min-width:64rem)", 1024) {
		t.Error("min-width:64rem (1024px) should match at exactly 1024px")
	}
	if mediaMatches("(min-width:80rem)", 1024) {
		t.Error("min-width:80rem (1280px) should NOT match at 1024px")
	}
	if !mediaMatches("(max-width:80rem)", 1024) {
		t.Error("max-width:80rem (1280px) should match at 1024px")
	}
	if mediaMatches("(max-width:40rem)", 1024) {
		t.Error("max-width:40rem (640px) should NOT match at 1024px")
	}
	// A narrower viewport sits below "lg" (64rem = 1024px) but at/above "sm"
	// (40rem = 640px).
	if mediaMatches("(min-width:64rem)", 800) {
		t.Error("min-width:64rem (1024px) should not match at 800px")
	}
	if !mediaMatches("(min-width:40rem)", 800) {
		t.Error("min-width:40rem (640px) should match at 800px")
	}
}

// TestMediaMatchesRangeComparisonSyntax covers the CSS Media Queries Level 4
// range-comparison syntax ("width<=X", "width>=X", and the value-first order
// "X<=width") GitHub's Primer design system uses for its PageLayout
// breakpoints — as invisible to a colon-only min-width:/max-width: matcher as
// the missing "rem" unit was, for the same reason (falls through to "unknown
// feature, assume it matches").
func TestMediaMatchesRangeComparisonSyntax(t *testing.T) {
	if !mediaMatches("(width<=1024px)", 1024) {
		t.Error("width<=1024px should match at exactly 1024px")
	}
	if mediaMatches("(width<=1024px)", 1025) {
		t.Error("width<=1024px should NOT match at 1025px")
	}
	if !mediaMatches("(width>=1024px)", 1024) {
		t.Error("width>=1024px should match at exactly 1024px")
	}
	if mediaMatches("(width>=1024px)", 1023) {
		t.Error("width>=1024px should NOT match at 1023px")
	}
	if mediaMatches("(width<800px)", 1024) {
		t.Error("width<800px should not match at 1024px")
	}
	if !mediaMatches("(width>800px)", 1024) {
		t.Error("width>800px should match at 1024px")
	}
	// GitHub's actual breakpoints, in rem.
	if mediaMatches("(width>=48rem)", 700) {
		t.Error("width>=48rem (768px) should not match at 700px")
	}
	if !mediaMatches("(width>=48rem)", 1024) {
		t.Error("width>=48rem (768px) should match at 1024px")
	}
	// Value-first order says the same thing as width-first, for every operator
	// (each exercises a different flipCmp branch).
	if !mediaMatches("(48rem<=width)", 1024) {
		t.Error("48rem<=width should mean the same as width>=48rem")
	}
	if mediaMatches("(48rem<=width)", 700) {
		t.Error("48rem<=width should not match at 700px")
	}
	if !mediaMatches("(1024px>=width)", 1024) {
		t.Error("1024px>=width should mean the same as width<=1024px")
	}
	if mediaMatches("(1024px>=width)", 1025) {
		t.Error("1024px>=width should not match at 1025px")
	}
	if mediaMatches("(1024px<width)", 1024) {
		t.Error("1024px<width should mean the same as width>1024px: not at 1024px")
	}
	if !mediaMatches("(1024px<width)", 1025) {
		t.Error("1024px<width should match at 1025px")
	}
	if !mediaMatches("(1024px>width)", 1023) {
		t.Error("1024px>width should mean the same as width<1024px: matches at 1023px")
	}
}

// TestMediaMatchesNotAllAnd covers a real regression, found live on
// tailwindcss.com's own homepage: Tailwind v4 compiles EVERY "max-*:"
// breakpoint variant (max-sm:, and the upper bound of a compound range like
// sm:max-md:) to "@media not all and (min-width:...)" rather than a direct
// max-width feature — CSS's own idiom for negating a feature test, since
// "all" (a media type that always matches) reduces "not all and (X)" to
// "not (X)". Before this was recognised, mediaMatches evaluated the
// min-width feature INSIDE the parens completely normally and silently
// ignored the "not", so a narrow-viewport-only utility (a "text-6xl" /
// "text-white" responsive-breakpoint label, and the ENTIRE header's
// responsive nav links, both confirmed live) matched at every viewport
// ABOVE the breakpoint instead of below it — the exact opposite of what the
// rule means.
func TestMediaMatchesNotAllAnd(t *testing.T) {
	// max-sm: real compiled form — matches only BELOW 40rem (640px).
	if !mediaMatches(" not all and (min-width:40rem)", 639) {
		t.Error("not all and (min-width:40rem) should match at 639px (below the breakpoint)")
	}
	if mediaMatches(" not all and (min-width:40rem)", 640) {
		t.Error("not all and (min-width:40rem) should NOT match at 640px (at/above the breakpoint)")
	}
	if mediaMatches(" not all and (min-width:40rem)", 1024) {
		t.Error("not all and (min-width:40rem) should NOT match at 1024px — the exact bug this covers")
	}
	// The case-insensitive, no-leading-space form (a bare @import/<link media>
	// query, which is not pre-lowercased or spaced the way an @media
	// prelude's own text always is).
	if mediaMatches("NOT ALL AND (min-width:1024px)", 1024) {
		t.Error("NOT ALL AND (case-insensitive, no leading space) should NOT match at 1024px")
	}
	if !mediaMatches("NOT ALL AND (min-width:1024px)", 1023) {
		t.Error("NOT ALL AND (min-width:1024px) should match at 1023px")
	}
	// "not" negates the whole query, whatever follows it (media.go) — so
	// "not screen" is false on screen and true on print, as in a browser.
	// Until Media existed this engine recognised only the exact "not all
	// and" idiom and let any other "not" fall through to match; a page's
	// `@media not screen` block then applied on screen, the reverse of what
	// it says.
	if mediaMatches("not screen", 1024) {
		t.Error(`"not screen" must NOT match on screen`)
	}
	if !mediaMatchesOn("not screen", Media{Type: Print, Width: 1024}) {
		t.Error(`"not screen" must match on print`)
	}
}

// TestMediaMatchesSimpleCalc covers GitHub's exact pattern: a "calc(A - B)"
// media-feature value opening a hair's-width gap below the next breakpoint up,
// so two adjacent responsive ranges never both match the same viewport width.
func TestMediaMatchesSimpleCalc(t *testing.T) {
	if !mediaMatches("(width<=calc(48rem - .02px))", 767) {
		t.Error("width<=calc(48rem - .02px) (~767.98px) should match at 767px")
	}
	if mediaMatches("(width<=calc(48rem - .02px))", 1024) {
		t.Error("width<=calc(48rem - .02px) should NOT match at 1024px")
	}
	if !mediaMatches("(width<=calc(1000px + 24px))", 1024) {
		t.Error("width<=calc(1000px + 24px) (1024px) should match at exactly 1024px")
	}
	if mediaMatches("(width<=calc(1000px + 24px))", 1025) {
		t.Error("width<=calc(1000px + 24px) should NOT match at 1025px")
	}
	// evalMediaCalcs (calc.go's general evaluator, shared with ordinary
	// property values) resolves a calc() with more than two terms and a
	// multiplication by a bare scalar just as well as a plain two-term one —
	// MDN's real reference-layout breakpoint is exactly this shape
	// (`calc(1rem * 2 + 15rem + 2rem + 31rem)`, five terms including one
	// product): a two-term-only evaluator left it unparsed, silently making a
	// MOBILE-ONLY breakpoint match at every viewport (see FIDELITY.md).
	if mediaMatches("(width<=calc(1px + 2px + 3px))", 999999) {
		t.Error("calc(1px + 2px + 3px) = 6px: width<=6px should NOT match at 999999px")
	}
	if !mediaMatches("(width<=calc(1px + 2px + 3px))", 6) {
		t.Error("calc(1px + 2px + 3px) = 6px: width<=6px should match at exactly 6px")
	}
	if !mediaMatches("(width<calc(1rem * 2 + 15rem + 2rem + 31rem))", 799) {
		t.Error("MDN's real breakpoint (800px): width<800px should match at 799px")
	}
	if mediaMatches("(width<calc(1rem * 2 + 15rem + 2rem + 31rem))", 1024) {
		t.Error("MDN's real breakpoint (800px): width<800px should NOT match at 1024px — the bug this covers")
	}
	// A construct genuinely outside what evalCalcExpr resolves (a percentage,
	// which needs layout-time context this text-level pass never has) is
	// left unparsed: the condition finds no width feature and matches
	// optimistically, same as any other value this matcher cannot handle.
	if !mediaMatches("(width<=calc(50% + 24px))", 999999) {
		t.Error("a calc() containing a percentage should fall through to match optimistically")
	}
	// A value the regex's digit/dot character class accepts but strconv
	// rejects (multiple dots, no actual digits) must not panic: the condition
	// is simply left unevaluated (matches optimistically), same as any other
	// unparseable value.
	if !mediaMatches("(width<=...px)", 1024) {
		t.Error("an unparseable numeric value should fall through to match optimistically")
	}
	// An unterminated "calc(" (no matching close paren anywhere) must not
	// panic or hang: evalMediaCalcs leaves the rest of the string untouched.
	if !mediaMatches("(width<=calc(48rem - .02px", 1024) {
		t.Error("an unterminated calc( should fall through to match optimistically")
	}
}

// TestMediaMatchesHoverAndPointer covers the interaction media features
// (hover/any-hover/pointer/any-pointer), which — unlike an ordinary unknown
// feature — must NOT fall through to "match optimistically": this engine
// models exactly one rendering context, a mouse-equipped desktop browser
// (matching the headless Chrome this project measures itself against), so a
// touch-oriented query must evaluate false. Found live on github.com: a
// Markdown heading's permalink icon is `opacity:0` by default (shown on
// hover/focus), with `@media (pointer:coarse){.anchor{opacity:1}}` as its
// touch-device fallback (a device with no fine pointer can never trigger
// :hover, so this makes the icon permanently visible for touch users) — this
// touch-only fallback wrongly matched here, showing the icon on every single
// Markdown heading unconditionally, and a whole family of near-identical
// query pairs (`hover:none`/`any-hover:none`) carries the exact same risk.
func TestMediaMatchesHoverAndPointer(t *testing.T) {
	if mediaMatches("(hover:none)", 1024) {
		t.Error("(hover:none) should NOT match a mouse-equipped desktop context")
	}
	if !mediaMatches("(hover:hover)", 1024) {
		t.Error("(hover:hover) SHOULD match a mouse-equipped desktop context")
	}
	if mediaMatches("(any-hover:none)", 1024) {
		t.Error("(any-hover:none) should NOT match")
	}
	if !mediaMatches("(any-hover:hover)", 1024) {
		t.Error("(any-hover:hover) SHOULD match")
	}
	if mediaMatches("(pointer:coarse)", 1024) {
		t.Error("(pointer:coarse) — GitHub's real touch-fallback query — should NOT match")
	}
	if mediaMatches("(pointer:none)", 1024) {
		t.Error("(pointer:none) should NOT match")
	}
	if !mediaMatches("(pointer:fine)", 1024) {
		t.Error("(pointer:fine) SHOULD match")
	}
	if mediaMatches("(any-pointer:coarse)", 1024) {
		t.Error("(any-pointer:coarse) should NOT match")
	}
	if !mediaMatches("(any-pointer:fine)", 1024) {
		t.Error("(any-pointer:fine) SHOULD match")
	}
	// A genuinely unrelated/unknown feature still matches optimistically,
	// unaffected by this — only hover/pointer get the stricter treatment.
	if !mediaMatches("(color-gamut:p3)", 1024) {
		t.Error("an unrelated unknown feature should still match optimistically")
	}
}

// TestFlipCmp covers every operator flipCmp reverses, plus the defensive
// default (an operator outside the four mediaWidthCmpRe can ever capture).
func TestFlipCmp(t *testing.T) {
	cases := map[string]string{"<": ">", "<=": ">=", ">": "<", ">=": "<=", "?": "?"}
	for in, want := range cases {
		if got := flipCmp(in); got != want {
			t.Errorf("flipCmp(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseStylesheetEmptyAndNoBrace(t *testing.T) {
	if r := ParseStylesheet(""); r != nil {
		t.Errorf("empty = %v", r)
	}
	if r := ParseStylesheet("p color red"); r != nil {
		t.Errorf("no-brace = %v", r)
	}
	// A rule whose block has no valid declarations is dropped.
	if r := ParseStylesheet("p { }"); r != nil {
		t.Errorf("empty block = %v", r)
	}
}

func TestApplyProperties(t *testing.T) {
	s := initialStyle()
	apply := func(p, v string, em float64) { s.apply(Declaration{Property: p, Value: v}, em, nil) }
	apply("display", "none", 16)
	if s.Display != DisplayNone {
		t.Error("display none")
	}
	apply("display", "flex", 16)
	if s.Display != DisplayFlex {
		t.Error("display flex")
	}
	apply("display", "inline-block", 16)
	if s.Display != DisplayInlineBlock {
		t.Error("display inline-block")
	}
	apply("display", "table", 16)
	if s.Display != DisplayTable {
		t.Error("display table")
	}
	apply("display", "none", 16)
	apply("display", "initial", 16)
	if s.Display != DisplayInline {
		t.Errorf("display initial = %v, want DisplayInline (its real spec initial value)", s.Display)
	}
	apply("display", "none", 16)
	apply("display", "unset", 16)
	if s.Display != DisplayInline {
		t.Errorf("display unset = %v, want DisplayInline (display is not inherited, so unset = initial)", s.Display)
	}
	apply("display", "block", 16)
	apply("background", "  #fff other", 16)
	if s.Background != (Color{255, 255, 255, 255}) {
		t.Errorf("background = %v", s.Background)
	}
	apply("font-size", "150%", 16)
	if s.FontSize != 24 {
		t.Errorf("font-size %% = %v", s.FontSize)
	}
	apply("font-weight", "bold", 16)
	if s.FontWeight != 700 {
		t.Error("weight bold")
	}
	apply("font-weight", "lighter", 16)
	if s.FontWeight != 400 {
		t.Error("weight lighter")
	}
	apply("font-weight", "600", 16)
	if s.FontWeight != 600 {
		t.Error("weight 600")
	}
	apply("font-style", "italic", 16)
	if !s.Italic {
		t.Error("font-style italic")
	}
	apply("font-style", "oblique", 16)
	if !s.Italic {
		t.Error("font-style oblique")
	}
	apply("font-style", "normal", 16)
	if s.Italic {
		t.Error("font-style normal resets italic")
	}
	apply("text-align", "center", 16)
	if s.TextAlign != AlignCenter {
		t.Error("align center")
	}
	apply("text-align", "right", 16)
	if s.TextAlign != AlignRight {
		t.Error("align right")
	}
	apply("white-space", "pre", 16)
	if s.WhiteSpace != WSPre {
		t.Error("white-space pre")
	}
	apply("white-space", "normal", 16)
	if s.WhiteSpace != WSNormal {
		t.Error("white-space normal")
	}
	for _, kw := range []string{"pixelated", "crisp-edges", "-webkit-optimize-contrast"} {
		s.ImageRendering = IRAuto
		apply("image-rendering", kw, 16)
		if s.ImageRendering != IRPixelated {
			t.Errorf("image-rendering %q should be pixelated", kw)
		}
	}
	for _, kw := range []string{"auto", "smooth", "high-quality", "optimizeQuality"} {
		s.ImageRendering = IRPixelated
		apply("image-rendering", kw, 16)
		if s.ImageRendering != IRAuto {
			t.Errorf("image-rendering %q should be auto", kw)
		}
	}
	// An unrecognised keyword leaves the value untouched.
	s.ImageRendering = IRPixelated
	apply("image-rendering", "bogus", 16)
	if s.ImageRendering != IRPixelated {
		t.Error("unknown image-rendering keyword should be ignored")
	}
	apply("width", "50%", 16)
	if !s.Width.IsPercent || s.Width.Percent != 0.5 {
		t.Errorf("width = %v", s.Width)
	}
	apply("margin", "3px", 16)
	if s.Margin != (Edges{3, 3, 3, 3}) {
		t.Errorf("margin = %v", s.Margin)
	}
	apply("margin-top", "7px", 16)
	apply("margin-right", "8px", 16)
	apply("margin-bottom", "9px", 16)
	apply("margin-left", "10px", 16)
	if s.Margin != (Edges{7, 8, 9, 10}) {
		t.Errorf("margin longhand = %v", s.Margin)
	}
	apply("padding", "2px", 16)
	apply("padding-top", "1px", 16)
	apply("padding-right", "2px", 16)
	apply("padding-bottom", "3px", 16)
	apply("padding-left", "4px", 16)
	if s.Padding != (Edges{1, 2, 3, 4}) {
		t.Errorf("padding longhand = %v", s.Padding)
	}
	apply("color", "notacolor", 16) // invalid ignored
	apply("unknown-prop", "x", 16)  // unknown ignored
}

// TestApplyAspectRatio covers `aspect-ratio`'s own parseAspectRatio grammar:
// a bare number, a "<W>/<H>" ratio, "auto" and any unparseable value all
// resetting AspectRatio to 0 (see css.Style.AspectRatio's own doc comment for
// the confirmed real need — round 86).
func TestApplyAspectRatio(t *testing.T) {
	s := initialStyle()
	apply := func(v string) { s.apply(Declaration{Property: "aspect-ratio", Value: v}, 16, nil) }

	apply("16/9")
	if s.AspectRatio != float64(16)/9 {
		t.Errorf("aspect-ratio 16/9 = %v, want %v", s.AspectRatio, float64(16)/9)
	}
	apply("1.5")
	if s.AspectRatio != 1.5 {
		t.Errorf("aspect-ratio bare number = %v, want 1.5", s.AspectRatio)
	}
	apply("auto")
	if s.AspectRatio != 0 {
		t.Errorf("aspect-ratio auto = %v, want 0 (no ratio)", s.AspectRatio)
	}
	// Re-set a real ratio, then confirm each unparseable form resets to 0
	// rather than leaving the PREVIOUS ratio in effect.
	for _, bad := range []string{"not-a-ratio", "16/0", "0/9", "-1", "16/"} {
		apply("2/1")
		if s.AspectRatio == 0 {
			t.Fatalf("setup: aspect-ratio 2/1 should have set a non-zero ratio")
		}
		apply(bad)
		if s.AspectRatio != 0 {
			t.Errorf("aspect-ratio %q = %v, want 0 (unparseable, must not keep the prior ratio)", bad, s.AspectRatio)
		}
	}
}

// TestApplyObjectFit covers the confirmed real-world values (round 89):
// `cover` (react.dev's/tailwindcss.com's own `object-cover` utility) and its
// sibling `contain`; every other/unrecognised keyword — including the
// initial `fill` — must resolve to ObjectFitFill, this engine's own prior
// (and still correct) unconditional-stretch behaviour.
func TestApplyObjectFit(t *testing.T) {
	s := initialStyle()
	apply := func(v string) { s.apply(Declaration{Property: "object-fit", Value: v}, 16, nil) }

	apply("cover")
	if s.ObjectFit != ObjectFitCover {
		t.Errorf("object-fit:cover = %v, want ObjectFitCover", s.ObjectFit)
	}
	apply("contain")
	if s.ObjectFit != ObjectFitContain {
		t.Errorf("object-fit:contain = %v, want ObjectFitContain", s.ObjectFit)
	}
	apply("fill")
	if s.ObjectFit != ObjectFitFill {
		t.Errorf("object-fit:fill = %v, want ObjectFitFill", s.ObjectFit)
	}
	// Re-set to a non-fill value, then confirm an unrecognised keyword resets
	// to fill rather than leaving the PREVIOUS value in effect.
	apply("cover")
	apply("scale-down")
	if s.ObjectFit != ObjectFitFill {
		t.Errorf("object-fit:scale-down (unrecognised) = %v, want ObjectFitFill, not the prior cover", s.ObjectFit)
	}
}

// TestApplyTabSize is the confirmed real-world regression (round 91):
// pkg.go.dev's own `pre,textarea.code{tab-size:4}` on its real, tab-indented
// Go source samples. tab-size is INHERITED (CSS Text 3), initial 8.
func TestApplyTabSize(t *testing.T) {
	if got := initialStyle().TabSize; got != 8 {
		t.Errorf("initialStyle().TabSize = %d, want 8 (CSS Text 3 initial value)", got)
	}

	s := initialStyle()
	apply := func(v string) { s.apply(Declaration{Property: "tab-size", Value: v}, 16, nil) }

	apply("4")
	if s.TabSize != 4 {
		t.Errorf("tab-size:4 = %d, want 4", s.TabSize)
	}
	apply("0")
	if s.TabSize != 0 {
		t.Errorf("tab-size:0 = %d, want 0 (a valid value: no tabs rendered)", s.TabSize)
	}
	// Re-set to a real value, then confirm each invalid form leaves it
	// UNCHANGED (0 is itself a valid, meaningful value, so an unparseable
	// declaration must not silently reset to it).
	for _, bad := range []string{"-1", "not-a-number", "1.5"} {
		apply("4")
		apply(bad)
		if s.TabSize != 4 {
			t.Errorf("tab-size:%q (invalid) = %d, want 4 (unchanged, not silently reset)", bad, s.TabSize)
		}
	}
	apply("initial")
	if s.TabSize != 8 {
		t.Errorf("tab-size:initial = %d, want 8", s.TabSize)
	}

	// unset on an inherited property re-inherits the parent's value.
	parent := initialStyle()
	parent.TabSize = 2
	s2 := initialStyle()
	s2.TabSize = 4
	s2.apply(Declaration{Property: "tab-size", Value: "unset"}, 16, &parent)
	if s2.TabSize != 2 {
		t.Errorf("tab-size:unset = %d, want 2 (inherited from parent)", s2.TabSize)
	}
}

func TestApplyTextWrapBalance(t *testing.T) {
	if initialStyle().TextWrapBalance {
		t.Error("initialStyle().TextWrapBalance = true, want false (initial value is auto)")
	}

	s := initialStyle()
	apply := func(prop, v string) { s.apply(Declaration{Property: prop, Value: v}, 16, nil) }

	apply("text-wrap", "balance")
	if !s.TextWrapBalance {
		t.Error("text-wrap:balance left TextWrapBalance false")
	}
	apply("text-wrap", "wrap")
	if s.TextWrapBalance {
		t.Error("text-wrap:wrap left TextWrapBalance true")
	}
	apply("text-wrap-style", "balance")
	if !s.TextWrapBalance {
		t.Error("text-wrap-style:balance (longhand) left TextWrapBalance false")
	}
	// The shorthand's grammar allows either order and either half omitted —
	// "wrap balance" should still select balance via the mode/style split.
	apply("text-wrap", "wrap")
	apply("text-wrap", "wrap balance")
	if !s.TextWrapBalance {
		t.Error(`text-wrap:"wrap balance" left TextWrapBalance false`)
	}
	// A recognised-but-not-modelled keyword (pretty/stable/avoid-short-last-
	// line/nowrap) is valid CSS, just resolves to false, same as auto.
	for _, v := range []string{"pretty", "stable", "avoid-short-last-line", "nowrap"} {
		apply("text-wrap", "balance")
		apply("text-wrap", v)
		if s.TextWrapBalance {
			t.Errorf("text-wrap:%s left TextWrapBalance true", v)
		}
	}
	// An unrecognised token is invalid CSS and must leave the value UNCHANGED,
	// not silently reset to false.
	apply("text-wrap", "balance")
	apply("text-wrap", "not-a-real-value")
	if !s.TextWrapBalance {
		t.Error("text-wrap:not-a-real-value (invalid) reset TextWrapBalance, want unchanged")
	}

	// unset on an inherited property re-inherits the parent's value.
	parent := initialStyle()
	parent.TextWrapBalance = true
	s2 := initialStyle()
	s2.TextWrapBalance = false
	s2.apply(Declaration{Property: "text-wrap", Value: "unset"}, 16, &parent)
	if !s2.TextWrapBalance {
		t.Error("text-wrap:unset did not inherit true from parent")
	}
	s3 := initialStyle()
	s3.TextWrapBalance = false
	s3.apply(Declaration{Property: "text-wrap-style", Value: "unset"}, 16, &parent)
	if !s3.TextWrapBalance {
		t.Error("text-wrap-style:unset (longhand) did not inherit true from parent")
	}
}

func TestApplyTextWrapNowrap(t *testing.T) {
	if initialStyle().TextWrapNowrap {
		t.Error("initialStyle().TextWrapNowrap = true, want false (initial value is wrap)")
	}

	s := initialStyle()
	apply := func(prop, v string) { s.apply(Declaration{Property: prop, Value: v}, 16, nil) }

	apply("text-wrap", "nowrap")
	if !s.TextWrapNowrap {
		t.Error("text-wrap:nowrap left TextWrapNowrap false")
	}
	apply("text-wrap", "wrap")
	if s.TextWrapNowrap {
		t.Error("text-wrap:wrap left TextWrapNowrap true")
	}
	apply("text-wrap-mode", "nowrap")
	if !s.TextWrapNowrap {
		t.Error("text-wrap-mode:nowrap (longhand) left TextWrapNowrap false")
	}

	// The shorthand resets EACH axis to its own initial value when the other
	// keyword is present — "balance" alone must reset text-wrap-mode back to
	// wrap even if nowrap was previously set, and vice versa.
	apply("text-wrap", "nowrap")
	apply("text-wrap", "balance")
	if s.TextWrapNowrap {
		t.Error(`text-wrap:"balance" left TextWrapNowrap true (shorthand must reset the omitted axis)`)
	}
	if !s.TextWrapBalance {
		t.Error(`text-wrap:"balance" left TextWrapBalance false`)
	}
	// Both axes together, either order.
	apply("text-wrap", "wrap")
	apply("text-wrap", "nowrap balance")
	if !s.TextWrapNowrap || !s.TextWrapBalance {
		t.Errorf(`text-wrap:"nowrap balance" = nowrap=%v balance=%v, want both true`, s.TextWrapNowrap, s.TextWrapBalance)
	}
	apply("text-wrap", "wrap")
	apply("text-wrap", "balance nowrap")
	if !s.TextWrapNowrap || !s.TextWrapBalance {
		t.Errorf(`text-wrap:"balance nowrap" = nowrap=%v balance=%v, want both true`, s.TextWrapNowrap, s.TextWrapBalance)
	}

	// An unrecognised token is invalid CSS and must leave the value UNCHANGED.
	apply("text-wrap", "nowrap")
	apply("text-wrap", "not-a-real-value")
	if !s.TextWrapNowrap {
		t.Error("text-wrap:not-a-real-value (invalid) reset TextWrapNowrap, want unchanged")
	}

	// unset on an inherited property re-inherits the parent's value.
	parent := initialStyle()
	parent.TextWrapNowrap = true
	s2 := initialStyle()
	s2.TextWrapNowrap = false
	s2.apply(Declaration{Property: "text-wrap-mode", Value: "unset"}, 16, &parent)
	if !s2.TextWrapNowrap {
		t.Error("text-wrap-mode:unset did not inherit true from parent")
	}
}

func TestApplyLineClamp(t *testing.T) {
	if initialStyle().LineClamp != 0 {
		t.Error("initialStyle().LineClamp != 0, want 0 (unclamped)")
	}

	s := initialStyle()
	apply := func(prop, v string) { s.apply(Declaration{Property: prop, Value: v}, 16, nil) }

	apply("-webkit-line-clamp", "2")
	if s.LineClamp != 2 {
		t.Errorf("-webkit-line-clamp:2 = %d, want 2", s.LineClamp)
	}
	apply("line-clamp", "3")
	if s.LineClamp != 3 {
		t.Errorf("line-clamp:3 (standard spelling) = %d, want 3", s.LineClamp)
	}
	apply("-webkit-line-clamp", "none")
	if s.LineClamp != 0 {
		t.Errorf("-webkit-line-clamp:none = %d, want 0", s.LineClamp)
	}

	// Zero, negative and non-numeric values are invalid and must leave the
	// property UNCHANGED, matching text-wrap's own invalid-value handling.
	apply("-webkit-line-clamp", "2")
	for _, bad := range []string{"0", "-1", "auto", "2.5"} {
		apply("-webkit-line-clamp", bad)
		if s.LineClamp != 2 {
			t.Errorf("-webkit-line-clamp:%q (invalid) = %d, want unchanged 2", bad, s.LineClamp)
		}
	}

	// Not inherited: a child must NOT pick up a parent's clamp via "unset".
	parent := initialStyle()
	parent.LineClamp = 4
	s2 := initialStyle()
	s2.apply(Declaration{Property: "line-clamp", Value: "unset"}, 16, &parent)
	if s2.LineClamp != 0 {
		t.Errorf("line-clamp:unset on a non-inherited property = %d, want 0 (never inherit)", s2.LineClamp)
	}
}

func TestApplyVerticalAlign(t *testing.T) {
	if initialStyle().VerticalAlign != VAlignBaseline {
		t.Error("initialStyle().VerticalAlign != VAlignBaseline")
	}

	s := initialStyle()
	apply := func(v string) { s.apply(Declaration{Property: "vertical-align", Value: v}, 16, nil) }

	cases := []struct {
		value string
		want  VerticalAlign
	}{
		{"baseline", VAlignBaseline},
		{"top", VAlignTop},
		{"bottom", VAlignBottom},
		{"text-top", VAlignTextTop},
		{"text-bottom", VAlignTextBottom},
		{"middle", VAlignMiddle},
		{"sub", VAlignSub},
		{"super", VAlignSuper},
		{"initial", VAlignBaseline},
		{"unset", VAlignBaseline},
	}
	for _, c := range cases {
		apply(c.value)
		if s.VerticalAlign != c.want {
			t.Errorf("vertical-align:%s = %v, want %v", c.value, s.VerticalAlign, c.want)
		}
	}

	// An unrecognised value leaves the property unchanged.
	apply("text-bottom")
	apply("not-a-real-value")
	if s.VerticalAlign != VAlignTextBottom {
		t.Errorf("vertical-align:not-a-real-value changed the property to %v, want unchanged VAlignTextBottom", s.VerticalAlign)
	}

	// Not inherited: a child must NOT pick up a parent's alignment via "unset".
	parent := initialStyle()
	parent.VerticalAlign = VAlignMiddle
	s2 := initialStyle()
	s2.VerticalAlign = VAlignTextBottom
	s2.apply(Declaration{Property: "vertical-align", Value: "unset"}, 16, &parent)
	if s2.VerticalAlign != VAlignBaseline {
		t.Errorf("vertical-align:unset on a non-inherited property = %v, want VAlignBaseline (never inherit)", s2.VerticalAlign)
	}
}

func TestApplyTextOverflow(t *testing.T) {
	if initialStyle().TextOverflowEllipsis {
		t.Error("initialStyle().TextOverflowEllipsis = true, want false (clip)")
	}

	s := initialStyle()
	apply := func(v string) { s.apply(Declaration{Property: "text-overflow", Value: v}, 16, nil) }

	apply("ellipsis")
	if !s.TextOverflowEllipsis {
		t.Error("text-overflow:ellipsis left TextOverflowEllipsis false")
	}
	apply("clip")
	if s.TextOverflowEllipsis {
		t.Error("text-overflow:clip left TextOverflowEllipsis true")
	}
	apply("ellipsis")
	apply("initial")
	if s.TextOverflowEllipsis {
		t.Error("text-overflow:initial left TextOverflowEllipsis true, want false (clip)")
	}

	// An unrecognised value (e.g. a custom replacement string, not modelled)
	// leaves the property unchanged.
	apply("ellipsis")
	apply(`"---"`)
	if !s.TextOverflowEllipsis {
		t.Error(`text-overflow:"---" changed the property, want unchanged true`)
	}

	// Not inherited: a child must NOT pick up a parent's value via "unset".
	parent := initialStyle()
	parent.TextOverflowEllipsis = true
	s2 := initialStyle()
	s2.TextOverflowEllipsis = true
	s2.apply(Declaration{Property: "text-overflow", Value: "unset"}, 16, &parent)
	if s2.TextOverflowEllipsis {
		t.Error("text-overflow:unset on a non-inherited property = true, want false (never inherit)")
	}
}

func TestApplyLetterSpacing(t *testing.T) {
	if initialStyle().LetterSpacing != 0 {
		t.Error("initialStyle().LetterSpacing != 0, want 0 (normal)")
	}

	s := initialStyle()
	apply := func(v string) { s.apply(Declaration{Property: "letter-spacing", Value: v}, 16, nil) }

	apply("2px")
	if s.LetterSpacing != 2 {
		t.Errorf("letter-spacing:2px = %v, want 2", s.LetterSpacing)
	}
	apply("-0.025em")
	if want := -0.025 * 16; s.LetterSpacing != want {
		t.Errorf("letter-spacing:-0.025em = %v, want %v (resolved against a 16px font)", s.LetterSpacing, want)
	}
	apply("normal")
	if s.LetterSpacing != 0 {
		t.Errorf("letter-spacing:normal = %v, want 0", s.LetterSpacing)
	}

	// An invalid value (a bare unitless number, not valid CSS for this
	// property) leaves it unchanged.
	apply("3px")
	apply("5")
	if s.LetterSpacing != 3 {
		t.Errorf("letter-spacing:5 (invalid, no unit) changed the property to %v, want unchanged 3", s.LetterSpacing)
	}

	// Inherited (unlike vertical-align/text-overflow/line-clamp above): a
	// child with no OWN declaration picks up its parent's value through the
	// cascade's default inheritance, not just the explicit "inherit" keyword.
	parent := initialStyle()
	parent.LetterSpacing = 4
	child := inheritFrom(parent)
	if child.LetterSpacing != 4 {
		t.Errorf("inheritFrom(parent).LetterSpacing = %v, want inherited 4", child.LetterSpacing)
	}

	// The explicit "inherit" keyword also works (inheritProperty).
	s3 := initialStyle()
	s3.apply(Declaration{Property: "letter-spacing", Value: "inherit"}, 16, &parent)
	if s3.LetterSpacing != 4 {
		t.Errorf("letter-spacing:inherit = %v, want parent's 4", s3.LetterSpacing)
	}
}

func TestApplyBorderSpacing(t *testing.T) {
	if h, v := initialStyle().BorderSpacingH, initialStyle().BorderSpacingV; h != 0 || v != 0 {
		t.Errorf("initialStyle().BorderSpacing = (%v, %v), want (0, 0)", h, v)
	}

	s := initialStyle()
	apply := func(v string) { s.apply(Declaration{Property: "border-spacing", Value: v}, 16, nil) }

	// One value sets BOTH axes.
	apply("3px")
	if s.BorderSpacingH != 3 || s.BorderSpacingV != 3 {
		t.Errorf("border-spacing:3px = (%v, %v), want (3, 3)", s.BorderSpacingH, s.BorderSpacingV)
	}
	// Two values: horizontal then vertical.
	apply("2px 5px")
	if s.BorderSpacingH != 2 || s.BorderSpacingV != 5 {
		t.Errorf("border-spacing:2px 5px = (%v, %v), want (2, 5)", s.BorderSpacingH, s.BorderSpacingV)
	}
	// em resolves against the font size passed to apply.
	apply("1em")
	if s.BorderSpacingH != 16 || s.BorderSpacingV != 16 {
		t.Errorf("border-spacing:1em (16px font) = (%v, %v), want (16, 16)", s.BorderSpacingH, s.BorderSpacingV)
	}

	// An invalid value (three lengths, not valid CSS for this property)
	// leaves it unchanged.
	apply("3px")
	apply("1px 2px 3px")
	if s.BorderSpacingH != 3 || s.BorderSpacingV != 3 {
		t.Errorf("border-spacing:1px 2px 3px (invalid) changed the property to (%v, %v), want unchanged (3, 3)", s.BorderSpacingH, s.BorderSpacingV)
	}

	// Inherited, per spec: a plain default-inheritance cascade picks it up.
	parent := initialStyle()
	parent.BorderSpacingH, parent.BorderSpacingV = 4, 7
	child := inheritFrom(parent)
	if child.BorderSpacingH != 4 || child.BorderSpacingV != 7 {
		t.Errorf("inheritFrom(parent).BorderSpacing = (%v, %v), want inherited (4, 7)", child.BorderSpacingH, child.BorderSpacingV)
	}

	// The explicit "inherit" keyword also works.
	s2 := initialStyle()
	s2.apply(Declaration{Property: "border-spacing", Value: "inherit"}, 16, &parent)
	if s2.BorderSpacingH != 4 || s2.BorderSpacingV != 7 {
		t.Errorf("border-spacing:inherit = (%v, %v), want parent's (4, 7)", s2.BorderSpacingH, s2.BorderSpacingV)
	}
}

func TestApplyWordBreak(t *testing.T) {
	s := initialStyle()
	apply := func(v string) { s.apply(Declaration{Property: "word-break", Value: v}, 16, nil) }

	if s.WordBreakAll {
		t.Error("initialStyle().WordBreakAll = true, want false (initial value is normal)")
	}
	apply("break-all")
	if !s.WordBreakAll {
		t.Error("word-break:break-all left WordBreakAll false")
	}
	apply("normal")
	if s.WordBreakAll {
		t.Error("word-break:normal left WordBreakAll true")
	}
	apply("break-all")
	apply("keep-all")
	if s.WordBreakAll {
		t.Error("word-break:keep-all left WordBreakAll true (not modelled, but must still reset like normal)")
	}
	// The deprecated break-word alias sets OverflowWrapAnywhere, not
	// WordBreakAll — see OverflowWrapAnywhere's own doc comment.
	apply("break-all")
	apply("break-word")
	if s.WordBreakAll {
		t.Error("word-break:break-word left WordBreakAll true, want false (it is overflow-wrap's alias, not break-all's)")
	}
	if !s.OverflowWrapAnywhere {
		t.Error("word-break:break-word did not set OverflowWrapAnywhere")
	}

	// An unrecognised token is invalid CSS and must leave the value UNCHANGED.
	s.WordBreakAll = true
	apply("not-a-real-value")
	if !s.WordBreakAll {
		t.Error("word-break:not-a-real-value (invalid) reset WordBreakAll, want unchanged")
	}

	// Inherited, per spec.
	parent := initialStyle()
	parent.WordBreakAll = true
	child := inheritFrom(parent)
	if !child.WordBreakAll {
		t.Error("inheritFrom(parent).WordBreakAll = false, want inherited true")
	}

	// unset re-inherits the parent's value; so does the explicit inherit keyword.
	s2 := initialStyle()
	s2.apply(Declaration{Property: "word-break", Value: "unset"}, 16, &parent)
	if !s2.WordBreakAll {
		t.Error("word-break:unset did not inherit true from parent")
	}
	s3 := initialStyle()
	s3.apply(Declaration{Property: "word-break", Value: "inherit"}, 16, &parent)
	if !s3.WordBreakAll {
		t.Error("word-break:inherit did not inherit true from parent")
	}
}

func TestApplyOverflowWrap(t *testing.T) {
	s := initialStyle()
	apply := func(prop, v string) { s.apply(Declaration{Property: prop, Value: v}, 16, nil) }

	if s.OverflowWrapAnywhere {
		t.Error("initialStyle().OverflowWrapAnywhere = true, want false (initial value is normal)")
	}
	apply("overflow-wrap", "break-word")
	if !s.OverflowWrapAnywhere {
		t.Error("overflow-wrap:break-word left OverflowWrapAnywhere false")
	}
	apply("overflow-wrap", "normal")
	if s.OverflowWrapAnywhere {
		t.Error("overflow-wrap:normal left OverflowWrapAnywhere true")
	}
	apply("overflow-wrap", "anywhere")
	if !s.OverflowWrapAnywhere {
		t.Error("overflow-wrap:anywhere left OverflowWrapAnywhere false")
	}

	// word-wrap is the legacy alias for the exact same property.
	s.OverflowWrapAnywhere = false
	apply("word-wrap", "break-word")
	if !s.OverflowWrapAnywhere {
		t.Error("word-wrap:break-word (legacy alias) left OverflowWrapAnywhere false")
	}

	// An unrecognised token is invalid CSS and must leave the value UNCHANGED.
	apply("overflow-wrap", "not-a-real-value")
	if !s.OverflowWrapAnywhere {
		t.Error("overflow-wrap:not-a-real-value (invalid) reset OverflowWrapAnywhere, want unchanged")
	}

	// Inherited, per spec.
	parent := initialStyle()
	parent.OverflowWrapAnywhere = true
	child := inheritFrom(parent)
	if !child.OverflowWrapAnywhere {
		t.Error("inheritFrom(parent).OverflowWrapAnywhere = false, want inherited true")
	}

	// unset re-inherits the parent's value; so does the explicit inherit
	// keyword, through either property name.
	s2 := initialStyle()
	s2.apply(Declaration{Property: "overflow-wrap", Value: "unset"}, 16, &parent)
	if !s2.OverflowWrapAnywhere {
		t.Error("overflow-wrap:unset did not inherit true from parent")
	}
	s3 := initialStyle()
	s3.apply(Declaration{Property: "word-wrap", Value: "inherit"}, 16, &parent)
	if !s3.OverflowWrapAnywhere {
		t.Error("word-wrap:inherit did not inherit true from parent")
	}
}

func TestBreaksOverlongWords(t *testing.T) {
	s := initialStyle()
	if s.BreaksOverlongWords() {
		t.Error("initialStyle().BreaksOverlongWords() = true, want false")
	}
	s.WordBreakAll = true
	if !s.BreaksOverlongWords() {
		t.Error("WordBreakAll alone did not make BreaksOverlongWords true")
	}
	s.WordBreakAll, s.OverflowWrapAnywhere = false, true
	if !s.BreaksOverlongWords() {
		t.Error("OverflowWrapAnywhere alone did not make BreaksOverlongWords true")
	}
}

func TestApplyColumnCount(t *testing.T) {
	s := initialStyle()
	apply := func(v string) { s.apply(Declaration{Property: "column-count", Value: v}, 16, nil) }

	if s.ColumnCount != 0 {
		t.Error("initialStyle().ColumnCount != 0, want 0 (not set)")
	}
	apply("5")
	if s.ColumnCount != 5 {
		t.Errorf("column-count:5 = %d, want 5", s.ColumnCount)
	}
	apply("auto")
	if s.ColumnCount != 0 {
		t.Errorf("column-count:auto = %d, want 0", s.ColumnCount)
	}
	apply("5")
	apply("0")
	if s.ColumnCount != 5 {
		t.Errorf("column-count:0 (invalid, not positive) changed ColumnCount to %d, want unchanged 5", s.ColumnCount)
	}
	apply("-1")
	if s.ColumnCount != 5 {
		t.Errorf("column-count:-1 (invalid) changed ColumnCount to %d, want unchanged 5", s.ColumnCount)
	}
	apply("not-a-number")
	if s.ColumnCount != 5 {
		t.Errorf("column-count:not-a-number (invalid) changed ColumnCount to %d, want unchanged 5", s.ColumnCount)
	}

	// Not inherited: a plain cascade resets to 0, but the explicit inherit
	// keyword still copies the parent's computed value.
	parent := initialStyle()
	parent.ColumnCount = 3
	child := inheritFrom(parent)
	if child.ColumnCount != 0 {
		t.Errorf("inheritFrom(parent).ColumnCount = %d, want 0 (not inherited)", child.ColumnCount)
	}
	s2 := initialStyle()
	s2.apply(Declaration{Property: "column-count", Value: "inherit"}, 16, &parent)
	if s2.ColumnCount != 3 {
		t.Errorf("column-count:inherit = %d, want parent's 3", s2.ColumnCount)
	}
	s3 := initialStyle()
	s3.ColumnCount = 7
	s3.apply(Declaration{Property: "column-count", Value: "unset"}, 16, &parent)
	if s3.ColumnCount != 0 {
		t.Errorf("column-count:unset = %d, want 0 (initial, not inherited)", s3.ColumnCount)
	}
}

func TestApplyColumnWidth(t *testing.T) {
	s := initialStyle()
	apply := func(v string) { s.apply(Declaration{Property: "column-width", Value: v}, 16, nil) }

	if !s.ColumnWidth.Auto {
		t.Error("initialStyle().ColumnWidth.Auto = false, want true (not set)")
	}
	apply("12.5rem")
	if s.ColumnWidth.Auto || s.ColumnWidth.Px != 200 {
		t.Errorf("column-width:12.5rem = %+v, want 200px", s.ColumnWidth)
	}
	apply("auto")
	if !s.ColumnWidth.Auto {
		t.Error("column-width:auto did not reset to Auto")
	}

	parent := initialStyle()
	parent.ColumnWidth = Length{Px: 150}
	child := inheritFrom(parent)
	if !child.ColumnWidth.Auto {
		t.Error("inheritFrom(parent).ColumnWidth not reset to Auto (not inherited)")
	}
	s2 := initialStyle()
	s2.apply(Declaration{Property: "column-width", Value: "inherit"}, 16, &parent)
	if s2.ColumnWidth.Auto || s2.ColumnWidth.Px != 150 {
		t.Errorf("column-width:inherit = %+v, want parent's 150px", s2.ColumnWidth)
	}
}

func TestApplyColumnsShorthand(t *testing.T) {
	s := initialStyle()
	apply := func(v string) { s.apply(Declaration{Property: "columns", Value: v}, 16, nil) }

	apply("5")
	if s.ColumnCount != 5 || !s.ColumnWidth.Auto {
		t.Errorf("columns:5 = count %d width %+v, want count 5, width auto", s.ColumnCount, s.ColumnWidth)
	}

	s = initialStyle()
	apply("12.5rem")
	if s.ColumnCount != 0 || s.ColumnWidth.Px != 200 {
		t.Errorf("columns:12.5rem = count %d width %+v, want count 0, width 200px", s.ColumnCount, s.ColumnWidth)
	}

	// pkg.go.dev's own real trigger, width-then-count order.
	s = initialStyle()
	apply("12.5rem 5")
	if s.ColumnCount != 5 || s.ColumnWidth.Px != 200 {
		t.Errorf("columns:12.5rem 5 = count %d width %+v, want count 5, width 200px", s.ColumnCount, s.ColumnWidth)
	}

	// The || combinator: same result with the two tokens swapped.
	s = initialStyle()
	apply("5 12.5rem")
	if s.ColumnCount != 5 || s.ColumnWidth.Px != 200 {
		t.Errorf("columns:5 12.5rem = count %d width %+v, want count 5, width 200px", s.ColumnCount, s.ColumnWidth)
	}

	s = initialStyle()
	s.ColumnCount, s.ColumnWidth = 5, Length{Px: 200}
	apply("auto")
	if s.ColumnCount != 0 || !s.ColumnWidth.Auto {
		t.Errorf("columns:auto = count %d width %+v, want both reset to auto/0", s.ColumnCount, s.ColumnWidth)
	}

	// Invalid shapes leave both sub-properties unchanged.
	for _, bad := range []string{"5 5", "12.5rem 12.5rem", "5 12.5rem 5", "not-a-value"} {
		s = initialStyle()
		s.ColumnCount, s.ColumnWidth = 3, Length{Px: 100}
		apply(bad)
		if s.ColumnCount != 3 || s.ColumnWidth.Px != 100 {
			t.Errorf("columns:%q (invalid) changed the value to count %d width %+v, want unchanged (3, 100px)", bad, s.ColumnCount, s.ColumnWidth)
		}
	}

	// Not inherited; explicit inherit keyword still copies both from parent.
	parent := initialStyle()
	parent.ColumnCount, parent.ColumnWidth = 4, Length{Px: 80}
	child := inheritFrom(parent)
	if child.ColumnCount != 0 || !child.ColumnWidth.Auto {
		t.Error("inheritFrom(parent) did not reset columns to initial (not inherited)")
	}
	s2 := initialStyle()
	s2.apply(Declaration{Property: "columns", Value: "inherit"}, 16, &parent)
	if s2.ColumnCount != 4 || s2.ColumnWidth.Px != 80 {
		t.Errorf("columns:inherit = count %d width %+v, want parent's (4, 80px)", s2.ColumnCount, s2.ColumnWidth)
	}
}
