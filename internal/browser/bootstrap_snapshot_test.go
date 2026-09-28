//go:build (windows || linux) && amd64

package browser

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	chrome152 "github.com/moreveal/mimic/chrome/152"
	v8engine "github.com/moreveal/mimic/internal/engine/v8"
)

func bootstrapSnapshotPage(t *testing.T) *Page {
	t.Helper()
	b, err := New(v8engine.Factory{}, chrome152.New())
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

func bootstrapSnapshotEvaluate(t *testing.T, p *Page, source string) any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	value, err := p.Evaluate(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func bootstrapSnapshotWarm(t *testing.T, p *Page) {
	t.Helper()
	bootstrapSnapshotEvaluate(t, p, "true")
	if os.Getenv("MIMIC_DISABLE_BOOTSTRAP_SNAPSHOT") == "1" {
		return
	}
	// A second observed realm starts the asynchronous build. Initial iframe
	// documents may defer their runtime, so exercise JavaScript before removing
	// the frame and waiting outside the JS turn.
	bootstrapSnapshotEvaluate(t, p, `(()=>{const f=document.createElement('iframe');document.body.appendChild(f);f.contentWindow.eval('true');f.remove();return true})()`)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := p.ctx.browser.bootstrapSnapshots.wait(ctx); err != nil {
		t.Fatal(err)
	}
}

func bootstrapSnapshotAssertRestored(t *testing.T, p *Page, minimum int) {
	t.Helper()
	if os.Getenv("MIMIC_DISABLE_BOOTSTRAP_SNAPSHOT") == "1" {
		return
	}
	count := 0
	for _, f := range p.Top.Realm.childFrames {
		if f.Realm != nil && f.Realm.bootstrapRestored {
			count++
		}
	}
	for _, f := range p.Top.Realm.retainedFrames {
		if f.Realm != nil && f.Realm.bootstrapRestored {
			count++
		}
	}
	if count < minimum {
		t.Fatalf("expected at least %d restored iframe contexts, got %d", minimum, count)
	}
}

// Mutate the first restored realm before constructing the next. Stale seed
// wrappers, tokens, or API slots fail through ordinary DOM and Symbol access.
func TestBootstrapSnapshotFramesHaveIndependentState(t *testing.T) {
	serialBrowserTest(t)
	p := bootstrapSnapshotPage(t)
	bootstrapSnapshotWarm(t, p)
	source, err := os.ReadFile("testdata/bootstrap_snapshot_oracle.js")
	if err != nil {
		t.Fatal(err)
	}
	result := bootstrapSnapshotEvaluate(t, p, string(source)).(map[string]any)
	for _, key := range []string{"pristine", "symbols", "realmIdentity", "parentState", "detached"} {
		if result[key] != true {
			t.Errorf("%s: %#v", key, result)
		}
	}
	for _, name := range []string{"first", "second"} {
		for key, value := range result[name].(map[string]any) {
			if value != true {
				t.Errorf("%s.%s: %v", name, key, value)
			}
		}
	}
	bootstrapSnapshotAssertRestored(t, p, 2)
}

func TestBootstrapSnapshotCallbacksAndJobsUseRestoredRealm(t *testing.T) {
	serialBrowserTest(t)
	p := bootstrapSnapshotPage(t)
	bootstrapSnapshotWarm(t, p)
	value := bootstrapSnapshotEvaluate(t, p, `(async()=>{
 globalThis.snapshotEvents=[];const frames=[];
 globalThis.snapshotReenter=label=>{snapshotEvents.push(label+' parent');return frames[label==='a'?0:1].contentWindow.eval('6*7')};
 for(const label of ['a','b']){const f=document.createElement('iframe');document.body.appendChild(f);frames.push(f);
 const result=f.contentWindow.eval("(()=>{const label="+JSON.stringify(label)+";let illegal=false;try{Document.prototype.createElement.call({},'div')}catch(e){illegal=e instanceof TypeError}Promise.resolve().then(()=>parent.snapshotEvents.push(label+' job'));return illegal&&parent.snapshotReenter(label)===42})()");
 if(!result)return false;snapshotEvents.push(label+' returned')}
 const sync=snapshotEvents.join(',');await new Promise(resolve=>setTimeout(resolve,0));
 return sync==='a parent,a returned,b parent,b returned'&&snapshotEvents.join(',')==='a parent,a returned,b parent,b returned,a job,b job';
})()`)
	if value != true {
		t.Fatalf("restored callback/job order: %v", value)
	}
	if p.crossRealmDepth != 0 {
		t.Fatalf("entry depth leaked: %d", p.crossRealmDepth)
	}
	bootstrapSnapshotAssertRestored(t, p, 2)
}

func TestBootstrapSnapshotNavigationRebindsDocumentAndOrigin(t *testing.T) {
	serialBrowserTest(t)
	p := bootstrapSnapshotPage(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "<!doctype html><body>"+r.URL.Path) }))
	defer server.Close()
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "<!doctype html><body>foreign") }))
	defer foreign.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := p.Navigate(ctx, server.URL+"/parent"); err != nil {
		t.Fatal(err)
	}
	bootstrapSnapshotWarm(t, p)
	value := bootstrapSnapshotEvaluate(t, p, `(async()=>{
 const make=()=>{const f=document.createElement('iframe');document.body.appendChild(f);return f};const first=make(),second=make(),saved=first.contentWindow;
 first.contentWindow.eval("globalThis.oldSnapshotMarker=1;document.body.textContent='old'");second.contentWindow.eval("document.body.textContent='sibling'");
 const load=url=>new Promise(resolve=>{first.onload=resolve;first.src=url});
 await load(`+strconv.Quote(foreign.URL)+`);let blocked=false;try{void saved.document}catch(e){blocked=String(e).includes('SecurityError')}
 if(!blocked||saved!==first.contentWindow||second.contentWindow.document.body.textContent!=='sibling')return false;
 await load(`+strconv.Quote(server.URL+"/replacement")+`);
 return saved===first.contentWindow&&saved.eval("typeof oldSnapshotMarker==='undefined'&&document.body.textContent==='/replacement'&&document.documentElement.ownerDocument===document&&parent===top&&isSecureContext")&&second.contentWindow.document.body.textContent==='sibling'&&document.body.textContent==='/parent';
})()`)
	if value != true {
		t.Fatalf("snapshot navigation state: %v", value)
	}
	bootstrapSnapshotAssertRestored(t, p, 2)
}

