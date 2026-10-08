// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

// Package css implements a deliberately small but real subset of CSS: a value
// model, a tokenizer/parser for stylesheets and declaration blocks, tag/class/
// id selectors with specificity, and a cascade with inheritance over a dom
// tree. It targets the handful of properties Phase 0 needs — display, color,
// background-color, font-size, font-weight, font-family, margin, padding,
// width, text-align — plus a user-agent default stylesheet for common tags.
package css

import (
	"strconv"
	"strings"

	gfxcolor "github.com/go-gfx/gfx/color"
)

// Color is an 8-bit-per-channel RGBA colour. A==0 is treated as transparent.
type Color struct{ R, G, B, A uint8 }

// Transparent is the fully-transparent colour (the initial background-color).
var Transparent = Color{}

// Visibility is the visibility property: unlike Display, it is INHERITED, and
// hiding an ancestor still reserves its layout space and does not stop a
// descendant that resets `visibility:visible` from painting. `collapse` (its
// real effect is table-row-specific: the row's space is reclaimed) is treated
// identically to VisibilityHidden — a documented simplification, not a bug.
type Visibility uint8

const (
	// VisibilityVisible is the initial value.
	VisibilityVisible Visibility = iota
	// VisibilityHidden paints nothing for this box (background, border,
	// shadows, inline content) while still occupying its layout space; a
	// descendant box may re-show itself with its own `visibility:visible`.
	VisibilityHidden
	// VisibilityCollapse is treated the same as VisibilityHidden (see above).
	VisibilityCollapse
)

// Display is the subset of the display property the engine understands.
type Display uint8

const (
	// DisplayInline is the default for unknown/inline elements.
	DisplayInline Display = iota
	// DisplayBlock stacks the box vertically in block flow.
	DisplayBlock
	// DisplayNone removes the element (and subtree) from layout.
	DisplayNone
	// DisplayInlineBlock is an atomic inline-level block (laid out as a block
	// but participating inline; Phase 1 treats it as block for simplicity).
	DisplayInlineBlock
	// DisplayFlex is a block-level flex container.
	DisplayFlex
	// DisplayTable is a block-level table box.
	DisplayTable
	// DisplayTableRow is a table row box.
	DisplayTableRow
	// DisplayTableCell is a table cell box.
	DisplayTableCell
	// DisplayTableRowGroup is a thead/tbody/tfoot grouping box (transparent to
	// the table's row collection).
	DisplayTableRowGroup
	// DisplayGrid is a block-level CSS grid container.
	DisplayGrid
	// DisplayContents makes the element generate no box of its own — its
	// children behave as if they were direct children of ITS parent instead
	// (the element itself becomes fully transparent to layout, though it still
	// exists in the DOM/cascade). See layout.renderedChildren, the single
	// chokepoint every layout algorithm (block, inline, flex, grid, table)
	// walks a container's children through, which is where this is resolved —
	// so a DisplayContents element never itself reaches box placement.
	DisplayContents
	// DisplayInlineFlex is a flex container that is itself an INLINE-level
	// box in its parent's flow (unlike DisplayFlex, which is block-level) —
	// distinct from DisplayFlex specifically so isBlockLevel and the inline-
	// collection walk can tell them apart; internally it lays out its
	// children with the identical flex algorithm. Confirmed live on
	// pkg.go.dev: `.go-Breadcrumb li{display:inline-flex}` stacked each
	// breadcrumb item on its own line instead of flowing in a row, because
	// this engine previously parsed `inline-flex` down to the SAME
	// DisplayFlex value as `flex`, losing the "inline" qualifier entirely and
	// making every such element block-level. See
	// layout.appendElementInline's own handling for how the atomic
	// inline-level box is produced.
	DisplayInlineFlex
)

// Position is the subset of the position property the engine understands.
type Position uint8

const (
	// PositionStatic is the initial value: the box is in normal flow with no
	// top/right/bottom/left offset applied.
	PositionStatic Position = iota
	// PositionRelative keeps the box in normal flow (it still reserves space) but
	// paints it shifted by its top/left/right/bottom offset.
	PositionRelative
	// PositionAbsolute removes the box from normal flow and positions it against
	// the padding box of the nearest positioned ancestor (else the initial
	// containing block).
	PositionAbsolute
	// PositionFixed removes the box from normal flow and positions it against the
	// initial containing block (the viewport). For a full-page static render it is
	// resolved to document coordinates so it paints once at its place.
	PositionFixed
	// PositionSticky is approximated as relative for a full-page static shot.
	PositionSticky
)

// OutOfFlow reports whether a position value takes the box out of normal flow
// (so it reserves no space in its parent's block/inline formatting context).
func (p Position) OutOfFlow() bool { return p == PositionAbsolute || p == PositionFixed }

// Positioned reports whether a position value makes the box a containing block
// for absolutely-positioned descendants (anything other than static).
func (p Position) Positioned() bool { return p != PositionStatic }

// Float is the subset of the float property the engine understands.
type Float uint8

const (
	// FloatNone is the initial value (no float).
	FloatNone Float = iota
	// FloatLeft floats the box to the left of its container.
	FloatLeft
	// FloatRight floats the box to the right of its container.
	FloatRight
)

// Clear is the subset of the clear property the engine understands.
type Clear uint8

const (
	// ClearNone is the initial value.
	ClearNone Clear = iota
	// ClearLeft clears past left floats.
	ClearLeft
	// ClearRight clears past right floats.
	ClearRight
	// ClearBoth clears past floats on both sides.
	ClearBoth
)

// Break is a break-before / break-after value (CSS Fragmentation Level 3)
// as it matters to paged media: a forced page break (page, left, right), an
// avoided one, or neither. The column- and region-only values (column,
// avoid-column, region, avoid-region) say nothing about pages and map to
// BreakAuto. This engine's own screen layout never fragments; the value is
// carried on Style for a paginating consumer (go-pdfkit/html2pdf and the
// paginate package) to read.
type Break uint8

const (
	// BreakAuto is the initial value: neither force nor forbid a break.
	BreakAuto Break = iota
	// BreakAvoid avoids a page break at this edge (avoid, avoid-page).
	BreakAvoid
	// BreakPage forces a page break: page, the legacy `always`, all, recto
	// and verso — any forced break that does not also choose a page side.
	BreakPage
	// BreakLeft forces one or two page breaks so the next page is a left page.
	BreakLeft
	// BreakRight forces one or two page breaks so the next page is a right
	// page.
	BreakRight
)

// parseBreakKeyword maps a break-before / break-after keyword to its value,
// reporting whether it was recognised. The legacy page-break-* spelling
// `always` is accepted from either property family (it is the alias's only
// value that differs from the modern one). The CSS-wide initial and unset
// keywords both mean auto here: neither property is inherited.
func parseBreakKeyword(s string) (Break, bool) {
	switch s {
	case "auto", "initial", "unset", "column", "avoid-column", "region", "avoid-region":
		return BreakAuto, true
	case "avoid", "avoid-page":
		return BreakAvoid, true
	case "page", "always", "all", "recto", "verso":
		return BreakPage, true
	case "left":
		return BreakLeft, true
	case "right":
		return BreakRight, true
	}
	return BreakAuto, false
}

// BreakInside is a break-inside value for paged media.
type BreakInside uint8

const (
	// BreakInsideAuto is the initial value: a page break may fall inside the
	// box.
	BreakInsideAuto BreakInside = iota
	// BreakInsideAvoid asks that no page break fall inside the box (avoid,
	// avoid-page). avoid-column / avoid-region say nothing about pages and
	// map to BreakInsideAuto.
	BreakInsideAvoid
)

// parseBreakInsideKeyword maps a break-inside keyword to its value, reporting
// whether it was recognised; initial and unset both mean auto (not
// inherited), as in parseBreakKeyword.
func parseBreakInsideKeyword(s string) (BreakInside, bool) {
	switch s {
	case "auto", "initial", "unset", "avoid-column", "avoid-region":
		return BreakInsideAuto, true
	case "avoid", "avoid-page":
		return BreakInsideAvoid, true
	}
	return BreakInsideAuto, false
}

// BoxSizing selects whether width/height apply to the content box or the
// border box.
type BoxSizing uint8

const (
	// ContentBox is the initial value: width is the content width.
	ContentBox BoxSizing = iota
	// BorderBox: width includes padding and border.
	BorderBox
)

// Overflow is the computed overflow of one axis. For the engine's headless
// paint every non-visible value (hidden/clip/scroll/auto) clips descendant
// painting to the box's padding box — there is no interactive scrolling, so a
// scroll/auto container renders exactly its visible window, matching the first
// paint a user would see. This is what keeps the universal `sr-only` /
// visually-hidden pattern (position:absolute;width:1px;height:1px;
// overflow:hidden;clip) from painting its screen-reader text at full size.
type Overflow uint8

const (
	// OverflowVisible is the initial value: content is not clipped.
	OverflowVisible Overflow = iota
	OverflowHidden
	OverflowClip
	OverflowScroll
	OverflowAuto
)

// Clips reports whether this overflow value clips descendant painting.
func (o Overflow) Clips() bool { return o != OverflowVisible }

