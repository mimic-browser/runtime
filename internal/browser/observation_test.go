package browser

import (
	"context"
	"testing"

	chrome152 "github.com/moreveal/mimic/chrome/152"
	"github.com/moreveal/mimic/internal/engine"
	gojaengine "github.com/moreveal/mimic/internal/engine/goja"
	v8engine "github.com/moreveal/mimic/internal/engine/v8"
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
