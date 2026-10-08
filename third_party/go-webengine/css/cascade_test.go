// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package css

import (
	"testing"

	"github.com/go-webengine/engine/dom"
)

func styleOf(t *testing.T, htmlSrc, tag string) *Style {
	t.Helper()
	root, err := dom.Parse(htmlSrc)
	if err != nil {
		t.Fatal(err)
	}
	sm := Cascade(root)
	n := dom.Find(root, tag)
	if n == nil {
		t.Fatalf("no <%s>", tag)
	}
	st := sm[n]
	if st == nil {
		t.Fatalf("no style for <%s>", tag)
	}
	return st
}

func TestCascadeUADefaults(t *testing.T) {
	h1 := styleOf(t, `<html><body><h1>x</h1></body></html>`, "h1")
	if h1.Display != DisplayBlock {
		t.Error("h1 not block")
	}
	if h1.FontSize != 32 {
		t.Errorf("h1 font-size = %v", h1.FontSize)
	}
	if !h1.Bold() {
		t.Error("h1 not bold")
	}
	a := styleOf(t, `<html><body><a>x</a></body></html>`, "a")
	if a.Color != (Color{0, 0, 238, 255}) {
		t.Errorf("a color = %v", a.Color)
	}
	if a.Display != DisplayInline {
		t.Error("a should be inline")
	}
	pre := styleOf(t, `<html><body><pre>x</pre></body></html>`, "pre")
	if pre.WhiteSpace != WSPre || pre.FontFamily != Mono {
		t.Errorf("pre = %+v", pre)
	}
	style := styleOf(t, `<html><body><p>x</p><style>p{color:red}</style></body></html>`, "style")
	if style.Display != DisplayNone {
		t.Error("style element should be display:none")
	}
}

func TestCascadeInheritance(t *testing.T) {
	// color and font-size inherit; background does not.
	st := styleOf(t, `<html><body style="color:green;font-size:20px;background-color:red">`+
		`<div><span>hi</span></div></body></html>`, "span")
	if st.Color != (Color{0, 128, 0, 255}) {
		t.Errorf("inherited color = %v", st.Color)
	}
	if st.FontSize != 20 {
		t.Errorf("inherited font-size = %v", st.FontSize)
	}
	if st.Background.A != 0 {
		t.Errorf("background should not inherit, got %v", st.Background)
	}
}

// TestTextDecorationUnderlinePropagates covers text-decoration: real
// text-decoration-line is not an inherited property — a line set on a
// container instead visually propagates through descendant inline boxes
// that do not declare their own. Style.Underline approximates that by being
// copied parent-to-child like a genuinely inherited property (see its own
// doc comment for the one edge case this gets wrong), so both the shorthand
// and the longhand, "none" clearing it, and a descendant's own explicit
// "none" overriding an ancestor's line all need checking.
func TestTextDecorationUnderlinePropagates(t *testing.T) {
	if st := styleOf(t, `<html><body><a style="text-decoration:underline">x<b>y</b></a></body></html>`, "b"); !st.Underline {
		t.Error("text-decoration:underline should propagate to a descendant with no decoration of its own")
	}
	if st := styleOf(t, `<html><body><a style="text-decoration:none">x</a></body></html>`, "a"); st.Underline {
		t.Error("text-decoration:none should not set Underline")
	}
	if st := styleOf(t, `<html><body><a style="text-decoration-line:underline">x</a></body></html>`, "a"); !st.Underline {
		t.Error("the text-decoration-line longhand should also set Underline")
	}
	if st := styleOf(t, `<html><body><a style="text-decoration:underline">`+
		`<b style="text-decoration:none">y</b></a></body></html>`, "b"); st.Underline {
		t.Error("a descendant's own text-decoration:none should override the propagated line")
	}
}

