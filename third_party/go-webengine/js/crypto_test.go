// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package js

import (
	"testing"
)

// TestCryptoDigestKnownVectors asserts crypto.subtle.digest returns the exact
// hash for a known input across all four supported algorithms.
func TestCryptoDigestKnownVectors(t *testing.T) {
	// Reference digests of the ASCII string "abc".
	cases := map[string]string{
		"SHA-1":   "a9993e364706816aba3e25717850c26c9cd0d89d",
		"SHA-256": "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
		"SHA-384": "cb00753f45a35e8bb5a03d699ac65007272c32ab0eded1631a8b605a43ff5bed8086072ba1e7cc2358baeca134c825a7",
		"SHA-512": "ddaf35a193617abacc417349ae20413112e6fa4e89a97ea20a9eeee64b55d39a2192992a274fc1a836ba3c23a3feebbd454d4423643ce80e2a9ac94fa54ca49f",
	}
	for algo, want := range cases {
		_, logs, _ := runJS(t, page(`
			crypto.subtle.digest('`+algo+`', new TextEncoder().encode('abc')).then(function(buf){
				var b = new Uint8Array(buf), h='';
				for (var i=0;i<b.length;i++){ h += ('0'+b[i].toString(16)).slice(-2); }
				console.log('D:'+h);
			});`))
		mustHave(t, logs, "D:"+want)
	}
}

// TestCryptoDigestOnArrayBuffer proves digest accepts a raw ArrayBuffer (not only
// a TypedArray view) as its BufferSource.
func TestCryptoDigestOnArrayBuffer(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		crypto.subtle.digest('SHA-256', new TextEncoder().encode('abc').buffer).then(function(buf){
			var b = new Uint8Array(buf), h='';
			for (var i=0;i<b.length;i++){ h += ('0'+b[i].toString(16)).slice(-2); }
			console.log('AB:'+h);
		});`))
	mustHave(t, logs, "AB:ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad")
}

// TestCryptoDigestErrors covers the rejected-promise branches: an unsupported
// algorithm and a non-BufferSource data argument.
func TestCryptoDigestErrors(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		crypto.subtle.digest('MD5', new Uint8Array([1])).catch(function(e){ console.log('E1:'+e.message); });
		crypto.subtle.digest('SHA-256', 'not-a-buffer').catch(function(e){ console.log('E2:'+e.message); });`))
	mustHave(t, logs, "E1:digest: unsupported algorithm MD5")
	if !has(logs, "E2:") {
		t.Errorf("digest on non-BufferSource should reject: %v", logs)
	}
}

// TestCryptoGetRandomValues proves the same array is returned and filled.
func TestCryptoGetRandomValues(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		var a = new Uint8Array(16);
		var r = crypto.getRandomValues(a);
		var any = 0; for (var i=0;i<a.length;i++){ any |= a[i]; }
		console.log('SAME:'+(r===a)+' FILLED:'+(any!==0)+' LEN:'+a.length);
		// A 32-bit view is filled too (byte-granular fill works for any int view).
		var b = new Uint32Array(4);
		crypto.getRandomValues(b);
		var any2=0; for (var i=0;i<b.length;i++){ any2 |= b[i]; }
		console.log('U32:'+(any2!==0));`))
	mustHave(t, logs, "SAME:true FILLED:true LEN:16", "U32:true")
}

// TestCryptoGetRandomValuesErrors covers the throwing branches: a non-view
// argument and an over-quota length.
func TestCryptoGetRandomValuesErrors(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		try { crypto.getRandomValues(5); } catch(e){ console.log('PRIM:'+e.message); }
		try { crypto.getRandomValues({}); } catch(e){ console.log('NV:'+e.message); }
		try { crypto.getRandomValues(new Uint8Array(65537)); } catch(e){ console.log('QE:'+e.message); }
		// A fake view whose buffer is not an ArrayBuffer (cast-fail branch).
		try { crypto.getRandomValues({buffer:{}, byteOffset:0, byteLength:0}); } catch(e){ console.log('FB:'+e.message); }
		// A fake view spanning past its ArrayBuffer (bounds branch).
		try { crypto.getRandomValues({buffer:new ArrayBuffer(4), byteOffset:2, byteLength:10}); } catch(e){ console.log('OOB:'+e.message); }`))
	for _, k := range []string{"PRIM:", "NV:", "QE:", "FB:", "OOB:"} {
		if !has(logs, k) {
			t.Errorf("expected throw %s: %v", k, logs)
		}
	}
}

