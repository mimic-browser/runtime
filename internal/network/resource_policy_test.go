package network

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/moreveal/mimic/internal/trace"
)

func policyBool(value bool) *bool { return &value }

func TestResourcePolicyMediaSourceURLIsNotAResponseBody(t *testing.T) {
	for _, test := range []struct {
		name   string
		policy ResourcePolicy
		denied bool
	}{
		{name: "ordinary"},
		{name: "blocked", policy: ResourcePolicy{Presets: []string{"noVisualAssets"}}, denied: true},
		{name: "report-only", policy: ResourcePolicy{Presets: []string{"noVisualAssets"}, ReportOnly: true}},
		{name: "headers-only", policy: ResourcePolicy{Presets: []string{"headersOnly"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			target, _ := url.Parse("blob:https://example.test/media-source")
			loader := NewLoader(testEnvironment, NewCookieStore(), trace.New())
			defer loader.CloseResponseBodies()
			loader.session.PutMediaSourceURL(target.String(), "owner", &testMediaSourceResource{id: "source"})
			state := &ResourcePolicyState{}
			if _, err := state.Update(test.policy); err != nil {
				t.Fatal(err)
			}
			loader.SetResourcePolicy(state)
			response, err := loader.Load(context.Background(), Request{URL: target, Initiator: Other, Kind: "media"})
			if err == nil || response.Status != 0 || len(response.Body) != 0 {
				t.Fatalf("MediaSource became a transport response: status=%d body=%d err=%v", response.Status, len(response.Body), err)
			}
			if strings.Contains(err.Error(), "ERR_BLOCKED_BY_CLIENT") != test.denied {
				t.Fatalf("policy rejection mismatch: %v", err)
			}
			stats := state.Stats()
			if stats.NetworkAcquisitions != 0 || stats.EncodedNetworkBodyBytes != 0 || stats.RetainedBodyBytes != 0 {
				t.Fatalf("local source incurred transport or retention work: %+v", stats)
			}
			if test.policy.ReportOnly && stats.WouldBlock != 1 {
				t.Fatalf("report-only omitted the policy decision: %+v", stats)
			}
		})
	}
}

type resourcePolicyFulfillInterceptor struct{ calls atomic.Int64 }

func (i *resourcePolicyFulfillInterceptor) Before(context.Context, Request) (Decision, error) {
	i.calls.Add(1)
	return Decision{Response: &Response{Status: http.StatusOK, Headers: http.Header{}, Body: []byte("synthetic")}}, nil
}

func (i *resourcePolicyFulfillInterceptor) After(_ context.Context, _ Request, response Response) (Response, error) {
	return response, nil
}

func TestResourcePolicyBlocksBeforeSyntheticFulfillment(t *testing.T) {
	target, _ := url.Parse("https://example.com/image.png")
	state := &ResourcePolicyState{}
	policy := ResourcePolicy{Rules: []ResourceRule{{ID: "deny", Match: ResourceMatch{Kinds: []string{"image"}}, Work: ResourceWork{CacheRead: policyBool(false), Network: policyBool(false)}}}}
	if _, err := state.Update(policy); err != nil {
		t.Fatal(err)
	}
	loader := NewLoader(testEnvironment, NewCookieStore(), trace.New())
	loader.SetResourcePolicy(state)
	interceptor := &resourcePolicyFulfillInterceptor{}
	loader.Use(interceptor)
	defer loader.CloseResponseBodies()
	if _, err := loader.Load(context.Background(), Request{URL: target, Initiator: Image}); err == nil || interceptor.calls.Load() != 0 {
		t.Fatalf("fulfillment bypassed active policy: err=%v calls=%d", err, interceptor.calls.Load())
	}
	policy.ReportOnly = true
	if _, err := state.Update(policy); err != nil {
		t.Fatal(err)
	}
	res, err := loader.Load(context.Background(), Request{URL: target, Initiator: Image})
	if err != nil || string(res.Body) != "synthetic" || interceptor.calls.Load() != 1 {
		t.Fatalf("report-only fulfillment: body=%q err=%v calls=%d", res.Body, err, interceptor.calls.Load())
	}
	if stats := state.Stats(); stats.WouldBlock != 1 || stats.KnownAvoidedBodyReadBytes != int64(len("synthetic")) {
		t.Fatalf("report-only counterfactual: %+v", stats)
	}
}

func TestResourcePolicyBodyBudgetStopsStreaming(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		for i := 0; i < 16; i++ {
			if _, err := w.Write([]byte("0123456789")); err != nil {
				return
			}
			w.(http.Flusher).Flush()
		}
	}))
	defer server.Close()
	target, _ := url.Parse(server.URL)
	state := &ResourcePolicyState{}
	if _, err := state.Update(ResourcePolicy{Budgets: ResourceBudgets{MaxBodyBytes: 15}}); err != nil {
		t.Fatal(err)
	}
	loader := NewLoader(testEnvironment, NewCookieStore(), trace.New())
	loader.SetResourcePolicy(state)
	defer loader.CloseResponseBodies()
	if _, err := loader.Load(context.Background(), Request{URL: target, Initiator: Image}); !errors.Is(err, errBodyBudget) {
		t.Fatalf("load error = %v", err)
	}
	if got := state.Stats().BodyBytesConsumed; got != 15 {
		t.Fatalf("body bytes consumed = %d, want 15", got)
	}
}

