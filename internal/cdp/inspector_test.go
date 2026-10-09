package cdp

import (
	"context"
	"strings"
	"testing"
)

func TestInspectorCSSLiveEditing(t *testing.T) {
	s, addr := runningServer(t)
	_, err := evaluatePageFixture(s.Page, `document.head.innerHTML='<style>#probe { color: red; }</style>'; document.body.innerHTML='<div id="probe" style="width: 12px">hello</div>'`)
	if err != nil {
		t.Fatal(err)
	}
	d, _ := s.Page.Document()
	node, _ := d.Find("#probe")
	c := browserConnection(t, addr)
	id := wireCall(t, c, 1, "Target.attachToTarget", map[string]any{"targetId": s.Page.ID, "flatten": true})["sessionId"].(string)
	flatCall(t, c, id, 2, "DOM.enable", map[string]any{})
	flatCall(t, c, id, 3, "CSS.enable", map[string]any{})
	matched := flatCall(t, c, id, 4, "CSS.getMatchedStylesForNode", map[string]any{"nodeId": node.ID})
	rules := matched["matchedCSSRules"].([]any)
	if len(rules) != 1 {
		t.Fatalf("matched: %#v", matched)
	}
	rule := rules[0].(map[string]any)["rule"].(map[string]any)
	style := rule["style"].(map[string]any)
	edits := []any{map[string]any{"styleSheetId": style["styleSheetId"], "range": style["range"], "text": " color: blue; "}}
	flatCall(t, c, id, 5, "CSS.setStyleTexts", map[string]any{"edits": edits})
	value, err := evaluatePageFixture(s.Page, `getComputedStyle(document.querySelector('#probe')).color`)
	if err != nil || value != "rgb(0, 0, 255)" {
		t.Fatalf("canonical CSS edit: %v %v", value, err)
	}
	inline := matched["inlineStyle"].(map[string]any)
	flatCall(t, c, id, 6, "CSS.setStyleTexts", map[string]any{"edits": []any{map[string]any{"styleSheetId": inline["styleSheetId"], "range": inline["range"], "text": "width: 24px;"}}})
	value, err = evaluatePageFixture(s.Page, `document.querySelector('#probe').style.width`)
	if err != nil || value != "24px" {
		t.Fatalf("inline edit: %v %v", value, err)
	}
	flatCall(t, c, id, 7, "CSS.disable", map[string]any{})
	s.Page.LockCommands()
	for _, client := range s.clientSnapshot() {
		for _, session := range client.snapshot() {
			if session.id == id && (session.styleSheets != nil || session.removeInspectorTurn != nil) {
				t.Error("disabled inspector retained state")
			}
		}
	}
	s.Page.UnlockCommands()
}

func TestInspectorDOMNotificationsAndCleanup(t *testing.T) {
	s, addr := runningServer(t)
	_, err := evaluatePageFixture(s.Page, `document.body.innerHTML='<div id="probe">hello</div>'`)
	if err != nil {
		t.Fatal(err)
	}
	c := browserConnection(t, addr)
	id := wireCall(t, c, 1, "Target.attachToTarget", map[string]any{"targetId": s.Page.ID, "flatten": true})["sessionId"].(string)
	flatCall(t, c, id, 2, "DOM.getDocument", map[string]any{"depth": -1})
	if err := c.WriteJSON(map[string]any{"id": 3, "sessionId": id, "method": "Runtime.evaluate", "params": map[string]any{"expression": `const e=document.querySelector('#probe');e.title='changed';e.firstChild.data='edited';e.appendChild(document.createElement('span'))`}}); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for len(seen) < 3 {
		var message map[string]any
		if err := c.ReadJSON(&message); err != nil {
			t.Fatal(err)
		}
		if method, _ := message["method"].(string); method == "DOM.attributeModified" || method == "DOM.characterDataModified" || method == "DOM.childNodeInserted" {
			seen[method] = true
		}
	}
	flatCall(t, c, id, 4, "DOM.disable", map[string]any{})
	s.Page.LockCommands()
	for _, client := range s.clientSnapshot() {
		for _, session := range client.snapshot() {
			if session.id == id && (session.domInspector != nil || session.removeInspectorTurn != nil) {
				t.Error("DOM disable retained frontend projection")
			}
		}
	}
	s.Page.UnlockCommands()
}

func TestInspectorSnapshotDoesNotExecuteOrAllocateCSSHandles(t *testing.T) {
	s, _ := runningServer(t)
	_, err := evaluatePageFixture(s.Page, `document.head.innerHTML='<style>div {color:red}</style>';document.body.innerHTML='<div id="probe" onclick="window.authorExecuted=true"></div><script>window.authorExecuted=true</script>'; document.styleSheets[0].cssRules[0].style.color='blue'`)
	if err != nil {
		t.Fatal(err)
	}
	s.Page.LockCommands()
	defer s.Page.UnlockCommands()
	snapshot, err := s.Page.CaptureInspectorSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	markup := string(snapshot.Files["index.html"])
	if !strings.Contains(markup, "blue") || !strings.Contains(markup, "data-mimic-preview-node") || strings.Contains(markup, "authorExecuted") {
		t.Fatalf("projection lost canonical styles or retained source execution: %s", markup)
	}
	status, err := s.Page.ProtocolInspectorCSS(context.Background(), s.Page.Top, map[string]any{"method": "status"})
	if err != nil || status["active"] != false {
		t.Fatalf("snapshot allocated CSS inspector: %v %v", status, err)
	}
}
