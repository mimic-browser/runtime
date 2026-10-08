// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package dom

import "testing"

// parseShadow is a small helper: Parse wraps htmlSrc's body content for these
// tests (Parse already returns a full document tree; callers use Find to
// reach the element they care about).
func parseShadow(t *testing.T, htmlSrc string) *Node {
	t.Helper()
	root, err := Parse(htmlSrc)
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestDeclarativeShadowRootOpen(t *testing.T) {
	root := parseShadow(t, `<body><my-elem>`+
		`<template shadowrootmode="open"><div class="wrap"><slot></slot></div></template>`+
		`<span>light child</span>`+
		`</my-elem></body>`)
	host := Find(root, "my-elem")
	if host == nil {
		t.Fatal("no <my-elem>")
	}
	if host.Shadow == nil {
		t.Fatal("shadow root not attached")
	}
	if host.Shadow.Host != host {
		t.Errorf("Shadow.Host = %v, want host itself", host.Shadow.Host)
	}
	// The <template> itself must not remain a light-DOM child.
	for _, c := range host.Children {
		if c.Type == Element && c.Tag == "template" {
			t.Error("template still present in light DOM")
		}
	}
	// The light-DOM <span> child remains (unslotted rendering is a
	// layout-level concern, not a DOM-attachment one).
	span := Find(host, "span")
	if span == nil || span.Parent != host {
		t.Fatal("light-DOM span child missing or reparented")
	}
	if len(host.Shadow.Children) != 1 || host.Shadow.Children[0].Tag != "div" {
		t.Fatalf("shadow content = %v", host.Shadow.Children)
	}
	div := host.Shadow.Children[0]
	if div.Parent != nil {
		t.Errorf("top-level shadow node Parent = %v, want nil", div.Parent)
	}
	if div.ShadowHost != host {
		t.Errorf("div.ShadowHost = %v, want host", div.ShadowHost)
	}
	slot := Find(div, "slot")
	if slot == nil {
		t.Fatal("no <slot> in shadow content")
	}
	if slot.ShadowHost != host {
		t.Errorf("slot.ShadowHost = %v, want host", slot.ShadowHost)
	}
	if slot.Parent != div {
		t.Errorf("slot.Parent = %v, want div (normal internal parent chain)", slot.Parent)
	}
}

func TestDeclarativeShadowRootClosed(t *testing.T) {
	root := parseShadow(t, `<body><my-elem>`+
		`<template shadowrootmode="closed"><p>closed content</p></template>`+
		`</my-elem></body>`)
	host := Find(root, "my-elem")
	if host.Shadow == nil {
		t.Fatal("closed shadow root not attached")
	}
	// "closed" is rendered identically to "open" — no mode is recorded.
	if len(host.Shadow.Children) != 1 || host.Shadow.Children[0].Tag != "p" {
		t.Fatalf("shadow content = %v", host.Shadow.Children)
	}
}

func TestTemplateWithoutShadowRootModeStaysPlainTemplate(t *testing.T) {
	root := parseShadow(t, `<body><my-elem><template><p>inert</p></template></my-elem></body>`)
	host := Find(root, "my-elem")
	if host.Shadow != nil {
		t.Fatal("shadow root attached without shadowrootmode")
	}
	tmpl := Find(host, "template")
	if tmpl == nil {
		t.Fatal("plain <template> should remain a light-DOM child")
	}
	// A <template>'s real content lives in its Content fragment, never in its
	// own Children (see Node.Content's doc comment) — this originally
	// asserted the content sat directly in tmpl.Children, which quietly
	// encoded the old, spec-incomplete model rather than testing the real
	// <template shadowrootmode> vs plain <template> distinction it's named
	// for; corrected to check both halves of the real shape.
	if len(tmpl.Children) != 0 {
		t.Fatalf("template's own Children = %v, want none (content lives in .Content)", tmpl.Children)
	}
	if tmpl.Content == nil || len(tmpl.Content.Children) != 1 || tmpl.Content.Children[0].Tag != "p" {
		t.Fatalf("template content = %v", tmpl.Content)
	}
}

func TestTemplateWithInvalidShadowRootModeStaysPlainTemplate(t *testing.T) {
	root := parseShadow(t, `<body><my-elem><template shadowrootmode="bogus"><p>x</p></template></my-elem></body>`)
	host := Find(root, "my-elem")
	if host.Shadow != nil {
		t.Fatal("shadow root attached for an invalid shadowrootmode value")
	}
}

func TestTemplateNotFirstElementChildIsNotDeclarativeShadowRoot(t *testing.T) {
	root := parseShadow(t, `<body><my-elem><div>first</div>`+
		`<template shadowrootmode="open"><p>x</p></template></my-elem></body>`)
	host := Find(root, "my-elem")
	if host.Shadow != nil {
		t.Fatal("shadow root attached when template was not the first element child")
	}
	// Both children remain in the light DOM, untouched.
	if len(host.Children) != 2 {
		t.Fatalf("host.Children = %v", host.Children)
	}
}

func TestDeclarativeShadowRootSkipsLeadingWhitespaceText(t *testing.T) {
	// A text-only (whitespace) node before the template does not disqualify
	// it: firstElementChild looks at the first ELEMENT child, matching how a
	// real parser is not tripped up by insignificant whitespace.
	root := parseShadow(t, "<body><my-elem>\n  <template shadowrootmode=\"open\"><p>x</p></template></my-elem></body>")
	host := Find(root, "my-elem")
	if host.Shadow == nil {
		t.Fatal("shadow root not attached past leading whitespace text")
	}
}

func TestNestedDeclarativeShadowRoots(t *testing.T) {
	root := parseShadow(t, `<body><outer-elem>`+
		`<template shadowrootmode="open">`+
		`<inner-elem><template shadowrootmode="open"><em>nested</em></template></inner-elem>`+
		`</template>`+
		`</outer-elem></body>`)
	outer := Find(root, "outer-elem")
	if outer.Shadow == nil {
		t.Fatal("outer shadow root not attached")
	}
	// Find is a light-DOM-only utility (deliberately: see its doc comment's
	// intent as a generic DOM helper) and does not descend into a Shadow
	// tree, so inner-elem is read directly off outer's shadow content rather
	// than located via Find(outer, "inner-elem").
	if len(outer.Shadow.Children) != 1 || outer.Shadow.Children[0].Tag != "inner-elem" {
		t.Fatalf("outer shadow content = %v", outer.Shadow.Children)
	}
	inner := outer.Shadow.Children[0]
	if inner.ShadowHost != outer {
		t.Errorf("inner-elem.ShadowHost = %v, want outer", inner.ShadowHost)
	}
	if inner.Shadow == nil {
		t.Fatal("nested shadow root not attached")
	}
	if inner.Shadow.Host != inner {
		t.Errorf("inner.Shadow.Host = %v, want inner itself", inner.Shadow.Host)
	}
	em := inner.Shadow.Children[0]
	if em.Tag != "em" {
		t.Fatalf("nested shadow content = %v", inner.Shadow.Children)
	}
	// The nested shadow content's ShadowHost must be the INNER host, not the
	// outer one it was transiently marked with before being hoisted.
	if em.ShadowHost != inner {
		t.Errorf("em.ShadowHost = %v, want inner (not outer)", em.ShadowHost)
	}
}

func TestElementWithNoChildrenHasNoFirstElementChild(t *testing.T) {
	root := parseShadow(t, `<body><br></body>`)
	br := Find(root, "br")
	if br == nil {
		t.Fatal("no <br>")
	}
	if got := firstElementChild(br); got != nil {
		t.Errorf("firstElementChild(<br>) = %v, want nil", got)
	}
	if br.Shadow != nil {
		t.Error("void element unexpectedly grew a shadow root")
	}
}

func TestPlainElementHasNilShadowFields(t *testing.T) {
	root := parseShadow(t, `<body><p>hi</p></body>`)
	p := Find(root, "p")
	if p.Shadow != nil || p.ShadowHost != nil {
		t.Errorf("plain element has shadow fields set: Shadow=%v ShadowHost=%v", p.Shadow, p.ShadowHost)
	}
}
