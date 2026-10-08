// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package layout

import (
	"testing"

	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/dom"
)

// childIndex returns the index of the box with the given id within b's direct
// children, or -1.
func childIndex(b *Box, id string) int {
	for i, c := range b.Children {
		if c.Node != nil && c.Node.ID() == id {
			return i
		}
	}
	return -1
}

// TestRelativeOffsetPaintsShiftedReservesSpace verifies a relative box paints at
// its original in-flow position plus its top/left offset while still reserving
// its normal-flow space (the following sibling is not pulled up).
func TestRelativeOffsetPaintsShiftedReservesSpace(t *testing.T) {
	src := `<html><body style="margin:0;padding:0">` +
		`<div id="a" style="width:100px;height:50px"></div>` +
		`<div id="b" style="position:relative;top:10px;left:20px;width:100px;height:50px"></div>` +
		`<div id="c" style="width:100px;height:50px"></div>` +
		`</body></html>`
	root := layoutHTML(t, src, 300)
	a, b, c := findBoxByID(root, "a"), findBoxByID(root, "b"), findBoxByID(root, "c")

	assertF(t, "a.Y", a.Y, 0)
	// b is in flow at y=50, shifted by (left:20, top:10).
	assertF(t, "b.X", b.X, 20)
	assertF(t, "b.Y", b.Y, 60)
	// c sits at y=100: b reserved its 50px of flow space (was not pulled up).
	assertF(t, "c.Y", c.Y, 100)
	if b.Position != css.PositionRelative {
		t.Errorf("b.Position = %v", b.Position)
	}
}

// TestRelativeRightBottomAndPercent covers the right/bottom offset arms and a
// percentage offset resolved against the containing block.
func TestRelativeRightBottomAndPercent(t *testing.T) {
	src := `<html><body style="margin:0;padding:0">` +
		`<div id="a" style="width:40px;height:40px"></div>` +
		`<div id="b" style="position:relative;right:10px;bottom:5px;width:40px;height:40px"></div>` +
		`<div id="p" style="position:sticky;left:50%;width:20px;height:10px"></div>` +
		`</body></html>`
	root := layoutHTML(t, src, 200)
	b, p := findBoxByID(root, "b"), findBoxByID(root, "p")
	// b was at (0,40); right:10 => -10 in x, bottom:5 => -5 in y.
	assertF(t, "b.X", b.X, -10)
	assertF(t, "b.Y", b.Y, 35)
	// sticky behaves like relative; left:50% of the 200px containing block = 100.
	assertF(t, "p.X", p.X, 100)
}

// TestAbsoluteAgainstPositionedAncestorPaddingBox places an absolute box against
// the padding box of its nearest positioned ancestor, and confirms it reserves
// no space (there are no in-flow children, height is fixed).
func TestAbsoluteAgainstPositionedAncestorPaddingBox(t *testing.T) {
	src := `<html><body style="margin:0;padding:0">` +
		`<div id="rel" style="position:relative;padding:5px;border:2px solid #000000;width:200px;height:100px">` +
		`<div id="abs" style="position:absolute;top:10px;left:15px;width:40px;height:20px"></div>` +
		`</div></body></html>`
	root := layoutHTML(t, src, 400)
	rel, abs := findBoxByID(root, "rel"), findBoxByID(root, "abs")

	// rel border box: 2+5+200+5+2 = 214 wide, 214? height 2+5+100+5+2 = 114.
	assertF(t, "rel.W", rel.W, 214)
	assertF(t, "rel.H", rel.H, 114)
	// Padding box origin is (2,2); abs offset (15,10) from there.
	assertF(t, "abs.X", abs.X, 17)
	assertF(t, "abs.Y", abs.Y, 12)
	assertF(t, "abs.W", abs.W, 40)
	assertF(t, "abs.H", abs.H, 20)
	if abs.Position != css.PositionAbsolute {
		t.Errorf("abs.Position = %v", abs.Position)
	}
}

