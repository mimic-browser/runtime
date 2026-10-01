package browser

import (
	"context"
	"encoding/json"
	"testing"
)

func liveDiagnosticCost(t *testing.T, page *Page, name string) uint64 {
	t.Helper()
	encoded, err := json.Marshal(page.LiveDiagnostics())
	if err != nil {
		t.Fatal(err)
	}
	var value any
	if err := json.Unmarshal(encoded, &value); err != nil {
		t.Fatal(err)
	}
	var total uint64
	var walk func(any)
	walk = func(value any) {
		switch value := value.(type) {
		case map[string]any:
			if costs, ok := value["costs"].(map[string]any); ok {
				if cost, ok := costs[name].(map[string]any); ok {
					total += uint64(cost["count"].(float64))
				}
			}
			for key, child := range value {
				if key != "costs" {
					walk(child)
				}
			}
		case []any:
			for _, child := range value {
				walk(child)
			}
		}
	}
	walk(value)
	return total
}

func TestIsolatedStyleObservationsUseCanonicalOwner(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		d := NewDebugger(p)
		defer d.Close()
		debuggerEval(t, d, `document.head.innerHTML='<style>#probe{width:20px}#probe:checked{width:40px}</style>';document.body.innerHTML='<input id="probe" type="checkbox">'`, DebuggerOptions{})
		world, err := p.IsolatedWorld(context.Background(), p.Top.ID, "style-observer")
		if err != nil {
			t.Fatal(err)
		}
		read := func(want string) {
			t.Helper()
			result, err := d.Evaluate(context.Background(), p.Top.ID, world, `(()=>{const e=document.getElementById('probe');return JSON.stringify([getComputedStyle(e).width,e.checkVisibility(),e.getBoundingClientRect().width,e===document.getElementById('probe')])})()`, DebuggerOptions{ReturnByValue: true})
			if err != nil || result["exceptionDetails"] != nil || result["result"].(map[string]any)["value"] != want {
				t.Fatalf("owner observation: %#v, %v; want %s", result, err, want)
			}
		}
		read(`["20px",true,20,true]`)
		debuggerEval(t, d, `document.styleSheets[0].insertRule('#probe{width:60px}',2)`, DebuggerOptions{})
		read(`["60px",true,60,true]`)
		debuggerEval(t, d, `document.getElementById('probe').style.display='none'`, DebuggerOptions{})
		read(`["60px",false,0,true]`)
		debuggerEval(t, d, `document.getElementById('probe').style.display='';document.styleSheets[0].deleteRule(2);document.getElementById('probe').checked=true`, DebuggerOptions{})
		read(`["40px",true,40,true]`)
		debuggerEval(t, d, `Element.prototype.checkVisibility=()=>{throw new Error('main-world override')}`, DebuggerOptions{})
		read(`["40px",true,40,true]`)
	})
}

func TestIsolatedStyleObservationInitializesDeferredOwner(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		d := NewDebugger(p)
		defer d.Close()
		world, err := p.IsolatedWorld(context.Background(), p.Top.ID, "initial-style")
		if err != nil {
			t.Fatal(err)
		}
		result, err := d.Evaluate(context.Background(), p.Top.ID, world, `document.body.checkVisibility()`, DebuggerOptions{ReturnByValue: true})
		if err != nil || result["exceptionDetails"] != nil || result["result"].(map[string]any)["value"] != true {
			t.Fatalf("initial owner: %#v %v", result, err)
		}
	})
}

func TestIsolatedStyleBatchesLargeStableDocument(t *testing.T) {
	serialBrowserTest(t)
	t.Setenv("MIMIC_PROFILE_HOSTS", "1")
	historyTestPages(t, func(t *testing.T, p *Page) {
		d := NewDebugger(p)
		defer d.Close()
		debuggerEval(t, d, `document.head.innerHTML='<style>.item{display:block}.item.hidden{display:none}</style>';document.body.innerHTML=Array.from({length:160},(_,i)=>'<div class="item '+(i%2?'hidden':'')+'"></div>').join('')`, DebuggerOptions{})
		world, err := p.IsolatedWorld(context.Background(), p.Top.ID, "style-batch")
		if err != nil {
			t.Fatal(err)
		}
		before := liveDiagnosticCost(t, p, "host:foreignComputedStyleFlatTree")
		result, err := d.Evaluate(context.Background(), p.Top.ID, world, `Array.from(document.querySelectorAll('.item'),e=>getComputedStyle(e).display).filter(v=>v==='none').length`, DebuggerOptions{ReturnByValue: true})
		if err != nil || result["exceptionDetails"] != nil || result["result"].(map[string]any)["value"] != float64(80) {
			t.Fatalf("batched styles: %#v %v", result, err)
		}
		after := liveDiagnosticCost(t, p, "host:foreignComputedStyleFlatTree")
		if calls := after - before; calls > 2 {
			t.Fatalf("large stable style read used %d owner crossings", calls)
		}
		result, err = d.Evaluate(context.Background(), p.Top.ID, world, `Array.from(document.querySelectorAll('.item'),e=>e.checkVisibility()).filter(Boolean).length`, DebuggerOptions{ReturnByValue: true})
		if err != nil || result["exceptionDetails"] != nil || result["result"].(map[string]any)["value"] != float64(80) {
			t.Fatalf("batched visibility: %#v %v", result, err)
		}
		visibilityAfter := liveDiagnosticCost(t, p, "host:foreignComputedStyleFlatTree")
		if calls := visibilityAfter - after; calls != 0 {
			t.Fatalf("large stable visibility read used %d owner crossings", calls)
		}
		debuggerEval(t, d, `document.styleSheets[0].insertRule('.item{display:inline}',2)`, DebuggerOptions{})
		result, err = d.Evaluate(context.Background(), p.Top.ID, world, `getComputedStyle(document.querySelector('.item')).display`, DebuggerOptions{ReturnByValue: true})
		if err != nil || result["exceptionDetails"] != nil || result["result"].(map[string]any)["value"] != "inline" {
			t.Fatalf("post-mutation style: %#v %v", result, err)
		}
	})
}