func TestCascadeImageRenderingInherits(t *testing.T) {
	// image-rendering is an inherited property: a value set on an ancestor reaches
	// a descendant <img> that never declares it.
	st := styleOf(t, `<html><body style="image-rendering:pixelated">`+
		`<div><img></div></body></html>`, "img")
	if st.ImageRendering != IRPixelated {
		t.Errorf("inherited image-rendering = %v, want IRPixelated", st.ImageRendering)
	}
	// The initial value is IRAuto (smooth) when nobody sets it.
	def := styleOf(t, `<html><body><img></body></html>`, "img")
	if def.ImageRendering != IRAuto {
		t.Errorf("default image-rendering = %v, want IRAuto", def.ImageRendering)
	}
}

func TestCascadeVisibilityInheritsAndIsOverridable(t *testing.T) {
	// visibility is inherited: a hidden ancestor's descendant is hidden too if it
	// never declares its own value.
	hidden := styleOf(t, `<html><body style="visibility:hidden">`+
		`<div><span>x</span></div></body></html>`, "span")
	if hidden.Visibility != VisibilityHidden {
		t.Errorf("inherited visibility = %v, want VisibilityHidden", hidden.Visibility)
	}
	// Unlike display:none, a descendant can override visibility back to visible
	// — this is the exact idiom real sites use for a "reveal on interaction"
	// element nested inside a permanently visibility:hidden wrapper.
	reshown := styleOf(t, `<html><body style="visibility:hidden">`+
		`<div><span style="visibility:visible">x</span></div></body></html>`, "span")
	if reshown.Visibility != VisibilityVisible {
		t.Errorf("overridden visibility = %v, want VisibilityVisible", reshown.Visibility)
	}
	// The initial value is visible when nobody sets it.
	def := styleOf(t, `<html><body><span>x</span></body></html>`, "span")
	if def.Visibility != VisibilityVisible {
		t.Errorf("default visibility = %v, want VisibilityVisible", def.Visibility)
	}
}

func TestCascadeUnitlessLineHeightInheritsAsFactor(t *testing.T) {
	// A unitless line-height set on an ancestor must inherit as the NUMBER, so a
	// larger-font descendant resolves to factor × its OWN font-size — not the
	// ancestor's collapsed pixel value. This is what stops big inline text from
	// overlapping adjacent lines.
	src := `<html><body style="font-size:16px;line-height:1.5">` +
		`<p style="font-size:16px">body-size <span style="font-size:32px">big</span></p>` +
		`</body></html>`
	sm := Cascade(mustParse(t, src))
	body := styleOf(t, src, "body")
	if body.LineHeight.Factor != 1.5 || body.LineHeight.Px != 0 {
		t.Fatalf("body line-height = %+v, want factor 1.5", body.LineHeight)
	}
	// The <span> inherits the factor and resolves against its own 32px font-size.
	var span *Style
	for n, st := range sm {
		if n.Tag == "span" {
			span = st
		}
	}
	if span == nil {
		t.Fatal("no span style")
	}
	if span.LineHeight.Factor != 1.5 {
		t.Fatalf("span inherited factor = %v, want 1.5", span.LineHeight.Factor)
	}
	if px, ok := span.LineHeight.Resolve(span.FontSize); !ok || px != 48 {
		t.Errorf("span line-height Resolve = %v,%v want 48,true (1.5×32)", px, ok)
	}
	// The 16px paragraph text still resolves to 24 with the same inherited factor.
	p := styleOf(t, src, "p")
	if px, ok := p.LineHeight.Resolve(p.FontSize); !ok || px != 24 {
		t.Errorf("p line-height Resolve = %v,%v want 24,true (1.5×16)", px, ok)
	}
}

func TestCascadeSpecificityOrdering(t *testing.T) {
	src := `<html><head><style>
		p { color: red }
		.c { color: green }
		#i { color: blue }
	</style></head><body>
		<p id="i" class="c" style="color: black">x</p>
	</body></html>`
	// inline (black) beats id (blue) beats class beats tag.
	if st := styleOf(t, src, "p"); st.Color != (Color{0, 0, 0, 255}) {
		t.Errorf("inline should win, got %v", st.Color)
	}

	// Without inline, id wins.
	src2 := `<html><head><style>
		p { color: red } .c { color: green } #i { color: blue }
	</style></head><body><p id="i" class="c">x</p></body></html>`
	if st := styleOf(t, src2, "p"); st.Color != (Color{0, 0, 255, 255}) {
		t.Errorf("id should win, got %v", st.Color)
	}

	// Equal specificity: later source rule wins.
	src3 := `<html><head><style>.c{color:red}.c{color:green}</style></head>` +
		`<body><p class="c">x</p></body></html>`
	if st := styleOf(t, src3, "p"); st.Color != (Color{0, 128, 0, 255}) {
		t.Errorf("later rule should win, got %v", st.Color)
	}
}

