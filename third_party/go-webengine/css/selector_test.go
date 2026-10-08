// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package css

import (
	"testing"

	"github.com/go-webengine/engine/dom"
)

func el(tag, id, class string) *dom.Node {
	attr := map[string]string{}
	if id != "" {
		attr["id"] = id
	}
	if class != "" {
		attr["class"] = class
	}
	return &dom.Node{Type: dom.Element, Tag: tag, Attr: attr}
}

// attach links children to a parent (with Parent back-pointers) for combinator
// tests and returns the parent.
func attach(parent *dom.Node, children ...*dom.Node) *dom.Node {
	for _, c := range children {
		c.Parent = parent
		parent.Children = append(parent.Children, c)
	}
	return parent
}

func TestParseSelectorList(t *testing.T) {
	sels := ParseSelectorList("a.foo#bar, p , , div span, .x:hover, > , *")
	// Expect: a.foo#bar, p, "div span" (2 parts), .x, * → 5 (empties dropped).
	if len(sels) != 5 {
		t.Fatalf("got %d selectors: %+v", len(sels), sels)
	}
	first := sels[0]
	key := first.parts[len(first.parts)-1]
	if key.Tag != "a" || key.ID != "bar" || len(key.Classes) != 1 || key.Classes[0] != "foo" {
		t.Errorf("first key = %+v", key)
	}
	dsp := sels[2]
	if len(dsp.parts) != 2 || dsp.parts[0].Tag != "div" || dsp.parts[1].Tag != "span" {
		t.Errorf("descendant parts = %+v", dsp.parts)
	}
	if len(dsp.combs) != 1 || dsp.combs[0] != combDescendant {
		t.Errorf("descendant comb = %+v", dsp.combs)
	}
	x := sels[3].parts[0]
	if x.Tag != "" || len(x.Classes) != 1 || x.Classes[0] != "x" {
		t.Errorf("pseudo = %+v", x)
	}
}

func TestSpecificity(t *testing.T) {
	id, _ := parseComplex("#a")
	cls, _ := parseComplex(".a")
	tag, _ := parseComplex("p")
	comp, _ := parseComplex("p.a.b#c")
	if !(id.Specificity() > cls.Specificity() && cls.Specificity() > tag.Specificity()) {
		t.Errorf("ordering id=%d cls=%d tag=%d", id.Specificity(), cls.Specificity(), tag.Specificity())
	}
	if comp.Specificity() != 1*10000+2*100+1 {
		t.Errorf("compound specificity = %d", comp.Specificity())
	}
	chain, _ := parseComplex("div.box p")
	if chain.Specificity() != 0*10000+1*100+2 {
		t.Errorf("chain specificity = %d", chain.Specificity())
	}
}

func TestMatchesSimple(t *testing.T) {
	n := el("a", "bar", "foo baz")
	s, _ := parseComplex("a.foo#bar")
	if !s.Matches(n) {
		t.Error("should match")
	}
	if s2, _ := parseComplex("a.missing"); s2.Matches(n) {
		t.Error("missing class should not match")
	}
	if s3, _ := parseComplex("#other"); s3.Matches(n) {
		t.Error("wrong id should not match")
	}
	if s4, _ := parseComplex("p"); s4.Matches(n) {
		t.Error("wrong tag should not match")
	}
	if u, _ := parseComplex("*"); !u.Matches(n) {
		t.Error("universal should match")
	}
	if s.Matches(&dom.Node{Type: dom.Text}) {
		t.Error("text node should not match")
	}
	if (Selector{}).Matches(n) {
		t.Error("empty selector should not match")
	}
}

func TestDescendantAndChild(t *testing.T) {
	// div > section > p.deep: p is a grandchild of div, a child of section.
	p := el("p", "", "deep")
	sec := attach(el("section", "", ""), p)
	attach(el("div", "", ""), sec)

	if desc, _ := parseComplex("div p"); !desc.Matches(p) {
		t.Error("descendant div p should match")
	}
	if child, _ := parseComplex("section > p"); !child.Matches(p) {
		t.Error("child section > p should match")
	}
	if notChild, _ := parseComplex("div > p"); notChild.Matches(p) {
		t.Error("div > p should NOT match (p is a grandchild)")
	}
	if classDesc, _ := parseComplex("div .deep"); !classDesc.Matches(p) {
		t.Error("div .deep should match")
	}
	if wrong, _ := parseComplex("article p"); wrong.Matches(p) {
		t.Error("article p should not match")
	}
}

func TestSiblingCombinators(t *testing.T) {
	h2 := el("h2", "", "")
	p1 := el("p", "", "")
	p2 := el("p", "", "")
	attach(el("div", "", ""), h2, p1, p2)

	adj, _ := parseComplex("h2 + p")
	if !adj.Matches(p1) {
		t.Error("h2 + p should match the immediately-following p")
	}
	if adj.Matches(p2) {
		t.Error("h2 + p should NOT match the second p")
	}
	gen, _ := parseComplex("h2 ~ p")
	if !gen.Matches(p1) || !gen.Matches(p2) {
		t.Error("h2 ~ p should match all following p siblings")
	}
	if prevElementSibling(h2) != nil {
		t.Error("first child has no previous element sibling")
	}
	// A node with no parent has no previous sibling.
	if prevElementSibling(el("p", "", "")) != nil {
		t.Error("orphan node has no previous sibling")
	}
}

func TestChainedCombinators(t *testing.T) {
	a := el("a", "", "")
	li := attach(el("li", "", ""), a)
	ul := attach(el("ul", "", ""), li)
	attach(el("nav", "", ""), ul)

	if sel, _ := parseComplex("nav > ul li a"); !sel.Matches(a) {
		t.Error("nav > ul li a should match")
	}
	if bad, _ := parseComplex("ul > a"); bad.Matches(a) {
		t.Error("ul > a should not match (parent is li)")
	}
	// Adjacent-with-no-previous: p + a where a is first child fails.
	first := el("a", "", "")
	attach(el("li", "", ""), first)
	if adj, _ := parseComplex("p + a"); adj.Matches(first) {
		t.Error("p + a should not match a first child")
	}
	if sib, _ := parseComplex("p ~ a"); sib.Matches(first) {
		t.Error("p ~ a should not match with no matching previous sibling")
	}
}

// checkbox builds an <input type=type> element, marked checked when checked.
func checkbox(id, typ string, checked bool) *dom.Node {
	attr := map[string]string{"type": typ}
	if id != "" {
		attr["id"] = id
	}
	if checked {
		attr["checked"] = ""
	}
	return &dom.Node{Type: dom.Element, Tag: "input", Attr: attr}
}

func TestCheckedPseudo(t *testing.T) {
	on := checkbox("t", "checkbox", true)
	off := checkbox("t", "checkbox", false)

	sel, ok := parseComplex("input:checked")
	if !ok {
		t.Fatal("input:checked should parse")
	}
	if !sel.Matches(on) {
		t.Error(":checked should match a checked input")
	}
	if sel.Matches(off) {
		t.Error(":checked should NOT match an unchecked input")
	}

	// A bare ":checked" is a valid, real constraint on its own.
	bare, ok := parseComplex(":checked")
	if !ok || bare.parts[0].Checked != true {
		t.Fatalf("bare :checked = %+v ok=%v", bare, ok)
	}
	if !bare.Matches(on) || bare.Matches(off) {
		t.Error("bare :checked match wrong")
	}

	// <option selected> counts as checked; a plain element never does.
	opt := &dom.Node{Type: dom.Element, Tag: "option", Attr: map[string]string{"selected": ""}}
	if !bare.Matches(opt) {
		t.Error(":checked should match <option selected>")
	}
	if bare.Matches(el("div", "", "")) {
		t.Error(":checked should not match a plain div")
	}
	// ":checked" contributes class-level specificity.
	if got := bare.Specificity(); got != 100 {
		t.Errorf(":checked specificity = %d, want 100", got)
	}
}

