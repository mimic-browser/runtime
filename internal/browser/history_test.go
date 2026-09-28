package browser

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	chrome152 "github.com/moreveal/mimic/chrome/152"
	"github.com/moreveal/mimic/internal/engine"
	gojaengine "github.com/moreveal/mimic/internal/engine/goja"
	v8engine "github.com/moreveal/mimic/internal/engine/v8"
)

func historyTestPages(t *testing.T, run func(*testing.T, *Page)) {
	t.Helper()
	for name, factory := range map[string]engine.Factory{"goja": gojaengine.Factory{}, "v8": v8engine.Factory{}} {
		t.Run(name, func(t *testing.T) {
			browser, err := New(factory, chrome152.New())
			if err != nil {
				t.Fatal(err)
			}
			c := browser.NewContext()
			t.Cleanup(func() { _ = c.Close() })
			page, err := c.NewPage()
			if err != nil {
				t.Fatal(err)
			}
			page.Trace().Start()
			run(t, page)
		})
	}
}

func historyEval(t *testing.T, p *Page, script string, expected any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	value, err := p.Evaluate(ctx, script)
	if err != nil || value != expected {
		debug, _ := p.Evaluate(context.Background(), "JSON.stringify({top:location.href,child:typeof childFrame==='undefined'?null:childFrame.contentWindow.location.href,length:history.length,childLength:typeof childFrame==='undefined'?null:childFrame.contentWindow.history.length})")
		t.Logf("debug: %v", debug)
		t.Fatalf("history evaluation: value=%v expected=%v err=%v\n%s", value, expected, err, script)
	}
}

const loadHistoryChild = "new Promise(resolve=>{const f=document.createElement('iframe');globalThis.childFrame=f;f.src='/child/original';f.onload=()=>resolve(true);document.body.appendChild(f)})"

// Chrome 152.0.7977.82: History length/traversal is joint, but URL and state
// mutations belong to the calling document. Parent traversal can restore a
// child entry without changing either the parent's URL or state.
func TestFrameHistoryIsJointAndDocumentScoped(t *testing.T) {
	parallelBrowserTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { fmt.Fprint(w, "<!doctype html><body>fixture</body>") }))
	defer server.Close()
	historyTestPages(t, func(t *testing.T, p *Page) {
		if err := p.Navigate(context.Background(), server.URL+"/top"); err != nil {
			t.Fatal(err)
		}
		historyEval(t, p, loadHistoryChild, true)
		historyEval(t, p, "history.replaceState({top:1},'');true", true)
		historyEval(t, p, "childFrame.contentWindow.eval("+strconv.Quote("(()=>{const initial=history.length;history.replaceState({n:1},'','replaced');if(location.pathname!=='/child/replaced'||history.length!==initial)return 'replace';history.pushState({n:2},'','pushed');if(history.length!==initial+1||history.state.n!==2)return 'push';location.hash='hash';if(location.hash!=='#hash'||history.length!==initial+2||history.state!==null)return 'hash';return true})()")+")", true)
		historyEval(t, p, "new Promise(resolve=>{addEventListener('message',e=>resolve(e.data&&location.pathname==='/top'&&history.state.top===1));childFrame.contentWindow.eval("+strconv.Quote("globalThis.historyEvents=[];addEventListener('popstate',()=>historyEvents.push('popstate'));addEventListener('hashchange',()=>{historyEvents.push('hashchange');parent.postMessage(location.pathname==='/child/pushed'&&location.hash===''&&history.state.n===2&&historyEvents.join(',')==='popstate,hashchange','*')});history.back()")+")})", true)
		historyEval(t, p, "new Promise(resolve=>{addEventListener('message',e=>resolve(e.data&&location.pathname==='/top'&&history.state.top===1));childFrame.contentWindow.eval("+strconv.Quote("addEventListener('popstate',()=>parent.postMessage(location.pathname==='/child/replaced'&&history.state.n===1,'*'))")+");history.back()})", true)
		if p.URL() != server.URL+"/top" {
			t.Fatalf("canonical top URL: %s", p.URL())
		}
		for _, child := range p.Top.Children() {
			if child.URL() != server.URL+"/child/replaced" {
				t.Fatalf("canonical child URL: %s", child.URL())
			}
		}
	})
}

func TestHistoryRejectsCrossOriginWithoutMutation(t *testing.T) {
	parallelBrowserTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { fmt.Fprint(w, "<!doctype html><body></body>") }))
	defer server.Close()
	historyTestPages(t, func(t *testing.T, p *Page) {
		if err := p.Navigate(context.Background(), server.URL+"/original#kept"); err != nil {
			t.Fatal(err)
		}
		historyEval(t, p, "(()=>{history.replaceState({n:1},'');const before=location.href,length=history.length;for(const method of ['pushState','replaceState']){try{history[method]({n:9},'','https://example.com');return 'accepted'}catch(e){if(e.name!=='SecurityError')return e.name}if(location.href!==before||history.length!==length||history.state.n!==1)return 'mutated'}return true})()", true)
	})
}