// parseOverflowKeyword maps a single overflow keyword to its value, reporting
// whether it was recognised.
func parseOverflowKeyword(s string) (Overflow, bool) {
	switch s {
	case "visible":
		return OverflowVisible, true
	case "hidden":
		return OverflowHidden, true
	case "clip":
		return OverflowClip, true
	case "scroll":
		return OverflowScroll, true
	case "auto":
		return OverflowAuto, true
	}
	return OverflowVisible, false
}

// parseClipRect parses the legacy `clip: rect(top, right, bottom, left)`
// value (CSS2's required comma-separated form, and the later space-separated
// relaxation both accepted). Only the case every real-world use of this
// property this engine has met actually needs — all four edges given as
// explicit lengths — is modelled; a bare `auto` edge, a percentage (the spec
// forbids one anyway), or anything else parseLength does not resolve makes
// the whole value unrecognised (ok=false) rather than guessed at.
func parseClipRect(v string, emRef float64) (Edges, bool) {
	v = strings.TrimSpace(v)
	if !strings.HasPrefix(v, "rect(") || !strings.HasSuffix(v, ")") {
		return Edges{}, false
	}
	inner := v[len("rect(") : len(v)-1]
	fields := strings.FieldsFunc(inner, func(r rune) bool { return r == ',' || r == ' ' })
	if len(fields) != 4 {
		return Edges{}, false
	}
	var vals [4]float64
	for i, f := range fields {
		l, ok := parseLength(f, emRef)
		if !ok || l.Auto || l.IsPercent {
			return Edges{}, false
		}
		vals[i] = l.Px
	}
	return Edges{Top: vals[0], Right: vals[1], Bottom: vals[2], Left: vals[3]}, true
}

// BorderStyle is the subset of border-style the engine paints. Any non-none,
// non-hidden line style renders as a solid line (dashed/dotted/etc. collapse to
// solid at this fidelity).
type BorderStyle uint8

const (
	// BorderNone is the initial value: no border line (even if width > 0).
	BorderNone BorderStyle = iota
	// BorderSolid renders a solid line of the border colour.
	BorderSolid
)

// BorderSide is one edge's border: its width, line style and colour.
type BorderSide struct {
	Width float64
	Style BorderStyle
	Color Color
}

// paints reports whether the side draws a visible line.
func (b BorderSide) paints() bool { return b.Width > 0 && b.Style != BorderNone && b.Color.A > 0 }

// Borders is the four border edges of a box.
type Borders struct{ Top, Right, Bottom, Left BorderSide }

// Widths returns the four border widths as Edges (0 when the style is none, so
// layout only reserves space for painted borders — matching a border-style:none
// edge contributing no width even if border-width is set).
func (b Borders) Widths() Edges {
	w := func(s BorderSide) float64 {
		if s.Style == BorderNone {
			return 0
		}
		return s.Width
	}
	return Edges{Top: w(b.Top), Right: w(b.Right), Bottom: w(b.Bottom), Left: w(b.Left)}
}

// FlexDirection is the main-axis direction of a flex container.
type FlexDirection uint8

const (
	// FlexRow lays items along the inline (horizontal) axis.
	FlexRow FlexDirection = iota
	// FlexColumn lays items along the block (vertical) axis.
	FlexColumn
)

// Justify is the main-axis distribution (justify-content).
type Justify uint8

const (
	// JustifyStart packs items at the main-start edge (initial value).
	JustifyStart Justify = iota
	// JustifyEnd packs items at the main-end edge.
	JustifyEnd
	// JustifyCenter centres items on the main axis.
	JustifyCenter
	// JustifySpaceBetween distributes free space between items.
	JustifySpaceBetween
	// JustifySpaceAround distributes free space around items.
	JustifySpaceAround
	// JustifySpaceEvenly distributes free space evenly incl. the ends.
	JustifySpaceEvenly
)

// AlignItems is the cross-axis alignment (align-items). In a grid container it
// is also reused for the block-axis (align-items) and, via JustifyItems, the
// inline-axis (justify-items) alignment of items within their cells.
type AlignItems uint8

const (
	// AlignStretch stretches items to fill the cross axis (initial value).
	AlignStretch AlignItems = iota
	// AlignFlexStart aligns items at the cross-start edge.
	AlignFlexStart
	// AlignFlexEnd aligns items at the cross-end edge.
	AlignFlexEnd
	// AlignCenterItems centres items on the cross axis.
	AlignCenterItems
)

// FlexWrap controls whether flex items wrap onto multiple lines.
type FlexWrap uint8

const (
	// FlexNoWrap keeps all items on a single line (initial value).
	FlexNoWrap FlexWrap = iota
	// FlexWrapOn wraps items onto new lines toward the cross-end.
	FlexWrapOn
	// FlexWrapReverse wraps items with the cross axis reversed.
	FlexWrapReverse
)

// AlignContent distributes flex lines (or grid tracks) along the cross axis
// when there is spare cross-axis space (multi-line flex / align-content).
type AlignContent uint8

const (
	// AlignContentStretch stretches lines to fill the cross axis (initial).
	AlignContentStretch AlignContent = iota
	// AlignContentStart packs lines at the cross-start edge.
	AlignContentStart
	// AlignContentEnd packs lines at the cross-end edge.
	AlignContentEnd
	// AlignContentCenter centres the lines as a group.
	AlignContentCenter
	// AlignContentSpaceBetween spreads lines with the ends flush.
	AlignContentSpaceBetween
	// AlignContentSpaceAround spreads lines with half-gaps at the ends.
	AlignContentSpaceAround
	// AlignContentSpaceEvenly spreads lines with equal gaps incl. the ends.
	AlignContentSpaceEvenly
)

// AlignSelf overrides a single item's cross-axis alignment. Auto (the initial
// value) defers to the container's align-items.
type AlignSelf uint8

const (
	// AlignSelfAuto uses the container's align-items value (initial).
	AlignSelfAuto AlignSelf = iota
	// AlignSelfStretch stretches this item on the cross axis.
	AlignSelfStretch
	// AlignSelfStart aligns this item at the cross-start edge.
	AlignSelfStart
	// AlignSelfEnd aligns this item at the cross-end edge.
	AlignSelfEnd
	// AlignSelfCenter centres this item on the cross axis.
	AlignSelfCenter
)

// resolve returns the effective AlignItems for an item, falling back to the
// container's align-items when the item's align-self is auto.
func (a AlignSelf) Resolve(container AlignItems) AlignItems {
	switch a {
	case AlignSelfStretch:
		return AlignStretch
	case AlignSelfStart:
		return AlignFlexStart
	case AlignSelfEnd:
		return AlignFlexEnd
	case AlignSelfCenter:
		return AlignCenterItems
	default:
		return container
	}
}

// Generic is one of CSS's generic font families — the bucket a run falls into
// when none of the families it named is available.
type Generic uint8

const (
	// GenericSans is the default sans-serif bucket.
	GenericSans Generic = iota
	// GenericSerif is the serif bucket.
	GenericSerif
	// GenericMono is the monospace bucket.
	GenericMono
)

// FontFamily is a resolved `font-family`: the families the declaration named,
// in order, and the generic bucket to fall back on when none of them can be
// loaded.
//
// It used to be the bucket alone, which meant a document could never ask for
// a typeface by name: a poster written for IBM Plex Sans and Spectral was
// typeset in the bundled Inter and Lora, silently and at different metrics.
// Names is what an @font-face rule (see FontFace) is matched against.
//
// It stays comparable — a struct of a string and a byte — because it is a map
// key in the font layer, which caches a parsed face per (family, weight,
// slant).
type FontFamily struct {
	// Names are the non-generic families named, lowercased and joined with
	// commas in declaration order. Empty when the declaration named only
	// generic keywords.
	Names string
	// Generic is the bucket to use when no named family is available — the
	// generic keyword the declaration ended with, or the one implied by a
	// name the engine recognises ("helvetica" means sans).
	Generic Generic
}

// The three generic families, for the places that mean the bucket itself: the
// UA default, and the bundled faces the font layer registers. Values rather
// than constants because Go has no struct constant; do not assign to them.
var (
	// Sans is the sans-serif bucket with no named family.
	Sans = FontFamily{Generic: GenericSans}
	// Serif is the serif bucket with no named family.
	Serif = FontFamily{Generic: GenericSerif}
	// Mono is the monospace bucket with no named family.
	Mono = FontFamily{Generic: GenericMono}
)

// NamedFamilies returns the families this declaration named, in the order it
// named them — what a consumer walks to find the first face it holds.
func (f FontFamily) NamedFamilies() []string {
	if f.Names == "" {
		return nil
	}
	return strings.Split(f.Names, ",")
}

// TextTransform is `text-transform`: the capitalisation a run of text is
// RENDERED with, leaving the document's own characters untouched. It is
// applied where layout turns a text node into items, so measurement and
// painting see the same string — a label written "Partners" and styled
// uppercase must be measured as "PARTNERS", which is wider.
//
// full-width and full-size-kana are not implemented; they parse as TTNone.
type TextTransform uint8

const (
	// TTNone renders the text as the document wrote it.
	TTNone TextTransform = iota
	// TTUppercase renders every character in upper case.
	TTUppercase
	// TTLowercase renders every character in lower case.
	TTLowercase
	// TTCapitalize upper-cases the first letter of each word.
	TTCapitalize
)

// WhiteSpace controls collapsing of whitespace and wrapping.
type WhiteSpace uint8

