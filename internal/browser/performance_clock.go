package browser

import (
	"crypto/rand"
	"encoding/binary"
	"net/url"
	"time"

	"github.com/moreveal/mimic/internal/network"
)

// Bind once to the initiating realm, not whichever document occupies its
// frame when the response arrives. No runtime handle escapes to the loader.
func (r *Realm) withResourceTiming(request network.Request) network.Request {
	request.ExecutionOwner = r.agent.Page().Top.ID
	p := r.agent.Page()
	p.mu.RLock()
	request.HistoryPhase = p.historyIndex
	p.mu.RUnlock()
	if frame, ok := r.agent.(*Frame); ok && frame != r.agent.Page().Top {
		request.ClientIsSubframe = true
	}
	if request.CauseURL == "" && r.currentScript != 0 {
		if source, exists := r.document.GetAttribute(r.currentScript, "src"); exists && request.SourceURL != nil {
			if u, err := url.Parse(source); err == nil {
				request.CauseURL = request.SourceURL.ResolveReference(u).String()
			}
		}
	}
	if request.Owner == "" {
		request.Owner = "frame"
	}
	if request.TopLevelURL == nil {
		request.TopLevelURL, _ = url.Parse(r.agent.Page().URL())
	}
	if request.PerformanceOwner == "" {
		request.PerformanceOwner = r.ID
		request.PerformanceStart = r.performanceClockNow()
		start, clock, scale := request.PerformanceStart, r.scheduler, p.environmentView().Time.NetworkScale
		request.ObservePerformanceCompletion = func(completedMillis float64) {
			completed := start.Add(time.Duration(completedMillis * scale * float64(time.Millisecond)))
			clock.AdvanceTo(completed)
			p.mu.Lock()
			if completed.After(p.clock) {
				p.clock = completed
			}
			p.mu.Unlock()
		}
	}
	return request
}

// A buffered navigation response is complete before document parsing begins.
// Advance the shared Page clock for both top and child commits, so lifecycle
// stamps and execution observe the same completed network operation.
func (p *Page) advanceNavigationResponseClock(origin time.Time, response network.Response) {
	completion := max(response.Duration, time.Duration(response.BrowserVisibleTiming.Phases["responseComplete"]*float64(time.Millisecond)))
	responseTime := origin.Add(time.Duration(float64(completion) * p.environmentView().Time.NavigationScale))
	p.mu.Lock()
	if responseTime.After(p.clock) {
		p.clock = responseTime
	}
	p.mu.Unlock()
}

// Document realms share one active Page turn. A borrowed operation must not
// sample the idle clock of its owning frame while another frame is running.
func (r *Realm) performanceClockNow() time.Time {
	now := r.agent.Page().ClockNow()
	if own := r.scheduler.Now(); own.After(now) {
		return own
	}
	return now
}

// Every entry produced by a document refers to the same committed navigation.
// Same-document history mutations leave its loader identity unchanged.
func (r *Realm) performanceNavigationID() int {
	id := uint32(2166136261)
	for _, octet := range []byte(r.navigationLoaderID) {
		id = (id ^ uint32(octet)) * 16777619
	}
	return int(id%9000) + 1000
}

// Chrome 152 coarsens absolute timestamps before subtracting the origin.
// Each bucket has a stable randomized transition, not fresh jitter per read.
// Reference: chromium/152.0.7977.82 third_party/blink/renderer/core/timing/time_clamper.cc.
// The immutable seed belongs to a Page and is shared with its frames/workers.
type performanceClamper struct{ seed uint64 }

func newPerformanceClamper() performanceClamper {
	var seed [8]byte
	if _, err := rand.Read(seed[:]); err != nil {
		panic(err)
	}
	return performanceClamper{binary.LittleEndian.Uint64(seed[:])}
}

func (c performanceClamper) micros(us int64, isolated bool) int64 {
	negative := us < 0
	if negative {
		us = -us
	}
	step := int64(100)
	if isolated {
		step = 5
	}
	low := us % 10000000000
	bucket := low - low%step
	h := uint64(bucket) ^ c.seed
	h ^= h >> 33
	h *= 0xff51afd7ed558ccd
	h ^= h >> 33
	h *= 0xc4ceb9fe1a85ec53
	h ^= h >> 33
	threshold := float64(h&((1<<52)-1)) / (1 << 52)
	if float64(low-bucket) >= threshold*float64(step) {
		bucket += step
	}
	result := us - low + bucket
	if negative {
		return -result
	}
	return result
}

func (c performanceClamper) now(now, origin time.Time, isolated bool) float64 {
	// Use one Page clock coordinate system; never quantize only the duration.
	delta := c.micros(now.UnixMicro(), isolated) - c.micros(origin.UnixMicro(), isolated)
	if delta < 0 {
		return 0
	}
	return float64(delta) / 1000
}