// TestTransformTranslateRelative covers a real regression: `transform` was
// entirely unimplemented, so an off-canvas element hidden via
// `transform: translate(...)` rendered fully in place. This covers the
// simplest case: a plain in-flow (here also relative, to prove the two
// shifts COMPOSE rather than one overwriting the other) box.
func TestTransformTranslateRelative(t *testing.T) {
	src := `<html><body style="margin:0;padding:0">` +
		`<div id="box" style="position:relative;top:10px;left:5px;transform:translate(3px,4px);width:40px;height:20px"></div>` +
		`</body></html>`
	root := layoutHTML(t, src, 400)
	box := findBoxByID(root, "box")
	// Static position (0,0) + relative offset (5,10) + transform (3,4).
	assertF(t, "box.X", box.X, 8)
	assertF(t, "box.Y", box.Y, 14)
}

// TestTransformTranslatePercentAgainstOwnBox covers that a translate()
// percentage resolves against the box's OWN border-box size, per the CSS
// Transforms spec's default reference box — a DIFFERENT rule from a position
// offset's percentage, which resolves against the containing block.
func TestTransformTranslatePercentAgainstOwnBox(t *testing.T) {
	src := `<html><body style="margin:0;padding:0">` +
		`<div id="box" style="width:40px;height:20px;transform:translate(50%,50%)"></div>` +
		`</body></html>`
	root := layoutHTML(t, src, 400)
	box := findBoxByID(root, "box")
	assertF(t, "box.X", box.X, 20) // 50% of the box's OWN 40px width
	assertF(t, "box.Y", box.Y, 10) // 50% of the box's OWN 20px height
}

// TestApplyTransformTranslateNilStyle covers the one box this engine ever
// produces with no Style at all (layout.go's empty-document fallback,
// &Box{}) — applyTransformTranslate must skip it rather than panic.
func TestApplyTransformTranslateNilStyle(t *testing.T) {
	box := &Box{}
	applyTransformTranslate(box) // must not panic
	assertF(t, "box.X", box.X, 0)
}

// TestTransformTranslateOnAbsoluteBoxNotCancelled is the exact live
// regression this engine's own settle/placement math could have silently
// re-introduced: an absolutely-positioned box's transform must compose ON
// TOP of its resolved (inset-based) position, not be applied before that
// position is computed and so get exactly cancelled by it. Confirmed live
// on pkg.go.dev: a `position:fixed` off-canvas nav drawer hides itself with
// `transform:translate(100%)` — computing the transform too early here
// silently erased it, rendering the drawer fully in place.
func TestTransformTranslateOnAbsoluteBoxNotCancelled(t *testing.T) {
	src := `<html><body style="margin:0;padding:0">` +
		`<div id="rel" style="position:relative;width:200px;height:100px">` +
		`<div id="abs" style="position:absolute;top:10px;left:15px;width:40px;height:20px;transform:translate(100%,0)"></div>` +
		`</div></body></html>`
	root := layoutHTML(t, src, 400)
	abs := findBoxByID(root, "abs")
	// Resolved position (15,10) relative to rel's padding box, PLUS a
	// translate(100%) of the box's own 40px width — not (15,10) alone, which
	// is what an "applied too early, then overwritten" bug would produce.
	assertF(t, "abs.X", abs.X, 55)
	assertF(t, "abs.Y", abs.Y, 10)
}

// TestAbsoluteSkipsStaticAncestorToPositionedOne covers the walk that steps past
// a static ancestor to the nearest positioned one.
func TestAbsoluteSkipsStaticAncestorToPositionedOne(t *testing.T) {
	src := `<html><body style="margin:0;padding:0">` +
		`<div id="rel" style="position:relative;width:200px;height:100px">` +
		`<div id="mid" style="width:200px;height:100px">` +
		`<div id="abs" style="position:absolute;top:5px;left:5px;width:10px;height:10px"></div>` +
		`</div></div></body></html>`
	root := layoutHTML(t, src, 400)
	rel, abs := findBoxByID(root, "rel"), findBoxByID(root, "abs")
	// rel padding box origin is (0,0); abs offsets from there, not from mid.
	assertF(t, "rel.X", rel.X, 0)
	assertF(t, "abs.X", abs.X, 5)
	assertF(t, "abs.Y", abs.Y, 5)
}