const (
	// WSNormal collapses runs of whitespace and wraps at the block width.
	WSNormal WhiteSpace = iota
	// WSPre preserves spaces and newlines and does not wrap (as in <pre>).
	WSPre
	// WSNoWrap collapses whitespace like WSNormal but never wraps: the
	// block's content stays on one line, however wide.
	WSNoWrap
)

// ImageRendering selects the resampling filter used to scale a raster image
// (an <img> or a background-image) to a size other than its intrinsic one. It
// inherits (per CSS Images 3).
type ImageRendering uint8

const (
	// IRAuto lets the engine pick a high-quality (smooth) filter — a bicubic
	// resample. It is the initial value and also covers `smooth` / `high-quality`.
	IRAuto ImageRendering = iota
	// IRPixelated preserves hard pixel edges by nearest-neighbour sampling, the
	// behaviour pixel art wants (`image-rendering: pixelated`). `crisp-edges`
	// maps here too: both ask the engine not to smooth the image.
	IRPixelated
)

// ListStyleType is the marker style of a display:list-item box. It inherits.
type ListStyleType uint8

const (
	// ListDisc is a filled circle (the initial value / <ul> default).
	ListDisc ListStyleType = iota
	// ListCircle is a hollow (stroked) circle.
	ListCircle
	// ListSquare is a filled square.
	ListSquare
	// ListDecimal is an ascending decimal number ("1.", "2.", …) — the <ol> default.
	ListDecimal
	// ListNone paints no marker.
	ListNone
)

// ListStylePosition is where the marker sits relative to the item's content. It
// inherits.
type ListStylePosition uint8

const (
	// ListOutside places the marker in the indent to the left of the content box
	// (the initial value).
	ListOutside ListStylePosition = iota
	// ListInside places the marker inside the content box, before the first line.
	ListInside
)

// TextAlign is the horizontal alignment of inline content in a block.
type TextAlign uint8

const (
	// AlignLeft is the initial value.
	AlignLeft TextAlign = iota
	// AlignCenter centres each line.
	AlignCenter
	// AlignRight right-aligns each line.
	AlignRight
	// AlignCenterBlocks centres inline content like AlignCenter and additionally
	// centres a definite-width block/table child within its container — the legacy
	// behaviour of the <center> element and the align="center" attribute (browsers'
	// -moz-center / -webkit-center). A plain text-align:center author rule never
	// produces it, so standards-mode blocks are unaffected.
	AlignCenterBlocks
)

// Edges holds the four sides of a margin or padding box, in pixels.
type Edges struct{ Top, Right, Bottom, Left float64 }

// Length is a resolved CSS length. Percentages are kept separately so the
// layout can resolve them against the containing block's width.
type Length struct {
	Px        float64
	Percent   float64 // 0..1; only meaningful when IsPercent
	IsPercent bool
	Auto      bool
}

// Resolve returns the length in pixels against a containing-block width.
func (l Length) Resolve(containing float64) float64 {
	if l.IsPercent {
		return l.Percent * containing
	}
	return l.Px
}

