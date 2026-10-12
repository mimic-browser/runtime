package cdp

import (
	"encoding/json"
	"fmt"

	"github.com/moreveal/mimic/internal/browser"
)

type nativeInspectorEnvelope struct {
	Result any `json:"result"`
	Error  *struct {
		Code    int64  `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (s *session) nativeInspectorNotification(raw []byte) {
	var event struct {
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if json.Unmarshal(raw, &event) != nil || event.Method == "" {
		return
	}
	var params any = map[string]any{}
	if len(event.Params) != 0 {
		// V8 already serialized the protocol payload. Keep it opaque through
		// transport instead of decoding thousands of scriptParsed notifications
		// into Go maps and encoding them again while author JavaScript runs.
		params = event.Params
	}
	s.event(event.Method, params)
}

func nativeInspectorCommand(m message) []byte {
	command := map[string]any{"id": m.ID, "method": m.Method}
	if len(m.Params) != 0 && string(m.Params) != "null" {
		command["params"] = json.RawMessage(m.Params)
	}
	raw, _ := json.Marshal(command)
	return raw
}

func nativeInspectorResult(raw []byte) (any, error) {
	var envelope nativeInspectorEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}
	if envelope.Error != nil {
		return nil, fmt.Errorf("%s", envelope.Error.Message)
	}
	if envelope.Result == nil {
		return map[string]any{}, nil
	}
	return envelope.Result, nil
}

func (s *session) handleDebugger(m message) (any, bool, error) {
	switch m.Method {
	case "Debugger.enable":
		if s.nativeInspector == nil {
			s.nativeInspector = browser.NewNativeInspector(s.page, s.nativeInspectorNotification)
		}
		raw, err := s.nativeInspector.Enable(nativeInspectorCommand(m))
		if err != nil {
			return nil, true, err
		}
		s.setDomain("Debugger", true)
		result, err := nativeInspectorResult(raw)
		return result, true, err
	case "Debugger.disable", "Debugger.getScriptSource", "Debugger.setBreakpoint", "Debugger.evaluateOnCallFrame", "Debugger.resume":
		if s.nativeInspector == nil {
			return nil, true, fmt.Errorf("Debugger is not enabled")
		}
		raw, err := s.nativeInspector.Dispatch(nativeInspectorCommand(m))
		if err != nil {
			return nil, true, err
		}
		result, err := nativeInspectorResult(raw)
		if m.Method == "Debugger.disable" && err == nil {
			err = s.nativeInspector.Close()
			s.nativeInspector = nil
			s.setDomain("Debugger", false)
		}
		return result, true, err
	default:
		return nil, false, nil
	}
}
