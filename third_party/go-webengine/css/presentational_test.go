// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package css

import "testing"

// TestCenterElementCentresBlocks covers the <center> UA rule: display:block plus
// the legacy centre-including-blocks alignment.
func TestCenterElementCentresBlocks(t *testing.T) {
	c := styleOf(t, `<html><body><center>x</center></body></html>`, "center")
	if c.Display != DisplayBlock {
		t.Errorf("center display = %v, want block", c.Display)
	}
	if c.TextAlign != AlignCenterBlocks {
		t.Errorf("center text-align = %v, want AlignCenterBlocks", c.TextAlign)
	}
	// A real author text-align:center must stay plain AlignCenter (no block centring).
	p := styleOf(t, `<html><body><p style="text-align:center">x</p></body></html>`, "p")
	if p.TextAlign != AlignCenter {
		t.Errorf("author text-align:center = %v, want AlignCenter", p.TextAlign)
	}
	// -moz-center is accepted too.
	d := styleOf(t, `<html><body><div style="text-align:-moz-center">x</div></body></html>`, "div")
	if d.TextAlign != AlignCenterBlocks {
		t.Errorf("-moz-center = %v, want AlignCenterBlocks", d.TextAlign)
	}
}

// TestLegacyInlineTags covers the font/big/nobr/strike UA rules.
func TestLegacyInlineTags(t *testing.T) {
	if f := styleOf(t, `<html><body><font>x</font></body></html>`, "font"); f.Display != DisplayInline {
		t.Errorf("font display = %v, want inline", f.Display)
	}
	if big := styleOf(t, `<html><body><big>x</big></body></html>`, "big"); big.FontSize <= 16 {
		t.Errorf("big font-size = %v, want > 16 (larger)", big.FontSize)
	}
	// nobr/strike are inline; strike carries a line-through decoration.
	if nb := styleOf(t, `<html><body><nobr>x</nobr></body></html>`, "nobr"); nb.Display != DisplayInline {
		t.Errorf("nobr display = %v, want inline", nb.Display)
	}
	if st := styleOf(t, `<html><body><strike>x</strike></body></html>`, "strike"); st.Display != DisplayInline {
		t.Errorf("strike display = %v, want inline", st.Display)
	}
}

// TestPresentationalWidthHeight covers width/height attribute mapping, including
// percentages and unparseable values.
func TestPresentationalWidthHeight(t *testing.T) {
	tab := styleOf(t, `<html><body><table width="750" height="40">x</table></body></html>`, "table")
	if tab.Width.Auto || tab.Width.Resolve(1000) != 750 {
		t.Errorf("table width = %+v, want 750px", tab.Width)
	}
	pct := styleOf(t, `<html><body><table width="80%">x</table></body></html>`, "table")
	if !pct.Width.IsPercent || pct.Width.Resolve(1000) != 800 {
		t.Errorf("table width 80%% = %+v, want 0.8*cw", pct.Width)
	}
	// An unparseable width is ignored (stays auto).
	bad := styleOf(t, `<html><body><table width="abc">x</table></body></html>`, "table")
	if !bad.Width.Auto {
		t.Errorf("table width=abc = %+v, want auto (ignored)", bad.Width)
	}
	// A trailing px is tolerated.
	pxw := styleOf(t, `<html><body><img src="x" width="120px">`, "img")
	if pxw.Width.Resolve(0) != 120 {
		t.Errorf("img width=120px = %+v, want 120", pxw.Width)
	}
}

