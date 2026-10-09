package cdp

import "testing"

func findInspectorNode(value any, attribute, name string) map[string]any {
	switch node := value.(type) {
	case map[string]any:
		if attrs, ok := node["attributes"].([]any); ok {
			for index := 0; index+1 < len(attrs); index += 2 {
				if attrs[index] == attribute && attrs[index+1] == name {
					return node
				}
			}
		}
		for _, key := range []string{"children", "shadowRoots", "contentDocument"} {
			if found := findInspectorNode(node[key], attribute, name); found != nil {
				return found
			}
		}
	case []any:
		for _, child := range node {
			if found := findInspectorNode(child, attribute, name); found != nil {
				return found
			}
		}
	}
	return nil
}

func TestInspectorFramesAndShadowEditing(t *testing.T) {
	s, addr := runningServer(t)
	_, err := evaluatePageFixture(s.Page, `document.body.innerHTML='<div id="top"></div><div id="host"></div><iframe></iframe>'; const child=document.querySelector('iframe').contentDocument;child.head.innerHTML='<style>#child {color: red}</style>';child.body.innerHTML='<div id="child">frame</div>';const root=document.querySelector('#host').attachShadow({mode:'closed'});root.innerHTML='<style>#shadow {color: blue}</style><span id="shadow">inside</span>'`)
	if err != nil {
		t.Fatal(err)
	}
	w := newInspectorWire(t, addr)
	id := w.call(t, "", "Target.attachToTarget", map[string]any{"targetId": s.Page.ID, "flatten": true})["sessionId"].(string)
	root := w.call(t, id, "DOM.getDocument", map[string]any{"depth": -1, "pierce": true})["root"]
	child := findInspectorNode(root, "id", "child")
	shadow := findInspectorNode(root, "id", "shadow")
	if child == nil || shadow == nil {
		t.Fatalf("missing frame/shadow nodes: child=%v shadow=%v", child, shadow)
	}
	if child["nodeId"] == shadow["nodeId"] {
		t.Fatal("frame node identity collided with top document")
	}
	w.call(t, id, "CSS.enable", map[string]any{})
	value, debugErr := evaluatePageFixture(s.Page, `JSON.stringify({child:document.querySelector('iframe').contentWindow.getComputedStyle(document.querySelector('iframe').contentDocument.querySelector('#child')).color,shadow:getComputedStyle(root.querySelector('#shadow')).color})`)
	if debugErr != nil || value != `{"child":"rgb(255, 0, 0)","shadow":"rgb(0, 0, 255)"}` {
		t.Fatalf("scoped canonical styles: %v %v", value, debugErr)
	}
	for _, node := range []map[string]any{child, shadow} {
		w.call(t, id, "DOM.setAttributeValue", map[string]any{"nodeId": node["nodeId"], "name": "title", "value": "edited"})
		attrs := w.call(t, id, "DOM.getAttributes", map[string]any{"nodeId": node["nodeId"]})["attributes"].([]any)
		found := false
		for index := 0; index+1 < len(attrs); index += 2 {
			if attrs[index] == "title" && attrs[index+1] == "edited" {
				found = true
			}
		}
		if !found {
			t.Fatalf("wrong document edited: %#v", attrs)
		}
		matched := w.call(t, id, "CSS.getMatchedStylesForNode", map[string]any{"nodeId": node["nodeId"]})
		rules := matched["matchedCSSRules"].([]any)
		if len(rules) != 1 {
			t.Fatalf("scoped rules for %v: %#v", node["attributes"], matched)
		}
		resolved := w.call(t, id, "DOM.resolveNode", map[string]any{"nodeId": node["nodeId"]})["object"].(map[string]any)
		value := w.call(t, id, "Runtime.callFunctionOn", map[string]any{"objectId": resolved["objectId"], "functionDeclaration": "function(){return this.id}", "returnByValue": true})["result"].(map[string]any)["value"]
		if value != "child" && value != "shadow" {
			t.Fatalf("resolved wrong realm: %v", value)
		}
	}
}
