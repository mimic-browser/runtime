package browser

import (
	"context"
	"fmt"

	"github.com/moreveal/mimic/internal/engine"
)

func (r *Realm) installCSSPropertyRegistrationHosts(host map[string]any) {
	host["installCSSPropertyRegistration"] = r.fn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		r.cssPropertyRegistration = args[0]
		return nil, nil
	})
	host["registerCSSProperty"] = r.transientFn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		owner := r
		if r.mainWorld != nil {
			owner = r.mainWorld
		}
		if owner.closed || owner.cssPropertyRegistration == nil {
			return nil, fmt.Errorf("CSS registration owner is unavailable")
		}
		payload := strarg(args, 0)
		var failure string
		run := func(ctx context.Context) error {
			return owner.runOnOwner(ctx, func(ctx context.Context) error {
				restore := r.enterFrameDocumentEntry(owner)
				defer restore()
				encoded := owner.val(payload)
				defer releaseDebuggerValue(owner, encoded)
				result, err := owner.runtime.Call(ctx, owner.cssPropertyRegistration, nil, encoded)
				defer releaseDebuggerValue(owner, result)
				if err == nil {
					failure = result.String()
				}
				return err
			})
		}
		var err error
		if nested, ok := r.runtime.(engine.ReentrantRuntime); ok {
			err = nested.RunNested(context.Background(), run)
		} else {
			err = run(context.Background())
		}
		return r.val(failure), err
	})
}
