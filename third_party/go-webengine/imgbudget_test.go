// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package engine

import (
	"context"
	"image/color"
	"strings"
	"testing"

	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/dom"
)

// imgNodes returns every <img> element in document order.
func imgNodes(root *dom.Node) []*dom.Node {
	var out []*dom.Node
	var walk func(n *dom.Node)
	walk = func(n *dom.Node) {
		if n.Type == dom.Element && n.Tag == "img" {
			out = append(out, n)
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	return out
}

const svgIcon = `<svg xmlns="http://www.w3.org/2000/svg" width="8" height="8"><rect width="8" height="8"/></svg>`

// TestImageVectorBudgetDoesNotStarveRaster is the regression for the DeepMind
// black-void bug: a wall of decorative vector icons (inline <svg> and
// <img src="*.svg">) ahead of the content raster photo must NOT consume the
// raster budget, so the raster image still loads.
func TestImageVectorBudgetDoesNotStarveRaster(t *testing.T) {
	e := New() // MaxImages=40, MaxVectorImages=400
	dataPNG := pngDataURI(t, 4, 4, color.RGBA{200, 100, 50, 255})

	// 45 leading vector icons (> MaxImages) then one raster hero.
	var b strings.Builder
	b.WriteString(`<html><body>`)
	for i := 0; i < 45; i++ {
		b.WriteString(svgIcon)
	}
	b.WriteString(`<img id="hero" src="` + dataPNG + `">`)
	b.WriteString(`</body></html>`)

	root, err := dom.Parse(b.String())
	if err != nil {
		t.Fatal(err)
	}
	doc := &Document{URL: "https://x.test/", Root: root}
	sizes, bitmaps := e.loadImages(context.Background(), doc, css.Cascade(root), 900)

	imgs := imgNodes(root)
	if len(imgs) != 1 {
		t.Fatalf("expected 1 <img>, got %d", len(imgs))
	}
	hero := imgs[0]
	if _, ok := bitmaps[hero]; !ok {
		t.Error("raster hero <img> not loaded — decorative SVG icons starved the raster budget")
	}
	if _, ok := sizes[hero]; !ok {
		t.Error("raster hero <img> has no intrinsic size")
	}
	// The leading vector icons loaded under their own (generous) budget.
	if len(bitmaps) < 46 {
		t.Errorf("expected 45 icons + 1 hero = 46 bitmaps, got %d", len(bitmaps))
	}
}

// TestImageRasterBudgetStillBounds confirms the raster budget still caps raster
// <img> decodes (so the split does not make raster loading unbounded).
func TestImageRasterBudgetStillBounds(t *testing.T) {
	e := New()
	e.MaxImages = 5
	dataPNG := pngDataURI(t, 3, 3, color.RGBA{10, 20, 30, 255})
	var b strings.Builder
	b.WriteString(`<html><body>`)
	for i := 0; i < 8; i++ { // 8 raster imgs, budget 5
		b.WriteString(`<img src="` + dataPNG + `">`)
	}
	b.WriteString(`</body></html>`)
	root, _ := dom.Parse(b.String())
	doc := &Document{URL: "https://x.test/", Root: root}
	_, bitmaps := e.loadImages(context.Background(), doc, css.Cascade(root), 900)
	if len(bitmaps) != 5 {
		t.Errorf("raster budget 5 loaded %d images, want 5", len(bitmaps))
	}
}

// TestImageVectorBudgetBounds confirms the vector budget caps SVG rasterisation.
func TestImageVectorBudgetBounds(t *testing.T) {
	e := New()
	e.MaxVectorImages = 3
	var b strings.Builder
	b.WriteString(`<html><body>`)
	for i := 0; i < 6; i++ {
		b.WriteString(svgIcon)
	}
	b.WriteString(`</body></html>`)
	root, _ := dom.Parse(b.String())
	doc := &Document{URL: "https://x.test/", Root: root}
	_, bitmaps := e.loadImages(context.Background(), doc, css.Cascade(root), 900)
	if len(bitmaps) != 3 {
		t.Errorf("vector budget 3 loaded %d svgs, want 3", len(bitmaps))
	}
}
