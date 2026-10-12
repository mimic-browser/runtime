//go:build (windows || linux) && amd64

package v8

import (
	"context"
	"errors"
	"fmt"
	"unicode/utf16"

	gov8 "github.com/maclof/gov8"
	"github.com/moreveal/mimic/internal/engine"
)

// The native scope preserves lexical declarations and author source/callers.
// It returns the raw value: even idOnly protocol wrapping can format a lazy
// Error.stack before omitting its description from the response.
func (a *adapter) EvalCommandLine(ctx context.Context, source string, selected []engine.Value, lastConsole engine.Value) (engine.Value, error) {
	return a.runContext(ctx, func(_ *state, realm *gov8.Context, scope *gov8.Scope) (engine.Value, error) {
		values := make([]gov8.Value, len(selected))
		for index, value := range selected {
			if value == nil {
				continue
			}
			local, err := a.local(scope, value)
			if err != nil {
				return nil, err
			}
			values[index] = local
		}
		var last gov8.Value
		if lastConsole != nil {
			var err error
			last, err = a.local(scope, lastConsole)
			if err != nil {
				return nil, err
			}
		}
		// Inspector's 8-bit view is Latin-1, while incoming Go source is UTF-8.
		view := gov8.NewInspectorStringView16(utf16.Encode([]rune(source)))
		local, status, err := realm.EvaluateCommandLine(scope, view, values, last)
		if err != nil {
			return nil, err
		}
		if status == gov8.CommandLineNotRun {
			return nil, fmt.Errorf("Native command-line execution context unavailable")
		}
		value, err := a.persist(scope, local)
		if err != nil {
			return nil, err
		}
		if status == gov8.CommandLineException {
			return nil, &callException{error: errors.New("command-line evaluation threw"), value: value}
		}
		return value, nil
	})
}
