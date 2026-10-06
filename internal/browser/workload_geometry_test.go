package browser

import (
	"context"
	"testing"
)

func TestIsolatedPercentStyleReadsOwnGeometryObservation(t *testing.T) {
	serialBrowserTest(t)
	p := blitzStandardsPage(t)
	d := NewDebugger(p)
	defer d.Close()
	debuggerEval(t, d, `
document.body.innerHTML = '<div style="width:200px">' +
  '<button id="probe" style="padding-left:10%;width:calc(50% + 1px);transform:translateX(10%)">Buy</button>' +
  '</div>';
`, DebuggerOptions{})
	world, err := p.IsolatedWorld(context.Background(), p.Top.ID, "isolated-percentage")
	if err != nil {
		t.Fatal(err)
	}
	result, err := d.Evaluate(context.Background(), p.Top.ID, world, `(() => {
  const element = document.getElementById('probe');
  const style = getComputedStyle(element);
  return JSON.stringify([style.paddingLeft, style.width, element.getBoundingClientRect().width]);
})()`, DebuggerOptions{ReturnByValue: true})
	if err != nil || result["exceptionDetails"] != nil {
		t.Fatalf("isolated resolved geometry: %#v %v", result, err)
	}
}

func TestIsolatedNativeAdmissionReadsShadowHostWithinObservation(t *testing.T) {
	serialBrowserTest(t)
	p := blitzStandardsPage(t)
	d := NewDebugger(p)
	defer d.Close()
	debuggerEval(t, d, `
document.body.innerHTML = '<button id="probe">Buy</button>' +
  '<div id="shadow" style="width:0;height:0"></div>';
document.getElementById('shadow').attachShadow({mode: 'open'}).innerHTML = '<span>badge</span>';
`, DebuggerOptions{})
	world, err := p.IsolatedWorld(context.Background(), p.Top.ID, "isolated-shadow")
	if err != nil {
		t.Fatal(err)
	}
	result, err := d.Evaluate(context.Background(), p.Top.ID, world, `document.getElementById('probe').getBoundingClientRect().width`, DebuggerOptions{ReturnByValue: true})
	if err != nil || result["exceptionDetails"] != nil {
		t.Fatalf("isolated native shadow admission: %#v %v", result, err)
	}
}
