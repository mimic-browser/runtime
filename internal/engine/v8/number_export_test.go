//go:build (windows || linux) && amd64

package v8

import (
	"context"
	"math"
	"testing"

	"github.com/moreveal/mimic/internal/engine"
)

func TestNumberExportPreservesSpecialValues(t *testing.T) {
	runtime := (Factory{}).New().(*adapter)
	defer runtime.Close()
	identity := func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		return runtime.Value(args[0].Export()), nil
	}
	if err := runtime.Set("plainNumber", runtime.TransientFunction(identity)); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Set("packedNumber", runtime.PackedFunction(identity, "n")); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"NaN", "Infinity", "-Infinity", "-0", "0", "1.25"} {
		t.Run(source, func(t *testing.T) {
			value, err := runtime.Eval(context.Background(), source, "number-export")
			if err != nil {
				t.Fatal(err)
			}
			number, ok := value.Export().(float64)
			if !ok {
				t.Fatalf("number exported as %T instead of float64", value.Export())
			}
			if source == "NaN" && !math.IsNaN(number) || source == "Infinity" && !math.IsInf(number, 1) || source == "-Infinity" && !math.IsInf(number, -1) || source == "-0" && !math.Signbit(number) {
				t.Fatalf("special numeric identity changed: %s -> %v", source, number)
			}
			result, err := runtime.Eval(context.Background(), "Object.is(plainNumber("+source+"),"+source+") && Object.is(packedNumber("+source+"),"+source+")", "number-host-roundtrip")
			if err != nil || result.Export() != true {
				t.Fatalf("host roundtrip: value=%v err=%v", result, err)
			}
		})
	}
}
