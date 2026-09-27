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
