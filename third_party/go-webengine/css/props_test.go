// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package css

import (
	"math"
	"testing"
)

func newStyle() *Style {
	s := initialStyle()
	return &s
}

func applyOn(s *Style, prop, val string, em float64) {
	s.apply(Declaration{Property: prop, Value: val}, em, nil)
}

func TestApplyDisplayValues(t *testing.T) {
	cases := map[string]Display{
		"block": DisplayBlock, "flow-root": DisplayBlock, "list-item": DisplayBlock,
		"grid": DisplayGrid, "inline-grid": DisplayGrid,
		"flex": DisplayFlex, "inline-flex": DisplayInlineFlex,
		"table": DisplayTable, "inline-table": DisplayTable,
		"table-row": DisplayTableRow, "table-cell": DisplayTableCell,
		"table-row-group": DisplayTableRowGroup, "table-header-group": DisplayTableRowGroup,
		"inline-block": DisplayInlineBlock, "inline": DisplayInline, "none": DisplayNone,
	}
	for v, want := range cases {
		s := newStyle()
		applyOn(s, "display", v, 16)
		if s.Display != want {
			t.Errorf("display:%s = %v want %v", v, s.Display, want)
		}
	}
}

func TestApplyVisibilityValues(t *testing.T) {
	cases := map[string]Visibility{
		"visible": VisibilityVisible, "hidden": VisibilityHidden, "collapse": VisibilityCollapse,
	}
	for v, want := range cases {
		s := newStyle()
		applyOn(s, "visibility", v, 16)
		if s.Visibility != want {
			t.Errorf("visibility:%s = %v want %v", v, s.Visibility, want)
		}
	}
	// An unrecognised value leaves the current value untouched (matches how
	// every other property in this switch is handled: an invalid declaration
	// is silently ignored rather than resetting to the initial value).
	s := newStyle()
	applyOn(s, "visibility", "hidden", 16)
	applyOn(s, "visibility", "nonsense", 16)
	if s.Visibility != VisibilityHidden {
		t.Errorf("invalid value changed visibility to %v, want it left at VisibilityHidden", s.Visibility)
	}
}

func TestApplyFillAndStroke(t *testing.T) {
	// A concrete colour sets the paint and clears the "none" flag (in case an
	// earlier, lower-precedence declaration had set fill:none).
	s := newStyle()
	s.FillNone = true
	applyOn(s, "fill", "#1e90ff", 16)
	if !s.FillSet || s.FillNone || s.Fill != (Color{0x1e, 0x90, 0xff, 0xff}) {
		t.Errorf("fill:#1e90ff = %+v FillSet=%v FillNone=%v", s.Fill, s.FillSet, s.FillNone)
	}
	// fill:none clears any concrete colour and sets FillNone instead.
	s = newStyle()
	applyOn(s, "fill", "#1e90ff", 16)
	applyOn(s, "fill", "none", 16)
	if !s.FillNone || s.FillSet {
		t.Errorf("fill:none FillSet=%v FillNone=%v, want FillSet=false FillNone=true", s.FillSet, s.FillNone)
	}
	// An unparseable value (e.g. an unresolved var()) is silently ignored,
	// matching every other colour property in this switch.
	s = newStyle()
	applyOn(s, "fill", "not-a-colour", 16)
	if s.FillSet || s.FillNone {
		t.Errorf("fill:not-a-colour should be a no-op, got FillSet=%v FillNone=%v", s.FillSet, s.FillNone)
	}

	// stroke mirrors fill exactly.
	s = newStyle()
	s.StrokeNone = true
	applyOn(s, "stroke", "white", 16)
	if !s.StrokeSet || s.StrokeNone || s.Stroke != (Color{0xff, 0xff, 0xff, 0xff}) {
		t.Errorf("stroke:white = %+v StrokeSet=%v StrokeNone=%v", s.Stroke, s.StrokeSet, s.StrokeNone)
	}
	s = newStyle()
	applyOn(s, "stroke", "white", 16)
	applyOn(s, "stroke", "none", 16)
	if !s.StrokeNone || s.StrokeSet {
		t.Errorf("stroke:none StrokeSet=%v StrokeNone=%v, want StrokeSet=false StrokeNone=true", s.StrokeSet, s.StrokeNone)
	}
	s = newStyle()
	applyOn(s, "stroke", "not-a-colour", 16)
	if s.StrokeSet || s.StrokeNone {
		t.Errorf("stroke:not-a-colour should be a no-op, got StrokeSet=%v StrokeNone=%v", s.StrokeSet, s.StrokeNone)
	}
}

func TestApplyBoxSizing(t *testing.T) {
	s := newStyle()
	applyOn(s, "box-sizing", "border-box", 16)
	if s.BoxSizing != BorderBox {
		t.Error("border-box")
	}
	applyOn(s, "box-sizing", "content-box", 16)
	if s.BoxSizing != ContentBox {
		t.Error("content-box")
	}
}

