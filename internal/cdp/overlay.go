package cdp

import (
	"context"
	"fmt"
)

// Overlay state is session-local and does not create a renderer. It is drawn
// only when this session has explicitly opened a Blink presentation.
func (s *session) handleOverlay(ctx context.Context, method string, p map[string]any) (any, bool, error) {
	switch method {
	case "Overlay.enable":
		s.setDomain("Overlay", true)
	case "Overlay.disable":
		s.updateHighlight(nil)
		s.setDomain("Overlay", false)
		s.stateMu.Lock()
		s.inspectMode = "none"
		s.stateMu.Unlock()
	case "Overlay.setInspectMode":
		mode := stringValue(p["mode"])
		if mode != "none" && mode != "searchForNode" && mode != "searchForUAShadowDOM" {
			return nil, true, fmt.Errorf("Unsupported inspect mode")
		}
		s.stateMu.Lock()
		s.inspectMode = mode
		s.stateMu.Unlock()
	case "Overlay.hideHighlight":
		s.updateHighlight(nil)
	case "Overlay.highlightNode":
		frame, id, err := s.nodeOwner(ctx, p)
		if err != nil {
			return nil, true, err
		}
		s.updateHighlight(&presentationHighlight{key: fmt.Sprintf("%s:%d", frame.RealmID(), id), config: p["highlightConfig"]})
	case "Overlay.setShowGridOverlays", "Overlay.setShowFlexOverlays", "Overlay.setShowScrollSnapOverlays", "Overlay.setShowContainerQueryOverlays", "Overlay.setShowIsolatedElements":
		// The frontend sends empty lists during startup and teardown. Non-empty
		// layout overlays require Blink projection identifiers, not Mimic layout.
		for _, value := range p {
			if list, ok := value.([]any); ok && len(list) > 0 {
				return nil, true, fmt.Errorf("Layout overlays are unsupported")
			}
		}
	case "Overlay.setShowViewportSizeOnResize":
		if p["show"] == true {
			return nil, true, fmt.Errorf("Viewport size overlay is unsupported")
		}
	default:
		return nil, false, nil
	}
	return map[string]any{}, true, nil
}

// A highlight is presentation state. Enqueue it without calling Chrome under
// the Page command boundary; the screencast worker applies it before capture.
type presentationHighlight struct {
	key    string
	config any
}

func (s *session) updateHighlight(highlight *presentationHighlight) {
	if cast := s.castState(); cast != nil {
		cast.mu.Lock()
		cast.highlight = highlight
		cast.overlayRevision++
		cast.mu.Unlock()
		select {
		case cast.wake <- struct{}{}:
		default:
		}
	}
}