// TestCascadeVisitedNeverMatches covers ":visited": this engine renders a
// single fresh fetch with no browsing history, so no real hyperlink can ever
// BE visited — the same "always false statically" treatment ":hover"
// already gets. Before this, ":visited" was simply unmodelled, and the
// generic "reduce, don't drop" default degraded it to matching its base
// selector unconditionally. That is actively wrong specifically because
// ":visited" and ":link" style opposite states of the SAME element: a
// higher-specificity `:visited` rule (inevitable, since it exists only to
// override the plain, lower-specificity unvisited default for the narrower
// "is visited" case) then won the cascade for EVERY link, not zero of them.
// Found live on developer.mozilla.org: `:is(.content-section a):visited
// {color:purple}` beat the page's own plain `a{color:blue}` for literally
// every in-article link.
func TestCascadeVisitedNeverMatches(t *testing.T) {
	src := `<html><head><style>
		a{color:blue}
		:is(.content-section a):visited{color:purple}
	</style></head><body>
		<div class="content-section"><a href="/foo">a link</a></div>
	</body></html>`
	if st := styleOf(t, src, "a"); st.Color != (Color{0, 0, 255, 255}) {
		t.Errorf(":visited wrongly matched a fresh, history-less render: color = %v, want blue", st.Color)
	}
}

// TestCascadeDisabledNeverMatchesPlainElement covers ":disabled": before
// this, it was simply unmodelled, and the generic "reduce, don't drop"
// default degraded it to matching its base selector unconditionally. That is
// actively wrong for the same reason as ":visited" above — ":disabled" and
// the plain base style opposite states of the SAME element, so a higher-
// specificity ":disabled" rule (which exists only to override the base for
// the narrower "actually disabled" case) won the cascade for EVERY element,
// not just disabled ones. Found live on pkg.go.dev's cookie-consent banner:
// `.go-Button:disabled{background-color:var(--color-button-disabled)}` beat
// the page's own `.go-Button{background-color:var(--color-button)}` for the
// "Okay" button, which carries no "disabled" attribute at all — turning it
// pale grey instead of its real brand teal.
func TestCascadeDisabledNeverMatchesPlainElement(t *testing.T) {
	src := `<html><head><style>
		.go-Button{background-color:#007d9c}
		.go-Button:disabled{background-color:#f0f1f2}
	</style></head><body>
		<button class="go-Button">Okay</button>
	</body></html>`
	if st := styleOf(t, src, "button"); st.Background != (Color{0, 0x7d, 0x9c, 255}) {
		t.Errorf(":disabled wrongly matched a plain button: background = %v, want teal", st.Background)
	}

	// A genuinely disabled element still gets the :disabled styling.
	src2 := `<html><head><style>
		.go-Button{background-color:#007d9c}
		.go-Button:disabled{background-color:#f0f1f2}
	</style></head><body>
		<button class="go-Button" disabled>Okay</button>
	</body></html>`
	if st := styleOf(t, src2, "button"); st.Background != (Color{0xf0, 0xf1, 0xf2, 255}) {
		t.Errorf(":disabled should still match a real disabled button: background = %v, want gray", st.Background)
	}
}

