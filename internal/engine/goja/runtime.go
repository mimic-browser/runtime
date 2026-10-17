package gojaengine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/big"
	"reflect"
	"time"

	"github.com/dop251/goja"
	"github.com/moreveal/mimic/internal/engine"
)

type Factory struct{}

func (Factory) New() engine.Runtime {
	vm := goja.New()
	// Goja creates descriptor fields with ordinary assignments. Use private
	// pristine intrinsics so author Object.prototype setters cannot observe
	// descriptor construction. This VM executes no author code or jobs.
	metadata := goja.New()
	descriptor, _ := goja.AssertFunction(metadata.Get("Object").ToObject(metadata).Get("getOwnPropertyDescriptor"))
	hasOwn, _ := goja.AssertFunction(metadata.Get("Object").ToObject(metadata).Get("prototype").ToObject(metadata).Get("hasOwnProperty"))
	return &runtime{vm: vm, nativeDescriptor: descriptor, nativeHasOwn: hasOwn}
}

type value struct{ v goja.Value }

func (v value) Export() any    { return v.v.Export() }
func (v value) String() string { return v.v.String() }

type runtime struct {
	vm               *goja.Runtime
	nativeDescriptor goja.Callable
	nativeHasOwn     goja.Callable
}

func (r *runtime) Eval(ctx context.Context, source, name string) (engine.Value, error) {
	return r.run(ctx, func() (goja.Value, error) { return r.vm.RunScript(name, source) })
}

func (r *runtime) run(ctx context.Context, execute func() (goja.Value, error)) (engine.Value, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	finished := make(chan struct{})
	watcherDone := make(chan struct{})
	go func() {
		defer close(watcherDone)
		select {
		case <-ctx.Done():
			select {
			case <-finished:
				return
			default:
				r.vm.Interrupt(ctx.Err())
			}
		case <-finished:
		}
	}()
	v, err := execute()
	close(finished)
	<-watcherDone
	r.vm.ClearInterrupt()
	if err != nil {
		return nil, r.jsError(err)
	}
	return value{v}, nil
}

func (r *runtime) nativeValue(v any) any {
	if wrapped, ok := v.(value); ok {
		return wrapped.v
	}
	out, _ := r.binaryProjection(v, nil)
	return out
}

// Retain the existing live Go map/slice projection when no explicit binary
// value occurs. Only records containing explicit binary values need a converted copy.
func (r *runtime) binaryProjection(v any, path []uintptr) (any, bool) {
	// Ordinary Go projections may be cyclic. Leave back edges untouched; this
	// conversion is only needed for acyclic, by-value binary transport records.
	switch v.(type) {
	case map[string]any, []any:
		identity := uintptr(reflect.ValueOf(v).UnsafePointer())
		for _, parent := range path {
			if parent == identity {
				return v, false
			}
		}
		path = append(path, identity)
	}
	switch v := v.(type) {
	case engine.BinaryString:
		return v.UTF8(), true
	case engine.BinaryBuffer:
		return r.vm.NewArrayBuffer(append([]byte(nil), v...)), true
	case map[string]any:
		var out map[string]any
		for key, member := range v {
			converted, changed := r.binaryProjection(member, path)
			if changed {
				if out == nil {
					out = make(map[string]any, len(v))
					for name, original := range v {
						out[name] = original
					}
				}
				out[key] = converted
			}
		}
		if out != nil {
			return out, true
		}
	case []any:
		var out []any
		for index, member := range v {
			converted, changed := r.binaryProjection(member, path)
			if changed {
				if out == nil {
					out = append([]any(nil), v...)
				}
				out[index] = converted
			}
		}
		if out != nil {
			return out, true
		}
	}
	return v, false
}

