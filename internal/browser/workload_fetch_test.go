package browser

import (
	"context"
	"fmt"
	"github.com/moreveal/mimic/internal/network"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPolicyFetchHeadersSurviveUnavailableBody(t *testing.T) {
	serialBrowserTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/payload" {
			w.Header().Set("X-Evidence", "present")
			w.Header().Set("Set-Cookie", "admitted=yes; Path=/")
			fmt.Fprint(w, strings.Repeat("payload", 1000))
			return
		}
		fmt.Fprint(w, "<!doctype html><body>ready</body>")
	}))
	defer server.Close()
	p := blitzStandardsPage(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := p.Navigate(ctx, server.URL); err != nil {
		t.Fatal(err)
	}
	if _, err := p.ctx.UpdateResourcePolicy(network.ResourcePolicy{Rules: []network.ResourceRule{{ID: "headers", Match: network.ResourceMatch{Kinds: []string{"fetch"}}, Work: network.ResourceWork{Body: "none"}}}}); err != nil {
		t.Fatal(err)
	}
	before := p.ctx.ResourcePolicyStats().EncodedNetworkBodyBytes
	value, err := p.Evaluate(ctx, `(async () => {
  const response = await fetch('/payload');
  const clone = response.clone();
  const errors = [];
  for (const candidate of [response, clone]) {
    try {
      await candidate.text();
      errors.push('unexpected');
    } catch (error) {
      errors.push(error.name);
    }
  }
  return [response.status, response.headers.get('x-evidence'), errors.join(','),
    response.bodyUsed, document.cookie.includes('admitted=yes')].join('|');
})()`)
	if err != nil || fmt.Sprint(value) != "200|present|TypeError,TypeError|true|true" {
		t.Fatalf("headers/body contract: %v %v", value, err)
	}
	if p.ctx.ResourcePolicyStats().EncodedNetworkBodyBytes != before {
		t.Fatal("headers-only response consumed HTTP body bytes")
	}
	if _, err := p.ctx.UpdateResourcePolicy(network.ResourcePolicy{}); err != nil {
		t.Fatal(err)
	}
	value, err = p.Evaluate(ctx, `(async () => {
  const response = await fetch('/payload');
  return (await response.text()).length;
})()`)
	if err != nil || fmt.Sprint(value) != "7000" {
		t.Fatalf("ordinary Fetch: %v %v", value, err)
	}
}
