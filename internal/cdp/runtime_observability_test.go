package cdp

import (
	"os"
	"testing"
)

// Instrumented Chrome is not the oracle for passive tooling. The control is
// the same author program with no protocol session attached.
func TestRuntimePassiveConsoleObservations(t *testing.T) {
	for _, snapshot := range []string{"1", "0"} {
		t.Run("disableSnapshot="+snapshot, func(t *testing.T) {
			t.Setenv("MIMIC_DISABLE_BOOTSTRAP_SNAPSHOT", snapshot)
			s, addr := runningServer(t)
			source, err := os.ReadFile("testdata/runtime_passive_probe.js")
			if err != nil {
				t.Fatal(err)
			}
			control, err := evaluatePageFixture(s.Page, string(source))
			if err != nil {
				t.Fatal(err)
			}
			w, id := inspectorSession(t, s, addr)
			for _, step := range []string{"disabled", "enabled", "disabledAgain", "reenabled", "secondSession"} {
				t.Run(step, func(t *testing.T) {
					switch step {
					case "enabled", "reenabled":
						w.call(t, id, "Runtime.enable", map[string]any{})
					case "disabledAgain":
						w.call(t, id, "Runtime.disable", map[string]any{})
					case "secondSession":
						other, otherID := inspectorSession(t, s, addr)
						other.call(t, otherID, "Runtime.enable", map[string]any{})
					}
					got := w.call(t, id, "Runtime.evaluate", map[string]any{"expression": string(source), "awaitPromise": true, "returnByValue": true})["result"].(map[string]any)["value"]
					if got != control {
						t.Fatalf("passive Runtime changed author observations:\ncontrol: %v\nattached: %v", control, got)
					}
				})
			}
		})
	}
}

func TestRuntimeInspectionDoesNotExecuteAuthorCode(t *testing.T) {
	s, addr := runningServer(t)
	w, id := inspectorSession(t, s, addr)
	w.call(t, id, "Runtime.enable", map[string]any{})
	loadRuntimeInspectionProbes(t, w, id)
	for _, test := range []struct {
		name                            string
		rejectByValue, rejectProperties bool
	}{
		{"proxy", true, true}, {"callableProxy", true, true}, {"revokedProxy", true, true},
		{"accessor", true, false}, {"arrayAccessor", true, false}, {"nestedAccessor", true, false},
		{"prototypeProxy", false, true}, {"constructorProxy", false, false},
		{"inheritedArrayAccessor", false, false},
	} {
		expression := "globalThis.effects=[];globalThis.inspectionValue=runtimeInspectionProbes." + test.name + "();inspectionValue"
		object := w.call(t, id, "Runtime.evaluate", map[string]any{"expression": expression})["result"].(map[string]any)
		// Unsupported unsafe inspection must reject without executing traps. Ordinary
		// accessor descriptors remain available; by-value accessors are unsupported.
		properties := w.request(t, id, "Runtime.getProperties", map[string]any{"objectId": object["objectId"]})
		if (properties["error"] != nil) != test.rejectProperties {
			t.Fatalf("%s property support boundary: %v", test.name, properties)
		}
		reply := w.request(t, id, "Runtime.evaluate", map[string]any{"expression": "inspectionValue", "returnByValue": true})
		if (reply["error"] != nil) != test.rejectByValue {
			t.Fatalf("%s by-value support boundary: %v", test.name, reply)
		}
		got := w.call(t, id, "Runtime.evaluate", map[string]any{"expression": "JSON.stringify(effects)", "returnByValue": true})["result"].(map[string]any)["value"]
		if got != "[]" {
			t.Fatalf("%s inspection executed author code: %v", test.name, got)
		}
	}
}