func (r *runtime) Set(name string, v any) error { return r.vm.Set(name, r.nativeValue(v)) }
func (r *runtime) Get(name string) engine.Value {
	v := r.vm.Get(name)
	if v == nil {
		v = goja.Undefined()
	}
	return value{v}
}
func (r *runtime) Value(v any) engine.Value { return value{r.vm.ToValue(r.nativeValue(v))} }
func (r *runtime) GetProperty(v engine.Value, name string) engine.Value {
	property := unwrap(v).ToObject(r.vm).Get(name)
	if property == nil {
		property = goja.Undefined()
	}
	return value{property}
}
func (r *runtime) SetProperty(v engine.Value, name string, x any) error {
	return unwrap(v).ToObject(r.vm).Set(name, r.nativeValue(x))
}
func (r *runtime) TypeOf(v engine.Value) string {
	x := unwrap(v)
	if x == nil || goja.IsUndefined(x) {
		return "undefined"
	}
	if goja.IsNull(x) {
		return "object"
	}
	if _, ok := goja.AssertFunction(x); ok {
		return "function"
	}
	// Exporting an object reads enumerable properties and can invoke getters.
	// JavaScript typeof never observes those properties or converts the object.
	if _, ok := x.(*goja.Object); ok {
		return "object"
	}
	if _, ok := x.(*goja.Symbol); ok {
		return "symbol"
	}
	switch x.Export().(type) {
	case bool:
		return "boolean"
	case string:
		return "string"
	case int, int32, int64, float32, float64:
		return "number"
	case *big.Int:
		return "bigint"
	default:
		return "object"
	}
}

func (r *runtime) StrictEqual(a, b engine.Value) bool {
	return unwrap(a).StrictEquals(unwrap(b))
}

func unwrap(v engine.Value) goja.Value {
	if v == nil {
		return goja.Undefined()
	}
	if x, ok := v.(value); ok {
		return x.v
	}
	panic("value belongs to another JavaScript engine")
}

func (r *runtime) Call(ctx context.Context, fn, this engine.Value, args ...engine.Value) (engine.Value, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, ok := goja.AssertFunction(unwrap(fn))
	if !ok {
		return nil, fmt.Errorf("value is not callable")
	}
	a := make([]goja.Value, len(args))
	for i := range args {
		a[i] = unwrap(args[i])
	}
	finished := make(chan struct{})
	watcherDone := make(chan struct{})
	go func() {
		defer close(watcherDone)
		select {
		case <-ctx.Done():
			select {
			case <-finished:
				return
			default:
				r.vm.Interrupt(ctx.Err())
			}
		case <-finished:
		}
	}()
	v, err := f(unwrap(this), a...)
	close(finished)
	<-watcherDone
	r.vm.ClearInterrupt()
	if err != nil {
		return nil, r.jsError(err)
	}
	return value{v}, nil
}

func (r *runtime) jsError(err error) error {
	// Exception.Unwrap and String can both execute author code. Classify the
	// engine's caught exception before generic Go error traversal, and retain
	// its native stack without coercing the thrown object.
	if exception, ok := err.(*goja.Exception); ok {
		var text bytes.Buffer
		thrown := exception.Value()
		if object, ok := thrown.(*goja.Object); ok {
			text.WriteString(object.ClassName() + " exception")
			// Native Error's message descriptor is passive; its stack descriptor
			// is not. Keep useful own string messages without invoking accessors
			// or consulting author replacements for the reflection intrinsics.
			if object.ClassName() == "Error" {
				if message := r.ownErrorMessage(object); message != "" {
					text.WriteString(": " + message)
				}
			}
		} else if thrown != nil {
			text.WriteString(thrown.String())
		}
		for _, frame := range exception.Stack() {
			text.WriteString("\n\tat ")
			frame.Write(&text)
		}
		return &callException{error: errors.New(text.String()), value: thrown}
	}
	var interrupted *goja.InterruptedError
	if errors.As(err, &interrupted) {
		if cause, ok := interrupted.Value().(error); ok {
			return fmt.Errorf("%w: %s", cause, interrupted.String())
		}
		return fmt.Errorf("JavaScript execution interrupted: %v", interrupted.Value())
	}
	return err
}

func (r *runtime) ownErrorMessage(object *goja.Object) string {
	desc, err := r.nativeDescriptor(goja.Undefined(), object, r.vm.ToValue("message"))
	if err != nil || goja.IsUndefined(desc) {
		return ""
	}
	own, err := r.nativeHasOwn(desc, r.vm.ToValue("value"))
	if err != nil || !own.ToBoolean() {
		return ""
	}
	message := desc.(*goja.Object).Get("value")
	if message.ExportType() == reflect.TypeOf("") {
		return message.String()
	}
	return ""
}

type callException struct {
	error
	value goja.Value
}

func (e *callException) ThrownValue() engine.Value { return value{e.value} }
func (e *callException) Unwrap() error             { return e.error }