func TestApplyMinMaxWidth(t *testing.T) {
	s := newStyle()
	applyOn(s, "min-width", "100px", 16)
	if s.MinWidth.Px != 100 {
		t.Errorf("min-width = %v", s.MinWidth)
	}
	applyOn(s, "max-width", "600px", 16)
	if s.MaxWidth.Px != 600 {
		t.Errorf("max-width = %v", s.MaxWidth)
	}
	applyOn(s, "max-width", "none", 16)
	if !s.MaxWidth.Auto {
		t.Error("max-width:none should reset to auto")
	}
	applyOn(s, "min-width", "none", 16)
	if !s.MinWidth.Auto {
		t.Error("min-width:none should reset to auto")
	}
	applyOn(s, "height", "50px", 16)
	if s.Height.Px != 50 {
		t.Errorf("height = %v", s.Height)
	}
}

func TestApplyLineHeight(t *testing.T) {
	s := newStyle()
	applyOn(s, "line-height", "normal", 16)
	if !s.LineHeight.Normal {
		t.Error("normal")
	}
	// A unitless multiplier is kept as a Factor (NOT collapsed to px), so it can
	// inherit as the number and re-multiply by each descendant's own font-size.
	applyOn(s, "line-height", "2", 16)
	if s.LineHeight.Normal || s.LineHeight.Px != 0 || s.LineHeight.Factor != 2 {
		t.Errorf("multiplier = %v", s.LineHeight)
	}
	if px, ok := s.LineHeight.Resolve(16); !ok || px != 32 {
		t.Errorf("factor Resolve(16) = %v,%v want 32,true", px, ok)
	}
	if px, ok := s.LineHeight.Resolve(30); !ok || px != 60 {
		t.Errorf("factor Resolve(30) = %v,%v want 60,true", px, ok)
	}
	applyOn(s, "line-height", "24px", 16)
	if s.LineHeight.Px != 24 {
		t.Errorf("px = %v", s.LineHeight)
	}
	applyOn(s, "line-height", "150%", 16)
	if s.LineHeight.Px != 24 {
		t.Errorf("pct = %v", s.LineHeight)
	}
	applyOn(s, "line-height", "1.5em", 16)
	if s.LineHeight.Px != 24 {
		t.Errorf("em = %v", s.LineHeight)
	}
	// Invalid is ignored (keeps previous).
	prev := s.LineHeight
	applyOn(s, "line-height", "garbage", 16)
	if s.LineHeight != prev {
		t.Errorf("garbage changed line-height to %v", s.LineHeight)
	}
	// Negative is rejected.
	if _, ok := parseLineHeight("-1", 16); ok {
		t.Error("negative multiplier should fail")
	}
}

func TestApplyFloatClear(t *testing.T) {
	s := newStyle()
	for v, want := range map[string]Float{"left": FloatLeft, "right": FloatRight, "none": FloatNone} {
		applyOn(s, "float", v, 16)
		if s.Float != want {
			t.Errorf("float:%s = %v", v, s.Float)
		}
	}
	for v, want := range map[string]Clear{"left": ClearLeft, "right": ClearRight, "both": ClearBoth, "none": ClearNone} {
		applyOn(s, "clear", v, 16)
		if s.Clear != want {
			t.Errorf("clear:%s = %v", v, s.Clear)
		}
	}
}

func TestApplyFlexProperties(t *testing.T) {
	s := newStyle()
	applyOn(s, "flex-direction", "column", 16)
	if s.FlexDirection != FlexColumn {
		t.Error("column")
	}
	applyOn(s, "flex-direction", "row", 16)
	if s.FlexDirection != FlexRow {
		t.Error("row")
	}
	applyOn(s, "justify-content", "space-between", 16)
	if s.JustifyContent != JustifySpaceBetween {
		t.Error("space-between")
	}
	applyOn(s, "align-items", "center", 16)
	if s.AlignItems != AlignCenterItems {
		t.Error("align center")
	}
	applyOn(s, "flex-grow", "2", 16)
	if s.FlexGrow != 2 {
		t.Errorf("grow = %v", s.FlexGrow)
	}
	applyOn(s, "flex-shrink", "0", 16)
	if s.FlexShrink != 0 {
		t.Errorf("shrink = %v", s.FlexShrink)
	}
	applyOn(s, "flex-basis", "120px", 16)
	if s.FlexBasis.Px != 120 {
		t.Errorf("basis = %v", s.FlexBasis)
	}
	applyOn(s, "flex-basis", "content", 16)
	if !s.FlexBasis.Auto {
		t.Error("basis content → auto")
	}
	// Invalid factors are ignored.
	applyOn(s, "flex-grow", "-1", 16)
	if s.FlexGrow != 2 {
		t.Errorf("negative grow changed to %v", s.FlexGrow)
	}
}

