//go:build (windows || linux) && amd64

package v8

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"

	"github.com/maclof/gov8"
	"github.com/moreveal/mimic/internal/engine"
)

type cloneErrorReporter struct {
	message   string
	scope     *gov8.Scope
	reject    *gov8.Function
	realm     *gov8.Context
	platforms []string
}

func (d *cloneErrorReporter) HasCustomHostObject() bool { return d.reject != nil }
func (d *cloneErrorReporter) GetSharedArrayBufferID(*gov8.SharedArrayBuffer) (uint32, bool) {
	d.message = "SharedArrayBuffer serialization is not supported."
	return 0, false
}
func (d *cloneErrorReporter) GetWasmModuleTransferID(gov8.Value) (uint32, bool) {
	d.message = "WebAssembly.Module serialization is not supported."
	return 0, false
}
func (d *cloneErrorReporter) IsHostObject(object *gov8.Object) (bool, bool) {
	_, host, ok := d.classifyHostObject(object)
	return host, ok
}

func (d *cloneErrorReporter) classifyHostObject(object *gov8.Object) (string, bool, bool) {
	result, ok, err := d.reject.Call(d.scope, object.Value, object.Value)
	if err != nil || !ok {
		if err != nil {
			d.message = err.Error()
		}
		return "", false, false
	}
	if text, e := result.IsString(); e != nil {
		d.message = e.Error()
		return "", false, false
	} else if text {
		payload, err := result.ToString(d.realm)
		if err != nil {
			d.message = err.Error()
		}
		return payload, err == nil, err == nil
	}
	reject, err := result.BooleanValue()
	if err != nil {
		return "", false, false
	}
	if reject {
		d.message = "The platform object could not be cloned."
		return "", false, false
	}
	return "", false, true
}

func (d *cloneErrorReporter) WriteHostObject(object *gov8.Object, w *gov8.DelegateValueSerializer) (bool, bool) {
	// V8 recognizes objects with internal fields as host objects directly and
	// can bypass IsHostObject. Classify the canonical object at this boundary
	// instead of assuming that a previous hook populated its platform payload.
	if d.reject == nil {
		d.message = "The platform object could not be cloned."
		return false, false
	}
	payload, host, ok := d.classifyHostObject(object)
	if !ok || !host {
		if d.message == "" {
			d.message = "The platform object could not be cloned."
		}
		return false, false
	}
	index := len(d.platforms)
	d.platforms = append(d.platforms, payload)
	if err := w.WriteUint32(uint32(index)); err != nil {
		d.message = err.Error()
		return false, false
	}
	return true, true
}

type platformCloneReader struct {
	objects []*gov8.Object
	err     error
}

func (d *platformCloneReader) ReadHostObject(r *gov8.DelegateValueDeserializer) (*gov8.Object, bool) {
	index, ok, err := r.ReadUint32()
	if err != nil || !ok || int(index) >= len(d.objects) {
		d.err = err
		return nil, false
	}
	return d.objects[index], true
}

const platformCloneMagic = "MimicClone1\x00"

func (d *cloneErrorReporter) ThrowDataCloneError(message string) bool {
	d.message = message
	return false
}

// SerializeStructuredClone owns wire bytes rather than engine handles. The
// receiver can deserialize them in its own realm without exporting JS objects
// through Go (which loses exotic brands, aliases and cycles).
func (a *adapter) SerializeStructuredClone(value engine.Value, rejectHostObject engine.Value) ([]byte, error) {
	var bytes []byte
	_, err := a.withCloneScope(func(iso *gov8.Isolate, realm *gov8.Context, scope *gov8.Scope) (engine.Value, error) {
		input, err := a.cloneLocal(scope, value)
		if err != nil {
			return nil, err
		}
		tc, err := iso.NewTryCatch()
		if err != nil {
			return nil, err
		}
		defer tc.Close()
		reporter := &cloneErrorReporter{scope: scope, realm: realm}
		if rejectHostObject != nil {
			predicate, err := a.cloneLocal(scope, rejectHostObject)
			if err != nil {
				return nil, err
			}
			reporter.reject, _, err = gov8.AsFunction(predicate, realm)
			if err != nil {
				return nil, err
			}
		}
		serializer, err := gov8.NewDelegateValueSerializer(scope, realm, reporter)
		if err != nil {
			return nil, err
		}
		defer serializer.Close()
		if err = serializer.WriteHeader(); err != nil {
			return nil, err
		}
		ok, err := serializer.WriteValue(realm, input, tc)
		if reporter.message != "" {
			return nil, &engine.DataCloneError{Message: reporter.message}
		}
		if caught, _ := tc.HasCaught(); caught {
			return nil, a.callError(tc, scope, realm)
		}
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, &engine.DataCloneError{Message: "The value could not be cloned."}
		}
		bytes, err = serializer.Release()
		if err == nil && len(reporter.platforms) > 0 {
			table, e := json.Marshal(reporter.platforms)
			if e != nil {
				return nil, e
			}
			header := []byte(platformCloneMagic)
			header = binary.LittleEndian.AppendUint32(header, uint32(len(table)))
			header = append(header, table...)
			bytes = append(header, bytes...)
		}
		if err != nil {
			return nil, err
		}

		return nil, err
	})
	return bytes, err
}

