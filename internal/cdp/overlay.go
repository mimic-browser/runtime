package cdp

import (
	"context"
	"fmt"
)

// Harmless frontend initialization is accepted without allocating presentation state.
func (s *session) handleOverlay(ctx context.Context, method string, p map[string]any) (any, bool, error) {
	switch method {
	case "Overlay.enable":
		s.setDomain("Overlay", true)
	case "Overlay.disable":
		s.setDomain("Overlay", false)
	case "Overlay.setInspectMode":
		mode := stringValue(p["mode"])
		if mode != "none" && mode != "searchForNode" && mode != "searchForUAShadowDOM" {
			return nil, true, fmt.Errorf("Unsupported inspect mode")
		}
		if mode != "none" {
			return nil, true, fmt.Errorf("Visual node picking is unsupported")
		}
	case "Overlay.hideHighlight":
	case "Overlay.highlightNode":
		return nil, true, fmt.Errorf("Visual node highlighting is unsupported")
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
