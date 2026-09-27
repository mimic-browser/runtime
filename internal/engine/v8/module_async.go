//go:build (windows || linux) && amd64

package v8

import (
	"context"
	"errors"

	gov8 "github.com/maclof/gov8"
	"github.com/moreveal/mimic/internal/engine"
)

func (a *adapter) SetDynamicModuleHandler(handler engine.DynamicModuleHandler) {
	_, _ = a.run(func(s *state, _ *gov8.Context, _ *gov8.Scope) (engine.Value, error) {
		a.dynamicModuleHandler = handler
		return nil, s.isolate.SetHostImportModuleDynamicallyCallback(func(request gov8.DynamicImportRequest) (gov8.Promise, error) {
			a, err := s.callbackAdapter(request.Scope.Scope())
			if err != nil {
				return gov8.Promise{}, err
			}
			referrer, err := request.Scope.ToString(request.ResourceName)
			if err != nil {
				return gov8.Promise{}, err
			}
			specifier, err := request.Scope.ToString(request.Specifier)
			if err != nil {
				return gov8.Promise{}, err
			}
			return a.importModuleAsync(request, specifier, referrer)
		})
	})
}

func (a *adapter) PrepareModule(ctx context.Context, source, name string) ([]string, error) {
	var imports []string
	_, err := a.runContext(ctx, func(s *state, realm *gov8.Context, scope *gov8.Scope) (engine.Value, error) {
		module := a.moduleCache[name]
		if module == nil {
			catcher, err := s.isolate.NewTryCatch()
			if err != nil {
				return nil, err
			}
			defer catcher.Close()
			module, err = realm.CompileModule(scope, source, name, catcher)
			if err != nil {
				return nil, a.evalError(catcher, scope, realm, name, err)
			}
			a.modules = append(a.modules, module)
			a.moduleNames[module] = name
			a.moduleCache[name] = module
		}
		requests, err := module.Requests()
		if err != nil {
			return nil, err
		}
		for _, request := range requests {
			imports = append(imports, request.Specifier)
		}
		return nil, nil
	})
	return imports, err
}

func (a *adapter) importModuleAsync(request gov8.DynamicImportRequest, specifier, referrer string) (gov8.Promise, error) {
	resolver, promise, err := request.Scope.NewCallbackPromiseResolver()
	if err != nil {
		return gov8.Promise{}, err
	}
	retained, err := a.persist(request.Scope.Scope(), resolver.Value)
	if err != nil {
		return gov8.Promise{}, err
	}
	completed := false
	a.dynamicModuleHandler(specifier, referrer, func(ctx context.Context, source, name string, loader engine.ModuleLoader, loadErr error) error {
		if completed {
			return nil
		}
		completed = true
		defer a.ReleaseValue(retained)
		var evaluation engine.Value
		if loadErr == nil {
			evaluation, loadErr = a.EvalModule(ctx, source, name, loader)
			var thrown engine.ThrownValue
			if errors.As(loadErr, &thrown) {
				defer a.ReleaseValue(thrown.ThrownValue())
			}
		}
		if evaluation != nil {
			defer a.ReleaseValue(evaluation)
		}
		// An incoming loadErr is borrowed from the module graph, which may
		// reject several concurrent or later imports with the same exception.
		_, err := a.runContext(ctx, func(_ *state, realm *gov8.Context, scope *gov8.Scope) (engine.Value, error) {
			local, err := a.local(scope, retained)
			if err != nil {
				return nil, err
			}
			resolver := gov8.PromiseResolver{Value: local}
			if loadErr != nil {
				var reason gov8.Value
				var thrown engine.ThrownValue
				if errors.As(loadErr, &thrown) {
					reason, err = a.local(scope, thrown.ThrownValue())
				} else {
					reason, err = realm.NewTypeError(scope, loadErr.Error())
				}
				if err != nil {
					return nil, err
				}
				_, err = resolver.Reject(realm, reason)
				return nil, err
			}
			namespace, err := a.moduleCache[name].Namespace(scope)
			if err != nil {
				return nil, err
			}
			factory, err := a.local(scope, a.moduleNamespaceFactory)
			if err != nil {
				return nil, err
			}
			function, _, err := gov8.AsFunction(factory, realm)
			if err != nil {
				return nil, err
			}
			promise, err := a.local(scope, evaluation)
			if err != nil {
				return nil, err
			}
			global, err := realm.GlobalObject(scope)
			if err != nil {
				return nil, err
			}
			result, _, err := function.Call(scope, global.Value, promise, namespace)
			if err != nil {
				return nil, err
			}
			_, err = resolver.Resolve(realm, result)
			return nil, err
		})
		return err
	})
	return promise, nil
}
