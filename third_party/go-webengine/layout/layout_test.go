// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package layout

import (
	"testing"

	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/dom"
)

// fakeMeasurer gives every rune a 10px advance (independent of size), a space
// 10px, ascent 8 and line height 20 — so geometry is exactly predictable.
type fakeMeasurer struct{}

func (fakeMeasurer) Measure(text string, _ css.FontFamily, _ float64, _ int, _ bool) float64 {
	return float64(len([]rune(text)) * 10)
}
func (fakeMeasurer) Metrics(css.FontFamily, float64, int, bool) (float64, float64) { return 8, 20 }

func layoutHTML(t *testing.T, src string, vpW float64) *Box {
	t.Helper()
	root, err := dom.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	sm := css.Cascade(root)
	box, _ := LayoutDocument(root, sm, vpW, fakeMeasurer{}, nil)
	return box
}

func findBox(b *Box, tag string) *Box {
	if b == nil {
		return nil
	}
	if b.Node != nil && b.Node.Type == dom.Element && b.Node.Tag == tag {
		return b
	}
	for _, c := range b.Children {
		if got := findBox(c, tag); got != nil {
			return got
		}
	}
	return nil
}

func firstLineItems(b *Box) []*InlineItem {
	if b == nil {
		return nil
	}
	if len(b.Lines) > 0 {
		return b.Lines[0].Items
	}
	for _, c := range b.Children {
		if items := firstLineItems(c); items != nil {
			return items
		}
	}
	return nil
}

func TestBlockBoxMetrics(t *testing.T) {
	src := `<html><body style="margin:0"><div style="width:100px;padding:10px;margin:5px">hi</div></body></html>`
	root := layoutHTML(t, src, 1024)
	div := findBox(root, "div")
	if div == nil {
		t.Fatal("no div box")
	}
	// margin 5, padding 10, content width 100.
	assertF(t, "div.X", div.X, 5)
	assertF(t, "div.Y", div.Y, 5)
	assertF(t, "div.W", div.W, 120) // 10 + 100 + 10
	assertF(t, "div.H", div.H, 40)  // 10 + 20(line) + 10
	assertF(t, "div.ContentX", div.ContentX, 15)
	assertF(t, "div.ContentY", div.ContentY, 15)
	assertF(t, "div.ContentW", div.ContentW, 100)

	// The single word "hi" (2 runes → 20px) sits at the content origin, on the
	// shared baseline (ascent==line ascent → top offset 0).
	items := div.Lines
	if len(items) != 1 || len(items[0].Items) != 1 {
		t.Fatalf("expected one line with one item, got %v", items)
	}
	it := items[0].Items[0]
	assertF(t, "hi.X", it.X, 15)
	assertF(t, "hi.Y", it.Y, 15)
	assertF(t, "hi.Width", it.Width, 20)
}

func TestBlockAutoWidthAndNesting(t *testing.T) {
	// body has UA margin 8; html margin 0. At viewport 200, body content width
	// is 200 - 8 - 8 = 184, and a block child fills it.
	src := `<html><body><div>x</div></body></html>`
	root := layoutHTML(t, src, 200)
	body := findBox(root, "body")
	assertF(t, "body.X", body.X, 8)
	assertF(t, "body.ContentW", body.ContentW, 184)
	div := findBox(root, "div")
	assertF(t, "div.ContentW", div.ContentW, 184)
	assertF(t, "div.X", div.X, 8)
}

func TestInlineWrappingInFlow(t *testing.T) {
	// content width chosen so exactly two words fit per line.
	// words a b c d each 10px, space 10px. "a"+" "+"b" = 30. +" "+"c" = 50 > 35.
	src := `<html><body style="margin:0"><div style="width:35px">a b c d</div></body></html>`
	root := layoutHTML(t, src, 1024)
	div := findBox(root, "div")
	if len(div.Lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(div.Lines))
	}
	// second line first item positioned at content origin (x=0 content-relative).
	l2 := div.Lines[1]
	assertF(t, "line2.item0.X", l2.Items[0].X, div.ContentX)
	assertF(t, "div.H", div.H, 40) // two 20px lines
}

func TestTextAlignCenterAndRight(t *testing.T) {
	center := findBox(layoutHTML(t,
		`<html><body style="margin:0"><div style="width:100px;text-align:center">ab</div></body></html>`, 1024), "div")
	it := center.Lines[0].Items[0]
	// used width 20, cw 100 → offset 40.
	assertF(t, "center.offset", it.X-center.ContentX, 40)

	right := findBox(layoutHTML(t,
		`<html><body style="margin:0"><div style="width:100px;text-align:right">ab</div></body></html>`, 1024), "div")
	itr := right.Lines[0].Items[0]
	assertF(t, "right.offset", itr.X-right.ContentX, 80)
}

