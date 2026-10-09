package browser

import (
	"context"
	"fmt"
)

// ResolveNodeFromFrame imports through the same canonical foreign-reference
// bridge used by page scripts. The evaluating realm and DOM owner can differ.
func (d *Debugger) ResolveNodeFromFrame(ctx context.Context, sourceFrameID, frameID, realmID string, nodeID int64, group string) (map[string]any, error) {
	if frameID == "" || frameID == sourceFrameID {
		return d.ResolveNode(ctx, sourceFrameID, realmID, nodeID, group)
	}
	frame, active := d.page.Frame(sourceFrameID)
	if !active {
		return nil, fmt.Errorf("Node document is no longer active")
	}
	if _, valid := d.page.InspectorCanonicalNode(frame, nodeID); !valid {
		return nil, fmt.Errorf("Could not find node with given id")
	}
	source, err := d.state(ctx, sourceFrameID, "")
	if err != nil {
		return nil, err
	}
	target, err := d.state(ctx, frameID, realmID)
	if err != nil {
		return nil, err
	}
	value, err := source.invoke(ctx, "resolveValue", map[string]any{"nodeId": nodeID}, nil)
	if err != nil {
		return nil, err
	}
	defer releaseDebuggerValue(source.realm, value)
	encoded, err := source.realm.crossRealmValue(value)
	if err != nil {
		return nil, err
	}
	target.realm.retainRealm(source.realm)
	imported, err := target.realm.importFrameReference(encoded)
	if err != nil {
		return nil, err
	}
	defer releaseDebuggerValue(target.realm, imported)
	result, err := target.json(ctx, "hold", map[string]any{"objectGroup": group}, imported)
	if err != nil {
		return nil, err
	}
	return result["result"].(map[string]any), nil
}