// TestAbsoluteAgainstICBReservesNoSpace places an absolute box with no
// positioned ancestor against the initial containing block, and confirms the
// following in-flow paragraph is not pushed down.
func TestAbsoluteAgainstICBReservesNoSpace(t *testing.T) {
	src := `<html><body style="margin:0;padding:0">` +
		`<div id="abs" style="position:absolute;top:25px;left:35px;width:40px;height:20px"></div>` +
		`<p id="p" style="margin:0">hi</p></body></html>`
	root := layoutHTML(t, src, 300)
	abs, p := findBoxByID(root, "abs"), findBoxByID(root, "p")
	assertF(t, "abs.X", abs.X, 35)
	assertF(t, "abs.Y", abs.Y, 25)
	// The paragraph starts at the very top; the absolute box reserved no space.
	assertF(t, "p.Y", p.Y, 0)
}

// TestAbsoluteRightAnchored covers the right-offset x arm and the top arm.
func TestAbsoluteRightAnchored(t *testing.T) {
	src := `<html><body style="margin:0;padding:0">` +
		`<div id="abs" style="position:absolute;top:5px;right:10px;width:40px;height:20px"></div>` +
		`</body></html>`
	root := layoutHTML(t, src, 300)
	abs := findBoxByID(root, "abs")
	// x = icb.w - right - outerW = 300 - 10 - 40 = 250.
	assertF(t, "abs.X", abs.X, 250)
	assertF(t, "abs.Y", abs.Y, 5)
}

// TestAbsoluteBottomAnchoredAgainstDocumentHeight covers the bottom arm and the
// left arm, resolving bottom against the in-flow document height (the ICB height
// approximation).
func TestAbsoluteBottomAnchoredAgainstDocumentHeight(t *testing.T) {
	src := `<html><body style="margin:0;padding:0">` +
		`<div id="spacer" style="height:200px"></div>` +
		`<div id="abs" style="position:absolute;left:0;bottom:20px;width:40px;height:30px"></div>` +
		`</body></html>`
	root := layoutHTML(t, src, 300)
	abs := findBoxByID(root, "abs")
	// icb.h = 200 (spacer). y = 200 - 20 - 30 = 150.
	assertF(t, "abs.X", abs.X, 0)
	assertF(t, "abs.Y", abs.Y, 150)
}

// TestAbsoluteWidthFromLeftRightPair covers width resolution from a left+right
// pair (auto width).
func TestAbsoluteWidthFromLeftRightPair(t *testing.T) {
	src := `<html><body style="margin:0;padding:0">` +
		`<div id="abs" style="position:absolute;left:10px;right:10px;height:20px"></div>` +
		`</body></html>`
	root := layoutHTML(t, src, 300)
	abs := findBoxByID(root, "abs")
	assertF(t, "abs.X", abs.X, 10)
	assertF(t, "abs.W", abs.W, 280) // 300 - 10 - 10
}

// TestAbsoluteBorderBoxWidth covers the border-box width path.
func TestAbsoluteBorderBoxWidth(t *testing.T) {
	src := `<html><body style="margin:0;padding:0">` +
		`<div id="abs" style="position:absolute;box-sizing:border-box;width:60px;padding:5px;height:20px"></div>` +
		`</body></html>`
	root := layoutHTML(t, src, 300)
	abs := findBoxByID(root, "abs")
	// border-box width 60 => content 50, box back to 5+50+5 = 60.
	assertF(t, "abs.W", abs.W, 60)
}

