//go:build (windows || linux) && amd64

package v8

import (
	"context"
	"errors"
	"testing"

	"github.com/moreveal/mimic/internal/engine"
)

func TestStructuredCloneClassifiesInternalFieldHostObjects(t *testing.T) {
	runtime := (Factory{}).New()
	defer runtime.Close()
	adapter := runtime.(*adapter)
	if err := runtime.Set("factory", adapter.PropertyObservationFactory()); err != nil {
		t.Fatal(err)
	}
	evaluate := func(source string) engine.Value {
		t.Helper()
		value, err := runtime.Eval(context.Background(), source, "clone-host-boundary")
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	object := evaluate(`factory(() => {})`)
	reject := evaluate(`() => true`)
	if wire, err := adapter.SerializeStructuredClone(object, reject); err == nil {
		t.Fatalf("uncloneable native host object produced %d wire bytes", len(wire))
	} else {
		var cloneError *engine.DataCloneError
		if !errors.As(err, &cloneError) {
			t.Fatalf("host rejection returned %T: %v", err, err)
		}
	}
	project := evaluate(`() => '{"value":7}'`)
	wire, err := adapter.SerializeStructuredClone(object, project)
	if err != nil {
		t.Fatal(err)
	}
	decoder := evaluate(`payload => JSON.parse(payload)`)
	clone, err := adapter.DeserializeStructuredClonePlatform(wire, decoder)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Set("clone", clone); err != nil {
		t.Fatal(err)
	}
	if value := evaluate(`clone.value === 7`); value.Export() != true {
		t.Fatalf("platform projection lost its own payload: %v", value)
	}
}