func TestJustifyAndAlignParsing(t *testing.T) {
	jcases := map[string]Justify{
		"flex-start": JustifyStart, "start": JustifyStart, "left": JustifyStart, "normal": JustifyStart,
		"flex-end": JustifyEnd, "right": JustifyEnd, "center": JustifyCenter,
		"space-between": JustifySpaceBetween, "space-around": JustifySpaceAround,
		"space-evenly": JustifySpaceEvenly,
	}
	for v, want := range jcases {
		if got, ok := parseJustify(v); !ok || got != want {
			t.Errorf("justify %s = %v %v", v, got, ok)
		}
	}
	if _, ok := parseJustify("bogus"); ok {
		t.Error("bogus justify should fail")
	}
	acases := map[string]AlignItems{
		"stretch": AlignStretch, "normal": AlignStretch, "flex-start": AlignFlexStart,
		"self-start": AlignFlexStart, "flex-end": AlignFlexEnd, "self-end": AlignFlexEnd,
		"center": AlignCenterItems,
	}
	for v, want := range acases {
		if got, ok := parseAlignItems(v); !ok || got != want {
			t.Errorf("align %s = %v %v", v, got, ok)
		}
	}
	if _, ok := parseAlignItems("bogus"); ok {
		t.Error("bogus align should fail")
	}
}

func TestFlexShorthand(t *testing.T) {
	check := func(v string, g, sh float64, basisAuto bool, basisPx float64) {
		s := newStyle()
		applyOn(s, "flex", v, 16)
		if s.FlexGrow != g || s.FlexShrink != sh {
			t.Errorf("flex:%q grow/shrink = %v/%v want %v/%v", v, s.FlexGrow, s.FlexShrink, g, sh)
		}
		if s.FlexBasis.Auto != basisAuto || (!basisAuto && s.FlexBasis.Px != basisPx) {
			t.Errorf("flex:%q basis = %v want auto=%v px=%v", v, s.FlexBasis, basisAuto, basisPx)
		}
	}
	check("none", 0, 0, true, 0)
	check("auto", 1, 1, true, 0)
	check("initial", 0, 1, true, 0)
	check("1", 1, 1, false, 0)           // 1 → 1 1 0
	check("2 3", 2, 3, false, 0)         // grow shrink
	check("1 0 200px", 1, 0, false, 200) // grow shrink basis
	check("0 0 auto", 0, 0, true, 0)
}

func TestBorderShorthandParsing(t *testing.T) {
	s := newStyle()
	applyOn(s, "border", "3px solid red", 16)
	for _, side := range []BorderSide{s.Border.Top, s.Border.Right, s.Border.Bottom, s.Border.Left} {
		if side.Width != 3 || side.Style != BorderSolid || side.Color != (Color{255, 0, 0, 255}) {
			t.Errorf("border side = %+v", side)
		}
		if !side.paints() {
			t.Error("side should paint")
		}
	}
	// Keyword width and defaulted colour (currentColor).
	s2 := newStyle()
	s2.Color = Color{10, 20, 30, 255}
	applyOn(s2, "border", "thick dashed", 16)
	if s2.Border.Top.Width != 5 || s2.Border.Top.Style != BorderSolid {
		t.Errorf("thick dashed = %+v", s2.Border.Top)
	}
	if s2.Border.Top.Color != (Color{10, 20, 30, 255}) {
		t.Errorf("default colour = %+v", s2.Border.Top.Color)
	}
	// Per-side shorthand.
	s3 := newStyle()
	applyOn(s3, "border-left", "2px solid blue", 16)
	if s3.Border.Left.Width != 2 || s3.Border.Left.Color != (Color{0, 0, 255, 255}) {
		t.Errorf("border-left = %+v", s3.Border.Left)
	}
	applyOn(s3, "border-top", "1px dotted green", 16)
	applyOn(s3, "border-right", "4px double gold", 16)
	applyOn(s3, "border-bottom", "medium groove black", 16)
	if s3.Border.Bottom.Width != 3 {
		t.Errorf("medium = %v", s3.Border.Bottom.Width)
	}
}

func TestBorderLonghands(t *testing.T) {
	s := newStyle()
	applyOn(s, "border-width", "1px 2px 3px 4px", 16)
	if s.Border.Top.Width != 1 || s.Border.Right.Width != 2 || s.Border.Bottom.Width != 3 || s.Border.Left.Width != 4 {
		t.Errorf("border-width 4 = %+v", s.Border)
	}
	applyOn(s, "border-width", "thin", 16)
	if s.Border.Top.Width != 1 || s.Border.Left.Width != 1 {
		t.Errorf("border-width keyword = %+v", s.Border)
	}
	applyOn(s, "border-style", "solid", 16)
	if s.Border.Top.Style != BorderSolid {
		t.Error("border-style")
	}
	applyOn(s, "border-color", "red green blue", 16) // top/right/bottom/left = r/g/b/g
	if s.Border.Top.Color != (Color{255, 0, 0, 255}) || s.Border.Bottom.Color != (Color{0, 0, 255, 255}) {
		t.Errorf("border-color 3 = %+v", s.Border)
	}
	applyOn(s, "border-top-width", "6px", 16)
	applyOn(s, "border-right-width", "thick", 16)
	applyOn(s, "border-bottom-width", "7px", 16)
	applyOn(s, "border-left-width", "8px", 16)
	if s.Border.Top.Width != 6 || s.Border.Right.Width != 5 || s.Border.Bottom.Width != 7 || s.Border.Left.Width != 8 {
		t.Errorf("border edge widths = %+v", s.Border)
	}
	// Invalid longhands are ignored.
	applyOn(s, "border-width", "notalength", 16)
	applyOn(s, "border-style", "notastyle", 16)
	applyOn(s, "border-color", "notacolor", 16)
}