// TestPresentationalColorAndAlign covers bgcolor, <font> color/size/face, align
// on cells vs images vs tables, valign and nowrap.
func TestPresentationalColorAndAlign(t *testing.T) {
	td := styleOf(t, `<html><body><table><tr><td bgcolor="#ff0000" align="right" valign="top">x</td></tr></table></body></html>`, "td")
	if td.Background != (Color{0xff, 0, 0, 0xff}) {
		t.Errorf("td bgcolor = %v", td.Background)
	}
	if td.TextAlign != AlignRight {
		t.Errorf("td align=right → %v, want AlignRight", td.TextAlign)
	}
	if td.VerticalAlign != VAlignTop {
		t.Errorf("td valign=top → %v, want VAlignTop", td.VerticalAlign)
	}
	// align="center" on a flow cell centres blocks too; left/justify map straight.
	tdc := styleOf(t, `<html><body><table><tr><td align="center">x</td></tr></table></body></html>`, "td")
	if tdc.TextAlign != AlignCenterBlocks {
		t.Errorf("td align=center → %v, want AlignCenterBlocks", tdc.TextAlign)
	}
	if l := styleOf(t, `<html><body><div align="left">x</div></body></html>`, "div"); l.TextAlign != AlignLeft {
		t.Errorf("div align=left → %v, want AlignLeft", l.TextAlign)
	}
	// <font color size face>.
	fnt := styleOf(t, `<html><body><font color="#00ff00" size="5" face="Times">x</font></body></html>`, "font")
	if fnt.Color != (Color{0, 0xff, 0, 0xff}) {
		t.Errorf("font color = %v", fnt.Color)
	}
	if fnt.FontSize != 24 {
		t.Errorf("font size=5 → %v px, want 24", fnt.FontSize)
	}
	// img align=left floats; table align=center → auto margins.
	if im := styleOf(t, `<html><body><img src="x" align="left">`, "img"); im.Float != FloatLeft {
		t.Errorf("img align=left → float %v, want left", im.Float)
	}
	if im := styleOf(t, `<html><body><img src="x" align="right">`, "img"); im.Float != FloatRight {
		t.Errorf("img align=right → float %v, want right", im.Float)
	}
	tc := styleOf(t, `<html><body><table align="center"><tr><td>x</td></tr></table></body></html>`, "table")
	if !tc.MarginLeftAuto || !tc.MarginRightAuto {
		t.Errorf("table align=center → margins %v/%v, want auto/auto", tc.MarginLeftAuto, tc.MarginRightAuto)
	}
	if tl := styleOf(t, `<html><body><table align="left"><tr><td>x</td></tr></table></body></html>`, "table"); tl.Float != FloatLeft {
		t.Errorf("table align=left → float %v, want left", tl.Float)
	}
	if tr := styleOf(t, `<html><body><table align="right"><tr><td>x</td></tr></table></body></html>`, "table"); tr.Float != FloatRight {
		t.Errorf("table align=right → float %v, want right", tr.Float)
	}
}

// TestTdThDefaultToVerticalAlignMiddle covers the UA default a real browser
// gives table cells (unlike every other box, whose default is the property's
// own initial value, baseline) — confirmed load-bearing live on
// news.ycombinator.com's own top-nav table (round 147), whose three cells set
// no vertical-align/valign at all and rely on this default to centre an
// 18px logo image against a shorter line of nav text.
func TestTdThDefaultToVerticalAlignMiddle(t *testing.T) {
	if td := styleOf(t, `<html><body><table><tr><td>x</td></tr></table></body></html>`, "td"); td.VerticalAlign != VAlignMiddle {
		t.Errorf("plain td VerticalAlign = %v, want VAlignMiddle (UA default)", td.VerticalAlign)
	}
	if th := styleOf(t, `<html><body><table><tr><th>x</th></tr></table></body></html>`, "th"); th.VerticalAlign != VAlignMiddle {
		t.Errorf("plain th VerticalAlign = %v, want VAlignMiddle (UA default)", th.VerticalAlign)
	}
	// An author's own vertical-align still overrides the UA default.
	if td := styleOf(t, `<html><body><table><tr><td style="vertical-align:bottom">x</td></tr></table></body></html>`, "td"); td.VerticalAlign != VAlignBottom {
		t.Errorf("author vertical-align:bottom → %v, want VAlignBottom", td.VerticalAlign)
	}
}

// TestValignAttribute covers every valign keyword this engine models, and
// that the attribute is scoped to td/th only (the spec also allows it on
// tr/thead/tbody/tfoot/col/colgroup, not modelled — see presentational.go's
// own doc comment), plus that an unrecognised value is ignored.
func TestValignAttribute(t *testing.T) {
	cases := []struct {
		v    string
		want VerticalAlign
	}{
		{"top", VAlignTop},
		{"middle", VAlignMiddle},
		{"bottom", VAlignBottom},
		{"baseline", VAlignBaseline},
	}
	for _, c := range cases {
		td := styleOf(t, `<html><body><table><tr><td valign="`+c.v+`">x</td></tr></table></body></html>`, "td")
		if td.VerticalAlign != c.want {
			t.Errorf("td valign=%s → %v, want %v", c.v, td.VerticalAlign, c.want)
		}
	}
	// An unrecognised value is ignored, leaving the UA default (middle) standing.
	if td := styleOf(t, `<html><body><table><tr><td valign="bogus">x</td></tr></table></body></html>`, "td"); td.VerticalAlign != VAlignMiddle {
		t.Errorf("td valign=bogus (unrecognised) → %v, want VAlignMiddle (UA default left standing)", td.VerticalAlign)
	}
	// valign on a non-cell tag is not modelled — ignored.
	if tr := styleOf(t, `<html><body><table><tr valign="bottom"><td>x</td></tr></table></body></html>`, "tr"); tr.VerticalAlign != VAlignBaseline {
		t.Errorf("tr valign=bottom (unscoped tag) → %v, want VAlignBaseline (ignored, tr's own initial value)", tr.VerticalAlign)
	}
}

