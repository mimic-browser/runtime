// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package layout

import (
	"testing"

	"github.com/go-webengine/engine/css"
)

func TestResolveColumnsCountByWidthAlone(t *testing.T) {
	st := &css.Style{ColumnWidth: css.Length{Px: 100}, ColumnGap: css.Length{Auto: true}, FontSize: 10}
	// gap defaults to FontSize (10). floor((250+10)/(100+10)) = floor(260/110) = 2.
	count, colW, gap := resolveColumns(st, 250)
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}
	if gap != 10 {
		t.Errorf("gap = %v, want FontSize (10), the multicol-specific 'normal' default", gap)
	}
	if want := (250.0 - 1*10) / 2; colW != want {
		t.Errorf("colW = %v, want %v (stretched to evenly fill 250px)", colW, want)
	}
}

func TestResolveColumnsCountFixedAlone(t *testing.T) {
	st := &css.Style{ColumnCount: 3, ColumnWidth: css.Length{Auto: true}, ColumnGap: css.Length{Auto: true}, FontSize: 10}
	count, colW, _ := resolveColumns(st, 320)
	if count != 3 {
		t.Fatalf("count = %d, want 3 (column-count alone, no width constraint)", count)
	}
	if want := (320.0 - 2*10) / 3; colW != want {
		t.Errorf("colW = %v, want %v", colW, want)
	}
}

// TestResolveColumnsBothConstrainsToFit is pkg.go.dev's own real trigger
// shape (`columns:12.5rem 5`): a requested column-count that the container
// is too narrow to fit at the requested minimum column-width shrinks to
// however many actually fit — per the spec's own worked example ("if the
// container is less than 103ems wide ... there will be fewer than 12
// columns").
func TestResolveColumnsBothConstrainsToFit(t *testing.T) {
	st := &css.Style{ColumnCount: 5, ColumnWidth: css.Length{Px: 200}, ColumnGap: css.Length{Auto: true}, FontSize: 10}
	// floor((824+10)/(200+10)) = floor(834/210) = 3, fewer than the requested 5.
	count, colW, _ := resolveColumns(st, 824)
	if count != 3 {
		t.Fatalf("count = %d, want 3 (narrower than 5×200px+gaps)", count)
	}
	if want := (824.0 - 2*10) / 3; colW != want {
		t.Errorf("colW = %v, want %v (stretched to fill, not left at the requested 200px)", colW, want)
	}
}

func TestResolveColumnsBothFitsExactly(t *testing.T) {
	st := &css.Style{ColumnCount: 5, ColumnWidth: css.Length{Px: 100}, ColumnGap: css.Length{Auto: true}, FontSize: 10}
	// floor((1024+10)/(100+10)) = floor(1034/110) = 9 >= 5, so the requested count wins.
	count, _, _ := resolveColumns(st, 1024)
	if count != 5 {
		t.Fatalf("count = %d, want the requested 5 (container wide enough)", count)
	}
}

func TestResolveColumnsExplicitGapOverridesDefault(t *testing.T) {
	st := &css.Style{ColumnCount: 2, ColumnWidth: css.Length{Auto: true}, ColumnGap: css.Length{Px: 40}, FontSize: 10}
	_, colW, gap := resolveColumns(st, 300)
	if gap != 40 {
		t.Errorf("gap = %v, want the explicit 40px, not FontSize's default", gap)
	}
	if want := (300.0 - 40) / 2; colW != want {
		t.Errorf("colW = %v, want %v", colW, want)
	}
}

func TestResolveColumnsNeitherSetIsOneColumn(t *testing.T) {
	st := &css.Style{ColumnWidth: css.Length{Auto: true}, ColumnGap: css.Length{Auto: true}}
	count, colW, _ := resolveColumns(st, 500)
	if count != 1 || colW != 500 {
		t.Errorf("count=%d colW=%v, want a single column spanning the full width", count, colW)
	}
}

// col builds a <div> child holding a single-character text node — with
// fakeMeasurer's 10px-per-rune and 20px line height, each one is a uniform,
// predictable 20px-tall block, so a set of them behaves like the confirmed
// real trigger's own list of short, roughly-equal-height <li> filenames.
func col(text string) string { return `<div>` + text + `</div>` }