func TestBorderWidthsHelper(t *testing.T) {
	b := Borders{
		Top:    BorderSide{Width: 2, Style: BorderSolid, Color: Color{0, 0, 0, 255}},
		Bottom: BorderSide{Width: 4, Style: BorderNone, Color: Color{0, 0, 0, 255}},
	}
	w := b.Widths()
	if w.Top != 2 {
		t.Errorf("top width = %v", w.Top)
	}
	if w.Bottom != 0 {
		t.Errorf("none-style bottom should contribute 0, got %v", w.Bottom)
	}
	if b.Top.paints() != true || b.Bottom.paints() != false {
		t.Error("paints() wrong")
	}
}

func TestMarginAutoParsing(t *testing.T) {
	s := newStyle()
	applyOn(s, "margin", "0 auto", 16)
	if !s.MarginLeftAuto || !s.MarginRightAuto {
		t.Errorf("margin 0 auto: autos = %v %v", s.MarginLeftAuto, s.MarginRightAuto)
	}
	if s.Margin.Top != 0 || s.Margin.Left != 0 {
		t.Errorf("margin 0 auto edges = %+v", s.Margin)
	}
	s2 := newStyle()
	applyOn(s2, "margin-left", "auto", 16)
	if !s2.MarginLeftAuto {
		t.Error("margin-left:auto")
	}
	applyOn(s2, "margin-right", "12px", 16)
	if s2.MarginRightAuto || s2.Margin.Right != 12 {
		t.Errorf("margin-right = %v %v", s2.MarginRightAuto, s2.Margin.Right)
	}
	// 4-value margin with autos.
	s3 := newStyle()
	applyOn(s3, "margin", "1px auto 2px auto", 16)
	if s3.Margin.Top != 1 || s3.Margin.Bottom != 2 || !s3.MarginLeftAuto || !s3.MarginRightAuto {
		t.Errorf("4-val margin = %+v autos %v %v", s3.Margin, s3.MarginLeftAuto, s3.MarginRightAuto)
	}
	// Percentage margins collapse to 0.
	s4 := newStyle()
	applyOn(s4, "margin", "10%", 16)
	if s4.Margin.Top != 0 {
		t.Errorf("percent margin = %v", s4.Margin)
	}
	// Invalid margin token ignored.
	s5 := newStyle()
	applyOn(s5, "margin", "1px bad", 16)
	if s5.Margin.Top != 0 {
		t.Errorf("invalid margin applied: %+v", s5.Margin)
	}
}

func TestFourValuesEmpty(t *testing.T) {
	if _, _, _, _, ok := fourValues([]int{}); ok {
		t.Error("empty should fail")
	}
	if _, _, _, _, ok := fourValues([]int{1, 2, 3, 4, 5}); ok {
		t.Error("five should fail")
	}
}

func TestApplyOverflow(t *testing.T) {
	// Single value sets both axes.
	s := &Style{}
	applyOn(s, "overflow", "hidden", 16)
	if s.OverflowX != OverflowHidden || s.OverflowY != OverflowHidden {
		t.Errorf("overflow:hidden => x=%v y=%v", s.OverflowX, s.OverflowY)
	}
	// Two values are `overflow-x overflow-y`.
	s = &Style{}
	applyOn(s, "overflow", "scroll auto", 16)
	if s.OverflowX != OverflowScroll || s.OverflowY != OverflowAuto {
		t.Errorf("overflow:scroll auto => x=%v y=%v", s.OverflowX, s.OverflowY)
	}
	// Longhands.
	s = &Style{}
	applyOn(s, "overflow-x", "clip", 16)
	applyOn(s, "overflow-y", "visible", 16)
	if s.OverflowX != OverflowClip || s.OverflowY != OverflowVisible {
		t.Errorf("longhands => x=%v y=%v", s.OverflowX, s.OverflowY)
	}
	// An unrecognised keyword leaves the axis unchanged.
	s = &Style{OverflowX: OverflowHidden, OverflowY: OverflowHidden}
	applyOn(s, "overflow", "bogus", 16)
	applyOn(s, "overflow", "clip bogus", 16) // x set, y unchanged
	if s.OverflowX != OverflowClip || s.OverflowY != OverflowHidden {
		t.Errorf("bogus handling => x=%v y=%v", s.OverflowX, s.OverflowY)
	}
	applyOn(s, "overflow-x", "bogus", 16) // unchanged
	applyOn(s, "overflow-y", "bogus", 16)
	if s.OverflowX != OverflowClip || s.OverflowY != OverflowHidden {
		t.Errorf("bogus longhand changed value")
	}
	// Empty overflow value: no fields (len==0) — no change, no panic.
	s = &Style{}
	applyOn(s, "overflow", "", 16)
	if s.OverflowX != OverflowVisible {
		t.Errorf("empty overflow changed value")
	}
	// Clips() helper.
	if OverflowVisible.Clips() || !OverflowHidden.Clips() {
		t.Errorf("Clips() wrong")
	}
}

