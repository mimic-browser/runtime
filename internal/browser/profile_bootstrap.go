package browser

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/moreveal/mimic/internal/engine"
)

type profileBootstrapAttempt struct {
	done chan struct{}
	err  error
}

// A managed Context may start 100 navigations before the asynchronous snapshot
// builder finishes. Prepare the exact exposure graph on the first request for
// that graph; subsequent Pages wait on the same Browser-owned attempt. The
// temporary Pages use the installed baseline, but the cache key excludes
// rebinding-only profile values, so all generated identities share the result.
func (b *Browser) prepareProfileBootstrap(key [32]byte, security documentSecurity) error {
	if b.bootstrapSnapshots.hasSnapshotKey(key) {
		return nil
	}
	b.profileBootstrapMu.Lock()
	if b.profileBootstraps == nil {
		b.profileBootstraps = make(map[[32]byte]*profileBootstrapAttempt)
	}
	attempt := b.profileBootstraps[key]
	if attempt != nil {
		select {
		case <-attempt.done:
			if attempt.err != nil || !b.bootstrapSnapshots.hasSnapshotKey(key) {
				delete(b.profileBootstraps, key) // failed or evicted: prepare again
				attempt = nil
			}
		default:
		}
	}
	if attempt == nil {
		attempt = &profileBootstrapAttempt{done: make(chan struct{})}
		b.profileBootstraps[key] = attempt
		b.profileBootstrapMu.Unlock()
		ctx, cancel := context.WithTimeout(b.lifetime, 2*time.Minute)
		err := b.buildProfileBootstrap(ctx, key, security)
		cancel()
		b.profileBootstrapMu.Lock()
		attempt.err = err
		close(attempt.done)
		b.profileBootstrapMu.Unlock()
		return err
	}
	b.profileBootstrapMu.Unlock()
	select {
	case <-attempt.done:
		return attempt.err
	case <-b.lifetime.Done():
		return b.lifetime.Err()
	}
}

func (b *Browser) buildProfileBootstrap(ctx context.Context, key [32]byte, security documentSecurity) error {
	address := "http://untrusted.invalid/"
	if security.secureContext {
		address = "http://localhost/"
	}
	u, err := url.Parse(address)
	if err != nil {
		return err
	}
	c := b.NewContext()
	c.bootstrapPreparation = true
	defer c.Close()
	p, err := c.NewPage()
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.current = u
	p.documentSecurity = security
	p.Top.Realm.url = u
	p.Top.Realm.origin = originOf(address)
	p.mu.Unlock()
	_, err = p.Evaluate(ctx, "true")
	if err == nil && p.Top.Realm.bootstrapSource().key != key {
		err = fmt.Errorf("profile bootstrap preparation selected a different exposure graph")
	}
	c.ClosePage(p.ID)
	if err != nil {
		return err
	}
	if b.bootstrapSnapshots.hasSnapshotKey(key) {
		return nil
	}
	// The seed has already been captured. Admit its build directly instead of
	// constructing and bootstrapping a second throwaway Page merely to select
	// the same cache entry. The builder retains Browser lifetime ownership.
	factory, ok := b.factory.(engine.BootstrapSnapshotFactory)
	if !ok {
		return fmt.Errorf("profile bootstrap preparation requires a snapshot factory")
	}
	if _, _, err := b.bootstrapSnapshots.selectEntry(b.lifetime, factory, key); err != nil {
		return err
	}
	if err := b.bootstrapSnapshots.wait(ctx); err != nil {
		return err
	}
	if !b.bootstrapSnapshots.hasSnapshotKey(key) {
		return fmt.Errorf("profile bootstrap artifact was not admitted")
	}
	return nil
}
