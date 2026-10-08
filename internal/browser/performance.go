package browser

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/moreveal/mimic/internal/engine"
	"github.com/moreveal/mimic/internal/scheduler"
)

// performanceTimeline belongs to one execution agent. Loader/lifecycle records
// supply external entries; this store owns timeline membership, user timing,
// observer queues and buffer limits. JS caches wrappers, never another timeline.
// All methods execute on the owning event loop (including Worker agents).
type performanceTimeline struct {
	runtime                                                             engine.Runtime
	scheduler                                                           *scheduler.Scheduler
	now                                                                 func() float64
	navigationID                                                        func() int
	navigation                                                          func() map[string]any
	syncExternal                                                        func()
	entries                                                             []*performanceRecord
	records                                                             map[uint64]*performanceRecord
	observers                                                           map[uint64]*performanceSubscription
	next                                                                uint64
	resourceLimit                                                       uint64
	droppedEntries                                                      map[string]uint64
	secondary                                                           []*performanceRecord
	bufferQueued, deliveryQueued                                        bool
	bufferCallback                                                      engine.Value
	navigationRecord                                                    *performanceRecord
	navigationDelivered                                                 bool
	worker                                                              bool
	eventCounts                                                         map[string]uint64
	pendingEvents                                                       []*performanceRecord
	interactionCount, interactionID, keyInteraction, pointerInteraction uint64
	firstInput                                                          bool
}

type performanceRecord struct {
	id       uint64
	data     map[string]any
	detail   engine.Value
	buffered bool
	// Resource start before timer precision reduction; never exposed to scripts.
	sortStart time.Time
}

type performanceSubscription struct {
	id             uint64
	callback       engine.Value
	mode           string
	types          map[string]bool
	records        []*performanceRecord
	reportDropped  bool
	active         bool
	eventThreshold float64
}

func newPerformanceTimeline(runtime engine.Runtime, s *scheduler.Scheduler, now func() float64, navigationID func() int, worker bool) *performanceTimeline {
	return &performanceTimeline{runtime: runtime, scheduler: s, now: now, navigationID: navigationID, records: map[uint64]*performanceRecord{}, observers: map[uint64]*performanceSubscription{}, droppedEntries: map[string]uint64{}, resourceLimit: 250, worker: worker}
}

func (p *performanceTimeline) sync() {
	if p.syncExternal != nil {
		p.syncExternal()
	}
}

func (p *performanceTimeline) create(data map[string]any, detail engine.Value) *performanceRecord {
	p.next++
	r := &performanceRecord{id: p.next, data: data, detail: detail}
	data["_id"] = r.id
	if _, ok := data["navigationId"]; !ok {
		data["navigationId"] = p.navigationID()
	}
	p.records[r.id] = r
	return r
}

func (p *performanceTimeline) value(r *performanceRecord) map[string]any {
	if r == p.navigationRecord && p.navigation != nil {
		data := p.navigation()
		data["_id"] = r.id
		return data
	}
	return r.data
}

func (p *performanceTimeline) values(records []*performanceRecord, ordered bool) []map[string]any {
	if ordered {
		records = append([]*performanceRecord(nil), records...)
		sort.SliceStable(records, func(i, j int) bool {
			left, right := numberValue(records[i].data["startTime"]), numberValue(records[j].data["startTime"])
			if left != right {
				return left < right
			}
			if !records[i].sortStart.IsZero() && !records[j].sortStart.IsZero() {
				return records[i].sortStart.Before(records[j].sortStart)
			}
			return false
		})
	}
	values := make([]map[string]any, 0, len(records))
	for _, record := range records {
		values = append(values, p.value(record))
	}
	return values
}

func (p *performanceTimeline) append(r *performanceRecord) {
	typ, _ := r.data["entryType"].(string)
	if typ == "longtask" {
		count := 0
		for _, entry := range p.entries {
			if entry.data["entryType"] == typ {
				count++
			}
		}
		if count >= 200 {
			p.droppedEntries[typ]++
			p.publish(r)
			p.prune()
			return
		}
	}
	if typ == "resource" && (p.resourceCount() >= p.resourceLimit || p.bufferQueued) {
		p.droppedEntries[typ]++
		p.secondary = append(p.secondary, r)
		p.queueBufferFull()
	} else {
		r.buffered = true
		p.entries = append(p.entries, r)
	}
	p.publish(r)
}

