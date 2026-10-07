package browser

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	chrome152 "github.com/moreveal/mimic/chrome/152"
	"github.com/moreveal/mimic/internal/engine"
	gojaengine "github.com/moreveal/mimic/internal/engine/goja"
	v8engine "github.com/moreveal/mimic/internal/engine/v8"
)

func TestXPathCompiledExpressionChrome152(t *testing.T) {
	parallelBrowserTest(t)
	probe, err := os.ReadFile("testdata/xpath_compiled_probe.js")
	if err != nil {
		t.Fatal(err)
	}
	oracle, err := os.ReadFile("testdata/xpath_compiled_chrome152.json")
	if err != nil {
		t.Fatal(err)
	}
	var reference struct {
		Observations map[string]any `json:"observations"`
	}
	if err := json.Unmarshal(oracle, &reference); err != nil {
		t.Fatal(err)
	}
	for name, factory := range map[string]engine.Factory{"goja": gojaengine.Factory{}, "v8": v8engine.Factory{}} {
		t.Run(name, func(t *testing.T) {
			b, err := New(factory, chrome152.New())
			if err != nil {
				t.Fatal(err)
			}
			c := b.NewContext()
			defer c.Close()
			p, err := c.NewPage()
			if err != nil {
				t.Fatal(err)
			}
			value, err := p.Evaluate(context.Background(), string(probe))
			if err != nil {
				t.Fatal(err)
			}
			text, ok := value.(string)
			if !ok {
				t.Fatalf("XPath probe did not return observations: %v", value)
			}
			var actual map[string]any
			if err := json.Unmarshal([]byte(text), &actual); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(actual, reference.Observations) {
				t.Fatalf("compiled XPath differs from retained Chrome 152: %s", text)
			}
		})
	}
}

func TestXPathAutomationResultsUseCanonicalNodes(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	value, err := p.Evaluate(context.Background(), `(()=>{
 document.body.innerHTML='<main><button id="login"> Log   in </button><button data-action="login">Other</button></main>';
 const first=document.evaluate('//button[normalize-space(.)="Log in"]',document,null,XPathResult.FIRST_ORDERED_NODE_TYPE,null);
 const snapshot=document.evaluate('//*[@data-action="login"]',document,null,XPathResult.ORDERED_NODE_SNAPSHOT_TYPE,null);
 const iterator=document.evaluate('//button[contains(., "Log")]',document,null,XPathResult.ORDERED_NODE_ITERATOR_TYPE,null);
 const iterFirst=iterator.iterateNext(),iterSecond=iterator.iterateNext();
 return [first instanceof XPathResult,first.resultType,first.singleNodeValue===document.getElementById('login'),snapshot.snapshotLength,snapshot.snapshotItem(0)===document.querySelector('[data-action="login"]'),iterFirst===document.getElementById('login'),iterSecond===null];
})()`)
	want := []any{true, int64(9), true, int64(1), true, true, true}
	if err != nil || !reflect.DeepEqual(value, want) {
		t.Fatalf("xpath automation semantics: %v %v", value, err)
	}
}

func TestXPathMissingNodeReturnsResultInsteadOfUndefined(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	value, err := p.Evaluate(context.Background(), `(()=>{const r=document.evaluate('//button[@id="missing"]',document,null,XPathResult.FIRST_ORDERED_NODE_TYPE,null);return r.singleNodeValue===null})()`)
	if err != nil || value != true {
		t.Fatalf("xpath missing result: %v %v", value, err)
	}
}
