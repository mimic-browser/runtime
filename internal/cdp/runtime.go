package cdp

import (
	"context"
	"fmt"
	"time"

	"github.com/moreveal/mimic/internal/browser"
)

func (s *session) runtimeDebugger() *browser.Debugger {
	if s.debugger == nil {
		s.debugger = browser.NewDebugger(s.page)
		s.debugger.ConsoleEnabled = func() bool { return s.domainEnabled("Runtime") }
		s.debugger.BeforeWait = func() {
			// Awaiting a Promise yields this command's turn. The next queued
			// Runtime call may be the resolver, on the very same session.
			s.activeOrder.release()
			s.activeOrder = nil
			s.page.UnlockCommands()
			s.commandMu.Unlock()
		}
		s.debugger.AfterWait = func() { s.commandMu.Lock(); s.page.LockExternalCommand() }
		s.debugger.Console = func(realmID, name string, args []any) {
			contextID, ok := s.contextForRealm(realmID)
			if ok {
				s.event("Runtime.consoleAPICalled", map[string]any{"type": name, "args": args, "executionContextId": contextID, "timestamp": float64(time.Now().UnixMilli())})
			}
		}
		s.debugger.BindingCalled = func(realmID, name, payload string) {
			if contextID, ok := s.contextForRealm(realmID); ok {
				s.event("Runtime.bindingCalled", map[string]any{"name": name, "payload": payload, "executionContextId": contextID})
			}
		}
	}
	return s.debugger
}

func (s *session) runtimeContext(contextID int64) (string, string, error) {
	if contextID == 0 {
		return "", "", nil
	}
	s.contextMu.Lock()
	defer s.contextMu.Unlock()
	if world, ok := s.worldContexts[contextID]; ok {
		frame, valid := s.page.Frame(world.FrameID)
		if !valid || frame.Realm == nil || frame.Realm.ID != world.MainRealmID {
			return "", "", fmt.Errorf("Cannot find context with specified id")
		}
		return world.FrameID, world.RealmID, nil
	}
	frameID, ok := s.frameByContext[contextID]
	if !ok {
		return "", "", fmt.Errorf("Cannot find context with specified id")
	}
	return frameID, s.realmByFrame[frameID], nil
}

func (s *session) runtimeRequestContext(params map[string]any, key string) (string, string, error) {
	unique, _ := params["uniqueContextId"].(string)
	if unique == "" {
		return s.runtimeContext(int64(intValue(params[key], 0)))
	}
	if _, hasID := params[key]; hasID {
		return "", "", fmt.Errorf("uniqueContextId and %s are mutually exclusive", key)
	}
	s.contextMu.Lock()
	defer s.contextMu.Unlock()
	for frameID, realmID := range s.realmByFrame {
		if unique == realmID {
			return frameID, realmID, nil
		}
	}
	for _, world := range s.worldContexts {
		if unique == world.RealmID {
			return world.FrameID, world.RealmID, nil
		}
	}
	return "", "", fmt.Errorf("Cannot find context with specified unique id")
}

func (s *session) handleRuntime(ctx context.Context, method string, params map[string]any) (result any, handled bool, err error) {
	switch method {
	case "Page.createIsolatedWorld", "Runtime.evaluate", "Runtime.callFunctionOn", "Runtime.awaitPromise", "Runtime.getProperties", "Runtime.releaseObject", "Runtime.releaseObjectGroup", "Runtime.addBinding", "Runtime.removeBinding", "Runtime.discardConsoleEntries":
	default:
		return nil, false, nil
	}
	// DevTools eagerly evaluates the prompt before Enter. Without engine-level
	// side-effect checking, executing this request would mutate the real Page.
	if (method == "Runtime.evaluate" || method == "Runtime.callFunctionOn") && params["throwOnSideEffect"] == true {
		return nil, true, fmt.Errorf("Side-effect-free evaluation is unsupported")
	}
	var cancel context.CancelFunc
	if timeout, ok := params["timeout"].(float64); ok && timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, time.Duration(timeout*float64(time.Millisecond)))
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}
	defer cancel()
	debugger := s.runtimeDebugger()
	debugger.Prune()
	options := browser.DebuggerOptions{ObjectGroup: stringValue(params["objectGroup"])}
	options.ReturnByValue, _ = params["returnByValue"].(bool)
	options.AwaitPromise, _ = params["awaitPromise"].(bool)
	options.CommandLineAPI, _ = params["includeCommandLineAPI"].(bool)
	if allow, ok := params["allowUnsafeEvalBlockedByCSP"].(bool); ok {
		options.RespectCSP = !allow
	}
	if serialization, ok := params["serializationOptions"].(map[string]any); ok {
		switch stringValue(serialization["serialization"]) {
		case "json":
			options.ReturnByValue = true
		case "idOnly":
			options.ReturnByValue = false
		case "deep":
			return nil, true, fmt.Errorf("Deep serialization is not supported")
		}
	}
	var frameID, realmID string
	switch method {
	case "Runtime.addBinding":
		if _, hasID := params["executionContextId"]; hasID {
			if _, hasName := params["executionContextName"]; hasName {
				return nil, true, fmt.Errorf("executionContextName is mutually exclusive with executionContextId")
			}
			frameID, realmID, err = s.runtimeContext(int64(intValue(params["executionContextId"], 0)))
			if realmID == "" && err == nil {
				if frame, ok := s.page.Frame(frameID); ok && frame.Realm != nil {
					realmID = frame.Realm.ID
				}
			}
		}
		_, byName := params["executionContextName"]
		if err == nil {
			err = debugger.AddBinding(ctx, stringValue(params["name"]), realmID, stringValue(params["executionContextName"]), byName)
		}
		result = map[string]any{}
	case "Runtime.removeBinding":
		debugger.RemoveBinding(stringValue(params["name"]))
		result = map[string]any{}
	case "Runtime.discardConsoleEntries":
		err = debugger.ReleaseObjectGroup(ctx, "console")
		result = map[string]any{}
	case "Page.createIsolatedWorld":
		result, err = s.createIsolatedWorld(ctx, params)
	case "Runtime.evaluate":
		frameID, realmID, err = s.runtimeRequestContext(params, "contextId")
		if err == nil {
			result, err = debugger.Evaluate(ctx, frameID, realmID, stringValue(params["expression"]), options)
		}
	case "Runtime.callFunctionOn":
		frameID, realmID, err = s.runtimeRequestContext(params, "executionContextId")
		if err == nil {
			result, err = debugger.CallFunction(ctx, frameID, realmID, stringValue(params["functionDeclaration"]), params, options)
		}
	case "Runtime.awaitPromise":
		result, err = debugger.AwaitPromise(ctx, stringValue(params["promiseObjectId"]), options)
	case "Runtime.getProperties":
		result, err = debugger.GetProperties(ctx, params)
	case "Runtime.releaseObject":
		err = debugger.ReleaseObject(ctx, stringValue(params["objectId"]))
		result = map[string]any{}
	case "Runtime.releaseObjectGroup":
		err = debugger.ReleaseObjectGroup(ctx, stringValue(params["objectGroup"]))
		result = map[string]any{}
	}
	return result, true, err
}
