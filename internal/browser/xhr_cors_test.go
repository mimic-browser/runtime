package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func TestXHRResponseHeaderBoundary(t *testing.T) {
	parallelBrowserTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if r.URL.Path == "/document" {
			fmt.Fprint(w, "<!doctype html><title>XHR boundary</title>")
			return
		}
		w.Header().Set("Set-Cookie", "private=secret; Path=/; HttpOnly")
		w.Header().Set("Set-Cookie2", "legacy=secret")
		w.Header().Set("X-Visible", "visible")
		fmt.Fprint(w, "body")
	}))
	defer server.Close()
	historyTestPages(t, func(t *testing.T, page *Page) {
		if err := page.Navigate(context.Background(), server.URL+"/document"); err != nil {
			t.Fatal(err)
		}
		result, err := page.Evaluate(context.Background(), `new Promise(resolve => {
  const xhr = new XMLHttpRequest();
  xhr.open('GET', '/headers');
  xhr.onload = () => resolve(JSON.stringify({
    visible: xhr.getResponseHeader('X-Visible'),
    cookie: xhr.getResponseHeader('Set-Cookie'),
    cookie2: xhr.getResponseHeader('Set-Cookie2'),
    allLeaksCookie: /set-cookie/i.test(xhr.getAllResponseHeaders()),
    documentLeaksCookie: document.cookie.includes('private='),
  }));
  xhr.send();
})`)
		if err != nil || result != `{"visible":"visible","cookie":null,"cookie2":null,"allLeaksCookie":false,"documentLeaksCookie":false}` {
			t.Fatalf("XHR response boundary: %v %v", result, err)
		}
		stored := false
		for _, cookie := range page.Cookies().All() {
			stored = stored || cookie.Name == "private" && cookie.Value == "secret" && cookie.HttpOnly
		}
		if !stored {
			t.Fatal("the script header boundary discarded the canonical network cookie")
		}
	})
}

func TestXHRMatchesFrozenCORSBoundary(t *testing.T) {
	parallelBrowserTest(t)
	var preflight atomic.Bool
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if r.URL.Path == "/document" {
			fmt.Fprint(w, "<!doctype html><title>XHR boundary</title>")
			return
		}
		if r.URL.Path != "/deny" {
			origin := r.Header.Get("Origin")
			if r.URL.Path == "/wildcard" {
				origin = "*"
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Expose-Headers", "X-Visible")
		}
		if r.Method == "OPTIONS" {
			expected := r.Header.Get("Access-Control-Request-Method") == "PUT" && strings.ToLower(r.Header.Get("Access-Control-Request-Headers")) == "x-probe"
			if r.URL.Path == "/upload" {
				expected = r.Header.Get("Access-Control-Request-Method") == "POST" && r.Header.Get("Access-Control-Request-Headers") == ""
			}
			preflight.Store(expected && r.Header.Get("Cookie") == "")
			w.Header().Set("Access-Control-Allow-Methods", "PUT,POST")
			w.Header().Set("Access-Control-Allow-Headers", "X-Probe")
			return
		}
		w.Header().Set("Set-Cookie", "private=secret; Path=/; HttpOnly")
		w.Header().Set("Set-Cookie2", "legacy=secret")
		w.Header().Set("X-Visible", "visible")
		w.Header().Set("X-Hidden", "hidden")
		if (r.URL.Path == "/preflight" || r.URL.Path == "/upload") && preflight.Load() {
			fmt.Fprint(w, "preflight")
		} else {
			fmt.Fprint(w, "body")
		}
	})
	source := httptest.NewServer(handler)
	defer source.Close()
	target := httptest.NewServer(handler)
	defer target.Close()
	probe, err := os.ReadFile("testdata/xhr_cors_oracle.js")
	if err != nil {
		t.Fatal(err)
	}
	golden, err := os.ReadFile("testdata/xhr_cors_chrome152.json")
	if err != nil {
		t.Fatal(err)
	}
	var want any
	if err := json.Unmarshal(golden, &want); err != nil {
		t.Fatal(err)
	}
	historyTestPages(t, func(t *testing.T, page *Page) {
		preflight.Store(false)
		if err := page.Navigate(context.Background(), source.URL+"/document"); err != nil {
			t.Fatal(err)
		}
		result, err := page.Evaluate(context.Background(), string(probe)+"\nxhrCORSObservations("+fmt.Sprintf("%q", target.URL)+")")
		if err != nil {
			t.Fatal(err)
		}
		var got any
		if err := json.Unmarshal([]byte(result.(string)), &got); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("XHR CORS differs from frozen Chrome: got=%v want=%v", got, want)
		}
	})
}
