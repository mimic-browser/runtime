// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package layout

import (
	"testing"

	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/dom"
)

// TestFormControlExplicitCSSSize covers the common real-world case: an
// author-styled control (width/height in CSS, the way any real form does
// it) uses exactly that size, not a UA default.
func TestFormControlExplicitCSSSize(t *testing.T) {
	src := `<html><body><input id="e" style="width:200px;height:24px"></body></html>`
	items := firstLineItems(findBox(layoutHTML(t, src, 1024), "body"))
	if len(items) != 1 || items[0].FormControl == nil {
		t.Fatalf("expected one form-control item, got %v", items)
	}
	assertF(t, "width", items[0].Width, 200)
	assertF(t, "height", items[0].LineHeight, 24)
}

// TestFormControlPercentWidthResolvesAgainstContainer covers engine#(pending):
// a form control reached through ordinary inline collection (the common
// case — ANY input as a plain child of a div/label/form takes this path, not
// the rarer display:block-routed one) previously had NO containing width to
// resolve a percentage against at all, hardcoding cw=0 — so
// `width:40%` degraded straight to formControlDefaultSize's UA-shaped
// default (170px for a text input) regardless of the real container width.
// Confirmed live on caniuse.com's own `.ciu-search__input{width:40%}`.
// A wide container makes the two outcomes clearly distinguishable (170px
// default vs. 400px at 40% of 1000px) rather than coincidentally close.
func TestFormControlPercentWidthResolvesAgainstContainer(t *testing.T) {
	src := `<html><body style="margin:0"><div style="width:1000px">` +
		`<input id="e" style="width:40%"></div></body></html>`
	items := firstLineItems(findBox(layoutHTML(t, src, 1024), "div"))
	if len(items) != 1 || items[0].FormControl == nil {
		t.Fatalf("expected one form-control item, got %v", items)
	}
	assertF(t, "width", items[0].Width, 400)
}

// TestFormControlSpaceBeforeWhenPrecededByText covers the case a bare
// "<input>" fixture never exercises: a control preceded by inline text
// with trailing whitespace ("Label <input>", the normal shape of a real
// form) needs its own SpaceBefore, the same collapsible-space handling
// img/svg items already get, or the label and field would render glued
// together with no gap.
func TestFormControlSpaceBeforeWhenPrecededByText(t *testing.T) {
	src := `<html><body>Label <input id="e"></body></html>`
	items := firstLineItems(findBox(layoutHTML(t, src, 1024), "body"))
	if len(items) != 2 || items[1].FormControl == nil {
		t.Fatalf("expected [text, form-control], got %v", texts(items))
	}
	if items[1].SpaceBefore <= 0 {
		t.Fatalf("SpaceBefore = %v, want > 0 (a space separated \"Label\" from the input)", items[1].SpaceBefore)
	}
}

// TestFormControlDefaultSizes covers the UA-default fallback per control
// kind, for a page that (like plenty of real ones) leaves an input unstyled.
func TestFormControlDefaultSizes(t *testing.T) {
	cases := []struct {
		name       string
		src        string
		wantW      float64
		wantH      float64
		wantHExact bool
	}{
		{"text input", `<input id="e">`, 170, 0, false},
		{"checkbox", `<input id="e" type="checkbox">`, 13, 13, true},
		{"radio", `<input id="e" type="radio">`, 13, 13, true},
		{"select", `<select id="e"></select>`, 170, 0, false},
		{"textarea", `<textarea id="e"></textarea>`, 200, 60, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := `<html><body>` + c.src + `</body></html>`
			items := firstLineItems(findBox(layoutHTML(t, src, 1024), "body"))
			if len(items) != 1 || items[0].FormControl == nil {
				t.Fatalf("%s: expected one form-control item, got %v", c.name, items)
			}
			assertF(t, c.name+" width", items[0].Width, c.wantW)
			if c.wantHExact {
				assertF(t, c.name+" height", items[0].LineHeight, c.wantH)
			} else if items[0].LineHeight <= 0 {
				t.Errorf("%s: height = %v, want > 0", c.name, items[0].LineHeight)
			}
		})
	}
}

