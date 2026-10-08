// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package css

import (
	"testing"

	"github.com/go-webengine/engine/dom"
)

// findByID walks n's subtree for the element whose id attribute is id.
func findByID(n *dom.Node, id string) *dom.Node {
	if n.Type == dom.Element && n.ID() == id {
		return n
	}
	for _, c := range n.Children {
		if f := findByID(c, id); f != nil {
			return f
		}
	}
	return nil
}

// styleOfID is styleOf's counterpart for locating an element by id rather
// than tag, needed whenever a test's fixture has several elements of the same
// tag (e.g. several <div>s only distinguished by id).
func styleOfID(t *testing.T, htmlSrc, id string) *Style {
	t.Helper()
	root, err := dom.Parse(htmlSrc)
	if err != nil {
		t.Fatal(err)
	}
	sm := Cascade(root)
	n := findByID(root, id)
	if n == nil {
		t.Fatalf("no element with id=%q", id)
	}
	st := sm[n]
	if st == nil {
		t.Fatalf("no style for id=%q", id)
	}
	return st
}

// --- Parsing ---------------------------------------------------------------

// TestParseContainerRuleUnnamed covers the base case: an @container block with
// no name, only a size feature. Its selector's rule must come back tagged with
// a Container condition (not silently dropped, and not baked in as a match/no-
// match the way @media is — the decision is deferred to cascade time).
func TestParseContainerRuleUnnamed(t *testing.T) {
	rules := ParseStylesheetVW(`@container (min-width: 400px) { .widget { color: red } }`, 1024)
	if len(rules) != 1 {
		t.Fatalf("got %d rules, want 1: %+v", len(rules), rules)
	}
	c := rules[0].Container
	if c == nil {
		t.Fatal("expected a Container condition")
	}
	if c.Name != "" {
		t.Errorf("Name = %q, want empty (unnamed query)", c.Name)
	}
	if c.Cond != "(min-width: 400px)" {
		t.Errorf("Cond = %q", c.Cond)
	}
}

// TestParseContainerRuleNamed covers the "@container <name> (...)" form.
func TestParseContainerRuleNamed(t *testing.T) {
	rules := ParseStylesheetVW(`@container sidebar (min-width: 400px) { .widget { color: red } }`, 1024)
	if len(rules) != 1 {
		t.Fatalf("got %d rules, want 1", len(rules))
	}
	c := rules[0].Container
	if c == nil || c.Name != "sidebar" {
		t.Fatalf("Container = %+v, want Name=sidebar", c)
	}
}

// TestParseContainerStyleQueryDropped covers the explicitly out-of-scope
// "container style queries" form (@container style(...)): it must not crash
// and must not silently be treated as an always-matching or always-failing
// condition — the whole body is dropped, exactly like any other at-rule this
// engine does not understand.
func TestParseContainerStyleQueryDropped(t *testing.T) {
	rules := ParseStylesheetVW(`@container style(--theme: dark) { .widget { color: red } }`, 1024)
	if len(rules) != 0 {
		t.Errorf("expected @container style(...) to be dropped wholesale, got %+v", rules)
	}
}

// TestParseContainerNestedInsideMediaAndLayer covers @container nested inside
// @media and @layer (both already fixed, this session, to unwrap rather than
// drop their bodies) — the combination must not crash or lose content, and
// the @media condition must still gate inclusion at parse time while the
// @container condition rides along on the resulting rule for cascade time.
func TestParseContainerNestedInsideMediaAndLayer(t *testing.T) {
	css := `
	@layer utilities {
		@media (min-width: 100px) {
			@container sidebar (min-width: 400px) {
				.widget { color: red }
			}
		}
	}
	`
	rules := ParseStylesheetVW(css, 1024) // vw=1024 satisfies the @media condition
	if len(rules) != 1 {
		t.Fatalf("got %d rules, want 1: %+v", len(rules), rules)
	}
	if rules[0].Container == nil || rules[0].Container.Name != "sidebar" {
		t.Errorf("Container = %+v", rules[0].Container)
	}

	// The @media condition must still gate inclusion: at a viewport narrower
	// than 100px it should not match, and the whole nested body disappears.
	rules = ParseStylesheetVW(css, 50)
	if len(rules) != 0 {
		t.Errorf("expected @media(min-width:100px) to exclude content at vw=50, got %+v", rules)
	}
}

