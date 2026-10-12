package browser

import (
	"context"
	"github.com/moreveal/mimic/internal/engine"
	"github.com/moreveal/mimic/internal/trace"
)

func (r *Realm) debuggerConsole(name string, values engine.Value) {
	for debugger := range r.agent.Page().debuggers {
		if debugger.Console == nil || (debugger.ConsoleEnabled != nil && !debugger.ConsoleEnabled()) {
			continue
		}
		state, err := debugger.state(context.Background(), r.agent.ContextID(), r.ID)
		if err != nil {
			r.agent.Page().trace.Add(trace.Error, "consoleInspection", map[string]any{"error": err.Error(), "realm": r.ID})
			continue
		}
		result, err := state.json(context.Background(), "console", map[string]any{}, values)
		if err != nil {
			r.agent.Page().trace.Add(trace.Error, "consoleInspection", map[string]any{"error": err.Error(), "realm": r.ID})
			continue
		}
		args, _ := result["args"].([]any)
		debugger.Console(r.ID, name, args)
	}
}
