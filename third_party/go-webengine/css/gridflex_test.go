// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package css

import (
	"reflect"
	"testing"
)

// ---- flex property parsing -------------------------------------------------

func TestApplyFlexWrap(t *testing.T) {
	cases := map[string]FlexWrap{
		"nowrap": FlexNoWrap, "wrap": FlexWrapOn, "wrap-reverse": FlexWrapReverse,
	}
	for v, want := range cases {
		s := newStyle()
		applyOn(s, "flex-wrap", v, 16)
		if s.FlexWrap != want {
			t.Errorf("flex-wrap:%s = %v want %v", v, s.FlexWrap, want)
		}
	}
	// An unknown value leaves the initial value untouched.
	s := newStyle()
	applyOn(s, "flex-wrap", "bogus", 16)
	if s.FlexWrap != FlexNoWrap {
		t.Errorf("flex-wrap:bogus mutated to %v", s.FlexWrap)
	}
}

func TestApplyFlexFlow(t *testing.T) {
	s := newStyle()
	applyOn(s, "flex-flow", "column wrap", 16)
	if s.FlexDirection != FlexColumn || s.FlexWrap != FlexWrapOn {
		t.Errorf("flex-flow column wrap = dir %v wrap %v", s.FlexDirection, s.FlexWrap)
	}
	s2 := newStyle()
	applyOn(s2, "flex-flow", "wrap-reverse row-reverse", 16)
	if s2.FlexDirection != FlexRow || s2.FlexWrap != FlexWrapReverse {
		t.Errorf("flex-flow reverse = dir %v wrap %v", s2.FlexDirection, s2.FlexWrap)
	}
}

func TestApplyAlignContent(t *testing.T) {
	cases := map[string]AlignContent{
		"stretch": AlignContentStretch, "normal": AlignContentStretch,
		"flex-start": AlignContentStart, "start": AlignContentStart,
		"flex-end": AlignContentEnd, "end": AlignContentEnd,
		"center": AlignContentCenter, "space-between": AlignContentSpaceBetween,
		"space-around": AlignContentSpaceAround, "space-evenly": AlignContentSpaceEvenly,
	}
	for v, want := range cases {
		s := newStyle()
		applyOn(s, "align-content", v, 16)
		if s.AlignContent != want {
			t.Errorf("align-content:%s = %v want %v", v, s.AlignContent, want)
		}
	}
	s := newStyle()
	applyOn(s, "align-content", "nope", 16)
	if s.AlignContent != AlignContentStretch {
		t.Errorf("align-content:nope mutated")
	}
}

func TestApplyAlignSelfAndJustifySelf(t *testing.T) {
	cases := map[string]AlignSelf{
		"auto": AlignSelfAuto, "stretch": AlignSelfStretch, "normal": AlignSelfStretch,
		"flex-start": AlignSelfStart, "self-start": AlignSelfStart, "baseline": AlignSelfStart,
		"flex-end": AlignSelfEnd, "self-end": AlignSelfEnd, "center": AlignSelfCenter,
	}
	for v, want := range cases {
		s := newStyle()
		applyOn(s, "align-self", v, 16)
		if s.AlignSelf != want {
			t.Errorf("align-self:%s = %v want %v", v, s.AlignSelf, want)
		}
		s2 := newStyle()
		applyOn(s2, "justify-self", v, 16)
		if s2.JustifySelf != want {
			t.Errorf("justify-self:%s = %v want %v", v, s2.JustifySelf, want)
		}
	}
	s := newStyle()
	applyOn(s, "align-self", "weird", 16)
	if s.AlignSelf != AlignSelfAuto {
		t.Errorf("align-self:weird mutated")
	}
}

func TestAlignSelfResolve(t *testing.T) {
	cases := []struct {
		self      AlignSelf
		container AlignItems
		want      AlignItems
	}{
		{AlignSelfAuto, AlignCenterItems, AlignCenterItems},
		{AlignSelfStretch, AlignFlexEnd, AlignStretch},
		{AlignSelfStart, AlignStretch, AlignFlexStart},
		{AlignSelfEnd, AlignStretch, AlignFlexEnd},
		{AlignSelfCenter, AlignStretch, AlignCenterItems},
	}
	for _, c := range cases {
		if got := c.self.Resolve(c.container); got != c.want {
			t.Errorf("%v.Resolve(%v) = %v want %v", c.self, c.container, got, c.want)
		}
	}
}

