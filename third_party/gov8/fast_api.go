//go:build (windows || linux) && amd64

package gov8

import (
	"errors"
	"fmt"
	"math"
	"runtime"
	"sync"
	"unsafe"
)

// PublishFastClock anchors the isolate owner's native hot-path clock to the
// browser-owned clock. It must run on the isolate thread before script.
func PublishFastClock(unixMicros int64, scale float64) error {
	return callErr("FastClock.Publish", proc("gov8_fast_clock_publish"),
		uintptr(unixMicros), uintptr(math.Float64bits(scale)))
}

// NewNativeClockFunction creates a cold-path native V8 callback. Clock reads
// never enter Go, including before TurboFan has optimized the calling script.
func (i *Isolate) NewNativeClockFunction(s *Scope, c *Context, kind int, originMicros int64, seed uint64, precisionBase float64, isolated bool) (*Function, error) {
	if err := i.check(); err != nil {
		return nil, err
	}
	if s == nil || c == nil || s.iso != i || c.iso != i {
		return nil, foreignIsolate("native clock")
	}
	isolatedWord := uintptr(0)
	if isolated {
		isolatedWord = 1
	}
	handle, err := callHandle("NativeClock.New", proc("gov8_native_clock_function_new"),
		i.handleAssumingCheck(), s.handle, c.handle, uintptr(kind), uintptr(originMicros), uintptr(seed), uintptr(math.Float64bits(precisionBase)), isolatedWord)
	if err != nil {
		return nil, err
	}
	return &Function{Value: Value{iso: i, sc: s, h: handle}, ctx: c}, nil
}

// FastType mirrors v8::CTypeInfo::Type in pinned V8 15.2. The callback
// options marker is the upstream out-of-enum sentinel 255.
type FastType uint8

const (
	FastTypeVoid FastType = iota
	FastTypeBool
	FastTypeUint8
	FastTypeInt32
	FastTypeUint32
	FastTypeInt64
	FastTypeUint64
	FastTypeFloat32
	FastTypeFloat64
	FastTypePointer
	FastTypeV8Value
	FastTypeSeqOneByteString
	FastTypeAPIObject
	FastTypeAny

	FastTypeCallbackOptions FastType = 255
)

// FastTypeFlags mirrors v8::CTypeInfo::Flags.
type FastTypeFlags uint8

const (
	FastTypeAllowShared FastTypeFlags = 1 << iota
	FastTypeEnforceRange
	FastTypeClamp
	FastTypeIsRestricted
)

const fastTypeAllFlags = FastTypeAllowShared | FastTypeEnforceRange | FastTypeClamp | FastTypeIsRestricted

// FastTypeFlagsFromBits returns flags only when every bit is known.
func FastTypeFlagsFromBits(bits uint8) (FastTypeFlags, bool) {
	flags := FastTypeFlags(bits)
	return flags, flags&^fastTypeAllFlags == 0
}

// FastTypeFlagsFromBitsTruncated drops unknown bits.
func FastTypeFlagsFromBitsTruncated(bits uint8) FastTypeFlags {
	return FastTypeFlags(bits) & fastTypeAllFlags
}

// FastInt64Representation selects the JavaScript representation of 64-bit
// integer arguments and results.
type FastInt64Representation uint8

const (
	FastInt64AsNumber FastInt64Representation = iota
	FastInt64AsBigInt
)

// FastTypeInfo is immutable metadata corresponding to v8::CTypeInfo. Use
// NewFastTypeInfo so invalid enum and flag combinations are rejected before
// the engine's CHECK boundaries.
type FastTypeInfo struct {
	type_  FastType
	flags_ FastTypeFlags
}

// NewFastTypeInfo constructs validated fast-call type metadata.
func NewFastTypeInfo(fastType FastType, flags FastTypeFlags) (FastTypeInfo, error) {
	if !validFastType(fastType) {
		return FastTypeInfo{}, fmt.Errorf("gov8: invalid fast API type %d", fastType)
	}
	if flags&^fastTypeAllFlags != 0 {
		return FastTypeInfo{}, fmt.Errorf("gov8: invalid fast API type flags %#x", uint8(flags))
	}
	if fastType == FastTypeCallbackOptions && flags != 0 {
		return FastTypeInfo{}, errors.New("gov8: callback-options fast type cannot have flags")
	}
	if flags&(FastTypeEnforceRange|FastTypeClamp) != 0 && !fastTypeIntegral(fastType) {
		return FastTypeInfo{}, errors.New("gov8: range and clamp flags require an integral fast type")
	}
	if flags&FastTypeIsRestricted != 0 && fastType != FastTypeFloat32 && fastType != FastTypeFloat64 {
		return FastTypeInfo{}, errors.New("gov8: restricted flag requires a floating-point fast type")
	}
	return FastTypeInfo{type_: fastType, flags_: flags}, nil
}

