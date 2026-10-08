// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package engine

import (
	"context"
	"image"
	"strings"
	"testing"
)

// End-to-end tests for declarative Shadow DOM + <slot> projection + :host CSS
// scoping — the full pipeline (dom attach -> layout projection -> cascade
// scoping -> settle-loop re-cascade) exercised through the public engine API,
// on top of the package-level unit tests in dom/, layout/ and css/. These are
// the confirmed root cause (this session) of developer.mozilla.org's and
// github.com's header mega-menus rendering permanently expanded instead of
// hidden-until-interaction; see FIDELITY.md and cmd/render for the live
// verification against those two real pages.

func renderInfoOnly(t *testing.T, e *Engine, src string, w, h int) *RenderInfo {
	t.Helper()
	_, info, err := e.RenderHTML(context.Background(), src, "https://demo.test/", image.Rect(0, 0, w, h))
	if err != nil {
		t.Fatal(err)
	}
	return info
}

func TestShadowDOMSlotProjectionRendersAssignedLightContent(t *testing.T) {
	src := `<html><body><my-elem>` +
		`<template shadowrootmode="open"><div><slot></slot></div></template>` +
		`<p style="font-size:80px;margin:0">SLOTTED</p>` +
		`</my-elem></body></html>`
	e := New()
	e.DisableJS = true
	info := renderInfoOnly(t, e, src, 400, 100)
	if info.ContentHeight < 80 {
		t.Fatalf("slotted <p> (font-size 80px) should have been laid out inside the shadow tree's <div><slot>, contentHeight=%d", info.ContentHeight)
	}
}

func TestShadowDOMUnslottedLightContentNeverRenders(t *testing.T) {
	// The shadow tree has NO <slot> at all: per spec, a host's light-DOM
	// children are then never rendered anywhere, however large.
	src := `<html><body><my-elem>` +
		`<template shadowrootmode="open"><div>shadow-only, no slot here</div></template>` +
		`<p style="font-size:400px">SHOULD-NOT-RENDER</p>` +
		`</my-elem></body></html>`
	e := New()
	e.DisableJS = true
	info := renderInfoOnly(t, e, src, 400, 100)
	if info.ContentHeight > 100 {
		t.Fatalf("unslotted light content should never render: contentHeight=%d, want roughly one shadow-tree line", info.ContentHeight)
	}
}

func TestShadowDOMHostClosedByDefaultHidesSlot(t *testing.T) {
	// The exact idiom both confirmed real sites use: hidden until an
	// interactive [open] state is toggled. With no script at all, the host
	// never gains [open], so the slot (and everything projected into it)
	// stays display:none for the whole render.
	src := `<html><body><my-elem>` +
		`<template shadowrootmode="open">` +
		`<style>:host(:not([open])) slot{display:none}</style><slot></slot>` +
		`</template>` +
		`<p style="font-size:120px;margin:0">CONTENT</p>` +
		`</my-elem></body></html>`
	e := New()
	e.DisableJS = true
	info := renderInfoOnly(t, e, src, 400, 100)
	if info.ContentHeight > 100 {
		t.Fatalf("slot should be display:none while host lacks [open]: contentHeight=%d", info.ContentHeight)
	}
}

func TestShadowDOMHostAttributeTogglesSlotAcrossSettleLoop(t *testing.T) {
	// Point 4 of the shadow-DOM task: a script mutating the HOST's attribute
	// (exactly what a real click handler flipping [open] would do) must be
	// picked up by the settle loop's ordinary re-cascade against the shadow
	// tree too, not just the light DOM. No special wiring exists for this —
	// it works because cascade recomputes shadow-tree rule scoping fresh on
	// every CascadeVW call (see css/cascade.go), and the settle loop already
	// re-cascades the whole document after a script mutation.
	src := `<html><body><my-elem>` +
		`<template shadowrootmode="open">` +
		`<style>:host(:not([open])) slot{display:none}</style><slot></slot>` +
		`</template>` +
		`<p style="font-size:120px;margin:0">CONTENT</p>` +
		`</my-elem>` +
		`<script>document.querySelector('my-elem').setAttribute('open', '');</script>` +
		`</body></html>`
	e := New() // JS enabled (the default)
	info := renderInfoOnly(t, e, src, 400, 100)
	if info.ContentHeight < 120 {
		t.Fatalf("after the script sets [open], the slot should be visible again: contentHeight=%d, want >= 120", info.ContentHeight)
	}
}

func TestShadowDOMNamedSlotDistribution(t *testing.T) {
	src := `<html><body><my-elem>` +
		`<template shadowrootmode="open">` +
		`<div><slot name="footer"></slot></div>` + // only the named slot exists
		`</template>` +
		`<p slot="footer" style="font-size:60px;margin:0">FOOTER</p>` +
		`<p style="font-size:400px">UNNAMED-NOT-RENDERED</p>` + // no "slot" attr, no default slot exists
		`</my-elem></body></html>`
	e := New()
	e.DisableJS = true
	info := renderInfoOnly(t, e, src, 400, 100)
	if info.ContentHeight < 60 {
		t.Fatalf("the slot=\"footer\" paragraph should render (~60px), contentHeight=%d", info.ContentHeight)
	}
	if info.ContentHeight > 150 {
		t.Fatalf("the unnamed 400px paragraph must NOT render (no default slot exists): contentHeight=%d", info.ContentHeight)
	}
}

