package workload

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/moreveal/mimic/internal/network"
	"github.com/moreveal/mimic/internal/state"
	"github.com/moreveal/mimic/internal/trace"
)

type forbiddenTransport struct{ calls atomic.Int64 }

func (f *forbiddenTransport) RoundTrip(*http.Request) (*http.Response, error) {
	f.calls.Add(1)
	panic("replay reached live transport")
}

func readResponse(t *testing.T, transport network.Transport, target string) string {
	t.Helper()
	r, _ := http.NewRequest("GET", target, nil)
	response, err := transport.RoundTrip(r)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestConcurrentIdenticalRequestsRetainSeparateOccurrences(t *testing.T) {
	var arrived atomic.Int64
	ready := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if arrived.Add(1) == 2 {
			close(ready)
		}
		<-ready
		w.Header().Set("Date", "Mon, 05 Oct 2026 00:00:00 GMT")
		io.WriteString(w, "same representation")
	}))
	defer server.Close()
	record, err := New(true, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer record.Close()
	transport := record.Wrap(http.DefaultTransport)
	results := make(chan error, 2)
	for range 2 {
		go func() {
			request, _ := http.NewRequest("GET", server.URL+"/same", nil)
			response, err := transport.RoundTrip(request)
			if err == nil {
				_, err = io.Copy(io.Discard, response.Body)
				response.Body.Close()
			}
			results <- err
		}()
	}
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(t.TempDir(), "concurrent.mcap")
	if err := record.Save(path); err != nil {
		t.Fatal(err)
	}
	replay, err := New(false, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer replay.Close()
	offline := replay.Wrap(&forbiddenTransport{})
	for range 2 {
		if body := readResponse(t, offline, server.URL+"/same"); body != "same representation" {
			t.Fatal(body)
		}
	}
	request, _ := http.NewRequest("GET", server.URL+"/same", nil)
	if _, err := offline.RoundTrip(request); err == nil {
		t.Fatal("concurrent recording lost its occurrence quota")
	}
}

func TestCaptureIndependentCursorsAndContextIsolation(t *testing.T) {
	var count atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		w.Header().Add("Set-Cookie", "a=1")
		w.Header().Add("Set-Cookie", "b=2")
		w.Header().Set("X-Resource", r.URL.Path)
		io.WriteString(w, r.URL.Path)
	}))
	defer server.Close()
	record, _ := New(true, "", nil)
	t.Cleanup(func() { record.Close() })
	a := record.Wrap(http.DefaultTransport)
	b := record.Wrap(http.DefaultTransport)
	readResponse(t, a, server.URL+"/discard")
	readResponse(t, a, server.URL+"/repeat")
	readResponse(t, a, server.URL+"/repeat")
	readResponse(t, b, server.URL+"/other")
	path := filepath.Join(t.TempDir(), "capture.mcap")
	if err := record.Save(path); err != nil {
		t.Fatal(err)
	}
	server.Close()
	replay, err := New(false, path, nil)
	if replay != nil {
		t.Cleanup(func() { replay.Close() })
	}
	if err != nil {
		t.Fatal(err)
	}
	base := &forbiddenTransport{}
	ra := replay.Wrap(base)
	rb := replay.Wrap(base)
	// Skipping an earlier resource does not shift another request's occurrences.
	for range 2 {
		r, _ := http.NewRequest("GET", server.URL+"/repeat", nil)
		response, err := ra.RoundTrip(r)
		if err != nil {
			t.Fatal(err)
		}
		if response.Header.Get("X-Resource") != "/repeat" || len(response.Header.Values("Set-Cookie")) != 2 {
			t.Fatal(response.Header)
		}
		io.Copy(io.Discard, response.Body)
		response.Body.Close()
	}
	if got := readResponse(t, rb, server.URL+"/other"); got != "/other" {
		t.Fatal(got)
	}
	r, _ := http.NewRequest("GET", server.URL+"/repeat", nil)
	if _, err := ra.RoundTrip(r); err == nil {
		t.Fatal("exhausted request passed")
	}
	r, _ = http.NewRequest("GET", server.URL+"/unknown", nil)
	if _, err := rb.RoundTrip(r); err == nil {
		t.Fatal("unknown request passed")
	}
	if base.calls.Load() != 0 || len(replay.Snapshot().CaptureMisses) != 2 {
		t.Fatal(replay.Snapshot())
	}
}

