package workload

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestCapturedResponsePrefixWaitsForOwnerCancellation(t *testing.T) {
	const prefix = "received prefix"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Recorded", "headers")
		io.WriteString(w, prefix)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	record, err := New(true, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer record.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"/background", nil)
	response, err := record.Wrap(http.DefaultTransport).RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	data := make([]byte, len(prefix))
	if _, err := io.ReadFull(response.Body, data); err != nil || string(data) != prefix {
		t.Fatalf("recorded prefix: %q %v", data, err)
	}
	cancel()
	if _, err := io.ReadAll(response.Body); !errors.Is(err, context.Canceled) {
		t.Fatalf("recorded cancellation: %v", err)
	}
	response.Body.Close()
	path := filepath.Join(t.TempDir(), "canceled-body.mcap")
	if err := record.Save(path); err != nil {
		t.Fatal(err)
	}
	server.Close()
	replay, err := New(false, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer replay.Close()
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	request, _ = http.NewRequestWithContext(ctx, "GET", server.URL+"/background", nil)
	response, err = replay.Wrap(&forbiddenTransport{}).RoundTrip(request)
	if err != nil || response.Header.Get("X-Recorded") != "headers" {
		t.Fatalf("replay headers: %v %v", response, err)
	}
	if _, err := io.ReadFull(response.Body, data); err != nil || string(data) != prefix {
		t.Fatalf("replayed prefix: %q %v", data, err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := response.Body.Read(make([]byte, 1))
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("capture invented response completion: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("owner cancellation did not unblock replay")
	}
	response.Body.Close()
	metrics := replay.Snapshot()
	if metrics.Active != 0 || len(metrics.Violations) != 0 || len(metrics.Unsupported) != 0 || metrics.EncodedBodyBytes != int64(len(prefix)) {
		t.Fatalf("cancellation was treated as fake success or corruption: %+v", metrics)
	}
}