func TestDisabledEnabledPseudo(t *testing.T) {
	disabled := &dom.Node{Type: dom.Element, Tag: "button", Attr: map[string]string{"disabled": ""}}
	plain := &dom.Node{Type: dom.Element, Tag: "button", Attr: map[string]string{}}

	sel, ok := parseComplex("button:disabled")
	if !ok {
		t.Fatal("button:disabled should parse")
	}
	if !sel.Matches(disabled) {
		t.Error(":disabled should match a button with the disabled attribute")
	}
	if sel.Matches(plain) {
		t.Error(":disabled should NOT match a plain button (this was the bug: an unmodelled :disabled degraded to matching its base unconditionally)")
	}

	esel, ok := parseComplex("button:enabled")
	if !ok {
		t.Fatal("button:enabled should parse")
	}
	if esel.Matches(disabled) {
		t.Error(":enabled should NOT match a disabled button")
	}
	if !esel.Matches(plain) {
		t.Error(":enabled should match a plain button")
	}

	// A bare ":disabled" is a valid, real constraint on its own.
	bare, ok := parseComplex(":disabled")
	if !ok || bare.parts[0].Disabled != true {
		t.Fatalf("bare :disabled = %+v ok=%v", bare, ok)
	}
	if !bare.Matches(disabled) || bare.Matches(plain) {
		t.Error("bare :disabled match wrong")
	}

	// ":disabled" contributes class-level specificity, same as ":checked".
	if got := bare.Specificity(); got != 100 {
		t.Errorf(":disabled specificity = %d, want 100", got)
	}
}

// TestIndeterminatePseudo covers ":indeterminate" — entirely missing before
// this fix, unlike ":checked"/":disabled" above it has NO backing content
// attribute at all (dom.Node.Indeterminate is pure script-set runtime state,
// see its own doc comment), so a real checkbox is matched by setting the
// Node field directly rather than via an Attr map, mirroring how a script
// would set `.indeterminate = true` at runtime. Real corpus trigger:
// github.com's own behaviors.js sets this on its "select all" bulk-action
// checkboxes.
func TestIndeterminatePseudo(t *testing.T) {
	on := &dom.Node{Type: dom.Element, Tag: "input", Attr: map[string]string{"type": "checkbox"}, Indeterminate: true}
	off := &dom.Node{Type: dom.Element, Tag: "input", Attr: map[string]string{"type": "checkbox"}}

	sel, ok := parseComplex("input:indeterminate")
	if !ok {
		t.Fatal("input:indeterminate should parse")
	}
	if !sel.Matches(on) {
		t.Error(":indeterminate should match a checkbox with Indeterminate set")
	}
	if sel.Matches(off) {
		t.Error(":indeterminate should NOT match a plain checkbox")
	}

	// A bare ":indeterminate" is a valid, real constraint on its own.
	bare, ok := parseComplex(":indeterminate")
	if !ok || bare.parts[0].Indeterminate != true {
		t.Fatalf("bare :indeterminate = %+v ok=%v", bare, ok)
	}
	if !bare.Matches(on) || bare.Matches(off) {
		t.Error("bare :indeterminate match wrong")
	}

	// ":indeterminate" contributes class-level specificity, same as ":checked".
	if got := bare.Specificity(); got != 100 {
		t.Errorf(":indeterminate specificity = %d, want 100", got)
	}
}

func TestNotPseudo(t *testing.T) {
	box := el("div", "", "box")
	other := el("div", "", "other")

	// :not(.other) matches .box but not .other.
	sel, ok := parseComplex("div:not(.other)")
	if !ok {
		t.Fatal("div:not(.other) should parse")
	}
	if !sel.Matches(box) {
		t.Error("div:not(.other) should match .box")
	}
	if sel.Matches(other) {
		t.Error("div:not(.other) should NOT match .other")
	}

	// :not(:checked) — matches an unchecked input, not a checked one.
	on := checkbox("t", "checkbox", true)
	off := checkbox("t", "checkbox", false)
	nc, ok := parseComplex("input:not(:checked)")
	if !ok {
		t.Fatal("input:not(:checked) should parse")
	}
	if !nc.Matches(off) {
		t.Error(":not(:checked) should match an unchecked input")
	}
	if nc.Matches(on) {
		t.Error(":not(:checked) should NOT match a checked input")
	}

	// :not() over a selector list: fails if ANY alternative matches.
	list, ok := parseComplex("div:not(.a, .other)")
	if !ok {
		t.Fatal("div:not(.a, .other) should parse")
	}
	if list.Matches(other) {
		t.Error("div:not(.a, .other) should exclude .other")
	}
	if !list.Matches(box) {
		t.Error("div:not(.a, .other) should keep .box")
	}

	// :not(:hover) is always true statically → no constraint; .box still matches.
	nh, ok := parseComplex(".box:not(:hover)")
	if !ok {
		t.Fatal(".box:not(:hover) should parse")
	}
	if len(nh.parts[0].Not) != 0 {
		t.Errorf(":not(:hover) should impose no constraint, got %+v", nh.parts[0].Not)
	}
	if !nh.Matches(box) {
		t.Error(".box:not(:hover) should match .box statically")
	}

	// :not() specificity picks up its argument (id here).
	spec, ok := parseComplex("div:not(#x)")
	if !ok {
		t.Fatal("div:not(#x) should parse")
	}
	if got := spec.Specificity(); got != 10000+1 { // one id + one tag
		t.Errorf("div:not(#x) specificity = %d, want %d", got, 10001)
	}
}

// TestFirstChildPseudo covers ":first-child" and, critically, its use as a
// ":not()" argument — found live on caniuse.com, whose real CSS is
// `.home__list-item:not(:first-child){display:none}` (hide every "Did you
// know?" tip except the first). Before this, ":first-child" was an
// unmodelled pseudo like ":nth-child", and an unmodelled ":not()" argument
// was treated as "always true statically" (correct for a genuinely
// always-false dynamic pseudo like ":hover", wrong for a real structural
// fact that is only SOMETIMES true): ":not(:first-child)" degraded to no
// constraint at all, so the rule matched EVERY list item including the
// first, hiding the whole tip list instead of all-but-the-first.
func TestFirstChildPseudo(t *testing.T) {
	root, err := dom.Parse(`<html><body><ul class="home__dyk-list">
		<li class="home__list-item">first</li>
		<li class="home__list-item">second</li>
	</ul></body></html>`)
	if err != nil {
		t.Fatal(err)
	}
	ul := dom.Find(root, "ul")
	var items []*dom.Node
	for _, c := range ul.Children {
		if c.Type == dom.Element {
			items = append(items, c)
		}
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 <li> children, got %d", len(items))
	}
	first, second := items[0], items[1]

	fc, ok := parseComplex(":first-child")
	if !ok {
		t.Fatal(":first-child should parse")
	}
	if !fc.Matches(first) {
		t.Error(":first-child should match the first <li>")
	}
	if fc.Matches(second) {
		t.Error(":first-child should NOT match the second <li>")
	}

	// The real caniuse.com rule: hide every list item except the first.
	notFirst, ok := parseComplex(".home__list-item:not(:first-child)")
	if !ok {
		t.Fatal(".home__list-item:not(:first-child) should parse")
	}
	if notFirst.Matches(first) {
		t.Error(":not(:first-child) should NOT match the first <li> (it would wrongly hide it)")
	}
	if !notFirst.Matches(second) {
		t.Error(":not(:first-child) should match the second <li>")
	}
}