// TestApplyInset covers a real regression: the `inset` shorthand (and its
// logical-axis siblings inset-inline/inset-block) was entirely unimplemented,
// so top/right/bottom/left all stayed at their initial `auto` — confirmed
// live on tailwindcss.com, where an `inset-0` (`inset:0`), absolutely
// positioned `<img>` meant to fill a `relative` ancestor card instead fell
// back to its static in-flow position, landing at the top of the whole
// document instead of nested inside its card.
func TestApplyInset(t *testing.T) {
	auto := Length{Auto: true}
	zero := Length{Px: 0}

	// 1 value: all four sides.
	s := &Style{Top: auto, Right: auto, Bottom: auto, Left: auto}
	applyOn(s, "inset", "0", 16)
	if s.Top != zero || s.Right != zero || s.Bottom != zero || s.Left != zero {
		t.Errorf("inset:0 => top=%+v right=%+v bottom=%+v left=%+v", s.Top, s.Right, s.Bottom, s.Left)
	}

	// 2 values: vertical horizontal (the same expansion order as margin/padding).
	s = &Style{Top: auto, Right: auto, Bottom: auto, Left: auto}
	applyOn(s, "inset", "10px 20px", 16)
	want := Length{Px: 10}
	if s.Top != want || s.Bottom != want {
		t.Errorf("inset 2-value top/bottom = %+v/%+v, want 10px", s.Top, s.Bottom)
	}
	want = Length{Px: 20}
	if s.Left != want || s.Right != want {
		t.Errorf("inset 2-value left/right = %+v/%+v, want 20px", s.Left, s.Right)
	}

	// 3 values: top, left/right, bottom.
	s = &Style{Top: auto, Right: auto, Bottom: auto, Left: auto}
	applyOn(s, "inset", "1px 2px 3px", 16)
	if s.Top != (Length{Px: 1}) || s.Right != (Length{Px: 2}) || s.Bottom != (Length{Px: 3}) || s.Left != (Length{Px: 2}) {
		t.Errorf("inset 3-value = top=%+v right=%+v bottom=%+v left=%+v", s.Top, s.Right, s.Bottom, s.Left)
	}

	// 4 values: top right bottom left.
	s = &Style{Top: auto, Right: auto, Bottom: auto, Left: auto}
	applyOn(s, "inset", "1px 2px 3px 4px", 16)
	if s.Top != (Length{Px: 1}) || s.Right != (Length{Px: 2}) || s.Bottom != (Length{Px: 3}) || s.Left != (Length{Px: 4}) {
		t.Errorf("inset 4-value = top=%+v right=%+v bottom=%+v left=%+v", s.Top, s.Right, s.Bottom, s.Left)
	}

	// Empty value: zero fields parsed — a no-op, not a panic.
	s = &Style{Top: zero, Right: zero, Bottom: zero, Left: zero}
	applyOn(s, "inset", "", 16)
	if s.Top != zero || s.Right != zero || s.Bottom != zero || s.Left != zero {
		t.Errorf("empty inset should be a no-op, got top=%+v right=%+v bottom=%+v left=%+v", s.Top, s.Right, s.Bottom, s.Left)
	}

	// auto and percentages must be PRESERVED, not collapsed to 0 like
	// margin/padding's parseEdges does — position resolution depends on
	// telling auto/percentage/fixed apart.
	s = &Style{Top: zero, Right: zero, Bottom: zero, Left: zero}
	applyOn(s, "inset", "auto 50%", 16)
	if !s.Top.Auto || !s.Bottom.Auto {
		t.Errorf("inset auto should be preserved as Auto, got top=%+v bottom=%+v", s.Top, s.Bottom)
	}
	if !s.Left.IsPercent || s.Left.Percent != 0.5 {
		t.Errorf("inset 50%% should be preserved as a percentage, got left=%+v", s.Left)
	}

	// inset-inline: logical axis, 1-or-2-value (start[, end]), NOT the 4-side
	// box-edge expansion — a 2-value form assigns start/end independently,
	// unlike inset's "vertical horizontal" pairing.
	s = &Style{Left: auto, Right: auto}
	applyOn(s, "inset-inline", "4px 8px", 16)
	if s.Left != (Length{Px: 4}) || s.Right != (Length{Px: 8}) {
		t.Errorf("inset-inline 4px 8px => left=%+v right=%+v, want 4px/8px", s.Left, s.Right)
	}
	s = &Style{Left: auto, Right: auto}
	applyOn(s, "inset-inline", "6px", 16)
	if s.Left != (Length{Px: 6}) || s.Right != (Length{Px: 6}) {
		t.Errorf("inset-inline 6px (1-value) => left=%+v right=%+v, want both 6px", s.Left, s.Right)
	}

	// inset-block: same shape, top/bottom.
	s = &Style{Top: auto, Bottom: auto}
	applyOn(s, "inset-block", "5px 9px", 16)
	if s.Top != (Length{Px: 5}) || s.Bottom != (Length{Px: 9}) {
		t.Errorf("inset-block 5px 9px => top=%+v bottom=%+v, want 5px/9px", s.Top, s.Bottom)
	}

	// A malformed value (too many fields for the logical shorthands, or an
	// unparseable token) leaves the fields unchanged rather than panicking.
	s = &Style{Top: zero, Bottom: zero}
	applyOn(s, "inset-block", "1px 2px 3px", 16)
	if s.Top != zero || s.Bottom != zero {
		t.Errorf("malformed inset-block should be a no-op, got top=%+v bottom=%+v", s.Top, s.Bottom)
	}
	s = &Style{Top: zero, Right: zero, Bottom: zero, Left: zero}
	applyOn(s, "inset", "bogus", 16)
	if s.Top != zero || s.Right != zero || s.Bottom != zero || s.Left != zero {
		t.Errorf("unparseable inset should be a no-op, got top=%+v right=%+v bottom=%+v left=%+v", s.Top, s.Right, s.Bottom, s.Left)
	}
}

