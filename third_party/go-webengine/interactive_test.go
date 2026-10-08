// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package engine

import (
	"context"
	"image"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-webengine/engine/dom"
)

// openFixture parses src, opens it as a LiveDocument, and registers Close via
// t.Cleanup so a failing test still releases the session's watchdog goroutine.
func openFixture(t *testing.T, e *Engine, src string, viewport image.Rectangle) *LiveDocument {
	t.Helper()
	root, err := dom.Parse(src)
	if err != nil {
		t.Fatalf("dom.Parse: %v", err)
	}
	doc := &Document{URL: "https://demo.test/", Root: root}
	live, err := e.OpenDocument(context.Background(), doc, viewport)
	if err != nil {
		t.Fatalf("OpenDocument: %v", err)
	}
	t.Cleanup(live.Close)
	return live
}

// TestLiveDocumentJSStateSurvivesInteract is the load-bearing proof for F0: a
// script's own in-memory state (here, a counter that would be plainly wrong
// if the initial script re-ran) must NOT reset across separate Interact
// calls, which is exactly what distinguishes a LiveDocument from calling
// RenderDocument again after each interaction (a fresh JS runtime each time,
// silently discarding anything a stateful script held that the DOM/attribute
// mutation alone didn't capture).
func TestLiveDocumentJSStateSurvivesInteract(t *testing.T) {
	const src = `<html><head><style>
		#box { width: 50px; height: 50px; background: red; }
		#box.on { background: blue; }
	</style></head><body>
		<div id="box"></div>
		<script>
			window.__count = (window.__count || 0) + 1;
			document.title = 'count:' + window.__count;
		</script>
	</body></html>`

	live := openFixture(t, New(), src, image.Rect(0, 0, 200, 200))

	if got := live.Document().Title; got != "count:1" {
		t.Fatalf("after Open, title = %q, want %q", got, "count:1")
	}

	box := dom.Find(live.Document().Root, "div")
	if box == nil {
		t.Fatal("fixture div not found")
	}

	// Three separate interactions, each toggling the class that switches the
	// background red<->blue via CSS (not inline style), each proving BOTH
	// halves of F0: the visible re-layout/repaint DOES happen (resettle ran),
	// and the script-held counter does NOT advance (RunInitial did not).
	wantBG := []struct {
		on  bool
		rgb [3]uint8
	}{
		{true, [3]uint8{0, 0, 255}},  // .on -> blue
		{false, [3]uint8{255, 0, 0}}, // back off -> red
		{true, [3]uint8{0, 0, 255}},
	}
	for i, step := range wantBG {
		on := step.on
		img, _, err := live.Interact(context.Background(), func() {
			if on {
				box.Attr["class"] = "on"
			} else {
				delete(box.Attr, "class")
			}
		})
		if err != nil {
			t.Fatalf("Interact #%d: %v", i, err)
		}
		if got := live.Document().Title; got != "count:1" {
			t.Fatalf("Interact #%d: title = %q, want %q (initial script re-ran — JS state was NOT preserved)", i, got, "count:1")
		}
		r, g, b, _ := img.At(25, 25).RGBA()
		gotRGB := [3]uint8{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8)}
		if gotRGB != step.rgb {
			t.Fatalf("Interact #%d: pixel(25,25) = %v, want %v (resettle did not re-layout/repaint the mutation)", i, gotRGB, step.rgb)
		}
	}
}

// TestLiveDocumentFrameMatchesRenderDocument checks Open+Frame's initial
// output against the one-shot RenderDocument path on the SAME source, so a
// bug that only affects the LiveDocument path (as opposed to renderCore,
// shared by both) is caught rather than passing by coincidence.
func TestLiveDocumentFrameMatchesRenderDocument(t *testing.T) {
	const src = `<html><body><h1>Hello</h1><p>World</p></body></html>`
	viewport := image.Rect(0, 0, 300, 200)

	root1, err := dom.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	e := New()
	wantImg, wantInfo, err := e.RenderDocument(context.Background(), &Document{URL: "https://demo.test/", Root: root1}, viewport)
	if err != nil {
		t.Fatalf("RenderDocument: %v", err)
	}

	live := openFixture(t, e, src, viewport)
	gotImg, gotInfo, err := live.Frame()
	if err != nil {
		t.Fatalf("Frame: %v", err)
	}
	if gotInfo.ContentHeight != wantInfo.ContentHeight {
		t.Fatalf("ContentHeight = %d, want %d", gotInfo.ContentHeight, wantInfo.ContentHeight)
	}
	if gotImg.Bounds() != wantImg.Bounds() {
		t.Fatalf("bounds = %v, want %v", gotImg.Bounds(), wantImg.Bounds())
	}
	for i := range gotImg.Pix {
		if gotImg.Pix[i] != wantImg.Pix[i] {
			t.Fatalf("pixel byte %d diverges: LiveDocument.Frame() != RenderDocument()", i)
		}
	}
}

// TestLiveDocumentCloseIsIdempotent guards against a panic/double-release if
// a caller (e.g. an onboarding window's both "navigated away" and "window
// closed" handlers) calls Close more than once.
func TestLiveDocumentCloseIsIdempotent(t *testing.T) {
	live := openFixture(t, New(), `<html><body>hi</body></html>`, image.Rect(0, 0, 100, 100))
	live.Close()
	live.Close()

	if _, _, err := live.Frame(); err != ErrClosed {
		t.Fatalf("Frame after Close: err = %v, want ErrClosed", err)
	}
	if _, _, err := live.Interact(context.Background(), func() {}); err != ErrClosed {
		t.Fatalf("Interact after Close: err = %v, want ErrClosed", err)
	}
}

