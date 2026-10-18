package cdp

import (
	"context"
	"slices"
	"sort"
	"strings"
	"sync"
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
	childCount  int
	expanded    bool
	shadowRoots []int64
}

type domInspector struct {
	document               *dom.Document
	revision               uint64
	nodes                  map[int64]*inspectorNode
	characterMu            sync.Mutex
	characterChanges       map[inspectorParent]*inspectorCharacterChanges
	characterParents       map[inspectorParent]bool
	characterOwners        map[inspectorParent]int64
	characterSubscriptions map[*dom.Document]func()
}

type inspectorParent struct {
	frame   *browser.Frame
	localID int64
}

// Only counts and touched node IDs are retained, never author strings or an
// unbounded record per write. Repeated writes to one node use constant space.
type inspectorCharacterChanges struct {
	// A zero entry still records a hidden text write, so a visibility change
	// does not masquerade as a structural child removal.
	touched map[int64]int
}

func (s *session) clearDOMInspector() {
	if i := s.domInspector; i != nil {
		for _, remove := range i.characterSubscriptions {
			remove()
		}
		i.characterMu.Lock()
		i.characterChanges = nil
		i.characterParents = nil
		i.characterOwners = nil
		i.characterMu.Unlock()
	}
	s.domInspector = nil
}

func (s *session) observeInspectorCharacterData(d *dom.Document, frame *browser.Frame) {
	i := s.domInspector
	if i.characterSubscriptions == nil {
		i.characterSubscriptions = make(map[*dom.Document]func())
	}
	if i.characterSubscriptions[d] != nil {
		return
	}
	includeWhitespace := s.domIncludeWhitespace
	i.characterSubscriptions[d] = d.SubscribeCharacterData(func(change dom.CharacterDataChange) {
		i.characterMu.Lock()
		defer i.characterMu.Unlock()
		// Synthetic ShadowRoot handles are not arena parents. Use the
		// inspected attachment's child ownership without entering the realm.
		if parent := i.characterOwners[inspectorParent{frame, change.NodeID}]; parent != 0 {
			change.ParentID = parent
		}
		key := inspectorParent{frame, change.ParentID}
		if !i.characterParents[key] {
			return
		}
		if i.characterChanges == nil {
			i.characterChanges = make(map[inspectorParent]*inspectorCharacterChanges)
		}
		pending := i.characterChanges[key]
		if pending == nil {
			pending = &inspectorCharacterChanges{touched: make(map[int64]int)}
			i.characterChanges[key] = pending
		}
		writes := pending.touched[change.NodeID]
		if inspectorNodeVisible(change.Type, change.Text, includeWhitespace) {
			writes++
		}
		pending.touched[change.NodeID] = writes
	})
}

// Blink's inspector omits text containing only ASCII whitespace (including
// vertical tab), independently of CSS white-space and without changing the DOM.
func (s *session) inspectorVisible(n dom.Node) bool {
	return inspectorNodeVisible(n.Type, n.Text, s.domIncludeWhitespace)
}

func inspectorNodeVisible(nodeType, text string, includeWhitespace bool) bool {
	return includeWhitespace || nodeType != "text" || strings.Trim(text, " \t\n\r\v\f") != ""
}

