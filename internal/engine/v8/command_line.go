//go:build (windows || linux) && amd64

package v8

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"

	gov8 "github.com/maclof/gov8"
	"github.com/moreveal/mimic/internal/engine"
)

var inspectorBigIntLiteral = regexp.MustCompile(`^-?[0-9]+n$`)

type commandLineChannel struct {
	nativeProfileChannel
	contextID int
}

func (c *commandLineChannel) SendNotification(message *gov8.InspectorStringBuffer) {
	var event struct {
		Method string
		Params struct{ Context struct{ ID int } }
	}
	if json.Unmarshal([]byte(message.StringView().String()), &event) == nil && event.Method == "Runtime.executionContextCreated" {
		c.contextID = event.Params.Context.ID
	}
}

// Create the native inspector only for an explicit command-line evaluation.
// It owns no background tasks, debugger subscriptions or persistent contexts.
func (a *adapter) EvalCommandLine(ctx context.Context, source string, selected []engine.Value, lastConsole engine.Value) (engine.Value, error) {
	return a.runContext(ctx, func(s *state, realm *gov8.Context, scope *gov8.Scope) (engine.Value, error) {
		inspector, err := gov8.NewInspector(s.isolate)
		if err != nil {
			return nil, err
		}
		defer inspector.Close()
		empty := gov8.EmptyInspectorStringView()
		if err := inspector.ContextCreated(realm, 1, empty, gov8.NewInspectorStringView8([]byte(`{"isDefault":true}`))); err != nil {
			return nil, err
		}
		defer inspector.ContextDestroyed(realm)
		channel := &commandLineChannel{nativeProfileChannel: nativeProfileChannel{responses: map[int32]json.RawMessage{}}}
		session, err := inspector.Connect(1, channel, empty, gov8.InspectorFullyTrusted)
		if err != nil {
			return nil, err
		}
		defer session.Close()
		if err := session.DispatchProtocolMessage(gov8.NewInspectorStringView8([]byte(`{"id":2,"method":"Runtime.enable"}`))); err != nil {
			return nil, err
		}
		if channel.contextID == 0 {
			return nil, fmt.Errorf("Native inspector execution context unavailable")
		}
		if lastConsole != nil {
			local, err := a.local(scope, lastConsole)
			if err != nil {
				return nil, err
			}
			wrapped, present, err := session.WrapObject(scope, realm, local, empty, false)
			if err != nil {
				return nil, err
			}
			if !present {
				return nil, fmt.Errorf("Native inspector result is unavailable")
			}
			defer wrapped.Close()
			data, err := wrapped.ToBytes()
			if err != nil {
				return nil, err
			}
			data, valid, err := gov8.CRDTPCBORToJSON(data)
			if err != nil {
				return nil, err
			}
			if !valid {
				return nil, fmt.Errorf("Native inspector returned malformed remote object")
			}
			var remote map[string]any
			if err := json.Unmarshal(data, &remote); err != nil {
				return nil, err
			}
			argument := map[string]any{}
			for _, key := range []string{"objectId", "value", "unserializableValue"} {
				if value, ok := remote[key]; ok {
					argument[key] = value
				}
			}
			seed, err := json.Marshal(map[string]any{"id": 3, "method": "Runtime.callFunctionOn", "params": map[string]any{"executionContextId": channel.contextID, "functionDeclaration": "function(value){return value}", "arguments": []any{argument}, "objectGroup": "console"}})
			if err != nil {
				return nil, err
			}
			if err := session.DispatchProtocolMessage(gov8.NewInspectorStringView8(seed)); err != nil {
				return nil, err
			}
			var reply struct {
				Error  any
				Result struct{ ExceptionDetails any }
			}
			if err := json.Unmarshal(channel.responses[3], &reply); err != nil {
				return nil, err
			}
			if reply.Error != nil || reply.Result.ExceptionDetails != nil {
				return nil, fmt.Errorf("Native inspector last-result initialization failed: %s", channel.responses[3])
			}
		}
		for _, value := range selected {
			var global *gov8.Global
			if value != nil {
				owned, ok := value.(*runtimeValue)
				if !ok || owned.runtime.owner != a.owner || owned.global == nil {
					return nil, fmt.Errorf("Inspected selection belongs to an unavailable execution owner")
				}
				global = owned.global
			}
			inspectable, err := s.isolate.NewInspectorInspectable(func(callback *gov8.CallbackScope, _ *gov8.Context) (gov8.Value, error) {
				if global == nil {
					return callback.Scope().Undefined()
				}
				return global.ToLocal(callback.Scope())
			}, nil)
			if err != nil {
				return nil, err
			}
			if err := session.AddInspectedObject(inspectable); err != nil {
				inspectable.Close()
				return nil, err
			}
		}
		command, err := json.Marshal(map[string]any{"id": 1, "method": "Runtime.evaluate", "params": map[string]any{"expression": source, "contextId": channel.contextID, "includeCommandLineAPI": true, "allowUnsafeEvalBlockedByCSP": false}})
		if err != nil {
			return nil, err
		}
		if err := session.DispatchProtocolMessage(gov8.NewInspectorStringView8(command)); err != nil {
			return nil, err
		}
		var response struct {
			Error  *struct{ Message string }
			Result struct {
				Result           map[string]any
				ExceptionDetails map[string]any
			}
		}
		if err := json.Unmarshal(channel.responses[1], &response); err != nil {
			return nil, err
		}
		if response.Error != nil {
			return nil, fmt.Errorf("V8 command-line evaluation: %s", response.Error.Message)
		}
		remote := response.Result.Result
		value, err := a.inspectorResult(s.isolate, scope, realm, session, remote)
		if err != nil {
			return nil, err
		}
		if response.Result.ExceptionDetails != nil {
			return nil, &callException{error: fmt.Errorf("%v", remote["description"]), value: value}
		}
		return value, nil
	})
}

func (a *adapter) inspectorResult(isolate *gov8.Isolate, scope *gov8.Scope, realm *gov8.Context, session *gov8.InspectorSession, remote map[string]any) (engine.Value, error) {
	if id, ok := remote["objectId"].(string); ok {
		value, _, _, err := session.UnwrapObject(scope, gov8.NewInspectorStringView8([]byte(id)))
		if err != nil {
			return nil, err
		}
		return a.persist(scope, value)
	}
	if literal, ok := remote["unserializableValue"].(string); ok {
		if literal != "NaN" && literal != "Infinity" && literal != "-Infinity" && literal != "-0" && !inspectorBigIntLiteral.MatchString(literal) {
			return nil, fmt.Errorf("Unsupported native inspector scalar")
		}
		return a.evalScoped(isolate, realm, scope, literal, "")
	}
	if remote["type"] == "undefined" {
		value, err := scope.Undefined()
		if err != nil {
			return nil, err
		}
		return a.persist(scope, value)
	}
	value, err := a.marshal(scope, realm, remote["value"])
	if err != nil {
		return nil, err
	}
	return a.persist(scope, value)
}
