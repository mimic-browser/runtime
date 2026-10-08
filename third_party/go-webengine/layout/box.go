// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

// Package layout is a block-and-inline flow engine. Block boxes stack
// vertically honouring margins, padding and width; inline content (text runs
// and inline elements) is broken into lines within the containing block at a
// given viewport width using measured word advances supplied through a
// Measurer. The geometry is deterministic and, given a fake Measurer, exactly
// testable without any font machinery.
package layout

import (
	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/dom"
)

// Measurer supplies text metrics to the layout engine. The real implementation
// lives in the paint package (backed by go-opentype); tests inject a fake.
type Measurer interface {
	// Measure returns the advance width in pixels of text in the given family,
	// size, weight and style (italic).
	Measure(text string, fam css.FontFamily, sizePx float64, weight int, italic bool) float64
	// Metrics returns the ascent (baseline offset from the top) and the line
	// height in pixels for a family/size/weight/style.
	Metrics(fam css.FontFamily, sizePx float64, weight int, italic bool) (ascent, lineHeight float64)
}

// Box is a laid-out block box. X,Y,W,H describe the border box (which, with no
// borders in Phase 0, equals the padding box that a background fills). Content*
// describe the content box. A box either has block Children or holds inline
// Lines (an anonymous box may hold Lines and carry no Node).
type Box struct {
	Node       *dom.Node
	Style      *css.Style
	X, Y, W, H float64

	ContentX, ContentY, ContentW, ContentH float64

	Children  []*Box
	Lines     []*LineBox
	Anonymous bool

	// Float marks a box taken out of normal flow (float:left/right). Floated
	// boxes are painted like blocks but positioned by the float algorithm.
	Float css.Float

	// Position records the box's CSS position. Relative/sticky boxes remain in
	// flow but are shifted at the end of layout; absolute/fixed boxes are laid
	// out out of flow and appended to the root box (painted after in-flow
	// content) with their coordinates already resolved to document space.
	Position css.Position

	// Marker is the list-item marker box (bullet or ordinal), non-nil only on a
	// display:list-item box whose list-style-type is not `none`. Its coordinates
	// are in document space, positioned relative to this box's content box.
	Marker *Marker
}

// Marker is a laid-out list-item marker. For a bullet type (disc/circle/square)
// X,Y,W,H is the glyph's square bounding box. For the decimal type, Text is the
// ordinal string ("1.", "2.", …), X is its pen origin, Y the top of its line box
// and Ascent the baseline offset; W is the text advance (H unused). Style
// supplies the marker's colour and, for decimal, its font face.
type Marker struct {
	Type       css.ListStyleType
	Style      *css.Style
	X, Y, W, H float64
	Ascent     float64
	Text       string
}

