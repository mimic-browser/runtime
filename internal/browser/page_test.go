package browser

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	chrome152 "github.com/moreveal/mimic/chrome/152"
	"github.com/moreveal/mimic/compatibility"
	gojaengine "github.com/moreveal/mimic/internal/engine/goja"
	quickjsengine "github.com/moreveal/mimic/internal/engine/quickjs"
	v8engine "github.com/moreveal/mimic/internal/engine/v8"
	"github.com/moreveal/mimic/internal/network"
	"github.com/moreveal/mimic/internal/trace"
)

type surfaceTestBundle struct {
	compatibility.Bundle
	surface compatibility.WebAPISurface
}

func (b surfaceTestBundle) Surface() *compatibility.WebAPISurface { return &b.surface }

func testPage(t *testing.T) *Page {
	t.Helper()
	b, err := New(gojaengine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	c := b.NewContext()
	t.Cleanup(func() { _ = c.Close() })
	p, err := c.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	p.Trace().Start()
	return p
}

func TestExternalCommandWaiterSignal(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	p.LockCommands()
	acquired := make(chan struct{})
	go func() {
		p.LockExternalCommand()
		close(acquired)
		p.UnlockCommands()
	}()

	deadline := time.Now().Add(time.Second)
	for !p.ExternalCommandWaiting() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !p.ExternalCommandWaiting() {
		p.UnlockCommands()
		t.Fatal("external command did not advertise its queued state")
	}
	p.UnlockCommands()
	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatal("external command did not acquire the released Page boundary")
	}
	if p.ExternalCommandWaiting() {
		t.Fatal("external waiter signal remained set after acquisition")
	}
}

func TestRealmUsesSelectedCompatibilityBundleSurface(t *testing.T) {
	parallelBrowserTest(t)
	bundle := surfaceTestBundle{Bundle: chrome152.New(), surface: compatibility.WebAPISurface{GeneratedJavaScript: `globalThis.__selectedBundle = "custom"`}}
	b, err := New(gojaengine.Factory{}, bundle)
	if err != nil {
		t.Fatal(err)
	}
	p, err := b.NewContext().NewPage()
	if err != nil {
		t.Fatal(err)
	}
	value, err := p.Evaluate(context.Background(), `__selectedBundle`)
	if err != nil || value != "custom" {
		t.Fatalf("selected bundle surface was not installed: value=%v err=%v", value, err)
	}
}
func TestNavigateScriptsAndCanonicalViews(t *testing.T) {
	parallelBrowserTest(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/external.js" {
			fmt.Fprint(w, `window.externalRan=true`)
			return
		}
		fmt.Fprint(w, `<!doctype html><title>Hello</title><div id="x"></div><script>window.inlineRan=navigator.userAgent.includes('Chrome/152')</script><script src="/external.js"></script>`)
	}))
	defer ts.Close()
	p := testPage(t)
	if err := p.Navigate(context.Background(), ts.URL); err != nil {
		t.Fatal(err)
	}
	v, err := p.Evaluate(context.Background(), `({title:document.title,id:document.querySelector('#x').id,inlineRan,externalRan,dpr:devicePixelRatio,screenWidth:screen.width})`)
	if err != nil {
		t.Fatal(err)
	}
	m := v.(map[string]interface{})
	physicalWidth := float64(p.Environment().Display.PhysicalWidth)
	if m["title"] != "Hello" || m["id"] != "x" || m["inlineRan"] != true || m["externalRan"] != true || numberValue(m["dpr"])*numberValue(m["screenWidth"]) != physicalWidth {
		t.Fatalf("unexpected result: %#v", m)
	}
}

func TestScriptTypesAndStaticModuleGraph(t *testing.T) {
	parallelBrowserTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/":
			fmt.Fprint(w, `<script>window.order=['classic-before']</script><script type="application/json">{"bad":1}</script><script type="text/plain">throw new Error('must not run')</script><script type="module" src="/entry.js"></script><script>order.push('classic-after')</script>`)
		case "/entry.js":
			w.Header().Set("Content-Type", "text/javascript")
			fmt.Fprint(w, `import {answer} from './dep.js';order.push('module:'+answer);window.moduleCurrentScript=document.currentScript`)
		case "/dep.js":
			w.Header().Set("Content-Type", "text/javascript")
			fmt.Fprint(w, `export const answer=42`)
		default:
			http.NotFound(w, req)
		}
	}))
	defer server.Close()
	b, err := New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	p, err := b.NewContext().NewPage()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if err := p.Navigate(context.Background(), server.URL); err != nil {
		t.Fatal(err)
	}
	value, err := p.Evaluate(context.Background(), `(()=>({order,types:[...document.scripts].map(s=>s.type),moduleCurrentScript:globalThis.moduleCurrentScript}))()`)
	if err != nil {
		t.Fatal(err)
	}
	result := value.(map[string]any)
	if fmt.Sprint(result["order"]) != "[classic-before classic-after module:42]" {
		t.Fatalf("unexpected script order: %#v", result)
	}
	if fmt.Sprint(result["types"]) != "[ application/json text/plain module ]" {
		t.Fatalf("script type reflection mismatch: %#v", result["types"])
	}
	if result["moduleCurrentScript"] != nil {
		t.Fatalf("document.currentScript must be null in a module: %#v", result["moduleCurrentScript"])
	}
}

func TestTextEncoderUTF8AndBoundedDestination(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	defer p.Close()
	value, err := p.Evaluate(context.Background(), `(()=>{const encoder=new TextEncoder(),destination=new Uint8Array(4),progress=encoder.encodeInto('\u00e9\u{1f642}',destination);return{tag:Object.prototype.toString.call(encoder),encoding:encoder.encoding,bytes:Array.from(encoder.encode('A\u00e9\u{1f642}')),progress,partial:Array.from(destination)}})()`)
	if err != nil {
		t.Fatal(err)
	}
	result := value.(map[string]any)
	if result["tag"] != "[object TextEncoder]" || result["encoding"] != "utf-8" || fmt.Sprint(result["bytes"]) != "[65 195 169 240 159 153 130]" || fmt.Sprint(result["progress"]) != "map[read:1 written:2]" || fmt.Sprint(result["partial"]) != "[195 169 0 0]" {
		t.Fatalf("unexpected TextEncoder result: %#v", result)
	}
}

func TestDocumentCreateTextNode(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	defer p.Close()
	value, err := p.Evaluate(context.Background(), `(()=>{const value=document.createTextNode('hello');return[Object.prototype.toString.call(value),value.nodeType,value.nodeName,value.data,value.textContent,value.length,value.hasChildNodes(),value.parentNode]})()`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := fmt.Sprint(value), `[[object Text] 3 #text hello hello 5 false <nil>]`; got != want {
		t.Fatalf("unexpected text node: %s", got)
	}
}

func TestWebCryptoRSAOAEPSPKIImportAndEncrypt(t *testing.T) {
	parallelBrowserTest(t)
	b, err := New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	p, err := b.NewContext().NewPage()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	navigateCapabilityFixture(t, p)
	value, err := p.Evaluate(context.Background(), `(async()=>{const der=Uint8Array.from(atob('MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQDfK5CElVUHUMX0sCpkNSBa5EQ6QSmK0ZKt1tweJ7Q8LeuwFyXv9HeJq6Cp+naXAoewBUdP7ImRmtrADIwAqLlnbBUcTMBLlC/Mmx0lSFPZg2QLOaIhNt5ES3vwteKUplY2OHPv2NP7Ww/EvKNu8768FWLho+Dw3XI8xPUF5HwbQwIDAQAB'),c=>c.charCodeAt(0));const key=await crypto.subtle.importKey('spki',der,{name:'RSA-OAEP',hash:'SHA-1'},true,['encrypt']);const encrypted=await crypto.subtle.encrypt({name:'RSA-OAEP'},key,new TextEncoder().encode('regression'));return[Object.prototype.toString.call(key),key.type,key.extractable,key.algorithm.name,key.algorithm.modulusLength,Array.from(key.algorithm.publicExponent),key.algorithm.hash.name,key.usages,Object.prototype.toString.call(encrypted),encrypted.byteLength]})()`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := fmt.Sprint(value), `[[object CryptoKey] public true RSA-OAEP 1024 [1 0 1] SHA-1 [encrypt] [object ArrayBuffer] 128]`; got != want {
		t.Fatalf("unexpected RSA-OAEP result: %s", got)
	}
}

func TestWebCryptoAESGCMRawImportEncryptAndDecrypt(t *testing.T) {
	parallelBrowserTest(t)
	b, err := New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	p, err := b.NewContext().NewPage()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	navigateCapabilityFixture(t, p)
	value, err := p.Evaluate(context.Background(), `(async()=>{const hex=value=>Array.from(new Uint8Array(value),byte=>byte.toString(16).padStart(2,'0')).join(''),key=await crypto.subtle.importKey('raw',new Uint8Array(16),{name:'AES-GCM'},false,['encrypt','decrypt']),iv=new Uint8Array(12),empty=await crypto.subtle.encrypt({name:'AES-GCM',iv},key,new Uint8Array()),plain=new TextEncoder().encode('amazon challenge'),sealed=await crypto.subtle.encrypt({name:'AES-GCM',iv,additionalData:new Uint8Array([1,2,3]),tagLength:128},key,plain),opened=await crypto.subtle.decrypt({name:'AES-GCM',iv,additionalData:new Uint8Array([1,2,3]),tagLength:128},key,sealed);return[Object.prototype.toString.call(key),key.type,key.extractable,key.algorithm.name,key.algorithm.length,key.usages,hex(empty),new TextDecoder().decode(opened)]})()`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := fmt.Sprint(value), `[[object CryptoKey] secret false AES-GCM 128 [encrypt decrypt] 58e2fccefa7e3061367f1d57a4e7455a amazon challenge]`; got != want {
		t.Fatalf("unexpected AES-GCM result: %s", got)
	}
}

func TestFetchValueConstructors(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	defer p.Close()
	value, err := p.Evaluate(context.Background(), `(()=>{const headers=new Headers({X_Test:'one'});headers.append('x-test','two');const request=new Request('http://www.example.com/path',{headers,method:'POST'}),response=new Response();return{headers:[Object.prototype.toString.call(headers),headers.get('x-test'),JSON.stringify(Array.from(headers))],request:[Object.prototype.toString.call(request),request.url,request.method,request.mode,request.credentials,request.cache,request.redirect,request.referrer],response:[Object.prototype.toString.call(response),response.status,response.statusText,response.ok,response.type,response.url,response.redirected]}})()`)
	if err != nil {
		t.Fatal(err)
	}
	result := value.(map[string]any)
	if fmt.Sprint(result["headers"]) != `[[object Headers] two [["x-test","two"],["x_test","one"]]]` || fmt.Sprint(result["request"]) != "[[object Request] http://www.example.com/path POST cors same-origin default follow about:client]" || fmt.Sprint(result["response"]) != "[[object Response] 200  true default  false]" {
		t.Fatalf("unexpected fetch constructor result: %#v", result)
	}
}

func TestDOMMatrixIdentity(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	defer p.Close()
	value, err := p.Evaluate(context.Background(), `(()=>{const value=new DOMMatrix();return[Object.prototype.toString.call(value),value.is2D,value.a,value.d,value.m11,value.m22,value.m41,value.m42,value.toString(),JSON.stringify(Array.from(value.toFloat64Array()))]})()`)
	if err != nil {
		t.Fatal(err)
	}
	want := `[[object DOMMatrix] true 1 1 1 1 0 0 matrix(1, 0, 0, 1, 0, 0) [1,0,0,0,0,1,0,0,0,0,1,0,0,0,0,1]]`
	if fmt.Sprint(value) != want {
		t.Fatalf("unexpected DOMMatrix identity: %#v", value)
	}
}

func TestV8NavigationDispatchesDOMContentLoadedAndLoadInOrder(t *testing.T) {
	parallelBrowserTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `<script>
			window.lifecycleOrder=['script'];
			document.addEventListener('DOMContentLoaded',()=>{
				lifecycleOrder.push('dom-content-loaded:'+document.readyState);
				Promise.resolve().then(()=>lifecycleOrder.push('dom-content-loaded-microtask'));
			});
			addEventListener('load',()=>lifecycleOrder.push('load:'+document.readyState));
		</script>`)
	}))
	defer server.Close()
	b, err := New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	p, err := b.NewContext().NewPage()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if err := p.Navigate(context.Background(), server.URL); err != nil {
		t.Fatal(err)
	}
	value, err := p.Evaluate(context.Background(), `lifecycleOrder.join(',')`)
	if err != nil {
		t.Fatal(err)
	}
	want := "script,dom-content-loaded:interactive,dom-content-loaded-microtask,load:complete"
	if value != want {
		t.Fatalf("top-level lifecycle order = %#v, want %q", value, want)
	}
}

func TestV8ParserIframeCreatesAndNavigatesBrowsingContextWithoutContentWindowAccess(t *testing.T) {
	parallelBrowserTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/child" {
			fmt.Fprint(w, `<script>parent.postMessage({phase:'child-script',url:location.href},'*')</script>`)
			return
		}
		fmt.Fprint(w, `<script>window.parserMessages=[];addEventListener('message',event=>parserMessages.push(event.data))</script><iframe src="/child"></iframe>`)
	}))
	defer server.Close()
	b, err := New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	p, err := b.NewContext().NewPage()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if err := p.Navigate(context.Background(), server.URL); err != nil {
		t.Fatal(err)
	}
	value, err := p.Evaluate(context.Background(), `({messages:parserMessages,frames:document.querySelector('iframe').contentWindow!==null})`)
	if err != nil {
		t.Fatal(err)
	}
	result := value.(map[string]any)
	messages := result["messages"].([]any)
	if result["frames"] != true || len(messages) != 1 || messages[0].(map[string]any)["phase"] != "child-script" || messages[0].(map[string]any)["url"] != server.URL+"/child" {
		t.Fatalf("parser iframe was not attached/navigated during document loading: %#v", result)
	}
}

func TestV8InitScriptRunsInChildRealmBeforeDocumentScripts(t *testing.T) {
	parallelBrowserTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/child" {
			fmt.Fprint(w, `<script>parent.postMessage({realm:__frameInit.realm,readyState:__frameInit.readyState,arrayLocal:__frameInit.array===Array},'*')</script>`)
			return
		}
		fmt.Fprint(w, `<script>window.childInit=null;addEventListener('message',event=>childInit=event.data)</script><iframe src="/child"></iframe>`)
	}))
	defer server.Close()
	b, err := New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	p, err := b.NewContext().NewPage()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	p.AddInitScript(`globalThis.__frameInit={realm:parent===window?'top':'child',readyState:document.readyState,array:Array}`)
	if err := p.Navigate(context.Background(), server.URL); err != nil {
		t.Fatal(err)
	}
	value, err := p.Evaluate(context.Background(), `childInit`)
	if err != nil {
		t.Fatal(err)
	}
	result := value.(map[string]any)
	if result["realm"] != "child" || result["readyState"] != "loading" || result["arrayLocal"] != true {
		t.Fatalf("child init script did not run before document scripts in its realm: %#v", result)
	}
}