// TestTextWrapBalanceRedistributesLineBreaks guards the `text-wrap:balance`
// fast path in layoutInline (round 93): a plain greedy wrap of "a b c d e"
// (each word 10px, space 10px) at width 80px produces the classic uneven
// "orphan" split — "a b c d" (70px used) then a lone "e" (10px) — while
// `text-wrap:balance` must re-break at the SAME line count (2) but more
// evenly: the narrowest width that still wraps to 2 lines is 50px, giving
// "a b c" / "d e" (50px / 30px used) — see balanceWidth's own doc comment
// for why 50 is exactly that width.
func TestTextWrapBalanceRedistributesLineBreaks(t *testing.T) {
	greedy := findBox(layoutHTML(t,
		`<html><body style="margin:0"><div style="width:80px">a b c d e</div></body></html>`, 1024), "div")
	if len(greedy.Lines) != 2 {
		t.Fatalf("greedy: expected 2 lines, got %d", len(greedy.Lines))
	}
	if lineText(greedy.Lines[0]) != "a b c d" || lineText(greedy.Lines[1]) != "e" {
		t.Fatalf("greedy split = %q / %q, want %q / %q", lineText(greedy.Lines[0]), lineText(greedy.Lines[1]), "a b c d", "e")
	}

	balanced := findBox(layoutHTML(t,
		`<html><body style="margin:0"><div style="width:80px;text-wrap:balance">a b c d e</div></body></html>`, 1024), "div")
	if len(balanced.Lines) != 2 {
		t.Fatalf("balanced: expected 2 lines (balance must not change the line count), got %d", len(balanced.Lines))
	}
	if lineText(balanced.Lines[0]) != "a b c" || lineText(balanced.Lines[1]) != "d e" {
		t.Errorf("balanced split = %q / %q, want %q / %q", lineText(balanced.Lines[0]), lineText(balanced.Lines[1]), "a b c", "d e")
	}
}

// TestTextWrapBalanceSkipsWhenFloatsPresent confirms the fast path's own
// documented scope limit: with any float placed anywhere in the document
// already, balancing is skipped entirely and ordinary greedy wrapping
// applies — layoutInline has no single "cw" a float-narrowed line's own
// balance search could run against.
func TestTextWrapBalanceSkipsWhenFloatsPresent(t *testing.T) {
	box := findBox(layoutHTML(t, `<html><body style="margin:0">`+
		`<aside style="float:left;width:10px;height:10px"></aside>`+
		`<div style="width:80px;text-wrap:balance">a b c d e</div>`+
		`</body></html>`, 1024), "div")
	if len(box.Lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(box.Lines))
	}
	if lineText(box.Lines[0]) != "a b c d" || lineText(box.Lines[1]) != "e" {
		t.Errorf("split = %q / %q, want unbalanced greedy %q / %q (floats present, balance skipped)",
			lineText(box.Lines[0]), lineText(box.Lines[1]), "a b c d", "e")
	}
}

// TestTextWrapNowrapPreventsWrapping guards `text-wrap:nowrap` (round 95,
// flagged as a follow-up in round 93's own text-wrap:balance work): per CSS
// Text 4, "lines only break at forced line breaks; content that does not fit
// ... overflows" — the SAME never-wraps placement white-space:nowrap already
// gives, reached through the separate text-wrap-mode axis instead.
func TestTextWrapNowrapPreventsWrapping(t *testing.T) {
	src := `<html><body style="margin:0"><div style="width:20px;text-wrap:nowrap">aaaa bbbb cccc</div></body></html>`
	box := findBox(layoutHTML(t, src, 1024), "div")
	if len(box.Lines) != 1 {
		t.Fatalf("expected 1 overflowing line, got %d", len(box.Lines))
	}
	if lineText(box.Lines[0]) != "aaaa bbbb cccc" {
		t.Errorf("line = %q, want all three words on one line", lineText(box.Lines[0]))
	}
}

// TestTextWrapNowrapStillCollapsesWhitespace confirms the spec's own explicit
// carve-out: text-wrap-mode affects line-breaking opportunities only, never
// whitespace collapsing — unlike white-space:pre/pre-wrap (which text-wrap
// has no connection to at all), a run of source spaces under text-wrap:
// nowrap must still collapse to one, not render as a single preserved
// multi-space run.
func TestTextWrapNowrapStillCollapsesWhitespace(t *testing.T) {
	src := `<html><body style="margin:0"><div style="width:20px;text-wrap:nowrap">a    b</div></body></html>`
	box := findBox(layoutHTML(t, src, 1024), "div")
	if len(box.Lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(box.Lines))
	}
	items := box.Lines[0].Items
	if len(items) != 2 || items[0].Text != "a" || items[1].Text != "b" {
		t.Fatalf("expected two collapsed-whitespace words [a b], got %v", items)
	}
	if items[1].SpaceBefore <= 0 {
		t.Errorf("SpaceBefore = %v, want a normal collapsed single space, not zero (glued)", items[1].SpaceBefore)
	}
}