func TestApplyJustifyItems(t *testing.T) {
	s := newStyle()
	applyOn(s, "justify-items", "center", 16)
	if s.JustifyItems != AlignCenterItems {
		t.Errorf("justify-items:center = %v", s.JustifyItems)
	}
}

func TestApplyOrder(t *testing.T) {
	s := newStyle()
	applyOn(s, "order", "-3", 16)
	if s.Order != -3 {
		t.Errorf("order = %d", s.Order)
	}
	applyOn(s, "order", "x", 16) // invalid: unchanged
	if s.Order != -3 {
		t.Errorf("order after bad = %d", s.Order)
	}
}

func TestApplyMinMaxHeight(t *testing.T) {
	s := newStyle()
	applyOn(s, "min-height", "40px", 16)
	applyOn(s, "max-height", "200px", 16)
	if s.MinHeight.Px != 40 || s.MaxHeight.Px != 200 {
		t.Errorf("min/max-height = %v %v", s.MinHeight, s.MaxHeight)
	}
	applyOn(s, "max-height", "none", 16)
	if !s.MaxHeight.Auto {
		t.Errorf("max-height:none not auto")
	}
	applyOn(s, "min-height", "none", 16)
	if !s.MinHeight.Auto {
		t.Errorf("min-height:none not auto")
	}
}

// ---- gap -------------------------------------------------------------------

func TestApplyGapShorthand(t *testing.T) {
	s := newStyle()
	applyOn(s, "gap", "10px", 16)
	if s.RowGap.Px != 10 || s.ColumnGap.Px != 10 {
		t.Errorf("gap:10px = row %v col %v", s.RowGap, s.ColumnGap)
	}
	applyOn(s, "gap", "5px 25px", 16)
	if s.RowGap.Px != 5 || s.ColumnGap.Px != 25 {
		t.Errorf("gap:5 25 = row %v col %v", s.RowGap, s.ColumnGap)
	}
	// Invalid forms leave the previous value.
	applyOn(s, "gap", "auto", 16)
	if s.RowGap.Px != 5 {
		t.Errorf("gap:auto mutated row to %v", s.RowGap)
	}
	applyOn(s, "gap", "5px bad", 16)
	if s.ColumnGap.Px != 25 {
		t.Errorf("gap with bad token mutated col to %v", s.ColumnGap)
	}
}

func TestApplyRowColumnGap(t *testing.T) {
	s := newStyle()
	applyOn(s, "row-gap", "12px", 16)
	applyOn(s, "column-gap", "8px", 16)
	if s.RowGap.Px != 12 || s.ColumnGap.Px != 8 {
		t.Errorf("row/column-gap = %v %v", s.RowGap, s.ColumnGap)
	}
	applyOn(s, "grid-gap", "3px", 16) // alias sets both
	if s.RowGap.Px != 3 || s.ColumnGap.Px != 3 {
		t.Errorf("grid-gap alias = %v %v", s.RowGap, s.ColumnGap)
	}
}

// ---- place-* shorthands ----------------------------------------------------

func TestApplyPlaceItems(t *testing.T) {
	s := newStyle()
	applyOn(s, "place-items", "center start", 16)
	if s.AlignItems != AlignCenterItems || s.JustifyItems != AlignFlexStart {
		t.Errorf("place-items = align %v justify %v", s.AlignItems, s.JustifyItems)
	}
	s2 := newStyle()
	applyOn(s2, "place-items", "end", 16) // single value → both axes
	if s2.AlignItems != AlignFlexEnd || s2.JustifyItems != AlignFlexEnd {
		t.Errorf("place-items single = align %v justify %v", s2.AlignItems, s2.JustifyItems)
	}
	applyOn(newStyle(), "place-items", "", 16) // empty: no panic
}

