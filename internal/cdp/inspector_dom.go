package cdp

import (
	"context"
	"slices"
	"sort"
	"time"

	"github.com/moreveal/mimic/internal/browser"

	"github.com/moreveal/mimic/internal/dom"
)

// Only nodes requested by this frontend are retained. These copies describe
// what the client has seen, never browser state. Reads and edits use the arena.
type inspectorNode struct {
	frame       *browser.Frame
	document    *dom.Document
	localID     int64
	text        string
	attrs       map[string]string
	children    []int64
	expanded    bool
	shadowRoots []int64
}

type domInspector struct {
	document *dom.Document
	revision uint64
	nodes    map[int64]*inspectorNode
}

func (s *session) inspectorTurn() {
	if s.domInspector != nil {
		s.publishDOMChanges()
	}
	if s.domainEnabled("CSS") {
		s.publishStyleSheets()
		s.publishTrackedStyles()
	}
}

func (s *session) ensureInspectorTurn() {
	if s.removeInspectorTurn == nil {
		_, s.removeInspectorTurn = s.page.SubscribeTurn(s.inspectorTurn)
	}
}

func (s *session) releaseInspectorTurn() {
	if s.domInspector == nil && !s.domainEnabled("CSS") && s.removeInspectorTurn != nil {
		s.removeInspectorTurn()
		s.removeInspectorTurn = nil
	}
	if s.domInspector == nil && !s.domainEnabled("CSS") && !s.domainEnabled("DOM") && s.castState() == nil {
		for _, client := range s.server.clientSnapshot() {
			for _, other := range client.snapshot() {
				if other != s && other.page == s.page && other.ctx.Err() == nil && (other.domInspector != nil || other.domainEnabled("CSS") || other.domainEnabled("DOM") || other.castState() != nil) {
					return
				}
			}
		}
		s.page.ReleaseInspectorNodeIDs()
	}
}

func (s *session) rememberDOM(d *dom.Document, node map[string]any) {
	top, _ := s.page.Document()
	if s.domInspector == nil || s.domInspector.document != top {
		s.domInspector = &domInspector{document: top, nodes: make(map[int64]*inspectorNode)}
		s.ensureInspectorTurn()
	}
	id := int64(coordinateValue(node["nodeId"]))
	frame, localID, ok := s.page.InspectorNodeOwner(id)
	if !ok {
		return
	}
	n, ok := s.page.InspectorCanonicalNode(frame, localID)
	if !ok {
		return
	}
	prior := s.domInspector.nodes[id]
	state := &inspectorNode{text: n.Text, attrs: make(map[string]string, len(n.Attributes)), children: slices.Clone(n.Children)}
	state.frame, state.document, state.localID = frame, d, localID
	for index, child := range state.children {
		state.children[index] = s.page.InspectorNodeID(frame, child)
	}
	for key, value := range n.Attributes {
		state.attrs[key] = value
	}
	if prior != nil {
		state.expanded = prior.expanded
		state.shadowRoots = slices.Clone(prior.shadowRoots)
	}
	if children, ok := node["children"].([]any); ok {
		state.expanded = true
		state.children = nil
		for _, child := range children {
			state.children = append(state.children, int64(coordinateValue(child.(map[string]any)["nodeId"])))
			s.rememberDOM(d, child.(map[string]any))
		}
	}
	if roots, ok := node["shadowRoots"].([]any); ok {
		state.shadowRoots = nil
		for _, root := range roots {
			state.shadowRoots = append(state.shadowRoots, int64(coordinateValue(root.(map[string]any)["nodeId"])))
			s.rememberDOM(d, root.(map[string]any))
		}
	}
	if content, ok := node["contentDocument"].(map[string]any); ok {
		owner, _, valid := s.page.InspectorNodeOwner(int64(coordinateValue(content["nodeId"])))
		if valid {
			if doc, exists := s.page.InspectorDocument(owner); exists {
				s.rememberDOM(doc, content)
			}
		}
	}
	s.domInspector.nodes[id] = state
	s.domInspector.revision = s.page.InspectorRevision()
}