func TestRuntimeExceptionInspectionDoesNotCoerceThrownValues(t *testing.T) {
	s, addr := runningServer(t)
	w, id := inspectorSession(t, s, addr)
	w.call(t, id, "Runtime.enable", map[string]any{})
	loadRuntimeInspectionProbes(t, w, id)
	for _, kind := range []string{"plainException", "error", "lazyError", "proxy", "revokedProxy"} {
		setup := "globalThis.effects=[];globalThis.thrown=runtimeInspectionProbes." + kind + "();true"
		for _, method := range []string{"Runtime.evaluate", "Runtime.evaluate.CLI", "Runtime.callFunctionOn", "Runtime.awaitPromise"} {
			w.call(t, id, "Runtime.evaluate", map[string]any{"expression": setup})
			var reply map[string]any
			switch method {
			case "Runtime.evaluate":
				reply = w.call(t, id, method, map[string]any{"expression": "throw thrown"})
			case "Runtime.evaluate.CLI":
				reply = w.call(t, id, "Runtime.evaluate", map[string]any{"expression": "throw thrown", "includeCommandLineAPI": true})
			case "Runtime.callFunctionOn":
				reply = w.call(t, id, method, map[string]any{"functionDeclaration": "function(){throw thrown}"})
			case "Runtime.awaitPromise":
				object := w.call(t, id, "Runtime.evaluate", map[string]any{"expression": "Promise.reject(thrown)"})["result"].(map[string]any)
				reply = w.call(t, id, method, map[string]any{"promiseObjectId": object["objectId"]})
			}
			if reply["exceptionDetails"] == nil {
				t.Fatalf("exception lost: %v", reply)
			}
			got := w.call(t, id, "Runtime.evaluate", map[string]any{"expression": "JSON.stringify(effects)", "returnByValue": true})["result"].(map[string]any)["value"]
			if got != "[]" {
				t.Fatalf("%s %s coerced exception: %v", kind, method, got)
			}
			identity := w.call(t, id, "Runtime.callFunctionOn", map[string]any{"objectId": reply["result"].(map[string]any)["objectId"], "functionDeclaration": "function(){ return this === thrown; }", "returnByValue": true})["result"].(map[string]any)["value"]
			if identity != true {
				t.Fatal("exception identity changed")
			}
		}
	}
}

func TestRuntimeInspectionUsesPrivateTransportRecords(t *testing.T) {
	s, addr := runningServer(t)
	w, id := inspectorSession(t, s, addr)
	w.call(t, id, "Runtime.enable", map[string]any{})
	loadRuntimeInspectionProbes(t, w, id)
	object := w.call(t, id, "Runtime.evaluate", map[string]any{"expression": "globalThis.effects=[];runtimeInspectionProbes.poisonInfrastructure()"})["result"].(map[string]any)
	w.call(t, id, "Runtime.getProperties", map[string]any{"objectId": object["objectId"]})
	w.call(t, id, "Runtime.evaluate", map[string]any{"expression": "/passive/gi"})
	w.call(t, id, "Runtime.releaseObject", map[string]any{"objectId": object["objectId"]})
	w.call(t, id, "Runtime.releaseObjectGroup", map[string]any{"objectGroup": "console"})
	got := w.call(t, id, "Runtime.evaluate", map[string]any{"expression": "JSON.stringify(effects)", "returnByValue": true})["result"].(map[string]any)["value"]
	if got != "[]" {
		t.Fatalf("private transport consulted author prototypes: %v", got)
	}
}

func loadRuntimeInspectionProbes(t *testing.T, w *inspectorWire, id string) {
	t.Helper()
	source, err := os.ReadFile("testdata/runtime_inspection_probes.js")
	if err != nil {
		t.Fatal(err)
	}
	w.call(t, id, "Runtime.evaluate", map[string]any{"expression": string(source)})
}

func TestRuntimeCommandLineResultInspectionDoesNotExecuteAuthorCode(t *testing.T) {
	s, addr := runningServer(t)
	w, id := inspectorSession(t, s, addr)
	w.call(t, id, "Runtime.enable", map[string]any{})
	loadRuntimeInspectionProbes(t, w, id)
	for _, kind := range []string{"error", "lazyError", "proxy", "revokedProxy"} {
		expression := "globalThis.effects=[];globalThis.inspectionValue=runtimeInspectionProbes." + kind + "();inspectionValue"
		w.call(t, id, "Runtime.evaluate", map[string]any{"expression": expression, "includeCommandLineAPI": true, "objectGroup": "console"})
		first := w.call(t, id, "Runtime.evaluate", map[string]any{"expression": "JSON.stringify(effects)", "returnByValue": true})["result"].(map[string]any)["value"]
		if first != "[]" {
			t.Fatalf("%s CLI initial result inspection executed author code: %v", kind, first)
		}
		w.call(t, id, "Runtime.evaluate", map[string]any{"expression": "$_", "includeCommandLineAPI": true, "objectGroup": "console"})
		got := w.call(t, id, "Runtime.evaluate", map[string]any{"expression": "JSON.stringify(effects)", "returnByValue": true})["result"].(map[string]any)["value"]
		if got != "[]" {
			t.Fatalf("%s CLI result inspection executed author code: %v", kind, got)
		}
	}
}

