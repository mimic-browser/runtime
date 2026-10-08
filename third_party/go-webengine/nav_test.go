// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package engine

import (
	"net/url"
	"strings"
	"testing"

	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/dom"
	"github.com/go-webengine/engine/layout"
	"github.com/go-webengine/engine/paint"
)

// layoutForNav parses and lays out html at a 1024 px viewport with the real
// font machinery, the way a consumer that exports a document would: these
// tests want real line breaks and real atom positions, not a fake Measurer.
func layoutForNav(t *testing.T, html string) *layout.Box {
	t.Helper()
	root, err := dom.Parse(html)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	box, _ := layout.LayoutDocument(root, css.Cascade(root), 1024, paint.NewFonts(), nil)
	return box
}

// idSet turns DocumentIDs's result into the set LinkRuns takes.
func idSet(ids map[string]Anchor) map[string]struct{} {
	set := make(map[string]struct{}, len(ids))
	for id := range ids {
		set[id] = struct{}{}
	}
	return set
}

func TestLinkRunsSplitsAWrappedAnchorPerLine(t *testing.T) {
	// A link long enough to wrap in a narrow container yields one run per
	// line — the clickable area follows the text — not one bounding box.
	root := layoutForNav(t, `<html><body style="margin:0"><div style="width:200px">`+
		`<a href="https://example.org/">`+strings.Repeat("word ", 40)+`</a></div></body></html>`)
	base, _ := url.Parse("https://example.org/")
	runs := LinkRuns(root, base, nil)
	if len(runs) < 2 {
		t.Fatalf("runs = %d, want one per wrapped line (>= 2)", len(runs))
	}
	for i := 1; i < len(runs); i++ {
		if runs[i].Y <= runs[i-1].Y {
			t.Errorf("run %d (y=%v) is not below run %d (y=%v)", i, runs[i].Y, i-1, runs[i-1].Y)
		}
		if runs[i].URI != runs[0].URI {
			t.Errorf("run %d target %q differs from %q", i, runs[i].URI, runs[0].URI)
		}
		if runs[i].Node != runs[0].Node {
			t.Errorf("run %d belongs to %p, want the same anchor as run 0 (%p)", i, runs[i].Node, runs[0].Node)
		}
	}
	if runs[0].Node == nil || runs[0].Node.Tag != "a" {
		t.Errorf("run 0 node = %+v, want the <a> element", runs[0].Node)
	}
	if runs[0].Fragment != "" {
		t.Errorf("an external link carries Fragment %q, want none", runs[0].Fragment)
	}
}

func TestLinkRunsInDocumentFragmentAndDanglingOne(t *testing.T) {
	root := layoutForNav(t, `<html><body>
		<p><a href="#sec">jump</a> and <a href="#nowhere">dangling</a></p>
		<h2 id="sec">Section</h2><p>body</p>
	</body></html>`)
	ids := DocumentIDs(root)
	runs := LinkRuns(root, nil, idSet(ids))
	if len(runs) != 1 {
		t.Fatalf("runs = %+v, want exactly 1 (the dangling #nowhere must not become a link)", runs)
	}
	if runs[0].Fragment != "sec" || runs[0].URI != "" {
		t.Errorf("run = %+v, want Fragment \"sec\" and no URI", runs[0])
	}
	if dest, ok := ids["sec"]; !ok || dest.Y <= runs[0].Y {
		t.Errorf("destination %+v should sit below the link at y=%v", dest, runs[0].Y)
	}
}

func TestLinkRunsSameDocumentURLWithFragmentIsInternal(t *testing.T) {
	root := layoutForNav(t, `<html><body><a href="https://host/doc#top">up</a><div id="top">x</div></body></html>`)
	base, _ := url.Parse("https://host/doc")
	runs := LinkRuns(root, base, idSet(DocumentIDs(root)))
	if len(runs) != 1 {
		t.Fatalf("runs = %+v, want 1", runs)
	}
	if runs[0].URI != "" || runs[0].Fragment != "top" {
		t.Errorf("a link to the document's own URL + fragment should be a Fragment, not a URI: %+v", runs[0])
	}
}

func TestLinkRunsRelativeHrefResolvesAgainstBase(t *testing.T) {
	root := layoutForNav(t, `<html><body><a href="../other/page.html">rel</a></body></html>`)
	base, _ := url.Parse("https://host/a/b/c.html")
	runs := LinkRuns(root, base, nil)
	if len(runs) != 1 || runs[0].URI != "https://host/a/other/page.html" {
		t.Errorf("relative href not resolved against base: %+v", runs)
	}
}

func TestLinkRunsDropsNonNavigableLinks(t *testing.T) {
	root := layoutForNav(t, `<html><body>
		<a href="javascript:void(0)">js</a> <a href="mailto:a@b">mail</a> <a href="tel:+1">tel</a>
		<a href="">empty</a> <a href="   ">blank</a> <a href="data:text/plain,x">data</a>
	</body></html>`)
	if runs := LinkRuns(root, nil, nil); len(runs) != 0 {
		t.Errorf("javascript:/mailto:/tel:/data:/empty hrefs must not produce runs: %+v", runs)
	}
}

