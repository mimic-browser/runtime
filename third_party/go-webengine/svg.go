// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package engine

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
	"golang.org/x/image/draw"

	gfxsvg "github.com/go-gfx/gfx/svg"

	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/dom"
)

// maxSVGDim bounds a rasterised SVG's pixel dimensions on each axis (a cost
// guard: an absurd CSS/viewBox size cannot request a multi-gigapixel buffer).
const maxSVGDim = 4096

// looksLikeSVG reports whether the fetched image bytes are an SVG document,
// judged by the src extension (".svg", ignoring any query/fragment) or by
// sniffing an "<svg" tag near the start of the content (covers http responses
// and data: URIs whose payload is SVG regardless of the declared media type).
func looksLikeSVG(data []byte, src string) bool {
	if srcLooksLikeSVG(src) {
		return true
	}
	head := data
	if len(head) > 1024 {
		head = head[:1024]
	}
	return bytes.Contains(bytes.ToLower(head), []byte("<svg"))
}

// srcLooksLikeSVG reports whether a src/url alone (no fetched bytes) identifies
// an SVG: a ".svg" path (ignoring any query/fragment) or an "image/svg" media
// type in a data: URI. It lets the image budgeter classify vector vs raster
// BEFORE fetching, so a wall of decorative SVGs never spends the raster budget.
func srcLooksLikeSVG(src string) bool {
	s := strings.ToLower(strings.TrimSpace(src))
	if i := strings.IndexAny(s, "?#"); i >= 0 {
		s = s[:i]
	}
	return strings.HasSuffix(s, ".svg") || strings.Contains(s, "image/svg")
}

// svgToBitmap parses an SVG document and rasterises it at the used pixel size
// derived from its intrinsic viewBox, the element's CSS width/height and the
// viewport width (matching the raster-image path). currentColor, when non-empty,
// replaces the SVG's `currentColor` keyword (so a logo drawn with
// `fill="currentColor"` picks up the element's computed text colour). It reports
// false — never panicking — when the document cannot be parsed or has no usable
// intrinsic size, so a broken SVG renders as empty space rather than crashing
// the page.
func (e *Engine) svgToBitmap(data []byte, st *css.Style, attrW, attrH, viewportW int, currentColor string) (img image.Image, usedW, usedH int, ok bool) {
	defer func() {
		if recover() != nil {
			img, usedW, usedH, ok = nil, 0, 0, false
		}
	}()
	// oksvg parses the root <svg> width/height with strconv and errors out on any
	// unit/percentage form (`100%`, `1.33em`, `40px`), which would reject the
	// document wholesale. We size the box ourselves from the viewBox + CSS, so
	// strip those attributes first (injecting a viewBox from their numeric part
	// when the document has none).
	data, ownW, ownH := sanitizeSVGRoot(data)
	var (
		icon *oksvg.SvgIcon
		err  error
	)
	if currentColor != "" && bytes.Contains(data, []byte("currentColor")) {
		icon, err = oksvg.ReadReplacingCurrentColor(bytes.NewReader(data), currentColor, oksvg.IgnoreErrorMode)
	} else {
		icon, err = oksvg.ReadIconStream(bytes.NewReader(data), oksvg.IgnoreErrorMode)
	}
	if err != nil || icon == nil || icon.ViewBox.W <= 0 || icon.ViewBox.H <= 0 {
		return nil, 0, 0, false
	}
	// The SVG document's own root width/height (in px) are its intrinsic display
	// size (as a browser uses them); the viewBox is only the coordinate system.
	// Fall back to the viewBox when the document gives no px size, deriving a
	// missing axis from the viewBox aspect ratio.
	vbW, vbH := icon.ViewBox.W, icon.ViewBox.H
	var iw, ih int
	switch {
	case ownW > 0 && ownH > 0:
		iw, ih = iround(ownW), iround(ownH)
	case ownW > 0:
		iw, ih = iround(ownW), iround(ownW*vbH/vbW)
	case ownH > 0:
		iw, ih = iround(ownH*vbW/vbH), iround(ownH)
	default:
		iw, ih = iround(vbW), iround(vbH)
	}
	w, h := e.usedSize(st, attrW, attrH, iw, ih, viewportW)
	if w > maxSVGDim {
		w = maxSVGDim
	}
	if h > maxSVGDim {
		h = maxSVGDim
	}
	// The scanner samples in the viewBox coordinate system; SetTarget scales it
	// onto the w×h output.
	// gfx's path rasterizer preserves compound contours that rasterx can
	// incorrectly bridge with a filled shape. Keep oksvg for intrinsic sizing
	// and as a fallback for SVG constructs gfx cannot parse.
	if currentColor != "" {
		data = bytes.ReplaceAll(data, []byte("currentColor"), []byte(currentColor))
	}
	if rendered, err := gfxSVGRasterizer(string(data), gfxsvg.Options{Scale: min(float64(w)/vbW, float64(h)/vbH), Ink: color.RGBA{A: 255}}); err == nil {
		src := rendered.Image.ToNRGBA()
		if src.Bounds().Dx() == w && src.Bounds().Dy() == h {
			return src, w, h, true
		}
		out := image.NewNRGBA(image.Rect(0, 0, w, h))
		draw.ApproxBiLinear.Scale(out, out.Bounds(), src, src.Bounds(), draw.Src, nil)
		return out, w, h, true
	}
	return svgRasterizer(icon, w, h, iround(vbW), iround(vbH)), w, h, true
}

