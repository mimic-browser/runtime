//go:build (windows || linux) && amd64

package v8

import (
	"context"
	"fmt"

	gov8 "github.com/maclof/gov8"
	"github.com/moreveal/mimic/internal/engine"
)

// SetEvalSourceResolver retains a realm-owned brand check, not a second registry
// of trusted values. V8 still owns compilation and direct/indirect eval scope.
// All dynamic code creation passes through the resolver, including constructors
// reached through intrinsic prototypes and saved references.
func (a *adapter) SetEvalSourceResolver(resolver engine.Value) error {
	_, err := a.run(func(s *state, realm *gov8.Context, scope *gov8.Scope) (engine.Value, error) {
		if err := a.installEvalSourceResolver(s, realm, scope, resolver); err != nil {
			return nil, err
		}
		a.evalSourceResolver = resolver
		return nil, realm.AllowCodeGenerationFromStrings(false)
	})
	return err
}

func (a *adapter) SetEvalSourceUnconstrained(unconstrained bool) error {
	_, err := a.run(func(_ *state, realm *gov8.Context, _ *gov8.Scope) (engine.Value, error) {
		return nil, realm.AllowCodeGenerationFromStrings(unconstrained)
	})
	return err
}

func (a *adapter) installEvalSourceResolver(s *state, realm *gov8.Context, scope *gov8.Scope, resolver engine.Value) error {
	local, err := a.local(scope, resolver)
	if err != nil {
		return err
	}
	if callable, err := local.IsFunction(); err != nil || !callable {
		return fmt.Errorf("eval source resolver must be a function")
	}
	return s.isolate.SetModifyCodeGenerationFromStringsCallback(func(source gov8.Value, isCodeLike bool) (bool, *string) {
		// This callback already runs on the owning isolate thread. A nested
		// scope bounds all temporary handles to this one eval operation.
		scope, err := s.isolate.NewScope()
		if err != nil {
			return false, nil
		}
		defer scope.Close()
		local, err := a.local(scope, resolver)
		if err != nil {
			return false, nil
		}
		fn, ok, err := gov8.AsFunction(local, realm)
		if err != nil || !ok {
			return false, nil
		}
		receiver, err := scope.Undefined()
		if err != nil {
			return false, nil
		}
		codeLike, err := scope.Boolean(isCodeLike)
		if err != nil {
			return false, nil
		}
		catcher, err := s.isolate.NewTryCatch()
		if err != nil {
			return false, nil
		}
		unsafeEval, err := scope.Boolean(a.debuggerUnsafeEval)
		if err != nil {
			_ = catcher.Close()
			return false, nil
		}
		result, ok, err := fn.Call(scope, receiver, source, codeLike, unsafeEval)
		if err != nil || !ok {
			// Preserve callback exception identity; returning false alone would
			// replace a thrown application object with an engine EvalError.
			if caught, _ := catcher.HasCaught(); caught {
				_, _, _ = catcher.ReThrow(scope)
			} else {
				_ = catcher.Close()
			}
			return false, nil
		}
		_ = catcher.Close()
		if isString, err := result.IsString(); err != nil || !isString {
			return err == nil, nil
		}
		text, err := result.StringValue()
		if err != nil {
			return false, nil
		}
		return true, &text
	})
}

func (a *adapter) RunWithUnsafeEval(ctx context.Context, operation func(context.Context) error) error {
	_, err := a.runContext(ctx, func(_ *state, _ *gov8.Context, _ *gov8.Scope) (engine.Value, error) {
		previous := a.debuggerUnsafeEval
		a.debuggerUnsafeEval = true
		defer func() { a.debuggerUnsafeEval = previous }()
		return nil, operation(ctx)
	})
	return err
}
