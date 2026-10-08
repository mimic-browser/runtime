// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

// Package js runs a page's JavaScript against a minimal but real DOM binding on
// a pure-Go goja ECMAScript runtime. It is deliberately not a full WHATWG
// implementation: the goal is progressive-enhancement and light hydration —
// class toggling, attribute/style mutation, small tree edits — so that the
// subsequent CSS cascade and layout see what a real browser would.
//
// All mutations write through to the shared dom.Node tree. Execution is bounded
// by a wall-clock budget and a script error never propagates: it is caught,
// optionally logged, and the render continues.
package js

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/dop251/goja"

	"github.com/go-webengine/engine/dom"
)

// Options configures a Run.
type Options struct {
	// PageURL is the document's final URL, used to resolve script src and to
	// populate window.location.
	PageURL string
	// UserAgent is exposed as navigator.userAgent and sent when fetching scripts.
	UserAgent string
	// Client fetches external <script src>. When nil, external scripts are skipped
	// (inline scripts still run).
	Client *http.Client
	// Ctx bounds network fetches; its cancellation also stops script execution.
	Ctx context.Context
	// Timeout is the total wall-clock budget for all scripts and drained timers.
	// Zero selects DefaultTimeout.
	Timeout time.Duration
	// ViewportWidth / ViewportHeight back window.innerWidth / innerHeight.
	ViewportWidth  int
	ViewportHeight int
	// Log, if non-nil, receives console.* output as "level: message" lines.
	Log func(string)
}

// DefaultTimeout is the script budget when Options.Timeout is zero.
const DefaultTimeout = 5 * time.Second

// maxScripts and maxTimerJobs bound the work done regardless of the clock, so a
// pathological page cannot enqueue unbounded callbacks.
const (
	maxScripts   = 200
	maxTimerJobs = 2000
)

// Result reports what a Run did (used by tests and by the engine's logging).
type Result struct {
	ScriptsRun    int   // scripts that executed (inline + fetched)
	ScriptsFailed int   // scripts that threw or failed to compile
	TimersRun     int   // queued timer/animation callbacks drained
	Err           error // first fatal setup error (nil on normal completion)
}

// timerJob is a queued callback from setTimeout/requestAnimationFrame/etc.
type timerJob struct {
	fn    goja.Callable
	self  goja.Value
	delay int64 // requested delay in ms (setTimeout/setInterval only; 0 otherwise)
}

// binder holds the runtime and all live state for one page execution.
type binder struct {
	vm    *goja.Runtime
	root  *dom.Node // the Document node
	opt   Options
	cache map[*dom.Node]*goja.Object

	// protos maps each DOM/BOM interface name (HTMLElement, Text, Event, …) to its
	// `.prototype`, built by installInterfaces. wrap() stamps a node wrapper with
	// the matching one so `el instanceof HTMLElement` holds and subclassing works.
	protos map[string]*goja.Object

	windowNode *dom.Node // sentinel node keying window-level listeners
	docNode    *dom.Node // sentinel node keying document-level listeners
	listeners  map[*dom.Node]map[string][]*eventListenerEntry
	// onHandlers tracks the current "onX" IDL event-handler-attribute value per
	// (node, type) — e.g. `el.onload = fn` — as ONE slot that a later
	// assignment replaces, unlike addEventListener's list. Implemented as a
	// registered listener under the hood (see setOnHandler) so dispatch needs
	// no separate code path; this map exists only so a reassignment can find
	// and remove the PREVIOUS handler before adding the new one.
	onHandlers map[*dom.Node]map[string]goja.Value

	jobs   []timerJob
	nextID int64
	// mutationObservers holds every live `new MutationObserver(cb)` with an
	// active observe() call — see mutationobserver.go.
	mutationObservers []*mutationObserverReg
	deadman           time.Time
	cookie            string
	storage           map[string]*storageArea
	reqCount          int // JS-initiated HTTP requests this render (bounded by maxRequests)

	// metrics is the real geometry/used-value source read back by
	// getBoundingClientRect / offset* / getComputedStyle. Nil = report zeros /
	// inline styles (the legacy Run path with no layout feedback).
	metrics Metrics
	// executed records which <script> elements have already run, so a settle pass
	// runs only scripts injected since the previous pass (in document order),
	// never re-running a script.
	executed map[*dom.Node]bool
	// currentScript is the <script> element synchronously executing right now
	// (nil between scripts, during callbacks/timers, and during module-bundle
	// execution — matching the real document.currentScript spec: non-null only
	// for a synchronously-running classic script). Set/cleared by runScripts
	// around each execute call.
	currentScript *dom.Node

	// scrollPos holds each element's own script-set scrollTop/scrollLeft, so
	// a script that sets one reads the same value back — this engine has no
	// real scroll/clip model (a single static layout pass, no overflow
	// viewport), so the value has no effect on layout or paint; it exists
	// purely so scroll-position-driven script LOGIC (e.g. a "scroll the
	// active item into view" branch that compares against the current
	// scrollTop) does not silently misbehave against a value hardcoded to 0
	// forever. See scrollTop/scrollLeft's own accessor doc comment in dom.go.
	scrollPos map[*dom.Node][2]float64

	// mediaTime/mediaPaused hold a <video>/<audio> element's own script-set
	// currentTime and play()/pause() state — this engine does no real media
	// decoding/playback (a static renderer has no timeline to advance), so
	// neither has any effect on rendering; they exist purely so a script's
	// OWN seek/play/pause logic (e.g. "click a transcript entry, seek the
	// video there, and play it") stays internally consistent instead of
	// reading back a value hardcoded to 0/paused forever. See
	// currentTime/play/pause's own doc comment in dom.go.
	mediaTime   map[*dom.Node]float64
	mediaPaused map[*dom.Node]bool
}

