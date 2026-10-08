// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package layout

import "testing"

// TestTableRowCssHeightIsMinimum covers a <tr>'s own CSS height acting as a
// minimum row height (CSS 2.1 §17.5.3). news.ycombinator.com's spacer rows are
// `<tr class="spacer" style="height:5px">`, which laid out at 0px before this.
func TestTableRowCssHeightIsMinimum(t *testing.T) {
	box := layoutHTML(t, `<html><body style="margin:0"><table cellpadding="0"><tr id="r" style="height:30px">`+
		`<td>x</td></tr></table></body></html>`, 500)
	tr := findBoxByID(box, "r")
	if tr == nil {
		t.Fatal("no tr#r")
	}
	if tr.H != 30 {
		t.Errorf("tr height:30px laid out at H=%v, want 30", tr.H)
	}
}

// A row's CSS height never shrinks a row below its own content.
func TestTableRowCssHeightDoesNotShrinkContent(t *testing.T) {
	box := layoutHTML(t, `<html><body style="margin:0"><table cellpadding="0"><tr id="r" style="height:2px">`+
		`<td style="height:50px">x</td></tr></table></body></html>`, 500)
	tr := findBoxByID(box, "r")
	if tr == nil {
		t.Fatal("no tr#r")
	}
	if tr.H < 50 {
		t.Errorf("tr height:2px with a 50px cell laid out at H=%v, want >= 50", tr.H)
	}
}
