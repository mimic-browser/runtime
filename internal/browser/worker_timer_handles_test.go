package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/moreveal/mimic/internal/engine"
	"github.com/moreveal/mimic/internal/scheduler"
)

type workerTimerSample struct {
	Roots    int
	External uint64
	Timers   int
	Err      error
}

func sampleWorkerTimerState(t *testing.T, worker *DedicatedWorker) workerTimerSample {
	t.Helper()
	worker.mu.Lock()
	runtime, queue := worker.runtime, worker.scheduler
	worker.mu.Unlock()
	if runtime == nil || queue == nil {
		t.Fatal("worker is not running")
	}
	result := make(chan workerTimerSample, 1)
	queue.Post(scheduler.Control, 0, func(context.Context) error {
		var sample workerTimerSample
		defer func() { result <- sample }()
		for iteration := 0; iteration < 2; iteration++ {
			if sample.Err = runtime.(interface{ ProfileCollect() error }).ProfileCollect(); sample.Err != nil {
				return nil
			}
		}
		var value any
		value, sample.Err = runtime.(interface{ Diagnostics() (any, error) }).Diagnostics()
		if sample.Err != nil {
			return nil
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			sample.Err = err
			return nil
		}
		var native struct {
			Roots int                             `json:"persistent_handles"`
			Heap  struct{ ExternalMemory uint64 } `json:"heap"`
		}
		if sample.Err = json.Unmarshal(encoded, &native); sample.Err != nil {
			return nil
		}
		sample.Roots, sample.External, sample.Timers = native.Roots, native.Heap.ExternalMemory, len(worker.timers)
		return nil
	})
	worker.signal()
	select {
	case sample := <-result:
		if sample.Err != nil {
			t.Fatal(sample.Err)
		}
		return sample
	case <-time.After(10 * time.Second):
		t.Fatal("worker did not reach the diagnostic task")
		return workerTimerSample{}
	}
}

func TestWorkerTimersReleaseRootsAndCapturedPayloads(t *testing.T) {
	serialBrowserTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/worker.js" {
			w.Header().Set("Content-Type", "text/javascript")
			fmt.Fprint(w, `onmessage=async event=>{
 if(event.data==='ping'){postMessage(256);return}
 if(event.data==='fire')await new Promise(resolve=>{let count=0;for(let i=0;i<256;i++){const payload=new Uint8Array(4096);payload[0]=1;setTimeout(()=>{count+=payload[0];if(count===256)resolve()},0)}});
 if(event.data==='cancel')for(let i=0;i<256;i++){const payload=new Uint8Array(4096);payload[0]=1;clearTimeout(setTimeout(()=>payload[0],60000))}
 postMessage(256);
};`)
			return
		}
		fmt.Fprint(w, "<!doctype html><body>worker ownership fixture</body>")
	}))
	defer server.Close()
	p := newAsyncModulePage(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := p.Navigate(ctx, server.URL); err != nil {
		t.Fatal(err)
	}
	call := func(operation string) {
		t.Helper()
		source := fmt.Sprintf(`new Promise((resolve,reject)=>{globalThis.timerWorker ||= new Worker('/worker.js');timerWorker.onerror=event=>reject(new Error(event.message));timerWorker.onmessage=event=>resolve(event.data);timerWorker.postMessage(%q)})`, operation)
		value, err := p.Evaluate(ctx, source)
		if err != nil || value != float64(256) {
			t.Fatalf("worker %s: %v %v", operation, value, err)
		}
	}
	call("ping")
	var worker *DedicatedWorker
	for _, candidate := range p.Top.Realm.workers {
		worker = candidate
	}
	if worker == nil {
		t.Fatal("worker owner missing")
	}
	baseline := sampleWorkerTimerState(t, worker)
	for iteration := 0; iteration < 3; iteration++ {
		for _, operation := range []string{"fire", "cancel"} {
			call(operation)
			current := sampleWorkerTimerState(t, worker)
			if current.Roots != baseline.Roots || current.Timers != 0 {
				t.Fatalf("%s iteration %d: roots %d -> %d, registrations %d", operation, iteration, baseline.Roots, current.Roots, current.Timers)
			}
			if current.External > baseline.External+(64<<10) {
				t.Fatalf("%s iteration %d retained payload bytes: external %d -> %d", operation, iteration, baseline.External, current.External)
			}
			t.Logf("%s iteration %d roots=%d external=%d registrations=%d", operation, iteration, current.Roots, current.External, current.Timers)
		}
	}
	if err := worker.Close(); err != nil {
		t.Fatal(err)
	}
	worker.mu.Lock()
	defer worker.mu.Unlock()
	if worker.runtime != nil || worker.scheduler != nil || len(worker.timers) != 0 {
		t.Fatal("closed worker retained its runtime or timer queue")
	}
}

