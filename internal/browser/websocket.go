package browser

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	ws "github.com/bogdanfinn/websocket"
	"github.com/moreveal/mimic/internal/engine"
	"github.com/moreveal/mimic/internal/network"
	"github.com/moreveal/mimic/internal/scheduler"
	"github.com/moreveal/mimic/internal/trace"
)

type realmWebSocket struct {
	mu     sync.Mutex
	conn   network.WebSocketConn
	cancel context.CancelFunc
	closed bool
}

func (r *Realm) websocketCallback(callback engine.Value, payload map[string]any) {
	if r.closed || r.inactive || r.resourceContext.Err() != nil {
		return
	}
	_, _ = r.runtime.Call(context.Background(), callback, nil, r.runtime.Value(payload))
}

func (r *Realm) hostOpenWebSocket(_ engine.Value, args []engine.Value) (engine.Value, error) {
	if len(args) < 3 {
		return nil, errors.New("invalid WebSocket open")
	}
	callback, id, rawURL := args[0], strarg(args, 1), strarg(args, 2)
	protocols := stringSlice(arg(args, 3))
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "ws" && u.Scheme != "wss") {
		return nil, errors.New("invalid WebSocket URL")
	}
	if !r.contentPolicy().AllowsConnection(r.documentURL(), u) {
		r.scheduler.Post(scheduler.Network, 0, func(context.Context) error {
			r.websocketCallback(callback, map[string]any{"type": "error"})
			return nil
		})
		return r.val(false), nil
	}
	ctx, cancel := context.WithCancel(r.resourceContext)
	state := &realmWebSocket{cancel: cancel}
	if r.webSockets == nil {
		r.webSockets = make(map[string]*realmWebSocket)
	}
	r.webSockets[id] = state
	headers := make(http.Header)
	headers.Set("Origin", r.origin)
	headers.Set("User-Agent", r.agent.Page().env.Navigator().UserAgent)
	if len(protocols) > 0 {
		headers.Set("Sec-WebSocket-Protocol", strings.Join(protocols, ", "))
	}
	r.resourceWG.Add(1)
	go func() {
		defer r.resourceWG.Done()
		conn, dialErr := r.agent.Page().loader.DialWebSocket(ctx, rawURL, headers)
		if dialErr != nil {
			r.agent.Page().trace.Add(trace.Network, "webSocketFailed", map[string]any{"url": rawURL, "error": dialErr.Error(), "context": r.agent.ContextID()})
			r.scheduler.Post(scheduler.Network, 0, func(context.Context) error {
				delete(r.webSockets, id)
				r.websocketCallback(callback, map[string]any{"type": "error"})
				r.websocketCallback(callback, map[string]any{"type": "close", "code": 1006, "reason": "", "wasClean": false})
				return nil
			})
			return
		}
		state.mu.Lock()
		if state.closed {
			state.mu.Unlock()
			_ = conn.Close()
			return
		}
		state.conn = conn
		state.mu.Unlock()
		r.scheduler.Post(scheduler.Network, 0, func(context.Context) error {
			r.websocketCallback(callback, map[string]any{"type": "open", "protocol": conn.Subprotocol()})
			return nil
		})
		for {
			messageType, data, readErr := conn.ReadMessage()
			if readErr != nil {
				code, reason, clean := 1006, "", false
				if closeErr, ok := readErr.(*ws.CloseError); ok {
					code, reason = closeErr.Code, closeErr.Text
					clean = code != 1006
				}
				r.scheduler.Post(scheduler.Network, 0, func(context.Context) error {
					delete(r.webSockets, id)
					state.mu.Lock()
					localClosed := state.closed
					state.closed = true
					state.mu.Unlock()
					if !clean && !localClosed {
						r.websocketCallback(callback, map[string]any{"type": "error"})
					}
					r.websocketCallback(callback, map[string]any{"type": "close", "code": code, "reason": reason, "wasClean": clean || localClosed})
					return nil
				})
				return
			}
			payload := map[string]any{"type": "message", "data": string(data), "binary": messageType == ws.BinaryMessage}
			if messageType == ws.BinaryMessage {
				payload["bytes"] = engine.BinaryBuffer(data)
			}
			r.scheduler.Post(scheduler.Network, 0, func(context.Context) error {
				r.websocketCallback(callback, payload)
				return nil
			})
		}
	}()
	return nil, nil
}

func (r *Realm) hostSendWebSocket(_ engine.Value, args []engine.Value) (engine.Value, error) {
	state := r.webSockets[strarg(args, 0)]
	if state == nil {
		return nil, nil
	}
	messageType := ws.TextMessage
	data := []byte(strarg(args, 1))
	if binary, _ := arg(args, 2).(bool); binary {
		messageType, data = ws.BinaryMessage, byteSlice(arg(args, 1))
	}
	state.mu.Lock()
	conn := state.conn
	closed := state.closed
	state.mu.Unlock()
	if conn != nil && !closed {
		return nil, conn.WriteMessage(messageType, data)
	}
	return nil, nil
}

func (r *Realm) hostCloseWebSocket(_ engine.Value, args []engine.Value) (engine.Value, error) {
	state := r.webSockets[strarg(args, 0)]
	if state == nil {
		return nil, nil
	}
	code, reason := int(numarg(args, 1)), strarg(args, 2)
	state.mu.Lock()
	state.closed = true
	conn := state.conn
	state.mu.Unlock()
	if conn == nil {
		state.cancel()
		return nil, nil
	}
	deadline := time.Now().Add(time.Second)
	return nil, conn.WriteControl(ws.CloseMessage, ws.FormatCloseMessage(code, reason), deadline)
}

func (r *Realm) closeWebSockets() {
	for _, state := range r.webSockets {
		state.mu.Lock()
		state.closed = true
		conn := state.conn
		state.mu.Unlock()
		state.cancel()
		if conn != nil {
			_ = conn.Close()
		}
	}
	clear(r.webSockets)
}
