package cdp

import (
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestCommandTimingSeparatesPageWait(t *testing.T) {
	t.Setenv("MIMIC_PROFILE_CDP", "1")
	s, addr := runningServer(t)
	s.Page.Trace().Start()
	c, _, err := websocket.DefaultDialer.Dial("ws://"+addr+"/devtools/page/"+s.Page.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	// Complete attachment before holding the Page; newSession owns that lock.
	_ = c.WriteJSON(map[string]any{"id": 1, "method": "Runtime.evaluate", "params": map[string]any{"expression": "1"}})
	readReply(t, c, 1)
	s.Page.LockCommands()
	_ = c.WriteJSON(map[string]any{"id": 2, "method": "Runtime.evaluate", "params": map[string]any{"expression": "6*7"}})
	time.Sleep(40 * time.Millisecond)
	s.Page.UnlockCommands()
	if reply := readReply(t, c, 2); reply["error"] != nil {
		t.Fatal(reply)
	}
	deadline := time.Now().Add(time.Second)
	for {
		for _, event := range s.Page.Trace().Events() {
			if event.Name != "commandTiming" || event.Data["commandId"] != int64(2) {
				continue
			}
			if event.Data["pageWaitMs"].(float64) < 10 {
				t.Fatalf("missing page wait: %v", event.Data)
			}
			if event.Data["totalMs"].(float64) < event.Data["pageWaitMs"].(float64) {
				t.Fatal(event.Data)
			}
			for _, name := range []string{"queueMs", "sessionWaitMs", "workMs", "serializeMs", "writeWaitMs", "writeMs"} {
				if value, ok := event.Data[name].(float64); !ok || value < 0 {
					t.Fatalf("%s: %v", name, event.Data)
				}
			}
			if _, ok := event.Data["expression"]; ok {
				t.Fatal("diagnostics retained command parameters")
			}
			phases := map[string]bool{}
			for _, detail := range s.Page.Trace().Events() {
				if detail.Data["callID"] != "evaluate::2" {
					continue
				}
				if detail.Name == "evaluatePhase" {
					phases[detail.Data["phase"].(string)] = true
				}
				if detail.Name == "runtime.Eval.entry" || detail.Name == "runtime.Eval.execution" {
					phases[detail.Name] = true
				}
				if detail.Data["expression"] != nil {
					t.Fatal("diagnostics retained expression")
				}
			}
			for _, phase := range []string{"state", "shadowSnapshots", "inline", "execution", "finish", "remoteObject", "runtime.Eval.entry", "runtime.Eval.execution"} {
				if !phases[phase] {
					t.Fatalf("missing %s: %v", phase, phases)
				}
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("no command timing")
		}
		time.Sleep(time.Millisecond)
	}
}
