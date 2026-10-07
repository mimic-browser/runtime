package cdp

import (
	"encoding/json"
	"fmt"

	"github.com/moreveal/mimic/internal/browser"
)

func (s *session) handleMediaProfile(m message) (any, bool, error) {
	switch m.Method {
	case "Mimic.getMediaSources", "Mimic.getMediaPresets", "Mimic.validateMediaProfile", "Mimic.setMediaProfile", "Mimic.getMediaProfile":
	default:
		return nil, false, nil
	}
	params := map[string]json.RawMessage{}
	if len(m.Params) != 0 {
		if err := json.Unmarshal(m.Params, &params); err != nil || params == nil {
			return nil, true, invalidProtocolParameter("params", "expected object")
		}
	}
	context := s.server.Context
	if raw, exists := params["browserContextId"]; exists {
		var id string
		if err := json.Unmarshal(raw, &id); err != nil || id == "" {
			return nil, true, invalidProtocolParameter("params.browserContextId", "expected nonempty string")
		}
		var ok bool
		context, ok = s.server.Browser.Context(id)
		if !ok {
			return nil, true, fmt.Errorf("browser context not found")
		}
		delete(params, "browserContextId")
	}
	if m.Method == "Mimic.setMediaProfile" || m.Method == "Mimic.validateMediaProfile" {
		raw, err := json.Marshal(params)
		if err != nil {
			return nil, true, err
		}
		var profile browser.MediaProfile
		if m.Method == "Mimic.setMediaProfile" {
			profile, err = context.SetMediaProfileJSON(raw)
		} else {
			profile, err = context.ValidateMediaProfileJSON(raw)
		}
		if err != nil {
			return nil, true, invalidProtocolParameter("params", err.Error())
		}
		return map[string]any{"profile": profile, "nativeModesVerified": false}, true, nil
	}
	if len(params) != 0 {
		return nil, true, invalidProtocolParameter("params", "unexpected fields")
	}
	switch m.Method {
	case "Mimic.getMediaSources":
		sources, diagnostics := context.MediaSources()
		return map[string]any{"sources": sources, "diagnostics": diagnostics}, true, nil
	case "Mimic.getMediaPresets":
		return map[string]any{"presets": browser.MediaPresets()}, true, nil
	default:
		return map[string]any{"profile": context.MediaProfile(), "diagnostics": context.MediaDiagnostics()}, true, nil
	}
}