// TestAbsoluteShrinkToFit covers the shrink-to-fit width branch (auto width, no
// left/right pair) and the static-position x/y fallback.
func TestAbsoluteShrinkToFit(t *testing.T) {
	src := `<html><body style="margin:0;padding:0">` +
		`<div id="abs" style="position:absolute">hi</div></body></html>`
	root := layoutHTML(t, src, 300)
	abs := findBoxByID(root, "abs")
	// "hi" is 2 runes * 10px = 20; static position falls back to the CB origin.
	assertF(t, "abs.W", abs.W, 20)
	assertF(t, "abs.X", abs.X, 0)
	assertF(t, "abs.Y", abs.Y, 0)
}

// TestAbsoluteShrinkToFitWithOffsets covers the avail adjustments for a single
// left or right offset in the shrink-to-fit path.
func TestAbsoluteShrinkToFitWithOffsets(t *testing.T) {
	left := `<html><body style="margin:0;padding:0">` +
		`<div id="abs" style="position:absolute;left:10px">hi</div></body></html>`
	r1 := layoutHTML(t, left, 300)
	assertF(t, "left abs.X", findBoxByID(r1, "abs").X, 10)
	assertF(t, "left abs.W", findBoxByID(r1, "abs").W, 20)

	right := `<html><body style="margin:0;padding:0">` +
		`<div id="abs" style="position:absolute;right:10px">hi</div></body></html>`
	r2 := layoutHTML(t, right, 300)
	// right anchored: x = 300 - 10 - 20 = 270.
	assertF(t, "right abs.X", findBoxByID(r2, "abs").X, 270)
}

// TestAbsoluteNegativeWidthClampsToZero covers the contentW<0 guard.
func TestAbsoluteNegativeWidthClampsToZero(t *testing.T) {
	src := `<html><body style="margin:0;padding:0">` +
		`<div id="abs" style="position:absolute;left:200px;right:200px;height:10px"></div>` +
		`</body></html>`
	root := layoutHTML(t, src, 300)
	assertF(t, "abs.W", findBoxByID(root, "abs").W, 0)
}

// TestFixedResolvesToICBIgnoringPositionedAncestor confirms fixed uses the ICB
// (document origin) even inside a shifted relative ancestor, and reserves no
// space nor grows the page.
func TestFixedResolvesToICBIgnoringPositionedAncestor(t *testing.T) {
	src := `<html><body style="margin:0;padding:0">` +
		`<div id="rel" style="position:relative;top:100px;left:100px;width:200px;height:50px">` +
		`<div id="fx" style="position:fixed;top:5px;left:5px;width:20px;height:10px"></div>` +
		`</div>` +
		`<p id="p" style="margin:0">hello</p></body></html>`
	root := layoutHTML(t, src, 300)
	fx, p := findBoxByID(root, "fx"), findBoxByID(root, "p")
	// Fixed against the ICB at document origin, not the shifted rel padding box.
	assertF(t, "fx.X", fx.X, 5)
	assertF(t, "fx.Y", fx.Y, 5)
	if fx.Position != css.PositionFixed {
		t.Errorf("fx.Position = %v", fx.Position)
	}
	// rel reserves its own 50px of in-flow height (the top:100/left:100 shift is
	// paint-only and does not change reserved flow), so p follows at y=50; the
	// fixed box reserved nothing.
	assertF(t, "p.Y", p.Y, 50)
}

// TestNestedAbsoluteAgainstOuterAbsolute covers the growing out-of-flow queue:
// an absolute box inside another absolute box uses the outer's padding box.
func TestNestedAbsoluteAgainstOuterAbsolute(t *testing.T) {
	src := `<html><body style="margin:0;padding:0">` +
		`<div id="outer" style="position:absolute;top:50px;left:50px;padding:10px;width:100px;height:80px">` +
		`<div id="inner" style="position:absolute;top:5px;left:5px;width:20px;height:10px"></div>` +
		`</div></body></html>`
	root := layoutHTML(t, src, 300)
	outer, inner := findBoxByID(root, "outer"), findBoxByID(root, "inner")
	// outer at (50,50); its padding box origin equals its border-box origin
	// (no border), so inner offsets add directly.
	assertF(t, "outer.X", outer.X, 50)
	assertF(t, "inner.X", inner.X, 55)
	assertF(t, "inner.Y", inner.Y, 55)
}

