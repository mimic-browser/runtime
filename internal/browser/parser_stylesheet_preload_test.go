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
)

func TestParserStylesheetLookaheadOverlapsBlockedScriptWithoutEarlyApplication(t *testing.T) {
	for _, frame := range []bool{false, true} {
		t.Run(fmt.Sprintf("frame=%t", frame), func(t *testing.T) {
			historyTestPages(t, func(t *testing.T, p *Page) {
				requested := make(chan struct{})
				var once sync.Once
				var styles atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Cache-Control", "no-store")
					switch r.URL.Path {
					case "/blank":
						w.Header().Set("Content-Type", "text/html")
						fmt.Fprint(w, "<!doctype html><body>")
					case "/held.js":
						select {
						case <-requested:
						case <-r.Context().Done():
							return
						case <-time.After(2 * time.Second):
							t.Error("stylesheet fetch waited for the blocking parser")
							return
						}
						w.Header().Set("Content-Type", "text/javascript")
						fmt.Fprint(w, `events = [
  document.querySelector('link') === null,
  getComputedStyle(document.getElementById('probe')).marginLeft,
];
queueMicrotask(() => events.push('micro'));`)
					case "/later.css":
						styles.Add(1)
						once.Do(func() { close(requested) })
						w.Header().Set("Content-Type", "text/css")
						fmt.Fprint(w, "#probe { margin-left: 7px; }")
					default:
						w.Header().Set("Content-Type", "text/html")
						fmt.Fprint(w, `<!doctype html>
<div id="probe"></div>
<script src="/held.js"></script>
<link rel="stylesheet" href="/later.css" />
<script>
  events.push(getComputedStyle(document.getElementById('probe')).marginLeft);
</script>`)
					}
				}))
				defer server.Close()
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				target := server.URL
				if frame {
					target += "/blank"
				}
				if err := p.Navigate(ctx, target); err != nil {
					t.Fatal(err)
				}
				expression := `events.join('|');`
				if frame {
					expression = `new Promise((resolve) => {
  const frame = document.createElement('iframe');
  frame.onload = () => resolve(frame.contentWindow.events.join('|'));
  frame.src = '/page';
  document.body.append(frame);
});`
				}
				got, err := p.Evaluate(ctx, expression)
				if err != nil || got != "true|0px|micro|7px" {
					t.Fatalf("parser stylesheet ordering: %v, %v", got, err)
				}
				if got := styles.Load(); got != 1 {
					t.Fatalf("stylesheet was fetched %d times", got)
				}
			})
		})
	}
}
