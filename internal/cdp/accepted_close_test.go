package cdp

import (
	"bytes"
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/moreveal/mimic/internal/trace"
)

func TestBrowserOwnedCloseOnlyRetainsTerminalCommands(t *testing.T) {
	wrap := func(inner message) message {
		raw, _ := json.Marshal(inner)
		params, _ := json.Marshal(map[string]any{"sessionId": "child", "message": string(raw)})
		return message{Method: "Target.sendMessageToTarget", Params: params}
	}
	for _, tc := range []struct {
		name string
		m    message
		want bool
	}{
		{"close", message{Method: "Target.closeTarget"}, true},
		{"nested close", wrap(wrap(message{Method: "Page.close"})), true},
		{"evaluate", wrap(message{Method: "Runtime.evaluate"}), false},
		{"create", message{Method: "Target.createTarget"}, false},
		{"detach", message{Method: "Target.detachFromTarget"}, false},
		{"empty inner", wrap(message{}), false},
		{"missing message", message{Method: "Target.sendMessageToTarget", Params: json.RawMessage(`{}`)}, false},
		{"invalid JSON", message{Method: "Target.sendMessageToTarget", Params: json.RawMessage(`{"message":"{"}`)}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := bytes.Clone(tc.m.Params)
			if got := browserOwnedClose(tc.m); got != tc.want {
				t.Fatalf("ownership = %v, want %v", got, tc.want)
			}
			if !bytes.Equal(tc.m.Params, before) {
				t.Fatal("ownership inspection mutated command parameters")
			}
		})
	}
}

func TestTransportEOFStillCancelsRunningEvaluation(t *testing.T) {
	s, addr := runningServer(t)
	wire := browserConnection(t, addr)
	wireCall(t, wire, 1, "Runtime.enable", nil)
	transport := s.clientSnapshot()[0]
	entered := make(chan struct{})
	var once sync.Once
	unsubscribe := s.Page.Trace().SubscribeKinds([]trace.Kind{trace.Console}, func(event trace.Event) {
		once.Do(func() { close(entered) })
	})
	defer unsubscribe()
	if err := wire.WriteJSON(map[string]any{"id": 2, "method": "Runtime.evaluate", "params": map[string]any{"expression": `console.log("evaluation entered"); while (true) {}`}}); err != nil {
		t.Fatal(err)
	}
	waitCloseBoundary(t, entered, "running evaluation")
	_ = wire.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	waitCloseBoundary(t, transport.ctx.Done(), "transport EOF")
	finished := make(chan struct{})
	go func() { transport.work.Wait(); close(finished) }()
	waitCloseBoundary(t, finished, "evaluation cancellation")
	value, err := evaluatePageFixture(s.Page, "6 * 7")
	if err != nil || value != float64(42) {
		t.Fatalf("Page did not remain usable after cancellation: %v, %v", value, err)
	}
}

// Pause a dispatched worker at its first cancellation check, after admission
// but before execution. EOF can then win deterministically instead of relying
// on the scheduler to reproduce a client sending close and disconnecting.
type admittedCommandContext struct {
	context.Context
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (c *admittedCommandContext) Err() error {
	c.once.Do(func() {
		close(c.entered)
		<-c.release
	})
	return c.Context.Err()
}

func waitCloseBoundary(t *testing.T, done <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func TestAcceptedCloseSurvivesTransportEOF(t *testing.T) {
	for _, method := range []string{"Target.closeTarget", "Page.close", "Target.disposeBrowserContext", "Browser.close", "legacy Target.closeTarget", "legacy Page.close", "Runtime.evaluate", "Target.createTarget", "invalid Target.closeTarget"} {
		t.Run(method, func(t *testing.T) {
			s, addr := runningServer(t)
			wire := browserConnection(t, addr)
			contextID := wireCall(t, wire, 1, "Target.createBrowserContext", nil)["browserContextId"].(string)
			targetID := wireCall(t, wire, 2, "Target.createTarget", map[string]any{"url": "about:blank", "browserContextId": contextID})["targetId"].(string)
			page, _ := s.page(targetID)
			transport := s.clientSnapshot()[0]
			gate := &admittedCommandContext{Context: transport.ctx, entered: make(chan struct{}), release: make(chan struct{})}
			// This session is not subscribed to Page events: only the admitted
			// worker touches its gate, while the real transport performs EOF.
			ss := &session{server: s, transport: transport, conn: transport.conn, ctx: gate, page: page, domains: map[string]bool{}}
			m := message{ID: 10, Method: method}
			params := map[string]any{"targetId": targetID}
			wantClosed := true
			switch method {
			case "Target.disposeBrowserContext":
				params = map[string]any{"browserContextId": contextID}
			case "Page.close", "Browser.close":
				params = nil
			case "Runtime.evaluate":
				params = map[string]any{"expression": "globalThis.executedAfterEOF = true"}
				wantClosed = false
			case "Target.createTarget":
				params = map[string]any{"url": "about:blank", "browserContextId": contextID}
				wantClosed = false
			case "invalid Target.closeTarget":
				m.Method = "Target.closeTarget"
				params = map[string]any{"targetId": 42}
				wantClosed = false
			case "legacy Target.closeTarget", "legacy Page.close":
				child := transport.newSession(page, "accepted-close-child", ss, false, false)
				innerMethod := method[len("legacy "):]
				inner, _ := json.Marshal(map[string]any{"id": 11, "method": innerMethod, "params": params})
				m.Method = "Target.sendMessageToTarget"
				params = map[string]any{"sessionId": child.id, "message": string(inner)}
			}
			m.Params, _ = json.Marshal(params)
			transport.dispatch(ss, m)
			waitCloseBoundary(t, gate.entered, "admitted worker")
			_ = wire.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			waitCloseBoundary(t, transport.ctx.Done(), "transport EOF")
			close(gate.release)
			finished := make(chan struct{})
			go func() { transport.work.Wait(); close(finished) }()
			waitCloseBoundary(t, finished, "accepted command completion")
			if method == "Browser.close" {
				deadline := time.Now().Add(5 * time.Second)
				for {
					s.lifecycleMu.Lock()
					closed := s.closed
					s.lifecycleMu.Unlock()
					if closed || time.Now().After(deadline) {
						if !closed {
							t.Fatal("accepted Browser.close was discarded")
						}
						break
					}
					time.Sleep(time.Millisecond)
				}
				return
			}
			_, remains := s.page(targetID)
			if remains == wantClosed {
				t.Fatalf("target remains = %v, expected closed = %v", remains, wantClosed)
			}
			if method == "Target.disposeBrowserContext" {
				if _, remains := s.Browser.Context(contextID); remains {
					t.Fatal("disposed context remains")
				}
			}
			if !wantClosed {
				ctx, _ := s.Browser.Context(contextID)
				if len(ctx.Pages()) != 1 {
					t.Fatal("ordinary command created a Page after EOF")
				}
				value, err := evaluatePageFixture(page, "typeof executedAfterEOF")
				if err != nil || value != "undefined" {
					t.Fatalf("ordinary evaluation ran after EOF: %v, %v", value, err)
				}
			}
			if _, remains := s.page(s.Page.ID); !remains {
				t.Fatal("unrelated Page was closed")
			}
		})
	}
}
