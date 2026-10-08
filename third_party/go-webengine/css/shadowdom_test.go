// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package css

import (
	"testing"

	"github.com/go-webengine/engine/dom"
)

// parseAndCascade parses htmlSrc and returns the root plus its cascaded
// StyleMap, failing the test on a parse error.
func parseAndCascade(t *testing.T, htmlSrc string) (*dom.Node, StyleMap) {
	t.Helper()
	root, err := dom.Parse(htmlSrc)
	if err != nil {
		t.Fatal(err)
	}
	return root, Cascade(root)
}

func TestHostRuleAppliesToHostNotItsOwnTagRule(t *testing.T) {
	// The host element is itself a <div> — a plain (non-":host") shadow rule
	// matching "div" must NOT leak onto it (filterHostSelectors' whole
	// reason to exist); only the ":host" rule may.
	root, sm := parseAndCascade(t, `<body><div id="host">`+
		`<template shadowrootmode="open">`+
		`<style>div{color:blue}:host{color:red}</style>`+
		`<div class="inner">x</div>`+
		`</template>`+
		`</div></body>`)
	host := dom.Find(root, "div") // the outermost <div> is the host itself
	if host == nil || host.ID() != "host" {
		t.Fatal("host not found")
	}
	st := sm[host]
	if st == nil {
		t.Fatal("no style computed for host")
	}
	if st.Color != (Color{255, 0, 0, 255}) {
		t.Errorf("host color = %v, want red (from :host, not the shadow's own 'div' rule)", st.Color)
	}
}

func TestShadowStyleScopedToItsOwnTree(t *testing.T) {
	root, sm := parseAndCascade(t, `<body>`+
		`<div class="sibling">outside</div>`+
		`<my-elem>`+
		`<template shadowrootmode="open"><style>div{color:red}</style><div class="inner">x</div></template>`+
		`</my-elem>`+
		`</body>`)
	host := dom.Find(root, "my-elem")
	inner := host.Shadow.Children[1] // [0]=<style>, [1]=<div class="inner">
	if inner.Tag != "div" {
		t.Fatalf("shadow content = %v", host.Shadow.Children)
	}
	if st := sm[inner]; st == nil || st.Color != (Color{255, 0, 0, 255}) {
		t.Errorf("shadow-internal div color = %+v, want red", sm[inner])
	}
	sibling := dom.Find(root, "div") // the FIRST <div> in document order: the light-DOM sibling
	if sibling.Classes()[0] != "sibling" {
		t.Fatalf("expected the light-DOM sibling div, got %v", sibling)
	}
	if st := sm[sibling]; st == nil || st.Color == (Color{255, 0, 0, 255}) {
		t.Errorf("shadow-scoped 'div' rule leaked onto a light-DOM div outside the shadow tree: %+v", sm[sibling])
	}
}

func TestDocumentStyleDoesNotLeakIntoShadowTree(t *testing.T) {
	root, sm := parseAndCascade(t, `<body>`+
		`<style>div{background-color:rgb(0,128,0)}</style>`+
		`<my-elem>`+
		`<template shadowrootmode="open"><div class="inner">x</div></template>`+
		`</my-elem>`+
		`</body>`)
	host := dom.Find(root, "my-elem")
	inner := host.Shadow.Children[0]
	st := sm[inner]
	if st == nil {
		t.Fatal("no style for shadow-internal div")
	}
	// background-color is not inherited, so the only way it could be green is
	// if the document's "div" rule wrongly matched inside the shadow tree.
	if st.Background.A != 0 {
		t.Errorf("document rule leaked into shadow tree: background = %+v, want transparent", st.Background)
	}
}

