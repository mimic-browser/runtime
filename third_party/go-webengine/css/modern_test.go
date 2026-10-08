// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package css

import (
	"testing"

	"github.com/go-webengine/engine/dom"
)

// ---- modern colour syntax ---------------------------------------------------

func TestParseColorModern(t *testing.T) {
	cases := []struct {
		in   string
		want Color
	}{
		// Space-separated rgb() with and without a slash alpha (modern Tailwind).
		{"rgb(22 24 29)", Color{22, 24, 29, 255}},
		{"rgb(22 24 29/1)", Color{22, 24, 29, 255}},
		{"rgb(22 24 29 / 0.5)", Color{22, 24, 29, 128}},
		{"rgb(22 24 29 / 50%)", Color{22, 24, 29, 128}},
		{"rgba(8 126 164 / 1)", Color{8, 126, 164, 255}},
		// Legacy comma syntax still works.
		{"rgb(255, 0, 0)", Color{255, 0, 0, 255}},
		{"rgba(255, 0, 0, 0.5)", Color{255, 0, 0, 128}},
		// Percentage channels.
		{"rgb(100% 0% 0%)", Color{255, 0, 0, 255}},
		// hsl()/hsla(), comma and space forms.
		{"hsl(0 100% 50%)", Color{255, 0, 0, 255}},
		{"hsl(120, 100%, 50%)", Color{0, 255, 0, 255}},
		{"hsl(240 100% 50%)", Color{0, 0, 255, 255}},
		{"hsla(0 0% 100% / 0.05)", Color{255, 255, 255, 13}},
		{"hsl(0 0% 0%)", Color{0, 0, 0, 255}},
		// Extended hex forms.
		{"#1a2b3c", Color{26, 43, 60, 255}},
		{"#f00", Color{255, 0, 0, 255}},
		{"#f00f", Color{255, 0, 0, 255}},
		{"#11223344", Color{17, 34, 51, 68}},
	}
	for _, c := range cases {
		got, ok := parseColor(c.in)
		if !ok {
			t.Errorf("parseColor(%q) failed", c.in)
			continue
		}
		if !colorNear(got, c.want, 1) {
			t.Errorf("parseColor(%q) = %+v want %+v", c.in, got, c.want)
		}
	}
}

func TestParseColorModernInvalid(t *testing.T) {
	for _, in := range []string{
		"rgb(1 2)",         // too few channels
		"rgb(a b c)",       // non-numeric
		"rgb(1 2 3 / xyz)", // bad alpha
		"hsl(x 1% 1%)",     // bad hue
		"hsl(0 x% 50%)",    // bad saturation
		"currentColor",     // unsupported keyword
		"#12345",           // 5 hex digits is invalid
		"rgb(1 2 3",        // no closing paren
	} {
		if c, ok := parseColor(in); ok {
			t.Errorf("parseColor(%q) unexpectedly succeeded: %+v", in, c)
		}
	}
}

func colorNear(a, b Color, tol int) bool {
	d := func(x, y uint8) int {
		if x > y {
			return int(x - y)
		}
		return int(y - x)
	}
	return d(a.R, b.R) <= tol && d(a.G, b.G) <= tol && d(a.B, b.B) <= tol && d(a.A, b.A) <= tol
}

// TestModernBackgroundColorCascade proves the exact bug that made every Tailwind
// background render transparent: `background-color` used to share the shorthand
// path and split the value at the first space, so `rgb(22 24 29 / 1)` failed to
// parse. Now it (and the `background` shorthand's leading colour) resolve.
func TestModernBackgroundColorCascade(t *testing.T) {
	html := `<html><head><style>
	.a{--o:1;background-color:rgb(22 24 29/var(--o))}
	.b{background:rgb(246 247 249 / 1) url(x.png) no-repeat}
	.c{color:rgb(94 104 126 / .8)}
	</style></head><body><p class="a">a</p><p class="b">b</p><p class="c">c</p></body></html>`
	sm := cascadeHTML(t, html)
	if got := findStyle(t, sm, "a").Background; got != (Color{22, 24, 29, 255}) {
		t.Errorf(".a background = %+v want {22 24 29 255}", got)
	}
	if got := findStyle(t, sm, "b").Background; got != (Color{246, 247, 249, 255}) {
		t.Errorf(".b background = %+v want {246 247 249 255}", got)
	}
	if got := findStyle(t, sm, "c").Color; !colorNear(got, Color{94, 104, 126, 204}, 1) {
		t.Errorf(".c color = %+v want ~{94 104 126 204}", got)
	}
}

