package cdp

import (
	"reflect"
	"testing"
)

func TestBrowserWindowBoundsStayWithinOwningContext(t *testing.T) {
	s, address := runningServer(t)
	c := browserConnection(t, address)
	managed := wireCall(t, c, 1, "Mimic.createContext", map[string]any{"profile": map[string]any{"generate": map[string]any{"seed": "window-isolation"}}})["browserContextId"].(string)
	managedTarget := wireCall(t, c, 2, "Target.createTarget", map[string]any{"browserContextId": managed, "url": "about:blank"})["targetId"].(string)
	managedWindow := wireCall(t, c, 3, "Browser.getWindowForTarget", map[string]any{"targetId": managedTarget})
	managedID := managedWindow["windowId"]
	if managedID == float64(primaryWindowID) {
		t.Fatal("managed and default Context share a window")
	}
	ordinary := wireCall(t, c, 4, "Target.createBrowserContext", nil)["browserContextId"].(string)
	ordinaryTarget := wireCall(t, c, 5, "Target.createTarget", map[string]any{"browserContextId": ordinary, "url": "about:blank"})["targetId"].(string)
	otherTarget := wireCall(t, c, 6, "Target.createTarget", map[string]any{"browserContextId": ordinary, "url": "about:blank"})["targetId"].(string)
	ordinaryWindow := wireCall(t, c, 7, "Browser.getWindowForTarget", map[string]any{"targetId": ordinaryTarget})
	ordinaryID := ordinaryWindow["windowId"]
	if ordinaryID == managedID || ordinaryID == float64(primaryWindowID) {
		t.Fatal("independent Contexts share a window")
	}
	defaultBefore := s.Page.Environment().Window
	wireCall(t, c, 8, "Browser.setWindowBounds", map[string]any{"windowId": ordinaryID, "bounds": map[string]any{"left": 21, "top": 34, "width": 987, "height": 654}})
	for _, target := range []string{ordinaryTarget, otherTarget} {
		page, _, _ := s.target(target)
		w := page.Environment().Window
		if w.X != 21 || w.Y != 34 || w.OuterWidth != 987 || w.OuterHeight != 654 {
			t.Fatalf("ordinary page bounds: %+v", w)
		}
	}
	if !reflect.DeepEqual(defaultBefore, s.Page.Environment().Window) {
		t.Fatal("default Context window changed")
	}
	managedBounds := wireCall(t, c, 9, "Browser.getWindowBounds", map[string]any{"windowId": managedID})["bounds"]
	if !reflect.DeepEqual(managedBounds, managedWindow["bounds"]) {
		t.Fatal("managed Context window changed")
	}
	// The window ID, rather than the calling session's Page, selects the bounds.
	sid := wireCall(t, c, 10, "Target.attachToTarget", map[string]any{"targetId": managedTarget, "flatten": true})["sessionId"].(string)
	got := flatCall(t, c, sid, 11, "Browser.getWindowBounds", map[string]any{"windowId": ordinaryID})["bounds"].(map[string]any)
	if got["width"] != float64(987) || got["height"] != float64(654) {
		t.Fatal(got)
	}
	wireCall(t, c, 12, "Target.disposeBrowserContext", map[string]any{"browserContextId": ordinary})
	_ = c.WriteJSON(map[string]any{"id": 13, "method": "Browser.getWindowBounds", "params": map[string]any{"windowId": ordinaryID}})
	if readReply(t, c, 13)["error"] == nil {
		t.Fatal("disposed Context retained a live window")
	}
}
