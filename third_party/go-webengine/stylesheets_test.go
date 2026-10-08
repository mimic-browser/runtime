// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/dom"
)

// TestLoadStylesheetsSelectsLinksByMedia serves a page with a screen-only, a
// print-only and an unconditional stylesheet (plus a print-only @import
// inside the unconditional one) and checks that LoadStylesheets under each
// medium returns exactly the sheets a browser would apply there, in cascade
// order.
func TestLoadStylesheetsSelectsLinksByMedia(t *testing.T) {
	mux := http.NewServeMux()
	serve := func(path, body string) {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/css")
			w.Write([]byte(body))
		})
	}
	serve("/screen.css", ".nav{display:block}")
	serve("/print.css", ".nav{display:none}")
	serve("/both.css", `@import url("/imported-print.css") print; body{margin:0}`)
	serve("/imported-print.css", "aside{display:none}")
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><head>
<link rel="stylesheet" href="/screen.css" media="screen">
<link rel="stylesheet" href="/print.css" media="print">
<link rel="stylesheet" href="/both.css">
</head><body><div class="nav">nav</div><aside>x</aside></body></html>`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	e := New()
	e.Client = srv.Client()
	doc, err := e.Fetch(context.Background(), srv.URL+"/")
	if err != nil {
		t.Fatal(err)
	}
	join := func(sheets []string) string { return strings.Join(sheets, " | ") }

	screen := e.LoadStylesheets(context.Background(), doc, css.Media{Width: 1024})
	if got, want := join(screen), ".nav{display:block} | body{margin:0}"; !strings.Contains(got, "display:block") || strings.Contains(got, "display:none") || !strings.HasSuffix(got, "body{margin:0}") {
		t.Errorf("screen sheets = %q, want the screen link and the unconditional one only (%q)", got, want)
	}
	print := e.LoadStylesheets(context.Background(), doc, css.Media{Type: css.Print, Width: 1024})
	got := join(print)
	if strings.Contains(got, "display:block") || !strings.Contains(got, ".nav{display:none}") {
		t.Errorf("print sheets = %q, want the print link and not the screen one", got)
	}
	// The print-only @import loads before its importer's own rules.
	if i, j := strings.Index(got, "aside{display:none}"), strings.Index(got, "body{margin:0}"); i < 0 || j < 0 || i > j {
		t.Errorf("print sheets = %q, want the print @import before its importer", got)
	}

	// And the cascade agrees end to end: the nav is hidden in print only.
	nav := findClass(doc.Root, "nav")
	if nav == nil {
		t.Fatal("no .nav element")
	}
	smScreen := css.CascadeMedia(doc.Root, css.Media{Width: 1024}, screen)
	smPrint := css.CascadeMedia(doc.Root, css.Media{Type: css.Print, Width: 1024}, print)
	if smScreen[nav].Display == css.DisplayNone || smPrint[nav].Display != css.DisplayNone {
		t.Errorf("nav display: screen %q, print %q; want block-ish and none", smScreen[nav].Display, smPrint[nav].Display)
	}
	// fetchExternalSheets, the pre-Media entry point, is screen.
	if got := join(e.fetchExternalSheets(context.Background(), doc, 1024)); strings.Contains(got, "display:none") {
		t.Errorf("fetchExternalSheets = %q, must be the screen selection", got)
	}
}

// findClass returns the first element under n carrying class cls.
func findClass(n *dom.Node, cls string) *dom.Node {
	if n == nil {
		return nil
	}
	if n.Type == dom.Element {
		if c, _ := n.Attribute("class"); c == cls {
			return n
		}
	}
	for _, ch := range n.Children {
		if f := findClass(ch, cls); f != nil {
			return f
		}
	}
	return nil
}