// TestApplyTransformTranslate covers a real regression: `transform` was
// entirely unimplemented, so an off-canvas element hidden via
// `transform: translate(100%)` (confirmed live on pkg.go.dev: its mobile nav
// drawer, `position:fixed` and `display:block` at this engine's 1024px test
// viewport — its media-query-gated `display:none` correctly does not apply
// there, matching a real browser) rendered fully in place instead of pushed
// off-screen, since this engine ignored the translate entirely.
func TestApplyTransformTranslate(t *testing.T) {
	s := &Style{}
	applyOn(s, "transform", "translate(10px, 20px)", 16)
	if s.TranslateX != (Length{Px: 10}) || s.TranslateY != (Length{Px: 20}) {
		t.Errorf("translate(10px,20px) => %+v/%+v", s.TranslateX, s.TranslateY)
	}

	// A single argument only moves the X axis (Y defaults to 0, per spec).
	s = &Style{TranslateY: Length{Px: 99}}
	applyOn(s, "transform", "translate(50%)", 16)
	if !s.TranslateX.IsPercent || s.TranslateX.Percent != 0.5 {
		t.Errorf("translate(50%%) x = %+v, want 50%%", s.TranslateX)
	}
	if s.TranslateY != (Length{}) {
		t.Errorf("translate(50%%) y = %+v, want reset to 0", s.TranslateY)
	}

	// translateX/translateY longhand-style functions.
	s = &Style{}
	applyOn(s, "transform", "translateX(5px)", 16)
	if s.TranslateX != (Length{Px: 5}) || s.TranslateY != (Length{}) {
		t.Errorf("translateX(5px) => x=%+v y=%+v", s.TranslateX, s.TranslateY)
	}
	s = &Style{}
	applyOn(s, "transform", "translateY(7px)", 16)
	if s.TranslateY != (Length{Px: 7}) || s.TranslateX != (Length{}) {
		t.Errorf("translateY(7px) => x=%+v y=%+v", s.TranslateX, s.TranslateY)
	}

	// Multiple translate calls in one value compose additively (translations
	// commute, unlike rotate/scale).
	s = &Style{}
	applyOn(s, "transform", "translateX(3px) translateY(4px)", 16)
	if s.TranslateX != (Length{Px: 3}) || s.TranslateY != (Length{Px: 4}) {
		t.Errorf("composed translateX+translateY => x=%+v y=%+v", s.TranslateX, s.TranslateY)
	}
	s = &Style{}
	applyOn(s, "transform", "translateX(3px) translateX(4px)", 16)
	if s.TranslateX != (Length{Px: 7}) {
		t.Errorf("composed translateX+translateX => x=%+v, want 7px", s.TranslateX)
	}

	// Multiple spaces between two function calls are a well-formed
	// separator, not a malformed token.
	s = &Style{}
	applyOn(s, "transform", "translateX(3px)   translateY(4px)", 16)
	if s.TranslateX != (Length{Px: 3}) || s.TranslateY != (Length{Px: 4}) {
		t.Errorf("multi-space separator => x=%+v y=%+v", s.TranslateX, s.TranslateY)
	}

	// none / empty resets to zero.
	s = &Style{TranslateX: Length{Px: 1}, TranslateY: Length{Px: 2}}
	applyOn(s, "transform", "none", 16)
	if s.TranslateX != (Length{}) || s.TranslateY != (Length{}) {
		t.Errorf("transform:none => x=%+v y=%+v, want both reset", s.TranslateX, s.TranslateY)
	}

	// Any OTHER function, alone or MIXED with translate — including
	// rotate() now that it is its own recognised, but still separately-only,
	// function (see TestApplyTransformRotate below) — is unsupported: this
	// engine has no general transform support, so it must not apply a
	// partial, wrong composition. The fields are left as they were.
	cases := []string{
		"scale(2)",
		"translateX(5px) rotate(10deg)",
		"matrix(1,0,0,1,0,0)",
		"translateZ(5px)",
		"translate(",            // malformed: unterminated
		"bogus(5px)",            // unknown function name
		"5px",                   // not a function call at all
		"translate(bogus)",      // unparseable X
		"translate(5px, bogus)", // valid X, unparseable Y
		"translateX(bogus)",     // unparseable single arg
		"translateY(bogus)",     // unparseable single arg
	}
	for _, c := range cases {
		s = &Style{TranslateX: Length{Px: 42}, TranslateY: Length{Px: 42}}
		applyOn(s, "transform", c, 16)
		if s.TranslateX != (Length{Px: 42}) || s.TranslateY != (Length{Px: 42}) {
			t.Errorf("transform:%q should be a no-op, got x=%+v y=%+v", c, s.TranslateX, s.TranslateY)
		}
	}

	// Empty value: same as none.
	s = &Style{TranslateX: Length{Px: 1}}
	applyOn(s, "transform", "", 16)
	if s.TranslateX != (Length{}) {
		t.Errorf("transform:'' => x=%+v, want reset", s.TranslateX)
	}

	// addLength's other two branches: both-percent sums exactly; a mixed
	// px/percent pair on the same axis (a rare combination this engine's
	// Length type cannot represent as a single value) takes the later call.
	s = &Style{}
	applyOn(s, "transform", "translateX(10%) translateX(20%)", 16)
	if !s.TranslateX.IsPercent || math.Abs(s.TranslateX.Percent-0.3) > 1e-9 {
		t.Errorf("translateX(10%%)+translateX(20%%) => %+v, want 30%%", s.TranslateX)
	}
	s = &Style{}
	applyOn(s, "transform", "translateX(10px) translateX(50%)", 16)
	if !s.TranslateX.IsPercent || s.TranslateX.Percent != 0.5 {
		t.Errorf("translateX(10px)+translateX(50%%) => %+v, want the later call (50%%)", s.TranslateX)
	}
}

