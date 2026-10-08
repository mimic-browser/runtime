// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package paint

import (
	"image"
	"image/color"
	"testing"

	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/dom"
	"github.com/go-webengine/engine/layout"
)

// controlStyle mirrors what css/ua.go's real UA defaults now give a text-
// like control (background-color:#ffffff; border:1px solid #767676) — paint
// no longer supplies these itself, it reads them from the cascaded style
// (an author reset like `background:0 0;border:0` must be honoured, see
// TestPaintFormControlHonoursAuthorReset below), so a hand-built test style
// needs to carry them explicitly to exercise the SAME visible-box contract
// the old hardcoded-colour version tested.
func controlStyle() *css.Style {
	return &css.Style{FontFamily: css.Sans, FontSize: 14, FontWeight: 400, Color: css.Color{A: 255},
		Background: formFieldBg,
		Border: css.Borders{
			Top: css.BorderSide{Width: 1, Style: css.BorderSolid, Color: formBorder},
		},
	}
}

func TestTransparentRadioInputDoesNotPaintNativeControl(t *testing.T) {
	n := &dom.Node{Type: dom.Element, Tag: "input", Attr: map[string]string{"type": "radio", "checked": ""}}
	st := controlStyle()
	st.HasOpacity = true
	st.Opacity = 0
	img := paintControlStyled(t, n, 20, 20, st)
	if got := img.RGBAAt(10, 10); got != (color.RGBA{R: 255, G: 255, B: 255, A: 255}) {
		t.Fatalf("opacity:0 radio painted native control: %v", got)
	}
}

// paintControl lays out a single form-control InlineItem at (0,0) sized
// w×h and paints it onto a fresh white dst, mirroring how background_paint_
// test.go hand-builds a Box rather than going through the full HTML
// pipeline (paint's own tests are pipeline-agnostic by convention).
func paintControl(t *testing.T, n *dom.Node, w, h float64) *image.RGBA {
	t.Helper()
	return paintControlStyled(t, n, w, h, controlStyle())
}

// paintControlStyled is paintControl with an explicit style, for a test that
// needs a specific cascaded Background/Border (e.g. a button's own default,
// or an author reset) rather than controlStyle's plain text-field look.
func paintControlStyled(t *testing.T, n *dom.Node, w, h float64, style *css.Style) *image.RGBA {
	t.Helper()
	dst := white(int(w)+20, int(h)+20)
	item := &layout.InlineItem{
		Node: n, FormControl: n, Style: style,
		Width: w, Ascent: h, LineHeight: h, X: 5, Y: 5,
	}
	box := &layout.Box{
		Lines: []*layout.LineBox{{X: 5, Y: 5, W: w, H: h, Items: []*layout.InlineItem{item}}},
		W:     w + 10, H: h + 10,
	}
	PaintFull(dst, box, NewFonts(), nil, nil)
	return dst
}

func elem(tag string, attrs map[string]string) *dom.Node {
	return &dom.Node{Type: dom.Element, Tag: tag, Attr: attrs}
}

// TestPaintFormControlDrawsBackgroundAndBorder covers the base visible-box
// contract every kind shares: a border-colored pixel at the edge, a
// background-colored (not border, not raw white-canvas) pixel inside.
func TestPaintFormControlDrawsBackgroundAndBorder(t *testing.T) {
	n := elem("input", map[string]string{"id": "e"})
	dst := paintControl(t, n, 100, 24)

	edge := dst.RGBAAt(5, 5)
	if got, want := (css.Color{R: edge.R, G: edge.G, B: edge.B, A: 255}), formBorder; got != want {
		t.Errorf("top-left edge = %+v, want border colour %+v", got, want)
	}
	inside := dst.RGBAAt(50, 15)
	if got, want := (css.Color{R: inside.R, G: inside.G, B: inside.B, A: 255}), formFieldBg; got != want {
		t.Errorf("interior = %+v, want field background %+v", got, want)
	}
}

