// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package engine

import (
	"context"
	"image"
	"strings"
	"testing"

	"github.com/srwiley/oksvg"

	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/dom"
)

// --- looksLikeSVG ---------------------------------------------------------

func TestLooksLikeSVG(t *testing.T) {
	cases := []struct {
		name string
		data string
		src  string
		want bool
	}{
		{"extension", "", "/logo.svg", true},
		{"extension with query", "", "/logo.svg?v=2#x", true},
		{"data mime", "", "data:image/svg+xml,<svg/>", true},
		{"content sniff", "<?xml version='1.0'?>\n<SVG viewBox='0 0 1 1'></SVG>", "/x", true},
		{"png bytes not svg", "\x89PNG\r\n\x1a\n", "/logo.png", false},
		{"large non-svg head truncated", strings.Repeat("x", 2048), "/blob", false},
		{"large svg found in head", "<svg " + strings.Repeat("x", 2048), "/blob", true},
		{"empty", "", "/logo", false},
	}
	for _, c := range cases {
		if got := looksLikeSVG([]byte(c.data), c.src); got != c.want {
			t.Errorf("%s: looksLikeSVG=%v want %v", c.name, got, c.want)
		}
	}
}

// --- attrDim --------------------------------------------------------------

func TestAttrDim(t *testing.T) {
	el := &dom.Node{Type: dom.Element, Tag: "svg", Attr: map[string]string{
		"width":  "60",
		"height": "48px",
		"pct":    "50%",
		"bad":    "auto",
		"neg":    "-3",
		"zero":   "0px",
	}}
	cases := map[string]int{
		"width":   60,
		"height":  48,
		"pct":     0,
		"bad":     0,
		"neg":     0,
		"zero":    0,
		"missing": 0,
	}
	for name, want := range cases {
		if got := attrDim(el, name); got != want {
			t.Errorf("attrDim(%q)=%d want %d", name, got, want)
		}
	}
}

// --- colorHex -------------------------------------------------------------

func TestColorHex(t *testing.T) {
	if got := colorHex(nil); got != "" {
		t.Errorf("nil style: got %q want empty", got)
	}
	if got := colorHex(&css.Style{Color: css.Color{R: 10, G: 126, B: 168, A: 0}}); got != "" {
		t.Errorf("transparent: got %q want empty", got)
	}
	if got := colorHex(&css.Style{Color: css.Color{R: 10, G: 126, B: 168, A: 255}}); got != "#0a7ea8" {
		t.Errorf("opaque: got %q want #0a7ea8", got)
	}
}

// --- usedSize -------------------------------------------------------------

func TestUsedSize(t *testing.T) {
	e := New()
	cases := []struct {
		name         string
		st           *css.Style
		attrW, attrH int
		iw, ih, vpW  int
		wantW, wantH int
	}{
		{"intrinsic only", nil, 0, 0, 40, 20, 1024, 40, 20},
		{"attr both", nil, 60, 30, 40, 20, 1024, 60, 30},
		{"attr width derives height", nil, 80, 0, 40, 20, 1024, 80, 40},
		{"attr height derives width", nil, 0, 30, 40, 20, 1024, 60, 30},
		{"css overrides attr", &css.Style{Width: css.Length{Px: 100}, Height: css.Length{Px: 50}}, 60, 30, 40, 20, 1024, 100, 50},
		{"viewport clamp", nil, 0, 0, 2000, 1000, 1024, 1024, 512},
		{"viewport clamp wide-short floors height to 1", nil, 0, 0, 4000, 1, 1024, 1024, 1},
	}
	for _, c := range cases {
		w, h := e.usedSize(c.st, c.attrW, c.attrH, c.iw, c.ih, c.vpW)
		if w != c.wantW || h != c.wantH {
			t.Errorf("%s: got (%d,%d) want (%d,%d)", c.name, w, h, c.wantW, c.wantH)
		}
	}
}

// --- serializeSVG ---------------------------------------------------------

