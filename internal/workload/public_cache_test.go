package workload

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestReplayPublicCookieEquivalencePreservesCoverage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public,max-age=31536000,immutable")
		io.WriteString(w, "application bundle")
	}))
	record, _ := New(true, "", nil)
	defer record.Close()
	transport := record.Wrap(http.DefaultTransport)
	request, _ := http.NewRequest("GET", server.URL+"/bundle.js", nil)
	request.Header.Set("Cookie", "analytics=recorded")
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	path := filepath.Join(t.TempDir(), "environment.mcap")
	if err := record.Save(path); err != nil {
		t.Fatal(err)
	}
	server.Close()
	replay, err := New(false, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer replay.Close()
	offline := replay.Wrap(&forbiddenTransport{})
	request.Header.Set("Cookie", "analytics=different")
	response, err = offline.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if len(replay.Snapshot().CaptureMisses) != 0 || replay.Snapshot().Active != 0 {
		t.Fatal("equivalent request leaked activity or became uncovered")
	}
	if _, err := offline.RoundTrip(request); err == nil {
		t.Fatal("equivalence bypassed occurrence count")
	}
}

func TestPublicCookieEquivalenceIsNarrow(t *testing.T) {
	request, _ := http.NewRequest("GET", "https://app.test/bundle.js", nil)
	request.Header.Set("Cookie", "new=1")
	entry := &entry{emptyRequestBody: true, URL: request.URL.String(), RequestHost: request.Host, Method: "GET", Status: 200, Headers: http.Header{"Cache-Control": {"public,max-age=60"}}, RequestHeaders: http.Header{"Cookie": {"old=1"}}}
	if !freshRepresentationEquivalent(entry, request) {
		t.Fatal("explicit public freshness was rejected")
	}
	for _, directive := range []string{"private,max-age=60", "public,no-store,max-age=60", "public,max-age=0", "max-age=60"} {
		entry.Headers.Set("Cache-Control", directive)
		if freshRepresentationEquivalent(entry, request) {
			t.Fatalf("unsafe directive accepted: %s", directive)
		}
	}
	entry.Headers.Set("Cache-Control", "public,max-age=60")
	entry.Headers.Set("Vary", "Cookie")
	if freshRepresentationEquivalent(entry, request) {
		t.Fatal("Cookie variant ignored")
	}
	entry.Headers.Del("Vary")
	entry.Headers.Set("Vary", "Accept")
	request.Header.Set("Accept", "text/html")
	if freshRepresentationEquivalent(entry, request) {
		t.Fatal("non-cookie header ignored")
	}
	request.Header.Del("Accept")
	entry.Headers.Del("Vary")
	entry.Headers.Set("Set-Cookie", "session=changed")
	if freshRepresentationEquivalent(entry, request) {
		t.Fatal("response side effect ignored")
	}
}

func TestFreshRepresentationRequiresEmptyUnauthenticatedRequest(t *testing.T) {
	request, _ := http.NewRequest("GET", "https://app.test/resource", nil)
	e := &entry{emptyRequestBody: true, URL: request.URL.String(), RequestHost: request.Host, Method: "GET", Status: 200, Headers: http.Header{"Cache-Control": {"public,max-age=60,immutable"}}, RequestHeaders: http.Header{"Accept": {"*/*"}}}
	if !freshRepresentationEquivalent(e, request) {
		t.Fatal("positive empty request rejected")
	}
	e.emptyRequestBody = false
	if freshRepresentationEquivalent(e, request) {
		t.Fatal("recorded GET payload ignored")
	}
	e.emptyRequestBody = true
	e.RequestHeaders.Set("Authorization", "Bearer secret")
	if freshRepresentationEquivalent(e, request) {
		t.Fatal("recorded authorization ignored")
	}
}
