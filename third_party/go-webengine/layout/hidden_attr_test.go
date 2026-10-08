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

// TestHiddenAttributeRemovedFromLayout confirms the HTML `hidden` attribute
// (mapped to display:none in the cascade) removes the element and its subtree
// from layout — no boxes, no lines — while an author `display` on a hidden
// element still lays out, and a plain visible element is unaffected.
func TestHiddenAttributeRemovedFromLayout(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "testdata", "hidden_attr.html"))
	if err != nil {
		t.Fatal(err)
	}
	root, err := dom.Parse(string(data))
	if err != nil {
		t.Fatal(err)
	}
	sm := css.Cascade(root)
	box, _ := LayoutDocument(root, sm, 400, fakeMeasurer{}, nil)

	if findBoxWithText(box, "HIDDENTEXT") != nil {
		t.Error("text inside <div hidden> was laid out (should be removed)")
	}
	if findBoxWithText(box, "VISIBLETEXT") == nil {
		t.Error("visible text not laid out")
	}
	// A hidden element that an author rule sets to display:block still lays out.
	if findBoxWithText(box, "SHOWNTEXT") == nil {
		t.Error("author display:block on a hidden element should still lay out")
	}
}
