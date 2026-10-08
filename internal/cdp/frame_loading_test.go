package cdp

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFrameLoadingEventsFollowCanonicalNavigation(t *testing.T) {
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/child" {
			fmt.Fprint(w, "<title>Child</title>")
			return
		}
		fmt.Fprint(w, `<iframe src="/child"></iframe>`)
	}))
	defer fixture.Close()
	s, addr := runningServer(t)
	c := browserConnection(t, addr)
	sid := wireCall(t, c, 1, "Target.attachToTarget", map[string]any{"targetId": s.Page.ID, "flatten": true})["sessionId"].(string)
	flatCall(t, c, sid, 2, "Page.enable", nil)
	flatCall(t, c, sid, 3, "Page.setLifecycleEventsEnabled", map[string]any{"enabled": true})
	if err := c.WriteJSON(map[string]any{"id": 4, "sessionId": sid, "method": "Page.navigate", "params": map[string]any{"url": fixture.URL}}); err != nil {
		t.Fatal(err)
	}
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	started, loaded, stopped := map[string]int{}, map[string]int{}, map[string]int{}
	for stopped[s.Page.Top.ID] == 0 {
		var event map[string]any
		if err := c.ReadJSON(&event); err != nil {
			t.Fatalf("waiting for canonical loading completion: %v; starts=%v loads=%v stops=%v", err, started, loaded, stopped)
		}
		params, _ := event["params"].(map[string]any)
		frameID, _ := params["frameId"].(string)
		switch event["method"] {
		case "Page.frameStartedLoading":
			started[frameID]++
		case "Page.lifecycleEvent":
			if params["name"] == "load" {
				if started[frameID] != 1 {
					t.Fatalf("load without one preceding start: %v", event)
				}
				loaded[frameID]++
			}
		case "Page.frameStoppedLoading":
			if loaded[frameID] != 1 || started[frameID] != 1 {
				t.Fatalf("stop before completed canonical load: %v", event)
			}
			stopped[frameID]++
		}
	}
	if len(started) != 2 || len(stopped) != 2 {
		t.Fatalf("main/child loading projections: starts=%v stops=%v", started, stopped)
	}
}

func TestFrameStoppedLoadingOnExplicitStopWithoutSyntheticLoad(t *testing.T) {
	release := make(chan struct{})
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow.js" {
			select {
			case <-release:
			case <-r.Context().Done():
			}
			return
		}
		fmt.Fprint(w, `<script src="/slow.js"></script><title>Not parsed</title>`)
	}))
	defer fixture.Close()
	defer close(release)
	s, addr := runningServer(t)
	c := browserConnection(t, addr)
	sid := wireCall(t, c, 1, "Target.attachToTarget", map[string]any{"targetId": s.Page.ID, "flatten": true})["sessionId"].(string)
	flatCall(t, c, sid, 2, "Page.enable", nil)
	flatCall(t, c, sid, 3, "Network.enable", nil)
	flatCall(t, c, sid, 4, "Page.setLifecycleEventsEnabled", map[string]any{"enabled": true})
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	_ = c.WriteJSON(map[string]any{"id": 5, "sessionId": sid, "method": "Page.navigate", "params": map[string]any{"url": fixture.URL}})
	started, stopRequested, stopped := false, false, false
	for !stopped {
		var event map[string]any
		if err := c.ReadJSON(&event); err != nil {
			t.Fatalf("waiting for stop-loading projection: %v", err)
		}
		params, _ := event["params"].(map[string]any)
		switch event["method"] {
		case "Page.frameStartedLoading":
			started = true
		case "Network.requestWillBeSent":
			request := params["request"].(map[string]any)
			if request["url"] == fixture.URL+"/slow.js" {
				if !started {
					t.Fatal("resource request preceded frame loading start")
				}
				stopRequested = true
				_ = c.WriteJSON(map[string]any{"id": 6, "sessionId": sid, "method": "Page.stopLoading"})
			}
		case "Page.loadEventFired":
			t.Fatal("stopped parser fabricated a load event")
		case "Page.frameStoppedLoading":
			if !stopRequested || params["frameId"] != s.Page.Top.ID {
				t.Fatalf("premature or unrelated loading stop: %v", event)
			}
			stopped = true
		}
	}
}

func TestFailedNavigationStopsFrameLoading(t *testing.T) {
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connection, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = connection.Close()
		}
	}))
	defer fixture.Close()
	s, addr := runningServer(t)
	c := browserConnection(t, addr)
	sid := wireCall(t, c, 1, "Target.attachToTarget", map[string]any{"targetId": s.Page.ID, "flatten": true})["sessionId"].(string)
	flatCall(t, c, sid, 2, "Page.enable", nil)
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	for attempt, target := range []string{fixture.URL, "about:blank"} {
		_ = c.WriteJSON(map[string]any{"id": 3 + attempt, "sessionId": sid, "method": "Page.navigate", "params": map[string]any{"url": target}})
		started, stopped := 0, false
		for !stopped {
			var event map[string]any
			if err := c.ReadJSON(&event); err != nil {
				t.Fatalf("navigation %s loading completion: %v", target, err)
			}
			switch event["method"] {
			case "Page.frameStartedLoading":
				started++
			case "Page.loadEventFired":
				if attempt == 0 {
					t.Fatal("failed request fabricated document load")
				}
			case "Page.frameStoppedLoading":
				if started != 1 {
					t.Fatalf("navigation %s had %d starts before stopping", target, started)
				}
				stopped = true
			}
		}
	}
}