var gfxSVGRasterizer = gfxsvg.Rasterize

var (
	svgRootRe    = regexp.MustCompile(`(?is)<svg\b[^>]*>`)
	svgWHRe      = regexp.MustCompile(`(?is)\s+(?:width|height)\s*=\s*("[^"]*"|'[^']*'|[^\s>]+)`)
	svgViewBoxRe = regexp.MustCompile(`(?is)\bviewbox\s*=`)
	svgWidthRe   = regexp.MustCompile(`(?is)\swidth\s*=\s*("[^"]*"|'[^']*'|[^\s>]+)`)
	svgHeightRe  = regexp.MustCompile(`(?is)\sheight\s*=\s*("[^"]*"|'[^']*'|[^\s>]+)`)
)

// sanitizeSVGRoot removes width/height attributes from the document's root <svg>
// element so oksvg (which parses them with strconv and rejects any unit or
// percentage form) can read the document; we compute the used box size
// ourselves. It also returns the SVG's own root width/height in CSS px (0 when
// absent or non-px, e.g. `100%`/`1.33em`) — the document's intrinsic display
// size, which a browser prefers over the viewBox. When the root carries no
// viewBox but has a px width+height, a viewBox is synthesised so oksvg still has
// a coordinate system. Input without a root <svg> is returned unchanged.
func sanitizeSVGRoot(data []byte) (out []byte, ownW, ownH float64) {
	loc := svgRootRe.FindIndex(data)
	if loc == nil {
		return data, 0, 0
	}
	tag := data[loc[0]:loc[1]]
	ownW = svgRootLen(tag, svgWidthRe)
	ownH = svgRootLen(tag, svgHeightRe)
	var vb string
	if !svgViewBoxRe.Match(tag) && ownW > 0 && ownH > 0 {
		vb = fmt.Sprintf(` viewBox="0 0 %g %g"`, ownW, ownH)
	}
	newTag := svgWHRe.ReplaceAll(tag, nil)
	if vb != "" {
		// Insert the synthesised viewBox just before the closing '>'.
		newTag = append(newTag[:len(newTag)-1:len(newTag)-1], append([]byte(vb), '>')...)
	}
	out = make([]byte, 0, len(data)+len(vb))
	out = append(out, data[:loc[0]]...)
	out = append(out, newTag...)
	out = append(out, data[loc[1]:]...)
	return out, ownW, ownH
}

// svgRootLen extracts a length attribute value from the root <svg> tag and
// returns it in CSS px, accepting only a unitless number or an explicit `px`
// value (a percentage or font-relative unit yields 0 — it needs context we
// resolve elsewhere).
func svgRootLen(tag []byte, re *regexp.Regexp) float64 {
	m := re.FindSubmatch(tag)
	if m == nil {
		return 0
	}
	v := strings.TrimSpace(strings.Trim(string(m[1]), `"'`))
	v = strings.TrimSuffix(v, "px")
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil || f <= 0 {
		return 0
	}
	return f
}

// svgRasterizer draws icon into a w×h RGBA image, mapping the iw×ih viewBox onto
// it. It is a package variable so a test can substitute a panicking
// implementation and exercise the recover guard in svgToBitmap (rasterx does
// panic on some degenerate geometries, so the guard is not merely theoretical).
var svgRasterizer = func(icon *oksvg.SvgIcon, w, h, iw, ih int) image.Image {
	icon.SetTarget(0, 0, float64(w), float64(h))
	rgba := image.NewRGBA(image.Rect(0, 0, w, h))
	// The scanner must be sized to the OUTPUT image (w×h, already clamped to
	// maxSVGDim), not the source viewBox (iw×ih): NewScannerGV allocates internal
	// buffers proportional to its width×height, and a large viewBox (e.g. a 16px
	// icon with a 100000-unit viewBox) would otherwise allocate tens of GB. The
	// icon's ViewBox already maps source coordinates onto the SetTarget rect.
	scanner := rasterx.NewScannerGV(w, h, rgba, rgba.Bounds())
	raster := rasterx.NewDasher(w, h, scanner)
	icon.Draw(raster, 1.0)
	return rgba
}