func TestPrePreservesWhitespaceAndNewlines(t *testing.T) {
	src := "<html><body style=\"margin:0\"><pre style=\"margin:0;padding:0\">x  y\nzz</pre></body></html>"
	pre := findBox(layoutHTML(t, src, 1024), "pre")
	if len(pre.Lines) != 2 {
		t.Fatalf("expected 2 pre lines, got %d", len(pre.Lines))
	}
	// "x  y" preserved (4 chars incl. two spaces → 40px), no wrapping.
	first := pre.Lines[0].Items[0]
	if first.Text != "x  y" {
		t.Errorf("pre line0 text = %q", first.Text)
	}
	assertF(t, "pre.line0.width", first.Width, 40)
	if pre.Lines[1].Items[0].Text != "zz" {
		t.Errorf("pre line1 = %q", pre.Lines[1].Items[0].Text)
	}
}

func TestPreDoesNotWrap(t *testing.T) {
	src := `<html><body><pre style="width:10px">aaaaaa</pre></body></html>`
	pre := findBox(layoutHTML(t, src, 1024), "pre")
	if len(pre.Lines) != 1 {
		t.Fatalf("pre must not wrap; got %d lines", len(pre.Lines))
	}
}

// TestWordBreakNormalDoesNotSplit guards the DEFAULT, unchanged behaviour
// splitOverlongWord must leave alone: with no word-break/overflow-wrap set at
// all, a single unbreakable "word" wider than its own container still
// overflows as one whole line, exactly as it always has (see
// TestWrapItemsOverflowWord's identical claim at WrapItems' own pure level).
func TestWordBreakNormalDoesNotSplit(t *testing.T) {
	src := `<html><body style="margin:0"><div style="width:30px">aaaaaaaaaa</div></body></html>`
	div := findBox(layoutHTML(t, src, 1024), "div")
	if len(div.Lines) != 1 {
		t.Fatalf("expected 1 overflowing line, got %d", len(div.Lines))
	}
	if lineText(div.Lines[0]) != "aaaaaaaaaa" {
		t.Errorf("line = %q, want the whole unsplit word", lineText(div.Lines[0]))
	}
}

// TestWordBreakAllSplitsOverlongWord is the go.dev/blog... no — the
// pkg.go.dev/news.ycombinator.com/developer.mozilla.org confirmed real
// trigger (round 143): a container narrower than its own longest unbreakable
// word now splits it at a rune boundary instead of overflowing. With
// fakeMeasurer's 10px-per-rune and a 30px container, "aaaaaaaaaa" (10 runes)
// must split into 3-rune chunks (30px each, exactly filling the line) with a
// final 1-rune remainder — 4 lines, not 1 overflowing one.
func TestWordBreakAllSplitsOverlongWord(t *testing.T) {
	src := `<html><body style="margin:0"><div style="width:30px;word-break:break-all">aaaaaaaaaa</div></body></html>`
	div := findBox(layoutHTML(t, src, 1024), "div")
	if len(div.Lines) != 4 {
		t.Fatalf("expected 4 lines, got %d: %v", len(div.Lines), linesText(div.Lines))
	}
	want := []string{"aaa", "aaa", "aaa", "a"}
	for i, w := range want {
		if lineText(div.Lines[i]) != w {
			t.Errorf("line%d = %q, want %q", i, lineText(div.Lines[i]), w)
		}
	}
}

// TestOverflowWrapBreakWordSplitsOverlongWord confirms overflow-wrap reaches
// the exact same splitting code path as word-break:break-all (both OR into
// css.Style.BreaksOverlongWords) — the confirmed real trigger on
// developer.mozilla.org, which sets this on `html` itself.
func TestOverflowWrapBreakWordSplitsOverlongWord(t *testing.T) {
	src := `<html><body style="margin:0"><div style="width:30px;overflow-wrap:break-word">aaaaaaaaaa</div></body></html>`
	div := findBox(layoutHTML(t, src, 1024), "div")
	if len(div.Lines) != 4 {
		t.Fatalf("expected 4 lines, got %d: %v", len(div.Lines), linesText(div.Lines))
	}
	want := []string{"aaa", "aaa", "aaa", "a"}
	for i, w := range want {
		if lineText(div.Lines[i]) != w {
			t.Errorf("line%d = %q, want %q", i, lineText(div.Lines[i]), w)
		}
	}
}

