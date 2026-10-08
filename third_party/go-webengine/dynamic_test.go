// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package engine

import (
	"context"
	"image"
	"strings"
	"testing"
	"time"

	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/dom"
	jsengine "github.com/go-webengine/engine/js"
	"github.com/go-webengine/engine/layout"
	"github.com/go-webengine/engine/paint"
)

func TestStringHelpers(t *testing.T) {
	if got := colorString(css.Color{A: 0}); got != "rgba(0, 0, 0, 0)" {
		t.Errorf("transparent colour = %q", got)
	}
	if got := colorString(css.Color{R: 1, G: 2, B: 3, A: 255}); got != "rgb(1, 2, 3)" {
		t.Errorf("opaque colour = %q", got)
	}
	if got := colorString(css.Color{R: 10, G: 20, B: 30, A: 128}); !strings.HasPrefix(got, "rgba(10, 20, 30, 0.5") {
		t.Errorf("alpha colour = %q", got)
	}

	displays := map[css.Display]string{
		css.DisplayBlock: "block", css.DisplayNone: "none", css.DisplayInlineBlock: "inline-block",
		css.DisplayFlex: "flex", css.DisplayGrid: "grid", css.DisplayTable: "table",
		css.DisplayTableRow: "table-row", css.DisplayTableCell: "table-cell",
		css.DisplayTableRowGroup: "table-row-group", css.DisplayInline: "inline",
	}
	for d, want := range displays {
		if got := displayString(d); got != want {
			t.Errorf("displayString(%d) = %q, want %q", d, got, want)
		}
	}

	positions := map[css.Position]string{
		css.PositionStatic: "static", css.PositionRelative: "relative",
		css.PositionAbsolute: "absolute", css.PositionFixed: "fixed", css.PositionSticky: "sticky",
	}
	for p, want := range positions {
		if got := positionString(p); got != want {
			t.Errorf("positionString(%d) = %q, want %q", p, got, want)
		}
	}

	if textAlignString(css.AlignCenter) != "center" || textAlignString(css.AlignRight) != "right" ||
		textAlignString(css.AlignLeft) != "left" {
		t.Error("textAlignString wrong")
	}

	if lengthUsed(css.Length{Auto: true}) != "auto" {
		t.Error("lengthUsed auto")
	}
	if lengthUsed(css.Length{IsPercent: true, Percent: 0.25}) != "25%" {
		t.Error("lengthUsed percent")
	}
	if lengthUsed(css.Length{Px: 12}) != "12px" {
		t.Error("lengthUsed px")
	}
	if contentAxis(100, 14, 0) != 86 {
		t.Error("contentAxis normal")
	}
	if contentAxis(5, 20, 0) != 0 {
		t.Error("contentAxis clamp to 0")
	}
	if pxStr(3.5) != "3.5px" {
		t.Errorf("pxStr = %q", pxStr(3.5))
	}
}

