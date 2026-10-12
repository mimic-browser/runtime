package browser

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	chrome152 "github.com/moreveal/mimic/chrome/152"
	v8engine "github.com/moreveal/mimic/internal/engine/v8"
)

var trustedTypesPolicies = map[string]string{
	"wildcard": "trusted-types * 'allow-duplicates'", "reportonly": "require-trusted-types-for 'script'; trusted-types 'none'",
	"open": "", "required": "require-trusted-types-for 'script'",
	"restricted": "require-trusted-types-for 'script'; trusted-types allowed default",
	"none":       "trusted-types 'none'", "duplicates": "trusted-types allowed default 'allow-duplicates'",
	"noeval": "script-src 'self' 'unsafe-inline'; require-trusted-types-for 'script'",
}

func TestTrustedTypesSnapshotIsolation(t *testing.T) {
	serialBrowserTest(t)
	if os.Getenv("MIMIC_DISABLE_BOOTSTRAP_SNAPSHOT") == "1" {
		t.Skip("requires bootstrap restore")
	}
	seed := bootstrapSnapshotPage(t)
	bootstrapSnapshotWarm(t, seed)
	const require = `const meta=document.createElement('meta');meta.httpEquiv='Content-Security-Policy';meta.content="require-trusted-types-for 'script'";document.head.appendChild(meta);`
	bootstrapSnapshotEvaluate(t, seed, `(()=>{`+require+`trustedTypes.createPolicy('default',{createHTML:s=>'seed'});return true})()`)
	for i := 0; i < 3; i++ {
		page, err := seed.ctx.NewPage()
		if err != nil {
			t.Fatal(err)
		}
		value := bootstrapSnapshotEvaluate(t, page, `(()=>{const out={fresh:trustedTypes.defaultPolicy===null};document.body.innerHTML='open';`+require+`try{eval('42');out.blocked=false}catch(e){out.blocked=e.name==='EvalError'};let calls=[];trustedTypes.createPolicy('default',{createHTML:(...a)=>{calls.push(a);return a[0]},createScript:s=>s});document.body.innerHTML='own';out.html=document.body.innerHTML;out.calls=calls.length;out.eval=eval('42');return out})()`)
		got := value.(map[string]any)
		if !page.Top.Realm.bootstrapRestored {
			t.Fatal("expected restored bootstrap")
		}
		if got["fresh"] != true || got["blocked"] != true || got["html"] != "own" || numberValue(got["calls"]) != 1 || numberValue(got["eval"]) != 42 {
			t.Fatalf("restored policy state: %v", got)
		}
	}
}

func TestTrustedTypesConcurrentPages(t *testing.T) {
	parallelBrowserTest(t)
	server := trustedTypesServer(t)
	b, err := New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	bc := b.NewContext()
	t.Cleanup(func() { _ = bc.Close() })
	for i := 0; i < 4; i++ {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			t.Parallel()
			p, err := bc.NewPage()
			if err != nil {
				t.Fatal(err)
			}
			if err = p.Navigate(context.Background(), server.URL+"/?policy=required"); err != nil {
				t.Fatal(err)
			}
			value, err := p.Evaluate(context.Background(), `(()=>{let calls=0;trustedTypes.createPolicy('default',{createHTML:s=>{calls++;return s},createScript:s=>{calls++;return s}});for(let i=0;i<20;i++){document.body.innerHTML='own';if(eval('21*2')!==42)throw Error('eval')}return calls})()`)
			if err != nil || numberValue(value) != 40 {
				t.Fatalf("concurrent policy state: %v, %v", value, err)
			}
		})
	}
}

func trustedTypesServer(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/trusted-import.js" {
			w.Header().Set("Content-Type", "text/javascript")
			_, _ = w.Write([]byte("globalThis.imported = (globalThis.imported || 0) + 1;"))
			return
		}
		w.Header().Set("Content-Type", "text/html")
		if policy := trustedTypesPolicies[r.URL.Query().Get("policy")]; policy != "" {
			header := "Content-Security-Policy"
			if r.URL.Query().Get("policy") == "reportonly" {
				header += "-Report-Only"
			}
			w.Header().Set(header, policy)
		}
		_, _ = w.Write([]byte(`<!doctype html><title>Trusted Types oracle</title><body><div id="box"></div><iframe id="blank"></iframe></body>`))
	}))
	t.Cleanup(s.Close)
	return s
}

// The retained observations are from the exact .82 headful oracle. Compare
// exception names, returned values, and every policy argument in order. Existing
// DOM parser diagnostics have separate tests; TT diagnostics are checked below.
func TestTrustedTypesChrome152(t *testing.T) {
	parallelBrowserTest(t)
	var oracle struct {
		Cases map[string]map[string]any `json:"cases"`
	}
	data, err := os.ReadFile("testdata/trusted_types_chrome152.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &oracle); err != nil {
		t.Fatal(err)
	}
	server := trustedTypesServer(t)
	b, err := New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(oracle.Cases))
	for key := range oracle.Cases {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			bc := b.NewContext()
			defer bc.Close()
			p, err := bc.NewPage()
			if err != nil {
				t.Fatal(err)
			}
			parts := strings.Split(key, ":")
			mode := ""
			if len(parts) > 2 {
				mode = parts[2]
			}
			if parts[1] == "bypass" && mode == "before" {
				p.SetBypassCSP(true)
			}
			if err := p.Navigate(context.Background(), server.URL+"/?policy="+parts[0]); err != nil {
				t.Fatal(err)
			}
			if parts[1] == "bypass" {
				p.SetBypassCSP(true)
			}
			source, err := os.ReadFile("testdata/trusted_types_" + parts[1] + "_oracle.js")
			if err != nil {
				t.Fatal(err)
			}
			quoted, _ := json.Marshal(mode)
			value, err := p.Evaluate(context.Background(), strings.ReplaceAll(string(source), `"$MODE"`, string(quoted)))
			if err != nil {
				t.Fatal(err)
			}
			encoded, _ := json.Marshal(value)
			var actual map[string]any
			if err = json.Unmarshal(encoded, &actual); err != nil {
				t.Fatal(err)
			}
			for name, want := range oracle.Cases[key] {
				got := actual[name]
				// Exact messages are relevant to TT/CSP; unrelated DOM syntax errors
				// are compared by exception class and policy invocation ordering.
				if x, ok := want.(map[string]any); ok {
					message, _ := x["message"].(string)
					if !strings.Contains(message, "This document requires") && !strings.Contains(message, "Evaluating a string as JavaScript violates") {
						delete(x, "message")
						if y, ok := got.(map[string]any); ok {
							delete(y, "message")
						}
					}
				}
				if !reflect.DeepEqual(got, want) {
					t.Error(strings.NewReplacer("ordinary=", "Chrome=", "restored=", "Mimic=").Replace(bootstrapSnapshotDifference(name, want, got)))
				}
			}
		})
	}
}
