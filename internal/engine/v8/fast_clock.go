//go:build (windows || linux) && amd64

package v8

import (
	"context"
	"time"

	gov8 "github.com/maclof/gov8"
	"github.com/moreveal/mimic/internal/engine"
)

func (a *adapter) newFastClockFunction(kind int, origin time.Time, seed uint64, precisionBase float64, isolated bool) (engine.Value, error) {
	return a.run(func(s *state, realm *gov8.Context, scope *gov8.Scope) (engine.Value, error) {
		function, err := s.isolate.NewNativeClockFunction(scope, realm, kind, origin.UnixMicro(), seed, precisionBase, isolated)
		if err != nil {
			return nil, err
		}
		holderTemplate, err := s.isolate.NewObjectTemplate(scope)
		if err != nil {
			return nil, err
		}
		if _, err := holderTemplate.SetInternalFieldCount(1); err != nil {
			return nil, err
		}
		holder, ok, err := holderTemplate.NewInstance(scope, realm)
		if err != nil || !ok {
			return nil, err
		}
		if _, err := holder.SetByName(scope, realm, "clock", function.Value); err != nil {
			return nil, err
		}
		return a.persist(scope, holder.Value)
	})
}

func (a *adapter) NewPerformanceNow(origin time.Time, seed uint64, precisionBaseMillis float64, isolated bool) (engine.Value, error) {
	holder, err := a.newFastClockFunction(1, origin, seed, precisionBaseMillis, isolated)
	if err != nil {
		return nil, err
	}
	if err := a.Set("__mimicFastPerformanceNow", holder); err != nil {
		return nil, err
	}
	source := `
const holder = globalThis.__mimicFastPerformanceNow;
delete globalThis.__mimicFastPerformanceNow;
return () => holder.clock();`
	return a.EvalBootstrap(context.Background(), source, "mimic-performance-clock.js")
}

func (a *adapter) SetTimeScale(scale float64) { a.timeScale = scale }