// TestMultiColSkipsBareTextChildren confirms the disclosed scope limit on a
// multicol container's own bare text (see layoutMultiCol's own doc comment):
// unlike flex's real anonymous-flex-item handling for the identical shape,
// a text node alongside element children is simply skipped, not laid out as
// an extra column item.
func TestMultiColSkipsBareTextChildren(t *testing.T) {
	src := `<html><body style="margin:0"><div style="width:200px;column-count:2">` +
		`loose text` + col("a") + col("b") + `</div></body></html>`
	div := findBox(layoutHTML(t, src, 1024), "div")
	if len(div.Children) != 2 {
		t.Fatalf("expected 2 element children (bare text skipped), got %d", len(div.Children))
	}
}

// TestResolveColumnsNegativeWidthClampsToZero covers the "requested count
// alone, container far too narrow for it" edge: column-count is not itself
// width-constrained (see the "case st.ColumnCount > 0" branch, no fit
// check), so a small enough container can make the naive even-split
// arithmetic go negative; must clamp to 0, not return a negative width.
func TestResolveColumnsNegativeWidthClampsToZero(t *testing.T) {
	st := &css.Style{ColumnCount: 5, ColumnWidth: css.Length{Auto: true}, ColumnGap: css.Length{Auto: true}, FontSize: 16}
	_, colW, _ := resolveColumns(st, 10) // (10 - 4*16)/5 would be negative
	if colW != 0 {
		t.Errorf("colW = %v, want clamped to 0", colW)
	}
}

// TestColumnCountLaysOutBalancedColumns is the core confirmed real trigger
// (pkg.go.dev's own `.UnitFiles-fileList`): four uniform 20px-tall children
// with column-count:2 split into two perfectly balanced 40px columns side by
// side, not stacked into one 80px single column.
func TestColumnCountLaysOutBalancedColumns(t *testing.T) {
	src := `<html><body style="margin:0"><div style="width:200px;column-count:2">` +
		col("a") + col("b") + col("c") + col("d") + `</div></body></html>`
	div := findBox(layoutHTML(t, src, 1024), "div")
	if len(div.Children) != 4 {
		t.Fatalf("expected 4 children boxes, got %d", len(div.Children))
	}
	// Column 0 (a, b): X at the container's own content origin, stacked 0/20.
	if div.Children[0].X != div.ContentX || div.Children[0].Y != 0 {
		t.Errorf("child 'a' at (%v,%v), want column 0 top (%v,0)", div.Children[0].X, div.Children[0].Y, div.ContentX)
	}
	if div.Children[1].X != div.Children[0].X || div.Children[1].Y != 20 {
		t.Errorf("child 'b' at (%v,%v), want same column as 'a', y=20", div.Children[1].X, div.Children[1].Y)
	}
	// Column 1 (c, d): a real X shift, own stack restarting at y=0.
	colGap := 16.0 // FontSize default (initialStyle's 16), the multicol 'normal' gap
	colW := (200.0 - colGap) / 2
	wantCol1X := div.ContentX + colW + colGap
	if div.Children[2].X != wantCol1X || div.Children[2].Y != 0 {
		t.Errorf("child 'c' at (%v,%v), want column 1 top (%v,0)", div.Children[2].X, div.Children[2].Y, wantCol1X)
	}
	if div.Children[3].X != wantCol1X || div.Children[3].Y != 20 {
		t.Errorf("child 'd' at (%v,%v), want column 1, y=20", div.Children[3].X, div.Children[3].Y)
	}
	// Both columns are exactly 40px tall, so the container's own content
	// height is 40, not 80 (what stacking all four in one column would give).
	if div.ContentH != 40 {
		t.Errorf("div.ContentH = %v, want 40 (two balanced 2-item columns, not one 4-item column)", div.ContentH)
	}
}

func TestColumnWidthAloneDeterminesCount(t *testing.T) {
	// gap defaults to FontSize (16, initialStyle's default). floor((216+16)/(100+16)) = 2.
	src := `<html><body style="margin:0"><div style="width:216px;column-width:100px">` +
		col("a") + col("b") + col("c") + col("d") + `</div></body></html>`
	div := findBox(layoutHTML(t, src, 1024), "div")
	// Two columns of two: same shape as the column-count:2 case above.
	if div.ContentH != 40 {
		t.Errorf("div.ContentH = %v, want 40 (column-width alone resolved to 2 columns)", div.ContentH)
	}
	if div.Children[2].X == div.Children[0].X {
		t.Error("child 'c' has the same X as child 'a' — column-width did not actually split into columns")
	}
}

