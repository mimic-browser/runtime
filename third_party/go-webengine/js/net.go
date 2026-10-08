// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package js

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dop251/goja"
	"golang.org/x/net/html/charset"
)

// Network limits per render. The overall JS wall-clock budget (b.deadman) bounds
// total time; these bound request count and per-response size so a page cannot
// exhaust memory or issue unbounded requests within the budget.
const (
	maxRequests       = 100
	maxResponseBytes  = 8 << 20 // 8 MB per response
	perRequestTimeout = 6 * time.Second
)

// doRequest performs a single JS-initiated HTTP request through the engine's
// shared client (b.opt.Client) — the SAME client the engine uses, so any
// transport-level guard (e.g. the browserproxy's SSRF filter) applies to JS
// fetches too. It resolves rawURL against the page base, is bounded by the
// remaining JS budget, and reads at most maxResponseBytes.
func (b *binder) doRequest(method, rawURL string, headers map[string]string, body string) (*httpResult, error) {
	if b.opt.Client == nil {
		return nil, errString("no HTTP client available")
	}
	if b.reqCount >= maxRequests {
		return nil, errString("request limit exceeded")
	}
	abs, ok := resolveURL(b.opt.PageURL, rawURL)
	if !ok || !strings.HasPrefix(abs, "http") {
		return nil, errString("unsupported URL: " + rawURL)
	}
	// Bound the request by whatever remains of the JS budget.
	remaining := time.Until(b.deadman)
	if remaining <= 0 {
		return nil, errString("JS budget exhausted")
	}
	if remaining > perRequestTimeout {
		remaining = perRequestTimeout
	}
	b.reqCount++

	ctx, cancel := context.WithTimeout(b.opt.Ctx, remaining)
	defer cancel()
	if method == "" {
		method = http.MethodGet
	}
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, strings.ToUpper(method), abs, rdr)
	if err != nil {
		return nil, err
	}
	if b.opt.UserAgent != "" {
		req.Header.Set("User-Agent", b.opt.UserAgent)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := b.opt.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, err
	}
	final := abs
	if resp.Request != nil && resp.Request.URL != nil {
		final = resp.Request.URL.String()
	}
	return &httpResult{
		status:     resp.StatusCode,
		statusText: strings.TrimSpace(strings.TrimPrefix(resp.Status, strconv.Itoa(resp.StatusCode))),
		headers:    resp.Header,
		raw:        raw,
		finalURL:   final,
		redirected: final != abs,
	}, nil
}

// httpResult is a completed JS-initiated request.
type httpResult struct {
	status     int
	statusText string
	headers    http.Header
	raw        []byte
	finalURL   string
	redirected bool
}

// text decodes the body to UTF-8, honouring the Content-Type charset.
func (r *httpResult) text() string {
	rd, err := charset.NewReader(bytes.NewReader(r.raw), r.headers.Get("Content-Type"))
	if err != nil {
		return string(r.raw)
	}
	out, err := io.ReadAll(rd)
	if err != nil {
		return string(r.raw)
	}
	return string(out)
}

// installNet wires the real fetch/XMLHttpRequest/Headers/MessageChannel surface.
func (b *binder) installNet(g *goja.Object) {
	g.Set("fetch", func(call goja.FunctionCall) goja.Value { return b.fetch(call) })
	g.Set("XMLHttpRequest", func(call goja.ConstructorCall) *goja.Object { return b.newXHR() })
	g.Set("Headers", func(call goja.ConstructorCall) *goja.Object {
		return b.newHeaders(headerMapFromArg(call.Argument(0)))
	})
	g.Set("Request", func(call goja.ConstructorCall) *goja.Object {
		o := b.vm.NewObject()
		o.Set("url", call.Argument(0).String())
		o.Set("method", "GET")
		return o
	})
	g.Set("MessageChannel", func(call goja.ConstructorCall) *goja.Object { return b.newMessageChannel() })
}

// fetch performs a bounded request through e.Client and returns a real Promise
// that settles synchronously (the IO happens inline); .then/.catch reactions run
// via goja's job queue as the call stack unwinds.
func (b *binder) fetch(call goja.FunctionCall) goja.Value {
	url := fetchURL(call.Argument(0))
	method, headers, body := requestInit(call.Argument(1))

	promise, resolve, reject := b.vm.NewPromise()
	res, err := b.doRequest(method, url, headers, body)
	if err != nil {
		_ = reject(b.vm.NewTypeError("fetch failed: " + err.Error()))
	} else {
		_ = resolve(b.newResponse(res))
	}
	return b.vm.ToValue(promise)
}

