//go:build (windows || linux) && amd64

package browser

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/moreveal/mimic/internal/engine"
)

func TestIsolatedWorldRuntimeMaterializesOnFirstEvaluation(t *testing.T) {
	serialBrowserTest(t)
	page := bootstrapSnapshotPage(t)
	realmID, err := page.IsolatedWorld(context.Background(), page.Top.ID, "demand-driven")
	if err != nil {
		t.Fatal(err)
	}
	world := page.Top.Realm.isolatedWorlds["demand-driven"]
	deferred, ok := world.runtime.(*deferredRuntime)
	if !ok || deferred.closed || world.ID != realmID {
		t.Fatalf("isolated world was materialized before use: %T", world.runtime)
	}
	debugger := NewDebugger(page)
	defer debugger.Close()
	result, err := debugger.Evaluate(context.Background(), page.Top.ID, realmID, "globalThis.marker=41;marker+1", DebuggerOptions{ReturnByValue: true})
	if err != nil || result["result"].(map[string]any)["value"] != float64(42) {
		t.Fatalf("first isolated-world evaluation: %#v %v", result, err)
	}
	if world.runtime == deferred {
		t.Fatal("first evaluation did not materialize the isolated world")
	}
}

func TestDocumentTreeWorldsJoinFirstMaterializedOwner(t *testing.T) {
	serialBrowserTest(t)
	page := bootstrapSnapshotPage(t)
	main := page.Top.Realm
	ctx := context.Background()
	worldID, err := page.IsolatedWorld(ctx, page.Top.ID, "first-world")
	if err != nil {
		t.Fatal(err)
	}
	world := main.isolatedWorlds["first-world"]
	if main.runtimeGroup != world.runtimeGroup {
		t.Fatal("isolated world did not inherit its document tree")
	}
	if _, ok := main.runtime.(*deferredRuntime); !ok {
		t.Fatal("creating isolated world materialized main world")
	}
	debugger := NewDebugger(page)
	defer debugger.Close()
	result, err := debugger.Evaluate(ctx, page.Top.ID, worldID, `Array.prototype.worldOnly = 1; document.body.setAttribute('data-first-world', 'yes'); true`, DebuggerOptions{ReturnByValue: true})
	if err != nil || result["exceptionDetails"] != nil {
		t.Fatalf("first isolated execution: %#v %v", result, err)
	}
	if _, ok := main.runtime.(*deferredRuntime); !ok {
		t.Fatal("first isolated execution materialized main world")
	}
	if len(main.runtimeGroup.members) != 1 {
		t.Fatalf("first execution admitted %d contexts, want one", len(main.runtimeGroup.members))
	}
	observed, err := page.Evaluate(ctx, `Array.prototype.worldOnly === undefined && document.body.getAttribute('data-first-world') === 'yes'`)
	if err != nil || observed != true {
		t.Fatalf("main world isolation and shared DOM: %v %v", observed, err)
	}
	if len(main.runtimeGroup.members) != 2 {
		t.Fatalf("materialized worlds: %d, want two", len(main.runtimeGroup.members))
	}
	sameOwner := main.runtime.(interface{ SameOwner(engine.Runtime) bool })
	if !sameOwner.SameOwner(world.runtime) {
		t.Fatal("main and isolated worlds use different native isolates")
	}
	if _, err := page.Evaluate(ctx, `(()=>{const f=document.createElement('iframe');document.body.append(f);return f.contentWindow.eval('true')})()`); err != nil {
		t.Fatal(err)
	}
	if len(main.runtimeGroup.members) != 3 {
		t.Fatalf("frame did not join document tree: %d members", len(main.runtimeGroup.members))
	}
	for _, frame := range main.childFrames {
		if frame.Realm != nil && !sameOwner.SameOwner(frame.Realm.runtime) {
			t.Fatal("child frame uses another native isolate")
		}
	}
	if err := world.Close(); err != nil {
		t.Fatal(err)
	}
	if len(main.runtimeGroup.members) != 2 {
		t.Fatalf("closing isolated world affected siblings: %d members", len(main.runtimeGroup.members))
	}
	if observed, err := page.Evaluate(ctx, `document.body.getAttribute('data-first-world') === 'yes'`); err != nil || observed != true {
		t.Fatalf("main world after sibling close: %v %v", observed, err)
	}
}

func TestDocumentTreeOwnerIsReplacedOnNavigationAndIndependentPages(t *testing.T) {
	serialBrowserTest(t)
	page := bootstrapSnapshotPage(t)
	first := page.Top.Realm.runtimeGroup
	sibling, err := page.ctx.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	if first == sibling.Top.Realm.runtimeGroup {
		t.Fatal("independent Pages shared a runtime owner")
	}
	if _, err := page.Evaluate(context.Background(), `true`); err != nil {
		t.Fatal(err)
	}
	if _, err := sibling.Evaluate(context.Background(), `true`); err != nil {
		t.Fatal(err)
	}
	if len(first.members) != 1 || len(sibling.Top.Realm.runtimeGroup.members) != 1 {
		t.Fatal("independent Pages did not materialize separate owners")
	}
	if page.Top.Realm.runtime.(interface{ SameOwner(engine.Runtime) bool }).SameOwner(sibling.Top.Realm.runtime) {
		t.Fatal("independent Pages share a native isolate")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<!doctype html><body>replacement"))
	}))
	defer server.Close()
	if err := page.Navigate(context.Background(), server.URL); err != nil {
		t.Fatal(err)
	}
	if replacement := page.Top.Realm.runtimeGroup; replacement == first || replacement == sibling.Top.Realm.runtimeGroup {
		t.Fatal("new document reused an old Page owner")
	}
}

func TestSemanticallyEmptyInitScript(t *testing.T) {
	for _, source := range []string{"", " \t\r\n", "// sourceURL=internal\n", "/* block */ // line\n"} {
		if !semanticallyEmptyScript(source) {
			t.Fatalf("empty script rejected: %q", source)
		}
	}
	for _, source := range []string{"0", "/* unterminated", "/", "// comment\ntrue"} {
		if semanticallyEmptyScript(source) {
			t.Fatalf("executable script accepted: %q", source)
		}
	}
}

func TestInitScriptWorldMaterializesOnlyForExecutableSource(t *testing.T) {
	serialBrowserTest(t)
	page := bootstrapSnapshotPage(t)
	ctx := context.Background()
	page.AddInitScriptWorld("// sourceURL=automation", "empty-init")
	page.AddInitScriptWorld("globalThis.initMarker=42", "executable-init")
	page.runInitScripts(ctx, page.Top.Realm)

	empty := page.Top.Realm.isolatedWorlds["empty-init"]
	if _, ok := empty.runtime.(*deferredRuntime); !ok {
		t.Fatalf("comment-only init script materialized its realm: %T", empty.runtime)
	}
	executable := page.Top.Realm.isolatedWorlds["executable-init"]
	if _, ok := executable.runtime.(*deferredRuntime); ok {
		t.Fatal("executable init script left its realm deferred")
	}
	debugger := NewDebugger(page)
	defer debugger.Close()
	result, err := debugger.Evaluate(ctx, page.Top.ID, executable.ID, "initMarker", DebuggerOptions{ReturnByValue: true})
	if err != nil || result["result"].(map[string]any)["value"] != float64(42) {
		t.Fatalf("executable init script result: %#v %v", result, err)
	}
}
