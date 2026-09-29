//go:build (windows || linux) && amd64

package v8

import (
	gov8 "github.com/maclof/gov8"
	"github.com/moreveal/mimic/internal/engine"
)

func (a *adapter) installGeneratedLazyInstaller() error {
	_, err := a.run(func(s *state, realm *gov8.Context, scope *gov8.Scope) (engine.Value, error) {
		installer, err := s.isolate.NewGeneratedLazyInstaller(scope, realm)
		if err != nil {
			return nil, err
		}
		a.generatedLazyInstaller, err = a.persist(scope, installer)
		return nil, err
	})
	return err
}

func (a *adapter) GeneratedLazyInstaller() engine.Value {
	return a.generatedLazyInstaller
}

func (a *adapter) installGeneratedConstructorFactory() error {
	_, err := a.run(func(s *state, realm *gov8.Context, scope *gov8.Scope) (engine.Value, error) {
		factory, err := s.isolate.NewGeneratedConstructorFactory(scope, realm)
		if err != nil {
			return nil, err
		}
		a.generatedConstructorFactory, err = a.persist(scope, factory)
		return nil, err
	})
	return err
}

func (a *adapter) GeneratedConstructorFactory() engine.Value {
	return a.generatedConstructorFactory
}
