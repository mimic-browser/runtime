package cdp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/moreveal/mimic/internal/browser"
	"github.com/moreveal/mimic/internal/trace"
)

// A connection owns the wire, not a Page. Every attachment has an immutable
// Page binding and its own domain/Runtime state. Flattened and legacy sessions
// share the same registry and write lock; command IDs are scoped to a session.
type connection struct {
	profileCommands bool
	server          *Server
	conn            *websocket.Conn
	ctx             context.Context
	cancel          context.CancelFunc
	writeMu         sync.Mutex
	mu              sync.RWMutex
	root            *session
	sessions        map[string]*session
	contexts        map[string]bool
	work            sync.WaitGroup
}

func (s *Server) ws(w http.ResponseWriter, r *http.Request) {
	isBrowser := r.URL.Path == "/devtools/browser/"+s.browserID
	page := s.Page
	if !isBrowser {
		var ok bool
		page, ok = s.page(strings.TrimPrefix(r.URL.Path, "/devtools/page/"))
		if !ok {
			http.NotFound(w, r)
			return
		}
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := &connection{server: s, conn: conn, ctx: ctx, cancel: cancel, sessions: make(map[string]*session), contexts: make(map[string]bool), profileCommands: os.Getenv("MIMIC_PROFILE_CDP") == "1"}
	s.lifecycleMu.Lock()
	if s.closed {
		s.lifecycleMu.Unlock()
		cancel()
		_ = conn.Close()
		return
	}
	s.connections[conn] = cancel
	s.clients[c] = struct{}{}
	s.workers.Add(1)
	s.lifecycleMu.Unlock()
	if s.connectionOpened != nil {
		s.connectionOpened()
	}
	defer s.workers.Done()
	c.root = c.newSession(page, "", nil, false, isBrowser)
	defer func() {
		cancel()
		_ = conn.Close()
		for _, ss := range c.snapshot() {
			ss.interceptor.Close()
		}
		s.refreshCertificatePolicies()
		c.work.Wait()
		for _, ss := range c.snapshot() {
			ss.cancel()
			ss.unbindPage()
		}
		// disposeOnDetach belongs to the connection that created the Context.
		c.disposeContexts()
		s.lifecycleMu.Lock()
		delete(s.connections, conn)
		delete(s.clients, c)
		s.lifecycleMu.Unlock()
	}()
	for {
		var raw json.RawMessage
		if err := conn.ReadJSON(&raw); err != nil {
			return
		}
		var m message
		if err := json.Unmarshal(raw, &m); err != nil || m.Method == "" {
			c.write(map[string]any{"id": m.ID, "error": map[string]any{"code": -32600, "message": "Invalid request"}})
			continue
		}
		ss := c.root
		if m.SessionID != "" {
			c.mu.RLock()
			ss = c.sessions[m.SessionID]
			c.mu.RUnlock()
			if ss == nil || !ss.flat {
				c.write(map[string]any{"id": m.ID, "sessionId": m.SessionID, "error": map[string]any{"code": -32001, "message": "Session with given id not found."}})
				continue
			}
		}
		c.dispatch(ss, m)
	}
}

func (c *connection) newSession(page *browser.Page, id string, parent *session, flat, browserSession bool, targetTypes ...string) *session {
	ctx, cancel := context.WithCancel(c.ctx)
	s := &session{server: c.server, transport: c, conn: c.conn, ctx: ctx, cancel: cancel, page: page, id: id, parent: parent, flat: flat, browserSession: browserSession, navigationTimeout: c.server.navigationTimeout, domains: make(map[string]bool)}
	s.targetID = page.ID
	s.targetType = "page"
	if browserSession {
		s.targetID = c.server.browserID
		s.targetType = "browser"
	}
	if len(targetTypes) > 0 && targetTypes[0] == "tab" {
		s.targetType = "tab"
		s.targetID = c.server.tabID(page)
	}
	page.LockCommands()
	s.bindPage(page)
	page.UnlockCommands()
	c.mu.Lock()
	c.sessions[id] = s
	c.mu.Unlock()
	return s
}

func (c *connection) snapshot() []*session {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]*session, 0, len(c.sessions))
	for _, s := range c.sessions {
		out = append(out, s)
	}
	return out
}

type commandOrder struct {
	finished chan struct{}
	once     sync.Once
}

func (o *commandOrder) release() {
	if o != nil {
		o.once.Do(func() { close(o.finished) })
	}
}

// Controls must remain callable while application work or interception waits.
// Target/Browser and custom Context commands have their own explicit ownership
// boundaries rather than running under the connection's incidental Page.
func independentControlCommand(method string) bool {
	switch method {
	case "Fetch.disable", "Fetch.getResponseBody", "Network.setRequestInterception",
		"Fetch.continueRequest", "Fetch.continueResponse", "Fetch.failRequest", "Fetch.fulfillRequest",
		"Network.continueInterceptedRequest", "Mimic.startTrace", "Mimic.stopTrace", "Mimic.getTrace",
		"Mimic.getStatus", "Mimic.getDiagnostics", "Mimic.cancelExecution", "Page.stopLoading", "Target.closeTarget":
		return true
	case "Page.screencastFrameAck", "Page.stopScreencast":
		return true
	default:
		return false
	}
}

func orderedPageCommand(method string) bool {
	if method == "Target.sendMessageToTarget" {
		// Reserve legacy inner-message arrival order before dispatching them.
		return true
	}
	return !independentControlCommand(method) && !strings.HasPrefix(method, "Target.") &&
		!strings.HasPrefix(method, "Browser.") && !strings.HasPrefix(method, "Mimic.")
}

