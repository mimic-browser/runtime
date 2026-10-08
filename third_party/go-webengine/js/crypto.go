// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package js

import (
	"crypto/hmac"
	crand "crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"hash"
	"strings"

	"github.com/dop251/goja"
)

// installCrypto wires the Web Crypto surface hydration code relies on:
// crypto.getRandomValues, crypto.randomUUID and the async crypto.subtle
// (digest / importKey / sign / verify). It is backed by Go's crypto/rand and
// crypto/sha* + crypto/hmac, so it is pure-Go and CGO-free. Async methods return
// real goja Promises that settle inline (the work is synchronous), reusing the
// same Promise machinery as fetch so .then reactions run on the bounded job
// queue as the stack unwinds.
func (b *binder) installCrypto(g *goja.Object) {
	c := b.vm.NewObject()
	c.Set("getRandomValues", func(call goja.FunctionCall) goja.Value { return b.getRandomValues(call.Argument(0)) })
	c.Set("randomUUID", func(goja.FunctionCall) goja.Value { return b.vm.ToValue(randomUUID()) })
	c.Set("subtle", b.newSubtleCrypto())
	g.Set("crypto", c)
}

// getRandomValues fills the passed integer TypedArray with cryptographically
// strong random bytes and returns the SAME array, per the WebCrypto spec. It
// throws a TypeError for a non-view argument and mirrors the 65536-byte quota.
func (b *binder) getRandomValues(arg goja.Value) goja.Value {
	region, ok := b.viewMutableBytes(arg)
	if !ok {
		panic(b.vm.NewTypeError("getRandomValues: argument is not an integer TypedArray"))
	}
	if len(region) > 65536 {
		panic(b.vm.NewTypeError("getRandomValues: requested length exceeds 65536"))
	}
	if _, err := crand.Read(region); err != nil {
		panic(b.vm.NewTypeError("getRandomValues: entropy source failed: " + err.Error()))
	}
	return arg
}

