package cdp

import (
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func inspectorSession(t *testing.T, s *Server, addr string) (*inspectorWire, string) {
	t.Helper()
	w := newInspectorWire(t, addr)
	id := w.call(t, "", "Target.attachToTarget", map[string]any{"targetId": s.Page.ID, "flatten": true})["sessionId"].(string)
	return w, id
}

func TestInspectorInactiveAndIdleCost(t *testing.T) {
	s, addr := runningServer(t)
	w, id := inspectorSession(t, s, addr)
	for _, domain := range []string{"DOM", "CSS", "Runtime", "Network", "Page", "Overlay"} {
		w.call(t, id, domain+".enable", map[string]any{})
	}
	w.call(t, id, "DOM.getDocument", map[string]any{"depth": -1})
	allocations := testing.AllocsPerRun(100, func() { s.Page.LockCommands(); s.Page.UnlockCommands() })
	if allocations > 4 {
		t.Fatalf("unchanged inspector turns allocated %.1f objects", allocations)
	}
	w.call(t, id, "CSS.disable", map[string]any{})
	w.call(t, id, "DOM.disable", map[string]any{})
	allocations = testing.AllocsPerRun(100, func() { s.Page.LockCommands(); s.Page.UnlockCommands() })
	if allocations != 0 {
		t.Fatalf("disabled inspector turn allocated %.1f objects", allocations)
	}
}

func TestInspectorStylesSharedClientsAndCanonicalRuleIdentity(t *testing.T) {
	s, addr := runningServer(t)
	_, err := evaluatePageFixture(s.Page, `document.head.innerHTML='<style>#probe { color: red }</style>';document.body.innerHTML='<div id="probe">text</div>';globalThis.savedRule=document.styleSheets[0].cssRules[0];globalThis.savedStyle=savedRule.style`)
	if err != nil {
		t.Fatal(err)
	}
	a, aid := inspectorSession(t, s, addr)
	b, bid := inspectorSession(t, s, addr)
	for _, client := range []struct {
		w  *inspectorWire
		id string
	}{{a, aid}, {b, bid}} {
		client.w.call(t, client.id, "DOM.getDocument", map[string]any{"depth": -1})
		client.w.call(t, client.id, "CSS.enable", map[string]any{})
	}
	d, _ := s.Page.Document()
	node, _ := d.Find("#probe")
	matched := b.call(t, bid, "CSS.getMatchedStylesForNode", map[string]any{"nodeId": node.ID})
	style := matched["matchedCSSRules"].([]any)[0].(map[string]any)["rule"].(map[string]any)["style"].(map[string]any)
	a.call(t, aid, "CSS.disable", map[string]any{})
	b.call(t, bid, "CSS.setStyleTexts", map[string]any{"edits": []any{map[string]any{"styleSheetId": style["styleSheetId"], "range": style["range"], "text": " color: blue; "}}})
	value, err := evaluatePageFixture(s.Page, `savedRule===document.styleSheets[0].cssRules[0]&&savedStyle===savedRule.style&&savedStyle.color==='blue'`)
	if err != nil || value != true {
		t.Fatalf("CSSOM identity changed: %v %v", value, err)
	}
	b.event(t, "CSS.styleSheetChanged")
	b.call(t, bid, "CSS.trackComputedStyleUpdatesForNode", map[string]any{"nodeId": node.ID})
	_, err = evaluatePageFixture(s.Page, `document.querySelector('#probe').style.color='green'`)
	if err != nil {
		t.Fatal(err)
	}
	updated := b.event(t, "CSS.computedStyleUpdated")
	if updated["nodeId"] != float64(node.ID) {
		t.Fatalf("tracked style node: %v", updated)
	}
}

func TestInspectorComputedPropertyTracking(t *testing.T) {
	s, addr := runningServer(t)
	w, id := inspectorSession(t, s, addr)
	w.call(t, id, "Runtime.evaluate", map[string]any{"expression": `document.body.innerHTML='<div id="probe" style="color:red">tracked</div>'`})
	root := w.call(t, id, "DOM.getDocument", map[string]any{"depth": -1})["root"]
	node := findInspectorNode(root, "id", "probe")["nodeId"]
	w.call(t, id, "CSS.enable", map[string]any{})
	w.call(t, id, "CSS.trackComputedStyleUpdates", map[string]any{"propertiesToTrack": []any{map[string]any{"name": "color", "value": "rgb(255, 0, 0)"}}})
	w.call(t, id, "Runtime.evaluate", map[string]any{"expression": `document.querySelector('#probe').style.color='blue'`})
	updates := w.call(t, id, "CSS.takeComputedStyleUpdates", map[string]any{})["nodeIds"].([]any)
	found := false
	for _, changed := range updates {
		found = found || changed == node
	}
	if !found {
		t.Fatalf("changed tracked property missing: %v", updates)
	}
	if next := w.call(t, id, "CSS.takeComputedStyleUpdates", map[string]any{})["nodeIds"].([]any); len(next) != 0 {
		t.Fatalf("updates were not consumed: %v", next)
	}
	w.call(t, id, "CSS.trackComputedStyleUpdates", map[string]any{"propertiesToTrack": []any{}})
	w.call(t, id, "Runtime.evaluate", map[string]any{"expression": `document.querySelector('#probe').style.color='red'`})
	if next := w.call(t, id, "CSS.takeComputedStyleUpdates", map[string]any{})["nodeIds"].([]any); len(next) != 0 {
		t.Fatalf("cleared watch retained: %v", next)
	}
	w.call(t, id, "CSS.disable", map[string]any{})
	if reply := w.request(t, id, "CSS.takeComputedStyleUpdates", map[string]any{}); reply["error"] == nil {
		t.Fatal("disabled CSS tracking accepted")
	}
}

func TestInspectorDirectConnectionTargetClose(t *testing.T) {
	_, addr := runningServer(t)
	browser := newInspectorWire(t, addr)
	target := browser.call(t, "", "Target.createTarget", map[string]any{"url": "about:blank"})["targetId"].(string)
	conn, _, err := websocket.DefaultDialer.Dial("ws://"+addr+"/devtools/page/"+target, nil)
	if err != nil {
		t.Fatal(err)
	}
	direct := inspectorWireConnection(t, conn)
	direct.call(t, "", "DOM.getDocument", map[string]any{"depth": -1})
	direct.call(t, "", "CSS.enable", map[string]any{})
	direct.call(t, "", "Runtime.evaluate", map[string]any{"expression": "({retained:true})", "objectGroup": "console"})
	direct.call(t, "", "Page.enable", map[string]any{})
	browser.call(t, "", "Target.closeTarget", map[string]any{"targetId": target})
	select {
	case <-direct.done:
	case <-time.After(5 * time.Second):
		t.Fatal("direct frontend remained connected")
	}
}
