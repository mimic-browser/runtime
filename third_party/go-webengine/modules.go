// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package engine

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/go-webengine/esbuildsandbox"

	"github.com/go-webengine/engine/dom"
)

// ES-module handling.
//
// goja (the pure-Go JS runtime the js package embeds) has NO ES-module support:
// its parser rejects `import`/`export`/`import()`/`import.meta` as reserved
// words, and it exposes no ModuleRecord / ModuleLoader primitive — so a module
// graph cannot be compiled or linked natively (verified against goja HEAD).
//
// Modern SPAs (Mastodon, Vite/webpack output) ship their whole app as
// `<script type="module">` graphs, which the classic-script pass therefore
// skips entirely. To run them we take the only viable pure-Go path: transpile
// the module graph to a single classic IIFE with esbuild (github.com/evanw/
// esbuild, pure Go, CGO-free), fetching every module/import through the engine's
// own HTTP client, then hand the bundled classic source to the existing script
// pass. This resolves static imports, dynamic import(), re-exports and
// import.meta at bundle time, so goja only ever sees plain ES2017 it can run.
//
// The work is bounded (module count, bytes, wall-clock) and strictly gated: a
// page with no module scripts never touches esbuild, so non-module pages are
// unaffected.

const (
	// maxModuleEntryScripts caps how many page-level <script type="module"> entry
	// points a page may have before the whole bundle stage is skipped. esbuild's
	// api.Build is a synchronous, essentially uncancellable call, and a large app
	// (GitHub and other framework SPAs ship a dozen-plus module entries) fans its
	// import graph out into hundreds of parallel chunk fetches that balloon memory
	// and time well before any byte/fetch cap trips. Such pages are heavy apps the
	// engine cannot hydrate anyway and their server-rendered HTML already lays out
	// fine, so past this threshold we skip bundling entirely rather than risk an
	// OOM. Small module-using pages (a handful of entries) still bundle.
	maxModuleEntryScripts = 6
	// maxModuleFetches caps how many module/import sources one page may pull, so a
	// pathological graph cannot fan out without bound.
	maxModuleFetches = 1200
	// maxModuleSourceBytes caps the total fetched module source per page.
	maxModuleSourceBytes = 48 << 20
	// moduleBundleTimeout bounds the whole fetch+bundle stage; it is further capped
	// by whatever remains of the caller's deadline.
	moduleBundleTimeout = 20 * time.Second
	// moduleFetchTimeout bounds a single module source fetch.
	moduleFetchTimeout = 8 * time.Second
)

// moduleEntry is one page-level module script: an external src or inline source.
type moduleEntry struct {
	src    string // resolved-relative URL (empty for inline)
	inline string // inline module body (empty for external)
}

