package browser

import (
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	chrome152 "github.com/moreveal/mimic/chrome/152"
	v8engine "github.com/moreveal/mimic/internal/engine/v8"
)

func TestScriptIntegrityStrongestMetadata(t *testing.T) {
	parallelBrowserTest(t)
	body := []byte("globalThis.integrityExecuted=true")
	other := []byte("globalThis.integrityExecuted=false")
	sha256Sum := sha256.Sum256(body)
	sha384Sum := sha512.Sum384(body)
	sha512Sum := sha512.Sum512(body)
	other384 := sha512.Sum384(other)
	good256 := "sha256-" + base64.StdEncoding.EncodeToString(sha256Sum[:])
	good384 := "sha384-" + base64.StdEncoding.EncodeToString(sha384Sum[:])
	good512 := "sha512-" + base64.StdEncoding.EncodeToString(sha512Sum[:])
	bad384 := "sha384-" + base64.StdEncoding.EncodeToString(other384[:])
	for _, tc := range []struct {
		name, metadata string
		wantError      bool
	}{
		{"empty", "", false},
		{"unknown algorithm", "sha3-unknown", false},
		{"sha256", good256, false},
		{"sha384", good384, false},
		{"sha512", good512, false},
		{"multiple strongest", bad384 + " " + good384, false},
		{"strongest wins", good256 + " " + bad384, true},
		{"mismatch", bad384, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := scriptIntegrityError(body, tc.metadata); (got != nil) != tc.wantError {
				t.Fatalf("integrity %q: got %v, wantError %v", tc.metadata, got, tc.wantError)
			}
		})
	}
}

func TestExternalScriptIntegrityBlocksExecution(t *testing.T) {
	serialBrowserTest(t)
	const code = "globalThis.integrityExecutions=(globalThis.integrityExecutions||0)+1"
	sum := sha256.Sum256([]byte(code))
	badSum := sha256.Sum256([]byte("different"))
	good := "sha256-" + base64.StdEncoding.EncodeToString(sum[:])
	bad := "sha256-" + base64.StdEncoding.EncodeToString(badSum[:])
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/same.js" {
			w.Header().Set("Content-Type", "application/javascript")
			fmt.Fprint(w, code)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<!doctype html><body>
<script id="parser-good" src="/same.js" integrity=%q></script>
<script id="parser-bad" src="/same.js" integrity=%q></script>
<script id="async-bad" src="/same.js" integrity=%q async></script>
<script id="defer-bad" src="/same.js" integrity=%q defer></script>
<script>globalThis.parserContinued=true</script>`, good, bad, bad, bad)
	}))
	defer server.Close()
	b, err := New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	c := b.NewContext()
	defer c.Close()
	p, err := c.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	p.AddInitScript(`globalThis.integrityErrors=[];document.addEventListener('error',e=>{if(e.target instanceof HTMLScriptElement)integrityErrors.push(e.target.id)},true)`)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := p.Navigate(ctx, server.URL); err != nil {
		t.Fatal(err)
	}
	value, err := p.Evaluate(ctx, `(async()=>{
  const make=(id,integrity)=>new Promise(resolve=>{const s=document.createElement('script');s.id=id;s.src='/same.js';s.integrity=integrity;s.onload=()=>resolve('load');s.onerror=()=>resolve('error');document.body.append(s)});
  const events=await Promise.all([make('dynamic-good',`+fmt.Sprintf("%q", good)+`),make('dynamic-bad',`+fmt.Sprintf("%q", bad)+`)]);
  return JSON.stringify({executions:integrityExecutions,continued:parserContinued,errors:integrityErrors.sort(),events});
})()`)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"executions":2,"continued":true,"errors":["async-bad","defer-bad","dynamic-bad","parser-bad"],"events":["load","error"]}`
	if value != want {
		t.Fatalf("script integrity: got %v, want %s", value, want)
	}
}
