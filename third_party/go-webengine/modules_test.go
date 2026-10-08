// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package engine

import (
	"context"
	"image"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-webengine/engine/dom"
)

// findByID returns the first element whose id attribute equals id.
func findByID(root *dom.Node, id string) *dom.Node {
	var found *dom.Node
	var walk func(n *dom.Node)
	walk = func(n *dom.Node) {
		if found != nil {
			return
		}
		if n.Type == dom.Element && n.ID() == id {
			found = n
			return
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	return found
}

// moduleFixtureServer serves an HTML page whose ES-module entry imports a second
// local module and mutates the DOM, plus the two module files. It is fully
// offline (localhost) and deterministic.
func moduleFixtureServer() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><html><head><title>mod</title></head>
<body><div id="app">PLACEHOLDER</div>
<script type="module" src="/entry.js"></script>
</body></html>`))
	})
	mux.HandleFunc("/entry.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		// Static import of a second module + a named export consumed here.
		_, _ = w.Write([]byte(`import { greeting } from "./lib.js";
document.getElementById("app").textContent = greeting + " from module";`))
	})
	mux.HandleFunc("/lib.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		_, _ = w.Write([]byte(`export const greeting = "HELLO";`))
	})
	return httptest.NewServer(mux)
}

// TestModuleGraphExecutesAndMutatesDOM proves the esbuild transpile path runs an
// ES-module graph end-to-end: the entry module imports a second module and its
// DOM mutation is present in the rendered document.
func TestModuleGraphExecutesAndMutatesDOM(t *testing.T) {
	srv := moduleFixtureServer()
	defer srv.Close()

	e := New()
	e.Client = srv.Client() // reach the httptest server

	doc, err := e.Fetch(context.Background(), srv.URL+"/")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = e.RenderDocument(context.Background(), doc, image.Rect(0, 0, 400, 300))
	if err != nil {
		t.Fatal(err)
	}

	app := findByID(doc.Root, "app")
	if app == nil {
		t.Fatal("no #app node")
	}
	got := strings.TrimSpace(dom.TextContent(app))
	if got != "HELLO from module" {
		t.Fatalf("module DOM mutation not applied: #app = %q, want %q", got, "HELLO from module")
	}
}

// TestInlineModuleExecutes proves an INLINE `<script type=module>` (no src) that
// imports a local module is bundled and run, mutating the DOM.
func TestInlineModuleExecutes(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><div id="app">x</div>` +
			`<script type="module">import { g } from "/lib.js"; document.getElementById("app").textContent = g;</script>` +
			`</body></html>`))
	})
	mux.HandleFunc("/lib.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		_, _ = w.Write([]byte(`export const g = "INLINE-OK";`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	e := New()
	e.Client = srv.Client()
	doc, err := e.Fetch(context.Background(), srv.URL+"/")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.RenderDocument(context.Background(), doc, image.Rect(0, 0, 300, 200)); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(dom.TextContent(findByID(doc.Root, "app"))); got != "INLINE-OK" {
		t.Fatalf("inline module did not run: #app = %q", got)
	}
}