// TestWordBreakAllDoesNotAffectWordsThatAlreadyFit confirms this engine only
// models the evidenced "avoid overflow" effect, not break-all's stronger
// spec behaviour of breaking EAGERLY even when a word already fits on its
// own line (see WordBreakAll's own doc comment) — ordinary short content is
// completely unaffected.
func TestWordBreakAllDoesNotAffectWordsThatAlreadyFit(t *testing.T) {
	src := `<html><body style="margin:0"><div style="width:35px;word-break:break-all">a b c d</div></body></html>`
	div := findBox(layoutHTML(t, src, 1024), "div")
	if len(div.Lines) != 2 {
		t.Fatalf("expected 2 lines (ordinary greedy wrap, unaffected by break-all), got %d: %v", len(div.Lines), linesText(div.Lines))
	}
	if lineText(div.Lines[0]) != "a b" || lineText(div.Lines[1]) != "c d" {
		t.Errorf("lines = %q / %q, want %q / %q", lineText(div.Lines[0]), lineText(div.Lines[1]), "a b", "c d")
	}
}

// TestWordBreakAllSingleRuneCannotSplit covers splitOverlongWord's own
// "fewer than two runes" decline: a single-character item too wide for its
// own container (here, a 5px container against fakeMeasurer's 10px-per-rune)
// has nothing to split off, so it falls through to forceOne and overflows as
// one whole (single-item) line, same as with no word-break set at all.
func TestWordBreakAllSingleRuneCannotSplit(t *testing.T) {
	src := `<html><body style="margin:0"><div style="width:5px;word-break:break-all">a</div></body></html>`
	div := findBox(layoutHTML(t, src, 1024), "div")
	if len(div.Lines) != 1 || lineText(div.Lines[0]) != "a" {
		t.Fatalf("expected 1 overflowing line %q, got %d: %v", "a", len(div.Lines), linesText(div.Lines))
	}
}

// TestWordBreakAllInherits confirms WordBreakAll reaches a descendant text
// node through ordinary CSS inheritance (it is not, itself, testing anything
// about splitOverlongWord beyond that the style actually arrives there) —
// set on a div, read from a nested span's own overlong word.
func TestWordBreakAllInherits(t *testing.T) {
	src := `<html><body style="margin:0"><div style="width:30px;word-break:break-all"><span>aaaaaaaaaa</span></div></body></html>`
	div := findBox(layoutHTML(t, src, 1024), "div")
	if len(div.Lines) != 4 {
		t.Fatalf("expected 4 lines (inherited word-break:break-all), got %d: %v", len(div.Lines), linesText(div.Lines))
	}
}

// TestWordBreakAllDoesNotSplitAGluedRun exercises splitOverlongWord's own
// disclosed scope limit — a glued run of more than one item (here, text
// immediately followed by a same-element-boundary `<sup>`, engine#149's own
// `152,3<sup>†</sup>` shape) is never split WITHIN the run; splitOverlongWord
// declines (its glueRun check returns ok=false) and layoutInline falls
// through to forceOne exactly as it would with no word-break set at all.
//
// That fallback's own result is a genuine, PRE-EXISTING inconsistency this
// round found by accident and is NOT fixing: forceOne places only the run's
// FIRST item (see forceOne's own doc comment — it has never called glueRun
// at all), so the run ends up split at the glue seam onto two lines here,
// even though glueRun's own doc comment calls that exact seam unsplittable.
// Confirmed unrelated to this round's own change: the identical two-line
// split reproduces with no word-break/overflow-wrap property set anywhere.
// No confirmed real-world page trigger for THIS specific combination (an
// unbreakable glued run wider than its own container) was found, so it is
// disclosed here rather than chased — this test pins the actual current
// behaviour so a future round fixing forceOne itself has a red test to turn
// green, rather than silently asserting something false about today's code.
func TestWordBreakAllDoesNotSplitAGluedRun(t *testing.T) {
	src := `<html><body style="margin:0"><div style="width:30px;word-break:break-all">aaaaaaaaaa<sup>x</sup></div></body></html>`
	div := findBox(layoutHTML(t, src, 1024), "div")
	if len(div.Lines) != 2 {
		t.Fatalf("expected 2 lines (splitOverlongWord declines a glued run; forceOne's own pre-existing, unrelated behaviour then splits it at the glue seam instead), got %d: %v", len(div.Lines), linesText(div.Lines))
	}
	if lineText(div.Lines[0]) != "aaaaaaaaaa" || lineText(div.Lines[1]) != "x" {
		t.Errorf("lines = %v, want [aaaaaaaaaa] [x]", linesText(div.Lines))
	}
}

func TestDisplayNoneSkipped(t *testing.T) {
	src := `<html><body><div><span style="display:none">HIDDEN</span>y</div></body></html>`
	items := firstLineItems(findBox(layoutHTML(t, src, 1024), "div"))
	if len(items) != 1 || items[0].Text != "y" {
		t.Fatalf("display:none not skipped: %v", texts(items))
	}
}