// Style is the fully-computed style of an element, after cascade + inheritance.
type Style struct {
	Display    Display
	Visibility Visibility // inherited; see the Visibility doc comment
	Color      Color
	Background Color
	FontSize   float64 // px
	FontWeight int     // 400 = normal, 700 = bold
	FontFamily FontFamily
	Italic     bool // font-style: italic|oblique (inherited)
	// Underline is set by `text-decoration`/`text-decoration-line: underline`,
	// cleared by any other explicit line value (including "none"). Real CSS
	// text-decoration-line is NOT an inherited property — instead, a line set
	// on a block/inline CONTAINER visually propagates through its descendant
	// inline boxes unless a descendant sets its own line explicitly. This
	// engine approximates that (real) propagation the same way an actually-
	// inherited property would: copied from parent to child, then
	// overwritten by an explicit declaration on the child itself — correct
	// for the overwhelming majority of real content (an <a> or a heading
	// carries `text-decoration:underline`; its own descendant <strong>/<code>
	// inherits the line via this field exactly as it should), and only wrong
	// for the rare case of a descendant deliberately opting OUT with its own
	// `text-decoration-line:none` while an ancestor's line is meant to still
	// show through — not modelled here. Only the "underline" line is tracked
	// (line-through/overline/blink are not) and text-decoration-color/style/
	// thickness are not modelled: the rendered line is always solid, in the
	// text's own current colour — deliberately narrow, matching the
	// overwhelmingly common real-world "a hyperlink is underlined" pattern
	// this was found missing for entirely: text-decoration had NO Style
	// field, no parser case and no paint code at all before this — every
	// underline on every page this engine has ever rendered was silently
	// dropped, confirmed live on developer.mozilla.org's own in-article
	// links (`:is(.content-section a):not([href^="#"])
	// {text-decoration:underline}`, present in real Chrome, absent here).
	Underline bool

	// Fill/Stroke are the SVG paint properties (inherited, like Color).
	// FillSet/StrokeSet distinguish "CSS resolved a concrete colour" from
	// "left to the SVG document's own presentation attribute" — svg.go's
	// serializeSVG is the only reader: unset changes nothing (the element's
	// own `fill`/`stroke` XML attribute, if any, survives untouched), set
	// overrides it with the CSS value. FillNone/StrokeNone models
	// `fill:none`/`stroke:none` (paint suppressed) separately, since Color{}
	// (transparent black) is also a legitimate real colour value. Confirmed
	// load-bearing live: tailwindcss.com's nav icons (search glyph, version-
	// badge chevron, logo mark) and CTA underline are coloured entirely via
	// Tailwind's `fill-*`/`stroke-*` utility classes, never an XML `fill=`
	// attribute — before this, every such icon rasterised with SVG's initial
	// fill (black), often invisible against a dark background.
	Fill       Color
	FillSet    bool
	FillNone   bool
	Stroke     Color
	StrokeSet  bool
	StrokeNone bool

	Margin    Edges
	Padding   Edges
	Border    Borders
	Width     Length // Auto by default
	MinWidth  Length // Auto (== none) by default
	MaxWidth  Length // Auto (== none) by default
	Height    Length // Auto by default
	MinHeight Length // Auto (== none) by default
	MaxHeight Length // Auto (== none) by default
	// AspectRatio is the preferred width/height ratio from `aspect-ratio:
	// <W>/<H>` (0 means "auto", i.e. unset — no ratio constrains sizing).
	// Confirmed load-bearing live (round 86): Next.js's own auto-generated
	// image-placeholder wrapper divs (tailwindcss.com's blog/changelog cover
	// images) size themselves ENTIRELY via `aspect-ratio` plus a resolved
	// width, with no explicit height at all — reserving the correct space
	// before the image itself loads, to avoid a layout shift. Only the
	// width-known/height-auto direction is resolved (see usedHeight); the
	// reverse (height known, width auto) has no confirmed real caller and is
	// not attempted.
	AspectRatio float64
	// ObjectFit controls how a replaced element's OWN content (the loaded
	// image bitmap) is scaled into its box when the two aspect ratios don't
	// match — see ObjectFit's own doc comment. Fill (the zero value) is this
	// engine's own pre-existing behaviour (stretch to exactly fill the box).
	ObjectFit     ObjectFit
	BoxSizing     BoxSizing
	TextAlign     TextAlign
	WhiteSpace    WhiteSpace
	TextTransform TextTransform
	LineHeight    LineHeight

	// LetterSpacing is `letter-spacing`'s resolved value in px (0 = `normal`,
	// the initial value), added after EVERY character of a text run
	// (including its last, matching real browsers — this is why a run's
	// measured width grows even for a single-character string). Inherited,
	// per spec. Only the line-wrapping width (layout.appendWords, the
	// confirmed real path — tailwindcss.com's own large `tracking-tighter`
	// hero heading and `tracking-widest` sidebar labels) and painting
	// (paint.drawText) apply it; the shrink-to-fit/min-content intrinsic-
	// sizing paths (preferredWidth, minContentWidth, flex/table sizing) do
	// not add it to their own measurements — a documented, narrower scope
	// than every possible consumer of Measure, matching this engine's
	// established practice of shipping the confirmed-reachable path first.
	LetterSpacing float64

	// BorderSpacingH/V are `border-spacing`'s horizontal/vertical values in
	// px (both 0 = the initial value, no gap). Inherited, per spec (one of a
	// handful of table-specific properties CSS inherits despite table
	// layout itself not otherwise being inherited). Confirmed real on
	// en.wikipedia.org's own infobox (`.infobox{border-spacing:3px}`, cells
	// styled with only a `border-bottom` — the gap this property adds
	// between rows IS the infobox's visible row-to-row breathing room, not
	// merely a cosmetic nicety). layout.table applies it unconditionally,
	// without checking `border-collapse` (not modelled at all yet, so every
	// table is implicitly always "separate", the default) — spec says
	// border-spacing has no effect under `border-collapse:collapse`, but
	// both of this engine's own confirmed real cases are unaffected by that
	// simplification: Wikipedia's infobox never sets border-collapse at all
	// (stays at the default 'separate' this engine already assumes), and
	// pkg.go.dev's own `table{border-collapse:collapse;border-spacing:0}`
	// reset sets spacing to zero anyway, so applying it unconditionally
	// there is a no-op regardless.
	BorderSpacingH, BorderSpacingV float64

	// CenterAsBlock is TextAlign == AlignCenterBlocks for every element,
	// EXCEPT it stays true for a <table> whose PARENT has TextAlign ==
	// AlignCenterBlocks even in quirks mode, where the table's OWN TextAlign
	// is separately reset to AlignLeft (see css/ua.go's quirks-mode table
	// rule) so its cells' text is correctly left-, not centre-, aligned.
	// Layout's "centre a definite-width block within its container" rule
	// (the legacy <center>/align=center effect) reads THIS field rather than
	// TextAlign directly, because real browsers keep the two questions
	// (should MY OWN text be centred vs. should I, as a block, be centred
	// within my parent) independent for exactly this one quirks-mode case —
	// confirmed against the HTML standard, whose quirks-mode reset is scoped
	// to the `table` selector alone. Every other element (including a
	// <center> with its own definite width, which must centre ITSELF using
	// its own AlignCenterBlocks default regardless of its parent) is
	// unaffected: CenterAsBlock and TextAlign==AlignCenterBlocks always
	// agree for it.
	CenterAsBlock bool

	// ImageRendering selects the scaling filter for raster images (inherited).
	// The initial value IRAuto means high-quality (bicubic); IRPixelated asks
	// for nearest-neighbour (pixel art).
	ImageRendering ImageRendering

	// ListItem marks a display:list-item box (it generates a marker). It is not
	// inherited. ListStyleType and ListStylePosition are inherited and select the
	// marker glyph and its placement.
	ListItem          bool
	ListStyleType     ListStyleType
	ListStylePosition ListStylePosition

	// BorderRadius is the corner radius applied to the border box when painting
	// the background and border. It is a single (uniform) radius: the common real
	// case (Tailwind `rounded-*`, pills, circles) sets all four corners equal.
	// Differing per-corner radii are approximated by the last-applied value
	// (documented in FIDELITY.md). A px length is used as-is; a percentage
	// resolves against the box's smaller side at paint time. Zero (the initial
	// value) means square corners.
	BorderRadius Length

	// Auto-margin flags: a margin explicitly set to `auto` centres or pushes the
	// box; distinct from a 0 margin.
	MarginLeftAuto  bool
	MarginRightAuto bool
	// MarginLeft/RightIsPercent + MarginLeft/RightPercent (0..1) record a
	// percentage margin-left/margin-right — resolved against the containing
	// block's WIDTH at layout time in resolveWidths, the one place that
	// already has cw available for exactly this purpose (mirroring how Width/
	// Height keep percentages unresolved in a Length rather than eagerly
	// computing a wrong value against an unknown containing block). Margin is
	// a plain Edges (float64), not Length, everywhere else, so this is a
	// narrower, side-channel addition rather than a wholesale type change.
	// Scoped to margin-LEFT/RIGHT only — the confirmed real need (Wikipedia's
	// own `.ambox{margin:0 10%}`, `@media(min-width:720px)`-gated, centring
	// article cleanup-notice boxes within their column) only ever needs
	// horizontal percentages; margin-top/margin-bottom percentages (also
	// resolved against the containing block's WIDTH per spec, a common CSS
	// quirk) have no confirmed real caller and are not modelled — a
	// documented scope limit, not a silent gap for the case that matters.
	MarginLeftIsPercent  bool
	MarginLeftPercent    float64
	MarginRightIsPercent bool
	MarginRightPercent   float64

	// Overflow per axis (not inherited; initial value visible). Any non-visible
	// value clips descendant painting to this box's padding box.
	OverflowX Overflow
	OverflowY Overflow

	// HasClip/ClipRect model the legacy `clip: rect(top, right, bottom, left)`
	// property — deprecated in favour of clip-path, but still a live, real-
	// world pattern: confirmed load-bearing on pkg.go.dev's own "skip to main
	// content" link (`clip:rect(0 0 0 0)`, the CSS2-era screen-reader-only
	// idiom, predating the now-common `width:1px;height:1px;overflow:hidden`
	// version this engine already honours via Overflow above — that one has
	// no effect here since this element sets no explicit small width/height,
	// only `clip`). Per spec this only applies when Position is absolute or
	// fixed, and all four edges are distances from the box's own top-left
	// BORDER edge (not the CSS shorthand's usual top/right/bottom/left box
	// edges) — `rect(0,0,0,0)` is therefore a zero-size rectangle at the
	// box's own corner, clipping it and its content to nothing while it
	// still occupies its normal layout position (unlike display:none). Only
	// the common case (all four values explicit lengths) is modelled; a
	// `clip: auto` per edge, or any other value this engine's length parsing
	// does not resolve, leaves HasClip false — clip:rect() with a genuinely
	// variable edge is rare in practice (the sr-only idiom always hard-codes
	// all four to 0), so this is not a real-world loss.
	HasClip  bool
	ClipRect Edges // Top, Right, Bottom, Left offsets from the box's own corner

	// Positioning that affects normal flow.
	Float Float
	Clear Clear

	// CSS position and the box-offset properties. Top/Right/Bottom/Left are Auto
	// by default (the initial value of each offset). ZIndex is meaningful only
	// when ZIndexAuto is false; the initial value is auto (paint in tree order).
	Position   Position
	Top        Length // Auto by default
	Right      Length // Auto by default
	Bottom     Length // Auto by default
	Left       Length // Auto by default
	ZIndex     int
	ZIndexAuto bool // true == "auto" (the initial value)

	// TranslateX/TranslateY and RotateDeg are the TWO subsets of the
	// `transform` property this engine understands, EACH ONLY on its own —
	// a value naming any other function (scale, skew, matrix, 3D), or
	// mixing more than one of these two together (`translate(...)
	// rotate(...)` on the same element), is unsupported and leaves every
	// transform field at zero, same as before either was understood at all
	// — see FIDELITY.md's Known gaps. Translate (see parseTransformTranslate
	// in parse.go) is a paint-time offset applied like a relative position
	// shift, not a layout input; the initial/reset value (Length's Go zero
	// value) is exactly "no translation", so unlike most other reset-not-
	// inherited fields there is nothing to list explicitly in inheritFrom.
	//
	// RotateDeg (`rotate()`/`rotatez()`, see parseTransformRotate) is the
	// angle in degrees to rotate the element's own border-box rectangle by,
	// clockwise for a positive value — CSS's own convention. go-images'
	// own Rotate is the OPPOSITE sign convention (its own doc comment:
	// "rotated by angle degrees counter-clockwise", matching scikit-image),
	// so paint.paintRotated negates the angle it passes in; get this backward
	// and a page with any asymmetric rotated content (readable text/icons,
	// not just tailwindcss.com's own near-symmetric card shapes) would spin
	// the visibly wrong way, caught by checking the sign convention against
	// the dependency's own doc comment before writing any code, not
	// discovered after the fact. Unlike translate, this DOES need real
	// coordinate-transform machinery: paint.paintRotated renders the box's
	// own border-box rectangle to an offscreen buffer and rotates the
	// PIXELS with go-images' Rotate (bilinear, growing the buffer to fit the
	// rotated corners), rather than transforming any drawing coordinates —
	// so it does not compose with `filter`/opacity/`mask-image` on the same
	// element (paintBox only takes this path when none of those apply): no
	// confirmed real trigger combines rotate with any of them, and doing so
	// correctly needs the two offscreen-buffer mechanisms unified, a larger
	// change than this one evidenced case justifies. transform-origin is
	// not modelled either — always the default 50% 50% (the box's own
	// centre), which is what every confirmed trigger already uses.
	//
	// This field backs BOTH `transform: rotate()`/`rotateZ()` (parsed by
	// parseTransformRotate) and the newer, independent CSS Transforms Level
	// 2 `rotate` property (parsed directly in `case "rotate"`, a bare
	// angle with no function-call syntax at all — `rotate:90deg`,
	// `rotate:none`) — two different spellings of the identical visual
	// capability. Confirmed live (round 145): modern Tailwind (v4) compiles
	// its `rotate-*` utilities to the STANDALONE property, not the
	// `transform` function — `.rotate-(--angle){rotate:var(--angle)}` on
	// tailwindcss.com's own "P3 colors" section, a grid of diagonal colour-
	// name labels ("red", "orange", …) tilted -45°, confirmed directly from
	// its compiled CSS bundle rather than assumed from the class name alone
	// (an assumption that would have targeted the wrong property entirely
	// and shipped a fix that silently did nothing on the one confirmed real
	// page). `transform: rotate()` itself has no independently confirmed
	// trigger of its own in this session's corpus — kept anyway since it is
	// the same underlying, now-evidenced capability under CSS's older
	// syntax, not a distinct speculative addition.
	//
	// Both spellings write the SAME field and so cannot compose with each
	// other, or with `transform: translate()`/the (unimplemented) standalone
	// `translate`/`scale` properties, correctly per spec — whichever this
	// engine's cascade applies LAST simply overwrites the field. No
	// confirmed trigger combines any of these, so this is disclosed rather
	// than engineered around.
	TranslateX Length
	TranslateY Length
	RotateDeg  float64

	// AppearanceNone is `appearance: none` (or `-webkit-appearance: none`),
	// which suppresses a form control's native OS chrome so the author's own
	// background/background-image/border/border-radius paint instead. Not
	// inherited; the zero value (false) is the initial `auto`, matching every
	// other form control this engine already renders with its own hardcoded
	// UA-default look (see paintFormControl/paintCheckboxLike). Confirmed
	// load-bearing live: developer.mozilla.org's own `<mdn-switch>` custom
	// toggle-switch component styles a real `<input type=checkbox>` with
	// exactly `appearance:none;background-color:...;background-image:radial-
	// gradient(...);border-radius:9999px` UNCONDITIONALLY (not gated behind
	// `:checked` — only the knob's position/colour REFINES on `:checked`), so
	// a checkbox that sets this must stop taking this engine's own generic
	// checkbox square and instead paint like any other styled box. Only the
	// bare `none`/`auto` keywords are modelled; the CSS Basic UI vendor-
	// specific appearance keywords (`textfield`, `menulist-button`, etc.) have
	// no confirmed trigger and are left unsupported (equivalent to `auto`).
	AppearanceNone bool

	// Flex container properties (meaningful when Display == DisplayFlex).
	FlexDirection  FlexDirection
	FlexWrap       FlexWrap
	JustifyContent Justify
	AlignItems     AlignItems
	AlignContent   AlignContent

	// Gaps between flex lines/items and between grid tracks. RowGap is the
	// cross-line / block-axis gap; ColumnGap is the main-axis / inline-axis gap.
	RowGap    Length
	ColumnGap Length

	// Flex/grid item properties (meaningful for a child of a flex/grid container).
	FlexGrow   float64
	FlexShrink float64
	FlexBasis  Length // Auto == "auto" (use the item's width/content)
	Order      int    // reorders items within a line (ascending)
	AlignSelf  AlignSelf

	// Grid container properties (meaningful when Display == DisplayGrid).
	GridTemplateColumns []TrackSize
	GridTemplateRows    []TrackSize
	GridAutoRows        TrackSize
	GridAutoColumns     TrackSize
	GridAutoFlow        GridFlow
	// GridAutoFlowDense is the "dense" keyword on grid-auto-flow: the
	// auto-placement cursor restarts from the very first grid cell before
	// placing EACH item (potentially reordering items visually, out of DOM
	// order, to backfill an earlier gap a bigger item left open) instead of
	// only ever advancing forward from wherever the previous item landed
	// (the default, "sparse", packing). See layout.placeItems.
	GridAutoFlowDense bool
	GridTemplateAreas [][]string // row-major grid of area names ("" == empty)
	JustifyItems      AlignItems // inline-axis alignment of items in their cell

	// Grid item placement (meaningful for a child of a grid container).
	GridColumnStart GridLine
	GridColumnEnd   GridLine
	GridRowStart    GridLine
	GridRowEnd      GridLine
	GridArea        string // named area this item is placed into (via grid-area)
	JustifySelf     AlignSelf

	// CustomProps holds the element's resolved CSS custom properties (--name ->
	// raw value). It is inherited from the parent and overridden by matched
	// rules; var() references consult it at computed-value time. Nil until an
	// element (or an ancestor) defines a custom property.
	CustomProps map[string]string

	// Background image layers (gradients and url() bitmaps) and their paint
	// parameters. Each list is indexed per layer (first-listed paints on top);
	// a shorter size/position/repeat list repeats its last value. All nil == no
	// background image (only the solid Background colour paints).
	BackgroundImages   []BgImage
	BackgroundSize     []BgSize
	BackgroundPosition []BgPosition
	BackgroundRepeat   []BgRepeat

	// MaskImage is the resolved absolute URL of a `mask-image`/
	// `-webkit-mask-image: url(...)` — empty means no mask (the common case).
	// Unlike background-image, a mask does not paint its own pixels: it is an
	// alpha stencil applied over everything the ELEMENT ITSELF paints (colour,
	// background, borders, text, children), stretched to fill the element's
	// border box (this engine's one deliberate simplification — the real
	// mask-size/mask-position/mask-repeat grammar is not modelled, matching
	// how far this engine narrowly scoped `transform: translate()`). Confirmed
	// load-bearing live: modern MediaWiki (Wikipedia's Vector-2022 skin, and
	// most of its UI icon systems generally) renders EVERY toolbar icon —
	// hamburger menu, search, language switcher — as an empty, solid-coloured
	// <span> cut into shape by exactly this mechanism, so the icon recolours
	// for dark mode without needing a second image asset. A gradient or other
	// non-url() mask value is not modelled and leaves this empty.
	MaskImage string

	// BoxShadows are the element's box-shadow layers (first-listed paints on top).
	BoxShadows []BoxShadow

	// Opacity is the element's group opacity in [0,1]; HasOpacity distinguishes a
	// genuine opacity from the zero value (an unset Style is fully opaque).
	Opacity    float64
	HasOpacity bool

	// Filters is the element's `filter` function chain, applied in order to the
	// element's rendered output (its box plus subtree) as a group. Nil == no
	// filter. Not inherited (`filter` is a non-inherited property).
	Filters []Filter
	// BackdropFilters is `backdrop-filter`'s function chain (same grammar and
	// Filter type as Filters — see parseFilterList) applied to whatever has
	// already been painted BEHIND this box's own border-box, before the box's
	// own background/border/content paint on top — the standard "frosted
	// glass" idiom (a translucent bar over blurred page content). Confirmed
	// live on react.dev's own sticky nav bar and code-sandbox title bars
	// (`backdrop-filter:blur(16px) saturate(2)` via Tailwind's `backdrop-blur-
	// lg backdrop-saturate-200`, composed through CSS custom properties). Not
	// inherited, matching Filters.
	BackdropFilters []Filter

	// ContainerType and ContainerName are `container-type`/`container-name`
	// (or the `container` shorthand): whether this element establishes a
	// query container for descendant `@container` rules, and under what
	// name. Neither is inherited; the initial value is ContainerNormal / "".
	ContainerType ContainerType
	ContainerName string

	// Fragmentation (CSS Fragmentation Level 3), as far as paged media is
	// concerned — this engine's own screen layout ignores all five; a
	// paginating consumer (the paginate package, go-pdfkit/html2pdf) reads
	// them. break-before / break-after / break-inside are NOT inherited
	// (initial auto); the legacy page-break-before/after/inside properties
	// are aliases (always → page). orphans / widows ARE inherited (initial
	// 2; only a positive integer is a valid value, anything else leaves the
	// declaration ignored).
	BreakBefore, BreakAfter Break
	BreakInside             BreakInside
	Orphans, Widows         int

	// Multi-column layout (CSS Multi-column Layout Level 1): ColumnCount (0 =
	// not set) and ColumnWidth (Auto = not set) are NOT inherited — a plain
	// block box unless one or the other is set, exactly like `columns`'s own
	// two longhands. The column gap itself is the SAME `column-gap`/`gap`
	// property flex/grid already use (ColumnGap, declared with RowGap
	// above) — real CSS defines it once, in the Box Alignment module, for
	// all three layout modes, not a per-mode property. Its shared "unset"
	// default is Length{Auto:true} (see initialStyle/inheritFrom), which
	// flex/grid's own gapLen/gapPxCSS already treat identically to an
	// explicit 0 — safe to give the field this default without touching
	// their behaviour at all. Multi-column layout alone resolves Auto
	// differently: to 1em (see layoutMultiCol), matching every browser's
	// implementation of a value the spec itself only describes as "a
	// reasonable value comparable to the width of a typical typeface's
	// space character" — unlike flex/grid, where `normal` is defined to
	// compute to a plain 0.
	//
	// Confirmed live (round 143/144): pkg.go.dev's own `.UnitFiles-fileList{
	// columns:12.5rem 5;...}` — its package-documentation "Source Files"
	// list, real Chrome laying ~29 short filenames out in up to 5 narrow
	// columns instead of one long single column.
	//
	// What layout.layoutMultiCol actually models, deliberately narrower than
	// the full spec, is exactly what that one confirmed trigger needs: N
	// columns of even width, each in-flow ELEMENT child of the container
	// (never a bare text node — no confirmed trigger mixes one into a
	// columns container) placed as a single ATOMIC unit into whichever
	// column a simple cumulative-height greedy pass assigns it to, never
	// split across two columns even when a single child is themselves taller
	// than a whole column (the same "atomic child, no fragmentation"
	// simplification this engine already applies to a replaced element or a
	// table row elsewhere). NOT modelled: column-span (spec keyword `all`,
	// letting one child cross every column — no confirmed trigger),
	// column-rule (the vertical divider line between columns — pure
	// decoration, not modelled since the plain column gap alone is already
	// the confirmed visual difference from a single column), TRUE column
	// balancing (the real spec's iterative algorithm that minimises the
	// tallest column's own height while respecting break-inside/break-before
	// avoidance — this engine's single greedy pass, target = total÷count,
	// approximates it closely for a roughly-uniform list like the confirmed
	// trigger, but can leave a visibly taller last column for adversarial,
	// very unevenly-sized content), and fragmenting a SINGLE child's own
	// internal content (its own child elements, or its own wrapped text
	// lines) across two columns — a browser's real balancing can split one
	// long paragraph's lines between columns; this engine cannot, matching
	// its own "atomic child" scope decision above.
	ColumnCount int
	ColumnWidth Length

	// TabSize (CSS Text Module Level 3) is INHERITED, initial 8 — a preserved
	// tab (white-space:pre/pre-wrap) advances to the next multiple of this
	// many space-widths, counted as columns from the line's own start (see
	// layout's own expandTabs). Confirmed live (round 91): pkg.go.dev's own
	// `pre,textarea.code{tab-size:4}` on its real, tab-indented Go source
	// samples — this engine's tab expansion already existed but read a
	// hardcoded 8 regardless of this property, over-indenting by 2x. Only a
	// bare, non-negative `<number>` is parsed; the sibling `<length>` form is
	// spec-flagged "at risk" (may be dropped) and has no confirmed real use
	// here, so an unparseable/negative value leaves the inherited value in
	// place rather than resetting to some sentinel (0 is itself a valid,
	// meaningful value: "don't render tabs").
	TabSize int

	// TextWrapBalance (CSS Text Module Level 4, `text-wrap`/`text-wrap-style`)
	// is INHERITED, initial false ("auto", ordinary greedy wrapping) — `true`
	// requests the `balance` value: redistribute a SHORT block's own line
	// breaks so each line's leftover space is more even (a real browser's own
	// algorithm is UA-defined; layout's own balanceLines approximates it by
	// finding the narrowest width that still wraps to the SAME line count a
	// plain greedy wrap already produces, then re-breaking at that width —
	// the spec's own explicit invariant: balancing must never change the line
	// count for 5 lines or fewer, and a UA "may" give up and fall back to
	// `auto` past 10). Confirmed live (round 93): tailwindcss.com's own hero
	// `<h1 class="... text-balance ...">` heading. Only `balance` itself is
	// modelled — `pretty`/`stable`/`avoid-short-last-line` have no confirmed
	// real use in this engine's own corpus and fall back to ordinary
	// wrapping, same as the property's own initial `auto`.
	TextWrapBalance bool

	// TextWrapNowrap (CSS Text 4 `text-wrap`/`text-wrap-mode`) is INHERITED,
	// initial false ("wrap") — `true` requests `nowrap`: per spec, "lines
	// only break at forced line breaks; content that does not fit overflows"
	// — exactly this engine's EXISTING white-space:nowrap wrapping behaviour
	// (see layout.layoutInline's own `nowrap` parameter), but WITHOUT
	// white-space's OTHER effect of collapsing runs of whitespace: the spec
	// is explicit that text-wrap-mode affects line-breaking opportunities
	// only, never whitespace collapsing/preservation. Confirmed live (round
	// 93, flagged then as a follow-up rather than folded into that round's
	// own `balance` fix): tailwindcss.com's own `.text-nowrap{text-wrap:
	// nowrap}` on its code-toolbar filename labels.
	TextWrapNowrap bool

	// WordBreakAll is `word-break: break-all`, INHERITED, initial false. Per
	// spec it makes any character boundary a break opportunity for a line
	// that would otherwise overflow — this engine models only the ONE
	// visible effect that has a confirmed real-world trigger: a single
	// unbreakable "word" (one InlineItem — see appendWords) WIDER than the
	// whole line gets split at a rune boundary instead of overflowing its
	// container (layoutInline's splitOverlongWord). The stronger, more
	// aggressive part of the real spec — breaking mid-word EAGERLY even when
	// the word would otherwise have fit on its own line, purely for more
	// even line lengths — is not modelled: no corpus page's own confirmed
	// trigger (see OverflowWrapAnywhere below) needs it, and it would also
	// change ordinary greedy-wrap line counts for text that already fits,
	// a much larger blast radius for an unevidenced refinement. `keep-all`
	// (suppress even CJK's own default inter-character breaks) is not
	// modelled either — this engine does not implement CJK line-breaking in
	// the first place, so there is nothing for it to suppress.
	//
	// See the Style method BreaksOverlongWords, which OR-combines this with
	// OverflowWrapAnywhere: the two properties are independent (either one
	// requests the same one effect this engine models), so they are kept as
	// separate fields rather than one shared boolean — collapsing them at
	// parse time would let an unrelated `word-break: normal` on a LATER
	// declaration silently cancel an EARLIER `overflow-wrap: break-word`'s
	// own effect, which real CSS's cascade would not do (they are different
	// properties; each keeps its own computed value independently).
	WordBreakAll bool

	// OverflowWrapAnywhere is `overflow-wrap`/`word-wrap: break-word` (the
	// unprefixed and legacy-alias property names for the same property) or
	// `anywhere`, and also the deprecated `word-break: break-word` value
	// (per spec, an alias for `word-break: normal; overflow-wrap: anywhere`,
	// so it sets THIS field, not WordBreakAll). INHERITED, initial false.
	// The spec distinguishes `break-word` (breaks only as an overflow-
	// avoidance last resort, like this engine's one modelled effect) from
	// `anywhere` (also affects min-/max-content intrinsic-size calculations,
	// letting a container shrink narrower than its longest unbreakable
	// word) — that intrinsic-sizing distinction is not modelled, since
	// every confirmed real trigger (see BreaksOverlongWords) only needs the
	// line-breaking effect the two values already share.
	//
	// Confirmed live: developer.mozilla.org sets `overflow-wrap:break-word`
	// on `html` itself (inherited page-wide); pkg.go.dev's own package-doc
	// pages set `word-break:break-all` on `.UnitFiles-fileList` (a `<ul>` of
	// long bare filenames with no natural break points) and `word-break:
	// break-word` on `.UnitDirectories td` (a subdirectory-listing table
	// cell); news.ycombinator.com sets `word-break:break-word` on `.title a`
	// (every story headline link, which occasionally IS a long bare URL).
	// Before this, a real production page like any of these would overflow
	// its own container with unbroken text exactly where a real browser
	// wraps mid-word instead.
	OverflowWrapAnywhere bool

	// LineClamp is `-webkit-line-clamp`/`line-clamp`'s value: the maximum
	// number of lines an element's inline content renders before being cut
	// off, 0 meaning "not set" (unclamped — the CSS `none` keyword resolves
	// to this too). Not inherited, matching `overflow` and `display`, the two
	// properties `-webkit-line-clamp` is always paired with in real CSS (the
	// "-webkit-box" multi-line-truncation idiom: `display:-webkit-box;
	// -webkit-box-orient:vertical;-webkit-line-clamp:N;overflow:hidden`) —
	// this engine models the actual clamping this idiom exists for without
	// requiring the paired display/overflow declarations to also be present,
	// since the confirmed real-world callers (see layout's own consumer)
	// always clamp regardless of exactly which vendor ceremony wraps it.
	// Truncating to N lines by DROPPING the rest is modelled; the trailing
	// ellipsis glyph real browsers additionally insert on the last kept line
	// is not — a documented, narrower scope than the full feature, matching
	// the confirmed need (tailwindcss.com's own `line-clamp-2` on a `<p>`,
	// which primarily needs the container to stop growing past two lines,
	// not the exact glyph at the cut point).
	LineClamp int

	// VerticalAlign is `vertical-align`'s keyword value; VAlignBaseline (the
	// zero value) is the default. Only VAlignTextBottom currently shifts an
	// inline-level replaced element's position in its line — the sole
	// confirmed real need (github.com's own octicon SVG icons, styled
	// `vertical-align:text-bottom` throughout its Primer design system to
	// align an icon's bottom edge with the surrounding text's font, not its
	// baseline). The other keyword values are recognised here (so they don't
	// silently fall through to some unrelated case) but not yet given
	// positional effect — a narrower scope than the full property, matching
	// this engine's established practice of shipping the confirmed-reachable
	// value first. Not inherited, per spec.
	VerticalAlign VerticalAlign

	// TextOverflowEllipsis is `text-overflow:ellipsis` (vs. the default
	// `clip`, false). It only has a visible effect combined with
	// `white-space:nowrap` and a clipping `overflow-x` — the real, confirmed
	// idiom (Tailwind's own `.truncate` utility: `overflow:hidden;
	// text-overflow:ellipsis;white-space:nowrap`, found live on
	// tailwindcss.com's own team-member role labels). `overflow:hidden`
	// alone already clips the raw overflowing text with no code here at
	// all; this field's only job is swapping that raw clip for a "…"-
	// terminated one. Scoped to a line made ENTIRELY of plain text items (no
	// nested inline element, image or forced break) — the confirmed real
	// shape — a line mixing in any of those is left as plain overflow, a
	// documented narrower scope than the full property (which also allows a
	// custom replacement string, not modelled). Not inherited, per spec.
	TextOverflowEllipsis bool
}

