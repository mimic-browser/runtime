// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package engine

import (
	"context"
	"image"
	"net/url"
	"testing"

	"github.com/go-webengine/engine/dom"
	"github.com/go-webengine/engine/layout"
)

// anchor builds an <a href> element with a single text child whose Parent chain
// climbs to the anchor, matching what the layouter records on an inline atom.
func anchor(href, text string) (*dom.Node, *dom.Node) {
	a := &dom.Node{Type: dom.Element, Tag: "a", Attr: map[string]string{"href": href}}
	t := &dom.Node{Type: dom.Text, Text: text, Parent: a}
	a.Children = []*dom.Node{t}
	return a, t
}

// word is an inline text atom positioned in full-page space.
func word(node *dom.Node, x, y, w float64) *layout.InlineItem {
	return &layout.InlineItem{Text: "w", Node: node, X: x, Y: y, Width: w, LineHeight: 20}
}

func TestLinksFromBox_InlineUnionAndResolve(t *testing.T) {
	a, txt := anchor("/next", "click here") // relative href → resolved against base
	// Two atoms of the same anchor on two lines: the union must bound both.
	l1 := &layout.LineBox{Items: []*layout.InlineItem{word(txt, 10, 5, 50)}}
	l2 := &layout.LineBox{Items: []*layout.InlineItem{word(txt, 5, 25, 40)}}
	// Nest the lines inside a child box to exercise the recursive walk.
	child := &layout.Box{Node: a, Lines: []*layout.LineBox{l1, l2}}
	root := &layout.Box{Children: []*layout.Box{child}}

	links := LinksFromBox(root, "http://ex.com/dir/page")
	if len(links) != 1 {
		t.Fatalf("want 1 link, got %d: %+v", len(links), links)
	}
	if links[0].Href != "http://ex.com/next" {
		t.Errorf("href = %q, want http://ex.com/next", links[0].Href)
	}
	// union of (10,5)-(60,25) and (5,25)-(45,45) = (5,5)-(60,45).
	want := image.Rect(5, 5, 60, 45)
	if links[0].Rect != want {
		t.Errorf("rect = %v, want %v", links[0].Rect, want)
	}
}

func TestLinksFromBox_ImageLink(t *testing.T) {
	a := &dom.Node{Type: dom.Element, Tag: "a", Attr: map[string]string{"href": "http://img.example/"}}
	img := &dom.Node{Type: dom.Element, Tag: "img", Parent: a}
	a.Children = []*dom.Node{img}
	it := &layout.InlineItem{Image: img, Node: img, X: 4, Y: 8, Width: 20, ImgW: 20, ImgH: 30, LineHeight: 12}
	root := &layout.Box{Lines: []*layout.LineBox{{Items: []*layout.InlineItem{it}}}}

	links := LinksFromBox(root, "http://ex.com/")
	if len(links) != 1 {
		t.Fatalf("want 1 image link, got %d", len(links))
	}
	// Image height (30) is used, not the line height (12).
	if want := image.Rect(4, 8, 24, 38); links[0].Rect != want {
		t.Errorf("rect = %v, want %v", links[0].Rect, want)
	}
}

func TestLinksFromBox_DroppedAndSkipped(t *testing.T) {
	// Every anchor here must be dropped: empty href, pure fragment, and
	// non-http schemes. A plain (non-anchor) run and a <br> inside an anchor
	// exercise the "no anchor" and "empty rect" skip paths.
	empty, et := anchor("", "x")
	frag, ft := anchor("#top", "x")
	js, jt := anchor("javascript:void(0)", "x")
	mail, mt := anchor("mailto:a@b.c", "x")

	plainP := &dom.Node{Type: dom.Element, Tag: "p"}
	plainT := &dom.Node{Type: dom.Text, Text: "hi", Parent: plainP}
	plainP.Children = []*dom.Node{plainT}

	// A <br> inside a valid anchor: anchorFor finds the anchor but itemRect is
	// empty for a line break, so it contributes nothing and yields no link.
	brA := &dom.Node{Type: dom.Element, Tag: "a", Attr: map[string]string{"href": "/only-break"}}
	br := &dom.Node{Type: dom.Element, Tag: "br", Parent: brA}
	brA.Children = []*dom.Node{br}
	brItem := &layout.InlineItem{LineBreak: true, Node: br}

	// A zero-width word inside a valid anchor is also skipped (empty rect).
	zeroA, zt := anchor("/zero", "")
	zeroItem := &layout.InlineItem{Text: "", Node: zt, X: 1, Y: 1, Width: 0, LineHeight: 0}

	items := []*layout.InlineItem{
		word(et, 0, 0, 10), word(ft, 0, 0, 10), word(jt, 0, 0, 10),
		word(mt, 0, 0, 10), word(plainT, 0, 0, 10), brItem, zeroItem,
	}
	_ = empty
	_ = frag
	_ = js
	_ = mail
	_ = zeroA
	root := &layout.Box{Lines: []*layout.LineBox{{Items: items}}}

	if links := LinksFromBox(root, "http://ex.com/"); len(links) != 0 {
		t.Fatalf("want 0 links, got %d: %+v", len(links), links)
	}
}

