package browser

import (
	"context"
	_ "embed"
	"encoding/json"
	"testing"
	"time"

	chrome152 "github.com/moreveal/mimic/chrome/152"
	v8engine "github.com/moreveal/mimic/internal/engine/v8"
)

// The original frozen fixture is retained. This independent successor corrects
// its static-position assertion using the frozen geometry receipt; see
// docs/compatibility/geometry-integration-20260912.md.
//
//go:embed testdata/intersection_observer_regressions_v2.js
var intersectionObserverRegressions string

func TestIntersectionObserverInitialAndChangedObservations(t *testing.T) {
	serialBrowserTest(t)
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
	// The successor's exact ratio rectangles come from the standards-mode
	// /plain receipt, rather than an initial quirks-mode about:blank document.
	navigateCapabilityFixture(t, p)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got, err := p.Evaluate(ctx, intersectionObserverRegressions)
	if err != nil || got != "ok" {
		t.Fatalf("intersection regressions: %v, %v", got, err)
	}
}

func TestUnchangedIntersectionSampleAvoidsGeometryWork(t *testing.T) {
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := p.Evaluate(ctx, `document.body.innerHTML='<div style="width:40px;height:20px"></div>';window.entries=[];new IntersectionObserver(e=>entries.push(...e)).observe(document.body.firstElementChild)`); err != nil {
		t.Fatal(err)
	}
	advance := func() {
		t.Helper()
		if err := p.AdvanceTime(ctx, 20*time.Millisecond); err != nil {
			t.Fatal(err)
		}
	}
	count := func() uint64 {
		t.Helper()
		data, err := json.Marshal(p.LiveDiagnostics())
		if err != nil {
			t.Fatal(err)
		}
		var result struct {
			Costs map[string]struct{ Count uint64 }
		}
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatal(err)
		}
		// Canonical geometry consumes one bundled style observation. Count that
		// boundary so this retains the same no-work/invalidation invariant after
		// the retired per-property availability query was removed.
		return result.Costs["host:styleObservationState"].Count
	}
	advance()
	before := count()
	if before == 0 {
		t.Fatal("initial observation did not sample geometry")
	}
	for i := 0; i < 10; i++ {
		advance()
	}
	if after := count(); after != before {
		t.Fatalf("unchanged sample repeated geometry: %d -> %d", before, after)
	}
	if err := p.SetViewport(800, 600); err != nil {
		t.Fatal(err)
	}
	advance()
	if count() <= before {
		t.Fatal("viewport change did not invalidate observation")
	}
	// An otherwise idle shadow tree must obey the same epoch. Attachment and
	// synthetic membership changes still trigger fresh samples.
	if _, err := p.Evaluate(ctx, `window.owner=document.createElement('div');document.body.append(owner);window.root=owner.attachShadow({mode:'open'});root.innerHTML='<span style="display:block;width:20px;height:20px"></span>';window.target=root.firstChild;window.shadowEntries=[];new IntersectionObserver(e=>shadowEntries.push(...e.map(v=>v.isIntersecting))).observe(target)`); err != nil {
		t.Fatal(err)
	}
	advance()
	before = count()
	for i := 0; i < 10; i++ {
		advance()
	}
	if after := count(); after != before {
		t.Fatalf("unchanged shadow sample repeated geometry: %d -> %d", before, after)
	}
	for _, source := range []string{`root.removeChild(target)`, `root.appendChild(target)`, `owner.style.display='none'`, `owner.style.display='block'`} {
		if _, err := p.Evaluate(ctx, source); err != nil {
			t.Fatal(err)
		}
		advance()
	}
	if got, err := p.Evaluate(ctx, `shadowEntries.join(',')`); err != nil || got != "true,false,true,false,true" {
		t.Fatalf("shadow epoch transitions: %v %v", got, err)
	}
}

func TestIntersectionObserverUsesResolvedControlFontGeometry(t *testing.T) {
	serialBrowserTest(t)
	p := testPage(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got, err := p.Evaluate(ctx, `(()=>{
 const button=document.createElement('button');
 button.style.fontSize='var(--missing-font-size)';
 button.textContent='Log in';document.body.append(button);
 return new Promise(resolve=>new IntersectionObserver((entries,observer)=>{
  const actual=button.getBoundingClientRect();observer.disconnect();resolve([entries.length,entries[0].target===button,entries[0].intersectionRatio,entries[0].boundingClientRect.width===actual.width&&actual.width>16,entries[0].boundingClientRect.height===actual.height&&actual.height>1]);
 }).observe(button));
})()`)
	if err != nil {
		t.Fatal(err)
	}
	values, ok := got.([]any)
	if !ok || len(values) != 5 || values[0] != int64(1) || values[1] != true || values[3] != true || values[4] != true {
		t.Fatalf("unexpected control observation: %#v", got)
	}
}

func TestIntersectionObserverResolvesAncestorFontVariable(t *testing.T) {
	serialBrowserTest(t)
	p := testPage(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got, err := p.Evaluate(ctx, `(()=>{
 const parent=document.createElement('button'),target=document.createElement('span');
 parent.style.cssText='font-size:var(--missing-font-size);overflow:hidden';parent.textContent='';parent.append(target);document.body.append(parent);
 return new Promise(resolve=>new IntersectionObserver((entries,observer)=>{observer.disconnect();resolve([entries.length,entries[0].isIntersecting,entries[0].intersectionRatio])}).observe(target));
})()`)
	if err != nil {
		t.Fatal(err)
	}
	values, ok := got.([]any)
	if !ok || len(values) != 3 || values[0] != int64(1) || values[1] != true || values[2] != int64(1) {
		t.Fatalf("unexpected ancestor observation: %#v", got)
	}
}