// TestDynamicScriptTypeModuleSeesLoadEvent covers a real hand-rolled
// ResourceLoader idiom (confirmed live on pkg.go.dev's own loadScript()
// helper): a classic inline script creates a <script>, sets its type via the
// PROPERTY (`s.type = 'module'`, not setAttribute), sets its src, and appends
// it — and the module's own top-level code (a genuine ES module: it imports a
// second file, which only the module-bundle path, not a plain classic-script
// run, can resolve) registers a window "load" listener. Two separate,
// causally-linked bugs had to both be fixed for this to work: the missing
// generic `.type` accessor (without it, the property assignment never touched
// the real attribute, so the module-bundle pass never recognised the script
// as a module at all — it silently ran as an ordinary classic script instead,
// where the bare `import` below would itself throw a SyntaxError) and
// settle()'s ordering (window "load" — a one-shot event — used to fire before
// the module-bundle pass ran, so even a correctly-recognised module's own
// "load" listener registered too late to ever be called).
func TestDynamicScriptTypeModuleSeesLoadEvent(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><div id="app">x</div><script>
			var s = document.createElement('script');
			s.type = 'module';
			s.src = '/mod.js';
			document.head.appendChild(s);
		</script></body></html>`))
	})
	mux.HandleFunc("/mod.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		_, _ = w.Write([]byte(`import { text } from "/lib.js";
			window.addEventListener('load', function() {
				document.getElementById('app').textContent = text;
			});`))
	})
	mux.HandleFunc("/lib.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		_, _ = w.Write([]byte(`export const text = "LOADED";`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	e := New()
	e.Client = srv.Client()
	doc, err := e.Fetch(context.Background(), srv.URL+"/")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.RenderDocument(context.Background(), doc, image.Rect(0, 0, 300, 200)); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(dom.TextContent(findByID(doc.Root, "app"))); got != "LOADED" {
		t.Fatalf("dynamically-created module script's load listener did not fire: #app = %q, want %q", got, "LOADED")
	}
}

// TestModuleFetch404Fallback covers the fetch-failure path: an import of a
// missing (404) module fails the bundle gracefully, leaving the DOM untouched.
func TestModuleFetch404Fallback(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><div id="app">keep</div>` +
			`<script type="module" src="/entry.js"></script></body></html>`))
	})
	mux.HandleFunc("/entry.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		_, _ = w.Write([]byte(`import "/does-not-exist.js";`))
	})
	// /does-not-exist.js is intentionally unhandled -> 404.
	srv := httptest.NewServer(mux)
	defer srv.Close()

	e := New()
	e.Client = srv.Client()
	doc, err := e.Fetch(context.Background(), srv.URL+"/")
	if err != nil {
		t.Fatal(err)
	}
	_, stats, ok := e.bundleModuleScripts(context.Background(), doc)
	if ok {
		t.Fatal("expected bundle to fail on a 404 import")
	}
	if stats.entries == 0 {
		t.Fatal("expected entries>0")
	}
	if _, _, err := e.RenderDocument(context.Background(), doc, image.Rect(0, 0, 200, 100)); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(dom.TextContent(findByID(doc.Root, "app"))); got != "keep" {
		t.Fatalf("failed bundle should leave DOM untouched: #app = %q", got)
	}
}

// TestModuleGraphDisabledJS confirms the module path is gated by DisableJS: with
// JS off, the module graph is not bundled and the DOM is untouched.
func TestModuleGraphDisabledJS(t *testing.T) {
	srv := moduleFixtureServer()
	defer srv.Close()

	e := New()
	e.Client = srv.Client()
	e.DisableJS = true

	doc, err := e.Fetch(context.Background(), srv.URL+"/")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = e.RenderDocument(context.Background(), doc, image.Rect(0, 0, 400, 300)); err != nil {
		t.Fatal(err)
	}
	app := findByID(doc.Root, "app")
	if got := strings.TrimSpace(dom.TextContent(app)); got != "PLACEHOLDER" {
		t.Fatalf("DisableJS should leave the module DOM untouched: #app = %q", got)
	}
}

// TestClassicScriptUnaffectedByModulePath confirms a page with only a classic
// script behaves identically (no module bundling triggered).
func TestClassicScriptUnaffectedByModulePath(t *testing.T) {
	src := `<html><head><title>t</title></head><body><div id="app">before</div>` +
		`<script>document.getElementById('app').textContent='after';</script></body></html>`
	_, _, err := New().RenderHTML(context.Background(), src, "https://example.com/", image.Rect(0, 0, 200, 100))
	if err != nil {
		t.Fatal(err)
	}
	// Re-parse+render to read the mutated node deterministically.
	doc, _ := parseDoc(src)
	if _, _, err := New().RenderDocument(context.Background(), doc, image.Rect(0, 0, 200, 100)); err != nil {
		t.Fatal(err)
	}
	app := findByID(doc.Root, "app")
	if got := strings.TrimSpace(dom.TextContent(app)); got != "after" {
		t.Fatalf("classic script path changed: #app = %q, want %q", got, "after")
	}
}

