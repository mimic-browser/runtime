// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package layout

import (
	"math"
	"testing"
)

// TestAbsoluteUnitsLayOut: before css.parseLength understood the absolute
// units of CSS Values 4 §6.2, a `height:40mm` (and 4cm, 1.5in, 100pt, 10pc)
// laid out with H = 0 while `height:151px` gave 151 — every print
// stylesheet's mm and pt were silently dropped.
func TestAbsoluteUnitsLayOut(t *testing.T) {
	box := layoutHTML(t, `<html><body style="margin:0">`+
		`<div id="mm" style="height:40mm"></div>`+
		`<div id="cm" style="height:4cm"></div>`+
		`<div id="in" style="height:1.5in"></div>`+
		`<div id="pt" style="height:100pt"></div>`+
		`<div id="pc" style="height:10pc"></div>`+
		`<div id="q" style="height:400Q"></div>`+
		`<div id="px" style="height:151px"></div>`+
		`</body></html>`, 800)
	want := map[string]float64{
		"mm": 151.18, "cm": 151.18, "in": 144, "pt": 133.33, "pc": 160, "q": 377.95, "px": 151,
	}
	for id, h := range want {
		b := findBoxByID(box, id)
		if b == nil {
			t.Fatalf("no box for #%s", id)
		}
		if math.Abs(b.H-h) > 0.01 {
			t.Errorf("#%s: H = %.2f want %.2f", id, b.H, h)
		}
	}
}
