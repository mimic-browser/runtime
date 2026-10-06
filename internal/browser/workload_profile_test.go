package browser

import (
	"context"
	"crypto/sha256"
	"fmt"
	chrome152 "github.com/moreveal/mimic/chrome/152"
	gojaengine "github.com/moreveal/mimic/internal/engine/goja"
	"github.com/moreveal/mimic/internal/network"
	"github.com/moreveal/mimic/internal/workload"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestWorkloadProfileLiveAdmissionUsesNormalLoader(t *testing.T) {
	serialBrowserTest(t)
	var revision atomic.Int64
	var newRequests atomic.Int64
	var requests atomic.Int64
	body := `<!doctype html><script src="/optional.js"></script><p>ready</p>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/new.js" {
			newRequests.Add(1)
			w.Header().Set("Content-Type", "text/javascript")
			fmt.Fprint(w, `window.newResourceExecuted=true`)
			return
		}
		if r.URL.Path != "/" && r.URL.Path != "/untrained" {
			if r.URL.Path != "/optional.js" {
				http.NotFound(w, r)
				return
			}
			requests.Add(1)
			w.Header().Set("Content-Type", "text/javascript")
			fmt.Fprint(w, `window.executed=true`)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, body)
		if revision.Load() > 0 {
			fmt.Fprintf(w, `<p id="price">%d</p><script nonce="nonce-%d" src="/new.js"></script>`, revision.Load(), revision.Load())
		}
	}))
	defer server.Close()
	no := false
	profile := workload.Profile{Format: "mimic-workload-profile", Version: 1, RuntimeABI: workload.RuntimeABI, Engine: "goja", BrowserMode: "headful", Chrome: 152, BuildSHA256: strings.Repeat("a", 64), Confidence: "empirical-request-scoped", CaptureSHA256: []string{strings.Repeat("b", 64)}, Documents: []workload.DocumentEvidence{{URL: server.URL + "/", Status: 200, SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(body)))}}, Requests: []workload.RequestEvidence{{URL: server.URL + "/optional.js", Method: "GET", Kind: "script", DocumentURL: server.URL + "/", DocumentSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(body))), SourceURL: server.URL + "/", BodySHA256: fmt.Sprintf("%x", sha256.Sum256(nil)), HeadersSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("{}")))}}, Plan: workload.ExecutionPlan{Resources: []network.ResourceRule{{ID: "learned", Match: network.ResourceMatch{URLGlob: server.URL + "/optional.js"}, Work: network.ResourceWork{Network: &no, CacheRead: &no}}}}}
	b, err := NewWithOptions(gojaengine.Factory{}, chrome152.New(), Options{ExecutionProfile: &profile})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	// Mutating caller-owned evidence after construction cannot change the browser.
	profile.Plan.Resources = nil
	profile.Documents = nil
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	// Held-out HTML changes do not remove a trained exclusion. New resources,
	// and the same resource on an untrained route, use the normal loader.
	for _, trial := range []struct {
		path     string
		revision int64
		general  bool
	}{
		{"/", 0, false}, {"/", 1, false}, {"/", 2, false}, {"/untrained", 3, true},
	} {
		revision.Store(trial.revision)
		c := b.NewContext()
		p, err := c.NewPage()
		if err != nil {
			t.Fatal(err)
		}
		if err = p.Navigate(ctx, server.URL+trial.path); err != nil {
			t.Fatal(err)
		}
		v, err := p.Evaluate(ctx, `window.executed===true`)
		if err != nil || v != trial.general {
			t.Fatalf("trial=%+v author=%v err=%v", trial, v, err)
		}
		if trial.revision > 0 {
			v, err = p.Evaluate(ctx, `window.newResourceExecuted===true && document.getElementById('price').textContent === '`+fmt.Sprint(trial.revision)+`'`)
			if err != nil || v != true {
				t.Fatalf("held-out content/new script failed: %v %v", v, err)
			}
		}
		c.Close()
	}
	if requests.Load() != 1 || newRequests.Load() != 3 {
		t.Fatalf("known exclusions/new acquisitions: %d/%d", requests.Load(), newRequests.Load())
	}
}
