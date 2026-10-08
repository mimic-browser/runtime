//go:build (windows || linux) && amd64

package browser

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	chrome152 "github.com/moreveal/mimic/chrome/152"
	"github.com/moreveal/mimic/internal/engine"
	v8engine "github.com/moreveal/mimic/internal/engine/v8"
)

// Keep the native factory's optional interfaces while stopping the first
// capture after its Page has been admitted, before it has a runtime.
type blockedBootstrapFactory struct {
	v8engine.Factory
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

func (f *blockedBootstrapFactory) New() engine.Runtime {
	f.once.Do(func() {
		close(f.entered)
		<-f.release
	})
	return f.Factory.New()
}

func TestBootstrapContextsRemainPrivateUntilTeardown(t *testing.T) {
	serialBrowserTest(t)
	t.Setenv("MIMIC_DISABLE_BOOTSTRAP_SNAPSHOT", "0")
	for _, persistent := range []bool{true, false} {
		name := "profile"
		if persistent {
			name = "persistent"
		}
		t.Run(name, func(t *testing.T) {
			factory := &blockedBootstrapFactory{entered: make(chan struct{}), release: make(chan struct{})}
			b, err := New(factory, chrome152.New())
			if err != nil {
				t.Fatal(err)
			}
			var release sync.Once
			t.Cleanup(func() { release.Do(func() { close(factory.release) }); _ = b.Close() })
			public := b.NewContext()
			probe, err := public.NewPage()
			if err != nil {
				t.Fatal(err)
			}
			key, security := probe.Top.Realm.bootstrapSource().key, probe.Top.Realm.securityState()
			public.ClosePage(probe.ID)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			prepared := make(chan error, 1)
			dir := t.TempDir()
			go func() {
				if persistent {
					prepared <- b.PrepareBootstrap(ctx, dir)
				} else {
					prepared <- b.prepareProfileBootstrap(key, security)
				}
			}()
			select {
			case <-factory.entered:
			case err := <-prepared:
				t.Fatalf("preparation did not reach the capture factory: %v", err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}

			if contexts := b.Contexts(); len(contexts) != 1 || contexts[0] != public {
				t.Errorf("bootstrap Contexts were publicly visible: %d Contexts", len(contexts))
			}
			if found, ok := b.Context(public.ID); !ok || found != public {
				t.Error("public Context lookup changed")
			}
			b.mu.RLock()
			var private []*Context
			for _, c := range b.contexts {
				if c != public {
					private = append(private, c)
				}
			}
			b.mu.RUnlock()
			if len(private) == 0 {
				t.Fatal("Browser does not own the active bootstrap Context")
			}
			var pages []*Page
			for _, c := range private {
				if _, ok := b.Context(c.ID); ok {
					t.Error("bootstrap Context is addressable through public lookup")
				}
				if err := b.CloseContext(c.ID); err != nil || c.lifetime.Err() != nil {
					t.Error("public CloseContext reached private preparation")
				}
				pages = append(pages, c.Pages()...)
			}
			if len(pages) == 0 {
				t.Fatal("capture factory was not reached from an owned Page")
			}

			// A concurrent close may already be joining the private operation.
			// Browser.Close must join it too, without disposing an active realm or
			// letting the operation's own deferred Close wait on its own barrier.
			var contextClosers sync.WaitGroup
			for _, c := range private {
				contextClosers.Add(1)
				go func(c *Context) { defer contextClosers.Done(); _ = c.Close() }(c)
			}
			for _, c := range private {
				select {
				case <-c.lifetime.Done():
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			closed := make(chan error, 1)
			go func() { closed <- b.Close() }()
			select {
			case <-b.lifetime.Done():
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			select {
			case err := <-closed:
				t.Errorf("Browser.Close returned before private Page teardown: %v", err)
				closed <- err
			case <-time.After(25 * time.Millisecond):
			}
			release.Do(func() { close(factory.release) })
			select {
			case err := <-closed:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			contextClosers.Wait()
			select {
			case <-prepared: // Cancellation during shutdown is expected.
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			for _, p := range pages {
				if !p.closed.Load() || p.Top.Realm != nil || len(p.realmOwners) != 0 {
					t.Error("bootstrap Page retained realm state after Browser.Close")
				}
			}
			if c, err := b.newPrivateContext(); c != nil || !errors.Is(err, context.Canceled) {
				t.Fatalf("closed Browser admitted private work: Context=%v error=%v", c, err)
			}
			if err := b.PrepareBootstrap(ctx, ""); !errors.Is(err, context.Canceled) {
				t.Fatalf("persistent preparation after Browser.Close: %v", err)
			}
			if err := b.prepareProfileBootstrap(key, security); !errors.Is(err, context.Canceled) {
				t.Fatalf("profile preparation after Browser.Close: %v", err)
			}
			b.mu.RLock()
			remaining := len(b.contexts)
			b.mu.RUnlock()
			if remaining != 0 || len(b.Contexts()) != 0 {
				t.Error("Browser retained Contexts after shutdown")
			}
		})
	}
}