func TestIsolatedStyleUsesElementReadsWhenRetentionIsDisabled(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		d := NewDebugger(p)
		defer d.Close()
		debuggerEval(t, d, `
document.body.innerHTML = Array.from({ length: 160 }, (_, i) => '<div class="item">' + i + '</div>').join('');
globalThis.effect = document.querySelector('.item').animate({ opacity: [0, 1] }, { duration: 1000 });
`, DebuggerOptions{})
		world, err := p.IsolatedWorld(context.Background(), p.Top.ID, "unretained-style")
		if err != nil {
			t.Fatal(err)
		}
		owner := p.Top.Realm
		original := owner.computedStyleFlatRead
		if err := owner.runtime.Set("originalStyleReadForTest", original); err != nil {
			t.Fatal(err)
		}
		owner.computedStyleFlatRead, err = owner.runtime.Eval(context.Background(), `
globalThis.styleReadScopesForTest = {};
(id, kind, name) => {
  styleReadScopesForTest[kind] = (styleReadScopesForTest[kind] || 0) + 1;
  return originalStyleReadForTest(id, kind, name);
}`, "style-read-scopes")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { owner.computedStyleFlatRead = original }()
		result, err := d.Evaluate(context.Background(), p.Top.ID, world, `(() => {
  const displays = Array.from(document.querySelectorAll('.item'), (e) => getComputedStyle(e).display);
  return displays.length === 160 && displays.every((value) => value === 'block');
})()`, DebuggerOptions{ReturnByValue: true})
		if err != nil || result["exceptionDetails"] != nil || result["result"].(map[string]any)["value"] != true {
			t.Fatalf("unretained styles: %#v %v", result, err)
		}
		counts, err := owner.runtime.Eval(context.Background(), `JSON.stringify(styleReadScopesForTest)`, "style-read-scope-counts")
		if err != nil {
			t.Fatal(err)
		}
		var reads map[string]int
		if err := json.Unmarshal([]byte(counts.Export().(string)), &reads); err != nil {
			t.Fatal(err)
		}
		if reads["documentValues"] != 0 || (owner.styleProjections.dynamic && reads["values"] == 0) {
			t.Fatalf("unretained style read scopes: %#v", reads)
		}
	})
}

func TestIsolatedWorldsShareDocumentStyleBatch(t *testing.T) {
	serialBrowserTest(t)
	t.Setenv("MIMIC_PROFILE_HOSTS", "1")
	historyTestPages(t, func(t *testing.T, p *Page) {
		d := NewDebugger(p)
		defer d.Close()
		debuggerEval(t, d, `document.body.innerHTML=Array.from({length:160},(_,i)=>'<div class="item">'+i+'</div>').join('')`, DebuggerOptions{})
		read := func(name, selector, property, want string) {
			t.Helper()
			world, err := p.IsolatedWorld(context.Background(), p.Top.ID, name)
			if err != nil {
				t.Fatal(err)
			}
			result, err := d.Evaluate(context.Background(), p.Top.ID, world, `getComputedStyle(document.querySelector('`+selector+`')).`+property, DebuggerOptions{ReturnByValue: true})
			if err != nil || result["exceptionDetails"] != nil || result["result"].(map[string]any)["value"] != want {
				t.Fatalf("style read: %#v %v", result, err)
			}
		}
		read("style-batch-a", ".item:first-child", "display", "block")
		batchKey := styleProjectionKey{node: p.Top.Realm.document.Root().ID, kind: "documentValues", property: `["display","visibility"]`}
		if _, ok := p.Top.Realm.styleProjections.values[batchKey]; !ok {
			t.Fatal("document-wide style batch was not retained under the document root")
		}
		before := liveDiagnosticCost(t, p, "host:foreignComputedStyleFlatTree")
		read("style-batch-b", ".item:last-child", "visibility", "visible")
		if calls := liveDiagnosticCost(t, p, "host:foreignComputedStyleFlatTree") - before; calls > 1 {
			// A separate JS context may reuse its local document batch directly or
			// make one cheap owner-cache lookup, but it must not rebuild the batch.
			t.Fatalf("second isolated world used %d host calls, want at most one cached lookup", calls)
		}
	})
}

