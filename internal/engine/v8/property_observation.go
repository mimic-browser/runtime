//go:build (windows || linux) && amd64

package v8

import (
	gov8 "github.com/maclof/gov8"
	"github.com/moreveal/mimic/internal/engine"
)

func (a *adapter) installPropertyObservationFactory() error {
	_, err := a.run(func(s *state, realm *gov8.Context, scope *gov8.Scope) (engine.Value, error) {
		factory, err := s.isolate.NewPropertyObservationFactory(scope, realm)
		if err != nil {
			return nil, err
		}
		a.propertyObservationFactory, err = a.persist(scope, factory)
		return nil, err
	})
	return err
}

func (a *adapter) PropertyObservationFactory() engine.Value {
	return a.propertyObservationFactory
}
