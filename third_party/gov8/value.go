//go:build (windows || linux) && amd64

package gov8

import (
	"fmt"
	syscall "github.com/maclof/gov8/internal/native"
	"math"
	"sync"
	"unsafe"
)

// MicrotasksPolicy mirrors v8::MicrotasksPolicy. Auto drains microtasks when
// the engine considers the call stack empty; Explicit drains only on an
// explicit PerformMicrotaskCheckpoint.
type MicrotasksPolicy uint8

const (
	PolicyAuto MicrotasksPolicy = iota
	PolicyExplicit
)

// Value is a scope-local JS value: a raw v8 local-handle slot address bound
// to the Scope that created it. Methods on a Value whose Scope is closed, or
// that are invoked from a foreign thread, return errors without touching the
// engine. Values are plain structs — copying one is fine; lifetime, not
// identity, is what matters.
type Value struct {
	iso *Isolate
	sc  *Scope
	h   uintptr
}

type primitiveConstructor uint8

const (
	primitiveUndefined primitiveConstructor = iota
	primitiveNull
	primitiveEmptyString
	primitiveBoolean
	primitiveInt32
	primitiveUint32
	primitiveNumber
	primitiveBigIntInt64
	primitiveBigIntUint64
)

var (
	primitiveConstructorsOnce sync.Once
	primitiveConstructorAddrs [primitiveBigIntUint64 + 1]uintptr
	numberValueDirectOnce     sync.Once
	numberValueDirectAddr     uintptr
)

func resolvePrimitiveConstructors() {
	names := [...]string{
		"gov8_undefined",
		"gov8_null",
		"gov8_sb_string_empty",
		"gov8_boolean",
		"gov8_integer_new",
		"gov8_integer_new_unsigned",
		"gov8_number_new",
		"gov8_bigint_new_i64",
		"gov8_bigint_new_u64",
	}
	for index, name := range names {
		primitiveConstructorAddrs[index] = proc(name).Addr()
	}
}

func resolveNumberValueDirect() {
	numberValueDirectAddr = proc("gov8_value_number_value_direct").Addr()
}

// constructPrimitive keeps the public validation order while avoiding the
// escaping closure and variadic argument frame formerly created by every
// primitive constructor. All arguments are scalar native handles or values;
// no Go pointer crosses the call.
func (s *Scope) constructPrimitive(op string, kind primitiveConstructor, value uintptr) (Value, error) {
	if err := s.check(); err != nil {
		return Value{}, err
	}
	if err := requireInitialized(); err != nil {
		return Value{}, err
	}
	primitiveConstructorsOnce.Do(resolvePrimitiveConstructors)
	address := primitiveConstructorAddrs[kind]
	var raw uintptr
	if kind <= primitiveEmptyString {
		raw, _, _ = syscall.Syscall(address, 2,
			s.iso.handleAssumingCheck(), s.handle, 0)
	} else {
		raw, _, _ = syscall.Syscall(address, 3,
			s.iso.handleAssumingCheck(), value, s.handle)
	}
	if raw == 0 {
		return Value{}, shimError(op, 0)
	}
	return Value{iso: s.iso, sc: s, h: raw}, nil
}

func (v Value) check() error {
	if v.h == 0 {
		return fmt.Errorf("gov8: zero value handle")
	}
	return v.sc.check()
}

// Undefined returns the JS undefined value in the scope.
func (s *Scope) Undefined() (Value, error) {
	return s.constructPrimitive("Undefined", primitiveUndefined, 0)
}

// Null returns the JS null value in the scope.
func (s *Scope) Null() (Value, error) {
	return s.constructPrimitive("Null", primitiveNull, 0)
}

// Boolean returns a JS boolean in the scope.
func (s *Scope) Boolean(b bool) (Value, error) {
	value := uintptr(0)
	if b {
		value = 1
	}
	return s.constructPrimitive("Boolean", primitiveBoolean, value)
}

// Int32 returns a JS integer (int32 range) in the scope.
func (s *Scope) Int32(v int32) (Value, error) {
	return s.constructPrimitive("Int32", primitiveInt32, uintptr(v))
}