// VerticalAlign is the `vertical-align` property's keyword value.
type VerticalAlign uint8

const (
	VAlignBaseline VerticalAlign = iota
	VAlignTop
	VAlignBottom
	VAlignTextTop
	VAlignTextBottom
	VAlignMiddle
	VAlignSub
	VAlignSuper
)

// BoxShadow is one box-shadow layer.
type BoxShadow struct {
	OffsetX, OffsetY float64
	Blur, Spread     float64
	Color            Color
	Inset            bool
}

// LineHeight is a computed line-height. Normal means "use the font's own line
// height". Otherwise it is EITHER a fixed pixel height (Px, from a length or
// percentage value) OR a unitless Factor of the element's own font-size.
//
// The distinction is load-bearing for inheritance (CSS 2.1 §10.8.1): a length
// or percentage line-height computes to a fixed pixel value that descendants
// inherit unchanged, whereas a unitless number computes to the number itself
// and inherits AS the number, so each descendant re-multiplies by its OWN
// font-size. Collapsing a unitless line-height to pixels at the declaring
// element (as an earlier version did) leaks the ancestor's font-size onto
// larger/smaller descendants, making their line boxes too short and letting
// glyphs from adjacent lines overlap.
type LineHeight struct {
	Px     float64
	Factor float64 // unitless multiplier of the element's own font-size (0 = none)
	Normal bool
}