func TestInlineImageFromAttrs(t *testing.T) {
	src := `<html><body><p><img width="30" height="40px">after</p></body></html>`
	items := firstLineItems(findBox(layoutHTML(t, src, 1024), "p"))
	if len(items) != 2 {
		t.Fatalf("expected image + word, got %v", texts(items))
	}
	img := items[0]
	if img.Image == nil {
		t.Fatal("first item should be an image")
	}
	assertF(t, "img.ImgW", img.ImgW, 30)
	assertF(t, "img.ImgH", img.ImgH, 40)
}

// imgSizeHTML parses src and cascades it, returning the root, style map, and
// an imgSize override for the FIRST <img> found — used instead of the
// width/height HTML ATTRIBUTES (which css/presentational.go maps to real CSS
// width/height declarations, giving the element a DEFINITE, non-auto height
// that would short-circuit usedHeight's own explicit-height-always-wins rule
// before an aspect-ratio-driven resize is ever reached) so a test can exercise
// a purely-auto-sized image, matching the real regression's own shape exactly
// (github.com/golang/go's README image carries no width/height attributes at
// all, only `style="max-width:100%"`).
func imgSizeHTML(t *testing.T, src string, iw, ih float64) (*dom.Node, css.StyleMap, map[*dom.Node][2]float64) {
	t.Helper()
	root, err := dom.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	sm := css.Cascade(root)
	img := dom.Find(root, "img")
	return root, sm, map[*dom.Node][2]float64{img: {iw, ih}}
}

// TestInlineImageMaxWidthPercentResolvesAgainstContainer is the confirmed
// real-world regression (round 88): github.com/golang/go's own README image
// (`<img style="max-width:100%">`) sits in a ~646px-wide article column on a
// wider page — the pre-layout image loader can only size a percentage width
// against the page's own viewport (unaware of any narrower container), so it
// left the image at its full intrinsic width, overflowing the article and
// misaligning everything below it. Layout must apply max-width AGAINST THE
// REAL CONTAINER WIDTH, shrinking the display size (not the loaded ImgW/ImgH,
// which paint uses as the resample source) rather than the loader's own
// viewport-relative guess.
func TestInlineImageMaxWidthPercentResolvesAgainstContainer(t *testing.T) {
	root, sm, sizes := imgSizeHTML(t, `<html><body><div style="width:200px"><img style="max-width:100%"></div></body></html>`, 1000, 500)
	box, _ := LayoutDocument(root, sm, 1024, fakeMeasurer{}, sizes)
	items := firstLineItems(findBox(box, "div"))
	if len(items) != 1 || items[0].Image == nil {
		t.Fatalf("expected one image item, got %v", items)
	}
	img := items[0]
	assertF(t, "img.ImgW (unchanged loaded size)", img.ImgW, 1000)
	assertF(t, "img.ImgH (unchanged loaded size)", img.ImgH, 500)
	assertF(t, "img.Width (display size, clamped to container)", img.Width, 200)
	assertF(t, "img.LineHeight (scaled by aspect ratio)", img.LineHeight, 100)
}

// TestInlineImageWidthPercentResolvesAgainstContainer covers the sibling
// `width:N%` case (not just max-width), against the same real container.
func TestInlineImageWidthPercentResolvesAgainstContainer(t *testing.T) {
	root, sm, sizes := imgSizeHTML(t, `<html><body><div style="width:200px"><img style="width:50%"></div></body></html>`, 1000, 500)
	box, _ := LayoutDocument(root, sm, 1024, fakeMeasurer{}, sizes)
	items := firstLineItems(findBox(box, "div"))
	img := items[0]
	assertF(t, "img.Width", img.Width, 100)
	assertF(t, "img.LineHeight", img.LineHeight, 50)
}

// TestInlineImageNoCSSSizeStaysAtIntrinsicWidth confirms the overwhelmingly
// common no-CSS-size case is unaffected: without an explicit width/max-width,
// the image still displays at its own loaded size, even when that overflows
// its container — matching a real browser (which does not auto-fit an
// unstyled image to its container either).
func TestInlineImageNoCSSSizeStaysAtIntrinsicWidth(t *testing.T) {
	root, sm, sizes := imgSizeHTML(t, `<html><body><div style="width:200px"><img></div></body></html>`, 1000, 500)
	box, _ := LayoutDocument(root, sm, 1024, fakeMeasurer{}, sizes)
	items := firstLineItems(findBox(box, "div"))
	img := items[0]
	assertF(t, "img.Width", img.Width, 1000)
	assertF(t, "img.LineHeight", img.LineHeight, 500)
}