func (a *adapter) DeserializeStructuredClone(bytes []byte) (engine.Value, error) {
	return a.DeserializeStructuredClonePlatform(bytes, nil)
}

func (a *adapter) DeserializeStructuredClonePlatform(wire []byte, decoder engine.Value) (engine.Value, error) {
	return a.withCloneScope(func(iso *gov8.Isolate, realm *gov8.Context, scope *gov8.Scope) (engine.Value, error) {
		tc, err := iso.NewTryCatch()
		if err != nil {
			return nil, err
		}
		defer tc.Close()
		reader := &platformCloneReader{}
		if bytes.HasPrefix(wire, []byte(platformCloneMagic)) {
			wire = wire[len(platformCloneMagic):]
			if len(wire) < 4 {
				return nil, fmt.Errorf("invalid platform clone table")
			}
			length := binary.LittleEndian.Uint32(wire)
			wire = wire[4:]
			if uint64(length) > uint64(len(wire)) {
				return nil, fmt.Errorf("invalid platform clone length")
			}
			var table []string
			if err = json.Unmarshal(wire[:length], &table); err != nil {
				return nil, err
			}
			wire = wire[length:]
			if decoder == nil {
				return nil, &engine.DataCloneError{Message: "Platform clone decoder is unavailable."}
			}
			v, e := a.cloneLocal(scope, decoder)
			if e != nil {
				return nil, e
			}
			decode, ok, e := gov8.AsFunction(v, realm)
			if e != nil {
				return nil, e
			}
			if !ok {
				return nil, fmt.Errorf("invalid platform clone decoder")
			}
			// V8's ReadHostObject forbids JavaScript. Materialize registered projections
			// before entering ReadValue, then return only native locals from the hook.
			for _, payload := range table {
				v, e := scope.NewString(payload)
				if e != nil {
					return nil, e
				}
				out, ok, e := decode.Call(scope, v, v)
				if caught, _ := tc.HasCaught(); caught {
					return nil, a.callError(tc, scope, realm)
				}
				if e != nil {
					return nil, e
				}
				if !ok {
					return nil, fmt.Errorf("platform clone decoder returned no value")
				}
				obj, e := out.ToObject(scope, realm, tc)
				if e != nil {
					return nil, e
				}
				reader.objects = append(reader.objects, obj)
			}
		}

		deserializer, err := gov8.NewDelegateValueDeserializer(scope, realm, wire, reader)
		if err != nil {
			return nil, err
		}
		defer deserializer.Close()
		ok, err := deserializer.ReadHeader(realm, tc)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("invalid structured clone header")
		}
		output, err := deserializer.ReadValue(realm, tc)
		if caught, _ := tc.HasCaught(); caught {
			return nil, a.callError(tc, scope, realm)
		}
		if reader.err != nil {
			return nil, reader.err
		}
		if err != nil {
			return nil, err
		}
		return a.persist(scope, output)
	})
}

func (a *adapter) StructuredClone(value engine.Value, rejectHostObject engine.Value) (engine.Value, error) {
	bytes, err := a.SerializeStructuredClone(value, rejectHostObject)
	if err != nil {
		return nil, err
	}
	return a.DeserializeStructuredClone(bytes)
}

func (a *adapter) withCloneScope(clone func(*gov8.Isolate, *gov8.Context, *gov8.Scope) (engine.Value, error)) (engine.Value, error) {
	if callback := a.onCallback(); callback != nil {
		return clone(callback.scope.Isolate(), callback.ctx, callback.scope.Scope())
	}
	return a.run(func(s *state, realm *gov8.Context, scope *gov8.Scope) (engine.Value, error) {
		return clone(s.isolate, realm, scope)
	})
}

func (a *adapter) cloneLocal(scope *gov8.Scope, value engine.Value) (gov8.Value, error) {
	if a.onCallback() != nil {
		return a.localCallback(value)
	}
	return a.local(scope, value)
}

// A storage graph must reject proxies without invoking author traps. This also
// lets browser-owned serializers support platform values (Blob/File) while
// retaining the same native exotic-object check as V8's structured clone.
func (a *adapter) IsStructuredCloneProxy(value engine.Value) bool {
	var proxy bool
	check := func(scope *gov8.Scope) (engine.Value, error) {
		v, err := a.cloneLocal(scope, value)
		if err != nil {
			return nil, err
		}
		proxy, err = v.IsProxy()
		return nil, err
	}
	if callback := a.onCallback(); callback != nil {
		_, _ = check(callback.scope.Scope())
	} else {
		_, _ = a.run(func(_ *state, _ *gov8.Context, scope *gov8.Scope) (engine.Value, error) { return check(scope) })
	}
	return proxy
}
