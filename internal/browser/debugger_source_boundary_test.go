package browser

import (
	"context"
	chrome152 "github.com/moreveal/mimic/chrome/152"
	v8engine "github.com/moreveal/mimic/internal/engine/v8"
	"testing"
)

// Passive CDP observations from headful Chrome 152.0.7977.82 on about:blank.
func TestDebuggerSourceAndInvocationBoundary(t *testing.T) {
	serialBrowserTest(t)
	b, err := New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	c := b.NewContext()
	defer c.Close()
	p, err := c.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	d := NewDebugger(p)
	defer d.Close()
	ctx := context.Background()
	for _, tc := range []struct{ source, want string }{
		{`new Error("probe").stack`, "Error: probe\n    at <anonymous>:1:1"},
		{"new Error(\"probe\").stack\n//# sourceURL=author-probe.js", "Error: probe\n    at author-probe.js:1:1"},
	} {
		got := debuggerEval(t, d, tc.source, DebuggerOptions{ReturnByValue: true})
		if got["value"] != tc.want {
			t.Fatalf("evaluation source: got %v want %q", got, tc.want)
		}
	}
	got, err := d.CallFunction(ctx, "", "", `function probe(){ return new Error("probe").stack; }`, map[string]any{}, DebuggerOptions{ReturnByValue: true})
	if err != nil {
		t.Fatal(err)
	}
	if got["result"].(map[string]any)["value"] != "Error: probe\n    at probe (<anonymous>:1:27)" {
		t.Fatalf("native invocation: %v", got)
	}
	called, err := d.CallFunction(ctx, "", "", `function probe(){return this===globalThis && probe.caller===null}`, map[string]any{}, DebuggerOptions{ReturnByValue: true})
	if err != nil || called["result"].(map[string]any)["value"] != true {
		t.Fatalf("author caller: %v %v", called, err)
	}
	value, err := p.Evaluate(ctx, `new Error("probe").stack`)
	if err != nil || value != "Error: probe\n    at <anonymous>:1:1" {
		t.Fatalf("page evaluation source: %v %v", value, err)
	}
	thrown, err := d.CallFunction(ctx, "", "", `function(){throw 42}`, map[string]any{}, DebuggerOptions{})
	if err != nil || thrown["exceptionDetails"] == nil || thrown["result"].(map[string]any)["value"] != float64(42) {
		t.Fatalf("native thrown value: %v %v", thrown, err)
	}

}

// Native serialization executes enumerable getters, but its implementation
// frames must not join the author stack (headful Chrome 152 observation).
func TestDebuggerSerializationPreservesAuthorStack(t *testing.T) {
	serialBrowserTest(t)
	for _, mode := range []string{"cold", "snapshot"} {
		t.Run(mode, func(t *testing.T) {
			if mode == "cold" {
				t.Setenv("MIMIC_DISABLE_BOOTSTRAP_SNAPSHOT", "1")
			}
			p := bootstrapSnapshotPage(t)
			if mode == "snapshot" {
				bootstrapSnapshotWarm(t, p)
				var err error
				p, err = p.ctx.NewPage()
				if err != nil {
					t.Fatal(err)
				}
				bootstrapSnapshotEvaluate(t, p, "true")
				if !p.Top.Realm.bootstrapRestored {
					t.Fatal("stack probe did not restore its parent realm")
				}
			}
			d := NewDebugger(p)
			defer d.Close()
			debuggerEval(t, d, `globalThis.authorObservations = [];
Error.prepareStackTrace = (_, frames) =>
  frames.map((f) => ({ name: f.getFunctionName(), file: f.getFileName(), eval: f.isEval() }));
({
  get authorAccessor() {
    authorObservations.push(new Error().stack);
    return 1;
  },
  plain: 2,
});
`, DebuggerOptions{ReturnByValue: true})
			got := debuggerEval(t, d, `JSON.stringify(authorObservations)`, DebuggerOptions{ReturnByValue: true})
			const expected = `[[{"name":"get authorAccessor","file":"","eval":false}]]`
			if got["value"] != expected {
				t.Fatalf("serialization author stack: %v", got)
			}
		})
	}
}