// TestPaintFormControlButtonBackground covers the button-like kind's darker
// background (css/ua.go's own default for button/select), distinguishing it
// visually from a plain text field. Also exercises the button label's
// horizontal-centering draw path (an empty <button> — an icon-only submit
// button, e.g. — has no label at all since the fix for pkg.go.dev's
// "Submit" text wrongly appearing next to its search icon, so this needs an
// explicit Label to still reach that code path).
func TestPaintFormControlButtonBackground(t *testing.T) {
	n := elem("button", map[string]string{"id": "e"})
	style := &css.Style{FontFamily: css.Sans, FontSize: 14, FontWeight: 400, Color: css.Color{A: 255},
		Background: formButtonBg,
		Border:     css.Borders{Top: css.BorderSide{Width: 1, Style: css.BorderSolid, Color: formBorder}},
	}
	dst := white(100, 50)
	item := &layout.InlineItem{
		Node: n, FormControl: n, Style: style, Label: "Go",
		Width: 80, Ascent: 30, LineHeight: 30, X: 5, Y: 5,
	}
	box := &layout.Box{
		Lines: []*layout.LineBox{{X: 5, Y: 5, W: 80, H: 30, Items: []*layout.InlineItem{item}}},
		W:     90, H: 40,
	}
	PaintFull(dst, box, NewFonts(), nil, nil)
	// Near the top, above where the centered "Go" label's glyphs reach
	// (the label draw is centered vertically too, so avoid sampling where
	// an ascender could land).
	inside := dst.RGBAAt(40, 9)
	if got, want := (css.Color{R: inside.R, G: inside.G, B: inside.B, A: 255}), formButtonBg; got != want {
		t.Errorf("button interior = %+v, want button background %+v", got, want)
	}
}

// TestPaintFormControlHonoursAuthorReset covers a real regression: a
// button/select/input's background+border used to be a HARDCODED colour
// paint chose regardless of the element's own cascaded style, so an author
// reset (`background:0 0;border:0` — confirmed live on github.com's own top
// nav <button>s, styled to look like plain text links, not gray boxes) was
// always overridden by fake generic chrome. A zero-alpha Background and a
// BorderNone/zero-width Border (exactly what that reset cascades to) must
// now paint NOTHING for the box itself — only the canvas underneath.
func TestPaintFormControlHonoursAuthorReset(t *testing.T) {
	n := elem("button", map[string]string{"id": "e"})
	style := &css.Style{FontFamily: css.Sans, FontSize: 14, FontWeight: 400, Color: css.Color{A: 255}}
	// Background and Border are the zero value here — exactly what
	// `background:0 0;border:0` cascades to — deliberately, not an oversight.
	dst := paintControlStyled(t, n, 80, 30, style)

	edge := dst.RGBAAt(5, 5)
	if got := (css.Color{R: edge.R, G: edge.G, B: edge.B, A: 255}); got == formBorder {
		t.Errorf("edge = %+v, an author border:0 must not paint the UA border colour", got)
	}
	inside := dst.RGBAAt(40, 9)
	if got := (css.Color{R: inside.R, G: inside.G, B: inside.B, A: 255}); got == formButtonBg {
		t.Errorf("interior = %+v, an author background:0 must not paint the UA button colour", got)
	}
}

// TestPaintFormControlPerSideBorder covers a real regression, found live on
// caniuse.com: `border:0;border-bottom:1px solid #fff` (the author's own
// search input) previously drew NOTHING at all, since paintFormControl's own
// border check inspected ONLY Style.Border.Top's width/style/colour and
// stroked one uniform-colour rectangle for all four sides when it painted —
// `border:0` zeroes every side first, then `border-bottom:...` overrides
// only the bottom one, so Border.Top stayed at zero and the whole check
// (and everything it gated) was skipped. Now routed through paintEdges, the
// SAME per-side helper a real layout.Box's own border already used.
func TestPaintFormControlPerSideBorder(t *testing.T) {
	n := elem("input", map[string]string{"id": "e"})
	// A distinct, non-white border colour: paintControlStyled's canvas starts
	// all-white, so a white border pixel would be indistinguishable from an
	// unpainted (still-white) one — this colour makes "did the bottom edge
	// actually get painted, and did the top edge correctly NOT" unambiguous.
	borderColor := css.Color{R: 255, A: 255}
	style := &css.Style{FontFamily: css.Sans, FontSize: 14, FontWeight: 400, Color: css.Color{A: 255},
		Background: css.Color{}, // border:0;background:transparent, like the real page
		Border: css.Borders{
			Bottom: css.BorderSide{Width: 1, Style: css.BorderSolid, Color: borderColor},
		},
	}
	dst := paintControlStyled(t, n, 100, 24, style)

	bottom := dst.RGBAAt(50, 5+24-1)
	if got, want := (css.Color{R: bottom.R, G: bottom.G, B: bottom.B, A: 255}), borderColor; got != want {
		t.Errorf("bottom edge = %+v, want the border-bottom colour %+v", got, want)
	}
	top := dst.RGBAAt(50, 5)
	if got := (css.Color{R: top.R, G: top.G, B: top.B, A: 255}); got == borderColor {
		t.Errorf("top edge = %+v, a border-bottom-only style must not also paint the top edge", got)
	}
}