func TestResourcePolicyBodyBudgetAllowsExactKnownLength(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "15")
		_, _ = w.Write([]byte("0123456789abcde"))
	}))
	defer server.Close()
	target, _ := url.Parse(server.URL)
	state := &ResourcePolicyState{}
	if _, err := state.Update(ResourcePolicy{Budgets: ResourceBudgets{MaxBodyBytes: 15}}); err != nil {
		t.Fatal(err)
	}
	loader := NewLoader(testEnvironment, NewCookieStore(), trace.New())
	loader.SetResourcePolicy(state)
	defer loader.CloseResponseBodies()
	res, err := loader.Load(context.Background(), Request{URL: target, Initiator: Fetch})
	if err != nil || string(res.Body) != "0123456789abcde" {
		t.Fatalf("exact budget: body=%q err=%v", res.Body, err)
	}
}

func TestResourcePolicyRetainedBudgetReleasesAfterHistoryClose(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte("123456789012"))
	}))
	defer server.Close()
	target, _ := url.Parse(server.URL)
	state := &ResourcePolicyState{}
	if _, err := state.Update(ResourcePolicy{Budgets: ResourceBudgets{MaxRetainedBytes: 12}}); err != nil {
		t.Fatal(err)
	}
	loader := NewLoader(testEnvironment, NewCookieStore(), trace.New())
	loader.SetResourcePolicy(state)
	for _, id := range []string{"first", "second"} {
		res, err := loader.Load(context.Background(), Request{ID: id, URL: target, Initiator: Fetch})
		if err != nil || len(res.Body) != 12 {
			t.Fatalf("%s: body=%d err=%v", id, len(res.Body), err)
		}
	}
	if got := state.Stats().RetainedBodyBytes; got != 12 {
		t.Fatalf("retained bytes=%d", got)
	}
	if _, _, found, err := loader.CompletedBody("second"); !found || !errors.Is(err, errRetainedBudget) {
		t.Fatalf("second debug body: found=%v err=%v", found, err)
	}
	loader.CloseResponseBodies()
	if got := state.Stats().RetainedBodyBytes; got != 0 {
		t.Fatalf("retained bytes after close=%d", got)
	}
}

