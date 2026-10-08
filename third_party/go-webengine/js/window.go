// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package js

import (
	"encoding/base64"
	"net/url"
	"strings"

	"github.com/dop251/goja"

	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/dom"
)

// install wires the whole DOM/BOM surface onto the goja global scope.
func (b *binder) install() {
	g := b.vm.GlobalObject()
	// Build the DOM/BOM interface constructor hierarchy first, so installDocument
	// and every wrap() can stamp nodes with the right interface prototype.
	b.installInterfaces(g)
	doc := b.installDocument()
	g.Set("document", doc)

	// window/self/globalThis/top/parent/frames all alias the global object.
	for _, name := range []string{"window", "self", "globalThis", "top", "parent", "frames"} {
		g.Set(name, g)
	}
	b.accessor(g, "length", func() goja.Value { return b.vm.ToValue(0) }, nil)
	b.accessor(g, "closed", func() goja.Value { return b.vm.ToValue(false) }, nil)
	b.accessor(g, "name", func() goja.Value { return b.vm.ToValue("") }, nil)
	g.Set("innerWidth", b.opt.ViewportWidth)
	g.Set("innerHeight", b.opt.ViewportHeight)
	g.Set("outerWidth", b.opt.ViewportWidth)
	g.Set("outerHeight", b.opt.ViewportHeight)
	g.Set("devicePixelRatio", 1)
	g.Set("scrollX", 0)
	g.Set("scrollY", 0)
	g.Set("pageXOffset", 0)
	g.Set("pageYOffset", 0)

	g.Set("location", b.newLocation())
	g.Set("navigator", b.newNavigator())
	g.Set("console", b.newConsole())
	g.Set("history", b.newHistory())
	g.Set("screen", b.newScreen())
	g.Set("performance", b.newPerformance())
	g.Set("localStorage", b.newStorage("local"))
	g.Set("sessionStorage", b.newStorage("session"))

	b.installTimers(g)
	b.installEventTargets(g)
	b.installStubs(g)
	b.installConstructors(g)
	b.installNet(g)
	b.installCrypto(g)
	b.installEncoding(g)
	b.installURLSearchParams(g)
	b.installIntl(g)
}

// installTimers wires setTimeout/setInterval/rAF etc. onto g, all of which queue
// their callback for the bounded drain pass.
func (b *binder) installTimers(g *goja.Object) {
	// queue honours the caller's requested delay (defaulting to 0, clamped to
	// non-negative) as an ordering hint for drainTimers — see its own doc
	// comment for why delay order, not just enqueue order, matters.
	queue := func(v, delayArg goja.Value) goja.Value {
		if fn, ok := goja.AssertFunction(v); ok {
			delay := numOr(delayArg, 0)
			if delay < 0 {
				delay = 0
			}
			b.jobs = append(b.jobs, timerJob{fn: fn, self: goja.Undefined(), delay: delay})
		}
		b.nextID++
		return b.vm.ToValue(b.nextID)
	}
	g.Set("setTimeout", func(call goja.FunctionCall) goja.Value { return queue(call.Argument(0), call.Argument(1)) })
	// setInterval runs its callback once (never loops) to stay bounded.
	g.Set("setInterval", func(call goja.FunctionCall) goja.Value { return queue(call.Argument(0), call.Argument(1)) })
	g.Set("requestAnimationFrame", func(call goja.FunctionCall) goja.Value { return queue(call.Argument(0), goja.Undefined()) })
	g.Set("requestIdleCallback", func(call goja.FunctionCall) goja.Value { return queue(call.Argument(0), goja.Undefined()) })
	g.Set("queueMicrotask", func(call goja.FunctionCall) goja.Value { return queue(call.Argument(0), goja.Undefined()) })
	g.Set("setImmediate", func(call goja.FunctionCall) goja.Value { return queue(call.Argument(0), goja.Undefined()) })
	noop := func(goja.FunctionCall) goja.Value { return goja.Undefined() }
	for _, name := range []string{"clearTimeout", "clearInterval", "cancelAnimationFrame", "cancelIdleCallback"} {
		g.Set(name, noop)
	}
}

