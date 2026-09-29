package browser

import (
	"context"
	"testing"

	chrome152 "github.com/moreveal/mimic/chrome/152"
	"github.com/moreveal/mimic/internal/engine"
	gojaengine "github.com/moreveal/mimic/internal/engine/goja"
	v8engine "github.com/moreveal/mimic/internal/engine/v8"
	"github.com/moreveal/mimic/internal/trace"
)

func TestSharedObservationTracksSupportAndReceivers(t *testing.T) {
	parallelBrowserTest(t)
	for name, factory := range map[string]engine.Factory{"goja": gojaengine.Factory{}, "v8": v8engine.Factory{}} {
		t.Run(name, func(t *testing.T) {
			b, err := New(factory, chrome152.New())
			if err != nil {
				t.Fatal(err)
			}
			c := b.NewContext()
			defer c.Close()
			for page := 0; page < 2; page++ {
				p, err := c.NewPage()
				if err != nil {
					t.Fatal(err)
				}
				p.Trace().Start()
				v, err := p.Evaluate(context.Background(), `(()=>{
 const a=document.createElement('div'),b=document.createElement('div');
 void a.observationProbe;a.observationProbe=1;void a.observationProbe;void b.observationProbe;
 Object.defineProperty(b,'observationProbe',{get(){return this===b?2:0}});
 if(a.observationProbe!==1||b.observationProbe!==2)return false;
 delete a.observationProbe;void a.observationProbe;
 const proto=Object.create(Object.getPrototypeOf(a));
 Object.defineProperty(proto,'observationProbe',{get(){return this===a?3:0}});
 Object.setPrototypeOf(a,proto);
 return a.observationProbe===3&&b.observationProbe===2;
})()`)
				if err != nil || v != true {
					t.Fatalf("receiver/prototype: %v %v", v, err)
				}
				counts := map[bool]int{}
				for _, event := range p.Trace().Events() {
					if event.Name == "propertyAccess" && event.Data["property"] == "Element<div>.observationProbe" {
						counts[event.Data["supported"].(bool)]++
					}
				}
				if counts[true] != 1 || counts[false] != 1 {
					t.Fatalf("once per support state and Page: %v", counts)
				}
			}
		})
	}
}

func TestPropertyObservationStartsOnDemand(t *testing.T) {
	parallelBrowserTest(t)
	for name, factory := range map[string]engine.Factory{"goja": gojaengine.Factory{}, "v8": v8engine.Factory{}} {
		t.Run(name, func(t *testing.T) {
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
			read := func() {
				t.Helper()
				if _, err := p.Evaluate(context.Background(), `void document.body.tagName`); err != nil {
					t.Fatal(err)
				}
			}
			count := func() int {
				t.Helper()
				result := 0
				for _, event := range p.Trace().Events() {
					if event.Name == "propertyAccess" && event.Data["property"] == "Element<body>.tagName" {
						result++
					}
				}
				return result
			}
			read()
			if got := count(); got != 0 {
				t.Fatalf("unrequested capture: %d", got)
			}
			for capture := 0; capture < 2; capture++ {
				p.Trace().Start()
				read()
				if got := count(); got != 1 {
					t.Fatalf("capture %d: got %d property events, want 1", capture, got)
				}
				p.Trace().Stop()
				read()
				if got := count(); got != 1 {
					t.Fatalf("capture %d changed after stop: %d", capture, got)
				}
				if got := len(p.Top.Realm.apiSeen); got != 0 {
					t.Fatalf("capture %d retained %d deduplication entries after stop", capture, got)
				}
			}
			seen := 0
			unsubscribe := p.Trace().SubscribeKinds([]trace.Kind{trace.API}, func(event trace.Event) {
				if event.Name == "propertyAccess" && event.Data["property"] == "Element<body>.tagName" {
					seen++
				}
			})
			read()
			unsubscribe()
			if seen != 1 || count() != 1 {
				t.Fatalf("API subscriber saw %d events, retained capture has %d", seen, count())
			}
		})
	}
}

func TestPropertyObservationIsolatedBetweenPages(t *testing.T) {
	parallelBrowserTest(t)
	b, err := New(v8engine.Factory{}, chrome152.New())
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
	for _, page := range []*Page{first, second} {
		if _, err := page.Evaluate(context.Background(), `void document.body.tagName`); err != nil {
			t.Fatal(err)
		}
	}
	first.Trace().Start()
	results := make(chan error, 2)
	for _, page := range []*Page{first, second} {
		go func(page *Page) {
			_, err := page.Evaluate(context.Background(), `void document.body.tagName`)
			results <- err
		}(page)
	}
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	count := func(page *Page) int {
		t.Helper()
		result := 0
		for _, event := range page.Trace().Events() {
			if event.Name == "propertyAccess" && event.Data["property"] == "Element<body>.tagName" {
				result++
			}
		}
		return result
	}
	if got := count(first); got != 1 {
		t.Fatalf("first Page property events: %d, want 1", got)
	}
	if got := count(second); got != 0 {
		t.Fatalf("second Page collected without demand: %d events", got)
	}
	second.Trace().Start()
	if _, err := second.Evaluate(context.Background(), `void document.body.tagName`); err != nil {
		t.Fatal(err)
	}
	if got := count(second); got != 1 {
		t.Fatalf("second Page property events after start: %d, want 1", got)
	}
}
