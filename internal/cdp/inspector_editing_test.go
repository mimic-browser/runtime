package cdp

import "testing"

func TestInspectorElementsEditingCommands(t *testing.T) {
	for _, test := range []struct {
		name, selector, method string
		params                 map[string]any
		verify                 string
	}{
		{"attribute", "#probe", "DOM.setAttributeValue", map[string]any{"name": "title", "value": "Ω"}, `document.querySelector('#probe').title==='Ω'`},
		{"attributesText", "#probe", "DOM.setAttributesAsText", map[string]any{"name": "id", "text": `id="changed" data-note="a &amp; b"`}, `document.querySelector('#changed').getAttribute('data-note')==='a & b'`},
		{"removeAttribute", "#probe", "DOM.removeAttribute", map[string]any{"name": "title"}, `!document.querySelector('#probe').hasAttribute('title')`},
		{"text", "#probe", "DOM.setNodeValue", map[string]any{"value": "edited Ω"}, `document.querySelector('#probe').textContent==='edited Ω'`},
		{"outerHTML", "#probe", "DOM.setOuterHTML", map[string]any{"outerHTML": `<section id="changed"><b>edited</b></section>`}, `document.querySelector('#changed').firstChild.textContent==='edited'&&!document.querySelector('#probe')`},
		{"removeNode", "#probe", "DOM.removeNode", map[string]any{}, `!document.querySelector('#probe')`},
		{"focusAndType", "#field", "DOM.focus", map[string]any{}, `document.activeElement.id==='field'&&document.querySelector('#field').value==='typed Ω'`},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, addr := runningServer(t)
			_, err := evaluatePageFixture(s.Page, `document.body.innerHTML='<div id="probe" title="old">hello</div><input id="field">'`)
			if err != nil {
				t.Fatal(err)
			}
			w, id := inspectorSession(t, s, addr)
			root := w.call(t, id, "DOM.getDocument", map[string]any{"depth": -1})["root"].(map[string]any)
			node := findInspectorNode(root, "id", test.selector[1:])
			if test.name == "text" {
				node = node["children"].([]any)[0].(map[string]any)
			}
			test.params["nodeId"] = node["nodeId"]
			w.call(t, id, test.method, test.params)
			if test.name == "focusAndType" {
				w.call(t, id, "Input.insertText", map[string]any{"text": "typed Ω"})
			}
			result := w.call(t, id, "Runtime.evaluate", map[string]any{"expression": test.verify, "returnByValue": true})
			if result["result"].(map[string]any)["value"] != true {
				t.Fatalf("canonical edit: %v", result)
			}
		})
	}
}

func TestInspectorConsoleObjectsPromisesAndExceptions(t *testing.T) {
	s, addr := runningServer(t)
	w, id := inspectorSession(t, s, addr)
	w.call(t, id, "Runtime.enable", map[string]any{})
	w.call(t, id, "Runtime.evaluate", map[string]any{"expression": `globalThis.getterReads=0;globalThis.inspected={value:42,get secret(){getterReads++;return 'secret'}};console.log('message',inspected)`})
	event := w.event(t, "Runtime.consoleAPICalled")
	args := event["args"].([]any)
	if args[0].(map[string]any)["value"] != "message" {
		t.Fatalf("console arguments: %v", event)
	}
	objectID := args[1].(map[string]any)["objectId"]
	properties := w.call(t, id, "Runtime.getProperties", map[string]any{"objectId": objectID, "ownProperties": true})["result"].([]any)
	foundValue, foundGetter := false, false
	for _, v := range properties {
		p := v.(map[string]any)
		if p["name"] == "value" {
			foundValue = p["value"].(map[string]any)["value"] == float64(42)
		}
		if p["name"] == "secret" {
			foundGetter = p["get"] != nil
		}
	}
	if !foundValue || !foundGetter {
		t.Fatalf("property descriptors: %v", properties)
	}
	value := w.call(t, id, "Runtime.evaluate", map[string]any{"expression": "getterReads", "returnByValue": true})["result"].(map[string]any)["value"]
	if value != float64(0) {
		t.Fatalf("inspection invoked getter: %v", value)
	}
	value = w.call(t, id, "Runtime.evaluate", map[string]any{"expression": "Promise.resolve({answer:42})", "awaitPromise": true, "returnByValue": true})["result"].(map[string]any)["value"]
	if value.(map[string]any)["answer"] != float64(42) {
		t.Fatalf("awaited promise: %v", value)
	}
	result := w.call(t, id, "Runtime.evaluate", map[string]any{"expression": "throw new Error('inspector failure')"})
	if result["exceptionDetails"] == nil {
		t.Fatalf("exception lost: %v", result)
	}
	w.call(t, id, "Runtime.discardConsoleEntries", map[string]any{})
	if response := w.request(t, id, "Runtime.getProperties", map[string]any{"objectId": objectID}); response["error"] == nil {
		t.Fatalf("console object retained after discard: %v", response)
	}
}

func TestInspectorEagerEvaluationDoesNotExecute(t *testing.T) {
	s, addr := runningServer(t)
	w, id := inspectorSession(t, s, addr)
	w.call(t, id, "Runtime.enable", map[string]any{})
	w.call(t, id, "Runtime.evaluate", map[string]any{"expression": "globalThis.eagerEffects = 0"})
	for _, method := range []string{"Runtime.evaluate", "Runtime.callFunctionOn"} {
		params := map[string]any{"throwOnSideEffect": true, "expression": "console.log('hi'); ++eagerEffects", "functionDeclaration": "function(){console.log('hi'); return ++eagerEffects}"}
		for i := 0; i < 3; i++ {
			if reply := w.request(t, id, method, params); reply["error"] == nil {
				t.Fatalf("unsafe eager evaluation accepted: %s", method)
			}
		}
	}
	value := w.call(t, id, "Runtime.evaluate", map[string]any{"expression": "eagerEffects", "returnByValue": true})["result"].(map[string]any)["value"]
	if value != float64(0) {
		t.Fatalf("preview executed author code: %v", value)
	}
	value = w.call(t, id, "Runtime.evaluate", map[string]any{"expression": "console.log('hi'); ++eagerEffects", "throwOnSideEffect": false, "returnByValue": true})["result"].(map[string]any)["value"]
	if value != float64(1) {
		t.Fatalf("explicit execution count: %v", value)
	}
	event := w.event(t, "Runtime.consoleAPICalled")
	if event["args"].([]any)[0].(map[string]any)["value"] != "hi" {
		t.Fatalf("console event: %v", event)
	}
}
