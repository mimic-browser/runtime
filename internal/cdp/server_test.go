package cdp

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	chrome152 "github.com/moreveal/mimic/chrome/152"
	"github.com/moreveal/mimic/internal/browser"
	v8engine "github.com/moreveal/mimic/internal/engine/v8"
)

type discoveryTarget struct {
	WebSocketURL string `json:"webSocketDebuggerUrl"`
}

func runningServer(t *testing.T) (*Server, string) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return runningServerOnListener(t, l)
}

func runningServerOnListener(t *testing.T, l net.Listener) (*Server, string) {
	t.Helper()
	b, err := browser.New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(b)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = s.Serve(l) }()
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	return s, l.Addr().String()
}

// evaluatePageFixture serializes direct embedding evaluations with the CDP
// pump. Callers must not already hold the Page command boundary.
func evaluatePageFixture(page *browser.Page, source string) (any, error) {
	page.LockCommands()
	defer page.UnlockCommands()
	return page.Evaluate(context.Background(), source)
}

func readReply(t *testing.T, c *websocket.Conn, id float64) map[string]any {
	t.Helper()
	for {
		var raw map[string]any
		if err := c.ReadJSON(&raw); err != nil {
			t.Fatal(err)
		}
		if raw["id"] == id {
			return raw
		}
	}
}

func TestCDPProtocolProjectsActualTransportProtocol(t *testing.T) {
	cases := map[string]string{
		"HTTP/3.0": "h3",
		"h3":       "h3",
		"HTTP/2.0": "h2",
		"HTTP/1.1": "http/1.1",
	}
	for transport, want := range cases {
		if got := cdpProtocol(transport); got != want {
			t.Fatalf("cdpProtocol(%q) = %q, want %q", transport, got, want)
		}
	}
}

func TestDiscoveryAndRuntimeEvaluate(t *testing.T) {
	s, addr := runningServer(t)
	res, err := http.Get("http://" + addr + "/json/list")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var targets []discoveryTarget
	if err := json.NewDecoder(res.Body).Decode(&targets); err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 {
		t.Fatal(targets)
	}
	c, _, err := websocket.DefaultDialer.Dial(targets[0].WebSocketURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.WriteJSON(map[string]any{"id": 1, "method": "Runtime.evaluate", "params": map[string]any{"expression": "1+2"}})
	raw := readReply(t, c, 1)
	r := raw["result"].(map[string]any)["result"].(map[string]any)
	if r["value"] != float64(3) {
		t.Fatalf("reply=%#v page=%s", raw, s.Page.ID)
	}
}
func TestNavigateOverCDP(t *testing.T) {
	pageServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "<title>CDP</title><script>window.answer=42</script>")
	}))
	defer pageServer.Close()
	s, addr := runningServer(t)
	c, _, err := websocket.DefaultDialer.Dial("ws://"+addr+"/devtools/page/"+s.Page.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.WriteJSON(map[string]any{"id": 7, "method": "Page.navigate", "params": map[string]any{"url": pageServer.URL}})
	raw := readReply(t, c, 7)
	if raw["error"] != nil {
		t.Fatal(raw)
	}
	deadline := time.Now().Add(2 * time.Second)
	var v any
	for time.Now().Before(deadline) {
		_ = c.WriteJSON(map[string]any{"id": 8, "method": "Runtime.evaluate", "params": map[string]any{"expression": "globalThis.answer"}})
		reply := readReply(t, c, 8)
		if reply["error"] != nil {
			t.Fatal(reply)
		}
		v = reply["result"].(map[string]any)["result"].(map[string]any)["value"]
		if err == nil && v != nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	answer, ok := v.(float64)
	if err != nil || !ok || answer != 42 {
		t.Fatalf("value=%v err=%v", v, err)
	}
}

func TestStopLoadingCancelsNavigationAndKeepsCommittedDOM(t *testing.T) {
	slowStarted := make(chan struct{})
	pageServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow.js" {
			close(slowStarted)
			<-r.Context().Done()
			return
		}
		fmt.Fprint(w, `<div id="committed">ready</div><script src="/slow.js"></script>`)
	}))
	defer pageServer.Close()
	s, addr := runningServer(t)
	c, _, err := websocket.DefaultDialer.Dial("ws://"+addr+"/devtools/page/"+s.Page.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	_ = c.WriteJSON(map[string]any{"id": 1, "method": "Page.navigate", "params": map[string]any{"url": pageServer.URL}})
	if reply := readReply(t, c, 1); reply["error"] != nil {
		t.Fatal(reply)
	}
	select {
	case <-slowStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("navigation did not reach blocking resource")
	}
	_ = c.WriteJSON(map[string]any{"id": 2, "method": "Page.stopLoading"})
	if reply := readReply(t, c, 2); reply["error"] != nil {
		t.Fatal(reply)
	}
	_ = c.WriteJSON(map[string]any{"id": 3, "method": "Runtime.evaluate", "params": map[string]any{"expression": "document.getElementById('committed').textContent"}})
	reply := readReply(t, c, 3)
	if reply["error"] != nil {
		t.Fatal(reply)
	}
	value := reply["result"].(map[string]any)["result"].(map[string]any)["value"]
	if value != "ready" {
		t.Fatalf("committed DOM was lost after stopLoading: %#v", value)
	}
}

