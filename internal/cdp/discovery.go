package cdp

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/moreveal/mimic/internal/browser"
	"github.com/moreveal/mimic/internal/trace"
)

func (s *Server) protocol(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(protocolDefinitionJSON)
}
func (s *Server) newTarget(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Using unsafe HTTP verb GET to invoke /json/new. This action supports only PUT verb.", http.StatusMethodNotAllowed)
		return
	}
	page, err := s.Context.NewPage()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.ensurePump(page)
	s.targetCreated(page)
	if raw, err := url.QueryUnescape(r.URL.RawQuery); err == nil && raw != "" && raw != "about:blank" {
		s.startTargetNavigation(page, raw)
	}
	writeJSON(w, map[string]any{"id": page.ID, "type": "page", "title": page.Title(), "url": page.URL(), "webSocketDebuggerUrl": "ws://" + r.Host + "/devtools/page/" + page.ID, "devtoolsFrontendUrl": "devtools://devtools/bundled/inspector.html?ws=" + r.Host + "/devtools/page/" + page.ID})
}
func (s *Server) closeTargetHTTP(w http.ResponseWriter, r *http.Request) {
	page, ok := s.page(strings.TrimPrefix(r.URL.Path, "/json/close/"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	s.closePage(page)
	_, _ = w.Write([]byte("Target is closing"))
}
func (s *Server) activateTargetHTTP(w http.ResponseWriter, r *http.Request) {
	_, ok := s.page(strings.TrimPrefix(r.URL.Path, "/json/activate/"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	_, _ = w.Write([]byte("Target activated"))
}

func (s *Server) resumeTarget(page *browser.Page) {
	for _, c := range s.clientSnapshot() {
		for _, ss := range c.snapshot() {
			ss.stateMu.RLock()
			waiting := ss.page == page && ss.targetType == "page" && ss.id != "" && ss.waitForDebugger
			ss.stateMu.RUnlock()
			if waiting {
				return
			}
		}
	}
	s.lifecycleMu.Lock()
	raw := s.targetNavigations[page]
	delete(s.targetNavigations, page)
	s.lifecycleMu.Unlock()
	if raw != "" {
		s.startTargetNavigation(page, raw)
	}
}

// Navigation belongs to the Page, including loads initiated by a CDP session.
// Detaching the debugger releases interception and never cancels a live tab.
func (s *Server) startTargetNavigation(page *browser.Page, raw string) {
	_ = s.startNavigation(page, raw, "", s.navigationTimeout)
}
func (s *Server) startNavigation(page *browser.Page, raw, loaderID string, timeout time.Duration, committed ...func(error)) error {
	notify := func(err error) {
		for _, callback := range committed {
			callback(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	if timeout > 0 {
		cancel()
		ctx, cancel = context.WithTimeout(context.Background(), timeout)
	}
	s.lifecycleMu.Lock()
	if s.closed {
		s.lifecycleMu.Unlock()
		cancel()
		return fmt.Errorf("Browser closed")
	}
	s.workers.Add(1)
	s.lifecycleMu.Unlock()
	go func() {
		defer s.workers.Done()
		defer cancel()
		page.LockCommands()
		defer page.UnlockCommands()
		if _, ok := s.page(page.ID); !ok {
			notify(fmt.Errorf("Target closed"))
			return
		}
		s.lifecycleMu.Lock()
		if s.closed || s.pumps[page] == nil {
			s.lifecycleMu.Unlock()
			notify(fmt.Errorf("Target closed"))
			return
		}
		s.executions[page] = cancel
		s.lifecycleMu.Unlock()
		defer func() { s.lifecycleMu.Lock(); delete(s.executions, page); s.lifecycleMu.Unlock() }()
		if loaderID == "" {
			loaderID = page.ReserveNavigation()
		}
		if err := page.StartNavigation(ctx, raw, loaderID, committed...); err != nil {
			page.Trace().Add(trace.Error, "navigation", map[string]any{"url": raw, "error": err.Error()})
			notify(err)
		}
	}()
	return nil
}