// installEventTargets wires window-level event methods onto g.
func (b *binder) installEventTargets(g *goja.Object) {
	g.Set("addEventListener", func(call goja.FunctionCall) goja.Value {
		opts, _ := call.Argument(2).(*goja.Object)
		b.addListener(b.windowNode, call.Argument(0).String(), call.Argument(1), optBool(opts, "once"))
		return goja.Undefined()
	})
	g.Set("removeEventListener", func(call goja.FunctionCall) goja.Value {
		b.removeListener(b.windowNode, call.Argument(0).String(), call.Argument(1))
		return goja.Undefined()
	})
	g.Set("dispatchEvent", func(call goja.FunctionCall) goja.Value {
		b.dispatch(b.windowNode, eventType(call.Argument(0)), call.Argument(0))
		return b.vm.ToValue(true)
	})
}

// installStubs wires the harmless no-op / best-effort surface that hydration code
// probes: scrolling, dialogs, fetch, media queries, computed style, storage-less
// APIs. Present-but-inert beats absent-and-throwing.
func (b *binder) installStubs(g *goja.Object) {
	noop := func(goja.FunctionCall) goja.Value { return goja.Undefined() }
	for _, name := range []string{"scrollTo", "scroll", "scrollBy", "focus", "blur", "moveTo", "resizeTo", "print", "close", "stop"} {
		g.Set(name, noop)
	}
	g.Set("alert", noop)
	g.Set("confirm", func(goja.FunctionCall) goja.Value { return b.vm.ToValue(false) })
	g.Set("prompt", func(goja.FunctionCall) goja.Value { return goja.Null() })
	g.Set("open", func(goja.FunctionCall) goja.Value { return goja.Null() })
	g.Set("getComputedStyle", func(call goja.FunctionCall) goja.Value {
		if n := b.node(call.Argument(0)); n != nil {
			return b.newComputedStyle(n)
		}
		return b.vm.NewObject()
	})
	g.Set("matchMedia", func(call goja.FunctionCall) goja.Value {
		return b.newMediaQueryList(call.Argument(0).String())
	})
	g.Set("getSelection", func(goja.FunctionCall) goja.Value { return goja.Null() })
	// fetch and XMLHttpRequest are the real, e.Client-backed implementations
	// installed by installNet (see net.go).
	g.Set("customElements", b.newCustomElementRegistry())
	b.installClasslessStorageAPIs(g)
}

// newCustomElementRegistry stubs window.customElements (a CustomElementRegistry
// singleton, not a constructor — unlike the Observer classes above, script
// never does `new customElements`, it reads the global directly). This engine
// has no custom-element upgrade machinery at all (no connectedCallback,
// attributeChangedCallback, or Shadow DOM attachment triggered by define) —
// a real, already-documented gap, not attempted here. What WAS missing until
// now is the global itself: confirmed live on caniuse.com, whose bundle
// merely REFERENCES `customElements` (not even necessarily calling a method)
// early in a large bundled script — with the global entirely absent, that one
// reference threw an uncaught ReferenceError and aborted the ENTIRE script,
// silently losing everything after it in the same file, including an
// unrelated `document.documentElement.classList.remove("no-js")` call whose
// failure then cascaded into several nav links and controls staying
// CSS-hidden (`.no-js .site-nav-item--news{display:none}` and siblings) that
// have nothing to do with custom elements at all. define/get/upgrade are
// harmless no-ops and whenDefined returns a Promise that resolves immediately
// (nothing is ever "defined" to wait for, so there's nothing to legitimately
// stay pending on) — the same "present-but-inert beats absent-and-throwing"
// philosophy as every other stub in this function.
func (b *binder) newCustomElementRegistry() *goja.Object {
	o := b.vm.NewObject()
	noop := func(goja.FunctionCall) goja.Value { return goja.Undefined() }
	o.Set("define", noop)
	o.Set("get", noop)
	o.Set("upgrade", noop)
	o.Set("whenDefined", func(goja.FunctionCall) goja.Value { return b.resolved(goja.Undefined()) })
	return o
}

