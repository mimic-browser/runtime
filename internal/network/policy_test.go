package network

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/moreveal/mimic/internal/trace"
)

func TestRequestPolicySnapshotsOwnHeaderValues(t *testing.T) {
	var policy RequestPolicy
	headers := http.Header{"X-Page": {"first"}}
	policy.SetExtraHeaders(headers)
	headers.Set("X-Page", "mutated input")
	snapshot := policy.Snapshot()
	if got := snapshot.ExtraHeaders.Get("X-Page"); got != "first" {
		t.Fatalf("stored input was aliased: %q", got)
	}
	snapshot.ExtraHeaders.Set("X-Page", "mutated output")
	if got := policy.Snapshot().ExtraHeaders.Get("X-Page"); got != "first" {
		t.Fatalf("snapshot was aliased: %q", got)
	}
}

// Chrome 152.0.7977.82: two Pages share cached representations, while disabling
// one Page's cache bypasses its reads and still updates that shared cache.
func TestLoaderPolicyIsLocalWhileCacheIsShared(t *testing.T) {
	var mu sync.Mutex
	counts := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/headers" {
			fmt.Fprint(w, r.Header.Get("X-Page"))
			return
		}
		mu.Lock()
		counts[r.URL.Path]++
		count := counts[r.URL.Path]
		mu.Unlock()
		w.Header().Set("Cache-Control", "max-age=3600")
		fmt.Fprint(w, count)
	}))
	defer server.Close()
	session := NewSessionState()
	cookies := NewCookieStore()
	a := NewLoaderWithSession(testEnvironment, cookies, session, trace.New())
	b := NewLoaderWithSession(testEnvironment, cookies, session, trace.New())
	defer a.CloseOwnedTransport()
	defer b.CloseOwnedTransport()
	a.Policy().SetExtraHeaders(http.Header{"X-Page": {"one"}})
	b.Policy().SetExtraHeaders(http.Header{"X-Page": {"two"}})
	load := func(loader *Loader, path string) Response {
		t.Helper()
		resource, _ := url.Parse(server.URL + path)
		response, err := loader.Load(context.Background(), Request{URL: resource, Initiator: Fetch})
		if err != nil {
			t.Fatal(err)
		}
		return response
	}
	if got := string(load(a, "/headers").Body); got != "one" {
		t.Fatalf("Page A headers: %q", got)
	}
	if got := string(load(b, "/headers").Body); got != "two" {
		t.Fatalf("Page B headers: %q", got)
	}
	if response := load(a, "/cache"); response.FromCache || string(response.Body) != "1" {
		t.Fatalf("initial response: %+v", response)
	}
	if response := load(b, "/cache"); !response.FromCache || string(response.Body) != "1" {
		t.Fatalf("shared cache response: %+v", response)
	}
	a.Policy().SetCacheDisabled(true)
	if response := load(a, "/cache"); response.FromCache || string(response.Body) != "2" {
		t.Fatalf("Page A must bypass cache: %+v", response)
	}
	if response := load(b, "/cache"); !response.FromCache || string(response.Body) != "2" {
		t.Fatalf("Page B must see fresh shared cache: %+v", response)
	}
	load(a, "/new-cache")
	if response := load(b, "/new-cache"); !response.FromCache || string(response.Body) != "1" {
		t.Fatalf("bypass must still store response: %+v", response)
	}
	a.Policy().SetOffline(true)
	resource, _ := url.Parse(server.URL + "/headers")
	if _, err := a.Load(context.Background(), Request{URL: resource}); err == nil || !strings.Contains(err.Error(), "ERR_INTERNET_DISCONNECTED") {
		t.Fatalf("Page A offline request: %v", err)
	}
	if got := string(load(b, "/headers").Body); got != "two" {
		t.Fatalf("Page B must stay online: %q", got)
	}
	a.Policy().SetOffline(false)
	a.Policy().SetCacheDisabled(false)
	if response := load(a, "/cache"); !response.FromCache || string(response.Body) != "2" {
		t.Fatalf("restored Page A cache: %+v", response)
	}
}

func TestOfflinePolicyAllowsLocalDataAndBlobResources(t *testing.T) {
	session := NewSessionState()
	loader := NewLoaderWithSession(testEnvironment, NewCookieStore(), session, trace.New())
	defer loader.CloseOwnedTransport()
	loader.Policy().SetOffline(true)
	session.PutBlob("blob:https://example.test/probe", []byte("blob-value"), "text/plain", "test-owner")
	for raw, expected := range map[string]string{"data:text/plain,data-value": "data-value", "blob:https://example.test/probe": "blob-value"} {
		resource, _ := url.Parse(raw)
		response, err := loader.Load(context.Background(), Request{URL: resource, Initiator: Fetch})
		if err != nil || string(response.Body) != expected {
			t.Fatalf("local resource %s: %s, %v", raw, response.Body, err)
		}
	}
}

func TestCacheRefreshReplacesOnlyMatchingVaryVariant(t *testing.T) {
	session := NewSessionState()
	resource, _ := url.Parse("https://example.test/variant")
	now := time.Now()
	request := func(variant string) Request {
		return Request{URL: resource, Method: http.MethodGet, Headers: http.Header{"X-Variant": {variant}}}
	}
	store := func(variant, body, vary string) {
		session.PutCached(request(variant), Response{Status: http.StatusOK, URL: resource, Body: []byte(body), Headers: http.Header{"Cache-Control": {"max-age=60"}, "Vary": {vary}}}, now)
	}
	store("a", "a1", "X-Variant")
	store("b", "b1", "X-Variant")
	store("a", "a2", "X-Variant")
	for variant, expected := range map[string]string{"a": "a2", "b": "b1"} {
		response, ok := session.GetCached(request(variant), now)
		if !ok || string(response.Body) != expected {
			t.Fatalf("variant %q: %s, %t", variant, response.Body, ok)
		}
	}
	if count := len(session.cache[resource.String()]); count != 2 {
		t.Fatalf("cache retained superseded representations: %d", count)
	}
	store("a", "unvaried", "")
	if response, ok := session.GetCached(request("b"), now); !ok || string(response.Body) != "unvaried" {
		t.Fatalf("changed Vary returned an older representation: %s %t", response.Body, ok)
	}
}