// Info returns flag-free metadata for the type.
func (t FastType) Info() (FastTypeInfo, error) { return NewFastTypeInfo(t, 0) }

func validFastType(t FastType) bool { return t <= FastTypeAny || t == FastTypeCallbackOptions }

func fastTypeIntegral(t FastType) bool {
	return t == FastTypeUint8 || t == FastTypeInt32 || t == FastTypeUint32 ||
		t == FastTypeInt64 || t == FastTypeUint64
}

// Type reports the V8 fast-call type.
func (i FastTypeInfo) Type() FastType { return i.type_ }

// Flags reports the type flags.
func (i FastTypeInfo) Flags() FastTypeFlags { return i.flags_ }

// Identifier reports CTypeInfo::GetId (type in the high byte, flags in the
// low byte).
func (i FastTypeInfo) Identifier() uint32 {
	return uint32(uint8(i.type_))<<8 | uint32(uint8(i.flags_))
}

// CFunctionInfo is immutable CFunction signature metadata. Its private
// argument slice is copied at construction and copied again into native-owned
// storage when a fast FunctionTemplate is built.
type CFunctionInfo struct {
	returnInfo FastTypeInfo
	arguments  []FastTypeInfo
	repr       FastInt64Representation
}

// NewCFunctionInfo constructs a validated fast-call signature. CallbackOptions
// is allowed only as the final argument and is excluded from ArgumentCount,
// matching v8::CFunctionInfo.
func NewCFunctionInfo(returnInfo FastTypeInfo, arguments []FastTypeInfo, repr FastInt64Representation) (*CFunctionInfo, error) {
	if err := validateFastTypeInfo(returnInfo); err != nil {
		return nil, fmt.Errorf("gov8: invalid fast API return info: %w", err)
	}
	if returnInfo.type_ == FastTypeCallbackOptions {
		return nil, errors.New("gov8: callback options cannot be a fast API return type")
	}
	if repr > FastInt64AsBigInt {
		return nil, fmt.Errorf("gov8: invalid int64 representation %d", repr)
	}
	args := append([]FastTypeInfo(nil), arguments...)
	for index, arg := range args {
		if err := validateFastTypeInfo(arg); err != nil {
			return nil, fmt.Errorf("gov8: invalid fast API argument %d: %w", index, err)
		}
		if arg.type_ == FastTypeCallbackOptions && index != len(args)-1 {
			return nil, errors.New("gov8: callback options must be the final fast API argument")
		}
	}
	if uint64(len(args)) > uint64(^uint32(0)) {
		return nil, errors.New("gov8: too many fast API arguments")
	}
	return &CFunctionInfo{returnInfo: returnInfo, arguments: args, repr: repr}, nil
}

func validateFastTypeInfo(info FastTypeInfo) error {
	validated, err := NewFastTypeInfo(info.type_, info.flags_)
	if err != nil {
		return err
	}
	if validated != info {
		return errors.New("gov8: invalid fast API type info")
	}
	return nil
}

// ReturnInfo reports the signature return metadata.
func (i *CFunctionInfo) ReturnInfo() FastTypeInfo {
	if i == nil {
		return FastTypeInfo{}
	}
	return i.returnInfo
}

// ArgumentCount excludes a final CallbackOptions argument, as V8 does.
func (i *CFunctionInfo) ArgumentCount() int {
	if i == nil {
		return 0
	}
	count := len(i.arguments)
	if i.HasOptions() {
		count--
	}
	return count
}

// ArgumentInfo returns ordinary argument metadata. CallbackOptions is not
// addressable through this method because it is excluded from ArgumentCount.
func (i *CFunctionInfo) ArgumentInfo(index int) (FastTypeInfo, bool) {
	if i == nil || index < 0 || index >= i.ArgumentCount() {
		return FastTypeInfo{}, false
	}
	return i.arguments[index], true
}

