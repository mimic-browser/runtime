package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/moreveal/mimic/internal/trace"
)

func execute(t *testing.T, r *replay, method, url, owner, initiator string) (string, error) {
	t.Helper()
	r.observe(trace.Event{Kind: trace.Network, Name: "request", Data: map[string]any{"id": "request", "method": method, "url": url, "context": owner, "initiator": initiator, "headers": map[string]string{}}})
	request, _ := http.NewRequest(method, url, nil)
	response, err := r.RoundTrip(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	return string(body), nil
}

func TestReplayAdvancesDocumentsAndMatchesMethodContext(t *testing.T) {
	c := &capture{TopContext: "saved-top", Fixtures: []*fixture{
		{Index: 0, Cycle: 1, Request: recordedRequest{URL: "https://example.test/", Method: "GET", Context: "saved-top", Initiator: "navigation"}, Body: []byte("first")},
		{Index: 1, Cycle: 1, Request: recordedRequest{URL: "https://example.test/shared", Method: "POST", Context: "saved-child", Initiator: "xhr"}, Body: []byte("child-post")},
		{Index: 2, Cycle: 1, Request: recordedRequest{URL: "https://example.test/shared", Method: "GET", Context: "saved-top", Initiator: "fetch"}, Body: []byte("top-get")},
		{Index: 3, Cycle: 2, Request: recordedRequest{URL: "https://example.test/", Method: "GET", Context: "saved-top", Initiator: "navigation"}, Body: []byte("second")},
		{Index: 4, Cycle: 3, Request: recordedRequest{URL: "https://example.test/", Method: "GET", Context: "saved-top", Initiator: "navigation"}, Body: []byte("third")},
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := newReplay(c, cancel, nil, 20)
	checks := []struct{ method, url, owner, initiator, want string }{
		{"GET", "https://example.test/", "top", "navigation", "first"},
		{"GET", "https://example.test/shared", "top", "fetch", "top-get"},
		{"POST", "https://example.test/shared", "child", "xhr", "child-post"},
		{"GET", "https://example.test/", "top", "navigation", "second"},
		{"GET", "https://example.test/", "top", "navigation", "third"},
	}
	for _, check := range checks {
		got, err := execute(t, r, check.method, check.url, check.owner, check.initiator)
		if err != nil || got != check.want {
			t.Fatalf("got %q, %v, want %q", got, err, check.want)
		}
	}
	if _, err := execute(t, r, "GET", "https://example.test/", "top", "navigation"); err == nil || !strings.Contains(err.Error(), "queue-exhausted") {
		t.Fatalf("unexpected boundary: %v", err)
	}
	if ctx.Err() != context.Canceled {
		t.Fatal("queue exhaustion must cancel, never reuse the first GET")
	}
}

func TestReplayPreservesFailureAndSeparatesLocalRecords(t *testing.T) {
	c := &capture{TopContext: "top", Fixtures: []*fixture{
		{Index: 0, Cycle: 1, Request: recordedRequest{URL: "https://example.test/", Method: "GET", Context: "top", Initiator: "navigation"}},
		{Index: 1, Cycle: 1, Local: "cache", Request: recordedRequest{URL: "https://example.test/cached", Method: "GET", Context: "top", Initiator: "script"}},
		{Index: -1, Cycle: 1, Failure: "recorded DNS failure", Request: recordedRequest{URL: "https://offline.invalid/", Method: "GET", Context: "top", Initiator: "fetch"}},
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := newReplay(c, cancel, nil, 20)
	execute(t, r, "GET", "https://example.test/", "current", "navigation")
	if _, err := execute(t, r, "GET", "https://offline.invalid/", "current", "fetch"); err == nil || err.Error() != "recorded DNS failure" {
		t.Fatalf("failure altered: %v", err)
	}
	if ctx.Err() != nil {
		t.Fatal("recorded network failure is not the capture boundary")
	}
	if _, err := execute(t, r, "GET", "https://example.test/cached", "current", "script"); err == nil {
		t.Fatal("cache projection must not be served as a wire response")
	}
	if !r.used[2] || r.used[1] {
		t.Fatal("incorrect occurrence consumption")
	}
}

func TestReplayDynamicMappingIsOptIn(t *testing.T) {
	c := &capture{TopContext: "top", Fixtures: []*fixture{{Index: 0, Cycle: 1, Request: recordedRequest{URL: "https://example.test/id/saved/page", Method: "GET", Context: "top", Initiator: "navigation"}, Body: []byte("value=saved")}}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := newReplay(c, cancel, nil, 10)
	if _, err := execute(t, r, "GET", "https://example.test/id/new/page", "top", "navigation"); err == nil {
		t.Fatal("unconfigured substitution")
	}
	if ctx.Err() == nil {
		t.Fatal("missing boundary")
	}
	_, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	r = newReplay(c, cancel2, regexp.MustCompile(`/id/([^/]+)/`), 10)
	if got, err := execute(t, r, "GET", "https://example.test/id/new/page", "top", "navigation"); err != nil || got != "value=new" {
		t.Fatalf("got %q %v", got, err)
	}
}

func TestReadCaptureAccountsForCriticalRetry(t *testing.T) {
	dir := t.TempDir()
	request := func(sequence uint64) trace.Event {
		return trace.Event{Sequence: sequence, Kind: trace.Network, Name: "request", Data: map[string]any{"id": "one", "url": "https://example.test/", "method": "GET", "context": "top", "initiator": "navigation", "postData": "", "headers": map[string]string{}}}
	}
	events := []trace.Event{request(1), {Sequence: 2, Kind: trace.Network, Name: "criticalClientHintsRestart", Data: map[string]any{"id": "one"}}, request(3), {Sequence: 4, Kind: trace.Network, Name: "response", Data: map[string]any{"id": "one", "fromCache": false, "synthetic": false}}}
	data, _ := json.Marshal(map[string]any{"result": map[string]any{"events": events}})
	os.WriteFile(filepath.Join(dir, "trace.json"), data, 0600)
	os.WriteFile(filepath.Join(dir, "root-one.json"), []byte(`{"response":{"status":403,"url":"https://example.test/","headers":{}},"result":{"body":"body"}}`), 0600)
	c, err := readCapture(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Fixtures) != 2 || !c.Fixtures[0].CriticalRetryCopy || c.Fixtures[1].CriticalRetryCopy || c.Fixtures[0].Cycle != 1 || c.Fixtures[1].Cycle != 1 {
		t.Fatalf("bad duplicate accounting: %+v", c.Fixtures)
	}
	if !strings.Contains(c.DocumentURL, "example.test") {
		t.Fatal("missing document")
	}
}

func TestReadCaptureDoesNotInventMissingBodies(t *testing.T) {
	for _, synthetic := range []bool{false, true} {
		t.Run(fmt.Sprint(synthetic), func(t *testing.T) {
			dir := t.TempDir()
			events := []trace.Event{{Sequence: 1, Kind: trace.Network, Name: "request", Data: map[string]any{"id": "missing", "url": "https://example.test/", "method": "GET", "context": "top", "initiator": "navigation"}}, {Sequence: 2, Kind: trace.Network, Name: "response", Data: map[string]any{"id": "missing", "synthetic": synthetic}}}
			data, _ := json.Marshal(map[string]any{"result": map[string]any{"events": events}})
			if err := os.WriteFile(filepath.Join(dir, "trace.json"), data, 0600); err != nil {
				t.Fatal(err)
			}
			body := `{"response":{"url":"https://example.test/","status":200},"error":{"code":-32000,"message":"unknown request id"}}`
			if err := os.WriteFile(filepath.Join(dir, "root-missing.json"), []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			c, err := readCapture(dir)
			if !synthetic {
				if err == nil {
					t.Fatal("missing transport body accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(c.Fixtures) != 1 || !c.Fixtures[0].UnavailableBody || c.Fixtures[0].Local != "synthetic" {
				t.Fatal("missing synthetic body was not recorded")
			}
		})
	}
}

func TestReadCaptureResponseOnlyDataIsLocal(t *testing.T) {
	for _, localURL := range []string{"data:text/plain;base64,aW1hZ2U=", "https://example.test/orphan", "blob:https://example.test/orphan"} {
		t.Run(localURL, func(t *testing.T) {
			dir := t.TempDir()
			events := []trace.Event{
				{Sequence: 1, Kind: trace.Network, Name: "request", Data: map[string]any{"id": "document", "url": "https://example.test/", "method": "GET", "context": "top", "initiator": "navigation"}},
				{Sequence: 2, Kind: trace.Network, Name: "response", Data: map[string]any{"id": "document"}},
				{Sequence: 3, Kind: trace.Network, Name: "response", Data: map[string]any{"id": "local", "url": localURL, "context": "top", "initiator": "image", "synthetic": true, "status": 200}},
			}
			data, _ := json.Marshal(map[string]any{"result": map[string]any{"events": events}})
			if err := os.WriteFile(filepath.Join(dir, "trace.json"), data, 0600); err != nil {
				t.Fatal(err)
			}
			for id, url := range map[string]string{"document": "https://example.test/", "local": localURL} {
				body, _ := json.Marshal(map[string]any{"response": map[string]any{"url": url, "status": 200}, "result": map[string]any{"body": "image"}})
				if err := os.WriteFile(filepath.Join(dir, "root-"+id+".json"), body, 0600); err != nil {
					t.Fatal(err)
				}
			}
			c, err := readCapture(dir)
			if !strings.HasPrefix(localURL, "data:") {
				if err == nil || !strings.Contains(err.Error(), "has no request") {
					t.Fatalf("orphan request must remain a boundary: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			f := c.Fixtures[1]
			if !f.ResponseOnlyLocal || f.Local != "synthetic" || f.Request.Method != "" || f.Request.Sequence != 3 || f.Cycle != 1 {
				t.Fatalf("local response invented a wire request: %+v", f)
			}
			_, cancel := context.WithCancel(context.Background())
			defer cancel()
			r := newReplay(c, cancel, nil, 10)
			execute(t, r, "GET", "https://example.test/", "current", "navigation")
			wrong := events[2]
			wrong.Data = map[string]any{"id": "wrong", "url": "data:text/plain;base64,b3RoZXI=", "context": "current", "initiator": "image", "synthetic": true, "status": 200}
			r.observe(wrong)
			if r.used[1] {
				t.Fatal("different data bytes consumed the local fixture")
			}
			correct := events[2]
			correct.Data = map[string]any{"id": "correct", "url": localURL, "context": "current", "initiator": "image", "synthetic": true, "status": 200}
			r.observe(correct)
			if !r.used[1] || r.events[len(r.events)-1]["statusEqual"] != true {
				t.Fatal("response-only data load was not accounted for")
			}
		})
	}
}
