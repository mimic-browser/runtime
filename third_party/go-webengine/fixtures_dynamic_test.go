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

// renderFixtureJS renders a testdata fixture with the given DisableJS setting.
func renderFixtureJS(t *testing.T, name string, w, h int, disableJS bool) *image.RGBA {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	e := New()
	e.DisableJS = disableJS
	img, _, err := e.RenderHTML(context.Background(), string(src), "https://demo.test/", image.Rect(0, 0, w, h))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	return img
}

// TestDynamicMetricsToggleGolden proves the layout↔JS feedback loop end to end:
// a script reads a REAL laid-out metric (offsetWidth === 200) and toggles a
// class that changes layout. The final render must reflect the widened box, and
// the DisableJS control must NOT (proving it was the JS, not the static CSS).
func TestDynamicMetricsToggleGolden(t *testing.T) {
	on := renderFixtureJS(t, "dynamic_metrics_toggle.html", 260, 200, false)
	// Top of the box is always inside it; with JS it is the widened colour.
	assertPixel(t, on, 5, 10, 200, 100, 50, "js: box top recoloured")
	// y=60 is inside the box ONLY when the .wide class made it 120px tall.
	assertPixel(t, on, 5, 60, 200, 100, 50, "js: widened box covers y=60")

	off := renderFixtureJS(t, "dynamic_metrics_toggle.html", 260, 200, true)
	assertPixel(t, off, 5, 10, 10, 20, 30, "no-js: box keeps base colour")
	assertPixel(t, off, 5, 60, 255, 255, 255, "no-js: box stays 20px tall (white below)")

	checkGolden(t, on, "dynamic_metrics_toggle.png")
}

// TestDynamicStyleInjectGolden proves a JS-created <style> is re-collected by the
// next cascade and re-laid-out (the element is recoloured and grows taller).
func TestDynamicStyleInjectGolden(t *testing.T) {
	on := renderFixtureJS(t, "dynamic_style_inject.html", 200, 140, false)
	assertPixel(t, on, 5, 10, 9, 8, 7, "js: injected style recolours #t")
	assertPixel(t, on, 5, 60, 9, 8, 7, "js: injected style grows #t to 80px")

	off := renderFixtureJS(t, "dynamic_style_inject.html", 200, 140, true)
	assertPixel(t, off, 5, 10, 1, 1, 1, "no-js: #t keeps base colour")
	assertPixel(t, off, 5, 60, 255, 255, 255, "no-js: #t stays 20px tall")

	checkGolden(t, on, "dynamic_style_inject.png")
}

// TestDynamicScriptInjectGolden proves a dynamically appended inline <script> is
// executed in the same runtime and its DOM mutation reaches the final render.
func TestDynamicScriptInjectGolden(t *testing.T) {
	on := renderFixtureJS(t, "dynamic_script_inject.html", 160, 120, false)
	assertPixel(t, on, 5, 30, 40, 50, 60, "js: injected script recolours #host")

	off := renderFixtureJS(t, "dynamic_script_inject.html", 160, 120, true)
	assertPixel(t, off, 5, 30, 1, 2, 3, "no-js: #host keeps base colour")

	checkGolden(t, on, "dynamic_script_inject.png")
}
