// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestIsBotChallenge covers the interstitial detector's narrow gate.
func TestIsBotChallenge(t *testing.T) {
	cases := []struct {
		name   string
		status int
		hdr    http.Header
		body   string
		want   bool
	}{
		{"403 with marker", 403, nil, "Performing security verification", true},
		{"503 just a moment", 503, nil, "<title>Just a moment...</title>", true},
		{"403 cf-mitigated header", 403, http.Header{"Cf-Mitigated": {"challenge"}}, "whatever", true},
		{"200 mentioning verification is NOT a challenge", 200, nil, "our security verification process", false},
		{"403 without any marker", 403, nil, "plain forbidden", false},
		{"404 with marker text", 404, nil, "just a moment", false},
	}
	for _, c := range cases {
		if got := isBotChallenge(c.status, c.hdr, []byte(c.body)); got != c.want {
			t.Errorf("%s: isBotChallenge = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestGetRetriesOnBotChallenge covers the fetch fallback: a server that
// challenges the browser-like primary client but serves the real page to the
// plain fallback client yields the real content.
func TestGetRetriesOnBotChallenge(t *testing.T) {
	const real = `<html><head><title>Real</title></head><body>real content</body></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "BrowserUA" {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`<html><body>Performing security verification. Browser not supported.</body></html>`))
			return
		}
		_, _ = w.Write([]byte(real))
	}))
	defer srv.Close()

	e := &Engine{Client: &http.Client{}, UserAgent: "BrowserUA"}
	body, _, _, err := e.get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "real content") {
		t.Errorf("get returned %q, want the real content via the fallback client", body)
	}
}

// TestGetNoRetryOnNormalPage covers the common path: a normal 200 response is
// returned as-is, with no fallback request.
func TestGetNoRetryOnNormalPage(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte(`<html><body>ok</body></html>`))
	}))
	defer srv.Close()

	e := &Engine{Client: &http.Client{}, UserAgent: "BrowserUA"}
	body, _, _, err := e.get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "ok") {
		t.Errorf("body = %q, want ok", body)
	}
	if hits != 1 {
		t.Errorf("server hit %d times, want 1 (no fallback retry on a normal page)", hits)
	}
}

// TestGetKeepsChallengeWhenFallbackAlsoBlocked covers the case where the
// fallback is itself challenged: the original response is returned (best effort)
// rather than an error.
func TestGetKeepsChallengeWhenFallbackAlsoBlocked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`<html><body>Just a moment...</body></html>`))
	}))
	defer srv.Close()

	e := &Engine{Client: &http.Client{}, UserAgent: "BrowserUA"}
	body, _, _, err := e.get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "Just a moment") {
		t.Errorf("body = %q, want the original challenge (fallback also blocked)", body)
	}
}

// TestGetRequestError covers the transport-error path (an unroutable URL).
func TestGetRequestError(t *testing.T) {
	e := New()
	if _, _, _, err := e.get(context.Background(), "http://127.0.0.1:0"); err == nil {
		t.Error("expected a transport error for an unroutable URL")
	}
}
