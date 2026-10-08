// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package engine

import (
	"bytes"
	"context"
	"image"
	"os"
	"path/filepath"
	"testing"
)

// renderFixture renders a testdata HTML fixture offline at the given viewport.
func renderFixture(t *testing.T, name string, w, h int) *image.RGBA {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	img, _, err := New().RenderHTML(context.Background(), string(src), "https://demo.test/", image.Rect(0, 0, w, h))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	return img
}

// assertPixel asserts an exact RGB colour at (x,y).
func assertPixel(t *testing.T, img *image.RGBA, x, y int, r, g, b uint8, what string) {
	t.Helper()
	c := img.RGBAAt(x, y)
	if c.R != r || c.G != g || c.B != b {
		t.Errorf("%s: pixel (%d,%d) = #%02x%02x%02x, want #%02x%02x%02x", what, x, y, c.R, c.G, c.B, r, g, b)
	}
}

// checkGolden compares png bytes to testdata/golden/<name>, writing it when
// UPDATE_GOLDEN is set.
func checkGolden(t *testing.T, img *image.RGBA, name string) {
	t.Helper()
	png, err := EncodePNG(img)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("testdata", "golden", name)
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, png, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("golden %s updated", name)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (run with UPDATE_GOLDEN=1 to create): %v", err)
	}
	if !bytes.Equal(png, want) {
		t.Errorf("render does not match golden %s (%d vs %d bytes)", name, len(png), len(want))
	}
}

// TestPositionDemoGolden verifies CSS position relative/absolute/fixed and
// z-index stacking with exact painted-pixel invariants (the fixture uses only
// solid colour blocks, so its bytes are font-independent and byte-golden-safe).
func TestPositionDemoGolden(t *testing.T) {
	img := renderFixture(t, "position_demo.html", 400, 600)

	// Fixed bar painted at the document origin (0,0)-(300,24).
	assertPixel(t, img, 150, 12, 0x1f, 0x6f, 0xeb, "fixed bar")
	// The fixed bar reserved no flow space: the positioned ancestor #anchor still
	// begins at its own margin-top (y=60), so a point just inside it is anchor
	// background, not white and not shifted down by the bar's 24px.
	assertPixel(t, img, 200, 61, 0xe6, 0xed, 0xf3, "anchor top (fixed reserved no space)")
	assertPixel(t, img, 300, 240, 0xe6, 0xed, 0xf3, "anchor background")
	// Absolute box against #anchor's padding box (origin 40,60 + padding 20 is the
	// content box; padding box origin is 40,60; abs offset 10,10 → 50,70).
	assertPixel(t, img, 110, 100, 0x2d, 0xa4, 0x4e, "absolute box")
	// Relative box painted shifted (its slot is inside #anchor content, shifted
	// by top:90 left:20).
	assertPixel(t, img, 130, 190, 0xbf, 0x39, 0x89, "relative box")
	// z-index: #high (z:5) overlaps #low (z:1) exactly; the higher one wins even
	// though it comes first in source order → yellow, not red.
	assertPixel(t, img, 100, 360, 0xe3, 0xb3, 0x41, "z-index high on top")

	checkGolden(t, img, "position_demo.png")
}

// hiddenColorPresent reports whether any pixel matches the given RGB.
func hiddenColorPresent(img *image.RGBA, r, g, b uint8) bool {
	for y := img.Rect.Min.Y; y < img.Rect.Max.Y; y++ {
		for x := img.Rect.Min.X; x < img.Rect.Max.X; x++ {
			c := img.RGBAAt(x, y)
			if c.R == r && c.G == g && c.B == b {
				return true
			}
		}
	}
	return false
}

// TestDropdownHoverDemoHidden verifies the go.dev-class pattern: a submenu
// hidden until :hover and a panel hidden until :focus-within both stay hidden in
// a static render, reserving no space, so the article starts right under the nav.
func TestDropdownHoverDemoHidden(t *testing.T) {
	img := renderFixture(t, "dropdown_hover_demo.html", 400, 300)

	// Nav bar background at the top.
	assertPixel(t, img, 200, 20, 0x0b, 0x3d, 0x2e, "nav bar")
	// The article background begins immediately below the 40px nav (y=40), not
	// pushed down by an expanded dropdown.
	assertPixel(t, img, 5, 45, 0xf6, 0xf8, 0xfa, "article starts under nav")

	// Neither the :hover submenu (#d0f0e0) nor the :focus-within panel (#ffd0d0)
	// appears anywhere: the dynamic pseudo-classes never match statically.
	if hiddenColorPresent(img, 0xd0, 0xf0, 0xe0) {
		t.Error(":hover submenu background must not be painted in a static render")
	}
	if hiddenColorPresent(img, 0xff, 0xd0, 0xd0) {
		t.Error(":focus-within panel background must not be painted in a static render")
	}

	checkGolden(t, img, "dropdown_hover_demo.png")
}
