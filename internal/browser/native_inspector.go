package browser

import (
	"errors"
	"sync"

	"github.com/moreveal/mimic/internal/engine"
)

// NativeInspector is one CDP Debugger attachment. It follows the current
// document-tree isolate and carries native script/call-frame identifiers
// without reflecting author values through Mimic's Runtime serializer.
type NativeInspector struct {
	page    *Page
	notify  func([]byte)
	mu      sync.Mutex
	ownerID uint64
	session engine.InspectorSession
	enabled []byte
	closed  bool
}

func NewNativeInspector(page *Page, notify func([]byte)) *NativeInspector {
	d := &NativeInspector{page: page, notify: notify}
	if page.nativeInspectors == nil {
		page.nativeInspectors = make(map[*NativeInspector]struct{})
	}
	page.nativeInspectors[d] = struct{}{}
	return d
}

func (d *NativeInspector) attach(realm *Realm) error {
	runtime, ok := realm.runtime.(engine.InspectorRuntime)
	if !ok {
		return nil
	}
	ownerID := runtime.InspectorOwnerID()
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed || d.ownerID == ownerID && d.session != nil {
		return nil
	}
	if d.session != nil {
		if err := d.session.CloseInspector(); err != nil {
			return err
		}
		d.session = nil
		d.ownerID = 0
	}
	session, err := runtime.ConnectInspector(d.notify)
	if err != nil {
		return err
	}
	d.session = session
	d.ownerID = ownerID
	if len(d.enabled) != 0 {
		if _, err := d.session.DispatchInspector(d.enabled); err != nil {
			_ = d.session.CloseInspector()
			d.session = nil
			d.ownerID = 0
			return err
		}
	}
	return nil
}

func (p *Page) attachNativeInspectors(realm *Realm) error {
	for debugger := range p.nativeInspectors {
		if err := debugger.attach(realm); err != nil {
			return err
		}
	}
	return nil
}

func (p *Page) attachNativeInspectorRealms(debugger *NativeInspector) error {
	for _, realm := range p.evaluationRealms(nil) {
		if realm == nil {
			continue
		}
		if _, deferred := realm.runtime.(*deferredRuntime); deferred {
			continue
		}
		if err := debugger.attach(realm); err != nil {
			return err
		}
	}
	return nil
}

func (d *NativeInspector) Enable(command []byte) ([]byte, error) {
	if deferred, ok := d.page.Top.Realm.runtime.(*deferredRuntime); ok {
		if _, err := deferred.ready(); err != nil {
			return nil, err
		}
	}
	if err := d.page.attachNativeInspectorRealms(d); err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil, errors.New("native inspector is closed")
	}
	if d.session == nil {
		return nil, errors.New("native inspector has no materialized realm")
	}
	response, err := d.session.DispatchInspector(command)
	if err == nil {
		d.enabled = append(d.enabled[:0], command...)
	}
	return response, err
}

func (d *NativeInspector) Dispatch(command []byte) ([]byte, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed || d.session == nil {
		return nil, errors.New("native inspector is not enabled")
	}
	return d.session.DispatchInspector(command)
}

func (d *NativeInspector) Close() error {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return nil
	}
	d.closed = true
	session := d.session
	d.session = nil
	d.ownerID = 0
	d.enabled = nil
	d.mu.Unlock()
	delete(d.page.nativeInspectors, d)
	if session != nil {
		return session.CloseInspector()
	}
	return nil
}