func TestLayoutMetricsComputed(t *testing.T) {
	n := &dom.Node{Type: dom.Element, Tag: "div"}
	st := &css.Style{
		Display: css.DisplayBlock, Position: css.PositionRelative,
		Margin:     css.Edges{Top: 1, Right: 2, Bottom: 3, Left: 4},
		Padding:    css.Edges{Top: 5, Right: 6, Bottom: 7, Left: 8},
		FontSize:   16, FontWeight: 700, Italic: true,
		LineHeight: css.LineHeight{Px: 24},
		Color:      css.Color{R: 1, G: 2, B: 3, A: 255},
		Background: css.Color{R: 4, G: 5, B: 6, A: 255},
		TextAlign:  css.AlignCenter, BoxSizing: css.BorderBox,
		HasOpacity: true, Opacity: 0.5,
		ZIndexAuto: false, ZIndex: 7,
		Top:    css.Length{Px: 10}, Right: css.Length{Auto: true},
		Bottom: css.Length{IsPercent: true, Percent: 0.25}, Left: css.Length{Px: 0},
	}
	box := &layout.Box{Node: n, X: 0, Y: 0, W: 100, H: 50}
	m := newLayoutMetrics(box, css.StyleMap{n: st}, 1024, 768)

	want := map[string]string{
		"width": "86px", "height": "38px", "display": "block", "position": "relative",
		"margin-top": "1px", "margin-right": "2px", "margin-bottom": "3px", "margin-left": "4px",
		"padding-top": "5px", "padding-right": "6px", "padding-bottom": "7px", "padding-left": "8px",
		"font-size": "16px", "font-weight": "700", "font-style": "italic", "line-height": "24px",
		"color": "rgb(1, 2, 3)", "background-color": "rgb(4, 5, 6)", "text-align": "center",
		"box-sizing": "border-box", "opacity": "0.5", "z-index": "7", "visibility": "visible",
		"top": "10px", "right": "auto", "bottom": "25%", "left": "0px",
	}
	for prop, exp := range want {
		got, ok := m.Computed(n, prop)
		if !ok || got != exp {
			t.Errorf("Computed(%q) = %q,%v want %q", prop, got, ok, exp)
		}
	}
	if _, ok := m.Computed(n, "no-such-prop"); ok {
		t.Error("unknown property should report ok=false")
	}

	// A node with a rect but no style resolves width/height from the rect; other
	// props are unknown.
	only := &dom.Node{Type: dom.Element, Tag: "p"}
	box2 := &layout.Box{Node: only, X: 0, Y: 0, W: 42, H: 24}
	m2 := newLayoutMetrics(box2, css.StyleMap{}, 800, 600)
	if got, _ := m2.Computed(only, "width"); got != "42px" {
		t.Errorf("no-style width = %q", got)
	}
	if got, _ := m2.Computed(only, "height"); got != "24px" {
		t.Errorf("no-style height = %q", got)
	}
	if _, ok := m2.Computed(only, "color"); ok {
		t.Error("no-style non-geometry prop should be unknown")
	}

	// Defaults on an unset style: line-height normal, opacity 1, z-index auto,
	// font-style normal, display none when not laid out.
	def := &dom.Node{Type: dom.Element, Tag: "span"}
	defSt := &css.Style{Display: css.DisplayBlock, LineHeight: css.LineHeight{Normal: true}, ZIndexAuto: true}
	m3 := newLayoutMetrics(&layout.Box{}, css.StyleMap{def: defSt}, 800, 600) // def not in index
	assertComp(t, m3, def, "line-height", "normal")
	assertComp(t, m3, def, "opacity", "1")
	assertComp(t, m3, def, "z-index", "auto")
	assertComp(t, m3, def, "font-style", "normal")
	assertComp(t, m3, def, "display", "none") // not laid out
	if _, ok := m3.Computed(def, "width"); ok {
		t.Error("width with no rect and no border-relevant style should be unknown")
	}
}

func assertComp(t *testing.T, m *layoutMetrics, n *dom.Node, prop, want string) {
	t.Helper()
	if got, ok := m.Computed(n, prop); !ok || got != want {
		t.Errorf("Computed(%q) = %q,%v want %q", prop, got, ok, want)
	}
}

func TestLayoutMetricsRect(t *testing.T) {
	n := &dom.Node{Type: dom.Element, Tag: "div"}
	m := newLayoutMetrics(&layout.Box{Node: n, X: 3, Y: 4, W: 5, H: 6}, css.StyleMap{n: {}}, 100, 100)
	if x, y, w, h, ok := m.Rect(n); !ok || x != 3 || y != 4 || w != 5 || h != 6 {
		t.Fatalf("Rect = %v,%v,%v,%v,%v", x, y, w, h, ok)
	}
	if _, _, _, _, ok := m.Rect(&dom.Node{}); ok {
		t.Fatal("unknown node should report ok=false")
	}
}

func TestDomSignatureAndLinkKey(t *testing.T) {
	a, _ := dom.Parse(`<html><body><p class="x">hi</p></body></html>`)
	b, _ := dom.Parse(`<html><body><p class="y">hi</p></body></html>`)
	if domSignature(a) == domSignature(b) {
		t.Error("different class should change the signature")
	}
	if domSignature(a) != domSignature(a) {
		t.Error("signature must be stable")
	}
	// A tree with no <html> hashes to the empty digest (no panic).
	noHTML := &dom.Node{Type: dom.Document}
	_ = domSignature(noHTML)

	link, _ := dom.Parse(`<html><head><link rel="stylesheet" href="/a.css"><link rel="stylesheet" href="/b.css"></head><body></body></html>`)
	none, _ := dom.Parse(`<html><head></head><body></body></html>`)
	if linkKey(link) == linkKey(none) {
		t.Error("stylesheet links should change the link key")
	}
	if linkKey(none) != linkKey(&dom.Node{Type: dom.Document}) {
		t.Error("no links → empty key")
	}
}

