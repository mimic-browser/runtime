package cdp

import (
	"context"
	"fmt"

	"github.com/moreveal/mimic/internal/browser"
	"github.com/moreveal/mimic/internal/dom"
)

func coordinateValue(value any) float64 {
	switch value := value.(type) {
	case float64:
		return value
	case float32:
		return float64(value)
	case int:
		return float64(value)
	case int64:
		return float64(value)
	default:
		return 0
	}
}

func (s *session) nodeOwner(ctx context.Context, p map[string]any) (*browser.Frame, int64, error) {
	if object := stringValue(p["objectId"]); object != "" {
		frameID, id, err := s.runtimeDebugger().NodeObjectOwner(ctx, object)
		if err != nil {
			return nil, 0, err
		}
		frame, ok := s.page.Frame(frameID)
		if !ok {
			return nil, 0, fmt.Errorf("Node document is no longer active")
		}
		return frame, id, nil
	}
	id := int64(intValue(p["nodeId"], intValue(p["backendNodeId"], 0)))
	frame, id, ok := s.page.InspectorNodeOwner(id)
	if !ok {
		return nil, 0, fmt.Errorf("Could not find node with given id")
	}
	_, ok = s.page.InspectorDocument(frame)
	if !ok {
		return nil, 0, fmt.Errorf("No document")
	}
	if _, ok := s.page.InspectorCanonicalNode(frame, id); !ok {
		return nil, 0, fmt.Errorf("Could not find node with given id")
	}
	return frame, id, nil
}

func (s *session) nodeID(ctx context.Context, p map[string]any) (int64, error) {
	_, id, err := s.nodeOwner(ctx, p)
	return id, err
}

func (s *session) nodeFunction(ctx context.Context, p map[string]any, fn string, args []any) (any, error) {
	frame, id, err := s.nodeOwner(ctx, p)
	if err != nil {
		return nil, err
	}
	ok := frame != nil
	objectID := stringValue(p["objectId"])
	if objectID != "" {
		frameID, ownerErr := s.runtimeDebugger().ObjectFrameID(objectID)
		if ownerErr != nil {
			return nil, ownerErr
		}
		frame, ok = s.page.Frame(frameID)
	}
	if !ok {
		return nil, fmt.Errorf("Node does not belong to the document")
	}
	d := s.runtimeDebugger()
	if objectID == "" {
		object, err := d.ResolveNode(ctx, frame.ID, "", id, "")
		if err != nil {
			return nil, err
		}
		objectID = stringValue(object["objectId"])
		defer d.ReleaseObject(ctx, objectID)
	}
	result, err := d.CallFunction(ctx, frame.ID, "", fn, map[string]any{"objectId": objectID, "arguments": args}, browser.DebuggerOptions{ReturnByValue: true})
	if err != nil {
		return nil, err
	}
	if ex := result["exceptionDetails"]; ex != nil {
		return nil, fmt.Errorf("DOM operation failed: %v", ex)
	}
	value, _ := result["result"].(map[string]any)
	return value["value"], nil
}

func (s *session) frameElementOffset(ctx context.Context, frame *browser.Frame) (float64, float64, error) {
	parent := frame.Parent()
	if parent == nil {
		return 0, 0, nil
	}
	d := s.runtimeDebugger()
	object, err := d.ResolveNode(ctx, parent.ID, "", frame.ElementNodeID(), "")
	if err != nil {
		return 0, 0, err
	}
	objectID := stringValue(object["objectId"])
	defer d.ReleaseObject(ctx, objectID)
	result, err := d.CallFunction(ctx, parent.ID, "", `function(){const r=this.getBoundingClientRect(),s=getComputedStyle(this),n=k=>parseFloat(s[k])||0;return {x:r.x+n('borderLeftWidth')+n('paddingLeft'),y:r.y+n('borderTopWidth')+n('paddingTop')}}`, map[string]any{"objectId": objectID}, browser.DebuggerOptions{ReturnByValue: true})
	if err != nil {
		return 0, 0, err
	}
	if ex := result["exceptionDetails"]; ex != nil {
		return 0, 0, fmt.Errorf("iframe geometry failed: %v", ex)
	}
	remote, _ := result["result"].(map[string]any)
	offset, _ := remote["value"].(map[string]any)
	return coordinateValue(offset["x"]), coordinateValue(offset["y"]), nil
}

func (s *session) describeNode(id int64, depth int) (map[string]any, error) {
	frame, local, ok := s.page.InspectorNodeOwner(id)
	if !ok {
		return nil, fmt.Errorf("Could not find node with given id")
	}
	d, ok := s.page.InspectorDocument(frame)
	if !ok {
		return nil, fmt.Errorf("No document")
	}
	n, ok := s.page.InspectorCanonicalNode(frame, local)
	if !ok {
		return nil, fmt.Errorf("Could not find node with given id")
	}
	result := s.inspectorCDPNode(d, n, depth)
	return result, nil
}