// Once read from an attached client, closing an owned browser object belongs to
// that object's lifecycle, not to the requesting socket. EOF still cancels
// evaluations, navigation and other session work, but must not revoke an
// accepted close (clients may intentionally disconnect without its reply).
// Legacy envelopes carry the same ownership as the enclosed command; their
// normal handler still validates parameters and resolves the child session.
func browserOwnedClose(m message) bool {
	for {
		switch m.Method {
		case "Target.closeTarget", "Target.disposeBrowserContext", "Browser.close", "Page.close":
			return true
		case "Target.sendMessageToTarget":
			var envelope struct {
				Message string `json:"message"`
			}
			var inner message
			if json.Unmarshal(m.Params, &envelope) != nil || json.Unmarshal([]byte(envelope.Message), &inner) != nil {
				return false
			}
			m = inner
		default:
			return false
		}
	}
}

func (c *connection) dispatch(s *session, m message) {
	if c.profileCommands {
		m.timing = &commandTiming{queued: time.Now()}
		s.commandTimings.Store(m.ID, m.timing)
	}
	// A mutex inside the worker excludes concurrent execution but does not
	// preserve arrival order. Reserve each Page command on the reader before
	// starting its worker, including frame/context initialization and input.
	// Asynchronous navigation and debugger Promise waits explicitly release their
	// turn; cancellation/interception and other Page sessions remain independent.
	var previous <-chan struct{}
	if orderedPageCommand(m.Method) {
		s.orderMu.Lock()
		previous = s.orderTail
		m.order = &commandOrder{finished: make(chan struct{})}
		s.orderTail = m.order.finished
		s.orderMu.Unlock()
	}
	c.work.Add(1)
	go func() {
		defer c.work.Done()
		if m.order != nil {
			defer m.order.release()
			if previous != nil {
				select {
				case <-previous:
				case <-s.ctx.Done():
				}
			}
		}
		if s.ctx.Err() != nil && !browserOwnedClose(m) {
			s.reply(m.ID, nil, fmt.Errorf("Session closed"))
			if m.timing != nil {
				s.commandTimings.Delete(m.ID)
			}
			return
		}
		s.handle(m)
	}()
}

func (c *connection) write(v any) {
	c.writeTimed(v, nil)
}

func (c *connection) writeTimed(v any, timing *commandTiming) {
	var started time.Time
	if timing != nil {
		started = time.Now()
	}
	payload, err := json.Marshal(v)
	if timing != nil {
		timing.serialize += time.Since(started)
	}
	if err != nil {
		return
	}
	if timing != nil {
		started = time.Now()
	}
	c.writeMu.Lock()
	if timing != nil {
		timing.writeWait += time.Since(started)
		started = time.Now()
	}
	defer c.writeMu.Unlock()
	_ = c.conn.WriteMessage(websocket.TextMessage, payload)
	if timing != nil {
		timing.write += time.Since(started)
	}
}

func (s *session) send(v map[string]any) {
	s.sendTimed(v, nil)
}

func (s *session) sendTimed(v map[string]any, timing *commandTiming) {
	if s.id != "" {
		if s.flat {
			v["sessionId"] = s.id
		} else {
			var started time.Time
			if timing != nil {
				started = time.Now()
			}
			raw, err := json.Marshal(v)
			if timing != nil {
				timing.serialize += time.Since(started)
			}
			if err != nil {
				return
			}
			if s.parent.ctx.Err() == nil {
				s.parent.sendTimed(map[string]any{"method": "Target.receivedMessageFromTarget", "params": map[string]any{"sessionId": s.id, "message": string(raw), "targetId": s.targetID}}, timing)
			}
			return
		}
	}
	s.transport.writeTimed(v, timing)
}

func (s *session) reply(id int64, result any, err error) {
	var timing *commandTiming
	if s.transport.profileCommands {
		if value, ok := s.commandTimings.Load(id); ok {
			timing = value.(*commandTiming)
		}
	}
	if err != nil {
		if s.server.Browser.DevPreviewEnabled() && s.page.Trace().Wants(trace.CDP) {
			s.page.Trace().Add(trace.CDP, "commandError", map[string]any{"commandId": id, "sessionId": s.id, "error": err.Error()})
		}
		code := -32000
		// Generated validation errors retain Chrome's protocol error class.
		if e, ok := err.(interface{ ProtocolCode() int }); ok {
			code = e.ProtocolCode()
		}
		payload := map[string]any{"code": code, "message": err.Error()}
		if e, ok := err.(interface{ ProtocolData() any }); ok {
			if data := e.ProtocolData(); data != nil {
				payload["data"] = data
			}
		}
		s.sendTimed(map[string]any{"id": id, "error": payload}, timing)
		return
	}
	if result == nil {
		result = map[string]any{}
	}
	s.sendTimed(map[string]any{"id": id, "result": result}, timing)
}

func (s *session) event(method string, params any) {
	if s.ctx.Err() != nil {
		return
	}
	domain := strings.SplitN(method, ".", 2)[0]
	switch domain {
	case "Page", "Runtime", "Network", "DOM", "CSS", "Overlay", "Log", "Performance", "Audits", "WebMCP":
		if !s.domainEnabled(domain) {
			return
		}
	}
	if method == "Page.lifecycleEvent" {
		s.stateMu.RLock()
		enabled := s.lifecycleEvents
		s.stateMu.RUnlock()
		if !enabled {
			return
		}
	}
	s.page.Trace().Add(trace.CDP, "event", map[string]any{"method": method, "params": params, "sessionId": s.id})
	s.send(map[string]any{"method": method, "params": params})
}

func (s *session) rootEvent(method string, params any) { s.event(method, params) }

func (s *session) setDomain(domain string, enabled bool) {
	s.stateMu.Lock()
	s.domains[domain] = enabled
	s.stateMu.Unlock()
}
func (s *session) domainEnabled(domain string) bool {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return s.domains[domain]
}