// TestFormControlButtonSizesToLabel covers a button-like control's intrinsic
// (content-sized) default: with fakeMeasurer's exact 10px/char, the width is
// predictable, unlike a real font — this is what actually proves the sizing
// reaches the label text, not just a fixed constant.
func TestFormControlButtonSizesToLabel(t *testing.T) {
	src := `<html><body><input id="e" type="submit" value="Log in"></body></html>`
	items := firstLineItems(findBox(layoutHTML(t, src, 1024), "body"))
	if len(items) != 1 || items[0].FormControl == nil {
		t.Fatal("expected one form-control item")
	}
	// "Log in" = 6 runes * 10px (fakeMeasurer) + 2*formControlPadX(12) = 84.
	assertF(t, "submit button width", items[0].Width, 84)

	src2 := `<html><body><input id="e" type="submit" value="A much longer label"></body></html>`
	items2 := firstLineItems(findBox(layoutHTML(t, src2, 1024), "body"))
	if items2[0].Width <= items[0].Width {
		t.Fatalf("a longer label did not widen the button: %v vs %v", items2[0].Width, items[0].Width)
	}
}

// TestFormControlSelectSizesToWidestOption covers a real <select>'s sizing:
// the WIDEST option's label, not the flat text-input default — the common
// cross-engine pattern (e.g. WebKit's RenderMenuList::updateOptionsWidth),
// confirmed against that engine's own source before implementing here. With
// fakeMeasurer's exact 10px/char this is fully predictable, unlike a real
// font.
func TestFormControlSelectSizesToWidestOption(t *testing.T) {
	src := `<html><body><select id="e"><option>xx</option><option>xxxxx</option></select></body></html>`
	items := firstLineItems(findBox(layoutHTML(t, src, 1024), "body"))
	if len(items) != 1 || items[0].FormControl == nil {
		t.Fatal("expected one form-control item")
	}
	// "xxxxx" = 5 runes * 10px (fakeMeasurer) + 2*formControlPadX(12) = 74.
	assertF(t, "select width", items[0].Width, 74)
}

// TestFormControlSelectWidthIncludesOptgroupAndLabelAttribute covers that
// sizing walks through <optgroup> nesting (real markup routinely groups
// options this way) and honours an <option label="..."> override — plus
// that whitespace TEXT NODES between sibling tags (indentation/newlines,
// which real HTML always has and a single-line test string does not) are
// skipped rather than mistaken for elements.
func TestFormControlSelectWidthIncludesOptgroupAndLabelAttribute(t *testing.T) {
	src := `<html><body><select id="e"><optgroup label="A Long Group Label">
		<option label="Custom Wide Label">short</option>
	</optgroup></select></body></html>`
	items := firstLineItems(findBox(layoutHTML(t, src, 1024), "body"))
	// "A Long Group Label" = 18 runes * 10px + 24 = 204, the widest candidate
	// (wider than the 17-char "Custom Wide Label" option label).
	assertF(t, "select width", items[0].Width, 204)
}

// TestFormControlSelectNarrowerThanTextInputDefault covers that the flat
// text-input default (170) is NOT a floor a select with real options gets
// clamped up to — a real browser sizes a <select><option>x</option></select>
// to its one tiny option, not to an unrelated text-field width. The 170
// default applies only when there are no options at all (see
// TestFormControlDefaultSizes's "select" case, an empty <select>).
func TestFormControlSelectNarrowerThanTextInputDefault(t *testing.T) {
	src := `<html><body><select id="e"><option>x</option></select></body></html>`
	items := firstLineItems(findBox(layoutHTML(t, src, 1024), "body"))
	// "x" = 1 rune * 10px (fakeMeasurer) + 2*formControlPadX(12) = 34.
	assertF(t, "select width", items[0].Width, 34)
}

