package network

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/moreveal/mimic/internal/state"
	"github.com/moreveal/mimic/internal/trace"
)

func TestFetchCORSResponsePolicy(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("Origin") != "http://source.test" && r.URL.Path != "/opaque" {
			t.Errorf("Origin=%q", r.Header.Get("Origin"))
		}
		switch r.URL.Path {
		case "/allow", "/credentials":
			w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
		case "/wildcard", "/expose":
			w.Header().Set("Access-Control-Allow-Origin", "*")
		case "/multiple":
			w.Header().Add("Access-Control-Allow-Origin", "*")
			w.Header().Add("Access-Control-Allow-Origin", "http://source.test")
		}
		if r.URL.Path == "/credentials" {
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		if r.URL.Path == "/expose" {
			w.Header().Set("Access-Control-Expose-Headers", "X-Visible")
		}
		w.Header().Set("X-Visible", "visible")
		w.Header().Set("X-Hidden", "hidden")
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Set-Cookie", "hidden=1")
		_, _ = w.Write([]byte("body"))
	}))
	defer server.Close()
	source, _ := url.Parse("http://source.test/page")
	for _, tc := range []struct {
		path, mode, credentials string
		denied                  bool
	}{
		{"/deny", "cors", "same-origin", true}, {"/allow", "cors", "same-origin", false},
		{"/wildcard", "cors", "omit", false}, {"/wildcard", "cors", "include", true},
		{"/allow", "cors", "include", true}, {"/credentials", "cors", "include", false},
		{"/multiple", "cors", "omit", true}, {"/expose", "cors", "omit", false},
		{"/opaque", "no-cors", "omit", false}, {"/allow", "same-origin", "omit", true},
	} {
		t.Run(tc.path+tc.mode+tc.credentials, func(t *testing.T) {
			loader := NewLoader(testEnvironment, NewCookieStore(), trace.New())
			target, _ := url.Parse(server.URL + tc.path)
			before := requests.Load()
			res, err := loader.Load(context.Background(), Request{URL: target, SourceURL: source, Initiator: Fetch, Mode: tc.mode, Credentials: tc.credentials})
			if (err != nil) != tc.denied {
				t.Fatalf("response=%+v err=%v", res, err)
			}
			if tc.mode == "same-origin" && requests.Load() != before {
				t.Fatal("same-origin request reached server")
			}
			if err != nil {
				return
			}
			if tc.mode == "no-cors" {
				if res.Type != "opaque" || res.Status != 0 || res.URL != nil || len(res.Body) != 0 || len(res.Headers) != 0 {
					t.Fatalf("opaque leaked: %+v", res)
				}
				return
			}
			if res.Type != "cors" || string(res.Body) != "body" || res.Headers.Get("Content-Type") != "text/plain" || res.Headers.Get("X-Hidden") != "" || res.Headers.Get("Set-Cookie") != "" {
				t.Fatalf("filtered response=%+v", res)
			}
			if (res.Headers.Get("X-Visible") != "") != (tc.path == "/expose") {
				t.Fatal("expose headers")
			}
		})
	}
}

func TestFetchCORSPreflight(t *testing.T) {
	var actual, preflights atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
		if r.Method == "OPTIONS" {
			preflights.Add(1)
			if r.Header.Get("Cookie") != "" || r.Header.Get("Access-Control-Request-Method") != "PUT" || r.Header.Get("Access-Control-Request-Headers") != "x-test" {
				t.Errorf("preflight headers=%v", r.Header)
			}
			if r.URL.Path == "/allow" {
				w.Header().Set("Access-Control-Allow-Methods", "PUT")
				w.Header().Set("Access-Control-Allow-Headers", "X-Test")
			}
			return
		}
		actual.Add(1)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()
	source, _ := url.Parse("http://source.test/")
	for _, path := range []string{"/deny", "/allow"} {
		u, _ := url.Parse(server.URL + path)
		_, err := NewLoader(testEnvironment, NewCookieStore(), trace.New()).Load(context.Background(), Request{URL: u, SourceURL: source, Initiator: Fetch, Mode: "cors", Credentials: "omit", Method: "PUT", Headers: http.Header{"X-Test": {""}}})
		if (err != nil) != (path == "/deny") {
			t.Fatalf("%s: %v", path, err)
		}
	}
	if preflights.Load() != 2 || actual.Load() != 1 {
		t.Fatalf("preflight=%d actual=%d", preflights.Load(), actual.Load())
	}
}

func TestFetchCORSPreflightUsesBrowserLanguageAndOmitsClientHints(t *testing.T) {
	env := testEnvironment()
	env.Locale.Languages = []string{"ru-RU", "ru", "en-US", "en"}
	env.Locale.ReduceAcceptLanguage = true
	preflightHeaders := make(chan http.Header, 1)
	actualHeaders := make(chan http.Header, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
		if r.Method == http.MethodOptions {
			preflightHeaders <- r.Header.Clone()
			w.Header().Set("Access-Control-Allow-Methods", "POST")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			return
		}
		actualHeaders <- r.Header.Clone()
	}))
	defer server.Close()
	source, _ := url.Parse("http://source.test/")
	target, _ := url.Parse(server.URL)
	_, err := NewLoader(func() state.Environment { return env }, NewCookieStore(), trace.New()).Load(
		context.Background(), Request{
			URL: target, SourceURL: source, Initiator: Fetch, Mode: "cors",
			Method: http.MethodPost, Headers: http.Header{"Content-Type": {"application/octet-stream"}},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	preflight := <-preflightHeaders
	actual := <-actualHeaders
	if got, want := preflight.Get("Accept-Language"), "ru-RU,ru;q=0.9,en-US;q=0.8,en;q=0.7"; got != want {
		t.Fatalf("preflight Accept-Language = %q, want %q", got, want)
	}
	for _, header := range []string{"Sec-Ch-Ua", "Sec-Ch-Ua-Mobile", "Sec-Ch-Ua-Platform"} {
		if got := preflight.Get(header); got != "" {
			t.Fatalf("preflight %s = %q", header, got)
		}
	}
	if got, want := actual.Get("Accept-Language"), "ru-RU,ru;q=0.9"; got != want {
		t.Fatalf("actual Accept-Language = %q, want %q", got, want)
	}
	if actual.Get("Sec-Ch-Ua") == "" {
		t.Fatal("actual request lost document client hints")
	}
}

func TestFetchCORSSafelistedHeaders(t *testing.T) {
	for _, value := range []string{"text/plain", "application/x-www-form-urlencoded;charset=UTF-8", "multipart/form-data; boundary=abc"} {
		if !corsSafeHeader("Content-Type", value) {
			t.Fatal(value)
		}
	}
	for _, value := range []string{"application/json", "text/plain; bad=\"x\"", strings.Repeat("x", 129)} {
		if corsSafeHeader("Content-Type", value) {
			t.Fatal(value)
		}
	}
}