// TestCryptoRandomUUID asserts the RFC 4122 v4 shape.
func TestCryptoRandomUUID(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		var u = crypto.randomUUID();
		var re = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
		console.log('UUID:'+re.test(u)+' LEN:'+u.length);
		console.log('UNIQ:'+(crypto.randomUUID()!==crypto.randomUUID()));`))
	mustHave(t, logs, "UUID:true LEN:36", "UNIQ:true")
}

// TestGoRandomUUIDShape exercises randomUUID directly (Go-level) for version and
// variant bits, independent of the JS regex.
func TestGoRandomUUIDShape(t *testing.T) {
	u := randomUUID()
	if len(u) != 36 {
		t.Fatalf("length = %d, want 36", len(u))
	}
	if u[14] != '4' {
		t.Errorf("version nibble = %c, want 4 (%q)", u[14], u)
	}
	switch u[19] {
	case '8', '9', 'a', 'b':
	default:
		t.Errorf("variant nibble = %c, want 8/9/a/b (%q)", u[19], u)
	}
	for i, c := range u {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				t.Errorf("expected '-' at %d, got %c", i, c)
			}
		}
	}
}

// TestCryptoHMACSignVerify covers importKey → sign (known vector) → verify
// (true for the right sig, false for a tampered one) plus a hash extracted from
// the algorithm dict rather than the key.
func TestCryptoHMACSignVerify(t *testing.T) {
	// RFC/Wikipedia vector: HMAC-SHA256(key="key", "The quick brown fox jumps over the lazy dog").
	const want = "f7bc83f430538424b13298e6aa6fb143ef4d59a14946175997479dbc2d1a3cd8"
	_, logs, _ := runJS(t, page(`
		var enc = new TextEncoder();
		var msg = enc.encode('The quick brown fox jumps over the lazy dog');
		crypto.subtle.importKey('raw', enc.encode('key'), {name:'HMAC', hash:'SHA-256'}, false, ['sign','verify'])
		.then(function(k){
			return crypto.subtle.sign('HMAC', k, msg).then(function(sig){
				var b=new Uint8Array(sig), h='';
				for (var i=0;i<b.length;i++){ h += ('0'+b[i].toString(16)).slice(-2); }
				console.log('SIG:'+h);
				return crypto.subtle.verify('HMAC', k, sig, msg).then(function(ok){
					console.log('VOK:'+ok);
					var bad = new Uint8Array(sig); bad[0]^=0xff;
					return crypto.subtle.verify('HMAC', k, bad.buffer, msg).then(function(ok2){ console.log('VBAD:'+ok2); });
				});
			});
		});`))
	mustHave(t, logs, "SIG:"+want, "VOK:true", "VBAD:false")
}

// TestCryptoHMACHashFromObjectHash proves the hash can be given as {name:...}.
func TestCryptoHMACHashFromNestedHash(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		var enc = new TextEncoder();
		crypto.subtle.importKey('raw', enc.encode('key'), {name:'HMAC', hash:{name:'SHA-256'}}, false, ['sign'])
		.then(function(k){ return crypto.subtle.sign({name:'HMAC'}, k, enc.encode('The quick brown fox jumps over the lazy dog')); })
		.then(function(sig){
			var b=new Uint8Array(sig), h='';
			for (var i=0;i<b.length;i++){ h += ('0'+b[i].toString(16)).slice(-2); }
			console.log('NH:'+h);
		});`))
	mustHave(t, logs, "NH:f7bc83f430538424b13298e6aa6fb143ef4d59a14946175997479dbc2d1a3cd8")
}

