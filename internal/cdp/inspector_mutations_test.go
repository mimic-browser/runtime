package cdp

import "testing"

func TestInspectorShadowAttachmentAndChildUpdates(t *testing.T) {
	s, addr := runningServer(t)
	_, err := evaluatePageFixture(s.Page, `document.body.innerHTML='<div id="host"></div>'`)
	if err != nil {
		t.Fatal(err)
	}
	w, id := inspectorSession(t, s, addr)
	w.call(t, id, "DOM.getDocument", map[string]any{"depth": -1})
	w.call(t, id, "Runtime.evaluate", map[string]any{"expression": `globalThis.root=document.querySelector('#host').attachShadow({mode:'closed'});root.innerHTML='<span id="shadow">initial</span>'`})
	pushed := w.event(t, "DOM.shadowRootPushed")
	root := pushed["root"].(map[string]any)
	if root["nodeType"] != float64(11) || root["shadowRootType"] != "closed" {
		t.Fatalf("shadow root projection: %v", root)
	}
	w.call(t, id, "DOM.requestChildNodes", map[string]any{"nodeId": root["nodeId"], "depth": -1})
	children := w.event(t, "DOM.setChildNodes")
	node := findInspectorNode(children["nodes"], "id", "shadow")
	if node == nil {
		t.Fatalf("shadow children missing: %v", children)
	}
	queried := w.call(t, id, "DOM.querySelector", map[string]any{"nodeId": root["nodeId"], "selector": "#shadow"})
	if queried["nodeId"] != node["nodeId"] {
		t.Fatal("shadow root query lost identity")
	}
	w.call(t, id, "Runtime.evaluate", map[string]any{"expression": `root.querySelector('#shadow').title='updated'`})
	event := w.event(t, "DOM.attributeModified")
	if event["nodeId"] != node["nodeId"] || event["value"] != "updated" {
		t.Fatalf("shadow mutation identity: %v", event)
	}
	resolved := w.call(t, id, "DOM.resolveNode", map[string]any{"nodeId": root["nodeId"]})["object"].(map[string]any)
	result := w.call(t, id, "Runtime.callFunctionOn", map[string]any{"objectId": resolved["objectId"], "functionDeclaration": "function(){return this===root && this.host.id==='host'}", "returnByValue": true})
	if result["result"].(map[string]any)["value"] != true {
		t.Fatalf("shadow root wrapper lost canonical identity: %v", result)
	}
}

func TestInspectorChildFrameNotificationsAndSerialization(t *testing.T) {
	s, addr := runningServer(t)
	_, err := evaluatePageFixture(s.Page, `document.body.innerHTML='<iframe></iframe>';document.querySelector('iframe').contentDocument.body.innerHTML='<div id="child">initial</div>'`)
	if err != nil {
		t.Fatal(err)
	}
	w, id := inspectorSession(t, s, addr)
	root := w.call(t, id, "DOM.getDocument", map[string]any{"depth": -1})["root"]
	node := findInspectorNode(root, "id", "child")
	w.call(t, id, "Runtime.evaluate", map[string]any{"expression": `document.querySelector('iframe').contentDocument.querySelector('#child').title='child title'`})
	event := w.event(t, "DOM.attributeModified")
	if event["nodeId"] != node["nodeId"] || event["value"] != "child title" {
		t.Fatalf("child mutation bound to top document: %v", event)
	}
	markup := w.call(t, id, "DOM.getOuterHTML", map[string]any{"nodeId": node["nodeId"]})["outerHTML"]
	if markup != `<div id="child" title="child title">initial</div>` {
		t.Fatalf("child serialization: %v", markup)
	}
	w.call(t, id, "Runtime.evaluate", map[string]any{"expression": `document.querySelector('iframe').remove()`})
	if reply := w.request(t, id, "DOM.describeNode", map[string]any{"nodeId": node["nodeId"]}); reply["error"] == nil {
		t.Fatalf("detached frame handle accepted: %v", reply)
	}
}

func TestInspectorQueriesPreservePublishedTreeIdentity(t *testing.T) {
	s, addr := runningServer(t)
	w, id := inspectorSession(t, s, addr)
	w.call(t, id, "Runtime.evaluate", map[string]any{"expression": `document.body.innerHTML='<main><button id="probe">selected</button></main>'`})
	root := w.call(t, id, "DOM.getDocument", map[string]any{"depth": -1})["root"].(map[string]any)
	for attempt := 0; attempt < 3; attempt++ {
		w.call(t, id, "DOM.querySelector", map[string]any{"nodeId": root["nodeId"], "selector": "#probe"})
	}
	// The reply barrier includes all events queued by the previous commands.
	for {
		select {
		case event := <-w.events:
			if event["method"] == "DOM.setChildNodes" {
				t.Fatalf("query replaced an already published tree: %v", event)
			}
		default:
			return
		}
	}
}
