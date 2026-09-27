package browser

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// The event sequence was measured in headful frozen Chrome 152 with the same
// policy. Neither blocked request may reach the server.
func TestConnectionCSPFetchAndXHR(t *testing.T) {
	var connections atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/blocked" {
			connections.Add(1)
		}
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-eval' 'unsafe-inline'")
		fmt.Fprint(w, `<!doctype html><link rel="icon" href="data:,">`)
	}))
	defer server.Close()
	page, ctx := newXHRTestPage(t, server.URL)
	got, err := page.Evaluate(ctx, `(async()=>{
 const events=[];
 const xhrResult=await new Promise(resolve=>{
  const x=new XMLHttpRequest();
  x.onreadystatechange=()=>events.push('rs:'+x.readyState);
  x.onerror=()=>events.push('error');
  x.onload=()=>events.push('load');
  x.onloadend=()=>resolve({events,status:x.status});
  x.open('GET','/blocked'); x.send(); events.push('sent');
 });
 let fetchResult;
 try { await fetch('/blocked'); fetchResult='success'; }
 catch(e) { fetchResult=e.name; }
 return JSON.stringify({xhrResult,fetchResult});
})()`)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"xhrResult":{"events":["rs:1","sent","rs:4","error"],"status":0},"fetchResult":"TypeError"}`
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
	if connections.Load() != 0 {
		t.Fatalf("blocked requests reached transport: %d", connections.Load())
	}
}

func TestConnectionCSPChecksRedirectDestination(t *testing.T) {
	var destinationLoads atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		destinationLoads.Add(1)
		fmt.Fprint(w, "unexpected")
	}))
	defer destination.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, destination.URL+"/blocked", http.StatusFound)
			return
		}
		w.Header().Set("Content-Security-Policy", "connect-src 'self'; script-src 'unsafe-eval' 'unsafe-inline'")
		fmt.Fprint(w, `<!doctype html><link rel="icon" href="data:,">`)
	}))
	defer source.Close()
	page, ctx := newXHRTestPage(t, source.URL)
	got, err := page.Evaluate(ctx, `(async()=>{try{await fetch('/redirect');return 'loaded'}catch(e){return e.name}})()`)
	if err != nil {
		t.Fatal(err)
	}
	if got != "TypeError" {
		t.Fatalf("redirect result: %v", got)
	}
	if destinationLoads.Load() != 0 {
		t.Fatal("CSP-disallowed redirect reached destination")
	}
}

func TestConnectionCSPWorkerOwnPolicy(t *testing.T) {
	historyTestPages(t, func(t *testing.T, page *Page) {
		var blocked atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/worker.js":
				w.Header().Set("Content-Type", "text/javascript")
				w.Header().Set("Content-Security-Policy", "connect-src 'none'")
				fmt.Fprint(w, `onmessage=async()=>{try{await fetch('/blocked');postMessage('loaded')}catch(e){postMessage(e.name)}}`)
			case "/blocked":
				blocked.Add(1)
				fmt.Fprint(w, "unexpected")
			default:
				w.Header().Set("Content-Security-Policy", "connect-src 'self'; script-src 'self' 'unsafe-eval' 'unsafe-inline'")
				fmt.Fprint(w, `<!doctype html><link rel="icon" href="data:,">`)
			}
		}))
		defer server.Close()
		if err := page.Navigate(context.Background(), server.URL); err != nil {
			t.Fatal(err)
		}
		historyEval(t, page, `new Promise((resolve,reject)=>{const w=new Worker('/worker.js');w.onmessage=e=>{w.terminate();resolve(e.data)};w.onerror=e=>reject(Error(e.message));w.postMessage(null)})`, "TypeError")
		if blocked.Load() != 0 {
			t.Fatal("worker CSP-disallowed fetch reached transport")
		}
	})
}

func TestConnectionCSPWebSocketAdmission(t *testing.T) {
	historyTestPages(t, func(t *testing.T, page *Page) {
		var connections atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/blocked" {
				connections.Add(1)
			}
			w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-eval' 'unsafe-inline'")
			fmt.Fprint(w, `<!doctype html><link rel="icon" href="data:,">`)
		}))
		defer server.Close()
		if err := page.Navigate(context.Background(), server.URL); err != nil {
			t.Fatal(err)
		}
		historyEval(t, page, `new Promise(resolve=>{const events=[];const w=new WebSocket('ws://'+location.host+'/blocked');events.push('constructed:'+w.readyState);w.onclose=()=>events.push('close');w.onerror=()=>{events.push('error:'+w.readyState);setTimeout(()=>resolve(JSON.stringify(events)),0)}})`, `["constructed:3","error:3"]`)
		if connections.Load() != 0 {
			t.Fatal("CSP-disallowed socket reached transport")
		}
	})
}

func TestImageCSPDataURL(t *testing.T) {
	for _, tc := range []struct{ name, policy, want string }{
		{"blocked", "default-src 'none'", `{"events":["set:false","error"],"complete":true,"width":0}`},
		{"allowed", "default-src 'none'; img-src data:", `{"events":["set:false","load"],"complete":true,"width":1}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			historyTestPages(t, func(t *testing.T, page *Page) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Security-Policy", tc.policy+"; script-src 'unsafe-eval' 'unsafe-inline'")
					fmt.Fprint(w, `<!doctype html><link rel="icon" href="data:,">`)
				}))
				defer server.Close()
				if err := page.Navigate(context.Background(), server.URL); err != nil {
					t.Fatal(err)
				}
				historyEval(t, page, `(async()=>{const events=[];const i=new Image();await new Promise(resolve=>{i.onload=()=>{events.push('load');resolve()};i.onerror=()=>{events.push('error');resolve()};i.src='data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7';events.push('set:'+i.complete)});return JSON.stringify({events,complete:i.complete,width:i.naturalWidth})})()`, tc.want)
			})
		})
	}
}
