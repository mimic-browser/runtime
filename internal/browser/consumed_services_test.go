package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"
	"time"
)

func TestConsumedServicesChromeOracle(t *testing.T) {
	serialBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		source, err := os.ReadFile("testdata/consumed_services_oracle.js")
		if err != nil {
			t.Fatal(err)
		}
		expected, err := os.ReadFile("testdata/consumed_services_chrome152.json")
		if err != nil {
			t.Fatal(err)
		}
		actual, err := p.Evaluate(ctx, string(source))
		if err != nil {
			t.Fatal(err)
		}
		var want, got any
		if err := json.Unmarshal(expected, &want); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(fmt.Sprint(actual)), &got); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %s; want %s", actual, expected)
		}
	})
}

func TestNavigatorWebDriverInvariant(t *testing.T) {
	serialBrowserTest(t)
	if navigatorWebDriver {
		t.Fatal("WebDriver must never be enabled by host configuration")
	}
	historyTestPages(t, func(t *testing.T, p *Page) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		value, err := p.Evaluate(ctx, `(()=>{const f=document.createElement('iframe');document.body.append(f);const d=Object.getOwnPropertyDescriptor(Navigator.prototype,'webdriver');return navigator.webdriver===false&&f.contentWindow.navigator.webdriver===false&&d.get.call(navigator)===false})()`)
		if err != nil || value != true {
			t.Fatalf("webdriver invariant: %v, %v", value, err)
		}
	})
}

func TestCrashReportDiagnosticsOwnBuffer(t *testing.T) {
	serialBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, err := p.Evaluate(ctx, `(async()=>{await crashReport.initialize(32);crashReport.set('phase','ready');try{crashReport.set('phase','x'.repeat(64))}catch(e){if(e.name!=='NotAllowedError')throw e}})()`)
		if err != nil {
			t.Fatal(err)
		}
		reports := p.CrashReports()
		if reports[p.Top.ID] != `{"phase":"ready"}` {
			t.Fatalf("diagnostic buffer: %v", reports)
		}
		reports[p.Top.ID] = "modified"
		if p.CrashReports()[p.Top.ID] != `{"phase":"ready"}` {
			t.Fatal("diagnostics exposed mutable storage")
		}
	})
}

// Runtime snapshots must restore wrappers only. The host's requested bit,
// capacity, initialization and annotations belong to each document Realm.
func TestCrashReportHostStateAndSnapshotIsolation(t *testing.T) {
	serialBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, err := p.Evaluate(ctx, `(async()=>{
   await crashReport.initialize(64);crashReport.set('z','é');crashReport.set('10','ten');crashReport.set('2','two');crashReport.set('z','<');
   const f=document.createElement('iframe');document.body.append(f);await f.contentWindow.crashReport.initialize(2);
   try{f.contentWindow.crashReport.set('a','b');throw new Error('capacity ignored')}catch(e){if(e.name!=='NotAllowedError')throw e}
  })()`)
		if err != nil {
			t.Fatal(err)
		}
		owner := &p.Top.Realm.crashReport
		owner.mu.RLock()
		requested, initialized, capacity := owner.requested, owner.initialized, owner.capacity
		owner.mu.RUnlock()
		if !requested || !initialized || capacity != 64 {
			t.Fatal("host does not own initialization")
		}
		want := `{"2":"two","10":"ten","z":"<"}`
		if p.CrashReports()[p.Top.ID] != want {
			t.Fatalf("annotation encoding/order: %v", p.CrashReports())
		}
		for _, f := range p.Top.children {
			if p.CrashReports()[f.ID] != `{}` {
				t.Fatalf("child buffer: %v", p.CrashReports())
			}
		}
	})
}

func TestTaskSignalHostPriorityAndReentrancy(t *testing.T) {
	serialBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		value, err := p.Evaluate(ctx, `(async()=>{
   const c=new TaskController({priority:'background'}),observed=[];
   c.signal.onprioritychange=e=>{observed.push(e.previousPriority+':'+c.signal.priority);try{c.setPriority('user-visible')}catch(e){observed.push(e.name)}};
   const jobs=[scheduler.postTask(()=>observed.push('dynamic'),{signal:c.signal}),scheduler.postTask(()=>observed.push('fixed'),{priority:'user-visible',signal:c.signal})];
   c.setPriority('user-blocking');await Promise.all(jobs);
   return JSON.stringify(observed);
  })()`)
		want := `["background:user-blocking","NotAllowedError","dynamic","fixed"]`
		if err != nil || value != want {
			t.Fatalf("task signal: %v %v", value, err)
		}
	})
}