// collectModuleScripts returns the page's `type="module"` scripts in document
// order (external first via src, else inline body).
func collectModuleScripts(root *dom.Node) []moduleEntry {
	var out []moduleEntry
	var walk func(n *dom.Node)
	walk = func(n *dom.Node) {
		if n.Type == dom.Element && n.Tag == "script" {
			if typ, ok := n.Attribute("type"); ok && strings.EqualFold(strings.TrimSpace(typ), "module") {
				if src, ok := n.Attribute("src"); ok && strings.TrimSpace(src) != "" {
					out = append(out, moduleEntry{src: strings.TrimSpace(src)})
				} else {
					var sb strings.Builder
					for _, c := range n.Children {
						if c.Type == dom.Text {
							sb.WriteString(c.Text)
						}
					}
					if strings.TrimSpace(sb.String()) != "" {
						out = append(out, moduleEntry{inline: sb.String()})
					}
				}
			}
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	return out
}

// moduleBundleStats reports what a bundle did (for diagnostics/logging).
type moduleBundleStats struct {
	entries   int
	fetched   int
	failed    int // modules that could not be fetched (served as empty)
	bytesIn   int
	bytesOut  int
	errors    int
	warnings  int
	firstErr  string
	wallClock time.Duration
}

// bundleModuleScripts fetches and transpiles the page's module graph to a single
// classic IIFE using esbuild. Every module and import specifier is fetched
// through e.Client (so the page's cookies/TLS apply). It returns the bundled
// classic source and stats; ok is false when there are no module scripts, no
// HTTP client, or the bundle fails (in which case the page renders without the
// module app — the OpenGraph fallback still engages for an empty body).
func (e *Engine) bundleModuleScripts(ctx context.Context, doc *Document) (string, moduleBundleStats, bool) {
	entries := collectModuleScripts(doc.Root)
	var stats moduleBundleStats
	stats.entries = len(entries)
	if len(entries) == 0 || e.Client == nil {
		return "", stats, false
	}
	// A page with many module entry points is a heavy framework app whose graph
	// would fan out into an unbounded parallel fetch/parse burst; skip bundling
	// (its server HTML already renders) rather than risk an OOM.
	if len(entries) > maxModuleEntryScripts {
		stats.firstErr = "module bundle skipped: too many entry scripts"
		return "", stats, false
	}

	// Bound the whole stage by the smaller of our budget and the caller's deadline.
	bctx, cancel := context.WithTimeout(ctx, moduleBundleTimeout)
	defer cancel()

	// Build a virtual entry that pulls in every module script in document order:
	// external ones via an import of their absolute URL, inline ones inlined (their
	// own imports are resolved relative to the page URL).
	var entrySrc strings.Builder
	for _, en := range entries {
		if en.src != "" {
			if abs, ok := resolveURL(doc.URL, en.src); ok {
				fmt.Fprintf(&entrySrc, "import %q;\n", abs)
			}
		} else {
			entrySrc.WriteString(en.inline)
			entrySrc.WriteByte('\n')
		}
	}

	var (
		mu      sync.Mutex
		fetched int
		failed  int
		bytesIn int
	)
	// overBudget reports whether the bundle stage's time or size cap has been
	// spent. It MUST gate OnResolve as well as OnLoad: esbuild's api.Build is a
	// synchronous, uncancellable call, and OnResolve alone can keep enumerating a
	// large/pathological transitive import graph (observed live on
	// developer.mozilla.org — many minutes past moduleBundleTimeout) even once
	// every subsequent OnLoad is failing fast, because resolving a new specifier
	// costs nothing by itself and was never checked against either bound. Gating
	// resolution too is what actually stops the scan, not just the fetch.
	overBudget := func() bool {
		if bctx.Err() != nil {
			return true
		}
		mu.Lock()
		defer mu.Unlock()
		return fetched >= maxModuleFetches || bytesIn >= maxModuleSourceBytes
	}
	start := time.Now()
	plugin := api.Plugin{
		Name: "webengine-http",
		Setup: func(build api.PluginBuild) {
			build.OnResolve(api.OnResolveOptions{Filter: `.*`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				if overBudget() {
					return api.OnResolveResult{}, fmt.Errorf("module bundle budget exhausted")
				}
				abs, ok := resolveModuleSpecifier(doc.URL, args.Importer, args.Path)
				if !ok {
					return api.OnResolveResult{}, fmt.Errorf("cannot resolve %q", args.Path)
				}
				return api.OnResolveResult{Path: abs, Namespace: "http"}, nil
			})
			build.OnLoad(api.OnLoadOptions{Filter: `.*`, Namespace: "http"}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				if overBudget() {
					return api.OnLoadResult{}, fmt.Errorf("module graph too large")
				}
				src, ok := e.fetchModuleSource(bctx, args.Path)
				empty := ""
				if !ok {
					// A single unfetchable chunk (a rate-limited or optional
					// locale/vendor split under esbuild's parallel fetch burst)
					// must not abort the whole app bundle: serve it empty, the way
					// a browser's failed dynamic import only rejects that promise.
					// A genuinely critical missing module then surfaces as a bounded
					// runtime error we can see, not a total bundle failure.
					mu.Lock()
					failed++
					mu.Unlock()
					return api.OnLoadResult{Contents: &empty, Loader: api.LoaderJS, ResolveDir: esbuildsandbox.ResolveDir()}, nil
				}
				mu.Lock()
				fetched++
				bytesIn += len(src)
				mu.Unlock()
				contents := src
				return api.OnLoadResult{Contents: &contents, Loader: api.LoaderJS, ResolveDir: esbuildsandbox.ResolveDir()}, nil
			})
		},
	}

	// api.Build is synchronous and takes no context, so overBudget() alone cannot
	// guarantee it returns promptly: it only stops NEW resolves/loads, but a
	// large/pathological transitive graph can leave esbuild's internal scanner
	// draining an already-queued backlog for far longer than moduleBundleTimeout
	// — observed live on developer.mozilla.org, where the call was still blocked
	// in bundler.scanAllDependencies minutes after bctx had expired. awaitBounded
	// stops WAITING for it once the budget is spent instead; the abandoned call
	// keeps running (and is eventually garbage-collected once it finishes), but
	// it can no longer hold up this render.
	res, ok := awaitBounded(bctx, func() api.BuildResult {
		return api.Build(api.BuildOptions{
			Stdin: &api.StdinOptions{
				Contents:   entrySrc.String(),
				ResolveDir: esbuildsandbox.ResolveDir(),
				Sourcefile: doc.URL,
				Loader:     api.LoaderJS,
			},
			Bundle:        true,
			Format:        api.FormatIIFE,
			Target:        api.ES2017,
			Write:         false,
			LogLevel:      api.LogLevelSilent,
			Plugins:       []api.Plugin{plugin},
			Sourcemap:     api.SourceMapNone,
			LegalComments: api.LegalCommentsNone,
			Supported:     map[string]bool{"import-meta": false},
		})
	})
	if !ok {
		mu.Lock()
		stats.fetched, stats.failed, stats.bytesIn = fetched, failed, bytesIn
		mu.Unlock()
		stats.wallClock = time.Since(start)
		stats.firstErr = "module bundle abandoned: esbuild did not return within the bundle budget"
		return "", stats, false
	}

	stats.fetched = fetched
	stats.failed = failed
	stats.bytesIn = bytesIn
	stats.errors = len(res.Errors)
	stats.warnings = len(res.Warnings)
	stats.wallClock = time.Since(start)
	if len(res.Errors) > 0 {
		stats.firstErr = res.Errors[0].Text
		return "", stats, false
	}
	if len(res.OutputFiles) == 0 {
		return "", stats, false
	}
	out := string(res.OutputFiles[0].Contents)
	stats.bytesOut = len(out)
	return out, stats, true
}

// resolveModuleSpecifier resolves an import specifier against its importer (or
// the page URL when importing from the virtual entry), producing an absolute
// http(s) URL. Bare specifiers (no leading /, ./, ../ or scheme) are rejected —
// without an import map they cannot be located.
func resolveModuleSpecifier(pageURL, importer, spec string) (string, bool) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", false
	}
	if strings.HasPrefix(spec, "http://") || strings.HasPrefix(spec, "https://") {
		return spec, true
	}
	if !strings.HasPrefix(spec, "/") && !strings.HasPrefix(spec, "./") && !strings.HasPrefix(spec, "../") {
		return "", false // bare specifier
	}
	base := importer
	if base == "" || !strings.HasPrefix(base, "http") {
		base = pageURL
	}
	return resolveURL(base, spec)
}

