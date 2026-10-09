package cdp

import (
	"os"
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
	s.DevToolsChrome = "missing-inspector-renderer"
	w, id := inspectorSession(t, s, addr)
	for _, domain := range []string{"DOM", "CSS", "Runtime", "Network", "Page", "Overlay"} {
		w.call(t, id, domain+".enable", map[string]any{})
	}
	w.call(t, id, "DOM.getDocument", map[string]any{"depth": -1})
	s.Page.LockCommands()
	startedRenderer := false
	for _, c := range s.clientSnapshot() {
		for _, session := range c.snapshot() {
			if session.id == id && session.castState() != nil {
				startedRenderer = true
			}
		}
	}
	s.Page.UnlockCommands()
	if startedRenderer {
		t.Fatal("domain enable launched presentation")
	}
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

func TestInspectorBlinkBackpressureAndDisconnect(t *testing.T) {
	executable, err := rendererExecutable("")
	if err != nil {
		t.Skip(err)
	}
	s, addr := runningServer(t)
	s.DevToolsChrome = executable
	_, err = evaluatePageFixture(s.Page, `document.body.innerHTML='<div id="probe">initial</div>'`)
	if err != nil {
		t.Fatal(err)
	}
	w, id := inspectorSession(t, s, addr)
	w.call(t, id, "Page.enable", map[string]any{})
	w.call(t, id, "Page.startScreencast", map[string]any{"format": "png"})
	first := w.event(t, "Page.screencastFrame")
	started := time.Now()
	for index := 0; index < 5; index++ {
		w.call(t, id, "Runtime.evaluate", map[string]any{"expression": "document.querySelector('#probe').textContent+=' edited'"})
	}
	if time.Since(started) > 3*time.Second {
		t.Fatal("unacknowledged frame blocked Page commands")
	}
	w.call(t, id, "Page.screencastFrameAck", map[string]any{"sessionId": 0})
	deadline := time.NewTimer(150 * time.Millisecond)
loop:
	for {
		select {
		case event := <-w.events:
			if event["method"] == "Page.screencastFrame" {
				t.Fatal("frame bypassed ACK backpressure")
			}
		case <-deadline.C:
			break loop
		}
	}
	w.call(t, id, "Page.screencastFrameAck", map[string]any{"sessionId": first["sessionId"]})
	second := w.event(t, "Page.screencastFrame")
	if second["data"] == first["data"] {
		t.Fatal("coalesced update lost")
	}
	var cast *screencast
	s.Page.LockCommands()
	for _, c := range s.clientSnapshot() {
		for _, session := range c.snapshot() {
			if session.id == id {
				cast = session.castState()
			}
		}
	}
	s.Page.UnlockCommands()
	if cast == nil {
		t.Fatal("missing renderer ownership")
	}
	_ = w.c.Close()
	select {
	case <-cast.done:
	case <-time.After(8 * time.Second):
		t.Fatal("disconnect did not stop renderer")
	}
	if _, err := os.Stat(cast.renderer.profile); !os.IsNotExist(err) {
		t.Fatalf("renderer profile retained after disconnect: %v", err)
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

func TestInspectorBlinkRestartAndTargetClose(t *testing.T) {
	executable, err := rendererExecutable("")
	if err != nil {
		t.Skip(err)
	}
	s, addr := runningServer(t)
	s.DevToolsChrome = executable
	w := newInspectorWire(t, addr)
	target := w.call(t, "", "Target.createTarget", map[string]any{"url": "about:blank"})["targetId"].(string)
	id := w.call(t, "", "Target.attachToTarget", map[string]any{"targetId": target, "flatten": true})["sessionId"].(string)
	w.call(t, id, "Log.enable", map[string]any{})
	w.call(t, id, "Page.enable", map[string]any{})
	w.call(t, id, "Page.startScreencast", map[string]any{"format": "png"})
	w.event(t, "Page.screencastFrame")
	ownedCast := func() *screencast {
		for _, client := range s.clientSnapshot() {
			for _, ss := range client.snapshot() {
				if ss.id == id {
					return ss.castState()
				}
			}
		}
		return nil
	}
	first := ownedCast()
	if first == nil {
		t.Fatal("missing first view")
	}
	w.call(t, id, "Page.startScreencast", map[string]any{"format": "png"})
	w.event(t, "Page.screencastFrame")
	second := ownedCast()
	if second == nil || second == first {
		t.Fatal("view did not restart")
	}
	select {
	case <-first.done:
	default:
		t.Fatal("restart did not join old view")
	}
	if _, err := os.Stat(first.renderer.profile); !os.IsNotExist(err) {
		t.Fatalf("restart retained old profile: %v", err)
	}
	w.call(t, "", "Target.closeTarget", map[string]any{"targetId": target})
	select {
	case <-second.done:
	case <-time.After(8 * time.Second):
		t.Fatal("target close retained view")
	}
	if _, err := os.Stat(second.renderer.profile); !os.IsNotExist(err) {
		t.Fatalf("target close retained profile: %v", err)
	}
	// Independent default Page remains usable after another target teardown.
	alive := w.call(t, "", "Target.attachToTarget", map[string]any{"targetId": s.Page.ID, "flatten": true})["sessionId"].(string)
	result := w.call(t, alive, "Runtime.evaluate", map[string]any{"expression": "21*2", "returnByValue": true})["result"].(map[string]any)
	if result["value"] != float64(42) {
		t.Fatalf("unrelated Page was affected: %v", result)
	}
}

func TestInspectorDirectConnectionTargetClose(t *testing.T) {
	executable, err := rendererExecutable("")
	if err != nil {
		t.Skip(err)
	}
	s, addr := runningServer(t)
	s.DevToolsChrome = executable
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
	direct.call(t, "", "Page.startScreencast", map[string]any{"format": "png"})
	direct.event(t, "Page.screencastFrame")
	var cast *screencast
	for _, client := range s.clientSnapshot() {
		if client.root.targetID == target && !client.root.browserSession {
			cast = client.root.castState()
		}
	}
	if cast == nil {
		t.Fatal("missing direct view")
	}
	browser.call(t, "", "Target.closeTarget", map[string]any{"targetId": target})
	select {
	case <-cast.done:
	default:
		t.Fatal("target reply preceded renderer teardown")
	}
	select {
	case <-direct.done:
	case <-time.After(5 * time.Second):
		t.Fatal("direct frontend remained connected")
	}
	if _, err := os.Stat(cast.renderer.profile); !os.IsNotExist(err) {
		t.Fatalf("direct target retained profile: %v", err)
	}
}
