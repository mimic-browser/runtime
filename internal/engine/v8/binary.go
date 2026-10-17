//go:build (windows || linux) && amd64

package v8

import (
	"fmt"
	"runtime"
	"time"

	gov8 "github.com/maclof/gov8"
)

// exportBinaryBuffer preserves the viewed byte range across the JS-to-Go host
// boundary. JSON.stringify turns typed arrays into indexed objects and cannot
// serialize BigInt typed arrays at all.
func exportBinaryBuffer(value gov8.Value) ([]byte, bool) {
	if yes, _ := value.IsTypedArray(); yes {
		view, err := gov8.AsTypedArray(value)
		if err != nil {
			return nil, true
		}
		length, err := view.ByteLength()
		if err != nil {
			return nil, true
		}
		out := make([]byte, length)
		if copied, err := view.CopyContents(out); err != nil || copied != length {
			return nil, true
		}
		return out, true
	}
	if yes, _ := value.IsDataView(); yes {
		view, err := gov8.AsDataView(value)
		if err != nil {
			return nil, true
		}
		length, err := view.ByteLength()
		if err != nil {
			return nil, true
		}
		out := make([]byte, length)
		if copied, err := view.CopyContents(out); err != nil || copied != length {
			return nil, true
		}
		return out, true
	}
	if yes, _ := value.IsArrayBuffer(); yes {
		buffer, err := gov8.AsArrayBuffer(value)
		if err != nil {
			return nil, true
		}
		store, err := buffer.GetBackingStore()
		if err != nil {
			return nil, true
		}
		defer store.Close()
		length, err := buffer.ByteLength()
		if err != nil {
			return nil, true
		}
		out := make([]byte, length)
		if copied, err := store.ReadAt(out, 0); err != nil || copied != length {
			return nil, true
		}
		return out, true
	}
	if yes, _ := value.IsSharedArrayBuffer(); yes {
		buffer, err := gov8.AsSharedArrayBuffer(value)
		if err != nil {
			return nil, true
		}
		store, err := buffer.GetBackingStore()
		if err != nil {
			return nil, true
		}
		defer store.Close()
		length, err := buffer.ByteLength()
		if err != nil {
			return nil, true
		}
		out := make([]byte, length)
		if copied, err := store.ReadAt(out, 0); err != nil || copied != length {
			return nil, true
		}
		return out, true
	}
	return nil, false
}

func marshalBinaryBuffer(scope *gov8.Scope, realm *gov8.Context, data []byte) (gov8.Value, error) {
	buffer, err := gov8.NewArrayBuffer(scope, realm, len(data))
	if err != nil {
		return gov8.Value{}, err
	}
	if len(data) != 0 {
		store, err := buffer.GetBackingStore()
		if err != nil {
			return gov8.Value{}, err
		}
		// V8 owns the allocation. Drop our counted reference before returning;
		// JS reachability, not a Go handle or pinned Go slice, owns its lifetime.
		_, writeErr := store.WriteAt(data, 0)
		runtime.KeepAlive(data)
		closeErr := store.Close()
		if writeErr != nil {
			return gov8.Value{}, writeErr
		}
		if closeErr != nil {
			return gov8.Value{}, closeErr
		}
	}
	return buffer.Value, nil
}

// BinaryStringFunction converts a private typed-array result directly in its
// synchronous native callback. No generic Value export, author properties or
// JSON projection participates. The string owns a copy of the viewed bytes.
func (a *adapter) BinaryStringFunction() any { return binaryStringFunction{} }

type binaryStringFunction struct{}

func (a *adapter) makeBinaryStringFunction(scope *gov8.Scope, realm *gov8.Context) (gov8.Value, error) {
	function, err := a.activeIsolate.NewFunction(scope, realm, func(cs *gov8.CallbackScope, args gov8.FunctionCallbackArguments, result gov8.ReturnValue) {
		if a.profile != nil {
			defer a.recordCost("host:binaryStringFromBytes", time.Now())
		}
		fail := func(err error) {
			exception, _ := cs.NewError(err.Error())
			_ = cs.ThrowException(exception)
		}
		input, err := args.Get(0)
		if err != nil {
			fail(err)
			return
		}
		text, err := a.binaryStringFromBytes(cs.Scope(), input)
		if err != nil {
			fail(err)
			return
		}
		_ = result.Set(text)
	}, nil)
	if err != nil {
		return gov8.Value{}, err
	}
	return function.Value, nil
}

func (a *adapter) binaryStringFromBytes(scope *gov8.Scope, local gov8.Value) (gov8.Value, error) {
	view, err := gov8.AsTypedArray(local)
	if err != nil {
		return gov8.Value{}, err
	}
	length, err := view.ByteLength()
	if err != nil {
		return gov8.Value{}, err
	}
	// This conversion cannot invoke author code or reenter JavaScript. Reuse a
	// bounded private transfer buffer after V8 has copied its previous contents.
	var data []byte
	if length <= 64<<10 {
		if cap(a.binaryStringScratch) < length {
			a.binaryStringScratch = make([]byte, length)
		}
		data = a.binaryStringScratch[:length]
	} else {
		data = make([]byte, length)
	}
	if length != 0 {
		copied, err := view.CopyContents(data)
		if err != nil {
			return gov8.Value{}, err
		}
		if copied != length {
			return gov8.Value{}, fmt.Errorf("binary text projection copied %d of %d bytes", copied, length)
		}
	}
	text, err := scope.NewStringFromOneByte(data, gov8.StringNormal)
	if err != nil {
		return gov8.Value{}, err
	}
	return text, nil
}
