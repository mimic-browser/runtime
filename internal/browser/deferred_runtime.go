package browser

import (
	"context"
	"fmt"
	"time"

	"github.com/moreveal/mimic/internal/engine"
)

// The initial empty document has canonical DOM/lifecycle state immediately,
// but needs no JS isolate until JavaScript is observed. A Page navigated before
// evaluation therefore never constructs a throwaway bootstrap/isolate. All
// access remains inside the existing Page command boundary.
type deferredRuntime struct {
	realm    *Realm
	now      func() time.Time
	observer func(string, bool)
	err      error
	closed   bool
}

func (d *deferredRuntime) ready() (engine.Runtime, error) {
	if d.err != nil {
		return nil, d.err
	}
	if d.closed {
		return nil, fmt.Errorf("realm is closed")
	}
	if d.realm.runtime != d {
		return d.realm.runtime, nil
	}
	r, err := d.realm.newRuntime()
	if err != nil {
		d.err = err
		return nil, err
	}
	// Installation itself calls the engine; publish it on this same Page turn
	// before installing hosts to avoid recursive initialization.
	d.realm.runtime = r
	r.SetTimeSource(d.now)
	r.SetGlobalAccessObserver(d.observer)
	if err := d.realm.install(); err != nil {
		d.realm.runtime.Close()
		d.err = err
		d.realm.runtime = d
		return nil, err
	}
	if d.realm.checkpointClosed {
		if lifecycle, ok := r.(engine.PromiseJobLifecycleRuntime); ok {
			if err := lifecycle.DeactivatePromiseJobs(); err != nil {
				_ = d.realm.runtime.Close()
				d.err = err
				d.realm.runtime = d
				return nil, err
			}
		}
	}
	d.realm.registerPermissions()
	return d.realm.runtime, nil
}

func (d *deferredRuntime) Eval(c context.Context, s, n string) (engine.Value, error) {
	r, e := d.ready()
	if e != nil {
		return nil, e
	}
	return r.Eval(c, s, n)
}
func (d *deferredRuntime) Set(n string, v any) error {
	r, e := d.ready()
	if e != nil {
		return e
	}
	return r.Set(n, v)
}
func (d *deferredRuntime) Get(n string) engine.Value {
	r, e := d.ready()
	if e != nil {
		return nil
	}
	return r.Get(n)
}
func (d *deferredRuntime) Value(v any) engine.Value {
	r, e := d.ready()
	if e != nil {
		return nil
	}
	return r.Value(v)
}
func (d *deferredRuntime) GetProperty(v engine.Value, n string) engine.Value {
	r, e := d.ready()
	if e != nil {
		return nil
	}
	return r.GetProperty(v, n)
}
func (d *deferredRuntime) SetProperty(v engine.Value, n string, x any) error {
	r, e := d.ready()
	if e != nil {
		return e
	}
	return r.SetProperty(v, n, x)
}
func (d *deferredRuntime) TypeOf(v engine.Value) string {
	r, e := d.ready()
	if e != nil {
		return "undefined"
	}
	return r.TypeOf(v)
}
func (d *deferredRuntime) StrictEqual(a, b engine.Value) bool {
	r, e := d.ready()
	return e == nil && r.StrictEqual(a, b)
}
func (d *deferredRuntime) Call(c context.Context, f, t engine.Value, a ...engine.Value) (engine.Value, error) {
	r, e := d.ready()
	if e != nil {
		return nil, e
	}
	return r.Call(c, f, t, a...)
}
func (d *deferredRuntime) Function(f engine.Function) any {
	r, e := d.ready()
	if e != nil {
		return nil
	}
	return r.Function(f)
}
func (d *deferredRuntime) NewPromise() engine.Promise {
	r, e := d.ready()
	if e != nil {
		return engine.Promise{Resolve: func(any) error { return e }, Reject: func(any) error { return e }}
	}
	return r.NewPromise()
}
func (d *deferredRuntime) Await(v engine.Value) (engine.Value, bool, error) {
	r, e := d.ready()
	if e != nil {
		return nil, false, e
	}
	return r.Await(v)
}
func (d *deferredRuntime) SetTimeSource(now func() time.Time) {
	d.now = now
	if d.realm.runtime != d {
		d.realm.runtime.SetTimeSource(now)
	}
}
func (d *deferredRuntime) SetGlobalAccessObserver(f func(string, bool)) {
	d.observer = f
	if d.realm.runtime != d {
		d.realm.runtime.SetGlobalAccessObserver(f)
	}
}
func (d *deferredRuntime) MicrotaskCheckpoint() error {
	if d.realm.runtime == d {
		return nil
	}
	return d.realm.runtime.MicrotaskCheckpoint()
}
func (d *deferredRuntime) MicrotaskCheckpointContext(c context.Context) error {
	if e := c.Err(); e != nil {
		return e
	}
	if r, ok := d.realm.runtime.(interface{ MicrotaskCheckpointContext(context.Context) error }); ok && d.realm.runtime != d {
		return r.MicrotaskCheckpointContext(c)
	}
	return d.MicrotaskCheckpoint()
}
func (d *deferredRuntime) NativeTasksPending() bool {
	if d.realm.runtime == d {
		return false
	}
	r, ok := d.realm.runtime.(interface{ NativeTasksPending() bool })
	return ok && r.NativeTasksPending()
}
func (d *deferredRuntime) Close() error {
	d.closed = true
	if d.realm.runtime != d {
		return d.realm.runtime.Close()
	}
	return nil
}
func (d *deferredRuntime) EvalModule(c context.Context, s, n string, l engine.ModuleLoader) (engine.Value, error) {
	r, e := d.ready()
	if e != nil {
		return nil, e
	}
	if m, ok := r.(engine.ModuleRuntime); ok {
		return m.EvalModule(c, s, n, l)
	}
	return nil, fmt.Errorf("engine does not support modules")
}

func (d *deferredRuntime) PrepareModule(c context.Context, s, n string) ([]string, error) {
	r, err := d.ready()
	if err != nil {
		return nil, err
	}
	if m, ok := r.(engine.PreparedModuleRuntime); ok {
		return m.PrepareModule(c, s, n)
	}
	return nil, fmt.Errorf("engine does not support modules")
}

func (d *deferredRuntime) SetDynamicModuleHandler(handler engine.DynamicModuleHandler) {
	if r, err := d.ready(); err == nil {
		if m, ok := r.(engine.PreparedModuleRuntime); ok {
			m.SetDynamicModuleHandler(handler)
		}
	}
}
func (d *deferredRuntime) Diagnostics() (any, error) { return map[string]any{"deferred": true}, nil }

func (d *deferredRuntime) DeactivatePromiseJobs() error {
	if d.realm.runtime != d {
		if lifecycle, ok := d.realm.runtime.(engine.PromiseJobLifecycleRuntime); ok {
			return lifecycle.DeactivatePromiseJobs()
		}
	}
	return nil
}