func TestSerializeSVG(t *testing.T) {
	// Build an <svg> subtree the way the DOM would hold it: tag/attr names
	// lowercased, attribute values preserved.
	svg := &dom.Node{Type: dom.Element, Tag: "svg", Attr: map[string]string{
		"viewbox": "0 0 10 10", "width": "10",
	}}
	lg := &dom.Node{Type: dom.Element, Tag: "lineargradient", Attr: map[string]string{
		"id": "g", "gradientunits": "userSpaceOnUse",
	}, Parent: svg}
	rect := &dom.Node{Type: dom.Element, Tag: "rect", Attr: map[string]string{
		"fill": "url(#g)", "data-note": `a<b&"c`,
	}, Parent: svg}
	svg.Children = []*dom.Node{lg, rect}

	out := serializeSVG(svg, nil)

	// Mixed-case names restored for the tokens oksvg reads case-sensitively.
	for _, want := range []string{"viewBox=", "<linearGradient", "gradientUnits=", `xmlns="http://www.w3.org/2000/svg"`} {
		if !strings.Contains(out, want) {
			t.Errorf("serializeSVG missing %q in:\n%s", want, out)
		}
	}
	// Attribute values are XML-escaped.
	if !strings.Contains(out, "a&lt;b&amp;&quot;c") {
		t.Errorf("serializeSVG did not escape attribute value:\n%s", out)
	}
	// Attributes are emitted in sorted order (deterministic): within <rect> the
	// "data-note" attr precedes "fill".
	if i, j := strings.Index(out, "data-note="), strings.Index(out, "fill="); i < 0 || j < 0 || i > j {
		t.Errorf("attributes not in sorted order:\n%s", out)
	}
	// The serialised text round-trips through the rasteriser.
	if _, _, _, ok := New().svgToBitmap([]byte(out), nil, 0, 0, 1024, ""); !ok {
		t.Errorf("serialised SVG did not rasterise:\n%s", out)
	}
}

func TestSerializeSVGCSSFillOverridesAttribute(t *testing.T) {
	// A path with NO fill attribute at all (the common Tailwind
	// `class="fill-*"` idiom, confirmed live on tailwindcss.com's nav icons)
	// picks up the CSS-resolved colour; a path that ALREADY carries its own
	// fill attribute has it overridden by a set CSS value, and a path with no
	// computed style entry at all (sm has no entry for it) is left untouched.
	svg := &dom.Node{Type: dom.Element, Tag: "svg", Attr: map[string]string{"viewbox": "0 0 10 10"}}
	noAttr := &dom.Node{Type: dom.Element, Tag: "path", Attr: map[string]string{"d": "M0 0"}, Parent: svg}
	hasAttr := &dom.Node{Type: dom.Element, Tag: "path", Attr: map[string]string{"d": "M0 0", "fill": "red"}, Parent: svg}
	untouched := &dom.Node{Type: dom.Element, Tag: "path", Attr: map[string]string{"d": "M0 0", "fill": "purple"}, Parent: svg}
	noneCase := &dom.Node{Type: dom.Element, Tag: "path", Attr: map[string]string{"d": "M0 0", "fill": "red"}, Parent: svg}
	svg.Children = []*dom.Node{noAttr, hasAttr, untouched, noneCase}

	sm := css.StyleMap{
		noAttr:   {Fill: css.Color{R: 0x1e, G: 0x90, B: 0xff, A: 0xff}, FillSet: true},
		hasAttr:  {Fill: css.Color{R: 0x00, G: 0xff, B: 0x00, A: 0xff}, FillSet: true},
		noneCase: {FillNone: true},
	}
	out := serializeSVG(svg, sm)

	if !strings.Contains(out, `d="M0 0" fill="#1e90ff"`) {
		t.Errorf("CSS fill not added to a path with no attribute:\n%s", out)
	}
	if !strings.Contains(out, `d="M0 0" fill="#00ff00"`) {
		t.Errorf("CSS fill did not override the path's own fill attribute:\n%s", out)
	}
	if !strings.Contains(out, `fill="purple"`) {
		t.Errorf("a path absent from sm should keep its own attribute untouched:\n%s", out)
	}
	if !strings.Contains(out, `fill="none"`) {
		t.Errorf("FillNone should emit fill=\"none\":\n%s", out)
	}
}

