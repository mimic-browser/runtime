//go:build (windows || linux) && amd64

package v8

import (
	"context"
	"math"
	"testing"

	"github.com/moreveal/mimic/internal/engine"
)

func TestHostRecordsPreserveNestedNativeValues(t *testing.T) {
	for _, mode := range []string{"ordinary", "transient", "packed"} {
		t.Run(mode, func(t *testing.T) {
			r := (Factory{}).New().(*adapter)
			defer r.Close()
			ctx := context.Background()
			shared, err := r.Eval(ctx, `globalThis.shared = {
  object: { marker: 42 },
  function: (value) => value + 1,
  bytes: new Uint8Array([1, 2, 3]),
};
shared;`, "native-record-values")
			if err != nil {
				t.Fatal(err)
			}
			record := func(value engine.Value) map[string]any {
				return map[string]any{"value": value, "items": []any{value, r.GetProperty(value, "function")}}
			}
			if err := r.Set("direct", record(shared)); err != nil {
				t.Fatal(err)
			}
			host := func(_ engine.Value, args []engine.Value) (engine.Value, error) {
				value := shared
				if mode != "packed" {
					value = args[0]
				}
				return r.Value(record(value)), nil
			}
			var function any
			switch mode {
			case "ordinary":
				function = r.Function(host)
			case "transient":
				function = r.TransientFunction(host)
			case "packed":
				function = r.PackedFunction(host, "n")
			}
			if err := r.Set("record", function); err != nil {
				t.Fatal(err)
			}
			argument := "shared"
			if mode == "packed" {
				argument = "42"
			}
			value, err := r.Eval(ctx, `(() => {
  const returned = record(`+argument+`);
  return [direct, returned].every(
    (value) =>
      value.value === shared &&
      value.items[0] === shared &&
      value.items[1] === shared.function &&
      value.items[1](41) === 42 &&
      value.value.bytes === shared.bytes,
  );
})();`, "native-record-identity")
			if err != nil || value == nil || value.Export() != true {
				t.Fatalf("nested native values: %v, %v", value, err)
			}
		})
	}
}

func TestHostRecordsPreserveSmallIntegersWithoutJSON(t *testing.T) {
	r := (Factory{}).New().(*adapter)
	defer r.Close()
	record := map[string]any{
		"integers": []any{int8(-128), int16(-32768), uint8(255), uint16(65535)},
		"bytes":    []byte{0, 128, 255},
		"nan":      math.NaN(), // Forces the recursive callback conversion path.
	}
	if err := r.Set("direct", record); err != nil {
		t.Fatal(err)
	}
	// Go integers must use native conversion even if author code replaces JSON.
	if _, err := r.Eval(context.Background(), `JSON.parse = () => {
  throw new Error("author JSON.parse");
};`, "replace-json-parse"); err != nil {
		t.Fatal(err)
	}
	if err := r.Set("direct", record); err != nil {
		t.Fatal(err)
	}
	if err := r.Set("record", r.TransientFunction(func(engine.Value, []engine.Value) (engine.Value, error) {
		for _, number := range record["integers"].([]any) {
			if got := r.TypeOf(r.Value(number)); got != "number" {
				t.Errorf("host %T has type %q, want number", number, got)
			}
		}
		return r.Value(record), nil
	})); err != nil {
		t.Fatal(err)
	}
	value, err := r.Eval(context.Background(), `(() => {
  const expected = [-128, -32768, 255, 65535];
  return [direct, record()].every(
    (value) =>
      value.integers.every((number, index) => number === expected[index]) &&
      value.bytes.every((number, index) => number === [0, 128, 255][index]) &&
      Number.isNaN(value.nan),
  );
})();`, "small-integer-record")
	if err != nil || value == nil || value.Export() != true {
		t.Fatalf("small integer record: %v, %v", value, err)
	}
}

func TestHostRecordsRejectNestedForeignValues(t *testing.T) {
	r := (Factory{}).New().(*adapter)
	defer r.Close()
	foreign := (Factory{}).New()
	defer foreign.Close()
	value, err := foreign.Eval(context.Background(), `({ marker: 42 });`, "foreign-value")
	if err != nil {
		t.Fatal(err)
	}
	record := map[string]any{"items": []any{value}}
	if err := r.Set("direct", record); err == nil {
		t.Fatal("Set admitted a value from another runtime")
	}
	if err := r.Set("record", r.TransientFunction(func(engine.Value, []engine.Value) (engine.Value, error) {
		return r.Value(record), nil
	})); err != nil {
		t.Fatal(err)
	}
	result, err := r.Eval(context.Background(), `(() => {
  try {
    record();
    return false;
  } catch (error) {
    return error.message.includes("not local to this V8 invocation");
  }
})();`, "reject-foreign-record")
	if err != nil || result == nil || result.Export() != true {
		t.Fatalf("foreign value ownership: %v, %v", result, err)
	}
}