func (p *performanceTimeline) resourceCount() uint64 {
	var count uint64
	for _, r := range p.entries {
		if r.data["entryType"] == "resource" {
			count++
		}
	}
	return count
}

func (p *performanceTimeline) publish(r *performanceRecord) {
	typ, _ := r.data["entryType"].(string)
	for _, observer := range p.observers {
		if observer.active && observer.types[typ] && (typ != "event" || numberValue(r.data["duration"]) >= observer.eventThreshold) {
			observer.records = append(observer.records, r)
		}
	}
	p.queueDelivery()
}

func (p *performanceTimeline) queueDelivery() {
	if p.deliveryQueued {
		return
	}
	pending := false
	for _, observer := range p.observers {
		if observer.active && len(observer.records) > 0 {
			pending = true
			break
		}
	}
	if !pending {
		return
	}
	p.deliveryQueued = true
	p.scheduler.Post(scheduler.DOM, 0, func(ctx context.Context) error {
		p.deliveryQueued = false
		// Registration order, not Go map order. Mutations during a callback are
		// observed by later callbacks; newly registered observers wait a turn.
		ids := make([]uint64, 0, len(p.observers))
		for id, observer := range p.observers {
			if observer.active {
				ids = append(ids, id)
			}
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		for _, id := range ids {
			observer := p.observers[id]
			if !observer.active || len(observer.records) == 0 {
				continue
			}
			records := p.values(observer.records, true)
			observer.records = nil
			var dropped any
			if observer.reportDropped {
				var count uint64
				for typ := range observer.types {
					count += p.droppedEntries[typ]
				}
				dropped = count
				observer.reportDropped = false
			}
			if err := p.invoke(ctx, observer.callback, records, dropped); err != nil {
				return err
			}
		}
		return nil
	})
}

func (p *performanceTimeline) queueBufferFull() {
	if p.bufferQueued {
		return
	}
	p.bufferQueued = true
	p.scheduler.Post(scheduler.DOM, 0, func(ctx context.Context) error {
		defer func() { p.bufferQueued = false }()
		for len(p.secondary) > 0 {
			previous := len(p.secondary)
			if p.resourceCount() >= p.resourceLimit && p.bufferCallback != nil {
				if err := p.invoke(ctx, p.bufferCallback); err != nil {
					return err
				}
			}
			for len(p.secondary) > 0 && p.resourceCount() < p.resourceLimit {
				r := p.secondary[0]
				p.secondary = p.secondary[1:]
				r.buffered = true
				p.entries = append(p.entries, r)
			}
			if len(p.secondary) >= previous {
				p.secondary = nil
				p.prune()
			}
		}
		return nil
	})
}

// Notification arguments and return values belong only to this delivery. Even
// an undefined return owns a native handle. Keep creation, call and cleanup on
// the same owner operation; errors still escape to the scheduler's owner.
func (p *performanceTimeline) invoke(ctx context.Context, callback engine.Value, args ...any) error {
	operation := func(ctx context.Context) error {
		values := make([]engine.Value, len(args))
		for i, arg := range args {
			values[i] = p.runtime.Value(arg)
		}
		defer releaseRuntimeValues(p.runtime, values...)
		result, err := p.runtime.Call(ctx, callback, nil, values...)
		releaseRuntimeValues(p.runtime, result)
		return err
	}
	if owner, ok := p.runtime.(engine.OwnerRuntime); ok {
		return owner.RunOnOwner(ctx, operation)
	}
	return operation(ctx)
}

func (p *performanceTimeline) clear(typ, name string, hasName bool) {
	retained := p.entries[:0]
	for _, r := range p.entries {
		if r.data["entryType"] == typ && (!hasName || r.data["name"] == name) {
			r.buffered = false
		} else {
			retained = append(retained, r)
		}
	}
	clear(p.entries[len(retained):])
	p.entries = retained
}

// Immutable wrappers may outlive timeline membership. Their detail reference is
// materialized once by the binding, so removing the runtime's root does not
// change a retained wrapper. Pending observer queues keep canonical entries
// alive until all deliveries/takeRecords have materialized those same wrappers.
func (p *performanceTimeline) prune() []uint64 {
	protected := map[uint64]bool{}
	for _, o := range p.observers {
		for _, r := range o.records {
			protected[r.id] = true
		}
	}
	for _, r := range p.secondary {
		protected[r.id] = true
	}
	for _, r := range p.pendingEvents {
		protected[r.id] = true
	}
	var released []uint64
	for id, r := range p.records {
		if r.buffered || protected[id] {
			continue
		}
		delete(p.records, id)
		r.detail = nil
		released = append(released, id)
	}
	return released
}

func performanceQueryable(typ string) bool {
	switch typ {
	case "mark", "measure", "resource", "navigation", "paint", "visibility-state", "first-input":
		return true
	}
	return false
}

func (p *performanceTimeline) install(host map[string]any) {
	host["performanceInstallBuffer"] = p.runtime.Function(func(_ engine.Value, a []engine.Value) (engine.Value, error) { p.bufferCallback = a[0]; return nil, nil })
	host["performance"] = p.runtime.Function(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		op := strarg(a, 0)
		switch op {
		case "prune":
			return p.runtime.Value(p.prune()), nil
		case "worker":
			return p.runtime.Value(p.worker), nil
		case "eventCount":
			return p.runtime.Value(p.eventCounts[strarg(a, 1)]), nil
		case "interactionCount":
			return p.runtime.Value(p.interactionCount), nil
		case "installBuffer":
			p.bufferCallback = a[1]
			return nil, nil
		case "entries":
			p.sync()
			typ, name := strarg(a, 1), strarg(a, 2)
			hasName := len(a) > 3 && arg(a, 3) == true
			var records []*performanceRecord
			for _, r := range p.entries {
				entryType, _ := r.data["entryType"].(string)
				if performanceQueryable(entryType) && (typ == "" || typ == entryType) && (!hasName || r.data["name"] == name) {
					records = append(records, r)
				}
			}
			return p.runtime.Value(p.values(records, true)), nil
		case "entry":
			if r := p.records[uint64(numarg(a, 1))]; r != nil {
				return p.runtime.Value(p.value(r)), nil
			}
			return nil, fmt.Errorf("unknown Performance entry")
		case "detail":
			if r := p.records[uint64(numarg(a, 1))]; r != nil && r.detail != nil {
				return r.detail, nil
			}
			return p.runtime.Value(nil), nil
		case "add":
			data := map[string]any{"name": strarg(a, 1), "entryType": strarg(a, 2), "startTime": numarg(a, 3), "duration": numarg(a, 4)}
			r := p.create(data, a[5])
			if arg(a, 6) == true {
				p.append(r)
			}
			return p.runtime.Value(r.data), nil
		case "clear":
			p.sync()
			p.clear(strarg(a, 1), strarg(a, 2), arg(a, 3) == true)
			return nil, nil
		case "bufferSize":
			p.sync()
			p.resourceLimit = uint64(numarg(a, 1))
			return nil, nil
		case "latest":
			name := strarg(a, 1)
			var latest *performanceRecord
			for _, r := range p.entries {
				if r.data["entryType"] == "mark" && r.data["name"] == name {
					if latest == nil || numberValue(r.data["startTime"]) >= numberValue(latest.data["startTime"]) {
						latest = r
					}
				}
			}
			if latest != nil {
				return p.runtime.Value(latest.data["startTime"]), nil
			}
			return p.runtime.Value(nil), nil
		case "observerCreate":
			p.next++
			p.observers[p.next] = &performanceSubscription{id: p.next, types: map[string]bool{}}
			return p.runtime.Value(p.next), nil
		case "observe":
			p.sync()
			o := p.observers[uint64(numarg(a, 1))]
			mode := strarg(a, 2)
			if o.mode != "" && o.mode != mode {
				return p.runtime.Value("InvalidModificationError"), nil
			}
			o.mode = mode
			o.reportDropped = true
			o.callback = a[7]
			if strarg(a, 5) == "event" {
				o.eventThreshold = numarg(a, 6)
			}
			o.active = true
			if mode == "multiple" {
				o.types = map[string]bool{}
			}
			for _, raw := range arg(a, 3).([]any) {
				o.types[fmt.Sprint(raw)] = true
			}
			if mode == "single" && arg(a, 4) == true {
				for _, r := range p.entries {
					if o.types[fmt.Sprint(r.data["entryType"])] && r.data["entryType"] == strarg(a, 5) {
						o.records = append(o.records, r)
					}
				}
			}
			p.queueDelivery()
			return p.runtime.Value(""), nil
		case "disconnect":
			o := p.observers[uint64(numarg(a, 1))]
			o.active = false
			o.callback = nil
			o.types = map[string]bool{}
			o.records = nil
			o.reportDropped = false
			return nil, nil
		case "records":
			p.sync()
			o := p.observers[uint64(numarg(a, 1))]
			records := p.values(o.records, false)
			o.records = nil
			return p.runtime.Value(records), nil
		case "navigation":
			if p.navigation != nil {
				return p.runtime.Value(p.navigation()), nil
			}
			return p.runtime.Value(nil), nil
		}
		return nil, fmt.Errorf("unknown Performance operation %q", op)
	})
}

