package browser

import (
	"context"
	"encoding/json"
	"fmt"
)

// ProtocolComputedStyle reads the document owner's existing CSSOM projection.
// It never invokes replaceable author getters or builds a second style model.
// The caller holds the Page command boundary, as for ProtocolBoxModel.
func (p *Page) ProtocolComputedStyle(ctx context.Context, nodeID int64) (map[string]any, error) {
	frame, ok := p.FrameForDOMNode(nodeID)
	if !ok || frame.Realm == nil || frame.Realm.closed || frame.Realm.inactive {
		return nil, fmt.Errorf("Could not find node with given id")
	}
	r := frame.Realm
	var result map[string]any
	err := r.scheduler.RunInline(ctx, func(ctx context.Context) error {
		if deferred, ok := r.runtime.(*deferredRuntime); ok {
			if _, err := deferred.ready(); err != nil {
				return err
			}
		}
		if r.computedStyleFlatRead == nil {
			return fmt.Errorf("computed style owner is unavailable")
		}
		id, kind, property := r.val(nodeID), r.val("protocolComputedStyle"), r.val("")
		defer releaseDebuggerValue(r, id)
		defer releaseDebuggerValue(r, kind)
		defer releaseDebuggerValue(r, property)
		value, err := r.runtime.Call(ctx, r.computedStyleFlatRead, nil, id, kind, property)
		defer releaseDebuggerValue(r, value)
		if err != nil {
			return err
		}
		if value == nil {
			return fmt.Errorf("computed style projection returned no result")
		}
		return json.Unmarshal([]byte(value.String()), &result)
	})
	return result, err
}
