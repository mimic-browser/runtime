package browser

import (
	"context"
	"fmt"
	"strings"

	"github.com/moreveal/mimic/internal/engine"
)

// Inspection reads private wrapper metadata and canonical DOM state. Author
// getters, prototype traps and lazy stacks are never a source of CDP metadata.
func (r *Realm) debuggerInspect(_ engine.Value, args []engine.Value) (engine.Value, error) {
	operation := strarg(args, 1)
	if operation == "descriptorRuntime" {
		_, supported := r.runtime.(engine.DebuggerDescriptorRuntime)
		return r.val(supported), nil
	}
	if operation == "descriptor" {
		if descriptors, ok := r.runtime.(engine.DebuggerDescriptorRuntime); ok {
			return descriptors.DebuggerOwnPropertyDescriptor(args[0], args[2])
		}
		return nil, fmt.Errorf("Passive descriptor transport is unsupported by this engine")
	}
	owner, value := r, args[0]
	foreign := false
	if r.frameReferenceDescribe != nil {
		info, err := r.runtime.Call(context.Background(), r.frameReferenceDescribe, nil, value)
		if err != nil {
			return nil, err
		}
		defer releaseDebuggerValue(r, info)
		if reference, ok := info.Export().(map[string]any); ok && reference["frame"] != nil {
			foreign = true
			owner, err = r.referenceRealm(fmt.Sprint(reference["frame"]), fmt.Sprint(reference["realm"]))
			if err != nil {
				return nil, err
			}
			value = owner.crossValues[int64(numberValue(reference["handle"]))]
			if value == nil {
				return nil, fmt.Errorf("Object reference is no longer available")
			}
		}
	}
	if operation == "kind" || operation == "opaque" {
		// Foreign wrappers forward reflection to the author realm. Keep them opaque
		// until that reflection can be performed there without invoking author code.
		if foreign {
			if operation == "opaque" {
				return r.val(true), nil
			}
			return r.val("opaque"), nil
		}
		detector, ok := owner.runtime.(engine.DebuggerValueRuntime)
		if !ok {
			return nil, fmt.Errorf("Passive object inspection is unsupported by this engine")
		}
		if operation == "opaque" {
			proxy, err := detector.IsDebuggerProxy(value)
			if err != nil {
				return nil, err
			}
			return r.val(proxy), nil
		}
		kind, err := detector.DebuggerValueKind(value)
		if err != nil {
			return nil, err
		}
		return r.val(kind), nil
	}
	idValue, err := owner.runtime.Call(context.Background(), owner.frameNodeDescribe, nil, value)
	if err != nil {
		return nil, err
	}
	defer releaseDebuggerValue(owner, idValue)
	id := int64(numberValue(idValue.Export()))
	if id >= 1<<31 {
		return r.val("#document-fragment"), nil
	}
	node, ok := owner.document.Get(id)
	if !ok {
		return r.val("#node"), nil
	}
	description := ""
	switch node.Type {
	case "document":
		description = "#document"
	case "text":
		description = "#text"
	case "comment":
		description = "<!--" + node.Text + "-->"
	case "fragment":
		description = "#document-fragment"
	default:
		description = strings.ToLower(node.TagName)
		if id := node.Attributes["id"]; id != "" {
			description += "#" + id
		}
		for _, class := range strings.Fields(node.Attributes["class"]) {
			description += "." + class
		}
	}
	return r.val(description), nil
}
