// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package css

import (
	"testing"

	"github.com/go-webengine/engine/dom"
)

func TestResolveDeclValueNoVar(t *testing.T) {
	// A value without var() is returned unchanged and always valid.
	got, ok := resolveDeclValue("1px solid red", nil)
	if !ok || got != "1px solid red" {
		t.Fatalf("resolveDeclValue no-var = %q, %v", got, ok)
	}
}

func TestResolveDeclValueCalcFailurePropagates(t *testing.T) {
	// A calc() this engine cannot resolve (percentage-mixed) must invalidate
	// the whole declaration, the same as an unresolvable var().
	if _, ok := resolveDeclValue("calc(50% - 10px)", nil); ok {
		t.Fatal("unresolvable calc() should invalidate the declaration")
	}
	// Through a var(): the calc() only appears after substitution.
	props := map[string]string{"--w": "50%"}
	if _, ok := resolveDeclValue("calc(var(--w) - 10px)", props); ok {
		t.Fatal("unresolvable calc() reached via var() should invalidate the declaration")
	}
}

func TestSubstituteVarsBasic(t *testing.T) {
	props := map[string]string{"--c": "#336699", "--bg": "white"}
	cases := []struct {
		in, want string
		ok       bool
	}{
		{"var(--c)", "#336699", true},                     // whole value
		{"1px solid var(--c)", "1px solid #336699", true}, // inside shorthand
		{"var(--bg)", "white", true},
		{"var(--missing)", "", false},                             // unset, no fallback -> invalid
		{"var(--missing, #333)", "#333", true},                    // fallback used
		{"var(--c, #000)", "#336699", true},                       // set: fallback ignored
		{"var( --c )", "#336699", true},                           // whitespace around name
		{"a var(--c) b var(--bg) c", "a #336699 b white c", true}, // multiple
	}
	for _, tc := range cases {
		got, ok := resolveDeclValue(tc.in, props)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("resolveDeclValue(%q) = %q,%v want %q,%v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestSubstituteVarsFallbackWithComma(t *testing.T) {
	// A fallback may itself contain top-level commas (e.g. rgb(...)): only the
	// first comma separates name from fallback.
	got, ok := resolveDeclValue("var(--x, rgb(1, 2, 3))", nil)
	if !ok || got != "rgb(1, 2, 3)" {
		t.Fatalf("comma fallback = %q,%v", got, ok)
	}
}

func TestSubstituteVarsNested(t *testing.T) {
	// var() nested inside a value AND inside a custom-property's own value.
	props := map[string]string{"--a": "var(--b)", "--b": "teal"}
	got, ok := resolveDeclValue("var(--a)", props)
	if !ok || got != "teal" {
		t.Fatalf("nested in value = %q,%v", got, ok)
	}
	// var() nested inside a fallback resolves too.
	got, ok = resolveDeclValue("var(--missing, var(--b))", props)
	if !ok || got != "teal" {
		t.Fatalf("nested in fallback = %q,%v", got, ok)
	}
}

func TestSubstituteVarsCycle(t *testing.T) {
	// A reference cycle is invalid at computed-value time.
	props := map[string]string{"--a": "var(--b)", "--b": "var(--a)"}
	if _, ok := resolveDeclValue("var(--a)", props); ok {
		t.Fatal("cycle should be invalid")
	}
	// Self-reference is a cycle too.
	self := map[string]string{"--s": "var(--s)"}
	if _, ok := resolveDeclValue("var(--s)", self); ok {
		t.Fatal("self-reference should be invalid")
	}
}

func TestSubstituteVarsUnterminated(t *testing.T) {
	// An unterminated var( is malformed -> invalid.
	if _, ok := resolveDeclValue("var(--c", map[string]string{"--c": "red"}); ok {
		t.Fatal("unterminated var( should be invalid")
	}
	// Unterminated var( inside a fallback path.
	if _, ok := resolveDeclValue("var(--m, var(--c)", nil); ok {
		t.Fatal("unterminated nested var( should be invalid")
	}
}

func TestSubstituteVarsEmptyCustomProp(t *testing.T) {
	// A custom property set to empty substitutes nothing (but is still "set", so
	// the fallback is not used).
	props := map[string]string{"--empty": ""}
	got, ok := resolveDeclValue("[var(--empty, X)]", props)
	if !ok || got != "[]" {
		t.Fatalf("empty custom prop = %q,%v", got, ok)
	}
}

func TestSubstituteVarsInvalidNestedNoFallback(t *testing.T) {
	// A set property whose value references an unset property with no fallback is
	// invalid, which propagates.
	props := map[string]string{"--a": "var(--missing)"}
	if _, ok := resolveDeclValue("var(--a)", props); ok {
		t.Fatal("invalid nested reference should propagate")
	}
	// Same, but the failing var is inside a fallback branch.
	if _, ok := resolveDeclValue("var(--unset, var(--a))", props); ok {
		t.Fatal("invalid var in fallback should propagate")
	}
}

// TestSubstituteVarsInvalidNestedFallsBackToOwnFallback covers a real
// regression: a var() whose referenced custom property EXISTS but is itself
// invalid at computed-value time (its own raw value fails to resolve) must
// still try the REFERENCING var()'s own fallback, per the CSS Custom
// Properties spec's "the guaranteed-invalid value substitutes as though the
// property were unset" rule — not fail outright the way an actually-unset
// property with no fallback does (TestSubstituteVarsInvalidNestedNoFallback,
// unchanged, covers that separate case: no fallback anywhere to try).
//
// This is the real-world "CSS toggle" pattern (postcss-preset-env's
// light-dark() polyfill, seen live on developer.mozilla.org): a guard custom
// property is deliberately left unset for the non-matching theme, an
// intermediate property built from it has no fallback of its own, and only
// the OUTER consumer's var() carries the safe fallback that is meant to win.
func TestSubstituteVarsInvalidNestedFallsBackToOwnFallback(t *testing.T) {
	props := map[string]string{"--toggle": "var(--guard) blue"}
	got, ok := resolveDeclValue("var(--toggle, green)", props)
	if !ok || got != "green" {
		t.Fatalf("resolveDeclValue = %q,%v want %q,true", got, ok, "green")
	}
	// Still correctly invalid when NEITHER level has a fallback to fall back to.
	if _, ok := resolveDeclValue("var(--toggle)", props); ok {
		t.Fatal("no fallback anywhere should still be invalid")
	}
}

func TestFindVarFuncIdentifierBoundary(t *testing.T) {
	// "var(" preceded by an identifier char (e.g. inside "myvar(") is not a real
	// var() token, so the scan skips past it and finds none.
	if got := findVarFunc("myvar(x)", 0); got != -1 {
		t.Errorf("myvar( matched at %d", got)
	}
	// A genuine var( after such an embedded false match is still found.
	if got := findVarFunc("myvar(x) var(--c)", 0); got != 9 {
		t.Errorf("var( after myvar( at %d, want 9", got)
	}
	// No var( at all.
	if got := findVarFunc("1px solid red", 0); got != -1 {
		t.Errorf("no var expected, got %d", got)
	}
}

func TestVarCaseInsensitiveFunctionName(t *testing.T) {
	// The var() function name is ASCII case-insensitive.
	props := map[string]string{"--c": "red"}
	got, ok := resolveDeclValue("VAR(--c)", props)
	if !ok || got != "red" {
		t.Fatalf("VAR() = %q,%v", got, ok)
	}
}

func TestSplitVarArgs(t *testing.T) {
	name, fb, has := splitVarArgs("--x")
	if name != "--x" || has {
		t.Errorf("no-comma split = %q,%q,%v", name, fb, has)
	}
	name, fb, has = splitVarArgs("--x, y")
	if name != "--x" || fb != " y" || !has {
		t.Errorf("comma split = %q,%q,%v", name, fb, has)
	}
	// A parenthesised group before the top-level comma is skipped: commas inside
	// it do not separate name from fallback.
	name, fb, has = splitVarArgs("f(a,b), c")
	if name != "f(a,b)" || fb != " c" || !has {
		t.Errorf("nested-paren split = %q,%q,%v", name, fb, has)
	}
}

func TestIndexFoldEmptySub(t *testing.T) {
	if indexFold("abc", "") != 0 {
		t.Error("empty sub should match at 0")
	}
	if indexFold("abc", "z") != -1 {
		t.Error("absent sub should be -1")
	}
}

func TestMatchParenUnbalanced(t *testing.T) {
	if _, ok := matchParen("(a(b)", 0); ok {
		t.Error("unbalanced parens should not match")
	}
	if i, ok := matchParen("(a(b)c)", 0); !ok || i != 6 {
		t.Errorf("matchParen = %d,%v", i, ok)
	}
}

// --- Integration through the cascade ---------------------------------------

func TestCascadeCustomPropertyColor(t *testing.T) {
	// A custom property defined on <body> drives a descendant's color via var().
	st := styleOf(t, `<html><body style="--main:#ff0000">`+
		`<p style="color:var(--main)">x</p></body></html>`, "p")
	if st.Color != (Color{255, 0, 0, 255}) {
		t.Errorf("color = %v, want red", st.Color)
	}
}

func TestCascadeCustomPropertyInherited(t *testing.T) {
	// The custom property inherits down to a grandchild that consumes it.
	st := styleOf(t, `<html><body><style>`+
		`html{--t:#00ff00} span{color:var(--t)}`+
		`</style><div><span>x</span></div></body></html>`, "span")
	if st.Color != (Color{0, 255, 0, 255}) {
		t.Errorf("inherited custom prop color = %v", st.Color)
	}
}

func TestCascadeCustomPropertyOverride(t *testing.T) {
	// A child overriding the property changes what its own var() resolves to,
	// without affecting the parent (copy-on-write of the inherited map).
	root := mustParse(t, `<html><body><style>`+
		`html{--c:#111111}`+
		`.child{--c:#222222; color:var(--c)}`+
		`</style><p class="child">x</p></body></html>`)
	sm := Cascade(root)
	p := dom.Find(root, "p")
	if p == nil {
		t.Fatal("no <p>")
	}
	if got := sm[p].Color; got != (Color{0x22, 0x22, 0x22, 255}) {
		t.Errorf("child color = %v, want #222222", got)
	}
	// html keeps its own --c and did not get mutated.
	html := dom.Find(root, "html")
	if html == nil {
		t.Fatal("no <html>")
	}
	if sm[html].CustomProps["--c"] != "#111111" {
		t.Errorf("parent --c mutated: %q", sm[html].CustomProps["--c"])
	}
}

func TestCascadeVarInShorthand(t *testing.T) {
	// var() inside the border shorthand resolves its colour component.
	st := styleOf(t, `<html><body style="--bc:#0000ff">`+
		`<p style="border:2px solid var(--bc)">x</p></body></html>`, "p")
	if st.Border.Top.Color != (Color{0, 0, 255, 255}) || st.Border.Top.Width != 2 {
		t.Errorf("border = %+v", st.Border.Top)
	}
}

func TestCascadeVarInvalidDropsToInherited(t *testing.T) {
	// color:var(--missing) with no fallback is invalid at computed-value time and
	// must leave the inherited colour in place (green from the parent).
	st := styleOf(t, `<html><body style="color:green">`+
		`<p style="color:var(--missing)">x</p></body></html>`, "p")
	if st.Color != (Color{0, 128, 0, 255}) {
		t.Errorf("invalid var should keep inherited green, got %v", st.Color)
	}
}

func TestCascadeCustomPropertyCaseSensitive(t *testing.T) {
	// Custom-property names are case-sensitive: --Main and --main differ, so a
	// reference to the unset one falls through to the fallback.
	st := styleOf(t, `<html><body style="--Main:#123456">`+
		`<p style="color:var(--main, #654321)">x</p></body></html>`, "p")
	if st.Color != (Color{0x65, 0x43, 0x21, 255}) {
		t.Errorf("case-sensitive miss should use fallback, got %v", st.Color)
	}
}

// TestCascadeCSSToggleThroughMediaGuard reproduces the exact real-world
// pattern found live on developer.mozilla.org (postcss-preset-env's
// light-dark() polyfill): a guard custom property is set unconditionally,
// then overridden to EMPTY inside an @media (prefers-color-scheme:dark)
// block, and an intermediate property threads the guard into a real value.
// An unrecognised @media feature like prefers-color-scheme is matched
// optimistically (see mediaMatches), mirroring the "assume dark" convention
// this engine's JS side already uses for matchMedia — so the dark override
// should win here. Before the empty-custom-property-value parsing fix, that
// override's "--guard: " declaration was silently dropped (empty value ==
// "nothing to apply", true for an ordinary property but not for a custom
// one), leaving --guard stuck at its unconditional non-empty value forever
// and the intermediate property permanently built from the WRONG branch.
func TestCascadeCSSToggleThroughMediaGuard(t *testing.T) {
	st := styleOf(t, `<html><head><style>
		html{--guard:initial}
		@media (prefers-color-scheme:dark){html{--guard: }}
		p{--toggle:var(--guard) navy; color:var(--toggle, red)}
	</style></head><body><p>x</p></body></html>`, "p")
	// The dark override empties --guard, so --toggle substitutes to (empty)+
	// "navy" — a VALID, set value — and color uses it rather than falling
	// back to red.
	if st.Color != (Color{0, 0, 128, 255}) {
		t.Errorf("colour = %v, want navy (dark media override took effect)", st.Color)
	}
}

func TestCascadeRootCustomProperty(t *testing.T) {
	// The common real-world pattern: custom properties declared on :root, consumed
	// by a descendant via var(). Before :root matched, these rules were dropped.
	st := styleOf(t, `<html><head><style>`+
		`:root{--brand:#00838f}`+
		`p{color:var(--brand)}`+
		`</style></head><body><p>x</p></body></html>`, "p")
	if st.Color != (Color{0x00, 0x83, 0x8f, 255}) {
		t.Errorf(":root custom property color = %v, want #00838f", st.Color)
	}
}

func TestSelectorRootMatchesOnlyHTML(t *testing.T) {
	// :root matches <html> but not a nested element; :root as a bare selector and
	// combined (html:root, :root *) parse and match correctly.
	root := mustParse(t, `<html><body><p>x</p></body></html>`)
	sels := ParseSelectorList(":root, html:root, :root p")
	if len(sels) != 3 {
		t.Fatalf("parsed %d selectors, want 3", len(sels))
	}
	html := dom.Find(root, "html")
	p := dom.Find(root, "p")
	if !sels[0].Matches(html) {
		t.Error(":root should match <html>")
	}
	if sels[0].Matches(p) {
		t.Error(":root should not match <p>")
	}
	if !sels[1].Matches(html) {
		t.Error("html:root should match <html>")
	}
	if !sels[2].Matches(p) {
		t.Error(":root p should match <p>")
	}
	// :root carries pseudo-class specificity (class-level, weight 100).
	if got := sels[0].Specificity(); got != 100 {
		t.Errorf(":root specificity = %d, want 100", got)
	}
}

func TestCascadeVarFontSize(t *testing.T) {
	// var() also resolves in the font-size pass (which runs before other props).
	st := styleOf(t, `<html><body style="--fs:24px">`+
		`<p style="font-size:var(--fs)">x</p></body></html>`, "p")
	if st.FontSize != 24 {
		t.Errorf("font-size via var = %v, want 24", st.FontSize)
	}
}