// TestLastChildPseudo covers ":last-child", the symmetric counterpart to
// TestFirstChildPseudo above, and the exact compound shape that made it
// load-bearing live: pkg.go.dev's own breadcrumb rule
// `.go-Breadcrumb li:last-child>a{color:var(--color-text-subtle)}` dims only
// the final, unlinked crumb. Before this, ":last-child" was an unmodelled
// pseudo like ":nth-child", so the compound degraded to plain
// `.go-Breadcrumb li>a` — matching every breadcrumb link, not just the last —
// and since this rule has higher specificity than the page's generic
// `a,a:link,a:visited{color:var(--color-brand-primary)}` link-colour rule, it
// wrongly overrode the colour on EVERY breadcrumb link, not only the current
// page's.
func TestLastChildPseudo(t *testing.T) {
	root, err := dom.Parse(`<html><body><ul class="go-Breadcrumb">
		<li class="go-Breadcrumb-item"><a href="/">first</a></li>
		<li class="go-Breadcrumb-item"><a href="/std">second</a></li>
		<li class="go-Breadcrumb-item"><a href="/net">third</a></li>
	</ul></body></html>`)
	if err != nil {
		t.Fatal(err)
	}
	ul := dom.Find(root, "ul")
	var items []*dom.Node
	for _, c := range ul.Children {
		if c.Type == dom.Element {
			items = append(items, c)
		}
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 <li> children, got %d", len(items))
	}
	first, second, third := items[0], items[1], items[2]

	lc, ok := parseComplex(":last-child")
	if !ok {
		t.Fatal(":last-child should parse")
	}
	if lc.Matches(first) || lc.Matches(second) {
		t.Error(":last-child should NOT match the first or second <li>")
	}
	if !lc.Matches(third) {
		t.Error(":last-child should match the third (last) <li>")
	}

	// The real pkg.go.dev rule: dim only the current (last) crumb's link.
	sel, ok := parseComplex(".go-Breadcrumb-item:last-child>a")
	if !ok {
		t.Fatal(".go-Breadcrumb-item:last-child>a should parse")
	}
	firstA := dom.Find(first, "a")
	secondA := dom.Find(second, "a")
	thirdA := dom.Find(third, "a")
	if sel.Matches(firstA) || sel.Matches(secondA) {
		t.Error(".go-Breadcrumb-item:last-child>a must NOT match an earlier crumb's <a> " +
			"(it would wrongly override that link's colour too)")
	}
	if !sel.Matches(thirdA) {
		t.Error(".go-Breadcrumb-item:last-child>a should match the last crumb's <a>")
	}
}

// TestHasPseudo covers ":has(...)": the confirmed real shape from
// tailwindcss.com's own Typography plugin (`.prose h2:has(+h3)`, giving an h2
// immediately followed by an h3 a distinct "eyebrow" style), plus the other
// three relationship kinds this engine models (child, descendant, subsequent
// sibling), and the fail-closed default for an unmodelled alternative — the
// deliberate opposite of most other pseudo-classes in this file, since
// :has() is always a real narrowing constraint an author wrote for a reason.
func TestHasPseudo(t *testing.T) {
	root, err := dom.Parse(`<html><body>
		<div class="prose">
			<h2 id="paired">Paired</h2>
			<h3>subtitle</h3>
			<h2 id="alone">Alone</h2>
			<p>text</p>
		</div>
		<div id="parent"><span id="kid">x</span></div>
		<div id="grandparent"><section><span id="deep">y</span></section></div>
		<ul>
			<li id="li1">one</li>
			<li id="li2">two</li>
			<li class="target" id="li3">three</li>
		</ul>
	</body></html>`)
	if err != nil {
		t.Fatal(err)
	}

	// "+ X" — the real tailwindcss.com shape.
	sel, ok := parseComplex(".prose h2:has(+h3)")
	if !ok {
		t.Fatal(".prose h2:has(+h3) should parse")
	}
	if !sel.Matches(findByID(root, "paired")) {
		t.Error(":has(+h3) should match the h2 immediately followed by an h3")
	}
	if sel.Matches(findByID(root, "alone")) {
		t.Error(":has(+h3) must NOT match an h2 with no following h3 (the previous default made EVERY h2 match)")
	}

	// "> X" — a direct child.
	if sel, ok := parseComplex("#parent:has(> span)"); !ok || !sel.Matches(findByID(root, "parent")) {
		t.Error(":has(> span) should match a parent with a direct <span> child")
	}
	if sel, ok := parseComplex("#grandparent:has(> span)"); !ok || sel.Matches(findByID(root, "grandparent")) {
		t.Error(":has(> span) must NOT match when the <span> is a grandchild, not a direct child")
	}

	// Bare "X" — any descendant, at any depth.
	if sel, ok := parseComplex("#grandparent:has(span)"); !ok || !sel.Matches(findByID(root, "grandparent")) {
		t.Error(":has(span) (bare, descendant) should match a <span> at any depth")
	}

	// "~ X" — any following sibling.
	if sel, ok := parseComplex("#li1:has(~ .target)"); !ok || !sel.Matches(findByID(root, "li1")) {
		t.Error(":has(~ .target) should match when a later sibling has the class")
	}
	if sel, ok := parseComplex("#li3:has(~ .target)"); !ok || sel.Matches(findByID(root, "li3")) {
		t.Error(":has(~ .target) must NOT match the .target element itself (no LATER sibling has it)")
	}

	// An unmodelled alternative (a multi-compound chain) fails CLOSED — the
	// compound must match NOTHING, not degrade to "no constraint" the way
	// every other unmodelled pseudo in this file does.
	sel, ok = parseComplex("#parent:has(div span)")
	if !ok {
		t.Fatal("#parent:has(div span) should still parse (has() itself is recognised)")
	}
	if sel.Matches(findByID(root, "parent")) {
		t.Error(":has() with an unmodelled (multi-compound) argument must match NOTHING, not everything")
	}

	// A dynamic-pseudo alternative (never matches statically) is skipped, same
	// as an unmodelled one — still fails closed, not open.
	if sel, ok := parseComplex("#parent:has(:hover)"); !ok || sel.Matches(findByID(root, "parent")) {
		t.Error(":has(:hover) (a dynamic, always-false-statically alternative) must match NOTHING")
	}

	// A blank alternative in the list (a trailing/empty comma entry) is
	// skipped without affecting the others.
	if sel, ok := parseComplex(".prose h2:has(, +h3)"); !ok || !sel.Matches(findByID(root, "paired")) {
		t.Error(":has(, +h3) should still match via the second, non-empty alternative")
	}
}