// Run builds the DOM binding on root (a dom.Document node), sets the JS-enabled
// signal, executes the page's scripts in document order, drains queued timers,
// and dispatches DOMContentLoaded/load. It never returns a script error as
// fatal; Result.Err is only set for setup failures. It is the one-shot,
// no-layout-feedback entry point (used by the js package's own tests); the
// engine drives a Session directly so scripts can read real geometry.
func Run(root *dom.Node, opt Options) Result {
	s := Begin(root, opt)
	defer s.Close()
	s.RunInitial()
	return s.Result()
}

// runScripts executes every not-yet-run <script> in document order, honouring
// the budget. A script runs at most once across the whole session, so calling
// this again after the DOM gains new <script> elements runs only those new ones
// (a ResourceLoader-style injected-script chain), in document order.
func (b *binder) runScripts(res *Result) {
	if b.executed == nil {
		b.executed = map[*dom.Node]bool{}
	}
	for _, s := range collectScripts(b.root) {
		if b.executed[s.node] {
			continue
		}
		if res.ScriptsRun+res.ScriptsFailed >= maxScripts || b.expired() {
			return
		}
		b.executed[s.node] = true
		src, ok := b.scriptSource(s)
		if !ok {
			// A real browser fires "error" on the <script> element itself when
			// fetching its src fails (404, network error, …), distinct from a
			// runtime exception during execution (which still fires "load" on
			// the element — see below — reporting the exception separately via
			// window's own error event instead). Dynamically-injected script
			// loaders (webpack/Turbopack's own chunk-loading helper, confirmed
			// load-bearing live on react.dev) set onerror/onload on the element
			// specifically to detect this outcome; without it, a failed fetch
			// left their loading promise pending forever.
			b.dispatch(s.node, "error", b.newEvent("error"))
			continue
		}
		b.currentScript = s.node
		ok = b.execute(src, s.name)
		b.currentScript = nil
		b.dispatch(s.node, "load", b.newEvent("load"))
		if ok {
			res.ScriptsRun++
		} else {
			res.ScriptsFailed++
		}
	}
}

// execute compiles and runs one script, returning whether it completed without
// error. Compile and runtime errors (including interrupts) are contained.
func (b *binder) execute(src, name string) (ok bool) {
	prog, err := goja.Compile(name, src, false)
	if err != nil {
		b.logf("compile %s: %v", name, err)
		return false
	}
	defer func() {
		if r := recover(); r != nil {
			b.logf("panic in %s: %v", name, r)
			ok = false
		}
	}()
	b.vm.ClearInterrupt()
	// ClearInterrupt wipes any pending interrupt — including the watcher's
	// one-shot "context cancelled" fired for a context that was ALREADY cancelled
	// before this script started (the watcher's select fires once, then exits, so
	// it will not re-fire). Re-arm it here so an already-cancelled context still
	// interrupts this script immediately instead of letting it run to the budget
	// timeout.
	if b.opt.Ctx.Err() != nil {
		b.vm.Interrupt("context cancelled")
	}
	if _, err := b.vm.RunProgram(prog); err != nil {
		b.logf("run %s: %v", name, err)
		return false
	}
	return true
}

// dispatchLifecycle fires DOMContentLoaded then load, draining queued timers
// between and after so deferred hydration runs.
func (b *binder) dispatchLifecycle(res *Result) {
	b.dispatch(b.docNode, "DOMContentLoaded", b.newEvent("DOMContentLoaded"))
	b.dispatch(b.windowNode, "DOMContentLoaded", b.newEvent("DOMContentLoaded"))
	b.drainTimers(res)
	b.dispatch(b.windowNode, "load", b.newEvent("load"))
	b.dispatch(b.windowNode, "pageshow", b.newEvent("pageshow"))
	b.drainTimers(res)
}

