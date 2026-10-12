//go:build (windows || linux) && amd64

package browser

import (
	_ "embed"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestNativeFunctionSourcesMatchFrozenChrome(t *testing.T) {
	parallelBrowserTest(t)
	documentAllOracle(t, "native_function")
}

//go:embed testdata/platform_callable_entry.js
var platformCallableEntryProbe string

func TestPlatformCallableReceiverDiagnostics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "<!doctype html>") }))
	defer server.Close()
	page, ctx := newXHRTestPage(t, server.URL)
	value, err := page.Evaluate(ctx, platformCallableEntryProbe)
	if err != nil || value != "ok" {
		t.Fatalf("receiver diagnostics: %v %v", value, err)
	}
	expression := strings.TrimSuffix(strings.TrimSpace(platformCallableEntryProbe), ";")
	workerSource := "postMessage(" + expression + ")"
	value, err = page.Evaluate(ctx, `new Promise((resolve,reject)=>{const u=URL.createObjectURL(new Blob([`+strconv.Quote(workerSource)+`]));const w=new Worker(u);w.onmessage=e=>{w.terminate();URL.revokeObjectURL(u);resolve(e.data)};w.onerror=e=>reject(Error(e.message))})`)
	if err != nil || value != "ok" {
		t.Fatalf("worker receiver diagnostics: %v %v", value, err)
	}
}

func TestDOMOperationReceiverBrandsMatchFrozenChrome(t *testing.T) {
	parallelBrowserTest(t)
	p := bootstrapSnapshotPage(t)
	value := bootstrapSnapshotEvaluate(t, p, `(()=>{
 const failure=callback=>{try{callback();return null}catch(error){return [error.name,error.message]}};
 const frame=document.createElement('iframe');document.body.append(frame);
 const foreign=frame.contentWindow,container=document.createElement('div'),child=document.createElement('span');
 const result=[
   failure(()=>Element.prototype.append.call(undefined)),
   failure(()=>Node.prototype.appendChild.call(undefined)),
   failure(()=>Object.getOwnPropertyDescriptor(HTMLIFrameElement.prototype,'contentWindow').get.call(undefined)),
   foreign.Element.prototype.append.call(container,child)===undefined&&container.firstChild===child,
   Object.getOwnPropertyDescriptor(HTMLIFrameElement.prototype,'contentWindow').get.call(frame)===foreign,
 ];
 frame.remove();
 return JSON.stringify(result);
})()`)
	want := `[["TypeError","Illegal invocation"],["TypeError","Illegal invocation"],["TypeError","Illegal invocation"],true,true]`
	if value != want {
		t.Fatalf("DOM receiver brands: want %s, got %v", want, value)
	}
}

//go:embed testdata/constructor_object_brand.js
var constructorObjectBrandProbe string

func TestGeneratedConstructorObjectBrands(t *testing.T) {
	page := bootstrapSnapshotPage(t)
	probe := constructorObjectBrandProbe
	if value := bootstrapSnapshotEvaluate(t, page, probe); value != "ok" {
		t.Fatalf("ordinary object brand: %v", value)
	}
	bootstrapSnapshotWarm(t, page)
	restored, err := page.ctx.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	if value := bootstrapSnapshotEvaluate(t, restored, probe); value != "ok" {
		t.Fatalf("restored object brand: %v", value)
	}
	if os.Getenv("MIMIC_DISABLE_BOOTSTRAP_SNAPSHOT") != "1" && !restored.Top.Realm.bootstrapRestored {
		t.Fatal("constructor-brand probe did not use a restored realm")
	}
}