func TestLinksFromBox_NestedInlineInsideAnchor(t *testing.T) {
	// <a href="/y"><b>bold</b></a>: the atom's node is the <b>, so anchorFor
	// must climb two levels to the <a>.
	a := &dom.Node{Type: dom.Element, Tag: "a", Attr: map[string]string{"href": "/y"}}
	b := &dom.Node{Type: dom.Element, Tag: "b", Parent: a}
	txt := &dom.Node{Type: dom.Text, Text: "bold", Parent: b}
	b.Children = []*dom.Node{txt}
	a.Children = []*dom.Node{b}

	root := &layout.Box{Lines: []*layout.LineBox{{Items: []*layout.InlineItem{word(txt, 2, 3, 40)}}}}
	links := LinksFromBox(root, "http://ex.com/")
	if len(links) != 1 || links[0].Href != "http://ex.com/y" {
		t.Fatalf("nested anchor not resolved: %+v", links)
	}
}

func TestLinksFromBox_NilAndEmpty(t *testing.T) {
	if got := LinksFromBox(nil, "http://ex.com/"); got != nil {
		t.Errorf("nil root: want nil, got %+v", got)
	}
	if got := LinksFromBox(&layout.Box{}, "http://ex.com/"); len(got) != 0 {
		t.Errorf("empty box: want no links, got %+v", got)
	}
	// A nil child is walked without panicking.
	root := &layout.Box{Children: []*layout.Box{nil}}
	if got := LinksFromBox(root, "http://ex.com/"); len(got) != 0 {
		t.Errorf("nil child: want no links, got %+v", got)
	}
}

func TestLinkAt(t *testing.T) {
	links := []Link{
		{Rect: image.Rect(0, 0, 10, 10), Href: "http://a/"},
		{Rect: image.Rect(20, 20, 40, 40), Href: "http://b/"},
	}
	if href, ok := LinkAt(links, image.Pt(5, 5)); !ok || href != "http://a/" {
		t.Errorf("hit a: got %q,%v", href, ok)
	}
	if href, ok := LinkAt(links, image.Pt(30, 30)); !ok || href != "http://b/" {
		t.Errorf("hit b: got %q,%v", href, ok)
	}
	if _, ok := LinkAt(links, image.Pt(15, 15)); ok {
		t.Error("miss: want no hit in the gap")
	}
}

func TestResolveHref(t *testing.T) {
	baseURL, err := url.Parse("http://ex.com/a/b")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		raw     string
		want    string
		wantOK  bool
		nilBase bool
	}{
		{"page.html", "http://ex.com/a/page.html", true, false},
		{"/abs", "http://ex.com/abs", true, false},
		{"https://other/x", "https://other/x", true, false},
		{"", "", false, false},
		{"   ", "", false, false},
		{"#frag", "", false, false},
		{"mailto:a@b.c", "", false, false},
		{"javascript:void(0)", "", false, false},
		{"%zz", "", false, false},                        // unparseable → error branch
		{"http://direct/", "http://direct/", true, true}, // nil base uses ref as-is
	}
	for _, c := range cases {
		b := baseURL
		if c.nilBase {
			b = nil
		}
		got, ok := resolveHref(b, c.raw)
		if ok != c.wantOK || got != c.want {
			t.Errorf("resolveHref(%q, nilBase=%v) = (%q,%v), want (%q,%v)", c.raw, c.nilBase, got, ok, c.want, c.wantOK)
		}
	}
}