// Frozen Chrome 152.0.7977.82: a timer exception reaches Worker.onerror with
// the original object, and the next interval tick still uses a cancelable ID.
func TestWorkerIntervalErrorAndSelfCancelChrome152(t *testing.T) {
	serialBrowserTest(t)
	p := newAsyncModulePage(t)
	navigateCapabilityFixture(t, p)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	value, err := p.Evaluate(ctx, `new Promise((resolve,reject)=>{
 const source="const original=new TypeError('worker-timer-probe'),errors=[];let ticks=0;globalThis.savedCallback=()=>42;onerror=function(message,filename,line,column,error){errors.push([message,arguments.length,error===original,error instanceof TypeError]);return true};const id=setInterval(()=>{ticks++;if(ticks===1)throw original;if(ticks===2){clearInterval(id);setTimeout(()=>postMessage({ticks,errors,saved:savedCallback()}),20)}},5);";
 const url=URL.createObjectURL(new Blob([source],{type:'text/javascript'})),worker=new Worker(url);
 worker.onerror=event=>{worker.terminate();URL.revokeObjectURL(url);reject(new Error(event.message))};
 worker.onmessage=event=>{worker.terminate();URL.revokeObjectURL(url);resolve(JSON.stringify(event.data))};
})`)
	const expected = `{"ticks":2,"errors":[["Uncaught TypeError: worker-timer-probe",5,true,true]],"saved":42}`
	if err != nil || value != expected {
		t.Fatalf("Chrome 152 Worker timer oracle: %v %v", value, err)
	}
}

func TestWindowTimerTerminalFailureRemovesRegistration(t *testing.T) {
	serialBrowserTest(t)
	p := newAsyncModulePage(t)
	navigateCapabilityFixture(t, p)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r := p.Top.Realm
	if err := r.runtime.Set("privateTimer", r.transientFn(r.hostTimer)); err != nil {
		t.Fatal(err)
	}
	baseline := persistentHandleCount(t, r.runtime)
	for iteration := 0; iteration < 3; iteration++ {
		// Public timers report author errors and keep intervals alive. This
		// private callback exercises a terminal native error without a reporter.
		value, err := r.runtime.Eval(ctx, `privateTimer(()=>{throw new Error('terminal timer')},0,true)`, "terminal-timer.js")
		if err != nil {
			t.Fatal(err)
		}
		releaseRuntimeValues(r.runtime, value)
		err = r.RunReady(ctx)
		var thrown engine.ThrownValue
		if err == nil || !errors.As(err, &thrown) {
			t.Fatalf("terminal timer did not report original error: %v", err)
		}
		releaseRuntimeValues(r.runtime, thrown.ThrownValue())
		if len(r.timers) != 0 || persistentHandleCount(t, r.runtime) != baseline {
			t.Fatalf("terminal timer kept registration/root: registrations=%d", len(r.timers))
		}
	}
}

func TestWorkerErrorDeliveryReleasesConsumedValues(t *testing.T) {
	serialBrowserTest(t)
	p := newAsyncModulePage(t)
	navigateCapabilityFixture(t, p)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r := p.Top.Realm
	if _, err := p.Evaluate(ctx, `globalThis.workerReports=[];globalThis.deliveryError=new Error('delivery failure');undefined`); err != nil {
		t.Fatal(err)
	}
	callback, err := r.runtime.Eval(ctx, `(message)=>{workerReports.push(message);throw deliveryError}`, "worker-error-delivery.js")
	if err != nil {
		t.Fatal(err)
	}
	defer releaseRuntimeValues(r.runtime, callback)
	runtime := p.ctx.browser.factory.New()
	defer runtime.Close()
	value, err := runtime.Eval(ctx, `globalThis.original=new TypeError('worker failure');undefined`, "worker-error-source.js")
	if err != nil {
		t.Fatal(err)
	}
	releaseRuntimeValues(runtime, value)
	workerURL, _ := url.Parse("https://worker.example/worker.js")
	worker := &DedicatedWorker{parent: r, runtime: runtime, errorCallback: callback, url: workerURL}
	parentRoots, workerRoots := persistentHandleCount(t, r.runtime), persistentHandleCount(t, runtime)
	for iteration := 0; iteration < 32; iteration++ {
		_, err = runtime.Eval(ctx, `throw original`, "worker-error-source.js")
		if err == nil {
			t.Fatal("worker script did not throw")
		}
		if err := worker.reportError(err); err != nil {
			t.Fatal(err)
		}
		// Parent delivery keeps its existing policy of consuming callback errors.
		if err := r.RunReady(ctx); err != nil {
			t.Fatalf("delivery exposed a consumed exception: %v", err)
		}
		if persistentHandleCount(t, r.runtime) != parentRoots || persistentHandleCount(t, runtime) != workerRoots {
			t.Fatalf("iteration %d retained a handled error or delivery temporary", iteration)
		}
	}
	value, err = runtime.Eval(ctx, `original instanceof TypeError`, "worker-error-reference.js")
	if err != nil || value.Export() != true {
		t.Fatalf("handling worker errors lost a JS-owned exception: %v %v", value, err)
	}
	releaseRuntimeValues(runtime, value)
	result, err := p.Evaluate(ctx, `workerReports.length===32&&workerReports.every(message=>message.includes('worker failure'))&&deliveryError.message==='delivery failure'`)
	if err != nil || result != true {
		t.Fatalf("error delivery changed observable values: %v %v", result, err)
	}
}
