package browser

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func TestOriginAgentClusterResponseAdmission(t *testing.T) {
	for _, secure := range []bool{false, true} {
		for _, header := range []string{"", "?0", "?1", "invalid", " ?0 "} {
			want := secure && strings.TrimSpace(header) != "?0"
			if got := originAgentClusterForResponse(secure, header); got != want {
				t.Fatalf("secure=%v header=%q: %v", secure, header, got)
			}
		}
	}
	historyTestPages(t, func(t *testing.T, page *Page) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/absent" {
				w.Header().Set("Origin-Agent-Cluster", strings.TrimPrefix(r.RequestURI, "/"))
			}
			fmt.Fprint(w, "<!doctype html>")
		}))
		defer server.Close()
		for _, path := range []string{"absent", "?0", "?1", "invalid", "absent"} {
			if err := page.Navigate(context.Background(), server.URL+"/"+path); err != nil {
				t.Fatal(err)
			}
			historyEval(t, page, "originAgentCluster", path != "?0")
		}
	})
}

func TestOriginAgentClusterFrameChoices(t *testing.T) {
	historyTestPages(t, func(t *testing.T, page *Page) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/absent" {
				w.Header().Set("Origin-Agent-Cluster", strings.TrimPrefix(r.RequestURI, "/"))
			}
			fmt.Fprint(w, `<!doctype html><script>if(parent!==self)parent.postMessage(originAgentCluster,'*')</script>`)
		}))
		defer server.Close()
		address, _ := url.Parse(server.URL)
		cross := "http://localhost:" + address.Port()
		for _, parentHeader := range []string{"absent", "?0"} {
			for _, first := range []string{"absent", "?0"} {
				if err := page.Navigate(context.Background(), server.URL+"/"+parentHeader); err != nil {
					t.Fatal(err)
				}
				same := parentHeader != "?0"
				other := first != "?0"
				historyEval(t, page, "globalThis.clusterChoices=[];true", true)
				// Bound each navigation, rather than timing eight independent
				// engine bootstraps as one operation. Keep the same parent document
				// and decision group throughout removal and replacement.
				for _, base := range []string{server.URL, cross} {
					wantChoice := same
					if base == cross {
						wantChoice = other
					}
					for _, header := range []string{first, "?1", "absent", "?0"} {
						script := `(async () => {
  const frame = document.createElement('iframe');
  const choice = await new Promise(resolve => {
    const listener = event => {
      if (event.source !== frame.contentWindow) return;
      removeEventListener('message', listener);
      resolve(event.data);
    };
    addEventListener('message', listener);
    frame.src = ` + strconv.Quote(base+"/"+header) + `;
    document.body.append(frame);
  });
  clusterChoices.push(choice);
  frame.remove();
  return choice;
})()`
						historyEval(t, page, script, wantChoice)
					}
				}
				want := fmt.Sprintf("[%v,%v,%v,%v,%v,%v,%v,%v]", same, same, same, same, other, other, other, other)
				historyEval(t, page, "JSON.stringify(clusterChoices)", want)
			}
		}
	})
}