// TestFormControlSubmitDefaultLabel covers the UA-default label text ("Submit")
// an <input type=submit> with no value attribute gets — indirectly, through
// its effect on sizing (an empty label would be narrower).
func TestFormControlSubmitDefaultLabel(t *testing.T) {
	withValue := firstLineItems(findBox(layoutHTML(t,
		`<html><body><input id="e" type="submit" value="Submit"></body></html>`, 1024), "body"))
	noValue := firstLineItems(findBox(layoutHTML(t,
		`<html><body><input id="e" type="submit"></body></html>`, 1024), "body"))
	assertF(t, "default-label width", noValue[0].Width, withValue[0].Width)
}

// TestFormControlResetAndButtonDefaultLabels covers controlLabel's other UA
// defaults ("Reset" for type=reset, "Submit" for type=button — every major
// UA gives a plain <input type=button> with no value an empty label, but
// this engine treats it the same as submit for simplicity, documented
// there) via their effect on sizing, same technique as the submit case.
func TestFormControlResetAndButtonDefaultLabels(t *testing.T) {
	reset := firstLineItems(findBox(layoutHTML(t,
		`<html><body><input id="e" type="reset"></body></html>`, 1024), "body"))
	// "Reset" (5 runes) * 10px + 24 padding = 74.
	assertF(t, "reset default width", reset[0].Width, 74)

	btn := firstLineItems(findBox(layoutHTML(t,
		`<html><body><input id="e" type="button"></body></html>`, 1024), "body"))
	// "Submit" (6 runes) * 10px + 24 padding = 84.
	assertF(t, "button default width", btn[0].Width, 84)
}

// TestFormControlButtonTagUsesTextContent covers the <button>...</button>
// tag (distinct from <input type=button>): its label is its TEXT CONTENT,
// not a value attribute, and an empty one (an icon-only button, e.g.
// pkg.go.dev's search-submit <button> with only an <img> child) has NO
// fallback label at all — unlike <input type=button/submit>, no major
// browser fabricates default text for a content-less <button> tag.
func TestFormControlButtonTagUsesTextContent(t *testing.T) {
	labeled := firstLineItems(findBox(layoutHTML(t,
		`<html><body><button id="e">Log in</button></body></html>`, 1024), "body"))
	if len(labeled) != 1 || labeled[0].FormControl == nil {
		t.Fatalf("expected one form-control item, got %v", labeled)
	}
	// "Log in" = 6 runes * 10px + 24 padding = 84.
	assertF(t, "<button>Log in</button> width", labeled[0].Width, 84)

	empty := firstLineItems(findBox(layoutHTML(t,
		`<html><body><button id="e"></button></body></html>`, 1024), "body"))
	// Empty label: just the padding, 2*formControlPadX(12) = 24.
	assertF(t, "empty <button> width", empty[0].Width, 24)
}

// TestFormControlButtonLabelSkipsDisplayNoneDescendants covers a real
// regression, found live on github.com/golang/go: its site-header search
// button nests a responsive text label ("Search") next to a keyboard-shortcut
// hint ("/"), each shown at a DIFFERENT breakpoint via display:none on
// nested spans/kbd — never both at once. dom.TextContent (what this engine
// used to size and paint a <button>'s label, since a button is laid out as
// an atomic box, never as a real container of child boxes — see
// isFormControlTag above) has no notion of computed style, so it
// concatenated both into a single "Search/" label regardless of which one
// was actually display:none, at every width. The label must instead be
// exactly the text NOT under a display:none ancestor — here, "Search" only
// (the kbd hint is display:none), matching what a real browser would show
// were it to lay this button's children out normally.
func TestFormControlButtonLabelSkipsDisplayNoneDescendants(t *testing.T) {
	src := `<html><body><button id="e">` +
		`<span>Search</span><kbd style="display:none">/</kbd>` +
		`</button></body></html>`
	items := firstLineItems(findBox(layoutHTML(t, src, 1024), "body"))
	if len(items) != 1 || items[0].FormControl == nil {
		t.Fatalf("expected one form-control item, got %v", items)
	}
	if items[0].Label != "Search" {
		t.Errorf("Label = %q, want %q (the display:none kbd must not contribute)", items[0].Label, "Search")
	}
	// "Search" (6 runes) * 10px (fakeMeasurer) + 24 padding = 84 — NOT "Search/"
	// (7 runes) * 10 + 24 = 94, which is what dom.TextContent's unconditional
	// concatenation used to produce.
	assertF(t, "button width sized to the visible label only", items[0].Width, 84)
}

