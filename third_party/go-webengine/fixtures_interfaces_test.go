// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package engine

import (
	"context"
	"image"
	"os"
	"path/filepath"
	"testing"
)

// TestInterfaceConstructorsRenderGolden proves the DOM interface constructors
// end to end: a script does `class Panel extends HTMLElement {}`, checks
// `instanceof HTMLElement/Node`, `new Event('x') instanceof Event`, and
// `new DOMException('m','NotFoundError')` (name/code), and only then mutates the
// real DOM. The rendered output must therefore contain the subclass-injected
// green box — proving the whole chain works AND that the mutation reached layout
// + paint. The DisableJS control must NOT paint it (proving it was the JS).
func TestInterfaceConstructorsRenderGolden(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("testdata", "interface_constructors.html"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	render := func(disableJS bool) *image.RGBA {
		e := New()
		e.DisableJS = disableJS
		img, _, err := e.RenderHTML(context.Background(), string(src), "https://demo.test/", image.Rect(0, 0, 260, 160))
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		return img
	}

	on := render(false)
	// The subclass appended #out { background: rgb(30,160,90) } (200x80). Sample a
	// spot inside the box but clear of the text glyphs at the top-left.
	assertPixel(t, on, 150, 60, 30, 160, 90, "subclass-injected box painted")

	off := render(true)
	// With JS disabled the box never exists, so that spot is the white page.
	c := off.RGBAAt(150, 60)
	if c.R == 30 && c.G == 160 && c.B == 90 {
		t.Errorf("control: box painted with JS disabled at (5,10) = #%02x%02x%02x", c.R, c.G, c.B)
	}
}
