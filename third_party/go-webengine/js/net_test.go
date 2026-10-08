// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package js

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dop251/goja"
	"github.com/go-webengine/engine/dom"
)

// rtFunc adapts a function to an http.RoundTripper.
type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// netServer is a small server used by the fetch/XHR tests.
func netServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/x.txt":
			w.Write([]byte("hello"))
		case "/data.json":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"name":"go","n":42}`))
		case "/latin1":
			w.Header().Set("Content-Type", "text/plain; charset=iso-8859-1")
			w.Write([]byte{0x63, 0x61, 0x66, 0xe9}) // "café" in latin-1
		case "/echo":
			w.Header().Set("X-Method", r.Method)
			w.Header().Set("X-Custom", r.Header.Get("X-Custom"))
			body, _ := io.ReadAll(r.Body)
			w.Write(body)
		case "/bad.json":
			w.Write([]byte(`{not json`))
		default:
			w.WriteHeader(404)
		}
	}))
}

func runNet(t *testing.T, srv *httptest.Server, client *http.Client, body string) ([]string, Result) {
	t.Helper()
	root, err := dom.Parse(`<html><body><script>` + body + `</script></body></html>`)
	if err != nil {
		t.Fatal(err)
	}
	var logs []string
	res := Run(root, Options{
		PageURL:   srv.URL + "/",
		UserAgent: "TestUA",
		Client:    client,
		Timeout:   3 * time.Second,
		Log:       func(s string) { logs = append(logs, s) },
	})
	return logs, res
}

func TestFetchTextJSONHeaders(t *testing.T) {
	srv := netServer()
	defer srv.Close()
	logs, _ := runNet(t, srv, http.DefaultClient, `
		fetch('/x.txt').then(function(r){
			console.log('ct='+(r.headers.has('content-type'))+' url='+(r.url.indexOf('/x.txt')>=0));
			return r.text();
		}).then(function(t){ console.log('text='+t); });
		fetch('/data.json').then(function(r){
			console.log('ok='+r.ok+' status='+r.status+' st='+r.statusText);
			return r.json();
		}).then(function(j){ console.log('json='+j.name+':'+j.n); });
		fetch('/data.json').then(function(r){ return r.clone().text(); }).then(function(t){ console.log('clone='+(t.length>0)); });
		fetch('/data.json').then(function(r){ return r.arrayBuffer(); }).then(function(b){ console.log('ab='+b.byteLength); });
		fetch('/missing').then(function(r){ console.log('missing='+r.ok+':'+r.status); });
		fetch('/bad.json').then(function(r){ return r.json(); }).then(function(){ console.log('should-not'); }, function(e){ console.log('jsonerr:'+e); });
	`)
	mustHave(t, logs, "ct=true url=true", "text=hello", "ok=true status=200 st=OK",
		"json=go:42", "clone=true", "ab=20", "missing=false:404", "invalid json")
}

func TestFetchCharsetDecode(t *testing.T) {
	srv := netServer()
	defer srv.Close()
	logs, _ := runNet(t, srv, http.DefaultClient, `
		fetch('/latin1').then(function(r){return r.text();}).then(function(t){ console.log('latin1='+t); });
	`)
	mustHave(t, logs, "latin1=café")
}

func TestFetchPostWithInit(t *testing.T) {
	srv := netServer()
	defer srv.Close()
	logs, _ := runNet(t, srv, http.DefaultClient, `
		fetch('/echo', {method:'POST', headers:{'X-Custom':'abc'}, body:'payload'})
			.then(function(r){ console.log('m='+r.headers.get('x-method')+' c='+r.headers.get('x-custom')); return r.text(); })
			.then(function(t){ console.log('echo='+t); });
		fetch(new Request('/x.txt')).then(function(r){return r.text();}).then(function(t){console.log('reqobj='+t);});
	`)
	mustHave(t, logs, "m=POST c=abc", "echo=payload", "reqobj=hello")
}

func TestFetchError(t *testing.T) {
	srv := netServer()
	defer srv.Close()
	logs, _ := runNet(t, srv, &http.Client{Timeout: 500 * time.Millisecond}, `
		fetch('http://127.0.0.1:1/nope').then(function(){ console.log('resolved'); }, function(e){ console.log('rejected='+((''+e).indexOf('fetch failed')>=0)); });
		fetch('data:text/plain,hi').then(function(){ console.log('resolved2'); }, function(e){ console.log('dataurl-rejected'); });
	`)
	mustHave(t, logs, "rejected=true", "dataurl-rejected")
	if has(logs, "resolved") {
		t.Fatal("bad host should reject, not resolve")
	}
}

// TestFetchRoutesThroughClient is the critical guard: JS-initiated requests must
// go through the engine's own *http.Client (whose transport a proxy can wrap
// with an SSRF filter), NOT a fresh client. We inject a transport that rewrites
// every response and assert the JS fetch observed it.
func TestFetchRoutesThroughClient(t *testing.T) {
	srv := netServer()
	defer srv.Close()
	var seen int
	client := &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
		seen++
		return &http.Response{
			StatusCode: 200,
			Status:     "200 OK",
			Header:     http.Header{"Content-Type": {"text/plain"}},
			Body:       io.NopCloser(strings.NewReader("ROUTED:" + r.URL.Path)),
			Request:    r,
		}, nil
	})}
	logs, _ := runNet(t, srv, client, `
		fetch('/anything').then(function(r){return r.text();}).then(function(t){ console.log('routed='+t); });
		var x=new XMLHttpRequest(); x.open('GET','/xhrpath', false); x.send(); console.log('xhr-routed='+x.responseText);
	`)
	mustHave(t, logs, "routed=ROUTED:/anything", "xhr-routed=ROUTED:/xhrpath")
	if seen < 2 {
		t.Fatalf("expected both JS requests through injected transport, saw %d", seen)
	}
}

func TestFetchRequestLimit(t *testing.T) {
	srv := netServer()
	defer srv.Close()
	// Fire more than maxRequests fetches; those past the cap must reject.
	logs, _ := runNet(t, srv, http.DefaultClient, `
		var okc=0, errc=0, pending=`+itoa(maxRequests+20)+`;
		for (var i=0;i<`+itoa(maxRequests+20)+`;i++){
			fetch('/x.txt').then(function(){okc++; done();}, function(){errc++; done();});
		}
		function done(){ if(--pending===0){ console.log('ok='+okc+' err='+errc); } }
	`)
	// At least the over-cap requests rejected.
	got := false
	for _, l := range logs {
		if strings.Contains(l, "err=") && !strings.Contains(l, "err=0") {
			got = true
		}
	}
	if !got {
		t.Fatalf("expected some fetches to hit the request cap: %v", logs)
	}
}

func TestXHRAsyncAndListeners(t *testing.T) {
	srv := netServer()
	defer srv.Close()
	logs, _ := runNet(t, srv, http.DefaultClient, `
		var x=new XMLHttpRequest();
		var states=[];
		x.onreadystatechange=function(){ states.push(x.readyState); if(x.readyState===4){ console.log('rsc='+x.status); } };
		x.addEventListener('load', function(){ console.log('evt-load='+x.responseText+' url='+(x.responseURL.indexOf('/x.txt')>=0)); });
		x.open('GET','/x.txt', true);
		x.send();
		var j=new XMLHttpRequest(); j.responseType='json'; j.open('GET','/data.json', false); j.send();
		console.log('xhr-json='+j.response.n);
		var bad=new XMLHttpRequest();
		bad.onerror=function(){ console.log('xhr-error'); };
		bad.open('GET','http://127.0.0.1:1/x', false); bad.send();
	`)
	mustHave(t, logs, "rsc=200", "evt-load=hello url=true", "xhr-json=42", "xhr-error")
}

func TestHeadersObject(t *testing.T) {
	root, _ := dom.Parse(`<html><body><script>
		var h=new Headers({'A':'1'});
		h.set('b','2'); h.append('a','3'); h.append('c','4');
		var seen=[];
		h.forEach(function(v,k){ seen.push(k+'='+v); });
		console.log('get='+h.get('a')+' hasB='+h.has('b')+' hasZ='+h.has('z')+' count='+seen.length);
		h.delete('b'); console.log('afterDel='+h.has('b'));
	</script></body></html>`)
	var logs []string
	Run(root, Options{PageURL: "https://x/", Client: http.DefaultClient, Log: func(s string) { logs = append(logs, s) }})
	mustHave(t, logs, "get=1, 3", "hasB=true", "hasZ=false", "afterDel=false")
}

func TestMessageChannel(t *testing.T) {
	root, _ := dom.Parse(`<html><body><script>
		var mc=new MessageChannel();
		mc.port1.onmessage=function(e){ console.log('p1<-'+e.data); };
		mc.port2.addEventListener('message', function(e){ console.log('p2<-'+e.data); });
		mc.port2.postMessage('to1');
		mc.port1.postMessage('to2');
		mc.port1.start(); mc.port2.close();
	</script></body></html>`)
	var logs []string
	Run(root, Options{PageURL: "https://x/", Client: http.DefaultClient, Log: func(s string) { logs = append(logs, s) }})
	mustHave(t, logs, "p1<-to1", "p2<-to2")
}

// TestDoRequestErrorBranches covers the guard paths directly.
func TestDoRequestErrorBranches(t *testing.T) {
	// No client.
	b := &binder{vm: goja.New(), opt: Options{PageURL: "https://x/"}, deadman: time.Now().Add(time.Second)}
	if _, err := b.doRequest("GET", "/y", nil, ""); err == nil {
		t.Fatal("nil client should error")
	}
	// Request cap.
	b2 := &binder{vm: goja.New(), opt: Options{PageURL: "https://x/", Client: http.DefaultClient, Ctx: context.Background()}, deadman: time.Now().Add(time.Second), reqCount: maxRequests}
	if _, err := b2.doRequest("GET", "/y", nil, ""); err == nil {
		t.Fatal("request cap should error")
	}
	// Unsupported URL scheme.
	b3 := &binder{vm: goja.New(), opt: Options{PageURL: "https://x/", Client: http.DefaultClient, Ctx: context.Background()}, deadman: time.Now().Add(time.Second)}
	if _, err := b3.doRequest("GET", "data:text/plain,hi", nil, ""); err == nil {
		t.Fatal("data: URL should error")
	}
	// Budget exhausted.
	b4 := &binder{vm: goja.New(), opt: Options{PageURL: "https://x/", Client: http.DefaultClient, Ctx: context.Background()}, deadman: time.Now().Add(-time.Second)}
	if _, err := b4.doRequest("GET", "/y", nil, ""); err == nil {
		t.Fatal("exhausted budget should error")
	}
}

func TestNetHelpers(t *testing.T) {
	if errString("boom").Error() != "boom" {
		t.Fatal("errString")
	}
	if orUndefined(nil) != goja.Undefined() {
		t.Fatal("orUndefined nil")
	}
	vm := goja.New()
	v := vm.ToValue("x")
	if orUndefined(v) != v {
		t.Fatal("orUndefined passthrough")
	}
	// requestInit with a non-object and with an object.
	m, h, bd := requestInit(goja.Undefined())
	if m != "GET" || len(h) != 0 || bd != "" {
		t.Fatal("requestInit default")
	}
	obj := vm.NewObject()
	obj.Set("method", "PUT")
	obj.Set("body", "b")
	hdr := vm.NewObject()
	hdr.Set("X", "1")
	obj.Set("headers", hdr)
	m2, h2, bd2 := requestInit(obj)
	if m2 != "PUT" || bd2 != "b" || h2["X"] != "1" {
		t.Fatalf("requestInit object: %s %v %s", m2, h2, bd2)
	}
	// fetchURL from a string and from a Request-like object.
	if fetchURL(vm.ToValue("/p")) != "/p" {
		t.Fatal("fetchURL string")
	}
	req := vm.NewObject()
	req.Set("url", "/q")
	if fetchURL(req) != "/q" {
		t.Fatal("fetchURL object")
	}
	// httpHeaderMap flattening.
	hm := httpHeaderMap(http.Header{"Content-Type": {"text/html"}, "Empty": {}})
	if hm["content-type"] != "text/html" {
		t.Fatalf("httpHeaderMap: %v", hm)
	}
}

// itoa is a tiny int-to-string used only in test script bodies.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func TestDoRequestSuccessDirect(t *testing.T) {
	srv := netServer()
	defer srv.Close()
	// A far deadman exercises the perRequestTimeout cap branch and the full
	// request path (headers loop, final URL, redirected=false).
	b := &binder{
		vm:      goja.New(),
		opt:     Options{PageURL: srv.URL + "/", Client: http.DefaultClient, Ctx: context.Background(), UserAgent: "UA"},
		deadman: time.Now().Add(60 * time.Second),
	}
	res, err := b.doRequest("POST", "/echo", map[string]string{"X-Custom": "z"}, "body")
	if err != nil {
		t.Fatal(err)
	}
	if res.status != 200 || res.redirected || res.headers.Get("X-Custom") != "z" || res.text() != "body" {
		t.Fatalf("doRequest result: %+v text=%q", res, res.text())
	}
}

func TestXHRAccessorsAndMethods(t *testing.T) {
	srv := netServer()
	defer srv.Close()
	logs, _ := runNet(t, srv, http.DefaultClient, `
		var x=new XMLHttpRequest();
		x.responseType='text';
		x.timeout=1000; x.withCredentials=true;
		x.onload=function(){};
		console.log('rt='+x.responseType+' to='+x.timeout+' wc='+x.withCredentials+' onload='+(typeof x.onload));
		console.log('grh='+x.getResponseHeader('a')+' gah='+JSON.stringify(x.getAllResponseHeaders()));
		x.overrideMimeType('text/plain');
		x.removeEventListener('load', function(){});
		console.log('disp='+x.dispatchEvent({type:'x'}));
		x.abort();
		x.open('GET','/data.json', false); x.responseType='json'; x.send();
		console.log('jsonresp='+x.response.name);
		fetch('/data.json').then(function(r){ return r.blob(); }).then(function(b){ console.log('blob='+(typeof b)); });
	`)
	mustHave(t, logs, "rt=text to=0 wc=false onload=function", "grh=null gah=\"\"",
		"disp=true", "jsonresp=go", "blob=string")
}