func TestApplyPlaceContent(t *testing.T) {
	s := newStyle()
	applyOn(s, "place-content", "space-between center", 16)
	if s.AlignContent != AlignContentSpaceBetween || s.JustifyContent != JustifyCenter {
		t.Errorf("place-content = %v %v", s.AlignContent, s.JustifyContent)
	}
	s2 := newStyle()
	applyOn(s2, "place-content", "center", 16)
	if s2.AlignContent != AlignContentCenter || s2.JustifyContent != JustifyCenter {
		t.Errorf("place-content single = %v %v", s2.AlignContent, s2.JustifyContent)
	}
	applyOn(newStyle(), "place-content", "", 16)
}

func TestApplyPlaceSelf(t *testing.T) {
	s := newStyle()
	applyOn(s, "place-self", "end center", 16)
	if s.AlignSelf != AlignSelfEnd || s.JustifySelf != AlignSelfCenter {
		t.Errorf("place-self = %v %v", s.AlignSelf, s.JustifySelf)
	}
	s2 := newStyle()
	applyOn(s2, "place-self", "start", 16)
	if s2.AlignSelf != AlignSelfStart || s2.JustifySelf != AlignSelfStart {
		t.Errorf("place-self single = %v %v", s2.AlignSelf, s2.JustifySelf)
	}
	applyOn(newStyle(), "place-self", "", 16)
}

// ---- grid track list parsing ----------------------------------------------

func TestParseTrackListBasics(t *testing.T) {
	got, ok := parseTrackList("100px 1fr auto 20%", 16)
	if !ok || len(got) != 4 {
		t.Fatalf("parse = %v ok %v", got, ok)
	}
	if got[0].Kind != TrackPx || got[0].Px != 100 {
		t.Errorf("track0 = %+v", got[0])
	}
	if got[1].Kind != TrackFr || got[1].Fr != 1 {
		t.Errorf("track1 = %+v", got[1])
	}
	if got[2].Kind != TrackAuto {
		t.Errorf("track2 = %+v", got[2])
	}
	if got[3].Kind != TrackPercent || got[3].Percent != 0.2 {
		t.Errorf("track3 = %+v", got[3])
	}
}

func TestParseTrackListRepeat(t *testing.T) {
	got, ok := parseTrackList("repeat(3, 1fr)", 16)
	if !ok || len(got) != 3 {
		t.Fatalf("repeat = %v ok %v", got, ok)
	}
	for i, ts := range got {
		if ts.Kind != TrackFr || ts.Fr != 1 {
			t.Errorf("repeat[%d] = %+v", i, ts)
		}
	}
	// Nested repeat body with mixed tracks.
	got2, ok := parseTrackList("repeat(2, 100px 1fr) 50px", 16)
	if !ok || len(got2) != 5 {
		t.Fatalf("repeat2 = %v ok %v", got2, ok)
	}
}

func TestParseTrackListMinmax(t *testing.T) {
	got, ok := parseTrackList("minmax(100px, 1fr)", 16)
	if !ok || len(got) != 1 || got[0].Kind != TrackMinMax {
		t.Fatalf("minmax = %v ok %v", got, ok)
	}
	if got[0].Min.Kind != TrackPx || got[0].Min.Px != 100 {
		t.Errorf("minmax min = %+v", got[0].Min)
	}
	if got[0].Max.Kind != TrackFr || got[0].Max.Fr != 1 {
		t.Errorf("minmax max = %+v", got[0].Max)
	}
}

func TestParseTrackListLineNamesDropped(t *testing.T) {
	got, ok := parseTrackList("[full-start] 1fr [main-start] 3fr [full-end]", 16)
	if !ok || len(got) != 2 {
		t.Fatalf("line-name list = %v ok %v", got, ok)
	}
	// A list of only line names yields no tracks and is rejected.
	if got, ok := parseTrackList("[a] [b]", 16); ok {
		t.Errorf("all-line-name list unexpectedly ok = %v", got)
	}
}

func TestParseTrackListRejects(t *testing.T) {
	for _, v := range []string{
		"none", "", "repeat(auto-fill, 100px)", "repeat(0, 1fr)",
		"repeat(2)", "minmax(1px)", "-5fr", "garbage",
		"minmax(1fr, bad)", "repeat(2, bad)",
	} {
		if got, ok := parseTrackList(v, 16); ok {
			t.Errorf("parseTrackList(%q) unexpectedly ok = %v", v, got)
		}
	}
}