// All Pages share the immutable seed, but callbacks, wrappers and pending jobs
// must remain owned by the restored Page throughout concurrent teardown.
func TestBootstrapSnapshotConcurrentPageTeardown(t *testing.T) {
	serialBrowserTest(t)
	p := bootstrapSnapshotPage(t)
	bootstrapSnapshotWarm(t, p)
	const count = 10
	for wave := range 3 {
		t.Logf("concurrent restoration wave %d", wave+1)
		pages := make([]*Page, count)
		for i := range pages {
			var err error
			pages[i], err = p.ctx.NewPage()
			if err != nil {
				t.Fatal(err)
			}
		}
		errors := make(chan error, count)
		for i, page := range pages {
			go func(i int, page *Page) {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				value, err := page.Evaluate(ctx, fmt.Sprintf(`(async()=>{
 globalThis.pageMarker=%d;
 const f=document.createElement('iframe');document.body.appendChild(f);
 const result=f.contentWindow.eval("document.body.textContent=String(parent.pageMarker);setTimeout(()=>document.body.appendChild(document.createElement('div')),60000);document.body.textContent");
 await Promise.resolve();f.remove();return result===String(pageMarker);
})()`, wave*count+i))
				if err == nil && value != true {
					err = fmt.Errorf("Page %d observed another restore's state: %v", i, value)
				}
				if err == nil && os.Getenv("MIMIC_DISABLE_BOOTSTRAP_SNAPSHOT") != "1" && !page.Top.Realm.bootstrapRestored {
					err = fmt.Errorf("Page %d did not restore its main realm", i)
				}
				closeErr := page.Close()
				if err == nil {
					err = closeErr
				}
				errors <- err
			}(i, page)
		}
		for range count {
			if err := <-errors; err != nil {
				t.Error(err)
			}
		}
	}
	if bootstrapSnapshotEvaluate(t, p, "typeof pageMarker==='undefined'&&document.body.children.length===0") != true {
		t.Fatal("concurrent Pages mutated the seed Page")
	}
}