func (r *Realm) initPerformance(host map[string]any) {
	p := r.agent.Page()
	// The legacy Chrome extension reports microsecond elapsed time and integral
	// epoch stamps. It does not read the script-visible, clamped Performance API.
	host["legacyChromeCSI"] = r.transientFn(func(engine.Value, []engine.Value) (engine.Value, error) {
		onload := int64(0)
		if stamp := r.performanceLifecycleTimes["domContentLoadedEventEnd"]; !stamp.IsZero() {
			onload = stamp.UnixMilli()
		}
		elapsed := r.performanceClockNow().Sub(r.performanceOrigin).Microseconds()
		if elapsed < 0 {
			elapsed = 0
		}
		transition := 15
		switch r.navigationType {
		case "reload":
			transition = 16
		case "back_forward":
			transition = 6
		}
		return r.val([]any{r.performanceOrigin.UnixMilli(), onload, float64(elapsed) / 1000, transition}), nil
	})
	r.performance = newPerformanceTimeline(r.runtime, r.scheduler, func() float64 {
		return p.performanceClamper.now(r.performanceClockNow(), r.performanceOrigin, r.securityState().crossOriginIsolated)
	}, r.performanceNavigationID, false)
	r.performance.navigation = r.performanceNavigationEntry
	r.performance.syncExternal = r.syncPerformanceEntries
	r.performance.install(host)
	r.installPerformanceMemory(host)
	r.installPerformanceEvents(host)
}

// Lifecycle stamps share the document's scheduler clock. Navigation entries
// remain live; an observer receives that entry only after load has finished.
func (r *Realm) performanceLifecycle(name string) {
	if r.performanceLifecycleTimes == nil {
		r.performanceLifecycleTimes = map[string]time.Time{}
	}
	if r.performanceLifecycleTimes[name].IsZero() {
		r.performanceLifecycleTimes[name] = r.scheduler.Now()
	}
}

func installPerformanceClone(runtime engine.Runtime, host map[string]any) {
	if cloner, ok := runtime.(engine.StructuredCloneRuntime); ok {
		host["performanceClone"] = runtime.Function(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
			value, err := cloner.StructuredClone(a[0], a[1])
			var cloneError *engine.DataCloneError
			if errors.As(err, &cloneError) {
				return runtime.Value([]any{false, cloneError.Message}), nil
			}
			if err != nil {
				return nil, err
			}
			reply, err := runtime.Eval(context.Background(), "[true,null]", "mimic:performance-clone-result")
			if err != nil {
				return nil, err
			}
			if err := runtime.SetProperty(reply, "1", value); err != nil {
				return nil, err
			}
			return reply, nil
		})
	}
}
