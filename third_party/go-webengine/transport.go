// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package engine

import (
	"net/http"
	"sync"
)

// maxPerHostConcurrency bounds concurrent in-flight requests to any single
// host — independent of, and complementary to, imgWorkers' own overall
// fetch-concurrency bound (images.go), which has no per-host awareness at
// all. Confirmed necessary live (round 57): this engine's own concurrent
// per-page image fetching can fire more truly-simultaneous requests at one
// CDN host (upload.wikimedia.org, serving many thumbnails on a single
// Wikipedia article) than that host's own rate limiter tolerates, tripping
// 429s that round 57's Retry-After-honouring retry then has to pay for with
// real added latency. This cap reduces how often the limit is hit in the
// first place, rather than only recovering after it is — the two are
// complementary, not alternatives: even a capped page can occasionally
// exceed a host's own (possibly much stricter, or momentarily tightened)
// limit, so the retry stays.
//
// 2, not the commonly-cited real-browser ~6 per-host convention, is what the
// live measurement actually supports: re-fetching en.wikipedia.org's own
// infobox-heavy article repeatedly against the real upload.wikimedia.org CDN
// showed 10 429 responses at a cap of 6, only 3 at a cap of 2, and STILL 3
// (no further improvement) at a cap of 1 — meaning some of Wikimedia's 429s
// are not a pure concurrency effect this cap can fully eliminate (a
// request-rate or cold-cache-coalescing limit, not simultaneity alone; the
// round 57 retry stays for exactly this residue), and 2 gets essentially all
// of the achievable reduction while still fetching two images at once rather
// than fully serialising. Chosen from measurement, not the generic
// convention, which measured no better here.
const maxPerHostConcurrency = 2

// perHostLimitedTransport wraps an http.RoundTripper, bounding concurrent
// in-flight requests to any single host (req.URL.Host, which already
// includes a non-default port) to limit via a per-host semaphore created
// lazily the first time each distinct host is seen. It never rejects a
// request outright — one beyond the limit simply waits for a slot to free up,
// or aborts with the request's own context error if that context is
// cancelled first (never a silent indefinite hang). Wrapping happens once,
// in New() — a caller that replaces Engine.Client entirely (every existing
// test that redirects requests to an httptest.Server does exactly this) gets
// no limiting at all, which is correct: a same-process test server has no
// real rate limit to protect.
type perHostLimitedTransport struct {
	rt    http.RoundTripper
	limit int

	mu   sync.Mutex
	sems map[string]chan struct{}
}

// newPerHostLimitedTransport wraps rt, bounding concurrent requests to any
// one host to limit. rt must be non-nil; callers needing http.DefaultTransport
// pass it explicitly, matching how http.Client itself requires an explicit
// choice rather than defaulting silently.
func newPerHostLimitedTransport(rt http.RoundTripper, limit int) *perHostLimitedTransport {
	return &perHostLimitedTransport{rt: rt, limit: limit, sems: make(map[string]chan struct{})}
}

// semFor returns the semaphore for host, creating it (buffered to t.limit)
// the first time host is seen. Guarded by t.mu only for the map access itself
// — the semaphore channel it returns is then used lock-free, so acquiring or
// releasing a slot never contends across different hosts.
func (t *perHostLimitedTransport) semFor(host string) chan struct{} {
	t.mu.Lock()
	defer t.mu.Unlock()
	sem, ok := t.sems[host]
	if !ok {
		sem = make(chan struct{}, t.limit)
		t.sems[host] = sem
	}
	return sem
}

// RoundTrip acquires host's slot before delegating to t.rt, releasing it once
// that call returns (success or error alike) so a slow or failed request
// still frees its slot for the next one. Waiting for a slot honours the
// request's own context, so a cancelled render aborts a queued request
// immediately rather than leaving it blocked on a semaphore no one will ever
// signal.
func (t *perHostLimitedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	sem := t.semFor(req.URL.Host)
	select {
	case sem <- struct{}{}:
	case <-req.Context().Done():
		return nil, req.Context().Err()
	}
	defer func() { <-sem }()
	return t.rt.RoundTrip(req)
}
