package cdp

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func debuggerScriptParsed(t *testing.T, wire *inspectorWire, url string) map[string]any {
	t.Helper()
	for {
		parsed := wire.event(t, "Debugger.scriptParsed")
		if parsed["url"] == url {
			return parsed
		}
	}
}

func TestDebuggerNativeBreakpointPreservesSourceAndAuthorStack(t *testing.T) {
	s, addr := runningServer(t)
	w, id := inspectorSession(t, s, addr)
	w.call(t, id, "Runtime.enable", map[string]any{})

	w.call(t, id, "Runtime.evaluate", map[string]any{"expression": `globalThis.nativeStackProbe = function nativeStackProbe() {
  return new Error("stack-probe").stack;
};
//# sourceURL=https://example.test/stack-probe.js`, "returnByValue": true})
	control := w.call(t, id, "Runtime.evaluate", map[string]any{"expression": "nativeStackProbe()", "returnByValue": true})["result"].(map[string]any)["value"]

	w.call(t, id, "Debugger.enable", map[string]any{})
	const source = `globalThis.nativeBreakpointProbe = function nativeBreakpointProbe() {
  const plaintext = "proof-plaintext";
  return plaintext;
};
//# sourceURL=https://example.test/original-hsw.js`
	w.call(t, id, "Runtime.evaluate", map[string]any{"expression": source, "returnByValue": true})
	parsed := debuggerScriptParsed(t, w, "https://example.test/original-hsw.js")
	scriptID := parsed["scriptId"].(string)
	gotSource := w.call(t, id, "Debugger.getScriptSource", map[string]any{"scriptId": scriptID})["scriptSource"]
	if gotSource != source {
		t.Fatalf("debugger changed script source:\n%s", gotSource)
	}
	location := w.call(t, id, "Debugger.setBreakpoint", map[string]any{"location": map[string]any{
		"scriptId": scriptID, "lineNumber": 2, "columnNumber": 2,
	}})["actualLocation"].(map[string]any)
	if location["scriptId"] != scriptID {
		t.Fatalf("breakpoint resolved in another script: %v", location)
	}

	done := make(chan map[string]any, 1)
	go func() {
		done <- w.request(t, id, "Runtime.evaluate", map[string]any{"expression": "nativeBreakpointProbe()", "returnByValue": true})
	}()
	paused := w.event(t, "Debugger.paused")
	frames := paused["callFrames"].([]any)
	if len(frames) == 0 {
		t.Fatal("breakpoint supplied no call frame")
	}
	frame := frames[0].(map[string]any)
	plaintext := w.call(t, id, "Debugger.evaluateOnCallFrame", map[string]any{
		"callFrameId": frame["callFrameId"], "expression": "plaintext", "returnByValue": true,
	})["result"].(map[string]any)["value"]
	if plaintext != "proof-plaintext" {
		t.Fatalf("paused local: %v", plaintext)
	}
	w.call(t, id, "Debugger.resume", map[string]any{})
	select {
	case response := <-done:
		if response["error"] != nil {
			t.Fatalf("resumed evaluation: %v", response)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("evaluation remained paused after Debugger.resume")
	}

	attached := w.call(t, id, "Runtime.evaluate", map[string]any{"expression": "nativeStackProbe()", "returnByValue": true})["result"].(map[string]any)["value"]
	if attached != control {
		t.Fatalf("Debugger attachment changed Error.stack:\ncontrol: %v\nattached: %v", control, attached)
	}
	if !strings.Contains(attached.(string), "stack-probe.js") {
		t.Fatalf("stack lost original source URL: %v", attached)
	}
}

func TestFrameViewportReadsDoNotCompileTransportScripts(t *testing.T) {
	s, addr := runningServer(t)
	w, id := inspectorSession(t, s, addr)
	w.call(t, id, "Runtime.enable", map[string]any{})
	w.call(t, id, "Debugger.enable", map[string]any{})
	for len(w.events) > 0 {
		<-w.events
	}
	result := w.call(t, id, "Runtime.evaluate", map[string]any{
		"expression": `(() => { const frame = document.createElement('iframe'); document.body.appendChild(frame); const values = []; for (let i = 0; i < 24; i++) values.push(frame.contentWindow.innerWidth); return values.every(value => value > 0); })()`,
		"returnByValue": true,
	})
	if result["result"].(map[string]any)["value"] != true {
		t.Fatalf("frame viewport read failed: %v", result)
	}
	for len(w.events) > 0 {
		event := <-w.events
		if event["method"] != "Debugger.scriptParsed" {
			continue
		}
		parsed := event["params"].(map[string]any)
		if parsed["length"] != float64(len(`(value=>JSON.stringify(value))`)) {
			continue
		}
		source := w.call(t, id, "Debugger.getScriptSource", map[string]any{"scriptId": parsed["scriptId"]})["scriptSource"]
		if source == `(value=>JSON.stringify(value))` {
			t.Fatal("a passive frame viewport read compiled a JSON export script")
		}
	}
}

func TestDebuggerNativeAttachmentFollowsNavigationAndMultipleSessions(t *testing.T) {
	const source = `globalThis.navigationDebuggerProbe = "original-source";`
	for _, snapshots := range []string{"1", "0"} {
		t.Run("disableSnapshot="+snapshots, func(t *testing.T) {
			t.Setenv("MIMIC_DISABLE_BOOTSTRAP_SNAPSHOT", snapshots)
			mux := http.NewServeMux()
			server := httptest.NewServer(mux)
			defer server.Close()
			scriptURL := server.URL + "/probe.js"
			mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprintf(w, `<script src=%q></script>`, scriptURL)
			})
			mux.HandleFunc("/probe.js", func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/javascript")
				fmt.Fprint(w, source)
			})

			s, addr := runningServer(t)
			first, firstID := inspectorSession(t, s, addr)
			second, secondID := inspectorSession(t, s, addr)
			first.call(t, firstID, "Debugger.enable", map[string]any{})
			second.call(t, secondID, "Debugger.enable", map[string]any{})
			first.call(t, firstID, "Page.navigate", map[string]any{"url": server.URL})

			firstParsed := debuggerScriptParsed(t, first, scriptURL)
			secondParsed := debuggerScriptParsed(t, second, scriptURL)
			for _, item := range []struct {
				wire     *inspectorWire
				session  string
				scriptID any
			}{{first, firstID, firstParsed["scriptId"]}, {second, secondID, secondParsed["scriptId"]}} {
				got := item.wire.call(t, item.session, "Debugger.getScriptSource", map[string]any{"scriptId": item.scriptID})["scriptSource"]
				if got != source {
					t.Fatalf("navigation debugger changed source: %v", got)
				}
			}
		})
	}
}
