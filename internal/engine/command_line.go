package engine

import "context"

// CommandLineRuntime evaluates in the native inspector's temporary command-line
// scope. It preserves global declaration semantics and never rewrites source.
// Selected values are oldest first; nil denotes an unavailable foreign realm.
type CommandLineRuntime interface {
	EvalCommandLine(context.Context, string, []Value, Value) (Value, error)
}
