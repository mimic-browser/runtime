//go:build (windows || linux) && amd64

package v8

import (
	gov8 "github.com/maclof/gov8"
	"github.com/moreveal/mimic/internal/engine"
)

func (a *adapter) DebuggerValueKind(value engine.Value) (string, error) {
	kind := "object"
	_, err := a.withCloneScope(func(_ *gov8.Isolate, _ *gov8.Context, scope *gov8.Scope) (engine.Value, error) {
		v, err := a.cloneLocal(scope, value)
		if err != nil {
			return nil, err
		}
		for _, test := range []struct {
			name  string
			check func() (bool, error)
		}{
			{"proxy", v.IsProxy}, {"function", v.IsFunction}, {"array", v.IsArray}, {"error", v.IsNativeError},
			{"promise", v.IsPromise}, {"map", v.IsMap}, {"set", v.IsSet}, {"weakmap", v.IsWeakMap}, {"weakset", v.IsWeakSet},
			{"date", v.IsDate}, {"regexp", v.IsRegExp}, {"arraybuffer", v.IsArrayBuffer}, {"typedarray", v.IsTypedArray},
		} {
			match, err := test.check()
			if err != nil {
				return nil, err
			}
			if match {
				kind = test.name
				break
			}
		}
		return nil, nil
	})
	return kind, err
}