// randomUUID returns an RFC 4122 version-4 UUID string.
func randomUUID() string {
	var u [16]byte
	if _, err := crand.Read(u[:]); err != nil {
		// crand.Read never returns a short read on success; on the (practically
		// impossible) failure path fall back to a zeroed-but-well-formed UUID so a
		// caller still receives a valid v4 shape rather than a panic.
		u = [16]byte{}
	}
	u[6] = (u[6] & 0x0f) | 0x40 // version 4
	u[8] = (u[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", u[0:4], u[4:6], u[6:8], u[8:10], u[10:16])
}

// newSubtleCrypto builds the crypto.subtle object. Each method returns a real
// Promise; the computation happens inline and the promise settles synchronously.
func (b *binder) newSubtleCrypto() *goja.Object {
	o := b.vm.NewObject()

	o.Set("digest", func(call goja.FunctionCall) goja.Value {
		name := algorithmHash(call.Argument(0))
		data, ok := b.bufferSourceBytes(call.Argument(1))
		if !ok {
			return b.rejected(b.vm.NewTypeError("digest: data is not a BufferSource"))
		}
		sum, ok := hashBytes(name, data)
		if !ok {
			return b.rejected(b.vm.NewTypeError("digest: unsupported algorithm " + name))
		}
		return b.resolved(b.vm.ToValue(b.vm.NewArrayBuffer(sum)))
	})

	o.Set("importKey", func(call goja.FunctionCall) goja.Value {
		// importKey(format, keyData, algorithm, extractable, keyUsages)
		raw, ok := b.bufferSourceBytes(call.Argument(1))
		if !ok {
			return b.rejected(b.vm.NewTypeError("importKey: keyData is not a BufferSource"))
		}
		algo := call.Argument(2)
		key := b.vm.NewObject()
		key.Set("type", "secret")
		key.Set("extractable", call.Argument(3).ToBoolean())
		key.Set("algorithm", b.algorithmObject(algo))
		key.Set("usages", call.Argument(4))
		// Stash the raw material and hash on non-enumerable-ish helper fields the
		// engine reads back in sign/verify. Scripts never depend on these.
		key.Set("__rawHex", hex.EncodeToString(raw))
		key.Set("__hash", algorithmHash(algo))
		return b.resolved(key)
	})

	o.Set("sign", func(call goja.FunctionCall) goja.Value {
		raw, hashName, ok := keyMaterial(call.Argument(1))
		if !ok {
			return b.rejected(b.vm.NewTypeError("sign: unsupported key"))
		}
		data, ok := b.bufferSourceBytes(call.Argument(2))
		if !ok {
			return b.rejected(b.vm.NewTypeError("sign: data is not a BufferSource"))
		}
		mac, ok := hmacBytes(hashName, raw, data)
		if !ok {
			return b.rejected(b.vm.NewTypeError("sign: unsupported hash " + hashName))
		}
		return b.resolved(b.vm.ToValue(b.vm.NewArrayBuffer(mac)))
	})

	o.Set("verify", func(call goja.FunctionCall) goja.Value {
		raw, hashName, ok := keyMaterial(call.Argument(1))
		if !ok {
			return b.rejected(b.vm.NewTypeError("verify: unsupported key"))
		}
		sig, ok := b.bufferSourceBytes(call.Argument(2))
		if !ok {
			return b.rejected(b.vm.NewTypeError("verify: signature is not a BufferSource"))
		}
		data, ok := b.bufferSourceBytes(call.Argument(3))
		if !ok {
			return b.rejected(b.vm.NewTypeError("verify: data is not a BufferSource"))
		}
		mac, ok := hmacBytes(hashName, raw, data)
		if !ok {
			return b.rejected(b.vm.NewTypeError("verify: unsupported hash " + hashName))
		}
		return b.resolved(b.vm.ToValue(hmacEqual(mac, sig)))
	})

	return o
}

// algorithmObject normalises a WebCrypto algorithm argument (a string like
// "SHA-256" or a dict like {name:"HMAC", hash:"SHA-256"}) into a plain object
// exposing at least a name, for round-tripping onto a CryptoKey.
func (b *binder) algorithmObject(v goja.Value) *goja.Object {
	o := b.vm.NewObject()
	if s, ok := v.(*goja.Object); ok {
		if n := s.Get("name"); n != nil && !goja.IsUndefined(n) {
			o.Set("name", n.String())
		}
		if h := s.Get("hash"); h != nil && !goja.IsUndefined(h) {
			o.Set("hash", h)
		}
		return o
	}
	o.Set("name", v.String())
	return o
}

// algorithmHash extracts the hash algorithm name from a WebCrypto algorithm
// argument: a bare string, {name:"SHA-256"}, or {hash:"SHA-256"} / {hash:{name}}.
func algorithmHash(v goja.Value) string {
	obj, ok := v.(*goja.Object)
	if !ok {
		return normalizeHash(v.String())
	}
	if h := obj.Get("hash"); h != nil && !goja.IsUndefined(h) {
		if ho, ok := h.(*goja.Object); ok {
			if n := ho.Get("name"); n != nil && !goja.IsUndefined(n) {
				return normalizeHash(n.String())
			}
		}
		return normalizeHash(h.String())
	}
	if n := obj.Get("name"); n != nil && !goja.IsUndefined(n) {
		return normalizeHash(n.String())
	}
	return ""
}

// keyMaterial reads the raw secret and hash name off a CryptoKey produced by
// importKey.
func keyMaterial(v goja.Value) (raw []byte, hashName string, ok bool) {
	obj, isObj := v.(*goja.Object)
	if !isObj {
		return nil, "", false
	}
	hx := obj.Get("__rawHex")
	if hx == nil || goja.IsUndefined(hx) {
		return nil, "", false
	}
	dec, err := hex.DecodeString(hx.String())
	if err != nil {
		return nil, "", false
	}
	name := ""
	if h := obj.Get("__hash"); h != nil && !goja.IsUndefined(h) {
		name = h.String()
	}
	return dec, name, true
}

// normalizeHash canonicalises a hash name to the WebCrypto spelling.
func normalizeHash(name string) string {
	switch strings.ToUpper(strings.TrimSpace(name)) {
	case "SHA-1", "SHA1":
		return "SHA-1"
	case "SHA-256", "SHA256":
		return "SHA-256"
	case "SHA-384", "SHA384":
		return "SHA-384"
	case "SHA-512", "SHA512":
		return "SHA-512"
	}
	return strings.ToUpper(strings.TrimSpace(name))
}

// hashBytes computes a one-shot digest of data under the named algorithm.
func hashBytes(name string, data []byte) ([]byte, bool) {
	switch normalizeHash(name) {
	case "SHA-1":
		s := sha1.Sum(data)
		return s[:], true
	case "SHA-256":
		s := sha256.Sum256(data)
		return s[:], true
	case "SHA-384":
		s := sha512.Sum384(data)
		return s[:], true
	case "SHA-512":
		s := sha512.Sum512(data)
		return s[:], true
	}
	return nil, false
}

// hashNew returns a fresh hash.Hash constructor for the named algorithm.
func hashNew(name string) (func() hash.Hash, bool) {
	switch normalizeHash(name) {
	case "SHA-1":
		return sha1.New, true
	case "SHA-256":
		return sha256.New, true
	case "SHA-384":
		return sha512.New384, true
	case "SHA-512":
		return sha512.New, true
	}
	return nil, false
}

// hmacBytes computes HMAC(data) under the named hash and secret key.
func hmacBytes(hashName string, key, data []byte) ([]byte, bool) {
	fn, ok := hashNew(hashName)
	if !ok {
		return nil, false
	}
	m := hmac.New(fn, key)
	m.Write(data)
	return m.Sum(nil), true
}

// hmacEqual is a constant-time comparison of a computed MAC against a candidate.
func hmacEqual(mac, candidate []byte) bool { return hmac.Equal(mac, candidate) }

// --- byte-view helpers -------------------------------------------------------

// bufferSourceBytes returns a COPY of the bytes behind a BufferSource — an
// ArrayBuffer or any ArrayBufferView (TypedArray / DataView). It reports false
// for anything else.
func (b *binder) bufferSourceBytes(v goja.Value) ([]byte, bool) {
	obj, ok := v.(*goja.Object)
	if !ok {
		return nil, false
	}
	if ab, ok := obj.Export().(goja.ArrayBuffer); ok {
		return append([]byte(nil), ab.Bytes()...), true
	}
	region, ok := b.viewMutableBytes(v)
	if !ok {
		return nil, false
	}
	return append([]byte(nil), region...), true
}

// viewMutableBytes returns the live, writable byte region an ArrayBufferView
// spans over its backing ArrayBuffer (honouring byteOffset/byteLength), so a
// mutation writes through to the JS-visible array. It reports false for a
// non-view argument.
func (b *binder) viewMutableBytes(v goja.Value) ([]byte, bool) {
	obj, ok := v.(*goja.Object)
	if !ok {
		return nil, false
	}
	bufV := obj.Get("buffer")
	bufObj, ok := bufV.(*goja.Object)
	if !ok {
		return nil, false
	}
	ab, ok := bufObj.Export().(goja.ArrayBuffer)
	if !ok {
		return nil, false
	}
	raw := ab.Bytes()
	off := int(obj.Get("byteOffset").ToInteger())
	length := int(obj.Get("byteLength").ToInteger())
	if off < 0 || length < 0 || off+length > len(raw) {
		return nil, false
	}
	return raw[off : off+length], true
}

// newUint8Array wraps data in a JS Uint8Array (copying the bytes into a fresh
// ArrayBuffer) using the runtime's own constructor so the result is a genuine
// TypedArray with the full prototype.
func (b *binder) newUint8Array(data []byte) goja.Value {
	ab := b.vm.NewArrayBuffer(append([]byte(nil), data...))
	ctor := b.vm.GlobalObject().Get("Uint8Array")
	obj, err := b.vm.New(ctor, b.vm.ToValue(ab))
	if err != nil {
		return goja.Undefined()
	}
	return obj
}