// TestPresentationalBorderAndBody covers table border/cellspacing and body text.
func TestPresentationalBorderAndBody(t *testing.T) {
	// border="0" must NOT add a border; a positive border does.
	b0 := styleOf(t, `<html><body><table border="0"><tr><td>x</td></tr></table></body></html>`, "table")
	if b0.Border.Top.paints() {
		t.Error("table border=0 painted a border")
	}
	b2 := styleOf(t, `<html><body><table border="2"><tr><td>x</td></tr></table></body></html>`, "table")
	if !b2.Border.Top.paints() || b2.Border.Top.Width != 2 {
		t.Errorf("table border=2 → %+v, want a 2px solid border", b2.Border.Top)
	}
	// A real author rule overrides a presentational hint (bgcolor here).
	over := styleOf(t, `<html><head><style>td{background-color:#0000ff}</style></head><body><table><tr><td bgcolor="#ff0000">x</td></tr></table></body></html>`, "td")
	if over.Background != (Color{0, 0, 0xff, 0xff}) {
		t.Errorf("author rule should override bgcolor hint; got %v", over.Background)
	}
	body := styleOf(t, `<html><body text="#123456">x</body></html>`, "body")
	if body.Color != (Color{0x12, 0x34, 0x56, 0xff}) {
		t.Errorf("body text= → color %v", body.Color)
	}
}

// TestFontSizeAttr covers the absolute and relative <font size> ladder plus the
// invalid / out-of-range branches (exercised through the cascade).
func TestFontSizeAttr(t *testing.T) {
	cases := []struct {
		size string
		want float64
	}{
		{"1", 10}, {"3", 16}, {"7", 48},
		{"+2", 24}, // 3+2 = size 5 → 24
		{"-1", 13}, // 3-1 = size 2 → 13
		{"9", 48},  // clamped to 7
		{"0", 10},  // clamped to 1
	}
	for _, c := range cases {
		f := styleOf(t, `<html><body><font size="`+c.size+`">x</font></body></html>`, "font")
		if f.FontSize != c.want {
			t.Errorf("font size=%q → %v px, want %v", c.size, f.FontSize, c.want)
		}
	}
	// An unparseable size leaves the inherited size (16px default) untouched.
	f := styleOf(t, `<html><body><font size="xx">x</font></body></html>`, "font")
	if f.FontSize != 16 {
		t.Errorf("font size=xx → %v, want inherited 16", f.FontSize)
	}
	// An empty size string is ignored too.
	f2 := styleOf(t, `<html><body><font size="">x</font></body></html>`, "font")
	if f2.FontSize != 16 {
		t.Errorf("font size='' → %v, want inherited 16", f2.FontSize)
	}
}

// TestLengthHintEmptyAndBadPercent covers the empty-string and malformed-percent
// branches of lengthHint via width attributes.
func TestLengthHintEmptyAndBadPercent(t *testing.T) {
	empty := styleOf(t, `<html><body><table width="">x</table></body></html>`, "table")
	if !empty.Width.Auto {
		t.Errorf("width='' → %+v, want auto", empty.Width)
	}
	badPct := styleOf(t, `<html><body><table width="x%">x</table></body></html>`, "table")
	if !badPct.Width.Auto {
		t.Errorf("width='x%%' → %+v, want auto", badPct.Width)
	}
	neg := styleOf(t, `<html><body><table width="-5">x</table></body></html>`, "table")
	if !neg.Width.Auto {
		t.Errorf("width=-5 → %+v, want auto (negative rejected)", neg.Width)
	}
}
