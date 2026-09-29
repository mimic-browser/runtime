package network

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/moreveal/mimic/internal/trace"
)

func TestCachedResponseRetainsBytesButUsesRetrievalTimingOwner(t *testing.T) {
	recorder := trace.New()
	recorder.Start()
	loader := NewLoader(testEnvironment, NewCookieStore(), recorder)
	u, _ := url.Parse("http://example.test/cached")
	request := Request{URL: u, Method: "GET", Initiator: Fetch, Headers: http.Header{}}
	loader.session.PutCached(request, Response{
		URL: u, Status: 200, Headers: http.Header{"Cache-Control": {"max-age=600"}}, Body: []byte("cached bytes"),
		Duration: time.Hour, Protocol: "HTTP/1.1",
		TransportTiming:      TransportTimingSnapshot{ConnectionID: "original", Phases: map[string]float64{"responseComplete": 3600000}},
		BrowserVisibleTiming: TransportTimingSnapshot{Phases: map[string]float64{"responseComplete": 3600000}},
	}, time.Now())
	for _, owner := range []string{"original-document", "replacement-document"} {
		request.ID, request.PerformanceOwner = owner, owner
		request.PerformanceStart = time.Unix(100, 0)
		response, err := loader.Load(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		if !response.FromCache || string(response.Body) != "cached bytes" || response.Duration == time.Hour || len(response.TransportTiming.Phases) != 0 {
			t.Fatalf("cache retrieval: %+v", response)
		}
		if response.Protocol != "HTTP/1.1" || response.TransportTiming.ConnectionID != "original" {
			t.Fatal("stored transport identity changed")
		}
		if response.BrowserVisibleTiming.Phases["responseComplete"] != float64(response.Duration)/float64(time.Millisecond) {
			t.Fatal("retrieval duration and completion phase disagree")
		}
	}
	count := 0
	for _, event := range recorder.Events() {
		if event.Kind == trace.Network && event.Name == "response" {
			count++
			if event.Data["performanceOwner"] != event.Data["id"] || event.Data["performanceStart"] != request.PerformanceStart {
				t.Fatalf("cached request lost owner stamp: %v", event.Data)
			}
		}
	}
	if count != 2 {
		t.Fatalf("response count = %d", count)
	}
}