// TestParseContainerWrappingMediaAndLayer covers the reverse nesting —
// @media/@layer INSIDE @container — verifying the Container condition
// attaches to whatever rules the inner recursive parse produces, regardless
// of what at-rule shape they came from.
func TestParseContainerWrappingMediaAndLayer(t *testing.T) {
	css := `
	@container sidebar (min-width: 400px) {
		@layer utilities {
			@media (min-width: 100px) {
				.widget { color: red }
			}
		}
	}
	`
	rules := ParseStylesheetVW(css, 1024)
	if len(rules) != 1 {
		t.Fatalf("got %d rules, want 1: %+v", len(rules), rules)
	}
	if rules[0].Container == nil || rules[0].Container.Name != "sidebar" {
		t.Errorf("Container = %+v", rules[0].Container)
	}
}

// TestParseContainerNestedMerge covers a doubly-nested @container: both
// conditions must hold (AND), and a name given by the inner (nearer) one
// takes precedence over the outer.
func TestParseContainerNestedMerge(t *testing.T) {
	css := `
	@container (min-width: 200px) {
		@container inner (min-width: 400px) {
			.widget { color: red }
		}
	}
	`
	rules := ParseStylesheetVW(css, 1024)
	if len(rules) != 1 {
		t.Fatalf("got %d rules, want 1", len(rules))
	}
	c := rules[0].Container
	if c == nil {
		t.Fatal("expected a merged Container condition")
	}
	if c.Name != "inner" {
		t.Errorf("Name = %q, want inner (nearer name wins)", c.Name)
	}
	if c.Cond == "" || !containsBoth(c.Cond, "200px", "400px") {
		t.Errorf("Cond = %q, want both features present (AND)", c.Cond)
	}
}

// TestParseContainerNestedMergeOuterNameAndCondFallback covers the other two
// branches of mergeContainerCondition not exercised by
// TestParseContainerNestedMerge: an unnamed, condition-less inner query
// (empty Name AND empty Cond) must inherit BOTH the outer's name and its
// condition wholesale, rather than only ANDing a non-empty inner condition.
func TestParseContainerNestedMergeOuterNameAndCondFallback(t *testing.T) {
	css := `
	@container outer (min-width: 200px) {
		@container {
			.widget { color: red }
		}
	}
	`
	rules := ParseStylesheetVW(css, 1024)
	if len(rules) != 1 {
		t.Fatalf("got %d rules, want 1", len(rules))
	}
	c := rules[0].Container
	if c == nil {
		t.Fatal("expected a merged Container condition")
	}
	if c.Name != "outer" {
		t.Errorf("Name = %q, want outer (inherited from the outer condition)", c.Name)
	}
	if c.Cond != "(min-width: 200px)" {
		t.Errorf("Cond = %q, want the outer's condition inherited wholesale", c.Cond)
	}
}

// TestParseContainerNameOnlyNoCondition covers the lenient "@container name {
// ... }" form with no parenthesised condition at all (not valid CSS — a
// condition is required — but this engine includes the content with a
// name-only filter rather than dropping it, consistent with its general bias
// toward including content over discarding it; see the @layer fix this same
// session for the established precedent).
func TestParseContainerNameOnlyNoCondition(t *testing.T) {
	rules := ParseStylesheetVW(`@container sidebar { .widget { color: red } }`, 1024)
	if len(rules) != 1 {
		t.Fatalf("got %d rules, want 1", len(rules))
	}
	c := rules[0].Container
	if c == nil || c.Name != "sidebar" || c.Cond != "" {
		t.Errorf("Container = %+v, want Name=sidebar Cond=\"\"", c)
	}
}

func containsBoth(s, a, b string) bool {
	return containsSub(s, a) && containsSub(s, b)
}

