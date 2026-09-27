//go:build (windows || linux) && amd64

package browser

import (
	"context"
	_ "embed"
	"testing"

	chrome152 "github.com/moreveal/mimic/chrome/152"
	v8engine "github.com/moreveal/mimic/internal/engine/v8"
)

//go:embed testdata/frame_platform_stack.js
var framePlatformStackProbe string

//go:embed testdata/frame_stack_continuity.js
var frameStackContinuityProbe string

func TestConnectedFrameStacksMatchFrozenChrome(t *testing.T) {
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
			got := bootstrapSnapshotEvaluate(t, p, frameStackContinuityProbe)
			if got != "ok" {
				t.Fatalf("connected author stacks: %v", got)
			}
		})
	}
}

// Verify the platform provenance boundary independently of the still-separate
// realm stack continuity requirement. Author frames must remain observable.
func TestFrameReflectionDoesNotPublishPlatformFrames(t *testing.T) {
	serialBrowserTest(t)
	b, err := New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	c := b.NewContext()
	defer c.Close()
	p, err := c.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	got, err := p.Evaluate(context.Background(), framePlatformStackProbe)
	if err != nil || got != "ok" {
		t.Fatalf("frame stack provenance: %v %v", got, err)
	}
}
