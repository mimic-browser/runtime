package v8

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/moreveal/mimic/internal/engine"
)

func TestTransientHostValuesAndRetainedCallbacks(t *testing.T) {
	r := (Factory{}).New().(*adapter)
	defer r.Close()
	var retained engine.Value
	r.Set("keep", r.Function(func(_ engine.Value, args []engine.Value) (engine.Value, error) { retained = args[0]; return nil, nil }))
	r.Set("echo", r.TransientFunction(func(_ engine.Value, args []engine.Value) (engine.Value, error) { return args[0], nil }))
	r.Eval(context.Background(), `keep(()=>42)`, "keep.js")
	before := len(r.globals)
	v, err := r.Eval(context.Background(), `(()=>{for(let i=0;i<1000;i++)if(echo(i)!==i)throw Error('echo');return echo('Привет 😀')})()`, "echo.js")
	if err != nil || v.Export() != "Привет 😀" {
		t.Fatalf("%v %v", v, err)
	}
	if len(r.globals) > before+1 {
		t.Fatal("transient arguments created persistent roots")
	}
	v, err = r.Call(context.Background(), retained, nil)
	if err != nil || v.Export() != float64(42) {
		t.Fatalf("retained function: %v %v", v, err)
	}
}

func TestHostValueSurvivesItsCreatingCallback(t *testing.T) {
	r := (Factory{}).New().(*adapter)
	defer r.Close()
	var saved engine.Value
	if err := r.Set("save", r.TransientFunction(func(engine.Value, []engine.Value) (engine.Value, error) {
		saved = r.Value("kept")
		return r.Value(true), nil
	})); err != nil {
		t.Fatal(err)
	}
	if err := r.Set("read", r.TransientFunction(func(engine.Value, []engine.Value) (engine.Value, error) {
		if saved.Export() != "kept" || saved.String() != "kept" || r.TypeOf(saved) != "string" {
			t.Fatal("saved host value changed between callbacks")
		}
		return saved, nil
	})); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Eval(context.Background(), `if(save()!==true||read()!=="kept")throw Error("escaped host value")`, "saved-host.js"); err != nil {
		t.Fatal(err)
	}
}

func TestTransientCallbackPreservesReceiver(t *testing.T) {
	r := (Factory{}).New().(*adapter)
	defer r.Close()
	var seen []any
	if err := r.Set("receiver", r.TransientFunction(func(this engine.Value, _ []engine.Value) (engine.Value, error) {
		value := r.GetProperty(this, "tag")
		seen = append(seen, value.Export())
		return value, nil
	})); err != nil {
		t.Fatal(err)
	}
	value, err := r.Eval(context.Background(), `receiver.prototype.tag=99; receiver.call({tag: 42}) === 42 && (new receiver()).tag === 99`, "receiver.js")
	if err != nil || value.Export() != true {
		t.Fatalf("receiver: %v %v", value, err)
	}
	if len(seen) != 2 || seen[0] != float64(42) || seen[1] != float64(99) {
		t.Fatalf("callback receivers: %v", seen)
	}
}

func TestBatchedCallbackRecordsPreserveCollectionValues(t *testing.T) {
	r := (Factory{}).New().(*adapter)
	defer r.Close()
	r.Set("records", r.TransientFunction(func(engine.Value, []engine.Value) (engine.Value, error) {
		return r.Value([]map[string]any{{"empty": []int64(nil), "map": map[string]string(nil), "bytes": []byte{1, 2}, "text": "Привет 😀"}}), nil
	}))
	v, err := r.Eval(context.Background(), `JSON.stringify(records())`, "records.js")
	if err != nil || v.String() != `[{"bytes":[1,2],"empty":[],"map":{},"text":"Привет 😀"}]` {
		t.Fatalf("%v %v", v, err)
	}
}

func TestPlainRecordFastPathKeepsUnsupportedHostObjectsAsErrors(t *testing.T) {
	r := (Factory{}).New().(*adapter)
	defer r.Close()
	r.Set("record", r.TransientFunction(func(engine.Value, []engine.Value) (engine.Value, error) {
		return r.Value(map[string]any{"unsupported": struct{ Value int }{42}}), nil
	}))
	v, err := r.Eval(context.Background(), `(()=>{try{record();return false}catch(e){return String(e).includes('unsupported callback value')}})()`, "unsupported-record.js")
	if err != nil || v.Export() != true {
		t.Fatalf("unsupported object: %v %v", v, err)
	}
}

func TestTransientFramesSurviveReentrantCalls(t *testing.T) {
	r := (Factory{}).New().(*adapter)
	defer r.Close()
	if err := r.Set("reenter", r.TransientFunction(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		before := args[0].Export()
		if len(args) > 1 {
			if _, err := r.Call(context.Background(), args[1], nil); err != nil {
				return nil, err
			}
		}
		if args[0].Export() != before {
			t.Error("nested callback overwrote outer argument")
		}
		return args[0], nil
	})); err != nil {
		t.Fatal(err)
	}
	v, err := r.Eval(context.Background(), `(()=>{for(let i=0;i<100;i++){const x=reenter('outer',()=>reenter('inner',()=>reenter(42)));if(x!=='outer')throw Error(x)}return reenter('Привет 😀')})()`, "reentrant.js")
	if err != nil {
		t.Fatal(err)
	}
	if v.Export() != "Привет 😀" {
		t.Fatal(v.Export())
	}
	for _, frame := range r.transientFrames {
		if frame.callback.scope != nil || frame.callback.ctx != nil || frame.callback.id != 0 {
			t.Fatal("scratch frame retained callback context")
		}
		for _, value := range frame.values {
			if value.runtime != nil || value.global != nil || value.host != nil {
				t.Fatal("scratch frame retained callback state")
			}
		}
	}
}

// This diagnostic calibrates the present backend boundary; it is not a frozen
// workload result and cannot be used as an optimization acceptance gate.
func TestHostBoundaryProfile(t *testing.T) {
	dir := os.Getenv("MIMIC_BOUNDARY_PROFILE_DIR")
	if dir == "" {
		t.Skip("opt-in backend boundary calibration")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	r := (Factory{}).New().(*adapter)
	defer r.Close()
	r.Set("noop", r.TransientFunction(func(engine.Value, []engine.Value) (engine.Value, error) { return nil, nil }))
	r.Set("convert", r.TransientFunction(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		for _, arg := range args {
			_ = arg.Export()
		}
		return nil, nil
	}))
	var rows []map[string]any
	for _, mode := range []string{"js", "crossing", "conversion"} {
		source := `(()=>{const f=()=>{};for(let i=0;i<60000;i++)f(i,'class','active')})()`
		if mode == "crossing" {
			source = `(()=>{for(let i=0;i<60000;i++)noop(i,'class','active')})()`
		}
		if mode == "conversion" {
			source = `(()=>{for(let i=0;i<60000;i++)convert(i,'class','active')})()`
		}
		for i := -1; i < 10; i++ {
			started := time.Now()
			_, err := r.Eval(context.Background(), source, "boundary.js")
			elapsed := time.Since(started)
			if err != nil {
				t.Fatal(err)
			}
			rows = append(rows, map[string]any{"mode": mode, "iteration": i, "excluded": i < 0, "calls": 60000, "ms": float64(elapsed) / 1e6})
		}
	}
	data, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "boundary.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
}
