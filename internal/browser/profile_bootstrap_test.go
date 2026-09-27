//go:build (windows || linux) && amd64

package browser

import (
	"os"
	"sync/atomic"
	"testing"

	chrome152 "github.com/moreveal/mimic/chrome/152"
	"github.com/moreveal/mimic/internal/engine"
	v8engine "github.com/moreveal/mimic/internal/engine/v8"
)

type profilePreparationFactory struct {
	v8engine.Factory
	ordinaryRuntimes atomic.Int32
}

func (f *profilePreparationFactory) New() engine.Runtime {
	f.ordinaryRuntimes.Add(1)
	return f.Factory.New()
}

func TestProfileBootstrapPreparationUsesOneCaptureRealm(t *testing.T) {
	serialBrowserTest(t)
	if os.Getenv("MIMIC_DISABLE_BOOTSTRAP_SNAPSHOT") == "1" {
		t.Skip("snapshot preparation disabled")
	}
	factory := &profilePreparationFactory{}
	b, err := New(factory, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	c := b.NewContext()
	p, err := c.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	key := p.Top.Realm.bootstrapSource().key
	if err := b.prepareProfileBootstrap(key, p.Top.Realm.securityState()); err != nil {
		t.Fatal(err)
	}
	if !b.bootstrapSnapshots.hasSnapshotKey(key) || factory.ordinaryRuntimes.Load() != 1 {
		t.Fatalf("preparation: artifact=%v ordinary runtimes=%d", b.bootstrapSnapshots.hasSnapshotKey(key), factory.ordinaryRuntimes.Load())
	}
}
