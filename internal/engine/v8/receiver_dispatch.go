//go:build (windows || linux) && amd64

package v8

import (
	gov8 "github.com/maclof/gov8"
	"github.com/moreveal/mimic/internal/engine"
)

func (a *adapter) installReceiverDispatchFactory() error {
	_, err := a.run(func(s *state, realm *gov8.Context, scope *gov8.Scope) (engine.Value, error) {
		factory, err := s.isolate.NewReceiverDispatchFactory(scope, realm)
		if err != nil {
			return nil, err
		}
		a.receiverDispatchFactory, err = a.persist(scope, factory)
		return nil, err
	})
	return err
}

func (a *adapter) ReceiverDispatchFactory() engine.Value {
	return a.receiverDispatchFactory
}

// The same ordered table belongs to snapshot creators and consumers. Individual
// gov8 factories expose their own references; browser bindings cannot select
// independently synchronized tables or serialize Go callback registry handles.
func bootstrapNativeReferences() ([]gov8.ExternalReference, error) {
	observations, err := gov8.PropertyObservationReferences()
	if err != nil {
		return nil, err
	}
	dispatch, err := gov8.ReceiverDispatchReferences()
	if err != nil {
		return nil, err
	}
	exceptions, err := gov8.ExceptionStateReferences()
	if err != nil {
		return nil, err
	}
	lazy, err := gov8.GeneratedLazyReferences()
	if err != nil {
		return nil, err
	}
	constructors, err := gov8.GeneratedConstructorReferences()
	if err != nil {
		return nil, err
	}
	return append(append(append(append(observations, dispatch...), exceptions...), lazy...), constructors...), nil
}
