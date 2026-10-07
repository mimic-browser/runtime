package cdp

import (
	"encoding/json"

	"github.com/moreveal/mimic/internal/cliui"
	"github.com/moreveal/mimic/internal/network"
	"github.com/moreveal/mimic/internal/profile"
)

func (s *session) handlePortableProfile(m message) (any, bool, error) {
	allowed := map[string]bool{}
	switch m.Method {
	case "Mimic.getVersion":
	case "Mimic.generateProfile":
		for _, key := range []string{"browser", "version", "platform", "seed"} {
			allowed[key] = true
		}
	case "Mimic.importProfile":
		allowed["profile"], allowed["mode"] = true, true
	case "Mimic.exportProfile":
		allowed["profile"] = true
	case "Mimic.createContext":
		for _, key := range []string{"profile", "proxy", "resourcePolicy", "disposeOnDetach", "media"} {
			allowed[key] = true
		}
	default:
		return nil, false, nil
	}
	bad := func(path, reason string) (any, bool, error) {
		return nil, true, &profile.Error{Path: path, Reason: "invalidParameter", Message: reason}
	}
	raw := m.Params
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	params, err := profile.Decode(raw)
	if err != nil {
		return nil, true, err
	}
	for key, value := range params {
		if !allowed[key] || value == nil {
			return bad(key, "unknown parameter or null value")
		}
	}
	base := s.server.Browser.Environment()
	if m.Method == "Mimic.getVersion" {
		return map[string]any{"version": cliui.BuildVersion(), "chromeVersion": base.Product.FullVersion, "baseProfile": base.ProfileID}, true, nil
	}
	var descriptor profile.Descriptor
	var resolved profile.Document
	switch m.Method {
	case "Mimic.generateProfile":
		resolved, descriptor, err = profile.Generate(raw, base)
	case "Mimic.exportProfile":
		token, ok := params["profile"].(string)
		if !ok {
			return bad("profile", "expected a profile token")
		}
		resolved, descriptor, err = profile.ResolveToken(token, base)
		if err != nil {
			return nil, true, err
		}
		return map[string]any{"profile": descriptor, "warnings": profile.Warnings(descriptor.Mode)}, true, nil
	case "Mimic.importProfile":
		input, ok := params["profile"].(map[string]any)
		if !ok {
			return bad("profile", "expected an exported descriptor or explicit manual fields")
		}
		encoded, _ := json.Marshal(input)
		if mode, exists := params["mode"]; exists {
			if mode != "manual" {
				return bad("mode", "only explicit manual import accepts field overrides")
			}
			resolved, descriptor, err = profile.ImportManual(encoded, base)
		} else {
			resolved, descriptor, err = profile.Restore(encoded, base)
		}
	case "Mimic.createContext":
		value, exists := params["profile"]
		if !exists {
			value = map[string]any{"generate": map[string]any{}}
		}
		switch input := value.(type) {
		case string:
			resolved, descriptor, err = profile.ResolveToken(input, base)
		case map[string]any:
			generation, ok := input["generate"].(map[string]any)
			if len(input) != 1 || !ok {
				return bad("profile", "use {generate: {...}} or a profile token; manual JSON belongs in Mimic.importProfile")
			}
			encoded, _ := json.Marshal(generation)
			resolved, descriptor, err = profile.Generate(encoded, base)
		default:
			return bad("profile", "expected a profile token or generation request")
		}
	}
	if err != nil {
		return nil, true, err
	}
	token := descriptor.Token()
	if len(token) > 1<<20 {
		return bad("profile", "normalized profile token exceeds 1 MiB")
	}
	if m.Method == "Mimic.importProfile" || m.Method == "Mimic.generateProfile" {
		if validationErr := s.server.Browser.ValidateResolvedProfile(resolved); validationErr != nil {
			return nil, true, validationErr
		}
	}
	result := map[string]any{"profile": token, "profileId": descriptor.ProfileID, "mode": descriptor.Mode, "warnings": profile.Warnings(descriptor.Mode)}
	if m.Method != "Mimic.createContext" {
		return result, true, nil
	}
	var proxyJSON []byte
	if value, exists := params["proxy"]; exists {
		if _, ok := value.(map[string]any); !ok {
			return bad("proxy", "expected object")
		}
		proxyJSON, _ = json.Marshal(value)
	}
	var policy *network.ResourcePolicy
	if value, exists := params["resourcePolicy"]; exists {
		if _, ok := value.(map[string]any); !ok {
			return bad("resourcePolicy", "expected object")
		}
		encoded, _ := json.Marshal(value)
		parsed, parseErr := network.ParseResourcePolicy(encoded)
		if parseErr != nil {
			return nil, true, parseErr
		}
		policy = &parsed
	}
	dispose := false
	if value, exists := params["disposeOnDetach"]; exists {
		var ok bool
		dispose, ok = value.(bool)
		if !ok {
			return bad("disposeOnDetach", "expected boolean")
		}
	}
	c, err := s.server.Browser.NewResolvedProfileContext(resolved, proxyJSON, policy)
	if err != nil {
		return nil, true, err
	}
	if value, exists := params["media"]; exists {
		media, ok := value.(map[string]any)
		if !ok {
			_ = c.Close()
			return bad("media", "expected media configuration object")
		}
		if _, exists := media["seed"]; !exists && descriptor.Seed != "" {
			media["seed"] = descriptor.Seed
		}
		raw, _ := json.Marshal(media)
		configured, configureErr := c.SetMediaProfileJSON(raw)
		if configureErr != nil {
			_ = c.Close()
			return bad("media", configureErr.Error())
		}
		result["media"] = configured
	}
	s.transport.mu.Lock()
	s.transport.contexts[c.ID] = dispose
	s.transport.mu.Unlock()
	result["browserContextId"] = c.ID
	return result, true, nil
}
