package browser

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	chrome152 "github.com/moreveal/mimic/chrome/152"
	"github.com/moreveal/mimic/internal/engine"
	gojaengine "github.com/moreveal/mimic/internal/engine/goja"
)

type countingFactory struct{ count int }

func (f *countingFactory) New() engine.Runtime {
	f.count++
	return (gojaengine.Factory{}).New()
}

func TestInitialBlankRealmIsDeferredAndIndependent(t *testing.T) {
	parallelBrowserTest(t)
	factory := &countingFactory{}
	b, err := New(factory, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	c := b.NewContext()
	defer c.Close()
	first, err := c.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	second, err := c.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	if err := first.AdvanceTime(context.Background(), time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if factory.count != 0 {
		t.Fatalf("unobserved blank pages initialized %d runtimes", factory.count)
	}
	value, err := first.Evaluate(context.Background(), `Object.prototype.pageOnly=42;document.body.textContent='first';document.readyState==='complete'&&document.body.textContent==='first'`)
	if err != nil || value != true {
		t.Fatalf("initial blank: %v %v", value, err)
	}
	if factory.count != 1 {
		t.Fatalf("runtime count %d", factory.count)
	}
	value, err = second.Evaluate(context.Background(), `!('pageOnly' in {})&&document.body.textContent===''`)
	if err != nil || value != true {
		t.Fatalf("independent blank: %v %v", value, err)
	}
	if factory.count != 2 {
		t.Fatalf("runtime count %d", factory.count)
	}
}

func TestNavigationStartsScriptTransportBeforeRuntimeObservation(t *testing.T) {
	parallelBrowserTest(t)
	entered, release := make(chan struct{}), make(chan struct{})
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/held.js" {
			close(entered)
			select {
			case <-release:
			case <-r.Context().Done():
			}
			return
		}
		fmt.Fprint(w, `<script src="/held.js"></script>`)
	}))
	defer fixture.Close()
	defer close(release)
	factory := &countingFactory{}
	b, err := New(factory, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	c := b.NewContext()
	defer c.Close()
	p, err := c.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() { finished <- p.Navigate(ctx, fixture.URL) }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("external script transport did not start")
	}
	cancel()
	<-finished
	if factory.count != 0 {
		t.Fatalf("unobserved parser-suspended document initialized %d runtimes", factory.count)
	}
}

func TestInitialBlankChildRealmsInitializeOnlyWhenObserved(t *testing.T) {
	parallelBrowserTest(t)
	factory := &countingFactory{}
	b, err := New(factory, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	c := b.NewContext()
	defer c.Close()
	p, err := c.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	value, err := p.Evaluate(context.Background(), `globalThis.loads=0;globalThis.a=document.createElement('iframe');globalThis.b=document.createElement('iframe');a.onload=b.onload=()=>loads++;document.body.append(a,b);loads===2`)
	if err != nil || value != true {
		t.Fatalf("synchronous initial child load: %v, %v", value, err)
	}
	if factory.count != 1 {
		t.Fatalf("unobserved child documents created runtimes: %d", factory.count)
	}
	value, err = p.Evaluate(context.Background(), `const w=a.contentWindow,d=a.contentDocument;d.body.textContent='owned';w.eval('globalThis.childOnly=42');d===w.document&&d.readyState==='complete'&&w.childOnly===42&&typeof childOnly==='undefined'&&w.Document!==Document&&a.contentDocument===d`)
	if err != nil || value != true {
		t.Fatalf("observed child identity and isolation: %v, %v", value, err)
	}
	if factory.count != 2 {
		t.Fatalf("only the observed child should initialize: %d", factory.count)
	}
	value, err = p.Evaluate(context.Background(), `a.remove();b.remove();true`)
	if err != nil || value != true {
		t.Fatalf("child teardown: %v, %v", value, err)
	}
	if factory.count != 2 {
		t.Fatalf("teardown initialized an unobserved child: %d", factory.count)
	}
}