// TestNoModuleScriptsSkipsBundler proves bundleModuleScripts is a no-op (ok=false,
// zero entries) for a page with no module scripts — the non-module fast path.
func TestNoModuleScriptsSkipsBundler(t *testing.T) {
	doc, _ := parseDoc(`<html><body><script>var x=1;</script></body></html>`)
	_, stats, ok := New().bundleModuleScripts(context.Background(), doc)
	if ok || stats.entries != 0 {
		t.Fatalf("expected no-op for non-module page: ok=%v entries=%d", ok, stats.entries)
	}
}

// TestResolveModuleSpecifier covers the resolver's branches.
func TestResolveModuleSpecifier(t *testing.T) {
	const page = "https://ex.com/app/index.html"
	cases := []struct {
		importer, spec, want string
		ok                   bool
	}{
		{"", "https://cdn.ex/x.js", "https://cdn.ex/x.js", true},          // absolute
		{"", "/packs/a.js", "https://ex.com/packs/a.js", true},            // root-relative vs page
		{"https://ex.com/packs/a.js", "./b.js", "https://ex.com/packs/b.js", true}, // relative vs importer
		{"https://ex.com/packs/sub/a.js", "../c.js", "https://ex.com/packs/c.js", true},
		{"", "react", "", false},   // bare specifier
		{"", "", "", false},        // empty
	}
	for _, c := range cases {
		got, ok := resolveModuleSpecifier(page, c.importer, c.spec)
		if ok != c.ok || got != c.want {
			t.Errorf("resolveModuleSpecifier(%q,%q,%q) = %q,%v want %q,%v",
				page, c.importer, c.spec, got, ok, c.want, c.ok)
		}
	}
}

