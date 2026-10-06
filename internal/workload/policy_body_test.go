package workload

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"

	"github.com/moreveal/mimic/internal/network"
	"github.com/moreveal/mimic/internal/state"
	"github.com/moreveal/mimic/internal/trace"
)

func TestPolicyLimitedCaptureThroughLoader(t *testing.T) {
	for _, mode := range []string{"none", "prefix"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Length", "10")
				io.WriteString(w, "0123456789")
			}))
			defer server.Close()
			load := func(s *Session, transport network.Transport, bodyMode string) (network.Response, error) {
				env := state.ChromeDesktopWindows(state.Product{Name: "Chrome", Version: "152.0.0.0", FullVersion: "152.0.7977.82"})
				l := network.NewLoader(func() state.Environment { return env }, network.NewCookieStore(), trace.New())
				defer l.CloseResponseBodies()
				defer l.CloseOwnedTransport()
				l.SetTransport(s.Wrap(transport))
				policy := &network.ResourcePolicyState{}
				prefix := int64(0)
				if bodyMode == "prefix" {
					prefix = 3
				}
				if _, err := policy.Update(network.ResourcePolicy{Rules: []network.ResourceRule{{ID: "body", Work: network.ResourceWork{Body: bodyMode, PrefixBytes: prefix}}}}); err != nil {
					t.Fatal(err)
				}
				l.SetResourcePolicy(policy)
				u, _ := url.Parse(server.URL)
				return l.Load(context.Background(), network.Request{URL: u, Initiator: network.Fetch})
			}
			record, _ := New(true, "", nil)
			defer record.Close()
			original, err := load(record, http.DefaultTransport, mode)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "policy.mcap")
			if err = record.Save(path); err != nil {
				t.Fatal(err)
			}
			server.Close()
			replay, err := New(false, path, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer replay.Close()
			got, err := load(replay, &forbiddenTransport{}, mode)
			if err != nil || string(got.Body) != string(original.Body) || !got.Partial {
				t.Fatalf("partial replay: %+v %v", got, err)
			}
			if len(replay.Snapshot().Unsupported) != 0 {
				t.Fatal(replay.Snapshot())
			}
			uncovered, err := New(false, path, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer uncovered.Close()
			if _, err = load(uncovered, &forbiddenTransport{}, "full"); err == nil {
				t.Fatal("fabricated successful full response")
			}
			if len(uncovered.Snapshot().Unsupported) == 0 {
				t.Fatal("missing unsupported diagnosis")
			}
		})
	}
}

func TestOrdinaryEarlyCloseStillCannotBeSaved(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "unread body") }))
	defer server.Close()
	s, _ := New(true, "", nil)
	defer s.Close()
	req, _ := http.NewRequest("GET", server.URL, nil)
	res, err := s.Wrap(http.DefaultTransport).RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if s.Save(filepath.Join(t.TempDir(), "bad.mcap")) == nil {
		t.Fatal("unexplained early close accepted")
	}
}
