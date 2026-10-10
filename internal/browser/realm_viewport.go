package browser

import (
	"context"

	"github.com/moreveal/mimic/internal/state"
)

// observationWindow resolves the viewport through the canonical parent realm.
// Child documents must never use the top-level window's layout dimensions.
func (r *Realm) observationWindow() (state.Window, error) {
	w := r.agent.Page().environmentView().Window
	frame, ok := r.agent.(*Frame)
	if !ok {
		return w, nil
	}
	if frame.auxiliaryOpener != nil {
		w.ViewportWidth, w.ViewportHeight = frame.auxiliaryWidth, frame.auxiliaryHeight
		w.OuterWidth, w.OuterHeight = frame.auxiliaryWidth, frame.auxiliaryHeight
	}
	if frame.parent == nil || frame.parent.Realm == nil {
		return w, nil
	}
	parent := frame.parent.Realm
	if parent.frameViewportRead == nil {
		return w, nil
	}
	var size []any
	err := parent.runOnOwner(context.Background(), func(ctx context.Context) error {
		id := parent.val(frame.elementID)
		defer releaseDebuggerValue(parent, id)
		value, err := parent.runtime.Call(ctx, parent.frameViewportRead, nil, id)
		defer releaseDebuggerValue(parent, value)
		if err == nil {
			size, _ = value.Export().([]any)
		}
		return err
	})
	if err != nil {
		return w, err
	}
	if len(size) == 2 {
		w.ViewportWidth, w.ViewportHeight = int(numberValue(size[0])), int(numberValue(size[1]))
	}
	return w, nil
}