// TestLiveDocumentOpen_Live exercises Open's own fetch path (OpenDocument,
// tested extensively above, takes an already-fetched Document — this is the
// one thing only Open itself does). Mirrors RenderWithLinks_Live's shape.
func TestLiveDocumentOpen_Live(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live-network test in -short mode")
	}
	live, err := New().Open(context.Background(), "https://example.com", image.Rect(0, 0, 1024, 768))
	if err != nil {
		t.Skipf("network unavailable: %v", err)
	}
	defer live.Close()
	img, info, err := live.Frame()
	if err != nil {
		t.Fatalf("Frame: %v", err)
	}
	if img == nil || info.Title == "" {
		t.Fatalf("bad render: img=%v title=%q", img != nil, info.Title)
	}
}

// TestLiveDocumentOpen_FetchError covers Open's own fetch-error return path
// (distinct from OpenDocument, which never fetches).
func TestLiveDocumentOpen_FetchError(t *testing.T) {
	if _, err := New().Open(context.Background(), "http://127.0.0.1:1/nope", image.Rect(0, 0, 100, 100)); err == nil {
		t.Fatal("Open against an unreachable address: want an error, got nil")
	}
}

// TestLiveDocumentResettleRefetchesChangedStylesheets covers the linkKey
// branch: a synthetic interaction that adds a <link rel=stylesheet> must
// have it fetched and applied on the very next Frame, exactly like a
// script-injected stylesheet would be mid-settle.
func TestLiveDocumentResettleRefetchesChangedStylesheets(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		_, _ = w.Write([]byte("#box { background: blue; }"))
	}))
	defer srv.Close()

	live := openFixture(t, New(), `<html><head></head><body>
		<div id="box" style="width:20px;height:20px"></div>
	</body></html>`, image.Rect(0, 0, 100, 100))

	// Before the interaction, #box carries no background rule at all — white
	// canvas shows through — so a blue pixel afterward can only come from the
	// injected stylesheet actually being fetched and applied, not from
	// cascade/specificity coincidence with some other rule.
	imgBefore, _, err := live.Frame()
	if err != nil {
		t.Fatalf("Frame: %v", err)
	}
	if r, g, b, _ := imgBefore.At(10, 10).RGBA(); [3]uint8{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8)} == [3]uint8{0, 0, 255} {
		t.Fatal("pixel(10,10) already blue before the stylesheet was injected — test fixture is not isolating what it claims to")
	}

	head := dom.Find(live.Document().Root, "head")
	if head == nil {
		t.Fatal("fixture head not found")
	}
	link := &dom.Node{Type: dom.Element, Tag: "link", Attr: map[string]string{
		"rel": "stylesheet", "href": srv.URL + "/style.css",
	}}

	img, _, err := live.Interact(context.Background(), func() {
		dom.AppendChild(head, link)
	})
	if err != nil {
		t.Fatalf("Interact: %v", err)
	}
	r, g, b, _ := img.At(10, 10).RGBA()
	if got := [3]uint8{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8)}; got != [3]uint8{0, 0, 255} {
		t.Fatalf("pixel(10,10) = %v, want blue — injected stylesheet was not fetched/applied", got)
	}
}

// TestLiveDocumentResettleKeepsGoodLayoutOnWipe covers the renderedEmpty
// wipe-guard: a synthetic interaction that empties an already-substantial
// page must not be allowed to collapse the layout to nothing (mirrors
// settle's own guard — see dynamic.go — against a broken script re-render).
func TestLiveDocumentResettleKeepsGoodLayoutOnWipe(t *testing.T) {
	live := openFixture(t, New(), `<html><body>
		<div style="height:200px">Plenty of real visible content here, well past the empty-render threshold.</div>
	</body></html>`, image.Rect(0, 0, 300, 300))

	_, info, err := live.Frame()
	if err != nil {
		t.Fatalf("Frame: %v", err)
	}
	if info.ContentHeight < emptyRenderHeight {
		t.Fatalf("fixture ContentHeight = %d, want >= %d before the wipe", info.ContentHeight, emptyRenderHeight)
	}

	body := dom.Find(live.Document().Root, "body")
	_, info, err = live.Interact(context.Background(), func() {
		dom.SetTextContent(body, "")
	})
	if err != nil {
		t.Fatalf("Interact: %v", err)
	}
	if info.ContentHeight < emptyRenderHeight {
		t.Fatalf("after wipe, ContentHeight = %d, want the pre-wipe layout kept (>= %d)", info.ContentHeight, emptyRenderHeight)
	}
}

// TestLiveDocumentEvalReadsPageState is Eval's own load-bearing proof at
// the LiveDocument level: a host (a conformance harness, say) reading a
// value the page's own script set — not a value Eval itself defines.
func TestLiveDocumentEvalReadsPageState(t *testing.T) {
	e := New()
	live := openFixture(t, e, `<html><body>
		<script>window.__ready = true; window.__count = 2 + 3;</script>
	</body></html>`, image.Rect(0, 0, 320, 240))

	ready, err := live.Eval("window.__ready")
	if err != nil {
		t.Fatalf("Eval(__ready): %v", err)
	}
	if ready != true {
		t.Fatalf("Eval(__ready) = %#v, want true", ready)
	}

	count, err := live.Eval("window.__count")
	if err != nil {
		t.Fatalf("Eval(__count): %v", err)
	}
	if count != int64(5) {
		t.Fatalf("Eval(__count) = %#v, want 5", count)
	}
}

func TestLiveDocumentEvalOnAClosedDocumentErrors(t *testing.T) {
	e := New()
	live := openFixture(t, e, `<html><body></body></html>`, image.Rect(0, 0, 320, 240))
	live.Close()
	if _, err := live.Eval("1"); err != ErrClosed {
		t.Fatalf("Eval after Close = %v, want ErrClosed", err)
	}
}