// TestApplyTransformRotate covers the round-145 companion to
// TestApplyTransformTranslate: `rotate()`/`rotateZ()`, the second (and
// still separately-only) transform function this engine understands —
// confirmed live on tailwindcss.com's own `rotate-(--angle)` fanned card
// stack and its `-rotate-90` vertical section label.
func TestApplyTransformRotate(t *testing.T) {
	s := &Style{}
	applyOn(s, "transform", "rotate(45deg)", 16)
	if s.RotateDeg != 45 {
		t.Errorf("rotate(45deg) => RotateDeg=%v, want 45", s.RotateDeg)
	}

	// A valid rotate() clears any earlier translate, per CSS's own
	// "a new transform value replaces the whole property" cascade rule —
	// the mirror of TestApplyTransformTranslate's own "none" case above.
	s = &Style{TranslateX: Length{Px: 10}, TranslateY: Length{Px: 20}}
	applyOn(s, "transform", "rotate(90deg)", 16)
	if s.RotateDeg != 90 || s.TranslateX != (Length{}) || s.TranslateY != (Length{}) {
		t.Errorf("rotate(90deg) after a translate => RotateDeg=%v x=%+v y=%+v, want 90/0/0", s.RotateDeg, s.TranslateX, s.TranslateY)
	}

	// rotateZ is the 3D-syntax alias for the same 2D rotation.
	s = &Style{}
	applyOn(s, "transform", "rotateZ(-90deg)", 16)
	if s.RotateDeg != -90 {
		t.Errorf("rotateZ(-90deg) => RotateDeg=%v, want -90", s.RotateDeg)
	}

	// Other angle units, matching parseAngle's own coverage.
	s = &Style{}
	applyOn(s, "transform", "rotate(0.25turn)", 16)
	if s.RotateDeg != 90 {
		t.Errorf("rotate(0.25turn) => RotateDeg=%v, want 90", s.RotateDeg)
	}
	s = &Style{}
	applyOn(s, "transform", "rotate(3.14159265rad)", 16)
	if math.Abs(s.RotateDeg-180) > 1e-4 {
		t.Errorf("rotate(3.14159265rad) => RotateDeg=%v, want ~180", s.RotateDeg)
	}

	// A valid rotate() then a LATER, valid translate() fully replaces it in
	// turn — the same cascade rule, the other direction.
	s = &Style{}
	applyOn(s, "transform", "rotate(45deg)", 16)
	applyOn(s, "transform", "translate(5px)", 16)
	if s.RotateDeg != 0 || s.TranslateX != (Length{Px: 5}) {
		t.Errorf("translate() after rotate() => RotateDeg=%v TranslateX=%+v, want 0/5px", s.RotateDeg, s.TranslateX)
	}

	// none / empty resets rotate to zero, same as translate.
	s = &Style{RotateDeg: 45}
	applyOn(s, "transform", "none", 16)
	if s.RotateDeg != 0 {
		t.Errorf("transform:none => RotateDeg=%v, want reset to 0", s.RotateDeg)
	}

	// An INVALID rotate() (or any other malformed/unrecognised value) must
	// leave a pre-existing valid rotation UNTOUCHED, not reset it — an
	// invalid declaration is dropped by the cascade, the same rule
	// TestApplyTransformTranslate's own no-op cases already cover for
	// translate.
	for _, c := range []string{"rotate(bogus)", "rotate(", "rotate(45deg) rotate(10deg)"} {
		s = &Style{RotateDeg: 42}
		applyOn(s, "transform", c, 16)
		if s.RotateDeg != 42 {
			t.Errorf("transform:%q should be a no-op, got RotateDeg=%v", c, s.RotateDeg)
		}
	}
}

