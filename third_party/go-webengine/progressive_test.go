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

	"github.com/go-webengine/engine/dom"
)

// progressiveDoc loads an offline fixture into a fresh Document (a fresh parse,
// so JS mutations from one render never leak into another).
func progressiveDoc(t *testing.T, name string) *Document {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	root, err := dom.Parse(string(data))
	if err != nil {
		t.Fatal(err)
	}
	return &Document{URL: "https://fixture.test/", Title: dom.Title(root), Root: root, HTML: string(data)}
}

// TestProgressiveScriptEmitsStagedFrames covers the frame protocol on a
// script-heavy page: onFrame fires at least twice, the first frame is "initial"
// and precedes any "settle"/"final", exactly one frame is Final and it is last,
// and the geometry grows from the initial frame to the final one (the script's
// appended content appears progressively).
func TestProgressiveScriptEmitsStagedFrames(t *testing.T) {
	e := New()
	doc := progressiveDoc(t, "progressive_script.html")
	var frames []ProgressiveFrame
	info, err := e.RenderDocumentProgressive(context.Background(), doc, image.Rect(0, 0, 400, 120),
		func(f ProgressiveFrame) { frames = append(frames, f) })
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) < 2 {
		t.Fatalf("expected >= 2 frames, got %d", len(frames))
	}
	if frames[0].Stage != "initial" {
		t.Errorf("first frame stage = %q, want initial", frames[0].Stage)
	}
	// No "settle"/"final" may appear before the first "initial"; exactly one
	// Final, and it is the last frame.
	sawInitial := false
	finalCount := 0
	for i, f := range frames {
		if f.Stage == "initial" {
			sawInitial = true
		} else if !sawInitial {
			t.Errorf("frame %d stage %q appeared before any initial", i, f.Stage)
		}
		if f.Final {
			finalCount++
			if i != len(frames)-1 {
				t.Errorf("Final frame at index %d is not last (%d frames)", i, len(frames))
			}
			if f.Stage != "final" {
				t.Errorf("Final frame stage = %q, want final", f.Stage)
			}
		}
		if f.Img == nil {
			t.Errorf("frame %d has nil Img", i)
		}
	}
	if finalCount != 1 {
		t.Errorf("Final==true count = %d, want 1", finalCount)
	}
	// The appended content must make the final frame taller than the initial one.
	initH := frames[0].Info.ContentHeight
	finalH := frames[len(frames)-1].Info.ContentHeight
	if finalH <= initH {
		t.Errorf("final height %d not greater than initial %d (script content did not appear progressively)", finalH, initH)
	}
	if info.ContentHeight != finalH {
		t.Errorf("returned info height %d != final frame height %d", info.ContentHeight, finalH)
	}
	// Each frame's image is an independent allocation.
	for i := 1; i < len(frames); i++ {
		if frames[i].Img == frames[i-1].Img {
			t.Errorf("frames %d and %d share the same *image.RGBA", i-1, i)
		}
	}
}

// TestProgressiveFinalEqualsRenderWithLinks asserts the Final frame is
// byte-identical (pixels + links + info) to RenderWithLinks for the same input.
func TestProgressiveFinalEqualsRenderWithLinks(t *testing.T) {
	e := New()
	vp := image.Rect(0, 0, 400, 120)

	// Batch path (fresh parse).
	batch := progressiveDoc(t, "progressive_script.html")
	wantImg, wantInfo, wantLinks, err := e.RenderDocumentWithLinks(context.Background(), batch, vp)
	if err != nil {
		t.Fatal(err)
	}

	// Progressive path (fresh parse) — keep only the Final frame.
	prog := progressiveDoc(t, "progressive_script.html")
	var final ProgressiveFrame
	haveFinal := false
	_, err = e.RenderDocumentProgressive(context.Background(), prog, vp, func(f ProgressiveFrame) {
		if f.Final {
			final, haveFinal = f, true
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if !haveFinal {
		t.Fatal("no final frame emitted")
	}
	if final.Img.Rect != wantImg.Rect {
		t.Fatalf("final image bounds %v != batch %v", final.Img.Rect, wantImg.Rect)
	}
	if !bytes.Equal(final.Img.Pix, wantImg.Pix) {
		t.Error("final frame pixels differ from RenderWithLinks")
	}
	if len(final.Links) != len(wantLinks) {
		t.Fatalf("final links = %d, batch = %d", len(final.Links), len(wantLinks))
	}
	for i := range wantLinks {
		if final.Links[i] != wantLinks[i] {
			t.Errorf("link %d: progressive %+v != batch %+v", i, final.Links[i], wantLinks[i])
		}
	}
	if final.Info.ContentHeight != wantInfo.ContentHeight || final.Info.Title != wantInfo.Title || final.Info.URL != wantInfo.URL {
		t.Errorf("final info %+v != batch %+v", final.Info, wantInfo)
	}
}

// TestProgressiveStaticInitialEqualsFinal covers a static (no-script) page: a
// progressive render emits an initial and a final frame with identical geometry
// and identical pixels.
func TestProgressiveStaticInitialEqualsFinal(t *testing.T) {
	e := New()
	doc := progressiveDoc(t, "progressive_static.html")
	var frames []ProgressiveFrame
	_, err := e.RenderDocumentProgressive(context.Background(), doc, image.Rect(0, 0, 400, 120),
		func(f ProgressiveFrame) { frames = append(frames, f) })
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 2 {
		t.Fatalf("static page emitted %d frames, want 2 (initial+final)", len(frames))
	}
	if frames[0].Stage != "initial" || frames[1].Stage != "final" || !frames[1].Final {
		t.Errorf("stages = %q,%q (final=%v), want initial,final(true)", frames[0].Stage, frames[1].Stage, frames[1].Final)
	}
	if frames[0].Info.ContentHeight != frames[1].Info.ContentHeight {
		t.Errorf("static initial height %d != final %d", frames[0].Info.ContentHeight, frames[1].Info.ContentHeight)
	}
	if !bytes.Equal(frames[0].Img.Pix, frames[1].Img.Pix) {
		t.Error("static initial and final frames differ in pixels")
	}
}

// TestRenderProgressiveFetchError propagates a fetch failure (bad URL) instead
// of invoking onFrame.
func TestRenderProgressiveFetchError(t *testing.T) {
	called := false
	_, err := New().RenderProgressive(context.Background(), "://bad", image.Rect(0, 0, 10, 10),
		func(ProgressiveFrame) { called = true })
	if err == nil {
		t.Error("expected fetch error for bad URL")
	}
	if called {
		t.Error("onFrame must not be called on fetch error")
	}
}