func TestLaunchQueueRetainsAndDeliversURLLaunches(t *testing.T) {
	serialBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := p.QueueLaunch("https://example.test/first"); err != nil {
			t.Fatal(err)
		}
		if err := p.QueueLaunch("https://example.test/second"); err != nil {
			t.Fatal(err)
		}
		value, err := p.Evaluate(ctx, `new Promise(resolve=>{const out=[];launchQueue.setConsumer(p=>{out.push([p.targetURL,p.files.length,Object.isFrozen(p.files),p instanceof LaunchParams]);if(out.length===2)resolve(JSON.stringify(out))})})`)
		want := `[["https://example.test/first",0,true,true],["https://example.test/second",0,true,true]]`
		if err != nil || value != want {
			t.Fatalf("launch delivery: %v, %v", value, err)
		}
	})
}

func TestConsumedCacheChromeOracle(t *testing.T) {
	serialBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("<!doctype html><body>")) }))
		defer server.Close()
		if err := p.Navigate(ctx, server.URL); err != nil {
			t.Fatal(err)
		}

		source, err := os.ReadFile("testdata/consumed_cache_oracle.js")
		if err != nil {
			t.Fatal(err)
		}
		expected, err := os.ReadFile("testdata/consumed_cache_chrome152.json")
		if err != nil {
			t.Fatal(err)
		}
		actual, err := p.Evaluate(ctx, string(source))
		if err != nil {
			t.Fatal(err)
		}
		var want, got any
		if err := json.Unmarshal(expected, &want); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(fmt.Sprint(actual)), &got); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %s; want %s", actual, expected)
		}
	})
}

func TestCacheStorageUsageEstimateTracksCanonicalOriginState(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("<!doctype html><body>")) }))
		defer first.Close()
		second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("<!doctype html><body>")) }))
		defer second.Close()
		if err := p.Navigate(ctx, first.URL); err != nil {
			t.Fatal(err)
		}
		value, err := p.Evaluate(ctx, `(async()=>{
			await caches.delete('quota-accounting');
			const before=await navigator.storage.estimate(), cache=await caches.open('quota-accounting');
			const rows=[];
			for(const [url,response,init] of [
				['https://t.l/0',new Response(null,{status:204})],
				['https://t.l/null200',new Response(null)],
				['https://t.l/empty',new Response('')],
				['https://t.l/a',new Response('a')],
				['https://t.l/status',new Response(null,{statusText:'Hello'})],
				['https://t.l/h1',new Response(null,{headers:{x:'y'}})],
				['https://t.l/request',new Response(null),{headers:{a:'b'}}],
				['https://t.l/100',new Response('x'.repeat(100))],
				['https://t.l/1000',new Response('x'.repeat(1000))],
				['https://t.l/headers',new Response('abc',{headers:{'x-probe':'q'.repeat(37)}})],
			]){
				await cache.put(init?new Request(url,init):url,response);
				const estimate=await navigator.storage.estimate();
				rows.push([estimate.usage,estimate.quota-before.quota,estimate.usageDetails.caches||0]);
			}
			return JSON.stringify({before:[before.usage,before.usageDetails.caches||0],rows});
		})()`)
		const want = `{"before":[0,0],"rows":[[72,72,72],[150,150,150],[294,294,294],[435,435,435],[517,517,517],[598,598,598],[684,684,684],[926,926,926],[2069,2069,2069],[2269,2269,2269]]}`
		if err != nil || value != want {
			t.Fatalf("cache quota accounting: %v, %v", value, err)
		}
		if err := p.Navigate(ctx, second.URL); err != nil {
			t.Fatal(err)
		}
		value, err = p.Evaluate(ctx, `navigator.storage.estimate().then(value=>[value.usage,value.usageDetails.caches||0].join(','))`)
		if err != nil || value != "0,0" {
			t.Fatalf("cross-origin cache quota isolation: %v, %v", value, err)
		}
		if err := p.Navigate(ctx, first.URL); err != nil {
			t.Fatal(err)
		}
		value, err = p.Evaluate(ctx, `(async()=>{const retained=await navigator.storage.estimate();await caches.delete('quota-accounting');const removed=await navigator.storage.estimate();return [retained.usage,removed.usage,removed.usageDetails.caches||0].join(',')})()`)
		if err != nil || value != "2269,0,0" {
			t.Fatalf("cache quota deletion: %v, %v", value, err)
		}
	})
}

func TestConsumedCookieChromeOracle(t *testing.T) {
	serialBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("<!doctype html><body>")) }))
		defer server.Close()
		if err := p.Navigate(ctx, server.URL); err != nil {
			t.Fatal(err)
		}

		source, err := os.ReadFile("testdata/consumed_cookie_oracle.js")
		if err != nil {
			t.Fatal(err)
		}
		expected, err := os.ReadFile("testdata/consumed_cookie_chrome152.json")
		if err != nil {
			t.Fatal(err)
		}
		actual, err := p.Evaluate(ctx, string(source))
		if err != nil {
			t.Fatal(err)
		}
		var want, got any
		if err := json.Unmarshal(expected, &want); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(fmt.Sprint(actual)), &got); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %s; want %s", actual, expected)
		}
	})
}