// TestNthChildPseudo covers ":nth-child(An+B)", using the single most common
// real-world shape — zebra-striping a table's rows — confirmed across nearly
// every corpus stylesheet this session has fetched (tailwindcss.com,
// developer.mozilla.org, github.com, pkg.go.dev, ...): `tr:nth-child(2n)`.
// Before this was modelled, it degraded to matching EVERY row (not just even
// ones), collapsing an alternating-background table to one solid colour.
func TestNthChildPseudo(t *testing.T) {
	root, err := dom.Parse(`<html><body><table>
		<tr id="r1"><td>1</td></tr>
		<tr id="r2"><td>2</td></tr>
		<tr id="r3"><td>3</td></tr>
		<tr id="r4"><td>4</td></tr>
	</table></body></html>`)
	if err != nil {
		t.Fatal(err)
	}
	r1, r2, r3, r4 := findByID(root, "r1"), findByID(root, "r2"), findByID(root, "r3"), findByID(root, "r4")

	// "2n" and the named "even" keyword are equivalent forms of the same thing.
	for _, arg := range []string{"2n", "even"} {
		sel, ok := parseComplex("tr:nth-child(" + arg + ")")
		if !ok {
			t.Fatalf("tr:nth-child(%s) should parse", arg)
		}
		if sel.Matches(r1) || sel.Matches(r3) {
			t.Errorf("tr:nth-child(%s) must NOT match an odd row (the previous default made EVERY row match)", arg)
		}
		if !sel.Matches(r2) || !sel.Matches(r4) {
			t.Errorf("tr:nth-child(%s) should match every even row", arg)
		}
	}

	// "odd" and "2n+1" are equivalent named/explicit forms of the same thing.
	for _, arg := range []string{"odd", "2n+1"} {
		sel, ok := parseComplex("tr:nth-child(" + arg + ")")
		if !ok {
			t.Fatalf("tr:nth-child(%s) should parse", arg)
		}
		if !sel.Matches(r1) || !sel.Matches(r3) || sel.Matches(r2) || sel.Matches(r4) {
			t.Errorf("tr:nth-child(%s) should match only the odd rows", arg)
		}
	}

	// A bare integer B (no "n" at all) matches only that single position.
	if sel, ok := parseComplex("tr:nth-child(3)"); !ok || sel.Matches(r1) || sel.Matches(r2) || !sel.Matches(r3) || sel.Matches(r4) {
		t.Error("tr:nth-child(3) should match ONLY the third row")
	}

	// A negative coefficient ("-n+3", GitHub's own AvatarStack overflow idiom
	// in reverse: "the first 3") caps matches at the low end.
	if sel, ok := parseComplex("tr:nth-child(-n+3)"); !ok || !sel.Matches(r1) || !sel.Matches(r2) || !sel.Matches(r3) || sel.Matches(r4) {
		t.Error("tr:nth-child(-n+3) should match the first three rows and no more")
	}

	// "n+4" (GitHub's own AvatarStack overflow rule, hiding items past the
	// 4th) matches everything from the 4th position onward.
	if sel, ok := parseComplex("tr:nth-child(n+4)"); !ok || sel.Matches(r1) || sel.Matches(r2) || sel.Matches(r3) || !sel.Matches(r4) {
		t.Error("tr:nth-child(n+4) should match from the fourth row onward")
	}

	// A malformed argument falls through to the generic unmodelled case
	// (matches the base tag alone) rather than panicking or matching nothing.
	if sel, ok := parseComplex("tr:nth-child(not-a-formula)"); !ok || !sel.Matches(r1) {
		t.Error("an unparseable :nth-child argument should fall back to matching the base tag")
	}

	// An explicit leading "+" on the coefficient ("+2n+1", equivalent to
	// "2n+1"/"odd") is valid per spec and exercises parseAnB's own "+" branch.
	if sel, ok := parseComplex("tr:nth-child(+2n+1)"); !ok || !sel.Matches(r1) || sel.Matches(r2) {
		t.Error("tr:nth-child(+2n+1) should behave exactly like tr:nth-child(2n+1)")
	}

	// A node with no parent at all is always position 1 — elementPosition's
	// own base case, distinct from walking a real Children list.
	orphan := el("div", "", "")
	if elementPosition(orphan) != 1 {
		t.Errorf("elementPosition(orphan) = %d, want 1", elementPosition(orphan))
	}
}

// TestNthOfTypePseudo covers ":nth-of-type()", using github.com's own real
// "AvatarStack" overflow-widget shape as the fixture: a row of contributor
// avatars, each `.avatar`, where `:nth-of-type(n+3)` is meant to hide every
// avatar past the second in favour of a "+N more" overflow badge. An
// unmodelled ":nth-of-type" previously left every avatar visible.
func TestNthOfTypePseudo(t *testing.T) {
	root, err := dom.Parse(`<div>
		<span class="avatar" id="a1">1</span>
		<span class="avatar" id="a2">2</span>
		<span class="avatar" id="a3">3</span>
		<b id="notavatar">not an avatar</b>
		<span class="avatar" id="a4">4</span>
	</div>`)
	if err != nil {
		t.Fatal(err)
	}
	a1, a2, a3, a4 := findByID(root, "a1"), findByID(root, "a2"), findByID(root, "a3"), findByID(root, "a4")
	notavatar := findByID(root, "notavatar")

	sel, ok := parseComplex("span:nth-of-type(n+3)")
	if !ok {
		t.Fatal("span:nth-of-type(n+3) should parse")
	}
	if sel.Matches(a1) || sel.Matches(a2) {
		t.Error("span:nth-of-type(n+3) must NOT match the first two spans")
	}
	if !sel.Matches(a3) || !sel.Matches(a4) {
		t.Error("span:nth-of-type(n+3) should match the third span onward")
	}
	// A <b> interleaved between spans must not shift the same-tag count —
	// a4 is the FOURTH span overall but only the third <b>-excluding count,
	// confirming elementPositionOfType counts same-tag siblings only, not
	// every element sibling (that would be elementPosition, already tested).
	if sel2, ok := parseComplex("span:nth-of-type(4)"); !ok || !sel2.Matches(a4) {
		t.Error("span:nth-of-type(4) should match the 4th <span>, unaffected by the interleaved <b>")
	}
	if sel3, ok := parseComplex("b:nth-of-type(1)"); !ok || !sel3.Matches(notavatar) {
		t.Error("b:nth-of-type(1) should match the only <b>, which is position 1 among <b> siblings")
	}
}

// TestNthLastChildPseudo covers ":nth-last-child()", counting position from
// the END of the element siblings rather than the start.
func TestNthLastChildPseudo(t *testing.T) {
	root, err := dom.Parse(`<ul>
		<li id="l1">a</li>
		<li id="l2">b</li>
		<li id="l3">c</li>
	</ul>`)
	if err != nil {
		t.Fatal(err)
	}
	l1, l2, l3 := findByID(root, "l1"), findByID(root, "l2"), findByID(root, "l3")

	// The LAST child is position 1 counted from the end.
	if sel, ok := parseComplex("li:nth-last-child(1)"); !ok || sel.Matches(l1) || sel.Matches(l2) || !sel.Matches(l3) {
		t.Error("li:nth-last-child(1) should match only the LAST li")
	}
	// "2" from the end is the second-to-last.
	if sel, ok := parseComplex("li:nth-last-child(2)"); !ok || sel.Matches(l1) || !sel.Matches(l2) || sel.Matches(l3) {
		t.Error("li:nth-last-child(2) should match only the second-to-last li")
	}
	// "even" counted from the end still alternates, just from the other side.
	if sel, ok := parseComplex("li:nth-last-child(even)"); !ok || sel.Matches(l1) || !sel.Matches(l2) || sel.Matches(l3) {
		t.Error("li:nth-last-child(even) should match only l2 (2nd from the end, of 3)")
	}
}

