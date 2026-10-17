package cdp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLogicalInsetKeepsFixedMenuOutsideFrameHitTarget(t *testing.T) {
	s, addr := runningServer(t)
	if _, err := evaluatePageFixture(s.Page, `
document.body.style.margin = '0';
document.body.innerHTML = '<div id="menu" style="position:fixed;top:0;bottom:0;inset-inline-end:-280px;width:280px;z-index:30"></div>';
globalThis.frame = document.createElement('iframe');
frame.style.cssText = 'position:absolute;left:40px;top:30px;width:100px;height:100px';
document.body.append(frame);
frame.contentDocument.body.style.margin = '0';
frame.contentDocument.body.innerHTML = '<button id="control" style="position:absolute;left:10px;top:10px;width:30px;height:30px" onclick="window.clicked=true">go</button>';
true;
`); err != nil {
		t.Fatal(err)
	}
	if got, err := evaluatePageFixture(s.Page, `(()=>{const menu=document.getElementById('menu');return {hit:document.elementFromPoint(70,60)?.tagName,right:getComputedStyle(menu).right,logical:getComputedStyle(menu).insetInlineEnd,x:menu.getBoundingClientRect().x}})()`); err != nil || got == nil {
		t.Fatalf("failed to inspect fixed menu: %v %v", got, err)
	} else if hit, _ := got.(map[string]any); hit["hit"] != "IFRAME" {
		t.Fatalf("offscreen fixed menu covered iframe: %v", got)
	} else if hit["right"] != "-280px" || hit["logical"] != "-280px" {
		t.Fatalf("logical inset did not project to the physical side: %v", got)
	}
	if got, err := evaluatePageFixture(s.Page, `(()=>{const menu=document.getElementById('menu');menu.style.direction='rtl';return {right:menu.getBoundingClientRect().right,hit:document.elementFromPoint(70,60)?.tagName}})()`); err != nil || got == nil {
		t.Fatalf("failed to inspect RTL fixed menu: %v %v", got, err)
	} else if result, _ := got.(map[string]any); result["right"] != float64(0) || result["hit"] != "IFRAME" {
		t.Fatalf("RTL logical inset covered iframe: %v", got)
	}
	child := s.Page.Top.Children()[0]
	document, _ := s.Page.InspectorDocument(child)
	control, ok := document.Find("#control")
	if !ok {
		t.Fatal("missing frame control")
	}
	c := browserConnection(t, addr)
	sid := wireCall(t, c, 1, "Target.attachToTarget", map[string]any{"targetId": s.Page.ID, "flatten": true})["sessionId"].(string)
	quad := flatCall(t, c, sid, 2, "DOM.getBoxModel", map[string]any{"nodeId": s.Page.InspectorNodeID(child, control.ID)})["model"].(map[string]any)["content"].([]any)
	x, y := (coordinateValue(quad[0])+coordinateValue(quad[4]))/2, (coordinateValue(quad[1])+coordinateValue(quad[5]))/2
	for index, kind := range []string{"mousePressed", "mouseReleased"} {
		flatCall(t, c, sid, 3+index, "Input.dispatchMouseEvent", map[string]any{"type": kind, "x": x, "y": y, "button": "left", "clickCount": 1})
	}
	if got, err := evaluatePageFixture(s.Page, `frame.contentWindow.clicked === true`); err != nil || got != true {
		t.Fatalf("offscreen menu swallowed measured frame click: %v %v", got, err)
	}
}

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
	scrollY, err := evaluatePageFixture(s.Page, `scrollY`)
	if err != nil {
		t.Fatal(err)
	}
	hit := flatCall(t, c, sid, 50, "DOM.getNodeForLocation", map[string]any{"x": int(x), "y": int(y) + int(coordinateValue(scrollY))})
	if coordinateValue(hit["backendNodeId"]) != float64(nodeID) || hit["frameId"] != child.ID {
		t.Fatalf("scrolled frame hit: %v", hit)
	}
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