// Uint32 returns a JS unsigned integer (uint32 range) in the scope.
func (s *Scope) Uint32(v uint32) (Value, error) {
	return s.constructPrimitive("Uint32", primitiveUint32, uintptr(v))
}

// Number returns a JS number (float64) in the scope.
func (s *Scope) Number(f float64) (Value, error) {
	return s.constructPrimitive("Number", primitiveNumber, uintptr(math.Float64bits(f)))
}

// BigIntFromInt64 returns a BigInt constructed from an int64.
func (s *Scope) BigIntFromInt64(v int64) (Value, error) {
	return s.constructPrimitive("BigIntFromInt64", primitiveBigIntInt64, uintptr(v))
}

// BigIntFromUint64 returns a BigInt constructed from a uint64.
func (s *Scope) BigIntFromUint64(v uint64) (Value, error) {
	return s.constructPrimitive("BigIntFromUint64", primitiveBigIntUint64, uintptr(v))
}

// NewString creates a JS string from a UTF-8 Go string. Invalid UTF-8 is
// replaced with U+FFFD by the engine (matching the oracle's lossy
// conversion path).
func (s *Scope) NewString(str string) (Value, error) {
	if err := s.check(); err != nil {
		return Value{}, err
	}
	if err := requireInitialized(); err != nil {
		return Value{}, err
	}
	return s.newStringAssumingCheck(str)
}

// newStringAssumingCheck is the body of NewString for callers that already
// validated the scope's isolate state and thread affinity in the same
// operation (NewString's public path, and the key-construction step of
// GetByName/SetByName, which runs behind the object/context checks).
func (s *Scope) newStringAssumingCheck(str string) (Value, error) {
	var b []byte
	if len(str) > 0 {
		b = []byte(str)
	}
	var out uintptr
	var p uintptr
	if len(b) > 0 {
		p = uintptr(unsafe.Pointer(&b[0]))
	}
	r1, _, _ := proc("gov8_string_new_utf8").Call(s.iso.handleAssumingCheck(), s.handle, p, uintptr(len(b)), uintptr(unsafe.Pointer(&out)))
	if int64(r1) < 0 {
		return Value{}, shimError("NewString", r1)
	}
	return Value{iso: s.iso, sc: s, h: out}, nil
}

// --- predicates ---------------------------------------------------------------

func (v Value) predicate(op string) (bool, error) {
	if err := v.check(); err != nil {
		return false, err
	}
	if err := requireInitialized(); err != nil {
		return false, err
	}
	r1, _, _ := syscall.Syscall(proc(op).Addr(), 2, v.iso.handleAssumingCheck(), v.h, 0)
	if int64(r1) < 0 {
		return false, shimError(op, r1)
	}
	return r1 == 1, nil
}

// IsUndefined reports whether the value is undefined.
func (v Value) IsUndefined() (bool, error) { return v.predicate("gov8_is_undefined") }

// IsNull reports whether the value is null.
func (v Value) IsNull() (bool, error) { return v.predicate("gov8_is_null") }

// IsNullOrUndefined reports whether the value is null or undefined.
func (v Value) IsNullOrUndefined() (bool, error) {
	return v.predicate("gov8_is_null_or_undefined")
}

// IsBoolean reports whether the value is a boolean.
func (v Value) IsBoolean() (bool, error) { return v.predicate("gov8_is_boolean") }

// IsString reports whether the value is a string.
func (v Value) IsString() (bool, error) { return v.predicate("gov8_is_string") }

// IsInt32 reports whether the value is a 32-bit signed integer.
func (v Value) IsInt32() (bool, error) { return v.predicate("gov8_is_int32") }

// IsUint32 reports whether the value is a 32-bit unsigned integer.
func (v Value) IsUint32() (bool, error) { return v.predicate("gov8_is_uint32") }

// IsNumber reports whether the value is a number.
func (v Value) IsNumber() (bool, error) { return v.predicate("gov8_is_number") }

// IsObject reports whether the value is an object.
func (v Value) IsObject() (bool, error) { return v.predicate("gov8_is_object") }

// IsArray reports whether the value is an array.
func (v Value) IsArray() (bool, error) { return v.predicate("gov8_is_array") }