func TestCDPConsoleEventUsesCurrentExecutionContext(t *testing.T) {
	pageServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `<script>console.log("current realm")</script>`)
	}))
	defer pageServer.Close()
	s, addr := runningServer(t)
	c, _, err := websocket.DefaultDialer.Dial("ws://"+addr+"/devtools/page/"+s.Page.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	_ = c.WriteJSON(map[string]any{"id": 1, "method": "Runtime.enable"})
	_ = readReply(t, c, 1)
	_ = c.WriteJSON(map[string]any{"id": 2, "method": "Page.navigate", "params": map[string]any{"url": pageServer.URL}})

	var createdContextID, consoleContextID float64
	for createdContextID == 0 || consoleContextID == 0 {
		var raw map[string]any
		if err := c.ReadJSON(&raw); err != nil {
			t.Fatal(err)
		}
		params, _ := raw["params"].(map[string]any)
		switch raw["method"] {
		case "Runtime.executionContextCreated":
			contextPayload, _ := params["context"].(map[string]any)
			aux, _ := contextPayload["auxData"].(map[string]any)
			if aux["frameId"] == s.Page.Top.ID {
				createdContextID, _ = contextPayload["id"].(float64)
			}
		case "Runtime.consoleAPICalled":
			consoleContextID, _ = params["executionContextId"].(float64)
		}
	}
	if consoleContextID != createdContextID {
		t.Fatalf("console context = %v, current context = %v", consoleContextID, createdContextID)
	}
}

func TestCDPTopLegacyLifecycleEventsPrecedeLifecycleEvents(t *testing.T) {
	pageServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `<title>lifecycle order</title>`)
	}))
	defer pageServer.Close()
	s, addr := runningServer(t)
	c, _, err := websocket.DefaultDialer.Dial("ws://"+addr+"/devtools/page/"+s.Page.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	_ = c.WriteJSON(map[string]any{"id": 101, "method": "Page.enable"})
	_ = readReply(t, c, 101)
	_ = c.WriteJSON(map[string]any{"id": 1, "method": "Page.setLifecycleEventsEnabled", "params": map[string]any{"enabled": true}})
	_ = readReply(t, c, 1)
	_ = c.WriteJSON(map[string]any{"id": 2, "method": "Page.navigate", "params": map[string]any{"url": pageServer.URL}})

	order := []string{}
	for len(order) < 4 {
		var raw map[string]any
		if err := c.ReadJSON(&raw); err != nil {
			t.Fatal(err)
		}
		method, _ := raw["method"].(string)
		params, _ := raw["params"].(map[string]any)
		switch method {
		case "Page.domContentEventFired":
			order = append(order, "domContentEventFired")
		case "Page.loadEventFired":
			order = append(order, "loadEventFired")
		case "Page.lifecycleEvent":
			if params["frameId"] == s.Page.Top.ID && (params["name"] == "DOMContentLoaded" || params["name"] == "load") {
				order = append(order, params["name"].(string))
			}
		}
	}
	want := []string{"domContentEventFired", "DOMContentLoaded", "loadEventFired", "load"}
	for index := range want {
		if order[index] != want[index] {
			t.Fatalf("top lifecycle CDP order = %#v, want %#v", order, want)
		}
	}
}