func TestNodeForLocationGeometryAndNativeClick(t *testing.T) {
	s, addr := runningServer(t)
	if _, err := evaluatePageFixture(s.Page, `document.body.style.margin='0';document.body.innerHTML='<button id="target" style="position:absolute;left:20px;top:20px;width:100px;height:50px" onclick="this.dataset.clicked=Number(this.dataset.clicked||0)+1">go</button><div id="cover" style="position:absolute;left:20px;top:20px;width:100px;height:50px;z-index:10;pointer-events:none"></div>';document.elementFromPoint=()=>{throw Error('author override')};`); err != nil {
		t.Fatal(err)
	}
	d, _ := s.Page.Document()
	target, _ := d.Find("#target")
	cover, _ := d.Find("#cover")
	c := browserConnection(t, addr)
	sid := wireCall(t, c, 1, "Target.attachToTarget", map[string]any{"targetId": s.Page.ID, "flatten": true})["sessionId"].(string)
	next := 2
	call := func(method string, params map[string]any) map[string]any {
		next++
		return flatCall(t, c, sid, next, method, params)
	}
	hit := call("DOM.getNodeForLocation", map[string]any{"x": 40, "y": 40})
	if coordinateValue(hit["backendNodeId"]) != float64(target.ID) || hit["frameId"] != s.Page.Top.ID || hit["nodeId"] != nil {
		t.Fatalf("initial hit: %v", hit)
	}
	call("DOM.describeNode", map[string]any{"backendNodeId": target.ID})
	hit = call("DOM.getNodeForLocation", map[string]any{"x": 40, "y": 40})
	if hit["nodeId"] != nil {
		t.Fatalf("describeNode created a frontend document: %v", hit)
	}
	call("DOM.getDocument", map[string]any{})
	for _, state := range []struct {
		style  string
		ignore bool
		want   int64
	}{
		{"pointer-events:none", false, target.ID},
		{"pointer-events:none", true, cover.ID},
		{"pointer-events:auto", false, cover.ID},
		{"visibility:hidden", false, target.ID},
		{"display:none", false, target.ID},
	} {
		if _, err := evaluatePageFixture(s.Page, `document.getElementById('cover').style.cssText='position:absolute;left:20px;top:20px;width:100px;height:50px;z-index:10;`+state.style+`'`); err != nil {
			t.Fatal(err)
		}
		got := call("DOM.getNodeForLocation", map[string]any{"x": 40, "y": 40, "ignorePointerEventsNone": state.ignore})
		if coordinateValue(got["backendNodeId"]) != float64(state.want) || coordinateValue(got["nodeId"]) != float64(state.want) {
			t.Fatalf("%+v: %v", state, got)
		}
	}
	next++
	otherSID := wireCall(t, c, next, "Target.attachToTarget", map[string]any{"targetId": s.Page.ID, "flatten": true})["sessionId"].(string)
	next++
	otherHit := flatCall(t, c, otherSID, next, "DOM.getNodeForLocation", map[string]any{"x": 40, "y": 40})
	if otherHit["nodeId"] != nil || coordinateValue(otherHit["backendNodeId"]) != float64(target.ID) {
		t.Fatalf("session isolation: %v", otherHit)
	}
	box := call("DOM.getBoxModel", map[string]any{"nodeId": target.ID})["model"].(map[string]any)["border"].([]any)
	quad := call("DOM.getContentQuads", map[string]any{"nodeId": target.ID})["quads"].([]any)[0].([]any)
	for i := range box {
		if coordinateValue(box[i]) != coordinateValue(quad[i]) {
			t.Fatalf("box/quad disagreement: %v %v", box, quad)
		}
	}
	x, y := (coordinateValue(quad[0])+coordinateValue(quad[4]))/2, (coordinateValue(quad[1])+coordinateValue(quad[5]))/2
	got := call("DOM.getNodeForLocation", map[string]any{"x": x, "y": y})
	if coordinateValue(got["backendNodeId"]) != float64(target.ID) {
		t.Fatalf("measured hit: %v", got)
	}
	for _, kind := range []string{"mouseMoved", "mousePressed", "mouseReleased"} {
		call("Input.dispatchMouseEvent", map[string]any{"type": kind, "x": x, "y": y, "button": "left", "clickCount": 1})
	}
	if got, err := evaluatePageFixture(s.Page, `document.getElementById('target').dataset.clicked`); err != nil || got != "1" {
		t.Fatalf("native click: %v %v", got, err)
	}
}

