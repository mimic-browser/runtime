package cdp

import (
	"testing"
)

func TestFrameControlScrollMeasureAndClickWithoutRuntime(t *testing.T) {
	s, addr := runningServer(t)
	if _, err := evaluatePageFixture(s.Page, `
document.body.style.cssText = 'margin:0;min-height:3000px';
globalThis.frame = document.createElement('iframe');
frame.style.cssText = 'position:absolute;left:40px;top:2000px;width:200px;height:100px;border:2px solid';
document.body.append(frame);
frame.contentDocument.body.style.margin = '0';
frame.contentDocument.body.innerHTML = '<button id="control" style="position:absolute;left:10px;top:10px;width:30px;height:30px" onclick="window.clicked=true">go</button>';
globalThis.scrollCalls = 0;
frame.contentDocument.getElementById('control').scrollIntoView = () => { scrollCalls++; };
true;
`); err != nil {
		t.Fatal(err)
	}
	child := s.Page.Top.Children()[0]
	document, _ := s.Page.InspectorDocument(child)
	control, ok := document.Find("#control")
	if !ok {
		t.Fatal("missing frame control")
	}
	c := browserConnection(t, addr)
	sid := wireCall(t, c, 1, "Target.attachToTarget", map[string]any{"targetId": s.Page.ID, "flatten": true})["sessionId"].(string)
	nodeID := s.Page.InspectorNodeID(child, control.ID)
	before := flatCall(t, c, sid, 2, "DOM.getBoxModel", map[string]any{"nodeId": nodeID})["model"].(map[string]any)["content"].([]any)
	flatCall(t, c, sid, 3, "DOM.scrollIntoViewIfNeeded", map[string]any{"nodeId": nodeID})
	after := flatCall(t, c, sid, 4, "DOM.getBoxModel", map[string]any{"nodeId": nodeID})["model"].(map[string]any)["content"].([]any)
	if coordinateValue(after[1]) >= coordinateValue(before[1]) {
		t.Fatalf("frame control remained offscreen: before=%v after=%v", before, after)
	}
	x, y := (coordinateValue(after[0])+coordinateValue(after[4]))/2, (coordinateValue(after[1])+coordinateValue(after[5]))/2
	for index, kind := range []string{"mousePressed", "mouseReleased"} {
		flatCall(t, c, sid, 5+index, "Input.dispatchMouseEvent", map[string]any{"type": kind, "x": x, "y": y, "button": "left", "clickCount": 1})
	}
	if got, err := evaluatePageFixture(s.Page, `scrollCalls === 0 && frame.contentWindow.clicked === true`); err != nil || got != true {
		t.Fatalf("scrolled frame measurement/input disagreement: %v %v", got, err)
	}
}

func TestFrameBoxModelIgnoresAuthorGeometryOverrides(t *testing.T) {
	s, addr := runningServer(t)
	if _, err := evaluatePageFixture(s.Page, `
document.body.style.margin = '0';
document.body.innerHTML = '<label id="label" style="position:absolute;left:400px;top:300px;width:100px;height:100px">label</label>';
globalThis.frame = document.createElement('iframe');
frame.style.cssText = 'position:absolute;left:40px;top:30px;width:100px;height:100px;border:2px solid;padding:3px';
document.body.append(frame);
frame.contentDocument.body.style.margin = '0';
frame.contentDocument.body.innerHTML = '<button id="control" style="position:absolute;left:10px;top:10px;width:30px;height:30px" onclick="window.clicked=true">go</button>';
globalThis.geometryCalls = 0;
frame.getBoundingClientRect = () => { geometryCalls++; return {x:400,y:300}; };
globalThis.getComputedStyle = () => { geometryCalls++; return {borderLeftWidth:'0',borderTopWidth:'0',paddingLeft:'0',paddingTop:'0'}; };
true;
`); err != nil {
		t.Fatal(err)
	}
	child := s.Page.Top.Children()[0]
	document, _ := s.Page.InspectorDocument(child)
	control, ok := document.Find("#control")
	if !ok {
		t.Fatal("missing frame control")
	}
	c := browserConnection(t, addr)
	sid := wireCall(t, c, 1, "Target.attachToTarget", map[string]any{"targetId": s.Page.ID, "flatten": true})["sessionId"].(string)
	nodeID := s.Page.InspectorNodeID(child, control.ID)
	for index, method := range []string{"DOM.getBoxModel", "DOM.getContentQuads"} {
		result := flatCall(t, c, sid, 2+index, method, map[string]any{"nodeId": nodeID})
		var quad []any
		if method == "DOM.getBoxModel" {
			quad = result["model"].(map[string]any)["border"].([]any)
		} else {
			quad = result["quads"].([]any)[0].([]any)
		}
		if coordinateValue(quad[0]) != 55 || coordinateValue(quad[1]) != 45 {
			t.Fatalf("%s projected through author override: %v", method, quad)
		}
	}
	for index, kind := range []string{"mousePressed", "mouseReleased"} {
		flatCall(t, c, sid, 4+index, "Input.dispatchMouseEvent", map[string]any{"type": kind, "x": 70, "y": 60, "button": "left", "clickCount": 1})
	}
	if got, err := evaluatePageFixture(s.Page, `geometryCalls === 0 && frame.contentWindow.clicked === true`); err != nil || got != true {
		t.Fatalf("frame measurement/input disagreement: %v %v", got, err)
	}
}

