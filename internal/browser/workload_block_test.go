package browser

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/moreveal/mimic/internal/network"
)

func TestWorkloadBlockedParserResourcesComplete(t *testing.T) {
	serialBrowserTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<!doctype html><link rel=stylesheet href='/style.css'><script src='/side.js'></script><script>window.vendor || document.write('<script src="/fallback.js"><\/script>')</script><img src='/image.png'><p>ready</p><script>window.tailRan=true</script>`))
	}))
	defer server.Close()
	p := testPage(t)
	no := false
	_, err := p.ctx.UpdateResourcePolicy(network.ResourcePolicy{Rules: []network.ResourceRule{{ID: "doc", Match: network.ResourceMatch{Kinds: []string{"document"}}}, {ID: "block", Work: network.ResourceWork{Network: &no, CacheRead: &no}}}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := p.Navigate(ctx, server.URL); err != nil {
		t.Fatal(err)
	}
	value, err := p.Evaluate(ctx, `document.readyState==='complete' && window.tailRan===true && document.querySelector('p').textContent==='ready'`)
	if err != nil {
		t.Fatal(err)
	}
	if value != true {
		t.Fatalf("parser continuation was lost: %v", value)
	}
}
