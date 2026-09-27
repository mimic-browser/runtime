//go:build (windows || linux) && amd64

package gov8

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"unsafe"
)

// SideEffectType mirrors v8::SideEffectType. It is debugger metadata used by
// inspector evaluations that request throwOnSideEffect; it does not suppress
// callbacks during ordinary execution.
type SideEffectType uint8

const (
	SideEffectHasSideEffect SideEffectType = iota
	SideEffectHasNoSideEffect
	SideEffectHasSideEffectToReceiver
)

func validFunctionOptions(options *FunctionOptions) error {
	if options == nil {
		return nil
	}
	if options.ConstructorBehavior > ConstructorBehaviorAllow {
		return fmt.Errorf("gov8: invalid constructor behavior %d", options.ConstructorBehavior)
	}
	if options.SideEffectType > SideEffectHasSideEffectToReceiver {
		return fmt.Errorf("gov8: invalid side-effect type %d", options.SideEffectType)
	}
	if int64(options.Length) < -1<<31 || int64(options.Length) > 1<<31-1 {
		return fmt.Errorf("gov8: function length %d is outside the int32 range", options.Length)
	}
	return nil
}

// FunctionBuilder is the Go counterpart of v8::FunctionBuilder. Builder
// methods mutate and return the builder so calls can be chained.
type FunctionBuilder struct {
	iso      *Isolate
	callback FunctionCallback
	options  FunctionOptions
}

// FunctionBuilder starts a direct native Function builder.
func (i *Isolate) FunctionBuilder(callback FunctionCallback) *FunctionBuilder {
	return &FunctionBuilder{iso: i, callback: callback}
}

// Length sets the function's length property.
func (b *FunctionBuilder) Length(length int) *FunctionBuilder {
	b.options.Length = length
	return b
}

// Data sets the callback data value.
func (b *FunctionBuilder) Data(data Value) *FunctionBuilder {
	b.options.Data = data
	return b
}

// ConstructorBehavior selects regular (allow) or concise (throw) behavior.
func (b *FunctionBuilder) ConstructorBehavior(behavior ConstructorBehavior) *FunctionBuilder {
	b.options.ConstructorBehavior = behavior
	return b
}

// SideEffectType sets debugger side-effect metadata for the callback.
func (b *FunctionBuilder) SideEffectType(sideEffectType SideEffectType) *FunctionBuilder {
	b.options.SideEffectType = sideEffectType
	return b
}

// Build creates the function in c and binds its local handle to s.
func (b *FunctionBuilder) Build(s *Scope, c *Context) (*Function, error) {
	if b == nil || b.iso == nil {
		return nil, errors.New("gov8: nil function builder")
	}
	return b.iso.NewFunction(s, c, b.callback, &b.options)
}

// SetName sets Function::SetName. V8 intentionally ignores this operation on
// bound functions; no special case is applied by Go.
func (f *Function) SetName(name string) error {
	if err := f.check(); err != nil {
		return err
	}
	nameValue, err := f.sc.NewString(name)
	if err != nil {
		return err
	}
	return callErr("Function.SetName", proc("gov8_fa_function_set_name"),
		f.iso.handleAssumingCheck(), f.sc.handle, f.h, nameValue.h)
}

// FunctionScriptOrigin is the scope-local portion of Function::GetScriptOrigin.
// The presence bits distinguish V8's empty Local values for native functions
// from ordinary JavaScript values.
type FunctionScriptOrigin struct {
	ScriptID        int32
	ResourceName    Value
	SourceMapURL    Value
	HasResourceName bool
	HasSourceMapURL bool
}

func (f *Function) metadata() (line, column int32, origin FunctionScriptOrigin, err error) {
	if err = f.check(); err != nil {
		return
	}
	var resourceName, sourceMapURL uintptr
	r1, _, _ := proc("gov8_fa_function_metadata").Call(
		f.iso.handleAssumingCheck(), f.sc.handle, f.h,
		uintptr(unsafe.Pointer(&line)), uintptr(unsafe.Pointer(&column)),
		uintptr(unsafe.Pointer(&origin.ScriptID)),
		uintptr(unsafe.Pointer(&resourceName)), uintptr(unsafe.Pointer(&sourceMapURL)))
	if int64(r1) < 0 {
		err = shimError("Function.Metadata", r1)
		return
	}
	origin.ResourceName = Value{iso: f.iso, sc: f.sc, h: resourceName}
	origin.SourceMapURL = Value{iso: f.iso, sc: f.sc, h: sourceMapURL}
	origin.HasResourceName = resourceName != 0
	origin.HasSourceMapURL = sourceMapURL != 0
	return
}