// TestApplyStandaloneRotateProperty covers the ACTUAL confirmed real
// trigger for RotateDeg (round 145): CSS Transforms Level 2's own
// standalone `rotate` property — a bare angle, not a function call — which
// is what modern Tailwind (v4) compiles its `rotate-*` utilities to
// (`rotate:90deg`, `rotate:var(--angle)`, confirmed directly from
// tailwindcss.com's own compiled CSS bundle). Writes the SAME RotateDeg
// field as `transform: rotate()` (TestApplyTransformRotate above).
func TestApplyStandaloneRotateProperty(t *testing.T) {
	s := &Style{}
	applyOn(s, "rotate", "90deg", 16)
	if s.RotateDeg != 90 {
		t.Errorf("rotate:90deg => RotateDeg=%v, want 90", s.RotateDeg)
	}
	applyOn(s, "rotate", "-45deg", 16)
	if s.RotateDeg != -45 {
		t.Errorf("rotate:-45deg => RotateDeg=%v, want -45", s.RotateDeg)
	}
	applyOn(s, "rotate", "0.25turn", 16)
	if s.RotateDeg != 90 {
		t.Errorf("rotate:0.25turn => RotateDeg=%v, want 90", s.RotateDeg)
	}

	// none/initial/unset all reset to 0 — this property is not inherited.
	for _, v := range []string{"none", "initial", "unset"} {
		s = &Style{RotateDeg: 45}
		applyOn(s, "rotate", v, 16)
		if s.RotateDeg != 0 {
			t.Errorf("rotate:%s => RotateDeg=%v, want reset to 0", v, s.RotateDeg)
		}
	}

	// An invalid value leaves a pre-existing value untouched (dropped by
	// the cascade), matching every other property's own convention.
	s = &Style{RotateDeg: 42}
	applyOn(s, "rotate", "bogus", 16)
	if s.RotateDeg != 42 {
		t.Errorf("rotate:bogus (invalid) => RotateDeg=%v, want unchanged 42", s.RotateDeg)
	}

	// Not inherited by default, but the explicit inherit keyword still
	// copies the parent's computed value.
	parent := Style{RotateDeg: 30}
	s = &Style{}
	s.apply(Declaration{Property: "rotate", Value: "inherit"}, 16, &parent)
	if s.RotateDeg != 30 {
		t.Errorf("rotate:inherit => RotateDeg=%v, want parent's 30", s.RotateDeg)
	}
}

// TestApplyAppearanceProperty covers `appearance`/`-webkit-appearance`'s only
// modelled value: `none` (see AppearanceNone's own doc comment for the
// confirmed developer.mozilla.org <mdn-switch> trigger this exists for).
func TestApplyAppearanceProperty(t *testing.T) {
	s := &Style{}
	applyOn(s, "appearance", "none", 16)
	if !s.AppearanceNone {
		t.Errorf("appearance:none => AppearanceNone=%v, want true", s.AppearanceNone)
	}

	// auto/initial/unset all reset to false (the initial value) — this
	// property is not inherited.
	for _, v := range []string{"auto", "initial", "unset"} {
		s = &Style{AppearanceNone: true}
		applyOn(s, "appearance", v, 16)
		if s.AppearanceNone {
			t.Errorf("appearance:%s => AppearanceNone=%v, want reset to false", v, s.AppearanceNone)
		}
	}

	// The vendor-prefixed alias sets the same field.
	s = &Style{}
	applyOn(s, "-webkit-appearance", "none", 16)
	if !s.AppearanceNone {
		t.Errorf("-webkit-appearance:none => AppearanceNone=%v, want true", s.AppearanceNone)
	}

	// Any other CSS Basic UI keyword (textfield, menulist-button, ...) has no
	// confirmed trigger and is left untouched, matching every other
	// property's own "invalid/unsupported value dropped by the cascade"
	// convention rather than being guessed at as either none or auto.
	s = &Style{AppearanceNone: true}
	applyOn(s, "appearance", "menulist-button", 16)
	if !s.AppearanceNone {
		t.Errorf("appearance:menulist-button (unsupported) => AppearanceNone=%v, want unchanged true", s.AppearanceNone)
	}

	// Not inherited by default, but the explicit inherit keyword still
	// copies the parent's computed value.
	parent := Style{AppearanceNone: true}
	s = &Style{}
	s.apply(Declaration{Property: "appearance", Value: "inherit"}, 16, &parent)
	if !s.AppearanceNone {
		t.Errorf("appearance:inherit => AppearanceNone=%v, want parent's true", s.AppearanceNone)
	}
}

// TestDisplayContentsKeyword: `display: contents` parses to DisplayContents
// (the element generates no box of its own — see the Display doc comment).
func TestDisplayContentsKeyword(t *testing.T) {
	var s Style
	s.apply(Declaration{Property: "display", Value: "contents"}, 16, nil)
	if s.Display != DisplayContents {
		t.Errorf("display:contents = %v want DisplayContents", s.Display)
	}
}