func (r *runtime) Function(fn engine.Function) any {
	return func(call goja.FunctionCall) goja.Value {
		args := make([]engine.Value, len(call.Arguments))
		for i := range call.Arguments {
			args[i] = value{call.Arguments[i]}
		}
		v, err := fn(value{call.This}, args)
		if err != nil {
			var thrown *callException
			if errors.As(err, &thrown) {
				panic(thrown.value)
			}
			panic(r.vm.NewTypeError("%s", err))
		}
		return unwrap(v)
	}
}

func (r *runtime) SetGlobalAccessObserver(observer func(name string, supported bool)) {
	target := r.vm.GlobalObject()
	seen := make(map[string]struct{})
	report := func(property string, supported bool) {
		if _, ok := seen[property]; ok {
			return
		}
		seen[property] = struct{}{}
		observer(property, supported)
	}
	reflect := r.vm.Get("Reflect").ToObject(r.vm)
	hasFn, _ := goja.AssertFunction(reflect.Get("has"))
	getFn, _ := goja.AssertFunction(reflect.Get("get"))
	has := func(property string) bool {
		v, err := hasFn(goja.Undefined(), target, r.vm.ToValue(property))
		return err == nil && v.ToBoolean()
	}
	proxy := r.vm.NewProxy(target, &goja.ProxyTrapConfig{
		Has: func(_ *goja.Object, property string) bool {
			supported := has(property)
			report(property, supported)
			return supported
		},
		Get: func(_ *goja.Object, property string, receiver goja.Value) goja.Value {
			// Support is observed once per property. Do not reenter Reflect.has
			// for a trace event that report would discard. The Has trap above
			// still queries live state for JavaScript membership operations.
			if _, recorded := seen[property]; !recorded {
				report(property, has(property))
			}
			// Observation must preserve accessor receivers, including inherited
			// access through an object whose prototype is the global proxy.
			v, err := getFn(goja.Undefined(), target, r.vm.ToValue(property), receiver)
			if err != nil {
				panic(err)
			}
			return v
		},
	})
	r.vm.SetGlobalObject(r.vm.ToValue(proxy).(*goja.Object))
}

func (r *runtime) NewPromise() engine.Promise {
	p, resolve, reject := r.vm.NewPromise()
	return engine.Promise{
		Value:   value{r.vm.ToValue(p)},
		Resolve: func(v any) error { return resolve(r.nativeValue(v)) },
		Reject:  func(v any) error { return reject(r.nativeValue(v)) },
	}
}
func (r *runtime) Await(v engine.Value) (engine.Value, bool, error) {
	raw := unwrap(v)
	// goja can return a nil Value while a realm is being replaced by an
	// asynchronous navigation. Treat it as JavaScript undefined at the neutral
	// engine boundary instead of leaking a Go nil-pointer panic.
	if raw == nil {
		return value{goja.Undefined()}, true, nil
	}
	object, isObject := raw.(*goja.Object)
	if !isObject || object.ExportType() != reflect.TypeOf((*goja.Promise)(nil)) {
		return v, true, nil
	}
	p := object.Export().(*goja.Promise)
	switch p.State() {
	case goja.PromiseStateFulfilled:
		return value{p.Result()}, true, nil
	case goja.PromiseStateRejected:
		return nil, true, &callException{error: errors.New("promise rejected"), value: p.Result()}
	default:
		return v, false, nil
	}
}
func (r *runtime) SetTimeSource(now func() time.Time) { r.vm.SetTimeSource(now) }

// goja drains its Promise job queue when control returns from RunScript or a
// host call. An empty script provides an explicit browser scheduler checkpoint.
func (r *runtime) MicrotaskCheckpoint() error {
	_, err := r.vm.RunString("void 0")
	return err
}
func (r *runtime) Close() error { return nil }

func (r *runtime) DetachArrayBuffer(v engine.Value) error {
	object, ok := unwrap(v).(*goja.Object)
	if !ok || object.ExportType() != reflect.TypeOf(goja.ArrayBuffer{}) {
		return errors.New("expected ArrayBuffer")
	}
	buffer, ok := object.Export().(goja.ArrayBuffer)
	if !ok {
		return errors.New("expected ArrayBuffer")
	}
	buffer.Detach()
	return nil
}
