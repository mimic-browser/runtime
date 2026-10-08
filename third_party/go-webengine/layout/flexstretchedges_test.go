// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package layout

import "testing"

// TestFlexColumnStretchSubtractsItemEdges pins what stretch stretches: a
// stretched item's MARGIN box fills the line, so its padding and border come
// off the width it is given, not out of its container. The cross-axis pass
// handed layoutIsolated — whose parameter is a CONTENT width — the whole
// available width, so every padded item overflowed its container by exactly
// its own edges. One element under two parents of the same width is the whole
// test: an ordinary block parent gave it 600, a flex column gave it 650 (#228).
func TestFlexColumnStretchSubtractsItemEdges(t *testing.T) {
	const pad = `padding:20px;border:5px solid #000` // 50px of edges
	src := `<html><body style="margin:0">` +
		`<div style="display:flex;flex-direction:column;width:600px"><div style="` + pad + `">x</div></div>` +
		`<div style="width:600px"><div style="` + pad + `">x</div></div>` +
		`</body></html>`
	body := findBox(layoutHTML(t, src, 1000), "body")
	flexItem := body.Children[0].Children[0]
	blockChild := body.Children[1].Children[0]
	assertF(t, "stretched flex item.W", flexItem.W, 600)
	assertF(t, "same element under a block parent.W", blockChild.W, 600)
	if flexItem.W != blockChild.W {
		t.Errorf("the same element is %g wide in a flex column and %g under a block parent", flexItem.W, blockChild.W)
	}
}

// TestFlexColumnStretchHonoursBoxSizing is the same item with
// box-sizing:border-box, where the edges were already inside the width CSS
// asks for: the stretched result must still be the container's width, not
// less, so the two box-sizing values agree here (both fill the line).
func TestFlexColumnStretchHonoursBoxSizing(t *testing.T) {
	src := `<html><body style="margin:0">` +
		`<div style="display:flex;flex-direction:column;width:400px">` +
		`<div style="box-sizing:border-box;padding:16px;border:4px solid #000">x</div></div></body></html>`
	item := findBox(layoutHTML(t, src, 1000), "body").Children[0].Children[0]
	assertF(t, "stretched border-box item.W", item.W, 400)
}