// TestInlineImageExplicitHeightWinsOverAspectRatio is the confirmed
// real-world regression (round 90, found via object-fit's own verification):
// tailwindcss.com's own gallery `<img>`s set BOTH `width:100%` AND an
// independent explicit `height` (Tailwind's `w-full h-40`) — but
// resolvedReplacedSize derived height purely from the resolved width and the
// source's own aspect ratio, discarding the author's explicit height
// entirely. Real symptom: the oversized image (285px tall instead of the
// intended 160px) spilled downward over the listing card's own title/text
// sitting right below it. An explicit height must always win, exactly like
// an explicit width does — never overridden by an aspect-ratio derivation.
func TestInlineImageExplicitHeightWinsOverAspectRatio(t *testing.T) {
	root, sm, sizes := imgSizeHTML(t, `<html><body><div style="width:200px"><img style="width:100%;height:80px"></div></body></html>`, 1000, 500)
	box, _ := LayoutDocument(root, sm, 1024, fakeMeasurer{}, sizes)
	items := firstLineItems(findBox(box, "div"))
	img := items[0]
	assertF(t, "img.Width (from width:100%)", img.Width, 200)
	assertF(t, "img.LineHeight (explicit height, not aspect-derived 100)", img.LineHeight, 80)
}

// TestInlineImageAutoWidthExplicitHeightDerivesWidthFromRatio covers CSS
// 2.1 §10.3.2's own rule 2 — width:auto, height not auto, element has an
// intrinsic ratio: "used width = used height * intrinsic ratio" — which
// resolvedReplacedSize got wrong before this fix, always returning the
// UNSCALED intrinsic width paired with the explicit height instead (found
// via issue #226, a real A0-poster export through go-pdfkit/html2pdf where a
// height-only-styled image lost its aspect ratio and overflowed its grid
// track). This is the mirror image of
// TestInlineImageExplicitHeightWinsOverAspectRatio above: THAT test has BOTH
// width and height explicit (both must win, no ratio); this one has ONLY
// height explicit (width must be DERIVED from the ratio, not left at the raw
// intrinsic size).
func TestInlineImageAutoWidthExplicitHeightDerivesWidthFromRatio(t *testing.T) {
	root, sm, sizes := imgSizeHTML(t, `<html><body><div style="width:200px"><img style="height:80px"></div></body></html>`, 1000, 500)
	box, _ := LayoutDocument(root, sm, 1024, fakeMeasurer{}, sizes)
	items := firstLineItems(findBox(box, "div"))
	img := items[0]
	assertF(t, "img.Width (derived from ratio: 80*1000/500)", img.Width, 160)
	assertF(t, "img.LineHeight (explicit)", img.LineHeight, 80)
}

// TestBlockImageAutoWidthExplicitHeightDerivesWidthFromRatio is the
// block-level sibling of the inline test above, covering the same §10.3.2
// rule 2 in contents()'s isReplacedTag branch — the actual path a grid item
// that is itself a bare `<img>` reaches (grid blockifies its items), which is
// exactly how issue #226 was triggered in a real document. Checks
// Lines[0].Items[0] rather than img.W/H, for the same reason
// TestBlockImageExplicitHeightWinsOverAspectRatio's own comment already
// documents: place()'s pre-existing usedHeight override reports the outer
// box's own height/width from ordinary block sizing regardless of this fix,
// so asserting on img.W/H here would be a false-positive test that isn't
// exercising the actual regression at all.
func TestBlockImageAutoWidthExplicitHeightDerivesWidthFromRatio(t *testing.T) {
	root, sm, sizes := imgSizeHTML(t, `<html><body><div style="width:200px"><img style="display:block;height:80px"></div></body></html>`, 1000, 500)
	box, _ := LayoutDocument(root, sm, 1024, fakeMeasurer{}, sizes)
	img := findBox(box, "img")
	if img == nil || len(img.Lines) != 1 || len(img.Lines[0].Items) != 1 {
		t.Fatalf("expected one img box with one line item, got %v", img)
	}
	item := img.Lines[0].Items[0]
	assertF(t, "item.Width (derived from ratio: 80*1000/500)", item.Width, 160)
	assertF(t, "item.LineHeight (explicit)", item.LineHeight, 80)
}

// TestBlockImageMaxWidthPercentResolvesAgainstContainer covers the sibling
// block-level replaced-element path (contents()'s isReplacedTag branch,
// reached when e.g. a stylesheet's preflight reset makes img display:block) —
// the same container-width regression as the inline case above.
func TestBlockImageMaxWidthPercentResolvesAgainstContainer(t *testing.T) {
	root, sm, sizes := imgSizeHTML(t, `<html><body><div style="width:200px"><img style="display:block;max-width:100%"></div></body></html>`, 1000, 500)
	box, _ := LayoutDocument(root, sm, 1024, fakeMeasurer{}, sizes)
	img := findBox(box, "img")
	if img == nil {
		t.Fatal("no img box found")
	}
	assertF(t, "img.W (clamped to container)", img.W, 200)
	assertF(t, "img.H (scaled by aspect ratio)", img.H, 100)
}

