//go:build (windows || linux) && amd64

package v8

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"runtime"
	"strings"
	"time"
	"unicode/utf16"
	"unsafe"

	gov8 "github.com/maclof/gov8"
	"github.com/moreveal/mimic/internal/engine"
)

// One bounded, private ArrayBuffer per adapter. JS writes it immediately before
// the synchronous callback; Go decodes it before executing the host body.
// Nested calls use separate Go frames. No JS objects or borrowed native pointers
// escape their lifetime, and no cross-Page lock is introduced.
const packedBytes = 2048
const packedHeader = 64

type packedFrame struct {
	units    [packedBytes / 2]uint16
	ascii    [packedBytes / 2]byte
	values   [4]runtimeValue
	numbers  [4]packedNumberValue
	args     [4]engine.Value
	callback callbackContext
}

// Keep packed numeric arguments unboxed until a caller explicitly exports
// one. Most DOM hosts consume node IDs and coordinates through Number().
type packedNumberValue struct {
	runtime *adapter
	number  float64
}

func (v *packedNumberValue) Export() any {
	if v.runtime == nil {
		return nil
	}
	return v.number
}
func (v *packedNumberValue) String() string {
	if v.runtime == nil {
		return "undefined"
	}
	return fmt.Sprint(v.number)
}
func (v *packedNumberValue) Number() float64 { return v.number }

func packedFactorySource(signature string) string {
	var parameters, checks, writes []string
	length := fmt.Sprint(packedHeader)
	for i, kind := range signature {
		arg := fmt.Sprintf("a%d", i)
		parameters = append(parameters, arg)
		if kind == 'n' {
			checks = append(checks, "typeof "+arg+"!=='number'", "!finite("+arg+")")
			writes = append(writes, fmt.Sprintf("numbers[%d]=%s;", i, arg))
		} else {
			checks = append(checks, "typeof "+arg+"!=='string'")
			length += "+2*" + arg + ".length"
			writes = append(writes, fmt.Sprintf("numbers[%d]=%s.length;for(let j=0;j<%s.length;j++)units[offset++]=code(%s,j);", i, arg, arg, arg))
		}
	}
	return fmt.Sprintf(`(function(buffer,fast,slow){
const numbers=new Float64Array(buffer),units=new Uint16Array(buffer);
const code=Function.prototype.call.bind(String.prototype.charCodeAt),apply=Reflect.apply,finite=Number.isFinite;
return function(%s){
if(arguments.length!==%d||%s)return apply(slow,this,arguments);
if(%s>%d)return apply(slow,this,arguments);
let offset=%d;%s
const result=fast();
switch(numbers[4]){case 1:return null;case 2:return numbers[5]!==0;case 3:return numbers[5];case 4:return undefined;default:return result}
};})`, strings.Join(parameters, ","), len(signature), strings.Join(checks, "||"), length, packedBytes, packedHeader/2, strings.Join(writes, ""))
}