// installClasslessStorageAPIs wires atob/btoa and structuredClone.
func (b *binder) installClasslessStorageAPIs(g *goja.Object) {
	g.Set("structuredClone", func(call goja.FunctionCall) goja.Value { return call.Argument(0) })
	g.Set("btoa", func(call goja.FunctionCall) goja.Value {
		// btoa encodes a binary string (each char a byte) to base64.
		in := call.Argument(0).String()
		buf := make([]byte, len(in))
		for i := 0; i < len(in); i++ {
			buf[i] = in[i]
		}
		return b.vm.ToValue(base64.StdEncoding.EncodeToString(buf))
	})
	g.Set("atob", func(call goja.FunctionCall) goja.Value {
		s := strings.TrimSpace(call.Argument(0).String())
		dec, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			// Fall back to raw-std (no padding) as browsers are lenient.
			if dec2, err2 := base64.RawStdEncoding.DecodeString(strings.TrimRight(s, "=")); err2 == nil {
				dec = dec2
			} else {
				panic(b.vm.NewTypeError("Failed to decode base64"))
			}
		}
		// atob returns a binary string: each byte becomes one char.
		var sb strings.Builder
		for _, c := range dec {
			sb.WriteByte(c)
		}
		return b.vm.ToValue(sb.String())
	})
}

// newLocation builds window.location from the page URL. assign()/replace()
// update the location's own href/component fields and the binder's PageURL
// (resolving the target against the current href) so scripts that navigate via
// location see a consistent, updated location — the best-effort stand-in for a
// real navigation, which this single-document settle does not perform.
func (b *binder) newLocation() goja.Value {
	o := b.vm.NewObject()
	b.setLocationFields(o, b.opt.PageURL)
	navigate := func(call goja.FunctionCall) goja.Value {
		target := call.Argument(0).String()
		if abs, ok := resolveURL(b.opt.PageURL, target); ok {
			b.opt.PageURL = abs
			b.setLocationFields(o, abs)
		}
		return goja.Undefined()
	}
	o.Set("assign", navigate)
	o.Set("replace", navigate)
	o.Set("reload", func(goja.FunctionCall) goja.Value { return goja.Undefined() })
	o.Set("toString", func(goja.FunctionCall) goja.Value { return b.vm.ToValue(b.opt.PageURL) })
	return o
}

// setLocationFields (re)populates the URL-component data properties of a
// location object from a URL string.
func (b *binder) setLocationFields(o *goja.Object, raw string) {
	u, err := url.Parse(raw)
	if err != nil || u == nil {
		u = &url.URL{}
	}
	o.Set("href", raw)
	o.Set("protocol", withColon(u.Scheme))
	o.Set("host", u.Host)
	o.Set("hostname", u.Hostname())
	o.Set("port", u.Port())
	o.Set("pathname", pathOr(u.Path))
	o.Set("search", withPrefix("?", u.RawQuery))
	o.Set("hash", withPrefix("#", u.Fragment))
	o.Set("origin", origin(u))
}

func (b *binder) newNavigator() goja.Value {
	o := b.vm.NewObject()
	ua := b.opt.UserAgent
	o.Set("userAgent", ua)
	o.Set("appVersion", ua)
	o.Set("appName", "Netscape")
	o.Set("appCodeName", "Mozilla")
	o.Set("platform", "")
	o.Set("vendor", "")
	o.Set("product", "Gecko")
	o.Set("language", "en-US")
	o.Set("languages", b.vm.NewArray("en-US", "en"))
	o.Set("onLine", true)
	o.Set("cookieEnabled", true)
	o.Set("doNotTrack", goja.Null())
	o.Set("hardwareConcurrency", 1)
	o.Set("maxTouchPoints", 0)
	o.Set("javaEnabled", func(goja.FunctionCall) goja.Value { return b.vm.ToValue(false) })
	o.Set("sendBeacon", func(goja.FunctionCall) goja.Value { return b.vm.ToValue(false) })
	return o
}