func TestShadowDOMPlainPageUnaffected(t *testing.T) {
	// A page with no Shadow DOM at all renders exactly as before this feature
	// existed — this is the same fixture TestTemplateContentNotRendered (the
	// prior, narrower fix this builds on) already covers; repeated here to
	// pin it at the shadow-DOM-feature level too.
	src := `<html><body><p>before</p>` +
		`<template><p style="font-size:400px">SHOULD-NOT-RENDER</p></template>` +
		`<p>after</p></body></html>`
	e := New()
	e.DisableJS = true
	info := renderInfoOnly(t, e, src, 400, 100)
	if info.ContentHeight > 100 {
		t.Fatalf("plain <template> (no shadowrootmode) should still be inert: contentHeight=%d", info.ContentHeight)
	}
}

// TestShadowDOMInlineSVGIsDiscoveredForRasterization guards loadImages's own
// replaced-element walk (images.go), a SEPARATE tree walk from the layout/
// cascade ones already covered above — before it also descended into
// n.Shadow.Children, an inline <svg> that lived only in a host's declarative
// shadow content was never added to the fetch/rasterize job list at all, so
// it painted as empty space regardless of how correctly layout positioned its
// box or cascade computed its style. Confirmed root cause, live, of
// developer.mozilla.org's top-nav <mdn-search-button> rendering as an empty
// pill with no magnifying-glass icon (round 55; see FIDELITY.md) — this
// fixture reproduces the same shape (a custom element's declarative shadow
// root whose content is an inline <svg>) minimally, independent of that
// page's own additional, separately-scoped icon-only-<button> sizing
// limitation (see layout.formControlDefaultSize's "button" case), which is
// why the host here is a plain custom element, not a <button>.
func TestShadowDOMInlineSVGIsDiscoveredForRasterization(t *testing.T) {
	src := `<html><body style="margin:0;background:#000">` +
		`<my-icon><template shadowrootmode="open">` +
		`<svg width="20" height="20" viewBox="0 0 20 20"><rect width="20" height="20" fill="#ff0000"/></svg>` +
		`</template></my-icon></body></html>`
	img := renderHTMLTest(t, src, 40, 40)
	assertPixel(t, img, 10, 10, 0xff, 0x00, 0x00, "shadow-hosted inline <svg> should rasterize its red rect, not paint as empty background")
}

// TestShadowHostGetComputedStyleAndRect guards layout.BuildIndex's own
// inline-item-to-element attribution walk (layout/index.go's elementParent),
// a SEPARATE mechanism from every fixture above: a plain display:inline
// custom element (the spec default for one with no author styling) hosting a
// declarative shadow root contributes no Box of its own — its content is an
// InlineItem whose Node sits at the TOP of the shadow tree, whose Parent is
// nil (see attachDeclarativeShadowRoots — it is the root of a separate tree).
// Before elementParent bridged Parent==nil+ShadowHost!=nil back to the host,
// this walk stopped dead at the shadow node and the host never received ANY
// rect entry in the whole document — getComputedStyle/getBoundingClientRect
// then reported it as display:none/an all-zero rect regardless of its real,
// painted shadow content. Confirmed root cause, live, of the SAME shape on
// developer.mozilla.org's own `<mdn-sidebar-filter>` search widget (though
// that specific element also sits behind an unrelated, correctly-evaluated
// responsive breakpoint at this session's own 1024px bench viewport, so this
// fix has no visible bench effect on that page — see FIDELITY.md).
func TestShadowHostGetComputedStyleAndRect(t *testing.T) {
	src := `<html><body>` +
		`<my-widget><template shadowrootmode="open"><span>shadow content</span></template></my-widget>` +
		`<script>
			var el = document.querySelector('my-widget');
			console.log('RESULT:display=' + getComputedStyle(el).display);
			var r = el.getBoundingClientRect();
			console.log('RESULT:rect=' + r.width + 'x' + r.height);
		</script>` +
		`</body></html>`
	e := New()
	var logs []string
	e.JSLog = func(s string) { logs = append(logs, s) }
	if _, _, err := e.RenderHTML(context.Background(), src, "https://demo.test/", image.Rect(0, 0, 400, 100)); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(logs, "\n")
	if !strings.Contains(joined, "RESULT:display=inline") {
		t.Fatalf("shadow host's own computed display should be inline (the spec default), got logs:\n%s", joined)
	}
	if strings.Contains(joined, "RESULT:rect=0x0") {
		t.Fatalf("shadow host's own bounding rect should reflect its real shadow content, not be all-zero:\n%s", joined)
	}
}