// TestBackgroundShorthandResetsColorWhenAbsent covers a real regression: the
// `background` shorthand must reset background-color to transparent when the
// value doesn't carry a colour token, matching CSS's per-shorthand "resets
// every sub-property it represents" rule — `background:0 0` (position only)
// or `background:url(x) no-repeat` (image only) must NOT leave a previously
// cascaded (or UA-default) background-color untouched. Confirmed load-
// bearing live: github.com's own nav <button>s reset `background:0 0;
// border:0` to look like plain text links; this engine's own new <button>
// UA default (css/ua.go) survived that reset entirely before this fix,
// rendering every such button as a solid grey pill regardless of the
// author's own CSS.
func TestBackgroundShorthandResetsColorWhenAbsent(t *testing.T) {
	html := `<html><head><style>
	.a{background-color:red;background:0 0}
	.b{background-color:red;background:url(x.png) no-repeat}
	.c{background-color:red;background:none}
	</style></head><body>` +
		`<p class="a">a</p><p class="b">b</p><p class="c">c</p></body></html>`
	sm := cascadeHTML(t, html)
	for _, class := range []string{"a", "b", "c"} {
		if got := findStyle(t, sm, class).Background; got != Transparent {
			t.Errorf(".%s background = %+v, want transparent (the bare `background:` shorthand resets colour when it carries none)", class, got)
		}
	}
}

// TestModernBorderColor proves the `border` shorthand and `border-color` accept
// the modern space-separated rgb() syntax (previously the whitespace tokenizer
// shredded `rgb(230 120 40 / 1)` and the border fell back to the text colour).
func TestModernBorderColor(t *testing.T) {
	html := `<html><head><style>
	.s{border:2px solid rgb(230 120 40 / 1)}
	.c{border-color:rgb(10 20 30) rgb(40 50 60 / .5)}
	</style></head><body><div class="s">s</div><div class="c">c</div></body></html>`
	sm := cascadeHTML(t, html)
	s := findStyleClass(t, sm, "s")
	if s.Border.Top.Color != (Color{230, 120, 40, 255}) {
		t.Errorf("border shorthand colour = %+v want {230 120 40 255}", s.Border.Top.Color)
	}
	if s.Border.Top.Width != 2 || s.Border.Top.Style != BorderSolid {
		t.Errorf("border shorthand width/style lost: %+v", s.Border.Top)
	}
	c := findStyleClass(t, sm, "c")
	if c.Border.Top.Color != (Color{10, 20, 30, 255}) || !colorNear(c.Border.Right.Color, Color{40, 50, 60, 128}, 1) {
		t.Errorf("border-color two-value = top %+v right %+v", c.Border.Top.Color, c.Border.Right.Color)
	}
}

func TestTokenizeKeepingParens(t *testing.T) {
	got := tokenizeKeepingParens("2px solid rgb(230 120 40 / 1)")
	want := []string{"2px", "solid", "rgb(230 120 40 / 1)"}
	if len(got) != len(want) {
		t.Fatalf("tokenizeKeepingParens = %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("token %d = %q want %q", i, got[i], want[i])
		}
	}
	// An unbalanced '(' keeps the remainder as one token (no panic).
	if g := tokenizeKeepingParens("a rgb(1 2"); len(g) != 2 || g[1] != "rgb(1 2" {
		t.Errorf("unbalanced = %v", g)
	}
}

// ---- border-radius parsing --------------------------------------------------