func (b *binder) newConsole() goja.Value {
	o := b.vm.NewObject()
	mk := func(level string) func(goja.FunctionCall) goja.Value {
		return func(call goja.FunctionCall) goja.Value {
			if b.opt.Log != nil {
				parts := make([]string, len(call.Arguments))
				for i, a := range call.Arguments {
					parts[i] = b.consoleArg(a)
				}
				b.opt.Log(level + ": " + strings.Join(parts, " "))
			}
			return goja.Undefined()
		}
	}
	for _, level := range []string{"log", "warn", "error", "info", "debug", "trace"} {
		o.Set(level, mk(level))
	}
	noop := func(goja.FunctionCall) goja.Value { return goja.Undefined() }
	for _, name := range []string{"group", "groupEnd", "groupCollapsed", "table", "dir", "assert", "count", "time", "timeEnd", "clear"} {
		o.Set(name, noop)
	}
	return o
}

// consoleArg formats one console.* argument. For an error-like object (has both
// a message and a stack) it appends the stack, so a framework that logs a caught
// error via console.error surfaces the throw site in JSLog instead of just the
// message — the difference between a debuggable and an opaque hydration failure.
func (b *binder) consoleArg(a goja.Value) string {
	if obj, ok := a.(*goja.Object); ok {
		msg := obj.Get("message")
		stk := obj.Get("stack")
		if msg != nil && !goja.IsUndefined(msg) && stk != nil && !goja.IsUndefined(stk) {
			if s := stk.String(); s != "" {
				return s
			}
		}
	}
	return a.String()
}

func (b *binder) newHistory() goja.Value {
	o := b.vm.NewObject()
	o.Set("length", 1)
	o.Set("state", goja.Null())
	o.Set("scrollRestoration", "auto")
	noop := func(goja.FunctionCall) goja.Value { return goja.Undefined() }
	for _, name := range []string{"pushState", "replaceState", "back", "forward", "go"} {
		o.Set(name, noop)
	}
	return o
}

func (b *binder) newScreen() goja.Value {
	o := b.vm.NewObject()
	o.Set("width", b.opt.ViewportWidth)
	o.Set("height", b.opt.ViewportHeight)
	o.Set("availWidth", b.opt.ViewportWidth)
	o.Set("availHeight", b.opt.ViewportHeight)
	o.Set("colorDepth", 24)
	o.Set("pixelDepth", 24)
	o.Set("orientation", b.vm.NewObject())
	return o
}

func (b *binder) newPerformance() goja.Value {
	o := b.vm.NewObject()
	o.Set("now", func(goja.FunctionCall) goja.Value { return b.vm.ToValue(0) })
	o.Set("timeOrigin", 0)
	o.Set("getEntriesByType", func(goja.FunctionCall) goja.Value { return b.vm.NewArray() })
	o.Set("getEntriesByName", func(goja.FunctionCall) goja.Value { return b.vm.NewArray() })
	o.Set("getEntries", func(goja.FunctionCall) goja.Value { return b.vm.NewArray() })
	noop := func(goja.FunctionCall) goja.Value { return goja.Undefined() }
	for _, name := range []string{"mark", "measure", "clearMarks", "clearMeasures"} {
		o.Set(name, noop)
	}
	return o
}

// mediaQueryMatches evaluates a window.matchMedia() query. It mirrors the CSS
// cascade so scripts and stylesheets agree on the environment:
//   - prefers-color-scheme: dark → true (the CSS cascade already applies dark
//     @media blocks optimistically, and the reference headless Chromium follows
//     the host's dark appearance); light → false.
//   - prefers-reduced-motion / prefers-contrast → the no-preference default
//     (false), matching a default browser.
//   - min-width / max-width → evaluated against the real viewport width.
//   - anything else → true (optimistic), matching the CSS media evaluation.
//
// This is what lets a class-strategy dark-theme site (react.dev calls
// matchMedia('(prefers-color-scheme: dark)') and adds `dark` to <html>) resolve
// to the same theme the Chrome reference renders.
func (b *binder) mediaQueryMatches(q string) bool {
	lq := strings.ToLower(q)
	if strings.Contains(lq, "prefers-color-scheme") {
		if strings.Contains(lq, "dark") {
			return true
		}
		if strings.Contains(lq, "light") {
			return false
		}
	}
	if strings.Contains(lq, "prefers-reduced-motion") ||
		strings.Contains(lq, "prefers-contrast") ||
		strings.Contains(lq, "prefers-reduced-data") {
		return strings.Contains(lq, "no-preference")
	}
	return css.MediaApplies(q, float64(b.opt.ViewportWidth))
}