func TestSerializeSVGEscapesText(t *testing.T) {
	// The svg already declares xmlns (so the serialiser does not add another),
	// and a "greater-than" appears in both an attribute value and text.
	svg := &dom.Node{Type: dom.Element, Tag: "svg", Attr: map[string]string{
		"viewbox": "0 0 1 1", "xmlns": "http://www.w3.org/2000/svg", "data-x": "a>b",
	}}
	txt := &dom.Node{Type: dom.Text, Text: "x < y & z > w", Parent: svg}
	// A non element/text node child is skipped by the serialiser.
	doc := &dom.Node{Type: dom.Document, Parent: svg}
	svg.Children = []*dom.Node{txt, doc}
	out := serializeSVG(svg, nil)
	if !strings.Contains(out, "x &lt; y &amp; z &gt; w") {
		t.Errorf("text not escaped: %s", out)
	}
	if !strings.Contains(out, `data-x="a&gt;b"`) {
		t.Errorf("attr '>' not escaped: %s", out)
	}
	if strings.Count(out, "xmlns=") != 1 {
		t.Errorf("xmlns should not be duplicated: %s", out)
	}
}

// --- sanitizeSVGRoot ------------------------------------------------------

func TestSanitizeSVGRoot(t *testing.T) {
	// Unit/percent width+height are stripped; an existing viewBox is kept; the
	// non-px own-size is reported as 0 (needs context we resolve elsewhere).
	in := `<svg width="100%" height="1.33em" viewBox="0 0 10 10"><rect/></svg>`
	outB, ow, oh := sanitizeSVGRoot([]byte(in))
	out := string(outB)
	if strings.Contains(out, "width=") || strings.Contains(out, "height=") {
		t.Errorf("width/height not stripped: %s", out)
	}
	if !strings.Contains(out, `viewBox="0 0 10 10"`) {
		t.Errorf("viewBox lost: %s", out)
	}
	if ow != 0 || oh != 0 {
		t.Errorf("non-px own-size should be 0, got %v,%v", ow, oh)
	}
	// px own-size (unitless + explicit px) is reported; no viewBox present so one
	// is synthesised from it.
	in = `<svg width="40px" height="30"><rect/></svg>`
	outB, ow, oh = sanitizeSVGRoot([]byte(in))
	out = string(outB)
	if !strings.Contains(out, `viewBox="0 0 40 30"`) {
		t.Errorf("viewBox not synthesised: %s", out)
	}
	if ow != 40 || oh != 30 {
		t.Errorf("own-size: got %v,%v want 40,30", ow, oh)
	}
	// Input without a root <svg> is returned unchanged.
	if got, _, _ := sanitizeSVGRoot([]byte("<div>x</div>")); string(got) != "<div>x</div>" {
		t.Errorf("non-svg changed: %q", string(got))
	}
	// Own width alone (no height) is still reported (the caller derives the other
	// axis from the viewBox aspect ratio).
	if _, ow, oh := sanitizeSVGRoot([]byte(`<svg width="24" viewBox="0 0 48 12"><rect/></svg>`)); ow != 24 || oh != 0 {
		t.Errorf("width-only own-size: got %v,%v want 24,0", ow, oh)
	}
	// End to end: a px-sized, viewBox-less SVG (which oksvg would reject) now
	// rasterises via the synthesised viewBox at its own intrinsic size.
	e := New()
	svg := `<svg xmlns="http://www.w3.org/2000/svg" width="20" height="20"><rect x="0" y="0" width="20" height="20" fill="#334455"/></svg>`
	if _, w, h, ok := e.svgToBitmap([]byte(svg), nil, 0, 0, 1024, ""); !ok || w != 20 || h != 20 {
		t.Errorf("px-sized viewBox-less SVG failed: ok=%v w=%d h=%d", ok, w, h)
	}
}