func TestFrameContentQuadClickUsesResolvedPadding(t *testing.T) {
	s, addr := runningServer(t)
	if _, err := evaluatePageFixture(s.Page, `
document.body.style.cssText = 'margin:0;width:400px;position:relative';
globalThis.frame = document.createElement('iframe');
frame.style.cssText = 'position:absolute;left:40px;top:30px;width:100px;height:100px;border:2px solid;padding:10%';
document.body.append(frame);
frame.contentDocument.body.style.margin = '0';
frame.contentDocument.body.innerHTML = '<button id="control" style="position:absolute;left:10px;top:10px;width:30px;height:30px" onclick="window.clicks=(window.clicks||0)+1">go</button>';
true;
`); err != nil {
		t.Fatal(err)
	}
	child := s.Page.Top.Children()[0]
	document, _ := s.Page.InspectorDocument(child)
	control, _ := document.Find("#control")
	c := browserConnection(t, addr)
	sid := wireCall(t, c, 1, "Target.attachToTarget", map[string]any{"targetId": s.Page.ID, "flatten": true})["sessionId"].(string)
	// Frozen headful Chrome 152: initial border quad starts at (92,82),
	// and clicking its center increments the child control's counter once.
	for step, mutation := range []string{
		`true`,
		`document.body.style.width = '600px'`,
		`frame.style.padding = '5%'`,
		`frame.style.cssText = 'position:absolute;left:200px;top:150px;width:100px;height:100px;border:0;padding:12px'`,
	} {
		if _, err := evaluatePageFixture(s.Page, mutation); err != nil {
			t.Fatal(err)
		}
		quad := flatCall(t, c, sid, 2+step*3, "DOM.getContentQuads", map[string]any{"nodeId": s.Page.InspectorNodeID(child, control.ID)})["quads"].([]any)[0].([]any)
		if step == 0 && (coordinateValue(quad[0]) != 92 || coordinateValue(quad[1]) != 82) {
			t.Fatalf("initial quad disagrees with Chrome 152: %v", quad)
		}
		x := (coordinateValue(quad[0]) + coordinateValue(quad[2])) / 2
		y := (coordinateValue(quad[1]) + coordinateValue(quad[5])) / 2
		for index, kind := range []string{"mousePressed", "mouseReleased"} {
			flatCall(t, c, sid, 3+step*3+index, "Input.dispatchMouseEvent", map[string]any{"type": kind, "x": x, "y": y, "button": "left", "clickCount": 1})
		}
		if got, err := evaluatePageFixture(s.Page, `frame.contentWindow.clicks`); err != nil || coordinateValue(got) != float64(step+1) {
			t.Fatalf("step %d: measured frame control missed at (%g,%g): %v %v", step, x, y, got, err)
		}
	}
}

func TestContentQuadQueryDoesNotOverrideMouseHitTarget(t *testing.T) {
	s, addr := runningServer(t)
	if _, err := evaluatePageFixture(s.Page, `document.body.innerHTML='<a id="link" style="position:absolute;left:0;top:0;width:100px;height:100px"><span id="child" style="display:block;width:100px;height:100px">open</span></a>';globalThis.clicked='';document.addEventListener('click',e=>clicked=e.target.id);`); err != nil {
		t.Fatal(err)
	}
	d, _ := s.Page.Document()
	link, _ := d.Find("#link")
	c := browserConnection(t, addr)
	sid := wireCall(t, c, 1, "Target.attachToTarget", map[string]any{"targetId": s.Page.ID, "flatten": true})["sessionId"].(string)
	flatCall(t, c, sid, 2, "DOM.getContentQuads", map[string]any{"nodeId": link.ID})
	flatCall(t, c, sid, 3, "Input.dispatchMouseEvent", map[string]any{"type": "mousePressed", "x": 30, "y": 30, "button": "left", "clickCount": 1})
	flatCall(t, c, sid, 4, "Input.dispatchMouseEvent", map[string]any{"type": "mouseReleased", "x": 30, "y": 30, "button": "left", "clickCount": 1})
	if got, err := evaluatePageFixture(s.Page, `clicked`); err != nil || got != "child" {
		t.Fatalf("quad query changed event target: %v %v", got, err)
	}
	if _, err := evaluatePageFixture(s.Page, `document.body.insertAdjacentHTML('beforeend','<div id="cover" style="position:fixed;left:0;top:0;width:100px;height:100px;z-index:10"></div>')`); err != nil {
		t.Fatal(err)
	}
	flatCall(t, c, sid, 5, "Input.dispatchMouseEvent", map[string]any{"type": "mousePressed", "x": 30, "y": 30, "button": "left", "clickCount": 1})
	flatCall(t, c, sid, 6, "Input.dispatchMouseEvent", map[string]any{"type": "mouseReleased", "x": 30, "y": 30, "button": "left", "clickCount": 1})
	if got, err := evaluatePageFixture(s.Page, `clicked`); err != nil || got != "cover" {
		t.Fatalf("quad query bypassed overlay: %v %v", got, err)
	}
}
