package network

import (
	"context"
	"net/url"
	"testing"

	"github.com/moreveal/mimic/internal/trace"
)

func TestPerformanceCompletionUsesRecordedPhaseBoundary(t *testing.T) {
	for _, test := range []struct {
		name     string
		phases   map[string]float64
		redirect float64
		duration float64
		want     float64
	}{
		{"duration fallback", nil, 0, 40, 40},
		{"buffered timing", map[string]float64{"responseComplete": 50}, 0, 90, 50},
		{"redirect floor", map[string]float64{"responseComplete": 1}, 30, 1, 30},
		{"ordered phases", map[string]float64{"responseComplete": 40, "firstResponseByte": 50}, 0, 40, 50},
		{"TLS completion", map[string]float64{"tlsHandshakeEnd": 80, "tcpConnectEnd": 100}, 0, 20, 80},
		{"TCP completion", map[string]float64{"tcpConnectEnd": 100}, 0, 20, 100},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := PerformanceCompletionMillis(test.phases, test.redirect, test.duration); got != test.want {
				t.Fatalf("completion = %v, want %v", got, test.want)
			}
		})
	}
}

func TestPerformanceCompletionBelongsToRequestAcrossSharedCache(t *testing.T) {
	session := NewSessionState()
	defer session.Close()
	transport := &countingTransport{}
	resource, _ := url.Parse("http://clock.test/resource")
	var calls [2]int
	for owner := range calls {
		tr := trace.New()
		loader := NewLoaderWithSession(testEnvironment, NewCookieStore(), session, tr)
		loader.SetTransport(transport)
		stop := tr.SubscribeKinds([]trace.Kind{trace.Network}, func(event trace.Event) {
			if event.Name == "response" && calls[owner] != 1 {
				t.Error("response was observable before its clock completion")
			}
		})
		response, err := loader.Load(context.Background(), Request{
			URL: resource, Initiator: Other,
			ObservePerformanceCompletion: func(milliseconds float64) {
				calls[owner]++
				if milliseconds < 0 {
					t.Error("negative completion offset")
				}
			},
		})
		stop()
		if err != nil || response.FromCache != (owner == 1) {
			t.Fatalf("owner %d: cache=%v err=%v", owner, response.FromCache, err)
		}
	}
	if calls != [2]int{1, 1} || transport.calls != 1 {
		t.Fatalf("cached response retained another owner: calls=%v transport=%d", calls, transport.calls)
	}
}