// Resolve returns the used line-height in pixels for an element whose computed
// font-size is fontSize, and whether an explicit height applies. It returns
// (0, false) for `normal` (defer to the font's natural metrics) and for a
// non-positive height. A unitless Factor is multiplied by fontSize; a fixed Px
// is returned unchanged.
func (h LineHeight) Resolve(fontSize float64) (float64, bool) {
	if h.Normal {
		return 0, false
	}
	if h.Factor > 0 {
		px := h.Factor * fontSize
		return px, px > 0
	}
	if h.Px <= 0 {
		return 0, false
	}
	return h.Px, true
}

// Bold reports whether the weight renders as bold.
func (s *Style) Bold() bool { return s.FontWeight >= 600 }

// BreaksOverlongWords reports whether a single unbreakable "word" wider than
// its own line may be split at a rune boundary instead of overflowing — see
// WordBreakAll and OverflowWrapAnywhere's own doc comments for exactly which
// property values request this and why they are two independent fields
// rather than one.
func (s *Style) BreaksOverlongWords() bool { return s.WordBreakAll || s.OverflowWrapAnywhere }

// initialStyle is the root's starting style (CSS initial values for the
// inherited properties, block display for the viewport root).
func initialStyle() Style {
	return Style{
		Display:    DisplayBlock,
		Color:      Color{0, 0, 0, 255},
		Background: Transparent,
		FontSize:   16,
		FontWeight: 400,
		FontFamily: Serif, // UA default document font is serif
		Width:      Length{Auto: true},
		MinWidth:   Length{Auto: true},
		MaxWidth:   Length{Auto: true},
		Height:     Length{Auto: true},
		MinHeight:  Length{Auto: true},
		MaxHeight:  Length{Auto: true},
		FlexBasis:  Length{Auto: true},
		FlexShrink: 1, // CSS initial flex-shrink is 1
		TextAlign:  AlignLeft,
		LineHeight: LineHeight{Normal: true},

		Top:        Length{Auto: true},
		Right:      Length{Auto: true},
		Bottom:     Length{Auto: true},
		Left:       Length{Auto: true},
		ZIndexAuto: true,

		GridColumnStart: GridLine{Auto: true},
		GridColumnEnd:   GridLine{Auto: true},
		GridRowStart:    GridLine{Auto: true},
		GridRowEnd:      GridLine{Auto: true},

		Orphans: 2, // CSS Fragmentation 3: initial value 2, inherited
		Widows:  2,
		TabSize: 8, // CSS Text 3: initial value 8, inherited

		ColumnWidth: Length{Auto: true}, // initial: not set (ColumnCount's 0 is already its own zero value)
		ColumnGap:   Length{Auto: true}, // initial: normal
	}
}

