//go:build (windows || linux) && amd64

package v8

import (
	"errors"
	gov8 "github.com/maclof/gov8"
	"sync"
	"time"

	"github.com/moreveal/mimic/internal/engine"
)

// Retain an execution owner until its last connected realm has torn down. The
// original release belongs to the root adapter (ordinary or pooled snapshot).
// This counter does not serialize JS work or join independent Page owners.
type connectedRealmOwner struct {
	mu         sync.Mutex
	references int
	release    func() error
}

func (o *connectedRealmOwner) retain() {
	o.mu.Lock()
	o.references++
	o.mu.Unlock()
}

func (o *connectedRealmOwner) close() error {
	o.mu.Lock()
	o.references--
	last := o.references == 0
	o.mu.Unlock()
	if last {
		return o.release()
	}
	return nil
}

func (a *adapter) NewRealmRuntime(useBootstrap bool) (engine.Runtime, bool, error) {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return nil, false, errors.New("V8 realm is closed")
	}
	if a.realmOwner == nil {
		a.realmOwner = &connectedRealmOwner{references: 1, release: a.release}
		a.release = a.realmOwner.close
	}
	owner := a.realmOwner
	owner.retain()
	a.mu.Unlock()

	started := time.Now()
	restored := useBootstrap && a.owner.snapshot != nil
	var realm *Realm
	var err error
	if restored {
		realm, err = a.owner.NewRealm()
	} else {
		realm, err = a.owner.newBareRealm()
	}
	if err != nil {
		_ = owner.close()
		return nil, false, err
	}
	child, err := newAdapterForRealm(a.owner, realm, newDiagnostics(), owner.close, started)
	if err != nil {
		return nil, false, err
	}
	child.realmOwner = owner
	return child, restored, nil
}

// Isolate callbacks serve every connected context. Dispatch by the actual
// native context, never the last adapter to install a callback or drain jobs.
func (s *state) callbackAdapter(scope *gov8.Scope) (*adapter, error) {
	current, err := s.isolate.CurrentContext(scope)
	if err != nil {
		return nil, err
	}
	for id, adapter := range s.adapters {
		realm := s.realms[id]
		if realm == nil {
			continue
		}
		same, err := current.SameAs(realm)
		if err != nil {
			return nil, err
		}
		if same {
			return adapter, nil
		}
	}
	return nil, errors.New("native callback has no live originating realm")
}

func (a *adapter) DeactivatePromiseJobs() error {
	_, err := a.run(func(s *state, _ *gov8.Context, _ *gov8.Scope) (engine.Value, error) {
		if s.retiredMicrotasks == nil {
			s.retiredMicrotasks = make(map[uint64]bool)
		}
		s.retiredMicrotasks[a.realm.id] = true
		return nil, nil
	})
	return err
}

func (s *state) retireMicrotasks() error {
	for id := range s.retiredMicrotasks {
		if realm := s.realms[id]; realm != nil {
			if err := realm.DisableMicrotasks(); err != nil {
				return err
			}
		}
		delete(s.retiredMicrotasks, id)
	}
	return nil
}
