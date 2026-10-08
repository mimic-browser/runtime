package cdp

import "testing"

func TestDetachNotifiesNestedSessionsBeforeParent(t *testing.T) {
	s, addr := runningServer(t)
	c := browserConnection(t, addr)
	tabID := s.tabID(s.Page)
	parentID := wireCall(t, c, 1, "Target.attachToTarget", map[string]any{"targetId": tabID, "flatten": true})["sessionId"].(string)
	childID := flatCall(t, c, parentID, 2, "Target.attachToTarget", map[string]any{"targetId": s.Page.ID, "flatten": true})["sessionId"].(string)
	if err := c.WriteJSON(map[string]any{"id": 3, "method": "Target.detachFromTarget", "params": map[string]any{"sessionId": parentID}}); err != nil {
		t.Fatal(err)
	}
	var detached []map[string]any
	for {
		var message map[string]any
		if err := c.ReadJSON(&message); err != nil {
			t.Fatal(err)
		}
		if message["method"] == "Target.detachedFromTarget" {
			detached = append(detached, message)
		}
		if message["id"] == float64(3) {
			break
		}
	}
	if len(detached) != 2 {
		t.Fatalf("nested detach notifications = %v, want child then parent", detached)
	}
	if detached[0]["sessionId"] != parentID || detached[0]["params"].(map[string]any)["sessionId"] != childID {
		t.Fatalf("first detach must reach the child's still-live parent: %v", detached[0])
	}
	if detached[1]["sessionId"] != nil || detached[1]["params"].(map[string]any)["sessionId"] != parentID {
		t.Fatalf("last detach must close the parent: %v", detached[1])
	}
}
