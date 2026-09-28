//go:build (windows || linux) && amd64

package v8

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"runtime"
	"testing"

	"github.com/moreveal/mimic/internal/engine"
)

func TestPackedArgumentsMatchGeneric(t *testing.T) {
	r := (Factory{}).New().(*adapter)
	defer r.Close()
	f := func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		values := make([]any, len(args))
		for i, arg := range args {
			values[i] = arg.Export()
			// JSON has no representation for special numeric primitives. Tag
			// them explicitly while comparing the two lossless host paths.
			if number, ok := values[i].(float64); ok && (math.IsNaN(number) || math.IsInf(number, 0) || number == 0 && math.Signbit(number)) {
				values[i] = map[string]string{"number": fmt.Sprint(number)}
			}
		}
		data, err := json.Marshal(values)
		return r.Value(string(data)), err
	}
	if err := r.Set("plain", r.TransientFunction(f)); err != nil {
		t.Fatal(err)
	}
	if err := r.Set("packed", r.PackedFunction(f, "nssn")); err != nil {
		t.Fatal(err)
	}
	_, err := r.Eval(context.Background(), `
for(const args of [[],[1,'class','active',1],[0,'','',-1],
 [42,'Привет 😀\u0000','\ud800\udfff',0],[NaN,true,null,Infinity],
 ['7',{x:1},['a'],undefined],[false,undefined,123,-Infinity],
 [1,'x'.repeat(3000),'large',0],[1,'x','y',0,1,2,3,4,5,6]]) {
 if(plain(...args)!==packed(...args))throw Error('packed export mismatch: '+JSON.stringify(args));
}
const old=String.prototype.charCodeAt;
try {String.prototype.charCodeAt=()=>{throw Error('user charCodeAt called')};
 if(plain(1,'a','b',1)!==packed(1,'a','b',1))throw Error('intrinsic mismatch');
} finally {String.prototype.charCodeAt=old}
`, "packed-arguments.js")
	if err != nil {
		t.Fatal(err)
	}
}

func TestPackedResultsReentrancyAndErrors(t *testing.T) {
	r := (Factory{}).New().(*adapter)
	defer r.Close()
	var nested engine.Value
	f := func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		n := args[0].Export().(float64)
		before := args[1].Export()
		if n == 1 || n == 2 {
			if _, err := r.Call(context.Background(), nested, nil); err != nil {
				return nil, err
			}
			if args[1].Export() != before {
				return nil, fmt.Errorf("nested call overwrote arguments")
			}
			if n == 1 {
				return nil, nil
			}
			return r.Value(before), nil
		}
		switch n {
		case 3:
			return r.Value(42), nil
		case 4:
			return r.Value(nil), nil
		case 5:
			return r.Value(false), nil
		case 6:
			return r.Value(true), nil
		case 7:
			return r.Value(map[string]any{"value": before}), nil
		case 8:
			return nil, fmt.Errorf("packed failure")
		default:
			return r.Value(n), nil
		}
	}
	if err := r.Set("packed", r.PackedFunction(f, "ns")); err != nil {
		t.Fatal(err)
	}
	nestedValue, err := r.Eval(context.Background(), `(()=>packed(3,'inner'))`, "nested.js")
	if err != nil {
		t.Fatal(err)
	}
	nested = nestedValue
	_, err = r.Eval(context.Background(), `
const first=packed(1,'outer'),second=packed(2,'outer');if(first!==undefined||second!=='outer')throw Error('nested result '+String(first)+' / '+String(second));
if(packed(3,'')!==42||packed(4,'')!==null||packed(5,'')!==false||packed(6,'')!==true)throw Error('primitive result');
if(packed(7,'record').value!=='record')throw Error('record result');
let caught=false;try{packed(8,'')}catch(e){caught=String(e).includes('packed failure')}
if(!caught||packed(3,'')!==42)throw Error('exception recovery');
if(!Object.is(packed(-0,''),-0))throw Error('negative zero');
`, "packed-results.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, frame := range r.packedFrames {
		if frame.callback.scope != nil || frame.callback.ctx != nil || frame.callback.id != 0 {
			t.Fatal("packed frame retained callback context")
		}
		for _, v := range frame.values {
			if v.runtime != nil || v.host != nil {
				t.Fatal("packed frame retained values")
			}
		}
	}
}

func TestPackedBufferIsolateLifetime(t *testing.T) {
	for i := 0; i < 12; i++ {
		r := (Factory{}).New().(*adapter)
		if err := r.Set("packed", r.PackedFunction(func(_ engine.Value, args []engine.Value) (engine.Value, error) { return args[0], nil }, "s")); err != nil {
			t.Fatal(err)
		}
		runtime.GC()
		if err := r.ProfileCollect(); err != nil {
			t.Fatal(err)
		}
		v, err := r.Eval(context.Background(), `packed('alive 😀')`, "packed-lifetime.js")
		if err != nil || v.String() != "alive 😀" {
			t.Fatalf("%v %v", v, err)
		}
		if err := r.Close(); err != nil {
			t.Fatal(err)
		}
		if r.packedMemory != nil || r.packedStore != nil {
			t.Fatal("closed adapter retained backing store")
		}
	}
	runtime.GC()
}