// TestPaintCheckboxCheckedVsUnchecked covers the one kind with a state-
// dependent fill: unchecked is the plain field background, checked is the
// accent colour — the visible signal a login "remember me" box relies on.
func TestPaintCheckboxCheckedVsUnchecked(t *testing.T) {
	unchecked := elem("input", map[string]string{"type": "checkbox"})
	dstU := paintControl(t, unchecked, 13, 13)
	cu := dstU.RGBAAt(9, 9) // avoid the 1px border at (5,5)/(6,6)
	if got, want := (css.Color{R: cu.R, G: cu.G, B: cu.B, A: 255}), formFieldBg; got != want {
		t.Errorf("unchecked interior = %+v, want %+v", got, want)
	}

	checked := elem("input", map[string]string{"type": "checkbox", "checked": ""})
	dstC := paintControl(t, checked, 13, 13)
	cc := dstC.RGBAAt(9, 9)
	if got, want := (css.Color{R: cc.R, G: cc.G, B: cc.B, A: 255}), formAccent; got != want {
		t.Errorf("checked interior = %+v, want accent %+v", got, want)
	}
}

// TestPaintCheckboxIgnoresAuthorStyleWithoutAppearanceNone is a regression
// guard for the dispatch condition itself: WITHOUT `appearance:none`, a
// checkbox's own cascaded Background/Border must still be completely
// ignored in favour of the generic square, exactly as before this round —
// only AppearanceNone opts a checkbox out of that generic look. A custom
// magenta background here would otherwise leak through if the dispatch
// check were ever accidentally dropped or inverted.
func TestPaintCheckboxIgnoresAuthorStyleWithoutAppearanceNone(t *testing.T) {
	n := elem("input", map[string]string{"type": "checkbox"})
	style := &css.Style{Background: css.Color{R: 255, B: 255, A: 255}} // magenta, AppearanceNone: false
	dst := paintControlStyled(t, n, 13, 13, style)
	c := dst.RGBAAt(9, 9)
	if got, want := (css.Color{R: c.R, G: c.G, B: c.B, A: 255}), formFieldBg; got != want {
		t.Errorf("interior = %+v, want the generic square's %+v (author background must be ignored)", got, want)
	}
}

// TestPaintCheckboxAppearanceNoneHonoursBackground covers the confirmed real
// trigger's central mechanism (developer.mozilla.org's <mdn-switch>, see
// css.Style.AppearanceNone's own doc comment): `appearance:none` on a
// checkbox switches it from this engine's generic square to a plain styled
// box that paints the author's own cascaded background colour, matching
// paintFormControl's own non-checkbox path.
func TestPaintCheckboxAppearanceNoneHonoursBackground(t *testing.T) {
	n := elem("input", map[string]string{"type": "checkbox"})
	magenta := css.Color{R: 255, B: 255, A: 255}
	style := &css.Style{AppearanceNone: true, Background: magenta}
	dst := paintControlStyled(t, n, 13, 13, style)
	c := dst.RGBAAt(9, 9)
	if got := (css.Color{R: c.R, G: c.G, B: c.B, A: 255}); got != magenta {
		t.Errorf("interior = %+v, want author background %+v", got, magenta)
	}
}

// TestPaintCheckboxAppearanceNoneHonoursBackgroundImage covers the OTHER
// half of the real trigger: MDN's own switch knob is a `background-image:
// radial-gradient(...)`, not a solid colour — reusing paintBackgroundLayers
// (the same helper a real layout.Box's own gradient background already
// uses) must paint it here too.
func TestPaintCheckboxAppearanceNoneHonoursBackgroundImage(t *testing.T) {
	n := elem("input", map[string]string{"type": "checkbox"})
	style := linearGradStyle(90, stop(css.Color{R: 0, G: 0, B: 0, A: 255}), stop(css.Color{R: 255, G: 255, B: 255, A: 255}))
	style.AppearanceNone = true
	dst := paintControlStyled(t, n, 40, 13, style)
	left := dst.RGBAAt(5, 9)
	right := dst.RGBAAt(44, 9) // item spans X:[5,45) — 44 is its rightmost pixel
	if left.R > 8 {
		t.Errorf("left = %+v, want ~black (gradient start)", left)
	}
	if right.R < 247 {
		t.Errorf("right = %+v, want ~white (gradient end)", right)
	}
}

