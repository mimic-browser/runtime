// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package engine

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/go-opentype/fonts/cabin"
	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/paint"
)

// fontServer serves one TTF at /f.ttf and counts the requests for it.
func fontServer(t *testing.T) (*httptest.Server, func() int) {
	t.Helper()
	var mu sync.Mutex
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/f.ttf":
			mu.Lock()
			n++
			mu.Unlock()
			w.Header().Set("Content-Type", "font/ttf")
			_, _ = w.Write(cabin.TTF)
		case "/missing.ttf":
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, func() int {
		mu.Lock()
		defer mu.Unlock()
		return n
	}
}

// The whole path: a sheet's @font-face rule, the file fetched, the face
// registered, and the MEASUREMENT changed by it — which is the only thing
// that makes the typeface really the document's own.
func TestLoadFontFacesFetchesRegistersAndMeasures(t *testing.T) {
	srv, hits := fontServer(t)
	sheet := `@font-face { font-family: 'Cabin Web'; font-weight: 400; src: url(` + srv.URL + `/f.ttf) format('truetype'); }`
	doc := &Document{URL: srv.URL + "/page.html"}
	e := New()
	faces := e.LoadFontFaces(context.Background(), doc, []string{sheet}, css.Media{Width: 1024})
	if len(faces) != 1 {
		t.Fatalf("got %d faces, want 1", len(faces))
	}
	if faces[0].Family != "cabin web" || faces[0].Weight != 400 || faces[0].Italic {
		t.Errorf("face = %+v", faces[0])
	}
	if len(faces[0].Data) != len(cabin.TTF) {
		t.Errorf("Data is %d bytes, want the %d fetched", len(faces[0].Data), len(cabin.TTF))
	}
	if hits() != 1 {
		t.Errorf("fetched the file %d times, want 1", hits())
	}
	fonts := paint.NewFonts()
	fam := css.FontFamily{Names: "cabin web", Generic: css.GenericSans}
	before := fonts.Measure("Partenaires", fam, 40, 400, false)
	if n := RegisterFontFaces(fonts, faces); n != 1 {
		t.Fatalf("registered %d faces, want 1", n)
	}
	if after := fonts.Measure("Partenaires", fam, 40, 400, false); after == before {
		t.Errorf("the registered face did not change the measurement (%v)", before)
	}
}

// A rule whose file cannot be had leaves the page on its bundled family
// rather than half-loading a face.
func TestLoadFontFacesSkipsWhatItCannotDecode(t *testing.T) {
	srv, _ := fontServer(t)
	doc := &Document{URL: srv.URL + "/page.html"}
	e := New()
	for _, sheet := range []string{
		`@font-face { font-family: W; src: url(` + srv.URL + `/f.woff2) format('woff2'); }`,
		`@font-face { font-family: W; src: url(` + srv.URL + `/missing.ttf) format('truetype'); }`,
		`@font-face { font-family: W; src: url(data:font/ttf;base64,bm90IGEgZm9udA==) format('truetype'); }`,
	} {
		if faces := e.LoadFontFaces(context.Background(), doc, []string{sheet}, css.Media{}); len(faces) != 0 {
			t.Errorf("%q gave %d faces, want none", sheet, len(faces))
		}
	}
	// A file too short to carry a signature is refused; a WOFF or WOFF2
	// wrapper is NOT, since go-opentype unwraps both itself now — the whole
	// point, WOFF2 being what a font service serves a browser.
	if _, ok := decodeFontFile([]byte("ab")); ok {
		t.Error("a file too short for a signature must be refused")
	}
	for _, sig := range []string{"wOFFxxxx", "wOF2xxxx"} {
		if _, ok := decodeFontFile([]byte(sig)); !ok {
			t.Errorf("%q must reach the parser rather than be refused here", sig[:4])
		}
	}
}

// src entries are tried in order, so a woff2 first choice falls through to the
// truetype beside it — which is exactly how a font service's sheet is written.
func TestLoadFontFacesFallsThroughTheSrcList(t *testing.T) {
	srv, _ := fontServer(t)
	sheet := `@font-face { font-family: F; src: url(` + srv.URL + `/f.woff2) format('woff2'), url(` + srv.URL + `/f.ttf) format('truetype'); }`
	faces := New().LoadFontFaces(context.Background(), &Document{URL: srv.URL + "/p.html"}, []string{sheet}, css.Media{})
	if len(faces) != 1 || len(faces[0].Data) != len(cabin.TTF) {
		t.Fatalf("faces = %+v", faces)
	}
}

// A data: URI src, which is how a self-contained document carries its own
// typefaces with no network at all.
func TestLoadFontFacesReadsADataURI(t *testing.T) {
	uri := "data:font/ttf;base64," + base64.StdEncoding.EncodeToString(cabin.TTF)
	sheet := `@font-face { font-family: Inline; src: url(` + uri + `); }`
	faces := New().LoadFontFaces(context.Background(), &Document{}, []string{sheet}, css.Media{})
	if len(faces) != 1 || faces[0].Family != "inline" {
		t.Fatalf("faces = %+v", faces)
	}
}

