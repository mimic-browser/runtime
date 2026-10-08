// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package js

import "testing"

// TestTextEncoderEncode asserts UTF-8 byte output, the encoding property, and
// multi-byte (é = C3 A9) plus astral (😀 = F0 9F 98 80) encoding.
func TestTextEncoderEncode(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		var enc = new TextEncoder();
		console.log('ENC:'+enc.encoding);
		var e = enc.encode('é');
		console.log('EBYTES:'+e[0]+','+e[1]+' LEN:'+e.length+' TA:'+(e instanceof Uint8Array));
		var a = enc.encode('😀');
		console.log('ASTRAL:'+a[0]+','+a[1]+','+a[2]+','+a[3]+' LEN:'+a.length);
		console.log('EMPTY:'+enc.encode().length+','+enc.encode('').length);`))
	mustHave(t, logs,
		"ENC:utf-8",
		"EBYTES:195,169 LEN:2 TA:true",
		"ASTRAL:240,159,152,128 LEN:4",
		"EMPTY:0,0",
	)
}

// TestTextEncoderEncodeInto covers the happy path, a destination too small, and a
// non-view destination.
func TestTextEncoderEncodeInto(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		var enc = new TextEncoder();
		var dst = new Uint8Array(8);
		var r = enc.encodeInto('héllo', dst);
		console.log('INTO:'+r.read+','+r.written+' b0='+dst[0]+' b1='+dst[1]+' b2='+dst[2]);
		// Too-small destination stops at a rune boundary.
		var small = new Uint8Array(1);
		var r2 = enc.encodeInto('é', small);
		console.log('SMALL:'+r2.read+','+r2.written);
		// Non-view destination reports zeros rather than throwing.
		var r3 = enc.encodeInto('x', {});
		console.log('BADDST:'+r3.read+','+r3.written);
		// Astral rune counts two UTF-16 code units in read.
		var big = new Uint8Array(8);
		var r4 = enc.encodeInto('😀', big);
		console.log('AST:'+r4.read+','+r4.written);`))
	// 'héllo' = h(1) é(2) l(1) l(1) o(1) = 6 bytes, 5 code units.
	mustHave(t, logs,
		"INTO:5,6 b0=104 b1=195 b2=169",
		"SMALL:0,0",
		"BADDST:0,0",
		"AST:2,4",
	)
}

// TestTextDecoderDecode covers UTF-8 decoding, the default and labelled
// constructors, empty/undefined input, and a non-BufferSource input.
func TestTextDecoderDecode(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		var d = new TextDecoder();
		console.log('DEC:'+d.decode(new Uint8Array([0xC3,0xA9]))+' ENC:'+d.encoding+' FATAL:'+d.fatal);
		console.log('RT:'+new TextDecoder('utf-8').decode(new TextEncoder().encode('round trip é 😀')));
		console.log('EMPTY:['+d.decode()+']['+d.decode(new Uint8Array(0))+']');
		console.log('BAD:['+d.decode('not-a-buffer')+']');
		// A raw ArrayBuffer decodes too.
		console.log('AB:'+d.decode(new TextEncoder().encode('buf').buffer));`))
	mustHave(t, logs,
		"DEC:é ENC:utf-8 FATAL:false",
		"RT:round trip é 😀",
		"EMPTY:[][]",
		"BAD:[]",
		"AB:buf",
	)
}

// TestTextDecoderBOM covers BOM stripping (default) versus ignoreBOM:true, and a
// non-UTF-8 label degrading to a best-effort decode rather than throwing.
func TestTextDecoderBOM(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		var withBOM = new Uint8Array([0xEF,0xBB,0xBF,0x41]); // BOM + 'A'
		console.log('STRIP:['+new TextDecoder().decode(withBOM)+'] len='+new TextDecoder().decode(withBOM).length);
		console.log('KEEP:'+new TextDecoder('utf-8',{ignoreBOM:true}).decode(withBOM).length);
		var od = new TextDecoder('latin1');
		console.log('LABEL:'+od.encoding+' DEC:'+od.decode(new Uint8Array([0x41])));`))
	mustHave(t, logs,
		"STRIP:[A] len=1",
		"KEEP:2",
		"LABEL:latin1 DEC:A",
	)
}

// TestNormalizeEncodingLabel exercises the alias collapsing directly.
func TestNormalizeEncodingLabel(t *testing.T) {
	for in, want := range map[string]string{
		"utf-8": "utf-8", "utf8": "utf-8", "unicode-1-1-utf-8": "utf-8",
		"": "utf-8", "latin1": "latin1",
	} {
		if got := normalizeEncodingLabel(in); got != want {
			t.Errorf("normalizeEncodingLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestLocationAssignReplace proves assign()/replace() resolve a relative target
// against the current href and update the location component fields, and that an
// empty target leaves the location unchanged.
func TestLocationAssignReplace(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		location.assign('/next/page?q=1#frag');
		console.log('A:'+location.href+' P:'+location.pathname+' S:'+location.search+' H:'+location.hash);
		location.replace('https://other.example/x');
		console.log('R:'+location.href+' HOST:'+location.host+' ORIGIN:'+location.origin);
		var before = location.href;
		location.assign('');
		console.log('EMPTY_UNCHANGED:'+(location.href===before));`))
	// runJS uses testURL = https://ex.com/a/b?x=1&y=2#frag.
	mustHave(t, logs,
		"A:https://ex.com/next/page?q=1#frag P:/next/page S:?q=1 H:#frag",
		"R:https://other.example/x HOST:other.example ORIGIN:https://other.example",
		"EMPTY_UNCHANGED:true",
	)
}
