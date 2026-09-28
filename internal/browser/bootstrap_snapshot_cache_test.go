//go:build (windows || linux) && amd64

package browser

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moreveal/mimic/internal/engine"
	v8engine "github.com/moreveal/mimic/internal/engine/v8"
)

type cacheTestSnapshot struct {
	size   int
	closed atomic.Int32
}

func (s *cacheTestSnapshot) SizeBytes() int { return s.size }
func (s *cacheTestSnapshot) Close() error   { s.closed.Add(1); return nil }
func (s *cacheTestSnapshot) NewRuntime() (engine.Runtime, error) {
	return nil, errors.New("test snapshot")
}

type cacheTestFactory struct {
	calls   atomic.Int32
	result  *cacheTestSnapshot
	started chan struct{}
	release chan struct{}
}

func (f *cacheTestFactory) BootstrapSnapshotsEnabled() bool { return true }
func (f *cacheTestFactory) BuildBootstrapSnapshot(ctx context.Context, _ ...string) (engine.BootstrapSnapshot, error) {
	f.calls.Add(1)
	if f.started != nil {
		close(f.started)
	}
	if f.release != nil {
		select {
		case <-f.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return f.result, nil
}

func TestBootstrapSnapshotCacheAdmissionAndClose(t *testing.T) {
	serialBrowserTest(t)
	var cache bootstrapSnapshotCache
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	artifact := &cacheTestSnapshot{size: 1024}
	factory := &cacheTestFactory{result: artifact}
	key := [32]byte{1}
	got, entry, err := cache.selectEntry(ctx, factory, key)
	if got != nil || entry == nil || err != nil {
		t.Fatalf("first use: %v %v %v", got, entry, err)
	}
	cache.captured(entry, []string{"seed"}, nil)
	if factory.calls.Load() != 0 {
		t.Fatal("single realm pays snapshot build")
	}
	cache.selectEntry(ctx, factory, key)
	if err := cache.wait(ctx); err != nil {
		t.Fatal(err)
	}
	got, entry, err = cache.selectEntry(ctx, factory, key)
	if got != artifact || entry != nil || err != nil || factory.calls.Load() != 1 {
		t.Fatal("snapshot not reused")
	}
	if err := cache.close(); err != nil {
		t.Fatal(err)
	}
	if err := cache.close(); err != nil {
		t.Fatal(err)
	}
	if artifact.closed.Load() != 1 {
		t.Fatal("artifact not released exactly once")
	}
}

func TestBootstrapSnapshotCacheCancellationJoinsBuilder(t *testing.T) {
	serialBrowserTest(t)
	var cache bootstrapSnapshotCache
	ctx, cancel := context.WithCancel(context.Background())
	factory := &cacheTestFactory{started: make(chan struct{}), release: make(chan struct{})}
	_, entry, _ := cache.selectEntry(ctx, factory, [32]byte{1})
	cache.captured(entry, []string{"seed"}, nil)
	cache.selectEntry(ctx, factory, [32]byte{1})
	<-factory.started
	cancel()
	closed := make(chan error, 1)
	go func() { closed <- cache.close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("snapshot builder outlived cache Close")
	}
	if cache.entries != nil {
		t.Fatal("closed cache retains seeds")
	}
}

func TestBootstrapSnapshotCacheBoundsRetainedSeedsAndArtifacts(t *testing.T) {
	serialBrowserTest(t)
	var cache bootstrapSnapshotCache
	factory := &cacheTestFactory{}
	first := &cacheTestSnapshot{size: 20 << 20}
	old := &bootstrapSnapshotEntry{key: [32]byte{1}, snapshot: first, touched: 1}
	cache.entries = map[[32]byte]*bootstrapSnapshotEntry{old.key: old}
	_, entry, _ := cache.selectEntry(context.Background(), factory, [32]byte{2})
	cache.captured(entry, []string{strings.Repeat("x", 13<<20)}, nil)
	if first.closed.Load() != 1 || len(cache.entries) != 1 {
		t.Fatal("total cache budget did not evict older artifact")
	}
	_, oversize, _ := cache.selectEntry(context.Background(), factory, [32]byte{3})
	cache.captured(oversize, []string{strings.Repeat("x", bootstrapSnapshotBytes+1)}, nil)
	if oversize.err == nil || cache.entries[entry.key] != entry {
		t.Fatal("oversized seed evicted valid cache content")
	}
	cache.close()
}

func TestBootstrapSnapshotHostProxyKeepsPrivateIntrinsics(t *testing.T) {
	serialBrowserTest(t)
	factory := v8engine.Factory{}
	plain := factory.New()
	defer plain.Close()
	host := func(runtime engine.Runtime) map[string]any {
		return map[string]any{"probe": runtime.Function(func(engine.Value, []engine.Value) (engine.Value, error) { return runtime.Value(17), nil })}
	}
	if err := plain.Set("__mimic", host(plain)); err != nil {
		t.Fatal(err)
	}
	finish, err := plain.Eval(context.Background(), bootstrapCaptureSource, "capture")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = plain.Eval(context.Background(), `{const host=__mimic;globalThis.probe=()=>host.probe()}`, "bind"); err != nil {
		t.Fatal(err)
	}
	capture, err := plain.Call(context.Background(), finish, nil)
	if err != nil {
		t.Fatal(err)
	}
	check := func(runtime engine.Runtime) {
		t.Helper()
		v, e := runtime.Eval(context.Background(), `Reflect.get=()=>{throw Error('poisoned get')};probe()`, "poison")
		if e != nil || numberValue(v.Export()) != 17 {
			t.Fatalf("private host intrinsic: %v %v", v, e)
		}
	}
	check(plain)
	seed := bootstrapSeedSources(`globalThis.__mimicRestoreBootstrap=host=>{globalThis.probe=()=>host.probe()}`, capture.String())
	snapshot, err := factory.BuildBootstrapSnapshot(context.Background(), seed...)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	restored, err := snapshot.NewRuntime()
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if err = restored.Set("__mimic", host(restored)); err != nil {
		t.Fatal(err)
	}
	if _, err = restored.Call(context.Background(), restored.Get("__mimicRestoreBootstrap"), nil, restored.Get("__mimic")); err != nil {
		t.Fatal(err)
	}
	check(restored)
}

type failedBindingSnapshot struct{ closed bool }

func (s *failedBindingSnapshot) SizeBytes() int { return 1 }
func (s *failedBindingSnapshot) Close() error   { s.closed = true; return nil }
func (s *failedBindingSnapshot) NewRuntime() (engine.Runtime, error) {
	runtime := v8engine.Factory{}.New()
	_, err := runtime.Eval(context.Background(), `globalThis.__mimicRestoreBootstrap=host=>{host.installFrameReferenceBridge(x=>x,x=>null,x=>0);throw Error('bad snapshot binding')}`, "broken snapshot")
	return runtime, err
}

func TestBootstrapSnapshotBindingFailureFallsBackBeforeScripts(t *testing.T) {
	serialBrowserTest(t)
	if os.Getenv("MIMIC_DISABLE_BOOTSTRAP_SNAPSHOT") == "1" {
		t.Skip("snapshots disabled")
	}
	p := bootstrapSnapshotPage(t)
	bootstrapSnapshotWarm(t, p)
	if !(v8engine.Factory{}).BootstrapSnapshotsEnabled() {
		t.Skip("source diagnostics bypass snapshots")
	}
	cache := &p.ctx.browser.bootstrapSnapshots
	cache.mu.Lock()
	var old engine.BootstrapSnapshot
	broken := &failedBindingSnapshot{}
	for _, entry := range cache.entries {
		if entry.snapshot != nil {
			old = entry.snapshot
			entry.snapshot = broken
			break
		}
	}
	cache.mu.Unlock()
	if old == nil {
		t.Fatal("no snapshot to corrupt")
	}
	old.Close()
	// Exercise deferred main-frame initialization too: it must return the retry
	// runtime, not the already disposed initial snapshot runtime.
	sibling, err := p.ctx.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	sibling.Trace().Start()
	value := bootstrapSnapshotEvaluate(t, sibling, `(()=>{const e=document.createElement('div');e.id='fallback';document.body.appendChild(e);return document.querySelector('#fallback')===e&&typeof __mimicRestoreBootstrap==='undefined'})()`)
	if value != true || sibling.Top.Realm.bootstrapRestored {
		t.Fatal("failed binding did not retry ordinary initialization")
	}
	if !broken.closed {
		t.Fatal("failed snapshot remained reusable")
	}
	found := false
	for _, event := range sibling.trace.Events() {
		if event.Name == "bootstrapSnapshotBindingFailed" {
			found = true
		}
	}
	if !found {
		t.Fatal("binding failure diagnostic was lost")
	}
}

// Capture failure must invalidate the optional seed, never the live host call.
func TestBootstrapCaptureSerializationFailurePreservesHostResult(t *testing.T) {
	serialBrowserTest(t)
	runtime := (v8engine.Factory{}).New()
	defer runtime.Close()
	ctx := context.Background()
	_, err := runtime.Eval(ctx, `globalThis.value=new Proxy({}, {get(){throw Error('serialization denied')}});globalThis.__mimic={probe(){return value}}`, "setup")
	if err != nil {
		t.Fatal(err)
	}
	finish, err := runtime.Eval(ctx, bootstrapCaptureSource, "capture")
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Eval(ctx, `globalThis.saved=__mimic.probe; saved()===value`, "host-call")
	if err != nil || result.Export() != true {
		t.Fatalf("host call changed: %v %v", result, err)
	}
	if _, err = runtime.Call(ctx, finish, nil); err == nil {
		t.Fatal("invalid capture accepted")
	}
	result, err = runtime.Eval(ctx, `saved()===value && __mimic.probe()===value`, "after-capture")
	if err != nil || result.Export() != true {
		t.Fatalf("capture remained active: %v %v", result, err)
	}
}