// TestCascadeMediaOverrideDisplayInitial covers the real shape pkg.go.dev
// uses to show its "Rendered for" build-context label only above a width
// breakpoint: an unconditional `display:none`, overridden by
// `display:initial` inside a matching `@media (width>=…)` block. Before
// this, "initial" matched none of the display switch's cases, so the
// declaration was silently ignored — leaving the earlier `display:none`
// in effect no matter how wide the viewport was, which is exactly the
// live symptom (a "Rendered for" label with a correctly-recognised,
// correctly-MATCHING media query that still never showed).
func TestCascadeMediaOverrideDisplayInitial(t *testing.T) {
	src := `<html><head><style>
		.a{display:none}
		@media (width>=30rem){.a{display:initial}}
	</style></head><body><a class="a">a</a></body></html>`
	if st := styleOf(t, src, "a"); st.Display != DisplayInline {
		t.Errorf("display = %v, want DisplayInline (a real 1024px viewport is well above the 30rem breakpoint)", st.Display)
	}
}

// TestCascadeImportantBeatsSpecificity covers the cascade tier added for
// `!important`: a low-specificity class rule marked !important must win over a
// higher-specificity id rule that is not — the opposite of the normal
// specificity order proven in TestCascadeSpecificityOrdering above. This is
// exactly the shape a real site (MDN) relies on to force a component hidden
// (`.left-sidebar{display:none!important}`) regardless of any later, more
// specific rule that would otherwise reveal it.
func TestCascadeImportantBeatsSpecificity(t *testing.T) {
	src := `<html><head><style>
		.c { color: green !important }
		#i { color: blue }
	</style></head><body><p id="i" class="c">x</p></body></html>`
	if st := styleOf(t, src, "p"); st.Color != (Color{0, 128, 0, 255}) {
		t.Errorf("!important class should beat non-important id, got %v", st.Color)
	}
}

// TestCascadeImportantOrderingWithinTier covers that two !important
// declarations still resolve by the normal specificity/order rules AMONG
// themselves — !important lifts a declaration into a higher tier, it does not
// exempt it from the ordinary cascade within that tier.
func TestCascadeImportantOrderingWithinTier(t *testing.T) {
	// Higher specificity important wins over lower specificity important.
	src := `<html><head><style>
		.c { color: red !important }
		#i { color: blue !important }
	</style></head><body><p id="i" class="c">x</p></body></html>`
	if st := styleOf(t, src, "p"); st.Color != (Color{0, 0, 255, 255}) {
		t.Errorf("higher-specificity !important should win, got %v", st.Color)
	}

	// Equal specificity: the later !important rule wins.
	src2 := `<html><head><style>
		.c { color: red !important }
		.c { color: green !important }
	</style></head><body><p class="c">x</p></body></html>`
	if st := styleOf(t, src2, "p"); st.Color != (Color{0, 128, 0, 255}) {
		t.Errorf("later !important rule should win, got %v", st.Color)
	}
}

// TestCascadeImportantAbsentUnaffected proves a stylesheet with no !important
// at all sorts identically to before — the new comparator branch is a pure
// addition, not a behaviour change for the common case.
func TestCascadeImportantAbsentUnaffected(t *testing.T) {
	src := `<html><head><style>p{color:red}.c{color:green}#i{color:blue}</style></head>` +
		`<body><p id="i" class="c">x</p></body></html>`
	if st := styleOf(t, src, "p"); st.Color != (Color{0, 0, 255, 255}) {
		t.Errorf("id should still win with no !important present, got %v", st.Color)
	}
}

func TestCascadeAuthorOverridesUA(t *testing.T) {
	// Author rule beats UA default (a's blue link colour).
	src := `<html><head><style>a{color:red}</style></head><body><a>x</a></body></html>`
	if st := styleOf(t, src, "a"); st.Color != (Color{255, 0, 0, 255}) {
		t.Errorf("author should override UA, got %v", st.Color)
	}
}

// TestCascadeInheritKeywordCancelsUADefault covers the real regression this
// engine had: `inherit` was not understood as a value AT ALL, so
// `a{color:inherit}` (Tailwind's, and most modern frameworks', preflight
// reset — confirmed live on tailwindcss.com) failed to parse and was
// dropped, leaving the UA default `a{color:#0000ee}` (browser-default link
// blue) as the only surviving declaration. Every `fill="currentColor"` SVG
// icon inside such a reset anchor (a very common pattern for a linked logo)
// inherited that wrong blue too. `color` is already inherited BY DEFAULT
// (TestCascadeCustomPropertyInherited-style), but that default alone is not
// enough here: the UA rule runs earlier in the very same cascade and
// overwrites it, so `inherit` must explicitly copy the parent's value again
// to cancel that override, not merely no-op.
func TestCascadeInheritKeywordCancelsUADefault(t *testing.T) {
	src := `<html><head><style>a{color:inherit}</style></head>` +
		`<body style="color:green"><a>x</a></body></html>`
	if st := styleOf(t, src, "a"); st.Color != (Color{0, 128, 0, 255}) {
		t.Errorf("a{color:inherit} should cancel the UA blue default and use the parent's green, got %v", st.Color)
	}
}

