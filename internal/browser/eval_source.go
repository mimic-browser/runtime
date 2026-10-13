package browser

import (
	"github.com/moreveal/mimic/internal/csp"
	"github.com/moreveal/mimic/internal/engine"
)

func installEvalSourceResolver(runtime engine.Runtime) error {
	if native, ok := runtime.(engine.EvalSourceRuntime); ok {
		return native.SetEvalSourceResolver(runtime.Get("__mimicEvalSourceResolver"))
	}
	// Fallback engines retain native eval; wrapping it in JavaScript would
	// silently change direct eval scope even for ordinary strings.
	return nil
}

func setEvalSourcePolicy(runtime engine.Runtime, state csp.TrustedTypesState) error {
	if native, ok := runtime.(engine.EvalSourceRuntime); ok {
		return native.SetEvalSourceUnconstrained(!state.Required && state.EvalBlocked == "")
	}
	return nil
}