func TestParseBorderRadius(t *testing.T) {
	cases := []struct {
		in      string
		wantPx  float64
		wantPct float64
		isPct   bool
		ok      bool
	}{
		{"8px", 8, 0, false, true},
		{"0.5rem", 8, 0, false, true},
		{"50%", 0, 0.5, true, true},
		{"9999px", 9999, 0, false, true},
		{"8px 4px", 8, 0, false, true},      // multi-value → first
		{"10px / 20px", 10, 0, false, true}, // elliptical → horizontal
		{"-3px", 0, 0, false, true},         // clamped to 0
		{"auto", 0, 0, false, false},        // rejected
		{"", 0, 0, false, false},            // rejected
	}
	for _, c := range cases {
		l, ok := parseBorderRadius(c.in, 16)
		if ok != c.ok {
			t.Errorf("parseBorderRadius(%q) ok=%v want %v", c.in, ok, c.ok)
			continue
		}
		if !ok {
			continue
		}
		if c.isPct {
			if !l.IsPercent || l.Percent != c.wantPct {
				t.Errorf("parseBorderRadius(%q) = %+v want pct %v", c.in, l, c.wantPct)
			}
		} else if l.Px != c.wantPx {
			t.Errorf("parseBorderRadius(%q) = %+v want px %v", c.in, l, c.wantPx)
		}
	}
}

func TestBorderRadiusCascade(t *testing.T) {
	html := `<html><head><style>
	.r{border-radius:12px}
	.corner{border-top-left-radius:6px}
	</style></head><body><div class="r">r</div><div class="corner">c</div></body></html>`
	sm := cascadeHTML(t, html)
	if got := findStyle(t, sm, "r", "").BorderRadius.Px; got != 12 {
		t.Errorf("border-radius = %v want 12", got)
	}
	if got := findStyleClass(t, sm, "corner").BorderRadius.Px; got != 6 {
		t.Errorf("per-corner radius = %v want 6", got)
	}
}

// ---- escaped identifiers & :is()/:where() -----------------------------------

func TestEscapedVariantClasses(t *testing.T) {
	// A Tailwind dark-mode rule as actually emitted: :is() wrapper + escaped colon.
	html := `<html class="dark"><head><style>
	:is(.dark .dark\:bg-wash-dark){--o:1;background-color:rgb(35 39 47/var(--o))}
	.md\:flex{display:flex}
	</style></head><body class="bg-wash dark:bg-wash-dark md:flex">x</body></html>`
	sm := cascadeHTML(t, html)
	body := findStyleTag(t, sm, "body")
	if body.Background != (Color{35, 39, 47, 255}) {
		t.Errorf("dark:bg-wash-dark not applied: %+v", body.Background)
	}
	if body.Display != DisplayFlex {
		t.Errorf("md\\:flex not applied: display=%v", body.Display)
	}
}

// TestWhereSelfOrDescendantIdiom covers Tailwind v4's class-based dark-mode
// variant strategy as actually emitted: `.dark\:hidden:where(.dark,.dark *)`
// — a compound-attached :where() whose second alternative ends in a trailing
// descendant-combinator "*". Naive text splicing of "prefix"+"alt" turns this
// into ".dark\:hidden.dark *" (any element with an ancestor matching BOTH
// classes at once — a disjoint, almost-never-matching selector) instead of
// the intended ".dark .dark\:hidden" (the tested element itself, which has an
// ancestor .dark). Observed live on tailwindcss.com: a light/dark caption
// pair (`<span class="inline dark:hidden">text-gray-950</span><span
// class="hidden dark:inline">text-white</span>`) rendered BOTH spans
// simultaneously regardless of theme, because dark:hidden never actually
// fired.
func TestWhereSelfOrDescendantIdiom(t *testing.T) {
	css := `
	.hidden{display:none}
	.inline{display:inline}
	.dark\:hidden:where(.dark,.dark *){display:none}
	.dark\:inline:where(.dark,.dark *){display:inline}
	`
	html := `<html class="dark"><head><style>` + css + `</style></head><body>` +
		`<span class="inline dark:hidden">a</span>` +
		`<span class="hidden dark:inline">b</span>` +
		`</body></html>`
	sm := cascadeHTML(t, html)
	if st := findStyleClass(t, sm, "dark:hidden"); st.Display != DisplayNone {
		t.Errorf("dark:hidden under a .dark ancestor should compute display:none, got %v", st.Display)
	}
	if st := findStyleClass(t, sm, "dark:inline"); st.Display != DisplayInline {
		t.Errorf("dark:inline under a .dark ancestor should compute display:inline, got %v", st.Display)
	}

	// Without a .dark ancestor, neither variant's condition is met: the base
	// (non-variant) classes decide instead — "inline" and "hidden" respectively.
	htmlLight := `<html><head><style>` + css + `</style></head><body>` +
		`<span class="inline dark:hidden">a</span>` +
		`<span class="hidden dark:inline">b</span>` +
		`</body></html>`
	smLight := cascadeHTML(t, htmlLight)
	if st := findStyleClass(t, smLight, "dark:hidden"); st.Display != DisplayInline {
		t.Errorf("outside .dark, the base .inline class should decide, got %v", st.Display)
	}
	if st := findStyleClass(t, smLight, "dark:inline"); st.Display != DisplayNone {
		t.Errorf("outside .dark, the base .hidden class should decide, got %v", st.Display)
	}
}

