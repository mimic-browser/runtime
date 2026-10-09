package browser

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestParserScriptLookaheadOverlapsFetchesAndPreservesExecution(t *testing.T) {
	for _, frame := range []bool{false, true} {
		t.Run(fmt.Sprintf("frame=%t", frame), func(t *testing.T) {
			historyTestPages(t, func(t *testing.T, p *Page) {
				secondRequested := make(chan struct{})
				var once sync.Once
				var firstCount, secondCount atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Cache-Control", "no-store")
					switch r.URL.Path {
					case "/first.js":
						firstCount.Add(1)
						select {
						case <-secondRequested:
						case <-r.Context().Done():
							return
						case <-time.After(2 * time.Second):
							t.Error("lookahead did not overlap blocking script requests")
							return
						}
						w.Header().Set("Content-Type", "text/javascript")
						fmt.Fprint(w, `events.push('first:'+!!document.getElementById('tail')+':'+document.currentScript.id);queueMicrotask(()=>events.push('micro:first'))`)
					case "/second.js":
						secondCount.Add(1)
						once.Do(func() { close(secondRequested) })
						w.Header().Set("Content-Type", "text/javascript")
						fmt.Fprint(w, `events.push('second:'+!!document.getElementById('tail')+':'+document.currentScript.id)`)
					case "/page":
						fmt.Fprint(w, `<!doctype html><script>events=[];document.addEventListener('DOMContentLoaded',()=>events.push('DCL'))</script><script id="one" src="/first.js"></script><p id="tail">tail</p><script id="two" src="/second.js"></script>`)
					default:
						fmt.Fprint(w, `<!doctype html><body>`)
					}
				}))
				defer server.Close()
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				path := "/page"
				if frame {
					path = "/"
				}
				if err := p.Navigate(ctx, server.URL+path); err != nil {
					t.Fatal(err)
				}
				expression := `events.join('|')`
				if frame {
					expression = `new Promise(resolve=>{const frame=document.createElement('iframe');frame.onload=()=>resolve(frame.contentWindow.events.join('|'));frame.src='/page';document.body.append(frame)})`
				}
				got, err := p.Evaluate(ctx, expression)
				if err != nil || got != "first:false:one|micro:first|second:true:two|DCL" {
					t.Fatalf("parser execution: %v %v", got, err)
				}
				if firstCount.Load() != 1 || secondCount.Load() != 1 {
					t.Fatalf("speculation duplicated requests: %d/%d", firstCount.Load(), secondCount.Load())
				}
			})
		})
	}
}

func TestParserLookaheadRespectsInertContentAndCSP(t *testing.T) {
	historyTestPages(t, func(t *testing.T, p *Page) {
		var forbidden atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/favicon.ico" {
				w.WriteHeader(404)
				return
			}
			if r.URL.Path != "/page" {
				forbidden.Add(1)
				fmt.Fprint(w, `throw Error('unexpected execution')`)
				return
			}
			w.Header().Set("Content-Security-Policy", "script-src 'nonce-allowed'")
			fmt.Fprint(w, `<!doctype html><template/><script nonce="allowed" src="/inert.js"></script></template><noscript/><script nonce="allowed" src="/noscript.js"></script></noscript><script src="/denied.js"></script><script nonce="allowed">globalThis.allowedExecuted=true</script>`)
		}))
		defer server.Close()
		if err := p.Navigate(context.Background(), server.URL+"/page"); err != nil {
			t.Fatal(err)
		}
		value, err := p.Evaluate(context.Background(), `allowedExecuted`)
		if err != nil || value != true {
			t.Fatalf("nonce script: %v %v", value, err)
		}
		// Favicon is a separate browser-owned request; only scripts count here.
		if forbidden.Load() != 0 {
			t.Fatalf("inert or CSP-denied script requested: %d", forbidden.Load())
		}
	})
}