func (s *session) handleDOM(ctx context.Context, method string, p map[string]any) (any, bool, error) {
	if value, handled, err := s.handleDOMSearch(ctx, method, p); handled {
		return value, true, err
	}
	empty := map[string]any{}
	switch method {
	case "DOM.pushNodesByBackendIdsToFrontend":
		ids := []any{}
		for _, value := range p["backendNodeIds"].([]any) {
			id := int64(coordinateValue(value))
			owner, local, err := s.nodeOwner(ctx, map[string]any{"backendNodeId": id})
			if err == nil {
				d, _ := s.page.InspectorDocument(owner)
				s.emitDOMAncestors(d, local)
				ids = append(ids, id)
			} else {
				ids = append(ids, 0)
			}
		}
		return map[string]any{"nodeIds": ids}, true, nil
	case "DOM.setAttributesAsText":
		_, err := s.nodeFunction(ctx, p, `function(text,name){const template=this.ownerDocument.createElement('template');template.innerHTML='<span '+text+'></span>';const parsed=template.content.firstChild;if(!parsed)throw new Error('Invalid attributes');if(name)this.removeAttribute(name);for(const attribute of parsed.attributes)this.setAttribute(attribute.name,attribute.value)}`, []any{map[string]any{"value": p["text"]}, map[string]any{"value": p["name"]}})
		return empty, true, err
	case "DOM.setInspectedNode":
		frame, id, err := s.nodeOwner(ctx, p)
		if err != nil {
			return nil, true, err
		}
		return empty, true, s.runtimeDebugger().SetInspectedNode(ctx, frame.ID, id)
	case "DOM.getFrameOwner":
		frame, ok := s.page.Frame(stringValue(p["frameId"]))
		if !ok || frame.Parent() == nil || frame.ElementNodeID() == 0 {
			return nil, true, fmt.Errorf("Frame does not have an owner")
		}
		return map[string]any{"backendNodeId": s.page.InspectorNodeID(frame.Parent(), frame.ElementNodeID())}, true, nil
	case "DOM.getOuterHTML":
		frame, id, err := s.nodeOwner(ctx, p)
		if err != nil {
			return nil, true, err
		}
		d, _ := s.page.InspectorDocument(frame)
		var markup string
		if id >= 1<<31 {
			root, _ := s.page.InspectorCanonicalNode(frame, id)
			for _, child := range root.Children {
				text, childErr := d.OuterHTML(child)
				if childErr != nil {
					return nil, true, childErr
				}
				markup += text
			}
		} else {
			markup, err = d.OuterHTML(id)
		}
		return map[string]any{"outerHTML": markup}, true, err
	case "DOM.resolveNode":
		owner, id, err := s.nodeOwner(ctx, p)
		if err != nil {
			return nil, true, err
		}
		frameID, realmID, err := s.runtimeContext(int64(intValue(p["executionContextId"], 0)))
		if err != nil {
			return nil, true, err
		}
		if frameID == "" {
			frameID = owner.ID
		}
		object, err := s.runtimeDebugger().ResolveNodeFromFrame(ctx, owner.ID, frameID, realmID, id, stringValue(p["objectGroup"]))
		return map[string]any{"object": object}, true, err
	case "DOM.requestNode":
		frameID, id, err := s.runtimeDebugger().NodeObjectOwner(ctx, stringValue(p["objectId"]))
		if err == nil && id != 0 {
			owner, active := s.page.Frame(frameID)
			if !active {
				return nil, true, fmt.Errorf("Node document is no longer active")
			}
			if d, ok := s.page.InspectorDocument(owner); ok {
				s.emitDOMAncestors(d, id)
			}
			id = s.page.InspectorNodeID(owner, id)
		}
		return map[string]any{"nodeId": id}, true, err
	case "DOM.describeNode":
		owner, local, err := s.nodeOwner(ctx, p)
		if err != nil {
			return nil, true, err
		}
		node, err := s.describeNode(s.page.InspectorNodeID(owner, local), intValue(p["depth"], 0))
		return map[string]any{"node": node}, true, err
	case "DOM.getAttributes":
		owner, local, err := s.nodeOwner(ctx, p)
		if err != nil {
			return nil, true, err
		}
		node, err := s.describeNode(s.page.InspectorNodeID(owner, local), 0)
		if err != nil {
			return nil, true, err
		}
		attrs := node["attributes"]
		if attrs == nil {
			return nil, true, fmt.Errorf("Node is not an Element")
		}
		return map[string]any{"attributes": attrs}, true, nil
	case "DOM.requestChildNodes":
		owner, id, err := s.nodeOwner(ctx, p)
		if err != nil {
			return nil, true, err
		}
		d, _ := s.page.InspectorDocument(owner)
		children := []any{}
		depth := intValue(p["depth"], 1)
		if depth == 0 {
			return nil, true, fmt.Errorf("Depth should be a positive number or -1")
		}
		parent, _ := s.page.InspectorCanonicalNode(owner, id)
		for _, childID := range parent.Children {
			if child, ok := d.Get(childID); ok {
				children = append(children, s.inspectorCDPNode(d, child, depth-1))
			}
		}
		if s.domInspector != nil {
			if node := s.domInspector.nodes[s.page.InspectorNodeID(owner, id)]; node != nil {
				node.expanded = true
			}
		}
		s.event("DOM.setChildNodes", map[string]any{"parentId": s.page.InspectorNodeID(owner, id), "nodes": children})
		return empty, true, nil
	case "DOM.focus":
		_, err := s.nodeFunction(ctx, p, `function(){if(!this.isConnected||typeof this.focus!=='function')throw new Error('Element is not focusable');this.focus()}`, nil)
		return empty, true, err
	case "DOM.scrollIntoViewIfNeeded":
		owner, id, err := s.nodeOwner(ctx, p)
		if err == nil {
			err = s.page.ScrollNodeIntoViewInFrame(ctx, owner.ID, id, p["rect"])
		}
		return empty, true, err
	case "DOM.getContentQuads", "DOM.getBoxModel":
		frame, id, idErr := s.nodeOwner(ctx, p)
		if idErr != nil {
			return nil, true, idErr
		}
		model, err := s.page.ProtocolBoxModel(ctx, frame, id)
		if err != nil {
			return nil, true, err
		}
		for frame != nil && frame.Parent() != nil {
			dx, dy, offsetErr := s.frameElementOffset(ctx, frame)
			if offsetErr != nil {
				return nil, true, offsetErr
			}
			for _, name := range []string{"border", "padding", "content", "margin"} {
				quad, _ := model[name].([]any)
				for index := range quad {
					coordinate := coordinateValue(quad[index])
					if index%2 == 0 {
						quad[index] = coordinate + dx
					} else {
						quad[index] = coordinate + dy
					}
				}
			}
			frame = frame.Parent()
		}
		if method == "DOM.getContentQuads" {
			// Despite its name, Chrome returns the element's border quad,
			// not DOM.getBoxModel's content box. Padding remains clickable
			// even when the content width or height is zero.
			quad, _ := model["border"].([]any)
			return map[string]any{"quads": []any{quad}}, true, nil
		}
		return map[string]any{"model": model}, true, nil
	case "DOM.setAttributeValue", "DOM.removeAttribute", "DOM.setNodeValue", "DOM.setOuterHTML", "DOM.removeNode":
		fn := ""
		args := []any{}
		arg := func(v any) { args = append(args, map[string]any{"value": v}) }
		switch method {
		case "DOM.setAttributeValue":
			fn = `function(n,v){this.setAttribute(n,v)}`
			arg(p["name"])
			arg(p["value"])
		case "DOM.removeAttribute":
			fn = `function(n){this.removeAttribute(n)}`
			arg(p["name"])
		case "DOM.setNodeValue":
			fn = `function(v){if(![3,4,8].includes(this.nodeType))throw new Error('Node is not a character data node');this.nodeValue=v}`
			arg(p["value"])
		case "DOM.setOuterHTML":
			fn = `function(v){this.outerHTML=v}`
			arg(p["outerHTML"])
		case "DOM.removeNode":
			fn = `function(){if(!this.parentNode)throw new Error('Node has no parent');this.parentNode.removeChild(this)}`
		}
		_, err := s.nodeFunction(ctx, p, fn, args)
		return empty, true, err
	}
	return nil, false, nil
}

func (s *session) emitDOMAncestors(d *dom.Document, id int64) {
	n, ok := d.Get(id)
	if !ok || n.Parent == 0 {
		return
	}
	s.emitDOMAncestors(d, n.Parent)
	owner := s.page.Top
	var visit func(*browser.Frame)
	visit = func(frame *browser.Frame) {
		if doc, ok := s.page.InspectorDocument(frame); ok && doc == d {
			owner = frame
		}
		for _, child := range frame.Children() {
			visit(child)
		}
	}
	visit(s.page.Top)
	parentID := s.page.InspectorNodeID(owner, n.Parent)
	// Replacing an already published child list makes the frontend recreate
	// DOMNode objects and invalidates selected-node/cascade identity.
	if s.domInspector != nil {
		if node := s.domInspector.nodes[parentID]; node != nil && node.expanded {
			return
		}
	}
	children := []any{}
	for _, child := range d.Children(n.Parent) {
		children = append(children, s.inspectorCDPNode(d, child, 0))
	}
	if s.domInspector != nil {
		if node := s.domInspector.nodes[parentID]; node != nil {
			node.expanded = true
		}
	}
	s.event("DOM.setChildNodes", map[string]any{"parentId": parentID, "nodes": children})
}