// ---- grid line + placement parsing ----------------------------------------

func TestParseGridLine(t *testing.T) {
	cases := map[string]GridLine{
		"auto":    {Auto: true},
		"":        {Auto: true},
		"3":       {N: 3},
		"-1":      {N: -1},
		"span 2":  {Span: true, N: 2},
		"span":    {Span: true, N: 1},
		"span -1": {Span: true, N: 1}, // invalid span count → 1
		"header":  {Auto: true},       // named line unsupported
	}
	for v, want := range cases {
		if got := parseGridLine(v); got != want {
			t.Errorf("parseGridLine(%q) = %+v want %+v", v, got, want)
		}
	}
}

func TestParseGridPlacement(t *testing.T) {
	s, e := parseGridPlacement("1 / 3")
	if s != (GridLine{N: 1}) || e != (GridLine{N: 3}) {
		t.Errorf("placement 1/3 = %+v %+v", s, e)
	}
	s, e = parseGridPlacement("span 2")
	if s != (GridLine{Span: true, N: 2}) || !e.Auto {
		t.Errorf("placement span 2 = %+v %+v", s, e)
	}
}

func TestApplyGridColumnRow(t *testing.T) {
	s := newStyle()
	applyOn(s, "grid-column", "2 / span 3", 16)
	if s.GridColumnStart != (GridLine{N: 2}) || s.GridColumnEnd != (GridLine{Span: true, N: 3}) {
		t.Errorf("grid-column = %+v %+v", s.GridColumnStart, s.GridColumnEnd)
	}
	applyOn(s, "grid-row", "1 / 4", 16)
	if s.GridRowStart != (GridLine{N: 1}) || s.GridRowEnd != (GridLine{N: 4}) {
		t.Errorf("grid-row = %+v %+v", s.GridRowStart, s.GridRowEnd)
	}
	applyOn(s, "grid-row-start", "2", 16)
	applyOn(s, "grid-row-end", "5", 16)
	if s.GridRowStart.N != 2 || s.GridRowEnd.N != 5 {
		t.Errorf("grid-row-start/end = %+v %+v", s.GridRowStart, s.GridRowEnd)
	}
	applyOn(s, "grid-column-start", "3", 16)
	applyOn(s, "grid-column-end", "span 1", 16)
	if s.GridColumnStart.N != 3 || !s.GridColumnEnd.Span {
		t.Errorf("grid-column-start/end = %+v %+v", s.GridColumnStart, s.GridColumnEnd)
	}
}

func TestApplyGridArea(t *testing.T) {
	s := newStyle()
	applyOn(s, "grid-area", "sidebar", 16)
	if s.GridArea != "sidebar" {
		t.Errorf("grid-area name = %q", s.GridArea)
	}
	s2 := newStyle()
	applyOn(s2, "grid-area", "1 / 2 / 3 / 4", 16)
	if s2.GridRowStart.N != 1 || s2.GridColumnStart.N != 2 || s2.GridRowEnd.N != 3 || s2.GridColumnEnd.N != 4 {
		t.Errorf("grid-area lines = %+v", s2)
	}
	s3 := newStyle()
	applyOn(s3, "grid-area", "1 / 2", 16) // partial: missing ends → auto
	if s3.GridRowStart.N != 1 || s3.GridColumnStart.N != 2 || !s3.GridRowEnd.Auto {
		t.Errorf("grid-area partial = %+v", s3)
	}
	applyOn(newStyle(), "grid-area", "auto", 16) // bare auto: no name
}

// ---- grid-template-areas ---------------------------------------------------

