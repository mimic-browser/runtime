package browser

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestXHRBinaryBodyPreservesBytes(t *testing.T) {
	serialBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/echo" {
				io.WriteString(w, "<!doctype html><body>")
				return
			}
			body, _ := io.ReadAll(r.Body)
			json.NewEncoder(w).Encode([]string{r.Header.Get("Content-Type"), hex.EncodeToString(body)})
		}))
		defer server.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := p.Navigate(ctx, server.URL); err != nil {
			t.Fatal(err)
		}
		got, err := p.Evaluate(ctx, `new Promise(resolve => {
  const xhr = new XMLHttpRequest();
  xhr.open('POST', '/echo');
  xhr.setRequestHeader('Content-Type', 'application/octet-stream');
  xhr.onload = () => resolve(xhr.responseText.trim());
  xhr.send(new Uint8Array([0x92, 0xda, 0x02, 0xf6, 0xff, 0x80]));
})`)
		want := `["application/octet-stream","92da02f6ff80"]`
		if err != nil || got != want {
			t.Fatalf("binary XHR: got %v, %v; want %s", got, err, want)
		}
		got, err = p.Evaluate(ctx, `new Promise(resolve => {
  class EncodedProof extends Uint8Array {
    toString() { return new TextDecoder().decode(this); }
  }
  const xhr = new XMLHttpRequest();
  xhr.open('POST', '/echo');
  xhr.setRequestHeader('Content-Type', 'application/octet-stream');
  xhr.onload = () => resolve(xhr.responseText.trim());
  xhr.send(new EncodedProof([0x92, 0xda, 0x02, 0xf6, 0xff, 0x80]));
})`)
		if err != nil || got != want {
			t.Fatalf("XHR called binary body's text decoder: got %v, %v; want %s", got, err, want)
		}
		got, err = p.Evaluate(ctx, `new Promise(resolve => {
  const xhr = new XMLHttpRequest();
  xhr.open('POST', '/echo');
  xhr.setRequestHeader('Content-Type', 'application/octet-stream');
  xhr.onload = () => resolve(xhr.responseText.trim());
  xhr.send(new DataView(new Uint8Array([0, 0x92, 0xda, 0x02, 0xf6, 0xff, 0x80, 0]).buffer, 1, 6));
})`)
		if err != nil || got != want {
			t.Fatalf("XHR DataView byte range: got %v, %v; want %s", got, err, want)
		}
		got, err = p.Evaluate(ctx, `new Promise(resolve => {
  const xhr = new XMLHttpRequest();
  xhr.open('POST', '/echo');
  xhr.setRequestHeader('Content-Type', 'application/octet-stream');
  xhr.onload = () => resolve(xhr.responseText.trim());
  xhr.send(new Uint8Array([0x92, 0xda, 0x02, 0xf6, 0xff, 0x80]).buffer);
})`)
		if err != nil || got != want {
			t.Fatalf("XHR ArrayBuffer bytes: got %v, %v; want %s", got, err, want)
		}
	})
}

// Expected headers were received from frozen headful Chrome 152.0.7977.82
// by a local server (request-edges2-20260913-chrome diagnostic capture).
func TestXHRStringEncodingChrome152(t *testing.T) {
	serialBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/echo" {
				w.Header().Set("Content-Type", "text/html; charset=UTF-8")
				io.WriteString(w, "<!doctype html><body>")
				return
			}
			body, _ := io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "application/json; charset=UTF-8")
			json.NewEncoder(w).Encode([]string{r.Header.Get("Content-Type"), string(body)})
		}))
		defer server.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := p.Navigate(ctx, server.URL); err != nil {
			t.Fatal(err)
		}
		cases := [][2]string{
			{"application/x-www-form-urlencoded;charset=utf-8", "application/x-www-form-urlencoded;charset=UTF-8"},
			{"text/plain;charset=iso-8859-1", "text/plain;charset=UTF-8"},
			{"text/plain", "text/plain"},
			{`text/plain;foo=bar;charset="utf-8"`, `text/plain;foo=bar;charset="UTF-8"`},
			{`text/plain;foo=";charset=ascii";charset=ascii`, `text/plain;foo=";charset=UTF-8";charset=UTF-8`},
			{"text/plain; charset = utf-8", "text/plain; charset = UTF-8"},
			{"text/plain;CHARSET=latin1", "text/plain;CHARSET=UTF-8"},
			{"", "text/plain;charset=UTF-8"},
		}
		for _, tc := range cases {
			header, _ := json.Marshal(tc[0])
			for _, body := range []string{"Привет, мир 😀", ""} {
				encoded, _ := json.Marshal(body)
				got, err := p.Evaluate(ctx, `new Promise(resolve=>{const x=new XMLHttpRequest();x.open('POST','/echo');const h=`+string(header)+`;if(h)x.setRequestHeader('Content-Type',h);x.onload=()=>resolve(x.responseText.trim());x.send(`+string(encoded)+`)})`)
				want, _ := json.Marshal([]string{tc[1], body})
				if err != nil || got != string(want) {
					t.Fatalf("%q body %q: got %v, %v; want %s", tc[0], body, got, err, want)
				}
			}
		}
	})
}