// One fetch per slot: a second rule for the same family, weight and slant is
// the same face, and a page that repeats it should not pay twice.
func TestLoadFontFacesFetchesEachSlotOnce(t *testing.T) {
	srv, hits := fontServer(t)
	rule := `@font-face { font-family: F; font-weight: 400; src: url(` + srv.URL + `/f.ttf); }`
	faces := New().LoadFontFaces(context.Background(), &Document{URL: srv.URL + "/p.html"}, []string{rule, rule}, css.Media{})
	if len(faces) != 1 {
		t.Errorf("got %d faces, want 1", len(faces))
	}
	if hits() != 1 {
		t.Errorf("fetched %d times, want 1", hits())
	}
}

// Distinct slots of one family are distinct faces.
func TestLoadFontFacesKeepsDistinctSlots(t *testing.T) {
	srv, _ := fontServer(t)
	sheet := `@font-face { font-family: F; font-weight: 400; src: url(` + srv.URL + `/f.ttf); }
	          @font-face { font-family: F; font-weight: 700; src: url(` + srv.URL + `/f.ttf); }
	          @font-face { font-family: F; font-style: italic; src: url(` + srv.URL + `/f.ttf); }`
	faces := New().LoadFontFaces(context.Background(), &Document{URL: srv.URL + "/p.html"}, []string{sheet}, css.Media{})
	if len(faces) != 3 {
		t.Fatalf("got %d faces, want 3: %+v", len(faces), faces)
	}
}

func TestItoaAndBoolKey(t *testing.T) {
	for _, c := range []struct {
		n    int
		want string
	}{{0, "0"}, {7, "7"}, {400, "400"}, {12345678, "12345678"}} {
		if got := itoa(c.n); got != c.want {
			t.Errorf("itoa(%d) = %q want %q", c.n, got, c.want)
		}
	}
	if boolKey(true) != "i" || boolKey(false) != "n" {
		t.Error("boolKey")
	}
}

// The end-to-end case this whole thread was for: a real WOFF2 from a font
// service, fetched, unwrapped, registered, and MEASURING. Served locally so
// the test does not depend on the network, but the bytes are a font service's
// own — subsetted and glyf/loca transformed, which is what a browser gets.
func TestLoadFontFacesReadsAWOFF2(t *testing.T) {
	woff2, err := os.ReadFile("testdata/IBMPlexSans-Regular-subset.woff2")
	if err != nil {
		t.Skip(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "font/woff2")
		_, _ = w.Write(woff2)
	}))
	defer srv.Close()
	sheet := `@font-face { font-family: 'IBM Plex Sans'; font-style: normal; font-weight: 400;
	           src: url(` + srv.URL + `/f.woff2) format('woff2'); }`
	faces := New().LoadFontFaces(context.Background(), &Document{URL: srv.URL + "/p.html"}, []string{sheet}, css.Media{Width: 1024})
	if len(faces) != 1 {
		t.Fatalf("got %d faces, want 1 — a woff2 src must now be reachable", len(faces))
	}
	if faces[0].Family != "ibm plex sans" {
		t.Errorf("family = %q", faces[0].Family)
	}
	fonts := paint.NewFonts()
	fam := css.FontFamily{Names: "ibm plex sans", Generic: css.GenericSans}
	before := fonts.Measure("Partenaires", fam, 40, 400, false)
	if n := RegisterFontFaces(fonts, faces); n != 1 {
		t.Fatalf("registered %d", n)
	}
	after := fonts.Measure("Partenaires", fam, 40, 400, false)
	if after == before {
		t.Errorf("the woff2 face did not change the measurement (%v)", before)
	}
	t.Logf("Inter measured %.2f px, IBM Plex Sans from the woff2 %.2f px", before, after)
}

// Distinct slots load concurrently, not one after another, and the result
// keeps declaration order. Each font is held for 300ms: serially four would
// take 1.2s; concurrently the whole set finishes in roughly one delay.
func TestLoadFontFacesLoadsSlotsConcurrentlyInDeclarationOrder(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.Header().Set("Content-Type", "font/ttf")
		_, _ = w.Write(cabin.TTF)
	}))
	defer srv.Close()
	var sheet string
	for i, fam := range []string{"A", "B", "C", "D"} {
		sheet += `@font-face { font-family: ` + fam + `; font-weight: ` + itoa(400+100*i) + `; src: url(` + srv.URL + `/` + fam + `.ttf); }`
	}
	start := time.Now()
	faces := New().LoadFontFaces(context.Background(), &Document{URL: srv.URL + "/p.html"}, []string{sheet}, css.Media{})
	elapsed := time.Since(start)
	if len(faces) != 4 {
		t.Fatalf("got %d faces, want 4", len(faces))
	}
	for i, fam := range []string{"a", "b", "c", "d"} { // family names are case-folded by the css layer
		if faces[i].Family != fam {
			t.Errorf("faces[%d].Family = %q, want %q (declaration order)", i, faces[i].Family, fam)
		}
	}
	if elapsed >= 900*time.Millisecond {
		t.Errorf("four 300ms fonts took %v; they should load concurrently (serially would be 1.2s)", elapsed)
	}
}
