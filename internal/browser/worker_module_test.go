//go:build (windows || linux) && amd64

package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

const moduleWorkerResult = `(credentials) =>
  new Promise((resolve) => {
    const worker = new Worker('/entry.js', { type: 'module', credentials });
    worker.onmessage = (event) => {
      worker.terminate();
      resolve(event.data);
    };
    worker.onerror = (event) => {
      event.preventDefault();
      worker.terminate();
      resolve('error');
    };
  })`

func moduleWorkerHandler(w http.ResponseWriter, r *http.Request) {
	redirects := map[string]string{"/redirect-start.js": "/module/redirect-entry.js", "/alias-a.js": "/module/shared.js", "/alias-b.js": "/module/shared.js"}
	if target := redirects[r.URL.Path]; target != "" {
		http.Redirect(w, r, target, http.StatusFound)
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/module/") {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<!doctype html><body>module workers</body>"))
		return
	}
	data, err := os.ReadFile("testdata/worker_module/" + strings.TrimPrefix(r.URL.Path, "/module/"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/javascript")
	if r.URL.Path == "/module/mime.js" {
		w.Header().Set("Content-Type", "text/plain")
	}
	_, _ = w.Write(data)
}

func TestModuleWorkerCredentialsCORSAndCSP(t *testing.T) {
	serialBrowserTest(t)
	for _, tc := range []struct {
		name, credentials string
		denied, csp       bool
		want              any
	}{
		{"same-origin", "same-origin", false, false, float64(42)},
		{"include", "include", false, false, float64(42)},
		{"omit", "omit", false, false, float64(42)},
		{"cors-denied", "include", true, false, "error"},
		{"csp-denied", "same-origin", false, true, "error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rootCookies, remoteCookies, origins := make(chan string, 1), make(chan string, 1), make(chan string, 1)
			remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				remoteCookies <- r.Header.Get("Cookie")
				origins <- r.Header.Get("Origin")
				if !tc.denied {
					w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
					w.Header().Set("Access-Control-Allow-Credentials", "true")
				}
				w.Header().Set("Content-Type", "text/javascript")
				fmt.Fprint(w, `export default 42`)
			}))
			defer remote.Close()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/entry.js" {
					rootCookies <- r.Header.Get("Cookie")
					w.Header().Set("Content-Type", "text/javascript")
					if tc.csp {
						w.Header().Set("Content-Security-Policy", "script-src 'none'")
					}
					fmt.Fprintf(w, "import value from %s;\npostMessage(value);\n", strconv.Quote(remote.URL+"/remote.js"))
					return
				}
				w.Header().Set("Set-Cookie", "moduleCookie=1; Path=/")
				fmt.Fprint(w, "<!doctype html><body>fixture</body>")
			}))
			defer server.Close()
			p := bootstrapSnapshotPage(t)
			if err := p.Navigate(context.Background(), server.URL); err != nil {
				t.Fatal(err)
			}
			script := "(" + moduleWorkerResult + ")(" + strconv.Quote(tc.credentials) + ")"
			if got := bootstrapSnapshotEvaluate(t, p, script); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			wantRoot := "moduleCookie=1"
			if tc.credentials == "omit" {
				wantRoot = ""
			}
			if got := <-rootCookies; got != wantRoot {
				t.Fatalf("entry cookies %q; want %q", got, wantRoot)
			}
			if tc.csp {
				select {
				case <-remoteCookies:
					t.Fatal("CSP-denied module fetched")
				default:
				}
			} else {
				wantRemote := ""
				if tc.credentials == "include" {
					wantRemote = "moduleCookie=1"
				}
				if got := <-remoteCookies; got != wantRemote {
					t.Fatalf("dependency cookies %q; want %q", got, wantRemote)
				}
				if got := <-origins; got != server.URL {
					t.Fatalf("Origin %q; want %q", got, server.URL)
				}
			}
		})
	}
}

func TestModuleWorkerTerminationCancelsGraphFetch(t *testing.T) {
	serialBrowserTest(t)
	for _, mode := range []string{"terminate", "page-close"} {
		t.Run(mode, func(t *testing.T) {
			started, canceled := make(chan struct{}), make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/javascript")
				switch r.URL.Path {
				case "/entry.js":
					fmt.Fprint(w, `import './pending.js'`)
				case "/pending.js":
					close(started)
					<-r.Context().Done()
					close(canceled)
				default:
					w.Header().Set("Content-Type", "text/html")
					fmt.Fprint(w, "<!doctype html><body>fixture</body>")
				}
			}))
			defer server.Close()
			p := bootstrapSnapshotPage(t)
			defer p.Close()
			if err := p.Navigate(context.Background(), server.URL); err != nil {
				t.Fatal(err)
			}
			bootstrapSnapshotEvaluate(t, p, `globalThis.moduleWorker=new Worker('/entry.js',{type:'module'});true`)
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("graph fetch did not start")
			}
			if mode == "terminate" {
				bootstrapSnapshotEvaluate(t, p, `moduleWorker.terminate();true`)
			} else if err := p.Close(); err != nil {
				t.Fatal(err)
			}
			select {
			case <-canceled:
			case <-time.After(time.Second):
				t.Fatal("graph fetch survived teardown")
			}
		})
	}
}

func TestModuleWorkerMatchesFrozenChrome(t *testing.T) {
	serialBrowserTest(t)
	for _, fixture := range []string{"worker_module", "worker_module_edges"} {
		t.Run(fixture, func(t *testing.T) { moduleWorkerOracle(t, fixture) })
	}
}

func moduleWorkerOracle(t *testing.T, fixture string) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(moduleWorkerHandler))
	defer server.Close()
	p := bootstrapSnapshotPage(t)
	if err := p.Navigate(context.Background(), server.URL); err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile("testdata/" + fixture + "_oracle.js")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("testdata/" + fixture + "_chrome152.json")
	if err != nil {
		t.Fatal(err)
	}
	var capture struct {
		Result struct {
			Result struct{ Value map[string]any }
		}
	}
	if err := json.Unmarshal(data, &capture); err != nil {
		t.Fatal(err)
	}
	value := bootstrapSnapshotEvaluate(t, p, "(async()=>JSON.stringify(await "+string(source)+"))()")
	var got map[string]any
	if err := json.Unmarshal([]byte(value.(string)), &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, capture.Result.Result.Value) {
		t.Fatalf("module worker: got %#v; want %#v", got, capture.Result.Result.Value)
	}
}
