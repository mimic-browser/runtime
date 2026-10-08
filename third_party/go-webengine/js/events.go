// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package js

import (
	"net/url"

	"github.com/dop251/goja"

	"github.com/go-webengine/engine/dom"
)

// eventListenerEntry is one registered listener: the callback plus its own
// `once` flag. DOM Standard §2.7's own "add an event listener" algorithm
// stores capture/passive/once/signal per registration, not just the bare
// callback — this engine only reads `once` out of the options object so far
// (see the addEventListener bindings in dom.go/window.go); capture only
// affects dispatch ORDER (this engine has no capture phase at all, so it
// cannot matter yet), passive is a no-op in a synchronous non-scrolling
// engine, and signal would need AbortController support this engine lacks.
type eventListenerEntry struct {
	handler goja.Value
	once    bool
}

// addListener registers handler for typ on node n (deduplicating identical
// registrations, matching the DOM). once marks a listener that removes
// itself after its first invocation (the addEventListener options' own
// `once` flag).
func (b *binder) addListener(n *dom.Node, typ string, handler goja.Value, once bool) {
	if handler == nil || goja.IsUndefined(handler) || goja.IsNull(handler) {
		return
	}
	if b.listeners[n] == nil {
		b.listeners[n] = map[string][]*eventListenerEntry{}
	}
	for _, e := range b.listeners[n][typ] {
		if e.handler == handler {
			return
		}
	}
	b.listeners[n][typ] = append(b.listeners[n][typ], &eventListenerEntry{handler: handler, once: once})
}

// setOnHandler implements an "onX" IDL event-handler-attribute assignment
// (`el.onload = fn`): removes whatever handler this exact (n, typ) slot
// previously held (a real browser lets a later assignment replace, not
// stack, unlike addEventListener), then registers the new one the same way
// addEventListener would — dispatch needs no separate code path for onX
// handlers versus addEventListener ones. A non-function value (including
// null/undefined, the standard way script clears a handler) only removes.
func (b *binder) setOnHandler(n *dom.Node, typ string, v goja.Value) {
	if m := b.onHandlers[n]; m != nil {
		if old, ok := m[typ]; ok {
			b.removeListener(n, typ, old)
			delete(m, typ)
		}
	}
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return
	}
	if _, ok := goja.AssertFunction(v); !ok {
		return
	}
	b.addListener(n, typ, v, false)
	if b.onHandlers[n] == nil {
		b.onHandlers[n] = map[string]goja.Value{}
	}
	b.onHandlers[n][typ] = v
}

// onHandler returns the current onX handler for (n, typ), or nil.
func (b *binder) onHandler(n *dom.Node, typ string) goja.Value {
	if m := b.onHandlers[n]; m != nil {
		return m[typ]
	}
	return nil
}

// removeListener unregisters a previously added handler.
func (b *binder) removeListener(n *dom.Node, typ string, handler goja.Value) {
	m := b.listeners[n]
	if m == nil {
		return
	}
	hs := m[typ]
	for i, e := range hs {
		if e.handler == handler {
			m[typ] = append(hs[:i], hs[i+1:]...)
			return
		}
	}
}

// dispatch fires every listener registered for typ on n (the target phase),
// then — when event.bubbles is true and no handler called stopPropagation/
// stopImmediatePropagation — continues up n's ancestor chain doing the same,
// matching real DOM event bubbling (needed for delegation: a handler
// registered on a container rather than the specific button/input a
// synthetic click/keystroke targets). event.target and .currentTarget are
// kept live across the walk. Handler errors/panics are contained per node,
// same as before this walked more than one node.
//
// Scope: does not continue past the document root to a window-level
// listener (window has no place in n's Parent chain) — document-level
// delegation, the common real-world case, works; window-level does not yet.
func (b *binder) dispatch(n *dom.Node, typ string, event goja.Value) {
	obj, _ := event.(*goja.Object)
	// stopped halts moving to an ancestor; stoppedImmediate ALSO halts the
	// remaining listeners on the CURRENT node — the real distinction between
	// stopPropagation (later listeners on this same node still run) and
	// stopImmediatePropagation (they don't) that a single flag would blur.
	stopped, stoppedImmediate := false, false
	bubbles := false
	if obj != nil {
		obj.Set("target", b.wrap(n))
		obj.Set("stopPropagation", func(goja.FunctionCall) goja.Value { stopped = true; return goja.Undefined() })
		obj.Set("stopImmediatePropagation", func(goja.FunctionCall) goja.Value {
			stopped, stoppedImmediate = true, true
			return goja.Undefined()
		})
		if bv := obj.Get("bubbles"); bv != nil {
			bubbles = bv.ToBoolean()
		}
	}

	for cur := n; cur != nil; cur = cur.Parent {
		if m := b.listeners[cur]; m != nil {
			self := b.wrap(cur)
			if cur == b.windowNode || cur == b.docNode {
				self = b.vm.GlobalObject()
			}
			if obj != nil {
				obj.Set("currentTarget", self)
			}
			// Copy so a handler that mutates the list mid-dispatch is safe.
			hs := append([]*eventListenerEntry(nil), m[typ]...)
			for _, e := range hs {
				// Per the DOM standard's own "inner invoke" (§2.9): a once
				// listener is removed BEFORE its callback runs, not after.
				// {once: true} is real, live usage — caniuse.com's own
				// ad-network bundle.js (already load-bearing elsewhere in
				// this session, see cloneNode's own importNode doc comment)
				// calls addEventListener("featureRendered", cb, {once:true}).
				if e.once {
					b.removeListener(cur, typ, e.handler)
				}
				fn := b.handlerFunc(e.handler)
				b.callSafely(fn, self, event)
				if stoppedImmediate {
					break
				}
			}
		}
		if stopped || !bubbles {
			break
		}
	}
}

