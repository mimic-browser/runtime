package cdp

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestInspectorNavigationInvalidatesNodeAndObjectHandles(t *testing.T) {
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`<div id="new">new</div>`)) }))
	t.Cleanup(fixture.Close)
	s, addr := runningServer(t)
	w, id := inspectorSession(t, s, addr)
	w.call(t, id, "Page.enable", map[string]any{})
	w.call(t, id, "Runtime.enable", map[string]any{})
	w.call(t, id, "Runtime.evaluate", map[string]any{"expression": `document.body.innerHTML='<div id="old">old</div><iframe></iframe>';document.querySelector('iframe').contentDocument.body.innerHTML='<div id="child">old child</div>'`})
	root := w.call(t, id, "DOM.getDocument", map[string]any{"depth": -1})["root"]
	old := findInspectorNode(root, "id", "old")
	child := findInspectorNode(root, "id", "child")
	object := w.call(t, id, "DOM.resolveNode", map[string]any{"nodeId": old["nodeId"]})["object"].(map[string]any)
	if reply := w.call(t, id, "Page.navigate", map[string]any{"url": fixture.URL}); reply["errorText"] != nil {
		t.Fatalf("navigation failed: %v", reply)
	}
	w.event(t, "DOM.documentUpdated")
	root = w.call(t, id, "DOM.getDocument", map[string]any{"depth": -1})["root"]
	newNode := findInspectorNode(root, "id", "new")
	if newNode == nil || newNode["nodeId"] == old["nodeId"] {
		t.Fatalf("navigation reused a live node handle: %v", newNode)
	}
	for _, node := range []map[string]any{old, child} {
		if reply := w.request(t, id, "DOM.setAttributeValue", map[string]any{"nodeId": node["nodeId"], "name": "title", "value": "wrong"}); reply["error"] == nil {
			t.Fatalf("old document handle accepted: %v", reply)
		}
	}
	if reply := w.request(t, id, "Runtime.getProperties", map[string]any{"objectId": object["objectId"]}); reply["error"] == nil {
		t.Fatalf("old realm object accepted: %v", reply)
	}
	w.call(t, id, "DOM.setAttributeValue", map[string]any{"nodeId": newNode["nodeId"], "name": "title", "value": "current"})
}

func TestInspectorResourceTreeAndNavigationScope(t *testing.T) {
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<link rel="stylesheet" href="/top.css"><iframe src="/frame"></iframe>`))
		case "/frame":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<link rel="stylesheet" href="/child.css"><div>child</div>`))
		default:
			w.Header().Set("Content-Type", "text/css")
			_, _ = w.Write([]byte(`div { color: red }`))
		}
	}))
	t.Cleanup(fixture.Close)
	s, addr := runningServer(t)
	w, id := inspectorSession(t, s, addr)
	w.call(t, id, "Page.enable", map[string]any{})
	w.call(t, id, "Page.navigate", map[string]any{"url": fixture.URL + "/"})
	w.event(t, "Page.loadEventFired")
	tree := w.call(t, id, "Page.getResourceTree", map[string]any{})["frameTree"].(map[string]any)
	assertResources := func(frame map[string]any, want string) {
		t.Helper()
		found := false
		for _, v := range frame["resources"].([]any) {
			resource := v.(map[string]any)
			if resource["url"] == fixture.URL+want {
				found = true
			}
			if resource["url"] == fixture.URL+"/top.css" && want == "/child.css" || resource["url"] == fixture.URL+"/child.css" && want == "/top.css" {
				t.Fatalf("resource assigned to wrong frame: %v", frame)
			}
		}
		if !found {
			t.Fatalf("missing resource %s: %v", want, frame)
		}
	}
	assertResources(tree, "/top.css")
	child := tree["childFrames"].([]any)[0].(map[string]any)
	assertResources(child, "/child.css")
	childID := child["frame"].(map[string]any)["id"]
	content := w.call(t, id, "Page.getResourceContent", map[string]any{"frameId": childID, "url": fixture.URL + "/child.css"})
	if content["content"] != "div { color: red }" {
		t.Fatalf("resource content: %v", content)
	}
	if reply := w.request(t, id, "Page.getResourceContent", map[string]any{"frameId": s.Page.Top.ID, "url": fixture.URL + "/child.css"}); reply["error"] == nil {
		t.Fatal("resource exposed to wrong frame")
	}
	if reply := w.call(t, id, "Page.navigate", map[string]any{"url": "about:blank"}); reply["errorText"] != nil {
		t.Fatalf("navigation failed: %v", reply)
	}
	tree = w.call(t, id, "Page.getResourceTree", map[string]any{})["frameTree"].(map[string]any)
	if len(tree["resources"].([]any)) != 0 {
		t.Fatalf("new document inherited old resources: %v", tree)
	}
}

func TestInspectorSearchWithoutTreeInvalidatesOnNavigation(t *testing.T) {
	s, addr := runningServer(t)
	w, id := inspectorSession(t, s, addr)
	w.call(t, id, "DOM.enable", map[string]any{})
	w.call(t, id, "Runtime.evaluate", map[string]any{"expression": `document.body.innerHTML='<div id="probe">searchable</div>'`})
	search := w.call(t, id, "DOM.performSearch", map[string]any{"query": "#probe"})
	if search["resultCount"] != float64(1) {
		t.Fatalf("search: %v", search)
	}
	w.call(t, id, "Page.navigate", map[string]any{"url": "about:blank"})
	w.event(t, "DOM.documentUpdated")
	if reply := w.request(t, id, "DOM.getSearchResults", map[string]any{"searchId": search["searchId"], "fromIndex": 0, "toIndex": 1}); reply["error"] == nil {
		t.Fatal("retired search retained")
	}
}
