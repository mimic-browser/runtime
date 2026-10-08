package cdp

import (
	"strings"
	"testing"
)

func TestCSSReadOnlyDomainUsesCanonicalStyle(t *testing.T) {
	s, addr := runningServer(t)
	if _, err := evaluatePageFixture(s.Page, `document.head.innerHTML='<style>#probe { color: rgb(12, 34, 56); --sdk-tone: orchid; }</style>'; document.body.innerHTML='<div id="probe">text</div>'`); err != nil {
		t.Fatal(err)
	}
	document, _ := s.Page.Document()
	node, _ := document.Find("#probe")
	connection := browserConnection(t, addr)
	sessionID := wireCall(t, connection, 1, "Target.attachToTarget", map[string]any{"targetId": s.Page.ID, "flatten": true})["sessionId"].(string)
	callError := func(id int, method string, params map[string]any, message string) {
		t.Helper()
		if err := connection.WriteJSON(map[string]any{"id": id, "sessionId": sessionID, "method": method, "params": params}); err != nil {
			t.Fatal(err)
		}
		reply := readReply(t, connection, float64(id))
		failure, ok := reply["error"].(map[string]any)
		if !ok || !strings.Contains(failure["message"].(string), message) {
			t.Fatalf("%s: expected %q, got %#v", method, message, reply)
		}
	}
	callError(2, "CSS.enable", map[string]any{}, "DOM agent needs to be enabled first")
	flatCall(t, connection, sessionID, 3, "DOM.enable", map[string]any{})
	flatCall(t, connection, sessionID, 4, "CSS.enable", map[string]any{})
	flatCall(t, connection, sessionID, 5, "CSS.enable", map[string]any{})
	checkStyle := func(id int, expected string) {
		t.Helper()
		result := flatCall(t, connection, sessionID, id, "CSS.getComputedStyleForNode", map[string]any{"nodeId": node.ID})
		styles := map[string]string{}
		for _, entry := range result["computedStyle"].([]any) {
			property := entry.(map[string]any)
			styles[property["name"].(string)] = property["value"].(string)
		}
		if styles["color"] != expected || styles["--sdk-tone"] != "orchid" {
			t.Fatalf("canonical style: color=%q --sdk-tone=%q", styles["color"], styles["--sdk-tone"])
		}
		if result["extraFields"].(map[string]any)["isAppearanceBase"] != false {
			t.Fatalf("appearance projection: %#v", result["extraFields"])
		}
	}
	checkStyle(6, "rgb(12, 34, 56)")
	if _, err := evaluatePageFixture(s.Page, `document.querySelector('#probe').style.color='rgb(65, 43, 21)'; window.getComputedStyle=()=>{throw new Error('author override must not run')}; CSSStyleDeclaration.prototype.getPropertyValue=()=>{throw new Error('author prototype must not run')}`); err != nil {
		t.Fatal(err)
	}
	checkStyle(7, "rgb(65, 43, 21)")
	callError(8, "CSS.getComputedStyleForNode", map[string]any{"nodeId": -1}, "Could not find node")
	flatCall(t, connection, sessionID, 9, "CSS.disable", map[string]any{})
	flatCall(t, connection, sessionID, 10, "CSS.disable", map[string]any{})
	callError(11, "CSS.getComputedStyleForNode", map[string]any{"nodeId": node.ID}, "CSS agent was not enabled")
	flatCall(t, connection, sessionID, 12, "CSS.enable", map[string]any{})
	checkStyle(13, "rgb(65, 43, 21)")
	other := browserConnection(t, addr)
	otherSession := wireCall(t, other, 1, "Target.attachToTarget", map[string]any{"targetId": s.Page.ID, "flatten": true})["sessionId"].(string)
	if err := other.WriteJSON(map[string]any{"id": 2, "sessionId": otherSession, "method": "CSS.getComputedStyleForNode", "params": map[string]any{"nodeId": node.ID}}); err != nil {
		t.Fatal(err)
	}
	if reply := readReply(t, other, 2); reply["error"] == nil {
		t.Fatal("CSS domain enable leaked into another session")
	}
}
