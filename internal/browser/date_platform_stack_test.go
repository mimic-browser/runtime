//go:build (windows || linux) && amd64

package browser

import (
	_ "embed"
	"testing"
)

//go:embed testdata/date_platform_stack.js
var datePlatformStackProbe string

// Structured frames measured in headful Chrome 152.0.7977.82. The ordinary
// browser surface adds locale projection to the engine's clock constructor.
func TestDateCoercionStackMatchesFrozenChrome(t *testing.T) {
	serialBrowserTest(t)
	for _, mode := range []string{"cold", "snapshot"} {
		t.Run(mode, func(t *testing.T) {
			if mode == "cold" {
				t.Setenv("MIMIC_DISABLE_BOOTSTRAP_SNAPSHOT", "1")
			}
			p := bootstrapSnapshotPage(t)
			if mode == "snapshot" {
				bootstrapSnapshotWarm(t, p)
				var err error
				p, err = p.ctx.NewPage()
				if err != nil {
					t.Fatal(err)
				}
				bootstrapSnapshotEvaluate(t, p, "true")
				if !p.Top.Realm.bootstrapRestored {
					t.Fatal("stack probe did not restore its parent realm")
				}
			}
			if got := bootstrapSnapshotEvaluate(t, p, datePlatformStackProbe); got != "ok" {
				t.Fatalf("Date stack: %v", got)
			}
		})
	}
}