// TestFormControlDisplayBlockButtonLabelSkipsDisplayNoneDescendants is
// TestFormControlButtonLabelSkipsDisplayNoneDescendants's counterpart for the
// display:block entry point (contents(), not appendElementInline) — the
// label computation is duplicated at both entry points (see layout.go), and
// both need covering, same as the hidden-input case above.
func TestFormControlDisplayBlockButtonLabelSkipsDisplayNoneDescendants(t *testing.T) {
	src := `<html><body><button id="e" style="display:block">` +
		`<span>Search</span><kbd style="display:none">/</kbd>` +
		`</button></body></html>`
	box := findBox(layoutHTML(t, src, 1024), "button")
	if box == nil {
		t.Fatal("display:block button did not get its own box")
	}
	items := firstLineItems(box)
	if len(items) != 1 || items[0].FormControl == nil {
		t.Fatalf("display:block button's own box has no form-control item: %v", items)
	}
	if items[0].Label != "Search" {
		t.Errorf("Label = %q, want %q (the display:none kbd must not contribute)", items[0].Label, "Search")
	}
	assertF(t, "display:block button width sized to the visible label only", items[0].Width, 84)
}

// TestFormControlHiddenInputTakesNoBox covers the one control kind that must
// NOT get a box at all: a hidden input, matching real UA behavior (an
// invisible, un-clickable field carries no visible box or click target).
func TestFormControlHiddenInputTakesNoBox(t *testing.T) {
	src := `<html><body><input id="e" type="hidden" value="csrf-token">visible text</body></html>`
	items := firstLineItems(findBox(layoutHTML(t, src, 1024), "body"))
	for _, it := range items {
		if it.FormControl != nil {
			t.Fatalf("a hidden input produced a box: %v", it)
		}
	}
	if len(items) != 1 || items[0].Text != "visible" {
		// "visible text" is two words after fakeMeasurer's word-splitting;
		// just confirm the surrounding text still lays out normally.
		found := false
		for _, it := range items {
			if it.Text == "visible" {
				found = true
			}
		}
		if !found {
			t.Fatalf("surrounding text missing around the hidden input: %v", texts(items))
		}
	}
}

// TestFormControlDisplayBlockRoutesThroughContents covers the rarer path:
// author CSS giving a control `display:block` routes through the SAME
// sizing (contents()' own isFormControlTag branch), not appendElementInline
// — proving the two entry points agree rather than one being dead code that
// silently diverges from the other.
func TestFormControlDisplayBlockRoutesThroughContents(t *testing.T) {
	src := `<html><body><input id="e" style="display:block;width:150px;height:20px"></body></html>`
	box := findBox(layoutHTML(t, src, 1024), "input")
	if box == nil {
		t.Fatal("display:block input did not get its own box")
	}
	items := firstLineItems(box)
	if len(items) != 1 || items[0].FormControl == nil {
		t.Fatalf("display:block input's own box has no form-control item: %v", items)
	}
	assertF(t, "display:block width", items[0].Width, 150)
	assertF(t, "display:block height", items[0].LineHeight, 20)
}