func TestIsolatedStyleDocumentBatchAccumulatesProperties(t *testing.T) {
	serialBrowserTest(t)
	t.Setenv("MIMIC_PROFILE_HOSTS", "1")
	historyTestPages(t, func(t *testing.T, p *Page) {
		d := NewDebugger(p)
		defer d.Close()
		debuggerEval(t, d, `document.body.innerHTML=Array.from({length:160},(_,i)=>'<div class="item">'+i+'</div>').join('')`, DebuggerOptions{})
		world, err := p.IsolatedWorld(context.Background(), p.Top.ID, "style-columns")
		if err != nil {
			t.Fatal(err)
		}
		before := liveDiagnosticCost(t, p, "host:foreignComputedStyleFlatTree")
		result, err := d.Evaluate(context.Background(), p.Top.ID, world, `(()=>{const e=document.querySelector('.item');return JSON.stringify([getComputedStyle(e).content,getComputedStyle(e).cursor,getComputedStyle(e).content])})()`, DebuggerOptions{ReturnByValue: true})
		if err != nil || result["exceptionDetails"] != nil || result["result"].(map[string]any)["value"] != `["normal","auto","normal"]` {
			t.Fatalf("alternating properties: %#v %v", result, err)
		}
		calls := liveDiagnosticCost(t, p, "host:foreignComputedStyleFlatTree") - before
		if calls > 2 {
			t.Fatalf("alternating properties used %d owner crossings, want at most 2 additive batches", calls)
		}
		// Engines without a separate isolated-world runtime can answer locally and
		// therefore do not populate the cross-realm owner cache.
		if calls == 0 {
			return
		}
		contentKey := styleProjectionKey{node: p.Top.Realm.document.Root().ID, kind: "documentValues", property: `["content","display","visibility"]`}
		cursorKey := styleProjectionKey{node: p.Top.Realm.document.Root().ID, kind: "documentValues", property: `["cursor"]`}
		if _, ok := p.Top.Realm.styleProjections.values[contentKey]; !ok {
			t.Fatal("initial document property projection was not retained")
		}
		if _, ok := p.Top.Realm.styleProjections.values[cursorKey]; !ok {
			t.Fatal("incremental document property projection was not retained")
		}
	})
}

func TestIsolatedInnerTextTracksOwnerMutations(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		d := NewDebugger(p)
		defer d.Close()
		debuggerEval(t, d, `document.body.innerHTML='<div id="probe"><span>A</span><span style="display:none">B</span></div>'`, DebuggerOptions{})
		world, err := p.IsolatedWorld(context.Background(), p.Top.ID, "text-observer")
		if err != nil {
			t.Fatal(err)
		}
		read := func(want string) {
			t.Helper()
			result, err := d.Evaluate(context.Background(), p.Top.ID, world, `document.getElementById('probe').innerText`, DebuggerOptions{ReturnByValue: true})
			if err != nil || result["exceptionDetails"] != nil || result["result"].(map[string]any)["value"] != want {
				t.Fatalf("innerText: %#v %v; want %q", result, err, want)
			}
		}
		read("A")
		debuggerEval(t, d, `document.querySelector('#probe span:last-child').style.display='inline'`, DebuggerOptions{})
		read("AB")
		debuggerEval(t, d, `document.querySelector('#probe span').textContent='C'`, DebuggerOptions{})
		read("CB")
		// Frozen headful Chrome 152 keeps source newlines between inline buttons
		// as spaces; they are not rendered line boundaries.
		debuggerEval(t, d, `document.getElementById('probe').innerHTML='<button>First sources</button>\n    <button>Second sources</button>'`, DebuggerOptions{})
		read("First sources Second sources")
		debuggerEval(t, d, `document.getElementById('probe').innerHTML='<p>Paragraph</p>\n<button>First sources</button>\n<button>Second sources</button>'`, DebuggerOptions{})
		read("Paragraph\n\nFirst sources Second sources")
		debuggerEval(t, d, `document.getElementById('probe').innerHTML='<p>Paragraph</p><button>Sources</button><ul><li>Reference</li></ul>'`, DebuggerOptions{})
		read("Paragraph\n\nSources\nReference")
		debuggerEval(t, d, `document.getElementById('probe').innerHTML='First<br><br>Second'`, DebuggerOptions{})
		read("First\n\nSecond")
		debuggerEval(t, d, `document.getElementById('probe').innerHTML='\n<button>Sources</button>\n<ul style="display:none"><li>Hidden</li></ul>\n'`, DebuggerOptions{})
		read("Sources")
		result, err := d.Evaluate(context.Background(), p.Top.ID, world, `(()=>{const inert=document.implementation.createHTMLDocument(''),node=inert.createElement('div');node.textContent='detached';return node.innerText})()`, DebuggerOptions{ReturnByValue: true})
		if err != nil || result["exceptionDetails"] != nil || result["result"].(map[string]any)["value"] != "detached" {
			t.Fatalf("inert innerText: %#v %v", result, err)
		}
	})
}
