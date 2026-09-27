package network

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/moreveal/mimic/internal/trace"
)

func TestXHRSharedCORSPolicy(t *testing.T) {
	var actual, preflight atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/deny" {
			w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
		}
		if r.Method == "OPTIONS" {
			preflight.Add(1)
			w.Header().Set("Access-Control-Allow-Methods", "PUT")
			w.Header().Set("Access-Control-Allow-Headers", "X-Probe")
			return
		}
		actual.Add(1)
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Access-Control-Expose-Headers", "X-Visible")
		w.Header().Set("X-Visible", "visible")
		w.Header().Set("X-Hidden", "hidden")
		w.Header().Set("Set-Cookie", "private=secret; HttpOnly")
		w.Write([]byte("body"))
	}))
	defer server.Close()
	source, _ := url.Parse("http://source.test/document")
	for _, tc := range []struct {
		path, method string
		denied       bool
	}{
		{"/deny", "GET", true},
		{"/allow", "GET", false},
		{"/allow", "PUT", false},
	} {
		t.Run(tc.path+tc.method, func(t *testing.T) {
			u, _ := url.Parse(server.URL + tc.path)
			before := preflight.Load()
			res, err := NewLoader(testEnvironment, NewCookieStore(), trace.New()).Load(context.Background(), Request{
				URL: u, SourceURL: source, Initiator: XHR, Mode: "cors", Credentials: "same-origin", Method: tc.method,
				Headers: http.Header{"X-Probe": {"probe"}},
			})
			if (err != nil) != tc.denied {
				t.Fatalf("CORS boundary: status=%d err=%v", res.Status, err)
			}
			if preflight.Load() != before+1 {
				t.Fatal("unsafe XHR author header did not preflight")
			}
			if err == nil && (res.Headers.Get("X-Visible") != "visible" || res.Headers.Get("X-Hidden") != "" || res.Headers.Get("Set-Cookie") != "") {
				t.Fatal("XHR response did not use the CORS header boundary")
			}
		})
	}
	if actual.Load() != 2 {
		t.Fatalf("denied preflight reached the actual endpoint: %d", actual.Load())
	}
}
