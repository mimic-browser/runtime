package browser

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
)

// Fixed pixel viewport and percentage-height cases also measured in frozen
// Chrome 152 headless. No font, display, or device-dependent sizes are asserted.
func TestQuirksPercentageHeightThroughAutoBlocks(t *testing.T) {
	parallelBrowserTest(t)
	script, err := os.ReadFile("testdata/quirks_percentage_height.js")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		`["quirks-auto","BackCompat",200]`,
		`["quirks-half","BackCompat",100]`,
		`["quirks-edges","BackCompat",190]`,
		`["quirks-definite","BackCompat",40]`,
		`["quirks-inline-block","BackCompat",200]`,
		`["quirks-flex","BackCompat",0]`,
		`["standards-auto","CSS1Compat",0]`,
		`["standards-definite","CSS1Compat",0]`,
	}
	historyTestPages(t, func(t *testing.T, p *Page) {
		// Each evaluation measures one iframe. Creating unrelated realms must
		// not consume the current case's evaluation deadline on a busy CI host.
		for index, expected := range want {
			t.Run(strconv.Itoa(index), func(t *testing.T) {
				selection := "globalThis.quirksPercentageHeightProbeIndex = " + strconv.Itoa(index) + ";\n"
				historyEval(t, p, selection+string(script), "["+expected+"]")
			})
		}
	})
}

func TestHeightDimensionProjectionMatchesFrozenChrome152(t *testing.T) {
	parallelBrowserTest(t)
	testCSSObservation(t, "css_height_projection")
}

// Dimensions require the target's own flow, not the positions of its siblings.
// This checks the architectural boundary without a machine-dependent deadline.
func TestHeightObservationKeepsUnrelatedDocumentFlowCorrect(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		navigateCapabilityFixture(t, p)
		value, err := p.Evaluate(context.Background(), `(()=>{
 document.body.innerHTML='<div id="target" style="height:120px;padding:3px;border:2px solid"></div><div style="width:300px">Unrelated sibling text whose line height belongs to the rectangle observation.</div>';
 const target=document.getElementById('target');globalThis.heightTarget=target;
 return [target.offsetHeight,target.clientHeight,getComputedStyle(target).height].join(',');
 })()`)
		if err != nil || value != "130,126,120px" {
			t.Fatalf("height observation: %v %v", value, err)
		}
		value, err = p.Evaluate(context.Background(), `String(heightTarget.getBoundingClientRect().height)`)
		if err != nil || value != "130" {
			t.Fatalf("rectangle: %v %v", value, err)
		}
	})
}

func TestDefiniteHeightSkipsDescendantFlowWithoutCachingIncompleteBoxes(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		navigateCapabilityFixture(t, p)
		value, err := p.Evaluate(context.Background(), `(()=>{
 document.body.innerHTML='<div id="heightParent" style="height:120px;padding:3px;border:2px solid"><div style="height:7px"></div><div style="height:9px"></div><div>DescendantFlowMarker text measured only when full flow is requested.</div></div>';
 globalThis.heightParent=document.getElementById('heightParent');
 const node=heightParent;return [node.offsetHeight,node.clientHeight,getComputedStyle(node).height].join(',');
 })()`)
		if err != nil || value != "130,126,120px" {
			t.Fatalf("definite height: %v %v", value, err)
		}
		if cache := p.Top.Realm.textShapeCache; cache != nil {
			for key := range cache.values {
				if strings.Contains(key.text, "DescendantFlowMarker") {
					t.Fatal("definite height eagerly shaped descendant content")
				}
			}
		}
		value, err = p.Evaluate(context.Background(), `(()=>{
 const p=heightParent,a=p.children[0],b=p.children[1],style=getComputedStyle(p);
 const initial=p.offsetHeight,dy=b.getBoundingClientRect().y-a.getBoundingClientRect().y;
 p.style.height='80px';p.style.boxSizing='border-box';
 const resized=[p.offsetHeight,p.clientHeight,style.height];
 a.style.height='17px';const changed=b.getBoundingClientRect().y-a.getBoundingClientRect().y;
 return JSON.stringify({initial,dy,resized,changed,same:a===p.firstElementChild});
 })()`)
		if err != nil || value != `{"initial":130,"dy":7,"resized":[80,76,"80px"],"changed":17,"same":true}` {
			t.Fatalf("height projection poisoned full flow: %v %v", value, err)
		}
	})
}
