package browser

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	chrome152 "github.com/moreveal/mimic/chrome/152"
	v8engine "github.com/moreveal/mimic/internal/engine/v8"
	"github.com/moreveal/mimic/internal/network"
)

func TestDeniedSpeculationDoesNotPoisonDemandedModule(t *testing.T) {
	serialBrowserTest(t)
	var unused, required atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<link rel="modulepreload" href="/unused.js"><link rel="modulepreload" href="/required.js"><script type="module">import {value} from '/required.js';window.result=value;</script>`)
		case "/unused.js":
			unused.Add(1)
			fmt.Fprint(w, `export const unused=true`)
		case "/required.js":
			required.Add(1)
			fmt.Fprint(w, `export const value=42`)
		}
	}))
	defer server.Close()
	b, err := New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	c := b.NewContext()
	defer c.Close()
	p, err := c.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.ctx.UpdateResourcePolicy(network.ResourcePolicy{Presets: []string{"noSpeculativeLoads"}}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	if err := p.Navigate(ctx, server.URL); err != nil {
		t.Fatal(err)
	}
	value, err := p.Evaluate(ctx, `window.result`)
	if err != nil || value != float64(42) {
		t.Fatalf("demanded module failed: %v %v", value, err)
	}
	if unused.Load() != 0 || required.Load() != 1 {
		t.Fatalf("speculation/demand acquisitions: %d/%d", unused.Load(), required.Load())
	}
}

func TestModulePreloadsShareParallelFetchesAndPreserveEvaluationOrder(t *testing.T) {
	serialBrowserTest(t)
	var mu sync.Mutex
	counts := map[string]int{}
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.URL.Path == "/" {
			fmt.Fprint(w, `<link rel="modulepreload" href="/a.js"><link rel="modulepreload" href="/b.js"><script type="module" src="/a.js"></script><script type="module" src="/a.js"></script>`)
			return
		}
		if r.URL.Path != "/a.js" && r.URL.Path != "/b.js" {
			return
		}
		mu.Lock()
		counts[r.URL.Path]++
		mu.Unlock()
		select {
		case started <- struct{}{}:
		default:
		}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "text/javascript")
		if r.URL.Path == "/a.js" {
			fmt.Fprint(w, `import './b.js';globalThis.order.push('a')`)
		} else {
			fmt.Fprint(w, `globalThis.order=['b']`)
		}
	}))
	defer server.Close()
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	b, err := New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	c := b.NewContext()
	defer c.Close()
	p, err := c.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- p.Navigate(ctx, server.URL) }()
	for range 2 {
		select {
		case <-started:
		case <-ctx.Done():
			close(release)
			<-done
			t.Fatal("module preloads did not overlap")
		}
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	value, err := p.Evaluate(ctx, `order.join(',')`)
	if err != nil || value != "b,a" {
		t.Fatalf("evaluation: %v %v", value, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if counts["/a.js"] != 1 || counts["/b.js"] != 1 {
		t.Fatalf("no-store module fetches: %v", counts)
	}
}