// Unlike the warmed teardown test, the first wave races ordinary bootstrap,
// seed capture and asynchronous admission while unrelated origins are active.
func TestBootstrapSnapshotConcurrentColdAdmission(t *testing.T) {
	serialBrowserTest(t)
	seed := bootstrapSnapshotPage(t)
	serve := func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "<!doctype html><body><p id='path'>%s</p>", r.URL.Path)
	}
	one := httptest.NewServer(http.HandlerFunc(serve))
	defer one.Close()
	two := httptest.NewServer(http.HandlerFunc(serve))
	defer two.Close()
	type outcome struct {
		err      error
		restored bool
	}
	restoredCount := 0
	for wave := range 3 {
		results := make(chan outcome, 10)
		for index := range 10 {
			page, err := seed.ctx.NewPage()
			if err != nil {
				t.Fatal(err)
			}
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				address := one.URL
				if index%2 != 0 {
					address = two.URL
				}
				path := fmt.Sprintf("/wave-%d/page-%d", wave, index)
				err := page.Navigate(ctx, address+path)
				var result any
				if err == nil {
					result, err = page.Evaluate(ctx, `(()=>{const path=document.getElementById('path');const f=document.createElement('iframe');document.body.appendChild(f);const child=f.contentWindow;const correct=path.ownerDocument===document&&document.querySelector('#path')===path&&child.eval('document===window.document&&document.defaultView===window&&parent===top&&parent!==window')&&child.Document!==Document;f.remove();return correct&&path.textContent===location.pathname})()`)
					if err == nil && result != true {
						err = fmt.Errorf("wave %d Page %d identity: %v", wave, index, result)
					}
				}
				restored := page.Top.Realm != nil && page.Top.Realm.bootstrapRestored
				closeErr := page.Close()
				if err == nil {
					err = closeErr
				}
				results <- outcome{err, restored}
			}()
		}
		for range 10 {
			result := <-results
			if result.err != nil {
				t.Error(result.err)
			}
			if result.restored {
				restoredCount++
			}
		}
		// Join any build only after every Page in this wave has already closed.
		// The next wave must safely reuse an artifact built from retired Pages.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err := seed.ctx.browser.bootstrapSnapshots.wait(ctx)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
	}
	if os.Getenv("MIMIC_DISABLE_BOOTSTRAP_SNAPSHOT") != "1" && restoredCount == 0 {
		t.Fatal("cold admission never produced a restored consumer")
	}
}

func TestBootstrapSnapshotExceptionStacksMatchOrdinary(t *testing.T) {
	serialBrowserTest(t)
	p := bootstrapSnapshotPage(t)
	const source = `JSON.stringify([()=>new Document(),()=>Document.prototype.createElement.call({},'div')].map(fn=>{try{fn();return null}catch(e){return e.stack}}))`
	normal := bootstrapSnapshotEvaluate(t, p, source)
	bootstrapSnapshotWarm(t, p)
	restored, err := p.ctx.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	actual := bootstrapSnapshotEvaluate(t, restored, source)
	if os.Getenv("MIMIC_DISABLE_BOOTSTRAP_SNAPSHOT") != "1" && !restored.Top.Realm.bootstrapRestored {
		t.Fatal("candidate did not restore")
	}
	if normal != actual {
		t.Fatalf("exception stack mismatch\nordinary %s\nrestored %s", normal, actual)
	}
}
