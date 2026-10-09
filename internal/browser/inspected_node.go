package browser

import "context"

// SetInspectedNode retains at most Chrome's five selected canonical wrappers.
// It neither installs author-visible globals nor creates a native inspector.
func (d *Debugger) SetInspectedNode(ctx context.Context, frameID string, id int64) error {
	state, err := d.state(ctx, frameID, "")
	if err != nil {
		return err
	}
	value, err := state.invoke(ctx, "resolveValue", map[string]any{"nodeId": id}, nil)
	if err != nil {
		return err
	}
	d.inspected = append(d.inspected, debuggerInspectedNode{realm: state.realm, value: value})
	if len(d.inspected) > 5 {
		old := d.inspected[0]
		releaseDebuggerValue(old.realm, old.value)
		d.inspected = d.inspected[1:]
	}
	return nil
}