// TestZIndexStackingOrder confirms positioned boxes paint after in-flow content
// and are ordered by z-index (higher last), regardless of source order.
func TestZIndexStackingOrder(t *testing.T) {
	src := `<html><body style="margin:0;padding:0">` +
		`<div id="flow" style="width:10px;height:10px"></div>` +
		`<div id="hi" style="position:absolute;top:0;left:0;width:50px;height:50px;z-index:5"></div>` +
		`<div id="lo" style="position:absolute;top:0;left:0;width:50px;height:50px;z-index:1"></div>` +
		`</body></html>`
	root := layoutHTML(t, src, 300) // root is the <html> box
	// Out-of-flow boxes are appended to the root box after the in-flow subtree
	// (the <body> child at index 0 holds all in-flow content, incl. #flow), so
	// they paint on top of it.
	body := -1
	for i, c := range root.Children {
		if c.Node != nil && c.Node.Tag == "body" {
			body = i
		}
	}
	lo := childIndex(root, "lo")
	hi := childIndex(root, "hi")
	if body < 0 || lo < 0 || hi < 0 {
		t.Fatalf("indices body=%d lo=%d hi=%d", body, lo, hi)
	}
	if findBoxByID(root.Children[body], "flow") == nil {
		t.Fatal("#flow should be inside the in-flow body subtree")
	}
	// Both positioned boxes paint after the in-flow body subtree.
	if lo <= body || hi <= body {
		t.Errorf("positioned boxes must paint after in-flow: body=%d lo=%d hi=%d", body, lo, hi)
	}
	// z-index 1 (lo) paints before z-index 5 (hi) even though hi is earlier in
	// source order.
	if !(lo < hi) {
		t.Errorf("expected lo(%d) before hi(%d) by z-index", lo, hi)
	}
}

// TestPositionedPageHeightGrowsForAbsolute confirms an absolute box below the
// in-flow content extends the reported page height, while a fixed box does not.
func TestPositionedPageHeightGrowsForAbsolute(t *testing.T) {
	src := `<html><body style="margin:0;padding:0">` +
		`<div id="abs" style="position:absolute;top:400px;left:0;width:10px;height:30px"></div>` +
		`</body></html>`
	root, h := layoutHTMLHeight(t, src, 300)
	_ = root
	if h != 430 { // 400 + 30
		t.Errorf("page height = %v, want 430", h)
	}
}

// TestFixedAnchorsToRealViewportNotDocumentHeight confirms LayoutDocumentViewport
// resolves a fixed box's containing block against the REAL viewport height
// passed in, not the eventual (much taller) document height — the bug
// confirmed live on pkg.go.dev/net/http's own `position:fixed;bottom:0`
// cookie-consent banner (round 84): without a real viewport height, `bottom:0`
// resolved against the full document height and landed the banner at the very
// bottom of a 78,000px+ page instead of near the top, the opposite of what a
// real browser's full-page screenshot shows (a fixed element stays pinned to
// the ORIGINAL viewport bounds). LayoutDocument (no viewport height given)
// keeps the old document-height approximation unchanged.
func TestFixedAnchorsToRealViewportNotDocumentHeight(t *testing.T) {
	src := `<html><body style="margin:0;padding:0">` +
		`<div id="tall" style="height:5000px"></div>` +
		`<div id="banner" style="position:fixed;bottom:0;left:0;width:10px;height:20px"></div>` +
		`</body></html>`
	root, err := dom.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	sm := css.Cascade(root)

	// With a real viewport height of 768, the banner's bottom:0 resolves
	// against 768, not the ~5000px document — it should land near the TOP.
	viewportBox, _ := LayoutDocumentViewport(root, sm, 300, 768, fakeMeasurer{}, nil)
	banner := findBoxByID(viewportBox, "banner")
	wantY := 768.0 - 20 // bottom:0 in a 768px viewport, box height 20
	assertF(t, "banner.Y (real viewport height)", banner.Y, wantY)

	// LayoutDocument (no viewport height) keeps the pre-existing
	// document-height approximation: bottom:0 resolves against the full
	// ~5000px document instead, landing the banner near the BOTTOM.
	docHeightBox, docHeight := LayoutDocument(root, sm, 300, fakeMeasurer{}, nil)
	banner2 := findBoxByID(docHeightBox, "banner")
	wantY2 := docHeight - 20
	assertF(t, "banner.Y (document-height approximation)", banner2.Y, wantY2)

	// The two must differ substantially — confirming this really is a
	// different containing block, not a coincidental match.
	if banner2.Y-banner.Y < 1000 {
		t.Errorf("expected the two placements to differ by thousands of px, got viewport=%.0f docHeight=%.0f", banner.Y, banner2.Y)
	}
}