func TestCascadeInheritKeywordOtherProperties(t *testing.T) {
	// visibility, font-weight and text-align are not overridden by any UA
	// default the way color is, but inherit must still work for them.
	src := `<html><body style="visibility:hidden;font-weight:700;text-align:center">` +
		`<p style="visibility:inherit;font-weight:inherit;text-align:inherit">x</p></body></html>`
	st := styleOf(t, src, "p")
	if st.Visibility != VisibilityHidden {
		t.Errorf("visibility:inherit = %v, want hidden", st.Visibility)
	}
	if st.FontWeight != 700 {
		t.Errorf("font-weight:inherit = %v, want 700", st.FontWeight)
	}
	if st.TextAlign != AlignCenter {
		t.Errorf("text-align:inherit = %v, want center", st.TextAlign)
	}
}

func TestCascadeInheritKeywordRemainingProperties(t *testing.T) {
	src := `<html><body style="white-space:pre;line-height:2;list-style-type:square;list-style-position:inside">` +
		`<p style="white-space:inherit;line-height:inherit;list-style-type:inherit;list-style-position:inherit">x</p></body></html>`
	st := styleOf(t, src, "p")
	if st.WhiteSpace != WSPre {
		t.Errorf("white-space:inherit = %v, want WSPre", st.WhiteSpace)
	}
	if st.LineHeight != (LineHeight{Factor: 2}) {
		t.Errorf("line-height:inherit = %+v, want Factor 2", st.LineHeight)
	}
	if st.ListStyleType != ListSquare {
		t.Errorf("list-style-type:inherit = %v, want ListSquare", st.ListStyleType)
	}
	if st.ListStylePosition != ListInside {
		t.Errorf("list-style-position:inherit = %v, want ListInside", st.ListStylePosition)
	}
}

func TestCascadeInheritUnknownPropertyIsNoop(t *testing.T) {
	// background-color is NOT inherited per spec (transparent resets it), and
	// this engine does not implement `inherit` for it — the declaration must
	// be silently dropped rather than panic or corrupt the style.
	src := `<html><body style="background-color:red">` +
		`<p style="background-color:inherit">x</p></body></html>`
	st := styleOf(t, src, "p")
	if st.Background != Transparent {
		t.Errorf("background-color:inherit (unsupported) = %v, want the reset default transparent", st.Background)
	}
}

// TestCascadeBackgroundColorInitialResetsUAButton covers `background-color:
// initial`/`unset`, using MDN's own real component rule shape
// (`.color-theme__button{background-color:initial;border:none}`, its
// color-theme toggle button) as the fixture: a real UA-default-styled
// <button> must have its default gray chrome reset to transparent, the
// property's spec initial value — matching the already-handled
// `background:0 0`/`background:transparent` reset idiom.
func TestCascadeBackgroundColorInitialResetsUAButton(t *testing.T) {
	initial := styleOf(t, `<html><body><button style="background-color:initial">x</button></body></html>`, "button")
	if initial.Background != Transparent {
		t.Errorf("background-color:initial = %v, want Transparent", initial.Background)
	}
	unset := styleOf(t, `<html><body><button style="background-color:unset">x</button></body></html>`, "button")
	if unset.Background != Transparent {
		t.Errorf("background-color:unset = %v, want Transparent", unset.Background)
	}
}

