package browser

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/moreveal/mimic/internal/network"
)

// These are causal clock/ownership relations, not exact wall-clock durations.
// They hold in frozen Chrome for both fresh and cached resource retrievals.
func TestResourceTimingUsesInitiatingRealmClock(t *testing.T) {
	serialBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/resource" {
				time.Sleep(25 * time.Millisecond)
				w.Header().Set("Cache-Control", "max-age=600")
			} else {
				w.Header().Set("Cache-Control", "no-store")
			}
			fmt.Fprint(w, "<!doctype html><body></body>")
		}))
		defer server.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for cycle := 0; cycle < 2; cycle++ {
			if err := p.Navigate(ctx, server.URL); err != nil {
				t.Fatal(err)
			}
			if err := p.AdvanceTime(ctx, 10*time.Second); err != nil {
				t.Fatal(err)
			}
			historyEval(t, p, `(async()=>{
const before=performance.now();await(await fetch('/resource')).text();
const now=performance.now(),entries=performance.getEntriesByType('resource').filter(e=>e.name.endsWith('/resource'));
if(entries.length!==1)throw new Error('resource count: '+entries.length);
const e=entries[0],nav=performance.getEntriesByType('navigation')[0];
if(!(e.startTime>=before-1&&e.startTime<=e.responseEnd&&e.responseEnd<=now+1))throw new Error(JSON.stringify({before,now,entry:e.toJSON()}));
return e.navigationId===nav.navigationId&&nav.toJSON().navigationId===nav.navigationId&&e.secureConnectionStart===0;
})()`, true)
			if cycle == 1 {
				historyEval(t, p, `(()=>{const e=performance.getEntriesByType('resource').find(e=>e.name.endsWith('/resource'));return e.deliveryType==='cache'&&e.nextHopProtocol===''&&e.transferSize===0&&e.domainLookupStart===e.fetchStart&&e.connectEnd===e.fetchStart})()`, true)
			}
		}
	})
}

type resourceClockResponse struct{ script string }

func (fixture resourceClockResponse) Before(_ context.Context, request network.Request) (network.Decision, error) {
	if request.URL.Path != "/resource" {
		return network.Decision{}, nil
	}
	body, contentType := "resource", "text/plain"
	if request.Initiator == network.Script {
		body, contentType = fixture.script, "text/javascript"
	}
	return network.Decision{Response: &network.Response{
		Status: http.StatusOK, Headers: http.Header{"Content-Type": {contentType}},
		URL: request.URL, Body: []byte(body), Duration: 2 * time.Second,
		BrowserVisibleTiming: network.TransportTimingSnapshot{
			Phases: map[string]float64{"firstResponseByte": 4000, "responseComplete": 5000},
		},
	}}, nil
}

func TestParserResourceCompletionPrecedesScriptExecution(t *testing.T) {
	serialBrowserTest(t)
	for _, kind := range []string{"classic", "module"} {
		t.Run(kind, func(t *testing.T) {
			run := func(t *testing.T, p *Page) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					fmt.Fprintf(w, `<!doctype html><script type="%s" src="/resource"></script>`, map[string]string{"classic": "text/javascript", "module": "module"}[kind])
				}))
				defer server.Close()
				p.Loader().Use(resourceClockResponse{script: `globalThis.resourceClockAtScript=(()=>{
const now=performance.now(),e=performance.getEntriesByType('resource').find(e=>e.name.endsWith('/resource'));
return e&&e.responseEnd<=now+1&&e.duration>=4999;
})()`})
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				if err := p.Navigate(ctx, server.URL); err != nil {
					t.Fatal(err)
				}
				historyEval(t, p, `resourceClockAtScript`, true)
			}
			if kind == "module" {
				// SourceTextModule graphs belong to the V8 engine contract.
				run(t, newAsyncModulePage(t))
			} else {
				historyTestPages(t, run)
			}
		})
	}
}

func (resourceClockResponse) After(_ context.Context, _ network.Request, response network.Response) (network.Response, error) {
	return response, nil
}

// The same recorded completion applies to document fetch, XHR and a worker's
// own timeline. Network scaling and a buffered/synthetic response must not
// leave its handlers earlier than the resource that made them ready.
func TestResourceCompletionClockUsesRecordedTiming(t *testing.T) {
	serialBrowserTest(t)
	for _, mechanism := range []string{"fetch", "xhr", "worker"} {
		t.Run(mechanism, func(t *testing.T) {
			historyTestPages(t, func(t *testing.T, p *Page) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					fmt.Fprint(w, "<!doctype html><body></body>")
				}))
				defer server.Close()
				p.Loader().Use(resourceClockResponse{})
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				if err := p.Navigate(ctx, server.URL); err != nil {
					t.Fatal(err)
				}
				p.mu.Lock()
				p.env.Time.NetworkScale = 2
				p.mu.Unlock()
				other, err := p.ctx.NewPage()
				if err != nil {
					t.Fatal(err)
				}
				defer other.Close()
				otherNow := other.ClockNow()
				read := `const now=performance.now(),entry=performance.getEntriesByType('resource').find(e=>e.name.endsWith('/resource'));
if(!entry||entry.responseEnd>now+1||entry.duration<9999)throw Error(JSON.stringify({now,entry:entry&&entry.toJSON()}));
return true;`
				source := `(async()=>{await(await fetch('/resource')).text();` + read + `})()`
				if mechanism == "xhr" {
					source = `new Promise((resolve,reject)=>{const x=new XMLHttpRequest();x.onload=()=>{try{resolve((()=>{` + read + `})())}catch(e){reject(e)}};x.onerror=()=>reject(Error('XHR failed'));x.open('GET','/resource');x.send()})`
				} else if mechanism == "worker" {
					worker := `onmessage=async()=>{try{await(await fetch(` + strconv.Quote(server.URL+"/resource") + `)).text();postMessage((()=>{` + read + `})())}catch(e){postMessage(String(e))}}`
					source = `new Promise((resolve,reject)=>{const url=URL.createObjectURL(new Blob([` + strconv.Quote(worker) + `]));const w=new Worker(url);w.onerror=e=>reject(Error(e.message));w.onmessage=e=>{w.terminate();URL.revokeObjectURL(url);resolve(e.data)};w.postMessage('start')})`
				}
				historyEval(t, p, source, true)
				if other.ClockNow() != otherNow {
					t.Fatal("resource completion advanced an independent Page")
				}
				if mechanism == "worker" {
					historyEval(t, p, `performance.getEntriesByType('resource').filter(e=>e.name.endsWith('/resource')).length===0`, true)
				}
			})
		})
	}
}

// A transport can finish while the embedding thread is descheduled between
// event-loop turns. Completion must advance the same clock that the settled
// Promise observes, even when none of that wait occurred inside WaitAny.
func TestResourceCompletionClockIncludesBetweenTurnWait(t *testing.T) {
	serialBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		completed := make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/resource" {
				time.Sleep(40 * time.Millisecond)
				defer close(completed)
			}
			fmt.Fprint(w, "<!doctype html><body></body>")
		}))
		defer server.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := p.Navigate(ctx, server.URL); err != nil {
			t.Fatal(err)
		}
		historyEval(t, p, `globalThis.pendingResource=fetch('/resource');true`, true)
		select {
		case <-completed:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		historyEval(t, p, `(async()=>{
await(await pendingResource).text();
const now=performance.now(),e=performance.getEntriesByType('resource').find(e=>e.name.endsWith('/resource'));
if(!e||e.responseEnd>now+1)throw Error(JSON.stringify({now,entry:e&&e.toJSON()}));
return true;
})()`, true)
	})
}