// ScriptLineNumber returns the zero-based function definition line. ok is
// false for native functions and other functions without source metadata.
func (f *Function) ScriptLineNumber() (line int32, ok bool, err error) {
	line, _, _, err = f.metadata()
	return line, err == nil && line >= 0, err
}

// ScriptColumnNumber returns the zero-based function definition column. ok is
// false for native functions and other functions without source metadata.
func (f *Function) ScriptColumnNumber() (column int32, ok bool, err error) {
	_, column, _, err = f.metadata()
	return column, err == nil && column >= 0, err
}

// ScriptID returns Function::ScriptId (zero for a native function).
func (f *Function) ScriptID() (int32, error) {
	_, _, origin, err := f.metadata()
	return origin.ScriptID, err
}

// ScriptOrigin returns Function::GetScriptOrigin.
func (f *Function) ScriptOrigin() (FunctionScriptOrigin, error) {
	_, _, origin, err := f.metadata()
	return origin, err
}

// BoundTarget returns the original target for a bound function. For an
// unbound function it returns JavaScript undefined, matching GetBoundFunction.
func (f *Function) BoundTarget() (Value, error) {
	if err := f.check(); err != nil {
		return Value{}, err
	}
	var out uintptr
	r1, _, _ := proc("gov8_fa_function_bound_target").Call(
		f.iso.handleAssumingCheck(), f.sc.handle, f.h,
		uintptr(unsafe.Pointer(&out)))
	if int64(r1) < 0 {
		return Value{}, shimError("Function.BoundTarget", r1)
	}
	return Value{iso: f.iso, sc: f.sc, h: out}, nil
}

type functionCompileMetadata struct{}

// FunctionCodeCache is engine-produced cache data for a CompileFunction
// result. Its payload is intentionally opaque: accepting arbitrary bytes would
// expose V8 152's process-fatal deserializer boundary. A cache remains usable
// after its producer isolate closes and can therefore be consumed in another
// isolate.
type FunctionCodeCache struct {
	data []byte
}

// Len returns the cache payload size.
func (c *FunctionCodeCache) Len() int {
	if c == nil {
		return 0
	}
	return len(c.data)
}

// ErrFunctionNotCacheable is returned before V8 is called when a Function did
// not originate from CompileFunctionAdvanced. Upstream would fatal-abort for
// ordinary native/script functions and access-violate for bound functions.
var ErrFunctionNotCacheable = errors.New("gov8: function is not a cacheable CompileFunction result")

// CreateCodeCache creates cache data for a function compiled by
// CompileFunctionAdvanced. Provenance is checked in Go before the fatal V8 API
// is entered.
func (f *Function) CreateCodeCache() (*FunctionCodeCache, error) {
	if err := f.check(); err != nil {
		return nil, err
	}
	if f.compileMetadata == nil {
		return nil, ErrFunctionNotCacheable
	}
	cacheHandle, err := callHandle("Function.CreateCodeCache",
		proc("gov8_fa_function_code_cache_new"), f.iso.handleAssumingCheck(),
		f.sc.handle, f.h)
	if err != nil {
		return nil, err
	}
	capacity := 4096
	for attempt := 0; attempt < 2; attempt++ {
		buffer := make([]byte, capacity)
		var length int64
		r1, _, _ := proc("gov8_fa_function_code_cache_read_delete").Call(
			cacheHandle, uintptr(unsafe.Pointer(&buffer[0])), uintptr(capacity),
			uintptr(unsafe.Pointer(&length)))
		if int64(r1) == errNoMemory {
			if length <= 0 {
				return nil, shimError("Function.CreateCodeCache", r1)
			}
			capacity = int(length)
			// The too-small call deleted the engine object, so recreate it.
			cacheHandle, err = callHandle("Function.CreateCodeCache",
				proc("gov8_fa_function_code_cache_new"), f.iso.handleAssumingCheck(),
				f.sc.handle, f.h)
			if err != nil {
				return nil, err
			}
			continue
		}
		if int64(r1) < 0 {
			return nil, shimError("Function.CreateCodeCache", r1)
		}
		if length < 0 || length > int64(len(buffer)) {
			return nil, errors.New("gov8: invalid function code-cache length")
		}
		return &FunctionCodeCache{data: append([]byte(nil), buffer[:length]...)}, nil
	}
	return nil, errors.New("gov8: function code-cache read failed")
}

