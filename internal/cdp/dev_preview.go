package cdp

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/moreveal/mimic/internal/trace"
)

//go:embed dev_preview.html
var previewHTML []byte

//go:embed dev_preview_dom.js
var previewDOM []byte

//go:embed dev_preview.js
var previewClient []byte

//go:embed dev_preview.css
var previewCSS []byte

func (s *Server) registerPreview(mux *http.ServeMux) {
	mux.HandleFunc("/debug/preview/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/debug/preview/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline' http: https:; img-src http: https: data:; font-src http: https: data:; media-src http: https: data:; connect-src 'self'; frame-src 'self' about:; object-src 'none'; form-action 'none'")
		body := bytes.Replace(previewHTML, []byte("/* preview_dom */"), previewDOM, 1)
		body = bytes.Replace(body, []byte("/* preview_client */"), previewClient, 1)
		body = bytes.Replace(body, []byte("/* preview_css */"), previewCSS, 1)
		_, _ = w.Write(body)
	})
	mux.HandleFunc("/debug/preview/ws", s.previewWS)
}

func (s *Server) previewWS(w http.ResponseWriter, r *http.Request) {
	page, ok := s.page(r.URL.Query().Get("target"))
	if !ok {
		http.Error(w, "unknown target", http.StatusNotFound)
		return
	}
	// Default upgrader enforces same-origin requests (unlike the CDP endpoint).
	u := websocket.Upgrader{}
	conn, err := u.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	s.lifecycleMu.Lock()
	if s.closed {
		s.lifecycleMu.Unlock()
		cancel()
		conn.Close()
		return
	}
	s.connections[conn] = cancel
	s.workers.Add(1)
	s.lifecycleMu.Unlock()
	defer s.workers.Done()
	defer func() {
		cancel()
		conn.Close()
		s.lifecycleMu.Lock()
		delete(s.connections, conn)
		s.lifecycleMu.Unlock()
	}()
	page.LockCommands()
	sub, err := page.SubscribePreview()
	// Activity has a separate bounded mailbox: snapshot coalescing must not
	// erase commands, and slow viewers must never stall the Page.
	activity := make(chan []byte, 256)
	var dropped atomic.Uint64
	unsubscribe := page.Trace().SubscribeKinds([]trace.Kind{trace.CDP, trace.Console, trace.Exception, trace.Lifecycle, trace.Network, trace.Error}, func(event trace.Event) {
		wire := previewActivity(event)
		if wire == nil {
			return
		}
		select {
		case activity <- wire:
		default:
			dropped.Add(1)
		}
	})
	page.UnlockCommands()
	defer unsubscribe()
	if err != nil {
		return
	}
	defer func() { page.LockCommands(); page.UnsubscribePreview(sub); page.UnlockCommands() }()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer cancel()
		conn.SetReadLimit(1024)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()
	defer func() { conn.Close(); <-done }()
	for {
		select {
		case wire := <-sub.Updates:
			conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if conn.WriteMessage(websocket.TextMessage, wire) != nil {
				return
			}
		case <-ctx.Done():
			return
		case wire := <-activity:
			conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if count := dropped.Swap(0); count > 0 {
				if conn.WriteJSON(map[string]any{"type": "activity", "dropped": count}) != nil {
					return
				}
			}
			if conn.WriteMessage(websocket.TextMessage, wire) != nil {
				return
			}
		}
	}
}

// Project immutable diagnostics without bodies, headers or engine-owned values.
// This does not enable API tracing or attach a CDP session/interceptor.
func previewActivity(event trace.Event) []byte {
	data := make(map[string]any)
	copyFields := func(source map[string]any, names ...string) {
		for _, name := range names {
			switch value := source[name].(type) {
			case string:
				data[name] = previewText(value)
			case bool, int, int64, uint64, float64, json.Number:
				data[name] = value
			}
		}
	}
	copyFields(event.Data, "method", "commandId", "sessionId", "url", "status", "error", "source", "frameId", "id", "mimeType")
	if event.Kind == trace.CDP {
		if event.Name != "method" && event.Name != "commandError" {
			return nil
		}
		var params map[string]any
		if raw, ok := event.Data["params"].(json.RawMessage); ok {
			_ = json.Unmarshal(raw, &params)
		}
		copyFields(params, "type", "x", "y", "button", "key", "text", "deltaX", "deltaY", "url", "width", "height", "expression", "functionDeclaration", "selector")
	}
	if event.Kind == trace.Console {
		// args is the already formatted console text, even when remoteValues
		// indicates a separate debugger delivery of original object handles.
		var text []string
		switch args := event.Data["args"].(type) {
		case []any:
			for _, arg := range args[:min(len(args), 20)] {
				if value, ok := arg.(string); ok {
					text = append(text, previewText(value))
				}
			}
		case []string:
			for _, value := range args[:min(len(args), 20)] {
				text = append(text, previewText(value))
			}
		}
		if args, err := json.Marshal(text); err == nil {
			data["args"] = previewText(string(args))
		}
	}
	wire, err := json.Marshal(map[string]any{"type": "activity", "event": trace.Event{Sequence: event.Sequence, Time: event.Time, Kind: event.Kind, Name: event.Name, Data: data}})
	if err != nil {
		return nil
	}
	return wire
}

func previewText(value string) string {
	const limit = 1600
	if len(value) <= limit {
		return value
	}
	return strings.ToValidUTF8(value[:limit], "") + "… [truncated]"
}