// newResponse builds a Response object over an httpResult. text()/json() return
// already-resolved promises because the body is fully read.
func (b *binder) newResponse(r *httpResult) *goja.Object {
	o := b.vm.NewObject()
	o.Set("ok", r.status >= 200 && r.status < 300)
	o.Set("status", r.status)
	o.Set("statusText", r.statusText)
	o.Set("url", r.finalURL)
	o.Set("redirected", r.redirected)
	o.Set("type", "basic")
	o.Set("bodyUsed", false)
	o.Set("headers", b.newHeaders(httpHeaderMap(r.headers)))
	text := r.text()
	o.Set("text", func(goja.FunctionCall) goja.Value { return b.resolved(b.vm.ToValue(text)) })
	o.Set("json", func(goja.FunctionCall) goja.Value {
		v, err := b.jsonParse(text)
		if err != nil {
			return b.rejected(b.vm.NewTypeError("invalid json: " + err.Error()))
		}
		return b.resolved(v)
	})
	o.Set("arrayBuffer", func(goja.FunctionCall) goja.Value {
		return b.resolved(b.vm.ToValue(b.vm.NewArrayBuffer(append([]byte(nil), r.raw...))))
	})
	o.Set("blob", func(goja.FunctionCall) goja.Value { return b.resolved(b.vm.ToValue(text)) })
	o.Set("clone", func(goja.FunctionCall) goja.Value { return b.newResponse(r) })
	return o
}

// newHeaders builds a Headers object with case-insensitive access. Incoming keys
// are normalised to lower case so header names collide case-insensitively.
func (b *binder) newHeaders(src map[string]string) *goja.Object {
	m := make(map[string]string, len(src))
	for k, v := range src {
		m[strings.ToLower(k)] = v
	}
	o := b.vm.NewObject()
	o.Set("get", func(call goja.FunctionCall) goja.Value {
		if v, ok := m[strings.ToLower(call.Argument(0).String())]; ok {
			return b.vm.ToValue(v)
		}
		return goja.Null()
	})
	o.Set("has", func(call goja.FunctionCall) goja.Value {
		_, ok := m[strings.ToLower(call.Argument(0).String())]
		return b.vm.ToValue(ok)
	})
	o.Set("set", func(call goja.FunctionCall) goja.Value {
		m[strings.ToLower(call.Argument(0).String())] = call.Argument(1).String()
		return goja.Undefined()
	})
	o.Set("append", func(call goja.FunctionCall) goja.Value {
		k := strings.ToLower(call.Argument(0).String())
		v := call.Argument(1).String()
		if old, ok := m[k]; ok {
			m[k] = old + ", " + v
		} else {
			m[k] = v
		}
		return goja.Undefined()
	})
	o.Set("delete", func(call goja.FunctionCall) goja.Value {
		delete(m, strings.ToLower(call.Argument(0).String()))
		return goja.Undefined()
	})
	o.Set("forEach", func(call goja.FunctionCall) goja.Value {
		if fn, ok := goja.AssertFunction(call.Argument(0)); ok {
			for k, v := range m {
				b.callSafely(fn, goja.Undefined(), b.vm.ToValue(v), b.vm.ToValue(k))
			}
		}
		return goja.Undefined()
	})
	return o
}

// resolved / rejected build already-settled promises.
func (b *binder) resolved(v goja.Value) goja.Value {
	p, resolve, _ := b.vm.NewPromise()
	_ = resolve(v)
	return b.vm.ToValue(p)
}

func (b *binder) rejected(v goja.Value) goja.Value {
	p, _, reject := b.vm.NewPromise()
	_ = reject(v)
	return b.vm.ToValue(p)
}

// jsonParse runs the runtime's JSON.parse on s.
func (b *binder) jsonParse(s string) (goja.Value, error) {
	jsonObj, ok := b.vm.GlobalObject().Get("JSON").(*goja.Object)
	if !ok {
		return nil, errString("no JSON global")
	}
	parse, ok := goja.AssertFunction(jsonObj.Get("parse"))
	if !ok {
		return nil, errString("no JSON.parse")
	}
	return parse(goja.Undefined(), b.vm.ToValue(s))
}

