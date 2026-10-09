package cdp

import (
	"fmt"
	"testing"
)

func TestInspectorCommandLineChildSelection(t *testing.T) {
	s, addr := runningServer(t)
	w, id := inspectorSession(t, s, addr)
	w.call(t, id, "Runtime.enable", map[string]any{})
	context := w.event(t, "Runtime.executionContextCreated")["context"].(map[string]any)["id"]
	w.call(t, id, "Runtime.evaluate", map[string]any{"expression": `document.body.innerHTML='<iframe></iframe>';document.querySelector('iframe').contentDocument.body.innerHTML='<div id="child">child</div>'`})
	root := w.call(t, id, "DOM.getDocument", map[string]any{"depth": -1})["root"]
	node := findInspectorNode(root, "id", "child")
	w.call(t, id, "DOM.setInspectedNode", map[string]any{"nodeId": node["nodeId"]})
	result := w.call(t, id, "Runtime.evaluate", map[string]any{"expression": `$0===document.querySelector('iframe').contentDocument.querySelector('#child')`, "includeCommandLineAPI": true, "returnByValue": true})
	if result["exceptionDetails"] != nil || result["result"].(map[string]any)["value"] != true {
		t.Fatalf("child selection did not cross the connected execution owner: %v", result)
	}
	object := w.call(t, id, "Runtime.evaluate", map[string]any{"expression": "$0", "includeCommandLineAPI": true})["result"].(map[string]any)
	requested := w.call(t, id, "DOM.requestNode", map[string]any{"objectId": object["objectId"]})
	if requested["nodeId"] != node["nodeId"] {
		t.Fatalf("foreign Console object lost node owner: %v", requested)
	}
	markup := w.call(t, id, "DOM.getOuterHTML", map[string]any{"objectId": object["objectId"]})
	if markup["outerHTML"] != `<div id="child">child</div>` {
		t.Fatalf("foreign node serialization: %v", markup)
	}
	byObject := w.call(t, id, "DOM.getBoxModel", map[string]any{"objectId": object["objectId"]})
	byNode := w.call(t, id, "DOM.getBoxModel", map[string]any{"nodeId": node["nodeId"]})
	if fmt.Sprint(byObject) != fmt.Sprint(byNode) {
		t.Fatalf("foreign object geometry used wrong document: %v != %v", byObject, byNode)
	}
	resolved := w.call(t, id, "DOM.resolveNode", map[string]any{"nodeId": node["nodeId"], "executionContextId": context})["object"].(map[string]any)
	if requested := w.call(t, id, "DOM.requestNode", map[string]any{"objectId": resolved["objectId"]}); requested["nodeId"] != node["nodeId"] {
		t.Fatalf("explicit parent-context resolution lost node owner: %v", requested)
	}
}
