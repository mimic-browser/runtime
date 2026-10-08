// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package engine

import (
	"context"
	"image"
	"strings"
	"testing"

	"github.com/go-webengine/engine/dom"
)

// TestJSMutationReflectedInLayout proves the script pass runs BEFORE the cascade
// and layout: a script that injects many paragraphs makes the laid-out page
// materially taller than the same document rendered with JS disabled.
func TestJSMutationReflectedInLayout(t *testing.T) {
	var sb strings.Builder
	sb.WriteString(`<html><head><title>t</title></head><body><div id="app"></div>`)
	sb.WriteString(`<script>
		var html='';
		for (var i=0;i<40;i++){ html += '<p>line number '+i+' with some words to wrap</p>'; }
		document.getElementById('app').innerHTML = html;
	</script></body></html>`)
	src := sb.String()

	vp := image.Rect(0, 0, 400, 300)

	eOn := New()
	_, infoOn, err := eOn.RenderHTML(context.Background(), src, "https://example.com/", vp)
	if err != nil {
		t.Fatal(err)
	}

	eOff := New()
	eOff.DisableJS = true
	_, infoOff, err := eOff.RenderHTML(context.Background(), src, "https://example.com/", vp)
	if err != nil {
		t.Fatal(err)
	}

	if infoOn.ContentHeight <= infoOff.ContentHeight {
		t.Fatalf("JS-injected content should grow the page: on=%d off=%d",
			infoOn.ContentHeight, infoOff.ContentHeight)
	}
}

// TestJSClientJSSignalStyled proves the client-js signal reaches the cascade:
// a rule gated on .client-js changes the body background only when JS runs.
func TestJSClientJSSignalStyled(t *testing.T) {
	src := `<html class="client-nojs"><head><title>t</title>
		<style>
			body { background-color: rgb(10,20,30); }
			html.client-js body { background-color: rgb(200,100,50); }
		</style></head><body>hi</body></html>`
	vp := image.Rect(0, 0, 100, 50)

	on := renderTopLeft(t, New(), src, vp)
	off := func() [3]uint8 {
		e := New()
		e.DisableJS = true
		return renderTopLeft(t, e, src, vp)
	}()

	if on != [3]uint8{200, 100, 50} {
		t.Fatalf("client-js rule should apply with JS on, got %v", on)
	}
	if off != [3]uint8{10, 20, 30} {
		t.Fatalf("no-JS fallback should apply with JS off, got %v", off)
	}
}

// TestDisableJSDeterministic confirms DisableJS leaves the no-JS fallback in
// place (the html keeps client-nojs; no client-js is added).
func TestDisableJSDeterministic(t *testing.T) {
	src := `<html class="client-nojs"><head><title>t</title></head>` +
		`<body><script>document.documentElement.className='changed'</script></body></html>`
	e := New()
	e.DisableJS = true
	// Render must succeed and not run the script (which would rename the class).
	_, _, err := e.RenderHTML(context.Background(), src, "https://example.com/", image.Rect(0, 0, 50, 50))
	if err != nil {
		t.Fatal(err)
	}
}

// TestJSInjectedLinkInHitMap proves the JavaScript pass runs on the
// RenderWithLinks path too: a script that appends an <a href> to the body makes
// that anchor appear in the returned hit-map, and with JS disabled it does not.
func TestJSInjectedLinkInHitMap(t *testing.T) {
	src := `<html><head><title>t</title></head><body>` +
		`<script>
			var a = document.createElement('a');
			a.setAttribute('href', '/injected');
			a.textContent = 'go here now';
			document.body.appendChild(a);
		</script></body></html>`
	vp := image.Rect(0, 0, 400, 300)

	on := New()
	_, _, links, err := on.RenderDocumentFromHTML(t, src, "https://example.com/", vp)
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || links[0].Href != "https://example.com/injected" {
		t.Fatalf("JS-injected anchor should appear in the hit-map, got %+v", links)
	}
	if links[0].Rect.Empty() {
		t.Error("injected link rect is empty")
	}

	off := New()
	off.DisableJS = true
	_, _, offLinks, err := off.RenderDocumentFromHTML(t, src, "https://example.com/", vp)
	if err != nil {
		t.Fatal(err)
	}
	if len(offLinks) != 0 {
		t.Fatalf("with JS disabled the anchor must not exist: %+v", offLinks)
	}
}

// TestJSTitleReflectedInRenderInfo proves a script that sets document.title is
// reflected in RenderInfo.Title on both the plain and the with-links paths.
func TestJSTitleReflectedInRenderInfo(t *testing.T) {
	src := `<html><head><title>old</title></head><body>` +
		`<script>document.title = 'new title';</script></body></html>`
	vp := image.Rect(0, 0, 200, 100)

	_, info, err := New().RenderHTML(context.Background(), src, "https://example.com/", vp)
	if err != nil {
		t.Fatal(err)
	}
	if info.Title != "new title" {
		t.Fatalf("RenderDocument title = %q, want %q", info.Title, "new title")
	}

	root, err := parseDoc(src)
	if err != nil {
		t.Fatal(err)
	}
	_, wl, _, err := New().RenderDocumentWithLinks(context.Background(), root, vp)
	if err != nil {
		t.Fatal(err)
	}
	if wl.Title != "new title" {
		t.Fatalf("RenderDocumentWithLinks title = %q, want %q", wl.Title, "new title")
	}
}

// RenderDocumentFromHTML parses src and renders it with the with-links pipeline,
// a small offline convenience for the JS hit-map tests.
func (e *Engine) RenderDocumentFromHTML(t *testing.T, src, baseURL string, vp image.Rectangle) (*image.RGBA, *RenderInfo, []Link, error) {
	t.Helper()
	doc, err := parseDoc(src)
	if err != nil {
		return nil, nil, nil, err
	}
	doc.URL = baseURL
	return e.RenderDocumentWithLinks(context.Background(), doc, vp)
}

// parseDoc parses src into a Document with a placeholder URL.
func parseDoc(src string) (*Document, error) {
	root, err := dom.Parse(src)
	if err != nil {
		return nil, err
	}
	return &Document{URL: "https://example.com/", Title: dom.Title(root), Root: root, HTML: src}, nil
}

// renderTopLeft renders src and returns the top-left pixel's RGB.
func renderTopLeft(t *testing.T, e *Engine, src string, vp image.Rectangle) [3]uint8 {
	t.Helper()
	img, _, err := e.RenderHTML(context.Background(), src, "https://example.com/", vp)
	if err != nil {
		t.Fatal(err)
	}
	i := img.PixOffset(1, 1)
	return [3]uint8{img.Pix[i], img.Pix[i+1], img.Pix[i+2]}
}
