// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package engine

import (
	"bytes"
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-webengine/engine/css"
)

// Three 16×12 images of the same gradient: a JPEG, a lossy WebP and an
// RGBA PNG (generated with Pillow; quality 80 for the lossy two).
const (
	fxJPEG     = "/9j/4AAQSkZJRgABAQAAAQABAAD/2wBDAAYEBQYFBAYGBQYHBwYIChAKCgkJChQODwwQFxQYGBcUFhYaHSUfGhsjHBYWICwgIyYnKSopGR8tMC0oMCUoKSj/2wBDAQcHBwoIChMKChMoGhYaKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCj/wAARCAAMABADASIAAhEBAxEB/8QAHwAAAQUBAQEBAQEAAAAAAAAAAAECAwQFBgcICQoL/8QAtRAAAgEDAwIEAwUFBAQAAAF9AQIDAAQRBRIhMUEGE1FhByJxFDKBkaEII0KxwRVS0fAkM2JyggkKFhcYGRolJicoKSo0NTY3ODk6Q0RFRkdISUpTVFVWV1hZWmNkZWZnaGlqc3R1dnd4eXqDhIWGh4iJipKTlJWWl5iZmqKjpKWmp6ipqrKztLW2t7i5usLDxMXGx8jJytLT1NXW19jZ2uHi4+Tl5ufo6erx8vP09fb3+Pn6/8QAHwEAAwEBAQEBAQEBAQAAAAAAAAECAwQFBgcICQoL/8QAtREAAgECBAQDBAcFBAQAAQJ3AAECAxEEBSExBhJBUQdhcRMiMoEIFEKRobHBCSMzUvAVYnLRChYkNOEl8RcYGRomJygpKjU2Nzg5OkNERUZHSElKU1RVVldYWVpjZGVmZ2hpanN0dXZ3eHl6goOEhYaHiImKkpOUlZaXmJmaoqOkpaanqKmqsrO0tba3uLm6wsPExcbHyMnK0tPU1dbX2Nna4uPk5ebn6Onq8vP09fb3+Pn6/9oADAMBAAIRAxEAPwDzXSfCn3f3f6V2ek+FPu/u/wBK7fSdOtuPkrs9J0624+Sljs7qanPwrxFV90//2Q=="
	fxWebP     = "UklGRloAAABXRUJQVlA4IE4AAADQAQCdASoQAAwAAUAmJbACdAEOtXQyAAD+/bOzjTlhXS6AZn+GESZhx+//EivNcLPRNg/06ub6ev165//0sN+7//+kHkwYKQ/meILIAAA="
	fxPNGAlpha = "iVBORw0KGgoAAAANSUhEUgAAABAAAAAMCAYAAABr5z2BAAAAIklEQVR4nGM8ISfXwEABYKJE86gBEMDEQCFgGjWAgeIwAADCqgGcPWPU6QAAAABJRU5ErkJggg=="
)

func b64(s string) []byte { b, _ := base64.StdEncoding.DecodeString(s); return b }

// TestLoadImageSetKeepsSourceBytesAndFormat serves the three fixtures and
// checks that LoadImageSet returns, per <img>, the exact bytes fetched,
// their sniffed format and lossiness, the source pixel size, and the same
// size/bitmap LoadImages returns.
func TestLoadImageSetKeepsSourceBytesAndFormat(t *testing.T) {
	files := map[string][]byte{"/a.jpg": b64(fxJPEG), "/b.webp": b64(fxWebP), "/c.png": b64(fxPNGAlpha)}
	mux := http.NewServeMux()
	for path, data := range files {
		path, data := path, data
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) { w.Write(data) })
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><body><img src="/a.jpg"><img src="/b.webp"><img src="/c.png"><svg width="10" height="10"><rect width="10" height="10"/></svg></body></html>`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	e := New()
	e.Client = srv.Client()
	doc, err := e.Fetch(context.Background(), srv.URL+"/")
	if err != nil {
		t.Fatal(err)
	}
	sm := css.CascadeVW(doc.Root, 1024, nil)
	set := e.LoadImageSet(context.Background(), doc, sm, 1024)
	sizes, bitmaps := e.LoadImages(context.Background(), doc, sm, 1024)
	if len(set) != 4 || len(sizes) != 4 {
		t.Fatalf("loaded %d in the set, %d in the maps; want 4 and 4", len(set), len(sizes))
	}
	want := map[string]struct {
		format string
		lossy  bool
	}{"/a.jpg": {"jpeg", true}, "/b.webp": {"webp", true}, "/c.png": {"png", false}}
	for n, li := range set {
		if n.Tag == "svg" {
			// An inline <svg> keeps its own serialisation in Data (#229) so a
			// consumer needing another density than the CSS-pixel raster can
			// render it again rather than upscaling pixels.
			if li.Format != "svg" || li.Bitmap == nil {
				t.Errorf("inline svg: format %q bitmap %v", li.Format, li.Bitmap != nil)
			}
			if !bytes.Contains(li.Data, []byte("<svg")) {
				t.Errorf("inline svg: Data is %d bytes and carries no <svg> root", len(li.Data))
			}
			continue
		}
		src, _ := n.Attribute("src")
		w := want[src]
		if li.Format != w.format || li.Lossy != w.lossy {
			t.Errorf("%s: format %q lossy %v, want %q %v", src, li.Format, li.Lossy, w.format, w.lossy)
		}
		if !bytes.Equal(li.Data, files[src]) {
			t.Errorf("%s: Data is not the fetched bytes (%d vs %d)", src, len(li.Data), len(files[src]))
		}
		if li.SourceW != 16 || li.SourceH != 12 {
			t.Errorf("%s: source %dx%d, want 16x12", src, li.SourceW, li.SourceH)
		}
		if b := li.Bitmap.Bounds(); b.Dx() != 16 || b.Dy() != 12 || li.Size != [2]float64{16, 12} {
			t.Errorf("%s: bitmap %v size %v, want 16x12", src, b, li.Size)
		}
		if sizes[n] != li.Size {
			t.Errorf("%s: LoadImages and LoadImageSet disagree on size", src)
		}
		if bitmaps[n].Bounds() != li.Bitmap.Bounds() {
			t.Errorf("%s: LoadImages and LoadImageSet disagree on bitmap", src)
		}
	}
}

func TestSniffImageFormat(t *testing.T) {
	cases := []struct {
		data   []byte
		format string
		lossy  bool
	}{
		{b64(fxJPEG), "jpeg", true},
		{b64(fxWebP), "webp", true},
		{b64(fxPNGAlpha), "png", false},
		{[]byte("RIFF\x00\x00\x00\x00WEBPVP8L\x00"), "webp", false},
		{[]byte("GIF89a"), "gif", false},
		{[]byte("BM\x00"), "bmp", false},
		{[]byte("<svg/>"), "", false},
		{nil, "", false},
	}
	for _, c := range cases {
		if f, l := sniffImageFormat(c.data); f != c.format || l != c.lossy {
			t.Errorf("sniff(%.12q) = %q,%v want %q,%v", c.data, f, l, c.format, c.lossy)
		}
	}
}
