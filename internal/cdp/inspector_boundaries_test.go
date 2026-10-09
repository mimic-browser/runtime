package cdp

import "testing"

func TestInspectorOverlayAndRendererBoundaries(t *testing.T) {
	s, addr := runningServer(t)
	s.DevToolsChrome = "missing-devtools-renderer"
	w, id := inspectorSession(t, s, addr)
	w.call(t, id, "DOM.getDocument", map[string]any{})
	w.call(t, id, "Overlay.enable", map[string]any{})
	for _, mode := range []string{"searchForNode", "searchForUAShadowDOM", "none"} {
		w.call(t, id, "Overlay.setInspectMode", map[string]any{"mode": mode})
	}
	w.call(t, id, "Overlay.hideHighlight", map[string]any{})
	for _, test := range []struct{ method, field string }{
		{"Overlay.setShowGridOverlays", "gridNodeHighlightConfigs"}, {"Overlay.setShowFlexOverlays", "flexNodeHighlightConfigs"}, {"Overlay.setShowScrollSnapOverlays", "scrollSnapHighlightConfigs"}, {"Overlay.setShowContainerQueryOverlays", "containerQueryHighlightConfigs"}, {"Overlay.setShowIsolatedElements", "isolatedElementHighlightConfigs"},
	} {
		w.call(t, id, test.method, map[string]any{test.field: []any{}})
	}
	w.call(t, id, "Overlay.setShowViewportSizeOnResize", map[string]any{"show": false})
	w.call(t, id, "Overlay.disable", map[string]any{})
	for _, params := range []map[string]any{{"format": "webp"}, {"quality": 101}, {"maxWidth": 0}, {"everyNthFrame": 2}, {"format": "png"}} {
		if response := w.request(t, id, "Page.startScreencast", params); response["error"] == nil {
			t.Fatalf("unsupported/invalid renderer start accepted: %v", params)
		}
	}
	for _, c := range s.clientSnapshot() {
		for _, session := range c.snapshot() {
			if session.id == id && session.castState() != nil {
				t.Fatal("failed startup retained renderer ownership")
			}
		}
	}
}