// TestSVGIntrinsicFromOwnSize verifies that an SVG's own root width/height (in
// px) are its intrinsic display size — the viewBox is only its coordinate
// system. This is the go.dev icon case: <svg width="16" height="16"
// viewBox="0 0 568 501"> must render 16×16, not 568×501.
func TestSVGIntrinsicFromOwnSize(t *testing.T) {
	e := New()
	// Own size drives the intrinsic; a large viewBox does not.
	svg := `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 568 501"><rect width="568" height="501" fill="#1185fe"/></svg>`
	img, w, h, ok := e.svgToBitmap([]byte(svg), nil, 0, 0, 1024, "")
	if !ok || w != 16 || h != 16 {
		t.Fatalf("own-size intrinsic: ok=%v w=%d h=%d want 16x16", ok, w, h)
	}
	// The fill still covers the (downscaled) box.
	if r, g, b, _ := img.At(8, 8).RGBA(); r>>8 != 0x11 || g>>8 != 0x85 || b>>8 != 0xfe {
		t.Errorf("centre pixel = #%02x%02x%02x want #1185fe", r>>8, g>>8, b>>8)
	}
	// One px own axis derives the other from the viewBox aspect (viewBox 40x20 =>
	// 2:1, so width 30 -> height 15).
	if _, w, h, ok := e.svgToBitmap([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="30" viewBox="0 0 40 20"><rect width="40" height="20"/></svg>`), nil, 0, 0, 1024, ""); !ok || w != 30 || h != 15 {
		t.Errorf("width-only intrinsic aspect: ok=%v w=%d h=%d want 30x15", ok, w, h)
	}
	// Height-only own axis likewise (viewBox 40x20 => height 10 -> width 20).
	if _, w, h, ok := e.svgToBitmap([]byte(`<svg xmlns="http://www.w3.org/2000/svg" height="10" viewBox="0 0 40 20"><rect width="40" height="20"/></svg>`), nil, 0, 0, 1024, ""); !ok || w != 20 || h != 10 {
		t.Errorf("height-only intrinsic aspect: ok=%v w=%d h=%d want 20x10", ok, w, h)
	}
}

// --- svgToBitmap: rasterise + error paths ---------------------------------

func TestSVGToBitmapErrors(t *testing.T) {
	e := New()
	// Not XML at all -> parse error -> false.
	if _, _, _, ok := e.svgToBitmap([]byte("not svg <<<"), nil, 0, 0, 1024, ""); ok {
		t.Error("garbage parsed as SVG")
	}
	// Well-formed but no viewBox and no width/height -> zero intrinsic -> false.
	if _, _, _, ok := e.svgToBitmap([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><rect/></svg>`), nil, 0, 0, 1024, ""); ok {
		t.Error("sizeless SVG unexpectedly rasterised")
	}
	// A non-finite viewBox overflows the raster dimensions; the render degrades
	// without leaking a panic.
	inf := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1e400 1e400"><rect width="1" height="1" fill="#000"/></svg>`
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("svgToBitmap leaked a panic: %v", r)
			}
		}()
		e.svgToBitmap([]byte(inf), nil, 0, 0, 1024, "")
	}()

	// The recover guard turns a panic from the raster step (rasterx panics on
	// some degenerate geometries) into a clean false, never crashing the render.
	orig := svgRasterizer
	svgRasterizer = func(_ *oksvg.SvgIcon, _, _, _, _ int) image.Image { panic("boom") }
	defer func() { svgRasterizer = orig }()
	good := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><rect width="10" height="10"/></svg>`
	if img, _, _, ok := e.svgToBitmap([]byte(good), nil, 0, 0, 1024, ""); ok || img != nil {
		t.Errorf("recover guard failed: ok=%v img=%v", ok, img != nil)
	}
}

func TestSVGToBitmapCurrentColor(t *testing.T) {
	e := New()
	svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10">` +
		`<rect x="0" y="0" width="10" height="10" fill="currentColor"/></svg>`
	img, w, h, ok := e.svgToBitmap([]byte(svg), nil, 0, 0, 1024, "#0a7ea4")
	if !ok || w != 10 || h != 10 {
		t.Fatalf("rasterise failed ok=%v w=%d h=%d", ok, w, h)
	}
	r, g, b, a := img.At(5, 5).RGBA()
	if r>>8 != 0x0a || g>>8 != 0x7e || b>>8 != 0xa4 || a>>8 != 0xff {
		t.Errorf("currentColor not applied: got R%d G%d B%d A%d", r>>8, g>>8, b>>8, a>>8)
	}
}

func TestSVGToBitmapMaxDim(t *testing.T) {
	e := New()
	// A viewBox far larger than maxSVGDim is clamped, not allocated verbatim.
	svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100000 100000"><rect width="100000" height="100000" fill="#123456"/></svg>`
	img, w, h, ok := e.svgToBitmap([]byte(svg), nil, 0, 0, 0, "")
	if !ok {
		t.Fatal("clamped raster failed")
	}
	if w > maxSVGDim || h > maxSVGDim {
		t.Errorf("dimension not clamped: %dx%d", w, h)
	}
	if img.Bounds().Dx() != w || img.Bounds().Dy() != h {
		t.Errorf("bitmap bounds %v disagree with reported %dx%d", img.Bounds(), w, h)
	}
}