// TestBlockImageExplicitHeightWinsOverAspectRatio is the block-level sibling
// of TestInlineImageExplicitHeightWinsOverAspectRatio, covering the same
// round-90 regression in contents()'s isReplacedTag branch. Checks the box's
// own Lines[0].Items[0] (what paint actually reads to scale the bitmap), not
// img.H/ContentH: place()'s PRE-EXISTING usedHeight override already forces
// the outer box's own reported height to the explicit CSS value regardless of
// this fix (confirmed with a plain non-replaced `<div style="height:80px">`,
// which shows the identical override, unrelated to images entirely) — so
// checking img.H/ContentH here would pass even with this round's fix
// reverted, a false-positive test that isn't exercising the actual
// regression at all.
func TestBlockImageExplicitHeightWinsOverAspectRatio(t *testing.T) {
	root, sm, sizes := imgSizeHTML(t, `<html><body><div style="width:200px"><img style="display:block;width:100%;height:80px"></div></body></html>`, 1000, 500)
	box, _ := LayoutDocument(root, sm, 1024, fakeMeasurer{}, sizes)
	img := findBox(box, "img")
	if img == nil || len(img.Lines) != 1 || len(img.Lines[0].Items) != 1 {
		t.Fatalf("expected one img box with one line item, got %v", img)
	}
	item := img.Lines[0].Items[0]
	assertF(t, "item.Width (from width:100%)", item.Width, 200)
	assertF(t, "item.LineHeight (explicit height, not aspect-derived 100)", item.LineHeight, 80)
}

func TestImageSizeMapOverride(t *testing.T) {
	root, _ := dom.Parse(`<html><body><p><img></p></body></html>`)
	sm := css.Cascade(root)
	img := dom.Find(root, "img")
	sizes := map[*dom.Node][2]float64{img: {50, 60}}
	box, _ := LayoutDocument(root, sm, 1024, fakeMeasurer{}, sizes)
	items := firstLineItems(findBox(box, "p"))
	if len(items) != 1 || items[0].ImgW != 50 || items[0].ImgH != 60 {
		t.Fatalf("size map override failed: %v", items)
	}
}

func TestImageZeroSizeSkipped(t *testing.T) {
	// No width/height and no size map → intrinsic 0 → not laid out.
	src := `<html><body><p><img src="x.png">t</p></body></html>`
	items := firstLineItems(findBox(layoutHTML(t, src, 1024), "p"))
	if len(items) != 1 || items[0].Text != "t" {
		t.Fatalf("zero-size image should be skipped: %v", texts(items))
	}
}

func TestBrInFlow(t *testing.T) {
	src := `<html><body style="margin:0"><div style="margin:0;padding:0">a<br>b</div></body></html>`
	div := findBox(layoutHTML(t, src, 1024), "div")
	if len(div.Lines) != 2 {
		t.Fatalf("br should force 2 lines, got %d", len(div.Lines))
	}
	assertF(t, "div.H", div.H, 40)
}

func TestEmptyDocument(t *testing.T) {
	// A document with no element produces an empty box and zero height.
	root := &dom.Node{Type: dom.Document}
	box, h := LayoutDocument(root, css.StyleMap{}, 100, fakeMeasurer{}, nil)
	if box == nil || h != 0 {
		t.Fatalf("empty doc: box=%v h=%v", box, h)
	}
}

func TestAttrFloat(t *testing.T) {
	el := &dom.Node{Type: dom.Element, Tag: "img", Attr: map[string]string{
		"width": "12px", "bad": "xyz",
	}}
	if got := attrFloat(el, "width"); got != 12 {
		t.Errorf("width = %v", got)
	}
	if got := attrFloat(el, "bad"); got != 0 {
		t.Errorf("bad = %v", got)
	}
	if got := attrFloat(el, "missing"); got != 0 {
		t.Errorf("missing = %v", got)
	}
}

func TestMixedInlineAndBlockChildren(t *testing.T) {
	// A div with inline text, then a block <p>, then more inline text generates
	// two anonymous inline boxes around the block child.
	src := `<html><body style="margin:0"><div style="margin:0;padding:0">before<p style="margin:0">mid</p>after</div></body></html>`
	div := findBox(layoutHTML(t, src, 1024), "div")
	// Children: anon(before), p(mid), anon(after) → 3 boxes, stacked.
	if len(div.Children) != 3 {
		t.Fatalf("expected 3 children, got %d", len(div.Children))
	}
	if !div.Children[0].Anonymous || div.Children[1].Node == nil || !div.Children[2].Anonymous {
		t.Errorf("child kinds = %+v", div.Children)
	}
	// Vertical stacking: before at y=0, p next, after last.
	assertF(t, "before.Y", div.Children[0].Y, 0)
	assertF(t, "before.H", div.Children[0].H, 20)
	assertF(t, "p.Y", div.Children[1].Y, 20)
	assertF(t, "after.Y", div.Children[2].Y, 40)
	if div.Children[0].Lines[0].Items[0].Text != "before" {
		t.Errorf("anon0 text = %q", div.Children[0].Lines[0].Items[0].Text)
	}
}

