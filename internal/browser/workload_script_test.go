package browser

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestWorkloadClassicSuppressionKeepsAcquisitionAndLifecycle(t *testing.T) {
	serialBrowserTest(t)
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		if r.URL.Path == "/side.js" {
			requests.Add(1)
			fmt.Fprint(w, `events.push('author');`)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html>
<script>
  events = ['inline'];
  document.addEventListener('DOMContentLoaded', () => events.push('DCL'));
</script>
<script src="/side.js" onload="events.push('load:blocking')"></script>
<script defer src="/side.js" onload="events.push('load:defer')"></script>
<body><p>ready</p></body>`)
	}))
	defer server.Close()
	p := testPage(t)
	p.ctx.browser.classicScriptAdmission = func(url, source string, external bool) bool { return !external || !strings.HasSuffix(url, "/side.js") }
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := p.Navigate(ctx, server.URL); err != nil {
		t.Fatal(err)
	}
	v, err := p.Evaluate(ctx, `new Promise((resolve) => {
  const done = () => resolve(events.join('|'));
  if (document.readyState === 'complete') done();
  else addEventListener('load', done, {once: true});
})`)
	// The existing navigation path does not dispatch a parser-blocking load
	// event. This hook preserves that path; deferred/dynamic events are checked.
	if err != nil || fmt.Sprint(v) != "inline|load:defer|DCL" {
		t.Fatalf("%v %v", v, err)
	}
	if requests.Load() < 1 {
		t.Fatal("script was not acquired")
	}
	v, err = p.Evaluate(ctx, `new Promise((resolve) => {
  const script = document.createElement('script');
  script.src = '/side.js';
  script.onload = () => resolve(events.join('|') + '|dynamic');
  document.body.appendChild(script);
})`)
	if err != nil || strings.Contains(fmt.Sprint(v), "author") || !strings.HasSuffix(fmt.Sprint(v), "|dynamic") {
		t.Fatalf("dynamic: %v %v", v, err)
	}
}
