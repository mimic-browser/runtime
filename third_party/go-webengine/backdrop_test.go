// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package engine

import (
	"context"
	"image"
	"testing"

	"github.com/go-webengine/engine/css"
)

// pixel returns the RGBA at (x,y).
func pixelRGBA(img *image.RGBA, x, y int) (r, g, b, a uint8) {
	o := img.PixOffset(x, y)
	return img.Pix[o], img.Pix[o+1], img.Pix[o+2], img.Pix[o+3]
}

// TestBackdropDefaultsWhite: with no Backdrop, a page with no background of its
// own paints on white paper.
func TestBackdropDefaultsWhite(t *testing.T) {
	img, _, err := New().RenderHTML(context.Background(), `<html><body></body></html>`, "https://x/", image.Rect(0, 0, 40, 30))
	if err != nil {
		t.Fatal(err)
	}
	if r, g, b, _ := pixelRGBA(img, 5, 5); r != 255 || g != 255 || b != 255 {
		t.Errorf("default base = (%d,%d,%d), want white", r, g, b)
	}
}

// TestBackdropFillsBareCanvas: a set Backdrop paints under a page that declares
// no background (a bare image / empty body) — so a dark host frames it dark.
func TestBackdropFillsBareCanvas(t *testing.T) {
	e := New()
	e.Backdrop = css.Color{R: 0x1e, G: 0x1e, B: 0x22, A: 0xff}
	img, _, err := e.RenderHTML(context.Background(), `<html><body></body></html>`, "https://x/", image.Rect(0, 0, 40, 30))
	if err != nil {
		t.Fatal(err)
	}
	if r, g, b, a := pixelRGBA(img, 5, 5); r != 0x1e || g != 0x1e || b != 0x22 || a != 0xff {
		t.Errorf("backdrop base = (%d,%d,%d,%d), want the configured dark colour", r, g, b, a)
	}
}

// TestBackdropYieldsToPageBackground: a page that declares its own background
// still paints it over the Backdrop, so themed hosts don't recolour normal sites.
func TestBackdropYieldsToPageBackground(t *testing.T) {
	e := New()
	e.Backdrop = css.Color{R: 0x1e, G: 0x1e, B: 0x22, A: 0xff}
	img, _, err := e.RenderHTML(context.Background(), `<html><body style="background:#ff0000"></body></html>`, "https://x/", image.Rect(0, 0, 40, 30))
	if err != nil {
		t.Fatal(err)
	}
	if r, g, b, _ := pixelRGBA(img, 5, 5); r != 0xff || g != 0 || b != 0 {
		t.Errorf("page background = (%d,%d,%d), want red (page bg wins over backdrop)", r, g, b)
	}
}