// TestFormControlDisplayBlockHiddenTakesNoBox is
// TestFormControlHiddenInputTakesNoBox's counterpart for the display:block
// entry point (contents(), not appendElementInline) — the hidden check is
// duplicated at both entry points (see layout.go), and both need covering.
func TestFormControlDisplayBlockHiddenTakesNoBox(t *testing.T) {
	src := `<html><body><input id="e" type="hidden" style="display:block" value="csrf-token"></body></html>`
	box := findBox(layoutHTML(t, src, 1024), "input")
	if box != nil && len(firstLineItems(box)) > 0 {
		t.Fatalf("a display:block hidden input produced content: %v", firstLineItems(box))
	}
}

// TestFormControlSizeCountsTowardPreferredWidth covers a THIRD entry point
// into a form control's size, distinct from both tests above: preferredWidth
// (layout/floats.go), called when a form control sits behind one or more
// ELEMENT ancestors that preferredWidth recurses into directly (the flex-row
// sum and hasBlockLevelChild-max branches), never going through
// appendElementInline or contents()' own isFormControlTag branch at all.
// Confirmed live on github.com's "Go to file" search box: an unstyled
// `<input>` wrapped in a `<span style="display:flex">` (itself block-level,
// so a flex-shrink:0 ancestor's preferredWidth recursed straight into the
// span) reported 0 preferred width instead of the input's ~170px UA default,
// collapsing the container that should have made room for it and crowding
// the input against the following "Code" button.
func TestFormControlSizeCountsTowardPreferredWidth(t *testing.T) {
	src := `<html><body style="margin:0"><div style="display:flex;width:300px">` +
		`<div id="filler" style="flex:auto">filler filler filler</div>` +
		`<div id="wrap" style="flex-shrink:0"><span style="display:flex">` +
		`<input type="text"></span></div></div></body></html>`
	wrap := findBoxByID(layoutHTML(t, src, 300), "wrap")
	if wrap == nil {
		t.Fatal("wrap box not found")
	}
	// 170 (text input UA default, see formControlDefaultSize) + 2px of the
	// input's own UA-default 1px border on each side (see css/ua.go).
	assertF(t, "wrap.W", wrap.W, 172)
}

// TestFormControlButtonIconSizesToIconAndIsPainted covers the common
// entry point (appendElementInline, a plain unstyled inline <button> — the
// normal case, see formControlDefaultSize's UA-default display:inline doc
// comment) for an icon-only <button>: no visible text, a single svg child
// with a known intrinsic size. Before buttonIcon existed, this sized to
// just 2*formControlPadX/Y (a tiny padding-only box, the same "no
// fabricated Submit label" floor a genuinely empty button gets) and the
// icon was never referenced anywhere in the box tree — confirmed live on
// developer.mozilla.org's <mdn-search-button> and cited in
// formControlDefaultSize's own pre-existing doc comment for pkg.go.dev's
// search-submit button.
func TestFormControlButtonIconSizesToIconAndIsPainted(t *testing.T) {
	src := `<html><body><button id="e"><svg id="icon"></svg></button></body></html>`
	root, err := dom.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	sm := css.Cascade(root)
	icon := dom.Find(root, "svg")
	sizes := map[*dom.Node][2]float64{icon: {24, 24}}
	box, _ := LayoutDocument(root, sm, 1024, fakeMeasurer{}, sizes)
	items := firstLineItems(findBox(box, "body"))
	if len(items) != 1 || items[0].FormControl == nil {
		t.Fatalf("expected one form-control item, got %v", items)
	}
	item := items[0]
	if item.Label != "" {
		t.Fatalf("Label = %q, want empty (icon-only, no visible text)", item.Label)
	}
	if item.Icon != icon {
		t.Fatalf("Icon = %v, want the svg child %v", item.Icon, icon)
	}
	// 24 (icon) + 2*formControlPadX(12) = 48; 24 + 2*formControlPadY(6) = 36.
	assertF(t, "icon button width", item.Width, 48)
	assertF(t, "icon button height", item.LineHeight, 36)
}

