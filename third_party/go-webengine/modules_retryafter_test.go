// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// A module answered 429 with Retry-After: 1 must be fetched again only after
// that second has passed. The fixed 150ms/300ms backoff retried inside the
// window, drew another 429, ran out of attempts and dropped the module.
func TestFetchModuleSourceHonoursRetryAfter(t *testing.T) {
	var mu sync.Mutex
	var first time.Time
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if first.IsZero() {
			first = time.Now()
		}
		if time.Since(first) < time.Second {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte("export const x = 1;"))
	}))
	defer srv.Close()
	e := New()
	start := time.Now()
	src, ok := e.fetchModuleSource(context.Background(), srv.URL+"/chunk.js")
	elapsed := time.Since(start)
	if !ok || src != "export const x = 1;" {
		t.Fatalf("module not fetched: ok=%v src=%q", ok, src)
	}
	if elapsed < 900*time.Millisecond {
		t.Errorf("retried after %v, want to wait for Retry-After (about 1s)", elapsed)
	}
}
