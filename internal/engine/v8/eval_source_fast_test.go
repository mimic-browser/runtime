//go:build (windows || linux) && amd64

package v8

import (
	"context"
	"testing"

	"github.com/moreveal/mimic/internal/engine"
)

func TestUnconstrainedStringCodeGenerationStaysInsideV8(t *testing.T) {
	runtime := Factory{}.New()
	defer runtime.Close()
	evalRuntime := runtime.(engine.EvalSourceRuntime)

	resolver, err := runtime.Eval(context.Background(), `globalThis.evalPolicyCalls=0;(source)=>{evalPolicyCalls++;return String(source)}`, "resolver")
	if err != nil {
		t.Fatal(err)
	}
	if err := evalRuntime.SetEvalSourceResolver(resolver); err != nil {
		t.Fatal(err)
	}
	if err := evalRuntime.SetEvalSourceUnconstrained(true); err != nil {
		t.Fatal(err)
	}
	value, err := runtime.Eval(context.Background(), `for(let i=0;i<100;i++)Function('return 1');evalPolicyCalls`, "unconstrained")
	if err != nil || value.String() != "0" {
		t.Fatalf("unconstrained resolver calls: %v %v", value.Export(), err)
	}

	if err := evalRuntime.SetEvalSourceUnconstrained(false); err != nil {
		t.Fatal(err)
	}
	value, err = runtime.Eval(context.Background(), `for(let i=0;i<5;i++)Function('return 1');evalPolicyCalls`, "constrained")
	if err != nil || value.String() != "5" {
		t.Fatalf("constrained resolver calls: %v %v", value.Export(), err)
	}
}