// TestFormControlDisplayBlockButtonIconRoutesThroughContents is
// TestFormControlButtonIconSizesToIconAndIsPainted's counterpart for the
// display:block entry point (contents(), not appendElementInline) — the
// SAME icon-detection and sizing logic is duplicated at both entry points
// (mirroring TestFormControlDisplayBlockRoutesThroughContents above for
// plain sizing), and a first version of this fix only patched
// appendElementInline: a display:block button (or any button reached via
// contents() rather than inline collection) still lost its icon entirely
// until this path was fixed too.
func TestFormControlDisplayBlockButtonIconRoutesThroughContents(t *testing.T) {
	src := `<html><body><button id="e" style="display:block"><svg id="icon"></svg></button></body></html>`
	root, err := dom.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	sm := css.Cascade(root)
	icon := dom.Find(root, "svg")
	sizes := map[*dom.Node][2]float64{icon: {24, 24}}
	box, _ := LayoutDocument(root, sm, 1024, fakeMeasurer{}, sizes)
	btnBox := findBox(box, "button")
	if btnBox == nil {
		t.Fatal("display:block button did not get its own box")
	}
	items := firstLineItems(btnBox)
	if len(items) != 1 || items[0].FormControl == nil {
		t.Fatalf("display:block button's own box has no form-control item: %v", items)
	}
	if items[0].Icon != icon {
		t.Fatalf("Icon = %v, want the svg child %v", items[0].Icon, icon)
	}
	assertF(t, "display:block icon button width", items[0].Width, 48)
}

// TestFormControlButtonIconFallsBackWhenAmbiguousOrUnsized covers
// buttonIcon's deliberate refusal to guess: a button with more than one
// img/svg child (ambiguous — no confirmed real caller mixes multiple bare
// icons under one button), or whose lone icon's size never resolved (no
// imgSize entry and no width/height attributes), falls back to the
// pre-existing padding-only (or text-only) sizing with no Icon set, exactly
// as before this feature existed. A button with visible text ALONGSIDE a
// (resolvable, unambiguous) icon is no longer a fallback case — see
// TestFormControlButtonTextAndIconBothPaint below.
func TestFormControlButtonIconFallsBackWhenAmbiguousOrUnsized(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"two icon children", `<button id="e"><svg id="icon"></svg><svg id="icon2"></svg></button>`},
		{"icon size never resolved", `<button id="e"><svg id="icon"></svg></button>`},
		{"text alongside icon whose size never resolved", `<button id="e">Search<svg id="icon"></svg></button>`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root, err := dom.Parse(`<html><body>` + c.src + `</body></html>`)
			if err != nil {
				t.Fatal(err)
			}
			sm := css.Cascade(root)
			var sizes map[*dom.Node][2]float64
			if c.name == "two icon children" {
				icon := dom.Find(root, "svg")
				sizes = map[*dom.Node][2]float64{icon: {24, 24}}
			}
			box, _ := LayoutDocument(root, sm, 1024, fakeMeasurer{}, sizes)
			items := firstLineItems(findBox(box, "body"))
			if len(items) != 1 || items[0].FormControl == nil {
				t.Fatalf("expected one form-control item, got %v", items)
			}
			if items[0].Icon != nil {
				t.Fatalf("Icon = %v, want nil (fallback case)", items[0].Icon)
			}
		})
	}
}

