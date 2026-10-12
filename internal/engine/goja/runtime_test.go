package gojaengine

import (
	"context"
	"errors"
	"github.com/moreveal/mimic/internal/engine"
	"strings"
	"testing"
	"time"
)

func TestCaughtExceptionDoesNotCoerceAuthorValue(t *testing.T) {
	r := Factory{}.New()
	defer r.Close()
	ctx := context.Background()
	if _, err := r.Eval(ctx, `globalThis.effects = [];
globalThis.thrown = { toString() { effects.push('toString'); return 'author'; } };`, "setup.js"); err != nil {
		t.Fatal(err)
	}
	_, err := r.Eval(ctx, "throw thrown", "throw.js")
	if err == nil {
		t.Fatal("exception lost")
	}
	value, readErr := r.Eval(ctx, "JSON.stringify(effects)", "read.js")
	if readErr != nil || value.String() != "[]" {
		t.Fatalf("exception coercion: %v %v", value, readErr)
	}
	if _, err := r.Eval(ctx, `globalThis.thrown = new Error('original');
Object.defineProperties(thrown, {
  name: { get() { effects.push('name'); return 'Error'; } },
  message: { get() { effects.push('message'); return 'author'; } },
});
Object.getOwnPropertyDescriptor = () => { effects.push('descriptor'); throw 1; };
Object.defineProperty(Object.prototype, 'value', {
  get() { effects.push('inheritedValue'); throw 1; }, configurable: true,
});`, "error-setup.js"); err != nil {
		t.Fatal(err)
	}
	_, err = r.Eval(ctx, "throw thrown", "error-throw.js")
	if err == nil {
		t.Fatal("Error exception lost")
	}
	value, readErr = r.Eval(ctx, "JSON.stringify(effects)", "error-read.js")
	if readErr != nil || value.String() != "[]" {
		t.Fatalf("Error diagnostics executed author code: %v %v", value, readErr)
	}
	_, err = r.Eval(ctx, "throw new Error('useful diagnostic')", "native-message.js")
	if err == nil || !strings.Contains(err.Error(), "useful diagnostic") {
		t.Fatalf("own data message lost: %v", err)
	}
}

func TestEvalHonorsContextCancellationAndRuntimeRemainsUsable(t *testing.T) {
	runtime := Factory{}.New()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := runtime.Eval(ctx, `for (;;) {}`, "infinite.js"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline, got %v", err)
	}
	value, err := runtime.Eval(context.Background(), `6 * 7`, "after.js")
	if err != nil || value.Export() != int64(42) {
		t.Fatalf("runtime unusable after interruption: value=%v err=%v", value, err)
	}
}

func TestGlobalAccessObserverPreservesAccessorReceiverAndException(t *testing.T) {
	r := Factory{}.New().(*runtime)
	defer r.Close()
	r.SetGlobalAccessObserver(func(string, bool) {})
	if err := r.vm.Set("observedGlobal", r.vm.GlobalObject()); err != nil {
		t.Fatal(err)
	}
	value, err := r.Eval(context.Background(), `(() => {
		const marker = {};
		Object.defineProperty(observedGlobal, 'receiverProbe', {get() { return this; }});
		Object.defineProperty(observedGlobal, 'throwProbe', {get() { throw marker; }});
		const child = Object.create(observedGlobal);
		if (observedGlobal.receiverProbe !== observedGlobal || child.receiverProbe !== child) return false;
		try { observedGlobal.throwProbe; } catch (error) { return error === marker; }
		return false;
	})()`, "global-observer.js")
	if err != nil || value.Export() != true {
		t.Fatalf("observer changed accessor semantics: value=%v err=%v", value, err)
	}
}

func TestGlobalAccessObserverDeduplicatesSupportQueriesWithoutCachingHas(t *testing.T) {
	r := Factory{}.New().(*runtime)
	defer r.Close()
	if _, err := r.Eval(context.Background(), `(() => {
  const original = Reflect.has;
  let queries = 0;
  Reflect.has = function(target, key) { if (key === 'subject') queries++; return original(target, key); };
  globalThis.readSupportQueries = () => queries;
  globalThis.subject = 42;
 })()`, "support-query-counter.js"); err != nil {
		t.Fatal(err)
	}
	observations := 0
	r.SetGlobalAccessObserver(func(name string, supported bool) {
		if name == "subject" {
			observations++
			if !supported {
				t.Error("first subject observation should be supported")
			}
		}
	})
	if err := r.vm.Set("observedGlobal", r.vm.GlobalObject()); err != nil {
		t.Fatal(err)
	}
	value, err := r.Eval(context.Background(), `(() => {
  for (let i = 0; i < 20; i++) if (observedGlobal.subject !== 42) return false;
  delete observedGlobal.subject;
  if (observedGlobal.subject !== undefined || 'subject' in observedGlobal) return false;
  return readSupportQueries() === 2;
 })()`, "deduplicated-support.js")
	if err != nil || value.Export() != true || observations != 1 {
		t.Fatalf("support observation: value=%v err=%v observations=%d", value, err, observations)
	}
}

func TestMissingPropertiesAreUndefined(t *testing.T) {
	r := Factory{}.New()
	defer r.Close()
	object, err := r.Eval(context.Background(), `({})`, "missing.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []engine.Value{r.Get("missingGlobal"), r.GetProperty(object, "missingMember")} {
		if v.String() != "undefined" || v.Export() != nil || r.TypeOf(v) != "undefined" {
			t.Fatalf("missing property: %v", v)
		}
	}
}
