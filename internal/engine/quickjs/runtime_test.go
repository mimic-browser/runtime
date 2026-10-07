package quickjsengine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/moreveal/mimic/internal/engine"
)

func TestEvalCallPromiseAndCancellation(t *testing.T) {
	r := Factory{}.New()
	defer r.Close()

	if err := r.Set("host", map[string]any{"twice": r.Function(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		return r.Value(args[0].Export().(int64) * 2), nil
	})}); err != nil {
		t.Fatal(err)
	}
	value, err := r.Eval(context.Background(), `host.twice(21)`, "host.js")
	if err != nil || value.Export() != int64(42) {
		t.Fatalf("host call: value=%v err=%v", value, err)
	}
	value, err = r.Eval(context.Background(), `6*7`, "basic.js")
	if err != nil || value.Export() != int64(42) {
		t.Fatalf("eval: value=%v err=%v", value, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err = r.Eval(ctx, `for(;;){}`, "infinite.js"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline, got %v", err)
	}
	value, err = r.Eval(context.Background(), `21*2`, "after.js")
	if err != nil || value.Export() != int64(42) {
		t.Fatalf("runtime after interrupt: value=%v err=%v", value, err)
	}
}

func TestHostCallbackValueSurvivesBridgeReturn(t *testing.T) {
	r := Factory{}.New()
	defer r.Close()
	var retained engine.Value
	if err := r.Set("keep", r.Function(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		retained = args[0]
		return nil, nil
	})); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Eval(context.Background(), `keep(()=>42)`, "retain.js"); err != nil {
		t.Fatal(err)
	}
	got, err := r.Call(context.Background(), retained, nil)
	if err != nil || got.Export() != int64(42) {
		t.Fatalf("retained callback: value=%v err=%v", got, err)
	}
}

func TestDateUsesLiveTimeSourceDuringExecution(t *testing.T) {
	r := Factory{}.New()
	defer r.Close()
	r.SetTimeSource(time.Now)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	value, err := r.Eval(ctx, `let start=Date.now();while(Date.now()-start<25){};Date.now()-start`, "clock.js")
	if err != nil {
		t.Fatal(err)
	}
	elapsed := value.Export()
	switch n := elapsed.(type) {
	case int64:
		if n < 25 {
			t.Fatalf("Date did not advance during execution: %#v", elapsed)
		}
	case float64:
		if n < 25 {
			t.Fatalf("Date did not advance during execution: %#v", elapsed)
		}
	default:
		t.Fatalf("unexpected Date result: %#v", elapsed)
	}
}

func TestMicrotaskCheckpointDrainsJobsQueuedByJobs(t *testing.T) {
	r := Factory{}.New()
	defer r.Close()
	promise := r.NewPromise()
	if err := r.Set("hostPromise", promise.Value); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Eval(context.Background(), `globalThis.result=0;hostPromise.then(x=>x+1).then(x=>{result=x})`, "microtasks.js"); err != nil {
		t.Fatal(err)
	}
	if err := promise.Resolve(int64(41)); err != nil {
		t.Fatal(err)
	}
	if err := r.MicrotaskCheckpoint(); err != nil {
		t.Fatal(err)
	}
	value, err := r.Eval(context.Background(), `result`, "result.js")
	if err != nil || value.Export() != int64(42) {
		t.Fatalf("nested Promise jobs were not drained: value=%v err=%v", value.Export(), err)
	}
}

func TestErrorConstructionDoesNotReadSubclassAccessors(t *testing.T) {
	r := Factory{}.New()
	defer r.Close()
	r.SetTimeSource(time.Now)
	v, err := r.Eval(context.Background(), `let nameReads = 0;
class SubclassError extends Error {
  get name() {
    nameReads++;
    throw new Error('read before initialization');
  }
}
new SubclassError('capture');
nameReads === 0;`, "error-subclass.js")
	if err != nil || v.Export() != true {
		t.Fatalf("constructor invoked name getter: %v %v", v, err)
	}
}
