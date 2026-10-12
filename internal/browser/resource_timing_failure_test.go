package browser

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/moreveal/mimic/internal/network"
)

type blockedTimingResource struct{}

func (blockedTimingResource) Before(_ context.Context, request network.Request) (network.Decision, error) {
	if strings.HasSuffix(request.URL.Path, "/blocked") {
		return network.Decision{Block: errors.New("request blocked by controller")}, nil
	}
	return network.Decision{}, nil
}

func (blockedTimingResource) After(_ context.Context, _ network.Request, response network.Response) (network.Response, error) {
	return response, nil
}

// A failed fetch still completes a Resource Timing entry in headful Chrome.
// The retained Chrome control included a controller-blocked hCaptcha fetch.
func TestBlockedFetchHasResourceTimingEntry(t *testing.T) {
	parallelBrowserTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `<!doctype html><title>timing</title>`)
	}))
	defer server.Close()
	p := testPage(t)
	p.Loader().Use(blockedTimingResource{})
	if err := p.Navigate(context.Background(), server.URL); err != nil {
		t.Fatal(err)
	}
	value, err := p.Evaluate(context.Background(), `(async()=>{
  try { await fetch('/blocked'); } catch (_) {}
  await new Promise(resolve=>setTimeout(resolve,20));
  const entries=performance.getEntriesByName(location.origin+'/blocked');
  return entries.map(e=>({type:e.entryType,initiator:e.initiatorType,status:e.responseStatus,
    request:e.requestStart,response:e.responseStart,end:e.responseEnd,duration:e.duration}));
})()`)
	if err != nil {
		t.Fatal(err)
	}
	entries, ok := value.([]any)
	if !ok || len(entries) != 1 {
		t.Fatalf("blocked fetch timing entries = %#v", value)
	}
	entry := entries[0].(map[string]any)
	if entry["type"] != "resource" || entry["initiator"] != "fetch" || numberValue(entry["status"]) != 0 || numberValue(entry["request"]) != 0 || numberValue(entry["response"]) != 0 || numberValue(entry["end"]) < 0 || numberValue(entry["duration"]) < 0 {
		t.Fatalf("unexpected blocked fetch timing: %#v", entry)
	}
}
