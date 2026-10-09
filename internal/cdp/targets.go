package cdp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/moreveal/mimic/internal/browser"
)

func (s *Server) pages() []*browser.Page {
	var out []*browser.Page
	for _, c := range s.Browser.Contexts() {
		out = append(out, c.Pages()...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
func (s *Server) page(id string) (*browser.Page, bool) {
	for _, c := range s.Browser.Contexts() {
		if p, ok := c.Page(id); ok {
			return p, true
		}
	}
	return nil, false
}
func (s *Server) clientSnapshot() []*connection {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	out := make([]*connection, 0, len(s.clients))
	for c := range s.clients {
		out = append(out, c)
	}
	return out
}
func (s *Server) info(page *browser.Page) map[string]any {
	return s.targetInfo(page, "page")
}
func (s *Server) tabID(page *browser.Page) string {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.tabTargets == nil {
		s.tabTargets = make(map[*browser.Page]string)
	}
	if s.tabTargets[page] == "" {
		s.tabTargets[page] = uuid.NewString()
	}
	return s.tabTargets[page]
}
func (s *Server) target(id string) (*browser.Page, string, bool) {
	if p, ok := s.page(id); ok {
		return p, "page", true
	}
	for _, p := range s.pages() {
		if s.tabID(p) == id {
			return p, "tab", true
		}
	}
	return nil, "", false
}
func (s *Server) targetInfo(page *browser.Page, typ string) map[string]any {
	attached := false
	for _, c := range s.clientSnapshot() {
		for _, ss := range c.snapshot() {
			if ss.page == page && ss.targetType == typ {
				attached = true
				break
			}
		}
	}
	info := targetInfo(page, attached)
	info["type"] = typ
	if typ == "tab" {
		info["targetId"] = s.tabID(page)
	}
	info["canAccessOpener"] = false
	info["browserContextId"] = page.ContextID()
	s.lifecycleMu.Lock()
	opener := s.popupOpeners[page]
	s.lifecycleMu.Unlock()
	if opener != nil {
		info["openerId"] = opener.ID
		info["openerFrameId"] = opener.Top.ID
		info["canAccessOpener"] = true
	}
	return info
}

func targetMatches(filter []any, typ string) bool {
	if filter == nil {
		return typ != "browser" && typ != "tab"
	}
	for _, raw := range filter {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if wanted := stringValue(entry["type"]); wanted != "" && wanted != typ {
			continue
		}
		exclude, _ := entry["exclude"].(bool)
		return !exclude
	}
	return false
}

func (s *Server) targetChanged(page *browser.Page) {
	for _, c := range s.clientSnapshot() {
		for _, ss := range c.snapshot() {
			ss.stateMu.RLock()
			discover, filter := ss.discover, ss.targetFilter
			ss.stateMu.RUnlock()
			for _, typ := range []string{"tab", "page"} {
				if discover && targetMatches(filter, typ) {
					ss.event("Target.targetInfoChanged", map[string]any{"targetInfo": s.targetInfo(page, typ)})
				}
			}
		}
	}
}

func (s *Server) targetCreated(page *browser.Page) {
	for _, c := range s.clientSnapshot() {
		for _, ss := range c.snapshot() {
			ss.stateMu.RLock()
			discover, auto, filter, autoFilter := ss.discover, ss.autoAttach, ss.targetFilter, ss.autoFilter
			ss.stateMu.RUnlock()
			for _, typ := range []string{"tab", "page"} {
				if discover && targetMatches(filter, typ) {
					ss.event("Target.targetCreated", map[string]any{"targetInfo": s.targetInfo(page, typ)})
				}
				if auto && ss.browserSession && targetMatches(autoFilter, typ) {
					ss.attach(page, typ, true)
				}
			}
		}
	}
}

func (s *session) attach(page *browser.Page, typ string, automatic bool) *session {
	s.attachMu.Lock()
	defer s.attachMu.Unlock()
	s.stateMu.RLock()
	flat, waiting := s.autoFlat, s.waitForDebugger
	s.stateMu.RUnlock()
	if automatic {
		for _, existing := range s.transport.snapshot() {
			if existing.parent == s && existing.page == page && existing.targetType == typ {
				return existing
			}
		}
	}
	id := uuid.NewString()
	child := s.transport.newSession(page, id, s, flat, false, typ)
	child.stateMu.Lock()
	child.waitForDebugger = automatic && waiting
	child.stateMu.Unlock()
	s.event("Target.attachedToTarget", map[string]any{"sessionId": id, "targetInfo": s.server.targetInfo(page, typ), "waitingForDebugger": automatic && waiting})
	return child
}

func (c *connection) detach(ss *session, notify bool) {
	c.mu.Lock()
	if c.sessions[ss.id] != ss {
		c.mu.Unlock()
		return
	}
	delete(c.sessions, ss.id)
	c.mu.Unlock()
	for _, child := range c.snapshot() {
		if child.parent == ss {
			// Clients retain nested targets independently of their parent. Deliver
			// each child's detach while the parent session still exists remotely;
			// otherwise a tab close leaves a stale Page in the client's catalog.
			c.detach(child, notify)
		}
	}
	ss.cancel()
	ss.interceptor.Close()
	c.server.refreshCertificatePolicies()
	// Cancellation precedes waiting for this session's running browser turn.
	ss.commandMu.Lock()
	ss.unbindPage()
	ss.commandMu.Unlock()
	if notify && ss.parent != nil {
		ss.parent.event("Target.detachedFromTarget", map[string]any{"sessionId": ss.id, "targetId": ss.targetID})
	}
}

func (s *Server) closePage(page *browser.Page) bool {
	return s.closePageFrom(page, nil)
}

// A direct requester must receive its close reply before its transport closes.
// Other frontends still disconnect as part of the synchronous target teardown.
func (s *Server) closePageFrom(page *browser.Page, requester *connection) bool {
	c, ok := s.Browser.Context(page.ContextID())
	if !ok {
		return false
	}
	s.stopPump(page)
	for _, conn := range s.clientSnapshot() {
		for _, ss := range conn.snapshot() {
			if ss.page == page && ss.id != "" && !ss.browserSession {
				conn.detach(ss, true)
			} else if ss.page == page && ss.id == "" && !ss.browserSession && ss.targetType == "page" {
				ss.stopScreencast()
				if conn != requester {
					conn.cancel()
					_ = conn.conn.Close()
				}
			}
		}
	}
	page.LockCommands()
	closed := c.ClosePage(page.ID)
	page.UnlockCommands()
	if closed {
		for _, conn := range s.clientSnapshot() {
			for _, ss := range conn.snapshot() {
				ss.stateMu.RLock()
				discover, filter := ss.discover, ss.targetFilter
				ss.stateMu.RUnlock()
				if discover {
					for _, target := range []struct {
						typ string
						id  string
					}{{"tab", s.tabID(page)}, {"page", page.ID}} {
						if targetMatches(filter, target.typ) {
							ss.event("Target.targetDestroyed", map[string]any{"targetId": target.id})
						}
					}
				}
			}
		}
	}
	s.lifecycleMu.Lock()
	delete(s.tabTargets, page)
	delete(s.popupOpeners, page)
	s.lifecycleMu.Unlock()
	return closed
}

func (c *connection) disposeContexts() {
	c.mu.RLock()
	var ids []string
	for id, dispose := range c.contexts {
		if dispose {
			ids = append(ids, id)
		}
	}
	c.mu.RUnlock()
	for _, id := range ids {
		_ = c.server.disposeContext(id)
	}
}
func (s *Server) disposeContext(id string) error {
	c, ok := s.Browser.Context(id)
	if !ok || c == s.Context {
		return fmt.Errorf("Failed to find context with id %s", id)
	}
	c.Cancel()
	for _, p := range c.Pages() {
		s.closePage(p)
	}
	s.lifecycleMu.Lock()
	delete(s.downloadPolicies, id)
	s.lifecycleMu.Unlock()
	return s.Browser.CloseContext(id)
}

func (s *session) handleTarget(m message, p map[string]any) (any, bool, error) {
	empty := map[string]any{}
	switch m.Method {
	case "Browser.setPermission", "Browser.grantPermissions", "Browser.resetPermissions":
		permissionContext := s.server.Context
		if id := stringValue(p["browserContextId"]); id != "" {
			var ok bool
			permissionContext, ok = s.server.Browser.Context(id)
			if !ok {
				return nil, true, fmt.Errorf("Failed to find context with id %s", id)
			}
		}
		if m.Method == "Browser.resetPermissions" {
			permissionContext.ResetPermissions()
			return empty, true, nil
		}
		origin := stringValue(p["origin"])
		if origin != "" {
			parsed, err := url.Parse(origin)
			if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
				return nil, true, fmt.Errorf("Invalid permission origin")
			}
			origin = parsed.Scheme + "://" + parsed.Host
		}
		if m.Method == "Browser.setPermission" {
			descriptor, _ := p["permission"].(map[string]any)
			name := stringValue(descriptor["name"])
			if name != "geolocation" && name != "camera" && name != "microphone" && name != "notifications" && name != "clipboard-read" && name != "clipboard-write" {
				return nil, true, fmt.Errorf("Unsupported permission %q", name)
			}
			if name == "camera" && descriptor["panTiltZoom"] == true {
				return nil, true, fmt.Errorf("Camera pan/tilt/zoom permission is unsupported")
			}
			return empty, true, permissionContext.SetPermission(origin, name, stringValue(p["setting"]))
		}
		permissions, _ := p["permissions"].([]any)
		grants := []string{}
		for _, value := range permissions {
			name := map[string]string{"geolocation": "geolocation", "videoCapture": "camera", "audioCapture": "microphone", "notifications": "notifications", "clipboardReadWrite": "clipboard-read", "clipboardSanitizedWrite": "clipboard-write"}[stringValue(value)]
			if name == "" {
				return nil, true, fmt.Errorf("Unsupported permission %q", stringValue(value))
			}
			grants = append(grants, name)
			if stringValue(value) == "clipboardReadWrite" {
				grants = append(grants, "clipboard-write")
			}
		}
		permissionContext.GrantPermissions(origin, grants)
		return empty, true, nil
	case "Browser.getVersion":
		return map[string]any{"protocolVersion": "1.3", "product": s.server.Browser.String(), "revision": s.server.Browser.Compatibility().Version().ChromiumCommit, "userAgent": s.page.Environment().Navigator().UserAgent, "jsVersion": "virtual"}, true, nil
	case "Browser.setDownloadBehavior":
		return empty, true, s.server.setDownloadBehavior(p)
	case "Browser.close":
		// Acknowledge before shutting down the transport carrying the reply.
		s.reply(m.ID, empty, nil)
		go func() { _ = s.server.Close(context.Background()) }()
		return nil, true, errReplySent
	case "Target.getBrowserContexts":
		ids := []string{}
		for _, c := range s.server.Browser.Contexts() {
			if c != s.server.Context {
				ids = append(ids, c.ID)
			}
		}
		sort.Strings(ids)
		return map[string]any{"browserContextIds": ids}, true, nil
	case "Target.createBrowserContext":
		for _, key := range []string{"proxyServer", "proxyBypassList", "originsWithUniversalNetworkAccess"} {
			if value, ok := p[key]; ok {
				// Explicitly empty bypass rules have the same meaning as omission.
				if text, isString := value.(string); key == "proxyBypassList" && isString && text == "" {
					continue
				}
				return nil, true, fmt.Errorf("%s is not supported", key)
			}
		}
		c := s.server.Browser.NewContext()
		dispose, _ := p["disposeOnDetach"].(bool)
		s.transport.mu.Lock()
		s.transport.contexts[c.ID] = dispose
		s.transport.mu.Unlock()
		return map[string]any{"browserContextId": c.ID}, true, nil
	case "Target.disposeBrowserContext":
		return empty, true, s.server.disposeContext(stringValue(p["browserContextId"]))
	case "Target.getTargets":
		filter, _ := p["filter"].([]any)
		infos := []any{}
		for _, page := range s.server.pages() {
			for _, typ := range []string{"tab", "page"} {
				if targetMatches(filter, typ) {
					infos = append(infos, s.server.targetInfo(page, typ))
				}
			}
		}
		return map[string]any{"targetInfos": infos}, true, nil
	case "Target.getTargetInfo":
		id := stringValue(p["targetId"])
		if id == s.server.browserID {
			return map[string]any{"targetInfo": browserTargetInfo(id)}, true, nil
		}
		if id == "" {
			if s.browserSession {
				return map[string]any{"targetInfo": browserTargetInfo(s.server.browserID)}, true, nil
			}
			id = s.targetID
		}
		page, typ, ok := s.server.target(id)
		if !ok {
			return nil, true, fmt.Errorf("No target with given id found")
		}
		return map[string]any{"targetInfo": s.server.targetInfo(page, typ)}, true, nil
	case "Target.setDiscoverTargets":
		discover, _ := p["discover"].(bool)
		filter, _ := p["filter"].([]any)
		s.stateMu.Lock()
		was := s.discover
		s.discover = discover
		s.targetFilter = filter
		s.stateMu.Unlock()
		if discover && !was {
			// Chrome discovers the attached browser target when the filter
			// includes it, although Target.getTargets does not enumerate it.
			if targetMatches(filter, "browser") {
				s.event("Target.targetCreated", map[string]any{"targetInfo": browserTargetInfo(s.server.browserID)})
			}
			for _, page := range s.server.pages() {
				for _, typ := range []string{"tab", "page"} {
					if targetMatches(filter, typ) {
						s.event("Target.targetCreated", map[string]any{"targetInfo": s.server.targetInfo(page, typ)})
					}
				}
			}
		}
		return empty, true, nil
	case "Target.setAutoAttach":
		auto, _ := p["autoAttach"].(bool)
		waiting, _ := p["waitForDebuggerOnStart"].(bool)
		flat, _ := p["flatten"].(bool)
		filter, _ := p["filter"].([]any)
		s.stateMu.Lock()
		s.autoAttach = auto
		s.waitForDebugger = waiting
		s.autoFlat = flat
		s.autoFilter = filter
		s.stateMu.Unlock()
		if auto && (s.browserSession || s.id == "") {
			for _, page := range s.server.pages() {
				for _, typ := range []string{"tab", "page"} {
					if targetMatches(filter, typ) {
						s.attach(page, typ, true)
					}
				}
			}
		}
		if auto && s.targetType == "tab" && targetMatches(filter, "page") {
			s.attach(s.page, "page", true)
		}
		if !auto {
			for _, child := range s.transport.snapshot() {
				if child.parent == s {
					s.transport.detach(child, true)
				}
			}
		}
		return empty, true, nil
	case "Target.attachToTarget":
		if stringValue(p["targetId"]) == s.server.browserID {
			flat, _ := p["flatten"].(bool)
			child := s.transport.newSession(s.server.Page, uuid.NewString(), s, flat, true)
			s.event("Target.attachedToTarget", map[string]any{"sessionId": child.id, "targetInfo": browserTargetInfo(s.server.browserID), "waitingForDebugger": false})
			return map[string]any{"sessionId": child.id}, true, nil
		}
		page, typ, ok := s.server.target(stringValue(p["targetId"]))
		if !ok {
			return nil, true, fmt.Errorf("No target with given id found")
		}
		flat, _ := p["flatten"].(bool)
		child := s.transport.newSession(page, uuid.NewString(), s, flat, false, typ)
		s.event("Target.attachedToTarget", map[string]any{"sessionId": child.id, "targetInfo": s.server.targetInfo(page, typ), "waitingForDebugger": false})
		return map[string]any{"sessionId": child.id}, true, nil
	case "Target.attachToBrowserTarget":
		child := s.transport.newSession(s.server.Page, uuid.NewString(), s, true, true)
		return map[string]any{"sessionId": child.id}, true, nil
	case "Target.detachFromTarget":
		id := stringValue(p["sessionId"])
		s.transport.mu.RLock()
		child := s.transport.sessions[id]
		s.transport.mu.RUnlock()
		if child == nil || child.parent != s {
			return nil, true, fmt.Errorf("No session with given id")
		}
		s.transport.detach(child, true)
		return empty, true, nil
	case "Target.sendMessageToTarget":
		id := stringValue(p["sessionId"])
		s.transport.mu.RLock()
		child := s.transport.sessions[id]
		s.transport.mu.RUnlock()
		if child == nil || child.parent != s {
			return nil, true, fmt.Errorf("No session with given id")
		}
		if child.flat {
			return nil, true, fmt.Errorf("When using flat protocol, messages are routed to the target via the sessionId attribute")
		}
		var inner message
		if err := json.Unmarshal([]byte(stringValue(p["message"])), &inner); err != nil {
			return nil, true, fmt.Errorf("Invalid message")
		}
		s.reply(m.ID, empty, nil)
		s.transport.dispatch(child, inner)
		return nil, true, errReplySent
	case "Target.createTarget":
		c := s.server.Context
		if id := stringValue(p["browserContextId"]); id != "" {
			var ok bool
			c, ok = s.server.Browser.Context(id)
			if !ok {
				return nil, true, fmt.Errorf("Failed to find browser context with id %s", id)
			}
		}
		page, err := c.NewPage()
		if err != nil {
			return nil, true, err
		}
		s.server.ensurePump(page)
		raw := stringValue(p["url"])
		if raw != "" && raw != "about:blank" {
			s.server.lifecycleMu.Lock()
			s.server.targetNavigations[page] = raw
			s.server.lifecycleMu.Unlock()
		}
		s.server.targetCreated(page)
		waiting := false
		for _, conn := range s.server.clientSnapshot() {
			for _, child := range conn.snapshot() {
				child.stateMu.RLock()
				waiting = waiting || child.page == page && child.id != "" && child.waitForDebugger
				child.stateMu.RUnlock()
			}
		}
		if !waiting {
			s.server.resumeTarget(page)
		}
		return map[string]any{"targetId": page.ID}, true, nil
	case "Target.closeTarget":
		page, _, ok := s.server.target(stringValue(p["targetId"]))
		if !ok {
			return nil, true, fmt.Errorf("No target with given id found")
		}
		return map[string]any{"success": s.server.closePageFrom(page, s.transport)}, true, nil
	case "Target.activateTarget":
		_, _, ok := s.server.target(stringValue(p["targetId"]))
		if !ok {
			return nil, true, fmt.Errorf("No target with given id found")
		}
		return empty, true, nil
	}
	if strings.HasPrefix(m.Method, "Browser.") {
		return nil, false, nil
	}
	return nil, false, nil
}

var errReplySent = fmt.Errorf("reply already sent")