func TestResourcePolicySharedCacheAndDebugBodyChargedOnce(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "max-age=3600")
		_, _ = w.Write([]byte("123456789012"))
	}))
	defer server.Close()
	target, _ := url.Parse(server.URL)
	state := &ResourcePolicyState{}
	if _, err := state.Update(ResourcePolicy{Budgets: ResourceBudgets{MaxRetainedBytes: 12}}); err != nil {
		t.Fatal(err)
	}
	session := NewSessionState()
	loader := NewLoaderWithSession(testEnvironment, NewCookieStore(), session, trace.New())
	loader.SetResourcePolicy(state)
	res, err := loader.Load(context.Background(), Request{ID: "shared", URL: target, Initiator: Image})
	if err != nil || len(res.Body) != 12 || state.Stats().RetainedBodyBytes != 12 {
		t.Fatalf("shared retention: body=%d err=%v stats=%+v", len(res.Body), err, state.Stats())
	}
	loader.CloseResponseBodies()
	if got := state.Stats().RetainedBodyBytes; got != 12 {
		t.Fatalf("cache owner retained=%d", got)
	}
	session.Close()
	if got := state.Stats().RetainedBodyBytes; got != 0 {
		t.Fatalf("after cache close=%d", got)
	}
}

func TestResourcePolicyAbsentNeverChargesRetention(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "max-age=3600")
		_, _ = w.Write([]byte("ordinary body"))
	}))
	defer server.Close()
	target, _ := url.Parse(server.URL)
	state := &ResourcePolicyState{}
	loader := NewLoader(testEnvironment, NewCookieStore(), trace.New())
	loader.SetResourcePolicy(state)
	if _, err := loader.Load(context.Background(), Request{URL: target, Initiator: Fetch}); err != nil {
		t.Fatal(err)
	}
	loader.CloseResponseBodies()
	if got := state.Stats().RetainedBodyBytes; got != 0 {
		t.Fatalf("absent policy retention charge=%d", got)
	}
}

func TestResourcePolicyRetainedBudgetSeesPreexistingCache(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "max-age=3600")
		_, _ = w.Write([]byte("123456789012"))
	}))
	defer server.Close()
	first, _ := url.Parse(server.URL + "/first")
	second, _ := url.Parse(server.URL + "/second")
	state := &ResourcePolicyState{}
	loader := NewLoader(testEnvironment, NewCookieStore(), trace.New())
	loader.SetResourcePolicy(state)
	defer loader.CloseResponseBodies()
	if _, err := loader.Load(context.Background(), Request{ID: "before", URL: first, Initiator: Fetch}); err != nil {
		t.Fatal(err)
	}
	if _, err := state.Update(ResourcePolicy{Budgets: ResourceBudgets{MaxRetainedBytes: 12}}); err != nil {
		t.Fatal(err)
	}
	res, err := loader.Load(context.Background(), Request{ID: "after", URL: second, Initiator: Fetch})
	if err != nil || len(res.Body) != 12 {
		t.Fatalf("delivery after update: body=%d err=%v", len(res.Body), err)
	}
	if _, _, found, err := loader.CompletedBody("after"); !found || !errors.Is(err, errRetainedBudget) {
		t.Fatalf("preexisting body bypassed retention limit: found=%v err=%v", found, err)
	}
	if got := loader.session.BodyStorageStats().ResidentBytes; got != 12 {
		t.Fatalf("retained storage grew past limit: %d", got)
	}
}

func TestResourcePolicyBodyBudgetConcurrentReservations(t *testing.T) {
	state := &ResourcePolicyState{}
	if _, err := state.Update(ResourcePolicy{Budgets: ResourceBudgets{MaxBodyBytes: 15}}); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			reader := &resourcePolicyBodyReader{state: state, policy: state.Capture(), source: bytes.NewReader([]byte("0123456789")), expected: 10}
			_, err := io.ReadAll(reader)
			result <- err
		}()
	}
	for i := 0; i < 2; i++ {
		<-result
	}
	if got := state.Stats().BodyBytesConsumed; got < 10 || got > 15 {
		t.Fatalf("concurrent body consumption=%d, outside [10,15]", got)
	}
}

