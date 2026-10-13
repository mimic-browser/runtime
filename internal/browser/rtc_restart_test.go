//go:build (windows || linux) && amd64

package browser

import (
	"context"
	_ "embed"
	"os"
	"strings"
	"testing"
	"time"
)

//go:embed testdata/rtc_restart.js
var rtcRestartScript string

//go:embed testdata/rtc_rollback_unsupported.js
var rtcRollbackUnsupportedScript string

func TestRTCMediaRollbackRejectsWithoutChangingDescriptions(t *testing.T) {
	serialBrowserTest(t)
	cameraTestPages(t, func(t *testing.T, p *Page) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		result, err := p.Evaluate(ctx, strings.TrimSuffix(strings.TrimSpace(rtcRollbackUnsupportedScript), ";")+".catch(error => error.name + ': ' + error.message)")
		if err != nil || result != true {
			t.Fatalf("media rollback boundary: %v; result %v", err, result)
		}
	})
}

func TestRTCNativeRestartRollbackPreservesNegotiationAndDataChannel(t *testing.T) {
	serialBrowserTest(t)
	run := func(t *testing.T, p *Page) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		result, err := p.Evaluate(ctx, strings.TrimSuffix(strings.TrimSpace(rtcRestartScript), ";")+".catch(error => error.name + ': ' + error.message)")
		if err != nil || result != true {
			t.Fatalf("native restart/rollback: %v; result %v", err, result)
		}
	}
	cameraTestPages(t, run)
	t.Run("restored", func(t *testing.T) {
		if os.Getenv("MIMIC_DISABLE_BOOTSTRAP_SNAPSHOT") == "1" {
			t.Skip("bootstrap snapshots disabled by the test environment")
		}
		seed := bootstrapSnapshotPage(t)
		navigateCapabilityFixture(t, seed)
		bootstrapSnapshotWarm(t, seed)
		p, err := seed.ctx.NewPage()
		if err != nil {
			t.Fatal(err)
		}
		navigateCapabilityFixture(t, p)
		run(t, p)
		if !p.Top.Realm.bootstrapRestored {
			t.Fatal("RTC regression did not execute in a restored realm")
		}
	})
}
