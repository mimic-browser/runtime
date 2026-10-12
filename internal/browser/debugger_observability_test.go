package browser

import (
	"context"
	"os"
	"testing"
)

func TestDebuggerPassiveInspectionInColdAndRestoredRealms(t *testing.T) {
	serialBrowserTest(t)
	source, err := os.ReadFile("../cdp/testdata/runtime_passive_probe.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"cold", "restored"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("MIMIC_DISABLE_BOOTSTRAP_SNAPSHOT", map[string]string{"cold": "1", "restored": "0"}[mode])
			p := bootstrapSnapshotPage(t)
			if mode == "restored" {
				bootstrapSnapshotWarm(t, p)
				p, err = p.ctx.NewPage()
				if err != nil {
					t.Fatal(err)
				}
				bootstrapSnapshotEvaluate(t, p, "true")
				if !p.Top.Realm.bootstrapRestored {
					t.Fatal("passive inspection control did not restore its realm")
				}
			}
			control := bootstrapSnapshotEvaluate(t, p, string(source))
			d := NewDebugger(p)
			defer d.Close()
			d.Console = func(string, string, []any) {}
			for _, cli := range []bool{false, true} {
				got, err := d.Evaluate(context.Background(), "", "", string(source), DebuggerOptions{AwaitPromise: true, ReturnByValue: true, CommandLineAPI: cli})
				if err != nil {
					t.Fatal(err)
				}
				if value := got["result"].(map[string]any)["value"]; value != control {
					t.Fatalf("CLI=%v changed passive observations:\ncontrol: %v\nattached: %v", cli, control, value)
				}
			}
		})
	}
}

func TestDebuggerExceptionObservationHasNoAuthorCoercion(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		d := NewDebugger(p)
		defer d.Close()
		debuggerEval(t, d, `globalThis.effects = [];
globalThis.thrown = {
  toString() { effects.push('toString'); return 'author'; },
  get name() { effects.push('name'); return 'Error'; },
  get message() { effects.push('message'); return 'author'; },
  get stack() { effects.push('stack'); return 'author'; },
};`, DebuggerOptions{})
		result, err := d.Evaluate(context.Background(), "", "", "throw thrown", DebuggerOptions{})
		if err != nil || result["exceptionDetails"] == nil {
			t.Fatalf("exception lost: %v %v", result, err)
		}
		if got := debuggerEval(t, d, "JSON.stringify(effects)", DebuggerOptions{ReturnByValue: true})["value"]; got != "[]" {
			t.Fatalf("exception observation coerced author value: %v", got)
		}
		promise := debuggerEval(t, d, "Promise.reject(thrown)", DebuggerOptions{})
		result, err = d.AwaitPromise(context.Background(), promise["objectId"].(string), DebuggerOptions{})
		if err != nil || result["exceptionDetails"] == nil {
			t.Fatalf("rejection lost: %v %v", result, err)
		}
		if got := debuggerEval(t, d, "JSON.stringify(effects)", DebuggerOptions{ReturnByValue: true})["value"]; got != "[]" {
			t.Fatalf("rejection observation coerced author value: %v", got)
		}
	})
}

func TestDebuggerPassiveConsoleAcrossEngines(t *testing.T) {
	parallelBrowserTest(t)
	source, err := os.ReadFile("../cdp/testdata/runtime_passive_probe.js")
	if err != nil {
		t.Fatal(err)
	}
	historyTestPages(t, func(t *testing.T, p *Page) {
		control, err := p.Evaluate(context.Background(), string(source))
		if err != nil {
			t.Fatal(err)
		}
		d := NewDebugger(p)
		defer d.Close()
		d.Console = func(string, string, []any) {}
		got, err := d.Evaluate(context.Background(), "", "", string(source), DebuggerOptions{AwaitPromise: true, ReturnByValue: true})
		if err != nil {
			t.Fatal(err)
		}
		if value := got["result"].(map[string]any)["value"]; value != control {
			t.Fatalf("passive console changed author observations:\ncontrol: %v\nattached: %v", control, value)
		}
	})
}

func TestDebuggerPrivateDescriptorTransportAcrossEngines(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		d := NewDebugger(p)
		defer d.Close()
		debuggerEval(t, d, `globalThis.effects = [];
globalThis.subject = { number: 42 };
const define = Object.defineProperty;
const fields = ['value', 'get', 'set', 'writable', 'enumerable', 'configurable'];
for (let index = 0; index < fields.length; index++) {
  const key = fields[index];
  const descriptor = Object.create(null);
  descriptor.get = () => { effects.push('get:' + key); throw 1; };
  descriptor.set = () => { effects.push('set:' + key); throw 1; };
  descriptor.configurable = true;
  define(Object.prototype, key, descriptor);
}
Object.getOwnPropertyDescriptor = () => { effects.push('descriptor'); throw 1; };
String.prototype.slice = () => { effects.push('slice'); throw 1; };
true`, DebuggerOptions{})
		object := debuggerEval(t, d, "subject", DebuggerOptions{})
		if _, err := d.GetProperties(context.Background(), map[string]any{"objectId": object["objectId"], "ownProperties": true}); err != nil {
			t.Fatal(err)
		}
		debuggerEval(t, d, "subject", DebuggerOptions{ReturnByValue: true})
		got, err := d.CallFunction(context.Background(), "", "", "function(value){return value===123n}", map[string]any{
			"arguments": []any{map[string]any{"unserializableValue": "123n"}},
		}, DebuggerOptions{ReturnByValue: true})
		if err != nil || got["result"].(map[string]any)["value"] != true {
			t.Fatalf("primitive transport: %v %v", got, err)
		}
		grouped := debuggerEval(t, d, "subject", DebuggerOptions{ObjectGroup: "passive"})
		if err := d.ReleaseObjectGroup(context.Background(), "passive"); err != nil {
			t.Fatal(err)
		}
		if _, err := d.GetProperties(context.Background(), map[string]any{"objectId": grouped["objectId"]}); err == nil {
			t.Fatal("released group retained its handle")
		}
		if err := d.ReleaseObject(context.Background(), object["objectId"].(string)); err != nil {
			t.Fatal(err)
		}
		if got := debuggerEval(t, d, "JSON.stringify(effects)", DebuggerOptions{ReturnByValue: true})["value"]; got != "[]" {
			t.Fatalf("descriptor transport executed author code: %v", got)
		}
	})
}