func TestParseGridTemplateAreas(t *testing.T) {
	got, ok := parseGridTemplateAreas(`"h h h" "s c c" "f f f"`)
	if !ok {
		t.Fatal("areas parse failed")
	}
	want := [][]string{{"h", "h", "h"}, {"s", "c", "c"}, {"f", "f", "f"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("areas = %v", got)
	}
	// Dot is an empty cell; single quotes accepted.
	got2, ok := parseGridTemplateAreas(`'a .' '. b'`)
	if !ok || got2[0][1] != "" || got2[1][0] != "" {
		t.Errorf("areas dots = %v ok %v", got2, ok)
	}
	// Trailing non-quote text after the last row is ignored.
	got3, ok := parseGridTemplateAreas(`"a b"  trailing`)
	if !ok || len(got3) != 1 {
		t.Errorf("areas trailing = %v ok %v", got3, ok)
	}
}

func TestParseGridTemplateAreasRejects(t *testing.T) {
	for _, v := range []string{
		``,              // no rows
		`"a b" "c"`,     // ragged
		`"unterminated`, // missing close quote
		`"" ""`,         // zero columns
	} {
		if got, ok := parseGridTemplateAreas(v); ok {
			t.Errorf("areas(%q) unexpectedly ok = %v", v, got)
		}
	}
}

func TestGridTemplateShorthand(t *testing.T) {
	rows, columns, areas, ok := parseGridTemplate(`min-content 1fr / 12.25rem minmax(0,1fr)`, 16)
	if !ok || len(rows) != 2 || len(columns) != 2 || len(areas) != 0 {
		t.Fatalf("rows=%v columns=%v areas=%v ok=%v", rows, columns, areas, ok)
	}
	rows, columns, areas, ok = parseGridTemplate(`'title .' min-content 'content side' 1fr / minmax(0,59.25rem) min-content`, 16)
	if !ok || len(rows) != 2 || len(columns) != 2 || len(areas) != 2 || areas[1][1] != "side" {
		t.Fatalf("named rows=%v columns=%v areas=%v ok=%v", rows, columns, areas, ok)
	}
}

func TestApplyGridTemplates(t *testing.T) {
	s := newStyle()
	applyOn(s, "grid-template-columns", "1fr 2fr", 16)
	applyOn(s, "grid-template-rows", "50px auto", 16)
	applyOn(s, "grid-auto-rows", "30px", 16)
	applyOn(s, "grid-auto-columns", "minmax(10px, 1fr)", 16)
	applyOn(s, "grid-auto-flow", "column dense", 16)
	applyOn(s, "grid-template-areas", `"a a"`, 16)
	if len(s.GridTemplateColumns) != 2 || len(s.GridTemplateRows) != 2 {
		t.Errorf("templates = %v %v", s.GridTemplateColumns, s.GridTemplateRows)
	}
	if s.GridAutoRows.Px != 30 || s.GridAutoColumns.Kind != TrackMinMax {
		t.Errorf("auto tracks = %+v %+v", s.GridAutoRows, s.GridAutoColumns)
	}
	if s.GridAutoFlow != GridFlowColumn {
		t.Errorf("auto-flow = %v", s.GridAutoFlow)
	}
	if !s.GridAutoFlowDense {
		t.Error("auto-flow dense keyword not recognised")
	}
	if len(s.GridTemplateAreas) != 1 {
		t.Errorf("areas = %v", s.GridTemplateAreas)
	}
	// grid-auto-flow row keyword (no "dense") clears the dense flag again.
	applyOn(s, "grid-auto-flow", "row", 16)
	if s.GridAutoFlow != GridFlowRow {
		t.Errorf("auto-flow row = %v", s.GridAutoFlow)
	}
	if s.GridAutoFlowDense {
		t.Error("auto-flow dense should have cleared without the keyword")
	}
	// Invalid template list leaves the previous value.
	applyOn(s, "grid-template-columns", "repeat(auto-fill, 1fr)", 16)
	if len(s.GridTemplateColumns) != 2 {
		t.Errorf("invalid template mutated columns to %v", s.GridTemplateColumns)
	}
}

func TestSplitTopLevelComma(t *testing.T) {
	got := splitTopLevelComma("minmax(1px, 2px), auto, repeat(2, 1fr)")
	want := []string{"minmax(1px, 2px)", "auto", "repeat(2, 1fr)"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("splitTopLevelComma = %v", got)
	}
}

func TestDisplayGridCascade(t *testing.T) {
	// display:grid resolves to the grid box through the normal apply path.
	s := newStyle()
	applyOn(s, "display", "inline-grid", 16)
	if s.Display != DisplayGrid {
		t.Errorf("inline-grid = %v", s.Display)
	}
}