// TestFormControlButtonTextAndIconBothPaint covers the confirmed real case
// buttonIcon's own doc comment now describes: github.com's own nav dropdown
// triggers ("Platform▾" and friends) are a <button> with BOTH a visible text
// label AND a trailing icon — previously an unconfirmed, deliberately
// unattempted case that fell back to text-only sizing with the icon silently
// dropped (never painted at all, not merely mis-sized), found live round 85.
func TestFormControlButtonTextAndIconBothPaint(t *testing.T) {
	src := `<html><body><button id="e">Platform<svg id="icon"></svg></button></body></html>`
	root, err := dom.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	sm := css.Cascade(root)
	icon := dom.Find(root, "svg")
	sizes := map[*dom.Node][2]float64{icon: {16, 16}}
	box, _ := LayoutDocument(root, sm, 1024, fakeMeasurer{}, sizes)
	items := firstLineItems(findBox(box, "body"))
	if len(items) != 1 || items[0].FormControl == nil {
		t.Fatalf("expected one form-control item, got %v", items)
	}
	item := items[0]
	if item.Label != "Platform" {
		t.Fatalf("Label = %q, want %q", item.Label, "Platform")
	}
	if item.Icon != icon {
		t.Fatalf("Icon = %v, want the svg child %v (must not be silently dropped)", item.Icon, icon)
	}
	// fakeMeasurer measures 10px per rune (see its own definition), so
	// "Platform" (8 runes) measures 80px: 80 (label) + buttonIconGap(4) +
	// 16 (icon) + 2*formControlPadX(12) = 124. Height: max(fontSize, 16) +
	// 2*formControlPadY(6) — the default font size (16, css.Cascade's own
	// root default) ties the icon's own height, so 16 + 12 = 28.
	assertF(t, "text+icon button width", item.Width, 124)
	assertF(t, "text+icon button height", item.LineHeight, 28)
}

// TestFormControlButtonTextAndIconHeightUsesTallerOfTheTwo exercises the
// OTHER side of the height formula TestFormControlButtonTextAndIconBothPaint
// left untested (that fixture's icon height happened to tie the default font
// size, so the "icon taller than the label's own line" branch never ran): an
// icon taller than the default font size must win the button's height, not
// be clipped down to the text's own line height.
func TestFormControlButtonTextAndIconHeightUsesTallerOfTheTwo(t *testing.T) {
	src := `<html><body><button id="e">OK<svg id="icon"></svg></button></body></html>`
	root, err := dom.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	sm := css.Cascade(root)
	icon := dom.Find(root, "svg")
	sizes := map[*dom.Node][2]float64{icon: {40, 40}} // well taller than the default 16px font size
	box, _ := LayoutDocument(root, sm, 1024, fakeMeasurer{}, sizes)
	items := firstLineItems(findBox(box, "body"))
	if len(items) != 1 {
		t.Fatalf("expected one form-control item, got %v", items)
	}
	// Height: max(fontSize=16, iconHeight=40) + 2*formControlPadY(6) = 52.
	assertF(t, "text+tall-icon button height", items[0].LineHeight, 52)
}

// TestFormControlButtonLeadingTallIconAndTextHeightUsesTallerOfTheTwo mirrors
// TestFormControlButtonTextAndIconHeightUsesTallerOfTheTwo for a LEADING icon
// (formControlDefaultSize's leading and trailing height comparisons are two
// independent if-blocks — see buttonIconGap's own doc comment on round 92 —
// so a tall trailing icon alone does not exercise the tall-leading-icon path).
func TestFormControlButtonLeadingTallIconAndTextHeightUsesTallerOfTheTwo(t *testing.T) {
	src := `<html><body><button id="e"><svg id="icon"></svg>OK</button></body></html>`
	root, err := dom.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	sm := css.Cascade(root)
	icon := dom.Find(root, "svg")
	sizes := map[*dom.Node][2]float64{icon: {40, 40}} // well taller than the default 16px font size
	box, _ := LayoutDocument(root, sm, 1024, fakeMeasurer{}, sizes)
	items := firstLineItems(findBox(box, "body"))
	if len(items) != 1 {
		t.Fatalf("expected one form-control item, got %v", items)
	}
	// Height: max(fontSize=16, iconHeight=40) + 2*formControlPadY(6) = 52.
	assertF(t, "leading-tall-icon+text button height", items[0].LineHeight, 52)
}

