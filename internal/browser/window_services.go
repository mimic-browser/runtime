package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/moreveal/mimic/internal/engine"
	"github.com/moreveal/mimic/internal/scheduler"
)

// The bounded annotation buffer belongs to the document, not its JS wrapper.
// Diagnostics can read it while the runtime is blocked, without entering V8.
type crashAnnotation struct {
	// JSON string tokens preserve JavaScript UTF-16 escaping exactly, including
	// lone surrogates. This is binding serialization, not a second JS data store.
	key, value string
}

type crashReportState struct {
	mu          sync.RWMutex
	requested   bool
	initialized bool
	capacity    uint32
	annotations []crashAnnotation
}

func crashReportJSON(annotations []crashAnnotation) string {
	// JSON.stringify orders array-index property names before string names;
	// other keys retain insertion order and updates do not change that order.
	ordered := append([]crashAnnotation(nil), annotations...)
	index := func(key string) (uint64, bool) {
		var decoded string
		if json.Unmarshal([]byte(key), &decoded) != nil {
			return 0, false
		}
		n, err := strconv.ParseUint(decoded, 10, 32)
		return n, err == nil && n < 4294967295 && strconv.FormatUint(n, 10) == decoded
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		a, ai := index(ordered[i].key)
		b, bi := index(ordered[j].key)
		if ai != bi {
			return ai
		}
		return ai && a < b
	})
	var out strings.Builder
	out.WriteByte('{')
	for i, item := range ordered {
		if i > 0 {
			out.WriteByte(',')
		}
		out.WriteString(item.key)
		out.WriteByte(':')
		out.WriteString(item.value)
	}
	out.WriteByte('}')
	return out.String()
}

func (s *crashReportState) request(size uint32) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.requested {
		return false
	}
	s.requested = true
	s.capacity = size
	return true
}
func (s *crashReportState) initialize() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.requested || s.capacity > 65536 {
		return false
	}
	s.initialized = true
	return true
}
func (s *crashReportState) mutate(method, key, value string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.initialized {
		return "InvalidStateError"
	}
	next := append([]crashAnnotation(nil), s.annotations...)
	found := false
	for i, item := range next {
		if item.key == key {
			found = true
			if method == "delete" {
				next = append(next[:i], next[i+1:]...)
			} else {
				next[i].value = value
			}
			break
		}
	}
	if !found && method == "set" {
		next = append(next, crashAnnotation{key, value})
	}
	if len(crashReportJSON(next)) > int(s.capacity) {
		return "NotAllowedError"
	}
	s.annotations = next
	return ""
}

func (p *Page) CrashReports() map[string]string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	result := map[string]string{}
	for id, frame := range p.frames {
		if frame.Realm == nil {
			continue
		}
		s := &frame.Realm.crashReport
		s.mu.RLock()
		if s.initialized {
			result[id] = crashReportJSON(s.annotations)
		}
		s.mu.RUnlock()
	}
	return result
}

func addWindowServiceHosts(r *Realm, h map[string]any) {
	addPictureInPictureHosts(r, h)
	addNavigationHosts(r, h)
	addIndexedDBHosts(r, h)
	addCacheHosts(r, h)
	addCookieStoreHosts(r, h)
	addLaunchHosts(r, h)
	addSpeechHosts(r, h)
	addCameraHosts(r, h)
	addMicrophoneHosts(r, h)
	addRTCMediaHosts(r, h)
	h["enqueueWebTask"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		fn := a[0]
		var abort engine.Value
		if len(a) > 5 {
			abort = a[5]
		}
		id := r.scheduler.Post(scheduler.WebTask, time.Duration(numarg(a, 2)*float64(time.Millisecond)), func(ctx context.Context) error {
			p := r.agent.Page()
			r.webTaskAbort = abort
			p.userScriptDepth++
			defer func() { p.userScriptDepth-- }()
			_, err := r.runtime.Call(ctx, fn, nil)
			return err
		})
		continuation, _ := arg(a, 3).(bool)
		r.scheduler.SetWebTaskPriority(id, int(numarg(a, 1)), continuation)
		r.scheduler.SetWebTaskSignal(id, uint64(numarg(a, 4)))
		return r.val(id), nil
	})
	h["changeWebTask"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		id := uint64(numarg(a, 0))
		if numarg(a, 1) < 0 {
			r.scheduler.Cancel(id)
		} else {
			continuation, _ := arg(a, 2).(bool)
			r.scheduler.SetWebTaskPriority(id, int(numarg(a, 1)), continuation)
		}
		return nil, nil
	})
	h["currentWebTask"] = r.fn(func(_ engine.Value, _ []engine.Value) (engine.Value, error) {
		id, priority, signal := r.scheduler.CurrentWebTask()
		reply := r.val([]any{id, priority, signal, nil})
		if id != 0 && r.webTaskAbort != nil {
			if err := r.runtime.SetProperty(reply, "3", r.webTaskAbort); err != nil {
				return nil, err
			}
		}
		return reply, nil
	})
	h["newWebTaskSignal"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		return r.val(r.scheduler.NewWebTaskSignal(int(numarg(a, 0)))), nil
	})
	h["webTaskSignalPriority"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		return r.val(r.scheduler.WebTaskSignalPriority(uint64(numarg(a, 0)))), nil
	})
	h["beginWebTaskPriorityChange"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		previous, status := r.scheduler.BeginWebTaskPriorityChange(uint64(numarg(a, 0)), int(numarg(a, 1)))
		return r.val([]any{previous, status}), nil
	})
	h["endWebTaskPriorityChange"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		r.scheduler.EndWebTaskPriorityChange(uint64(numarg(a, 0)))
		return nil, nil
	})
	h["requestCrashReport"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		return r.val(r.crashReport.request(uint32(numarg(a, 0)))), nil
	})
	h["initializeCrashReport"] = r.fn(func(engine.Value, []engine.Value) (engine.Value, error) {
		return r.val(r.crashReport.initialize()), nil
	})
	h["mutateCrashReport"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		return r.val(r.crashReport.mutate(strarg(a, 0), strarg(a, 1), strarg(a, 2))), nil
	})
}

// QueueLaunch is the embedder entry point for an OS/app URL launch. Launches
// wait for a consumer in the active top document and survive its navigation.
func (p *Page) QueueLaunch(targetURL string) error {
	u, err := url.Parse(targetURL)
	if err != nil || !u.IsAbs() {
		return fmt.Errorf("launch URL must be absolute")
	}
	p.mu.Lock()
	if p.Top == nil || p.Top.Realm == nil {
		p.mu.Unlock()
		return fmt.Errorf("page is closed")
	}
	p.launches = append(p.launches, u.String())
	r := p.Top.Realm
	if r != nil && r.launchNotifier != nil {
		callback := r.launchNotifier
		r.scheduler.Post(scheduler.UserInteraction, 0, func(ctx context.Context) error { _, err := r.runtime.Call(ctx, callback, nil); return err })
	}
	p.mu.Unlock()
	return nil
}
func addLaunchHosts(r *Realm, h map[string]any) {
	h["installLaunchNotifier"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		p := r.agent.Page()
		p.mu.Lock()
		r.launchNotifier = a[0]
		p.mu.Unlock()
		return nil, nil
	})
	h["nextLaunch"] = r.fn(func(_ engine.Value, _ []engine.Value) (engine.Value, error) {
		p := r.agent.Page()
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.Top.Realm != r || len(p.launches) == 0 {
			return nil, nil
		}
		value := p.launches[0]
		p.launches = p.launches[1:]
		return r.val(value), nil
	})
}