func (a *adapter) makePackedFunction(scope *gov8.Scope, realm *gov8.Context, host hostFunction) (gov8.Value, error) {
	if len(host.packed) > 4 || strings.Trim(host.packed, "ns") != "" {
		return gov8.Value{}, errors.New("invalid packed signature")
	}
	if a.packedStore == nil {
		memory := new([packedBytes]byte)
		pin := new(runtime.Pinner)
		pin.Pin(memory)
		// The SDK explicitly supports caller-owned backing stores. Pin this
		// pointer-free Go allocation until V8 drops its final store reference,
		// including when collection happens after adapter.Close releases ours.
		store, err := a.activeIsolate.NewBackingStoreFromPtr(unsafe.Pointer(memory), packedBytes, func(unsafe.Pointer, int, uintptr) { pin.Unpin() }, 0)
		if err != nil {
			pin.Unpin()
			return gov8.Value{}, err
		}
		buffer, err := gov8.NewArrayBufferWithBackingStore(scope, realm, store)
		if err != nil {
			store.Close()
			return gov8.Value{}, err
		}
		global, err := a.newGlobal(scope, buffer.Value)
		if err != nil {
			store.Close()
			return gov8.Value{}, err
		}
		a.packedStore, a.packedBuffer = store, global
		a.packedMemory = memory
		a.packedFactories = map[string]*gov8.Global{}
	}
	factory := a.packedFactories[host.packed]
	if factory == nil {
		script, err := realm.Compile(scope, packedFactorySource(host.packed), nil)
		if err != nil {
			return gov8.Value{}, err
		}
		value, err := script.Run(scope, nil)
		script.Close()
		if err != nil {
			return gov8.Value{}, err
		}
		factory, err = a.newGlobal(scope, value)
		if err != nil {
			return gov8.Value{}, err
		}
		a.packedFactories[host.packed] = factory
	}
	slow, err := a.makeFunction(scope, realm, host.function, host.name, true)
	if err != nil {
		return gov8.Value{}, err
	}
	fast, err := a.activeIsolate.NewFunction(scope, realm, func(cs *gov8.CallbackScope, _ gov8.FunctionCallbackArguments, rv gov8.ReturnValue) {
		if a.profile != nil {
			defer a.recordCost("host:"+host.name, time.Now())
		}
		a.callbackSeq++
		if a.processorSamples != nil && a.callbackSeq%1024 == 0 {
			processor, _, _ := diagnosticProcessor.Call()
			a.processorSamples[processor]++
		}
		var frame *packedFrame
		if n := len(a.packedFrames); n > 0 {
			frame = a.packedFrames[n-1]
			a.packedFrames[n-1] = nil
			a.packedFrames = a.packedFrames[:n-1]
		} else {
			frame = new(packedFrame)
		}
		previous := a.callback
		frame.callback = callbackContext{scope: cs, ctx: realm, result: rv, id: a.callbackSeq}
		a.callback = &frame.callback
		defer func() { a.callback = previous }()
		defer func() {
			frame.callback = callbackContext{}
			for i := range frame.values {
				frame.values[i] = runtimeValue{}
				frame.numbers[i] = packedNumberValue{}
				frame.args[i] = nil
			}
			if len(a.packedFrames) < 8 {
				a.packedFrames = append(a.packedFrames, frame)
			}
		}()
		fail := func(err error) { exception, _ := cs.NewError(err.Error()); _ = cs.ThrowException(exception) }
		data := a.packedMemory[:]
		offset := packedHeader
		for i, kind := range host.packed {
			number := math.Float64frombits(binary.LittleEndian.Uint64(data[i*8:]))
			if kind == 'n' {
				frame.numbers[i] = packedNumberValue{runtime: a, number: number}
				frame.args[i] = &frame.numbers[i]
				continue
			}
			var value string
			if number < 0 || number > float64((packedBytes-offset)/2) || number != math.Trunc(number) {
				fail(errors.New("invalid packed string length"))
				return
			}
			length := int(number)
			ascii := true
			for j := 0; j < length; j++ {
				frame.units[j] = binary.LittleEndian.Uint16(data[offset+j*2:])
				frame.ascii[j] = byte(frame.units[j])
				ascii = ascii && frame.units[j] < 128
			}
			if ascii {
				value = string(frame.ascii[:length])
			} else {
				value = string(utf16.Decode(frame.units[:length]))
			}
			offset += length * 2
			frame.values[i] = runtimeValue{runtime: a, host: value, hostSet: true}
			frame.args[i] = &frame.values[i]
		}
		result, err := host.function(nil, frame.args[:len(host.packed)])
		if err != nil {
			fail(err)
			return
		}
		// An inner call may have overwritten the shared return slots. Publish
		// only after the outer body completes, including the undefined case.
		binary.LittleEndian.PutUint64(data[32:], 0)
		publish := func(kind, value float64) {
			binary.LittleEndian.PutUint64(data[32:], math.Float64bits(kind))
			binary.LittleEndian.PutUint64(data[40:], math.Float64bits(value))
		}
		if result == nil {
			publish(4, 0)
			return
		}
		if host, ok := hostPayload(result); ok {
			switch v := host.(type) {
			case nil:
				publish(1, 0)
				return
			case bool:
				if v {
					publish(2, 1)
				} else {
					publish(2, 0)
				}
				return
			case int64:
				publish(3, float64(v))
				return
			case int:
				publish(3, float64(v))
				return
			case float64:
				publish(3, v)
				return
			}
		}
		value, err := a.localCallback(result)
		if err != nil {
			fail(err)
			return
		}
		_ = rv.Set(value)
	}, nil)
	if err != nil {
		return gov8.Value{}, err
	}
	local, err := factory.ToLocal(scope)
	if err != nil {
		return gov8.Value{}, err
	}
	fn, ok, err := gov8.AsFunction(local, realm)
	if err != nil || !ok {
		return gov8.Value{}, fmt.Errorf("packed factory: %v", err)
	}
	buffer, err := a.packedBuffer.ToLocal(scope)
	if err != nil {
		return gov8.Value{}, err
	}
	undefined, err := scope.Undefined()
	if err != nil {
		return gov8.Value{}, err
	}
	value, ok, err := fn.Call(scope, undefined, buffer, fast.Value, slow)
	if err != nil || !ok {
		return gov8.Value{}, fmt.Errorf("packed wrapper: %v", err)
	}
	return value, nil
}
