package browser

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/fnv"

	"github.com/moreveal/mimic/internal/engine"
)

// InspectorRevision reads canonical epochs without entering JavaScript.
func (p *Page) InspectorRevision() uint64 {
	h := fnv.New64a()
	var buf [8]byte
	add := func(v uint64) { binary.LittleEndian.PutUint64(buf[:], v); _, _ = h.Write(buf[:]) }
	var visit func(*Frame)
	visit = func(frame *Frame) {
		if r := frame.Realm; r != nil && !r.closed && !r.inactive {
			_, _ = h.Write([]byte(r.ID))
			add(r.document.ObservationRevision())
			add(r.resourceRevision.Load())
		}
		for _, child := range frame.Children() {
			visit(child)
		}
	}
	visit(p.Top)
	add(p.inspectorViewEpoch)
	add(uint64(p.env.Window.ViewportWidth))
	add(uint64(p.env.Window.ViewportHeight))
	return h.Sum64()
}

// ProtocolComputedStyle reads the document owner's existing CSSOM projection.
// It never invokes replaceable author getters or builds a second style model.
// The caller holds the Page command boundary, as for ProtocolBoxModel.
func (p *Page) ProtocolComputedStyle(ctx context.Context, nodeID int64) (map[string]any, error) {
	frame, ok := p.FrameForDOMNode(nodeID)
	if !ok {
		return nil, fmt.Errorf("Could not find node with given id")
	}
	return p.ProtocolComputedStyleInFrame(ctx, frame, nodeID)
}

func (p *Page) ProtocolComputedStyleInFrame(ctx context.Context, frame *Frame, nodeID int64) (map[string]any, error) {
	ok := frame != nil
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

// ProtocolInspectorCSS accesses the retained canonical CSSOM callback. Inspector
// handles are allocated by that callback on the first request, not at startup.
func (p *Page) ProtocolInspectorCSS(ctx context.Context, frame *Frame, request map[string]any) (map[string]any, error) {
	if frame == nil || frame.Realm == nil || frame.Realm.closed || frame.Realm.inactive {
		return nil, fmt.Errorf("Stylesheet document is no longer available")
	}
	r := frame.Realm
	var result map[string]any
	err := r.scheduler.RunInline(ctx, func(ctx context.Context) error {
		if deferred, ok := r.runtime.(*deferredRuntime); ok {
			if _, err := deferred.ready(); err != nil {
				return err
			}
		}
		payload, err := json.Marshal(request)
		if err != nil {
			return err
		}
		args := []engine.Value{r.val(0), r.val("inspectorCSS"), r.val(string(payload))}
		defer func() {
			for _, value := range args {
				releaseDebuggerValue(r, value)
			}
		}()
		value, err := r.runtime.Call(ctx, r.computedStyleFlatRead, nil, args...)
		defer releaseDebuggerValue(r, value)
		if err != nil {
			return err
		}
		return json.Unmarshal([]byte(value.String()), &result)
	})
	return result, err
}