func TestCascadeFontSizeEm(t *testing.T) {
	// font-size:2em on a child is relative to the parent's font-size (20px→40px);
	// a margin in em is relative to the element's own computed font-size.
	src := `<html><body style="font-size:20px">` +
		`<p style="font-size:2em;margin:1em">x</p></body></html>`
	st := styleOf(t, src, "p")
	if st.FontSize != 40 {
		t.Errorf("font-size em = %v want 40", st.FontSize)
	}
	if st.Margin.Top != 40 {
		t.Errorf("margin em = %v want 40 (relative to own 40px)", st.Margin.Top)
	}
}

func TestCascadeSkipsNonElements(t *testing.T) {
	// A document with only text at the root still cascades without panic.
	root, _ := dom.Parse(`plain text`)
	sm := Cascade(root)
	if len(sm) == 0 {
		t.Error("expected some element styles (html/body auto-inserted)")
	}
}

func TestCascadeVWExternalSheetsAndMedia(t *testing.T) {
	root, err := dom.Parse(`<html><body><div class="box"><p class="lead">hi</p></div></body></html>`)
	if err != nil {
		t.Fatal(err)
	}
	external := []string{`.box { background-color: #eee } .lead { color: red }`}
	sm := CascadeVW(root, 1024, external)
	div := dom.Find(root, "div")
	if sm[div].Background != (Color{0xee, 0xee, 0xee, 255}) {
		t.Errorf("external .box background = %+v", sm[div].Background)
	}
	p := dom.Find(root, "p")
	if sm[p].Color != (Color{255, 0, 0, 255}) {
		t.Errorf("external .lead color = %+v", sm[p].Color)
	}
}

func TestCascadeVWMediaWidth(t *testing.T) {
	src := `<html><head><style>
	@media (min-width: 640px) { .side { float: right; width: 200px } }
	@media (max-width: 639px) { .side { float: none } }
	</style></head><body><div class="side">x</div></body></html>`
	root, err := dom.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	div := dom.Find(root, "div")
	// Desktop width: the min-width rule floats it right.
	if st := CascadeVW(root, 1024, nil)[div]; st.Float != FloatRight || st.Width.Px != 200 {
		t.Errorf("desktop side = float %v width %+v", st.Float, st.Width)
	}
	// Narrow width: the mobile rule wins (float none).
	if st := CascadeVW(root, 480, nil)[div]; st.Float != FloatNone {
		t.Errorf("mobile side float = %v want none", st.Float)
	}
}

func TestCascadeHiddenAttribute(t *testing.T) {
	// A bare `hidden` attribute maps to display:none at UA origin.
	if d := styleOf(t, `<html><body><div hidden>x</div></body></html>`, "div").Display; d != DisplayNone {
		t.Errorf("<div hidden> display = %v, want none", d)
	}
	// An inline author `display` beats the UA [hidden] rule (origin > specificity).
	if d := styleOf(t, `<html><body><p hidden style="display:block">x</p></body></html>`, "p").Display; d != DisplayBlock {
		t.Errorf("<p hidden style=display:block> = %v, want block (author wins)", d)
	}
	// Even a low-specificity author rule (tag selector) beats UA [hidden].
	if d := styleOf(t, `<html><head><style>span{display:flex}</style></head><body><span hidden>x</span></body></html>`, "span").Display; d != DisplayFlex {
		t.Errorf("span[hidden] with author span{display:flex} = %v, want flex (author wins)", d)
	}
	// hidden="until-found" is revealable content, NOT hidden.
	if d := styleOf(t, `<html><body><div hidden="until-found">x</div></body></html>`, "div").Display; d != DisplayBlock {
		t.Errorf(`<div hidden="until-found"> = %v, want block (not hidden)`, d)
	}
}