// drainTimers runs queued callbacks in ascending order of their own requested
// delay (ties broken FIFO by original enqueue order), until the queue empties,
// the job cap is hit, or the budget expires. This is not a real clock — no
// wall-clock waiting happens between jobs — but ordering by delay rather than
// pure enqueue order matters: real page code commonly races a near-immediate
// setTimeout(fn, 0) retry/continuation against a much later
// setTimeout(giveUp, 30000+) safety-net timeout, and draining strict FIFO can
// let the safety-net job run first purely because it happened to be enqueued
// earlier, when every real browser would run the smaller-delay job first.
// Callbacks may enqueue more work, which is honoured within those bounds.
func (b *binder) drainTimers(res *Result) {
	for len(b.jobs) > 0 {
		if res.TimersRun >= maxTimerJobs || b.expired() {
			return
		}
		best := 0
		for i := 1; i < len(b.jobs); i++ {
			if b.jobs[i].delay < b.jobs[best].delay {
				best = i
			}
		}
		job := b.jobs[best]
		b.jobs = append(b.jobs[:best], b.jobs[best+1:]...)
		res.TimersRun++
		b.callSafely(job.fn, job.self)
	}
}

// callSafely invokes a stored callable, containing any error or panic.
func (b *binder) callSafely(fn goja.Callable, self goja.Value, args ...goja.Value) {
	if fn == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			b.logf("callback panic: %v", r)
		}
	}()
	b.vm.ClearInterrupt()
	if _, err := fn(self, args...); err != nil {
		b.logf("callback: %v", err)
	}
}

// expired reports whether the wall-clock budget is spent.
func (b *binder) expired() bool { return !time.Now().Before(b.deadman) }

// logf routes an internal diagnostic to the console sink, if any.
func (b *binder) logf(format string, args ...any) {
	if b.opt.Log != nil {
		b.opt.Log("engine: " + fmt.Sprintf(format, args...))
	}
}

// setClientJS marks the document root as JS-enabled (client-js) and clears the
// no-JS fallback class — the single change that restores JS-gated CSS (e.g.
// MediaWiki's nav collapse) even if the page's own scripts do not run.
func (b *binder) setClientJS() {
	html := dom.Find(b.root, "html")
	if html == nil {
		return
	}
	classRemove(html, "client-nojs")
	classAdd(html, "client-js")
}

// script is one collected <script> element with a display name.
type script struct {
	node *dom.Node
	name string
}

// collectScripts returns runnable <script> elements in document order, skipping
// module/non-JavaScript types (handled gracefully rather than mis-run).
func collectScripts(root *dom.Node) []script {
	var out []script
	i := 0
	var walk func(n *dom.Node)
	walk = func(n *dom.Node) {
		if n.Type == dom.Element && n.Tag == "script" && scriptIsJS(n) {
			i++
			out = append(out, script{node: n, name: scriptName(n, i)})
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	return out
}

// hasPendingScripts reports whether the tree now holds a <script> element that
// runScripts has not executed yet — see RunPending's own doc comment for why
// this is checked without actually running anything here.
func (b *binder) hasPendingScripts() bool {
	for _, s := range collectScripts(b.root) {
		if !b.executed[s.node] {
			return true
		}
	}
	return false
}

// scriptIsJS reports whether a <script>'s type is (classic) JavaScript. Module,
// JSON and other typed scripts are skipped.
func scriptIsJS(n *dom.Node) bool {
	t, ok := n.Attribute("type")
	if !ok {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "", "text/javascript", "application/javascript", "application/ecmascript",
		"text/ecmascript", "text/jscript":
		return true
	}
	return false
}

func scriptName(n *dom.Node, i int) string {
	if src, ok := n.Attribute("src"); ok && strings.TrimSpace(src) != "" {
		return strings.TrimSpace(src)
	}
	return "inline#" + strconv.Itoa(i)
}

// scriptSource returns the JS source for a script: its fetched src, or its inline
// text. Missing/unfetchable external scripts report false.
func (b *binder) scriptSource(s script) (string, bool) {
	if src, ok := s.node.Attribute("src"); ok && strings.TrimSpace(src) != "" {
		return b.fetchScript(strings.TrimSpace(src))
	}
	var sb strings.Builder
	for _, c := range s.node.Children {
		if c.Type == dom.Text {
			sb.WriteString(c.Text)
		}
	}
	if strings.TrimSpace(sb.String()) == "" {
		return "", false
	}
	return sb.String(), true
}

// fetchScript retrieves an external script's text, bounded and best-effort.
func (b *binder) fetchScript(src string) (string, bool) {
	if b.opt.Client == nil {
		return "", false
	}
	abs, ok := resolveURL(b.opt.PageURL, src)
	if !ok || !strings.HasPrefix(abs, "http") {
		return "", false
	}
	ctx, cancel := context.WithTimeout(b.opt.Ctx, scriptFetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, abs, nil)
	if err != nil {
		return "", false
	}
	if b.opt.UserAgent != "" {
		req.Header.Set("User-Agent", b.opt.UserAgent)
	}
	resp, err := b.opt.Client.Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", false
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxScriptBytes))
	if err != nil {
		return "", false
	}
	return string(body), true
}

const (
	scriptFetchTimeout = 8 * time.Second
	maxScriptBytes     = 4 << 20
)

// resolveURL resolves ref against base.
func resolveURL(base, ref string) (string, bool) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", false
	}
	b, err := url.Parse(base)
	if err != nil {
		return "", false
	}
	r, err := url.Parse(ref)
	if err != nil {
		return "", false
	}
	return b.ResolveReference(r).String(), true
}
