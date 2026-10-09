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

func TestCSSBackgroundAdmissionIncludesNewAdoptedSheets(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		var images atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/adopted.svg" {
				images.Add(1)
				w.Header().Set("Content-Type", "image/svg+xml")
				fmt.Fprint(w, `<svg xmlns="http://www.w3.org/2000/svg" width="1" height="1"/>`)
			} else {
				fmt.Fprint(w, `<!doctype html><body><div>Initially unstyled</div>`)
			}
		}))
		defer server.Close()
		if err := p.Navigate(context.Background(), server.URL); err != nil {
			t.Fatal(err)
		}
		historyEval(t, p, `(() => {
 const sheet = new CSSStyleSheet();
 sheet.replaceSync('div { background-image: url(/adopted.svg) }');
 document.adoptedStyleSheets = [sheet];
 return true;
})()`, true)
		historyEval(t, p, `new Promise(resolve => setTimeout(() => resolve(true), 30))`, true)
		deadline := time.Now().Add(time.Second)
		for images.Load() == 0 && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if images.Load() != 1 {
			t.Fatalf("adopted background requests: %d", images.Load())
		}
	})
}

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

// Both native computed styles and their semantic fallback must observe
// visibility transitions and form pseudo classes without fetching hidden URLs.
func TestCSSBackgroundResourcesTrackHiddenBranchesAndFormState(t *testing.T) {
	for _, fallback := range []bool{false, true} {
		t.Run(fmt.Sprintf("fallback=%t", fallback), func(t *testing.T) {
			historyTestPages(t, func(t *testing.T, p *Page) {
				var mu sync.Mutex
				counts := map[string]int{}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/" {
						fmt.Fprint(w, `<!doctype html><style>
#hidden{display:none} #hidden div{display:block;background-image:url(/hidden.svg)}
#probe:focus{background-image:url(/focus.svg)}
body.typed #probe{background-image:url(/typed.svg)}
#invisible{visibility:hidden;background-image:url(/invisible.svg)}
</style><body><input id="probe" required><div id="invisible"></div><section id="hidden"><div></div></section>`)
					} else {
						mu.Lock()
						counts[r.URL.Path]++
						mu.Unlock()
						w.Header().Set("Content-Type", "image/svg+xml")
						fmt.Fprint(w, `<svg xmlns="http://www.w3.org/2000/svg" width="1" height="1"/>`)
					}
				}))
				defer server.Close()
				if fallback {
					reduced := true
					p.SetMediaPreferences("", &reduced)
				}
				if err := p.Navigate(context.Background(), server.URL); err != nil {
					t.Fatal(err)
				}
				wait := func(path string) {
					t.Helper()
					deadline := time.Now().Add(2 * time.Second)
					for time.Now().Before(deadline) {
						mu.Lock()
						count := counts[path]
						mu.Unlock()
						if count == 1 {
							return
						}
						historyEval(t, p, `new Promise(resolve=>setTimeout(()=>resolve(true),10))`, true)
					}
					t.Fatalf("resource not fetched: %s", path)
				}
				wait("/invisible.svg")
				mu.Lock()
				hidden := counts["/hidden.svg"]
				mu.Unlock()
				if hidden != 0 {
					t.Fatal("hidden descendant fetched background")
				}
				historyEval(t, p, `document.querySelector('#probe').addEventListener('input',()=>document.body.classList.add('typed'));document.querySelector('#probe').focus();true`, true)
				wait("/focus.svg")
				if err := p.DispatchProtocolInput(context.Background(), "Input.insertText", map[string]any{"text": "a"}); err != nil {
					t.Fatal(err)
				}
				wait("/typed.svg")
				historyEval(t, p, `document.querySelector('#hidden').style.display='block';true`, true)
				wait("/hidden.svg")
				historyEval(t, p, `document.querySelector('#hidden').remove();document.querySelector('#probe').value='';true`, true)
				if fallback {
					// A negative resource admission belongs only to its exact epoch.
					// Changing media must restore canonical native observations.
					reduced := false
					p.SetMediaPreferences("", &reduced)
					historyEval(t, p, `document.querySelector('#invisible').style.width='40px';document.querySelector('#invisible').getBoundingClientRect().width===40`, true)
					state := p.Top.Realm.blitz
					if state == nil || state.fallback != "" || state.document.Owner == nil {
						t.Fatal("resource fallback survived a new native admission epoch")
					}
				}
				mu.Lock()
				defer mu.Unlock()
				for path, count := range counts {
					if count != 1 {
						t.Fatalf("duplicate resource %s: %d", path, count)
					}
				}
			})
		})
	}
}
