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

//go:embed testdata/timer_numeric_conversion.js
var timerNumericConversionProbe string

func TestTimerNumericConversionUsesToNumber(t *testing.T) {
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
	got, err := p.Evaluate(ctx, timerNumericConversionProbe)
	if err != nil || got != "ok" {
		t.Fatalf("Window timer numeric conversion: %v %v", got, err)
	}
	workerSource := "postMessage(" + strings.TrimSuffix(strings.TrimSpace(timerNumericConversionProbe), ";") + ")"
	got, err = p.Evaluate(ctx, `new Promise((resolve, reject) => {
  const url = URL.createObjectURL(new Blob([`+strconv.Quote(workerSource)+`]));
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
		t.Fatalf("Worker timer numeric conversion: %v %v", got, err)
	}
}

// Measured with passive CDP in frozen headful Chrome 152.0.7977.82.
func TestTimerCallbackNativeEntry(t *testing.T) {
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
	for _, tc := range []struct{ name, source, want string }{
		{"window", "new Promise(resolve=>setTimeout(function timerProbe(){resolve(new Error(\"timer\").stack)},0))", "Error: timer\n    at timerProbe (<anonymous>:1:63)"},
		{"string", "new Promise(resolve=>{globalThis.finishTimer=resolve;setTimeout(\"finishTimer(new Error(\\\"timer\\\").stack)\",0)})", "Error: timer\n    at <anonymous>:1:13"},
		{"worker", "new Promise(resolve=>{const source=`setTimeout(function workerProbe(){\"use strict\";postMessage({stack:new Error(\"timer\").stack,receiver:this===self})},0);\n//# sourceURL=timer-worker.js`;const url=URL.createObjectURL(new Blob([source],{type:\"text/javascript\"}));const worker=new Worker(url);worker.onmessage=e=>{worker.terminate();URL.revokeObjectURL(url);resolve(JSON.stringify(e.data))}})", "{\"stack\":\"Error: timer\\n    at workerProbe (timer-worker.js:1:67)\",\"receiver\":true}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := p.Evaluate(context.Background(), tc.source)
			if err != nil || got != tc.want {
				t.Fatalf("callback entry: %v %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestTimerArgumentsAndSelfCancellationError(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		got, err := p.Evaluate(context.Background(), `new Promise(resolve=>{
   const payload={},original=new Error('timer-self-cancel');let seen=false;
   onerror=function(message,filename,line,column,error){onerror=null;resolve(seen && error===original);return true};
   function callback(a,b){'use strict';seen=this===window && a===payload && b===7;clearInterval(id);throw original}
   callback.apply=()=>{throw Error('author apply')};
   const id=setInterval(callback,0,payload,7);
  })`)
		if err != nil || got != true {
			t.Fatalf("timer args and cancellation: %v %v", got, err)
		}
	})
}
