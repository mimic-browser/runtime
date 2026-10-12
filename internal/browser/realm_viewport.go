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
	var width, height int
	var haveSize bool
	err := parent.runOnOwner(context.Background(), func(ctx context.Context) error {
		id := parent.val(frame.elementID)
		defer releaseDebuggerValue(parent, id)
		value, err := parent.runtime.Call(ctx, parent.frameViewportRead, nil, id)
		defer releaseDebuggerValue(parent, value)
		if err == nil {
			// The captured viewport reader returns an internal two-element array.
			// Exporting that array compiles and runs JSON.stringify in the parent
			// realm on every geometry read, adding scripts and author-visible work.
			first := parent.runtime.GetProperty(value, "0")
			second := parent.runtime.GetProperty(value, "1")
			defer releaseDebuggerValue(parent, first)
			defer releaseDebuggerValue(parent, second)
			if first != nil && second != nil {
				width, height = int(numberValue(first.Export())), int(numberValue(second.Export()))
				haveSize = true
			}
		}
		return err
	})
	if err != nil {
		return w, err
	}
	if haveSize {
		w.ViewportWidth, w.ViewportHeight = width, height
	}
	return w, nil
}