// usedSize resolves an SVG's used pixel size. Precedence, high to low: a
// definite CSS width/height (from the style), the HTML presentation width/height
// attributes (attrW/attrH, 0 == unset), then the intrinsic viewBox size (iw×ih).
// For each source a single specified axis derives the other from the intrinsic
// aspect ratio. Finally an image wider than the viewport is scaled down
// proportionally.
func (e *Engine) usedSize(st *css.Style, attrW, attrH, iw, ih, viewportW int) (int, int) {
	w, h := iw, ih
	// Presentation attributes (lower priority than CSS, applied first).
	switch {
	case attrW > 0 && attrH > 0:
		w, h = attrW, attrH
	case attrW > 0:
		w, h = attrW, iround(float64(attrW)*float64(ih)/float64(iw))
	case attrH > 0:
		w, h = iround(float64(attrH)*float64(iw)/float64(ih)), attrH
	}
	// CSS width/height overrides the presentation attributes.
	if cw, ch, ok := cssImageSize(st, iw, ih); ok {
		w, h = cw, ch
	}
	if viewportW > 0 && w > viewportW {
		nh := int(float64(h) * float64(viewportW) / float64(w))
		if nh < 1 {
			nh = 1
		}
		w, h = viewportW, nh
	}
	return w, h
}

// attrDim parses an HTML presentation dimension attribute (e.g. <img width>,
// <svg height>) as a non-negative pixel count, tolerating a trailing "px" or
// other unit suffix and ignoring percentages (which need a containing block).
// It returns 0 when absent, a percentage, or unparseable.
func attrDim(n *dom.Node, name string) int {
	v, ok := n.Attribute(name)
	if !ok {
		return 0
	}
	v = strings.TrimSpace(v)
	if strings.HasSuffix(v, "%") {
		return 0
	}
	end := 0
	for end < len(v) && (v[end] >= '0' && v[end] <= '9' || v[end] == '.') {
		end++
	}
	if end == 0 {
		return 0
	}
	f, err := strconv.ParseFloat(v[:end], 64)
	if err != nil || f <= 0 {
		return 0
	}
	return iround(f)
}

// colorHex formats an element's computed text colour as an SVG-usable
// `#rrggbb` string, for substituting `currentColor`. It returns "" when the
// style is nil or the colour is fully transparent (leave currentColor as-is).
func colorHex(st *css.Style) string {
	if st == nil || st.Color.A == 0 {
		return ""
	}
	return fmt.Sprintf("#%02x%02x%02x", st.Color.R, st.Color.G, st.Color.B)
}

// svgTagCase and svgAttrCase restore the mixed-case SVG names that the HTML
// parser and our DOM lowercase. Only the names oksvg reads case-sensitively
// (the gradient elements and their layout attributes) plus a few common SVG
// containers need restoring; every other name is emitted lowercase, which is
// how SVG defines it. Values are never touched.
var svgTagCase = map[string]string{
	"lineargradient": "linearGradient",
	"radialgradient": "radialGradient",
	"clippath":       "clipPath",
	"textpath":       "textPath",
	"foreignobject":  "foreignObject",
}

var svgAttrCase = map[string]string{
	"viewbox":             "viewBox",
	"gradienttransform":   "gradientTransform",
	"gradientunits":       "gradientUnits",
	"spreadmethod":        "spreadMethod",
	"patterntransform":    "patternTransform",
	"patternunits":        "patternUnits",
	"preserveaspectratio": "preserveAspectRatio",
	"clippathunits":       "clipPathUnits",
	"startoffset":         "startOffset",
}

// serializeSVG serialises an inline <svg> DOM subtree back to SVG text so it can
// be rasterised. It restores the handful of mixed-case names the DOM lowercased,
// sorts attributes for deterministic output, XML-escapes values and text, and
// ensures the root carries an xmlns. Elements are always written with an
// explicit end tag (valid XML for empties too).
//
// sm supplies each element's cascaded style so a `fill`/`stroke` resolved by
// an ordinary CSS rule (e.g. Tailwind's `fill-*`/`stroke-*` utility classes) is
// written into the regenerated XML as a `fill`/`stroke` attribute — oksvg
// parses raw SVG presentation attributes only and has no notion of this
// engine's CSS cascade, so without this override a class-driven paint colour
// is silently lost and every such path/shape rasterises with SVG's initial
// fill (black). sm may be nil (e.g. a synthetic/detached subtree with no
// computed styles), in which case every element's own original attributes
// pass through unchanged, exactly as before this override existed.
func serializeSVG(n *dom.Node, sm css.StyleMap) string {
	var sb strings.Builder
	writeSVGNode(&sb, n, true, sm)
	return sb.String()
}