// TestWhereSelfOrDescendantChildCombinator covers the same idiom with an
// explicit child combinator (">") instead of the implicit descendant
// (whitespace) one, proving stripTrailingSelfCombinator generalises beyond
// the one form Tailwind happens to emit.
func TestWhereSelfOrDescendantChildCombinator(t *testing.T) {
	sels := ParseSelectorList(".x:where(.p>*)")
	if len(sels) != 1 {
		t.Fatalf("expected 1 expanded selector, got %d: %+v", len(sels), sels)
	}
	html := `<div class="p"><em class="x">a</em></div><i class="x">b</i>`
	root, _ := dom.Parse(html)
	child := dom.Find(root, "em")
	other := dom.Find(root, "i")
	if !sels[0].Matches(child) {
		t.Error(".x:where(.p>*) should match a direct child of .p carrying .x")
	}
	if sels[0].Matches(other) {
		t.Error(".x:where(.p>*) should not match .x outside of .p")
	}
}

func TestIsWhereExpansion(t *testing.T) {
	// :is() with a comma list distributes; :where() likewise.
	sels := ParseSelectorList(":is(h1, h2).title")
	if len(sels) != 2 {
		t.Fatalf(":is comma-list expanded to %d selectors, want 2", len(sels))
	}
	html := `<html><body><h1 class="title">a</h1><h2 class="title">b</h2><h3 class="title">c</h3></body></html>`
	root, _ := dom.Parse(html)
	h1 := dom.Find(root, "h1")
	h2 := dom.Find(root, "h2")
	h3 := dom.Find(root, "h3")
	matchAny := func(n *dom.Node) bool {
		for _, s := range sels {
			if s.Matches(n) {
				return true
			}
		}
		return false
	}
	if !matchAny(h1) || !matchAny(h2) {
		t.Error(":is(h1,h2).title should match h1.title and h2.title")
	}
	if matchAny(h3) {
		t.Error(":is(h1,h2).title should NOT match h3.title")
	}
}