// The audit's Chrome 152 relations oracle records copied input/nested identity,
// a stable repeated history.state identity, and DataCloneError for functions.
func TestHistoryStructuredState(t *testing.T) {
	parallelBrowserTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { fmt.Fprint(w, "<!doctype html><body></body>") }))
	defer server.Close()
	historyTestPages(t, func(t *testing.T, p *Page) {
		if err := p.Navigate(context.Background(), server.URL+"/state"); err != nil {
			t.Fatal(err)
		}
		historyEval(t, p, `(()=>{
          for(const method of ['pushState','replaceState']){
            let reads=0;const input={nested:{n:1},get observed(){reads++;return {x:7}}};input.self=input;
            history[method](input,'');const saved=history.state;input.nested.n=2;
            if(saved===input||saved.nested===input.nested||saved.nested.n!==1||saved.self!==saved||saved!==history.state||reads!==1||saved.observed.x!==7)return 'copy';
            const length=history.length,url=location.href;
            for(const invalid of [()=>{},Symbol('x'),new WeakMap(),document.body,{node:document.body},new Map([[1,document.body]]),new Proxy({},{ownKeys(){throw Error('proxy trap')}})]){
              try{history[method](invalid,'','#bad');return 'accepted'}catch(e){if(e.name!=='DataCloneError')return e.name}
              if(history.state!==saved||history.length!==length||location.href!==url)return 'mutated';
            }
            const exception={marker:1};try{history[method]({get fail(){throw exception}},'');return 'getter accepted'}catch(e){if(e!==exception)return 'exception identity'}
            if(history.state!==saved)return 'getter mutated';
          }
          const buffer=new Uint8Array([1,2,3]).buffer,key={k:1};history.replaceState({date:new Date(123),regexp:/a/gi,map:new Map([[key,key]]),set:new Set([key]),buffer,view:new Uint8Array(buffer),big:123n},'');
          const s=history.state,k=[...s.map.keys()][0];
          return s.date.getTime()===123&&s.regexp.source==='a'&&s.regexp.flags==='gi'&&s.map.get(k)===k&&s.set.has(k)&&s.buffer===s.view.buffer&&s.view[1]===2&&s.big===123n&&s.buffer!==buffer;
        })()`, true)
		historyEval(t, p, `(()=>{history.replaceState({n:1},'');globalThis.oldState=history.state;oldState.n=9;history.pushState({n:2},'');return new Promise(resolve=>{addEventListener('popstate',function handler(e){removeEventListener('popstate',handler);resolve(e.state===history.state&&history.state.n===1&&history.state!==oldState)});history.back()})})()`, true)
	})
}

// Blob/File are cloneable in Chrome, but need platform serialization hooks.
// Until those exist, reject explicitly rather than silently storing an empty object.
func TestHistoryUnsupportedPlatformStorage(t *testing.T) {
	parallelBrowserTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { fmt.Fprint(w, "<!doctype html><body></body>") }))
	defer server.Close()
	historyTestPages(t, func(t *testing.T, p *Page) {
		if err := p.Navigate(context.Background(), server.URL+"/state"); err != nil {
			t.Fatal(err)
		}
		historyEval(t, p, `(()=>{history.replaceState({n:1},'');const state=history.state,url=location.href,length=history.length;const blob=new Blob(['abc']),file=new File(['abc'],'a.txt');
          for(const method of ['pushState','replaceState'])for(const value of [blob,file,{blob},new Map([[1,file]])]){
            try{history[method](value,'','#bad');return 'accepted'}catch(e){if(e.name!=='NotSupportedError')return e.name}
            if(history.state!==state||location.href!==url||history.length!==length)return 'mutated';
          }return true})()`, true)
	})
}

func TestChildLocationNavigatesOnlyChildAndResolvesFromHistoryURL(t *testing.T) {
	parallelBrowserTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		fmt.Fprint(w, "<!doctype html><body><script>globalThis.loadedPath=location.pathname+location.search</script></body>")
	}))
	defer server.Close()
	historyTestPages(t, func(t *testing.T, p *Page) {
		if err := p.Navigate(context.Background(), server.URL+"/top"); err != nil {
			t.Fatal(err)
		}
		historyEval(t, p, loadHistoryChild, true)
		historyEval(t, p, "childFrame.contentWindow.eval("+strconv.Quote("history.replaceState(null,'','/changed/base');true")+")", true)
		for _, child := range p.Top.Children() {
			resource, err := child.Realm.resolveDocument("resource")
			if err != nil || resource.String() != server.URL+"/changed/resource" {
				t.Fatalf("child resource URL: %v %v", resource, err)
			}
		}
		for _, script := range []string{"location.assign('next')", "location.search='?query=1'", "location.pathname='/final'"} {
			historyEval(t, p, "new Promise(resolve=>{childFrame.onload=()=>resolve(location.pathname==='/top');childFrame.contentWindow.eval("+strconv.Quote(script)+")})", true)
		}
		historyEval(t, p, "(()=>{const child=childFrame.contentWindow;return child.loadedPath==='/final?query=1'&&child.document.URL===child.location.href&&location.pathname==='/top'})()", true)
		historyEval(t, p, "new Promise(resolve=>{const before=history.length;childFrame.onload=()=>resolve(history.length===before&&location.pathname==='/top');childFrame.contentWindow.eval("+strconv.Quote("location.replace('/replaced#hash')")+")})", true)
		historyEval(t, p, "childFrame.contentWindow.eval("+strconv.Quote("globalThis.marker=1;location.assign('#fragment');marker===1&&location.hash==='#fragment'")+")", true)
		historyEval(t, p, "new Promise(resolve=>{const before=history.length;childFrame.onload=()=>resolve(history.length===before&&childFrame.contentWindow.eval('typeof marker !== typeof 1'));childFrame.contentWindow.eval("+strconv.Quote("history.go(0)")+")})", true)
	})
}
