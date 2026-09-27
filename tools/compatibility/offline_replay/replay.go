package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/moreveal/mimic/internal/trace"
)

type replay struct {
	mu            sync.Mutex
	capture       *capture
	used          map[int]bool
	contextMap    map[string]string
	pending       []recordedRequest
	events        []map[string]any
	cycle         int
	cancel        context.CancelFunc
	segment       *regexp.Regexp
	maxRequests   int
	requests      int
	httpDateShift *time.Duration
}

func newReplay(c *capture, cancel context.CancelFunc, segment *regexp.Regexp, max int) *replay {
	return &replay{capture: c, used: map[int]bool{}, contextMap: map[string]string{}, cancel: cancel, segment: segment, maxRequests: max}
}

func (t *replay) route(s string) string {
	if t.segment != nil {
		return t.segment.ReplaceAllString(s, "/OFFLINE-DYNAMIC/")
	}
	return s
}

func (t *replay) observe(e trace.Event) {
	if e.Kind != trace.Network {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	d := e.Data
	if e.Name == "request" {
		t.pending = append(t.pending, recordedRequest{ID: stringField(d, "id"), URL: stringField(d, "url"), Method: stringField(d, "method"), Context: stringField(d, "context"), Initiator: stringField(d, "initiator"), Headers: stringMap(d["headers"])})
		return
	}
	if e.Name != "response" && e.Name != "failed" {
		return
	}
	var request recordedRequest
	for i, r := range t.pending {
		if r.ID == stringField(d, "id") {
			request = r
			t.pending = append(t.pending[:i], t.pending[i+1:]...)
			break
		}
	}
	if request.ID == "" {
		request, _ = responseOnlyDataRequest(e)
	}
	local := ""
	if boolField(d, "fromCache") {
		local = "cache"
	} else if boolField(d, "synthetic") {
		local = "synthetic"
	}
	if local == "" {
		return
	}
	for i, f := range t.capture.Fixtures {
		if t.used[i] || f.Local != local || f.Cycle != t.cycle || f.Request.Method != request.Method || f.Request.Initiator != request.Initiator || !t.contextCompatible(request.Context, f.Request.Context) {
			continue
		}
		urlMatches := t.route(f.Request.URL) == t.route(request.URL)
		if local == "synthetic" && strings.HasPrefix(f.Request.URL, "blob:") {
			a, _ := url.Parse(f.Request.URL)
			b, _ := url.Parse(request.URL)
			urlMatches = a != nil && b != nil && a.Scheme == b.Scheme
		}
		if !urlMatches {
			continue
		}
		t.used[i] = true
		t.contextMap[request.Context] = f.Request.Context
		t.events = append(t.events, map[string]any{"kind": "local", "local": local, "fixtureIndex": f.Index, "cycle": f.Cycle, "statusEqual": intField(d, "status") == f.Status, "responseOnlyLocal": f.ResponseOnlyLocal})
		return
	}
	t.events = append(t.events, map[string]any{"kind": "unmatched-local", "local": local, "cycle": t.cycle, "urlHash": shortHash(request.URL)})
}

func intField(d map[string]any, k string) int {
	switch v := d[k].(type) {
	case int:
		return v
	case float64:
		return int(v)
	}
	return 0
}
func (t *replay) contextCompatible(actual, expected string) bool {
	v, ok := t.contextMap[actual]
	return !ok || v == expected
}

func (t *replay) requestContext(r *http.Request) recordedRequest {
	for i, p := range t.pending {
		if p.URL != r.URL.String() || p.Method != r.Method || p.Headers["sec-fetch-dest"] != r.Header.Get("Sec-Fetch-Dest") {
			continue
		}
		t.pending = append(t.pending[:i], t.pending[i+1:]...)
		return p
	}
	return recordedRequest{URL: r.URL.String(), Method: r.Method}
}

func (t *replay) RoundTrip(r *http.Request) (*http.Response, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.requests++
	if t.requests > t.maxRequests {
		return nil, t.boundary("request-limit", r)
	}
	actual := t.requestContext(r)
	if actual.Context == "" {
		return nil, t.boundary("missing-trace-context", r)
	}
	var candidate int = -1
	known := false
	for i, f := range t.capture.Fixtures {
		if f.Local != "" || f.Request.Method != r.Method || t.route(f.Request.URL) != t.route(r.URL.String()) {
			continue
		}
		known = true
		if t.used[i] || f.Request.Initiator != actual.Initiator || !t.contextCompatible(actual.Context, f.Request.Context) {
			continue
		}
		if actual.Initiator != "navigation" && f.Cycle != t.cycle {
			continue
		}
		candidate = i
		break
	}
	if candidate < 0 {
		kind := "uncaptured-request"
		if known {
			kind = "response-queue-exhausted-or-context-mismatch"
		}
		return nil, t.boundary(kind, r)
	}
	f := t.capture.Fixtures[candidate]
	t.used[candidate] = true
	t.contextMap[actual.Context] = f.Request.Context
	if f.Request.Context == t.capture.TopContext && f.Request.Initiator == "navigation" {
		t.cycle = f.Cycle
	}
	var body []byte
	var err error
	if r.Body != nil {
		body, err = io.ReadAll(io.LimitReader(r.Body, (32<<20)+1))
		if err != nil {
			return nil, err
		}
		if len(body) > 32<<20 {
			delete(t.used, candidate)
			return nil, t.boundary("request-body-limit", r)
		}
	}
	differentHeaders := []string{}
	names := map[string]bool{}
	for k := range f.Request.Headers {
		names[strings.ToLower(k)] = true
	}
	for k := range r.Header {
		names[strings.ToLower(k)] = true
	}
	for k := range names {
		if f.Request.Headers[k] != r.Header.Get(k) {
			differentHeaders = append(differentHeaders, k)
		}
	}
	sort.Strings(differentHeaders)
	event := map[string]any{"kind": "fixture", "fixtureIndex": f.Index, "cycle": f.Cycle, "method": r.Method, "urlHash": shortHash(r.URL.String()), "requestContextHash": shortHash(actual.Context), "recordedContextHash": shortHash(f.Request.Context), "requestBodyBytes": len(body), "recordedBodyBytes": len(f.Request.PostData), "requestBodySHA256": digest(body), "recordedBodySHA256": digest([]byte(f.Request.PostData)), "requestBodyEqual": bytes.Equal(body, []byte(f.Request.PostData)), "differentHeaderNames": differentHeaders, "criticalRetryCopy": f.CriticalRetryCopy}
	if f.Failure != "" {
		event["kind"] = "recorded-failure"
		event["failureSHA256"] = digest([]byte(f.Failure))
		t.events = append(t.events, event)
		return nil, errors.New(f.Failure)
	}
	responseBody := append([]byte(nil), f.Body...)
	if t.segment != nil {
		a, b := t.segment.FindStringSubmatch(f.Request.URL), t.segment.FindStringSubmatch(r.URL.String())
		if len(a) > 1 && len(b) > 1 && a[1] != b[1] {
			responseBody = bytes.ReplaceAll(responseBody, []byte(a[1]), []byte(b[1]))
			event["dynamicSegmentMapping"] = true
		}
	}
	h := http.Header{}
	for k, v := range f.Headers {
		h.Set(k, v)
	}
	if t.httpDateShift != nil {
		rebaseHTTPDates(h, *t.httpDateShift)
		event["httpDateShiftSeconds"] = t.httpDateShift.Seconds()
	}
	h.Del("Content-Encoding")
	h.Del("Content-Length")
	event["status"] = f.Status
	event["responseBodySHA256"] = digest(responseBody)
	t.events = append(t.events, event)
	return &http.Response{StatusCode: f.Status, Header: h, Body: io.NopCloser(bytes.NewReader(responseBody)), Request: r, ContentLength: int64(len(responseBody))}, nil
}

// Translate absolute cache dates together; preserve age/lifetimes and never
// alter the source capture. This is an explicit replay environment control.
func rebaseHTTPDates(h http.Header, shift time.Duration) {
	for _, name := range []string{"Date", "Expires", "Last-Modified"} {
		if date, err := http.ParseTime(h.Get(name)); err == nil {
			h.Set(name, date.Add(shift).UTC().Format(http.TimeFormat))
		}
	}
}

func (t *replay) boundary(kind string, r *http.Request) error {
	t.events = append(t.events, map[string]any{"kind": kind, "method": r.Method, "urlHash": shortHash(r.URL.String()), "cycle": t.cycle})
	t.cancel()
	return fmt.Errorf("offline boundary: %s (%s %s)", kind, r.Method, shortHash(r.URL.String()))
}

func (t *replay) summary() map[string]any {
	t.mu.Lock()
	defer t.mu.Unlock()
	unused := []map[string]any{}
	unavailable := []int{}
	for i, f := range t.capture.Fixtures {
		if f.UnavailableBody {
			unavailable = append(unavailable, f.Index)
		}
		if !t.used[i] {
			unused = append(unused, map[string]any{"fixtureIndex": f.Index, "cycle": f.Cycle, "local": f.Local, "method": f.Request.Method, "urlHash": shortHash(f.Request.URL), "recordedFailure": f.Failure != "", "criticalRetryCopy": f.CriticalRetryCopy})
		}
	}
	return map[string]any{"transport": "captured occurrences only; no live fallback", "requests": append([]map[string]any(nil), t.events...), "unusedFixtures": unused, "unavailableSyntheticBodies": unavailable, "lastCycle": t.cycle, "sourceHashes": t.capture.SourceHashes, "requestBodyPolicy": "compare and report only; stored server responses do not evaluate submissions", "timingPolicy": "immediate saved responses; no wire timing or H3 replay", "dynamicSegmentPattern": fmt.Sprint(t.segment)}
}