func (s *session) inspectorCDPNode(d *dom.Document, n dom.Node, depth int) map[string]any {
	frame := s.page.Top
	var find func(*browser.Frame)
	find = func(f *browser.Frame) {
		if document, ok := s.page.InspectorDocument(f); ok && document == d {
			frame = f
		}
		for _, child := range f.Children() {
			find(child)
		}
	}
	find(s.page.Top)
	shadows, _ := frame.Realm.ShadowSnapshots(context.Background())
	byHost := make(map[int64]dom.ShadowSnapshot)
	for _, shadow := range shadows {
		byHost[shadow.HostID] = shadow
	}
	var project func(dom.Node, int) map[string]any
	project = func(node dom.Node, depth int) map[string]any {
		out := cdpNode(d, node, 0)
		out["nodeId"], out["backendNodeId"] = s.page.InspectorNodeID(frame, node.ID), s.page.InspectorNodeID(frame, node.ID)
		if node.Type == "document" {
			out["documentURL"], out["baseURL"], out["xmlVersion"] = frame.URL(), frame.URL(), ""
		}
		if depth != 0 {
			children := []any{}
			for _, id := range node.Children {
				if child, ok := d.Get(id); ok {
					children = append(children, project(child, depth-1))
				}
			}
			out["children"] = children
		}
		if shadow, ok := byHost[node.ID]; ok && shadow.RootID != 0 {
			if root, exists := s.page.InspectorCanonicalNode(frame, shadow.RootID); exists {
				wire := project(root, 0)
				wire["nodeName"] = "#document-fragment"
				wire["nodeType"] = 11
				wire["shadowRootType"] = shadow.Mode
				wire["childNodeCount"] = len(shadow.Children)
				if depth != 0 {
					children := []any{}
					for _, id := range shadow.Children {
						if child, exists := d.Get(id); exists {
							children = append(children, project(child, depth-1))
						}
					}
					wire["children"] = children
				}
				out["shadowRoots"] = []any{wire}
			}
		}
		for _, child := range frame.Children() {
			if child.ElementNodeID() == node.ID {
				out["frameId"] = child.ID
				if childDoc, exists := s.page.InspectorDocument(child); exists {
					out["contentDocument"] = s.inspectorCDPNode(childDoc, childDoc.Root(), depth)
				}
			}
		}
		return out
	}
	out := project(n, depth)
	s.rememberDOM(d, out)
	return out
}