// handlerFunc resolves an EventListener value to a callable: a function, or an
// object exposing handleEvent.
func (b *binder) handlerFunc(h goja.Value) goja.Callable {
	if fn, ok := goja.AssertFunction(h); ok {
		return fn
	}
	if obj, ok := h.(*goja.Object); ok {
		if fn, ok := goja.AssertFunction(obj.Get("handleEvent")); ok {
			return fn
		}
	}
	return nil
}

// newEvent builds a minimal Event object of the given type, stamped with
// Event.prototype (when the interface hierarchy is installed) so dispatched
// events are `instanceof Event`.
func (b *binder) newEvent(typ string) *goja.Object {
	var o *goja.Object
	if b.protos != nil && b.protos["Event"] != nil {
		o = b.vm.CreateObject(b.protos["Event"])
	} else {
		o = b.vm.NewObject()
	}
	o.Set("type", typ)
	o.Set("bubbles", false)
	o.Set("cancelable", false)
	o.Set("defaultPrevented", false)
	o.Set("target", goja.Null())
	o.Set("currentTarget", goja.Null())
	o.Set("timeStamp", 0)
	o.Set("isTrusted", false)
	noop := func(goja.FunctionCall) goja.Value { return goja.Undefined() }
	o.Set("preventDefault", func(goja.FunctionCall) goja.Value {
		o.Set("defaultPrevented", true)
		return goja.Undefined()
	})
	o.Set("stopPropagation", noop)
	o.Set("stopImmediatePropagation", noop)
	o.Set("composedPath", func(goja.FunctionCall) goja.Value { return b.vm.NewArray() })
	return o
}

// eventType extracts the type of a dispatched event value.
func eventType(v goja.Value) string {
	if obj, ok := v.(*goja.Object); ok {
		if t := obj.Get("type"); t != nil && !goja.IsUndefined(t) {
			return t.String()
		}
	}
	if v == nil {
		return ""
	}
	return v.String()
}

// installConstructors wires the remaining constructible globals hydration code
// expects: the observer classes, URL and Image. The DOM/BOM interface hierarchy
// (Event/CustomEvent, Node, HTMLElement, DOMException, …) is installed separately
// by installInterfaces, which makes those properly subclassable.
func (b *binder) installConstructors(g *goja.Object) {
	observer := func(call goja.ConstructorCall) *goja.Object {
		o := b.vm.NewObject()
		noop := func(goja.FunctionCall) goja.Value { return goja.Undefined() }
		o.Set("observe", noop)
		o.Set("unobserve", noop)
		o.Set("disconnect", noop)
		o.Set("takeRecords", func(goja.FunctionCall) goja.Value { return b.vm.NewArray() })
		return o
	}
	// MutationObserver is real (see installMutationObserver); the other three
	// still need layout geometry or paint timing this engine doesn't feed back
	// into JS yet, so they stay inert stubs.
	g.Set("IntersectionObserver", observer)
	g.Set("ResizeObserver", observer)
	g.Set("PerformanceObserver", observer)
	b.installMutationObserver(g)

	g.Set("URL", func(call goja.ConstructorCall) *goja.Object {
		return b.buildURL(call.Argument(0).String(), call.Argument(1))
	})
	g.Set("Image", func(call goja.ConstructorCall) *goja.Object {
		return b.vm.NewObject()
	})
}

// applyEventInit copies the bubbles/cancelable flags from an EventInit dict.
func applyEventInit(ev *goja.Object, init goja.Value) {
	obj, ok := init.(*goja.Object)
	if !ok {
		return
	}
	if v := obj.Get("bubbles"); v != nil && !goja.IsUndefined(v) {
		ev.Set("bubbles", v.ToBoolean())
	}
	if v := obj.Get("cancelable"); v != nil && !goja.IsUndefined(v) {
		ev.Set("cancelable", v.ToBoolean())
	}
}

// buildURL constructs a WHATWG-ish URL object (a subset: the parsed components).
func (b *binder) buildURL(ref string, baseV goja.Value) *goja.Object {
	var u *url.URL
	if base := optString(baseV); base != "" {
		if bu, err := url.Parse(base); err == nil {
			if r, err := url.Parse(ref); err == nil {
				u = bu.ResolveReference(r)
			}
		}
	}
	if u == nil {
		u, _ = url.Parse(ref)
	}
	if u == nil {
		u = &url.URL{}
	}
	o := b.vm.NewObject()
	o.Set("href", u.String())
	o.Set("protocol", withColon(u.Scheme))
	o.Set("host", u.Host)
	o.Set("hostname", u.Hostname())
	o.Set("port", u.Port())
	o.Set("pathname", pathOr(u.Path))
	o.Set("search", withPrefix("?", u.RawQuery))
	o.Set("hash", withPrefix("#", u.Fragment))
	o.Set("origin", origin(u))
	o.Set("searchParams", b.newURLSearchParams(b.vm.ToValue(u.RawQuery)))
	o.Set("toString", func(goja.FunctionCall) goja.Value { return b.vm.ToValue(u.String()) })
	return o
}

// optString returns the string of v unless it is undefined/null.
func optString(v goja.Value) string {
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return ""
	}
	return v.String()
}