func writeSVGNode(sb *strings.Builder, n *dom.Node, root bool, sm css.StyleMap) {
	switch n.Type {
	case dom.Text:
		xmlEscapeText(sb, n.Text)
		return
	case dom.Element:
		// handled below
	default:
		return
	}
	tag := n.Tag
	if c, ok := svgTagCase[tag]; ok {
		tag = c
	}
	sb.WriteByte('<')
	sb.WriteString(tag)
	attrs := n.Attr
	if st := sm[n]; st != nil {
		if st.FillSet || st.FillNone || st.StrokeSet || st.StrokeNone {
			attrs = make(map[string]string, len(n.Attr)+2)
			for k, v := range n.Attr {
				attrs[k] = v
			}
			switch {
			case st.FillNone:
				attrs["fill"] = "none"
			case st.FillSet:
				attrs["fill"] = fmt.Sprintf("#%02x%02x%02x", st.Fill.R, st.Fill.G, st.Fill.B)
			}
			switch {
			case st.StrokeNone:
				attrs["stroke"] = "none"
			case st.StrokeSet:
				attrs["stroke"] = fmt.Sprintf("#%02x%02x%02x", st.Stroke.R, st.Stroke.G, st.Stroke.B)
			}
		}
	}
	keys := make([]string, 0, len(attrs))
	for k := range attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	hasXMLNS := false
	for _, k := range keys {
		name := k
		if c, ok := svgAttrCase[k]; ok {
			name = c
		}
		if k == "xmlns" {
			hasXMLNS = true
		}
		sb.WriteByte(' ')
		sb.WriteString(name)
		sb.WriteString(`="`)
		xmlEscapeAttr(sb, attrs[k])
		sb.WriteByte('"')
	}
	if root && !hasXMLNS {
		sb.WriteString(` xmlns="http://www.w3.org/2000/svg"`)
	}
	sb.WriteByte('>')
	for _, c := range n.Children {
		writeSVGNode(sb, c, false, sm)
	}
	sb.WriteString("</")
	sb.WriteString(tag)
	sb.WriteByte('>')
}

func xmlEscapeAttr(sb *strings.Builder, s string) {
	for _, r := range s {
		switch r {
		case '&':
			sb.WriteString("&amp;")
		case '<':
			sb.WriteString("&lt;")
		case '>':
			sb.WriteString("&gt;")
		case '"':
			sb.WriteString("&quot;")
		default:
			sb.WriteRune(r)
		}
	}
}

func xmlEscapeText(sb *strings.Builder, s string) {
	for _, r := range s {
		switch r {
		case '&':
			sb.WriteString("&amp;")
		case '<':
			sb.WriteString("&lt;")
		case '>':
			sb.WriteString("&gt;")
		default:
			sb.WriteRune(r)
		}
	}
}

// RasterizeSVG renders SVG source into a bitmap of exactly w×h pixels,
// whatever the document's own width/height say. The size is the caller's to
// pick, because an SVG has no pixels of its own — which is the whole point of
// this entry: the bitmap LoadImageSet hands back is sized for the RASTER
// CANVAS, where one CSS px is one device px, and on paper that is a hard 96
// dpi ceiling. A PDF exporter is the case this exists for. Together with the
// serialisation LoadedImage.Data carries for an inline <svg>, it can render
// the same drawing again at the density the paper deserves.
//
// currentColor is what `currentColor` resolves to, as a CSS colour string;
// empty leaves the document's own. ok is false for a non-positive size or
// source oksvg cannot parse — the same failure the internal path reports, and
// the same recover() around it, since oksvg panics on some malformed input.
// w and h are clamped to maxSVGDim.
func RasterizeSVG(data []byte, w, h int, currentColor string) (img image.Image, ok bool) {
	defer func() {
		if recover() != nil {
			img, ok = nil, false
		}
	}()
	if w <= 0 || h <= 0 {
		return nil, false
	}
	if w > maxSVGDim {
		w = maxSVGDim
	}
	if h > maxSVGDim {
		h = maxSVGDim
	}
	data, _, _ = sanitizeSVGRoot(data)
	var (
		icon *oksvg.SvgIcon
		err  error
	)
	if currentColor != "" && bytes.Contains(data, []byte("currentColor")) {
		icon, err = oksvg.ReadReplacingCurrentColor(bytes.NewReader(data), currentColor, oksvg.IgnoreErrorMode)
	} else {
		icon, err = oksvg.ReadIconStream(bytes.NewReader(data), oksvg.IgnoreErrorMode)
	}
	if err != nil || icon == nil || icon.ViewBox.W <= 0 || icon.ViewBox.H <= 0 {
		return nil, false
	}
	return svgRasterizer(icon, w, h, iround(icon.ViewBox.W), iround(icon.ViewBox.H)), true
}