func TestCDPProjectsChildFrameTreeLifecycleAndExecutionContext(t *testing.T) {
	pageServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/child" {
			fmt.Fprint(w, `<title>child title</title><script>window.childMarker=42</script>`)
			return
		}
		fmt.Fprint(w, `<title>parent title</title><iframe src="/child"></iframe>`)
	}))
	defer pageServer.Close()
	s, addr := runningServer(t)
	c, _, err := websocket.DefaultDialer.Dial("ws://"+addr+"/devtools/page/"+s.Page.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	_ = c.WriteJSON(map[string]any{"id": 101, "method": "Page.enable"})
	_ = readReply(t, c, 101)
	_ = c.WriteJSON(map[string]any{"id": 1, "method": "Runtime.enable"})
	_ = readReply(t, c, 1)
	_ = c.WriteJSON(map[string]any{"id": 2, "method": "Page.setLifecycleEventsEnabled", "params": map[string]any{"enabled": true}})
	_ = readReply(t, c, 2)
	_ = c.WriteJSON(map[string]any{"id": 20, "method": "Network.enable"})
	_ = readReply(t, c, 20)
	_ = c.WriteJSON(map[string]any{"id": 3, "method": "Page.navigate", "params": map[string]any{"url": pageServer.URL}})

	var childFrameID string
	var childContextID float64
	var childRequestFrameID string
	childRequestUsesLoader := false
	navigationAcknowledged, pageLoaded := false, false
	for !navigationAcknowledged || !pageLoaded || childFrameID == "" || childContextID == 0 || childRequestFrameID == "" {
		var raw map[string]any
		if err := c.ReadJSON(&raw); err != nil {
			t.Fatal(err)
		}
		if raw["id"] == float64(3) {
			navigationAcknowledged = true
			continue
		}
		method, _ := raw["method"].(string)
		params, _ := raw["params"].(map[string]any)
		switch method {
		case "Page.frameAttached":
			if params["parentFrameId"] == s.Page.Top.ID {
				childFrameID, _ = params["frameId"].(string)
			}
		case "Runtime.executionContextCreated":
			contextPayload, _ := params["context"].(map[string]any)
			aux, _ := contextPayload["auxData"].(map[string]any)
			if aux["frameId"] == childFrameID {
				childContextID, _ = contextPayload["id"].(float64)
			}
		case "Network.requestWillBeSent":
			requestPayload, _ := params["request"].(map[string]any)
			if requestPayload["url"] == pageServer.URL+"/child" {
				childRequestFrameID, _ = params["frameId"].(string)
				childRequestUsesLoader = params["requestId"] == params["loaderId"]
			}
		case "Page.loadEventFired":
			pageLoaded = true
		}
	}
	if childRequestFrameID != childFrameID || !childRequestUsesLoader {
		t.Fatalf("child document network attribution: frame=%q child=%q requestIsLoader=%t", childRequestFrameID, childFrameID, childRequestUsesLoader)
	}

	_ = c.WriteJSON(map[string]any{"id": 4, "method": "Page.getFrameTree"})
	treeReply := readReply(t, c, 4)
	tree := treeReply["result"].(map[string]any)["frameTree"].(map[string]any)
	children, _ := tree["childFrames"].([]any)
	if len(children) != 1 {
		t.Fatalf("CDP frame tree omitted child: %#v", tree)
	}
	childPayload := children[0].(map[string]any)["frame"].(map[string]any)
	if childPayload["id"] != childFrameID || childPayload["parentId"] != s.Page.Top.ID || childPayload["url"] != pageServer.URL+"/child" {
		t.Fatalf("incorrect child frame payload: %#v", childPayload)
	}

	_ = c.WriteJSON(map[string]any{"id": 5, "method": "Runtime.evaluate", "params": map[string]any{"contextId": childContextID, "expression": `({title:document.title,parent:parent!==self,marker:childMarker})`, "returnByValue": true}})
	evaluation := readReply(t, c, 5)
	value := evaluation["result"].(map[string]any)["result"].(map[string]any)["value"].(map[string]any)
	if value["title"] != "child title" || value["parent"] != true || value["marker"] != float64(42) {
		t.Fatalf("Runtime.evaluate did not target child context: %#v", evaluation)
	}
}

