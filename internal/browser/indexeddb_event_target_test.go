package browser

import (
	"context"
	"fmt"
	"github.com/moreveal/mimic/internal/engine"
	"github.com/moreveal/mimic/internal/scheduler"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIndexedDBRequestEventTargetFromDocumentScript(t *testing.T) {
	parallelBrowserTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!doctype html><body><div id="out">PENDING</div><script>
 window.errors=[];window.onerror=(message)=>errors.push(message);
 var rq=indexedDB.open('script-target',1);
 rq.onupgradeneeded=function(e){e.target.result.createObjectStore('s',{keyPath:'id'});};
 rq.onerror=function(){document.querySelector('#out').textContent='ERR:'+rq.error;};
 rq.onsuccess=function(e){var db=e.target.result;var tx=db.transaction('s','readwrite');tx.objectStore('s').put({id:1,name:'kept'});tx.oncomplete=function(){var g=db.transaction('s').objectStore('s').get(1);g.onsuccess=function(){document.querySelector('#out').textContent=g.result.name;};};};
 </script>`)
	}))
	defer server.Close()
	historyTestPages(t, func(t *testing.T, p *Page) {
		if err := p.Navigate(context.Background(), server.URL); err != nil {
			t.Fatal(err)
		}
		historyEval(t, p, `new Promise(resolve=>setTimeout(()=>resolve(document.querySelector('#out').textContent+'|'+errors.join(',')),100))`, `kept|`)
	})
}

func TestIndexedDBUpgradeUsesInnermostPageTask(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		navigateCapabilityFixture(t, p)
		ctx := context.Background()
		world, err := p.IsolatedWorld(ctx, p.Top.ID, "nested-idb")
		if err != nil {
			t.Fatal(err)
		}
		d := NewDebugger(p)
		defer d.Close()
		if _, err := d.Evaluate(ctx, p.Top.ID, world, `globalThis.upgradeResult='pending'`, DebuggerOptions{}); err != nil {
			t.Fatal(err)
		}
		inner := p.realmOwners[world]
		if inner == nil {
			t.Fatal("isolated realm missing")
		}
		p.Top.Realm.scheduler.Post(scheduler.Control, 0, func(ctx context.Context) error {
			inner.scheduler.Post(scheduler.Control, 0, func(ctx context.Context) error {
				value, err := inner.runtime.Eval(ctx, `(()=>{const request=indexedDB.open('nested-task-upgrade',1);request.onupgradeneeded=()=>{try{for(let i=0;i<64;i++)request.result.createObjectStore('store'+i).createIndex('byName','name');upgradeResult='ok';}catch(error){upgradeResult=error.name;}};request.onerror=()=>upgradeResult=String(request.error);})()`, "nested-upgrade")
				if value != nil {
					if owner, ok := inner.runtime.(engine.ValueReleaser); ok {
						owner.ReleaseValue(value)
					}
				}
				return err
			})
			return inner.scheduler.RunUntilIdle(ctx, 256)
		})
		if err := p.Top.Realm.scheduler.RunUntilIdle(ctx, 256); err != nil {
			t.Fatal(err)
		}
		result, err := d.Evaluate(ctx, p.Top.ID, world, `upgradeResult`, DebuggerOptions{ReturnByValue: true})
		if err != nil || result["exceptionDetails"] != nil || result["result"].(map[string]any)["value"] != "ok" {
			t.Fatalf("nested upgrade: %#v, %v", result, err)
		}
	})
}

func TestIndexedDBRequestEventTarget(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		navigateCapabilityFixture(t, p)
		historyEval(t, p, `new Promise(resolve=>{
   const r=indexedDB.open('request-target',1);const seen=[];
   r.onupgradeneeded=e=>{
    try {seen.push(e.target===r,e.currentTarget===r,e.target.result===r.result);e.target.result.createObjectStore('s',{keyPath:'id'});}
    catch(error){resolve(error.name+':'+error.message)}
   };
   r.onerror=()=>resolve('open:'+r.error);
   r.onsuccess=e=>{
    try {
     const db=e.target.result,tx=db.transaction('s','readwrite');
     tx.objectStore('s').put({id:1,name:'kept'});
     tx.oncomplete=()=>{
      const g=db.transaction('s').objectStore('s').get(1);
      g.onsuccess=event=>resolve(seen.join(',')+'|'+event.target.result.name);
     };
    }catch(error){resolve(error.name+':'+error.message)}
   };
  })`, `true,true,true|kept`)
	})
}
