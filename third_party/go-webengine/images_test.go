// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-webengine/engine/css"
)

func TestCSSImageSize(t *testing.T) {
	// Intrinsic 900x298 (the go.dev Google wordmark shape).
	cases := []struct {
		name       string
		st         *css.Style
		iw, ih     int
		wantW      int
		wantH      int
		wantOK     bool
	}{
		{"nil style keeps intrinsic", nil, 900, 298, 0, 0, false},
		{"no CSS dims keeps intrinsic",
			&css.Style{Width: css.Length{Auto: true}, Height: css.Length{Auto: true}}, 900, 298, 0, 0, false},
		{"height only scales width by aspect",
			&css.Style{Width: css.Length{Auto: true}, Height: css.Length{Px: 24}}, 900, 298, 72, 24, true},
		{"width only scales height by aspect",
			&css.Style{Width: css.Length{Px: 100}, Height: css.Length{Auto: true}}, 200, 100, 100, 50, true},
		{"both axes set uses both",
			&css.Style{Width: css.Length{Px: 120}, Height: css.Length{Px: 40}}, 900, 298, 120, 40, true},
		{"percentage width is ignored (needs containing block)",
			&css.Style{Width: css.Length{Percent: 0.5, IsPercent: true}, Height: css.Length{Auto: true}}, 900, 298, 0, 0, false},
		{"zero intrinsic reports false",
			&css.Style{Height: css.Length{Px: 24}}, 0, 100, 0, 0, false},
	}
	for _, c := range cases {
		w, h, ok := cssImageSize(c.st, c.iw, c.ih)
		if ok != c.wantOK || (ok && (w != c.wantW || h != c.wantH)) {
			t.Errorf("%s: got (%d,%d,%v) want (%d,%d,%v)", c.name, w, h, ok, c.wantW, c.wantH, c.wantOK)
		}
	}
}

func TestIRound(t *testing.T) {
	cases := map[float64]int{0.0: 1, 0.4: 1, 0.6: 1, 1.4: 1, 1.6: 2, 71.5: 72, 72.4: 72}
	for in, want := range cases {
		if got := iround(in); got != want {
			t.Errorf("iround(%v) = %d want %d", in, got, want)
		}
	}
}

// TestImgByteCacheDedupesWithinRender is the regression guard for the
// duplicate-fetch bug found live on caniuse.com: the settle loop reloads
// images after any script-driven relayout (dynamic.go), so without a
// render-scoped cache the SAME url was fetched over the network again even
// though nothing about it had changed — three images cost a second full
// round trip apiece for no reason (measured: ~500ms wasted on that page
// alone). e.ImageCache is opt-in and nil by default (cross-render reuse is a
// separate, deliberate choice — see its doc comment), so this cache must
// work with no ImageCache configured at all.
func TestImgByteCacheDedupesWithinRender(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Write([]byte("pretend-image-bytes"))
	}))
	defer srv.Close()

	e := New()
	e.Client = srv.Client()
	ctx := withImgByteCache(context.Background(), newImgByteCache())

	for i := 0; i < 3; i++ {
		data, ok := e.fetchImageBytes(ctx, srv.URL+"/", "/pic.png")
		if !ok {
			t.Fatalf("call %d: fetch failed", i)
		}
		if string(data) != "pretend-image-bytes" {
			t.Fatalf("call %d: data = %q", i, data)
		}
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("server saw %d requests, want exactly 1 (later calls should hit the render cache)", got)
	}
}

// TestImgByteCacheScopedPerContextNotGlobal proves the cache is NOT shared
// across renders that don't share a context — e.ImageCache (nil by default)
// governs cross-render reuse, not this mechanism; a fresh render must still
// fetch, exactly as before this fix.
func TestImgByteCacheScopedPerContextNotGlobal(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Write([]byte("pretend-image-bytes"))
	}))
	defer srv.Close()

	e := New()
	e.Client = srv.Client()

	for i := 0; i < 2; i++ {
		ctx := withImgByteCache(context.Background(), newImgByteCache())
		if _, ok := e.fetchImageBytes(ctx, srv.URL+"/", "/pic.png"); !ok {
			t.Fatalf("render %d: fetch failed", i)
		}
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Errorf("server saw %d requests, want 2 (one per independent render)", got)
	}
}