func TestCeilPx(t *testing.T) {
	for _, c := range []struct {
		in   float64
		want int
	}{{10, 10}, {10.1, 11}, {0.4, 1}, {0, 1}, {-3, 1}} {
		if got := ceilPx(c.in); got != c.want {
			t.Errorf("ceilPx(%v) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestItemRect(t *testing.T) {
	if r := itemRect(&layout.InlineItem{LineBreak: true}); !r.Empty() {
		t.Errorf("line break: want empty, got %v", r)
	}
	if r := itemRect(&layout.InlineItem{Width: 0, LineHeight: 10}); !r.Empty() {
		t.Errorf("zero width: want empty, got %v", r)
	}
	if r := itemRect(&layout.InlineItem{Width: 10, LineHeight: 0}); !r.Empty() {
		t.Errorf("zero height: want empty, got %v", r)
	}
	got := itemRect(&layout.InlineItem{Width: 12, LineHeight: 15, X: 3, Y: 4})
	if want := image.Rect(3, 4, 15, 19); got != want {
		t.Errorf("word rect = %v, want %v", got, want)
	}
}

func TestAnchorFor(t *testing.T) {
	if a := anchorFor(nil); a != nil {
		t.Errorf("nil node: want nil anchor")
	}
	// An <a> without href is not a link anchor.
	noHref := &dom.Node{Type: dom.Element, Tag: "a", Attr: map[string]string{}}
	child := &dom.Node{Type: dom.Text, Parent: noHref}
	if a := anchorFor(child); a != nil {
		t.Errorf("href-less anchor: want nil, got %+v", a)
	}
}

// TestRenderDocumentWithLinks drives the real cascade → layout → paint pipeline
// offline (no network: no external sheets, no images) and asserts the image and
// the hit-map describe the same page.
func TestRenderDocumentWithLinks(t *testing.T) {
	src := `<html><body><p>see <a href="/deep">the docs</a></p></body></html>`
	root, err := dom.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	doc := &Document{URL: "http://ex.com/home", Title: dom.Title(root), Root: root, HTML: src}
	img, info, links, err := New().RenderDocumentWithLinks(context.Background(), doc, image.Rect(0, 0, 400, 300))
	if err != nil {
		t.Fatal(err)
	}
	if img == nil || img.Bounds().Dx() != 400 {
		t.Fatalf("bad image: %v", img.Bounds())
	}
	if info.URL != "http://ex.com/home" {
		t.Errorf("info URL = %q", info.URL)
	}
	if len(links) != 1 || links[0].Href != "http://ex.com/deep" {
		t.Fatalf("links = %+v, want one → http://ex.com/deep", links)
	}
	if links[0].Rect.Empty() {
		t.Error("link rect is empty")
	}
	// The link rect must sit inside the rendered image bounds.
	if !links[0].Rect.In(img.Bounds()) {
		t.Errorf("link rect %v escapes image bounds %v", links[0].Rect, img.Bounds())
	}
}

// TestRenderDocumentWithLinks_Branches covers the viewport-default, page
// background and content-taller-than-viewport branches: a zero-width viewport
// (→ default 1024), a coloured body background and enough stacked paragraphs to
// grow the canvas past the (zero) viewport height.
func TestRenderDocumentWithLinks_Branches(t *testing.T) {
	src := `<html><body style="background-color:#ff0000">` +
		`<p>one</p><p>two</p><p>three</p><p>four</p><p>five</p>` +
		`<p><a href="/x">link</a></p></body></html>`
	root, err := dom.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	doc := &Document{URL: "http://ex.com/", Title: dom.Title(root), Root: root, HTML: src}
	img, _, links, err := New().RenderDocumentWithLinks(context.Background(), doc, image.Rect(0, 0, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 1024 {
		t.Errorf("zero-width viewport should default to 1024, got %d", img.Bounds().Dx())
	}
	if img.Bounds().Dy() <= 0 {
		t.Errorf("content should have grown the canvas, got %d", img.Bounds().Dy())
	}
	if len(links) != 1 {
		t.Errorf("want 1 link, got %d", len(links))
	}
}

// TestRenderDocumentWithLinks_EmptyPage covers the canvasH<=0 → 1 clamp: an
// empty document with a zero-height viewport has no content to grow the canvas.
func TestRenderDocumentWithLinks_EmptyPage(t *testing.T) {
	// A bare Document node has no renderable content, so layout height is 0;
	// with a zero-height viewport the canvas must clamp up to 1.
	doc := &Document{URL: "http://ex.com/", Root: &dom.Node{Type: dom.Document}}
	img, _, links, err := New().RenderDocumentWithLinks(context.Background(), doc, image.Rect(0, 0, 300, 0))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dy() < 1 {
		t.Errorf("canvas height should clamp to >=1, got %d", img.Bounds().Dy())
	}
	if len(links) != 0 {
		t.Errorf("empty page: want 0 links, got %d", len(links))
	}
}

// TestRenderWithLinks_FetchError covers the early error return without touching
// the network (an unparseable URL fails request construction).
func TestRenderWithLinks_FetchError(t *testing.T) {
	_, _, _, err := New().RenderWithLinks(context.Background(), "://bad", image.Rect(0, 0, 100, 100))
	if err == nil {
		t.Fatal("want error for unparseable URL")
	}
}

// TestRenderWithLinks_Live fetches a real page and asserts it yields links. It
// is skipped under -short (the offline gate), matching the engine's live tests.
func TestRenderWithLinks_Live(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live-network test in -short mode")
	}
	img, info, links, err := New().RenderWithLinks(context.Background(), "https://example.com", image.Rect(0, 0, 1024, 768))
	if err != nil {
		t.Skipf("network unavailable: %v", err)
	}
	if img == nil || info.Title == "" {
		t.Fatalf("bad render: img=%v title=%q", img != nil, info.Title)
	}
	// example.com has a "More information..." link → at least one hit.
	if len(links) == 0 {
		t.Error("want at least one link on example.com")
	}
}