func (b *binder) newMediaQueryList(q string) goja.Value {
	o := b.vm.NewObject()
	o.Set("matches", b.mediaQueryMatches(q))
	o.Set("media", q)
	noop := func(goja.FunctionCall) goja.Value { return goja.Undefined() }
	for _, name := range []string{"addListener", "removeListener", "addEventListener", "removeEventListener", "dispatchEvent"} {
		o.Set(name, noop)
	}
	return o
}

// storageArea is a single Web Storage backing map.
type storageArea struct {
	data  map[string]string
	order []string
}

// newStorage builds a localStorage/sessionStorage object over a backing area.
func (b *binder) newStorage(kind string) goja.Value {
	area := b.storage[kind]
	if area == nil {
		area = &storageArea{data: map[string]string{}}
		b.storage[kind] = area
	}
	o := b.vm.NewObject()
	o.Set("getItem", func(call goja.FunctionCall) goja.Value {
		if v, ok := area.data[call.Argument(0).String()]; ok {
			return b.vm.ToValue(v)
		}
		return goja.Null()
	})
	o.Set("setItem", func(call goja.FunctionCall) goja.Value {
		k := call.Argument(0).String()
		if _, ok := area.data[k]; !ok {
			area.order = append(area.order, k)
		}
		area.data[k] = call.Argument(1).String()
		return goja.Undefined()
	})
	o.Set("removeItem", func(call goja.FunctionCall) goja.Value {
		k := call.Argument(0).String()
		delete(area.data, k)
		for i, o := range area.order {
			if o == k {
				area.order = append(area.order[:i], area.order[i+1:]...)
				break
			}
		}
		return goja.Undefined()
	})
	o.Set("clear", func(goja.FunctionCall) goja.Value {
		area.data = map[string]string{}
		area.order = nil
		return goja.Undefined()
	})
	o.Set("key", func(call goja.FunctionCall) goja.Value {
		i := int(call.Argument(0).ToInteger())
		if i < 0 || i >= len(area.order) {
			return goja.Null()
		}
		return b.vm.ToValue(area.order[i])
	})
	b.accessor(o, "length", func() goja.Value { return b.vm.ToValue(len(area.order)) }, nil)
	return o
}

// datasetDynObj implements element.dataset over data-* attributes.
type datasetDynObj struct {
	b *binder
	n *dom.Node
}

func (d *datasetDynObj) Get(key string) goja.Value {
	if v, ok := d.n.Attribute("data-" + camelToKebab(key)); ok {
		return d.b.vm.ToValue(v)
	}
	return goja.Undefined()
}

func (d *datasetDynObj) Set(key string, val goja.Value) bool {
	d.b.setAttr(d.n, "data-"+camelToKebab(key), val.String())
	return true
}

func (d *datasetDynObj) Has(key string) bool {
	_, ok := d.n.Attribute("data-" + camelToKebab(key))
	return ok
}

func (d *datasetDynObj) Delete(key string) bool {
	d.b.removeAttr(d.n, "data-"+camelToKebab(key))
	return true
}

func (d *datasetDynObj) Keys() []string {
	var out []string
	for k := range d.n.Attr {
		if strings.HasPrefix(k, "data-") {
			out = append(out, k[len("data-"):])
		}
	}
	return out
}

// --- small URL helpers -------------------------------------------------------

func withColon(scheme string) string {
	if scheme == "" {
		return ""
	}
	return scheme + ":"
}

func withPrefix(prefix, s string) string {
	if s == "" {
		return ""
	}
	return prefix + s
}

func pathOr(p string) string {
	if p == "" {
		return "/"
	}
	return p
}

func origin(u *url.URL) string {
	if u.Scheme == "" || u.Host == "" {
		return "null"
	}
	return u.Scheme + "://" + u.Host
}

// mergeCookie appends a "name=value" set-cookie assignment to the cookie string.
func mergeCookie(existing, assignment string) string {
	pair := assignment
	if i := strings.IndexByte(assignment, ';'); i >= 0 {
		pair = assignment[:i]
	}
	pair = strings.TrimSpace(pair)
	if pair == "" {
		return existing
	}
	if existing == "" {
		return pair
	}
	return existing + "; " + pair
}