func TestResourcePolicyMatchesOriginAndSchemefulTopLevelSite(t *testing.T) {
	state := &ResourcePolicyState{}
	policy := ResourcePolicy{Rules: []ResourceRule{{ID: "scoped", Match: ResourceMatch{Origins: []string{"https://assets.example:443"}, TopLevelSite: "https://example.com", Kinds: []string{"fetch"}}, Work: ResourceWork{Network: policyBool(false)}}}}
	if _, err := state.Update(policy); err != nil {
		t.Fatal(err)
	}
	target, _ := url.Parse("https://assets.example/a.png")
	top, _ := url.Parse("https://shop.example.com/")
	if decision := state.Capture().decide(Request{URL: target, TopLevelURL: top, Initiator: Fetch}); decision.RuleID != "scoped" {
		t.Fatalf("fetch match: %+v", decision)
	}
	if decision := state.Capture().decide(Request{URL: target, TopLevelURL: top, Initiator: Image}); decision.RuleID != "" {
		t.Fatalf("image should not match fetch rule: %+v", decision)
	}
	httpTop, _ := url.Parse("http://shop.example.com/")
	if decision := state.Capture().decide(Request{URL: target, TopLevelURL: httpTop, Initiator: Fetch}); decision.RuleID != "" {
		t.Fatalf("wrong top-level scheme matched: %+v", decision)
	}
}

func TestResourcePolicyDeniedFetchDoesNotSendCORSPreflight(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	target, _ := url.Parse(server.URL)
	source, _ := url.Parse("https://other.example/")
	state := &ResourcePolicyState{}
	if _, err := state.Update(ResourcePolicy{Rules: []ResourceRule{{ID: "deny-fetch", Match: ResourceMatch{Kinds: []string{"fetch"}}, Work: ResourceWork{Network: policyBool(false)}}}}); err != nil {
		t.Fatal(err)
	}
	loader := NewLoader(testEnvironment, NewCookieStore(), trace.New())
	loader.SetResourcePolicy(state)
	defer loader.CloseResponseBodies()
	_, err := loader.Load(context.Background(), Request{URL: target, SourceURL: source, Initiator: Fetch, Mode: "cors", Method: "PUT", Headers: http.Header{"X-Policy-Test": {"value"}}})
	if err == nil || requests.Load() != 0 {
		t.Fatalf("denied fetch error=%v, physical requests=%d", err, requests.Load())
	}
}

func TestResourcePolicyCauseClassification(t *testing.T) {
	for _, tc := range []struct {
		request Request
		want    string
	}{
		{Request{Initiator: Navigation}, "document"},
		{Request{Initiator: Iframe}, "iframe"},
		{Request{Initiator: Script}, "script"},
		{Request{Initiator: Stylesheet}, "stylesheet"},
		{Request{Initiator: Image}, "image"},
		{Request{Initiator: Fetch}, "fetch"},
		{Request{Initiator: XHR}, "xhr"},
		{Request{Initiator: Worker}, "worker"},
		{Request{Initiator: Other, Kind: "font"}, "font"},
		{Request{Initiator: Other, Kind: "media"}, "media"},
		{Request{Initiator: Other, Kind: "favicon"}, "favicon"},
		{Request{Kind: "websocket"}, "websocket"},
	} {
		if got := tc.request.ResourceKind(); got != tc.want {
			t.Fatalf("request %+v classified %q, want %q", tc.request, got, tc.want)
		}
	}
	target, _ := url.Parse("https://example.com/x.png")
	if got := (Request{URL: target, Initiator: Fetch}).ResourceKind(); got != "fetch" {
		t.Fatalf("extension overrode cause: %q", got)
	}
}