// CompileFunctionAdvanced compiles source as a function body with declared
// parameters. Passing nil cache performs a normal compile. A non-nil cache is
// consumed directly. V8 may reject changed source or truncated engine-produced
// data and cleanly recompile; changed parameter names/count may accept the
// cache and retain the cached function's original parameter metadata. rejected
// reports V8's cached-data rejected bit. Arbitrary external bytes cannot be
// constructed as FunctionCodeCache, preserving the safe deserializer boundary.
func (c *Context) CompileFunctionAdvanced(s *Scope, source string, params []string, cache *FunctionCodeCache, tc *TryCatch) (function *Function, rejected bool, err error) {
	return c.compileFunctionAdvanced(s, source, params, cache, tc, false)
}

func (c *Context) compileFunctionAdvanced(s *Scope, source string, params []string, cache *FunctionCodeCache, tc *TryCatch, platform bool) (function *Function, rejected bool, err error) {
	if err = c.check(); err != nil {
		return nil, false, err
	}
	if s.iso != c.iso {
		return nil, false, foreignIsolate("scope")
	}
	if tc != nil {
		if tc.iso != c.iso {
			return nil, false, foreignIsolate("trycatch")
		}
		if err = tc.check(); err != nil {
			return nil, false, err
		}
	}
	scopeHandle, err := s.checkedHandleAssumingIsolate()
	if err != nil {
		return nil, false, err
	}
	for _, parameter := range params {
		if strings.IndexByte(parameter, 0) >= 0 {
			return nil, false, errors.New("gov8: function parameter contains NUL")
		}
	}
	if cache != nil {
		if len(cache.data) == 0 {
			return nil, false, errors.New("gov8: empty function code cache")
		}
	}

	parameterPointers := make([]uintptr, len(params))
	parameterStorage := make([][]byte, len(params))
	for index, parameter := range params {
		bytes := make([]byte, len(parameter)+1)
		copy(bytes, parameter)
		parameterStorage[index] = bytes
		parameterPointers[index] = uintptr(unsafe.Pointer(&bytes[0]))
	}
	var parametersArg uintptr
	if len(parameterPointers) != 0 {
		parametersArg = uintptr(unsafe.Pointer(&parameterPointers[0]))
	}
	var tryCatchHandle uintptr
	if tc != nil {
		tryCatchHandle = tc.handle
	}
	sourceBytes := []byte(source)
	var cacheBytes []byte
	consume := int32(0)
	if cache != nil {
		cacheBytes = cache.data
		consume = 1
	}
	var out uintptr
	var rejectedInt int32
	operation := "gov8_fa_compile_function"
	if platform {
		operation = "gov8_fa_compile_platform_function"
	}
	r1, _, _ := proc(operation).Call(
		c.iso.handleAssumingCheck(), c.handle, scopeHandle, tryCatchHandle,
		bytesArg(sourceBytes), uintptr(len(sourceBytes)), parametersArg,
		uintptr(len(params)), bytesArg(cacheBytes), uintptr(len(cacheBytes)),
		uintptr(consume), uintptr(unsafe.Pointer(&out)),
		uintptr(unsafe.Pointer(&rejectedInt)))
	runtime.KeepAlive(sourceBytes)
	runtime.KeepAlive(parameterPointers)
	runtime.KeepAlive(parameterStorage)
	runtime.KeepAlive(cacheBytes)
	if int64(r1) < 0 {
		return nil, false, shimError("CompileFunctionAdvanced", r1)
	}
	metadata := &functionCompileMetadata{}
	return &Function{
		Value:           Value{iso: c.iso, sc: s, h: out},
		ctx:             c,
		compileMetadata: metadata,
	}, rejectedInt != 0, nil
}
