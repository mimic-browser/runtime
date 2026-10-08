// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package js

import "testing"

// TestURLSearchParamsFromString covers construction from a query string
// (leading '?' stripped), get/getAll/has, and toString round-trip.
func TestURLSearchParamsFromString(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		var p = new URLSearchParams('?a=1&b=2&a=3');
		console.log('GET:'+p.get('a')+' MISS:'+p.get('z')+' HAS:'+p.has('b')+' NOHAS:'+p.has('z'));
		console.log('ALL:'+p.getAll('a').join(',')+' SIZE:'+p.size);
		console.log('STR:'+p.toString());`))
	mustHave(t, logs,
		"GET:1 MISS:null HAS:true NOHAS:false",
		"ALL:1,3 SIZE:3",
		"STR:a=1&b=2&a=3",
	)
}

// TestURLSearchParamsMutation covers set (replace-first + drop-rest), append,
// delete, and sort — all order-preserving per spec.
func TestURLSearchParamsMutation(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		var p = new URLSearchParams('a=1&a=2&b=3');
		p.set('a','9');
		console.log('SET:'+p.toString());
		p.append('c','4');
		console.log('APPEND:'+p.toString());
		p.delete('b');
		console.log('DELETE:'+p.toString());
		var q = new URLSearchParams('c=1&a=2&b=3'); q.sort();
		console.log('SORT:'+q.toString());`))
	mustHave(t, logs,
		"SET:a=9&b=3",
		"APPEND:a=9&b=3&c=4",
		"DELETE:a=9&c=4",
		"SORT:a=2&b=3&c=1",
	)
}

// TestURLSearchParamsEncoding covers form-urlencoding: spaces become '+', and
// reserved characters are percent-encoded, with a decode round-trip.
func TestURLSearchParamsEncoding(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		var p = new URLSearchParams();
		p.append('q','a b&c');
		p.append('name','é');
		console.log('ENC:'+p.toString());
		var r = new URLSearchParams(p.toString());
		console.log('DEC:'+r.get('q')+'|'+r.get('name'));`))
	mustHave(t, logs,
		"ENC:q=a+b%26c&name=%C3%A9",
		"DEC:a b&c|é",
	)
}

// TestURLSearchParamsFromObjectAndArray covers the object and array-of-pairs
// init forms, plus the empty/undefined init.
func TestURLSearchParamsFromObjectAndArray(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		var fromObj = new URLSearchParams({a:'1', b:'2'});
		console.log('OBJ:'+fromObj.get('a')+','+fromObj.get('b'));
		var fromArr = new URLSearchParams([['x','1'],['y','2'],['x','3']]);
		console.log('ARR:'+fromArr.getAll('x').join('/')+' y='+fromArr.get('y'));
		var empty = new URLSearchParams();
		console.log('EMPTY:['+empty.toString()+'] size='+empty.size);`))
	mustHave(t, logs,
		"OBJ:1,2",
		"ARR:1/3 y=2",
		"EMPTY:[] size=0",
	)
}

// TestURLSearchParamsIteration covers forEach/keys/values/entries.
func TestURLSearchParamsIteration(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		var p = new URLSearchParams('a=1&b=2');
		var seen = [];
		p.forEach(function(v,k){ seen.push(k+'='+v); });
		console.log('EACH:'+seen.join(','));
		console.log('KEYS:'+p.keys().join(',')+' VALUES:'+p.values().join(','));
		var e = p.entries();
		console.log('ENTRIES:'+e[0][0]+e[0][1]+'|'+e[1][0]+e[1][1]);`))
	mustHave(t, logs,
		"EACH:a=1,b=2",
		"KEYS:a,b VALUES:1,2",
		"ENTRIES:a1|b2",
	)
}

// TestURLSearchParamsEdgeCases covers empty segments, a key without '=', a
// malformed percent-escape (kept verbatim), and a short array pair (missing
// value → empty string).
func TestURLSearchParamsEdgeCases(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		var p = new URLSearchParams('a=1&&b=2&c');
		console.log('EMPTYSEG:'+p.toString()+' c=['+p.get('c')+']');
		var q = new URLSearchParams('x=%zz');
		console.log('BADESC:'+q.get('x'));
		var qk = new URLSearchParams('%zz=1');
		console.log('BADKEY:'+qk.has('%zz'));
		var r = new URLSearchParams([['k'], ['m','v']]);
		console.log('SHORT:k=['+r.get('k')+'] m='+r.get('m'));
		var noneForEach = new URLSearchParams('a=1');
		noneForEach.forEach('not-a-function');
		console.log('SAFEEACH:ok');`))
	mustHave(t, logs,
		"EMPTYSEG:a=1&b=2&c= c=[]",
		"BADESC:%zz",
		"BADKEY:true",
		"SHORT:k=[] m=v",
		"SAFEEACH:ok",
	)
}

// TestURLObjectHasSearchParams proves a URL object exposes a live searchParams.
func TestURLObjectHasSearchParams(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		var u = new URL('https://ex.com/p?x=1&y=two');
		console.log('SP:'+u.searchParams.get('x')+','+u.searchParams.get('y')+' has='+u.searchParams.has('y'));`))
	mustHave(t, logs, "SP:1,two has=true")
}