func TestSlottedContentKeepsDocumentStylingNotShadowStyling(t *testing.T) {
	root, sm := parseAndCascade(t, `<body>`+
		`<style>span{color:rgb(0,128,0)}</style>`+
		`<my-elem>`+
		`<template shadowrootmode="open"><style>span{color:blue}</style><slot></slot></template>`+
		`<span>light</span>`+
		`</my-elem>`+
		`</body>`)
	span := dom.Find(root, "span")
	st := sm[span]
	if st == nil {
		t.Fatal("no style for the slotted span")
	}
	if st.Color != (Color{0, 128, 0, 255}) {
		t.Errorf("slotted span color = %+v, want the DOCUMENT's green, not the shadow's blue", st.Color)
	}
}

func TestHostAttributeTogglesSlotVisibility(t *testing.T) {
	// The real idiom both confirmed sites use: closed by default, revealed by
	// an attribute the settle loop's re-cascade must pick up (see
	// TestHostAttributeToggleAcrossReCascade for the two-pass version of this).
	src := `<body><my-elem>` +
		`<template shadowrootmode="open"><style>:host(:not([open])) slot{display:none}</style><slot></slot></template>` +
		`<span>content</span>` +
		`</my-elem></body>`
	root, sm := parseAndCascade(t, src)
	host := dom.Find(root, "my-elem")
	slot := host.Shadow.Children[1]
	if slot.Tag != "slot" {
		t.Fatalf("shadow content = %v", host.Shadow.Children)
	}
	if st := sm[slot]; st == nil || st.Display != DisplayNone {
		t.Errorf("slot display = %+v, want none (host lacks [open])", sm[slot])
	}
}

func TestHostAttributeToggleAcrossReCascade(t *testing.T) {
	// Simulates the settle loop: a script sets an attribute on the host (e.g.
	// in response to a click) between two cascade passes over the SAME DOM,
	// exactly what engine/dynamic.go's settle loop does by re-calling
	// CascadeVW after a script mutation. Point 4 of the task: the shadow
	// tree's cascade must react on the SECOND pass, not just the first.
	root, err := dom.Parse(`<body><my-elem>` +
		`<template shadowrootmode="open"><style>:host(:not([open])) slot{display:none}</style><slot></slot></template>` +
		`</my-elem></body>`)
	if err != nil {
		t.Fatal(err)
	}
	host := dom.Find(root, "my-elem")
	slot := host.Shadow.Children[1]

	sm1 := Cascade(root)
	if st := sm1[slot]; st == nil || st.Display != DisplayNone {
		t.Fatalf("pass 1: slot display = %+v, want none", sm1[slot])
	}

	host.Attr["open"] = ""
	sm2 := Cascade(root)
	if st := sm2[slot]; st == nil || st.Display == DisplayNone {
		t.Fatalf("pass 2 (host now has [open]): slot display = %+v, want NOT none", sm2[slot])
	}
}

func TestNestedShadowTreeStyleDoesNotLeakToOuterShadowTree(t *testing.T) {
	root, sm := parseAndCascade(t, `<body><outer-elem>`+
		`<template shadowrootmode="open">`+
		`<style>span{color:red}</style>`+
		`<inner-elem><template shadowrootmode="open"><span class="deep">x</span></template></inner-elem>`+
		`</template>`+
		`</outer-elem></body>`)
	outer := dom.Find(root, "outer-elem")
	inner := outer.Shadow.Children[1]
	deepSpan := inner.Shadow.Children[0]
	if deepSpan.Tag != "span" {
		t.Fatalf("nested shadow content = %v", inner.Shadow.Children)
	}
	if st := sm[deepSpan]; st == nil || st.Color == (Color{255, 0, 0, 255}) {
		t.Errorf("outer shadow tree's 'span' rule leaked into the NESTED shadow tree: %+v", sm[deepSpan])
	}
}