// TestPaintCheckboxAppearanceNoneHonoursBorder covers the author's own
// border (paintEdges, the SAME per-side helper a real element's border
// uses) replacing the generic square's hardcoded outline — and, with no
// border declared at all (css/ua.go's own `border:0` UA default for
// checkbox/radio, left standing since `appearance:none` does not itself
// paint a native fallback border), that NOTHING paints at the edge, mirroring
// a real browser's own bare, unstyled appearance:none checkbox.
func TestPaintCheckboxAppearanceNoneHonoursBorder(t *testing.T) {
	n := elem("input", map[string]string{"type": "checkbox"})
	green := css.Color{G: 128, A: 255}
	styled := &css.Style{AppearanceNone: true, Border: css.Borders{
		Top: css.BorderSide{Width: 2, Style: css.BorderSolid, Color: green},
	}}
	dst := paintControlStyled(t, n, 13, 13, styled)
	edge := dst.RGBAAt(5, 5)
	if got := (css.Color{R: edge.R, G: edge.G, B: edge.B, A: 255}); got != green {
		t.Errorf("top edge = %+v, want author border %+v", got, green)
	}

	bare := &css.Style{AppearanceNone: true} // no Background, no Border at all
	dstBare := paintControlStyled(t, n, 13, 13, bare)
	// The item spans X:[5,18) Y:[5,18) on an 33x33 canvas — check every pixel
	// inside that box (not hasNonBackgroundPixel, whose fixed 100px-wide scan
	// range assumes the larger controls elsewhere in this file and would read
	// past this small canvas's own bounds) stayed untouched white.
	for y := 5; y < 18; y++ {
		for x := 5; x < 18; x++ {
			if c := dstBare.RGBAAt(x, y); c.R != 255 || c.G != 255 || c.B != 255 {
				t.Fatalf("pixel (%d,%d) = %+v, want untouched white (no author background/border)", x, y, c)
			}
		}
	}
}

// TestPaintCheckboxAppearanceNoneHonoursBorderRadius covers styleRadius
// integration: a pill-shaped (border-radius: 50%) appearance:none checkbox
// must leave its own corners unpainted (outside the rounded shape) while its
// centre still fills — MDN's own switch relies on exactly this for its pill
// track.
func TestPaintCheckboxAppearanceNoneHonoursBorderRadius(t *testing.T) {
	n := elem("input", map[string]string{"type": "checkbox"})
	blue := css.Color{B: 255, A: 255}
	style := &css.Style{AppearanceNone: true, Background: blue, BorderRadius: css.Length{IsPercent: true, Percent: 0.5}}
	dst := paintControlStyled(t, n, 20, 20, style)
	center := dst.RGBAAt(15, 15)
	if got := (css.Color{R: center.R, G: center.G, B: center.B, A: 255}); got != blue {
		t.Errorf("centre = %+v, want fill %+v", got, blue)
	}
	corner := dst.RGBAAt(5, 5) // the box's own extreme top-left corner pixel
	if got := (css.Color{R: corner.R, G: corner.G, B: corner.B, A: 255}); got == blue {
		t.Errorf("corner = %+v, want untouched white (outside the rounded/circular shape)", got)
	}
}

// TestPaintFormControlDrawsSomeText covers that a control WITH a value
// actually draws glyphs (some non-background pixel inside), and one with
// none does not — the difference proves text painting is actually wired,
// not just the box.
func TestPaintFormControlDrawsSomeText(t *testing.T) {
	withValue := elem("input", map[string]string{"value": "hello"})
	dst := paintControl(t, withValue, 100, 24)
	if !hasNonBackgroundPixel(dst, formFieldBg) {
		t.Error("a valued input painted no glyphs at all")
	}

	empty := elem("input", map[string]string{})
	dstEmpty := paintControl(t, empty, 100, 24)
	if hasNonBackgroundPixel(dstEmpty, formFieldBg) {
		t.Error("an empty, placeholder-less input painted something other than its box")
	}

	// A placeholder (muted text) must ALSO paint glyphs — the muted colour
	// is a different draw color, not a skip.
	placeholder := elem("input", map[string]string{"placeholder": "Email"})
	dstPH := paintControl(t, placeholder, 100, 24)
	if !hasNonBackgroundPixel(dstPH, formFieldBg) {
		t.Error("a placeholder input painted no glyphs at all")
	}
}

