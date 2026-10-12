package gojaengine

import (
	"github.com/dop251/goja"
	"github.com/moreveal/mimic/internal/engine"
	"reflect"
)

func (r *runtime) IsStructuredCloneProxy(v engine.Value) bool {
	return unwrap(v).ExportType() == reflect.TypeOf(goja.Proxy{})
}

func (r *runtime) IsDebuggerProxy(v engine.Value) (bool, error) {
	return r.IsStructuredCloneProxy(v), nil
}
