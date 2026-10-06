package workload

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestAmbiguousEvidenceIsPreservedButNeverServed(t *testing.T) {
	var counter atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if counter.Add(1) == 1 {
			io.WriteString(w, "first")
		} else {
			io.WriteString(w, "second")
		}
	}))
	defer server.Close()
	record, _ := New(true, "", nil)
	defer record.Close()
	transport := record.Wrap(http.DefaultTransport)
	readResponse(t, transport, server.URL)
	readResponse(t, transport, server.URL)
	path := filepath.Join(t.TempDir(), "evidence.mcap")
	if err := record.SaveEvidence(path); err == nil {
		t.Fatal("ambiguous recording was advertised as supported")
	}
	replay, err := New(false, path, nil)
	if err != nil {
		t.Fatal("complete evidence was not preserved", err)
	}
	defer replay.Close()
	request, _ := http.NewRequest("GET", server.URL, nil)
	if _, err := replay.Wrap(&forbiddenTransport{}).RoundTrip(request); err == nil {
		t.Fatal("ambiguous response was returned")
	}
	if len(replay.Snapshot().CaptureMisses) != 1 {
		t.Fatal("ambiguity was not classified as uncovered")
	}
}