// IsFunction reports whether the value is a function.
func (v Value) IsFunction() (bool, error) { return v.predicate("gov8_is_function") }

// --- direct readers -------------------------------------------------------------

// BooleanValue returns the ECMAScript ToBoolean of the value.
func (v Value) BooleanValue() (bool, error) {
	if err := v.check(); err != nil {
		return false, err
	}
	if err := requireInitialized(); err != nil {
		return false, err
	}
	r1, _, _ := proc("gov8_boolean_value").Call(v.iso.handleAssumingCheck(), v.h)
	if int64(r1) < 0 {
		return false, shimError("BooleanValue", r1)
	}
	return r1 == 1, nil
}

// IntegerValueRaw returns v8::Integer::Value (the int64 payload of an
// Integer-typed value) without a context conversion.
func (v Value) IntegerValueRaw() (int64, error) {
	if err := v.check(); err != nil {
		return 0, err
	}
	if err := requireInitialized(); err != nil {
		return 0, err
	}
	var out int64
	r1, _, _ := proc("gov8_integer_raw_value").Call(v.iso.handleAssumingCheck(), v.h, uintptr(unsafe.Pointer(&out)))
	if int64(r1) < 0 {
		return 0, shimError("IntegerValueRaw", r1)
	}
	return out, nil
}

// NumberValueRaw returns v8::Number::Value (the float64 payload of a
// Number-typed value) without a context conversion.
func (v Value) NumberValueRaw() (float64, error) {
	if err := v.check(); err != nil {
		return 0, err
	}
	if err := requireInitialized(); err != nil {
		return 0, err
	}
	var out float64
	r1, _, _ := proc("gov8_number_raw_value").Call(v.iso.handleAssumingCheck(), v.h, uintptr(unsafe.Pointer(&out)))
	if int64(r1) < 0 {
		return 0, shimError("NumberValueRaw", r1)
	}
	return out, nil
}

// --- context conversions ----------------------------------------------------------

func (v Value) ctxHandle(c *Context) error {
	if err := v.check(); err != nil {
		return err
	}
	if c == nil || c.iso != v.iso {
		return foreignIsolate("context")
	}
	return c.check()
}

// toStringBufCap is the initial buffer size for engine-produced UTF-8 text
// (ToString results, exception messages). It grows on demand via the
// shim's reported required size.
const toStringBufCap = 4096

// callTextFn runs a shim text function with a caller buffer, retrying with
// the exact required size if the first buffer is too small.
func callTextFn(op string, fn func(buf *byte, cap int, outLen *int64) uintptr) (string, error) {
	buf := make([]byte, toStringBufCap)
	var n int64
	r := fn(&buf[0], len(buf), &n)
	if int64(r) == errNoMemory {
		if n <= 0 {
			return "", shimError(op, r)
		}
		buf = make([]byte, n)
		r = fn(&buf[0], len(buf), &n)
	}
	if int64(r) < 0 {
		return "", shimError(op, r)
	}
	if n < 0 || int(n) > len(buf) {
		n = int64(len(buf))
	}
	return string(buf[:n]), nil
}

// ToString returns the ECMAScript ToString of the value as a Go string
// (lossy UTF-8, matching the oracle's to_rust_string_lossy).
func (v Value) ToString(c *Context) (string, error) {
	if err := v.ctxHandle(c); err != nil {
		return "", err
	}
	// ctxHandle proved the isolate's state and affinity (v.check transit
	// through the scope) and the context's closed flag, so the scope check
	// here skips the isolate validation the old code re-ran a third time.
	sh, err := v.sc.checkedHandleAssumingIsolate()
	if err != nil {
		return "", err
	}
	return callTextFn("ToString", func(buf *byte, cap int, outLen *int64) uintptr {
		r, _, _ := proc("gov8_value_to_string_utf8").Call(
			v.iso.handleAssumingCheck(), c.handle, sh, v.h,
			uintptr(unsafe.Pointer(buf)), uintptr(cap), uintptr(unsafe.Pointer(outLen)))
		return r
	})
}