func TestPyppeteerBrowserAndSessionHandshake(t *testing.T) {
	s, addr := runningServer(t)
	c, _, err := websocket.DefaultDialer.Dial("ws://"+addr+"/devtools/page/"+s.Page.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.WriteJSON(map[string]any{"id": 1, "method": "Target.getBrowserContexts"})
	if got := readReply(t, c, 1)["result"].(map[string]any)["browserContextIds"].([]any); len(got) != 0 {
		t.Fatal(got)
	}
	_ = c.WriteJSON(map[string]any{"id": 2, "method": "Target.setDiscoverTargets", "params": map[string]any{"discover": true}})
	_ = readReply(t, c, 2)
	_ = c.WriteJSON(map[string]any{"id": 3, "method": "Target.attachToTarget", "params": map[string]any{"targetId": s.Page.ID}})
	attached := readReply(t, c, 3)
	sid := attached["result"].(map[string]any)["sessionId"].(string)
	if sid == "" {
		t.Fatal(attached)
	}
	inner, _ := json.Marshal(map[string]any{"id": 1, "method": "Page.getFrameTree", "params": map[string]any{}})
	_ = c.WriteJSON(map[string]any{"id": 4, "method": "Target.sendMessageToTarget", "params": map[string]any{"sessionId": sid, "message": string(inner)}})
	_ = readReply(t, c, 4)
	for {
		var raw map[string]any
		if err := c.ReadJSON(&raw); err != nil {
			t.Fatal(err)
		}
		if raw["method"] != "Target.receivedMessageFromTarget" {
			continue
		}
		params := raw["params"].(map[string]any)
		var response map[string]any
		if json.Unmarshal([]byte(params["message"].(string)), &response) != nil {
			continue
		}
		if response["id"] == float64(1) {
			result := response["result"].(map[string]any)
			if result["frameTree"] == nil {
				t.Fatal(response)
			}
			break
		}
	}
}

func TestTargetCreateTargetCreatesIndependentPage(t *testing.T) {
	s, addr := runningServer(t)
	c, _, err := websocket.DefaultDialer.Dial("ws://"+addr+"/devtools/page/"+s.Page.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.WriteJSON(map[string]any{"id": 1, "method": "Target.createTarget", "params": map[string]any{"url": "about:blank"}})
	created := readReply(t, c, 1)
	targetID := created["result"].(map[string]any)["targetId"].(string)
	if targetID == "" || targetID == s.Page.ID {
		t.Fatalf("createTarget did not create an independent page: %#v", created)
	}
	if _, ok := s.Context.Page(targetID); !ok {
		t.Fatalf("created page %s is absent from BrowserContext", targetID)
	}
	_ = c.WriteJSON(map[string]any{"id": 2, "method": "Target.attachToTarget", "params": map[string]any{"targetId": targetID}})
	attached := readReply(t, c, 2)
	sessionID := attached["result"].(map[string]any)["sessionId"].(string)
	inner, _ := json.Marshal(map[string]any{"id": 3, "method": "Runtime.evaluate", "params": map[string]any{"expression": "globalThis.createdPage=42;createdPage"}})
	_ = c.WriteJSON(map[string]any{"id": 4, "method": "Target.sendMessageToTarget", "params": map[string]any{"sessionId": sessionID, "message": string(inner)}})
	_ = readReply(t, c, 4)
	for {
		var raw map[string]any
		if err := c.ReadJSON(&raw); err != nil {
			t.Fatal(err)
		}
		if raw["method"] != "Target.receivedMessageFromTarget" {
			continue
		}
		params := raw["params"].(map[string]any)
		var response map[string]any
		if json.Unmarshal([]byte(params["message"].(string)), &response) == nil && response["id"] == float64(3) {
			value := response["result"].(map[string]any)["result"].(map[string]any)["value"]
			if value != float64(42) {
				t.Fatal(response)
			}
			break
		}
	}
}

func TestCDPEventLoopAdvancesTimersInRealTime(t *testing.T) {
	pageServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `<script>window.fired=false;setTimeout(()=>window.fired=true,25)</script>`)
	}))
	defer pageServer.Close()
	s, addr := runningServer(t)
	c, _, err := websocket.DefaultDialer.Dial("ws://"+addr+"/devtools/page/"+s.Page.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.WriteJSON(map[string]any{"id": 1, "method": "Page.navigate", "params": map[string]any{"url": pageServer.URL}})
	if raw := readReply(t, c, 1); raw["error"] != nil {
		t.Fatal(raw)
	}
	// Page.navigate acknowledges that navigation started; it does not imply
	// that parsing and realm initialization have completed. Poll through the
	// protocol so this test measures timer delivery rather than goroutine race.
	// Bootstrap cost under the race detector is outside the timer budget.
	_ = c.WriteJSON(map[string]any{"id": 1000, "method": "Runtime.evaluate", "params": map[string]any{"expression": "typeof fired"}})
	_ = readReply(t, c, 1000)
	deadline := time.Now().Add(time.Second)
	for id := 2; ; id++ {
		time.Sleep(25 * time.Millisecond)
		_ = c.WriteJSON(map[string]any{"id": id, "method": "Runtime.evaluate", "params": map[string]any{"expression": "typeof fired !== 'undefined' && fired"}})
		raw := readReply(t, c, float64(id))
		remote := raw["result"].(map[string]any)["result"].(map[string]any)
		if remote["value"] == true {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timer was not delivered by CDP event loop: %#v", raw)
		}
	}
}

