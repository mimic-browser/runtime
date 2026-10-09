package browser

import (
	"context"
	"github.com/moreveal/mimic/internal/dom"
)

type inspectorNodeKey struct {
	realm string
	id    int64
}
type inspectorNodeBinding struct {
	frame *Frame
	realm string
	id    int64
}

// Child documents and synthetic roots receive Page-owned int32 CDP handles.
// They are shared by sessions and allocated only for inspected nodes. Ordinary
// top-document arena IDs retain the existing contract.
func (p *Page) InspectorNodeID(frame *Frame, id int64) int64 {
	if frame == p.Top && id < (1<<30) && (p.inspectorTopRealm == "" || p.inspectorTopRealm == frame.RealmID()) {
		p.inspectorTopRealm = frame.RealmID()
		return id
	}
	if p.inspectorNodeIDs == nil {
		p.inspectorNodeIDs = make(map[inspectorNodeKey]int64)
		p.inspectorNodeOwners = make(map[int64]inspectorNodeBinding)
		p.inspectorNodeSequence = 1 << 30
	}
	key := inspectorNodeKey{frame.RealmID(), id}
	if handle, ok := p.inspectorNodeIDs[key]; ok {
		return handle
	}
	p.inspectorNodeSequence++
	if p.inspectorNodeSequence >= 1<<31 {
		return 0
	}
	p.inspectorNodeIDs[key] = p.inspectorNodeSequence
	p.inspectorNodeOwners[p.inspectorNodeSequence] = inspectorNodeBinding{frame, frame.RealmID(), id}
	return p.inspectorNodeSequence
}

func (p *Page) InspectorNodeOwner(id int64) (*Frame, int64, bool) {
	if id > 0 && id < (1<<30) {
		return p.Top, id, p.Top.Realm != nil && (p.inspectorTopRealm == "" || p.inspectorTopRealm == p.Top.RealmID())
	}
	binding, ok := p.inspectorNodeOwners[id]
	if !ok {
		return nil, 0, false
	}
	active, live := p.Frame(binding.frame.ID)
	return binding.frame, binding.id, live && active == binding.frame && active.RealmID() == binding.realm
}

func (p *Page) PruneInspectorNodeIDs() {
	for handle, binding := range p.inspectorNodeOwners {
		if _, _, ok := p.InspectorNodeOwner(handle); !ok {
			delete(p.inspectorNodeIDs, inspectorNodeKey{binding.realm, binding.id})
			delete(p.inspectorNodeOwners, handle)
		}
	}
}

func (p *Page) ReleaseInspectorNodeIDs() {
	p.inspectorNodeIDs = nil
	p.inspectorNodeOwners = nil
	p.inspectorTopRealm = ""
}

func (p *Page) InspectorDocument(frame *Frame) (*dom.Document, bool) {
	if frame == nil || frame.Realm == nil || frame.Realm.closed || frame.Realm.inactive {
		return nil, false
	}
	return frame.Realm.document, true
}

// ShadowRoot is owned by the realm's canonical synthetic attachment slots.
// Its inspection ID is a projection, not a new fragment in the DOM arena.
func (p *Page) InspectorCanonicalNode(frame *Frame, id int64) (dom.Node, bool) {
	d, ok := p.InspectorDocument(frame)
	if !ok {
		return dom.Node{}, false
	}
	if id < 1<<31 {
		return d.Get(id)
	}
	shadows, err := frame.Realm.ShadowSnapshots(context.Background())
	if err != nil {
		return dom.Node{}, false
	}
	for _, shadow := range shadows {
		if shadow.RootID == id {
			return dom.Node{ID: id, Type: "fragment", Parent: shadow.HostID, Children: shadow.Children}, true
		}
	}
	return dom.Node{}, false
}
