//go:build (windows || linux) && amd64

package browser

import (
	"context"
	_ "embed"
	"strconv"
	"strings"
	"testing"

	chrome152 "github.com/moreveal/mimic/chrome/152"
	v8engine "github.com/moreveal/mimic/internal/engine/v8"
)

//go:embed testdata/domexception_platform_stack.js
var domExceptionPlatformStackProbe string

func TestDOMExceptionPlatformStacksMatchFrozenChrome(t *testing.T) {
	serialBrowserTest(t)
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
	ctx := context.Background()
	got, err := p.Evaluate(ctx, domExceptionPlatformStackProbe)
	if err != nil || got != "ok" {
		t.Fatalf("Window DOMException stack: %v %v", got, err)
	}
	source := "postMessage(" + strings.TrimSuffix(strings.TrimSpace(domExceptionPlatformStackProbe), ";") + ")"
	got, err = p.Evaluate(ctx, `new Promise((resolve, reject) => {
  const url = URL.createObjectURL(new Blob([`+strconv.Quote(source)+`]));
  const worker = new Worker(url);
  worker.onmessage = event => {
    worker.terminate();
    URL.revokeObjectURL(url);
    resolve(event.data);
  };
  worker.onerror = event => {
    worker.terminate();
    URL.revokeObjectURL(url);
    reject(new Error(event.message));
  };
})`)
	if err != nil || got != "ok" {
		t.Fatalf("Worker DOMException stack: %v %v", got, err)
	}
}

func TestDOMExceptionStateMatchesFrozenChrome(t *testing.T) {
	parallelBrowserTest(t)
	documentAllOracle(t, "domexception_state")
	documentAllOracle(t, "domexception_worker")
}