// newXHR builds a working XMLHttpRequest backed by e.Client. Synchronous sends
// complete inline; asynchronous sends complete inline too but deliver their
// readystatechange/load callbacks via the bounded timer drain, keeping ordering
// deterministic.
func (b *binder) newXHR() *goja.Object {
	o := b.vm.NewObject()
	st := &xhrState{method: "GET", async: true, reqHeaders: map[string]string{}}

	b.accessor(o, "readyState", func() goja.Value { return b.vm.ToValue(st.readyState) }, nil)
	b.accessor(o, "status", func() goja.Value { return b.vm.ToValue(st.status) }, nil)
	b.accessor(o, "statusText", func() goja.Value { return b.vm.ToValue(st.statusText) }, nil)
	b.accessor(o, "responseText", func() goja.Value { return b.vm.ToValue(st.responseText) }, nil)
	b.accessor(o, "responseURL", func() goja.Value { return b.vm.ToValue(st.responseURL) }, nil)
	b.accessor(o, "response", func() goja.Value {
		if st.responseType == "json" && st.responseText != "" {
			if v, err := b.jsonParse(st.responseText); err == nil {
				return v
			}
		}
		return b.vm.ToValue(st.responseText)
	}, nil)
	b.accessor(o, "responseType",
		func() goja.Value { return b.vm.ToValue(st.responseType) },
		func(v goja.Value) { st.responseType = v.String() })
	b.accessor(o, "onreadystatechange",
		func() goja.Value { return orUndefined(st.onReadyStateChange) },
		func(v goja.Value) { st.onReadyStateChange = v })
	b.accessor(o, "onload", func() goja.Value { return orUndefined(st.onLoad) }, func(v goja.Value) { st.onLoad = v })
	b.accessor(o, "onerror", func() goja.Value { return orUndefined(st.onError) }, func(v goja.Value) { st.onError = v })
	b.accessor(o, "timeout", func() goja.Value { return b.vm.ToValue(0) }, func(goja.Value) {})
	b.accessor(o, "withCredentials", func() goja.Value { return b.vm.ToValue(false) }, func(goja.Value) {})

	o.Set("open", func(call goja.FunctionCall) goja.Value {
		st.method = call.Argument(0).String()
		st.url = call.Argument(1).String()
		if len(call.Arguments) >= 3 {
			st.async = call.Argument(2).ToBoolean()
		}
		b.setXHRReady(o, st, 1) // OPENED
		return goja.Undefined()
	})
	o.Set("setRequestHeader", func(call goja.FunctionCall) goja.Value {
		st.reqHeaders[call.Argument(0).String()] = call.Argument(1).String()
		return goja.Undefined()
	})
	o.Set("send", func(call goja.FunctionCall) goja.Value {
		body := ""
		if a := call.Argument(0); !goja.IsUndefined(a) && !goja.IsNull(a) {
			body = a.String()
		}
		res, err := b.doRequest(st.method, st.url, st.reqHeaders, body)
		complete := func() {
			if err != nil {
				st.readyState = 4
				b.fireXHR(o, st, "error")
				b.fireXHR(o, st, "readystatechange")
				return
			}
			st.status = res.status
			st.statusText = res.statusText
			st.responseText = res.text()
			st.responseURL = res.finalURL
			st.readyState = 4
			b.fireXHR(o, st, "readystatechange")
			b.fireXHR(o, st, "load")
		}
		if st.async {
			// Deliver callbacks on the event loop, not inline.
			b.jobs = append(b.jobs, timerJob{fn: b.wrapJob(complete), self: goja.Undefined()})
		} else {
			complete()
		}
		return goja.Undefined()
	})
	o.Set("abort", func(goja.FunctionCall) goja.Value { return goja.Undefined() })
	o.Set("getResponseHeader", func(call goja.FunctionCall) goja.Value { return goja.Null() })
	o.Set("getAllResponseHeaders", func(goja.FunctionCall) goja.Value { return b.vm.ToValue("") })
	o.Set("overrideMimeType", func(goja.FunctionCall) goja.Value { return goja.Undefined() })
	o.Set("addEventListener", func(call goja.FunctionCall) goja.Value {
		st.listeners = append(st.listeners, xhrListener{typ: call.Argument(0).String(), fn: call.Argument(1)})
		return goja.Undefined()
	})
	o.Set("removeEventListener", func(goja.FunctionCall) goja.Value { return goja.Undefined() })
	o.Set("dispatchEvent", func(goja.FunctionCall) goja.Value { return b.vm.ToValue(true) })
	return o
}

// xhrState holds one XHR's mutable state.
type xhrState struct {
	method, url        string
	async              bool
	reqHeaders         map[string]string
	readyState         int
	status             int
	statusText         string
	responseText       string
	responseURL        string
	responseType       string
	onReadyStateChange goja.Value
	onLoad             goja.Value
	onError            goja.Value
	listeners          []xhrListener
}

type xhrListener struct {
	typ string
	fn  goja.Value
}

// setXHRReady updates readyState and fires the change synchronously.
func (b *binder) setXHRReady(o *goja.Object, st *xhrState, state int) {
	st.readyState = state
	b.fireXHR(o, st, "readystatechange")
}

