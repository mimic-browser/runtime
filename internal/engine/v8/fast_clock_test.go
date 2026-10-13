//go:build (windows || linux) && amd64

package v8

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moreveal/mimic/internal/engine"
)

func TestHotClockReadsStayInsideIsolate(t *testing.T) {
	runtime := (Factory{}).New()
	defer runtime.Close()
	var samples atomic.Int64
	origin := time.Unix(1700000000, 0)
	runtime.SetTimeSource(func() time.Time {
		samples.Add(1)
		return origin
	})
	runtime.(engine.FastClockRuntime).SetTimeScale(0)
	performanceNow, err := runtime.(engine.FastClockRuntime).NewPerformanceNow(origin, 123456789, 81910026.7959, false)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.(engine.ValueReleaser).ReleaseValue(performanceNow)
	if err := runtime.Set("__clock", performanceNow); err != nil {
		t.Fatal(err)
	}
	before := samples.Load()
	result, err := runtime.Eval(context.Background(), `
let stable = true;
for (let i = 0; i < 100000; i++) {
  stable = stable && Date.now() === 1700000000000 && __clock() === 0;
}
stable;
`, "fast-clock-regression.js")
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.(engine.ValueReleaser).ReleaseValue(result)
	if result.Export() != true {
		t.Fatal("native clocks did not preserve the published browser time")
	}
	if calls := samples.Load() - before; calls > 2 {
		t.Fatalf("hot clock loop crossed into the host %d times", calls)
	}
}
