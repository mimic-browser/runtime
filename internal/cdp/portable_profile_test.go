package cdp

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestPortableProfilesCDP(t *testing.T) {
	s, addr := runningServer(t)
	c := browserConnection(t, addr)
	baseline := len(s.Browser.Contexts())
	version := wireCall(t, c, 100, "Mimic.getVersion", map[string]any{})
	if version["version"] == "" || version["chromeVersion"] == "" {
		t.Fatal(version)
	}
	generated := wireCall(t, c, 1, "Mimic.generateProfile", map[string]any{"seed": "portable"})
	exported := wireCall(t, c, 2, "Mimic.exportProfile", map[string]any{"profile": generated["profile"]})
	imported := wireCall(t, c, 3, "Mimic.importProfile", map[string]any{"profile": exported["profile"]})
	if imported["profile"] != generated["profile"] || len(s.Browser.Contexts()) != baseline {
		t.Fatal("roundtrip or allocation", imported)
	}
	created := wireCall(t, c, 4, "Mimic.createContext", map[string]any{"profile": imported["profile"], "disposeOnDetach": true})
	target := wireCall(t, c, 5, "Target.createTarget", map[string]any{"browserContextId": created["browserContextId"], "url": "about:blank"})["targetId"].(string)
	sid := wireCall(t, c, 6, "Target.attachToTarget", map[string]any{"targetId": target, "flatten": true})["sessionId"].(string)
	before := wireCall(t, c, 7, "Mimic.getProfile", map[string]any{"targetId": target})
	page, _, ok := s.target(target)
	if !ok {
		t.Fatal("created target disappeared")
	}
	differentScheme := "dark"
	if page.Environment().Preferences.ColorScheme == "dark" {
		differentScheme = "light"
	}
	commands := []struct {
		method string
		params map[string]any
	}{
		{"Emulation.setUserAgentOverride", map[string]any{"userAgent": "broken"}},
		{"Network.setUserAgentOverride", map[string]any{"userAgent": "broken"}},
		{"Emulation.setDeviceMetricsOverride", map[string]any{"width": 800, "height": 600, "deviceScaleFactor": 1, "mobile": false}},
		{"Emulation.setLocaleOverride", map[string]any{"locale": "fr-FR"}},
		{"Emulation.setTimezoneOverride", map[string]any{"timezoneId": "Europe/Paris"}},
		{"Emulation.setEmulatedMedia", map[string]any{"features": []any{map[string]any{"name": "prefers-color-scheme", "value": differentScheme}}}},
		{"Page.setFontFamilies", map[string]any{"fontFamilies": map[string]any{"sansSerif": "Mimic Changed Family"}}},
		{"Mimic.setViewport", map[string]any{"width": 800, "height": 600}},
		{"Mimic.updateProfile", map[string]any{"targetId": target, "patch": map[string]any{"window": map[string]any{"viewportWidth": 800}}}},
	}
	for i, command := range commands {
		id := 10 + i
		if err := c.WriteJSON(map[string]any{"id": id, "sessionId": sid, "method": command.method, "params": command.params}); err != nil {
			t.Fatal(err)
		}
		reply := readReply(t, c, float64(id))
		raw, _ := json.Marshal(reply)
		if !strings.Contains(string(raw), "profileLocked") {
			t.Fatal(command.method, string(raw))
		}
	}
	after := wireCall(t, c, 30, "Mimic.getProfile", map[string]any{"targetId": target})
	a, _ := json.Marshal(before)
	b, _ := json.Marshal(after)
	if string(a) != string(b) {
		t.Fatal("rejected mutation changed profile")
	}
	wireCall(t, c, 31, "Target.disposeBrowserContext", map[string]any{"browserContextId": created["browserContextId"]})
	if len(s.Browser.Contexts()) != baseline {
		t.Fatal("context retained")
	}
	for i, params := range []map[string]any{
		{"profile": map[string]any{}},
		{"profile": map[string]any{"schemaVersion": 1}},
		{"contractVersion": 2},
		{"profile": generated["profile"], "proxy": map[string]any{"server": "socks5://user:secret@host:1080"}},
		{"resourcePolicy": map[string]any{"presets": []string{"unknown"}}},
		{"resourcePolicy": map[string]any{"schemaVersion": 1}},
		{"disposeOnDetach": "yes"},
	} {
		id := 40 + i
		if err := c.WriteJSON(map[string]any{"id": id, "method": "Mimic.createContext", "params": params}); err != nil {
			t.Fatal(err)
		}
		reply := readReply(t, c, float64(id))
		raw, _ := json.Marshal(reply)
		if reply["error"] == nil || strings.Contains(string(raw), "secret") || len(s.Browser.Contexts()) != baseline {
			t.Fatal("non-atomic invalid create", string(raw))
		}
	}
}

func TestPortableProfileDisposeOnDetach(t *testing.T) {
	s, addr := runningServer(t)
	c := browserConnection(t, addr)
	baseline := len(s.Browser.Contexts())
	result := wireCall(t, c, 1, "Mimic.createContext", map[string]any{"disposeOnDetach": true})
	if result["profile"] == nil || len(s.Browser.Contexts()) != baseline+1 {
		t.Fatal(result)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(5 * time.Second)
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for len(s.Browser.Contexts()) != baseline {
		select {
		case <-deadline:
			t.Fatal("creating connection left a context after detach")
		case <-tick.C:
		}
	}
}
