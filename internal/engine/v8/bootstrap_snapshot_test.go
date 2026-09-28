//go:build (windows || linux) && amd64

package v8

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/moreveal/mimic/internal/engine"
)

func TestBootstrapSnapshotClosuresAndLifetime(t *testing.T) {
	snapshot, err := (Factory{}).BuildBootstrapSnapshot(context.Background(), `(()=>{let host; class Item{}; globalThis.seed={Item, array:[], bind(value){host=value}, read(){return host()}, make(){return new Item}}})()`)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	if snapshot.SizeBytes() == 0 {
		t.Fatal("empty snapshot")
	}
	for i := 0; i < 3; i++ {
		r, err := snapshot.NewRuntime()
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		if err = r.Set("nativeRead", r.Function(func(engine.Value, []engine.Value) (engine.Value, error) { return r.Value(i + 40), nil })); err != nil {
			t.Fatal(err)
		}
		got, err := r.Eval(context.Background(), fmt.Sprintf(`seed.bind(nativeRead); seed.read()===%d && seed.make() instanceof seed.Item && seed.Item.prototype.constructor===seed.Item && seed.array.length===0 && Array.prototype.mark===undefined`, i+40), "verify")
		if err != nil || got.Export() != true {
			t.Fatalf("restore %d: %v %v", i, got, err)
		}
		if _, err = r.Eval(context.Background(), `seed.array.push(1); Array.prototype.mark=true`, "mutate"); err != nil {
			t.Fatal(err)
		}
		if i == 2 {
			if err = snapshot.Close(); err != nil {
				t.Fatal(err)
			}
			got, err = r.Eval(context.Background(), `seed.read()===42`, "after-artifact-close")
			if err != nil || got.Export() != true {
				t.Fatalf("live runtime after close: %v %v", got, err)
			}
		}
	}
	if _, err = snapshot.NewRuntime(); err == nil {
		t.Fatal("closed artifact accepted runtime")
	}
}

