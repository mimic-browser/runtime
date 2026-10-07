package browser

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestCSSBackgroundResourcesUseAppliedDeclarations(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		var mu sync.Mutex
		counts := map[string]int{}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/css/main.css":
				w.Header().Set("Content-Type", "text/css")
				fmt.Fprint(w, `.box{background-image:url('../topo.svg')} .unused{background-image:url('../unused.svg')} #hidden{display:none}`)
			case "/topo.svg", "/unused.svg", "/hidden.svg", "/changed.svg":
				mu.Lock()
				counts[r.URL.Path]++
				mu.Unlock()
				w.Header().Set("Content-Type", "image/svg+xml")
				fmt.Fprint(w, `<svg xmlns="http://www.w3.org/2000/svg" width="1" height="1"/>`)
			default:
				fmt.Fprint(w, `<!doctype html><link rel="stylesheet" href="/css/main.css"><body><div id="box" class="box">background</div><section id="hidden"><div style="background-image:url('/hidden.svg')">hidden</div></section>`)
			}
		}))
		defer server.Close()
		if err := p.Navigate(context.Background(), server.URL); err != nil {
			t.Fatal(err)
		}
		historyEval(t, p, `new Promise(resolve=>setTimeout(()=>resolve(getComputedStyle(document.querySelector('#box')).backgroundImage.includes('/topo.svg')),30))`, true)
		mu.Lock()
		initial := counts["/topo.svg"]
		forbidden := counts["/unused.svg"] + counts["/hidden.svg"]
		mu.Unlock()
		if initial != 1 || forbidden != 0 {
			t.Fatalf("initial=%d forbidden=%d", initial, forbidden)
		}
		historyEval(t, p, `(()=>{document.querySelector('#box').style.backgroundImage='url(/changed.svg)';return true})()`, true)
		historyEval(t, p, `new Promise(resolve=>setTimeout(()=>resolve(true),30))`, true)
		deadline := time.Now().Add(time.Second)
		for {
			mu.Lock()
			changed := counts["/changed.svg"]
			mu.Unlock()
			if changed == 1 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("CSS mutation did not start its background resource")
			}
			time.Sleep(time.Millisecond)
		}
		historyEval(t, p, `document.querySelector('#box').setAttribute('data-unrelated','1');true`, true)
		historyEval(t, p, `new Promise(resolve=>setTimeout(()=>resolve(true),30))`, true)
		mu.Lock()
		defer mu.Unlock()
		if counts["/changed.svg"] != 1 || counts["/topo.svg"] != 1 {
			t.Fatalf("unchanged style refetched images: %v", counts)
		}
	})
}
