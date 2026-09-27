package browser

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
)

func TestLinkMetadataMatchesFrozenChrome(t *testing.T) {
	parallelBrowserTest(t)
	documentAllOracle(t, "link_metadata")
}

func TestLinkMetadataDoesNotAcquireResourcesOrChangeCookies(t *testing.T) {
	parallelBrowserTest(t)
	var unexpected atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if request.URL.Path != "/" {
			unexpected.Add(1)
			http.SetCookie(w, &http.Cookie{Name: "metadata_guard", Value: "changed", Path: "/"})
		}
		fmt.Fprint(w, `<!doctype html><link rel="icon" href="data:,"><body></body>`)
	}))
	t.Cleanup(server.Close)
	page := bootstrapSnapshotPage(t)
	if err := page.Navigate(context.Background(), server.URL); err != nil {
		t.Fatal(err)
	}
	bootstrapSnapshotEvaluate(t, page, `(async()=>{document.cookie='metadata_guard=initial; path=/'; await new Promise(resolve=>setTimeout(resolve,20));})()`)
	before := page.ctx.ResourcePolicyStats()
	source, err := os.ReadFile("testdata/link_metadata_oracle.js")
	if err != nil {
		t.Fatal(err)
	}
	bootstrapSnapshotEvaluate(t, page, string(source))
	if unexpected.Load() != 0 {
		t.Fatalf("metadata links issued %d HTTP requests", unexpected.Load())
	}
	if cookie := bootstrapSnapshotEvaluate(t, page, "document.cookie"); cookie != "metadata_guard=initial" {
		t.Fatalf("metadata links changed cookies: %v", cookie)
	}
	after := page.ctx.ResourcePolicyStats()
	if after.Requests != before.Requests || after.NetworkAcquisitions != before.NetworkAcquisitions || after.EncodedNetworkBodyBytes != before.EncodedNetworkBodyBytes || after.RetainedBodyBytes != before.RetainedBodyBytes {
		t.Fatalf("metadata links charged resource work: before=%+v after=%+v", before, after)
	}
}