func TestNodeForLocationNestedCrossOriginFrames(t *testing.T) {
	leaf := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><body style="margin:0;height:1200px"><button id="control" style="position:absolute;left:10px;top:220px;width:60px;height:40px" onclick="this.dataset.clicked='yes'">go</button>`)
	}))
	defer leaf.Close()
	middle := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<!doctype html><body style="margin:0"><iframe src="%s" onload="document.body.dataset.childLoaded='yes'" style="position:absolute;left:10px;top:10px;width:180px;height:120px;border:2px solid"></iframe><script>onload=()=>document.body.dataset.ordered=document.body.dataset.childLoaded||'no'</script>`, leaf.URL)
	}))
	defer middle.Close()
	top := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<!doctype html><body style="margin:0;height:2000px"><iframe src="%s" style="position:absolute;left:30px;top:600px;width:240px;height:180px;border:3px solid"></iframe>`, middle.URL)
	}))
	defer top.Close()
	s, addr := runningServer(t)
	s.Page.Trace().Start()
	c := browserConnection(t, addr)
	sid := wireCall(t, c, 1, "Target.attachToTarget", map[string]any{"targetId": s.Page.ID, "flatten": true})["sessionId"].(string)
	flatCall(t, c, sid, 80, "Page.navigate", map[string]any{"url": top.URL})
	flatCall(t, c, sid, 83, "Runtime.evaluate", map[string]any{"expression": "scrollTo(0,500)"})
	deadline := time.Now().Add(10 * time.Second)
	for len(s.Page.Top.Children()) != 1 || len(s.Page.Top.Children()[0].Children()) != 1 {
		if time.Now().After(deadline) {
			t.Fatalf("nested frames not loaded: top=%d tree=%v", len(s.Page.Top.Children()), flatCall(t, c, sid, 82, "Page.getFrameTree", nil))
		}
		flatCall(t, c, sid, 81, "Runtime.evaluate", map[string]any{"expression": "true"})
		time.Sleep(10 * time.Millisecond)
	}
	child := s.Page.Top.Children()[0].Children()[0]
	document, _ := s.Page.InspectorDocument(child)
	control, ok := document.Find("#control")
	for !ok {
		if time.Now().After(deadline) {
			t.Fatal("leaf document did not load")
		}
		flatCall(t, c, sid, 84, "Runtime.evaluate", map[string]any{"expression": "true"})
		time.Sleep(10 * time.Millisecond)
		document, _ = s.Page.InspectorDocument(child)
		control, ok = document.Find("#control")
	}

	id := s.Page.InspectorNodeID(child, control.ID)
	flatCall(t, c, sid, 2, "DOM.scrollIntoViewIfNeeded", map[string]any{"backendNodeId": id})
	quad := flatCall(t, c, sid, 3, "DOM.getContentQuads", map[string]any{"backendNodeId": id})["quads"].([]any)[0].([]any)
	x, y := int((coordinateValue(quad[0])+coordinateValue(quad[4]))/2), int((coordinateValue(quad[1])+coordinateValue(quad[5]))/2)
	scrollY, err := evaluatePageFixture(s.Page, `scrollY`)
	if err != nil {
		t.Fatal(err)
	}
	hit := flatCall(t, c, sid, 4, "DOM.getNodeForLocation", map[string]any{"x": x, "y": y + int(coordinateValue(scrollY))})
	if coordinateValue(hit["backendNodeId"]) != float64(id) || hit["frameId"] != child.ID {
		t.Fatalf("nested frame hit: %v quad=%v", hit, quad)
	}
	for index, kind := range []string{"mouseMoved", "mousePressed", "mouseReleased"} {
		flatCall(t, c, sid, 5+index, "Input.dispatchMouseEvent", map[string]any{"type": kind, "x": x, "y": y, "button": "left", "clickCount": 1})
	}
	for {
		ready := flatCall(t, c, sid, 85, "Runtime.evaluate", map[string]any{"expression": "document.readyState", "returnByValue": true})["result"].(map[string]any)["value"]
		if ready == "complete" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("nested document load did not complete")
		}
		time.Sleep(10 * time.Millisecond)
	}
	s.Page.LockCommands()
	ordered, orderedErr := s.Page.EvaluateFrame(context.Background(), child.Parent().ID, `document.body.dataset.ordered`)
	s.Page.UnlockCommands()
	if orderedErr != nil || ordered != "yes" {
		t.Fatalf("parent load preceded child owner load: %v %v", ordered, orderedErr)
	}
	loads := map[string]uint64{}
	for _, event := range s.Page.Trace().Events() {
		if event.Name == "load" {
			id, _ := event.Data["frameId"].(string)
			loads[id] = event.Sequence
		}
	}
	if loads[child.ID] == 0 || loads[child.ID] >= loads[child.Parent().ID] || loads[child.Parent().ID] >= loads[s.Page.Top.ID] {
		t.Fatalf("nested load order: %v", loads)
	}
	node, _ := document.Get(control.ID)
	if node.Attributes["data-clicked"] != "yes" {
		t.Fatalf("native cross-origin click not delivered: %v", node.Attributes)
	}
}

func TestNodeForLocationAuthorShadowRoot(t *testing.T) {
	s, addr := runningServer(t)
	if _, err := evaluatePageFixture(s.Page, `document.body.innerHTML='<div id="host" style="position:absolute;left:20px;top:20px;width:100px;height:100px"></div>';const root=document.getElementById('host').attachShadow({mode:'closed'});root.innerHTML='<button style="width:100px;height:100px" onclick="this.dataset.clicked=1">go</button>';globalThis.shadowButton=root.firstChild;`); err != nil {
		t.Fatal(err)
	}
	c := browserConnection(t, addr)
	sid := wireCall(t, c, 1, "Target.attachToTarget", map[string]any{"targetId": s.Page.ID, "flatten": true})["sessionId"].(string)
	first := flatCall(t, c, sid, 2, "DOM.getNodeForLocation", map[string]any{"x": 40, "y": 40})
	second := flatCall(t, c, sid, 3, "DOM.getNodeForLocation", map[string]any{"x": 40, "y": 40, "includeUserAgentShadowDOM": true})
	if first["backendNodeId"] != second["backendNodeId"] {
		t.Fatalf("author shadow changed with UA flag: %v %v", first, second)
	}
	described := flatCall(t, c, sid, 4, "DOM.describeNode", map[string]any{"backendNodeId": first["backendNodeId"]})["node"].(map[string]any)
	if described["nodeName"] != "BUTTON" {
		t.Fatalf("shadow target: %v", described)
	}
	for index, kind := range []string{"mousePressed", "mouseReleased"} {
		flatCall(t, c, sid, 5+index, "Input.dispatchMouseEvent", map[string]any{"type": kind, "x": 40, "y": 40, "button": "left", "clickCount": 1})
	}
	if got, err := evaluatePageFixture(s.Page, `shadowButton.dataset.clicked`); err != nil || got != "1" {
		t.Fatalf("shadow click: %v %v", got, err)
	}
}

func TestNodeForLocationDocumentCoordinatesAfterWheel(t *testing.T) {
	s, addr := runningServer(t)
	if _, err := evaluatePageFixture(s.Page, `document.body.style.margin='0';document.body.innerHTML='<div style="height:3000px"></div><button id="target" style="position:absolute;left:20px;top:1200px;width:100px;height:50px" onclick="this.dataset.clicked=1">go</button>';scrollTo(0,1000);`); err != nil {
		t.Fatal(err)
	}
	d, _ := s.Page.Document()
	target, _ := d.Find("#target")
	c := browserConnection(t, addr)
	sid := wireCall(t, c, 1, "Target.attachToTarget", map[string]any{"targetId": s.Page.ID, "flatten": true})["sessionId"].(string)
	flatCall(t, c, sid, 2, "Input.dispatchMouseEvent", map[string]any{"type": "mouseWheel", "x": 300, "y": 50, "deltaX": 0, "deltaY": 100})
	metrics := flatCall(t, c, sid, 3, "Page.getLayoutMetrics", nil)
	layout := metrics["cssLayoutViewport"].(map[string]any)
	visual := metrics["cssVisualViewport"].(map[string]any)
	if coordinateValue(layout["pageY"]) != 1100 || coordinateValue(visual["pageY"]) != 1100 {
		t.Fatalf("wheel metrics: %v", metrics)
	}
	quad := flatCall(t, c, sid, 4, "DOM.getContentQuads", map[string]any{"backendNodeId": target.ID})["quads"].([]any)[0].([]any)
	if coordinateValue(quad[1]) != 100 {
		t.Fatalf("scrolled viewport quad: %v", quad)
	}
	hit := flatCall(t, c, sid, 5, "DOM.getNodeForLocation", map[string]any{"x": 40, "y": 1220})
	if coordinateValue(hit["backendNodeId"]) != float64(target.ID) {
		t.Fatalf("document hit: %v", hit)
	}
	if got, err := evaluatePageFixture(s.Page, `document.elementFromPoint(40,120).id==='target'&&document.elementsFromPoint(40,120)[0].id==='target'`); err != nil || got != true {
		t.Fatalf("runtime viewport hit: %v %v", got, err)
	}
	for index, kind := range []string{"mousePressed", "mouseReleased"} {
		flatCall(t, c, sid, 6+index, "Input.dispatchMouseEvent", map[string]any{"type": kind, "x": 40, "y": 120, "button": "left", "clickCount": 1})
	}
	if got, err := evaluatePageFixture(s.Page, `document.getElementById('target').dataset.clicked`); err != nil || got != "1" {
		t.Fatalf("scrolled native click: %v %v", got, err)
	}
}

func TestNodeForLocationOutsideViewport(t *testing.T) {
	s, addr := runningServer(t)
	w, id := inspectorSession(t, s, addr)
	response := w.request(t, id, "DOM.getNodeForLocation", map[string]any{"x": -1, "y": -1})
	err, ok := response["error"].(map[string]any)
	if !ok || err["message"] != "No node found at given location" {
		t.Fatalf("outside hit: %v", response)
	}
}
