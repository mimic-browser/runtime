package cdp

import (
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// A real frontend reads replies and events concurrently. Keep all events so
// regressions cannot pass by accidentally dropping frames between commands.
type inspectorWire struct {
	c       *websocket.Conn
	mu      sync.Mutex
	writeMu sync.Mutex
	seq     int
	replies map[int]chan map[string]any
	events  chan map[string]any
	done    chan struct{}
	readErr error
}

func newInspectorWire(t *testing.T, addr string) *inspectorWire {
	t.Helper()
	return inspectorWireConnection(t, browserConnection(t, addr))
}

func inspectorWireConnection(t *testing.T, conn *websocket.Conn) *inspectorWire {
	t.Helper()
	w := &inspectorWire{c: conn, replies: make(map[int]chan map[string]any), events: make(chan map[string]any, 2048), done: make(chan struct{})}
	_ = w.c.SetReadDeadline(time.Time{})
	go func() {
		defer close(w.done)
		for {
			var message map[string]any
			if err := w.c.ReadJSON(&message); err != nil {
				w.readErr = err
				return
			}
			if id, ok := message["id"].(float64); ok {
				w.mu.Lock()
				response := w.replies[int(id)]
				w.mu.Unlock()
				if response != nil {
					response <- message
				}
			} else {
				select {
				case w.events <- message:
				case <-time.After(5 * time.Second):
					return
				}
			}
		}
	}()
	t.Cleanup(func() { _ = w.c.Close(); <-w.done })
	return w
}

func (w *inspectorWire) call(t *testing.T, session, method string, params map[string]any) map[string]any {
	t.Helper()
	response := w.request(t, session, method, params)
	if response["error"] != nil {
		t.Fatalf("%s: %#v", method, response["error"])
	}
	return response["result"].(map[string]any)
}

func (w *inspectorWire) request(t *testing.T, session, method string, params map[string]any) map[string]any {
	t.Helper()
	w.mu.Lock()
	w.seq++
	id := w.seq
	reply := make(chan map[string]any, 1)
	w.replies[id] = reply
	w.mu.Unlock()
	defer func() { w.mu.Lock(); delete(w.replies, id); w.mu.Unlock() }()
	message := map[string]any{"id": id, "method": method, "params": params}
	if session != "" {
		message["sessionId"] = session
	}
	w.writeMu.Lock()
	err := w.c.WriteJSON(message)
	w.writeMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	select {
	case response := <-reply:
		return response
	case <-w.done:
		t.Fatalf("inspector disconnected: %v", w.readErr)
	case <-time.After(25 * time.Second):
		t.Fatalf("%s timed out", method)
	}
	return nil
}

func (w *inspectorWire) event(t *testing.T, method string) map[string]any {
	t.Helper()
	timeout := time.NewTimer(25 * time.Second)
	defer timeout.Stop()
	for {
		select {
		case message := <-w.events:
			if message["method"] == "Log.entryAdded" {
				t.Logf("inspector diagnostic: %v", message["params"])
			}
			if message["method"] == method {
				return message["params"].(map[string]any)
			}
		case <-w.done:
			t.Fatal("inspector disconnected")
		case <-timeout.C:
			t.Fatalf("waiting for %s", method)
		}
	}
}
