package cdp

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/moreveal/mimic/internal/scheduler"
	"github.com/moreveal/mimic/internal/trace"
)

func busySnapshotPage(t *testing.T) (*Server, *websocket.Conn) {
	t.Helper()
	s, addr := runningServer(t)
	c, _, err := websocket.DefaultDialer.Dial("ws://"+addr+"/devtools/page/"+s.Page.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	if err := c.WriteJSON(map[string]any{"id": 1, "method": "Page.enable"}); err != nil {
		t.Fatal(err)
	}
	readReply(t, c, 1)
	entered := make(chan struct{}, 1)
	unsubscribe := s.Page.Trace().SubscribeKinds([]trace.Kind{trace.Console}, func(event trace.Event) {
		// The fixture emits its only console event after the first DOM mutation.
		// A running timer's wall time also includes scheduling and compilation.
		if event.Kind == trace.Console {
			select {
			case entered <- struct{}{}:
			default:
			}
		}
	})
	defer unsubscribe()
	s.Page.LockCommands()
	_, err = s.Page.Top.Realm.Evaluate(context.Background(), `setTimeout(()=>{document.body.textContent='committed first turn';console.log('first turn entered');while(true){}},0);setTimeout(()=>{document.body.textContent='second turn';while(true){}},0)`, "snapshot-boundary-fixture")
	s.Page.UnlockCommands()
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
		status := s.Page.ExecutionStatus()
		if !status.Running || status.Source != scheduler.Timer {
			t.Fatalf("first timer is not running: %+v", status)
		}
		return s, c
	case <-time.After(3 * time.Second):
		t.Fatal("timer did not reach its first DOM mutation")
	}
	return nil, nil
}

func TestSnapshotInterruptReservesTaskBoundary(t *testing.T) {
	s, c := busySnapshotPage(t)
	defer s.stopPump(s.Page)
	c.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err := c.WriteJSON(map[string]any{"id": 2, "method": "Mimic.captureSnapshot", "params": map[string]any{"interrupt": true}}); err != nil {
		t.Fatal(err)
	}
	reply := readReply(t, c, 2)
	if reply["error"] != nil {
		t.Fatal(reply)
	}
	files := reply["result"].(map[string]any)["files"].(map[string]any)
	html, err := base64.StdEncoding.DecodeString(files["index.html"].(string))
	if err != nil || !strings.Contains(string(html), "committed first turn") || strings.Contains(string(html), "second turn") {
		t.Fatalf("wrong task boundary: %s, %v", html, err)
	}
}

func TestCloseTargetCancelsBusyPumpBeforeLock(t *testing.T) {
	s, c := busySnapshotPage(t)
	c.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err := c.WriteJSON(map[string]any{"id": 2, "method": "Target.closeTarget", "params": map[string]any{"targetId": s.Page.ID}}); err != nil {
		t.Fatal(err)
	}
	reply := readReply(t, c, 2)
	if reply["error"] != nil || reply["result"].(map[string]any)["success"] != true {
		t.Fatal(reply)
	}
}

func TestSynchronousEvaluationDoesNotDrainTimers(t *testing.T) {
	s, addr := runningServer(t)
	c, _, err := websocket.DefaultDialer.Dial("ws://"+addr+"/devtools/page/"+s.Page.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	defer s.stopPump(s.Page)
	c.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err := c.WriteJSON(map[string]any{"id": 1, "method": "Runtime.evaluate", "params": map[string]any{"expression": `setTimeout(()=>{while(true){}},0);'immediate'`, "returnByValue": true}}); err != nil {
		t.Fatal(err)
	}
	reply := readReply(t, c, 1)
	if reply["error"] != nil {
		t.Fatal(reply)
	}
	result := reply["result"].(map[string]any)["result"].(map[string]any)
	if result["value"] != "immediate" {
		t.Fatal(reply)
	}
}

func TestCloseTargetCancelsUnboundedParserScript(t *testing.T) {
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<body>committed parser DOM<script>while(true){}</script>`)
	}))
	defer fixture.Close()
	s, addr := runningServer(t)
	s.SetNavigationTimeout(0)
	c, _, err := websocket.DefaultDialer.Dial("ws://"+addr+"/devtools/page/"+s.Page.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	defer s.stopPump(s.Page)
	c.SetReadDeadline(time.Now().Add(5 * time.Second))
	if err := c.WriteJSON(map[string]any{"id": 1, "method": "Page.navigate", "params": map[string]any{"url": fixture.URL}}); err != nil {
		t.Fatal(err)
	}
	if reply := readReply(t, c, 1); reply["error"] != nil {
		t.Fatal(reply)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		status := s.Page.ExecutionStatus()
		if status.Running && status.Elapsed > 10*time.Millisecond {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("parser task did not start")
		}
		time.Sleep(time.Millisecond)
	}
	if err := c.WriteJSON(map[string]any{"id": 2, "method": "Target.closeTarget", "params": map[string]any{"targetId": s.Page.ID}}); err != nil {
		t.Fatal(err)
	}
	if reply := readReply(t, c, 2); reply["error"] != nil || reply["result"].(map[string]any)["success"] != true {
		t.Fatal(reply)
	}
}