func TestResourcePolicyCacheAndNetworkAreIndependent(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Cache-Control", "max-age=3600")
		_, _ = w.Write([]byte("cached representation"))
	}))
	defer server.Close()
	target, _ := url.Parse(server.URL)
	state := &ResourcePolicyState{}
	loader := NewLoader(testEnvironment, NewCookieStore(), trace.New())
	loader.SetResourcePolicy(state)
	defer loader.CloseResponseBodies()
	request := Request{URL: target, Initiator: Image}
	if _, err := loader.Load(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	_, err := state.Update(ResourcePolicy{Rules: []ResourceRule{{ID: "cache-only", Match: ResourceMatch{Kinds: []string{"image"}}, Work: ResourceWork{Network: policyBool(false)}}}})
	if err != nil {
		t.Fatal(err)
	}
	if res, err := loader.Load(context.Background(), request); err != nil || string(res.Body) != "cached representation" {
		t.Fatalf("cached response: %q, %v", res.Body, err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("network requests = %d", got)
	}
	uncached, _ := url.Parse(server.URL + "/miss")
	if _, err := loader.Load(context.Background(), Request{URL: uncached, Initiator: Image}); err == nil || !strings.Contains(err.Error(), "ERR_BLOCKED_BY_CLIENT") {
		t.Fatalf("cache miss: %v", err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("blocked miss used network: %d", got)
	}
}

func TestResourcePolicyReportOnlyCountsDeniedCachedRepresentation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "max-age=3600")
		_, _ = w.Write([]byte("cache body"))
	}))
	defer server.Close()
	target, _ := url.Parse(server.URL)
	state := &ResourcePolicyState{}
	loader := NewLoader(testEnvironment, NewCookieStore(), trace.New())
	loader.SetResourcePolicy(state)
	defer loader.CloseResponseBodies()
	request := Request{URL: target, Initiator: Image}
	if _, err := loader.Load(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if _, err := state.Update(ResourcePolicy{ReportOnly: true, Rules: []ResourceRule{{ID: "deny", Match: ResourceMatch{Kinds: []string{"image"}}, Work: ResourceWork{CacheRead: policyBool(false), Network: policyBool(false)}}}}); err != nil {
		t.Fatal(err)
	}
	res, err := loader.Load(context.Background(), request)
	if err != nil || !res.FromCache || string(res.Body) != "cache body" {
		t.Fatalf("report-only cache delivery: %+v %v", res, err)
	}
	stats := state.Stats()
	if stats.WouldBlock != 1 || stats.WouldBypassCache != 1 || stats.KnownAvoidedBodyReadBytes != int64(len("cache body")) {
		t.Fatalf("counterfactual cached decision: %+v", stats)
	}
}

func TestResourcePolicyBodyLimitsPreserveGET(t *testing.T) {
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte("0123456789"))
	}))
	defer server.Close()
	target, _ := url.Parse(server.URL)
	state := &ResourcePolicyState{}
	loader := NewLoader(testEnvironment, NewCookieStore(), trace.New())
	loader.SetResourcePolicy(state)
	defer loader.CloseResponseBodies()
	for _, tc := range []struct {
		mode string
		size int64
		want string
	}{{"none", 0, ""}, {"prefix", 4, "0123"}} {
		_, err := state.Update(ResourcePolicy{Rules: []ResourceRule{{ID: tc.mode, Match: ResourceMatch{Kinds: []string{"image"}}, Work: ResourceWork{Body: tc.mode, PrefixBytes: tc.size}}}})
		if err != nil {
			t.Fatal(err)
		}
		res, err := loader.Load(context.Background(), Request{URL: target, Initiator: Image})
		if err == nil || string(res.Body) != tc.want || !res.Partial {
			t.Fatalf("%s: body %q partial %v error %v", tc.mode, res.Body, res.Partial, err)
		}
	}
	if len(methods) != 2 || methods[0] != "GET" || methods[1] != "GET" {
		t.Fatalf("methods: %v", methods)
	}
	if got := state.Stats().BodyBytesConsumed; got != 4 {
		t.Fatalf("consumed = %d", got)
	}
}