func TestCaptureLoaderPreservesRedirectCookieAndEncodedBody(t *testing.T) {
	var encoded bytes.Buffer
	g := gzip.NewWriter(&encoded)
	g.Write([]byte("decoded environment"))
	g.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			w.Header().Add("Set-Cookie", "session=yes; Path=/")
			w.Header().Set("Location", "/final")
			w.WriteHeader(302)
			return
		}
		if !strings.Contains(r.Header.Get("Cookie"), "session=yes") {
			t.Error("redirect lost cookie")
		}
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Type", "text/plain")
		w.Write(encoded.Bytes())
	}))
	defer server.Close()
	client := &http.Client{Transport: &http.Transport{DisableCompression: true}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	record, _ := New(true, "", nil)
	t.Cleanup(func() { record.Close() })
	load := func(session *Session, base network.Transport) network.Response {
		env := state.ChromeDesktopWindows(state.Product{Name: "Chrome", Version: "152.0.0.0", FullVersion: "152.0.7977.82"})
		loader := network.NewLoader(func() state.Environment { return env }, network.NewCookieStore(), trace.New())
		defer loader.CloseResponseBodies()
		defer loader.CloseOwnedTransport()
		loader.SetTransport(session.Wrap(base))
		target, _ := url.Parse(server.URL + "/start")
		response, err := loader.Load(context.Background(), network.Request{URL: target, Initiator: network.Navigation})
		if err != nil {
			t.Fatal(err)
		}
		return response
	}
	if got := string(load(record, network.HTTPTransport{Client: client}).Body); got != "decoded environment" {
		t.Fatal(got)
	}
	path := filepath.Join(t.TempDir(), "capture.mcap")
	if err := record.Save(path); err != nil {
		t.Fatal(err)
	}
	server.Close()
	replay, err := New(false, path, nil)
	if replay != nil {
		t.Cleanup(func() { replay.Close() })
	}
	if err != nil {
		t.Fatal(err)
	}
	if got := string(load(replay, &forbiddenTransport{}).Body); got != "decoded environment" {
		t.Fatal(got)
	}
	if replay.Snapshot().EncodedBodyBytes != int64(encoded.Len()) {
		t.Fatal(replay.Snapshot())
	}
}

func TestCaptureRejectsPartialAndUnsupported(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "abcdef") }))
	defer server.Close()
	record, _ := New(true, "", nil)
	t.Cleanup(func() { record.Close() })
	transport := record.Wrap(http.DefaultTransport)
	r, _ := http.NewRequest("GET", server.URL, nil)
	response, err := transport.RoundTrip(r)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Read(make([]byte, 1))
	response.Body.Close()
	if err := record.Save(filepath.Join(t.TempDir(), "bad.mcap")); err == nil {
		t.Fatal("partial capture admitted")
	}
	if err := transport.(interface{ RejectUnsupported(string) error }).RejectUnsupported("WebSocket"); err == nil {
		t.Fatal("socket admitted")
	}
}

func TestConcurrentIdenticalRequestsAreAmbiguous(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "body") }))
	defer server.Close()
	record, _ := New(true, "", nil)
	t.Cleanup(func() { record.Close() })
	transport := record.Wrap(http.DefaultTransport)
	r, _ := http.NewRequest("GET", server.URL, nil)
	first, err := transport.RoundTrip(r)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Body.Close()
	second, err := transport.RoundTrip(r)
	if err != nil {
		t.Fatal("recording changed live behavior", err)
	}
	second.Body.Close()
	if len(record.Snapshot().Unsupported) == 0 {
		t.Fatal("unsupported capture not diagnosed")
	}
}

func TestCaptureStreamsToDiskAndReplayOpensBodiesLazily(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, strings.Repeat("x", 1<<20)) }))
	defer server.Close()
	record, _ := New(true, "", nil)
	defer record.Close()
	tr := record.Wrap(http.DefaultTransport)
	request, _ := http.NewRequest("GET", server.URL, nil)
	response, err := tr.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 2048)
	n, err := response.Body.Read(buffer)
	if err != nil || n == 0 {
		t.Fatalf("read: %d %v", n, err)
	}
	stat, err := record.spool.Stat()
	if err != nil || stat.Size() != int64(n) {
		t.Fatalf("recorded bytes were not immediately written: %v %v", stat, err)
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	path := filepath.Join(t.TempDir(), "disk.mcap")
	if err := record.Save(path); err != nil {
		t.Fatal(err)
	}
	replay, err := New(false, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer replay.Close()
	if replay.archive == nil || replay.capture.Entries[0].file == nil {
		t.Fatal("missing lazy archive")
	}
	if replay.Snapshot().EncodedBodyBytes != 0 {
		t.Fatal("replay acquired a body before a request")
	}
	spoolPath := record.spool.Name()
	record.Close()
	if _, err := os.Stat(spoolPath); !os.IsNotExist(err) {
		t.Fatal("spool leaked")
	}
}

func TestExplicitVolatileQueryMatchingAndOrdinaryMiss(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "stable payload") }))
	defer server.Close()
	record, _ := New(true, "", nil)
	defer record.Close()
	if err := record.SetVolatileQuery([]string{"timestamp"}); err != nil {
		t.Fatal(err)
	}
	readResponse(t, record.Wrap(http.DefaultTransport), server.URL+"/asset?id=product-1&timestamp=100")
	path := filepath.Join(t.TempDir(), "query.mcap")
	if err := record.Save(path); err != nil {
		t.Fatal(err)
	}
	server.Close()
	replay, err := New(false, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer replay.Close()
	tr := replay.Wrap(&forbiddenTransport{})
	if got := readResponse(t, tr, server.URL+"/asset?id=product-1&timestamp=999"); got != "stable payload" {
		t.Fatal(got)
	}
	r, _ := http.NewRequest("GET", server.URL+"/asset?id=product-2&timestamp=999", nil)
	if _, err := tr.RoundTrip(r); err == nil {
		t.Fatal("semantic query change silently matched")
	}
	metrics := replay.Snapshot()
	if len(metrics.CaptureMisses) != 1 || len(metrics.Violations) != 0 {
		t.Fatal("capture miss mislabeled as semantic failure", metrics)
	}
}

