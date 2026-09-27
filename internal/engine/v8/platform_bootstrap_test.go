//go:build (windows || linux) && amd64

package v8

import (
	"context"
	_ "embed"
	"strings"
	"testing"

	"github.com/moreveal/mimic/internal/engine"
)

//go:embed testdata/platform_bootstrap_stack.js
var platformBootstrapStackProbe string

func TestPlatformBootstrapStackBoundary(t *testing.T) {
	ctx := context.Background()
	const source = `globalThis.platformEntry = function platformEntry(callback) {
  if (callback) return callback();
  throw new TypeError('platform failure');
};`
	verify := func(t *testing.T, r engine.Runtime) {
		t.Helper()
		value, err := r.Eval(ctx, platformBootstrapStackProbe, "author-probe.js")
		if err != nil || value.String() != "ok" {
			t.Fatalf("platform stack boundary: %v %v", value, err)
		}
		// A resource name is not authority to compile author code as platform code.
		value, err = r.Eval(ctx, `(() => {
  function authorWithInternalName() { throw new Error('author'); }
  try { authorWithInternalName(); } catch (error) { return error.stack; }
})()`, "mimic:webapi-surface")
		if err != nil || !strings.Contains(value.String(), "at authorWithInternalName (mimic:webapi-surface:") {
			t.Fatalf("author resource-name boundary: %v %v", value, err)
		}
	}
	for _, name := range []string{"cold", "cached"} {
		t.Run(name, func(t *testing.T) {
			r := (Factory{}).New().(*adapter)
			defer r.Close()
			if _, err := r.EvalBootstrap(ctx, source, "platform-stack-boundary"); err != nil {
				t.Fatal(err)
			}
			verify(t, r)
		})
	}
	if cached := bootstrapCode.get(bootstrapKeyFor(source, "platform-stack-boundary")); cached == nil {
		t.Fatal("bootstrap code cache was not produced")
	}
	snapshot, err := (Factory{}).BuildBootstrapSnapshot(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	t.Run("snapshot", func(t *testing.T) {
		r, err := snapshot.NewRuntime()
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		verify(t, r)
	})
}
