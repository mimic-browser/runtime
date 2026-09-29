package browser

import (
	"github.com/moreveal/mimic/internal/engine"
	"github.com/moreveal/mimic/internal/trace"
	"strings"
	"testing"
)

func TestSemanticBoundaryDiagnostics(t *testing.T) {
	parallelBrowserTest(t)
	r := trace.New()
	r.Start()
	for _, owner := range []string{"realm", "worker"} {
		recordSemanticBoundary(r, owner, "test", "fixture.js:1", []engine.Value{diagnosticValue("CSS.fontSizeResolution"), diagnosticValue(`{"reason":"unsupported-expression","value":"2ex"}`)})
		e := r.Events()[len(r.Events())-1]
		if e.Data[owner] != "test" || e.Data["reason"] != "unsupported-expression" || e.Data["site"] != "fixture.js:1" || e.Data["reasonAvailable"] != true {
			t.Fatalf("lost context: %v", e.Data)
		}
	}
	recordSemanticBoundary(r, "realm", "test", "fixture.js:2", []engine.Value{diagnosticValue("Canvas2D.approximateTextObservations")})
	e := r.Events()[2]
	if e.Data["category"] != "approximation" || e.Data["reasonAvailable"] != false {
		t.Fatal(e.Data)
	}
	recordSemanticBoundary(r, "realm", "test", "fixture.js:3", []engine.Value{diagnosticValue("bounded"), diagnosticValue(strings.Repeat("я", 3000))})
	e = r.Events()[3]
	if e.Data["detailTruncated"] != true || len([]rune(e.Data["detail"].(string))) != 2048 {
		t.Fatal("unbounded diagnostic")
	}
}

type diagnosticValue string

func (v diagnosticValue) Export() any    { return string(v) }
func (v diagnosticValue) String() string { return string(v) }
