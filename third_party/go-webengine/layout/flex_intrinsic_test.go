// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package layout

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/dom"
)

// TestFlexRowIntrinsicWidthSums is the regression for the flex-row intrinsic
// sizing bug: a content-sized flex row's max-content is the SUM of its items,
// so the items lay out side by side with proper advance instead of being shrunk
// on top of each other (the GitHub repo-nav "Code / Issues / …" overlap).
func TestFlexRowIntrinsicWidthSums(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "testdata", "flex_row_intrinsic.html"))
	if err != nil {
		t.Fatal(err)
	}
	root, err := dom.Parse(string(data))
	if err != nil {
		t.Fatal(err)
	}
	sm := css.Cascade(root)
	box, _ := LayoutDocument(root, sm, 400, fakeMeasurer{}, nil)

	nav := findBoxByID(box, "nav")
	if nav == nil {
		t.Fatal("no #nav box")
	}
	// fakeMeasurer: 10px/char → Code=40, Issues=60, Actions=70; sum=170.
	if nav.ContentW < 169 {
		t.Errorf("#nav content width %.0f, want ~170 (sum of items, not max)", nav.ContentW)
	}
	if len(nav.Children) != 3 {
		t.Fatalf("#nav has %d flex items, want 3", len(nav.Children))
	}
	// The three items must lay out left-to-right with no horizontal overlap.
	for i := 1; i < len(nav.Children); i++ {
		prev, cur := nav.Children[i-1], nav.Children[i]
		if cur.X < prev.X+prev.W-0.5 {
			t.Errorf("flex item %d [X=%.0f W=%.0f] overlaps previous [X=%.0f W=%.0f]",
				i, cur.X, cur.W, prev.X, prev.W)
		}
		if cur.W < 1 {
			t.Errorf("flex item %d collapsed to width %.0f", i, cur.W)
		}
	}
}
