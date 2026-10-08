package cdp

import "testing"

func TestDOMQueriesPublishAncestorPathBeforeReply(t *testing.T) {
	s, address := runningServer(t)
	if _, err := evaluatePageFixture(s.Page, `document.body.innerHTML='<main><section><h1 id="first">One</h1><h1 id="second">Two</h1></section></main>'`); err != nil {
		t.Fatal(err)
	}
	document, _ := s.Page.Document()
	first, _ := document.Find("#first")
	second, _ := document.Find("#second")
	c := browserConnection(t, address)
	id := wireCall(t, c, 1, "Target.attachToTarget", map[string]any{"targetId": s.Page.ID, "flatten": true})["sessionId"].(string)
	root := flatCall(t, c, id, 2, "DOM.getDocument", map[string]any{"depth": 1})["root"].(map[string]any)
	for index, method := range []string{"DOM.querySelector", "DOM.querySelectorAll"} {
		commandID := float64(index + 3)
		if err := c.WriteJSON(map[string]any{"id": commandID, "sessionId": id, "method": method, "params": map[string]any{"nodeId": root["nodeId"], "selector": "h1"}}); err != nil {
			t.Fatal(err)
		}
		bound := map[float64]bool{root["nodeId"].(float64): true}
		for {
			var message map[string]any
			if err := c.ReadJSON(&message); err != nil {
				t.Fatal(err)
			}
			if message["method"] == "DOM.setChildNodes" {
				params := message["params"].(map[string]any)
				if !bound[params["parentId"].(float64)] {
					t.Fatalf("child arrived before its parent: %#v", params)
				}
				for _, item := range params["nodes"].([]any) {
					bound[item.(map[string]any)["nodeId"].(float64)] = true
				}
			}
			if message["id"] == commandID {
				if message["error"] != nil {
					t.Fatal(message)
				}
				if !bound[float64(first.ID)] || (index == 1 && !bound[float64(second.ID)]) {
					t.Fatalf("selector replied before node binding: %#v", message)
				}
				break
			}
		}
	}
}