// TestColumnsShorthandNarrowerThanRequestedCount mirrors pkg.go.dev's own
// exact real trigger shape and confirms the shorthand parses BOTH values
// through to the same width-constrained resolution TestResolveColumns
// BothConstrainsToFit already covers at the resolveColumns level directly.
func TestColumnsShorthandNarrowerThanRequestedCount(t *testing.T) {
	src := `<html><body style="margin:0"><div style="width:100px;columns:100px 5">` +
		col("a") + col("b") + `</div></body></html>`
	div := findBox(layoutHTML(t, src, 1024), "div")
	// gap defaults to 16 (FontSize); floor((100+16)/(100+16)) = 1, far fewer than the requested 5.
	if div.Children[1].X != div.Children[0].X {
		t.Errorf("with only 100px available for a 100px min column, expected ONE column (both children same X), got %v vs %v",
			div.Children[0].X, div.Children[1].X)
	}
}

func TestMultiColSkipsDisplayNoneChildren(t *testing.T) {
	src := `<html><body style="margin:0"><div style="width:200px;column-count:2">` +
		col("a") + `<div style="display:none">b</div>` + col("c") + `</div></body></html>`
	div := findBox(layoutHTML(t, src, 1024), "div")
	if len(div.Children) != 2 {
		t.Fatalf("expected 2 in-flow children (display:none skipped), got %d", len(div.Children))
	}
}

func TestMultiColRoutesOutOfFlowChildAway(t *testing.T) {
	src := `<html><body style="margin:0"><div style="width:200px;column-count:2;position:relative">` +
		col("a") + `<div style="position:absolute;top:0;left:0">b</div>` + col("c") + `</div></body></html>`
	div := findBox(layoutHTML(t, src, 1024), "div")
	if len(div.Children) != 2 {
		t.Fatalf("expected 2 in-flow columned children (absolute child routed to outOfFlow), got %d", len(div.Children))
	}
}

// TestMultiColSingleOverlongChildStaysWhole confirms the deliberate "atomic
// child, no fragmentation" scope decision (see ColumnCount's own doc
// comment): a child much taller than the target column height is never
// split — it stays whole in one column even though that makes the column
// visibly taller than the balanced target, exactly like a too-wide word
// overflowing rather than being invented content.
func TestMultiColSingleOverlongChildStaysWhole(t *testing.T) {
	tall := `<div style="height:100px"></div>`
	src := `<html><body style="margin:0"><div style="width:200px;column-count:2">` +
		tall + col("a") + col("b") + `</div></body></html>`
	div := findBox(layoutHTML(t, src, 1024), "div")
	if len(div.Children) != 3 {
		t.Fatalf("expected 3 children, got %d", len(div.Children))
	}
	// The 100px-tall child alone already exceeds the balanced target, but it
	// must still be the ONLY thing in its own column (never split), and 'a'
	// starts a fresh column rather than being forced to share.
	if div.Children[0].H != 100 {
		t.Fatalf("tall child height = %v, want 100 (unsplit)", div.Children[0].H)
	}
	if div.Children[1].X == div.Children[0].X {
		t.Error("'a' shares a column with the 100px-tall child; want it to start a fresh column instead")
	}
}

func TestMultiColWithNoElementChildrenReturnsCleanly(t *testing.T) {
	src := `<html><body style="margin:0"><div style="width:200px;column-count:2"></div></body></html>`
	div := findBox(layoutHTML(t, src, 1024), "div")
	if len(div.Children) != 0 || div.ContentH != 0 {
		t.Errorf("empty multicol container: children=%d contentH=%v, want both 0", len(div.Children), div.ContentH)
	}
}

// TestColumnCountOneIsOrdinaryBlockStacking confirms resolveColumns' own
// count<=1 case falls all the way back to plain single-column block layout
// (the SAME code path as no columns property at all), not a one-column
// "multicol" box that behaves any differently.
func TestColumnCountOneIsOrdinaryBlockStacking(t *testing.T) {
	src := `<html><body style="margin:0"><div style="width:200px;column-count:1">` +
		col("a") + col("b") + `</div></body></html>`
	div := findBox(layoutHTML(t, src, 1024), "div")
	if len(div.Children) != 2 || div.Children[1].Y != 20 || div.Children[1].X != div.Children[0].X {
		t.Errorf("column-count:1 did not stack ordinarily: children=%v", div.Children)
	}
}