func TestMixedRunWithInlineElement(t *testing.T) {
	// An inline element (<strong>) shares an anonymous box with adjacent text,
	// alongside a block sibling — exercising element handling in a mixed run.
	src := `<html><body style="margin:0"><div style="margin:0;padding:0">` +
		`x<strong>bold</strong><p style="margin:0">para</p>tail</div></body></html>`
	div := findBox(layoutHTML(t, src, 1024), "div")
	if len(div.Children) != 3 {
		t.Fatalf("expected 3 children, got %d", len(div.Children))
	}
	anon0 := div.Children[0]
	got := texts(anon0.Lines[0].Items)
	if len(got) != 2 || got[0] != "x" || got[1] != "bold" {
		t.Fatalf("anon0 items = %v", got)
	}
	if !anon0.Lines[0].Items[1].Style.Bold() {
		t.Error("<strong> item should be bold")
	}
}

func TestNegativeContentWidthClamped(t *testing.T) {
	// Margins larger than the viewport must clamp content width to 0, not panic.
	src := `<html><body style="margin:0"><div style="margin:2000px">x</div></body></html>`
	div := findBox(layoutHTML(t, src, 100), "div")
	assertF(t, "clamped ContentW", div.ContentW, 0)
}

// TestTrailingBrGivesNoExtraLine guards round 97's own fix: a `<br>` with
// nothing after it must NOT open a further, visibly-empty line — real Chrome
// gives "a<br>" the exact same height as plain "a" (confirmed live on
// go.dev/blog, whose own `<span class="author">…<br></span>` markup ends
// every post title with a trailing break this way). This replaces an OLDER
// version of this same test (TestTrailingBrEmptyLine) that had encoded the
// bug itself as the expected behavior.
func TestTrailingBrGivesNoExtraLine(t *testing.T) {
	src := `<html><body style="margin:0"><div style="margin:0;padding:0">a<br></div></body></html>`
	div := findBox(layoutHTML(t, src, 1024), "div")
	if len(div.Lines) != 1 {
		t.Fatalf("expected no extra trailing line, got %d lines", len(div.Lines))
	}
	plain := findBox(layoutHTML(t, `<html><body style="margin:0"><div style="margin:0;padding:0">a</div></body></html>`, 1024), "div")
	assertF(t, "trailing-br div height matches no-br div height", div.H, plain.H)
}

// TestTrailingDoubleBrKeepsOneRealBlankLine guards the OTHER half of round
// 97's fix: only the break that is genuinely the LAST thing in a run, with
// nothing left to end up on a following line, is suppressed — an EARLIER
// break in the same trailing run still opens its own real, visible blank
// line, exactly like "a<br><br>b" already does mid-content (see
// TestPreDoubleNewline). "a<br><br>" (nothing after the second break) must
// therefore end up ONE line taller than "a<br>" (nothing after the first),
// not zero taller (both breaks suppressed) and not two taller (neither
// suppressed).
func TestTrailingDoubleBrKeepsOneRealBlankLine(t *testing.T) {
	one := findBox(layoutHTML(t, `<html><body style="margin:0"><div style="margin:0;padding:0">a<br></div></body></html>`, 1024), "div")
	two := findBox(layoutHTML(t, `<html><body style="margin:0"><div style="margin:0;padding:0">a<br><br></div></body></html>`, 1024), "div")
	if len(two.Lines) != 2 {
		t.Fatalf("expected 2 lines (a, one real blank line), got %d", len(two.Lines))
	}
	if len(one.Lines) != 1 {
		t.Fatalf("expected 1 line for the single-trailing-br case, got %d", len(one.Lines))
	}
	assertF(t, "double-trailing-br div height", two.H, one.H+two.Lines[1].H)
}

func TestPreDoubleNewline(t *testing.T) {
	src := "<html><body><pre style=\"margin:0;padding:0\">a\n\nb</pre></body></html>"
	pre := findBox(layoutHTML(t, src, 1024), "pre")
	if len(pre.Lines) != 3 {
		t.Fatalf("expected 3 lines (a, blank, b), got %d", len(pre.Lines))
	}
	if len(pre.Lines[1].Items) != 0 {
		t.Errorf("middle line should be blank")
	}
}

func TestMissingStyleFallback(t *testing.T) {
	// Elements absent from the StyleMap fall back to a default block style
	// without panicking.
	root, _ := dom.Parse(`<html><body><div>hi</div></body></html>`)
	box, h := LayoutDocument(root, css.StyleMap{}, 200, fakeMeasurer{}, nil)
	if box == nil || h <= 0 {
		t.Fatalf("fallback layout failed: box=%v h=%v", box, h)
	}
}

func assertF(t *testing.T, name string, got, want float64) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %v, want %v", name, got, want)
	}
}

func texts(items []*InlineItem) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.Text)
	}
	return out
}
