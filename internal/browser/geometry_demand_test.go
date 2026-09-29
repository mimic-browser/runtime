package browser

import (
	"context"
	"encoding/json"
	"testing"

	chrome152 "github.com/moreveal/mimic/chrome/152"
	v8engine "github.com/moreveal/mimic/internal/engine/v8"
)

func TestGeometryMaintenanceStartsAtFirstObservation(t *testing.T) {
	serialBrowserTest(t)
	t.Setenv("MIMIC_PROFILE_HOSTS", "1")
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
	eval := func(source string) any {
		t.Helper()
		value, err := p.Evaluate(context.Background(), source)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	counts := func() map[string]uint64 {
		t.Helper()
		data, err := json.Marshal(p.LiveDiagnostics())
		if err != nil {
			t.Fatal(err)
		}
		var diagnostic struct {
			Costs map[string]struct{ Count uint64 }
		}
		if err := json.Unmarshal(data, &diagnostic); err != nil {
			t.Fatal(err)
		}
		result := make(map[string]uint64)
		for _, name := range []string{"host:parentNode", "host:isConnected", "host:observationRevision"} {
			result[name] = diagnostic.Costs[name].Count
		}
		return result
	}
	eval(`document.body.innerHTML='<section id="root"></section>';globalThis.root=document.getElementById('root')`)
	before := counts()
	eval(`for(let i=0;i<100;i++){const node=document.createElement('div');node.setAttribute('data-i',String(i));root.appendChild(node)}`)
	for name, after := range counts() {
		if after != before[name] {
			t.Fatalf("unobserved geometry performed canonical ancestry maintenance: %s %d -> %d", name, before[name], after)
		}
	}
	// First observation must include all preceding writes. Subsequent writes,
	// including a newly attached shadow tree, must invalidate the populated epoch.
	got := eval(`(()=>{
root.style.width='40px';const first=root.getBoundingClientRect().width;
root.style.width='80px';const changed=root.getBoundingClientRect().width;
const host=document.createElement('div');document.body.appendChild(host);
const shadow=host.attachShadow({mode:'open'});
shadow.innerHTML='<style>div{width:21px;height:10px}div[data-wide]{width:42px}</style><div></div>';
const child=shadow.querySelector('div');const narrow=child.getBoundingClientRect().width;
child.setAttribute('data-wide','');const wide=child.getBoundingClientRect().width;
child.removeAttribute('data-wide');const restored=child.getBoundingClientRect().width;
return JSON.stringify([root.children.length,first,changed,narrow,wide,restored]);
})()`)
	if got != `[100,40,80,21,42,21]` {
		t.Fatalf("geometry demand transition: %v", got)
	}
}
