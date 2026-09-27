package browser

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/moreveal/mimic/internal/network"
)

type navigationClockResponse struct{}

func (navigationClockResponse) Before(_ context.Context, request network.Request) (network.Decision, error) {
	return network.Decision{Response: &network.Response{
		Status: http.StatusOK, Headers: http.Header{"Content-Type": {"text/html"}},
		URL: request.URL, Body: []byte("<!doctype html><p>frame</p>"),
		Duration: 200 * time.Millisecond,
		BrowserVisibleTiming: network.TransportTimingSnapshot{
			Phases: map[string]float64{"responseComplete": 5000},
		},
	}}, nil
}

func (navigationClockResponse) After(_ context.Context, _ network.Request, response network.Response) (network.Response, error) {
	return response, nil
}

func TestFrameNavigationResponsePrecedesLifecycleOnPageClock(t *testing.T) {
	serialBrowserTest(t)
	source, err := os.ReadFile("testdata/frame_navigation_response_clock.js")
	if err != nil {
		t.Fatal(err)
	}
	historyTestPages(t, func(t *testing.T, page *Page) {
		page.Loader().Use(navigationClockResponse{})
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := page.Navigate(ctx, "http://clock.invalid/top"); err != nil {
			t.Fatal(err)
		}
		value, err := page.Evaluate(ctx, string(source))
		if err != nil || value != true {
			t.Fatalf("frame response/lifecycle clock ordering: %v %v", value, err)
		}
	})
}