// TestNthLastOfTypePseudo covers ":nth-last-of-type()", combining
// same-tag-only counting with counting from the end.
func TestNthLastOfTypePseudo(t *testing.T) {
	root, err := dom.Parse(`<div>
		<span id="s1">1</span>
		<b id="mid">x</b>
		<span id="s2">2</span>
		<span id="s3">3</span>
	</div>`)
	if err != nil {
		t.Fatal(err)
	}
	s1, s2, s3 := findByID(root, "s1"), findByID(root, "s2"), findByID(root, "s3")

	// s3 is the last <span> overall, so it's position 1 from the end among
	// spans, even though an interleaved <b> sits between s1 and s2.
	if sel, ok := parseComplex("span:nth-last-of-type(1)"); !ok || !sel.Matches(s3) || sel.Matches(s1) || sel.Matches(s2) {
		t.Error("span:nth-last-of-type(1) should match only the LAST span")
	}
	if sel, ok := parseComplex("span:nth-last-of-type(2)"); !ok || !sel.Matches(s2) || sel.Matches(s1) || sel.Matches(s3) {
		t.Error("span:nth-last-of-type(2) should match only the second-to-last span")
	}
}

// TestOnlyChildAndOfTypeVariants covers ":only-child", ":first-of-type",
// ":last-of-type", and ":only-of-type" together, since they share the same
// small "no sibling on one/either side" shape as the already-tested
// FirstChild/LastChild.
func TestOnlyChildAndOfTypeVariants(t *testing.T) {
	root, err := dom.Parse(`<div>
		<div id="solo-wrap"><span id="solo" class="item">only</span></div>
		<div id="multi-wrap">
			<span id="m1" class="item">a</span>
			<span id="m2" class="item">b</span>
		</div>
		<article>
			<p id="p1">first</p>
			<p id="p2">mid</p>
			<p id="p3">last</p>
		</article>
	</div>`)
	if err != nil {
		t.Fatal(err)
	}
	solo, m1, m2 := findByID(root, "solo"), findByID(root, "m1"), findByID(root, "m2")
	p1, p2, p3 := findByID(root, "p1"), findByID(root, "p2"), findByID(root, "p3")

	if sel, ok := parseComplex(".item:only-child"); !ok || !sel.Matches(solo) || sel.Matches(m1) || sel.Matches(m2) {
		t.Error(".item:only-child should match only the sibling-less span")
	}
	if sel, ok := parseComplex(".item:only-of-type"); !ok || !sel.Matches(solo) || sel.Matches(m1) || sel.Matches(m2) {
		t.Error(".item:only-of-type should match only the sibling-less span (same-tag count of 1)")
	}
	if sel, ok := parseComplex("p:first-of-type"); !ok || !sel.Matches(p1) || sel.Matches(p2) || sel.Matches(p3) {
		t.Error("p:first-of-type should match only the first <p>")
	}
	if sel, ok := parseComplex("p:last-of-type"); !ok || sel.Matches(p1) || sel.Matches(p2) || !sel.Matches(p3) {
		t.Error("p:last-of-type should match only the last <p>")
	}

	// A node with no parent at all is always position 1 from either
	// direction — elementPositionOfType/elementPositionOfTypeFromEnd's own
	// base case, matching elementPosition's precedent above.
	orphan := el("div", "", "")
	if elementPositionOfType(orphan) != 1 {
		t.Errorf("elementPositionOfType(orphan) = %d, want 1", elementPositionOfType(orphan))
	}
	if elementPositionFromEnd(orphan) != 1 {
		t.Errorf("elementPositionFromEnd(orphan) = %d, want 1", elementPositionFromEnd(orphan))
	}
	if elementPositionOfTypeFromEnd(orphan) != 1 {
		t.Errorf("elementPositionOfTypeFromEnd(orphan) = %d, want 1", elementPositionOfTypeFromEnd(orphan))
	}

	// A node that claims a Parent but isn't actually among that parent's
	// Children (never happens via real parsing/DOM mutation — every node in
	// this tree is reached by walking Children — but each function's loop
	// still has a defensive post-loop fallback for it) exercises that
	// fallback line directly, since real DOM structure can't reach it.
	parent := el("div", "", "")
	detached := el("span", "", "")
	detached.Parent = parent
	parent.Children = []*dom.Node{el("span", "", "")} // some OTHER child, not detached
	if got := elementPositionOfType(detached); got != 1 {
		t.Errorf("elementPositionOfType(detached-but-parented) = %d, want 1 (post-loop fallback)", got)
	}
	if got := elementPositionFromEnd(detached); got != 1 {
		t.Errorf("elementPositionFromEnd(detached-but-parented) = %d, want 1 (post-loop fallback)", got)
	}
	if got := elementPositionOfTypeFromEnd(detached); got != 1 {
		t.Errorf("elementPositionOfTypeFromEnd(detached-but-parented) = %d, want 1 (post-loop fallback)", got)
	}
}

