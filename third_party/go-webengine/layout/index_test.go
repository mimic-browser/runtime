// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package layout

import (
	"testing"

	"github.com/go-webengine/engine/dom"
)

func TestRectUnion(t *testing.T) {
	z := Rect{}
	a := Rect{X: 10, Y: 10, W: 20, H: 20}
	b := Rect{X: 40, Y: 5, W: 10, H: 40}

	if got := z.union(a); got != a {
		t.Fatalf("zero.union(a) = %+v, want %+v", got, a)
	}
	if got := a.union(z); got != a {
		t.Fatalf("a.union(zero) = %+v, want %+v", got, a)
	}
	// Union covers both: left=10, top=5, right=max(30,50)=50, bottom=max(30,45)=45.
	want := Rect{X: 10, Y: 5, W: 40, H: 40}
	if got := a.union(b); got != want {
		t.Fatalf("a.union(b) = %+v, want %+v", got, want)
	}
	if got := z.union(z); got != z {
		t.Fatalf("zero.union(zero) = %+v, want zero", got)
	}
}

func TestMinMax2(t *testing.T) {
	if min2(1, 2) != 1 || min2(2, 1) != 1 {
		t.Fatal("min2 wrong")
	}
	if max2(1, 2) != 2 || max2(2, 1) != 2 {
		t.Fatal("max2 wrong")
	}
}

func TestItemRect(t *testing.T) {
	word := &InlineItem{X: 3, Y: 4, Width: 12, LineHeight: 18}
	if got := itemRect(word); got != (Rect{X: 3, Y: 4, W: 12, H: 18}) {
		t.Fatalf("word itemRect = %+v", got)
	}
	// An image taller than the line height uses the image height.
	img := &InlineItem{X: 0, Y: 0, Width: 40, LineHeight: 18, Image: &dom.Node{}, ImgH: 60}
	if got := itemRect(img); got.H != 60 {
		t.Fatalf("image itemRect H = %v, want 60", got.H)
	}
	// A short image keeps the line height.
	short := &InlineItem{X: 0, Y: 0, Width: 40, LineHeight: 18, Image: &dom.Node{}, ImgH: 5}
	if got := itemRect(short); got.H != 18 {
		t.Fatalf("short image itemRect H = %v, want 18", got.H)
	}
}

func TestElementParent(t *testing.T) {
	if elementParent(&dom.Node{Type: dom.Element}) != nil {
		t.Fatal("no parent should be nil")
	}
	el := &dom.Node{Type: dom.Element, Tag: "div"}
	txt := &dom.Node{Type: dom.Text, Parent: el}
	child := &dom.Node{Type: dom.Element, Tag: "span", Parent: txt}
	// child's parent is a text node → skip to the div.
	if got := elementParent(child); got != el {
		t.Fatalf("elementParent skipped-text = %v, want div", got)
	}
}

func TestElementParentBridgesShadowHost(t *testing.T) {
	// A node at the top of an attached shadow tree has Parent == nil (see
	// attachDeclarativeShadowRoots) but ShadowHost set — elementParent must
	// bridge to the host instead of stopping, so BuildIndex's own attribution
	// walk (the only caller) can reach a shadow host that generates no Box of
	// its own (a plain display:inline custom element).
	host := &dom.Node{Type: dom.Element, Tag: "my-widget"}
	shadowTop := &dom.Node{Type: dom.Element, Tag: "span", ShadowHost: host}
	if got := elementParent(shadowTop); got != host {
		t.Fatalf("elementParent(shadow top-level node) = %v, want host", got)
	}
	// A genuinely detached, non-shadow node (no Parent, no ShadowHost) still
	// has no parent at all.
	detached := &dom.Node{Type: dom.Element, Tag: "div"}
	if got := elementParent(detached); got != nil {
		t.Fatalf("elementParent(detached) = %v, want nil", got)
	}
}