// HasOptions reports whether the final native parameter is
// FastApiCallbackOptions.
func (i *CFunctionInfo) HasOptions() bool {
	return i != nil && len(i.arguments) != 0 && i.arguments[len(i.arguments)-1].type_ == FastTypeCallbackOptions
}

// Int64Representation reports the signature's 64-bit integer policy.
func (i *CFunctionInfo) Int64Representation() FastInt64Representation {
	if i == nil {
		return FastInt64AsNumber
	}
	return i.repr
}

// CFunction describes one caller-owned native fast-call address. The address
// must name process-lifetime executable native code whose ABI exactly matches
// TypeInfo. Go callbacks and Go pointers are intentionally unsupported: V8 may
// invoke this address without entering the Go callback ABI.
type CFunction struct {
	address  uintptr
	typeInfo *CFunctionInfo
}

// NewCFunction binds a nonzero native address to immutable type metadata.
func NewCFunction(address uintptr, typeInfo *CFunctionInfo) (CFunction, error) {
	if address == 0 {
		return CFunction{}, errors.New("gov8: zero fast API function address")
	}
	if typeInfo == nil {
		return CFunction{}, errors.New("gov8: nil fast API function type info")
	}
	if _, err := marshalCFunctionInfo(typeInfo); err != nil {
		return CFunction{}, err
	}
	return CFunction{address: address, typeInfo: typeInfo}, nil
}

// Address reports the native entry address.
func (f CFunction) Address() uintptr { return f.address }

// TypeInfo reports immutable signature metadata.
func (f CFunction) TypeInfo() *CFunctionInfo { return f.typeInfo }

type fastTypeWire struct {
	Type  uint8
	Flags uint8
}

type fastFunctionWire struct {
	Address     uintptr
	ArgOffset   uint32
	ArgCount    uint32
	ReturnType  uint8
	ReturnFlags uint8
	Repr        uint8
	Reserved    uint8
}

func marshalCFunctionInfo(info *CFunctionInfo) ([]fastTypeWire, error) {
	if info == nil {
		return nil, errors.New("gov8: nil fast API function type info")
	}
	if _, err := NewCFunctionInfo(info.returnInfo, info.arguments, info.repr); err != nil {
		return nil, err
	}
	args := make([]fastTypeWire, len(info.arguments))
	for index, arg := range info.arguments {
		args[index] = fastTypeWire{Type: uint8(arg.type_), Flags: uint8(arg.flags_)}
	}
	return args, nil
}

// BuildFast creates a FunctionTemplate with native fast-call overloads and
// this builder's ordinary Go callback as its slow fallback. Like rusty_v8's
// FunctionBuilder::build_fast, construction is always forbidden even if the
// builder's ConstructorBehavior option was set to Allow.
func (b *FunctionBuilder) BuildFast(s *Scope, overloads []CFunction) (*FunctionTemplate, error) {
	if b == nil || b.iso == nil {
		return nil, errors.New("gov8: nil function builder")
	}
	return b.iso.NewFastFunctionTemplate(s, b.callback, &b.options, overloads)
}