func TestResourcePolicyRetentionIsIndependent(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Cache-Control", "max-age=3600")
		_, _ = w.Write([]byte("runtime body"))
	}))
	defer server.Close()
	target, _ := url.Parse(server.URL)
	state := &ResourcePolicyState{}
	_, err := state.Update(ResourcePolicy{Rules: []ResourceRule{{ID: "no-storage", Match: ResourceMatch{Kinds: []string{"image"}}, Work: ResourceWork{CacheRetain: policyBool(false), DebugRetain: policyBool(false)}}}})
	if err != nil {
		t.Fatal(err)
	}
	loader := NewLoader(testEnvironment, NewCookieStore(), trace.New())
	loader.SetResourcePolicy(state)
	defer loader.CloseResponseBodies()
	for i := 0; i < 2; i++ {
		res, err := loader.Load(context.Background(), Request{ID: "image-retention", URL: target, Initiator: Image})
		if err != nil || string(res.Body) != "runtime body" {
			t.Fatalf("load %d: %q %v", i, res.Body, err)
		}
	}
	if requests.Load() != 2 {
		t.Fatalf("cache retained representation: %d requests", requests.Load())
	}
	if _, _, ok, _ := loader.CompletedBody("image-retention"); ok {
		t.Fatal("debug body retained")
	}
}

func TestResourcePolicyReportOnlyDoesNotChangeDelivery(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte("full body"))
	}))
	defer server.Close()
	target, _ := url.Parse(server.URL)
	state := &ResourcePolicyState{}
	_, err := state.Update(ResourcePolicy{ReportOnly: true, Rules: []ResourceRule{{ID: "blocked", Match: ResourceMatch{Kinds: []string{"image"}}, Work: ResourceWork{Network: policyBool(false), Body: "none", Decode: policyBool(false)}}}})
	if err != nil {
		t.Fatal(err)
	}
	loader := NewLoader(testEnvironment, NewCookieStore(), trace.New())
	loader.SetResourcePolicy(state)
	defer loader.CloseResponseBodies()
	res, err := loader.Load(context.Background(), Request{URL: target, Initiator: Image})
	if err != nil || res.Partial || res.DecodeDisallowed || string(res.Body) != "full body" {
		t.Fatalf("report only: %q partial %v decode %v error %v", res.Body, res.Partial, res.DecodeDisallowed, err)
	}
	if requests.Load() != 1 || state.Stats().WouldBlock != 1 {
		t.Fatalf("requests %d stats %+v", requests.Load(), state.Stats())
	}
	if got := state.Stats().KnownAvoidedBodyReadBytes; got != int64(len("full body")) {
		t.Fatalf("known avoided body reads = %d", got)
	}
}

