package browser

import (
	"context"
	"fmt"
	"strconv"

	"github.com/moreveal/mimic/internal/engine"
)

// Resolve the Window's current realm for every operation. Object references
// returned by descriptors retain their original realm through crossFrameData.
func (r *Realm) installWindowReflection(host map[string]any) {
	if native, ok := r.runtime.(engine.ExceptionStateRuntime); ok {
		host["initializeExceptionState"] = native.ExceptionStateFactory()
	}
	if native, ok := r.runtime.(engine.ReceiverDispatchRuntime); ok {
		host["createReceiverDispatch"] = native.ReceiverDispatchFactory()
	}
	if native, ok := r.runtime.(engine.PropertyObservationRuntime); ok {
		host["createObservedObject"] = native.PropertyObservationFactory()
	}
	if native, ok := r.runtime.(engine.GeneratedLazyRuntime); ok {
		host["createGeneratedLazyInstaller"] = native.GeneratedLazyInstaller()
	}
	if native, ok := r.runtime.(engine.GeneratedConstructorRuntime); ok {
		host["createGeneratedConstructorFactory"] = native.GeneratedConstructorFactory()
	}
	if native, ok := r.runtime.(engine.InterceptedObjectRuntime); ok {
		host["createWindowObject"] = r.fn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
			return native.NewInterceptedObject(args[0])
		})
	}
	host["frameGlobalReflect"] = r.fn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		frame := r.agent.Page().frame(strarg(args, 0))
		if !r.canAccess(frame) {
			return nil, fmt.Errorf("SecurityError: Blocked cross-origin frame access")
		}
		target, err := r.worldForFrame(frame)
		if err != nil {
			return nil, err
		}
		operation := strarg(args, 1)
		return r.crossFrameData(target, func(ctx context.Context) (any, error) {
			global := target.runtime.Get("globalThis")
			if operation == "keys" {
				keys, err := target.callFrameReflection(ctx, "keys", global, nil, nil)
				if err != nil {
					return nil, err
				}
				length := int(numberValue(target.runtime.GetProperty(keys, "length").Export()))
				out := make([]any, 0, length)
				for i := 0; i < length; i++ {
					out = append(out, target.encodeFrameKey(target.runtime.GetProperty(keys, strconv.Itoa(i))))
				}
				return out, nil
			}
			if operation == "prototype" {
				value, err := target.callFrameReflection(ctx, operation, global, nil, nil)
				if err != nil {
					return nil, err
				}
				return target.crossRealmValue(value)
			}
			key, err := target.decodeFrameKey(ctx, arg(args, 2))
			if err != nil {
				return nil, err
			}
			if operation == "delete" {
				value, err := target.callFrameReflection(ctx, operation, global, key, nil)
				if err != nil {
					return nil, err
				}
				return value.Export(), nil
			}
			if operation == "define" {
				raw, _ := arg(args, 3).(map[string]any)
				// Keep decoded functions/objects as JS values. Nesting engine
				// handles in a Go map either leaks wrappers (Goja) or cannot be
				// marshaled during a reentrant V8 callback.
				descriptor, err := target.callFrameReflection(ctx, "object", nil, nil, nil)
				if err != nil {
					return nil, err
				}
				for name, value := range raw {
					var field engine.Value
					if name == "value" || name == "get" || name == "set" {
						decoded, err := target.decodeFrameArgument(value)
						if err != nil {
							return nil, err
						}
						field = decoded
					} else {
						field = target.val(value)
					}
					if err := target.runtime.SetProperty(descriptor, name, field); err != nil {
						return nil, err
					}
				}
				value, err := target.callFrameReflection(ctx, operation, global, key, descriptor)
				if err != nil {
					return nil, err
				}
				return value.Export(), nil
			}
			desc, err := target.callFrameReflection(ctx, "descriptor", global, key, nil)
			if err != nil {
				return nil, err
			}
			field := func(name string) engine.Value { return target.runtime.GetProperty(desc, name) }
			if field("exists").Export() != true {
				return map[string]any{"exists": false}, nil
			}
			accessor := field("accessor").Export() == true
			out := map[string]any{"exists": true, "accessor": accessor, "enumerable": field("enumerable").Export(), "configurable": field("configurable").Export()}
			if accessor {
				out["get"], err = target.crossRealmValue(field("get"))
				if err == nil {
					out["set"], err = target.crossRealmValue(field("set"))
				}
			} else {
				out["writable"] = field("writable").Export()
				out["value"], err = target.encodeReflectedValue(desc)
			}
			return out, err
		})
	})
}