func TestBuildIndexShadowHostWithNoOwnBox(t *testing.T) {
	// A plain display:inline custom element hosting a declarative shadow root
	// (the spec default for one with no author styling) contributes no Box of
	// its own — see layouter.appendElementInline's "plain inline element"
	// default case. Its content becomes an InlineItem whose Node sits at the
	// TOP of the shadow tree (Parent nil, ShadowHost set). Before
	// elementParent bridged Parent==nil+ShadowHost!=nil back to the host, this
	// attribution walk stopped dead at the shadow node and the host never
	// received ANY rect entry at all — getComputedStyle/getBoundingClientRect
	// then reported it as display:none regardless of its real, painted
	// content (confirmed live on developer.mozilla.org's own
	// `<mdn-sidebar-filter>` search widget).
	body := &dom.Node{Type: dom.Element, Tag: "body"}
	host := &dom.Node{Type: dom.Element, Tag: "my-widget", Parent: body}
	shadowSpan := &dom.Node{Type: dom.Element, Tag: "span", ShadowHost: host}

	box := &Box{
		Node: body, X: 0, Y: 0, W: 300, H: 40,
		Lines: []*LineBox{{
			Items: []*InlineItem{
				{Node: shadowSpan, X: 10, Y: 5, Width: 80, LineHeight: 20},
			},
		}},
	}
	idx := BuildIndex(box)
	if r := idx[shadowSpan]; r != (Rect{X: 10, Y: 5, W: 80, H: 20}) {
		t.Fatalf("shadow span rect = %+v", r)
	}
	if r := idx[host]; r != (Rect{X: 10, Y: 5, W: 80, H: 20}) {
		t.Fatalf("host rect = %+v, want the union bridged in from its shadow content", r)
	}
	// body keeps its own authoritative block rect, untouched by the bridge.
	if r := idx[body]; r != (Rect{X: 0, Y: 0, W: 300, H: 40}) {
		t.Fatalf("body rect = %+v", r)
	}
}

func TestBuildIndexNil(t *testing.T) {
	if got := BuildIndex(nil); len(got) != 0 {
		t.Fatalf("BuildIndex(nil) = %v", got)
	}
}

func TestBuildIndexBlockAndInline(t *testing.T) {
	// Build a small box tree: a block <div> containing a line with two inline
	// fragments — a word in a <span> and a plain text word (Node=div).
	// InlineItem.Node is the element that produced the run (a text run's immediate
	// parent element), matching what the layout engine records.
	div := &dom.Node{Type: dom.Element, Tag: "div"}
	span := &dom.Node{Type: dom.Element, Tag: "span", Parent: div}

	box := &Box{
		Node: div, X: 0, Y: 0, W: 300, H: 40,
		Lines: []*LineBox{{
			Items: []*InlineItem{
				{Node: span, X: 0, Y: 0, Width: 50, LineHeight: 20},
				{Node: div, X: 60, Y: 0, Width: 40, LineHeight: 20},
			},
		}},
	}
	idx := BuildIndex(box)

	// The block div keeps its authoritative border-box rect.
	if r := idx[div]; r != (Rect{X: 0, Y: 0, W: 300, H: 40}) {
		t.Fatalf("div rect = %+v, want the box rect", r)
	}
	// The inline <span> gets the union of its fragment(s).
	if r := idx[span]; r != (Rect{X: 0, Y: 0, W: 50, H: 20}) {
		t.Fatalf("span rect = %+v", r)
	}
}

func TestBuildIndexInlineStopsAtBlockAncestor(t *testing.T) {
	// A nested block: outer block box contains an inner block box which owns a
	// line. The inline fragment must NOT be unioned up into the inner (or outer)
	// block node — those keep their own authoritative rects.
	outer := &dom.Node{Type: dom.Element, Tag: "section"}
	inner := &dom.Node{Type: dom.Element, Tag: "p", Parent: outer}

	innerBox := &Box{Node: inner, X: 5, Y: 5, W: 100, H: 20,
		Lines: []*LineBox{{Items: []*InlineItem{{Node: inner, X: 5, Y: 5, Width: 30, LineHeight: 20}}}}}
	outerBox := &Box{Node: outer, X: 0, Y: 0, W: 200, H: 30, Children: []*Box{innerBox}}

	idx := BuildIndex(outerBox)
	if idx[inner] != (Rect{X: 5, Y: 5, W: 100, H: 20}) {
		t.Fatalf("inner block rect overwritten: %+v", idx[inner])
	}
	if idx[outer] != (Rect{X: 0, Y: 0, W: 200, H: 30}) {
		t.Fatalf("outer block rect overwritten: %+v", idx[outer])
	}
}