// TestAbsoluteChildOfFlexContainerIsNotAFlexItem confirms an absolutely
// positioned child of a flex container is taken out of flex flow (it does not
// consume a flex track) and is placed against its containing block.
func TestAbsoluteChildOfFlexContainerIsNotAFlexItem(t *testing.T) {
	src := `<html><body style="margin:0;padding:0">` +
		`<div id="flex" style="display:flex;position:relative;width:200px;height:60px">` +
		`<div id="item" style="width:40px;height:40px"></div>` +
		`<div id="abs" style="position:absolute;top:5px;left:5px;width:20px;height:10px"></div>` +
		`</div></body></html>`
	root := layoutHTML(t, src, 300)
	flex := findBoxByID(root, "flex")
	item := findBoxByID(root, "item")
	abs := findBoxByID(root, "abs")
	// The lone flex item sits at the container origin (abs did not push it).
	assertF(t, "item.X", item.X, 0)
	assertF(t, "item.Y", item.Y, 0)
	// abs is placed against the flex container's padding box (it is positioned).
	assertF(t, "abs.X", abs.X, 5)
	assertF(t, "abs.Y", abs.Y, 5)
	_ = flex
}

// TestAbsoluteChildOfGridContainerIsNotAGridItem is the grid analogue.
func TestAbsoluteChildOfGridContainerIsNotAGridItem(t *testing.T) {
	src := `<html><body style="margin:0;padding:0">` +
		`<div id="grid" style="display:grid;position:relative;grid-template-columns:100px 100px;width:200px;height:60px">` +
		`<div id="c1" style="height:30px"></div>` +
		`<div id="abs" style="position:absolute;top:8px;left:8px;width:20px;height:10px"></div>` +
		`<div id="c2" style="height:30px"></div>` +
		`</div></body></html>`
	root := layoutHTML(t, src, 300)
	c1 := findBoxByID(root, "c1")
	c2 := findBoxByID(root, "c2")
	abs := findBoxByID(root, "abs")
	// The two real grid items fill columns 0 and 1 of the same row; the absolute
	// child does not occupy a track, so c2 lands in column 1 (x=100), not pushed
	// to a new cell.
	assertF(t, "c1.X", c1.X, 0)
	assertF(t, "c2.X", c2.X, 100)
	// abs against the grid container padding box.
	assertF(t, "abs.X", abs.X, 8)
	assertF(t, "abs.Y", abs.Y, 8)
}

// layoutHTMLHeight is like layoutHTML but also returns the reported page height.
func layoutHTMLHeight(t *testing.T, src string, vpW float64) (*Box, float64) {
	t.Helper()
	root, err := dom.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	sm := css.Cascade(root)
	return LayoutDocument(root, sm, vpW, fakeMeasurer{}, nil)
}