type cancelTransport struct{ started chan struct{} }

func (t cancelTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if t.started != nil {
		close(t.started)
	}
	<-r.Context().Done()
	return nil, r.Context().Err()
}
func TestCaptureCancellationWaitsForBrowserOwnershipOnReplay(t *testing.T) {
	record, _ := New(true, "", nil)
	defer record.Close()
	started := make(chan struct{})
	tr := record.Wrap(cancelTransport{started: started})
	ctx, cancel := context.WithCancel(context.Background())
	r, _ := http.NewRequestWithContext(ctx, "GET", "https://example.test/canceled", nil)
	recordedDone := make(chan error, 1)
	go func() { _, err := tr.RoundTrip(r); recordedDone <- err }()
	<-started
	cancel()
	if err := <-recordedDone; err != context.Canceled {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "cancel.mcap")
	if err := record.Save(path); err != nil {
		t.Fatal(err)
	}
	replay, err := New(false, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer replay.Close()
	tr = replay.Wrap(&forbiddenTransport{})
	ctx, cancel = context.WithCancel(context.Background())
	r, _ = http.NewRequestWithContext(ctx, "GET", "https://example.test/canceled", nil)
	done := make(chan error, 1)
	go func() { _, err := tr.RoundTrip(r); done <- err }()
	select {
	case <-done:
		t.Fatal("recorded cancellation replayed before actual cancellation")
	default:
	}
	cancel()
	if err := <-done; err != context.Canceled {
		t.Fatal(err)
	}
}

func TestCaptureDiskFailureDoesNotChangeLiveResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "live bytes") }))
	defer server.Close()
	record, _ := New(true, "", nil)
	defer record.Close()
	record.spool.Close()
	if got := readResponse(t, record.Wrap(http.DefaultTransport), server.URL); got != "live bytes" {
		t.Fatal(got)
	}
	if len(record.Snapshot().Violations) == 0 {
		t.Fatal("disk failure was hidden")
	}
	if err := record.Save(filepath.Join(t.TempDir(), "bad.mcap")); err == nil {
		t.Fatal("failed disk capture admitted")
	}
}

func TestDifferentRepeatedResponsesRejectCaptureWithoutChangingLiveRun(t *testing.T) {
	var count atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if count.Add(1) == 1 {
			io.WriteString(w, "first")
		} else {
			io.WriteString(w, "second")
		}
	}))
	defer server.Close()
	record, _ := New(true, "", nil)
	defer record.Close()
	tr := record.Wrap(http.DefaultTransport)
	if got := readResponse(t, tr, server.URL); got != "first" {
		t.Fatal(got)
	}
	if got := readResponse(t, tr, server.URL); got != "second" {
		t.Fatal(got)
	}
	if err := record.Save(filepath.Join(t.TempDir(), "ambiguous.mcap")); err == nil {
		t.Fatal("ambiguous environment admitted")
	}
}

func TestUnrecordableLiveRequestPreservesBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { body, _ := io.ReadAll(r.Body); w.Write(body) }))
	defer server.Close()
	session, _ := New(true, "", nil)
	defer session.Close()
	request, _ := http.NewRequest("POST", server.URL, io.NopCloser(strings.NewReader("ordinary streaming input")))
	response, err := session.Wrap(http.DefaultTransport).RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || string(body) != "ordinary streaming input" {
		t.Fatalf("recording changed live semantics: %q %v", body, err)
	}
	if len(session.Snapshot().Unsupported) == 0 {
		t.Fatal("unrecordable stream accepted as replay evidence")
	}
	if err := session.Save(filepath.Join(t.TempDir(), "invalid.mcap")); err == nil {
		t.Fatal("unrecordable evidence published")
	}
}