// TestPaintFormControlNilStylePaintsBoxOnly guards paintFormControl's own
// nil-Style path (distinct from Text=="" — Style itself absent, which a box
// with no computed style at all would hit): the box/border must still
// paint without a nil-pointer panic; text painting is simply skipped.
func TestPaintFormControlNilStylePaintsBoxOnly(t *testing.T) {
	n := elem("input", map[string]string{"value": "hello"})
	dst := white(120, 40)
	item := &layout.InlineItem{Node: n, FormControl: n, Style: nil, Width: 100, Ascent: 24, LineHeight: 24, X: 5, Y: 5}
	box := &layout.Box{Lines: []*layout.LineBox{{X: 5, Y: 5, W: 100, H: 24, Items: []*layout.InlineItem{item}}}, W: 110, H: 34}
	PaintFull(dst, box, NewFonts(), nil, nil) // must not panic
	edge := dst.RGBAAt(5, 5)
	if got, want := (css.Color{R: edge.R, G: edge.G, B: edge.B, A: 255}), formBorder; got != want {
		t.Errorf("nil-Style control still painted no border: got %+v want %+v", got, want)
	}
}

// TestPaintFormControlIconDrawsBitmapCentered guards paintFormControl's
// InlineItem.Icon path: an icon-only button (Label=="") must blit its
// icon's own bitmap (looked up in imgs, the SAME map an ordinary Image item
// uses) centred in the control's box, not leave it empty the way a bare
// FormControl item with no text used to before this field existed.
func TestPaintFormControlIconDrawsBitmapCentered(t *testing.T) {
	n := elem("button", map[string]string{})
	iconNode := elem("svg", map[string]string{})
	icon := image.NewRGBA(image.Rect(0, 0, 10, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			icon.Set(x, y, color.RGBA{R: 0xff, A: 0xff})
		}
	}
	dst := white(50, 50)
	item := &layout.InlineItem{
		Node: n, FormControl: n, Icon: iconNode, Style: controlStyle(),
		Width: 30, Ascent: 30, LineHeight: 30, X: 5, Y: 5,
	}
	box := &layout.Box{
		Lines: []*layout.LineBox{{X: 5, Y: 5, W: 30, H: 30, Items: []*layout.InlineItem{item}}},
		W:     40, H: 40,
	}
	PaintFull(dst, box, NewFonts(), map[*dom.Node]image.Image{iconNode: icon}, nil)
	// Box spans x,y in [5,35); a 10x10 icon centred in it covers [15,25).
	center := dst.RGBAAt(20, 20)
	if center.R != 0xff || center.G != 0 || center.B != 0 {
		t.Errorf("icon center pixel = %+v, want opaque red", center)
	}
	// Outside the icon but still inside the box: the control's own
	// background, not the icon colour bleeding out.
	corner := dst.RGBAAt(6, 6)
	if corner.R == 0xff && corner.G == 0 && corner.B == 0 {
		t.Errorf("icon painted outside its own bounds: corner = %+v", corner)
	}
}