// --- decodeDataURI percent-decode -----------------------------------------

func TestDecodeDataURISVG(t *testing.T) {
	// Non-base64, percent-encoded SVG payload must be percent-decoded.
	uri := "data:image/svg+xml,%3Csvg%3E%3C/svg%3E"
	b, ok := decodeDataURI(uri)
	if !ok || string(b) != "<svg></svg>" {
		t.Errorf("percent decode: ok=%v got %q", ok, string(b))
	}
	// Base64 payload still decodes.
	b, ok = decodeDataURI("data:text/plain;base64,aGk=")
	if !ok || string(b) != "hi" {
		t.Errorf("base64 decode: ok=%v got %q", ok, string(b))
	}
	// A malformed percent-escape can't be decoded: fall back to the raw payload.
	if b, ok := decodeDataURI("data:text/plain,%zz"); !ok || string(b) != "%zz" {
		t.Errorf("bad percent-escape fallback: ok=%v got %q", ok, string(b))
	}
	// No comma -> false.
	if _, ok := decodeDataURI("data:nope"); ok {
		t.Error("missing comma should fail")
	}
	// Bad base64 -> false.
	if _, ok := decodeDataURI("data:x;base64,@@@"); ok {
		t.Error("bad base64 should fail")
	}
}

// --- end-to-end: inline <svg> intrinsic-size layout -----------------------

func TestInlineSVGIntrinsicLayout(t *testing.T) {
	// An inline <svg> with no CSS size lays out at its viewBox intrinsic size and
	// paints (a solid-filled rect is sampled at a known interior pixel).
	html := `<!doctype html><html><body style="margin:0;background:#fff">` +
		`<svg viewBox="0 0 40 40"><rect x="0" y="0" width="40" height="40" fill="#12ab34"/></svg>` +
		`</body></html>`
	img := renderHTMLTest(t, html, 200, 100)
	// Intrinsic 40x40 at the top-left; centre pixel is the fill colour.
	assertPixel(t, img, 20, 20, 0x12, 0xab, 0x34, "inline svg fill")
	// Just outside the 40px box stays white (the box did not overflow to 200px).
	assertPixel(t, img, 60, 20, 0xff, 0xff, 0xff, "outside svg box white")
}

func TestInlineSVGCSSClassFill(t *testing.T) {
	// A <rect> painted purely via a CSS class (Tailwind's `fill-*` idiom, no
	// `fill=` attribute at all) picks up the cascaded colour instead of
	// rasterising with SVG's initial fill (black) — confirmed load-bearing
	// live on tailwindcss.com's own nav icons and logo mark (engine#126).
	html := `<!doctype html><html><body style="margin:0;background:#fff">` +
		`<style>.blue{fill:#1e90ff}</style>` +
		`<svg viewBox="0 0 40 40"><rect class="blue" width="40" height="40"/></svg>` +
		`</body></html>`
	img := renderHTMLTest(t, html, 100, 100)
	assertPixel(t, img, 20, 20, 0x1e, 0x90, 0xff, "CSS-class fill on inline svg")
}

func TestFlexRowSVGSiblingNotOverlapped(t *testing.T) {
	// A flex item's preferredWidth (used to size a flex row before it has a
	// definite width) special-cased only the "img" tag, never "svg" — fine
	// as long as an inline SVG stayed CSS-display:inline (appendElementInline
	// always sizes it correctly regardless of display, via a separate tag
	// switch), but hasBlockLevelChild has no such exception: an SVG made
	// display:block (Tailwind's own preflight reset,
	// `img,svg,video,...{display:block}`) routed its ANCESTOR's preferredWidth
	// through the "widest block child" branch instead, which recurses into
	// preferredWidth for the SVG with no img/svg case of its own — falling
	// through to a generic inline-content measurement that sees only the
	// SVG's non-text <path>/<rect> children and reports ~0. Confirmed live on
	// tailwindcss.com: the nav logo (an <svg class="h-5">, sized by CSS alone,
	// no width/height attributes) measured as a zero-width flex item, so its
	// sibling (a version-badge button) rendered overlapping it at the row's
	// left edge instead of to its right (engine#129).
	html := `<!doctype html><html><body style="margin:0;background:#fff">` +
		`<style>svg{display:block}</style>` +
		`<div style="display:flex;align-items:center">` +
		`<svg viewBox="0 0 100 20" style="height:20px"><rect width="100" height="20" fill="#1e90ff"/></svg>` +
		`<div style="width:10px;height:10px;background:#ff00ff"></div>` +
		`</div>` +
		`</body></html>`
	img := renderHTMLTest(t, html, 200, 30)
	assertPixel(t, img, 50, 10, 0x1e, 0x90, 0xff, "svg sized by CSS alone, no width/height attrs")
	assertPixel(t, img, 105, 5, 0xff, 0x00, 0xff, "sibling positioned AFTER the svg, not overlapping it")
}

