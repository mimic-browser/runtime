package browser

import (
	"time"

	"github.com/moreveal/mimic/internal/engine"
)

// memoryProjection reports a synthetic application heap, not the Go heap, V8
// bootstrap heap or process RSS. Allocation deltas are optional provider input;
// capacities and reporting precision belong to the platform-neutral model.
// Exact sizes and GC scheduling are environment-dependent, as in Chrome.
type memoryProjection struct {
	baseline           uint64
	ready              bool
	last               time.Time
	used, total, limit uint64
}

func (r *Realm) initializeMemoryProjection() {
	if r.memoryProjection.ready {
		return
	}
	r.memoryProjection.ready = true
	if provider, ok := r.runtime.(engine.AllocationRuntime); ok {
		if sample, err := provider.SampleAllocations(); err == nil {
			r.memoryProjection.baseline = sample.UsedBytes
		}
	}
}

func (r *Realm) installPerformanceMemory(host map[string]any) {
	host["performanceMemory"] = r.fn(func(engine.Value, []engine.Value) (engine.Value, error) {
		r.initializeMemoryProjection()
		m := &r.memoryProjection
		now := r.scheduler.Now()
		if m.last.IsZero() || now.Sub(m.last) >= 50*time.Millisecond {
			const quantum = uint64(64 * 1024)
			// The machine profile supplies an application budget. A document's
			// modeled initial graph plus post-bootstrap allocations supply usage.
			physical := uint64(r.agent.Page().environmentView().Hardware.DeviceMemoryGB * float64(uint64(1)<<30))
			// 64-bit desktop V8's default old-generation budget is accompanied by
			// its young-generation reservation. Chrome reports their combined
			// 4192 MiB limit through the non-standard performance.memory surface.
			const desktopV8Limit = uint64(4192) * 1024 * 1024
			m.limit = max(16*quantum, min(physical/2, desktopV8Limit))
			logical := uint64(len(r.document.TextContent(r.document.Root().ID)))*2 + quantum
			if provider, ok := r.runtime.(engine.AllocationRuntime); ok {
				sample, err := provider.SampleAllocations()
				if err != nil {
					return nil, err
				}
				if sample.UsedBytes > m.baseline {
					logical += sample.UsedBytes - m.baseline
				}
			}
			m.used = min(m.limit, ((logical+quantum-1)/quantum)*quantum)
			m.total = min(m.limit, ((m.used+m.used/2+quantum-1)/quantum)*quantum)
			m.last = now
		}
		return r.val(map[string]any{"usedJSHeapSize": m.used, "totalJSHeapSize": m.total, "jsHeapSizeLimit": m.limit}), nil
	})
}