// TestCascadeClosedDetailsHidesNonSummaryChildren covers a real regression:
// <details>/<summary> semantics were entirely unimplemented, so a closed
// <details> (the default — no "open" attribute) rendered ALL its content,
// not just the <summary>. Confirmed load-bearing live: pkg.go.dev's help
// tooltips are <details class="go-Tooltip"><summary>...</summary>
// <p role="tooltip">the tooltip text</p></details>, only ever opened by a
// click a static render never triggers — every such tooltip's real text
// rendered permanently visible, overlapping the page.
func TestCascadeClosedDetailsHidesNonSummaryChildren(t *testing.T) {
	src := `<html><body><details>` +
		`<summary>Toggle</summary>` +
		`<p>hidden body</p>` +
		`</details></body></html>`
	root, err := dom.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	sm := Cascade(root)
	summary := dom.Find(root, "summary")
	p := dom.Find(root, "p")
	if sm[summary].Display == DisplayNone {
		t.Error("a closed <details>'s <summary> must stay visible")
	}
	if sm[p].Display != DisplayNone {
		t.Errorf("a closed <details>'s non-summary child must be hidden, got display=%v", sm[p].Display)
	}
}

func TestCascadeOpenDetailsShowsEverything(t *testing.T) {
	src := `<html><body><details open>` +
		`<summary>Toggle</summary>` +
		`<p>visible body</p>` +
		`</details></body></html>`
	root, err := dom.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	sm := Cascade(root)
	p := dom.Find(root, "p")
	if sm[p].Display == DisplayNone {
		t.Error("an OPEN <details>'s children must not be hidden")
	}
}

func TestCascadeDetailsAuthorOverride(t *testing.T) {
	// An author rule targeting the hidden child still beats the UA default,
	// same as [hidden] does — origin, not specificity, decides first.
	src := `<html><head><style>p{display:block}</style></head>` +
		`<body><details><summary>Toggle</summary><p>x</p></details></body></html>`
	if d := styleOf(t, src, "p"); d.Display != DisplayBlock {
		t.Errorf("author p{display:block} inside closed <details> = %v, want block (author wins)", d.Display)
	}
}

// TestCascadeClipDeclaration covers the `clip: rect(...)` cascade apply
// branch end-to-end (parse.go's declaration switch, not just parseClipRect
// in isolation): a recognised rect() sets HasClip/ClipRect, an unrecognised
// clip value (a bare "auto" edge) leaves HasClip false, matching any other
// declaration this engine cannot resolve.
func TestCascadeClipDeclaration(t *testing.T) {
	ok := styleOf(t, `<html><body><a style="position:absolute;clip:rect(1px,2px,3px,4px)">x</a></body></html>`, "a")
	if !ok.HasClip || ok.ClipRect != (Edges{Top: 1, Right: 2, Bottom: 3, Left: 4}) {
		t.Errorf("clip:rect(1px,2px,3px,4px) = HasClip=%v ClipRect=%v", ok.HasClip, ok.ClipRect)
	}
	unresolved := styleOf(t, `<html><body><a style="position:absolute;clip:rect(auto,2px,3px,4px)">x</a></body></html>`, "a")
	if unresolved.HasClip {
		t.Error("an unresolvable clip value must leave HasClip false")
	}
}

// TestCascadeMarginLogicalProperties covers margin-block-start/end and
// margin-inline-start/end, using go.dev/blog's real rule shape
// (`p.blogtitle{margin-block-end:0px}` / `p.blogsummary{margin-block-start:0px}`)
// as the fixture — this engine has no bidi/vertical writing-mode support
// anywhere, so these always map directly to the physical top/bottom and
// left/right edges, matching the inset-block/inset-inline precedent.
func TestCascadeMarginLogicalProperties(t *testing.T) {
	title := styleOf(t, `<html><body><p style="margin-block-end:0px;margin-block-start:1em">x</p></body></html>`, "p")
	if title.Margin.Bottom != 0 {
		t.Errorf("margin-block-end:0px -> Margin.Bottom = %v, want 0", title.Margin.Bottom)
	}
	if title.Margin.Top != 16 {
		t.Errorf("margin-block-start:1em -> Margin.Top = %v, want 16", title.Margin.Top)
	}

	inline := styleOf(t, `<html><body><p style="margin-inline-start:2px;margin-inline-end:auto">x</p></body></html>`, "p")
	if inline.Margin.Left != 2 {
		t.Errorf("margin-inline-start:2px -> Margin.Left = %v, want 2", inline.Margin.Left)
	}
	if !inline.MarginRightAuto {
		t.Error("margin-inline-end:auto -> MarginRightAuto = false, want true")
	}
}