// renderDoc parses src and renders it through RenderDocument, returning the doc
// (so the mutated tree can be inspected) and the render info.
func renderDoc(t *testing.T, e *Engine, src string, ctx context.Context, vp image.Rectangle) (*Document, *RenderInfo) {
	t.Helper()
	root, err := dom.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	doc := &Document{URL: "https://demo.test/", Title: dom.Title(root), Root: root, HTML: src}
	_, info, err := e.RenderDocument(ctx, doc, vp)
	if err != nil {
		t.Fatal(err)
	}
	return doc, info
}

// TestSettleFixpointCap proves the settle loop is bounded: a self-replicating
// script chain (each injected script appends the next) is stopped after exactly
// maxSettlePasses re-layout passes — it does not run away or hang.
func TestSettleFixpointCap(t *testing.T) {
	src := `<html><head></head><body><script>
		var code = "var b=document.body; var n=(+(b.getAttribute('data-d')||'0'))+1;" +
			"b.setAttribute('data-d',''+n);" +
			"var s=document.createElement('script'); s.textContent=b.getAttribute('data-code'); b.appendChild(s);";
		document.body.setAttribute('data-code', code);
		var s=document.createElement('script'); s.textContent=code; document.body.appendChild(s);
	</script></body></html>`

	done := make(chan struct{})
	var doc *Document
	go func() {
		doc, _ = renderDoc(t, New(), src, context.Background(), image.Rect(0, 0, 200, 200))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("settle loop did not terminate")
	}
	// The seed appends script#1. settle() now runs one pending script BEFORE the
	// pass-counted loop even starts (a RunPending() call ahead of
	// DispatchLifecycle, so a script a classic script just appended — e.g. a
	// hand-rolled ResourceLoader's own dynamically-created <script> — gets a
	// chance to register a window "load" listener before "load" fires; see
	// settle's own comment). That pre-loop pass runs script#1, so passes 0..2
	// of the maxSettlePasses-bounded loop each run one FURTHER generation —
	// one more than before this existed — before the cap stops it.
	body := dom.Find(doc.Root, "body")
	if got, _ := body.Attribute("data-d"); got != "4" {
		t.Fatalf("data-d = %q, want %q (one pre-loop pass plus exactly maxSettlePasses loop passes)", got, "4")
	}
}

// TestSettleKeepsGoodRenderOnScriptWipe covers a guard added after a live
// regression: react.dev's client router failed to load its own error route
// and, in the process, unmounted the whole app tree — but the DOM was still
// "changed" (so the no-op signature check does not catch it), and the settle
// loop happily re-laid-out and kept the now-empty result, turning a real page
// into a blank render. If a page had real content before a script pass and has
// essentially none after it, the pass must be discarded and the last good
// layout kept instead.
func TestSettleKeepsGoodRenderOnScriptWipe(t *testing.T) {
	src := `<html><head></head><body>
		<p>Real, visible article content long enough to lay out well above the
		empty-render threshold, exactly like a page's initial static HTML.</p>
		<script>document.body.innerHTML = '';</script>
	</body></html>`
	_, info := renderDoc(t, New(), src, context.Background(), image.Rect(0, 0, 400, 200))
	if info.ContentHeight < emptyRenderHeight {
		t.Fatalf("script wipe emptied the page: contentHeight=%d, want the pre-script layout kept", info.ContentHeight)
	}
}

