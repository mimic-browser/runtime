//go:build (windows || linux) && amd64

package cdp

import (
	"context"
	"net"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	chrome152 "github.com/moreveal/mimic/chrome/152"
	"github.com/moreveal/mimic/internal/browser"
	"github.com/moreveal/mimic/internal/engine"
	v8engine "github.com/moreveal/mimic/internal/engine/v8"
)

// Embedding preserves the real snapshot/connected-realm factory capabilities.
// The first lazy runtime creation pauses after preparation has created its Page.
type blockedBootstrapCDPFactory struct {
	v8engine.Factory
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (f *blockedBootstrapCDPFactory) New() engine.Runtime {
	f.once.Do(func() {
		close(f.entered)
		<-f.release
	})
	return f.Factory.New()
}

func TestBootstrapPreparationNeverExposesCDPTargets(t *testing.T) {
	factory := &blockedBootstrapCDPFactory{entered: make(chan struct{}), release: make(chan struct{})}
	b, err := browser.New(factory, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(b)
	if err != nil {
		_ = b.Close()
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = b.Close()
		t.Fatal(err)
	}
	go func() { _ = s.Serve(listener) }()
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	connection := browserConnection(t, listener.Addr().String())
	retained := wireCall(t, connection, 1, "Target.createBrowserContext", map[string]any{})["browserContextId"].(string)
	before := wireCall(t, connection, 2, "Target.getBrowserContexts", nil)
	if !reflect.DeepEqual(before["browserContextIds"], []any{retained}) {
		t.Fatalf("unexpected initial contexts: %v", before)
	}
	targetIDs := func(result map[string]any) []string {
		var ids []string
		for _, value := range result["targetInfos"].([]any) {
			ids = append(ids, value.(map[string]any)["targetId"].(string))
		}
		sort.Strings(ids)
		return ids
	}
	beforeTargets := targetIDs(wireCall(t, connection, 3, "Target.getTargets", nil))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	done := make(chan error, 1)
	dir := t.TempDir()
	go func() { done <- b.PrepareBootstrap(ctx, dir) }()
	var release sync.Once
	defer func() {
		release.Do(func() { close(factory.release) })
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("prepare bootstrap: %v", err)
			}
		case <-ctx.Done():
			t.Error("bootstrap preparation did not finish")
		}
	}()
	select {
	case <-factory.entered:
	case <-ctx.Done():
		t.Fatal("bootstrap preparation did not enter the controlled runtime boundary")
	}
	if during := wireCall(t, connection, 4, "Target.getBrowserContexts", nil); !reflect.DeepEqual(during, before) {
		t.Errorf("internal bootstrap Context became externally visible: before=%v during=%v", before, during)
	}
	if during := targetIDs(wireCall(t, connection, 5, "Target.getTargets", nil)); !reflect.DeepEqual(during, beforeTargets) {
		t.Errorf("internal bootstrap Page became externally visible: before=%v during=%v", beforeTargets, during)
	}
}