// An anchor whose only content is an empty styled block — a vote arrow, an
// icon drawn by CSS — lays out no atom, so the line pass finds nothing; the
// box pass gives it the block's own rectangle, once, and a text-bearing
// anchor is not doubled by it. The fixture sits inside a wrapping <div> so
// the fallback is seen to take the outermost box under the anchor (the
// 10x10 block), not the block above it that merely contains the anchor.
func TestLinkRunsGivesAnAtomlessAnchorItsBoxRect(t *testing.T) {
	root := layoutForNav(t, `<html><body><div>
<a href="https://example.com/vote"><div style="width:10px;height:10px"></div></a>
<p><a href="https://example.com/text">one <span>two</span></a></p>
</div></body></html>`)
	runs := LinkRuns(root, nil, nil)
	var votes, texts int
	for _, r := range runs {
		switch r.URI {
		case "https://example.com/vote":
			votes++
			if r.W != 10 || r.H != 10 {
				t.Errorf("vote run = %vx%v at (%v,%v), want the 10x10 block", r.W, r.H, r.X, r.Y)
			}
			if r.Node == nil || r.Node.Tag != "a" {
				t.Errorf("vote run node = %+v, want the <a> element, not the block under it", r.Node)
			}
		case "https://example.com/text":
			texts++
		default:
			t.Errorf("unexpected run %+v", r)
		}
	}
	if votes != 1 || texts != 1 {
		t.Fatalf("runs: vote %d, text %d; want 1 and 1 (%d runs total)", votes, texts, len(runs))
	}
}

func TestLinkRunsNilRoot(t *testing.T) {
	if runs := LinkRuns(nil, nil, nil); runs != nil {
		t.Errorf("nil root: want nil, got %+v", runs)
	}
	if runs := LinkRuns(&layout.Box{Children: []*layout.Box{nil}}, nil, nil); len(runs) != 0 {
		t.Errorf("nil child: want no runs, got %+v", runs)
	}
}

func TestDocumentIDsBlockAndInlineAndLegacyName(t *testing.T) {
	root := layoutForNav(t, `<html><body style="margin:0">
		<p>intro</p>
		<div id="block">block</div>
		<p>text <span id="inline">inline</span> text <a name="legacy">old</a></p>
		<div id="block">duplicate</div>
	</body></html>`)
	ids := DocumentIDs(root)
	for _, id := range []string{"block", "inline", "legacy"} {
		if _, ok := ids[id]; !ok {
			t.Errorf("id %q not found", id)
		}
	}
	if ids["inline"].Y <= ids["block"].Y {
		t.Errorf("inline id (y=%v) should sit below the block id (y=%v)", ids["inline"].Y, ids["block"].Y)
	}
	if ids["inline"].X <= 0 {
		t.Errorf("inline id x = %v, want the first atom's x after the word \"text\"", ids["inline"].X)
	}
	if len(ids) != 3 {
		t.Errorf("ids = %v, want exactly 3 (duplicate id keeps the first)", ids)
	}
	if got := DocumentIDs(nil); len(got) != 0 {
		t.Errorf("nil root: want an empty map, got %v", got)
	}
}

func TestHeadingsLevelsTitlesAndIconOnlySkipped(t *testing.T) {
	root := layoutForNav(t, `<html><body>
		<h1>Chapter One</h1><p>a</p><h2>Part <b>Alpha</b></h2><p>b</p><h3></h3><h4>   </h4>
	</body></html>`)
	heads := Headings(root)
	if len(heads) != 2 {
		t.Fatalf("headings = %+v, want 2 (the empty <h3> and blank <h4> are skipped)", heads)
	}
	want := []struct {
		level int
		title string
	}{{1, "Chapter One"}, {2, "Part Alpha"}}
	for i, w := range want {
		if heads[i].Level != w.level || heads[i].Title != w.title {
			t.Errorf("heading %d = %d %q, want %d %q", i, heads[i].Level, heads[i].Title, w.level, w.title)
		}
		if heads[i].Node == nil || heads[i].Node.Tag != "h"+string(rune('0'+w.level)) {
			t.Errorf("heading %d node = %+v, want the <h%d> element", i, heads[i].Node, w.level)
		}
	}
	if heads[1].Y <= heads[0].Y {
		t.Errorf("h2 (y=%v) should sit below h1 (y=%v)", heads[1].Y, heads[0].Y)
	}
	if got := Headings(nil); got != nil {
		t.Errorf("nil root: want nil, got %+v", got)
	}
}

func TestResolveAnchorEdgeCases(t *testing.T) {
	base, _ := url.Parse("https://host/dir/page")
	ids := map[string]struct{}{"here": {}}
	for _, tc := range []struct {
		raw          string
		wantOK       bool
		wantURI      string
		wantFragment string
		withNoBase   bool
	}{
		{raw: "#here", wantOK: true, wantFragment: "here"},
		{raw: "#gone", wantOK: false},
		{raw: "https://host/dir/page#here", wantOK: true, wantFragment: "here"},
		{raw: "https://host/dir/page#gone", wantOK: false},
		{raw: "https://other/#here", wantOK: true, wantURI: "https://other/#here"},
		{raw: "sub/x", wantOK: true, wantURI: "https://host/dir/sub/x"},
		{raw: "://bad url", wantOK: false},
		{raw: "ftp://host/f", wantOK: false},
		{raw: "https://abs/x", wantOK: true, wantURI: "https://abs/x", withNoBase: true},
		{raw: "rel/x", wantOK: false, withNoBase: true}, // no base: a relative href resolves to nothing navigable
	} {
		b := base
		if tc.withNoBase {
			b = nil
		}
		got := resolveAnchor(b, tc.raw, ids)
		if got.ok != tc.wantOK || got.uri != tc.wantURI || got.fragment != tc.wantFragment {
			t.Errorf("resolveAnchor(%q) = %+v, want ok=%v uri=%q fragment=%q", tc.raw, got, tc.wantOK, tc.wantURI, tc.wantFragment)
		}
	}
}