func (s *session) inspectorChildren(frame *browser.Frame, children []int64) []int64 {
	visible := make([]int64, 0, len(children))
	for _, id := range children {
		if node, ok := s.page.InspectorCanonicalNode(frame, id); ok && s.inspectorVisible(node) {
			visible = append(visible, id)
		}
	}
	return visible
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
	if s.domInspector == nil && !s.domainEnabled("CSS") && !s.domainEnabled("DOM") {
		for _, client := range s.server.clientSnapshot() {
			for _, other := range client.snapshot() {
				if other != s && other.page == s.page && other.ctx.Err() == nil && (other.domInspector != nil || other.domainEnabled("CSS") || other.domainEnabled("DOM")) {
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
		s.clearDOMInspector()
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
	s.observeInspectorCharacterData(d, frame)
	prior := s.domInspector.nodes[id]
	state := &inspectorNode{text: n.Text, attrs: make(map[string]string, len(n.Attributes)), children: s.inspectorChildren(frame, n.Children)}
	state.frame, state.document, state.localID = frame, d, localID
	state.childCount = int(coordinateValue(node["childNodeCount"]))
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
	i := s.domInspector
	i.characterMu.Lock()
	key := inspectorParent{frame, localID}
	if localID >= 1<<31 {
		for child, parent := range i.characterOwners {
			if child.frame == frame && parent == localID {
				delete(i.characterOwners, child)
			}
		}
	}
	if !state.expanded {
		if i.characterParents == nil {
			i.characterParents = make(map[inspectorParent]bool)
		}
		i.characterParents[key] = true
		if n.Type == "fragment" && localID >= 1<<31 {
			if i.characterOwners == nil {
				i.characterOwners = make(map[inspectorParent]int64)
			}
			for _, child := range n.Children {
				i.characterOwners[inspectorParent{frame, child}] = localID
			}
		}
	} else {
		delete(i.characterParents, key)
	}
	i.characterMu.Unlock()
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
		visibleChildren := s.inspectorChildren(frame, node.Children)
		out["childNodeCount"] = len(visibleChildren)
		out["nodeId"], out["backendNodeId"] = s.page.InspectorNodeID(frame, node.ID), s.page.InspectorNodeID(frame, node.ID)
		if node.Type == "document" {
			out["documentURL"], out["baseURL"], out["xmlVersion"] = frame.URL(), frame.URL(), ""
		}
		if depth != 0 {
			children := []any{}
			for _, id := range visibleChildren {
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
				visibleShadowChildren := s.inspectorChildren(frame, shadow.Children)
				wire["childNodeCount"] = len(visibleShadowChildren)
				if depth != 0 {
					children := []any{}
					for _, id := range visibleShadowChildren {
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
		s.clearDOMInspector()
		s.domSearches = nil
		s.publishDocumentUpdated(d)
		s.releaseInspectorTurn()
		return
	}
	i.characterMu.Lock()
	characterChanges := i.characterChanges
	i.characterChanges = nil
	i.characterMu.Unlock()
	if revision := s.page.InspectorRevision(); revision == i.revision && len(characterChanges) == 0 {
		return
	} else {
		i.revision = revision
	}
	s.page.PruneInspectorNodeIDs()
	for _, node := range i.nodes {
		if current, ok := s.page.InspectorDocument(node.frame); !ok || current != node.document {
			s.clearDOMInspector()
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
			s.forgetDOM(id)
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
			old.childCount = len(n.Children)
		} else {
			// Chrome keeps a frontend counter for unexpanded parents. An unbound
			// visible character-data notification increments it as an insertion,
			// even for a same-value write; hidden text writes do not decrement it.
			beforeCount := old.childCount
			pending := characterChanges[inspectorParent{old.frame, old.localID}]
			touched := map[int64]bool{}
			visibleWrites := 0
			if pending != nil {
				for localID, writes := range pending.touched {
					wireID := s.page.InspectorNodeID(old.frame, localID)
					touched[wireID] = true
					if i.nodes[wireID] == nil {
						visibleWrites += writes
					}
				}
			}
			delta := inspectorChildrenChanges(old.children, n.Children)
			for child := range delta.removed {
				if !touched[child] {
					old.childCount--
				}
			}
			for child := range delta.inserted {
				if !touched[child] {
					old.childCount++
				}
			}
			if visibleWrites != 0 {
				for range visibleWrites {
					old.childCount++
					s.event("DOM.childNodeCountUpdated", map[string]any{"nodeId": id, "childNodeCount": old.childCount})
				}
			}
			if visibleWrites == 0 && old.childCount != beforeCount {
				s.event("DOM.childNodeCountUpdated", map[string]any{"nodeId": id, "childNodeCount": old.childCount})
			}
		}
		wire := cdpNode(old.document, n, 0)
		wire["nodeId"] = id
		wire["childNodeCount"] = old.childCount
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
	n.Children = s.inspectorChildren(old.frame, n.Children)
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
		s.domInspector.characterMu.Lock()
		key := inspectorParent{old.frame, old.localID}
		delete(s.domInspector.characterParents, key)
		delete(s.domInspector.characterChanges, key)
		if old.localID >= 1<<31 {
			for child, parent := range s.domInspector.characterOwners {
				if child.frame == old.frame && parent == old.localID {
					delete(s.domInspector.characterOwners, child)
				}
			}
		}
		s.domInspector.characterMu.Unlock()
		delete(s.domInspector.nodes, id)
		for _, child := range old.children {
			s.forgetDOM(child)
		}
		for _, root := range old.shadowRoots {
			s.forgetDOM(root)
		}
	}
}
