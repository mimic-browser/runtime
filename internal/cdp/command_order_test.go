package cdp

import (
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// Pipelined page initialization must establish its frame tree before Runtime
// announces execution contexts. Real clients discard unknown-frame contexts.
func TestPipelinedPageCommandsPreserveFrameContextOrder(t *testing.T) {
	s, addr := runningServer(t)
	c, _, err := websocket.DefaultDialer.Dial("ws://"+addr+"/devtools/page/"+s.Page.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetReadDeadline(time.Now().Add(8 * time.Second))
	wireCall(t, c, 1, "Page.enable", nil)

	// Queue a burst behind the Page boundary, making worker mutex scheduling
	// observable instead of depending on network timing to create contention.
	s.Page.LockCommands()
	const rounds = 32
	for i := 0; i < rounds; i++ {
		for offset, method := range []string{"Page.getFrameTree", "Runtime.enable", "Runtime.disable"} {
			if err := c.WriteJSON(map[string]any{"id": 2 + i*3 + offset, "method": method}); err != nil {
				s.Page.UnlockCommands()
				t.Fatal(err)
			}
		}
	}
	s.Page.UnlockCommands()
	contexts := 0
	for nextReply := 2; nextReply < 2+rounds*3; {
		var reply map[string]any
		if err := c.ReadJSON(&reply); err != nil {
			t.Fatal(err)
		}
		if reply["method"] == "Runtime.executionContextCreated" {
			if (nextReply-2)%3 != 1 {
				t.Fatalf("execution context arrived before its frame tree: next reply %d, event %v", nextReply, reply)
			}
			contexts++
		}
		if id, ok := reply["id"].(float64); ok {
			if reply["error"] != nil || int(id) != nextReply {
				t.Fatalf("page command order: want reply %d, got %v", nextReply, reply)
			}
			nextReply++
		}
	}
	if contexts != rounds {
		t.Fatalf("execution contexts = %d, want %d", contexts, rounds)
	}
}

func TestQueuedPageCommandsDoNotBlockIndependentSessions(t *testing.T) {
	s, addr := runningServer(t)
	c := browserConnection(t, addr)
	target := wireCall(t, c, 1, "Target.createTarget", map[string]any{"url": "about:blank"})["targetId"].(string)
	a := wireCall(t, c, 2, "Target.attachToTarget", map[string]any{"targetId": s.Page.ID, "flatten": true})["sessionId"].(string)
	b := wireCall(t, c, 3, "Target.attachToTarget", map[string]any{"targetId": target, "flatten": true})["sessionId"].(string)
	s.Page.LockCommands()
	defer s.Page.UnlockCommands()
	_ = c.WriteJSON(map[string]any{"id": 4, "sessionId": a, "method": "Page.getFrameTree"})
	_ = c.WriteJSON(map[string]any{"id": 5, "sessionId": a, "method": "Runtime.enable"})
	result := flatCall(t, c, b, 6, "Runtime.evaluate", map[string]any{"expression": "6*7"})
	if result["result"].(map[string]any)["value"] != float64(42) {
		t.Fatal(result)
	}
}