// TestSettleWipeGuardReskinsWithNewStyle covers a live regression on top of the
// guard tested above: react.dev's dark-mode toggle (classList.add('dark') on
// <html>, driven by an ordinary matchMedia listener, nothing to do with
// hydration) ran in the SAME script pass as an unrelated client-side router
// failure that emptied the tree. The wipe guard correctly kept the pre-script
// geometry, but discarding the whole pass ALSO discarded the harmless class
// toggle's style change, so the page stayed on its light-mode background
// forever even though the DOM plainly showed <html class="dark">. The pass's
// style-only change must still land on the preserved box tree.
func TestSettleWipeGuardReskinsWithNewStyle(t *testing.T) {
	src := `<html><head><style>
			body{background-color:rgb(255,255,255)}
			.dark body{background-color:rgb(35,39,47)}
		</style></head><body>
		<p>Real, visible article content long enough to lay out well above the
		empty-render threshold, exactly like a page's initial static HTML.</p>
		<script>
			document.documentElement.classList.add('dark');
			document.body.innerHTML = '';
		</script>
	</body></html>`
	_, info := renderDoc(t, New(), src, context.Background(), image.Rect(0, 0, 400, 200))
	if info.ContentHeight < emptyRenderHeight {
		t.Fatalf("script wipe emptied the page: contentHeight=%d, want the pre-script layout kept", info.ContentHeight)
	}

	// RenderDocument does not expose the settled box tree, so drive settle
	// directly (as TestSettleBudgetGuard does) to inspect body's Style after
	// the wipe-guard's re-skin.
	root, err := dom.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	doc := &Document{URL: "https://demo.test/", Root: root}
	jsengine.MarkJSEnabled(doc.Root)
	fonts := paint.NewFonts()
	rp := &renderPass{}
	rp.sm = css.CascadeVW(doc.Root, 400, nil)
	rp.imgSize = map[*dom.Node][2]float64{}
	rp.box, rp.height = layout.LayoutDocument(doc.Root, rp.sm, 400, fonts, rp.imgSize)
	e := New()
	e.settle(context.Background(), doc, 400, 200, fonts, rp, time.Millisecond, nil).Close()

	if rp.height < emptyRenderHeight {
		t.Fatalf("settle rp.height = %v, want the pre-wipe geometry kept", rp.height)
	}
	body := dom.Find(doc.Root, "body")
	if body == nil {
		t.Fatal("body not found")
	}
	var found *css.Style
	var walk func(b *layout.Box)
	walk = func(b *layout.Box) {
		if b == nil {
			return
		}
		if b.Node == body {
			found = b.Style
		}
		for _, c := range b.Children {
			walk(c)
		}
	}
	walk(rp.box)
	if found == nil {
		t.Fatal("body box not found in settled tree")
	}
	want := css.Color{R: 35, G: 39, B: 47, A: 255}
	if found.Background != want {
		t.Errorf("body background after settle = %+v, want %+v (dark mode should have been adopted)", found.Background, want)
	}
}

// TestSettleNoMutationEarlyReturn covers the branch where the scripts change
// nothing layout-relevant: the initial layout stands, no re-layout happens.
func TestSettleNoMutationEarlyReturn(t *testing.T) {
	src := `<html><head><title>t</title></head><body><div>hi</div><script>var x=1+1;</script></body></html>`
	_, info := renderDoc(t, New(), src, context.Background(), image.Rect(0, 0, 200, 100))
	if info.ContentHeight <= 0 {
		t.Fatal("expected a rendered page")
	}
}

// TestSettleInjectedLink covers the branch that refetches external stylesheets
// when a script injects a new <link rel=stylesheet>. The href uses a scheme the
// sheet fetcher rejects before any network I/O, so the test stays offline.
func TestSettleInjectedLink(t *testing.T) {
	src := `<html><head></head><body><div id="d">hi</div><script>
		var l=document.createElement('link');
		l.setAttribute('rel','stylesheet');
		l.setAttribute('href','ftp://never.invalid/x.css');
		document.head.appendChild(l);
	</script></body></html>`
	doc, info := renderDoc(t, New(), src, context.Background(), image.Rect(0, 0, 200, 100))
	if info.ContentHeight <= 0 {
		t.Fatal("expected a rendered page")
	}
	if dom.Find(doc.Root, "link") == nil {
		t.Fatal("injected link should be in the tree")
	}
}

// TestSettleBudgetGuard covers the deadline guard: with an initial-layout cost
// that dwarfs the remaining time, a mutation is NOT re-laid-out (the pre-settle
// render is kept rather than risk a timeout).
func TestSettleBudgetGuard(t *testing.T) {
	src := `<html class="client-nojs"><head><style>#b{width:50px;height:10px}</style></head>` +
		`<body><div id="b"></div><script>document.getElementById('b').className='changed';</script></body></html>`
	root, err := dom.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	doc := &Document{URL: "https://demo.test/", Root: root}
	jsengine.MarkJSEnabled(doc.Root)

	fonts := paint.NewFonts()
	rp := &renderPass{}
	rp.sm = css.CascadeVW(doc.Root, 400, nil)
	rp.imgSize = map[*dom.Node][2]float64{}
	rp.box, rp.height = layout.LayoutDocument(doc.Root, rp.sm, 400, fonts, rp.imgSize)

	var logs []string
	e := New()
	e.JSLog = func(s string) { logs = append(logs, s) }

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(80*time.Millisecond))
	defer cancel()
	// A huge claimed initial-layout duration forces the 2*layoutDur > remaining
	// guard to trip on the first re-layout.
	e.settle(ctx, doc, 400, 300, fonts, rp, time.Hour, nil).Close()

	if !strings.Contains(strings.Join(logs, "\n"), "re-layout skipped") {
		t.Fatalf("expected the deadline guard to skip re-layout; logs:\n%s", strings.Join(logs, "\n"))
	}
}