// inheritFrom returns a fresh style whose inherited properties are copied from
// the parent and whose non-inherited properties are reset to their initial
// values. The inherited set (per CSS): color, font-size, font-weight,
// font-family, text-align.
func inheritFrom(parent Style) Style {
	return Style{
		Display:        DisplayInline, // reset (non-inherited)
		Visibility:     parent.Visibility,
		Color:          parent.Color,
		Fill:           parent.Fill,
		FillSet:        parent.FillSet,
		FillNone:       parent.FillNone,
		Stroke:         parent.Stroke,
		StrokeSet:      parent.StrokeSet,
		StrokeNone:     parent.StrokeNone,
		Background:     Transparent, // reset
		FontSize:       parent.FontSize,
		FontWeight:     parent.FontWeight,
		FontFamily:     parent.FontFamily,
		Italic:         parent.Italic,      // inherited
		Underline:      parent.Underline,   // propagated (see its own doc comment)
		Width:          Length{Auto: true}, // reset
		MinWidth:       Length{Auto: true},
		MaxWidth:       Length{Auto: true},
		Height:         Length{Auto: true},
		MinHeight:      Length{Auto: true},
		MaxHeight:      Length{Auto: true},
		FlexBasis:      Length{Auto: true},
		FlexShrink:     1,
		TextAlign:      parent.TextAlign,      // inherited
		WhiteSpace:     parent.WhiteSpace,     // inherited
		TextTransform:  parent.TextTransform,  // inherited
		LineHeight:     parent.LineHeight,     // inherited
		LetterSpacing:  parent.LetterSpacing,  // inherited
		BorderSpacingH: parent.BorderSpacingH, // inherited
		BorderSpacingV: parent.BorderSpacingV, // inherited
		ImageRendering: parent.ImageRendering, // inherited
		// list-style-type / list-style-position inherit; list-item does not.
		ListStyleType:     parent.ListStyleType,
		ListStylePosition: parent.ListStylePosition,
		CustomProps:       parent.CustomProps, // inherited (shared until copy-on-write)

		// position and the offsets are not inherited: reset to static / auto.
		Top:        Length{Auto: true},
		Right:      Length{Auto: true},
		Bottom:     Length{Auto: true},
		Left:       Length{Auto: true},
		ZIndexAuto: true,

		GridColumnStart: GridLine{Auto: true},
		GridColumnEnd:   GridLine{Auto: true},
		GridRowStart:    GridLine{Auto: true},
		GridRowEnd:      GridLine{Auto: true},

		// container-type/container-name are not inherited: every element
		// resets to the initial value (ContainerNormal / "") here, which is
		// also the Go zero value, so this is a no-op left explicit for
		// documentation alongside the rest of this reset list.
		ContainerType: ContainerNormal,

		// break-before/after/inside are not inherited: reset to auto (the
		// zero value, listed here for documentation like ContainerType);
		// orphans and widows are inherited.
		BreakBefore:     BreakAuto,
		BreakAfter:      BreakAuto,
		BreakInside:     BreakInsideAuto,
		Orphans:         parent.Orphans,
		Widows:          parent.Widows,
		TabSize:         parent.TabSize,
		TextWrapBalance: parent.TextWrapBalance,
		TextWrapNowrap:  parent.TextWrapNowrap,

		WordBreakAll:         parent.WordBreakAll,         // inherited
		OverflowWrapAnywhere: parent.OverflowWrapAnywhere, // inherited

		// column-count/column-width/column-gap are not inherited: reset to
		// their own initial values, same as width/height above.
		ColumnWidth: Length{Auto: true},
		ColumnGap:   Length{Auto: true},
	}
}

// parseColor parses a colour value: named colours, #rgb / #rgba / #rrggbb /
// #rrggbbaa, and the rgb()/rgba()/hsl()/hsla() functional notations in BOTH the
// legacy comma syntax and the modern space-separated `R G B / A` syntax (the
// form modern Tailwind and most current design systems emit). It returns the
// colour and whether parsing succeeded.
func parseColor(s string) (Color, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "transparent" {
		return Transparent, true
	}
	if c, ok := namedColors[s]; ok {
		return c, true
	}
	if strings.HasPrefix(s, "#") {
		return parseHexColor(s[1:])
	}
	if strings.HasPrefix(s, "rgb") || strings.HasPrefix(s, "hsl") {
		return parseColorFunc(s)
	}
	return Color{}, false
}

func parseHexColor(h string) (Color, bool) {
	switch len(h) {
	case 3: // #rgb
		r, ok1 := hexNibblePair(h[0], h[0])
		g, ok2 := hexNibblePair(h[1], h[1])
		b, ok3 := hexNibblePair(h[2], h[2])
		if ok1 && ok2 && ok3 {
			return Color{r, g, b, 255}, true
		}
	case 4: // #rgba
		r, ok1 := hexNibblePair(h[0], h[0])
		g, ok2 := hexNibblePair(h[1], h[1])
		b, ok3 := hexNibblePair(h[2], h[2])
		a, ok4 := hexNibblePair(h[3], h[3])
		if ok1 && ok2 && ok3 && ok4 {
			return Color{r, g, b, a}, true
		}
	case 6: // #rrggbb
		r, ok1 := hexNibblePair(h[0], h[1])
		g, ok2 := hexNibblePair(h[2], h[3])
		b, ok3 := hexNibblePair(h[4], h[5])
		if ok1 && ok2 && ok3 {
			return Color{r, g, b, 255}, true
		}
	case 8: // #rrggbbaa
		r, ok1 := hexNibblePair(h[0], h[1])
		g, ok2 := hexNibblePair(h[2], h[3])
		b, ok3 := hexNibblePair(h[4], h[5])
		a, ok4 := hexNibblePair(h[6], h[7])
		if ok1 && ok2 && ok3 && ok4 {
			return Color{r, g, b, a}, true
		}
	}
	return Color{}, false
}

func hexNibblePair(hi, lo byte) (uint8, bool) {
	h, ok1 := hexNibble(hi)
	l, ok2 := hexNibble(lo)
	if !ok1 || !ok2 {
		return 0, false
	}
	return h<<4 | l, true
}

