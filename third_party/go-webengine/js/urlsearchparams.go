// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package js

import (
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/dop251/goja"
)

// installURLSearchParams wires the WHATWG URLSearchParams constructor and hangs a
// live `searchParams` off URL objects. SPA routers and request builders rely on
// it heavily (it is the immediate next global a transpiled module app reaches
// after URL). It is order-preserving and form-urlencoded per spec.
func (b *binder) installURLSearchParams(g *goja.Object) {
	g.Set("URLSearchParams", func(call goja.ConstructorCall) *goja.Object {
		return b.newURLSearchParams(call.Argument(0))
	})
}

// kvPair is one ordered name/value entry.
type kvPair struct{ k, v string }

// newURLSearchParams builds a URLSearchParams over an init value: a query string
// (optionally leading '?'), a plain object, an array of [k,v] pairs, or another
// URLSearchParams-like object (its toString()).
func (b *binder) newURLSearchParams(init goja.Value) *goja.Object {
	pairs := b.parseSearchParamsInit(init)
	o := b.vm.NewObject()

	find := func(k string) (int, bool) {
		for i, p := range *pairs {
			if p.k == k {
				return i, true
			}
		}
		return 0, false
	}

	o.Set("get", func(call goja.FunctionCall) goja.Value {
		k := call.Argument(0).String()
		if i, ok := find(k); ok {
			return b.vm.ToValue((*pairs)[i].v)
		}
		return goja.Null()
	})
	o.Set("getAll", func(call goja.FunctionCall) goja.Value {
		k := call.Argument(0).String()
		var out []interface{}
		for _, p := range *pairs {
			if p.k == k {
				out = append(out, p.v)
			}
		}
		return b.vm.NewArray(out...)
	})
	o.Set("has", func(call goja.FunctionCall) goja.Value {
		_, ok := find(call.Argument(0).String())
		return b.vm.ToValue(ok)
	})
	o.Set("set", func(call goja.FunctionCall) goja.Value {
		k, v := call.Argument(0).String(), call.Argument(1).String()
		// Replace the first occurrence, drop the rest, per spec.
		if i, ok := find(k); ok {
			(*pairs)[i].v = v
			kept := (*pairs)[:0]
			for j, p := range *pairs {
				if p.k == k && j != i {
					continue
				}
				kept = append(kept, p)
			}
			*pairs = kept
		} else {
			*pairs = append(*pairs, kvPair{k, v})
		}
		return goja.Undefined()
	})
	o.Set("append", func(call goja.FunctionCall) goja.Value {
		*pairs = append(*pairs, kvPair{call.Argument(0).String(), call.Argument(1).String()})
		return goja.Undefined()
	})
	o.Set("delete", func(call goja.FunctionCall) goja.Value {
		k := call.Argument(0).String()
		kept := (*pairs)[:0]
		for _, p := range *pairs {
			if p.k != k {
				kept = append(kept, p)
			}
		}
		*pairs = kept
		return goja.Undefined()
	})
	o.Set("sort", func(goja.FunctionCall) goja.Value {
		sort.SliceStable(*pairs, func(i, j int) bool { return (*pairs)[i].k < (*pairs)[j].k })
		return goja.Undefined()
	})
	o.Set("forEach", func(call goja.FunctionCall) goja.Value {
		if fn, ok := goja.AssertFunction(call.Argument(0)); ok {
			for _, p := range *pairs {
				b.callSafely(fn, goja.Undefined(), b.vm.ToValue(p.v), b.vm.ToValue(p.k), o)
			}
		}
		return goja.Undefined()
	})
	o.Set("keys", func(goja.FunctionCall) goja.Value {
		out := make([]interface{}, len(*pairs))
		for i, p := range *pairs {
			out[i] = p.k
		}
		return b.vm.NewArray(out...)
	})
	o.Set("values", func(goja.FunctionCall) goja.Value {
		out := make([]interface{}, len(*pairs))
		for i, p := range *pairs {
			out[i] = p.v
		}
		return b.vm.NewArray(out...)
	})
	o.Set("entries", func(goja.FunctionCall) goja.Value {
		out := make([]interface{}, len(*pairs))
		for i, p := range *pairs {
			out[i] = b.vm.NewArray(p.k, p.v)
		}
		return b.vm.NewArray(out...)
	})
	o.Set("toString", func(goja.FunctionCall) goja.Value {
		return b.vm.ToValue(encodeSearchParams(*pairs))
	})
	b.accessor(o, "size", func() goja.Value { return b.vm.ToValue(len(*pairs)) }, nil)
	return o
}

// parseSearchParamsInit reads a URLSearchParams init into ordered pairs.
func (b *binder) parseSearchParamsInit(init goja.Value) *[]kvPair {
	pairs := &[]kvPair{}
	if init == nil || goja.IsUndefined(init) || goja.IsNull(init) {
		return pairs
	}
	if obj, ok := init.(*goja.Object); ok {
		// Array of [k,v] pairs?
		if isArrayLike(obj) {
			length := int(obj.Get("length").ToInteger())
			for i := 0; i < length; i++ {
				el := obj.Get(strconv.Itoa(i))
				if eo, ok := el.(*goja.Object); ok {
					k := eo.Get("0")
					v := eo.Get("1")
					*pairs = append(*pairs, kvPair{valStr(k), valStr(v)})
				}
			}
			return pairs
		}
		// Plain object: own enumerable keys in order.
		for _, k := range obj.Keys() {
			*pairs = append(*pairs, kvPair{k, valStr(obj.Get(k))})
		}
		return pairs
	}
	// A query string.
	*pairs = decodeSearchParams(init.String())
	return pairs
}

// isArrayLike reports whether o looks like an Array (has a numeric length and an
// Array-ish shape). goja arrays report a numeric length property.
func isArrayLike(o *goja.Object) bool {
	if o.ClassName() == "Array" {
		return true
	}
	l := o.Get("length")
	return l != nil && !goja.IsUndefined(l) && o.Get("0") != nil && !goja.IsUndefined(o.Get("0"))
}

// decodeSearchParams parses a form-urlencoded query string into ordered pairs,
// stripping a single leading '?'.
func decodeSearchParams(s string) []kvPair {
	s = strings.TrimPrefix(s, "?")
	var out []kvPair
	if s == "" {
		return out
	}
	for _, seg := range strings.Split(s, "&") {
		if seg == "" {
			continue
		}
		k, v, _ := strings.Cut(seg, "=")
		dk, err := url.QueryUnescape(k)
		if err != nil {
			dk = k
		}
		dv, err := url.QueryUnescape(v)
		if err != nil {
			dv = v
		}
		out = append(out, kvPair{dk, dv})
	}
	return out
}

// encodeSearchParams serializes ordered pairs as an application/x-www-form-
// urlencoded string, preserving insertion order (spaces as '+').
func encodeSearchParams(pairs []kvPair) string {
	var sb strings.Builder
	for i, p := range pairs {
		if i > 0 {
			sb.WriteByte('&')
		}
		sb.WriteString(url.QueryEscape(p.k))
		sb.WriteByte('=')
		sb.WriteString(url.QueryEscape(p.v))
	}
	return sb.String()
}

// valStr converts a goja value to string, mapping nil/undefined/null to "".
func valStr(v goja.Value) string {
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return ""
	}
	return v.String()
}
