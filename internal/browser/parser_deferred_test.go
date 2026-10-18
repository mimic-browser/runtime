package browser

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	chrome152 "github.com/moreveal/mimic/chrome/152"
	v8engine "github.com/moreveal/mimic/internal/engine/v8"
)

func deferredParserFixture(t *testing.T, module bool) *httptest.Server {
	t.Helper()
	fastRequested := make(chan struct{})
	var once sync.Once
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		switch r.URL.Path {
		case "/slow.js":
			select {
			case <-fastRequested:
			case <-time.After(2 * time.Second):
				t.Error("later deferred fetch did not start while earlier response was held")
				return
			case <-r.Context().Done():
				return
			}
			fmt.Fprint(w, `events.push('slow:'+document.readyState+':'+!!document.body+':'+document.currentScript.id);queueMicrotask(()=>events.push('micro:slow'));`)
		case "/fast.js":
			once.Do(func() { close(fastRequested) })
			fmt.Fprint(w, `events.push('fast:'+document.readyState+':'+!!document.body);`)
		case "/missing.js":
			w.WriteHeader(http.StatusNotFound)
		case "/throws.js":
			fmt.Fprint(w, `throw new Error('expected deferred exception')`)
		default:
			w.Header().Set("Content-Type", "text/html; charset=UTF-8")
			middle := ""
			if module {
				middle = `<script type=module>events.push('module:'+document.readyState+':'+!!document.body)</script>`
			}
			fmt.Fprint(w, `<!doctype html><head><script>events=[];document.addEventListener('DOMContentLoaded',()=>events.push('DCL'))</script><script defer>events.push('inline:'+document.readyState+':'+!!document.body)</script><script id=slow defer src='/slow.js' onload="events.push('load:slow')"></script>`+middle+`<script defer src='/missing.js' onerror="events.push('error:missing')"></script><script defer src='/throws.js' onload="events.push('load:throws')" onerror="events.push('wrong:error')"></script><script defer src='/fast.js' onload="events.push('load:fast')"></script></head><body><p id=tail>ready</p><script>events.push('tail')</script>`)
		}
	}))
}

func TestParserInsertedAsyncClassicDoesNotPauseParsing(t *testing.T) {
	serialBrowserTest(t)
	started := make(chan struct{})
	parsed := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/async.js" {
			close(started)
			select {
			case <-release:
				fmt.Fprint(w, `events.push('async:'+document.readyState+':'+!!document.getElementById('tail'))`)
			case <-r.Context().Done():
			}
			return
		}
		if r.URL.Path == "/parsed" {
			close(parsed)
			return
		}
		fmt.Fprint(w, `<!doctype html><head><script>events=[]</script><script async defer src="/async.js"></script></head><body><div id="tail">ready</div><script>events.push('tail');fetch('/parsed')</script>`)
	}))
	defer server.Close()
	defer releaseOnce.Do(func() { close(release) })
	b, err := New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	c := b.NewContext()
	defer c.Close()
	p, err := c.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	{
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		navigation := make(chan error, 1)
		go func() { navigation <- p.Navigate(ctx, server.URL) }()
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("async script fetch did not start")
		}
		select {
		case <-parsed:
		case <-time.After(2 * time.Second):
			t.Fatal("parser waited for async script response before executing tail")
		}
		releaseOnce.Do(func() { close(release) })
		if err := <-navigation; err != nil {
			t.Fatal(err)
		}
		got, err := p.Evaluate(ctx, `JSON.stringify({state:document.readyState,events})`)
		if err != nil || got != `{"state":"complete","events":["tail","async:interactive:true"]}` {
			t.Fatalf("async execution or load ordering: %v, %v", got, err)
		}
	}
}

func TestDeferredClassicParserLifecycle(t *testing.T) {
	serialBrowserTest(t)
	for _, mode := range []string{"navigation", "iframe", "document-write"} {
		t.Run(mode, func(t *testing.T) {
			historyTestPages(t, func(t *testing.T, p *Page) {
				server := deferredParserFixture(t, false)
				defer server.Close()
				ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
				defer cancel()
				if err := p.Navigate(ctx, server.URL); err != nil {
					t.Fatal(err)
				}
				var expression string
				switch mode {
				case "navigation":
					expression = `new Promise(resolve=>{const done=()=>resolve(events.join('|'));if(document.readyState==='complete')done();else addEventListener('load',done,{once:true})})`
				case "iframe":
					expression = `new Promise(resolve=>{const f=document.createElement('iframe');f.onload=()=>resolve(f.contentWindow.events.join('|'));f.src='/child';document.body.appendChild(f)})`
				case "document-write":
					expression = `(async()=>{const html=await(await fetch('/written')).text();return new Promise(resolve=>{const f=document.createElement('iframe');document.body.appendChild(f);const d=f.contentDocument;d.open();f.onload=()=>resolve(f.contentWindow.events.join('|'));d.write(html);d.close()})})()`
				}
				got, err := p.Evaluate(ctx, expression)
				want := "inline:loading:false|tail|slow:interactive:true:slow|micro:slow|load:slow|error:missing|load:throws|fast:interactive:true|load:fast|DCL"
				if mode == "document-write" {
					// This case checks parser order and body availability. The existing
					// document.open readyState model differs from Chrome's initial
					// about:blank iframe and is independent of deferred execution.
					withoutState := strings.NewReplacer(":loading:", ":", ":interactive:", ":", ":complete:", ":")
					if value, ok := got.(string); ok {
						got = withoutState.Replace(value)
					}
					want = withoutState.Replace(want)
				}
				if err != nil || got != want {
					t.Fatalf("%v, %v; want %s", got, err, want)
				}
			})
		})
	}
}

// Order and microtask/load placement were measured against frozen Chrome 152
// with a local receiver, independently of Google's application.
func TestDeferredClassicAndModuleShareParserOrder(t *testing.T) {
	serialBrowserTest(t)
	p := newAsyncModulePage(t)
	server := deferredParserFixture(t, true)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := p.Navigate(ctx, server.URL); err != nil {
		t.Fatal(err)
	}
	got, err := p.Evaluate(ctx, `new Promise(resolve=>{const done=()=>resolve(events.join('|'));if(document.readyState==='complete')done();else addEventListener('load',done,{once:true})})`)
	want := "inline:loading:false|tail|slow:interactive:true:slow|micro:slow|load:slow|module:interactive:true|error:missing|load:throws|fast:interactive:true|load:fast|DCL"
	if err != nil || got != want {
		t.Fatalf("%v, %v; want %s", got, err, want)
	}
}

func TestDeferredScriptDoesNotDestructivelyWriteDocument(t *testing.T) {
	serialBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/write.js" {
				w.Header().Set("Content-Type", "text/javascript")
				fmt.Fprint(w, `document.write('<p id=wrong>replaced</p>');globalThis.executed=true`)
				return
			}
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<head><script defer src=/write.js></script></head><body><p id=original>retained</p>`)
		}))
		defer server.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := p.Navigate(ctx, server.URL); err != nil {
			t.Fatal(err)
		}
		got, err := p.Evaluate(ctx, `!!globalThis.executed&&!!document.getElementById('original')&&!document.getElementById('wrong')`)
		if err != nil || got != true {
			t.Fatalf("%v %v", got, err)
		}
	})
}
