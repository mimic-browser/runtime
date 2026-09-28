package browser

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	chrome152 "github.com/moreveal/mimic/chrome/152"
	v8engine "github.com/moreveal/mimic/internal/engine/v8"
)

func TestModuleThrowIsTracedWithoutStoppingOtherScripts(t *testing.T) {
	parallelBrowserTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bad.js" {
			w.Header().Set("Content-Type", "text/javascript")
			fmt.Fprint(w, `throw new Error('module failure sentinel')`)
			return
		}
		fmt.Fprint(w, `<script type="module" src="/bad.js"></script><script type="module">globalThis.nextModule=true</script>`)
	}))
	defer server.Close()
	b, err := New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	c := b.NewContext()
	defer c.Close()
	p, err := c.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	p.Trace().Start()
	if err := p.Navigate(context.Background(), server.URL); err != nil {
		t.Fatal(err)
	}
	value, err := p.Evaluate(context.Background(), `globalThis.nextModule===true`)
	if err != nil || value != true {
		t.Fatalf("next module: %v %v", value, err)
	}
	found := false
	for _, event := range p.trace.Events() {
		if string(event.Kind) == "exception" && strings.Contains(fmt.Sprint(event.Data), "module failure sentinel") {
			found = true
		}
	}
	if !found {
		t.Fatal("module rejection absent from exception trace")
	}
}

func TestParserImportMapResolvesBareAndPrefixSpecifiers(t *testing.T) {
	parallelBrowserTest(t)
	var requested []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.Path)
		switch r.URL.Path {
		case "/":
			fmt.Fprint(w, `<script type="importmap">{"imports":{"pkg":"/vendor/pkg.js","lib/":"/vendor/lib/"}}</script><script type="module">import value from 'pkg';import {suffix} from 'lib/part.js';globalThis.importMapResult=value+suffix</script>`)
		case "/vendor/pkg.js":
			fmt.Fprint(w, `export default 'mapped-'`)
		case "/vendor/lib/part.js":
			fmt.Fprint(w, `export const suffix='prefix'`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	b, err := New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	c := b.NewContext()
	defer c.Close()
	p, err := c.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Navigate(context.Background(), server.URL); err != nil {
		t.Fatal(err)
	}
	value, err := p.Evaluate(context.Background(), `globalThis.importMapResult`)
	if err != nil || value != "mapped-prefix" {
		t.Fatalf("mapped module result: %v %v (requests %v)", value, err, requested)
	}
	for _, path := range requested {
		if path == "/pkg" || path == "/lib/part.js" {
			t.Fatalf("bare specifier fetched without mapping: %v", requested)
		}
	}
}

func TestImportMetaResolveUsesImmutableModuleBaseWithoutFetching(t *testing.T) {
	parallelBrowserTest(t)
	b, err := New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	c := b.NewContext()
	defer c.Close()
	p, err := c.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_, err = p.Top.Realm.EvaluateModule(ctx, `
const resolve=import.meta.resolve;
const descriptor=Object.getOwnPropertyDescriptor(import.meta,'resolve');
if(resolve.name!=='resolve'||resolve.length!==1||!descriptor.writable||!descriptor.enumerable||!descriptor.configurable)throw Error('descriptor');
import.meta.url='https://changed.test/';
globalThis.moduleResolved=[resolve('./x'),resolve('../y'),resolve('/z'),resolve('https://a.test/f')];
for(const value of ['bare','?q','#f',undefined,Symbol('x')]){try{resolve(value);throw Error('accepted invalid specifier')}catch(e){if(!(e instanceof TypeError))throw e}}
try{new resolve('./x');throw Error('constructable')}catch(e){if(!(e instanceof TypeError))throw e}
globalThis.retainedResolve=resolve;
`, "https://example.test/modules/entry.js", func(string, string) (string, string, error) {
		t.Error("resolve fetched a module")
		return "", "", fmt.Errorf("unexpected fetch")
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Top.Realm.EvaluateModule(ctx, `globalThis.anotherResolve=import.meta.resolve`, "https://other.test/entry.js", func(string, string) (string, string, error) { return "", "", fmt.Errorf("unexpected fetch") })
	if err != nil {
		t.Fatal(err)
	}
	value, err := p.Evaluate(ctx, `moduleResolved.concat(retainedResolve('./late'),anotherResolve('./late'),Object.hasOwn(globalThis,'__mimicImportMetaResolveFactory')).join('|')`)
	const want = "https://example.test/modules/x|https://example.test/y|https://example.test/z|https://a.test/f|https://example.test/modules/late|https://other.test/late|false"
	if err != nil || value != want {
		t.Fatalf("resolve: %v %v", value, err)
	}
}

func TestModulePendingEvaluationDoesNotPumpMicrotasks(t *testing.T) {
	parallelBrowserTest(t)
	b, err := New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	c := b.NewContext()
	defer c.Close()
	p, err := c.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	r := p.Top.Realm
	v, err := r.EvaluateModule(context.Background(), `globalThis.moduleJobRan=false;
Promise.resolve().then(()=>moduleJobRan=true);await new Promise(()=>{});`,
		"https://example.test/pending.js", func(string, string) (string, string, error) { return "", "", fmt.Errorf("unexpected import") })
	if err != nil {
		t.Fatal(err)
	}
	if _, settled, err := r.runtime.Await(v); err != nil || settled {
		t.Fatalf("pending: settled=%v err=%v", settled, err)
	}
	if r.runtime.Get("moduleJobRan").Export() != false {
		t.Fatal("module diagnostics pumped microtasks")
	}
	if err := r.runtime.MicrotaskCheckpoint(); err != nil {
		t.Fatal(err)
	}
	if r.runtime.Get("moduleJobRan").Export() != true {
		t.Fatal("explicit checkpoint did not run queued job")
	}
}