// TestButtonIconOnlyRendersIcon is the end-to-end counterpart of the
// layout- and paint-package unit tests for the same fix: a <button> whose
// entire content is a single img/svg child and no visible text (e.g.
// developer.mozilla.org's nav search button, pkg.go.dev's search-submit
// button) used to size to a tiny padding-only box and paint nothing inside
// it at all — the button was laid out as an atomic label-text-only box
// (layout.formControlDefaultSize's pre-existing "button" case), discarding
// any non-text child regardless of how correctly loadImages fetched and
// rasterised it (a separate, already-fixed bug — see
// TestShadowDOMInlineSVGIsDiscoveredForRasterization in shadowdom_test.go —
// that on its own was NOT sufficient to fix this: a plain, shadow-DOM-free
// button with an svg child lost its icon exactly the same way).
func TestButtonIconOnlyRendersIcon(t *testing.T) {
	src := `<html><body style="margin:0;background:#000">` +
		`<button style="width:30px;height:30px">` +
		`<svg width="16" height="16" viewBox="0 0 16 16"><rect width="16" height="16" fill="#ff0000"/></svg>` +
		`</button></body></html>`
	img := renderHTMLTest(t, src, 50, 50)
	assertPixel(t, img, 15, 15, 0xff, 0x00, 0x00, "icon-only button should paint its svg child's bitmap, not leave it empty")
}

// TestRetryAfterDelay covers retryAfterDelay's parsing of the Retry-After
// header's delay-seconds form (the only form observed live — see
// doWithRateLimitRetry's doc comment), independent of any network I/O.
func TestRetryAfterDelay(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want time.Duration
	}{
		{"empty falls back to default", "", defaultRateLimitBackoff},
		{"plain seconds", "2", 2 * time.Second},
		{"zero seconds is honoured", "0", 0},
		{"negative falls back to default", "-1", defaultRateLimitBackoff},
		{"non-numeric falls back to default", "Wed, 21 Oct type date", defaultRateLimitBackoff},
		{"surrounding whitespace trimmed", "  3  ", 3 * time.Second},
		{"absurd value clamped to ceiling", "600", maxRateLimitBackoff},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := retryAfterDelay(c.in); got != c.want {
				t.Errorf("retryAfterDelay(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

// TestFetchImageBytesRetriesOn429 guards the actual bug: en.wikipedia.org's
// upload.wikimedia.org CDN replies 429 Too Many Requests (with a real
// Retry-After header) to some fraction of this engine's own concurrent
// per-page image fetches on an image-heavy article, so a real, fetchable
// image was silently and non-deterministically dropped — confirmed live,
// the infobox logo was missing in 2 of 3 renders of the same page. A bare
// httptest.Server standing in for the CDN returns 429 for the first two
// requests (Retry-After: 0, so the test runs fast) then 200 with real bytes
// on the third; fetchImageBytes must retry through the 429s and return the
// eventual success.
func TestFetchImageBytesRetriesOn429(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&hits, 1)
		if n <= 2 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Write([]byte("real-image-bytes"))
	}))
	defer srv.Close()

	e := New()
	e.Client = srv.Client()
	ctx := withImgByteCache(context.Background(), newImgByteCache())
	data, ok := e.fetchImageBytes(ctx, srv.URL+"/", "/pic.png")
	if !ok {
		t.Fatal("fetchImageBytes failed after retrying through 429s")
	}
	if string(data) != "real-image-bytes" {
		t.Errorf("data = %q, want the eventual 200 body", data)
	}
	if got := atomic.LoadInt32(&hits); got != 3 {
		t.Errorf("server saw %d requests, want exactly 3 (2 x 429 + 1 x 200)", got)
	}
}

// TestFetchImageBytesGivesUpAfterMaxRetries covers the OTHER half of the same
// fix: a host that is persistently (not just momentarily) rate-limiting must
// not turn into an unbounded retry loop — fetchImageBytes gives up after
// maxRateLimitRetries and reports failure, exactly as it already did for any
// other non-200 status, rather than hanging the render.
func TestFetchImageBytesGivesUpAfterMaxRetries(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	e := New()
	e.Client = srv.Client()
	ctx := withImgByteCache(context.Background(), newImgByteCache())
	_, ok := e.fetchImageBytes(ctx, srv.URL+"/", "/pic.png")
	if ok {
		t.Fatal("fetchImageBytes should fail once a host is persistently rate-limited, not retry forever")
	}
	if want := int32(1 + maxRateLimitRetries); atomic.LoadInt32(&hits) != want {
		t.Errorf("server saw %d requests, want exactly %d (1 initial + %d retries)", hits, want, maxRateLimitRetries)
	}
}