// fireXHR invokes the on<type> property handler plus any matching listeners.
func (b *binder) fireXHR(o *goja.Object, st *xhrState, typ string) {
	ev := b.newEvent(typ)
	ev.Set("target", o)
	var direct goja.Value
	switch typ {
	case "readystatechange":
		direct = st.onReadyStateChange
	case "load":
		direct = st.onLoad
	case "error":
		direct = st.onError
	}
	if fn := b.handlerFunc(direct); fn != nil {
		b.callSafely(fn, o, ev)
	}
	for _, l := range st.listeners {
		if l.typ == typ {
			if fn := b.handlerFunc(l.fn); fn != nil {
				b.callSafely(fn, o, ev)
			}
		}
	}
}

// wrapJob adapts a Go closure into a goja.Callable for the timer queue.
func (b *binder) wrapJob(fn func()) goja.Callable {
	return func(this goja.Value, args ...goja.Value) (goja.Value, error) {
		fn()
		return goja.Undefined(), nil
	}
}

// newMessageChannel builds a MessageChannel whose ports deliver postMessage
// payloads to the peer port's onmessage via the bounded event loop. React's
// scheduler relies on this; without it React falls back to setTimeout, but the
// channel keeps its scheduling faithful and bounded.
func (b *binder) newMessageChannel() *goja.Object {
	ch := b.vm.NewObject()
	p1 := b.vm.NewObject()
	p2 := b.vm.NewObject()
	b.wirePort(p1, p2)
	b.wirePort(p2, p1)
	ch.Set("port1", p1)
	ch.Set("port2", p2)
	return ch
}

// wirePort makes src.postMessage schedule delivery to dst's message handlers.
func (b *binder) wirePort(src, dst *goja.Object) {
	src.Set("postMessage", func(call goja.FunctionCall) goja.Value {
		data := call.Argument(0)
		b.jobs = append(b.jobs, timerJob{fn: b.wrapJob(func() {
			ev := b.newEvent("message")
			ev.Set("data", data)
			ev.Set("target", dst)
			if fn := b.handlerFunc(dst.Get("onmessage")); fn != nil {
				b.callSafely(fn, dst, ev)
			}
		}), self: goja.Undefined()})
		return goja.Undefined()
	})
	src.Set("onmessage", goja.Null())
	src.Set("start", func(goja.FunctionCall) goja.Value { return goja.Undefined() })
	src.Set("close", func(goja.FunctionCall) goja.Value { return goja.Undefined() })
	src.Set("addEventListener", func(call goja.FunctionCall) goja.Value {
		if call.Argument(0).String() == "message" {
			src.Set("onmessage", call.Argument(1))
		}
		return goja.Undefined()
	})
	src.Set("removeEventListener", func(goja.FunctionCall) goja.Value { return goja.Undefined() })
}

// --- helpers -----------------------------------------------------------------

// errString is a tiny error type for internal messages.
type errString string

func (e errString) Error() string { return string(e) }

func orUndefined(v goja.Value) goja.Value {
	if v == nil {
		return goja.Undefined()
	}
	return v
}

// fetchURL extracts the URL from a fetch resource argument (string or Request).
func fetchURL(v goja.Value) string {
	if obj, ok := v.(*goja.Object); ok {
		if u := obj.Get("url"); u != nil && !goja.IsUndefined(u) {
			return u.String()
		}
	}
	return v.String()
}

// requestInit extracts method/headers/body from a fetch init object.
func requestInit(v goja.Value) (method string, headers map[string]string, body string) {
	method, headers, body = "GET", map[string]string{}, ""
	obj, ok := v.(*goja.Object)
	if !ok {
		return
	}
	if m := obj.Get("method"); m != nil && !goja.IsUndefined(m) {
		method = m.String()
	}
	if bd := obj.Get("body"); bd != nil && !goja.IsUndefined(bd) && !goja.IsNull(bd) {
		body = bd.String()
	}
	for k, v := range headerMapFromArg(obj.Get("headers")) {
		headers[k] = v
	}
	return
}

// headerMapFromArg reads a headers argument (a plain object or a Headers-like
// object) into a name->value map.
func headerMapFromArg(v goja.Value) map[string]string {
	out := map[string]string{}
	obj, ok := v.(*goja.Object)
	if !ok {
		return out
	}
	for _, k := range obj.Keys() {
		val := obj.Get(k)
		if val != nil && !goja.IsUndefined(val) {
			out[k] = val.String()
		}
	}
	return out
}

// httpHeaderMap flattens an http.Header into a lowercased single-value map.
func httpHeaderMap(h http.Header) map[string]string {
	out := map[string]string{}
	for k, vs := range h {
		if len(vs) > 0 {
			out[strings.ToLower(k)] = strings.Join(vs, ", ")
		}
	}
	return out
}