// InlineItem is one atom of inline content: a word, an image, or a forced line
// break. Positions (X,Y = top-left) are filled during line layout.
type InlineItem struct {
	Text  string
	Style *css.Style

	// Node is the element that directly produced this item — the immediate
	// parent element of a text run, or the <img> element of an image item. It
	// lets a consumer walk up the DOM (e.g. to the nearest <a href> ancestor)
	// without re-deriving document structure from geometry. It is nil only for
	// synthetic items with no originating element (a forced <br> carries its
	// own element; anonymous runs never occur here).
	Node *dom.Node

	Width       float64 // advance of the word / DISPLAY width of the image
	SpaceBefore float64 // width of a space in this item's font
	Ascent      float64
	LineHeight  float64

	// BaselineShift moves this item DOWN (positive) from where plain
	// baseline alignment (Y = line top + line baseline - Ascent) would place
	// it — the effect of a `vertical-align` value other than the default.
	// Zero for every item except one created with VAlignTextBottom (see
	// css.Style.VerticalAlign's own doc comment for the current scope).
	// lineMetrics folds it into the line's own ascent/descent accounting so a
	// shifted item still fits inside the line box it was measured for.
	BaselineShift float64

	// Image is non-nil when this item is an <img>. ImgW/ImgH are the loaded
	// bitmap's own pixel size; Width/LineHeight (see resolvedReplacedSize) are
	// the DISPLAY size after the element's own width/max-width is resolved
	// against its real containing width, and can be smaller than ImgW/ImgH —
	// paint scales the bitmap down to fit when they differ.
	Image      *dom.Node
	ImgW, ImgH float64

	// FormControl is non-nil when this item is a form control
	// (input/button/select/textarea) — an atomic box like an image, but
	// drawn as a control (background+border+label/value text) rather than
	// blitted decoded image bytes. Width/Ascent/LineHeight carry its box
	// size, same convention as Image/ImgW/ImgH.
	FormControl *dom.Node

	// Label is a <button>'s precomputed rendered label — the concatenation of
	// its VISIBLE (non-display:none) descendant text, resolved here at layout
	// time because this is where the style map needed to check each
	// descendant's computed display is available (paint operates on the box
	// tree alone and has no such map). Only ever set for FormControl tag
	// "button" (empty there means the button has no visible text — a real
	// browser draws NO label at all in that case, not a fabricated default;
	// see Icon below for the common reason a button has no text); every other
	// FormControl tag draws its label/value from an attribute instead (see
	// paint.formControlDisplayText) and always leaves this empty.
	Label string

	// Icon is set when FormControl is a "button" whose content includes an
	// img/svg child found AFTER its own visible text in document order (a
	// TRAILING icon) — either as the button's WHOLE content (Label == "",
	// e.g. MDN's nav <mdn-search-button> or pkg.go.dev's search-submit
	// button, drawn centred in the control's box) or alongside a text label
	// (Label != "", e.g. github.com's own nav dropdown triggers —
	// "Platform▾" and friends — drawn after the label with a small gap; see
	// paintFormControl). LeadingIcon is the mirror case: an img/svg found
	// BEFORE the text (e.g. github.com's own Primer "<> Code ▾" button,
	// round 92 — which in fact has BOTH a leading AND a trailing icon
	// alongside its label, so the two fields are independent and either,
	// neither, or both may be set on the same item). See layouter.
	// buttonIcons: a side is nil when the button has no img/svg on that
	// side, has more than one on that SAME side (ambiguous — no confirmed
	// real case mixes two on one side), or that child's size never resolved
	// (e.g. its fetch failed) — in which case that side simply draws
	// nothing, independent of the other side's own outcome.
	Icon, LeadingIcon *dom.Node

	LineBreak bool // a <br>: forces the current line to end

	// BlockBreak is non-nil when this item is a SENTINEL, not real inline
	// content: a genuinely block-level element (display:block/flex/grid/
	// table, or a form control explicitly given one of those) found while
	// collecting inline content under an inline-context ancestor — e.g.
	// `<a><div>...</div></a>` (news.ycombinator.com's own real markup) or
	// `<code><span style="display:block">...</span></code>` (a syntax-
	// highlighted code line, tailwindcss.com). Per CSS 2.1 §9.2.1.1 this
	// element must be promoted to a REAL sibling box (with its own margins
	// and block/flex/grid layout), splitting the surrounding inline content
	// into anonymous block boxes around it — never flattened into the
	// inline run's plain text as if it were ordinary inline content. Never
	// reaches WrapItems/layoutInline directly: placeInlineSegments strips
	// every BlockBreak sentinel out and places its node as a real box
	// BEFORE the remaining runs on either side are wrapped and laid out.
	// Style carries the element's own computed style (its display, margins,
	// etc.) for placeInlineSegments to use.
	BlockBreak *dom.Node

	// NestedBox is non-nil when this item is an INLINE-level box that lays
	// out its own content via a full nested formatting context — currently
	// only display:inline-flex (see appendElementInline). Unlike Image/
	// FormControl (opaque atomic content with no box tree of their own),
	// NestedBox is a real *Box, already laid out at (0,0)-relative
	// coordinates by layoutIsolated; layoutInline's positioning loop
	// translates it to its final (X,Y) once resolved (the same translateBox
	// step flex/grid/table items already get) and paint recurses into it
	// like any other box, so borders, backgrounds, and its own children's
	// flex-resolved positions all render correctly. Width/Ascent/LineHeight
	// carry its outer box size, the same convention Image/FormControl use;
	// Ascent equals the box's own height (its bottom edge sits ON the line's
	// baseline), the same simplification already used for Image/FormControl.
	NestedBox *Box

	X, Y float64

	// decor is the chain of inline-level ancestors of this item that generate
	// a box of their own — one that reserves horizontal space (padding or
	// border) or paints (background or border) — ordered OUTERMOST FIRST. It
	// is shared by reference across every item of the same element, so an
	// element's chain is allocated once, not per word, and stays nil for
	// ordinary text (the overwhelmingly common case) which therefore costs
	// nothing. See newInlineDecor and resolveInlineEdges.
	decor []inlineDecor

	// decorFirst and decorLast are the depths from which this item is,
	// respectively, the FIRST and the LAST item of the decor entries at or
	// below them: decor[decorFirst:] all begin at this item and
	// decor[decorLast:] all end at it. They are what makes
	// box-decoration-break: slice (the CSS default) computable — an
	// element's leading edge is reserved and painted once, at its first
	// item, and its trailing edge once, at its last, never again on a
	// wrapped continuation line.
	decorFirst, decorLast int

	// padLead and padTrail are the horizontal space this item reserves in the
	// line's advance for the leading/trailing border+padding of the decor
	// entries that begin/end at it (0 for plain text). Unlike SpaceBefore,
	// padLead is NOT dropped at the start of a line: collapsible whitespace
	// legitimately disappears there, an element's own padding does not.
	padLead, padTrail float64
}