func TestRuntimeCommandLineUnicodeAndExplicitAccessor(t *testing.T) {
	s, addr := runningServer(t)
	w, id := inspectorSession(t, s, addr)
	w.call(t, id, "Runtime.enable", map[string]any{})
	loadRuntimeInspectionProbes(t, w, id)
	result := w.call(t, id, "Runtime.evaluate", map[string]any{"expression": "let команда = 'Привет 🛰'; команда", "includeCommandLineAPI": true, "returnByValue": true})["result"].(map[string]any)
	if result["value"] != "Привет 🛰" {
		t.Fatalf("command-line source encoding: %v", result)
	}
	w.call(t, id, "Runtime.evaluate", map[string]any{"expression": "globalThis.effects=[];globalThis.inspectionValue=runtimeInspectionProbes.error();true"})
	result = w.call(t, id, "Runtime.callFunctionOn", map[string]any{"functionDeclaration": "function() { return inspectionValue.stack; }", "returnByValue": true})["result"].(map[string]any)
	if result["value"] != "author" {
		t.Fatalf("explicit getter call: %v", result)
	}
	got := w.call(t, id, "Runtime.evaluate", map[string]any{"expression": "JSON.stringify(effects)", "returnByValue": true})["result"].(map[string]any)["value"]
	if got != `["stack","job:stack"]` {
		t.Fatalf("explicit getter effects changed: %v", got)
	}
}

func TestRuntimeAwaitPromiseDoesNotReadAuthorProperties(t *testing.T) {
	s, addr := runningServer(t)
	w, id := inspectorSession(t, s, addr)
	w.call(t, id, "Runtime.enable", map[string]any{})
	loadRuntimeInspectionProbes(t, w, id)
	object := w.call(t, id, "Runtime.evaluate", map[string]any{"expression": "globalThis.effects=[];runtimeInspectionProbes.promise()"})["result"].(map[string]any)
	result := w.call(t, id, "Runtime.awaitPromise", map[string]any{"promiseObjectId": object["objectId"], "returnByValue": true})["result"].(map[string]any)
	if result["value"] != float64(42) {
		t.Fatalf("promise state observation: %v", result)
	}
	got := w.call(t, id, "Runtime.evaluate", map[string]any{"expression": "JSON.stringify(effects)", "returnByValue": true})["result"].(map[string]any)["value"]
	if got != "[]" {
		t.Fatalf("promise inspection read author properties: %v", got)
	}
}

func TestRuntimeInspectionKeepsErrorStackLazy(t *testing.T) {
	s, addr := runningServer(t)
	w, id := inspectorSession(t, s, addr)
	w.call(t, id, "Runtime.enable", map[string]any{})
	loadRuntimeInspectionProbes(t, w, id)
	object := w.call(t, id, "Runtime.evaluate", map[string]any{"expression": `globalThis.effects = [];
globalThis.inspectedLazy = runtimeInspectionProbes.lazyError();
inspectedLazy.toString = () => 'custom';
console.log(inspectedLazy);
inspectedLazy;`})["result"].(map[string]any)
	w.call(t, id, "Runtime.getProperties", map[string]any{"objectId": object["objectId"], "ownProperties": true})
	result := w.call(t, id, "Runtime.evaluate", map[string]any{"expression": `Error.prepareStackTrace = () => {
  effects.push('fresh-prepare');
  return 'fresh';
};
inspectedLazy.stack;`, "returnByValue": true})["result"].(map[string]any)
	if result["value"] != "fresh" {
		t.Fatalf("inspection cached a lazy stack: %v", result)
	}
	got := w.call(t, id, "Runtime.evaluate", map[string]any{"expression": "JSON.stringify(effects)", "returnByValue": true})["result"].(map[string]any)["value"]
	if got != `["fresh-prepare"]` {
		t.Fatalf("unexpected lazy-stack effects: %v", got)
	}
}
