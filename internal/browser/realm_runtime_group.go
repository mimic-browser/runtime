package browser

import (
	"sync"

	"github.com/moreveal/mimic/internal/engine"
)

// A document tree shares one execution owner, while each world retains its own
// context and mutable globals. The group contains only materialized runtimes:
// creating an isolated world or frame without executing it must stay cheap.
// The small registry lock also covers Page teardown initiated outside its
// command boundary; it is never held while calling into V8.
type realmRuntimeGroup struct {
	mu      sync.Mutex
	members map[*Realm]engine.RealmRuntimeFactory
	// The native owner carries the seed selected by its first materialized
	// context. A later bare context can have a different exposure key without
	// changing the snapshot that this owner is able to restore.
	seedKey [32]byte
}

func (g *realmRuntimeGroup) connected(except *Realm) engine.RealmRuntimeFactory {
	if g == nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for realm, runtime := range g.members {
		if realm != except && runtime != nil {
			return runtime
		}
	}
	return nil
}

func (g *realmRuntimeGroup) ownerSeedKey() [32]byte {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.seedKey
}

func (g *realmRuntimeGroup) add(realm *Realm) {
	if g == nil {
		return
	}
	if runtime, ok := realm.runtime.(engine.RealmRuntimeFactory); ok {
		var key [32]byte
		if realm.bootstrapPlan != nil {
			key = realm.bootstrapPlan.key
		}
		g.mu.Lock()
		defer g.mu.Unlock()
		if g.members == nil {
			g.members = make(map[*Realm]engine.RealmRuntimeFactory)
		}
		if len(g.members) == 0 {
			g.seedKey = key
		}
		g.members[realm] = runtime
	}
}

func (g *realmRuntimeGroup) remove(realm *Realm) {
	if g != nil {
		g.mu.Lock()
		defer g.mu.Unlock()
		delete(g.members, realm)
		if len(g.members) == 0 {
			g.seedKey = [32]byte{}
		}
	}
}
