package cdp

import (
	"fmt"

	"github.com/moreveal/mimic/internal/browser"
)

const primaryWindowID = 1

// A modeled window belongs to one BrowserContext. Chrome cannot put ordinary
// and isolated-profile tabs in one window. Keep only the identity projection
// here; bounds continue to come from the Page's canonical environment.
func (s *Server) windowID(contextID string) int {
	s.windowMu.Lock()
	defer s.windowMu.Unlock()
	if s.windowIDs == nil {
		s.windowIDs = map[string]int{s.Context.ID: primaryWindowID}
		s.nextWindowID = primaryWindowID
	}
	if id, ok := s.windowIDs[contextID]; ok {
		return id
	}
	s.nextWindowID++
	s.windowIDs[contextID] = s.nextWindowID
	return s.nextWindowID
}

func (s *Server) windowPages(id int) []*browser.Page {
	contexts := s.Browser.Contexts()
	s.windowMu.Lock()
	defer s.windowMu.Unlock()
	if s.windowIDs == nil {
		s.windowIDs = map[string]int{s.Context.ID: primaryWindowID}
		s.nextWindowID = primaryWindowID
	}
	// Prune disposed contexts without reusing their public window IDs.
	live := make(map[string]*browser.Context, len(contexts))
	for _, context := range contexts {
		live[context.ID] = context
	}
	var found *browser.Context
	for contextID, windowID := range s.windowIDs {
		context := live[contextID]
		if context == nil {
			delete(s.windowIDs, contextID)
		} else if windowID == id {
			found = context
		}
	}
	if found == nil {
		return nil
	}
	return found.Pages()
}

func windowBounds(page *browser.Page) map[string]any {
	w := page.Environment().Window
	return map[string]any{
		"left":        w.X,
		"top":         w.Y,
		"width":       w.OuterWidth,
		"height":      w.OuterHeight,
		"windowState": "normal",
	}
}

func optionalInt(value any) *int {
	if value == nil {
		return nil
	}
	v := intValue(value, 0)
	return &v
}

func (s *session) handleBrowserWindow(method string, p map[string]any) (any, bool, error) {
	switch method {
	case "Browser.getWindowForTarget":
		page := s.page
		if targetID := stringValue(p["targetId"]); targetID != "" {
			var ok bool
			page, _, ok = s.server.target(targetID)
			if !ok {
				return nil, true, fmt.Errorf("No target with given id found")
			}
		}
		if page == nil {
			return nil, true, fmt.Errorf("No target with given id found")
		}
		return map[string]any{"windowId": s.server.windowID(page.ContextID()), "bounds": windowBounds(page)}, true, nil
	case "Browser.getWindowBounds":
		pages := s.server.windowPages(intValue(p["windowId"], 0))
		if len(pages) == 0 {
			return nil, true, fmt.Errorf("Browser window not found")
		}
		return map[string]any{"bounds": windowBounds(pages[0])}, true, nil
	case "Browser.setWindowBounds":
		pages := s.server.windowPages(intValue(p["windowId"], 0))
		if len(pages) == 0 {
			return nil, true, fmt.Errorf("Browser window not found")
		}
		bounds, _ := p["bounds"].(map[string]any)
		if state := stringValue(bounds["windowState"]); state != "" && state != "normal" {
			return nil, true, fmt.Errorf("Window state %s is not supported", state)
		}
		for _, page := range pages {
			if err := page.SetWindowBounds(optionalInt(bounds["left"]), optionalInt(bounds["top"]), optionalInt(bounds["width"]), optionalInt(bounds["height"])); err != nil {
				return nil, true, err
			}
		}
		return map[string]any{}, true, nil
	}
	return nil, false, nil
}