func hexNibble(c byte) (uint8, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

// parseColorFunc parses rgb()/rgba()/hsl()/hsla() in either the legacy
// comma-separated syntax (`rgb(1, 2, 3, .5)`) or the modern space-separated
// syntax with an optional slash alpha (`rgb(1 2 3 / .5)`). Channel values may be
// numbers or percentages; the alpha may be a number 0..1 or a percentage.
func parseColorFunc(s string) (Color, bool) {
	isHSL := strings.HasPrefix(s, "hsl")
	open := strings.IndexByte(s, '(')
	close := strings.LastIndexByte(s, ')')
	if open < 0 || close < open {
		return Color{}, false
	}
	inside := s[open+1 : close]
	// Separate an explicit slash alpha (modern syntax) from the channels.
	alphaStr := ""
	haveAlpha := false
	if i := strings.IndexByte(inside, '/'); i >= 0 {
		alphaStr = strings.TrimSpace(inside[i+1:])
		inside = inside[:i]
		haveAlpha = true
	}
	// Channels are separated by commas and/or whitespace (both are accepted).
	fields := strings.FieldsFunc(inside, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	})
	if len(fields) < 3 {
		return Color{}, false
	}
	// A legacy 4th comma field is the alpha when no slash form was used.
	if !haveAlpha && len(fields) >= 4 {
		alphaStr, haveAlpha = fields[3], true
	}

	a := uint8(255)
	if haveAlpha {
		av, ok := parseAlpha(alphaStr)
		if !ok {
			return Color{}, false
		}
		a = av
	}
	if isHSL {
		return parseHSLChannels(fields[0], fields[1], fields[2], a)
	}
	r, ok1 := parseRGBChannel(fields[0])
	g, ok2 := parseRGBChannel(fields[1])
	b, ok3 := parseRGBChannel(fields[2])
	if !ok1 || !ok2 || !ok3 {
		return Color{}, false
	}
	return Color{r, g, b, a}, true
}

// parseRGBChannel parses one rgb() channel: a 0..255 number or a percentage.
func parseRGBChannel(p string) (uint8, bool) {
	p = strings.TrimSpace(p)
	if strings.HasSuffix(p, "%") {
		f, err := strconv.ParseFloat(strings.TrimSpace(p[:len(p)-1]), 64)
		if err != nil {
			return 0, false
		}
		return clampByte(f / 100 * 255), true
	}
	f, err := strconv.ParseFloat(p, 64)
	if err != nil {
		return 0, false
	}
	return clampByte(f), true
}

// parseAlpha parses an alpha value: a number in 0..1 or a percentage.
func parseAlpha(p string) (uint8, bool) {
	p = strings.TrimSpace(p)
	if strings.HasSuffix(p, "%") {
		f, err := strconv.ParseFloat(strings.TrimSpace(p[:len(p)-1]), 64)
		if err != nil {
			return 0, false
		}
		return clampByte(f / 100 * 255), true
	}
	f, err := strconv.ParseFloat(p, 64)
	if err != nil {
		return 0, false
	}
	return clampByte(f * 255), true
}

// parseHSLChannels converts hue/saturation/lightness (h in degrees, s and l as
// percentages) plus an 8-bit alpha into an RGBA colour.
func parseHSLChannels(hs, ss, ls string, a uint8) (Color, bool) {
	h, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(hs, "deg")), 64)
	if err != nil {
		return Color{}, false
	}
	sPerc, ok1 := parsePercentUnit(ss)
	lPerc, ok2 := parsePercentUnit(ls)
	if !ok1 || !ok2 {
		return Color{}, false
	}
	// HSL -> sRGB is a colour-space conversion, not CSS parsing: route it through
	// go-gfx's shared colour layer rather than a local copy. gfxcolor.HSLToSRGB
	// returns gamma-sRGB channels in 0..1; scale and clamp to 8-bit exactly as
	// the previous in-package hslToRGB did (byte-identical).
	rf, gf, bf := gfxcolor.HSLToSRGB(gfxcolor.HSL{H: h, S: sPerc, L: lPerc})
	return Color{clampByte(rf * 255), clampByte(gf * 255), clampByte(bf * 255), a}, true
}

// parsePercentUnit parses a "50%" (or bare "0.5"→treated as fraction*100 not
// applied; here we require the percent form used by hsl()).
func parsePercentUnit(p string) (float64, bool) {
	p = strings.TrimSpace(p)
	p = strings.TrimSuffix(p, "%")
	f, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
	if err != nil {
		return 0, false
	}
	return f / 100, true
}

func clampByte(f float64) uint8 {
	if f < 0 {
		return 0
	}
	if f > 255 {
		return 255
	}
	return uint8(f + 0.5)
}

// absoluteUnitPx is the size in px of every absolute length unit, per CSS
// Values 4 §6.2: 1in = 96px = 2.54cm = 25.4mm = 72pt = 6pc, and 1Q is a
// quarter of a millimetre. Before this table only px was understood, so a
// `height:40mm`, `font-size:12pt` or `margin:1in` — routine in print
// stylesheets, whose native units are mm and pt — fell through parseLength
// unrecognised and laid out exactly as if the declaration were absent
// (measured: a `<div style="height:40mm">` had H = 0; `height:151px` had
// 151). Every property that resolves a length goes through parseLength
// (calc() terms and line-height included), so this one table fixes them all.
var absoluteUnitPx = map[string]float64{
	"px": 1,
	"in": 96,
	"cm": 96 / 2.54,
	"mm": 96 / 25.4,
	"q":  96 / 25.4 / 4,
	"pt": 96.0 / 72,
	"pc": 16,
}

// splitUnit splits a dimension token into its number and its trailing
// identifier unit: "40mm" → ("40", "mm"), "12" → ("12", ""). Only the unit's
// letters are split off, so a keyword such as "thin" yields ("th", "in") and
// then fails the numeric parse — a unit never swallows an identifier.
func splitUnit(s string) (num, unit string) {
	i := len(s)
	for i > 0 && s[i-1] >= 'a' && s[i-1] <= 'z' {
		i--
	}
	return strings.TrimSpace(s[:i]), s[i:]
}

// parseLength parses a length value against a reference font-size (for em).
// It understands the absolute units (px, in, cm, mm, Q, pt, pc — see
// absoluteUnitPx), em and rem, %, vw/vh, the keyword auto, and a bare 0.
func parseLength(s string, emRef float64) (Length, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "auto" {
		return Length{Auto: true}, true
	}
	if s == "0" {
		return Length{Px: 0}, true
	}
	switch {
	case strings.HasSuffix(s, "rem"):
		// rem is relative to the root font-size; approximated as 16px (the
		// Phase-0 root size, matching most pages). Checked before "em".
		f, err := strconv.ParseFloat(strings.TrimSpace(s[:len(s)-3]), 64)
		if err != nil {
			return Length{}, false
		}
		return Length{Px: f * 16}, true
	case strings.HasSuffix(s, "em"):
		f, err := strconv.ParseFloat(strings.TrimSpace(s[:len(s)-2]), 64)
		if err != nil {
			return Length{}, false
		}
		return Length{Px: f * emRef}, true
	case strings.HasSuffix(s, "%"):
		f, err := strconv.ParseFloat(strings.TrimSpace(s[:len(s)-1]), 64)
		if err != nil {
			return Length{}, false
		}
		return Length{Percent: f / 100, IsPercent: true}, true
	case strings.HasSuffix(s, "vw"), strings.HasSuffix(s, "vh"):
		// Viewport units are approximated as a percentage of the containing
		// block width. For the root/body (whose containing block is the viewport)
		// vw is exact; vh and nested vw are best-effort at this fidelity.
		f, err := strconv.ParseFloat(strings.TrimSpace(s[:len(s)-2]), 64)
		if err != nil {
			return Length{}, false
		}
		return Length{Percent: f / 100, IsPercent: true}, true
	}
	// Everything else is a number followed by an absolute unit, or not a
	// length at all (a bare number, an unknown unit, a keyword).
	num, unit := splitUnit(s)
	scale, known := absoluteUnitPx[unit]
	if !known {
		return Length{}, false
	}
	f, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return Length{}, false
	}
	return Length{Px: f * scale}, true
}

// parseAspectRatio parses an `aspect-ratio` value's <ratio> grammar: a bare
// number ("1.333") or "<W>/<H>" ("16/9"). The real, mixed "auto <ratio>" /
// "<ratio> auto" forms (letting content size override the ratio) and the
// bare "auto" keyword are not distinguished from any other unparseable
// value here — both return ok=false, which the caller (applyProperty) turns
// into AspectRatio=0, this style's own "no ratio" value, matching the
// pre-existing convention every other unmodelled/reset keyword in this file
// already follows (see e.g. min-width/max-height's own "none" handling).
func parseAspectRatio(v string) (float64, bool) {
	v = strings.TrimSpace(v)
	w, h, ok := strings.Cut(v, "/")
	wf, err := strconv.ParseFloat(strings.TrimSpace(w), 64)
	if err != nil || wf <= 0 {
		return 0, false
	}
	if !ok {
		return wf, true
	}
	hf, err := strconv.ParseFloat(strings.TrimSpace(h), 64)
	if err != nil || hf <= 0 {
		return 0, false
	}
	return wf / hf, true
}
