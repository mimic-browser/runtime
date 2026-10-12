package gojaengine

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/dop251/goja"
	"github.com/moreveal/mimic/internal/engine"
)

func (r *runtime) DebuggerOwnPropertyDescriptor(subject, key engine.Value) (engine.Value, error) {
	if r.IsStructuredCloneProxy(subject) {
		return nil, fmt.Errorf("Opaque object property inspection is unsupported")
	}
	raw, rawKey := unwrap(subject), unwrap(key)
	if object, ok := raw.(*goja.Object); ok && object.ClassName() == "Error" && rawKey == r.vm.ToValue("stack") {
		return nil, fmt.Errorf("Passive Error.stack descriptor inspection is unsupported by this engine")
	}
	desc, err := r.nativeDescriptor(goja.Undefined(), raw, rawKey)
	if err != nil {
		return nil, r.jsError(err)
	}
	return value{desc}, nil
}

func (r *runtime) DebuggerValueKind(value engine.Value) (string, error) {
	raw := unwrap(value)
	if r.IsStructuredCloneProxy(value) {
		return "proxy", nil
	}
	if _, callable := goja.AssertFunction(raw); callable {
		return "function", nil
	}
	object, ok := raw.(*goja.Object)
	if !ok {
		return "object", nil
	}
	class := object.ClassName()
	switch class {
	case "Error":
		return "error-opaque", nil
	case "Array", "Date", "RegExp", "Map", "Set", "WeakMap", "WeakSet", "Promise":
		return strings.ToLower(class), nil
	}
	kind := object.ExportType()
	if kind == reflect.TypeOf([][2]interface{}{}) {
		return "map", nil
	}
	// Native arrays (including Go slice projections) have class Array; Set is
	// the remaining Object-class value with Goja's []interface{} export type.
	if class == "Object" && kind == reflect.TypeOf([]interface{}{}) {
		return "set", nil
	}
	if kind == reflect.TypeOf(goja.ArrayBuffer{}) {
		return "arraybuffer", nil
	}
	if kind == reflect.TypeOf((*goja.Promise)(nil)) {
		return "promise", nil
	}
	if kind != nil && kind.Kind() == reflect.Slice {
		switch kind.Elem().Kind() {
		case reflect.Int8, reflect.Uint8, reflect.Int16, reflect.Uint16, reflect.Int32, reflect.Uint32,
			reflect.Int64, reflect.Uint64, reflect.Float32, reflect.Float64:
			return "typedarray", nil
		}
	}
	return "object", nil
}