// NumberValue returns v8 Value::NumberValue (a context conversion). ok is
// false when the conversion failed without throwing.
func (v Value) NumberValue(c *Context) (val float64, ok bool, err error) {
	if err := v.ctxHandle(c); err != nil {
		return 0, false, err
	}
	numberValueDirectOnce.Do(resolveNumberValueDirect)
	result, _, _ := syscall.Syscall(numberValueDirectAddr, 3,
		v.iso.handle, c.handle, v.h)
	if int64(result) < 0 {
		return 0, false, shimError("NumberValue", result)
	}
	if result == 0 {
		return 0, false, nil
	}
	// The native address refers to per-thread shim storage and is consumed
	// immediately on the isolate's locked owner thread. Bit-copy through a
	// pointer word so vet does not treat uintptr arithmetic as a Go pointer.
	pointer := *(*unsafe.Pointer)(unsafe.Pointer(&result))
	return *(*float64)(pointer), true, nil
}

// IntegerValue returns v8 Value::IntegerValue (a context conversion to i64).
func (v Value) IntegerValue(c *Context) (val int64, ok bool, err error) {
	if err := v.ctxHandle(c); err != nil {
		return 0, false, err
	}
	var out int64
	var okv int32
	r1, _, _ := proc("gov8_value_integer_value").Call(
		v.iso.handle, c.handle, v.h, uintptr(unsafe.Pointer(&out)), uintptr(unsafe.Pointer(&okv)))
	if int64(r1) < 0 {
		return 0, false, shimError("IntegerValue", r1)
	}
	return out, okv == 1, nil
}

// Int32Value returns v8 Value::Int32Value (a context conversion to i32).
func (v Value) Int32Value(c *Context) (val int32, ok bool, err error) {
	if err := v.ctxHandle(c); err != nil {
		return 0, false, err
	}
	var out int32
	var okv int32
	r1, _, _ := proc("gov8_value_int32_value").Call(
		v.iso.handle, c.handle, v.h, uintptr(unsafe.Pointer(&out)), uintptr(unsafe.Pointer(&okv)))
	if int64(r1) < 0 {
		return 0, false, shimError("Int32Value", r1)
	}
	return out, okv == 1, nil
}

var (
	valueUint32ProcOnce sync.Once
	valueUint32ProcAddr uintptr
)

func resolveValueUint32Proc() {
	valueUint32ProcAddr = proc("gov8_value_uint32_value_direct").Addr()
}

// Uint32Value returns v8 Value::Uint32Value (a context conversion to u32).
func (v Value) Uint32Value(c *Context) (val uint32, ok bool, err error) {
	if err := v.ctxHandle(c); err != nil {
		return 0, false, err
	}
	valueUint32ProcOnce.Do(resolveValueUint32Proc)
	r1, _, _ := syscall.Syscall(valueUint32ProcAddr, 3,
		v.iso.handle, c.handle, v.h)
	if int64(r1) < 0 {
		return 0, false, shimError("Uint32Value", r1)
	}
	if uint64(r1) == uint64(1)<<32 {
		return 0, false, nil
	}
	return uint32(r1), true, nil
}

// BigIntInt64 returns (value, lossless) for a BigInt via v8 BigInt::Int64Value.
func (v Value) BigIntInt64() (val int64, lossless bool, err error) {
	if err := v.requireBigInt(); err != nil {
		return 0, false, err
	}
	if err := requireInitialized(); err != nil {
		return 0, false, err
	}
	var out int64
	var lossy int32
	r1, _, _ := proc("gov8_bigint_i64_value").Call(
		v.iso.handleAssumingCheck(), v.h, uintptr(unsafe.Pointer(&out)), uintptr(unsafe.Pointer(&lossy)))
	if int64(r1) < 0 {
		return 0, false, shimError("BigIntInt64", r1)
	}
	return out, lossy == 1, nil
}

// --- strings ----------------------------------------------------------------------