// --- end-to-end: <img src=svg> sized by width/height attrs ----------------

func TestImgSVGAttrSizing(t *testing.T) {
	// A 10x10-viewBox green SVG requested at width/height 50 fills a 50x50 box.
	html := `<!doctype html><html><body style="margin:0;background:#fff">` +
		`<img width="50" height="50" src="data:image/svg+xml,` +
		`%3Csvg%20xmlns='http://www.w3.org/2000/svg'%20viewBox='0%200%2010%2010'%3E` +
		`%3Crect%20width='10'%20height='10'%20fill='%2300cc44'/%3E%3C/svg%3E"></body></html>`
	img := renderHTMLTest(t, html, 200, 120)
	assertPixel(t, img, 25, 25, 0x00, 0xcc, 0x44, "img svg fill at 50px")
	assertPixel(t, img, 70, 25, 0xff, 0xff, 0xff, "beyond 50px box white")
}

// --- end-to-end: broken inline <svg> degrades to empty (no crash) ---------

func TestBrokenSVGRendersEmpty(t *testing.T) {
	// An <svg> with neither viewBox nor size cannot be rasterised: the page still
	// renders (white), never crashes.
	html := `<!doctype html><html><body style="margin:0;background:#fff">` +
		`<svg><rect fill="#ff0000"/></svg></body></html>`
	img := renderHTMLTest(t, html, 120, 80)
	assertPixel(t, img, 5, 5, 0xff, 0xff, 0xff, "broken svg leaves page white")
}

// renderHTMLTest renders an HTML string offline (JS disabled for determinism).
func renderHTMLTest(t *testing.T, html string, w, h int) *image.RGBA {
	t.Helper()
	e := New()
	e.DisableJS = true
	img, _, err := e.RenderHTML(context.Background(), html, "https://demo.test/", image.Rect(0, 0, w, h))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	return img
}

// --- golden: inline SVG with gradient, stroke, path, viewBox scaling ------

func TestSVGGolden(t *testing.T) {
	img := renderFixture(t, "svg_golden.html", 200, 140)

	d := func(a, b uint8) int {
		if a > b {
			return int(a - b)
		}
		return int(b - a)
	}
	near := func(x, y int, r, g, b uint8, tol int, what string) {
		c := img.RGBAAt(x, y)
		if d(c.R, r) > tol || d(c.G, g) > tol || d(c.B, b) > tol {
			t.Errorf("%s: (%d,%d) = #%02x%02x%02x want ~#%02x%02x%02x (tol %d)", what, x, y, c.R, c.G, c.B, r, g, b, tol)
		}
	}

	// viewBox 0..50 rendered at 100x100 => 2x scale.
	// Gradient rect (top half, black->white left->right), sampled clear of the
	// red triangle: left near-black, right near-white.
	near(4, 8, 0, 0, 0, 12, "gradient left black")
	near(95, 8, 255, 255, 255, 12, "gradient right white")
	// Solid blue rect (bottom half, viewBox y25-50 => pixels y50-100), sampled
	// well clear of the centred circle.
	near(12, 90, 0x22, 0x44, 0xcc, 8, "solid blue rect")
	// Red triangle interior (apex viewBox(25,6)->pixel(50,12); body around y=25px).
	near(50, 26, 0xee, 0, 0, 20, "red triangle fill")
	// currentColor stroke of the circle (centre viewBox(25,37)->pixel(50,74),
	// r8->16px; left stroke around x=34px) is the div's teal #0a7ea4.
	near(34, 74, 0x0a, 0x7e, 0xa4, 40, "currentColor stroke teal")

	checkGolden(t, img, "svg_golden.png")
}