func (s *session) publishDOMChanges() {
	i := s.domInspector
	d, ok := s.page.Document()
	if !ok || d != i.document {
		s.domInspector = nil
		s.domSearches = nil
		s.publishDocumentUpdated(d)
		s.releaseInspectorTurn()
		return
	}
	if revision := s.page.InspectorRevision(); revision == i.revision {
		return
	} else {
		i.revision = revision
	}
	s.page.PruneInspectorNodeIDs()
	for _, node := range i.nodes {
		if current, ok := s.page.InspectorDocument(node.frame); !ok || current != node.document {
			s.domInspector = nil
			s.event("DOM.documentUpdated", map[string]any{})
			s.releaseInspectorTurn()
			return
		}
	}
	ids := make([]int64, 0, len(i.nodes))
	for id := range i.nodes {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	shadowsByFrame := map[*browser.Frame]map[int64]dom.ShadowSnapshot{}
	for _, id := range ids {
		old := i.nodes[id]
		if old == nil {
			continue
		}
		byHost, read := shadowsByFrame[old.frame]
		if !read {
			shadows, err := old.frame.Realm.ShadowSnapshots(s.ctx)
			if err != nil {
				s.event("Log.entryAdded", map[string]any{"entry": map[string]any{"source": "other", "level": "error", "text": err.Error(), "timestamp": float64(time.Now().UnixMilli())}})
				continue
			}
			byHost = map[int64]dom.ShadowSnapshot{}
			for _, shadow := range shadows {
				byHost[shadow.HostID] = shadow
			}
			shadowsByFrame[old.frame] = byHost
		}
		if shadow, ok := byHost[old.localID]; ok {
			wireID := s.page.InspectorNodeID(old.frame, shadow.RootID)
			if !slices.Contains(old.shadowRoots, wireID) {
				root, exists := s.page.InspectorCanonicalNode(old.frame, shadow.RootID)
				if exists {
					wire := s.inspectorCDPNode(old.document, root, 0)
					wire["shadowRootType"] = shadow.Mode
					old.shadowRoots = append(old.shadowRoots, wireID)
					s.event("DOM.shadowRootPushed", map[string]any{"hostId": id, "root": wire})
				}
			}
		}
	}
	changes := make(map[int64]inspectorChildChanges)
	// Remove first, then insert: a moved node must never exist under two parents
	// in the frontend. Removed subtrees lose their frontend bindings recursively.
	for _, id := range ids {
		old := i.nodes[id]
		if old == nil || !old.expanded {
			continue
		}
		n, ok := s.currentInspectorNode(old)
		if !ok {
			continue
		}
		change := inspectorChildrenChanges(old.children, n.Children)
		changes[id] = change
		for _, child := range old.children {
			if change.removed[child] {
				s.event("DOM.childNodeRemoved", map[string]any{"parentNodeId": id, "nodeId": child})
				s.forgetDOM(child)
			}
		}
	}
	for _, id := range ids {
		old := i.nodes[id]
		if old == nil {
			continue
		}
		n, ok := s.currentInspectorNode(old)
		if !ok {
			delete(i.nodes, id)
			continue
		}
		keys := make([]string, 0, len(old.attrs)+len(n.Attributes))
		for key := range old.attrs {
			keys = append(keys, key)
		}
		for key := range n.Attributes {
			if _, ok := old.attrs[key]; !ok {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		for _, key := range keys {
			value, present := n.Attributes[key]
			previous, existed := old.attrs[key]
			if !present {
				s.event("DOM.attributeRemoved", map[string]any{"nodeId": id, "name": key})
			} else if !existed || value != previous {
				s.event("DOM.attributeModified", map[string]any{"nodeId": id, "name": key, "value": value})
			}
		}
		if n.Text != old.text {
			s.event("DOM.characterDataModified", map[string]any{"nodeId": id, "characterData": n.Text})
		}
		if old.expanded {
			var previous int64
			for _, child := range n.Children {
				if changes[id].inserted[child] {
					_, local, valid := s.page.InspectorNodeOwner(child)
					if node, ok := old.document.Get(local); valid && ok {
						s.event("DOM.childNodeInserted", map[string]any{"parentNodeId": id, "previousNodeId": previous, "node": s.inspectorCDPNode(old.document, node, 0)})
					}
				}
				previous = child
			}
		} else if len(n.Children) != len(old.children) {
			s.event("DOM.childNodeCountUpdated", map[string]any{"nodeId": id, "childNodeCount": len(n.Children)})
		}
		wire := cdpNode(old.document, n, 0)
		wire["nodeId"] = id
		s.rememberDOM(old.document, wire)
	}
}

// Lifecycle and turn notifications project the same canonical replacement.
// Parsing completion must not invalidate a root already invalidated on commit.
func (s *session) publishDocumentUpdated(document *dom.Document) {
	s.stateMu.Lock()
	changed := s.documentUpdatedDocument != document
	s.documentUpdatedDocument = document
	s.stateMu.Unlock()
	if changed {
		s.event("DOM.documentUpdated", map[string]any{})
	}
}

func (s *session) currentInspectorNode(old *inspectorNode) (dom.Node, bool) {
	n, ok := s.page.InspectorCanonicalNode(old.frame, old.localID)
	n.Children = slices.Clone(n.Children)
	if n.Type == "fragment" {
		shadows, err := old.frame.Realm.ShadowSnapshots(s.ctx)
		if err == nil {
			for _, shadow := range shadows {
				if shadow.RootID == old.localID {
					n.Children = slices.Clone(shadow.Children)
				}
			}
		}
	}
	for index, child := range n.Children {
		n.Children[index] = s.page.InspectorNodeID(old.frame, child)
	}
	return n, ok
}

type inspectorChildChanges struct {
	removed, inserted map[int64]bool
}

// Compare surviving sibling order once per parent, not once per child.
// Unchanged lists take a linear read with no allocation. Changed lists retain
// the existing coalesced move semantics while avoiding cubic sibling scans.
func inspectorChildrenChanges(before, after []int64) inspectorChildChanges {
	if slices.Equal(before, after) {
		return inspectorChildChanges{}
	}
	oldRanks := make(map[int64]int, len(before))
	newIDs := make(map[int64]bool, len(after))
	for _, id := range after {
		newIDs[id] = true
	}
	change := inspectorChildChanges{removed: make(map[int64]bool), inserted: make(map[int64]bool)}
	rank := 0
	for _, id := range before {
		if newIDs[id] {
			oldRanks[id] = rank
			rank++
		} else {
			change.removed[id] = true
		}
	}
	rank = 0
	for _, id := range after {
		oldRank, survived := oldRanks[id]
		if !survived {
			change.inserted[id] = true
			continue
		}
		if oldRank != rank {
			change.removed[id], change.inserted[id] = true, true
		}
		rank++
	}
	return change
}

func (s *session) forgetDOM(id int64) {
	if old := s.domInspector.nodes[id]; old != nil {
		delete(s.domInspector.nodes, id)
		for _, child := range old.children {
			s.forgetDOM(child)
		}
		for _, root := range old.shadowRoots {
			s.forgetDOM(root)
		}
	}
}