func TestBootstrapSnapshotErrorsAndCancellation(t *testing.T) {
	for _, source := range []string{`function {`, `throw new Error("seed failure")`} {
		if snapshot, err := (Factory{}).BuildBootstrapSnapshot(context.Background(), source); err == nil {
			snapshot.Close()
			t.Fatal("bad seed accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (Factory{}).BuildBootstrapSnapshot(ctx, ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-cancel: %v", err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := (Factory{}).BuildBootstrapSnapshot(ctx, `while(true){}`); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("running cancel: %v", err)
	}
	snapshot, err := (Factory{}).BuildBootstrapSnapshot(context.Background(), `globalThis.answer=42`)
	if err != nil {
		t.Fatalf("creator recovered after errors: %v", err)
	}
	snapshot.Close()
}

func TestBootstrapSnapshotConcurrentCloseAndRestore(t *testing.T) {
	snapshot, err := (Factory{}).BuildBootstrapSnapshot(context.Background(), `globalThis.answer=42`)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	created := make(chan struct{}, 8)
	release := make(chan struct{})
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			r, err := snapshot.NewRuntime()
			if err != nil {
				created <- struct{}{}
				return
			}
			defer r.Close()
			created <- struct{}{}
			<-release
			got, err := r.Eval(context.Background(), `answer`, "concurrent")
			if err != nil || got.Export() != int64(42) && got.String() != "42" {
				t.Errorf("restore: %v %v", got, err)
			}
		}()
	}
	close(start)
	<-created // At least one consumer exists before racing Close with the rest.
	if err = snapshot.Close(); err != nil {
		t.Fatal(err)
	}
	close(release)
	wg.Wait()
	if err = snapshot.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestBootstrapSnapshotSeparateStages(t *testing.T) {
	snapshot, err := (Factory{}).BuildBootstrapSnapshot(context.Background(),
		`globalThis.temporarySeedData=JSON.parse('{"answer":42}')`,
		`let seedAnswer; globalThis.readSeedAnswer=()=>seedAnswer`,
		`seedAnswer=temporarySeedData.answer`,
		`delete globalThis.temporarySeedData`)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	r, err := snapshot.NewRuntime()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	got, err := r.Eval(context.Background(), `readSeedAnswer()===42 && !Object.hasOwn(globalThis,"temporarySeedData")`, "stages")
	if err != nil || got.Export() != true {
		t.Fatalf("staged restore: %v %v", got, err)
	}
	if bad, err := (Factory{}).BuildBootstrapSnapshot(context.Background(), `globalThis.answer=1`, `throw new Error("stage 2")`, `globalThis.answer=42`); err == nil {
		bad.Close()
		t.Fatal("stage failure ignored")
	}
}

func TestBootstrapSnapshotBareRuntime(t *testing.T) {
	snapshot, err := (Factory{}).BuildBootstrapSnapshot(context.Background(), `globalThis.seed={value:42}`)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	pool := snapshot.(*bootstrapSnapshot).NewRuntimePool(1).(*bootstrapRuntimePool)
	defer pool.Close()

	bare, err := pool.NewBareRuntime()
	if err != nil {
		t.Fatal(err)
	}
	defer bare.Close()
	value, err := bare.Eval(context.Background(), `typeof seed === 'undefined' && Array.isArray([])`, "bare")
	if err != nil || value.Export() != true {
		t.Fatalf("bare runtime restored the platform context: %v %v", value, err)
	}

	restored, err := pool.NewRuntime()
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	value, err = restored.Eval(context.Background(), `seed.value === 42`, "restored")
	if err != nil || value.Export() != true {
		t.Fatalf("restored runtime lost the platform context: %v %v", value, err)
	}
}

func TestBootstrapRuntimePoolSharesIsolateAndIsolatesRealms(t *testing.T) {
	snapshot, err := (Factory{}).BuildBootstrapSnapshot(context.Background(), `globalThis.seed={value:1}`)
	if err != nil {
		t.Fatal(err)
	}
	pool := snapshot.(*bootstrapSnapshot).NewRuntimePool(2)
	first, err := pool.NewRuntime()
	if err != nil {
		t.Fatal(err)
	}
	second, err := pool.NewRuntime()
	if err != nil {
		t.Fatal(err)
	}
	third, err := pool.NewRuntime()
	if err != nil {
		t.Fatal(err)
	}
	if first.(*adapter).owner != second.(*adapter).owner {
		t.Fatal("pool did not place realms in the same isolate")
	}
	if first.(*adapter).owner == third.(*adapter).owner {
		t.Fatal("pool exceeded the per-isolate realm limit")
	}
	if _, err = first.Eval(context.Background(), `seed.value=9;globalThis.localOnly=true`, "mutate"); err != nil {
		t.Fatal(err)
	}
	value, err := second.Eval(context.Background(), `seed.value===1&&typeof localOnly==='undefined'`, "isolated")
	if err != nil || value.Export() != true {
		t.Fatalf("realm state crossed Page boundary: %v %v", value, err)
	}
	if err = first.Close(); err != nil {
		t.Fatal(err)
	}
	value, err = second.Eval(context.Background(), `seed.value`, "sibling-after-close")
	if err != nil || value.String() != "1" {
		t.Fatalf("closing one realm invalidated sibling: %v %v", value, err)
	}
	if err = pool.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.NewRuntime(); err == nil {
		t.Fatal("closed pool accepted runtime")
	}
	if _, err = second.Eval(context.Background(), `seed.value`, "live-after-pool-close"); err != nil {
		t.Fatalf("pool close invalidated live realm: %v", err)
	}
	if err = second.Close(); err != nil {
		t.Fatal(err)
	}
	if err = third.Close(); err != nil {
		t.Fatal(err)
	}
	if err = snapshot.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestBootstrapRuntimePoolDisposesIdleIsolate(t *testing.T) {
	snapshot, err := (Factory{}).BuildBootstrapSnapshot(context.Background(), `globalThis.seed=42`)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	pool := snapshot.(*bootstrapSnapshot).NewRuntimePool(1).(*bootstrapRuntimePool)
	defer pool.Close()

	first, err := pool.NewRuntime()
	if err != nil {
		t.Fatal(err)
	}
	owner := first.(*adapter).owner
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-owner.done:
	default:
		t.Fatal("last realm closed but its isolate is still alive")
	}
	pool.mu.Lock()
	lanes := len(pool.lanes)
	pool.mu.Unlock()
	if lanes != 0 {
		t.Fatalf("pool retained %d idle isolates", lanes)
	}

	second, err := pool.NewRuntime()
	if err != nil {
		t.Fatal(err)
	}
	if second.(*adapter).owner == owner {
		t.Fatal("reused disposed isolate")
	}
	value, err := second.Eval(context.Background(), `seed`, "new-page-after-close")
	if err != nil || value.String() != "42" {
		t.Fatalf("snapshot restore after isolate disposal: %v %v", value, err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestBootstrapSnapshotWasmIntrinsics(t *testing.T) {
	s, err := (Factory{}).BuildBootstrapSnapshot(context.Background(), `globalThis.seedWasm=typeof WebAssembly; globalThis.WebAssembly={}`, `delete globalThis.WebAssembly`)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r, err := s.NewRuntime()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	v, err := r.Eval(context.Background(), `seedWasm==='undefined' && typeof WebAssembly.instantiate==='function' && WebAssembly.validate(new Uint8Array([0,97,115,109,1,0,0,0])) && Object.getPrototypeOf(new WebAssembly.Instance(new WebAssembly.Module(new Uint8Array([0,97,115,109,1,0,0,0]))).exports)===null`, "wasm")
	if err != nil || v.Export() != true {
		t.Fatalf("native WebAssembly restoration: %v %v", v, err)
	}
}

// V8 deliberately installs experimental globals and Wasm only on restore.
// An embedder seed must leave those names absent until V8 publishes them.
func TestBootstrapSnapshotBuiltinInventory(t *testing.T) {
	const inventory = `JSON.stringify((()=>{const seen=new Map();const visit=(o,depth)=>{if(o===null||typeof o!=='object'&&typeof o!=='function')return [typeof o,String(o)];if(seen.has(o))return ['ref',seen.get(o)];seen.set(o,seen.size);const info=[typeof o,typeof o==='function'?Function.prototype.toString.call(o):null];if(depth===0)return info;info.push(Reflect.ownKeys(o).map(k=>{const d=Object.getOwnPropertyDescriptor(o,k);return [String(k),d.writable,d.enumerable,d.configurable,'value'in d?visit(d.value,depth-1):null,d.get?visit(d.get,0):null,d.set?visit(d.set,0):null]}));return info};return visit(globalThis,3)})())`
	normal := (Factory{}).New()
	defer normal.Close()
	nv, err := normal.Eval(context.Background(), inventory, "inventory")
	if err != nil {
		t.Fatal(err)
	}
	s, err := (Factory{}).BuildBootstrapSnapshot(context.Background(), ``)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r, err := s.NewRuntime()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	restored, err := r.Eval(context.Background(), inventory, "inventory")
	if err != nil {
		t.Fatal(err)
	}
	if restored.String() != nv.String() {
		t.Fatal("restored intrinsic graph/order/descriptors differ from ordinary V8 context")
	}
}
