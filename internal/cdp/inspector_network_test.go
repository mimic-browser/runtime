package cdp

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/moreveal/mimic/internal/network"
)

func TestInspectorNetworkRedirectPostAndBodies(t *testing.T) {
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redirect":
			http.Redirect(w, r, "/text", http.StatusFound)
		case "/text":
			w.Header().Set("Content-Type", "text/plain")
			w.Header().Set("X-Inspector", "visible")
			_, _ = io.WriteString(w, "response text")
		case "/post":
			body, _ := io.ReadAll(r.Body)
			_, _ = w.Write(body)
		case "/binary":
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write([]byte{0xff, 0, 0x80, 1})
		}
	}))
	t.Cleanup(fixture.Close)
	s, addr := runningServer(t)
	w, sessionID := inspectorSession(t, s, addr)
	w.call(t, sessionID, "Network.enable", map[string]any{})
	for _, test := range []struct {
		path, method, body string
		redirects          int
	}{
		{"/redirect", "GET", "", 1}, {"/post", "POST", "payload Ω", 0}, {"/binary", "GET", "", 0},
	} {
		t.Run(test.path, func(t *testing.T) {
			u, _ := url.Parse(fixture.URL + test.path)
			id := "inspector" + test.path
			response, err := s.Page.Loader().Load(context.Background(), network.Request{ID: id, ContextID: s.Page.Top.ID, URL: u, Method: test.method, Body: []byte(test.body), Initiator: network.Fetch})
			if err != nil {
				t.Fatal(err)
			}
			request := w.event(t, "Network.requestWillBeSent")
			if request["requestId"] != id {
				t.Fatalf("request identity: %v", request)
			}
			for i := 0; i < test.redirects; i++ {
				redirect := w.event(t, "Network.requestWillBeSent")
				if redirect["requestId"] != id || redirect["redirectResponse"].(map[string]any)["status"] != float64(302) {
					t.Fatalf("redirect chain: %v", redirect)
				}
			}
			received := w.event(t, "Network.responseReceived")
			if received["requestId"] != id || received["response"].(map[string]any)["status"] != float64(200) {
				t.Fatalf("response: %v", received)
			}
			finished := w.event(t, "Network.loadingFinished")
			if finished["requestId"] != id {
				t.Fatalf("finished identity: %v", finished)
			}
			result := w.call(t, sessionID, "Network.getResponseBody", map[string]any{"requestId": id})
			body := []byte(result["body"].(string))
			if result["base64Encoded"] == true {
				body, err = base64.StdEncoding.DecodeString(string(body))
				if err != nil {
					t.Fatal(err)
				}
			}
			if !bytes.Equal(body, response.Body) {
				t.Fatalf("body corrupted: %x != %x", body, response.Body)
			}
			if test.body != "" {
				post := w.call(t, sessionID, "Network.getRequestPostData", map[string]any{"requestId": id})
				if post["postData"] != test.body {
					t.Fatalf("post body: %v", post)
				}
			}
		})
	}
	w.call(t, sessionID, "Network.disable", map[string]any{})
	for _, c := range s.clientSnapshot() {
		for _, client := range c.snapshot() {
			if client.id == sessionID {
				client.stateMu.RLock()
				retained := client.networkInspection != nil
				client.stateMu.RUnlock()
				if retained {
					t.Fatal("Network.disable retained inspector history")
				}
			}
		}
	}
}

func TestInspectorPostHistoryIsLazyAndBounded(t *testing.T) {
	s := &session{}
	s.rememberPostData("empty", "")
	if s.networkInspection != nil {
		t.Fatal("empty requests allocated body history")
	}
	for i := 0; i < 200; i++ {
		s.rememberPostData(string(rune(i)), string(bytes.Repeat([]byte{'x'}, 65536)))
	}
	history := s.networkInspection
	if len(history.postData) > 128 || history.postBytes > 4<<20 {
		t.Fatalf("unbounded post history: %d / %d", len(history.postData), history.postBytes)
	}
	s.rememberPostData("oversized", string(bytes.Repeat([]byte{'x'}, (1<<20)+1)))
	if _, ok := history.postData["oversized"]; ok {
		t.Fatal("oversized post retained")
	}
}

func TestInspectorNetworkFailureAndMissingBodies(t *testing.T) {
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	endpoint := fixture.URL
	fixture.Close()
	s, addr := runningServer(t)
	w, id := inspectorSession(t, s, addr)
	w.call(t, id, "Network.enable", map[string]any{})
	u, _ := url.Parse(endpoint)
	_, err := s.Page.Loader().Load(context.Background(), network.Request{ID: "inspector-failed", ContextID: s.Page.Top.ID, URL: u, Method: "GET", Initiator: network.Fetch})
	if err == nil {
		t.Fatal("closed endpoint unexpectedly succeeded")
	}
	request := w.event(t, "Network.requestWillBeSent")
	failed := w.event(t, "Network.loadingFailed")
	if request["requestId"] != "inspector-failed" || failed["requestId"] != "inspector-failed" || stringValue(failed["errorText"]) == "" {
		t.Fatalf("failure lost identity/diagnostic: %v %v", request, failed)
	}
	if reply := w.request(t, id, "Network.getResponseBody", map[string]any{"requestId": "inspector-failed"}); reply["error"] == nil {
		t.Fatal("failed request invented a response body")
	}
	if reply := w.request(t, id, "Network.getRequestPostData", map[string]any{"requestId": "unknown"}); reply["error"] == nil {
		t.Fatal("missing POST history invented a body")
	}
}
