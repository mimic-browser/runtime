package cdp

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDocumentUpdatedAndFrameSecurityProjection(t *testing.T) {
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/isolated" {
			w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
			w.Header().Set("Cross-Origin-Embedder-Policy", "require-corp")
		}
		if r.URL.Path == "/child" {
			fmt.Fprint(w, "<!doctype html><p>child</p>")
		} else {
			fmt.Fprint(w, "<!doctype html><p id='probe'>current document</p><iframe src='/child'></iframe>")
		}
	}))
	defer fixture.Close()
	s, address := runningServer(t)
	c := browserConnection(t, address)
	_ = c.SetReadDeadline(time.Now().Add(15 * time.Second))
	id := wireCall(t, c, 1, "Target.attachToTarget", map[string]any{"targetId": s.Page.ID, "flatten": true})["sessionId"].(string)
	flatCall(t, c, id, 2, "Page.enable", map[string]any{})
	flatCall(t, c, id, 3, "DOM.getDocument", map[string]any{})
	requestID := 10
	navigate := func(path string, wantUpdates int) {
		t.Helper()
		requestID++
		if err := c.WriteJSON(map[string]any{"id": requestID, "sessionId": id, "method": "Page.navigate", "params": map[string]any{"url": fixture.URL + path}}); err != nil {
			t.Fatal(err)
		}
		updates, replied, loaded := 0, false, false
		for !replied || !loaded {
			var event map[string]any
			if err := c.ReadJSON(&event); err != nil {
				t.Fatal(err)
			}
			if event["id"] == float64(requestID) {
				replied = true
				if event["error"] != nil {
					t.Fatal(event)
				}
			}
			if event["sessionId"] != id {
				continue
			}
			switch event["method"] {
			case "DOM.documentUpdated":
				updates++
			case "Page.loadEventFired":
				loaded = true
			}
		}
		if updates != wantUpdates {
			t.Fatalf("%s: documentUpdated count = %d, want %d", path, updates, wantUpdates)
		}
	}
	navigate("/first", 1)
	root := flatCall(t, c, id, 20, "DOM.getDocument", map[string]any{})["root"].(map[string]any)["nodeId"]
	current := flatCall(t, c, id, 21, "DOM.querySelector", map[string]any{"nodeId": root, "selector": "#probe"})
	if current["nodeId"] == float64(0) {
		t.Fatal("document refresh did not project the new canonical tree")
	}
	checkFrame := func(wantIsolated string, featureCount int) {
		t.Helper()
		frame := flatCall(t, c, id, 22, "Page.getFrameTree", map[string]any{})["frameTree"].(map[string]any)["frame"].(map[string]any)
		if frame["secureContextType"] != "SecureLocalhost" || frame["crossOriginIsolatedContextType"] != wantIsolated {
			t.Fatalf("security projection: %#v", frame)
		}
		features, ok := frame["gatedAPIFeatures"].([]any)
		if !ok || len(features) != featureCount {
			t.Fatalf("gated features: %#v", frame["gatedAPIFeatures"])
		}
	}
	checkFrame("NotIsolated", 0)
	navigate("/isolated", 1)
	checkFrame("Isolated", 2)
	flatCall(t, c, id, 23, "DOM.disable", map[string]any{})
	navigate("/disabled", 0)
}
