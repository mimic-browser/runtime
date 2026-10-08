// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package js

import (
	"math"

	"github.com/dop251/goja"

	"github.com/go-webengine/engine/dom"
)

// rectOf returns n's used border-box rect via the installed Metrics source, or
// (0,0,0,0,false) when there is no layout feedback / n was not laid out.
func (b *binder) rectOf(n *dom.Node) (x, y, w, h float64, ok bool) {
	if b.metrics == nil {
		return 0, 0, 0, 0, false
	}
	return b.metrics.Rect(n)
}

// boundingRect builds a DOMRect object for n from its laid-out geometry. An
// unlaid-out node yields an all-zero rect (matching a display:none element).
func (b *binder) boundingRect(n *dom.Node) goja.Value {
	x, y, w, h, ok := b.rectOf(n)
	if !ok {
		return b.zeroRect()
	}
	r := b.vm.NewObject()
	r.Set("x", x)
	r.Set("y", y)
	r.Set("left", x)
	r.Set("top", y)
	r.Set("width", w)
	r.Set("height", h)
	r.Set("right", x+w)
	r.Set("bottom", y+h)
	return r
}

// borderW / borderH return n's integer border-box width/height (offset*, client*
// and scroll* all resolve to this at the engine's fidelity — there are no
// scrollbars, and overflow is not clipped).
func (b *binder) borderW(n *dom.Node) int {
	_, _, w, _, _ := b.rectOf(n)
	return int(math.Round(w))
}

func (b *binder) borderH(n *dom.Node) int {
	_, _, _, h, _ := b.rectOf(n)
	return int(math.Round(h))
}

// offsetTop / offsetLeft return n's position relative to its offsetParent (the
// nearest ancestor element with a laid-out box), matching the DOM offset model
// closely enough for layout-probing scripts.
func (b *binder) offsetTop(n *dom.Node) int {
	x, y := b.offsetOrigin(n)
	_, ny, _, _, _ := b.rectOf(n)
	_ = x
	return int(math.Round(ny - y))
}

func (b *binder) offsetLeft(n *dom.Node) int {
	x, y := b.offsetOrigin(n)
	nx, _, _, _, _ := b.rectOf(n)
	_ = y
	return int(math.Round(nx - x))
}

// offsetOrigin returns the document-space top-left of n's offsetParent, or the
// document origin (0,0) when no ancestor has a laid-out box.
func (b *binder) offsetOrigin(n *dom.Node) (x, y float64) {
	for p := elementParent(n); p != nil; p = elementParent(p) {
		if px, py, _, _, ok := b.rectOf(p); ok {
			return px, py
		}
	}
	return 0, 0
}

// computedStyleObj implements window.getComputedStyle(el): a read-only view that
// resolves geometry-relevant properties to their USED values from the last
// layout pass (via Metrics). Unknown properties resolve to "" as in a browser.
type computedStyleObj struct {
	b *binder
	n *dom.Node
}

func (c *computedStyleObj) prop(name string) string {
	if c.b.metrics == nil {
		return styleProp(c.n, camelToKebab(name))
	}
	if v, ok := c.b.metrics.Computed(c.n, camelToKebab(name)); ok {
		return v
	}
	return ""
}

func (c *computedStyleObj) Get(key string) goja.Value {
	switch key {
	case "getPropertyValue":
		return c.b.vm.ToValue(func(call goja.FunctionCall) goja.Value {
			return c.b.vm.ToValue(c.prop(call.Argument(0).String()))
		})
	case "getPropertyPriority":
		return c.b.vm.ToValue(func(goja.FunctionCall) goja.Value { return c.b.vm.ToValue("") })
	case "setProperty", "removeProperty":
		// getComputedStyle is read-only: these are inert no-ops.
		return c.b.vm.ToValue(func(goja.FunctionCall) goja.Value { return goja.Undefined() })
	case "cssText":
		return c.b.vm.ToValue("")
	}
	return c.b.vm.ToValue(c.prop(key))
}

func (c *computedStyleObj) Set(string, goja.Value) bool { return true } // read-only
func (c *computedStyleObj) Has(string) bool             { return true }
func (c *computedStyleObj) Delete(string) bool          { return true }
func (c *computedStyleObj) Keys() []string              { return nil }

// newComputedStyle returns the getComputedStyle view for n.
func (b *binder) newComputedStyle(n *dom.Node) goja.Value {
	return b.vm.NewDynamicObject(&computedStyleObj{b: b, n: n})
}