// TestEmptyPseudo covers the ":empty" structural pseudo-class, using
// pkg.go.dev's own real rule shape as the fixture:
// `.Documentation-toc:empty{display:none}` is meant to hide a genuinely
// empty table-of-contents list, but a plain (non-":not()") unmodelled
// pseudo degrades the compound to matching its base class ALONE — hiding
// even a real, non-empty <ul> unconditionally.
func TestEmptyPseudo(t *testing.T) {
	root, err := dom.Parse(`<html><body>
		<ul class="Documentation-toc"><li>Clients and Transports</li></ul>
		<ul class="Documentation-toc"></ul>
	</body></html>`)
	if err != nil {
		t.Fatal(err)
	}
	var lists []*dom.Node
	var walk func(n *dom.Node)
	walk = func(n *dom.Node) {
		if n.Type == dom.Element && n.Tag == "ul" {
			lists = append(lists, n)
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	if len(lists) != 2 {
		t.Fatalf("expected 2 <ul> elements, got %d", len(lists))
	}
	nonEmpty, empty := lists[0], lists[1]

	sel, ok := parseComplex(".Documentation-toc:empty")
	if !ok {
		t.Fatal(".Documentation-toc:empty should parse")
	}
	if sel.Matches(nonEmpty) {
		t.Error(".Documentation-toc:empty should NOT match a <ul> with a real <li> child (it would wrongly hide it)")
	}
	if !sel.Matches(empty) {
		t.Error(".Documentation-toc:empty should match a genuinely empty <ul>")
	}
}

// TestUniversalCompoundWithClass covers a compound combining an explicit "*"
// with a further qualifier — "*.line", "*#id" — using tailwindcss.com's own
// real compiled rule as the fixture:
// `:is(.\*\*\:\[\.line\]\:block *).line{display:block}` (Tailwind v4's
// `**:[.line]:block` arbitrary-variant utility, used on its syntax-
// highlighted code samples so each `.line` span gets its own line).
// expandFunctionalPseudos splices this to
// `.\*\*\:\[\.line\]\:block *.line` — a "*.line" compound — which
// scanCompound (with no notion that "*" is special) parsed as a LITERAL tag
// name "*", never matching any real element (a <span>'s tag is "span", not
// the string "*"). Without the fix, tailwindcss.com's whole multi-line code
// demo collapsed onto one line.
func TestUniversalCompoundWithClass(t *testing.T) {
	line := el("span", "", "line")
	attach(el("div", "", "**:[.line]:block"), line)

	sels := ParseSelectorList(`:is(.\*\*\:\[\.line\]\:block *).line`)
	if len(sels) != 1 {
		t.Fatalf("the real tailwindcss.com selector should parse to 1 selector, got %d", len(sels))
	}
	if !sels[0].Matches(line) {
		t.Error("the real tailwindcss.com selector should match .line nested under the **:[.line]:block ancestor")
	}

	// A bare "*.foo" compound (no wrapping :is()) must also match — the
	// splice isn't the only source of this shape.
	bareSels := ParseSelectorList(".parent *.foo")
	if len(bareSels) != 1 {
		t.Fatalf(".parent *.foo should parse to 1 selector, got %d", len(bareSels))
	}
	foo := attach(el("div", "", "parent"), el("span", "", "foo"))
	if !bareSels[0].Matches(foo.Children[0]) {
		t.Error(".parent *.foo should match a descendant with class foo, regardless of its own tag")
	}
}

func TestNotUnmodelledDoesNotDropRule(t *testing.T) {
	// A :not() whose argument is empty or genuinely UNMODELLED (a pseudo-
	// element) must NOT drop the rule — it degrades to "no constraint" so the
	// compound keeps matching its base. Attribute selectors are NOT in this
	// list any more: they are modelled (see TestNotAttributeSelector below),
	// so `:not([data-x])` is a real negation now, not a no-op.
	div := el("div", "", "")
	for _, s := range []string{"div:not()", "div:not(   )", "div:not(::before)"} {
		sel, ok := parseComplex(s)
		if !ok {
			t.Errorf("parseComplex(%q) should parse (unmodelled :not = no constraint)", s)
			continue
		}
		if len(sel.parts[0].Not) != 0 {
			t.Errorf("%q: unmodelled :not should impose no constraint, got %+v", s, sel.parts[0].Not)
		}
		if !sel.Matches(div) {
			t.Errorf("%q should still match a plain div", s)
		}
	}

	// The dark-theme shape that regressed go.dev/pkg.go.dev: `:root:not([attr])`
	// must still select the document root (so its dark custom properties apply)
	// when the attribute is genuinely absent — this is now a REAL attribute
	// check, not a no-op, so also prove the negative: it must NOT match once
	// the attribute is present, which the old "no constraint" behaviour could
	// never have distinguished.
	root := el("html", "", "")
	sel, ok := parseComplex(`:root:not([data-theme])`)
	if !ok || !sel.Matches(root) {
		t.Errorf(":root:not([data-theme]) should match the root element; ok=%v", ok)
	}
	rootThemed := el("html", "", "")
	rootThemed.Attr = map[string]string{"data-theme": "dark"}
	if sel.Matches(rootThemed) {
		t.Error(":root:not([data-theme]) should NOT match once data-theme is present")
	}

	// A comma-separated list is always fully preserved (nothing dropped).
	sels := ParseSelectorList("div:not(), .ok")
	if len(sels) != 2 {
		t.Fatalf("got %d selectors, want 2: %+v", len(sels), sels)
	}
	if !sels[1].Matches(el("span", "", "ok")) {
		t.Error(".ok should match")
	}
}

func TestSplitPseudos(t *testing.T) {
	// A colon inside an attribute value must not split the pseudo list.
	base, ps := splitPseudos(`a[href="ht:tp"]:hover`)
	if base != `a[href="ht:tp"]` || len(ps) != 1 || ps[0] != "hover" {
		t.Errorf("splitPseudos attr-colon = %q %v", base, ps)
	}
	// Nested pseudo argument stays a single token.
	base, ps = splitPseudos(":not(:checked)")
	if base != "" || len(ps) != 1 || ps[0] != "not(:checked)" {
		t.Errorf("splitPseudos nested = %q %v", base, ps)
	}
	// Multiple chained pseudos.
	_, ps = splitPseudos("input:focus:checked")
	if len(ps) != 2 || ps[0] != "focus" || ps[1] != "checked" {
		t.Errorf("splitPseudos chained = %v", ps)
	}
	// No pseudo.
	base, ps = splitPseudos("div.box")
	if base != "div.box" || ps != nil {
		t.Errorf("splitPseudos none = %q %v", base, ps)
	}
	// An escaped colon in the base is not a pseudo boundary.
	base, ps = splitPseudos(`a\:b:checked`)
	if base != `a\:b` || len(ps) != 1 || ps[0] != "checked" {
		t.Errorf("splitPseudos escaped-colon = %q %v", base, ps)
	}
	// A bracketed argument nested inside :not() tokenizes as one compound
	// (exercises the depth-tracking of nested [] and () in tokenizeSelector);
	// both the tag prefix AND the attribute selector are modelled.
	if sel, ok := parseComplex("a:not(b[c])"); !ok || len(sel.parts) != 1 ||
		len(sel.parts[0].Not) != 1 || sel.parts[0].Not[0].Tag != "b" ||
		len(sel.parts[0].Not[0].Attrs) != 1 || sel.parts[0].Not[0].Attrs[0].Name != "c" {
		t.Errorf("a:not(b[c]) = %+v ok=%v", sel, ok)
	}
	// An attribute-only :not() argument IS modelled (a real negation), so the
	// compound keeps its base ("input") AND records the attribute constraint —
	// it is not dropped, and not a no-op either. See TestNotUnmodelledDoesNotDropRule.
	if sel, ok := parseComplex(`input:not([type="x"])`); !ok ||
		len(sel.parts[0].Not) != 1 || sel.parts[0].Tag != "input" ||
		len(sel.parts[0].Not[0].Attrs) != 1 || sel.parts[0].Not[0].Attrs[0] != (attrMatch{Name: "type", Op: attrEqual, Value: "x"}) {
		t.Errorf(`input:not([type="x"]) = %+v ok=%v`, sel, ok)
	}
	// pseudoNameArg forms.
	if n, a := pseudoNameArg("not(:checked)"); n != "not" || a != ":checked" {
		t.Errorf("pseudoNameArg func = %q %q", n, a)
	}
	if n, a := pseudoNameArg("checked"); n != "checked" || a != "" {
		t.Errorf("pseudoNameArg plain = %q %q", n, a)
	}
}

// TestAttributeSelectorOperators covers every attribute-selector operator this
// engine models, both positive and negative cases, plus attribute-name
// case-insensitivity (HTML attribute names always compare case-insensitively)
// and specificity (each attribute selector is one class-level unit).
func TestAttributeSelectorOperators(t *testing.T) {
	withAttr := func(tag string, attrs map[string]string) *dom.Node {
		return &dom.Node{Type: dom.Element, Tag: tag, Attr: attrs}
	}
	cases := []struct {
		sel    string
		attrs  map[string]string
		want   bool
		reason string
	}{
		{"[hidden]", map[string]string{"hidden": ""}, true, "presence: attribute set (even empty)"},
		{"[hidden]", map[string]string{}, false, "presence: attribute absent"},
		{"[data-x=1]", map[string]string{"data-x": "1"}, true, "exact match"},
		{"[data-x=1]", map[string]string{"data-x": "2"}, false, "exact mismatch"},
		{`[data-x="1"]`, map[string]string{"data-x": "1"}, true, "exact match, double-quoted"},
		{"[data-x='1']", map[string]string{"data-x": "1"}, true, "exact match, single-quoted"},
		{"[href^=https]", map[string]string{"href": "https://x.test"}, true, "prefix match"},
		{"[href^=https]", map[string]string{"href": "http://x.test"}, false, "prefix mismatch"},
		{"[href$=.pdf]", map[string]string{"href": "doc.pdf"}, true, "suffix match"},
		{"[href$=.pdf]", map[string]string{"href": "doc.txt"}, false, "suffix mismatch"},
		{"[title*=llo]", map[string]string{"title": "hello world"}, true, "substring match"},
		{"[title*=zzz]", map[string]string{"title": "hello world"}, false, "substring mismatch"},
		{"[class~=b]", map[string]string{"class": "a b c"}, true, "word match: exact word present"},
		{"[class~=bc]", map[string]string{"class": "a b c"}, false, "word match: substring is not a whole word"},
		{"[lang|=en]", map[string]string{"lang": "en"}, true, "dash match: exact"},
		{"[lang|=en]", map[string]string{"lang": "en-US"}, true, "dash match: prefix-then-hyphen"},
		{"[lang|=en]", map[string]string{"lang": "eng"}, false, "dash match: prefix without hyphen"},
		{"[DATA-X=1]", map[string]string{"data-x": "1"}, true, "attribute NAME is case-insensitive"},
	}
	for _, c := range cases {
		sel, ok := parseComplex(c.sel)
		if !ok {
			t.Errorf("%s: parseComplex failed", c.sel)
			continue
		}
		if got := sel.Matches(withAttr("div", c.attrs)); got != c.want {
			t.Errorf("%s vs %v = %v, want %v (%s)", c.sel, c.attrs, got, c.want, c.reason)
		}
	}

	// Empty-value ^=/$=/*= never match, per spec.
	empty := withAttr("div", map[string]string{"data-x": "anything"})
	for _, sel := range []string{`[data-x^=""]`, `[data-x$=""]`, `[data-x*=""]`} {
		s, ok := parseComplex(sel)
		if !ok {
			t.Errorf("%s: parseComplex failed", sel)
			continue
		}
		if s.Matches(empty) {
			t.Errorf("%s should never match (empty value)", sel)
		}
	}

	// Specificity: an attribute selector is one class-level unit, same as a
	// plain class — "a[b]" and "a.c" must have equal specificity.
	withAttrSel, _ := parseComplex("a[b]")
	withClassSel, _ := parseComplex("a.c")
	if withAttrSel.Specificity() != withClassSel.Specificity() {
		t.Errorf("a[b] specificity=%d, a.c specificity=%d, want equal",
			withAttrSel.Specificity(), withClassSel.Specificity())
	}

	// A trailing case-sensitivity flag ("i"/"s") is stripped, not treated as
	// part of the value, and must not break parsing.
	flagged, ok := parseComplex(`[data-x="1" i]`)
	if !ok {
		t.Fatal(`[data-x="1" i] should parse`)
	}
	if !flagged.Matches(withAttr("div", map[string]string{"data-x": "1"})) {
		t.Error(`[data-x="1" i] should match data-x="1" (flag stripped, not compared)`)
	}
}

// TestAttributeSelectorGitHubDarkModeBug is the exact shape that hid
// github.com's entire repository content: a compound-attached :where() over
// an attribute selector, gating visibility on a boolean-as-string
// data-attribute. Before attribute selectors were modelled, "[data-is-hidden-
// narrow=true]" was dropped entirely, degrading the rule to an unconditional
// ".ContentWrapper{display:none}" that fired regardless of the attribute's
// real (false) value.
func TestAttributeSelectorGitHubDarkModeBug(t *testing.T) {
	rule := `.ContentWrapper:where([data-is-hidden=true]){display:none}`
	shown := cascadeHTML(t, `<html><head><style>`+rule+`</style></head><body>`+
		`<div class="ContentWrapper" data-is-hidden="false">real repo content</div>`+
		`</body></html>`)
	if st := findStyleClass(t, shown, "ContentWrapper"); st.Display == DisplayNone {
		t.Error(`data-is-hidden="false" must NOT be hidden — this is the github.com content-loss bug`)
	}

	hidden := cascadeHTML(t, `<html><head><style>`+rule+`</style></head><body>`+
		`<div class="ContentWrapper" data-is-hidden="true">should be hidden</div>`+
		`</body></html>`)
	if st := findStyleClass(t, hidden, "ContentWrapper"); st.Display != DisplayNone {
		t.Error(`data-is-hidden="true" should be hidden — the rule must still work when the attribute genuinely matches`)
	}
}

// TestCheckboxHack is the crux: a hidden-by-default menu revealed only when the
// toggle is :checked. With no user interaction the toggle is unchecked, so the
// reveal rule must NOT apply and the menu stays hidden — Chrome's static state.
func TestCheckboxHack(t *testing.T) {
	hide, _ := parseComplex(".menu")                      // display:none base rule
	reveal, _ := parseComplex("#toggle:checked ~ .menu")  // reveal when checked

	toggle := checkbox("toggle", "checkbox", false)
	menu := el("div", "", "menu")
	attach(el("div", "", ""), toggle, menu)

	if !hide.Matches(menu) {
		t.Fatal(".menu base rule should match the menu")
	}
	if reveal.Matches(menu) {
		t.Error("with an UNCHECKED toggle, the reveal rule must NOT apply (menu stays hidden)")
	}

	// Now mark the toggle checked: the reveal rule applies (menu shown).
	toggleOn := checkbox("toggle", "checkbox", true)
	menu2 := el("div", "", "menu")
	attach(el("div", "", ""), toggleOn, menu2)
	if !reveal.Matches(menu2) {
		t.Error("with a CHECKED toggle, the reveal rule should apply")
	}

	// The inverse MediaWiki form: hide the container while the checkbox is not
	// checked. Unchecked → hidden; checked → the hide rule stops matching.
	hideWhileUnchecked, _ := parseComplex("#toggle:not(:checked) ~ .menu")
	if !hideWhileUnchecked.Matches(menu) {
		t.Error(":not(:checked) ~ .menu should hide the menu of an unchecked toggle")
	}
	if hideWhileUnchecked.Matches(menu2) {
		t.Error(":not(:checked) ~ .menu should stop matching once the toggle is checked")
	}
}

func TestParseComplexEdgeCases(t *testing.T) {
	for _, bad := range []string{"", "   ", "> p", "p >", "p > > a", "::before"} {
		if _, ok := parseComplex(bad); ok {
			t.Errorf("parseComplex(%q) should fail", bad)
		}
	}
	// A bare dynamic pseudo now parses to a never-matching compound (equivalent
	// net effect to being dropped: it applies to nothing in a static render),
	// which is what lets ":not(:hover)" resolve to "no constraint".
	if sel, ok := parseComplex(":hover"); !ok {
		t.Error("parseComplex(:hover) should parse")
	} else if sel.Matches(el("div", "", "")) {
		t.Error(":hover must never match statically")
	}
	c, ok := parseSimple("p.")
	if !ok || c.Tag != "p" || len(c.Classes) != 0 {
		t.Errorf("p. = %+v %v", c, ok)
	}
	attr, ok := parseComplex("input[type=text]")
	if !ok || attr.parts[0].Tag != "input" ||
		len(attr.parts[0].Attrs) != 1 || attr.parts[0].Attrs[0] != (attrMatch{Name: "type", Op: attrEqual, Value: "text"}) {
		t.Errorf("attr selector = %+v %v", attr, ok)
	}
	if _, ok := parseSimple(""); ok {
		t.Error("empty simple should fail")
	}
	// A bare attribute compound (no tag/class/id) is a real, modelled
	// constraint on its own — e.g. the "[hidden]" idiom — so it must parse,
	// not be dropped as "reduces to nothing".
	if c, ok := parseSimple("[x]"); !ok || len(c.Attrs) != 1 || c.Attrs[0] != (attrMatch{Name: "x", Op: attrPresence}) {
		t.Errorf("bare attribute compound = %+v %v, want a presence check on \"x\"", c, ok)
	}
}

// TestPseudoElementMatchesNothing verifies that a compound carrying a pseudo-
// ELEMENT (::before/::after/… and their CSS2 single-colon spellings) matches no
// real element: the rule targets a generated box the engine does not synthesise,
// so its declarations must NOT be applied to the originating element. This is the
// fix for clearfix idioms like `.wrap::after{height:0;overflow:hidden}` wrongly
// collapsing the real `.wrap` element.
func TestPseudoElementMatchesNothing(t *testing.T) {
	wrap := el("div", "", "wrap")
	// Both the double-colon and legacy single-colon spellings, and a bare form.
	for _, sel := range []string{".wrap::after", ".wrap:after", ".wrap::before", "div::first-line", "p::marker"} {
		s, ok := parseComplex(sel)
		if !ok {
			t.Fatalf("parseComplex(%q) should parse (keeping the rule, matching nothing)", sel)
		}
		if !s.parts[len(s.parts)-1].PseudoElement {
			t.Errorf("%q: key compound should be flagged PseudoElement", sel)
		}
		if s.Matches(wrap) && sel == ".wrap::after" {
			t.Errorf("%q must not match the real .wrap element", sel)
		}
	}
	// A pseudo-CLASS on the same base still matches (only pseudo-ELEMENTS are
	// suppressed): guards against over-broadening the suppression.
	if s, _ := parseComplex(".wrap:first-child"); !s.Matches(wrap) {
		t.Error(".wrap:first-child (pseudo-class, unmodelled) should still match its base")
	}
	// isPseudoElement direct: a name that is not a pseudo-element returns false.
	if isPseudoElement("hover") || isPseudoElement("nth-child") {
		t.Error("pseudo-classes must not be classified as pseudo-elements")
	}
	if !isPseudoElement("after") || !isPseudoElement("-webkit-scrollbar") {
		t.Error("pseudo-elements must be classified as such")
	}
	// A real regression, found live on pkg.go.dev: "summary::-webkit-details-
	// marker,summary::marker{display:none}" (WebKit's vendor-prefixed name for
	// a <details>'s native disclosure triangle, always paired with the
	// standard ::marker for cross-browser coverage) hides a <details>'s
	// entire disclosure-triangle marker in a real browser. Before
	// "-webkit-details-marker" was added to isPseudoElement, it fell through
	// to "unmodelled pseudo, ignore it" — so "summary::-webkit-details-marker"
	// reduced to plain "summary" and WRONGLY matched the real <summary>
	// element, hiding its entire visible content (the SAME real element the
	// standard "summary::marker" alternative in the same rule correctly
	// leaves alone).
	summary := el("summary", "", "")
	if !isPseudoElement("-webkit-details-marker") {
		t.Error("-webkit-details-marker must be classified as a pseudo-element")
	}
	if s, ok := parseComplex("summary::-webkit-details-marker"); !ok {
		t.Fatal("parseComplex(summary::-webkit-details-marker) should parse")
	} else if s.Matches(summary) {
		t.Error("summary::-webkit-details-marker must not match the real <summary> element")
	}
	// The SAME bug class, found live on caniuse.com: `.ciu-search__input
	// ::-ms-clear{display:none}` (a theme hiding IE/Edge's own native
	// "clear field" X button) degraded to plain `.ciu-search__input` before
	// -ms-clear was added to isPseudoElement, WRONGLY matching the real
	// search <input> and hiding it entirely (Display computed to
	// DisplayNone) — not a rendering nuance, total invisibility.
	// -ms-reveal (the "show password" eye icon) is -ms-clear's own
	// near-inseparable sibling, added alongside it for the same reason.
	input := el("input", "feat_search", "ciu-search__input")
	if !isPseudoElement("-ms-clear") || !isPseudoElement("-ms-reveal") {
		t.Error("-ms-clear and -ms-reveal must both be classified as pseudo-elements")
	}
	if s, ok := parseComplex(".ciu-search__input::-ms-clear"); !ok {
		t.Fatal("parseComplex(.ciu-search__input::-ms-clear) should parse")
	} else if s.Matches(input) {
		t.Error(".ciu-search__input::-ms-clear must not match the real <input> element")
	}
}

// TestStripTrailingSelfCombinator covers stripTrailingSelfCombinator's own
// branches directly: the "X <comb> *" shape it recognises (across all four
// combinator kinds, since combinatorChar's non-descendant branches otherwise
// have no other test exercising them), and the shapes it must reject —
// nothing to strip, no combinator before the trailing "*", or a chain that
// doesn't end in a bare "*" at all.
// TestExtractAttrSelectorsEdgeCases covers the parsing edge cases direct
// end-to-end tests don't reach: an escaped bracket that must not be mistaken
// for an attribute selector's boundary, a quoted value containing a literal
// "]" (must not end the selector early), an unterminated "[" (kept literally
// rather than eating the rest of the compound), an empty attribute name
// (rejected), and the "s" case-sensitivity flag (the "i" flag's sibling).
func TestExtractAttrSelectorsEdgeCases(t *testing.T) {
	if rest, attrs := extractAttrSelectors(`a\[b`); rest != `a\[b` || attrs != nil {
		t.Errorf(`extractAttrSelectors(a\[b) = %q,%v, want the escaped bracket left untouched`, rest, attrs)
	}
	// An escaped quote INSIDE the brackets must not toggle the quote-tracking
	// state (only an unescaped matching quote closes the value).
	if rest, attrs := extractAttrSelectors(`a[data-x="a\"]b"]`); rest != "a" || len(attrs) != 1 {
		t.Errorf(`extractAttrSelectors(a[data-x="a\"]b"]) = %q,%+v`, rest, attrs)
	}
	rest, attrs := extractAttrSelectors(`a[data-x="a]b"]`)
	if rest != "a" || len(attrs) != 1 || attrs[0] != (attrMatch{Name: "data-x", Op: attrEqual, Value: "a]b"}) {
		t.Errorf(`extractAttrSelectors(a[data-x="a]b"]) = %q,%+v`, rest, attrs)
	}
	if rest, attrs := extractAttrSelectors("a[unterminated"); rest != "a[unterminated" || attrs != nil {
		t.Errorf("extractAttrSelectors(a[unterminated) = %q,%v, want kept literally", rest, attrs)
	}
	if _, ok := parseAttrSelector("=x"); ok {
		t.Error(`parseAttrSelector("=x") should fail: empty attribute name`)
	}
	if _, ok := parseAttrSelector("   "); ok {
		t.Error(`parseAttrSelector("   ") should fail: empty (bare presence with no name)`)
	}
	if am, ok := parseAttrSelector(`data-x=1 s`); !ok || am != (attrMatch{Name: "data-x", Op: attrEqual, Value: "1"}) {
		t.Errorf(`parseAttrSelector("data-x=1 s") = %+v,%v, want the "s" flag stripped`, am, ok)
	}
}

func TestStripTrailingSelfCombinator(t *testing.T) {
	cases := []struct {
		alt      string
		wantHead string
		wantComb byte
		wantOK   bool
	}{
		{".dark *", ".dark", ' ', true},
		{".dark>*", ".dark", '>', true},
		{".dark+*", ".dark", '+', true},
		{".dark~*", ".dark", '~', true},
		{".dark", "", 0, false},      // no combinator at all: bare compound
		{".dark .foo", "", 0, false}, // trailing token is not "*"
		{"*", "", 0, false},          // only one token, nothing to strip
	}
	for _, c := range cases {
		head, comb, ok := stripTrailingSelfCombinator(c.alt)
		if ok != c.wantOK || (ok && (head != c.wantHead || comb != c.wantComb)) {
			t.Errorf("stripTrailingSelfCombinator(%q) = %q,%q,%v want %q,%q,%v",
				c.alt, head, string(comb), ok, c.wantHead, string(c.wantComb), c.wantOK)
		}
	}
}
