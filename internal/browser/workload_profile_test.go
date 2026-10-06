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
	var changed atomic.Bool
	var requests atomic.Int64
	body := `<!doctype html><script src="/optional.js"></script><p>ready</p>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			requests.Add(1)
			w.Header().Set("Content-Type", "text/javascript")
			fmt.Fprint(w, `window.executed=true`)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, body)
		if changed.Load() {
			fmt.Fprint(w, `<p>new experiment</p>`)
		}
	}))
	defer server.Close()
	no := false
	profile := workload.Profile{Format: "mimic-workload-profile", Version: 1, RuntimeABI: workload.RuntimeABI, Engine: "goja", BrowserMode: "headful", Chrome: 152, BuildSHA256: strings.Repeat("a", 64), Confidence: "empirical-document-guarded", CaptureSHA256: []string{strings.Repeat("b", 64)}, Documents: []workload.DocumentEvidence{{URL: server.URL + "/", Status: 200, SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(body)))}}, Requests: []workload.RequestEvidence{{URL: server.URL + "/optional.js", Method: "GET", Kind: "script", DocumentURL: server.URL + "/", DocumentSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(body))), SourceURL: server.URL + "/", BodySHA256: fmt.Sprintf("%x", sha256.Sum256(nil)), HeadersSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("{}")))}}, Plan: workload.ExecutionPlan{Resources: []network.ResourceRule{{ID: "learned", Match: network.ResourceMatch{URLGlob: server.URL + "/optional.js"}, Work: network.ResourceWork{Network: &no, CacheRead: &no}}}}}
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
	for _, general := range []bool{false, true} {
		changed.Store(general)
		c := b.NewContext()
		p, err := c.NewPage()
		if err != nil {
			t.Fatal(err)
		}
		if err = p.Navigate(ctx, server.URL+"/"); err != nil {
			t.Fatal(err)
		}
		v, err := p.Evaluate(ctx, `window.executed===true`)
		if err != nil || v != general {
			t.Fatalf("general=%v author=%v err=%v", general, v, err)
		}
		c.Close()
	}
	if requests.Load() != 1 {
		t.Fatalf("profile blocked unknown document or acquired known exclusion: %d", requests.Load())
	}
}