func TestWhereAndNestedIs(t *testing.T) {
	// :where() wrapper and a nested descendant :is() both resolve.
	sels := ParseSelectorList(":where(.nav) :is(.a, .b)")
	if len(sels) != 2 {
		t.Fatalf("expanded to %d, want 2", len(sels))
	}
	html := `<html><body><div class="nav"><span class="a">a</span><span class="b">b</span></div><span class="a">out</span></body></html>`
	root, _ := dom.Parse(html)
	var inA, outA *dom.Node
	var walk func(n *dom.Node)
	walk = func(n *dom.Node) {
		if n.Type == dom.Element && n.Tag == "span" {
			for _, cl := range n.Classes() {
				if cl == "a" {
					if elementParent(n) != nil && elementParent(n).Tag == "div" {
						inA = n
					} else {
						outA = n
					}
				}
			}
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	match := func(n *dom.Node) bool {
		for _, s := range sels {
			if s.Matches(n) {
				return true
			}
		}
		return false
	}
	if !match(inA) {
		t.Error(".nav .a should match")
	}
	if match(outA) {
		t.Error("a .a outside .nav should not match")
	}
}

func TestSplitSelectorCommasRespectsParens(t *testing.T) {
	got := splitSelectorCommas(":is(.a, .b), .c")
	if len(got) != 2 {
		t.Fatalf("splitSelectorCommas gave %d parts (%v), want 2", len(got), got)
	}
}

// ---- edge cases for full branch coverage of the new helpers -----------------

func TestHSLHueSectors(t *testing.T) {
	// Cover every branch of the hslToRGB hue switch (0/60/120/180/240/300/neg).
	cases := []struct {
		in   string
		want Color
	}{
		{"hsl(0 100% 50%)", Color{255, 0, 0, 255}},
		{"hsl(60 100% 50%)", Color{255, 255, 0, 255}},
		{"hsl(120 100% 50%)", Color{0, 255, 0, 255}},
		{"hsl(180 100% 50%)", Color{0, 255, 255, 255}},
		{"hsl(240 100% 50%)", Color{0, 0, 255, 255}},
		{"hsl(300 100% 50%)", Color{255, 0, 255, 255}},
		{"hsl(-60 100% 50%)", Color{255, 0, 255, 255}}, // negative hue normalises to 300
		{"hsl(420 100% 50%)", Color{255, 255, 0, 255}}, // >360 normalises to 60
	}
	for _, c := range cases {
		got, ok := parseColor(c.in)
		if !ok || !colorNear(got, c.want, 1) {
			t.Errorf("%q = %+v ok=%v want %+v", c.in, got, ok, c.want)
		}
	}
}

func TestRGBChannelAndAlphaPercentErrors(t *testing.T) {
	// Percentage-form channel / alpha with a non-numeric body must fail cleanly.
	for _, in := range []string{"rgb(x% 0% 0%)", "rgb(1 2 3 / x%)"} {
		if c, ok := parseColor(in); ok {
			t.Errorf("parseColor(%q) unexpectedly ok: %+v", in, c)
		}
	}
}

func TestExpandFunctionalPseudosEdgeCases(t *testing.T) {
	// Unmatched paren: returned unchanged (and then fails to parse downstream).
	if got := expandFunctionalPseudos(":is(.a"); len(got) != 1 || got[0] != ":is(.a" {
		t.Errorf("unmatched paren = %v", got)
	}
	// Empty alternative between commas is skipped.
	got := expandFunctionalPseudos(":is(.a,,.b)")
	if len(got) != 2 {
		t.Errorf(":is(.a,,.b) expanded to %v, want 2", got)
	}
	// A wholly empty :is() drops the wrapper, leaving the surrounding selector.
	if g := expandFunctionalPseudos("a:is()"); len(g) != 1 || g[0] != "a" {
		t.Errorf("a:is() = %v, want [a]", g)
	}
	// No wrapper: unchanged.
	if g := expandFunctionalPseudos(".plain"); len(g) != 1 || g[0] != ".plain" {
		t.Errorf(".plain = %v", g)
	}
}

func TestIndexUnescaped(t *testing.T) {
	// A backslash-escaped occurrence is skipped; a later real one is found.
	if i := indexUnescaped(`a\:is(x):is(y)`, ":is("); i != 8 {
		t.Errorf("indexUnescaped skipped-escaped = %d, want 8", i)
	}
	if i := indexUnescaped(`no match here`, ":is("); i != -1 {
		t.Errorf("indexUnescaped none = %d, want -1", i)
	}
}

func TestMatchParenAtEscaped(t *testing.T) {
	// An escaped ')' inside the argument is not treated as the closer.
	s := `:is(.a\)b)`
	open := 3 // the '('
	close, ok := matchParenAt(s, open)
	if !ok || s[close] != ')' || close != len(s)-1 {
		t.Errorf("matchParenAt escaped = %d ok=%v", close, ok)
	}
	if _, ok := matchParenAt(":is(.a", 3); ok {
		t.Error("matchParenAt on unbalanced should report false")
	}
}

// ---- shared cascade test helpers --------------------------------------------

func cascadeHTML(t *testing.T, html string) StyleMap {
	t.Helper()
	root, err := dom.Parse(html)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return Cascade(root)
}

// findStyle returns the computed style of the first element carrying the given
// class (variadic extra args are ignored placeholders to keep call sites terse).
func findStyle(t *testing.T, sm StyleMap, class string, _ ...string) *Style {
	t.Helper()
	return findStyleClass(t, sm, class)
}

func findStyleClass(t *testing.T, sm StyleMap, class string) *Style {
	t.Helper()
	for n, st := range sm {
		for _, cl := range n.Classes() {
			if cl == class {
				return st
			}
		}
	}
	t.Fatalf("no element with class %q", class)
	return nil
}

func findStyleTag(t *testing.T, sm StyleMap, tag string) *Style {
	t.Helper()
	for n, st := range sm {
		if n.Tag == tag {
			return st
		}
	}
	t.Fatalf("no <%s> element", tag)
	return nil
}