// TestCryptoSubtleErrorBranches covers the remaining rejected-promise paths:
// sign/verify on a non-key, sign on a bad data arg, and an unsupported key hash.
func TestCryptoSubtleErrorBranches(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		var enc = new TextEncoder();
		crypto.subtle.sign('HMAC', {}, enc.encode('x')).catch(function(e){ console.log('SK:'+e.message); });
		crypto.subtle.verify('HMAC', {}, enc.encode('x'), enc.encode('y')).catch(function(e){ console.log('VK:'+e.message); });
		crypto.subtle.importKey('raw', 'not-a-buffer', {name:'HMAC',hash:'SHA-256'}, false, ['sign']).catch(function(e){ console.log('IK:'+e.message); });
		crypto.subtle.importKey('raw', enc.encode('k'), {name:'HMAC',hash:'MD5'}, false, ['sign'])
			.then(function(k){ return crypto.subtle.sign('HMAC', k, enc.encode('x')); })
			.catch(function(e){ console.log('BH:'+e.message); });
		crypto.subtle.importKey('raw', enc.encode('k'), {name:'HMAC',hash:'SHA-256'}, false, ['sign'])
			.then(function(k){ return crypto.subtle.sign('HMAC', k, 'not-a-buffer'); })
			.catch(function(e){ console.log('SD:'+e.message); });
		// verify branches: bad signature arg, bad data arg, unsupported key hash.
		crypto.subtle.importKey('raw', enc.encode('k'), {name:'HMAC',hash:'SHA-256'}, false, ['verify']).then(function(k){
			crypto.subtle.verify('HMAC', k, 'not-a-buffer', enc.encode('x')).catch(function(e){ console.log('VS:'+e.message); });
			crypto.subtle.verify('HMAC', k, enc.encode('sig'), 'not-a-buffer').catch(function(e){ console.log('VD:'+e.message); });
		});
		crypto.subtle.importKey('raw', enc.encode('k'), {name:'HMAC',hash:'MD5'}, false, ['verify']).then(function(k){
			return crypto.subtle.verify('HMAC', k, enc.encode('sig'), enc.encode('x'));
		}).catch(function(e){ console.log('VH:'+e.message); });
		// digest data that is an object but neither ArrayBuffer nor a view.
		crypto.subtle.digest('SHA-256', {}).catch(function(e){ console.log('DO:'+e.message); });`))
	for _, k := range []string{"SK:", "VK:", "IK:", "BH:", "SD:", "VS:", "VD:", "VH:", "DO:"} {
		if !has(logs, k) {
			t.Errorf("expected error branch %s in logs: %v", k, logs)
		}
	}
}

// TestNormalizeHashAliases exercises the hash-name canonicalisation directly.
func TestNormalizeHashAliases(t *testing.T) {
	for in, want := range map[string]string{
		"sha1": "SHA-1", "SHA256": "SHA-256", " sha-384 ": "SHA-384",
		"SHA512": "SHA-512", "unknown": "UNKNOWN",
	} {
		if got := normalizeHash(in); got != want {
			t.Errorf("normalizeHash(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestHashHelpersUnsupported covers the false branch of hashBytes / hashNew.
func TestHashHelpersUnsupported(t *testing.T) {
	if _, ok := hashBytes("MD5", []byte("x")); ok {
		t.Error("hashBytes should reject MD5")
	}
	if _, ok := hashNew("MD5"); ok {
		t.Error("hashNew should reject MD5")
	}
	if _, ok := hmacBytes("MD5", []byte("k"), []byte("x")); ok {
		t.Error("hmacBytes should reject MD5")
	}
}

// TestHashAndHmacAllAlgos exercises every supported algorithm through the Go
// helpers (covering each hashNew / hashBytes / hmacBytes case), asserting the
// digest and HMAC output lengths and non-emptiness.
func TestHashAndHmacAllAlgos(t *testing.T) {
	lens := map[string]int{"SHA-1": 20, "SHA-256": 32, "SHA-384": 48, "SHA-512": 64}
	for algo, n := range lens {
		d, ok := hashBytes(algo, []byte("abc"))
		if !ok || len(d) != n {
			t.Errorf("hashBytes(%s) len=%d ok=%v, want %d", algo, len(d), ok, n)
		}
		if _, ok := hashNew(algo); !ok {
			t.Errorf("hashNew(%s) not ok", algo)
		}
		m, ok := hmacBytes(algo, []byte("key"), []byte("data"))
		if !ok || len(m) != n {
			t.Errorf("hmacBytes(%s) len=%d ok=%v, want %d", algo, len(m), ok, n)
		}
	}
}

// TestCryptoAlgorithmArgVariants covers algorithmHash / algorithmObject branches:
// a digest algorithm given as {name:...}, an empty dict (no name/hash → reject),
// a string algorithm passed to importKey, and a non-object key passed to sign.
func TestCryptoAlgorithmArgVariants(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		var enc = new TextEncoder();
		crypto.subtle.digest({name:'SHA-256'}, enc.encode('abc')).then(function(buf){
			var b=new Uint8Array(buf); console.log('OBJALGO:'+b.length);
		});
		crypto.subtle.digest({}, enc.encode('abc')).catch(function(e){ console.log('NOALGO:'+e.message); });
		crypto.subtle.importKey('raw', enc.encode('k'), 'HMAC', false, ['sign']).then(function(k){
			console.log('STRALGO:'+(k.algorithm && k.algorithm.name));
		});
		crypto.subtle.sign('HMAC', 'stringkey', enc.encode('x')).catch(function(e){ console.log('STRKEY:'+e.message); });`))
	mustHave(t, logs, "OBJALGO:32", "NOALGO:", "STRALGO:HMAC")
	if !has(logs, "STRKEY:") {
		t.Errorf("sign with a non-object key should reject: %v", logs)
	}
}

// TestNewUint8ArrayRoundTrip confirms the Go-side wrapper produces a genuine,
// readable Uint8Array.
func TestNewUint8ArrayRoundTrip(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		var e = new TextEncoder().encode('AB');
		console.log('TA:'+(e instanceof Uint8Array)+' '+e[0]+','+e[1]);`))
	mustHave(t, logs, "TA:true 65,66")
}
