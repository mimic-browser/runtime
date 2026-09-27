package browser

import (
	"context"
	"fmt"

	"github.com/moreveal/mimic/internal/engine"
	"github.com/moreveal/mimic/internal/trace"
)

type IsolatedWorldInfo struct{ FrameID, RealmID, MainRealmID, Name, URL string }

func (p *Page) IsolatedWorlds() []IsolatedWorldInfo {
	var worlds []IsolatedWorldInfo
	for _, realm := range p.evaluationRealms(nil) {
		if realm != nil && realm.mainWorld != nil && !realm.closed && !realm.inactive {
			worlds = append(worlds, IsolatedWorldInfo{realm.agent.ContextID(), realm.ID, realm.mainWorld.ID, realm.worldName, realm.documentURL().String()})
		}
	}
	return worlds
}

// IsolatedWorld creates a real JavaScript realm over the frame's current DOM.
// The document owns its worlds; attaching or detaching a debugger does not
// replace them. Every world participates in the same Page event loop.
func (p *Page) IsolatedWorld(ctx context.Context, frameID, name string) (string, error) {
	frame, ok := p.Frame(frameID)
	if !ok || frame.Realm == nil {
		return "", fmt.Errorf("No frame for given id found")
	}
	world, err := p.isolatedWorld(ctx, frame.Realm, name)
	if err != nil {
		return "", err
	}
	return world.ID, nil
}

func (p *Page) isolatedWorld(ctx context.Context, main *Realm, name string) (*Realm, error) {
	if main.mainWorld != nil {
		main = main.mainWorld
	}
	if world := main.isolatedWorlds[name]; world != nil {
		return world, nil
	}
	if main.closed || main.inactive {
		return nil, fmt.Errorf("Execution context was destroyed")
	}
	world, err := newRealmStateWithNavigation(p, main.agent, main.document, main.url, true, main.performanceOrigin, main.navigationLoaderID)
	if err != nil {
		return nil, err
	}
	world.mainWorld, world.worldName = main, name
	world.origin, world.readyState = main.origin, main.readyState
	world.childFrames = main.childFrames
	world.retainedFrames = main.retainedFrames
	world.documentSecurity = main.documentSecurity
	world.agentClusters = main.agentClusters
	world.documentReferrer, world.referrerPolicy = main.documentReferrer, main.referrerPolicy
	world.navigationURL, world.navigationType = main.navigationURL, main.navigationType
	world.navigationLoadEnd, world.loadCompleted = main.navigationLoadEnd, main.loadCompleted
	if main.isolatedWorlds == nil {
		main.isolatedWorlds = make(map[string]*Realm)
	}
	main.isolatedWorlds[name] = world
	p.trace.Add(trace.Lifecycle, "isolatedWorldCreated", map[string]any{"frameId": main.agent.ContextID(), "realm": world.ID, "mainRealm": main.ID, "worldName": name, "url": main.url.String()})
	return world, nil
}

func (r *Realm) worldForFrame(frame *Frame) (*Realm, error) {
	if frame == nil || frame.Realm == nil {
		return nil, fmt.Errorf("Execution context was destroyed")
	}
	if r.mainWorld == nil {
		return frame.Realm, nil
	}
	if world := frame.Realm.isolatedWorlds[r.worldName]; world != nil {
		return world, nil
	}
	var world *Realm
	create := func(ctx context.Context) error {
		var err error
		world, err = r.agent.Page().isolatedWorld(ctx, frame.Realm, r.worldName)
		return err
	}
	var err error
	if nested, ok := r.runtime.(engine.ReentrantRuntime); ok {
		err = nested.RunNested(context.Background(), create)
	} else {
		err = create(context.Background())
	}
	return world, err
}

func (r *Realm) documentWorlds() []*Realm {
	main := r
	if r.mainWorld != nil {
		main = r.mainWorld
	}
	worlds := make([]*Realm, 0, len(main.isolatedWorlds)+1)
	worlds = append(worlds, main)
	for _, world := range main.isolatedWorlds {
		if !world.closed && !world.inactive {
			worlds = append(worlds, world)
		}
	}
	return worlds
}

// callWorld crosses an engine owner only while the Page event-loop turn is
// held. The destination's jobs checkpoint after the source's script unwinds.
func (r *Realm) callWorld(target *Realm, callback engine.Value, args ...any) error {
	return r.runWorld(target, func(ctx context.Context) error {
		values := make([]engine.Value, len(args))
		for i, arg := range args {
			values[i] = target.val(arg)
			defer releaseDebuggerValue(target, values[i])
		}
		value, err := target.runtime.Call(ctx, callback, nil, values...)
		releaseDebuggerValue(target, value)
		return err
	})
}

func (r *Realm) runWorld(target *Realm, invoke func(context.Context) error) error {
	operation := func(ctx context.Context) error { return target.runOnOwner(ctx, invoke) }
	var err error
	if nested, ok := r.runtime.(engine.ReentrantRuntime); ok && r != target {
		err = nested.RunNested(context.Background(), operation)
	} else {
		err = operation(context.Background())
	}
	if err == nil {
		r.agent.Page().requireCheckpoint(target)
	}
	return err
}

func (r *Realm) installWorldObservationBridge(host map[string]any) {
	host["registerWorldMutationReceiver"] = r.fn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		r.worldMutationReceiver = args[0]
		hasOther := false
		for _, world := range r.documentWorlds() {
			hasOther = hasOther || world != r && world.worldHasObservers
		}
		return nil, r.callWorld(r, r.worldMutationReceiver, "enabled", hasOther)
	})
	host["worldObserverPresence"] = r.fn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		r.worldHasObservers = arg(args, 0) == true
		worlds := r.documentWorlds()
		for _, world := range worlds {
			if world.worldMutationReceiver == nil {
				continue
			}
			hasOther := false
			for _, candidate := range worlds {
				hasOther = hasOther || candidate != world && candidate.worldHasObservers
			}
			if err := r.callWorld(world, world.worldMutationReceiver, "enabled", hasOther); err != nil {
				return nil, err
			}
		}
		return nil, nil
	})
	host["worldMutation"] = r.transientFn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		record := arg(args, 0)
		for _, world := range r.documentWorlds() {
			if world == r || !world.worldHasObservers || world.worldMutationReceiver == nil {
				continue
			}
			if err := r.callWorld(world, world.worldMutationReceiver, "record", record); err != nil {
				return nil, err
			}
		}
		return nil, nil
	})
}
