package workload

import (
	"context"
	"fmt"
	"github.com/moreveal/mimic/internal/network"
	"github.com/moreveal/mimic/internal/state"
	"github.com/moreveal/mimic/internal/trace"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestReplayDistinguishesNavigationPhaseWithoutGuessingResponses(t *testing.T) {
	var n atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, n.Add(1)) }))
	defer server.Close()
	target, _ := url.Parse(server.URL)
	source, _ := url.Parse(server.URL + "/application")
	record, _ := New(true, "", nil)
	defer record.Close()
	loader := network.NewLoader(func() state.Environment { return state.Environment{} }, network.NewCookieStore(), trace.New())
	loader.SetTransport(record.Wrap(http.DefaultTransport))
	request := network.Request{URL: target, SourceURL: source, Initiator: network.Navigation, ExecutionOwner: "frame"}
	for _, phase := range []int{1, 3} {
		request.HistoryPhase = phase
		if _, err := loader.Load(context.Background(), request); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(t.TempDir(), "phases.mcap")
	if err := record.Save(path); err != nil {
		t.Fatal(err)
	}
	server.Close()
	replay, err := New(false, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer replay.Close()
	loader.SetTransport(replay.Wrap(&forbiddenTransport{}))
	for index, phase := range []int{3, 1} {
		request.HistoryPhase = phase
		response, err := loader.Load(context.Background(), request)
		expected := []string{"2", "1"}[index]
		if err != nil || string(response.Body) != expected {
			t.Fatalf("phase %d: %s %v", phase, response.Body, err)
		}
	}
	request.HistoryPhase = 2
	if _, err := loader.Load(context.Background(), request); err == nil {
		t.Fatal("unrecorded phase was served")
	}
}