// TestFormControlButtonLeadingIconAndTextBothPaint is the mirror of
// TestFormControlButtonTextAndIconBothPaint: an icon found BEFORE the
// button's own text (github.com's own Primer buttons commonly lead with an
// icon, e.g. the "<> Code" button — see the sibling leading+trailing test
// below for that exact real shape). buttonIcons splits purely on document
// order, so this is the same mechanism, just the icon on the other side.
func TestFormControlButtonLeadingIconAndTextBothPaint(t *testing.T) {
	src := `<html><body><button id="e"><svg id="icon"></svg>Platform</button></body></html>`
	root, err := dom.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	sm := css.Cascade(root)
	icon := dom.Find(root, "svg")
	sizes := map[*dom.Node][2]float64{icon: {16, 16}}
	box, _ := LayoutDocument(root, sm, 1024, fakeMeasurer{}, sizes)
	items := firstLineItems(findBox(box, "body"))
	if len(items) != 1 || items[0].FormControl == nil {
		t.Fatalf("expected one form-control item, got %v", items)
	}
	item := items[0]
	if item.Label != "Platform" {
		t.Fatalf("Label = %q, want %q", item.Label, "Platform")
	}
	if item.LeadingIcon != icon {
		t.Fatalf("LeadingIcon = %v, want the svg child %v", item.LeadingIcon, icon)
	}
	if item.Icon != nil {
		t.Fatalf("Icon (trailing) = %v, want nil — the icon precedes the text", item.Icon)
	}
	// Same arithmetic as TestFormControlButtonTextAndIconBothPaint (only the
	// icon's SIDE differs, not the total reserved space): 124 x 28.
	assertF(t, "leading-icon+text button width", item.Width, 124)
	assertF(t, "leading-icon+text button height", item.LineHeight, 28)
}

// TestFormControlButtonLeadingAndTrailingIconBothPaint is the confirmed
// real-world shape (round 92): github.com's own Primer "<> Code ▾" button
// wraps a LEADING icon (a code-bracket glyph) and a TRAILING icon (a
// dropdown chevron) around its own "Code" label, each icon nested inside a
// non-replaced `<span data-component="leadingVisual"/"trailingVisual">`
// wrapper rather than being a direct child — buttonIcons searches ALL
// descendants, not just direct children, specifically for this shape.
// Previously buttonIcon (direct children only, single icon only) found
// NEITHER icon at all: not a direct child of the button, and the button's
// own DIRECT children were both wrapper spans (2 of them), tripping the
// "more than one candidate" ambiguity bail-out even before considering
// nesting depth.
func TestFormControlButtonLeadingAndTrailingIconBothPaint(t *testing.T) {
	// Uses distinct replaced tags (svg for the leading icon, img for the
	// trailing one) purely so the test can locate each by tag name — real
	// markup uses <svg> for both, which buttonIcons treats identically.
	src := `<html><body><button id="e"><span><svg id="lead"></svg></span>Code<span><img id="trail"></span></button></body></html>`
	root, err := dom.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	sm := css.Cascade(root)
	lead := dom.Find(root, "svg")
	trail := dom.Find(root, "img")
	sizes := map[*dom.Node][2]float64{lead: {16, 16}, trail: {16, 16}}
	box, _ := LayoutDocument(root, sm, 1024, fakeMeasurer{}, sizes)
	items := firstLineItems(findBox(box, "body"))
	if len(items) != 1 || items[0].FormControl == nil {
		t.Fatalf("expected one form-control item, got %v", items)
	}
	item := items[0]
	if item.Label != "Code" {
		t.Fatalf("Label = %q, want %q", item.Label, "Code")
	}
	if item.LeadingIcon != lead {
		t.Fatalf("LeadingIcon = %v, want %v (nested inside a wrapper span)", item.LeadingIcon, lead)
	}
	if item.Icon != trail {
		t.Fatalf("Icon (trailing) = %v, want %v (nested inside a wrapper span)", item.Icon, trail)
	}
	// "Code" (4 runes) measures 40px: 40 (label) + 2*formControlPadX(12) +
	// leading(16+gap4) + trailing(16+gap4) = 104. Height ties all three at
	// 16px: 16 + 2*formControlPadY(6) = 28.
	assertF(t, "leading+trailing-icon button width", item.Width, 104)
	assertF(t, "leading+trailing-icon button height", item.LineHeight, 28)
}
