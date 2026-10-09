//go:build (windows || linux) && amd64

package v8

import (
	"sync/atomic"
	"unsafe"

	gov8 "github.com/maclof/gov8"
	"github.com/moreveal/mimic/internal/engine"
)

type sharedRevision struct {
	word  *uint64
	store *gov8.BackingStore
}

func (r *sharedRevision) Publish(revision uint64) { atomic.StoreUint64(r.word, revision) }
func (r *sharedRevision) Close() error            { return r.store.Close() }

// Each realm owns an eight-byte native word. The JavaScript view keeps the
// mapping alive through V8's counted backing store; canonical DOM publication
// uses an aligned atomic store without entering an isolate or invoking JS.
func (a *adapter) NewSharedRevision() (engine.Value, engine.SharedRevision, error) {
	word, free, err := allocateRevisionWord()
	if err != nil {
		return nil, nil, err
	}
	var store *gov8.BackingStore
	value, err := a.run(func(s *state, realm *gov8.Context, scope *gov8.Scope) (engine.Value, error) {
		var err error
		store, err = s.isolate.NewSharedArrayBufferBackingStoreFromPtr(unsafe.Pointer(word), 8, func(unsafe.Pointer, int, uintptr) { free() }, 0)
		if err != nil {
			return nil, err
		}
		buffer, err := gov8.NewSharedArrayBufferWithBackingStore(scope, realm, store)
		if err != nil {
			return nil, err
		}
		return a.persist(scope, buffer.Value)
	})
	if err != nil {
		if store != nil {
			_ = store.Close()
		} else {
			free()
		}
		return nil, nil, err
	}
	return value, &sharedRevision{word: word, store: store}, nil
}
