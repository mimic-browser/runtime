//go:build (windows || linux) && amd64

package v8

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"github.com/moreveal/mimic/internal/engine"
)

func TestBootstrapKeyForMatchesCopiedSource(t *testing.T) {
	for _, source := range []string{"", "a", strings.Repeat("globalThis.value=42;", 32768)} {
		got := bootstrapKeyFor(source, "bootstrap")
		want := bootstrapKey{source: sha256.Sum256([]byte(source)), name: "bootstrap"}
		if got != want {
			t.Fatal("bootstrap key changed")
		}
	}
}

var bootstrapKeyAllocationSink bootstrapKey

func TestBootstrapKeyForDoesNotCopyLargeSource(t *testing.T) {
	source := strings.Repeat("globalThis.value=42;", 32768)
	if allocations := testing.AllocsPerRun(10, func() {
		bootstrapKeyAllocationSink = bootstrapKeyFor(source, "bootstrap")
	}); allocations != 0 {
		t.Fatalf("bootstrap key allocations = %g, want zero", allocations)
	}
}

func TestBootstrapCodeCacheRetainsCodeNotRealmState(t *testing.T) {
	// Exceed the native cache reader's initial 4 KiB buffer. Include executed
	// functions as well as nested closures, and destroy the producing isolate.
	var source strings.Builder
	source.WriteString("(()=>{const object={};")
	for i := 0; i < 300; i++ {
		fmt.Fprintf(&source, "object.f%d=()=>hostSeed()+%d;object.f%d();", i, i, i)
	}
	source.WriteString("globalThis.bootstrapObject=object;globalThis.bootstrapArray=[];})()")
	code := source.String()
	const name = "bootstrap-isolation-test"
	run := func(t *testing.T, seed int) {
		r := (Factory{}).New().(*adapter)
		defer r.Close()
		if err := r.Set("hostSeed", r.Function(func(engine.Value, []engine.Value) (engine.Value, error) {
			return r.Value(seed), nil
		})); err != nil {
			t.Fatal(err)
		}
		if _, err := r.EvalBootstrap(context.Background(), code, name); err != nil {
			t.Fatal(err)
		}
		value, err := r.Eval(context.Background(), fmt.Sprintf("bootstrapObject.f299()===%d&&bootstrapArray.length===0&&Array.prototype.cachedRealmMutation===undefined", seed+299), "verify")
		if err != nil || value.Export() != true {
			t.Fatalf("realm state: %v %v", value, err)
		}
		if _, err := r.Eval(context.Background(), "bootstrapArray.push(1);Array.prototype.cachedRealmMutation=1", "mutate"); err != nil {
			t.Fatal(err)
		}
	}
	run(t, 10)
	if cache := bootstrapCode.get(bootstrapKeyFor(code, name)); cache == nil || cache.Len() <= 4096 {
		t.Fatalf("expected a large compiled cache, got %v", cache)
	}
	for i := 0; i < 4; i++ {
		t.Run(fmt.Sprint(i), func(t *testing.T) { t.Parallel(); run(t, 100+i) })
	}
}

func TestBootstrapCacheKeysExactSource(t *testing.T) {
	for _, source := range []string{"globalThis.answer=41", "globalThis.answer=42"} {
		r := (Factory{}).New().(*adapter)
		_, err := r.EvalBootstrap(context.Background(), source, "same-bootstrap-name")
		if err != nil {
			t.Fatal(err)
		}
		if got := r.Get("answer").String(); got != source[len(source)-2:] {
			t.Fatalf("wrong source: %s", got)
		}
		_ = r.Close()
	}
}

func TestOwnerBatchKeepsCallbacksAndMicrotasksOrdered(t *testing.T) {
	r := (Factory{}).New().(*adapter)
	defer r.Close()
	err := r.RunOnOwner(context.Background(), func(ctx context.Context) error {
		if err := r.Set("read", r.Function(func(engine.Value, []engine.Value) (engine.Value, error) { return r.Get("answer"), nil })); err != nil {
			return err
		}
		value, err := r.Eval(ctx, "globalThis.answer=42;globalThis.order=[];Promise.resolve().then(()=>order.push('micro'));order.push(read());order.join(',')", "batch")
		if err != nil {
			return err
		}
		if value.String() != "42" {
			return fmt.Errorf("unexpected order %s", value.String())
		}
		return r.RunOnOwner(ctx, func(context.Context) error {
			if r.Get("order").String() != "42" {
				return fmt.Errorf("premature checkpoint")
			}
			return nil
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.MicrotaskCheckpoint(); err != nil {
		t.Fatal(err)
	}
	if got := r.Get("order").String(); got != "42,micro" {
		t.Fatal(got)
	}
}
