// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package js

import (
	"strings"
	"unicode/utf8"

	"github.com/dop251/goja"
)

// installEncoding wires the TextEncoder / TextDecoder constructors. Both are
// UTF-8 focused (the encoding SPAs actually use); a non-UTF-8 decoder label is
// accepted and decoded best-effort as UTF-8 rather than throwing, so hydration
// code that constructs `new TextDecoder('utf-8')` (the common case) works and an
// exotic label degrades instead of aborting a script.
func (b *binder) installEncoding(g *goja.Object) {
	g.Set("TextEncoder", func(call goja.ConstructorCall) *goja.Object { return b.newTextEncoder() })
	g.Set("TextDecoder", func(call goja.ConstructorCall) *goja.Object {
		return b.newTextDecoder(call.Argument(0), call.Argument(1))
	})
}

// newTextEncoder builds a TextEncoder whose encode() returns a Uint8Array of the
// string's UTF-8 bytes and whose encodeInto() writes into a caller-supplied
// Uint8Array, reporting {read, written} per spec.
func (b *binder) newTextEncoder() *goja.Object {
	o := b.vm.NewObject()
	o.Set("encoding", "utf-8")
	o.Set("encode", func(call goja.FunctionCall) goja.Value {
		s := ""
		if a := call.Argument(0); !goja.IsUndefined(a) {
			s = a.String()
		}
		return b.newUint8Array([]byte(s))
	})
	o.Set("encodeInto", func(call goja.FunctionCall) goja.Value {
		src := call.Argument(0).String()
		dst, ok := b.viewMutableBytes(call.Argument(1))
		res := b.vm.NewObject()
		if !ok {
			res.Set("read", 0)
			res.Set("written", 0)
			return res
		}
		read, written := 0, 0
		for _, r := range src {
			n := utf8.RuneLen(r)
			if n < 0 || written+n > len(dst) {
				break
			}
			written += utf8.EncodeRune(dst[written:], r)
			// read counts UTF-16 code units consumed (1 for BMP, 2 for astral).
			if r > 0xFFFF {
				read += 2
			} else {
				read++
			}
		}
		res.Set("read", read)
		res.Set("written", written)
		return res
	})
	return o
}

// newTextDecoder builds a TextDecoder for the given label (default utf-8). It
// honours fatal:false leniency (invalid bytes become U+FFFD via Go's decoder)
// and the ignoreBOM option.
func (b *binder) newTextDecoder(labelV, optsV goja.Value) *goja.Object {
	label := "utf-8"
	if !goja.IsUndefined(labelV) && !goja.IsNull(labelV) {
		if s := strings.TrimSpace(strings.ToLower(labelV.String())); s != "" {
			label = s
		}
	}
	ignoreBOM := false
	if opts, ok := optsV.(*goja.Object); ok {
		if v := opts.Get("ignoreBOM"); v != nil && !goja.IsUndefined(v) {
			ignoreBOM = v.ToBoolean()
		}
	}

	o := b.vm.NewObject()
	o.Set("encoding", normalizeEncodingLabel(label))
	o.Set("fatal", false)
	o.Set("ignoreBOM", ignoreBOM)
	o.Set("decode", func(call goja.FunctionCall) goja.Value {
		a := call.Argument(0)
		if goja.IsUndefined(a) || goja.IsNull(a) {
			return b.vm.ToValue("")
		}
		data, ok := b.bufferSourceBytes(a)
		if !ok {
			return b.vm.ToValue("")
		}
		// Go strings are UTF-8; invalid sequences render as U+FFFD when the string
		// is later iterated, matching a non-fatal decoder.
		s := string(data)
		if !ignoreBOM {
			s = strings.TrimPrefix(s, "\ufeff")
		}
		return b.vm.ToValue(s)
	})
	return o
}

// normalizeEncodingLabel maps a decoder label to its canonical name, collapsing
// the UTF-8 aliases and reporting utf-8 for anything handled by the UTF-8 path.
func normalizeEncodingLabel(label string) string {
	switch label {
	case "utf-8", "utf8", "unicode-1-1-utf-8", "":
		return "utf-8"
	}
	return label
}
