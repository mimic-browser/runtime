package trace

import (
	"context"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Kind string

const (
	API             Kind = "api"
	Unsupported     Kind = "unsupported"
	SurfaceMissing  Kind = "surface-missing"
	SemanticMissing Kind = "semantic-missing"
	Network         Kind = "network"
	Console         Kind = "console"
	Exception       Kind = "exception"
	Lifecycle       Kind = "lifecycle"
	CDP             Kind = "cdp"
	Resource        Kind = "resource"
	DOM             Kind = "dom"
	JS              Kind = "js"
	CSP             Kind = "csp"
	Scheduler       Kind = "scheduler"
	Error           Kind = "error"
)

type Event struct {
	Sequence uint64         `json:"sequence"`
	Time     time.Time      `json:"time"`
	Kind     Kind           `json:"kind"`
	Name     string         `json:"name"`
	Data     map[string]any `json:"data,omitempty"`
}
type Recorder struct {
	mu         sync.RWMutex
	next       uint64
	events     []Event
	eventStart int
	enabled    bool
	// Network completions are consumed by Performance timelines independently of
	// the bounded diagnostic history. A slow consumer must not lose an entry.
	networkEvents     []Event
	subscribers       map[uint64]subscriber
	subID             uint64
	observationChange func(bool, bool)
}

type subscriber struct {
	callback func(Event)
	kinds    map[Kind]bool // nil receives every kind
}

const maxDiagnosticEvents = 8192
const maxDiagnosticResultIDs = 128

type correlation struct {
	id       string
	started  time.Time
	recorder *Recorder
	spanSeq  atomic.Uint64
}

type correlationKey struct{}

// WithCorrelation attaches one request-level monotonic timeline to a context.
// The recorder is optional so engine-neutral callers can still propagate the ID.
func WithCorrelation(ctx context.Context, id string, started time.Time, recorder *Recorder) context.Context {
	return context.WithValue(ctx, correlationKey{}, &correlation{id: id, started: started, recorder: recorder})
}

func CorrelationID(ctx context.Context) string {
	if value, ok := ctx.Value(correlationKey{}).(*correlation); ok && value != nil {
		return value.id
	}
	return ""
}

func NextCorrelationSpan(ctx context.Context) uint64 {
	if value, ok := ctx.Value(correlationKey{}).(*correlation); ok && value != nil {
		return value.spanSeq.Add(1)
	}
	return 0
}

func CorrelatedData(ctx context.Context, data map[string]any) map[string]any {
	out := make(map[string]any, len(data)+4)
	for key, value := range data {
		out[key] = value
	}
	value, ok := ctx.Value(correlationKey{}).(*correlation)
	if !ok || value == nil {
		return out
	}
	out["callID"] = value.id
	out["elapsedNs"] = time.Since(value.started).Nanoseconds()
	out["goroutineID"] = goroutineID()
	return out
}

// Record emits an invocation-correlated event when the context carries a
// recorder. All elapsed values use time.Since on the original call start and
// therefore remain monotonic even though Event.Time is wall-clock metadata.
func Record(ctx context.Context, kind Kind, name string, data map[string]any) {
	value, ok := ctx.Value(correlationKey{}).(*correlation)
	if !ok || value == nil || value.recorder == nil {
		return
	}
	value.recorder.Add(kind, name, CorrelatedData(ctx, data))
}

func goroutineID() uint64 {
	var buffer [64]byte
	n := runtime.Stack(buffer[:], false)
	fields := strings.Fields(string(buffer[:n]))
	if len(fields) < 2 {
		return 0
	}
	id, _ := strconv.ParseUint(fields[1], 10, 64)
	return id
}

func New() *Recorder { return &Recorder{subscribers: map[uint64]subscriber{}} }
func (r *Recorder) Enabled() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.enabled
}

// Start begins a new explicit diagnostic capture. Live subscribers and
// Performance consumers receive events independently of capture.
func (r *Recorder) Start() {
	r.mu.Lock()
	r.events = nil
	r.eventStart = 0
	r.enabled = true
	change := r.observationChange
	r.mu.Unlock()
	if change != nil {
		change(true, true)
	}
}

func (r *Recorder) Stop() {
	r.mu.Lock()
	r.enabled = false
	wanted := r.observationWantedLocked()
	change := r.observationChange
	r.mu.Unlock()
	if change != nil {
		change(wanted, false)
	}
}

func (r *Recorder) observationWantedLocked() bool {
	if r.enabled {
		return true
	}
	for _, sub := range r.subscribers {
		if sub.kinds == nil || sub.kinds[API] || sub.kinds[Unsupported] {
			return true
		}
	}
	return false
}