// utf8Bytes reads the full UTF-8 (lossy) encoding of a string value.
func (v Value) utf8Bytes() ([]byte, error) {
	if err := v.requireString(); err != nil {
		return nil, err
	}
	if err := requireInitialized(); err != nil {
		return nil, err
	}
	ih := v.iso.handleAssumingCheck()
	r1, _, _ := proc("gov8_string_utf8_length").Call(ih, v.h)
	if int64(r1) < 0 {
		return nil, shimError("Utf8Length", r1)
	}
	n := int(r1)
	if n == 0 {
		return []byte{}, nil
	}
	buf := make([]byte, n)
	r2, _, _ := proc("gov8_string_write_utf8").Call(
		ih, v.h, uintptr(unsafe.Pointer(&buf[0])), uintptr(n))
	if int64(r2) < 0 {
		return nil, shimError("WriteUtf8", r2)
	}
	written := int(int64(r2))
	if written > n {
		written = n
	}
	return buf[:written], nil
}

// StringValue returns the value as a Go string, assuming it is a JS string.
func (v Value) StringValue() (string, error) {
	b, err := v.utf8Bytes()
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Utf8Length returns the number of bytes in the value's UTF-8 encoding
// (string values only).
func (v Value) Utf8Length() (int, error) {
	if err := v.requireString(); err != nil {
		return 0, err
	}
	if err := requireInitialized(); err != nil {
		return 0, err
	}
	r1, _, _ := proc("gov8_string_utf8_length").Call(v.iso.handleAssumingCheck(), v.h)
	if int64(r1) < 0 {
		return 0, shimError("Utf8Length", r1)
	}
	return int(r1), nil
}

// Length returns the number of UTF-16 code units in a string value.
func (v Value) Length() (int, error) {
	if err := v.requireString(); err != nil {
		return 0, err
	}
	if err := requireInitialized(); err != nil {
		return 0, err
	}
	r1, _, _ := proc("gov8_string_length").Call(v.iso.handleAssumingCheck(), v.h)
	if int64(r1) < 0 {
		return 0, shimError("Length", r1)
	}
	return int(r1), nil
}

// --- object property access ---------------------------------------------------------

// GetByName reads a named property from the object. ok is false when the
// getter threw. The scope and context must belong to the same isolate as the
// object.
func (o *Object) GetByName(s *Scope, c *Context, name string) (val Value, ok bool, err error) {
	if err := o.ctxHandle(c); err != nil {
		return Value{}, false, err
	}
	if s.iso != o.iso {
		return Value{}, false, foreignIsolate("scope")
	}
	// o.ctxHandle proved the isolate's state and affinity for this
	// operation, so the scope's handle check below only inspects its own
	// closed flag (the old code re-ran the isolate validation a second and
	// third time inside the scope check and the key construction).
	if _, err := s.checkedHandleAssumingIsolate(); err != nil {
		return Value{}, false, err
	}
	key, err := s.newStringAssumingCheck(name)
	if err != nil {
		return Value{}, false, err
	}
	var out uintptr
	var okv int32
	r1, _, _ := proc("gov8_object_get").Call(
		o.iso.handleAssumingCheck(), c.handle, s.handle, o.h, key.h,
		uintptr(unsafe.Pointer(&out)), uintptr(unsafe.Pointer(&okv)))
	if int64(r1) < 0 {
		return Value{}, false, shimError("Get", r1)
	}
	return Value{iso: o.iso, sc: s, h: out}, okv == 1, nil
}

// SetByName writes a named property. ok is false when the setter threw;
// the bool return of v8 Object::Set is folded into ok as well (the oracle
// treats both empty Maybe and false identically for this check). The scope,
// context, and value must all belong to the same isolate as the object.
func (o *Object) SetByName(s *Scope, c *Context, name string, v Value) (ok bool, err error) {
	if err := o.ctxHandle(c); err != nil {
		return false, err
	}
	if s.iso != o.iso {
		return false, foreignIsolate("scope")
	}
	if _, err := s.checkedHandleAssumingIsolate(); err != nil {
		return false, err
	}
	if err := v.check(); err != nil {
		return false, err
	}
	if v.iso != o.iso {
		return false, foreignIsolate("value")
	}
	key, err := s.newStringAssumingCheck(name)
	if err != nil {
		return false, err
	}
	var okv int32
	r1, _, _ := proc("gov8_object_set").Call(
		o.iso.handleAssumingCheck(), c.handle, s.handle, o.h, key.h, v.h, uintptr(unsafe.Pointer(&okv)))
	if int64(r1) < 0 {
		return false, shimError("Set", r1)
	}
	return okv == 1, nil
}
