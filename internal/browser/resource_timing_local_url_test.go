package browser

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Frozen headful Chrome 152 exposes neither a data favicon nor a data fetch
// in Resource Timing. Completion after clearResourceTimings must agree too.
func TestResourceTimingExcludesDataURLLoads(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `<!doctype html><link rel="icon" href="data:,"><title>Timing</title>`)
		}))
		defer server.Close()
		ctx := context.Background()
		if err := p.Navigate(ctx, server.URL); err != nil {
			t.Fatal(err)
		}
		if err := p.AdvanceTime(ctx, time.Second); err != nil {
			t.Fatal(err)
		}
		historyEval(t, p, `(async()=>{
const initial=performance.getEntriesByType('resource').length;
performance.clearResourceTimings();
const text=await(await fetch('data:text/plain,probe')).text();
await new Promise(resolve=>setTimeout(resolve,20));
return initial===0 && text==='probe' && performance.getEntriesByType('resource').length===0;
})()`, true)
	})
}
