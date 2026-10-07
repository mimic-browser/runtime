package browser

import (
	"fmt"
	"github.com/moreveal/mimic/internal/engine"
	"github.com/moreveal/mimic/internal/trace"
)

// realmOwners and realmReferences are Page-local. The existing bridge caches
// strong wrappers, so an imported reference pins its owner for the importing
// realm's lifetime. Tracing from active contexts releases unreachable cycles
// when the importer navigates; merely visiting a document never pins it.
func (p *Page) registerRealm(r *Realm) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.realmOwners == nil {
		p.realmOwners = make(map[string]*Realm)
	}
	p.realmOwners[r.ID] = r
}

func (r *Realm) retainRealm(target *Realm) {
	if target == nil || target == r {
		return
	}
	p := r.agent.Page()
	p.mu.Lock()
	defer p.mu.Unlock()
	if r.realmReferences == nil {
		r.realmReferences = make(map[string]struct{})
	}
	r.realmReferences[target.ID] = struct{}{}
}

// WindowProxy references retain a browsing context, not a particular document.
// Its current realm is selected when tracing, so a navigation does not pin the
// replaced document merely because another realm has read contentWindow.
func (r *Realm) retainWindowReference(frameID string) {
	p := r.agent.Page()
	frame := p.frame(frameID)
	if frame == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if r.windowReferences == nil {
		r.windowReferences = make(map[string]*Frame)
	}
	r.windowReferences[frameID] = frame
}

func (r *Realm) retainEncoded(value any) {
	switch value := value.(type) {
	case map[string]any:
		if value["__mimicCrossRealm"] == "window" || value["type"] == "window" {
			if id, ok := value["frame"].(string); ok {
				r.retainWindowReference(id)
			}
		}
		if id, ok := value["realm"].(string); ok {
			p := r.agent.Page()
			p.mu.RLock()
			target := p.realmOwners[id]
			p.mu.RUnlock()
			r.retainRealm(target)
		}
		for _, item := range value {
			r.retainEncoded(item)
		}
	case []any:
		for _, item := range value {
			r.retainEncoded(item)
		}
	case []map[string]any:
		for _, item := range value {
			r.retainEncoded(item)
		}
	}
}

// Window references resolve the current browsing context. Object references
// resolve their immutable owning realm, even after a cross-origin navigation.
func (r *Realm) referenceRealm(frameID, realmID string) (*Realm, error) {
	p := r.agent.Page()
	var target *Realm
	if realmID != "" {
		p.mu.RLock()
		target = p.realmOwners[realmID]
		p.mu.RUnlock()
		if target == nil || frameID != "" && target.agent.ContextID() != frameID {
			return nil, fmt.Errorf("cross-realm object is no longer available")
		}
	} else {
		frame := p.frame(frameID)
		if frame != nil {
			var err error
			target, err = r.worldForFrame(frame)
			if err != nil {
				return nil, err
			}
		}
	}
	if target == nil || target.origin != r.origin {
		return nil, fmt.Errorf("SecurityError: Blocked cross-origin frame access")
	}
	return target, nil
}

// Inactive documents keep their objects, not their runnable browser work.
// Removing a frame preserves retained language promises; navigation destroys
// the old document's execution context. Browser resources stop in either case.
func (r *Realm) deactivate() { r.deactivateContext(false) }

func (r *Realm) deactivateContext(keepPromiseJobs bool) {
	defer r.agent.Page().notifyDebuggerProgress()
	if r.inactive {
		return
	}
	if r.pictureInPicture != nil {
		r.closePictureInPictureWindow(r.pictureInPicture, false)
	}
	r.inactive = true
	r.deactivateMediaSources()
	r.stopMediaLoads()
	r.stopCameraTracks()
	r.closeRTCMedia()
	r.agent.Page().ctx.network.RevokeBlobsForOwner(r.ID)
	r.checkpointClosed = !keepPromiseJobs
	if !keepPromiseJobs {
		if lifecycle, ok := r.runtime.(engine.PromiseJobLifecycleRuntime); ok {
			if err := lifecycle.DeactivatePromiseJobs(); err != nil {
				r.agent.Page().trace.Add(trace.Error, "promiseJobRetirementFailed", map[string]any{"error": err.Error(), "realm": r.ID})
			}
		}
	}
	for _, world := range r.isolatedWorlds {
		world.deactivateContext(keepPromiseJobs)
	}
	r.scheduler.Close()
	r.cancelResources()
	r.closeSpeechProvider()
	if r.documentStream != nil {
		r.documentStream.cancel()
		r.documentStream.parser.Abort()
		r.documentStream = nil
	}
	for _, worker := range r.workers {
		_ = worker.Close()
	}
	r.workers = nil
	for _, frame := range r.childFrames {
		if frame.Realm != nil {
			frame.Realm.deactivateContext(keepPromiseJobs)
		}
	}
	for _, frame := range r.retainedFrames {
		if frame.Realm != nil {
			frame.Realm.deactivateContext(keepPromiseJobs)
		}
	}
}

func (p *Page) retireRealm(r *Realm) {
	if r == nil {
		return
	}
	r.deactivate()
	p.mu.Lock()
	p.retiredRealms = append(p.retiredRealms, r)
	p.mu.Unlock()
}

func (p *Page) collectRealmOwners() {
	// Collection never disposes an isolate while a JS callback is on its stack.
	if p.realmEvaluationDepth != 0 || p.crossRealmDepth != 0 {
		return
	}
	p.mu.Lock()
	seen := make(map[string]bool)
	var visit func(*Realm)
	visit = func(r *Realm) {
		if r == nil || seen[r.ID] {
			return
		}
		seen[r.ID] = true
		for _, world := range r.isolatedWorlds {
			visit(world)
		}
		for id := range r.realmReferences {
			visit(p.realmOwners[id])
		}
		for _, frame := range r.windowReferences {
			visit(frame.Realm)
		}
		for _, f := range r.childFrames {
			visit(f.Realm)
		}
	}
	for _, f := range p.frames {
		visit(f.Realm)
	}
	// The detached-frame lookup is an index, not a reference from JavaScript.
	// Actual exported Window/object references above determine reachability.
	for _, r := range p.realmOwners {
		for id, frame := range r.retainedFrames {
			if frame.Realm == nil || !seen[frame.Realm.ID] {
				delete(r.retainedFrames, id)
			}
		}
	}
	var dispose []*Realm
	for id, r := range p.realmOwners {
		if r.inactive && !seen[id] {
			delete(p.realmOwners, id)
			dispose = append(dispose, r)
		}
	}
	p.retiredRealms = nil
	p.mu.Unlock()
	for _, r := range dispose {
		_ = r.Close()
	}
}
