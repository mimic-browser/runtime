// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package layout

import (
	"testing"

	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/dom"
)

// TestBaselineShiftForVerticalAlign covers the real shape found live on
// github.com's own Primer design system: every octicon SVG icon carries
// `style="vertical-align:text-bottom"`, and fakeMeasurer's fixed metrics
// (ascent=8, font height=20) give a known, exact descent of 12 to check
// against. Only VAlignTextBottom has an effect (see css.Style.VerticalAlign's
// own doc comment for the current scope) — every other keyword, including
// the explicit default, must leave BaselineShift at zero.
func TestBaselineShiftForVerticalAlign(t *testing.T) {
	l := &layouter{m: fakeMeasurer{}}
	st := &css.Style{}
	cases := []struct {
		name  string
		align css.VerticalAlign
		want  float64
	}{
		{"unset (baseline)", css.VAlignBaseline, 0},
		{"top", css.VAlignTop, 0},
		{"bottom", css.VAlignBottom, 0},
		{"text-top", css.VAlignTextTop, 0},
		{"text-bottom", css.VAlignTextBottom, 12}, // fh(20) - asc(8)
		{"middle", css.VAlignMiddle, 0},
		{"sub", css.VAlignSub, 0},
		{"super", css.VAlignSuper, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st.VerticalAlign = c.align
			if got := l.baselineShiftFor(st); got != c.want {
				t.Errorf("baselineShiftFor(%v) = %v, want %v", c.align, got, c.want)
			}
		})
	}
}

// TestVerticalAlignTextBottomLowersInlineImage confirms the shift actually
// moves a real image item DOWN within its real line, relative to the same
// image at the default alignment — the end-to-end shape, not just the
// isolated helper.
func TestVerticalAlignTextBottomLowersInlineImage(t *testing.T) {
	imageItem := func(vAlign string) *InlineItem {
		style := ""
		if vAlign != "" {
			style = ` style="vertical-align:` + vAlign + `"`
		}
		root, err := dom.Parse(`<html><body><p id="p">x<img id="i" width="10" height="10"` + style + `> y</p></body></html>`)
		if err != nil {
			t.Fatal(err)
		}
		sm := css.Cascade(root)
		l := &layouter{sm: sm, m: fakeMeasurer{}, floats: &floatCtx{}}
		p := dom.Find(root, "p")
		items := l.collectInline(p, sm[p], false, 0)
		lines := WrapItems(items, 1000)
		if len(lines) != 1 {
			t.Fatalf("got %d lines, want 1", len(lines))
		}
		placeLine(lines[0], 0, 0, 0)
		for _, it := range lines[0].Items {
			if it.Image != nil {
				return it
			}
		}
		t.Fatal("image item not found")
		return nil
	}

	base := imageItem("")
	textBottom := imageItem("text-bottom")
	if base.BaselineShift != 0 {
		t.Fatalf("default alignment BaselineShift = %v, want 0", base.BaselineShift)
	}
	if textBottom.BaselineShift == 0 {
		t.Fatal("text-bottom BaselineShift = 0, want nonzero")
	}
	if textBottom.Y <= base.Y {
		t.Errorf("text-bottom Y = %v, want strictly greater than default Y = %v (should sit lower)", textBottom.Y, base.Y)
	}
	if got, want := textBottom.Y-base.Y, textBottom.BaselineShift; got != want {
		t.Errorf("Y difference = %v, want exactly BaselineShift = %v", got, want)
	}
}
