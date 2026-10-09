package cdp

import "testing"

func TestInspectorCommandLineSelectionAndGlobalSemantics(t *testing.T) {
	s, addr := runningServer(t)
	w, id := inspectorSession(t, s, addr)
	w.call(t, id, "Runtime.enable", map[string]any{})
	w.call(t, id, "Runtime.evaluate", map[string]any{"expression": `document.body.innerHTML='<div id="first"></div><div id="second"></div>'`})
	root := w.call(t, id, "DOM.getDocument", map[string]any{"depth": -1})["root"]
	first, second := findInspectorNode(root, "id", "first"), findInspectorNode(root, "id", "second")
	w.call(t, id, "DOM.setInspectedNode", map[string]any{"nodeId": first["nodeId"]})
	value := func(source string, cli bool) any {
		t.Helper()
		result := w.call(t, id, "Runtime.evaluate", map[string]any{"expression": source, "includeCommandLineAPI": cli, "returnByValue": true})
		if result["exceptionDetails"] != nil {
			t.Fatalf("command-line expression %s: %v", source, result)
		}
		return result["result"].(map[string]any)["value"]
	}
	if value(`$0===document.querySelector('#first')`, true) != true {
		t.Fatal("selected node lost canonical identity")
	}
	if value(`Object.hasOwn(window,'$0')`, false) != false {
		t.Fatal("command-line helper leaked into author global")
	}
	if value(`let consoleLexical=40;consoleLexical+2`, true) != float64(42) || value(`consoleLexical`, false) != float64(40) {
		t.Fatal("command-line evaluation changed global lexical scope")
	}
	w.call(t, id, "DOM.setInspectedNode", map[string]any{"nodeId": second["nodeId"]})
	if value(`$0.id==='second'&&$1.id==='first'`, true) != true {
		t.Fatal("selection history lost")
	}
	if value(`globalThis.$0='author';$0`, true) != "author" {
		t.Fatal("inspector overwrote author property")
	}
	if value(`$0`, false) != "author" {
		t.Fatal("author property removed on command-line exit")
	}
	value(`delete globalThis.$0`, false)
	if value(`$0.id`, true) != "second" {
		t.Fatal("command-line helpers did not recover after author property removal")
	}
	object := w.call(t, id, "Runtime.evaluate", map[string]any{"expression": "$0", "includeCommandLineAPI": true})["result"].(map[string]any)
	node := w.call(t, id, "DOM.requestNode", map[string]any{"objectId": object["objectId"]})
	if node["nodeId"] != second["nodeId"] {
		t.Fatal("native inspector object escaped the canonical debugger handle table")
	}
	for _, expression := range []string{"NaN", "Infinity", "-0", "123n"} {
		result := w.call(t, id, "Runtime.evaluate", map[string]any{"expression": expression, "includeCommandLineAPI": true})["result"].(map[string]any)
		if result["unserializableValue"] != expression {
			t.Fatalf("command-line scalar %s: %v", expression, result)
		}
	}
	if result := w.call(t, id, "Runtime.evaluate", map[string]any{"expression": `throw new Error('native inspector exception')`, "includeCommandLineAPI": true}); result["exceptionDetails"] == nil {
		t.Fatal("native inspector exception lost")
	}
	if value(`Promise.resolve(42)`, true) == nil { // Promise by value is an empty object; waiting is checked below.
		t.Fatal("native promise missing")
	}
	result := w.call(t, id, "Runtime.evaluate", map[string]any{"expression": "Promise.resolve(42)", "includeCommandLineAPI": true, "awaitPromise": true, "returnByValue": true})
	if result["result"].(map[string]any)["value"] != float64(42) {
		t.Fatalf("native promise: %v", result)
	}
	consoleValue := func(source string) any {
		t.Helper()
		result := w.call(t, id, "Runtime.evaluate", map[string]any{"expression": source, "includeCommandLineAPI": true, "objectGroup": "console", "returnByValue": true})
		if result["exceptionDetails"] != nil {
			t.Fatalf("console expression %s: %v", source, result)
		}
		return result["result"].(map[string]any)["value"]
	}
	consoleValue("42")
	if consoleValue("$_") != float64(42) {
		t.Fatal("console last result lost between native inspector scopes")
	}
	value("99", false)
	if consoleValue("$_") != float64(42) {
		t.Fatal("non-console evaluation overwrote console last result")
	}
	consoleValue("document.querySelector('#second')")
	if consoleValue("$_===document.querySelector('#second')") != true {
		t.Fatal("console last result lost canonical object identity")
	}
}
