package workload

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestCaptureExtensionPreservesKnownBodiesAndAddsOnlyImmutableBranches(t *testing.T) {
	body := "baseline"
	immutable := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Date", "Mon, 05 Oct 2026 00:00:00 GMT")
		if r.URL.Path == "/branch.js" {
			if immutable {
				w.Header().Set("Cache-Control", "public,max-age=60,immutable")
			}
			io.WriteString(w, "optional branch")
			return
		}
		io.WriteString(w, body)
	}))
	defer server.Close()
	record := func(path string, branch bool) {
		s, _ := New(true, "", nil)
		defer s.Close()
		tr := s.Wrap(http.DefaultTransport)
		read := func(path string) {
			request, _ := http.NewRequest("GET", server.URL+path, nil)
			request.Header.Set("Accept", "*/*")
			response, err := tr.RoundTrip(request)
			if err != nil {
				t.Fatal(err)
			}
			io.Copy(io.Discard, response.Body)
			response.Body.Close()
		}
		read("/document")
		if branch {
			read("/branch.js")
			read("/branch.js")
		}
		if err := s.Save(path); err != nil {
			t.Fatal(err)
		}
	}
	dir := t.TempDir()
	base := filepath.Join(dir, "base.mcap")
	branch := filepath.Join(dir, "branch.mcap")
	out := filepath.Join(dir, "extended.mcap")
	record(base, false)
	record(branch, true)
	before, _ := FileSHA256(base)
	if added, err := ExtendCapture(base, branch, out); err != nil || added != 2 {
		t.Fatalf("extension: %d %v", added, err)
	}
	after, _ := FileSHA256(base)
	if before != after {
		t.Fatal("original evidence changed")
	}
	replay, err := New(false, out, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer replay.Close()
	tr := replay.Wrap(&forbiddenTransport{})
	for _, path := range []string{"/document", "/branch.js", "/branch.js"} {
		request, _ := http.NewRequest("GET", server.URL+path, nil)
		request.Header.Set("Accept", "*/*")
		response, err := tr.RoundTrip(request)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, response.Body)
		response.Body.Close()
	}
	if len(replay.capture.Parents) != 2 {
		t.Fatal("parent provenance missing")
	}
	if _, err := ExtendCapture(base, branch, base); err == nil {
		t.Fatal("parent overwrite accepted")
	}
	body = "changed state"
	record(branch, true)
	if _, err := ExtendCapture(base, branch, filepath.Join(dir, "changed.mcap")); err == nil {
		t.Fatal("changed existing state was merged")
	}
	body = "baseline"
	immutable = false
	record(branch, true)
	if _, err := ExtendCapture(base, branch, filepath.Join(dir, "private.mcap")); err == nil {
		t.Fatal("mutable new response was merged")
	}
}