func TestPartSelectorReachesIntoShadowTree(t *testing.T) {
	// The real shape found live on developer.mozilla.org:
	// `.wrap my-elem::part(button){...}` styling a real, opted-in
	// `part="button"` element inside a custom element's declarative shadow
	// root, from an OUTER document stylesheet.
	root, sm := parseAndCascade(t, `<body><style>.wrap my-elem::part(button){color:rgb(255,0,0)}</style>`+
		`<div class="wrap"><my-elem>`+
		`<template shadowrootmode="open"><button part="button" class="btn">x</button></template>`+
		`</my-elem></div>`+
		`</body>`)
	host := dom.Find(root, "my-elem")
	btn := host.Shadow.Children[0]
	if btn.Tag != "button" {
		t.Fatalf("shadow content = %v", host.Shadow.Children)
	}
	if st := sm[btn]; st == nil || st.Color != (Color{255, 0, 0, 255}) {
		t.Errorf("::part(button) did not reach the shadow-internal button: %+v", sm[btn])
	}
}

func TestPartSelectorRequiresMatchingPartAttribute(t *testing.T) {
	// A sibling shadow-internal element WITHOUT part="button" must be
	// unaffected — ::part() opts in per-element, it is not "style everything
	// inside this host's shadow tree".
	root, sm := parseAndCascade(t, `<body><style>my-elem::part(button){color:rgb(255,0,0)}</style>`+
		`<my-elem><template shadowrootmode="open">`+
		`<button part="button" class="a">a</button><button class="b">b</button>`+
		`</template></my-elem>`+
		`</body>`)
	host := dom.Find(root, "my-elem")
	withPart, withoutPart := host.Shadow.Children[0], host.Shadow.Children[1]
	if st := sm[withPart]; st == nil || st.Color != (Color{255, 0, 0, 255}) {
		t.Errorf("part=\"button\" element not styled: %+v", sm[withPart])
	}
	if st := sm[withoutPart]; st == nil || st.Color == (Color{255, 0, 0, 255}) {
		t.Errorf("plain button (no part attribute) wrongly styled by ::part(button): %+v", sm[withoutPart])
	}
}

func TestPartSelectorRequiresMatchingHostTag(t *testing.T) {
	// The compound's own tag ("other-elem") must match the HOST, not just any
	// host — a ::part(button) written for a different custom element must not
	// reach into an unrelated one's shadow tree.
	root, sm := parseAndCascade(t, `<body><style>other-elem::part(button){color:rgb(255,0,0)}</style>`+
		`<my-elem><template shadowrootmode="open"><button part="button">x</button></template></my-elem>`+
		`</body>`)
	host := dom.Find(root, "my-elem")
	btn := host.Shadow.Children[0]
	if st := sm[btn]; st == nil || st.Color == (Color{255, 0, 0, 255}) {
		t.Errorf("::part(button) wrongly reached across to an unrelated host tag: %+v", sm[btn])
	}
}

func TestPartAttributeIgnoredOutsideAnyShadowTree(t *testing.T) {
	// A bare "part" HTML attribute on an ordinary light-DOM element is
	// meaningless without a shadow tree to expose it from — a document-level
	// "::part(button)" rule (nonsensical CSS, but must not crash or wrongly
	// match) has no host at all in that scope, so it must match nothing.
	root, sm := parseAndCascade(t, `<body><style>x::part(button){color:rgb(255,0,0)}</style>`+
		`<button part="button" class="x">x</button>`+
		`</body>`)
	btn := dom.Find(root, "button")
	if st := sm[btn]; st == nil || st.Color == (Color{255, 0, 0, 255}) {
		t.Errorf("::part() wrongly matched a light-DOM element outside any shadow tree: %+v", sm[btn])
	}
}

func TestPlainPageUnaffectedByShadowScoping(t *testing.T) {
	// A page with no Shadow DOM at all must cascade exactly as before: host
	// stays nil throughout, so every MatchesHost call is Matches.
	_, sm := parseAndCascade(t, `<html><body><h1>x</h1></body></html>`)
	var found bool
	for n, st := range sm {
		if n.Tag == "h1" {
			found = true
			if st.Display != DisplayBlock || st.FontSize != 32 {
				t.Errorf("h1 style regressed: %+v", st)
			}
		}
	}
	if !found {
		t.Fatal("no h1 style computed")
	}
}