// TestPaintFormControlTextAndIconBothDraw guards paintFormControl's combined
// text+icon path (github.com's own nav dropdown triggers — "Platform▾" and
// friends, round 85): a button item with BOTH a non-empty Label AND a
// non-nil Icon must draw the icon's own bitmap SOMEWHERE in its box, not
// just the label text — the icon was previously silently dropped whenever a
// label was also present (an early return before it was ever reached).
func TestPaintFormControlTextAndIconBothDraw(t *testing.T) {
	n := elem("button", map[string]string{})
	iconNode := elem("svg", map[string]string{})
	icon := image.NewRGBA(image.Rect(0, 0, 10, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			icon.Set(x, y, color.RGBA{B: 0xff, A: 0xff}) // pure blue, unlike any text/bg colour here
		}
	}
	dst := white(100, 50)
	st := controlStyle()
	item := &layout.InlineItem{
		Node: n, FormControl: n, Icon: iconNode, Label: "OK", Style: st,
		Width: 80, Ascent: 30, LineHeight: 30, X: 5, Y: 5,
	}
	box := &layout.Box{
		Lines: []*layout.LineBox{{X: 5, Y: 5, W: 80, H: 30, Items: []*layout.InlineItem{item}}},
		W:     90, H: 40,
	}
	PaintFull(dst, box, NewFonts(), map[*dom.Node]image.Image{iconNode: icon}, nil)

	foundBlue := false
	foundDark := false
	for y := 5; y < 35; y++ {
		for x := 5; x < 85; x++ {
			c := dst.RGBAAt(x, y)
			if c.B == 0xff && c.R == 0 && c.G == 0 {
				foundBlue = true
			}
			if c.R < 100 && c.G < 100 && c.B < 100 {
				foundDark = true
			}
		}
	}
	if !foundBlue {
		t.Error("icon bitmap not found anywhere in the control's box (Icon silently dropped alongside Label)")
	}
	if !foundDark {
		t.Error("label text not found anywhere in the control's box")
	}
}

// TestPaintFormControlLeadingAndTrailingIconBothDraw guards paintFormControl's
// leading+trailing+label path (github.com's own Primer "<> Code ▾" button,
// round 92): a leading icon, a trailing icon and a label together must each
// draw at their own DOCUMENT-ORDER position — leading strictly left of the
// label's own pixels, trailing strictly right of them — not just "present
// somewhere" (TestPaintFormControlTextAndIconBothDraw already guards that
// weaker property for the single-icon case).
func TestPaintFormControlLeadingAndTrailingIconBothDraw(t *testing.T) {
	n := elem("button", map[string]string{})
	leadNode := elem("svg", map[string]string{})
	trailNode := elem("img", map[string]string{})
	lead := image.NewRGBA(image.Rect(0, 0, 10, 10))
	trail := image.NewRGBA(image.Rect(0, 0, 10, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			lead.Set(x, y, color.RGBA{R: 0xff, A: 0xff})  // pure red
			trail.Set(x, y, color.RGBA{G: 0xff, A: 0xff}) // pure green
		}
	}
	dst := white(140, 50)
	st := controlStyle()
	item := &layout.InlineItem{
		Node: n, FormControl: n, LeadingIcon: leadNode, Icon: trailNode, Label: "OK", Style: st,
		Width: 130, Ascent: 30, LineHeight: 30, X: 5, Y: 5,
	}
	box := &layout.Box{
		Lines: []*layout.LineBox{{X: 5, Y: 5, W: 130, H: 30, Items: []*layout.InlineItem{item}}},
		W:     140, H: 40,
	}
	PaintFull(dst, box, NewFonts(), map[*dom.Node]image.Image{leadNode: lead, trailNode: trail}, nil)

	var redX, greenX, darkX []int
	for y := 5; y < 35; y++ {
		for x := 5; x < 135; x++ {
			c := dst.RGBAAt(x, y)
			switch {
			case c.R == 0xff && c.G == 0 && c.B == 0:
				redX = append(redX, x)
			case c.G == 0xff && c.R == 0 && c.B == 0:
				greenX = append(greenX, x)
			case c.R < 100 && c.G < 100 && c.B < 100:
				darkX = append(darkX, x)
			}
		}
	}
	if len(redX) == 0 {
		t.Fatal("leading icon (red) not found anywhere in the control's box")
	}
	if len(greenX) == 0 {
		t.Fatal("trailing icon (green) not found anywhere in the control's box")
	}
	if len(darkX) == 0 {
		t.Fatal("label text not found anywhere in the control's box")
	}
	maxRed, minGreen, minDark, maxDark := max(redX), min(greenX), min(darkX), max(darkX)
	if maxRed >= minDark {
		t.Errorf("leading icon (rightmost red x=%d) not strictly left of label (leftmost dark x=%d)", maxRed, minDark)
	}
	if minGreen <= maxDark {
		t.Errorf("trailing icon (leftmost green x=%d) not strictly right of label (rightmost dark x=%d)", minGreen, maxDark)
	}
}

func max(xs []int) int {
	m := xs[0]
	for _, x := range xs[1:] {
		if x > m {
			m = x
		}
	}
	return m
}

func min(xs []int) int {
	m := xs[0]
	for _, x := range xs[1:] {
		if x < m {
			m = x
		}
	}
	return m
}

