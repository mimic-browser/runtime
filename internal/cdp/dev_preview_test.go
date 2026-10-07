package cdp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	chrome152 "github.com/moreveal/mimic/chrome/152"
	"github.com/moreveal/mimic/internal/browser"
	v8engine "github.com/moreveal/mimic/internal/engine/v8"
	"github.com/moreveal/mimic/internal/trace"
)

func TestDevPreviewRoutesAndWebSocket(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		b, err := browser.NewWithOptions(v8engine.Factory{}, chrome152.New(), browser.Options{DevPreview: enabled})
		if err != nil {
			t.Fatal(err)
		}
		s, err := New(b)
		if err != nil {
			t.Fatal(err)
		}
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		go s.Serve(l)
		defer s.Close(context.Background())
		base := "http://" + l.Addr().String()
		resp, err := http.Get(base + "/debug/preview/")
		if err != nil {
			t.Fatal(err)
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			t.Fatal(readErr)
		}
		want := 200
		if !enabled {
			want = 404
		}
		if resp.StatusCode != want {
			t.Fatalf("route: %d", resp.StatusCode)
		}
		if !enabled {
			resp, err = http.Get(base + "/debug/preview/ws")
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != 404 {
				t.Fatal("disabled WS exposed")
			}
			continue
		}
		if !bytes.Contains(body, []byte("Disconnected — reconnecting…")) || !bytes.Contains(body, []byte("Math.min(reconnectDelay * 2, 5000)")) || !bytes.Contains(body, []byte("list(true)")) {
			t.Fatal("preview client does not automatically reconnect after a server restart")
		}
		conn, _, err := websocket.DefaultDialer.Dial("ws://"+l.Addr().String()+"/debug/preview/ws?target="+s.Page.ID, nil)
		if err != nil {
			t.Fatal(err)
		}
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		var packet map[string]any
		if err := conn.ReadJSON(&packet); err != nil {
			t.Fatal(err)
		}
		if packet["error"] != nil || packet["target"] != s.Page.ID || packet["html"] == nil {
			t.Fatalf("packet: %v", packet)
		}
		if s.Page.Trace().ObservationWanted() {
			t.Fatal("passive viewer enabled property/API tracing")
		}
		client, _, err := websocket.DefaultDialer.Dial("ws://"+l.Addr().String()+"/devtools/page/"+s.Page.ID, nil)
		if err != nil {
			t.Fatal(err)
		}
		client.SetReadDeadline(time.Now().Add(5 * time.Second))
		for id, command := range []map[string]any{
			{"method": "Runtime.evaluate", "params": map[string]any{"expression": "console.log('preview activity'); document.body.innerHTML='<p>Updated</p>'; 42"}},
			{"method": "Input.dispatchMouseEvent", "params": map[string]any{"type": "mouseMoved", "x": 31, "y": 47}},
			{"method": "Missing.command"},
		} {
			command["id"] = id + 1
			if err := client.WriteJSON(command); err != nil {
				t.Fatal(err)
			}
			var reply map[string]any
			if err := client.ReadJSON(&reply); err != nil {
				t.Fatal(err)
			}
		}
		client.Close()
		other, err := s.Context.NewPage()
		if err != nil {
			t.Fatal(err)
		}
		other.Trace().Add(trace.Console, "log", map[string]any{"args": []any{"other target"}})
		s.Page.Trace().Add(trace.Network, "response", map[string]any{"url": "https://example.test/asset", "status": 200, "headers": map[string]any{"secret": "omit"}, "body": "omit"})
		s.Page.Trace().Add(trace.Lifecycle, "previewTestEnd", nil)
		seen := map[string]bool{}
		for !seen["previewTestEnd"] {
			if err := conn.ReadJSON(&packet); err != nil {
				t.Fatal(err)
			}
			if packet["type"] != "activity" {
				continue
			}
			event := packet["event"].(map[string]any)
			name := event["name"].(string)
			data, _ := event["data"].(map[string]any)
			seen[name] = true
			if name == "method" && data["method"] == "Input.dispatchMouseEvent" {
				if data["x"] != float64(31) || data["y"] != float64(47) || data["type"] != "mouseMoved" {
					t.Fatalf("input activity: %v", data)
				}
				seen["input"] = true
			}
			if name == "log" && data["args"] != `["preview activity"]` {
				t.Fatalf("wrong target console activity: %v", data)
			}
			if name == "response" && (data["headers"] != nil || data["body"] != nil) {
				t.Fatalf("activity retained network payloads: %v", data)
			}
		}
		for _, name := range []string{"input", "log", "commandError", "response"} {
			if !seen[name] {
				t.Fatalf("missing %s activity: %v", name, seen)
			}
		}
		conn.Close()
	}
}

func TestDevPreviewActivityProjection(t *testing.T) {
	event := trace.Event{Kind: trace.CDP, Name: "method", Data: map[string]any{
		"method": "Runtime.evaluate",
		"params": json.RawMessage(`{"expression":"` + strings.Repeat("a", 10000) + `","arguments":["omit"]}`),
	}}
	wire := previewActivity(event)
	if len(wire) > 2200 || !bytes.Contains(wire, []byte("[truncated]")) || bytes.Contains(wire, []byte("arguments")) {
		t.Fatalf("unbounded projection (%d bytes)", len(wire))
	}
	console := previewActivity(trace.Event{Kind: trace.Console, Data: map[string]any{"remoteValues": true, "args": []any{"formatted text", make(chan int)}}})
	if !bytes.Contains(console, []byte("formatted text")) || bytes.Contains(console, []byte("remoteValues")) {
		t.Fatal("viewer did not project formatted console text independently of debugger values")
	}
	if previewActivity(trace.Event{Kind: trace.CDP, Name: "callFunctionOn.begin"}) != nil {
		t.Fatal("internal profiling spans reached viewer")
	}
}