func TestHistoryStateUpdatesDynamicResourceReferrer(t *testing.T) {
	parallelBrowserTest(t)
	var scriptReferrer string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/dynamic.js" {
			scriptReferrer = r.Referer()
			fmt.Fprint(w, `window.dynamicLoaded=true`)
			return
		}
		fmt.Fprint(w, `<script>history.replaceState(null,'','/?token=canonical');const s=document.createElement('script');s.src='/dynamic.js';document.head.appendChild(s)</script>`)
	}))
	defer ts.Close()
	p := testPage(t)
	if err := p.Navigate(context.Background(), ts.URL); err != nil {
		t.Fatal(err)
	}
	if scriptReferrer != ts.URL+"/?token=canonical" {
		t.Fatalf("dynamic resource used stale document referrer %q", scriptReferrer)
	}
}

func TestImageTransportDoesNotBlockDynamicScriptScheduling(t *testing.T) {
	serialBrowserTest(t)
	scriptRequested := make(chan struct{})
	imageObservedScript := make(chan bool, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/image.png":
			select {
			case <-scriptRequested:
				imageObservedScript <- true
			case <-time.After(time.Second):
				imageObservedScript <- false
			}
		case "/dynamic.js":
			close(scriptRequested)
			fmt.Fprint(w, `window.dynamicAfterImage=true`)
		default:
			fmt.Fprint(w, `<body></body>`)
		}
	}))
	defer ts.Close()
	p := testPage(t)
	if err := p.Navigate(context.Background(), ts.URL); err != nil {
		t.Fatal(err)
	}
	// Measure concurrent resource work after generated realm bootstrap.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := p.Evaluate(ctx, `const i=document.createElement('img');i.src='/image.png';document.body.appendChild(i);const s=document.createElement('script');s.src='/dynamic.js';document.head.appendChild(s)`); err != nil {
		t.Fatal(err)
	}
	if err := p.AdvanceTime(ctx, time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if observed := <-imageObservedScript; !observed {
		t.Fatal("image transport blocked the realm before the dynamic script request")
	}
}
func TestRealmIsolation(t *testing.T) {
	parallelBrowserTest(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<script>globalThis.secret=(globalThis.secret||0)+1</script>`)
	}))
	defer ts.Close()
	p := testPage(t)
	if err := p.Navigate(context.Background(), ts.URL); err != nil {
		t.Fatal(err)
	}
	if err := p.Navigate(context.Background(), ts.URL); err != nil {
		t.Fatal(err)
	}
	v, err := p.Evaluate(context.Background(), `secret`)
	if err != nil {
		t.Fatal(err)
	}
	if v.(int64) != 1 {
		t.Fatalf("realm leaked: %v", v)
	}
}
func TestTimerAndFetchUseScheduler(t *testing.T) {
	parallelBrowserTest(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/data" {
			fmt.Fprint(w, "ok")
			return
		}
		fmt.Fprint(w, `<script>window.order=[]; setTimeout(()=>order.push('timer'),0); window.fetchDone=fetch('/data').then(r=>r.text()).then(x=>order.push(x))</script>`)
	}))
	defer ts.Close()
	p := testPage(t)
	if err := p.Navigate(context.Background(), ts.URL); err != nil {
		t.Fatal(err)
	}
	v, err := p.Evaluate(context.Background(), `fetchDone.then(()=>order.join(','))`)
	if err != nil {
		t.Fatal(err)
	}
	if v != "timer,ok" {
		t.Fatalf("order=%v", v)
	}
}

func TestCanonicalClockAndStorage(t *testing.T) {
	parallelBrowserTest(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "<title>x</title>") }))
	defer ts.Close()
	p := testPage(t)
	if err := p.Navigate(context.Background(), ts.URL); err != nil {
		t.Fatal(err)
	}
	v, err := p.Evaluate(context.Background(), `new Promise(resolve=>setTimeout(()=>{localStorage.setItem('k','local');sessionStorage.setItem('k','session');document.cookie='a=b; Path=/';resolve({wall:Date.now(),integral:Number.isInteger(Date.now()),mono:performance.now(),local:localStorage.getItem('k'),session:sessionStorage.getItem('k'),cookie:document.cookie})},25))`)
	if err != nil {
		t.Fatal(err)
	}
	m := v.(map[string]interface{})
	wall := float64(int64Number(m["wall"]))
	mono, ok := m["mono"].(float64)
	if !ok {
		mono = float64(int64Number(m["mono"]))
	}
	wantWall := float64(p.PerformanceOrigin().UnixMilli()) + mono
	if diff := wall - wantWall; diff < -2 || diff > 2 || mono < 25 || m["integral"] != true || m["local"] != "local" || m["session"] != "session" || m["cookie"] != "a=b" {
		t.Fatalf("incoherent state: %#v", m)
	}
}

func TestPerformanceUsesChromeObjectModel(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	value, err := p.Evaluate(context.Background(), `({instance:performance instanceof Performance,own:Object.getOwnPropertyNames(performance),methodOwn:Object.prototype.hasOwnProperty.call(performance,'getEntries'),prototype:Object.getPrototypeOf(performance)===Performance.prototype})`)
	if err != nil {
		t.Fatal(err)
	}
	result := value.(map[string]any)
	if result["instance"] != true || result["prototype"] != true || result["methodOwn"] != false || len(result["own"].([]any)) != 0 {
		t.Fatalf("unexpected Performance object model: %#v", result)
	}
}

func TestResourceTimingProjectsServerTimingFromResponseHeaders(t *testing.T) {
	parallelBrowserTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/metric" {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("Server-Timing", `edge;dur=12.5;desc="cache, hit", app;dur=3`)
			_, _ = fmt.Fprint(w, `{}`)
			return
		}
		_, _ = fmt.Fprint(w, `<!doctype html><title>timing</title>`)
	}))
	defer server.Close()
	p := testPage(t)
	if err := p.Navigate(context.Background(), server.URL); err != nil {
		t.Fatal(err)
	}
	value, err := p.Evaluate(context.Background(), `fetch('/metric').then(()=>{const entry=performance.getEntriesByName(location.origin+'/metric')[0],timing=entry.serverTiming;return{contentType:entry.contentType,frozen:Object.isFrozen(timing),brands:timing.map(value=>value instanceof PerformanceServerTiming),json:timing.map(value=>value.toJSON()),entryJSON:entry.toJSON().serverTiming.map(value=>value.toJSON())}})`)
	if err != nil {
		t.Fatal(err)
	}
	result := value.(map[string]any)
	if result["contentType"] != "application/json" || result["frozen"] != true || fmt.Sprint(result["brands"]) != "[true true]" {
		t.Fatalf("unexpected Server-Timing projection: %#v", result)
	}
	want := `[map[description:cache, hit duration:12.5 name:edge] map[description: duration:3 name:app]]`
	if fmt.Sprint(result["json"]) != want || fmt.Sprint(result["entryJSON"]) != want {
		t.Fatalf("unexpected Server-Timing values: %#v", result)
	}
}

func TestTimerStringHandlerAndArguments(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	if _, err := p.Evaluate(context.Background(), `globalThis.timerResult='';setTimeout("timerResult+='s'",0);setTimeout((a,b)=>timerResult+=a+b,0,'a','b')`); err != nil {
		t.Fatal(err)
	}
	value, err := p.Evaluate(context.Background(), `timerResult`)
	if err != nil || value != "sab" {
		t.Fatalf("timer handlers: value=%v err=%v", value, err)
	}
}

func TestXMLHttpRequestHeadersAndLifecycle(t *testing.T) {
	parallelBrowserTest(t)
	captured := make(chan [2]string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/document" {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, "<!doctype html><title>XHR lifecycle</title>")
			return
		}
		if req.URL.Path != "/xhr" || req.Method != http.MethodPost {
			http.NotFound(w, req)
			return
		}
		body, _ := io.ReadAll(req.Body)
		captured <- [2]string{req.Header.Get("X-Test"), string(body)}
		w.Header().Set("X-Reply", "yes")
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer srv.Close()
	p := testPage(t)
	// Exercise the intended same-origin lifecycle, rather than relying on a
	// cross-origin request from an opaque about:blank document bypassing CORS.
	if err := p.Navigate(context.Background(), srv.URL+"/document"); err != nil {
		t.Fatal(err)
	}
	v, err := p.Evaluate(context.Background(), `new Promise(resolve => {
  const states = [], xhr = new XMLHttpRequest();
  xhr.onreadystatechange = () => states.push(xhr.readyState);
  xhr.onloadend = () => resolve({
    status: xhr.status,
    text: xhr.responseText,
    header: xhr.getResponseHeader('X-Reply'),
    states: states.join(',')
  });
  xhr.open('POST', `+strconv.Quote(srv.URL+"/xhr")+`);
  xhr.setRequestHeader('X-Test', 'one');
  xhr.setRequestHeader('X-Test', 'two');
  xhr.send('body');
})`)
	if err != nil {
		t.Fatal(err)
	}
	var request [2]string
	select {
	case request = <-captured:
	default:
		t.Fatal("XHR completed without the expected POST reaching transport")
	}
	requestHeader, requestBody := request[0], request[1]
	m := v.(map[string]any)
	if requestHeader != "one, two" || requestBody != "body" || m["status"] != int64(200) || m["text"] != `{"ok":true}` || m["header"] != "yes" || m["states"] != "1,2,3,4" {
		t.Fatalf("unexpected XHR semantics: requestHeader=%q requestBody=%q result=%#v", requestHeader, requestBody, m)
	}
}

func TestRTCDataChannelInitialState(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	v, err := p.Evaluate(context.Background(), `(()=>{const peer=new RTCPeerConnection(),channel=peer.createDataChannel('probe',{ordered:false,maxRetransmits:2,protocol:'x'});const result={tag:Object.prototype.toString.call(channel),label:channel.label,ordered:channel.ordered,maxRetransmits:channel.maxRetransmits,maxPacketLifeTime:channel.maxPacketLifeTime,protocol:channel.protocol,negotiated:channel.negotiated,id:channel.id,readyState:channel.readyState,binaryType:channel.binaryType,reliable:channel.reliable};channel.close();peer.close();result.closed=channel.readyState;return result})()`)
	if err != nil {
		t.Fatal(err)
	}
	m := v.(map[string]any)
	if m["tag"] != "[object RTCDataChannel]" || m["label"] != "probe" || m["ordered"] != false || m["maxRetransmits"] != int64(2) || m["maxPacketLifeTime"] != nil || m["protocol"] != "x" || m["negotiated"] != false || m["id"] != nil || m["readyState"] != "connecting" || m["binaryType"] != "arraybuffer" || m["reliable"] != false || m["closed"] != "closing" {
		t.Fatalf("unexpected RTCDataChannel semantics: %#v", m)
	}
}

func TestRTCSessionDescriptionUsesWebIDLAccessorsAndInternalSlots(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	v, err := p.Evaluate(context.Background(), `(()=>{
		const value=new RTCSessionDescription({type:'offer',sdp:'v=0\r\n'}),prototype=RTCSessionDescription.prototype;
		const type=Object.getOwnPropertyDescriptor(prototype,'type'),sdp=Object.getOwnPropertyDescriptor(prototype,'sdp');
		let brandError='';try{type.get.call({})}catch(error){brandError=error.name}
		return{own:Object.getOwnPropertyNames(value),type:value.type,sdp:value.sdp,json:value.toJSON(),typeDescriptor:{enumerable:type.enumerable,configurable:type.configurable,setter:typeof type.set},sdpDescriptor:{enumerable:sdp.enumerable,configurable:sdp.configurable,setter:typeof sdp.set},brandError}
	})()`)
	if err != nil {
		t.Fatal(err)
	}
	result := v.(map[string]any)
	if len(result["own"].([]any)) != 0 || result["type"] != "offer" || result["sdp"] != "v=0\r\n" || result["brandError"] != "TypeError" {
		t.Fatalf("unexpected RTCSessionDescription slot semantics: %#v", result)
	}
	jsonValue := result["json"].(map[string]any)
	if jsonValue["type"] != "offer" || jsonValue["sdp"] != "v=0\r\n" {
		t.Fatalf("unexpected RTCSessionDescription JSON: %#v", jsonValue)
	}
	for _, key := range []string{"typeDescriptor", "sdpDescriptor"} {
		descriptor := result[key].(map[string]any)
		if descriptor["enumerable"] != true || descriptor["configurable"] != true || descriptor["setter"] != "undefined" {
			t.Fatalf("unexpected %s: %#v", key, descriptor)
		}
	}
}

func TestRTCInternalStateTransitionsBypassPublicAccessors(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	v, err := p.Evaluate(context.Background(), `new Promise(async resolve=>{
		let peerSets=0,channelSets=0;
		for(const key of ['localDescription','pendingLocalDescription','signalingState','iceGatheringState'])Object.defineProperty(RTCPeerConnection.prototype,key,{get:Object.getOwnPropertyDescriptor(RTCPeerConnection.prototype,key).get,set(){peerSets++},configurable:true});
		Object.defineProperty(RTCDataChannel.prototype,'label',{get:Object.getOwnPropertyDescriptor(RTCDataChannel.prototype,'label').get,set(){channelSets++},configurable:true});
		const peer=new RTCPeerConnection(),channel=peer.createDataChannel('probe');
		peer.onicecandidate=e=>{if(!e.candidate)resolve({peerSets,channelSets,label:channel.label,state:peer.iceGatheringState})};
		await peer.setLocalDescription(await peer.createOffer());
	})`)
	if err != nil {
		t.Fatal(err)
	}
	result := v.(map[string]any)
	if result["peerSets"] != int64(0) || result["channelSets"] != int64(0) || result["label"] != "probe" || result["state"] != "complete" {
		t.Fatalf("browser-internal RTC state leaked through public setters: %#v", result)
	}
}

func TestEventDispatchUsesInternalEventSlots(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	v, err := p.Evaluate(context.Background(), `(()=>{const target=new EventTarget(),event=new Event('probe',{cancelable:true});let typeReads=0,defaultReads=0,called=0;Object.defineProperty(Event.prototype,'type',{get(){typeReads++;return'wrong'},configurable:true});Object.defineProperty(Event.prototype,'defaultPrevented',{get(){defaultReads++;return true},configurable:true});target.addEventListener('probe',()=>called++);const result=target.dispatchEvent(event);return{typeReads,defaultReads,called,result}})()`)
	if err != nil {
		t.Fatal(err)
	}
	result := v.(map[string]any)
	if result["typeReads"] != int64(0) || result["defaultReads"] != int64(0) || result["called"] != int64(1) || result["result"] != true {
		t.Fatalf("event dispatch leaked through public Event accessors: %#v", result)
	}
}

func TestRTCIceCandidatesDeriveFromCanonicalNetworkProfile(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	v, err := p.Evaluate(context.Background(), `new Promise(async resolve=>{const peer=new RTCPeerConnection({iceServers:[{urls:'stun:stun.example.test'}]}),seen=[];peer.createDataChannel('probe');peer.onicecandidate=e=>{if(e.candidate)seen.push(e.candidate.candidate);else resolve({seen,state:peer.iceGatheringState,sdp:peer.localDescription.sdp})};const offer=await peer.createOffer();await peer.setLocalDescription(offer)})`)
	if err != nil {
		t.Fatal(err)
	}
	result := v.(map[string]any)
	seen := result["seen"].([]any)
	if len(seen) != 6 || result["state"] != "complete" {
		t.Fatalf("unexpected ICE lifecycle: %#v", result)
	}
	for index, candidate := range seen {
		text := fmt.Sprint(candidate)
		if index < 3 {
			if !strings.Contains(text, " typ host ") || !strings.Contains(text, ".local ") {
				t.Fatalf("candidate %d is not a host candidate: %q", index, text)
			}
		} else if !strings.Contains(text, " 94.43.38.7 ") || !strings.Contains(text, " typ srflx ") {
			t.Fatalf("candidate %d does not project the canonical public address: %q", index, text)
		}
		sdpCandidate := text
		if before, after, found := strings.Cut(text, " ufrag "); found {
			if _, suffix, found := strings.Cut(after, " network-cost"); found {
				sdpCandidate = before + " network-cost" + suffix
			}
		}
		if !strings.Contains(fmt.Sprint(result["sdp"]), sdpCandidate) {
			t.Fatalf("localDescription is missing candidate %q", text)
		}
	}
	ports := make([]int, len(seen))
	for index, candidate := range seen {
		fields := strings.Fields(fmt.Sprint(candidate))
		ports[index], err = strconv.Atoi(fields[5])
		if err != nil {
			t.Fatalf("invalid candidate port: %q", fields[5])
		}
	}
	hostBase, reflexiveBase := ports[0], ports[3]
	if got := []int{ports[0] - hostBase, ports[1] - hostBase, ports[2] - hostBase}; fmt.Sprint(got) != "[0 2 7]" {
		t.Fatalf("host candidate order does not match selected network profile: %v", got)
	}
	if got := []int{ports[3] - reflexiveBase, ports[4] - reflexiveBase, ports[5] - reflexiveBase}; fmt.Sprint(got) != "[0 7 2]" {
		t.Fatalf("reflexive candidate order does not match selected network profile: %v", got)
	}
}

func TestGeneratedLegacyWindowAliasPreservesConstructorIdentity(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	v, err := p.Evaluate(context.Background(), `({same:webkitRTCPeerConnection===RTCPeerConnection,prototype:webkitRTCPeerConnection.prototype===RTCPeerConnection.prototype,name:webkitRTCPeerConnection.name,text:Function.prototype.toString.call(webkitRTCPeerConnection),urlText:Function.prototype.toString.call(URL)})`)
	if err != nil {
		t.Fatal(err)
	}
	m := v.(map[string]any)
	if m["same"] != true || m["prototype"] != true || m["name"] != "RTCPeerConnection" || m["text"] != "function RTCPeerConnection() { [native code] }" || m["urlText"] != "function URL() { [native code] }" {
		t.Fatalf("LegacyWindowAlias must reference the canonical constructor: %#v", m)
	}
}

func TestRTCInternalEntropyDoesNotCallPublicCryptoMethods(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	v, err := p.Evaluate(context.Background(), `new Promise(async resolve=>{crypto.getRandomValues=()=>{throw new Error('public entropy leaked')};crypto.randomUUID=()=>{throw new Error('public UUID leaked')};const peer=new RTCPeerConnection();peer.createDataChannel('probe');peer.onicecandidate=e=>{if(!e.candidate)resolve(peer.localDescription.sdp.includes('a=ice-ufrag:'))};const offer=await peer.createOffer();await peer.setLocalDescription(offer)})`)
	if err != nil || v != true {
		t.Fatalf("RTC must use internal entropy: value=%v err=%v", v, err)
	}
}

func TestWebGPUAdapterPromise(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	navigateCapabilityFixture(t, p)
	v, err := p.Evaluate(context.Background(), `new Promise(resolve=>navigator.gpu.requestAdapter().then(a=>resolve({gpu:Object.prototype.toString.call(navigator.gpu),adapter:Object.prototype.toString.call(a),format:navigator.gpu.getPreferredCanvasFormat(),hasInfo:!!a.info})))`)
	if err != nil {
		t.Fatal(err)
	}
	m, ok := v.(map[string]any)
	if !ok || m["gpu"] != "[object GPU]" || m["adapter"] != "[object GPUAdapter]" || m["format"] != "bgra8unorm" || m["hasInfo"] != true {
		t.Fatalf("unexpected WebGPU adapter observation: %#v", v)
	}
}

func TestWebGPUAdapterInitializationUsesEnvironmentProfile(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	navigateCapabilityFixture(t, p)
	v, err := p.Evaluate(context.Background(), `new Promise(resolve=>{let done=false;const finish=value=>{if(!done){done=true;resolve(value)}};navigator.gpu.requestAdapter().then(()=>finish('adapter'));setTimeout(()=>finish('timeout'),100)})`)
	if err != nil {
		t.Fatal(err)
	}
	if v != "timeout" {
		t.Fatalf("adapter discovery ignored the canonical graphics profile: %#v", v)
	}
}

func TestQuickJSEvaluateSettlesNestedPromiseReactions(t *testing.T) {
	parallelBrowserTest(t)
	b, err := New(quickjsengine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	p, err := b.NewContext().NewPage()
	if err != nil {
		t.Fatal(err)
	}
	navigateCapabilityFixture(t, p)
	v, err := p.Evaluate(context.Background(), `new Promise(resolve=>navigator.gpu.requestAdapter().then(a=>resolve(Object.prototype.toString.call(a))))`)
	if err != nil || v != "[object GPUAdapter]" {
		t.Fatalf("QuickJS Promise reaction did not settle: value=%#v error=%v", v, err)
	}
}

func TestQuickJSMicrotasksPrecedeTimerTasks(t *testing.T) {
	parallelBrowserTest(t)
	b, err := New(quickjsengine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	p, err := b.NewContext().NewPage()
	if err != nil {
		t.Fatal(err)
	}
	v, err := p.Evaluate(context.Background(), `new Promise(resolve=>{const order=[];setTimeout(()=>{order.push('timer');resolve(order)},0);Promise.resolve().then(()=>order.push('microtask'))})`)
	if err != nil {
		t.Fatal(err)
	}
	order, ok := v.([]any)
	if !ok || len(order) != 2 || order[0] != "microtask" || order[1] != "timer" {
		t.Fatalf("unexpected task ordering: %#v", v)
	}
}

func TestDynamicScriptInsertion(t *testing.T) {
	parallelBrowserTest(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/d.js" {
			fmt.Fprint(w, "window.dynamicResult=7")
			return
		}
		fmt.Fprint(w, "<body></body>")
	}))
	defer ts.Close()
	p := testPage(t)
	if err := p.Navigate(context.Background(), ts.URL); err != nil {
		t.Fatal(err)
	}
	v, err := p.Evaluate(context.Background(), `new Promise((resolve,reject)=>{const s=document.createElement('script');s.src='/d.js';s.onload=()=>resolve(dynamicResult);s.onerror=()=>reject(new Error('script load failed'));document.querySelector('body').appendChild(s)})`)
	if err != nil {
		t.Fatal(err)
	}
	if v.(int64) != 7 {
		t.Fatal(v)
	}
}

func TestGetElementsByTagNameCollection(t *testing.T) {
	parallelBrowserTest(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `<html><body><script></script><script></script></body></html>`)
	}))
	defer srv.Close()
	p := testPage(t)
	if err := p.Navigate(context.Background(), srv.URL); err != nil {
		t.Fatal(err)
	}
	v, err := p.Evaluate(context.Background(), `(() => { const c=document.getElementsByTagName('script'); return [c.length, c[0].tagName, c.item(1).tagName, [...c].length] })()`)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := v.([]any)
	if !ok || len(got) != 4 || got[0] != int64(2) || got[1] != "SCRIPT" || got[2] != "SCRIPT" || got[3] != int64(2) {
		t.Fatalf("unexpected collection: %#v", v)
	}
}

func TestCryptoRandomSurface(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	navigateCapabilityFixture(t, p)
	v, err := p.Evaluate(context.Background(), `(() => { const a=new Uint8Array(32); const same=crypto.getRandomValues(a)===a; return {same, nonzero:a.some(x=>x!==0), uuid:/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(crypto.randomUUID())} })()`)
	if err != nil {
		t.Fatal(err)
	}
	m := v.(map[string]any)
	if m["same"] != true || m["nonzero"] != true || m["uuid"] != true {
		t.Fatalf("unexpected crypto surface: %#v", m)
	}
}

func TestAsyncExceptionDoesNotPoisonLaterEvaluation(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	if _, err := p.Evaluate(context.Background(), `setTimeout(()=>{throw new Error('background')},0); 1`); err != nil {
		t.Fatalf("background exception leaked into initiating evaluation: %v", err)
	}
	v, err := p.Evaluate(context.Background(), `6*7`)
	if err != nil || v != int64(42) {
		t.Fatalf("page was not usable after asynchronous exception: value=%#v error=%v", v, err)
	}
}

func TestWindowEventsAndAnchorURLProjection(t *testing.T) {
	parallelBrowserTest(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "<body></body>") }))
	defer srv.Close()
	p := testPage(t)
	if err := p.Navigate(context.Background(), srv.URL); err != nil {
		t.Fatal(err)
	}
	v, err := p.Evaluate(context.Background(), `(() => { let called=false; addEventListener('x',()=>called=true); dispatchEvent(new Event('x')); const a=document.createElement('a');a.href='/p?q=1#h';return {called,protocol:a.protocol,host:a.host,path:a.pathname,documentLocation:document.location===location} })()`)
	if err != nil {
		t.Fatal(err)
	}
	m := v.(map[string]any)
	if m["called"] != true || m["protocol"] != "http:" || m["host"] == "" || m["path"] != "/p" || m["documentLocation"] != true {
		t.Fatalf("unexpected projections: %#v", m)
	}
}

func TestInitScriptExceptionDoesNotAbortNavigation(t *testing.T) {
	parallelBrowserTest(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `<body><script>globalThis.__pageScriptRan = true</script></body>`)
	}))
	defer srv.Close()
	p := testPage(t)
	p.AddInitScript(`throw new Error("init failure")`)
	if err := p.Navigate(context.Background(), srv.URL); err != nil {
		t.Fatalf("init-script exception aborted navigation: %v", err)
	}
	value, err := p.Evaluate(context.Background(), `globalThis.__pageScriptRan === true`)
	if err != nil {
		t.Fatal(err)
	}
	if value != true {
		t.Fatalf("page script did not run after init-script exception: %#v", value)
	}
}

func TestCanonicalInlineStyleAndAnchorReflections(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	v, err := p.Evaluate(context.Background(), `(()=>{const e=document.createElement('div');document.body.appendChild(e);e.style.backgroundColor='red';e.style.setProperty('width','12px','important');const same=e.style;const removed=e.style.removeProperty('background-color');const computed=getComputedStyle(e);const a=document.createElement('a');a.target='_blank';a.rel='noopener noreferrer';return {stable:same===e.style,attr:e.getAttribute('style'),length:e.style.length,first:e.style[0],width:e.style.width,priority:e.style.getPropertyPriority('width'),removed,computedWidth:computed.getPropertyValue('width'),display:computed.display,target:a.getAttribute('target'),rel:a.relList.contains('noopener')}})()`)
	if err != nil {
		t.Fatal(err)
	}
	m := v.(map[string]any)
	if m["stable"] != true || m["attr"] != "width: 12px !important;" || m["length"] != int64(1) || m["first"] != "width" || m["width"] != "12px" || m["priority"] != "important" || m["removed"] != "red" || m["computedWidth"] != "12px" || m["display"] != "block" || m["target"] != "_blank" || m["rel"] != true {
		t.Fatalf("unexpected style/anchor semantics: %#v", m)
	}
}

func TestURLSearchParamsAndNamespacedElements(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	v, err := p.Evaluate(context.Background(), `(()=>{const u=new URL('/p?a=1','https://example.test/base');u.searchParams.append('b','hello world');u.hash='x';const svg=document.createElementNS('http://www.w3.org/2000/svg','svg');return {href:u.href,a:u.searchParams.get('a'),params:String(u.searchParams),valid:URL.canParse('/x',u),invalid:URL.parse('http://[')===null,svg:svg instanceof SVGElement,namespace:svg.namespaceURI,local:svg.localName}})()`)
	if err != nil {
		t.Fatal(err)
	}
	m := v.(map[string]any)
	if m["href"] != "https://example.test/p?a=1&b=hello+world#x" || m["a"] != "1" || m["params"] != "a=1&b=hello+world" || m["valid"] != true || m["invalid"] != true || m["svg"] != true || m["namespace"] != "http://www.w3.org/2000/svg" || m["local"] != "svg" {
		t.Fatalf("unexpected URL/namespace semantics: %#v", m)
	}
}

func TestShadowDOMTrustedTypesAndPermissionsPolicy(t *testing.T) {
	parallelBrowserTest(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Permissions-Policy", "camera=(), xr-spatial-tracking=*")
		_, _ = w.Write([]byte(`<!doctype html><html><body></body></html>`))
	}))
	defer srv.Close()
	p := testPage(t)
	if err := p.Navigate(context.Background(), srv.URL); err != nil {
		t.Fatal(err)
	}
	v, err := p.Evaluate(context.Background(), `(()=>{const host=document.createElement('div');host.title='tip';document.body.appendChild(host);const root=host.attachShadow({mode:'open',delegatesFocus:true});const child=document.createElement('span');child.id='inside';child.textContent='ok';root.appendChild(child);let duplicate='';try{host.attachShadow({mode:'open'})}catch(error){duplicate=error.name}const closedHost=document.createElement('div'),closed=closedHost.attachShadow({mode:'closed'});const input=document.createElement('input');input.name='query';input.type='EMAIL';const invalid=document.createElement('input');invalid.type='not-a-type';const policy=trustedTypes.createPolicy('probe',{createHTML:value=>value.toUpperCase(),createScript:value=>value,createScriptURL:value=>value});const html=policy.createHTML('safe');const hostConnected=host.isConnected,childConnected=child.isConnected,closedConnected=closedHost.isConnected;return{rootTag:Object.prototype.toString.call(root),rootIdentity:root.host===host&&root.mode==='open'&&root.delegatesFocus,query:root.querySelector('#inside')===child,parent:child.parentNode===root,hostConnected,childConnected,closedConnected,connected:hostConnected&&childConnected&&!closedConnected,closed:closedHost.shadowRoot===null&&closed.mode==='closed',duplicate,title:host.getAttribute('title'),input:input.name==='query'&&input.type==='email'&&invalid.type==='text',trusted:trustedTypes.isHTML(html)&&String(html)==='SAFE'&&html instanceof TrustedHTML,featurePolicy:document.featurePolicy instanceof PermissionsPolicy&&!document.featurePolicy.allowsFeature('camera')&&document.featurePolicy.allowsFeature('xr-spatial-tracking')}})()`)
	if err != nil {
		t.Fatal(err)
	}
	m := v.(map[string]any)
	for _, key := range []string{"rootIdentity", "query", "parent", "connected", "closed", "input", "trusted", "featurePolicy"} {
		if m[key] != true {
			t.Fatalf("%s is not true: %#v", key, m)
		}
	}
	if m["rootTag"] != "[object ShadowRoot]" || m["duplicate"] != "NotSupportedError" || m["title"] != "tip" {
		t.Fatalf("unexpected shadow/binding surface: %#v", m)
	}
}

func TestBlobFileAndObjectURL(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	v, err := p.Evaluate(context.Background(), `new Promise(async resolve=>{const b=new Blob(['h\u00e9',new Uint8Array([33])],{type:'TEXT/PLAIN'}),url=URL.createObjectURL(b),slice=b.slice(1);const f=new File([b],'a/b.txt',{lastModified:42});const result={size:b.size,type:b.type,text:await b.text(),slice:await slice.text(),file:f.name,last:f.lastModified,blobURL:url.startsWith('blob:'+location.origin+'/')};URL.revokeObjectURL(url);resolve(result)})`)
	if err != nil {
		t.Fatal(err)
	}
	m := v.(map[string]any)
	if m["size"] != int64(4) || m["type"] != "text/plain" || m["text"] != "h\u00e9!" || m["slice"] != "\u00e9!" || m["file"] != "a:b.txt" || m["last"] != int64(42) || m["blobURL"] != true {
		t.Fatalf("unexpected Blob/File semantics: %#v", m)
	}
}

func TestFileReaderReadsBlobAndDispatchesLifecycleEvents(t *testing.T) {
	serialBrowserTest(t)
	p := testPage(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	v, err := p.Evaluate(ctx, `new Promise((resolve,reject)=>{const reader=new FileReader(),events=[];for(const name of ['loadstart','progress','load','loadend'])reader.addEventListener(name,()=>events.push(name));reader.onerror=()=>reject(reader.error);reader.onloadend=()=>resolve({tag:Object.prototype.toString.call(reader),state:reader.readyState,result:reader.result,events});reader.readAsDataURL(new Blob(['hello'],{type:'text/plain'}))})`)
	if err != nil {
		t.Fatal(err)
	}
	m := v.(map[string]any)
	if m["tag"] != "[object FileReader]" || m["state"] != int64(2) || m["result"] != "data:text/plain;base64,aGVsbG8=" || fmt.Sprint(m["events"]) != "[loadstart progress load loadend]" {
		t.Fatalf("unexpected FileReader semantics: %#v", m)
	}
}

func TestFormDataSerializesMultipartRequestBody(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	v, err := p.Evaluate(context.Background(), `(async()=>{const data=new FormData();data.append('field','value');data.append('upload',new Blob(['payload'],{type:'application/octet-stream'}),'probe.bin');const request=new Request('https://example.test/upload',{method:'POST',body:data}),body=await request.text();return{tag:Object.prototype.toString.call(data),field:data.get('field'),file:data.get('upload').name,type:request.headers.get('content-type'),body}})()`)
	if err != nil {
		t.Fatal(err)
	}
	m := v.(map[string]any)
	if m["tag"] != "[object FormData]" || m["field"] != "value" || m["file"] != "probe.bin" || !strings.HasPrefix(fmt.Sprint(m["type"]), "multipart/form-data; boundary=----WebKitFormBoundaryMimic") {
		t.Fatalf("unexpected FormData surface: %#v", m)
	}
	body := fmt.Sprint(m["body"])
	for _, fragment := range []string{`name="field"`, "value", `name="upload"; filename="probe.bin"`, "Content-Type: application/octet-stream", "payload"} {
		if !strings.Contains(body, fragment) {
			t.Fatalf("multipart body missing %q: %q", fragment, body)
		}
	}
}

func TestDedicatedWorkerHasIndependentRealmAndScheduledMessages(t *testing.T) {
	serialBrowserTest(t)
	p := testPage(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	v, err := p.Evaluate(ctx, `new Promise(resolve=>{globalThis.realmMarker='page';const source="onmessage=e=>postMessage({value:e.data*2,independent:typeof realmMarker==='undefined',same:self===globalThis})";const url=URL.createObjectURL(new Blob([source],{type:'text/javascript'}));const worker=new Worker(url);worker.onmessage=e=>{worker.terminate();URL.revokeObjectURL(url);resolve(e.data)};worker.postMessage(21)})`)
	if err != nil {
		t.Fatal(err)
	}
	m := v.(map[string]any)
	if m["value"] != int64(42) || m["independent"] != true || m["same"] != true {
		t.Fatalf("unexpected dedicated worker result: %#v", m)
	}
	loadedThroughResourcePipeline := false
	for _, event := range p.Trace().Events() {
		if event.Kind == "resource" && event.Name == "loadEnd" && fmt.Sprint(event.Data["type"]) == "worker" {
			loadedThroughResourcePipeline = true
		}
	}
	if !loadedThroughResourcePipeline {
		t.Fatal("blob worker bypassed the shared resource pipeline")
	}
}

func TestDedicatedWorkerTrustedTypesAreRealmLocal(t *testing.T) {
	serialBrowserTest(t)
	p := testPage(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	v, err := p.Evaluate(ctx, `new Promise((resolve,reject)=>{const source="onmessage=()=>{const p=trustedTypes.createPolicy('worker',{createHTML:v=>v});const value=p.createHTML('ok');postMessage({tag:Object.prototype.toString.call(value),valid:trustedTypes.isHTML(value),text:String(value),windowOnly:typeof document})}";const url=URL.createObjectURL(new Blob([source],{type:'text/javascript'}));const worker=new Worker(url);worker.onmessage=e=>{worker.terminate();URL.revokeObjectURL(url);resolve(e.data)};worker.onerror=e=>reject(new Error(e.message));worker.postMessage(null)})`)
	if err != nil {
		t.Fatal(err)
	}
	m := v.(map[string]any)
	if m["tag"] != "[object TrustedHTML]" || m["valid"] != true || m["text"] != "ok" || m["windowOnly"] != "undefined" {
		t.Fatalf("unexpected worker Trusted Types surface: %#v", m)
	}
}

func TestImmediateWorkerTerminateCancelsStartupBeforeResourceLoad(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	value, err := p.Evaluate(context.Background(), `(()=>{const url=URL.createObjectURL(new Blob(['postMessage("unexpected")'],{type:'text/javascript'})),worker=new Worker(url);worker.terminate();URL.revokeObjectURL(url);return true})()`)
	if err != nil || value != true {
		t.Fatalf("immediate terminate evaluation failed: value=%#v err=%v", value, err)
	}
	for _, event := range p.Trace().Events() {
		if event.Name == "workerBootstrapStart" || event.Name == "workerStart" || (event.Kind == "resource" && fmt.Sprint(event.Data["type"]) == "worker") {
			t.Fatalf("terminated worker startup became observable: %s %s %#v", event.Kind, event.Name, event.Data)
		}
	}
}

func TestDedicatedWorkerMicrotasksRunBeforeTimers(t *testing.T) {
	serialBrowserTest(t)
	p := testPage(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	v, err := p.Evaluate(ctx, `new Promise((resolve,reject)=>{const source="onmessage=()=>{const order=[];Promise.resolve().then(()=>order.push('microtask'));setTimeout(()=>{order.push('timer');postMessage(order)},0);order.push('task-end')}";const url=URL.createObjectURL(new Blob([source],{type:'text/javascript'}));const worker=new Worker(url);worker.onmessage=e=>{worker.terminate();URL.revokeObjectURL(url);resolve(e.data)};worker.onerror=e=>reject(new Error(e.message));worker.postMessage(null)})`)
	if err != nil {
		for _, event := range p.Trace().Events() {
			t.Logf("trace %s %s %#v", event.Kind, event.Name, event.Data)
		}
		t.Fatal(err)
	}
	values := v.([]any)
	if len(values) != 3 || values[0] != "task-end" || values[1] != "microtask" || values[2] != "timer" {
		t.Fatalf("unexpected worker scheduling order: %#v", values)
	}
}

func TestV8DedicatedWorkerRealmAndScheduling(t *testing.T) {
	serialBrowserTest(t)
	b, err := New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	p, err := b.NewContext().NewPage()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	v, err := p.Evaluate(ctx, `new Promise((resolve,reject)=>{const source="onmessage=()=>{const order=[];Promise.resolve().then(()=>order.push('microtask'));setTimeout(()=>{order.push('timer');postMessage(order)},0);order.push('task-end')}";const url=URL.createObjectURL(new Blob([source],{type:'text/javascript'}));const worker=new Worker(url);worker.onmessage=e=>{worker.terminate();URL.revokeObjectURL(url);resolve(e.data)};worker.onerror=e=>reject(new Error(e.message));worker.postMessage(null)})`)
	if err != nil {
		for _, event := range p.Trace().Events() {
			t.Logf("trace %s %s %#v", event.Kind, event.Name, event.Data)
		}
		t.Fatal(err)
	}
	values := v.([]any)
	if len(values) != 3 || values[0] != "task-end" || values[1] != "microtask" || values[2] != "timer" {
		t.Fatalf("unexpected V8 worker scheduling order: %#v", values)
	}
}

func TestV8SameOriginIframeEvalAndRealmGlobals(t *testing.T) {
	serialBrowserTest(t)
	b, err := New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	p, err := b.NewContext().NewPage()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	v, err := p.Evaluate(ctx, `(()=>{const iframe=document.createElement('iframe');document.body.appendChild(iframe);const child=iframe.contentWindow,evalText=Function.prototype.toString.call(child.eval),evaluated=child.eval("({own:self===globalThis,Document,Element})"),ChildDocument=evaluated.Document,ChildElement=evaluated.Element;iframe.remove();return{own:evaluated.own,evalText,documentType:typeof ChildDocument,elementType:typeof ChildElement,directDocument:ChildDocument===child.Document,directElement:ChildElement===child.Element,distinctDocument:ChildDocument!==Document,distinctElement:ChildElement!==Element}})()`)
	if err != nil {
		t.Fatal(err)
	}
	result := v.(map[string]any)
	if result["own"] != true || result["evalText"] != "function eval() { [native code] }" || result["documentType"] != "function" || result["elementType"] != "function" || result["directDocument"] != true || result["directElement"] != true || result["distinctDocument"] != true || result["distinctElement"] != true {
		t.Fatalf("V8 iframe realm forwarding mismatch: %#v", result)
	}
}

func TestV8EvaluationTimeoutAfterIframeHostCallbacks(t *testing.T) {
	serialBrowserTest(t)
	b, err := New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	p, err := b.NewContext().NewPage()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err = p.Evaluate(ctx, `(()=>{const iframe=document.createElement('iframe');document.body.appendChild(iframe);const child=iframe.contentWindow;void child.eval;iframe.remove();for(;;){}})()`)
	if err == nil {
		t.Fatal("V8 evaluation was not interrupted")
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("V8 evaluation timeout after iframe callbacks took %v", elapsed)
	}
}

func TestIFrameOwnsChildFrameRealmAndWindowProxy(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	v, err := p.Evaluate(context.Background(), `new Promise(resolve=>{globalThis.parentMarker=1;const iframe=document.createElement('iframe');document.body.appendChild(iframe);const child=iframe.contentWindow;addEventListener('message',e=>resolve({data:e.data,source:e.source===child,stable:child===iframe.contentWindow,url:iframe.contentDocument.URL}));child.eval("onmessage=e=>parent.postMessage({independent:typeof parentMarker==='undefined',ownGlobal:self===globalThis,parentIsProxy:parent!==self})");child.postMessage('go')})`)
	if err != nil {
		t.Fatal(err)
	}
	m := v.(map[string]any)
	data := m["data"].(map[string]any)
	if data["independent"] != true || data["ownGlobal"] != true || data["parentIsProxy"] != true || m["source"] != true || m["stable"] != true || m["url"] != "about:blank" {
		t.Fatalf("unexpected iframe realm/window proxy semantics: %#v", m)
	}
}

func TestChildDocumentForwardsPrimitiveProperties(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	v, err := p.Evaluate(context.Background(), `(()=>{const f=document.createElement('iframe');document.body.appendChild(f);f.contentDocument.title='child';return {title:f.contentDocument.title,ready:f.contentDocument.readyState}})()`)
	if err != nil {
		t.Fatal(err)
	}
	m := v.(map[string]any)
	if m["title"] != "child" || m["ready"] != "complete" {
		t.Fatalf("incorrect child document values: %#v", m)
	}
}

func TestChildMessageTaskCheckpointsReceivingRealm(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	v, err := p.Evaluate(context.Background(), `new Promise(resolve=>{const f=document.createElement('iframe');document.body.appendChild(f);f.contentWindow.eval("var seen=[];onmessage=e=>{seen.push(e.data);Promise.resolve().then(()=>{seen.push('microtask');if(e.data===2)parent.postMessage(seen,'*')})}");addEventListener('message',e=>resolve(JSON.stringify(e.data)));f.contentWindow.postMessage(1,'*');f.contentWindow.postMessage(2,'*')})`)
	if err != nil {
		t.Fatal(err)
	}
	if v != `[1,"microtask",2,"microtask"]` {
		t.Fatalf("message task checkpoint order: %#v", v)
	}
}

func TestFramePostMessageTransfersRealmLocalMessagePort(t *testing.T) {
	parallelBrowserTest(t)
	b, err := New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	p, err := b.NewContext().NewPage()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	value, err := p.Evaluate(context.Background(), `new Promise(resolve=>{const frame=document.createElement('iframe');document.body.appendChild(frame);addEventListener('message',event=>{const port=event.ports[0];port.onmessage=reply=>resolve({length:event.ports.length,instance:port instanceof MessagePort,source:event.source===frame.contentWindow,origin:event.origin,reply:reply.data});port.postMessage('ping')});frame.contentWindow.eval("const channel=new MessageChannel();channel.port1.onmessage=event=>channel.port1.postMessage('pong:'+event.data);parent.postMessage('with-port','*',[channel.port2])")})`)
	if err != nil {
		t.Fatal(err)
	}
	result := value.(map[string]any)
	if numberValue(result["length"]) != 1 || result["instance"] != true || result["source"] != true || result["origin"] != "null" || result["reply"] != "pong:ping" {
		t.Fatalf("cross-realm MessageEvent ports/source/origin mismatch: %#v", result)
	}
}

func TestV8NavigatedFrameTransferredMessagePortRemainsEntangled(t *testing.T) {
	parallelBrowserTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/child" {
			fmt.Fprint(w, `<script>const channel=new MessageChannel();channel.port1.onmessage=event=>channel.port1.postMessage('pong:'+event.data);parent.postMessage('port','*',[channel.port2])</script>`)
			return
		}
		fmt.Fprint(w, `<script>window.portReply=null;addEventListener('message',event=>{const port=event.ports[0];port.onmessage=reply=>portReply=reply.data;port.postMessage('ping')})</script><iframe src="/child"></iframe>`)
	}))
	defer server.Close()
	b, err := New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	p, err := b.NewContext().NewPage()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if err := p.Navigate(context.Background(), server.URL); err != nil {
		t.Fatal(err)
	}
	value, err := p.Evaluate(context.Background(), `portReply`)
	if err != nil {
		t.Fatal(err)
	}
	if value != "pong:ping" {
		t.Fatalf("navigated child transferred port reply = %#v", value)
	}
}

func TestConnectedShadowTreeIframeOwnsBrowsingContext(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	v, err := p.Evaluate(context.Background(), `(()=>{const host=document.createElement('div');document.body.appendChild(host);const root=host.attachShadow({mode:'closed'});const iframe=document.createElement('iframe');root.appendChild(iframe);return{connected:iframe.isConnected,window:iframe.contentWindow!==null,document:iframe.contentDocument!==null,stable:iframe.contentWindow===iframe.contentWindow}})()`)
	if err != nil {
		t.Fatal(err)
	}
	result := v.(map[string]any)
	if result["connected"] != true || result["window"] != true || result["document"] != true || result["stable"] != true {
		t.Fatalf("connected shadow-tree iframe has no browsing context: %#v", result)
	}
}

func TestConnectedShadowTreeIframeLoadsItsDocument(t *testing.T) {
	parallelBrowserTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/child" {
			_, _ = w.Write([]byte(`<script>document.addEventListener('DOMContentLoaded',()=>parent.postMessage({url:location.href,frame:frameElement.tagName,ready:document.readyState}))</script>`))
			return
		}
		_, _ = w.Write([]byte(`<!doctype html><body></body>`))
	}))
	defer server.Close()
	p := testPage(t)
	if err := p.Navigate(context.Background(), server.URL); err != nil {
		t.Fatal(err)
	}
	v, err := p.Evaluate(context.Background(), `new Promise(resolve=>{addEventListener('message',e=>resolve(e.data));const host=document.createElement('div');document.body.appendChild(host);const iframe=document.createElement('iframe');iframe.src='/child';host.attachShadow({mode:'closed'}).appendChild(iframe);void iframe.contentWindow})`)
	if err != nil {
		t.Fatal(err)
	}
	result := v.(map[string]any)
	if result["url"] != server.URL+"/child" || result["frame"] != "IFRAME" || result["ready"] != "interactive" {
		t.Fatalf("shadow-tree iframe document did not execute: %#v", result)
	}
}

func TestDynamicIframeFiresOwnerLoadAfterChildCompletes(t *testing.T) {
	parallelBrowserTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/child" {
			_, _ = w.Write([]byte(`<body><script>globalThis.childRan='yes'</script></body>`))
			return
		}
		_, _ = w.Write([]byte(`<!doctype html><body></body>`))
	}))
	defer server.Close()
	p := testPage(t)
	if err := p.Navigate(context.Background(), server.URL); err != nil {
		t.Fatal(err)
	}
	v, err := p.Evaluate(context.Background(), `new Promise(resolve=>{const iframe=document.createElement('iframe');iframe.src='/child';iframe.addEventListener('load',()=>resolve('loaded'));document.body.appendChild(iframe)})`)
	if err != nil {
		t.Fatal(err)
	}
	if v != "loaded" {
		t.Fatalf("iframe owner load did not fire: %#v", v)
	}
	var child *Frame
	for _, candidate := range p.Top.children {
		child = candidate
		break
	}
	if child == nil || child.Realm == nil || child.Realm.readyState != "complete" {
		t.Fatalf("iframe owner load fired before child completion: %#v", child)
	}
}

func TestShadowTreeIframeFiresOwnerLoadAfterChildCompletes(t *testing.T) {
	parallelBrowserTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_, _ = w.Write([]byte(`<!doctype html><body></body>`))
	}))
	defer server.Close()
	p := testPage(t)
	if err := p.Navigate(context.Background(), server.URL); err != nil {
		t.Fatal(err)
	}
	v, err := p.Evaluate(context.Background(), `new Promise(resolve=>{const host=document.createElement('div');document.body.appendChild(host);const iframe=document.createElement('iframe');iframe.src='/child';iframe.addEventListener('load',()=>resolve('loaded'));host.attachShadow({mode:'closed'}).appendChild(iframe)})`)
	if err != nil {
		t.Fatal(err)
	}
	if v != "loaded" {
		t.Fatalf("shadow-tree iframe owner load did not fire: %#v", v)
	}
}

func TestConnectingShadowHostStartsExistingIframeNavigation(t *testing.T) {
	parallelBrowserTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_, _ = w.Write([]byte(`<!doctype html><body></body>`))
	}))
	defer server.Close()
	p := testPage(t)
	if err := p.Navigate(context.Background(), server.URL); err != nil {
		t.Fatal(err)
	}
	v, err := p.Evaluate(context.Background(), `new Promise(resolve=>{const host=document.createElement('div'),root=host.attachShadow({mode:'closed'}),iframe=document.createElement('iframe');iframe.src='/child';iframe.addEventListener('load',()=>resolve({loaded:true,url:iframe.src}));root.appendChild(iframe);document.body.appendChild(host)})`)
	if err != nil {
		t.Fatal(err)
	}
	result := v.(map[string]any)
	if result["loaded"] != true || result["url"] != server.URL+"/child" {
		t.Fatalf("connecting shadow host did not start child navigation: %#v", result)
	}
}

func TestIframeRenavigationPreservesProxyAndRemovalClearsElementAccess(t *testing.T) {
	parallelBrowserTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		fmt.Fprintf(w, "<!doctype html><title>%s</title><body></body>", req.URL.Path)
	}))
	defer server.Close()
	p := testPage(t)
	if err := p.Navigate(context.Background(), server.URL); err != nil {
		t.Fatal(err)
	}
	v, err := p.Evaluate(context.Background(), `new Promise(resolve=>{const f=document.createElement('iframe');let first,firstArray,loads=0;f.onload=()=>{if(++loads===1){first=f.contentWindow;firstArray=first.Array;first.eval('window.oldMarker=1');f.src='/second';return}const result={stable:first===f.contentWindow,title:f.contentDocument.title,fresh:first.eval("typeof oldMarker==='undefined'"),freshArray:firstArray!==f.contentWindow.Array};f.remove();result.windowNull=f.contentWindow===null;result.documentNull=f.contentDocument===null;resolve(result)};f.src='/first';document.body.appendChild(f)})`)
	if err != nil {
		t.Fatal(err)
	}
	m := v.(map[string]any)
	if m["stable"] != true || m["fresh"] != true || m["freshArray"] != true || m["title"] != "/second" || m["windowNull"] != true || m["documentNull"] != true {
		t.Fatalf("iframe navigation/removal: %#v", m)
	}
}

func TestV8IframeRemovalUnblocksParentLoadBeforeDisconnect(t *testing.T) {
	parallelBrowserTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/child" {
			fmt.Fprint(w, `<!doctype html><title>child</title>`)
			return
		}
		fmt.Fprint(w, `<!doctype html><script>
			window.order=[];
			let frame;
			addEventListener('load',()=>order.push('parent:'+frame.isConnected+':'+!!frame.contentWindow));
			document.addEventListener('DOMContentLoaded',()=>{
				frame=document.createElement('iframe');
				frame.src='/child';
				let loads=0;
				frame.onload=()=>{
					order.push('frame'+(++loads));
					if(loads===1){frame.src='/child?second';return}
					frame.remove();order.push('removed');
				};
				document.body.appendChild(frame);
			});
		</script><body></body>`)
	}))
	defer server.Close()
	b, err := New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	p, err := b.NewContext().NewPage()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if err := p.Navigate(context.Background(), server.URL); err != nil {
		t.Fatal(err)
	}
	value, err := p.Evaluate(context.Background(), `order.join(',')`)
	if err != nil {
		t.Fatal(err)
	}
	if value != "frame1,frame2,parent:true:true,removed" {
		t.Fatalf("iframe removal/load boundary = %#v", value)
	}
}

func TestDynamicScriptMicrotasksPrecedeLoad(t *testing.T) {
	parallelBrowserTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/dynamic.js" {
			_, _ = w.Write([]byte(`seen.push('script');Promise.resolve().then(()=>seen.push('microtask'))`))
			return
		}
		_, _ = w.Write([]byte(`<!doctype html><body></body>`))
	}))
	defer server.Close()
	p := testPage(t)
	if err := p.Navigate(context.Background(), server.URL); err != nil {
		t.Fatal(err)
	}
	v, err := p.Evaluate(context.Background(), `new Promise(resolve=>{window.seen=[];const s=document.createElement('script');s.src='/dynamic.js';s.onload=()=>{seen.push('load');resolve(JSON.stringify(seen))};document.head.appendChild(s)})`)
	if err != nil {
		t.Fatal(err)
	}
	if v != `["script","microtask","load"]` {
		t.Fatalf("dynamic script checkpoint: %#v", v)
	}
}

func TestChildResourceTimingBelongsToInitiatingContext(t *testing.T) {
	parallelBrowserTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/child":
			fmt.Fprint(w, `<!doctype html><script src="/child.js"></script>`)
		case "/child.js":
			fmt.Fprint(w, `window.loaded=true`)
		default:
			fmt.Fprint(w, `<!doctype html><body></body>`)
		}
	}))
	defer server.Close()
	p := testPage(t)
	if err := p.Navigate(context.Background(), server.URL); err != nil {
		t.Fatal(err)
	}
	v, err := p.Evaluate(context.Background(), `new Promise(resolve=>{const f=document.createElement('iframe');f.src='/child';f.onload=()=>resolve({parent:performance.getEntriesByType('resource').filter(e=>e.name.endsWith('/child.js')).length,child:JSON.parse(f.contentWindow.eval("JSON.stringify(performance.getEntriesByType('resource').map(e=>e.initiatorType))"))});document.body.appendChild(f)})`)
	if err != nil {
		t.Fatal(err)
	}
	m := v.(map[string]any)
	if numberValue(m["parent"]) != 0 || fmt.Sprint(m["child"]) != "[script]" {
		t.Fatalf("resource timing context ownership: %#v", m)
	}
}

func TestFramePostMessageQueuedBeforeNavigationTargetsCommittedRealm(t *testing.T) {
	parallelBrowserTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/child" {
			_, _ = w.Write([]byte(`<script>onmessage=e=>parent.postMessage({data:e.data,url:location.href})</script>`))
			return
		}
		_, _ = w.Write([]byte(`<!doctype html><body></body>`))
	}))
	defer server.Close()
	p := testPage(t)
	if err := p.Navigate(context.Background(), server.URL); err != nil {
		t.Fatal(err)
	}
	v, err := p.Evaluate(context.Background(), `new Promise(resolve=>{addEventListener('message',event=>resolve(event.data));const iframe=document.createElement('iframe');iframe.src='/child';document.body.appendChild(iframe);iframe.contentWindow.postMessage('queued-before-commit','*')})`)
	if err != nil {
		t.Fatal(err)
	}
	result := v.(map[string]any)
	if result["data"] != "queued-before-commit" || result["url"] != server.URL+"/child" {
		t.Fatalf("queued WindowProxy message missed committed realm: %#v", result)
	}
}

func TestElementAttributesProjectsCanonicalDOM(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	v, err := p.Evaluate(context.Background(), `(()=>{const element=document.createElement('div');element.setAttribute('data-one','1');element.setAttribute('title','two');const attributes=element.attributes;return{tag:Object.prototype.toString.call(attributes),length:attributes.length,first:attributes[0].name+':'+attributes[0].value,named:attributes.getNamedItem('title').value,missing:attributes.item(9)}})()`)
	if err != nil {
		t.Fatal(err)
	}
	result := v.(map[string]any)
	first := result["first"]
	if result["tag"] != "[object NamedNodeMap]" || result["length"] != int64(2) || (first != "data-one:1" && first != "title:two") || result["named"] != "two" || result["missing"] != nil {
		t.Fatalf("Element.attributes mismatch: %#v", result)
	}
}

func TestDocumentCreateNodeIteratorTraversesElements(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	v, err := p.Evaluate(context.Background(), `(()=>{const root=document.createElement('div'),a=document.createElement('span'),b=document.createElement('p');root.appendChild(a);root.appendChild(b);const iterator=document.createNodeIterator(root,NodeFilter.SHOW_ELEMENT,{acceptNode:n=>n.tagName==='SPAN'?NodeFilter.FILTER_REJECT:NodeFilter.FILTER_ACCEPT}),names=[];for(let node;(node=iterator.nextNode());)names.push(node.tagName);return{tag:Object.prototype.toString.call(iterator),names}})()`)
	if err != nil {
		t.Fatal(err)
	}
	result := v.(map[string]any)
	names := result["names"].([]any)
	if result["tag"] != "[object NodeIterator]" || len(names) != 2 || names[0] != "DIV" || names[1] != "P" {
		t.Fatalf("NodeIterator mismatch: %#v", result)
	}
}

func TestDocumentCollectionsAreLiveDOMProjections(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	v, err := p.Evaluate(context.Background(), `(()=>{const script=document.createElement('script'),style=document.createElement('style');document.head.appendChild(script);document.head.appendChild(style);return{scripts:document.scripts.length,script:document.scripts[0]===script,sheets:document.styleSheets.length,owner:document.styleSheets[0].ownerNode===style,referrer:document.referrer}})()`)
	if err != nil {
		t.Fatal(err)
	}
	result := v.(map[string]any)
	if result["scripts"] != int64(1) || result["script"] != true || result["sheets"] != int64(1) || result["owner"] != true || result["referrer"] != "" {
		t.Fatalf("document collection projection mismatch: %#v", result)
	}
}

func TestParserStylesheetUsesSharedLoaderAndCSSOMProjection(t *testing.T) {
	parallelBrowserTest(t)
	requested := make(chan http.Header, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/site.css" {
			requested <- r.Header.Clone()
			w.Header().Set("Content-Type", "text/css")
			fmt.Fprint(w, "body{color:red}")
			return
		}
		fmt.Fprint(w, `<link rel="alternate stylesheet" href="/site.css" media="screen"><title>styled</title>`)
	}))
	defer ts.Close()
	p := testPage(t)
	if err := p.Navigate(context.Background(), ts.URL); err != nil {
		t.Fatal(err)
	}
	select {
	case headers := <-requested:
		if headers.Get("Sec-Fetch-Dest") != "style" || headers.Get("Accept") != "text/css,*/*;q=0.1" {
			t.Fatalf("unexpected stylesheet request headers: %#v", headers)
		}
	default:
		t.Fatal("parser stylesheet was not requested")
	}
	v, err := p.Evaluate(context.Background(), `(()=>{const sheet=document.styleSheets[0];return{length:document.styleSheets.length,owner:sheet.ownerNode===document.querySelector('link'),href:sheet.href,media:sheet.media.mediaText}})()`)
	if err != nil {
		t.Fatal(err)
	}
	result := v.(map[string]any)
	if numberValue(result["length"]) != 1 || result["owner"] != true || result["href"] != ts.URL+"/site.css" || result["media"] != "screen" {
		t.Fatalf("unexpected stylesheet projection: %#v", result)
	}
	entries, err := p.Evaluate(context.Background(), `performance.getEntriesByType('resource').map(e=>({name:e.name,initiatorType:e.initiatorType}))`)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, raw := range entries.([]any) {
		entry := raw.(map[string]any)
		if entry["name"] == ts.URL+"/site.css" && entry["initiatorType"] == "link" {
			found = true
		}
	}
	if !found {
		t.Fatalf("stylesheet ResourceTiming entry missing: %#v", entries)
	}
}

func TestAnimationFrameUsesBrowserScheduler(t *testing.T) {
	serialBrowserTest(t)
	p := testPage(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Chrome's frame timestamp precedes callback execution; performance.now()
	// remains live inside the callback. The frozen Chrome 152 observations are
	// recorded in testdata/animation_frame_clock_chrome152.json.
	v, err := p.Evaluate(ctx, `new Promise(resolve=>{const order=[];requestAnimationFrame(timestamp=>{order.push('frame');const entry=performance.now();while(performance.now()-entry<2){}const end=performance.now();resolve({order,timestamp,entry,end})});Promise.resolve().then(()=>order.push('microtask'))})`)
	if err != nil {
		t.Fatal(err)
	}
	result := v.(map[string]any)
	order := result["order"].([]any)
	timestamp, entry, end := numberValue(result["timestamp"]), numberValue(result["entry"]), numberValue(result["end"])
	if len(order) != 2 || order[0] != "microtask" || order[1] != "frame" || !(timestamp >= 0 && entry >= timestamp && end-entry >= 2) {
		t.Fatalf("animation frame scheduling mismatch: %#v", result)
	}
}

func TestMessageChannelUsesPostedMessageTaskSource(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	v, err := p.Evaluate(context.Background(), `new Promise(resolve=>{const order=['sync'],channel=new MessageChannel();channel.port1.onmessage=event=>{order.push('message:'+event.data.value);resolve({order,ports:channel.port1 instanceof MessagePort&&channel.port2 instanceof MessagePort})};channel.port2.postMessage({value:7});queueMicrotask(()=>order.push('microtask'))})`)
	if err != nil {
		t.Fatal(err)
	}
	result := v.(map[string]any)
	order := result["order"].([]any)
	if fmt.Sprint(order) != "[sync microtask message:7]" || result["ports"] != true {
		t.Fatalf("unexpected MessageChannel result: %#v", result)
	}
}

func TestDocumentVisibilityDatasetAndNavigationState(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	v, err := p.Evaluate(context.Background(), `(()=>{const node=document.createElement('div');node.dataset.longName='value';document.body.appendChild(node);const navigation=performance.getEntriesByType('navigation')[0];return{visibility:document.visibilityState,hidden:document.hidden,prerendering:document.prerendering,discarded:document.wasDiscarded,dataset:node.getAttribute('data-long-name'),keys:Object.keys(node.dataset),activation:navigation.activationStart,navigationId:navigation.navigationId}})()`)
	if err != nil {
		t.Fatal(err)
	}
	result := v.(map[string]any)
	if result["visibility"] != "visible" || result["hidden"] != false || result["prerendering"] != false || result["discarded"] != false || result["dataset"] != "value" || fmt.Sprint(result["keys"]) != "[longName]" || numberValue(result["activation"]) != 0 || numberValue(result["navigationId"]) < 1000 {
		t.Fatalf("unexpected canonical document/navigation state: %#v", result)
	}
}

func TestDocumentElementParentUsesDocumentSingleton(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	v, err := p.Evaluate(context.Background(), `(()=>{const parent=document.documentElement.parentNode;return{same:parent===document,document:parent instanceof Document,element:parent instanceof Element,parentElement:document.documentElement.parentElement}})()`)
	if err != nil {
		t.Fatal(err)
	}
	result := v.(map[string]any)
	if result["same"] != true || result["document"] != true || result["element"] != false || result["parentElement"] != nil {
		t.Fatalf("unexpected document-element parent projection: %#v", result)
	}
}

func TestExplicitElementDimensionsProjectAsDOMRect(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	v, err := p.Evaluate(context.Background(), `(()=>{document.body.style.margin='0';const a=document.createElement('span'),b=document.createElement('iframe');b.style.width='300px';b.style.height='65px';b.style.border='0';document.body.appendChild(a);document.body.appendChild(b);const rect=b.getBoundingClientRect(),descriptor=Object.getOwnPropertyDescriptor(DOMRect.prototype,'width');return{width:b.offsetWidth,height:b.offsetHeight,rectWidth:rect.width,right:rect.right,brand:rect instanceof DOMRect&&rect instanceof DOMRectReadOnly,writable:typeof descriptor.get==='function'&&typeof descriptor.set==='function',contains:document.body.contains(b),previous:b.previousElementSibling===a}})()`)
	if err != nil {
		t.Fatal(err)
	}
	result := v.(map[string]any)
	if numberValue(result["width"]) != 300 || numberValue(result["height"]) != 65 || numberValue(result["rectWidth"]) != 300 || numberValue(result["right"]) != 300 || result["brand"] != true || result["writable"] != true || result["contains"] != true || result["previous"] != true {
		t.Fatalf("unexpected explicit layout projection: %#v", result)
	}
}

func TestAutoBlockLayoutDerivesFromChildrenAndViewport(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	v, err := p.Evaluate(context.Background(), `(()=>{const box=document.createElement('div'),child=document.createElement('iframe');document.body.style.margin='0';child.style.cssText='display:block;border:0;width:300px;height:65px';box.appendChild(child);document.body.appendChild(box);const rect=box.getBoundingClientRect();return{width:rect.width,height:rect.height,bodyWidth:document.body.getBoundingClientRect().width,bodyHeight:document.body.getBoundingClientRect().height}})()`)
	if err != nil {
		t.Fatal(err)
	}
	result := v.(map[string]any)
	if numberValue(result["width"]) != float64(p.Environment().Window.ViewportWidth) || numberValue(result["height"]) != 65 || numberValue(result["bodyWidth"]) != float64(p.Environment().Window.ViewportWidth) || numberValue(result["bodyHeight"]) != 65 {
		t.Fatalf("unexpected auto block layout: %#v", result)
	}
}

func TestOutOfFlowChildrenDoNotContributeToParentHeight(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	v, err := p.Evaluate(context.Background(), `(()=>{const measure=position=>{const container=document.createElement('div'),child=document.createElement('iframe');container.style.width='100px';child.style.cssText='border:none;width:1px;height:1px;position:'+position+';left:0;top:0';container.appendChild(child);document.body.appendChild(container);const style=getComputedStyle(child),result={height:container.getBoundingClientRect().height,offsetHeight:container.offsetHeight,childHeight:child.getBoundingClientRect().height,childDisplay:style.display,childPosition:style.position};container.remove();return result};return{fixed:measure('fixed'),absolute:measure('absolute'),static:measure('static')}})()`)
	if err != nil {
		t.Fatal(err)
	}
	result := v.(map[string]any)
	for _, position := range []string{"fixed", "absolute"} {
		box := result[position].(map[string]any)
		if numberValue(box["height"]) != 0 || numberValue(box["offsetHeight"]) != 0 || numberValue(box["childHeight"]) != 1 || box["childDisplay"] != "block" || box["childPosition"] != position {
			t.Fatalf("unexpected %s child layout: %#v", position, box)
		}
	}
	// Frozen receipt: css_geometry_integration_chrome152.json (inlineFrame).
	static := result["static"].(map[string]any)
	if numberValue(static["height"]) != 18 || numberValue(static["offsetHeight"]) != 18 || numberValue(static["childHeight"]) != 1 || static["childDisplay"] != "inline" || static["childPosition"] != "static" {
		t.Fatalf("unexpected in-flow child layout: %#v", static)
	}
}

func TestShadowHostLayoutDerivesFromShadowChildren(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	v, err := p.Evaluate(context.Background(), `(()=>{document.body.style.margin='0';const outer=document.createElement('div'),host=document.createElement('div'),root=host.attachShadow({mode:'closed'}),frame=document.createElement('iframe');frame.style.width='300px';frame.style.height='65px';frame.style.border='0';root.appendChild(frame);outer.appendChild(host);document.body.appendChild(outer);return{height:host.getBoundingClientRect().height,outerHeight:outer.getBoundingClientRect().height,offsetHeight:host.offsetHeight,width:host.getBoundingClientRect().width}})()`)
	if err != nil {
		t.Fatal(err)
	}
	result := v.(map[string]any)
	if numberValue(result["height"]) != 65 || numberValue(result["outerHeight"]) != 65 || numberValue(result["offsetHeight"]) != 65 || numberValue(result["width"]) != float64(p.Environment().Window.ViewportWidth) {
		t.Fatalf("unexpected shadow host layout: %#v", result)
	}
}

func TestComputedStyleIncludesInheritedUAVisibility(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	v, err := p.Evaluate(context.Background(), `(()=>{const node=document.createElement('div'),frame=document.createElement('iframe');document.body.appendChild(node);document.body.appendChild(frame);const style=getComputedStyle(node);return{visibility:style.visibility,property:style.getPropertyValue('visibility'),contentVisibility:style.contentVisibility,opacity:style.opacity,transform:style.transform,frameDisplay:getComputedStyle(frame).display}})()`)
	if err != nil {
		t.Fatal(err)
	}
	result := v.(map[string]any)
	if result["visibility"] != "visible" || result["property"] != "visible" || result["contentVisibility"] != "visible" || result["opacity"] != "1" || result["transform"] != "none" || result["frameDisplay"] != "inline" {
		t.Fatalf("unexpected computed UA defaults: %#v", result)
	}
}

func TestComputedStyleAppliesGenericAuthorCascade(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	v, err := p.Evaluate(context.Background(), `(()=>{const sheet=document.createElement('style');sheet.textContent='*{box-sizing:border-box}.outer .box{display:flex;transform:translateX(2px)}.box{display:grid;opacity:.4}.box:hover{visibility:hidden}';document.head.appendChild(sheet);const outer=document.createElement('div'),node=document.createElement('div');outer.className='outer';node.className='box';outer.appendChild(node);document.body.appendChild(outer);node.style.opacity='.8';const style=getComputedStyle(node);return{display:style.display,transform:style.transform,opacity:style.opacity,visibility:style.visibility,boxSizing:style.boxSizing}})()`)
	if err != nil {
		t.Fatal(err)
	}
	result := v.(map[string]any)
	// Frozen receipt: css_geometry_integration_chrome152.json (computedTransform).
	if result["display"] != "flex" || result["transform"] != "matrix(1, 0, 0, 1, 2, 0)" || result["opacity"] != "0.8" || result["visibility"] != "visible" || result["boxSizing"] != "border-box" {
		t.Fatalf("unexpected author cascade: %#v", result)
	}
}

func TestComputedStyleUsesContainingShadowRootStyleSheets(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	v, err := p.Evaluate(context.Background(), `(()=>{const documentSheet=document.createElement('style');documentSheet.textContent='iframe{display:grid}';document.head.appendChild(documentSheet);const outside=document.createElement('iframe'),host=document.createElement('div'),root=host.attachShadow({mode:'closed'}),shadowSheet=document.createElement('style'),inside=document.createElement('iframe');shadowSheet.textContent='iframe{display:block;opacity:.4}';root.appendChild(shadowSheet);root.appendChild(inside);document.body.appendChild(outside);document.body.appendChild(host);return{outsideDisplay:getComputedStyle(outside).display,insideDisplay:getComputedStyle(inside).display,insideOpacity:getComputedStyle(inside).opacity}})()`)
	if err != nil {
		t.Fatal(err)
	}
	result := v.(map[string]any)
	if result["outsideDisplay"] != "grid" || result["insideDisplay"] != "block" || result["insideOpacity"] != "0.4" {
		t.Fatalf("unexpected shadow-root cascade: %#v", result)
	}
}

func TestElementMatchesAndClosestUseGenericSelectorSemantics(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	v, err := p.Evaluate(context.Background(), `(()=>{const outer=document.createElement('section'),middle=document.createElement('div'),node=document.createElement('span');outer.id='root';middle.className='middle';node.className='leaf';node.setAttribute('data-kind','target');middle.appendChild(node);outer.appendChild(middle);document.body.appendChild(outer);return{self:node.matches('span.leaf[data-kind="target"]'),list:node.matches('p, .leaf'),ancestor:node.closest('#root')===outer,middle:node.closest('div.middle')===middle,missing:node.closest('article')}})()`)
	if err != nil {
		t.Fatal(err)
	}
	result := v.(map[string]any)
	if result["self"] != true || result["list"] != true || result["ancestor"] != true || result["middle"] != true || result["missing"] != nil {
		t.Fatalf("unexpected matches/closest semantics: %#v", result)
	}
}

func TestMatchMediaDerivesFromCanonicalPreferences(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	p.env.Preferences.ColorScheme = "dark"
	p.env.Preferences.ReducedMotion = false
	v, err := p.Evaluate(context.Background(), `({dark:matchMedia('(prefers-color-scheme: dark)').matches,light:matchMedia('(prefers-color-scheme: light)').matches,reduce:matchMedia('(prefers-reduced-motion: reduce)').matches,noPreference:matchMedia('(prefers-reduced-motion: no-preference)').matches})`)
	if err != nil {
		t.Fatal(err)
	}
	result := v.(map[string]any)
	if result["dark"] != true || result["light"] != false || result["reduce"] != false || result["noPreference"] != true {
		t.Fatalf("media preferences diverged from canonical state: %#v", result)
	}
}

func TestCrossOriginResourceTimingRequiresTimingAllowOrigin(t *testing.T) {
	parallelBrowserTest(t)
	resource := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Query().Get("tao") == "1" {
			w.Header().Set("Timing-Allow-Origin", "*")
			w.Header().Set("Server-Timing", `edge;dur=4`)
		}
		_, _ = w.Write([]byte(`<!doctype html><body>child</body>`))
	}))
	defer resource.Close()
	document := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_, _ = w.Write([]byte(`<!doctype html><body></body>`))
	}))
	defer document.Close()
	p := testPage(t)
	if err := p.Navigate(context.Background(), document.URL); err != nil {
		t.Fatal(err)
	}
	source := fmt.Sprintf(`new Promise(resolve=>{const urls=[%q,%q],frames=urls.map(url=>{const frame=document.createElement('iframe');frame.src=url;return frame});let count=0;const loaded=()=>{if(++count!==2)return;const all=performance.getEntries();const entries=urls.map(url=>all.find(entry=>entry.name===url));resolve(entries.map(entry=>({requestStart:entry.requestStart,responseStart:entry.responseStart,responseEnd:entry.responseEnd,encodedBodySize:entry.encodedBodySize,responseStatus:entry.responseStatus,nextHopProtocol:entry.nextHopProtocol,serverTiming:entry.serverTiming.length,contentType:entry.contentType}))) };for(const frame of frames){frame.addEventListener('load',loaded);document.body.appendChild(frame)}})`, resource.URL, resource.URL+"?tao=1")
	v, err := p.Evaluate(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	entries := v.([]any)
	blocked := entries[0].(map[string]any)
	allowed := entries[1].(map[string]any)
	if numberValue(blocked["requestStart"]) != 0 || numberValue(blocked["responseStart"]) != 0 || numberValue(blocked["encodedBodySize"]) != 0 || numberValue(blocked["responseStatus"]) != 0 || blocked["nextHopProtocol"] != "" || numberValue(blocked["serverTiming"]) != 0 || blocked["contentType"] != "" || numberValue(blocked["responseEnd"]) <= 0 {
		t.Fatalf("cross-origin timing was not redacted: %#v", blocked)
	}
	if numberValue(allowed["requestStart"]) <= 0 || numberValue(allowed["responseStart"]) <= 0 || numberValue(allowed["encodedBodySize"]) <= 0 || numberValue(allowed["responseStatus"]) != 200 || numberValue(allowed["serverTiming"]) != 1 || allowed["contentType"] != "text/html" {
		t.Fatalf("TAO-authorized timing was redacted: %#v", allowed)
	}
}

func TestExplicitFaviconUsesDeclaredURL(t *testing.T) {
	parallelBrowserTest(t)
	requested := make(chan string, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/declared.ico" {
			requested <- r.URL.RawQuery
			return
		}
		fmt.Fprint(w, `<link rel="shortcut icon" href="/declared.ico?v=7">`)
	}))
	defer ts.Close()
	p := testPage(t)
	if err := p.Navigate(context.Background(), ts.URL); err != nil {
		t.Fatal(err)
	}
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	for {
		select {
		case query := <-requested:
			if query != "v=7" {
				t.Fatalf("declared favicon query lost: %q", query)
			}
			return
		case <-deadline.C:
			t.Fatal("declared favicon was not requested")
		default:
			if err := p.AdvanceTime(context.Background(), time.Millisecond); err != nil {
				t.Fatal(err)
			}
			time.Sleep(time.Millisecond)
		}
	}
}

func TestFrameElementDerivesFromBrowsingContext(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	v, err := p.Evaluate(context.Background(), `(()=>{const iframe=document.createElement('iframe');iframe.id='child';document.body.appendChild(iframe);const childResult=iframe.contentWindow.eval("({tag:frameElement.tagName,id:frameElement.getAttribute('id'),ownsWindow:frameElement.contentWindow===window})");return{top:frameElement,child:{tag:childResult.tag,id:childResult.id,ownsWindow:childResult.ownsWindow}}})()`)
	if err != nil {
		t.Fatal(err)
	}
	m := v.(map[string]any)
	if m["top"] != nil {
		t.Fatalf("top-level frameElement must be null: %#v", v)
	}
	child := m["child"].(map[string]any)
	if child["tag"] != "IFRAME" || child["id"] != "child" || child["ownsWindow"] != true {
		t.Fatalf("child frameElement is not derived from its browsing context: %#v", child)
	}
}

func TestWindowFramesReflectsDirectChildBrowsingContexts(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	v, err := p.Evaluate(context.Background(), `(()=>{
		const first=document.createElement('iframe'),second=document.createElement('iframe');
		document.body.append(first,second);
		const before={identity:frames===window,length:window.length,first:frames[0]===first.contentWindow,second:window[1]===second.contentWindow};
		document.body.insertBefore(second,first);
		const reordered={first:frames[0]===second.contentWindow,second:frames[1]===first.contentWindow};
		second.remove();
		return{before,reordered,after:{length:frames.length,first:frames[0]===first.contentWindow,stale:window[1]}};
	})()`)
	if err != nil {
		t.Fatal(err)
	}
	got := v.(map[string]any)
	before := got["before"].(map[string]any)
	reordered := got["reordered"].(map[string]any)
	after := got["after"].(map[string]any)
	if before["identity"] != true || before["length"] != int64(2) || before["first"] != true || before["second"] != true {
		t.Fatalf("initial Window frames projection = %#v", before)
	}
	if reordered["first"] != true || reordered["second"] != true {
		t.Fatalf("reordered Window frames projection = %#v", reordered)
	}
	if after["length"] != int64(1) || after["first"] != true || after["stale"] != nil {
		t.Fatalf("updated Window frames projection = %#v", after)
	}
}

func TestDocumentCreateEventInitializesLegacyCustomEvent(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	v, err := p.Evaluate(context.Background(), `(()=>{const event=document.createEvent('CustomEvent');event.initCustomEvent('ready',true,true,{value:7});return{ctor:event.constructor===CustomEvent,type:event.type,bubbles:event.bubbles,cancelable:event.cancelable,detail:event.detail.value}})()`)
	if err != nil {
		t.Fatal(err)
	}
	got := v.(map[string]any)
	if got["ctor"] != true || got["type"] != "ready" || got["bubbles"] != true || got["cancelable"] != true || got["detail"] != int64(7) {
		t.Fatalf("legacy CustomEvent = %#v", got)
	}
}

func TestFragmentInsertionDoesNotInvokeOverriddenRemoveChild(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	got, err := p.Evaluate(context.Background(), `(()=>{const target=document.createElement('div'),fragment=document.createDocumentFragment(),child=document.createElement('span');fragment.appendChild(child);fragment.removeChild=()=>{throw new Error('observable override')};target.appendChild(fragment);return target.firstChild===child&&fragment.childNodes.length===0})()`)
	if err != nil || got != true {
		t.Fatalf("fragment insertion invoked author removeChild: %v %v", got, err)
	}
}

func TestIFrameSandboxAssignmentForwardsToDOMTokenListValue(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	v, err := p.Evaluate(context.Background(), `(()=>{const iframe=document.createElement('iframe');iframe.sandbox='allow-scripts allow-same-origin';return{attribute:iframe.getAttribute('sandbox'),value:iframe.sandbox.value,contains:iframe.sandbox.contains('allow-scripts')}})()`)
	if err != nil {
		t.Fatal(err)
	}
	result := v.(map[string]any)
	if result["attribute"] != "allow-scripts allow-same-origin" || result["value"] != "allow-scripts allow-same-origin" || result["contains"] != true {
		t.Fatalf("iframe sandbox PutForwards semantics mismatch: %#v", result)
	}
}

func TestRemovingIframeDetachesChildBrowsingContext(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	if _, err := p.Evaluate(context.Background(), `(()=>{const iframe=document.createElement('iframe');document.body.appendChild(iframe);iframe.remove()})()`); err != nil {
		t.Fatal(err)
	}
	if len(p.frames) != 1 || len(p.Top.children) != 0 || len(p.Top.Realm.childFrames) != 0 {
		t.Fatalf("iframe browsing context leaked: frames=%d children=%d realmChildren=%d", len(p.frames), len(p.Top.children), len(p.Top.Realm.childFrames))
	}
}

func TestSameOriginWindowProxyForwardsRealmGlobalsAfterDetach(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	v, err := p.Evaluate(context.Background(), `(()=>{const iframe=document.createElement('iframe');document.body.appendChild(iframe);const child=iframe.contentWindow,ChildDocument=child.Document,ChildElement=child.Element;iframe.remove();return{documentType:typeof ChildDocument,elementType:typeof ChildElement,distinctDocument:ChildDocument!==Document,distinctElement:ChildElement!==Element,stable:ChildDocument===child.Document}})()`)
	if err != nil {
		t.Fatal(err)
	}
	result := v.(map[string]any)
	if result["documentType"] != "function" || result["elementType"] != "function" || result["distinctDocument"] != true || result["distinctElement"] != true || result["stable"] != true {
		t.Fatalf("same-origin WindowProxy did not forward child globals: %#v", result)
	}
}

func TestReadableWritableAndTransformStreams(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	v, err := p.Evaluate(context.Background(), `(async()=>{const blob=new Blob(['abc']),reader=blob.stream().getReader(),first=await reader.read(),last=await reader.read(),written=[];const writable=new WritableStream({write(v){written.push(v)}}),transform=new TransformStream({transform(v,c){c.enqueue(v*2)}}),transformedReader=transform.readable.getReader(),writer=transform.writable.getWriter();const transformedRead=transformedReader.read();await writer.write(3);await writer.close();const transformed=await transformedRead;const source=new ReadableStream({start(c){c.enqueue('x');c.close()}});await source.pipeTo(writable);return {instance:blob.stream() instanceof ReadableStream,text:String.fromCharCode(...first.value),done:last.done,written:written.join(''),transformed:transformed.value}})()`)
	if err != nil {
		t.Fatal(err)
	}
	m := v.(map[string]any)
	if m["instance"] != true || m["text"] != "abc" || m["done"] != true || m["written"] != "x" || m["transformed"] != int64(6) {
		t.Fatalf("unexpected stream semantics: %#v", m)
	}
}

func TestBase64WindowFunctions(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	v, err := p.Evaluate(context.Background(), `({encoded:btoa('\x00\xffabc'),decoded:Array.from(atob('AP9hYmM=')).map(x=>x.charCodeAt(0))})`)
	if err != nil {
		t.Fatal(err)
	}
	m := v.(map[string]any)
	if m["encoded"] != "AP9hYmM=" {
		t.Fatalf("unexpected encoding: %#v", m)
	}
	decoded := m["decoded"].([]any)
	if len(decoded) != 5 || decoded[0] != int64(0) || decoded[1] != int64(255) {
		t.Fatalf("unexpected decoding: %#v", decoded)
	}
}

func TestCSPUsesOnePolicyForInlineAndExternalScripts(t *testing.T) {
	parallelBrowserTest(t)
	var externalLoads int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/blocked.js" {
			externalLoads++
			fmt.Fprint(w, `window.externalRan=true`)
			return
		}
		w.Header().Set("Content-Security-Policy", "script-src 'none'")
		fmt.Fprint(w, `<script>window.inlineRan=true</script><script src="/blocked.js"></script>`)
	}))
	defer srv.Close()
	p := testPage(t)
	if err := p.Navigate(context.Background(), srv.URL); err != nil {
		t.Fatal(err)
	}
	v, err := p.Evaluate(context.Background(), `typeof inlineRan+','+typeof externalRan`)
	if err != nil || v != "undefined,undefined" || externalLoads != 0 {
		t.Fatalf("CSP did not block scripts before loading: value=%#v loads=%d error=%v", v, externalLoads, err)
	}
	p.SetBypassCSP(true)
	if err := p.Navigate(context.Background(), srv.URL); err != nil {
		t.Fatal(err)
	}
	v, err = p.Evaluate(context.Background(), `inlineRan===true&&externalRan===true`)
	if err != nil || v != true || externalLoads != 1 {
		t.Fatalf("CSP bypass failed: value=%#v loads=%d error=%v", v, externalLoads, err)
	}
}

func TestCSPNonceReflectionAllowsDynamicScript(t *testing.T) {
	parallelBrowserTest(t)
	var externalLoads int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/dynamic.js" {
			externalLoads++
			fmt.Fprint(w, `window.dynamicNonceRan=true`)
			return
		}
		w.Header().Set("Content-Security-Policy", "script-src 'nonce-good'")
		fmt.Fprint(w, `<body><script nonce="good">const s=document.createElement('script');s.nonce='good';s.onload=()=>window.dynamicLoadState=document.readyState;s.src='/dynamic.js';document.querySelector('body').appendChild(s)</script>`)
	}))
	defer srv.Close()
	p := testPage(t)
	if err := p.Navigate(context.Background(), srv.URL); err != nil {
		t.Fatal(err)
	}
	v, err := p.Evaluate(context.Background(), `dynamicNonceRan===true&&dynamicLoadState==='interactive'`)
	if err != nil || v != true || externalLoads != 1 {
		t.Fatalf("nonce-bearing dynamic script did not run: value=%#v loads=%d error=%v", v, externalLoads, err)
	}
}

func TestMicrotaskInsertedResourceDelaysCompleteState(t *testing.T) {
	parallelBrowserTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/outer.js":
			fmt.Fprint(w, `Promise.resolve().then(()=>{const nested=document.createElement('script');nested.src='/nested.js';nested.onload=()=>window.nestedLoadState=document.readyState;document.head.appendChild(nested)})`)
		case "/nested.js":
			fmt.Fprint(w, `window.nestedRan=true`)
		default:
			fmt.Fprint(w, `<script>const outer=document.createElement('script');outer.src='/outer.js';document.head.appendChild(outer)</script>`)
		}
	}))
	defer server.Close()
	p := testPage(t)
	if err := p.Navigate(context.Background(), server.URL); err != nil {
		t.Fatal(err)
	}
	value, err := p.Evaluate(context.Background(), `nestedRan===true&&nestedLoadState==='interactive'&&document.readyState==='complete'`)
	if err != nil || value != true {
		t.Fatalf("transitive resource did not delay load transition: value=%#v error=%v", value, err)
	}
}

func TestFeatureDetectionTracesMissingObjectProperty(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	if _, err := p.Evaluate(context.Background(), `navigator.notYetImplemented`); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range p.Trace().Events() {
		if event.Kind == "unsupported" && event.Name == "Navigator.notYetImplemented" {
			found = true
		}
	}
	if !found {
		t.Fatal("missing feature-detection access was not traced")
	}
}

func TestDocumentStructureReadyStateAndScopedQuery(t *testing.T) {
	parallelBrowserTest(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `<head></head><body><section><span id="inside"></span></section></body><script>window.stateDuringScript=document.readyState</script>`)
	}))
	defer srv.Close()
	p := testPage(t)
	if err := p.Navigate(context.Background(), srv.URL); err != nil {
		t.Fatal(err)
	}
	v, err := p.Evaluate(context.Background(), `({during:stateDuringScript,after:document.readyState,html:document.documentElement.tagName,head:document.head.tagName,body:document.body.tagName,inside:document.body.querySelector('#inside').id})`)
	if err != nil {
		t.Fatal(err)
	}
	m := v.(map[string]any)
	if m["during"] != "loading" || m["after"] != "complete" || m["html"] != "HTML" || m["head"] != "HEAD" || m["body"] != "BODY" || m["inside"] != "inside" {
		t.Fatalf("unexpected document semantics: %#v", m)
	}
}

func TestElementFeatureDetectionIsTraced(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	if _, err := p.Evaluate(context.Background(), `document.body.missingElementAPI`); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range p.Trace().Events() {
		if event.Kind == "unsupported" && event.Name == "Element<body>.missingElementAPI" {
			found = true
		}
	}
	if !found {
		t.Fatal("missing element property was not traced")
	}
}

func TestGlobalAccessTracingPreservesWindowSemantics(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	v, err := p.Evaluate(context.Background(), `({identity:window===globalThis&&self===window,dynamicIdentity:Function('return this===globalThis')(),tag:Object.prototype.toString.call(window),missing:window.missingGlobalAPI,reference:(()=>{try{missingGlobalAPI;return false}catch(e){return e instanceof ReferenceError}})()})`)
	if err != nil {
		t.Fatal(err)
	}
	m := v.(map[string]any)
	if m["identity"] != true || m["dynamicIdentity"] != true || m["tag"] != "[object Window]" || m["missing"] != nil || m["reference"] != true {
		t.Fatalf("global proxy changed semantics: %#v", m)
	}
}

func TestPerformanceObserverBufferedDelivery(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	v, err := p.Evaluate(context.Background(), `new Promise(resolve=>{const observer=new PerformanceObserver((list,self)=>resolve({same:self===observer,count:list.getEntriesByType('navigation').length,supported:PerformanceObserver.supportedEntryTypes.includes('resource')}));observer.observe({type:'navigation',buffered:true})})`)
	if err != nil {
		t.Fatal(err)
	}
	m := v.(map[string]any)
	if m["same"] != true || m["count"] != int64(1) || m["supported"] != true {
		t.Fatalf("unexpected observer delivery: %#v", m)
	}
}

func TestPerformanceObserverReceivesResourceWithoutPollingTimers(t *testing.T) {
	parallelBrowserTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = fmt.Fprint(w, "ok")
	}))
	defer server.Close()
	p := testPage(t)
	v, err := p.Evaluate(context.Background(), `new Promise(resolve=>{const observer=new PerformanceObserver(list=>{const entries=list.getEntriesByType('resource');if(entries.length){observer.disconnect();resolve({count:entries.length,name:entries[0].name})}});observer.observe({type:'resource'});fetch(`+fmt.Sprintf("%q", server.URL+"/observed")+`)})`)
	if err != nil {
		t.Fatal(err)
	}
	result := v.(map[string]any)
	if result["count"] != int64(1) || result["name"] != server.URL+"/observed" {
		t.Fatalf("unexpected event-driven PerformanceObserver result: %#v", result)
	}
}

func TestPerformanceResourceEntriesAreOrderedByFetchStart(t *testing.T) {
	parallelBrowserTest(t)
	releaseFirst := make(chan struct{})
	var releaseOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Cache-Control", "no-store")
		if request.URL.Path == "/first-slow" {
			select {
			case <-releaseFirst:
			case <-request.Context().Done():
				return
			}
		} else if request.URL.Path == "/release-first" {
			releaseOnce.Do(func() { close(releaseFirst) })
		}
		_, _ = fmt.Fprint(w, "ok")
	}))
	defer server.Close()
	defer releaseOnce.Do(func() { close(releaseFirst) })
	p := testPage(t)
	source := fmt.Sprintf(`(async () => {
  const first = fetch(%q);
  await new Promise(resolve => setTimeout(resolve, 5));
  const secondURL = %q;
  const secondEntry = new Promise(resolve => {
    const observer = new PerformanceObserver(list => {
      const entry = list.getEntriesByType('resource').find(item => item.name === secondURL);
      if (entry) {
        observer.disconnect();
        resolve(entry);
      }
    });
    observer.observe({type: 'resource', buffered: true});
  });
  const second = await fetch(secondURL);
  await second.text();
  await secondEntry;
  await new Promise(resolve => setTimeout(resolve, 5));
  await fetch(%q);
  await first;
  return performance.getEntriesByType('resource')
    .filter(entry => entry.name.includes('first-slow') || entry.name.includes('second-fast'))
    .map(entry => ({name: entry.name, startTime: entry.startTime, responseEnd: entry.responseEnd}));
})()`, server.URL+"/first-slow", server.URL+"/second-fast", server.URL+"/release-first")
	v, err := p.Evaluate(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	entries := v.([]any)
	if len(entries) != 2 {
		t.Fatalf("unexpected resource entries: %#v", entries)
	}
	first := entries[0].(map[string]any)
	second := entries[1].(map[string]any)
	if !strings.Contains(fmt.Sprint(first["name"]), "first-slow") || !strings.Contains(fmt.Sprint(second["name"]), "second-fast") {
		t.Fatalf("entries were ordered by completion rather than fetch start: %#v", entries)
	}
	if numberValue(first["responseEnd"]) <= numberValue(second["responseEnd"]) {
		t.Fatalf("test did not produce reverse completion order: %#v", entries)
	}
}

func TestPerformanceObserverReceivesFinalizedNavigationEntry(t *testing.T) {
	serialBrowserTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `<script>window.navigationDone=new Promise(resolve=>{new PerformanceObserver(list=>{const entries=list.getEntriesByType('navigation');if(entries.length)resolve({count:entries.length,duration:entries[0].duration,complete:document.readyState})}).observe({entryTypes:['navigation']})})</script>`)
	}))
	defer server.Close()
	p := testPage(t)
	if err := p.Navigate(context.Background(), server.URL); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	v, err := p.Evaluate(ctx, `navigationDone`)
	if err != nil {
		t.Fatalf("navigation observer: %v", err)
	}
	result := v.(map[string]any)
	if result["count"] != int64(1) || numberValue(result["duration"]) < 0 || result["complete"] != "complete" {
		t.Fatalf("unexpected finalized navigation delivery: %#v", result)
	}
}

func TestBrowserOwnedFallbackFaviconHasResourceTimingEntry(t *testing.T) {
	parallelBrowserTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/favicon.ico" {
			w.Header().Set("Content-Type", "image/x-icon")
			_, _ = fmt.Fprint(w, "icon")
			return
		}
		_, _ = fmt.Fprint(w, `<html><body>page without explicit icon</body></html>`)
	}))
	defer server.Close()
	p := testPage(t)
	if err := p.Navigate(context.Background(), server.URL); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		loaded := false
		for _, event := range p.Trace().Events() {
			if event.Kind == trace.Network && event.Name == "response" && event.Data["initiator"] == network.Other {
				loaded = true
				break
			}
		}
		if loaded {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fallback favicon did not finish")
		}
		time.Sleep(time.Millisecond)
	}
	if err := p.AdvanceTime(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	v, err := p.Evaluate(context.Background(), `(()=>{const entries=performance.getEntriesByName(`+fmt.Sprintf("%q", server.URL+"/favicon.ico")+`);return{count:entries.length,initiatorType:entries[0].initiatorType}})()`)
	if err != nil {
		t.Fatal(err)
	}
	result := v.(map[string]any)
	if result["count"] != int64(1) || result["initiatorType"] != "other" {
		t.Fatalf("unexpected fallback favicon timing: %#v", result)
	}
}

func TestClassSelectorAndCanonicalClassList(t *testing.T) {
	parallelBrowserTest(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `<main class="main-content old"></main>`)
	}))
	defer srv.Close()
	p := testPage(t)
	if err := p.Navigate(context.Background(), srv.URL); err != nil {
		t.Fatal(err)
	}
	v, err := p.Evaluate(context.Background(), `(()=>{const e=document.querySelector('.main-content');e.classList.remove('old');e.classList.add('ready');return {tag:e.tagName,ready:e.classList.contains('ready'),attr:e.getAttribute('class'),same:document.querySelector('main.ready')!==null}})()`)
	if err != nil {
		t.Fatal(err)
	}
	m := v.(map[string]any)
	if m["tag"] != "MAIN" || m["ready"] != true || m["attr"] != "main-content ready" || m["same"] != true {
		t.Fatalf("unexpected selector/token semantics: %#v", m)
	}
}

func TestInnerHTMLAndStaticQuerySelectorAll(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	v, err := p.Evaluate(context.Background(), `(()=>{const box=document.createElement('div');document.body.appendChild(box);box.innerHTML='<span class="x">one</span><span class="x">two</span>';const list=box.querySelectorAll('.x');box.innerHTML='<b>changed</b>';return {serialized:list[0].textContent+list[1].textContent,length:list.length,first:box.firstElementChild.tagName,html:box.innerHTML}})()`)
	if err != nil {
		t.Fatal(err)
	}
	m := v.(map[string]any)
	if m["serialized"] != "onetwo" || m["length"] != int64(2) || m["first"] != "B" || m["html"] != "<b>changed</b>" {
		t.Fatalf("unexpected fragment/NodeList semantics: %#v", m)
	}
}

func TestChromeDOMPrimitivesExercisedByModuleHydration(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	v, err := p.Evaluate(context.Background(), `(()=>{const box=document.createElement('div');box.innerHTML='<span>child</span>tail';const script=document.createElement('script');script.textContent='const answer=42';const comment=document.createComment('hello');const svg=document.createElementNS('http://www.w3.org/2000/svg','svg'),rect=svg.createSVGRect();const observer=new IntersectionObserver(()=>{});const fragment=document.createDocumentFragment(),fragmentChild=document.createElement('i');fragment.appendChild(fragmentChild);const result={children:box.childNodes.length,firstType:box.firstChild.nodeType,firstName:box.firstChild.nodeName,has:box.hasChildNodes(),scriptText:script.text,scriptContent:script.textContent,scriptOwn:Object.prototype.hasOwnProperty.call(HTMLScriptElement.prototype,'textContent'),commentTag:Object.prototype.toString.call(comment),commentType:comment.nodeType,commentName:comment.nodeName,commentData:comment.data,commentContent:comment.textContent,commentHas:comment.hasChildNodes(),svgTag:Object.prototype.toString.call(rect),rect:[rect.x,rect.y,rect.width,rect.height],observerTag:Object.prototype.toString.call(observer),root:observer.root,rootMargin:observer.rootMargin,scrollMargin:observer.scrollMargin,thresholds:observer.thresholds,records:observer.takeRecords(),fragmentTag:Object.prototype.toString.call(fragment),fragmentType:fragment.nodeType,fragmentName:fragment.nodeName,fragmentHas:fragment.hasChildNodes(),fragmentFirst:fragment.firstChild===fragmentChild,fragmentCount:fragment.childNodes.length,namespacesPresent:'namespaces' in document,namespacesUndefined:document.namespaces===undefined};observer.disconnect();return result})()`)
	if err != nil {
		t.Fatal(err)
	}
	m := v.(map[string]any)
	if m["children"] != int64(2) || m["firstType"] != int64(1) || m["firstName"] != "SPAN" || m["has"] != true || m["scriptText"] != "const answer=42" || m["scriptContent"] != "const answer=42" || m["scriptOwn"] != true || m["commentTag"] != "[object Comment]" || m["commentType"] != int64(8) || m["commentName"] != "#comment" || m["commentData"] != "hello" || m["commentContent"] != "hello" || m["commentHas"] != false || m["svgTag"] != "[object SVGRect]" || m["observerTag"] != "[object IntersectionObserver]" || m["root"] != nil || m["rootMargin"] != "0px 0px 0px 0px" || m["scrollMargin"] != "0px 0px 0px 0px" || m["fragmentTag"] != "[object DocumentFragment]" || m["fragmentType"] != int64(11) || m["fragmentName"] != "#document-fragment" || m["fragmentHas"] != true || m["fragmentFirst"] != true || m["fragmentCount"] != int64(1) || m["namespacesPresent"] != false || m["namespacesUndefined"] != true {
		t.Fatalf("unexpected DOM hydration primitives: %#v", m)
	}
}

func TestHistoryStateScrollRestorationAndLinkReflection(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	v, err := p.Evaluate(context.Background(), `(()=>{const link=document.createElement('link'),initial={href:link.href,rel:link.rel,as:link.as,crossOrigin:link.crossOrigin};link.href='/asset.js';link.rel='modulepreload';link.as='script';link.crossOrigin='anonymous';history.replaceState({answer:42},'');const automatic=history.scrollRestoration;history.scrollRestoration='manual';history.scrollRestoration='invalid';return{initial,tag:Object.prototype.toString.call(link),href:link.href,rel:link.rel,as:link.as,crossOrigin:link.crossOrigin,hrefAttr:link.getAttribute('href'),state:history.state.answer,automatic,scrollRestoration:history.scrollRestoration}})()`)
	if err != nil {
		t.Fatal(err)
	}
	m := v.(map[string]any)
	initial := m["initial"].(map[string]any)
	if initial["href"] != "" || initial["rel"] != "" || initial["as"] != "" || initial["crossOrigin"] != nil || m["tag"] != "[object HTMLLinkElement]" || m["href"] != "about:///asset.js" || m["rel"] != "modulepreload" || m["as"] != "script" || m["crossOrigin"] != "anonymous" || m["hrefAttr"] != "/asset.js" || m["state"] != int64(42) || m["automatic"] != "auto" || m["scrollRestoration"] != "manual" {
		t.Fatalf("unexpected history/link semantics: %#v", m)
	}
}

func TestDynamicLinkLoadEvents(t *testing.T) {
	parallelBrowserTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/module.js":
			w.Header().Set("Content-Type", "text/javascript")
			fmt.Fprint(w, "export default 1")
		case "/style.css":
			w.Header().Set("Content-Type", "text/css")
			fmt.Fprint(w, "body{color:red}")
		default:
			fmt.Fprint(w, "<!doctype html><html><head></head><body></body></html>")
		}
	}))
	defer server.Close()
	p := testPage(t)
	if err := p.Navigate(context.Background(), server.URL); err != nil {
		t.Fatal(err)
	}
	v, err := p.Evaluate(context.Background(), `(async()=>{const load=(rel,href)=>new Promise(resolve=>{const link=document.createElement('link');link.rel=rel;link.href=href;link.onload=()=>resolve('load');link.onerror=()=>resolve('error');document.head.appendChild(link)});return{modulepreload:await load('modulepreload','/module.js'),stylesheet:await load('stylesheet','/style.css')}})()`)
	if err != nil {
		t.Fatal(err)
	}
	m := v.(map[string]any)
	if m["modulepreload"] != "load" || m["stylesheet"] != "load" {
		t.Fatalf("unexpected dynamic link completion: %#v", m)
	}
}

func TestParentSiblingAndNodeReordering(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	v, err := p.Evaluate(context.Background(), `(()=>{const p=document.createElement('div'),a=document.createElement('i'),b=document.createElement('b');document.body.appendChild(p);p.appendChild(a);p.appendChild(b);p.insertBefore(b,a);const before={parent:b.parentNode===p,first:p.firstElementChild===null?null:p.firstElementChild.tagName,next:b.nextSibling.tagName,count:p.childElementCount};p.removeChild(b);return {...before,after:p.children.length,detached:b.parentNode===null}})()`)
	if err != nil {
		t.Fatal(err)
	}
	m := v.(map[string]any)
	if m["parent"] != true || m["first"] != "B" || m["next"] != "I" || m["count"] != int64(2) || m["after"] != int64(1) || m["detached"] != true {
		t.Fatalf("unexpected tree navigation: %#v", m)
	}
}

func TestDOMWrappersPreserveRealmLocalIdentity(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	v, err := p.Evaluate(context.Background(), `(()=>{const body=document.body,div=document.createElement('div');div.id='identity';body.appendChild(div);return{body:body===document.body,query:div===document.querySelector('#identity'),parent:div.parentNode===body,collection:div===body.children[0]}})()`)
	if err != nil {
		t.Fatal(err)
	}
	m := v.(map[string]any)
	if m["body"] != true || m["query"] != true || m["parent"] != true || m["collection"] != true {
		t.Fatalf("DOM wrapper identity diverged: %#v", m)
	}
}

func TestDynamicScriptLoadEvents(t *testing.T) {
	parallelBrowserTest(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/event.js" {
			fmt.Fprint(w, `window.loadedCode=true`)
			return
		}
		fmt.Fprint(w, `<body></body>`)
	}))
	defer srv.Close()
	p := testPage(t)
	if err := p.Navigate(context.Background(), srv.URL); err != nil {
		t.Fatal(err)
	}
	v, err := p.Evaluate(context.Background(), `new Promise(resolve=>{const s=document.createElement('script'),plain=new Event('plain').isTrusted;let listener=false,intercepted=false;const publicDispatch=EventTarget.prototype.dispatchEvent;EventTarget.prototype.dispatchEvent=function(...args){intercepted=true;return publicDispatch.apply(this,args)};s.addEventListener('load',()=>listener=true);s.onload=function(e){resolve({code:loadedCode,listener,type:e.type,trusted:e.isTrusted,plain,intercepted,receiver:this===s,state:document.readyState})};s.src='/event.js';document.body.appendChild(s)})`)
	if err != nil {
		t.Fatal(err)
	}
	m := v.(map[string]any)
	if m["code"] != true || m["listener"] != true || m["type"] != "load" || m["trusted"] != true || m["plain"] != false || m["intercepted"] != false || m["receiver"] != true || m["state"] != "complete" {
		t.Fatalf("unexpected dynamic script event: %#v", m)
	}
}

func TestScriptReflectionAndCurrentScript(t *testing.T) {
	parallelBrowserTest(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `<script id="running">window.seenCurrent=document.currentScript.id</script>`)
	}))
	defer srv.Close()
	p := testPage(t)
	if err := p.Navigate(context.Background(), srv.URL); err != nil {
		t.Fatal(err)
	}
	v, err := p.Evaluate(context.Background(), `(()=>{const s=document.createElement('script');s.src='/asset.js';s.async=true;s.defer=true;const reflected=s.async&&s.defer;s.async=false;s.defer=false;return {seen:seenCurrent,after:document.currentScript,reflected,cleared:!s.async&&!s.defer,src:s.src,raw:s.getAttribute('src')}})()`)
	if err != nil {
		t.Fatal(err)
	}
	m := v.(map[string]any)
	if m["seen"] != "running" || m["after"] != nil || m["reflected"] != true || m["cleared"] != true || m["src"] != srv.URL+"/asset.js" || m["raw"] != "/asset.js" {
		t.Fatalf("unexpected script semantics: %#v", m)
	}
}