func hasNonBackgroundPixel(img *image.RGBA, bg css.Color) bool {
	for y := 7; y < 20; y++ { // inside the box, away from the border
		for x := 7; x < 90; x++ {
			c := img.RGBAAt(x, y)
			if c.R != bg.R || c.G != bg.G || c.B != bg.B {
				return true
			}
		}
	}
	return false
}

func TestFormControlKind(t *testing.T) {
	cases := []struct {
		n    *dom.Node
		want controlKind
	}{
		{elem("input", map[string]string{"type": "checkbox"}), controlCheckbox},
		{elem("input", map[string]string{"type": "CHECKBOX"}), controlCheckbox},
		{elem("input", map[string]string{"type": "radio"}), controlRadio},
		{elem("input", map[string]string{"type": "submit"}), controlButtonLike},
		{elem("input", map[string]string{"type": "button"}), controlButtonLike},
		{elem("input", map[string]string{"type": "reset"}), controlButtonLike},
		{elem("input", map[string]string{"type": "text"}), controlText},
		{elem("input", map[string]string{}), controlText},
		{elem("button", map[string]string{}), controlButtonLike},
		{elem("select", map[string]string{}), controlSelect},
		{elem("textarea", map[string]string{}), controlTextarea},
		{elem("span", nil), controlText},
	}
	for _, c := range cases {
		if got := formControlKind(c.n); got != c.want {
			t.Errorf("formControlKind(%s type=%q) = %v, want %v", c.n.Tag, c.n.Attr["type"], got, c.want)
		}
	}
}

func TestFormControlDisplayText(t *testing.T) {
	cases := []struct {
		name      string
		n         *dom.Node
		label     string // InlineItem.Label — only meaningful for "button"
		wantText  string
		wantMuted bool
	}{
		{"text value", elem("input", map[string]string{"value": "hi"}), "", "hi", false},
		{"password masks", elem("input", map[string]string{"type": "password", "value": "abc"}), "", "•••", false},
		{"placeholder is muted", elem("input", map[string]string{"placeholder": "Email"}), "", "Email", true},
		{"empty, no placeholder", elem("input", map[string]string{}), "", "", false},
		{"submit uses controlLabel", elem("input", map[string]string{"type": "submit"}), "", "Submit", false},
		{"button uses precomputed label", elem("button", nil), "Go", "Go", false},
		{"button tag empty has no label", elem("button", map[string]string{}), "", "", false},
		{"textarea value attr", elem("textarea", map[string]string{"value": "explicit"}), "", "explicit", false},
		{"textarea text content", func() *dom.Node {
			n := elem("textarea", nil)
			n.Children = []*dom.Node{{Type: dom.Text, Text: "content"}}
			return n
		}(), "", "content", false},
		{"textarea placeholder", elem("textarea", map[string]string{"placeholder": "Bio"}), "", "Bio", true},
		{"textarea completely empty", elem("textarea", map[string]string{}), "", "", false},
		{"select with options", func() *dom.Node {
			n := elem("select", nil)
			n.Children = []*dom.Node{
				elem("option", map[string]string{"value": "a"}),
			}
			n.Children[0].Children = []*dom.Node{{Type: dom.Text, Text: "A"}}
			return n
		}(), "", "A", false},
		{"select with no options", elem("select", nil), "", "", false},
		{"unknown tag", elem("span", nil), "", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			text, muted := formControlDisplayText(c.n, c.label)
			if text != c.wantText || muted != c.wantMuted {
				t.Errorf("formControlDisplayText = (%q, %v), want (%q, %v)", text, muted, c.wantText, c.wantMuted)
			}
		})
	}
}

func TestControlLabelReset(t *testing.T) {
	if got := controlLabel(elem("input", map[string]string{"type": "reset"})); got != "Reset" {
		t.Errorf("controlLabel(reset) = %q, want Reset", got)
	}
	if got := controlLabel(elem("input", map[string]string{"value": "Go!"})); got != "Go!" {
		t.Errorf("controlLabel with a value = %q, want Go!", got)
	}
}

func TestSelectedOptionLabelNoOptions(t *testing.T) {
	if _, ok := selectedOptionLabel(elem("select", nil)); ok {
		t.Error("selectedOptionLabel with no <option> children: want ok=false")
	}
}