// TestModuleBundleFailureFallback proves a module page whose import graph cannot
// be resolved (bare specifier) fails the bundle gracefully: no panic, DOM
// unchanged, ok=false with entries>0.
func TestModuleBundleFailureFallback(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><div id="app">keep</div>` +
			`<script type="module" src="/e.js"></script></body></html>`))
	})
	mux.HandleFunc("/e.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		_, _ = w.Write([]byte(`import x from "totally-bare-specifier"; x();`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	e := New()
	e.Client = srv.Client()
	doc, err := e.Fetch(context.Background(), srv.URL+"/")
	if err != nil {
		t.Fatal(err)
	}
	_, stats, ok := e.bundleModuleScripts(context.Background(), doc)
	if ok {
		t.Fatal("expected bundle to fail on a bare specifier")
	}
	if stats.entries == 0 {
		t.Fatal("expected entries>0 for a module page")
	}
	// Full render must still succeed and leave the DOM untouched.
	if _, _, err := e.RenderDocument(context.Background(), doc, image.Rect(0, 0, 200, 100)); err != nil {
		t.Fatal(err)
	}
	app := findByID(doc.Root, "app")
	if got := strings.TrimSpace(dom.TextContent(app)); got != "keep" {
		t.Fatalf("failed bundle should leave DOM untouched: #app = %q", got)
	}
}

// TestAwaitBoundedTimesOutWhenFnNeverReturns proves the wall-clock guard added
// after a live hang: rendering developer.mozilla.org left the render stuck for
// minutes inside esbuild's api.Build (a synchronous call with no context/
// cancellation) long after its own 20s bundle budget had expired — the
// deadline was being honoured by the plugin's individual resolve/load
// callbacks, but nothing stopped esbuild's internal scanner from taking far
// longer to drain an already-queued backlog. awaitBounded is what
// bundleModuleScripts now waits on instead of the raw call: it must return
// promptly once ctx is done, even though fn (standing in for api.Build) never
// returns on its own — the abandoned goroutine is left running rather than
// held onto.
func TestAwaitBoundedTimesOutWhenFnNeverReturns(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	block := make(chan struct{}) // never closed: fn "hangs" like the live esbuild call did
	start := time.Now()
	_, ok := awaitBounded(ctx, func() int {
		<-block
		return 42
	})
	if ok {
		t.Fatal("expected ok=false: fn never returns before ctx is done")
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("awaitBounded blocked for %s past ctx's 20ms deadline", elapsed)
	}
}

// TestAwaitBoundedReturnsResult covers the ordinary path: fn returns well
// before ctx expires, so its value passes through unchanged.
func TestAwaitBoundedReturnsResult(t *testing.T) {
	v, ok := awaitBounded(context.Background(), func() string { return "done" })
	if !ok || v != "done" {
		t.Errorf("awaitBounded = %q,%v want %q,true", v, ok, "done")
	}
}

// TestModuleBundleAbandonedOnBudget is the integration-level companion to
// TestAwaitBoundedTimesOutWhenFnNeverReturns: with a bundle context that is
// already past its deadline by the time bundleModuleScripts runs (a page
// whose module fetch would otherwise hang), the call returns quickly with
// ok=false and a stats.firstErr naming the abandonment, rather than blocking.
func TestModuleBundleAbandonedOnBudget(t *testing.T) {
	// release must close BEFORE srv.Close() runs (srv.Close() blocks until every
	// handler has returned) — deferred in this order so LIFO unwinds it first.
	release := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><script type="module" src="/e.js"></script></body></html>`))
	})
	mux.HandleFunc("/e.js", func(w http.ResponseWriter, r *http.Request) {
		<-release // would hang the request well past the test's deadline below
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	defer close(release)

	e := New()
	e.Client = srv.Client()
	doc, err := e.Fetch(context.Background(), srv.URL+"/")
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, stats, ok := e.bundleModuleScripts(ctx, doc)
	elapsed := time.Since(start)
	if ok {
		t.Fatal("expected the bundle to be abandoned, not to succeed")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("bundleModuleScripts blocked for %s past its own budget", elapsed)
	}
	if !strings.Contains(stats.firstErr, "abandoned") {
		t.Errorf("stats.firstErr = %q, want it to mention the bundle was abandoned", stats.firstErr)
	}
}

// moduleGlobFixtureServer serves an entry module containing a dynamic import
// with a template-literal glob pattern whose variable segment is the FIRST
// path component (`./${id}/index.js`) — the shape of a common real-world
// i18n idiom (one locale-named directory, then a fixed file), and the one
// that actually reproduced the live bug: with no fixed literal directory
// ahead of the wildcard, esbuild's glob starts scanning at ResolveDir itself
// rather than some named subdirectory of it, so a real ResolveDir of "/"
// recursively walked the host's entire real filesystem tree. A pattern with
// a fixed leading directory (e.g. `./chunks/${id}.js`) does not reproduce
// this: if "chunks" doesn't exist it fails fast as an unresolved import
// instead, real bug or not.
func moduleGlobFixtureServer() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><body><script type="module" src="/entry.js"></script></body></html>`))
	})
	mux.HandleFunc("/entry.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		_, _ = w.Write([]byte("const id = 'a'; import(`./${id}/index.js`);"))
	})
	return httptest.NewServer(mux)
}

// TestModuleGlobImportDoesNotWalkRealFilesystem is the regression test for the
// bug fixed by using github.com/go-webengine/esbuildsandbox's ResolveDir:
// before it, every OnLoadResult (and the Stdin entry) in this file declared
// ResolveDir "/", so a page merely containing a glob-shaped dynamic import
// made the engine recursively walk the host's real root filesystem —
// observed live rendering a production MDN page, ~9s spent almost entirely
// in readdir/symlink syscalls for a single render (pprof). This test is
// integration-level on purpose: esbuildsandbox's own package tests prove its
// ResolveDir itself is safe, but only this test proves bundleModuleScripts
// actually WIRES it into every ResolveDir esbuild sees here, so a future edit
// reintroducing a stray "/" at one of the three call sites in modules.go
// would still be caught. With the sandbox in place the glob resolves to zero
// matches immediately, so this must complete well within a tight bound
// rather than stall the bundle budget.
func TestModuleGlobImportDoesNotWalkRealFilesystem(t *testing.T) {
	srv := moduleGlobFixtureServer()
	defer srv.Close()

	e := New()
	e.Client = srv.Client()
	doc, err := e.Fetch(context.Background(), srv.URL+"/")
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	_, stats, ok := e.bundleModuleScripts(ctx, doc)
	elapsed := time.Since(start)
	if !ok {
		t.Fatalf("bundle failed: %s", stats.firstErr)
	}
	if strings.Contains(stats.firstErr, "abandoned") {
		t.Fatalf("bundle was abandoned (budget exhausted): %s", stats.firstErr)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("bundleModuleScripts took %s — a glob import likely walked real directories again", elapsed)
	}
}
