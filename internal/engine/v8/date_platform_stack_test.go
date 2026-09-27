//go:build (windows || linux) && amd64

package v8

import (
	"context"
	"testing"
	"time"
)

func TestDatePlatformStackMatchesFrozenChrome(t *testing.T) {
	r := (Factory{}).New()
	defer r.Close()
	r.SetTimeSource(func() time.Time { return time.UnixMilli(1000) })
	value, err := r.Eval(context.Background(), `
(() => {
  Error.prepareStackTrace = (_, frames) =>
    frames.map((f) => ({ name: f.getFunctionName(), file: f.getFileName(), eval: f.isEval() }));
  let captured;
  function authorPrimitive() {
    return new Error().stack;
  }
  const argument = {
    [Symbol.toPrimitive]: function authorCoercion() {
      captured = authorPrimitive();
      return 0;
    },
  };
  function authorDate() {
    new Date(argument);
    return captured;
  }
  return JSON.stringify(authorDate());
})();
`, "")
	if err != nil {
		t.Fatal(err)
	}
	const expected = `[{"name":"authorPrimitive","file":"","eval":false},{"name":"authorCoercion","file":"","eval":false},{"name":"Date","file":null,"eval":false},{"name":"authorDate","file":"","eval":false},{"name":null,"file":"","eval":false},{"name":null,"file":"","eval":false}]`
	if value.String() != expected {
		t.Fatalf("Date coercion stack: %s", value.String())
	}
	// The platform compilation boundary must retain the injected clock.
	clock, err := r.Eval(context.Background(), `Date.now()===1000 && +new Date()===1000 && +new Date(0)===0`, "clock-check.js")
	if err != nil || clock.Export() != true {
		t.Fatalf("clock semantics: %v %v", clock, err)
	}
	r.(*adapter).ReleaseValue(clock)
	r.(*adapter).ReleaseValue(value)
}