// inlineDecor is one inline-level ancestor that generates a box: the element,
// its computed style, and the space its four edges (border + padding) occupy.
// lead/trail are horizontal and are reserved in the line's advance; top/bottom
// are vertical and are NOT — per CSS an inline box's vertical padding and
// border overflow the line box rather than growing it.
type inlineDecor struct {
	node                     *dom.Node
	style                    *css.Style
	lead, trail, top, bottom float64
}

// InlineFragment is one line box's worth of an inline-level element's own box:
// the piece of a <span>, <a>, <code>, <b>… that falls on a single line, with
// the geometry needed to paint its background, border and padding. An inline
// element that wraps across three lines produces three fragments; one that
// generates no box of its own (plain text styling only — no background, no
// border, no padding) produces none at all.
//
// All coordinates are absolute document pixels, the same space Box and
// InlineItem use, and describe the fragment's BORDER BOX: X..X+W spans the
// element's content on this line plus its leading edge (border-left +
// padding-left) when First and its trailing edge (border-right +
// padding-right) when Last; Y..Y+H spans the tallest font box of the items on
// this line, grown by border-top + padding-top above and border-bottom +
// padding-bottom below. H may therefore exceed the line box's own height —
// that is CSS: vertical padding on an inline box overflows the line instead of
// growing it.
//
// First reports that this is the element's FIRST fragment and Last that it is
// its LAST, which is exactly the box-decoration-break: slice rule (the CSS
// default): the left border/padding belongs to the first fragment only and the
// right border/padding to the last only, while top and bottom borders paint on
// every fragment.
//
// Fragments are ordered outermost-first within a line, so painting them in
// slice order draws an enclosing element's background beneath a nested one's.
type InlineFragment struct {
	// Node is the inline element this fragment is a piece of, and Style its
	// computed style — read Style.Background, Style.Border, Style.Padding and
	// Style.BorderRadius to paint it.
	Node  *dom.Node
	Style *css.Style

	X, Y, W, H  float64
	First, Last bool
}

// LineBox is one line of inline content with its positioned items.
type LineBox struct {
	X, Y, W, H float64
	Items      []*InlineItem

	// Inlines are the box fragments of the inline-level elements that generate
	// a box on this line, outermost first (so painting them in order layers an
	// enclosing element under a nested one). Empty for a line of plain text.
	Inlines []InlineFragment
}
