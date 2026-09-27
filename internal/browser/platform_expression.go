package browser

import (
	"context"
	"strings"

	"github.com/moreveal/mimic/internal/engine"
)

// These closures perform platform reflection and may invoke author getters or
// functions. Admit their provenance at compilation; resource names alone are
// not authority, and no captured stack is formatted or filtered here.
func evalPlatformExpression(runtime engine.Runtime, source, name string) (engine.Value, error) {
	if bootstrap, ok := runtime.(engine.BootstrapRuntime); ok {
		expression := strings.TrimSuffix(strings.TrimSpace(source), ";")
		return bootstrap.EvalBootstrap(context.Background(), "return ("+expression+");", name)
	}
	return runtime.Eval(context.Background(), source, name)
}