// NewFastFunctionTemplate is FunctionBuilder::build_fast for Go. V8 retains
// the CFunction array and all nested type metadata until isolate disposal, so
// the shim copies every descriptor into native-owned per-isolate storage.
// overloads may be reused or mutated by the caller after this method returns.
//
// The ordinary Go callback is the slow path used whenever V8 cannot take a
// fast overload. As in pinned rusty_v8, fast templates always use
// ConstructorBehavior::Throw; opts.ConstructorBehavior is validated but does
// not alter that build_fast rule.
func (i *Isolate) NewFastFunctionTemplate(s *Scope, callback FunctionCallback, opts *FunctionOptions, overloads []CFunction) (*FunctionTemplate, error) {
	if i == nil {
		return nil, errors.New("gov8: nil isolate")
	}
	if callback == nil {
		return nil, errors.New("gov8: nil slow callback")
	}
	if err := validFunctionOptions(opts); err != nil {
		return nil, err
	}
	if s == nil || s.iso != i {
		return nil, foreignIsolate("scope")
	}
	if err := s.check(); err != nil {
		return nil, err
	}
	ih, err := i.handleChecked()
	if err != nil {
		return nil, err
	}
	var data Value
	var signature *Signature
	length := 0
	sideEffect := SideEffectHasSideEffect
	if opts != nil {
		data = opts.Data
		signature = opts.Signature
		length = opts.Length
		sideEffect = opts.SideEffectType
	}
	if signature != nil {
		if err := signature.check(); err != nil {
			return nil, err
		}
		if signature.iso != i {
			return nil, foreignIsolate("signature")
		}
	}

	functions := make([]fastFunctionWire, len(overloads))
	var arguments []fastTypeWire
	arities := make(map[int]struct{}, len(overloads))
	for index, overload := range overloads {
		if overload.address == 0 {
			return nil, fmt.Errorf("gov8: fast API overload %d has a zero address", index)
		}
		args, err := marshalCFunctionInfo(overload.typeInfo)
		if err != nil {
			return nil, fmt.Errorf("gov8: fast API overload %d: %w", index, err)
		}
		arity := overload.typeInfo.ArgumentCount()
		if _, duplicate := arities[arity]; duplicate {
			return nil, fmt.Errorf("gov8: fast API overload %d duplicates public ArgumentCount %d", index, arity)
		}
		arities[arity] = struct{}{}
		if uint64(len(arguments))+uint64(len(args)) > uint64(^uint32(0)) {
			return nil, errors.New("gov8: too many aggregate fast API arguments")
		}
		functions[index] = fastFunctionWire{
			Address: overload.address, ArgOffset: uint32(len(arguments)), ArgCount: uint32(len(args)),
			ReturnType:  uint8(overload.typeInfo.returnInfo.type_),
			ReturnFlags: uint8(overload.typeInfo.returnInfo.flags_), Repr: uint8(overload.typeInfo.repr),
		}
		arguments = append(arguments, args...)
	}

	handle, err := registerFunctionCallback(i, callback, data)
	if err != nil {
		return nil, err
	}
	entry := lookupHostCallback(handle)
	if entry == nil {
		dropHostCallback(handle)
		return nil, errors.New("gov8: slow callback registration lost")
	}
	h, err := callHandle("FastFunctionTemplate.New", proc("gov8_fast_api_function_template_new"),
		ih, s.handle, entry.ctx, uintptr(int32(length)), sigHandle(signature), uintptr(sideEffect),
		slicePointer(functions), uintptr(len(functions)), slicePointer(arguments), uintptr(len(arguments)))
	runtime.KeepAlive(functions)
	runtime.KeepAlive(arguments)
	if err != nil {
		dropHostCallback(handle)
		return nil, err
	}
	fastAPIIsolates.Lock()
	fastAPIIsolates.entries[i] = struct{}{}
	fastAPIIsolates.Unlock()
	return &FunctionTemplate{iso: i, sc: s, h: h}, nil
}

var fastAPIIsolates = struct {
	sync.Mutex
	entries map[*Isolate]struct{}
}{entries: make(map[*Isolate]struct{})}

func fastAPIIsolateTracked(i *Isolate) bool {
	fastAPIIsolates.Lock()
	_, ok := fastAPIIsolates.entries[i]
	fastAPIIsolates.Unlock()
	return ok
}

// afterFastAPIIsolateDispose releases descriptors only after
// gov8_isolate_dispose has completed. V8 retains their raw addresses until
// that point. disposedHandle is used solely as an opaque map key by the shim;
// native code must never dereference it.
//
// Isolate.Close must call this after the dispose export, while retaining its
// pre-reset handle. Keeping this hook separate from ReleaseIsolateHostState is
// required: releasing callback contexts does not prove engine templates died.
func afterFastAPIIsolateDispose(i *Isolate, disposedHandle uintptr) error {
	if i == nil || disposedHandle == 0 {
		return errors.New("gov8: invalid disposed fast API isolate")
	}
	if !fastAPIIsolateTracked(i) {
		return nil
	}
	err := callErr("FastAPI.AfterIsolateDispose",
		proc("gov8_fast_api_after_isolate_dispose"), disposedHandle)
	fastAPIIsolates.Lock()
	delete(fastAPIIsolates.entries, i)
	fastAPIIsolates.Unlock()
	return err
}

func fastAPIDescriptorCount(i *Isolate) (uintptr, error) {
	if err := i.check(); err != nil {
		return 0, err
	}
	return fastAPIDescriptorCountHandle(i.handleAssumingCheck())
}

func fastAPIDescriptorCountHandle(handle uintptr) (uintptr, error) {
	var out uintptr
	if err := callErr("FastAPI.DescriptorCount", proc("gov8_fast_api_descriptor_count"),
		handle, uintptr(unsafe.Pointer(&out))); err != nil {
		return 0, err
	}
	return out, nil
}
