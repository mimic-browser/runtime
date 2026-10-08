// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package engine

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/go-webengine/engine/dom"
)

// moduleDoc builds a Document with n inline `type="module"` scripts.
func moduleDoc(t *testing.T, n int) *Document {
	t.Helper()
	var b strings.Builder
	b.WriteString("<html><body>")
	for i := 0; i < n; i++ {
		// Unique export name per module so esbuild does not reject duplicates.
		fmt.Fprintf(&b, `<script type="module">export const v%d = 1;</script>`, i)
	}
	b.WriteString("</body></html>")
	root, err := dom.Parse(b.String())
	if err != nil {
		t.Fatal(err)
	}
	return &Document{URL: "https://x.test/", Root: root, HTML: b.String()}
}

// TestModuleBundleSkipsHeavyEntryGraph covers the entry-count gate: a page with
// more than maxModuleEntryScripts module scripts skips bundling (so a heavy
// framework SPA cannot fan its import graph out into an OOM), while a page with a
// handful still bundles.
func TestModuleBundleSkipsHeavyEntryGraph(t *testing.T) {
	e := New()

	heavy := moduleDoc(t, maxModuleEntryScripts+1)
	_, stats, ok := e.bundleModuleScripts(context.Background(), heavy)
	if ok {
		t.Error("a page over the module-entry cap must skip bundling")
	}
	if stats.entries != maxModuleEntryScripts+1 {
		t.Errorf("entries = %d, want %d", stats.entries, maxModuleEntryScripts+1)
	}
	if !strings.Contains(stats.firstErr, "too many entry scripts") {
		t.Errorf("firstErr = %q, want the skip reason", stats.firstErr)
	}

	// A small module page (inline, no imports → no network) still bundles.
	light := moduleDoc(t, maxModuleEntryScripts)
	out, lstats, lok := e.bundleModuleScripts(context.Background(), light)
	if !lok || out == "" {
		t.Errorf("a light module page should bundle; ok=%v outLen=%d firstErr=%q", lok, len(out), lstats.firstErr)
	}
	if strings.Contains(lstats.firstErr, "too many entry scripts") {
		t.Error("a light module page must not hit the entry-count gate")
	}
}

// TestHeapGuardedContextDisabled covers the negative-cap disable path.
func TestHeapGuardedContextDisabled(t *testing.T) {
	e := New()
	e.MaxJSHeapBytes = -1
	ctx, stop := e.heapGuardedContext(context.Background())
	if ctx.Err() != nil {
		t.Error("a disabled guard must not pre-cancel the context")
	}
	stop()
	if ctx.Err() == nil {
		t.Error("stop must cancel the derived context")
	}
}

// TestHeapGuardedContextStop covers the normal stop path (no heap trip).
func TestHeapGuardedContextStop(t *testing.T) {
	e := New()
	ctx, stop := e.heapGuardedContext(context.Background())
	if ctx.Err() != nil {
		t.Fatal("guard cancelled before any heap growth")
	}
	stop()
	if ctx.Err() == nil {
		t.Error("stop must cancel the derived context")
	}
}

// TestHeapGuardedContextTripsOnGrowth covers the watchdog firing: an allocation
// that grows the heap past the cap cancels the context. A GC before the guard is
// created settles the baseline to the live set (HeapAlloc is a GC sawtooth, so a
// transient baseline could otherwise sit above a small post-GC live+alloc), and
// the allocation is an order of magnitude over the cap so it trips regardless of
// GC timing.
func TestHeapGuardedContextTripsOnGrowth(t *testing.T) {
	e := New()
	e.MaxJSHeapBytes = 8 << 20 // 8MB cap
	runtime.GC()               // settle the baseline the guard is about to capture
	ctx, stop := e.heapGuardedContext(context.Background())
	defer stop()
	// Allocate ~96MB and keep it live so HeapAlloc stays well above base+8MB.
	blob := make([]byte, 96<<20)
	for i := 0; i < len(blob); i += 4096 {
		blob[i] = byte(i)
	}
	select {
	case <-ctx.Done():
		// cancelled as expected
	case <-time.After(4 * time.Second):
		t.Error("heap guard did not cancel after heap growth")
	}
	runtime.KeepAlive(blob)
}
