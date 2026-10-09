package browser

import (
	"context"
	"fmt"
	chrome152 "github.com/moreveal/mimic/chrome/152"
	v8engine "github.com/moreveal/mimic/internal/engine/v8"
	"testing"
)

func TestBlitzNativeGenerationTracksCleanAndMutatedObservations(t *testing.T) {
	serialBrowserTest(t)
	t.Setenv("MIMIC_STYLE_ENGINE", "blitz")
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
	navigateCapabilityFixture(t, p)
	eval := func(code string, want string) {
		t.Helper()
		got, err := p.Evaluate(context.Background(), code)
		if err != nil || fmt.Sprint(got) != want {
			t.Fatalf("observation %v err %v want%s", got, err, want)
		}
	}
	eval(`document.body.innerHTML='<div id="box" style="width:40px;height:20px"></div>';document.querySelector('#box').getBoundingClientRect().width`, "40")
	state := p.Top.Realm.blitz
	if state == nil || state.document.Owner == nil || state.fallback != "" {
		t.Fatal("native generation gate used fallback")
	}
	generation, builds := state.generation, state.document.Builds
	if generation == 0 || builds != 1 {
		t.Fatalf("first-build accounting generation%d builds%d", generation, builds)
	}
	eval(`document.querySelector('#box').getBoundingClientRect().width`, "40")
	if state.generation != generation || state.document.Builds != builds {
		t.Fatal("clean observation rebuilt native state")
	}
	eval(`document.querySelector('#box').style.width='80px';document.querySelector('#box').getBoundingClientRect().width`, "80")
	if state.generation <= generation || state.document.Builds != builds {
		t.Fatal("mutation did not rebuild products within same owner")
	}
}

func TestComputedUnknownPropertiesDoNotObserveLayout(t *testing.T) {
	parallelBrowserTest(t)
	p := blitzStandardsPage(t)
	value, err := p.Evaluate(context.Background(), `document.body.innerHTML='<div style="width:20px"></div>';const style=getComputedStyle(document.querySelector('div'));['not-a-css-property','boxSizing','fontFamily'].every(name=>style.getPropertyValue(name)==='')`)
	if err != nil || value != true {
		t.Fatalf("unknown properties: %v %v", value, err)
	}
	if state := p.Top.Realm.blitz; state != nil && state.document.Owner != nil {
		t.Fatal("unknown property constructed native projection")
	}
}

func TestBlitzAcknowledgedStylesReplayAndObserveCSSOMChanges(t *testing.T) {
	parallelBrowserTest(t)
	p := blitzStandardsPage(t)
	eval := func(source, want string) {
		t.Helper()
		value, err := p.Evaluate(context.Background(), source)
		if err != nil || fmt.Sprint(value) != want {
			t.Fatalf("style observation: %v %v; want %s", value, err, want)
		}
	}
	eval(`document.body.innerHTML='<style>#probe{color:rgb(1,2,3);width:40px;height:20px}</style><div id="probe"></div>';getComputedStyle(document.querySelector('#probe')).color`, "rgb(1, 2, 3)")
	state := p.Top.Realm.blitz
	if state == nil || state.document.Owner == nil || state.sheetInputRevision == 0 {
		t.Fatal("stylesheet inputs were not acknowledged by the native owner")
	}
	revision := state.sheetInputRevision
	state.document.Close()
	eval(`document.querySelector('#probe').setAttribute('data-check','1');getComputedStyle(document.querySelector('#probe')).color`, "rgb(1, 2, 3)")
	if state.sheetInputRevision != revision {
		t.Fatal("unchanged stylesheet program changed its revision")
	}
	eval(`document.querySelector('style').sheet.cssRules[0].style.color='rgb(4,5,6)';getComputedStyle(document.querySelector('#probe')).color`, "rgb(4, 5, 6)")
	if state.sheetInputRevision <= revision {
		t.Fatal("CSSOM declaration mutation did not invalidate acknowledged inputs")
	}
	eval(`document.querySelector('style').sheet.disabled=true;getComputedStyle(document.querySelector('#probe')).color !== 'rgb(4, 5, 6)'`, "true")
	eval(`document.querySelector('style').sheet.disabled=false;getComputedStyle(document.querySelector('#probe')).width`, "40px")
}
