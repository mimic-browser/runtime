package cdp

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestConfigureEmptyFrameworkContext(t *testing.T) {
	s, address := runningServer(t)
	c := browserConnection(t, address)
	created := wireCall(t, c, 1, "Target.createBrowserContext", map[string]any{})
	id := created["browserContextId"].(string)
	probe := wireCall(t, c, 2, "Target.createTarget", map[string]any{"browserContextId": id, "url": "about:blank"})["targetId"].(string)
	generated := wireCall(t, c, 3, "Mimic.generateProfile", map[string]any{"seed": "sdk-context"})
	params := map[string]any{"browserContextId": id, "profile": generated["profile"], "media": map[string]any{"devices": []any{}}}
	if err := c.WriteJSON(map[string]any{"id": 4, "method": "Mimic.configureContext", "params": params}); err != nil {
		t.Fatal(err)
	}
	if readReply(t, c, 4)["error"] == nil {
		t.Fatal("configured Context with an existing Page")
	}
	wireCall(t, c, 5, "Target.closeTarget", map[string]any{"targetId": probe})
	before := wireCall(t, c, 6, "Mimic.getProfile", map[string]any{"browserContextId": id})
	bad := map[string]any{"browserContextId": id, "profile": generated["profile"], "media": map[string]any{"devices": nil}}
	if err := c.WriteJSON(map[string]any{"id": 7, "method": "Mimic.configureContext", "params": bad}); err != nil {
		t.Fatal(err)
	}
	if readReply(t, c, 7)["error"] == nil {
		t.Fatal("invalid media was accepted")
	}
	after := wireCall(t, c, 8, "Mimic.getProfile", map[string]any{"browserContextId": id})
	if !reflect.DeepEqual(before, after) {
		t.Fatal("failed configuration changed identity")
	}
	configured := wireCall(t, c, 9, "Mimic.configureContext", params)
	if configured["browserContextId"] != id || configured["profile"] != generated["profile"] {
		t.Fatal(configured)
	}
	if err := c.WriteJSON(map[string]any{"id": 10, "method": "Mimic.configureContext", "params": params}); err != nil {
		t.Fatal(err)
	}
	if readReply(t, c, 10)["error"] == nil {
		t.Fatal("managed Context was reconfigured")
	}
	target := wireCall(t, c, 11, "Target.createTarget", map[string]any{"browserContextId": id, "url": "about:blank"})["targetId"].(string)
	page, _, ok := s.target(target)
	if !ok || page.ContextID() != id {
		t.Fatal("framework Context identity changed")
	}
	if page.CheckProfileMutation() == nil {
		t.Fatal("new Page lost managed identity")
	}
	// Repeating the selected values neither unlocks nor changes the profile.
	sid := wireCall(t, c, 15, "Target.attachToTarget", map[string]any{"targetId": target, "flatten": true})["sessionId"].(string)
	fonts := page.Environment().Fonts
	for index, call := range []struct {
		method string
		params map[string]any
	}{
		{"Page.setFontFamilies", map[string]any{"fontFamilies": map[string]any{"serif": fonts.Serif, "sansSerif": fonts.SansSerif, "fixed": fonts.Monospace}}},
		{"Emulation.setEmulatedMedia", map[string]any{"features": []any{map[string]any{"name": "prefers-color-scheme", "value": ""}, map[string]any{"name": "prefers-reduced-motion", "value": ""}}}},
	} {
		if err := c.WriteJSON(map[string]any{"id": 16 + index, "sessionId": sid, "method": call.method, "params": call.params}); err != nil {
			t.Fatal(err)
		}
		if response := readReply(t, c, float64(16+index)); response["error"] != nil {
			t.Fatal(response)
		}
	}
	profile := wireCall(t, c, 12, "Mimic.getProfile", map[string]any{"targetId": target})
	contextProfile := wireCall(t, c, 13, "Mimic.getProfile", map[string]any{"browserContextId": id})
	if !reflect.DeepEqual(profile, contextProfile) {
		t.Fatal("Page and Context profiles differ")
	}
	wireCall(t, c, 14, "Target.disposeBrowserContext", map[string]any{"browserContextId": id})
}

func TestUnknownMimicCommandPreservesMethodNotFound(t *testing.T) {
	_, address := runningServer(t)
	c := browserConnection(t, address)
	if err := c.WriteJSON(map[string]any{"id": 1, "method": "Mimic.futureUnschematizedCommand", "params": map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	response := readReply(t, c, 1)
	raw, _ := json.Marshal(response["error"])
	var failure protocolError
	if err := json.Unmarshal(raw, &failure); err != nil || failure.Code != -32601 {
		t.Fatalf("error envelope: %s (%v)", raw, err)
	}
}

func TestBrowserContextEmptyProxyBypass(t *testing.T) {
	s, address := runningServer(t)
	c := browserConnection(t, address)
	before := len(s.Browser.Contexts())
	created := wireCall(t, c, 1, "Target.createBrowserContext", map[string]any{"proxyBypassList": ""})
	id := created["browserContextId"].(string)
	if len(s.Browser.Contexts()) != before+1 {
		t.Fatal("Context was not allocated")
	}
	if err := c.WriteJSON(map[string]any{"id": 2, "method": "Target.createBrowserContext", "params": map[string]any{"proxyBypassList": "example.org"}}); err != nil {
		t.Fatal(err)
	}
	if readReply(t, c, 2)["error"] == nil || len(s.Browser.Contexts()) != before+1 {
		t.Fatal("nonempty unsupported bypass allocated a Context")
	}
	wireCall(t, c, 3, "Target.disposeBrowserContext", map[string]any{"browserContextId": id})
}