// moduleFetchRetries is how many extra attempts a module fetch makes on a
// transient failure (a 429/5xx or connection error), the way a large module
// graph fanned out in parallel briefly rate-limits the origin.
const moduleFetchRetries = 2

// fetchModuleSource retrieves one module's source through e.Client, bounded, with
// a short bounded retry on transient failure.
func (e *Engine) fetchModuleSource(ctx context.Context, abs string) (string, bool) {
	if !strings.HasPrefix(abs, "http") {
		return "", false
	}
	for attempt := 0; ; attempt++ {
		src, retry, ok, after := e.fetchModuleOnce(ctx, abs)
		if ok {
			return src, true
		}
		if !retry || attempt >= moduleFetchRetries || ctx.Err() != nil {
			return "", false
		}
		// A rate-limited origin says how long to wait (Retry-After); retrying
		// sooner just draws another 429. Otherwise a small backoff bounded by ctx.
		wait := after
		if wait == 0 {
			wait = time.Duration(attempt+1) * 150 * time.Millisecond
		}
		select {
		case <-ctx.Done():
			return "", false
		case <-time.After(wait):
		}
	}
}

// fetchModuleOnce does a single fetch attempt. retry reports whether the failure
// looks transient (worth another attempt): a network error or a 429/5xx.
func (e *Engine) fetchModuleOnce(ctx context.Context, abs string) (body string, retry, ok bool, after time.Duration) {
	rctx, cancel := context.WithTimeout(ctx, moduleFetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(rctx, http.MethodGet, abs, nil)
	if err != nil {
		return "", false, false, 0
	}
	if e.UserAgent != "" {
		req.Header.Set("User-Agent", e.UserAgent)
	}
	resp, err := e.Client.Do(req)
	if err != nil {
		return "", true, false, 0 // network/timeout: transient
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		transient := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		if resp.StatusCode == http.StatusTooManyRequests && resp.Header.Get("Retry-After") != "" {
			after = retryAfterDelay(resp.Header.Get("Retry-After"))
		}
		return "", transient, false, after
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxModuleSourceBytes))
	if err != nil {
		return "", true, false, 0
	}
	return string(raw), false, true, 0
}

// awaitBounded runs fn in its own goroutine and returns its result with ok
// true, or a zero value with ok false if ctx is done first. There is no way to
// cancel an arbitrary synchronous call (like esbuild's api.Build, which takes
// no context) — only a way to stop WAITING for it. On the false path fn keeps
// running in the background; the goroutine exits and is collected once fn
// eventually returns, whether or not anything is still around to read it.
func awaitBounded[T any](ctx context.Context, fn func() T) (T, bool) {
	done := make(chan T, 1)
	go func() { done <- fn() }()
	select {
	case v := <-done:
		return v, true
	case <-ctx.Done():
		var zero T
		return zero, false
	}
}

// injectBundledScript appends a classic <script> carrying src to the document
// body (or <html> as a fallback), so the existing script pass runs it. It
// returns the injected node.
func injectBundledScript(root *dom.Node, src string) *dom.Node {
	host := dom.Find(root, "body")
	if host == nil {
		host = dom.Find(root, "html")
	}
	if host == nil {
		host = root
	}
	s := dom.NewElement("script")
	s.Attr["data-webengine-module-bundle"] = "1"
	dom.AppendChild(s, dom.NewText(src))
	dom.AppendChild(host, s)
	return s
}
