//go:build (windows || linux) && amd64

package v8

import (
	"context"
	"errors"
	"testing"

	"github.com/moreveal/mimic/internal/engine"
)

func TestRealmCallPreservesOwnerAndCancellation(t *testing.T) {
	parent := (Factory{}).New().(*adapter)
	defer parent.Close()
	child, _, err := parent.NewRealmRuntime(false)
	if err != nil {
		t.Fatal(err)
	}
	defer child.Close()
	independent := (Factory{}).New()
	defer independent.Close()
	for _, target := range []engine.Runtime{child, independent} {
		err := parent.RunOnOwner(context.Background(), func(ctx context.Context) error {
			return parent.RunRealmCall(ctx, target, func(ctx context.Context) error {
				if target == child && currentThreadID() != parent.owner.actorTID {
					t.Error("shared-owner call left the execution thread")
				}
				// A different owner must still be able to call back synchronously.
				_, err := target.Call(ctx, target.Value(target.Function(func(engine.Value, []engine.Value) (engine.Value, error) {
					value, err := parent.Eval(ctx, "6 * 7", "realm-call-back")
					if err != nil {
						return nil, err
					}
					return target.Value(value.Export()), nil
				})), nil)
				return err
			})
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	err = parent.RunOnOwner(context.Background(), func(context.Context) error {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		previous := parent.activeContext
		parent.activeContext = ctx
		defer func() { parent.activeContext = previous }()
		return parent.RunRealmCall(context.Background(), child, func(context.Context) error {
			t.Error("cancelled calling operation admitted a realm call")
			return nil
		})
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("active cancellation lost: %v", err)
	}
}
