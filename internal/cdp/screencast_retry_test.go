package cdp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestInspectorScreencastRetriesTimedOutCapture(t *testing.T) {
	var captures atomic.Int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, req, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			var command map[string]any
			if conn.ReadJSON(&command) != nil {
				return
			}
			result := map[string]any{}
			switch command["method"] {
			case "Page.getLayoutMetrics":
				result["cssVisualViewport"] = map[string]any{"pageX": 0, "pageY": 0}
			case "Page.captureScreenshot":
				if captures.Add(1) == 1 {
					// Lose one renderer response, without changing Mimic's DOM.
					continue
				}
				result["data"] = "AQ=="
			}
			if conn.WriteJSON(map[string]any{"id": command["id"], "result": result}) != nil {
				return
			}
		}
	}))
	t.Cleanup(backend.Close)
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(backend.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	renderer := &blinkRenderer{conn: conn, profile: t.TempDir(), pending: make(map[int64]chan map[string]any), done: make(chan struct{}), presentationInitialized: true, frameID: "fixture"}
	go renderer.read()
	s, addr := runningServer(t)
	w, id := inspectorSession(t, s, addr)
	w.call(t, id, "Page.enable", map[string]any{})
	w.call(t, id, "Log.enable", map[string]any{})
	w.call(t, id, "Runtime.evaluate", map[string]any{"expression": "document.body.innerHTML='<div>capture fixture</div>'"})
	// Isolate the lost backend response from cold runtime/style construction.
	s.Page.LockCommands()
	_, warmErr := s.Page.CaptureInspectorSnapshot(context.Background())
	s.Page.UnlockCommands()
	if warmErr != nil {
		t.Fatal(warmErr)
	}
	var session *session
	for _, client := range s.clientSnapshot() {
		for _, candidate := range client.snapshot() {
			if candidate.id == id {
				session = candidate
			}
		}
	}
	if session == nil {
		t.Fatal("missing inspector session")
	}
	ctx, cancel := context.WithCancel(session.ctx)
	cast := &screencast{cancel: cancel, done: make(chan struct{}), wake: make(chan struct{}, 1), ack: make(chan int, 1), renderer: renderer, removeTurn: func() {}}
	session.stateMu.Lock()
	session.screencast = cast
	session.stateMu.Unlock()
	go session.runScreencast(ctx, cast, "png", 100, map[string]any{}, 500*time.Millisecond)
	t.Cleanup(session.stopScreencast)
	cast.wake <- struct{}{}
	frame := w.event(t, "Page.screencastFrame")
	if frame["data"] != "AQ==" || captures.Load() != 2 {
		t.Fatalf("requested frame did not recover once: frame=%v captures=%d", frame, captures.Load())
	}
}