// TestCascadeMarginBlockInlineShorthand covers the margin-block/margin-inline
// axis shorthands (1-or-2-value: <start> [<end>]).
func TestCascadeMarginBlockInlineShorthand(t *testing.T) {
	oneValue := styleOf(t, `<html><body><p style="margin-block:3px">x</p></body></html>`, "p")
	if oneValue.Margin.Top != 3 || oneValue.Margin.Bottom != 3 {
		t.Errorf("margin-block:3px -> Margin.Top=%v Margin.Bottom=%v, want 3/3", oneValue.Margin.Top, oneValue.Margin.Bottom)
	}

	twoValue := styleOf(t, `<html><body><p style="margin-block:1px 2px">x</p></body></html>`, "p")
	if twoValue.Margin.Top != 1 || twoValue.Margin.Bottom != 2 {
		t.Errorf("margin-block:1px 2px -> Margin.Top=%v Margin.Bottom=%v, want 1/2", twoValue.Margin.Top, twoValue.Margin.Bottom)
	}

	inline := styleOf(t, `<html><body><p style="margin-inline:4px 5px">x</p></body></html>`, "p")
	if inline.Margin.Left != 4 || inline.Margin.Right != 5 {
		t.Errorf("margin-inline:4px 5px -> Margin.Left=%v Margin.Right=%v, want 4/5", inline.Margin.Left, inline.Margin.Right)
	}

	// An axis shorthand only ever takes 1 or 2 values; a 3-value form is
	// invalid and must leave the UA default untouched, matching how every
	// other unresolvable declaration in this package is handled.
	invalidBlock := styleOf(t, `<html><body><p style="margin-block:1px 2px 3px">x</p></body></html>`, "p")
	if invalidBlock.Margin.Top != 16 || invalidBlock.Margin.Bottom != 16 {
		t.Errorf("margin-block with 3 values must be ignored -> Margin.Top=%v Margin.Bottom=%v, want UA default 16/16", invalidBlock.Margin.Top, invalidBlock.Margin.Bottom)
	}
	invalidInline := styleOf(t, `<html><body><p style="margin-inline:1px 2px 3px">x</p></body></html>`, "p")
	if invalidInline.Margin.Left != 0 || invalidInline.Margin.Right != 0 {
		t.Errorf("margin-inline with 3 values must be ignored -> Margin.Left=%v Margin.Right=%v, want UA default 0/0", invalidInline.Margin.Left, invalidInline.Margin.Right)
	}
}

// TestCascadePaddingLogicalProperties covers padding-block-start/end,
// padding-inline-start/end, and the padding-block/padding-inline shorthands
// — the padding-side sibling of the margin logical-property fix above.
func TestCascadePaddingLogicalProperties(t *testing.T) {
	sides := styleOf(t, `<html><body><p style="padding-block-start:1px;padding-block-end:2px;padding-inline-start:3px;padding-inline-end:4px">x</p></body></html>`, "p")
	if sides.Padding != (Edges{Top: 1, Bottom: 2, Left: 3, Right: 4}) {
		t.Errorf("padding logical sides -> Padding = %+v, want {Top:1 Bottom:2 Left:3 Right:4}", sides.Padding)
	}

	shorthand := styleOf(t, `<html><body><p style="padding-block:5px 6px;padding-inline:7px">x</p></body></html>`, "p")
	if shorthand.Padding != (Edges{Top: 5, Bottom: 6, Left: 7, Right: 7}) {
		t.Errorf("padding-block/inline shorthand -> Padding = %+v, want {Top:5 Bottom:6 Left:7 Right:7}", shorthand.Padding)
	}

	// A 3-value axis shorthand is invalid and must leave the UA default (0)
	// untouched.
	invalid := styleOf(t, `<html><body><p style="padding-block:1px 2px 3px;padding-inline:1px 2px 3px">x</p></body></html>`, "p")
	if invalid.Padding != (Edges{}) {
		t.Errorf("padding-block/inline with 3 values must be ignored -> Padding = %+v, want zero", invalid.Padding)
	}
}
