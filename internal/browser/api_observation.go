package browser

import (
	"context"

	"github.com/moreveal/mimic/internal/trace"
)

// Property observation follows explicit trace/subscriber demand for this Page.
// A capture restart also resets the once-per-property state in each realm, so
// observations made before capture cannot suppress a later requested event.
func (p *Page) setPropertyTraceEnabled(enabled, reset bool) {
	p.mu.RLock()
	realms := make([]*Realm, 0, len(p.realmOwners))
	for _, realm := range p.realmOwners {
		realms = append(realms, realm)
	}
	p.mu.RUnlock()
	for _, realm := range realms {
		if realm.closed {
			continue
		}
		if reset || realm.apiTraceActive != enabled {
			realm.apiSeen = map[string]bool{}
		}
		if !reset && realm.apiTraceActive == enabled {
			continue
		}
		realm.apiTraceActive = enabled
		if realm.apiTraceSet == nil {
			continue
		}
		if runtime, ok := realm.runtime.(interface{ SetPropertyObservationEnabled(bool) error }); ok {
			if err := runtime.SetPropertyObservationEnabled(enabled); err != nil {
				p.trace.Add(trace.Error, "propertyObservationGate", map[string]any{"error": err.Error(), "realm": realm.ID})
				continue
			}
		}
		if _, err := realm.runtime.Call(context.Background(), realm.apiTraceSet, nil, realm.val(enabled)); err != nil {
			p.trace.Add(trace.Error, "propertyObservationState", map[string]any{"error": err.Error(), "realm": realm.ID})
		}
	}
}
