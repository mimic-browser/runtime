//go:build (windows || linux) && amd64

package browser

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	chrome152 "github.com/moreveal/mimic/chrome/152"
	v8engine "github.com/moreveal/mimic/internal/engine/v8"
)

func TestBrowserBootstrapSnapshotKeySharesHostStateProfiles(t *testing.T) {
	serialBrowserTest(t)
	b, err := New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	first := b.NewContext()
	defer first.Close()
	p, err := first.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	firstTimeZone, err := p.Evaluate(context.Background(), "Intl.DateTimeFormat().resolvedOptions().timeZone")
	if err != nil {
		t.Fatal(err)
	}
	firstKey := p.Top.Realm.bootstrapSource().key

	profile := fmt.Sprintf(`{"schemaVersion":1,"baseProfile":%q,"locale":{"timezone":"America/New_York","intlLocale":"en-US"}}`, b.env.ProfileID)
	second, err := b.NewContextWithProfile([]byte(profile))
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	q, err := second.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	secondTimeZone, err := q.Evaluate(context.Background(), "Intl.DateTimeFormat().resolvedOptions().timeZone")
	if err != nil {
		t.Fatal(err)
	}
	if q.Top.Realm.bootstrapSource().key != firstKey {
		t.Fatal("host-only profile values selected a separate bootstrap graph")
	}
	if firstTimeZone == secondTimeZone || secondTimeZone != "America/New_York" {
		t.Fatalf("host state leaked across contexts: first=%v second=%v", firstTimeZone, secondTimeZone)
	}
}

func TestBootstrapDiskSnapshotRestoresAcrossBrowserWallOrigins(t *testing.T) {
	serialBrowserTest(t)
	dir := t.TempDir()
	first, err := New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := first.PrepareBootstrap(ctx, dir); err != nil {
		first.Close()
		t.Fatal(err)
	}
	firstOrigin := first.env.Time.WallOrigin
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	second.env.Time.WallOrigin = firstOrigin.Add(2 * time.Hour)
	if err := second.PrepareBootstrap(ctx, dir); err != nil {
		t.Fatal(err)
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 1 {
		t.Fatalf("second Browser did not reuse the same disk artifact: files=%d err=%v", len(files), err)
	}
	c := second.NewContext()
	defer c.Close()
	p, err := c.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	defer c.ClosePage(p.ID)
	value, err := p.Evaluate(ctx, `Math.abs(Date.now()-performance.timeOrigin)<1000`)
	if err != nil || value != true || !p.Top.Realm.bootstrapRestored {
		t.Fatalf("disk restore must use the new Page clock: value=%v restored=%t err=%v", value, p.Top.Realm.bootstrapRestored, err)
	}
	origin, err := p.Evaluate(ctx, `performance.timeOrigin`)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := origin.(float64)
	if !ok || got < float64(firstOrigin.Add(2*time.Hour).UnixMilli()) {
		t.Fatalf("restored runtime retained the old wall origin: %v", origin)
	}
}

func TestBootstrapDiskSnapshotRestoresFirstPageWithoutPreparation(t *testing.T) {
	serialBrowserTest(t)
	dir := t.TempDir()
	seed, err := New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := seed.PrepareBootstrap(ctx, dir); err != nil {
		seed.Close()
		t.Fatal(err)
	}
	if err := seed.Close(); err != nil {
		t.Fatal(err)
	}

	b, err := New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if err := b.ConfigureBootstrapCache(dir); err != nil {
		t.Fatal(err)
	}
	c := b.NewContext()
	defer c.Close()
	p, err := c.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	defer c.ClosePage(p.ID)
	if _, err := p.Evaluate(ctx, "true"); err != nil {
		t.Fatal(err)
	}
	if !p.Top.Realm.bootstrapRestored {
		t.Fatal("first Page did not use the available disk snapshot")
	}
}

func TestConfigureBootstrapCacheDoesNotCreatePages(t *testing.T) {
	serialBrowserTest(t)
	b, err := New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if err := b.ConfigureBootstrapCache(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	b.mu.RLock()
	contexts := len(b.contexts)
	b.mu.RUnlock()
	b.bootstrapSnapshots.mu.Lock()
	entries := len(b.bootstrapSnapshots.entries)
	configured := b.bootstrapSnapshots.disk != nil
	b.bootstrapSnapshots.mu.Unlock()
	if contexts != 0 || entries != 0 || !configured {
		t.Fatalf("cache setup did bootstrap work: contexts=%d entries=%d configured=%t", contexts, entries, configured)
	}
}

func TestBootstrapSnapshotSharedAcrossContexts(t *testing.T) {
	serialBrowserTest(t)
	b, err := New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	seed := b.NewContext()
	p, err := seed.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Evaluate(context.Background(), "true"); err != nil {
		t.Fatal(err)
	}
	key := p.Top.Realm.bootstrapSource().key
	seed.ClosePage(p.ID)
	trigger, err := seed.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := trigger.Evaluate(context.Background(), "true"); err != nil {
		t.Fatal(err)
	}
	seed.ClosePage(trigger.ID)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := b.bootstrapSnapshots.wait(ctx); err != nil {
		t.Fatal(err)
	}
	if err := seed.Close(); err != nil {
		t.Fatal(err)
	}

	sibling := b.NewContext()
	defer sibling.Close()
	restored, err := sibling.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restored.Evaluate(context.Background(), "true"); err != nil {
		t.Fatal(err)
	}
	defer sibling.ClosePage(restored.ID)
	if restored.Top.Realm.bootstrapSource().key != key || !restored.Top.Realm.bootstrapRestored {
		b.bootstrapSnapshots.mu.Lock()
		entry := b.bootstrapSnapshots.entries[key]
		var snapshotReady bool
		var snapshotErr error
		if entry != nil {
			snapshotReady, snapshotErr = entry.snapshot != nil, entry.err
		}
		b.bootstrapSnapshots.mu.Unlock()
		t.Fatalf("new Context did not restore the Browser-owned bootstrap snapshot: keyMatch=%v ready=%v err=%v", restored.Top.Realm.bootstrapSource().key == key, snapshotReady, snapshotErr)
	}
}

func TestBrowserCloseDisposesSharedBootstrapAndRejectsPages(t *testing.T) {
	serialBrowserTest(t)
	b, err := New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	c := b.NewContext()
	if _, err := c.NewPage(); err != nil {
		t.Fatal(err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := b.NewContext().NewPage(); err == nil {
		t.Fatal("closed Browser admitted a new Page")
	}
	b.bootstrapSnapshots.mu.Lock()
	closed := b.bootstrapSnapshots.closed
	entries := len(b.bootstrapSnapshots.entries)
	b.bootstrapSnapshots.mu.Unlock()
	if !closed || entries != 0 {
		t.Fatalf("bootstrap cache after Browser.Close: closed=%v entries=%d", closed, entries)
	}
}