// ObservationWanted includes diagnostic capture and subscribers that consume
// API/unsupported-property events. It is independent of network event demand.
func (r *Recorder) ObservationWanted() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.observationWantedLocked()
}

// SetObservationChange connects a Page-local property observer to demand. The
// callback runs outside the recorder lock to permit realm callbacks to trace.
func (r *Recorder) SetObservationChange(change func(bool, bool)) {
	r.mu.Lock()
	r.observationChange = change
	wanted := r.observationWantedLocked()
	r.mu.Unlock()
	if change != nil {
		change(wanted, false)
	}
}

// Wants reports whether an event needs diagnostic capture or live delivery.
// Callers may avoid preparing expensive diagnostic-only payloads when false.
func (r *Recorder) Wants(kind Kind) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.enabled {
		return true
	}
	for _, sub := range r.subscribers {
		if sub.kinds == nil || sub.kinds[kind] {
			return true
		}
	}
	return false
}

func (r *Recorder) Add(kind Kind, name string, data map[string]any) {
	r.mu.Lock()
	if !r.enabled && !(kind == Network && name == "response") {
		wanted := false
		for _, sub := range r.subscribers {
			if sub.kinds == nil || sub.kinds[kind] {
				wanted = true
				break
			}
		}
		if !wanted {
			r.mu.Unlock()
			return
		}
	}
	r.next++
	e := Event{r.next, time.Now().UTC(), kind, name, data}
	if kind == Network && name == "response" {
		r.networkEvents = append(r.networkEvents, e)
	}
	if r.enabled {
		// Retain a short prefix of large selector results for diagnosis.
		if ids, ok := data["resultNodeIds"].([]int64); ok && len(ids) > maxDiagnosticResultIDs {
			limited := make(map[string]any, len(data)+1)
			for key, value := range data {
				limited[key] = value
			}
			limited["resultNodeIds"] = append([]int64(nil), ids[:maxDiagnosticResultIDs]...)
			limited["resultCount"] = len(ids)
			e.Data = limited
		}
		if len(r.events) == maxDiagnosticEvents {
			r.events[r.eventStart] = e
			r.eventStart = (r.eventStart + 1) % maxDiagnosticEvents
		} else {
			r.events = append(r.events, e)
		}
	}
	subs := make([]func(Event), 0, len(r.subscribers))
	for _, sub := range r.subscribers {
		if sub.kinds == nil || sub.kinds[kind] {
			subs = append(subs, sub.callback)
		}
	}
	r.mu.Unlock()
	for _, f := range subs {
		f(e)
	}
}
func (r *Recorder) Events() []Event {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Event, 0, len(r.events))
	out = append(out, r.events[r.eventStart:]...)
	out = append(out, r.events[:r.eventStart]...)
	return out
}

// EventsSince returns network completions for Performance timeline consumers.
// They are kept apart from the bounded diagnostic history so a slow realm or
// Worker does not silently lose resource timing entries.
func (r *Recorder) EventsSince(sequence uint64) []Event {
	r.mu.RLock()
	defer r.mu.RUnlock()
	start := sort.Search(len(r.networkEvents), func(i int) bool { return r.networkEvents[i].Sequence > sequence })
	return append([]Event(nil), r.networkEvents[start:]...)
}
func (r *Recorder) Clear() {
	r.mu.Lock()
	r.events = nil
	r.eventStart = 0
	r.mu.Unlock()
}
func (r *Recorder) Subscribe(f func(Event)) func() {
	return r.SubscribeKinds(nil, f)
}

// SubscribeKinds limits delivery to the kinds consumed by a subscriber.
// An empty kinds list retains Subscribe's all-event behavior.
func (r *Recorder) SubscribeKinds(kinds []Kind, f func(Event)) func() {
	r.mu.Lock()
	previous := r.observationWantedLocked()
	r.subID++
	id := r.subID
	var filter map[Kind]bool
	if len(kinds) != 0 {
		filter = make(map[Kind]bool, len(kinds))
		for _, kind := range kinds {
			filter[kind] = true
		}
	}
	r.subscribers[id] = subscriber{callback: f, kinds: filter}
	wanted := r.observationWantedLocked()
	change := r.observationChange
	r.mu.Unlock()
	if change != nil && wanted != previous {
		change(wanted, false)
	}
	return func() {
		r.mu.Lock()
		previous := r.observationWantedLocked()
		delete(r.subscribers, id)
		wanted := r.observationWantedLocked()
		change := r.observationChange
		r.mu.Unlock()
		if change != nil && wanted != previous {
			change(wanted, false)
		}
	}
}