func TestSelectedOptionLabelPicksSelected(t *testing.T) {
	sel := elem("select", nil)
	a := elem("option", map[string]string{"value": "a"})
	a.Children = []*dom.Node{{Type: dom.Text, Text: "A"}}
	b := elem("option", map[string]string{"value": "b", "selected": ""})
	b.Children = []*dom.Node{{Type: dom.Text, Text: "B"}}
	sel.Children = []*dom.Node{a, b}

	got, ok := selectedOptionLabel(sel)
	if !ok || got != "B" {
		t.Fatalf("selectedOptionLabel = (%q, %v), want (B, true)", got, ok)
	}
}

// TestSelectedOptionLabelLastSelectedWins covers the HTML standard's option
// selectedness algorithm (https://html.spec.whatwg.org/multipage/form-elements.html#concept-option-selectedness):
// when several <option> elements carry `selected`, the LAST one in tree
// order wins for a non-multiple <select> — not the first.
func TestSelectedOptionLabelLastSelectedWins(t *testing.T) {
	a := elem("option", map[string]string{"selected": ""})
	a.Children = []*dom.Node{{Type: dom.Text, Text: "A"}}
	b := elem("option", map[string]string{"selected": ""})
	b.Children = []*dom.Node{{Type: dom.Text, Text: "B"}}
	sel := elem("select", nil)
	// A whitespace text node between sibling <option>s, exactly like real
	// HTML markup (indentation/newlines) always has, must be skipped rather
	// than mistaken for an element.
	sel.Children = []*dom.Node{a, {Type: dom.Text, Text: "\n\t"}, b}

	got, ok := selectedOptionLabel(sel)
	if !ok || got != "B" {
		t.Fatalf("selectedOptionLabel = (%q, %v), want (B, true) — last selected wins", got, ok)
	}
}

// TestSelectedOptionLabelSkipsDisabledDefault covers the standard's default
// (no explicit selection) rule: the FIRST option that is not itself disabled
// wins, not simply the first option in source order.
func TestSelectedOptionLabelSkipsDisabledDefault(t *testing.T) {
	skip := elem("option", map[string]string{"disabled": ""})
	skip.Children = []*dom.Node{{Type: dom.Text, Text: "SKIP"}}
	first := elem("option", nil)
	first.Children = []*dom.Node{{Type: dom.Text, Text: "FIRST"}}
	sel := elem("select", nil)
	sel.Children = []*dom.Node{skip, first}

	got, ok := selectedOptionLabel(sel)
	if !ok || got != "FIRST" {
		t.Fatalf("selectedOptionLabel = (%q, %v), want (FIRST, true) — disabled option skipped", got, ok)
	}
}

// TestSelectedOptionLabelOptgroupDisabledSkipsChildren covers that an
// <optgroup disabled> disables every option inside it for default selection,
// even though the option itself carries no `disabled` attribute of its own.
func TestSelectedOptionLabelOptgroupDisabledSkipsChildren(t *testing.T) {
	inGroup := elem("option", nil)
	inGroup.Children = []*dom.Node{{Type: dom.Text, Text: "A"}}
	group := elem("optgroup", map[string]string{"disabled": ""})
	group.Children = []*dom.Node{inGroup}
	after := elem("option", nil)
	after.Children = []*dom.Node{{Type: dom.Text, Text: "B"}}
	sel := elem("select", nil)
	sel.Children = []*dom.Node{group, after}

	got, ok := selectedOptionLabel(sel)
	if !ok || got != "B" {
		t.Fatalf("selectedOptionLabel = (%q, %v), want (B, true) — A is inside a disabled optgroup", got, ok)
	}
}

// TestSelectedOptionLabelAttributeOverridesText covers the option label rule
// (https://html.spec.whatwg.org/multipage/form-elements.html#the-option-element):
// a non-empty `label` attribute is shown instead of the element's text
// content.
func TestSelectedOptionLabelAttributeOverridesText(t *testing.T) {
	opt := elem("option", map[string]string{"selected": "", "label": "Custom"})
	opt.Children = []*dom.Node{{Type: dom.Text, Text: "ignored text"}}
	sel := elem("select", nil)
	sel.Children = []*dom.Node{opt}

	got, ok := selectedOptionLabel(sel)
	if !ok || got != "Custom" {
		t.Fatalf("selectedOptionLabel = (%q, %v), want (Custom, true)", got, ok)
	}
}