func TestResourcePolicyRequestBudget(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()
	target, _ := url.Parse(server.URL)
	state := &ResourcePolicyState{}
	if _, err := state.Update(ResourcePolicy{Budgets: ResourceBudgets{MaxRequests: 1}}); err != nil {
		t.Fatal(err)
	}
	loader := NewLoader(testEnvironment, NewCookieStore(), trace.New())
	loader.SetResourcePolicy(state)
	defer loader.CloseResponseBodies()
	request := Request{URL: target, Initiator: Image}
	if _, err := loader.Load(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if _, err := loader.Load(context.Background(), request); err == nil || !strings.Contains(err.Error(), "budget exceeded") {
		t.Fatalf("second request: %v", err)
	}
	if requests.Load() != 1 || state.Stats().BudgetDenied != 1 {
		t.Fatalf("requests %d stats %+v", requests.Load(), state.Stats())
	}
}

func TestResourcePolicyRedirectKeepsCapturedGeneration(t *testing.T) {
	entered := make(chan struct{})
	resume := make(chan struct{})
	var targetHits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			close(entered)
			<-resume
			http.Redirect(w, r, "/target", http.StatusFound)
			return
		}
		targetHits.Add(1)
		_, _ = w.Write([]byte("target"))
	}))
	defer server.Close()
	target, _ := url.Parse(server.URL + "/start")
	state := &ResourcePolicyState{}
	if _, err := state.Update(ResourcePolicy{}); err != nil {
		t.Fatal(err)
	}
	loader := NewLoader(testEnvironment, NewCookieStore(), trace.New())
	loader.SetResourcePolicy(state)
	defer loader.CloseResponseBodies()
	done := make(chan error, 1)
	go func() {
		res, err := loader.Load(context.Background(), Request{URL: target, Initiator: Image})
		if err == nil && string(res.Body) != "target" {
			err = fmt.Errorf("body %q", res.Body)
		}
		done <- err
	}()
	<-entered
	no := false
	if _, err := state.Update(ResourcePolicy{Rules: []ResourceRule{{ID: "block-target", Match: ResourceMatch{Kinds: []string{"image"}}, Work: ResourceWork{Network: &no}}}}); err != nil {
		t.Fatal(err)
	}
	close(resume)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if targetHits.Load() != 1 {
		t.Fatalf("target requests %d", targetHits.Load())
	}
}

func TestResourcePolicyValidationAndPresets(t *testing.T) {
	for _, input := range []string{
		`{"unknown":true}`,
		`{"schemaVersion":1}`,
		`{"rules":[{"id":"x","work":{"body":"prefix"}}]}`,
		`{"presets":["unknown"]}`,
	} {
		if _, err := ParseResourcePolicy([]byte(input)); err == nil {
			t.Fatalf("accepted %s", input)
		}
	}
	config, err := ParseResourcePolicy([]byte(`{"presets":["noVisualAssets"],"rules":[{"id":"exception","match":{"hosts":["keep.example"],"kinds":["image"]},"work":{"network":true}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	state := &ResourcePolicyState{}
	if _, err := state.Update(config); err != nil {
		t.Fatal(err)
	}
	keep, _ := url.Parse("https://keep.example/logo.png")
	block, _ := url.Parse("https://block.example/logo.png")
	if d := state.Capture().decide(Request{URL: keep, Initiator: Image}); d.RuleID != "exception" {
		t.Fatalf("exception: %+v", d)
	}
	if d := state.Capture().decide(Request{URL: block, Initiator: Image}); d.RuleID != "preset:noVisualAssets" {
		t.Fatalf("preset: %+v", d)
	}
}

func TestResourcePolicyResponseSizeBudget(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("0123456789"))
	}))
	defer server.Close()
	target, _ := url.Parse(server.URL)
	state := &ResourcePolicyState{}
	if _, err := state.Update(ResourcePolicy{Budgets: ResourceBudgets{MaxResponseBytes: 5}}); err != nil {
		t.Fatal(err)
	}
	loader := NewLoader(testEnvironment, NewCookieStore(), trace.New())
	loader.SetResourcePolicy(state)
	defer loader.CloseResponseBodies()
	if _, err := loader.Load(context.Background(), Request{URL: target, Initiator: Image}); err == nil || !strings.Contains(err.Error(), "response size budget") {
		t.Fatalf("oversize: %v", err)
	}
	if state.Stats().BudgetDenied != 1 {
		t.Fatalf("stats: %+v", state.Stats())
	}
	if _, err := state.Update(ResourcePolicy{ReportOnly: true, Budgets: ResourceBudgets{MaxResponseBytes: 5}}); err != nil {
		t.Fatal(err)
	}
	res, err := loader.Load(context.Background(), Request{URL: target, Initiator: Image})
	if err != nil || string(res.Body) != "0123456789" {
		t.Fatalf("report only: %q %v", res.Body, err)
	}
}