func containsSub(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// TestApplyContainerTypeAndName covers the longhand properties.
func TestApplyContainerTypeAndName(t *testing.T) {
	st := styleOfID(t, `<html><body><div id="x" style="container-type:inline-size;container-name:sidebar"></div></body></html>`, "x")
	if st.ContainerType != ContainerInlineSize {
		t.Errorf("ContainerType = %v, want ContainerInlineSize", st.ContainerType)
	}
	if st.ContainerName != "sidebar" {
		t.Errorf("ContainerName = %q, want sidebar", st.ContainerName)
	}
}

// TestApplyContainerNameNoneClears covers the `container-name: none` longhand
// clearing an already-set name (distinct from the shorthand's own "none"
// handling in applyContainerShorthand, which is a different code path).
func TestApplyContainerNameNoneClears(t *testing.T) {
	st := styleOfID(t, `<html><body><div id="x" style="container-name:sidebar;container-name:none"></div></body></html>`, "x")
	if st.ContainerName != "" {
		t.Errorf("container-name:none should clear, got %q", st.ContainerName)
	}
}

// TestApplyContainerShorthand covers the `container: name / type` shorthand
// and its two single-component forms.
func TestApplyContainerShorthand(t *testing.T) {
	cases := []struct {
		value    string
		wantType ContainerType
		wantName string
	}{
		{"sidebar / inline-size", ContainerInlineSize, "sidebar"},
		{"card / size", ContainerTypeSize, "card"},
		{"inline-size", ContainerInlineSize, ""},
		{"none / size", ContainerTypeSize, ""},
	}
	for _, c := range cases {
		htmlSrc := `<html><body><div id="x" style="container:` + c.value + `"></div></body></html>`
		st := styleOfID(t, htmlSrc, "x")
		if st.ContainerType != c.wantType {
			t.Errorf("container:%s -> ContainerType = %v, want %v", c.value, st.ContainerType, c.wantType)
		}
		if st.ContainerName != c.wantName {
			t.Errorf("container:%s -> ContainerName = %q, want %q", c.value, st.ContainerName, c.wantName)
		}
	}
}

// TestContainerNotInherited covers that container-type/container-name reset
// on every element rather than inheriting, like the rest of this engine's
// non-inherited properties.
func TestContainerNotInherited(t *testing.T) {
	st := styleOfID(t, `<html><body style="container-type:inline-size;container-name:outer">`+
		`<div id="x"></div></body></html>`, "x")
	if st.ContainerType != ContainerNormal || st.ContainerName != "" {
		t.Errorf("container-type/name leaked via inheritance: %+v", st)
	}
}

// --- Cascade-time evaluation -------------------------------------------------

// TestCascadeVWContainersBasic is the core end-to-end case: an @container
// condition is inactive with no size information (CascadeVW / a nil
// containers map), and becomes active once CascadeVWContainers is given the
// container element's real measured size, gated correctly on both sides of
// the threshold.
func TestCascadeVWContainersBasic(t *testing.T) {
	htmlSrc := `<html><body><div id="box"><p id="target">hi</p></div></body></html>`
	sheet := `#box { container-type: inline-size; } ` +
		`@container (min-width: 400px) { #target { color: red } }`

	root, err := dom.Parse(htmlSrc)
	if err != nil {
		t.Fatal(err)
	}
	box := findByID(root, "box")
	target := findByID(root, "target")

	// No layout info yet: CascadeVW must not guess a match.
	sm := CascadeVW(root, 1024, []string{sheet})
	if sm[target].Color == (Color{255, 0, 0, 255}) {
		t.Error("condition matched with no container size known")
	}

	// Container measured wide enough: the rule must now apply.
	sm = CascadeVWContainers(root, 1024, []string{sheet}, map[*dom.Node]ContainerSize{box: {InlineSize: 500}})
	if sm[target].Color != (Color{255, 0, 0, 255}) {
		t.Errorf("condition should match at InlineSize=500, got color %+v", sm[target].Color)
	}

	// Container measured too narrow: the rule must not apply.
	sm = CascadeVWContainers(root, 1024, []string{sheet}, map[*dom.Node]ContainerSize{box: {InlineSize: 300}})
	if sm[target].Color == (Color{255, 0, 0, 255}) {
		t.Error("condition matched at InlineSize=300 (below the 400px threshold)")
	}
}

// TestCascadeVWContainersNamedSkipsNearerMismatch covers name-directed lookup:
// a named @container must walk PAST a nearer container that doesn't carry
// that name to reach a farther one that does, rather than stopping (and
// failing) at the first container found.
func TestCascadeVWContainersNamedSkipsNearerMismatch(t *testing.T) {
	htmlSrc := `<html><body>` +
		`<div id="outer"><div id="inner"><p id="target">hi</p></div></div>` +
		`</body></html>`
	sheet := `#outer { container-type: inline-size; container-name: outer; } ` +
		`#inner { container-type: inline-size; container-name: inner; } ` +
		`@container outer (min-width: 400px) { #target { color: red } }`

	root, err := dom.Parse(htmlSrc)
	if err != nil {
		t.Fatal(err)
	}
	outer, inner, target := findByID(root, "outer"), findByID(root, "inner"), findByID(root, "target")

	sizes := map[*dom.Node]ContainerSize{
		outer: {InlineSize: 500}, // satisfies the query
		inner: {InlineSize: 10},  // would NOT satisfy it, but has the wrong name
	}
	sm := CascadeVWContainers(root, 1024, []string{sheet}, sizes)
	if sm[target].Color != (Color{255, 0, 0, 255}) {
		t.Errorf("named query should skip the nearer mismatched container and match #outer, got %+v", sm[target].Color)
	}
}

// TestCascadeVWContainersUnnamedUsesNearest covers that an UNNAMED query uses
// the nearest container regardless of its name.
func TestCascadeVWContainersUnnamedUsesNearest(t *testing.T) {
	htmlSrc := `<html><body>` +
		`<div id="outer"><div id="inner"><p id="target">hi</p></div></div>` +
		`</body></html>`
	sheet := `#outer { container-type: inline-size; container-name: outer; } ` +
		`#inner { container-type: inline-size; container-name: inner; } ` +
		`@container (min-width: 400px) { #target { color: red } }`

	root, err := dom.Parse(htmlSrc)
	if err != nil {
		t.Fatal(err)
	}
	outer, inner, target := findByID(root, "outer"), findByID(root, "inner"), findByID(root, "target")

	sizes := map[*dom.Node]ContainerSize{
		outer: {InlineSize: 500},
		inner: {InlineSize: 10}, // the NEAREST container; too narrow
	}
	sm := CascadeVWContainers(root, 1024, []string{sheet}, sizes)
	if sm[target].Color == (Color{255, 0, 0, 255}) {
		t.Error("unnamed query should use the nearest container (#inner, too narrow), not skip to #outer")
	}
}

// TestCascadeVWContainersNoAncestor covers the "no qualifying container in
// scope at all" case: the condition must never match, regardless of what the
// sizes map otherwise contains.
func TestCascadeVWContainersNoAncestor(t *testing.T) {
	htmlSrc := `<html><body><p id="target">hi</p></body></html>`
	sheet := `@container (min-width: 0px) { #target { color: red } }`
	root, err := dom.Parse(htmlSrc)
	if err != nil {
		t.Fatal(err)
	}
	sm := CascadeVWContainers(root, 1024, []string{sheet}, map[*dom.Node]ContainerSize{})
	if sm[findByID(root, "target")].Color == (Color{255, 0, 0, 255}) {
		t.Error("condition matched with no ancestor container at all")
	}
}

// TestCascadeVWContainersSizeAxis covers container-type:size exposing the
// block axis, and container-type:inline-size NOT exposing it (a height
// feature must fail closed, never guess, on an inline-size-only container).
func TestCascadeVWContainersSizeAxis(t *testing.T) {
	htmlSrc := `<html><body><div id="box"><p id="target">hi</p></div></body></html>`
	root, err := dom.Parse(htmlSrc)
	if err != nil {
		t.Fatal(err)
	}
	box, target := findByID(root, "box"), findByID(root, "target")

	sheetSize := `#box { container-type: size; } ` +
		`@container (min-height: 200px) { #target { color: red } }`
	sm := CascadeVWContainers(root, 1024, []string{sheetSize}, map[*dom.Node]ContainerSize{box: {InlineSize: 500, BlockSize: 300}})
	if sm[target].Color != (Color{255, 0, 0, 255}) {
		t.Errorf("container-type:size should expose the block axis, got %+v", sm[target].Color)
	}

	sheetInline := `#box { container-type: inline-size; } ` +
		`@container (min-height: 200px) { #target { color: red } }`
	root2, _ := dom.Parse(htmlSrc)
	box2, target2 := findByID(root2, "box"), findByID(root2, "target")
	sm2 := CascadeVWContainers(root2, 1024, []string{sheetInline}, map[*dom.Node]ContainerSize{box2: {InlineSize: 500, BlockSize: 300}})
	if sm2[target2].Color == (Color{255, 0, 0, 255}) {
		t.Error("container-type:inline-size must not expose the block axis to a height feature")
	}
}

// TestCascadeVWContainersRangeSyntax covers the CSS Media Queries Level 4
// range-comparison syntax ("width<=48rem"), reused verbatim from @media
// support for @container conditions.
func TestCascadeVWContainersRangeSyntax(t *testing.T) {
	htmlSrc := `<html><body><div id="box"><p id="target">hi</p></div></body></html>`
	sheet := `#box { container-type: inline-size; } ` +
		`@container (width >= 400px) { #target { color: red } }`
	root, err := dom.Parse(htmlSrc)
	if err != nil {
		t.Fatal(err)
	}
	box, target := findByID(root, "box"), findByID(root, "target")
	sm := CascadeVWContainers(root, 1024, []string{sheet}, map[*dom.Node]ContainerSize{box: {InlineSize: 500}})
	if sm[target].Color != (Color{255, 0, 0, 255}) {
		t.Errorf("width>=400px range syntax should match at 500px, got %+v", sm[target].Color)
	}
}

// TestCascadeVWContainersMissingSizeEntry covers an ancestor that DOES
// establish a qualifying container, but for which the sizes map has no entry
// at all (the first cascade pass, before any layout has measured it, or a
// container that never got boxed) — must fail closed, never guess, and never
// panic on the map lookup.
func TestCascadeVWContainersMissingSizeEntry(t *testing.T) {
	htmlSrc := `<html><body><div id="box"><p id="target">hi</p></div></body></html>`
	sheet := `#box { container-type: inline-size; } ` +
		`@container (min-width: 0px) { #target { color: red } }`
	root, err := dom.Parse(htmlSrc)
	if err != nil {
		t.Fatal(err)
	}
	target := findByID(root, "target")
	sm := CascadeVWContainers(root, 1024, []string{sheet}, map[*dom.Node]ContainerSize{})
	if sm[target].Color == (Color{255, 0, 0, 255}) {
		t.Error("condition matched a container with no measured size on record")
	}
}

// TestCascadeVWContainersNameOnlyCondition covers a name-only @container
// condition (no size feature at all): it matches as soon as a same-named
// container exists AND has a recorded size (any size), and fails when either
// is missing.
func TestCascadeVWContainersNameOnlyCondition(t *testing.T) {
	htmlSrc := `<html><body><div id="box"><p id="target">hi</p></div></body></html>`
	sheet := `#box { container-type: inline-size; container-name: sidebar; } ` +
		`@container sidebar { #target { color: red } }`
	root, err := dom.Parse(htmlSrc)
	if err != nil {
		t.Fatal(err)
	}
	box, target := findByID(root, "box"), findByID(root, "target")

	sm := CascadeVWContainers(root, 1024, []string{sheet}, map[*dom.Node]ContainerSize{box: {InlineSize: 1}})
	if sm[target].Color != (Color{255, 0, 0, 255}) {
		t.Errorf("name-only condition should match once #box is a recorded container, got %+v", sm[target].Color)
	}

	sm = CascadeVWContainers(root, 1024, []string{sheet}, map[*dom.Node]ContainerSize{})
	if sm[target].Color == (Color{255, 0, 0, 255}) {
		t.Error("name-only condition should not match with no recorded size for #box")
	}
}

// TestContainerConditionMatchesFeatureSyntaxVariety exercises the size-feature
// syntaxes containerConditionMatches shares with mediaMatches — max- colon
// syntax (on both axes), every comparison operator in both operand orders,
// and a calc() expression — each as its own table case so a regression in any
// one branch fails on its own case rather than being masked by the others.
func TestContainerConditionMatchesFeatureSyntaxVariety(t *testing.T) {
	cases := []struct {
		name  string
		cond  string
		sz    ContainerSize
		typ   ContainerType
		match bool
	}{
		{"max-width colon, under", "(max-width: 400px)", ContainerSize{InlineSize: 300}, ContainerInlineSize, true},
		{"max-width colon, over", "(max-width: 400px)", ContainerSize{InlineSize: 500}, ContainerInlineSize, false},
		{"max-height colon, under", "(max-height: 200px)", ContainerSize{BlockSize: 100}, ContainerTypeSize, true},
		{"max-height colon, over", "(max-height: 200px)", ContainerSize{BlockSize: 300}, ContainerTypeSize, false},
		{"range < true", "(width < 400px)", ContainerSize{InlineSize: 300}, ContainerInlineSize, true},
		{"range < false", "(width < 400px)", ContainerSize{InlineSize: 500}, ContainerInlineSize, false},
		{"range > true", "(width > 400px)", ContainerSize{InlineSize: 500}, ContainerInlineSize, true},
		{"range > false", "(width > 400px)", ContainerSize{InlineSize: 300}, ContainerInlineSize, false},
		{"range <= boundary", "(width <= 400px)", ContainerSize{InlineSize: 400}, ContainerInlineSize, true},
		{"range <= false", "(width <= 400px)", ContainerSize{InlineSize: 500}, ContainerInlineSize, false},
		{"range height axis not exposed", "(height >= 100px)", ContainerSize{BlockSize: 500}, ContainerInlineSize, false},
		{"malformed colon number ignored", "(min-width: 1.2.3px)", ContainerSize{InlineSize: 1}, ContainerInlineSize, true},
		{"malformed range number ignored", "(1.2.3px <= width)", ContainerSize{InlineSize: 1}, ContainerInlineSize, true},
		{"value-first <=", "(400px <= width)", ContainerSize{InlineSize: 500}, ContainerInlineSize, true},
		{"value-first <= false", "(400px <= width)", ContainerSize{InlineSize: 300}, ContainerInlineSize, false},
		{"value-first height", "(200px <= height)", ContainerSize{BlockSize: 300}, ContainerTypeSize, true},
		{"calc() expression", "(min-width: calc(400px - 10px))", ContainerSize{InlineSize: 395}, ContainerInlineSize, true},
		{"calc() expression false", "(min-width: calc(400px - 10px))", ContainerSize{InlineSize: 300}, ContainerInlineSize, false},
		{"unrecognised feature matches optimistically", "(orientation: landscape)", ContainerSize{InlineSize: 1}, ContainerInlineSize, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := containerConditionMatches(c.cond, c.typ, c.sz)
			if got != c.match {
				t.Errorf("containerConditionMatches(%q, %v, %+v) = %v, want %v", c.cond, c.typ, c.sz, got, c.match)
			}
		})
	}
}

// TestContainerTypeKeywordNormalAndUnknown covers containerTypeKeyword's
// remaining branches: the explicit "normal" keyword (resetting an earlier
// value) and an unrecognised token (left as a no-op by apply, per ordinary
// CSS "invalid declarations are ignored" semantics).
func TestContainerTypeKeywordNormalAndUnknown(t *testing.T) {
	// Later same-precedence declaration explicitly resets to normal.
	st := styleOfID(t, `<html><body><div id="x" style="container-type:inline-size;container-type:normal"></div></body></html>`, "x")
	if st.ContainerType != ContainerNormal {
		t.Errorf("explicit container-type:normal should reset, got %v", st.ContainerType)
	}
	// An unrecognised value is simply ignored, leaving the prior value intact.
	st = styleOfID(t, `<html><body><div id="x" style="container-type:inline-size;container-type:bogus"></div></body></html>`, "x")
	if st.ContainerType != ContainerInlineSize {
		t.Errorf("invalid container-type should be ignored, got %v", st.ContainerType)
	}
}