// A stalled network operation on one Page must not prevent independent Pages
// from evaluating JavaScript. The server handler barrier proves overlap.
func TestIndependentPageCommandsDuringNavigation(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		close(entered)
		<-release
		fmt.Fprint(w, "<html><body>done</body></html>")
	}))
	defer fixture.Close()
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	s, addr := runningServer(t)
	other, err := s.Context.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	first, _, err := websocket.DefaultDialer.Dial("ws://"+addr+"/devtools/page/"+s.Page.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, _, err := websocket.DefaultDialer.Dial("ws://"+addr+"/devtools/page/"+other.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	first.WriteJSON(map[string]any{"id": 1, "method": "Page.navigate", "params": map[string]any{"url": fixture.URL}})
	// The first navigation acknowledges document commit, so its main-response
	// barrier must stay held while the independent Page executes its command.
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("navigation did not reach barrier")
	}
	second.SetReadDeadline(time.Now().Add(2 * time.Second))
	second.WriteJSON(map[string]any{"id": 2, "method": "Runtime.evaluate", "params": map[string]any{"expression": "6*7"}})
	reply := readReply(t, second, 2)
	if reply["result"].(map[string]any)["result"].(map[string]any)["value"] != float64(42) {
		t.Fatal(reply)
	}
	close(release)
	first.SetReadDeadline(time.Now().Add(2 * time.Second))
	if reply := readReply(t, first, 1); reply["error"] != nil || reply["result"].(map[string]any)["errorText"] != nil {
		t.Fatal("first Page did not commit after release", reply)
	}
}
