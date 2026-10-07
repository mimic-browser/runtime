package quickjsengine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	quickjs "github.com/buke/quickjs-go"
	"github.com/moreveal/mimic/internal/engine"
)

// Factory keeps QuickJS completely behind the engine-neutral runtime contract.
// Browser semantics must never import this package directly.
type Factory struct{}

func (Factory) New() engine.Runtime {
	// A Realm is already serialized by the browser execution agent. It may be
	// driven by different Go goroutines over its lifetime, so library-level
	// goroutine ownership checks would reject valid, sequential browser work.
	q := quickjs.NewRuntime(quickjs.WithOwnerGoroutineCheck(false))
	c := q.NewContext()
	r := &runtime{runtime: q, context: c, values: map[*quickjs.Value]struct{}{}}
	r.installAwaitProbe()
	return r
}

type value struct {
	r *runtime
	v *quickjs.Value
}

func (v *value) Export() any {
	if v == nil || v.v == nil || v.v.IsUndefined() || v.v.IsNull() {
		return nil
	}
	// CDP's by-value object serialization follows JSON semantics: properties
	// whose value is undefined are omitted, while explicit null is preserved.
	// quickjs-go's generic Unmarshal turns both into Go nil, which creates
	// observable extra keys. Prefer the engine's own JSON serialization for
	// ordinary objects and retain the structural fallback for cyclic/opaque
	// browser objects.
	if v.v.IsObject() && !v.v.IsFunction() {
		encoded := v.v.JSONStringify()
		if encoded != "" {
			var jsonValue any
			if err := json.Unmarshal([]byte(encoded), &jsonValue); err == nil {
				return jsonValue
			}
		}
	}
	var out any
	if err := v.r.context.Unmarshal(v.v, &out); err == nil {
		return out
	}
	// Functions and cyclic browser objects cannot be structurally unmarshaled.
	// They remain opaque engine values and are consumed through Call/property APIs.
	return v
}

func (v *value) String() string {
	if v == nil || v.v == nil {
		return "undefined"
	}
	return v.v.String()
}

type runtime struct {
	runtime    *quickjs.Runtime
	context    *quickjs.Context
	identity   *quickjs.Value
	awaitProbe *quickjs.Value
	closed     bool
	now        func() time.Time
	observer   func(string, bool)
	values     map[*quickjs.Value]struct{}
}

func (r *runtime) installAwaitProbe() {
	r.identity = r.context.Eval(`x=>x`)
	r.awaitProbe = r.context.Eval(`(()=>{const states=new WeakMap();return p=>{let s=states.get(p);if(!s){s={state:0,value:undefined};states.set(p,s);Promise.resolve(p).then(v=>{s.state=1;s.value=v},e=>{s.state=2;s.value=e})}return [s.state,s.value]}})()`)
}

func (r *runtime) Eval(ctx context.Context, source, name string) (engine.Value, error) {
	if err := r.available(ctx); err != nil {
		return nil, err
	}
	r.setInterrupt(ctx)
	v := r.context.Eval(source, quickjs.EvalFileName(name))
	r.runtime.ClearInterruptHandler()
	if v == nil {
		return nil, errors.New("QuickJS returned no value")
	}
	if v.IsException() {
		v.Free()
		return nil, r.jsError(ctx)
	}
	return r.wrap(v), nil
}

func (r *runtime) Set(name string, x any) error {
	if err := r.available(context.Background()); err != nil {
		return err
	}
	v, err := r.marshal(x)
	if err != nil {
		return err
	}
	r.context.Globals().Set(name, v)
	return nil
}

func (r *runtime) Get(name string) engine.Value {
	if r.closed {
		return nil
	}
	return r.wrap(r.context.Globals().Get(name))
}

func (r *runtime) Value(x any) engine.Value {
	v, err := r.marshal(x)
	if err != nil {
		return r.wrap(r.context.NewUndefined())
	}
	return r.wrap(v)
}

func (r *runtime) GetProperty(object engine.Value, name string) engine.Value {
	v, err := r.unwrap(object)
	if err != nil {
		return nil
	}
	return r.wrap(v.Get(name))
}

func (r *runtime) SetProperty(object engine.Value, name string, x any) error {
	v, err := r.unwrap(object)
	if err != nil {
		return err
	}
	xv, err := r.marshal(x)
	if err != nil {
		return err
	}
	v.Set(name, xv)
	return nil
}

func (r *runtime) TypeOf(x engine.Value) string {
	v, err := r.unwrap(x)
	if err != nil || v.IsUndefined() {
		return "undefined"
	}
	if v.IsFunction() {
		return "function"
	}
	if v.IsBool() {
		return "boolean"
	}
	if v.IsString() {
		return "string"
	}
	if v.IsNumber() || v.IsBigInt() {
		return "number"
	}
	return "object"
}

func (r *runtime) StrictEqual(a, b engine.Value) bool {
	av, err := r.unwrap(a)
	if err != nil {
		return false
	}
	bv, err := r.unwrap(b)
	return err == nil && av.StrictEqual(bv)
}

func (r *runtime) Call(ctx context.Context, function, this engine.Value, args ...engine.Value) (engine.Value, error) {
	if err := r.available(ctx); err != nil {
		return nil, err
	}
	fn, err := r.unwrap(function)
	if err != nil {
		return nil, err
	}
	thisValue, err := r.unwrapOrUndefined(this)
	if err != nil {
		return nil, err
	}
	argv := make([]*quickjs.Value, len(args))
	for i := range args {
		argv[i], err = r.unwrap(args[i])
		if err != nil {
			return nil, err
		}
	}
	r.setInterrupt(ctx)
	v := fn.Execute(thisValue, argv...)
	r.runtime.ClearInterruptHandler()
	if v == nil {
		return nil, errors.New("value is not callable")
	}
	if v.IsException() {
		v.Free()
		return nil, r.jsError(ctx)
	}
	return r.wrap(v), nil
}

func (r *runtime) Function(fn engine.Function) any {
	v := r.context.NewFunction(func(_ *quickjs.Context, this *quickjs.Value, args []*quickjs.Value) *quickjs.Value {
		wrapped := make([]engine.Value, len(args))
		for i := range args {
			wrapped[i] = r.wrap(r.duplicate(args[i]))
		}
		result, err := fn(r.wrap(r.duplicate(this)), wrapped)
		if err != nil {
			return r.context.ThrowTypeError("%s", err)
		}
		if result == nil {
			return r.context.NewUndefined()
		}
		unwrapped, err := r.unwrap(result)
		if err != nil {
			return r.context.ThrowTypeError("%s", err)
		}
		// C receives ownership of a host function's return value. Return a new
		// reference so an engine.Value retained by browser code remains valid.
		return r.duplicate(unwrapped)
	})
	return r.wrap(v)
}

func (r *runtime) NewPromise() engine.Promise {
	var resolve, reject func(*quickjs.Value)
	p := r.context.NewPromise(func(ok, fail func(*quickjs.Value)) {
		resolve, reject = ok, fail
	})
	settle := func(callback func(*quickjs.Value), x any) error {
		if r.closed {
			return errors.New("JavaScript runtime is closed")
		}
		// quickjs-go's resolver schedules the actual engine access onto the
		// Context, so this callback is safe when a transport goroutine completes.
		v, err := r.marshal(x)
		if err != nil {
			return err
		}
		callback(v)
		v.Free()
		return nil
	}
	return engine.Promise{
		Value:   r.wrap(p),
		Resolve: func(x any) error { return settle(resolve, x) },
		Reject:  func(x any) error { return settle(reject, x) },
	}
}

func (r *runtime) Await(promise engine.Value) (engine.Value, bool, error) {
	p, err := r.unwrap(promise)
	if err != nil {
		return nil, true, err
	}
	if !p.IsPromise() {
		return promise, true, nil
	}
	probe := r.awaitProbe
	if probe == nil {
		return nil, true, errors.New("promise observation is unavailable")
	}
	undefined := r.context.NewUndefined()
	defer undefined.Free()
	result := probe.Execute(undefined, p)
	if result == nil || result.IsException() {
		if result != nil {
			result.Free()
		}
		return nil, true, r.jsError(context.Background())
	}
	defer result.Free()
	state := result.GetIdx(0)
	defer state.Free()
	switch state.ToInt32() {
	case 0:
		return promise, false, nil
	case 1:
		return r.wrap(result.GetIdx(1)), true, nil
	default:
		reason := result.GetIdx(1)
		defer reason.Free()
		return nil, true, fmt.Errorf("promise rejected: %s", reason.String())
	}
}

func (r *runtime) MicrotaskCheckpoint() error {
	return r.MicrotaskCheckpointContext(context.Background())
}

func (r *runtime) MicrotaskCheckpointContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.setInterrupt(ctx)
	if r.closed {
		return nil
	}
	// A single marker is insufficient: a Promise job that precedes it may enqueue
	// another job behind it. QuickJS' public Go wrapper stops Await as soon as the
	// marker settles, whereas a browser microtask checkpoint must keep draining
	// jobs added by jobs. A guarded marker chain keeps work interleaved until all
	// practical Promise chains reach quiescence; the scheduler retains the outer
	// runaway-task guard.
	marker := r.context.Eval(`(()=>{let p=Promise.resolve();for(let i=0;i<64;i++)p=p.then(()=>undefined);return p})()`)
	if marker == nil || marker.IsException() {
		if marker != nil {
			marker.Free()
		}
		return r.jsError(context.Background())
	}
	result := r.context.Await(marker)
	if result == nil {
		return errors.New("QuickJS microtask checkpoint failed")
	}
	defer result.Free()
	if result.IsException() {
		return r.jsError(context.Background())
	}
	return nil
}

func (r *runtime) SetTimeSource(now func() time.Time) {
	r.now = now
	if r.closed || now == nil {
		return
	}
	hostNow := r.context.NewFunction(func(*quickjs.Context, *quickjs.Value, []*quickjs.Value) *quickjs.Value {
		return r.context.NewInt64(r.now().UnixMilli())
	})
	r.context.Globals().Set("__mimicDateNow", hostNow)
	v := r.context.Eval(`(() => {
  const mimicDateNow = globalThis.__mimicDateNow;
  delete globalThis.__mimicDateNow;
  const NativeDate = Date;
  const MimicDate = new Proxy(NativeDate, {
    apply(target, thisArg, args) {
      return args.length
        ? Reflect.apply(target, thisArg, args)
        : new NativeDate(mimicDateNow()).toString();
    },
    construct(target, args, newTarget) {
      return Reflect.construct(target, args.length ? args : [mimicDateNow()], newTarget);
    },
  });
  Object.defineProperty(MimicDate, 'now', {
    value: () => mimicDateNow(),
    writable: true,
    configurable: true,
  });
  Object.defineProperty(MimicDate.prototype, 'constructor', {
    value: MimicDate,
    writable: true,
    configurable: true,
  });
  globalThis.Date = MimicDate;
  const NativeError = Error;
  const normalize = (error) => {
    if (typeof error.stack === 'string') {
      let lines = error.stack
        .split('\n')
        .filter((line) => !/^\s*at (?:construct|apply) (?:\(native\)|\(<input>:)/.test(line));
      lines = lines.map((line) =>
        line.replace(/^\s*at (?:<anonymous>|<eval>) \((.+)\)$/, '    at $1'),
      );
      // Subclass accessors can depend on state initialized after super().
      // Error construction must not invoke them while adding a stack prefix.
      let name = 'Error';
      for (let prototype = error; prototype; prototype = Object.getPrototypeOf(prototype)) {
        const descriptor = Object.getOwnPropertyDescriptor(prototype, 'name');
        if (descriptor) {
          if (typeof descriptor.value === 'string') name = descriptor.value;
          break;
        }
      }
      const message = Object.getOwnPropertyDescriptor(error, 'message')?.value;
      if (!lines[0] || !lines[0].startsWith(name + ':'))
        lines.unshift(name + (message ? ': ' + message : ''));
      error.stack = lines.join('\n');
    }
    return error;
  };
  const MimicError = new Proxy(NativeError, {
    apply(target, self, args) {
      return normalize(Reflect.apply(target, self, args));
    },
    construct(target, args, newTarget) {
      return normalize(Reflect.construct(target, args, newTarget));
    },
  });
  Object.defineProperty(MimicError.prototype, 'constructor', {
    value: MimicError,
    writable: true,
    configurable: true,
  });
  Object.defineProperty(MimicError, 'captureStackTrace', {
    value: function captureStackTrace(target) {
      const error = normalize(new NativeError());
      Object.defineProperty(target, 'stack', {
        value: error.stack,
        writable: true,
        configurable: true,
      });
    },
    writable: true,
    configurable: true,
  });
  globalThis.Error = MimicError;
})();`)
	if v != nil {
		v.Free()
	}
}

// QuickJS does not expose replacement of the Realm global object. Generated
// bindings still report API/semantic misses explicitly; this hook is retained
// for the engine-neutral contract and future proxy support.
func (r *runtime) SetGlobalAccessObserver(observer func(string, bool)) { r.observer = observer }

func (r *runtime) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	if r.identity != nil {
		r.identity.Free()
		r.identity = nil
	}
	if r.awaitProbe != nil {
		r.awaitProbe.Free()
		r.awaitProbe = nil
	}
	for v := range r.values {
		v.Free()
	}
	r.values = nil
	r.context.Close()
	r.runtime.Close()
	return nil
}

func (r *runtime) available(ctx context.Context) error {
	if r.closed {
		return errors.New("JavaScript runtime is closed")
	}
	return ctx.Err()
}

func (r *runtime) setInterrupt(ctx context.Context) {
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		// The native QuickJS timeout avoids a CGo callback at every interrupt
		// poll, which is material for JavaScript interpreters/obfuscated VMs.
		// Keep sub-second test/control deadlines on the precise Go handler.
		if remaining >= 2*time.Second {
			seconds := uint64((remaining + time.Second - 1) / time.Second)
			r.runtime.SetExecuteTimeout(seconds)
			return
		}
	}
	r.runtime.SetInterruptHandler(func() int {
		select {
		case <-ctx.Done():
			return 1
		default:
			return 0
		}
	})
}

func (r *runtime) jsError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		jsErr := r.context.Exception()
		if jsErr != nil {
			return fmt.Errorf("%w: %v", err, jsErr)
		}
		return err
	}
	if err := r.context.Exception(); err != nil {
		var detailed *quickjs.Error
		if errors.As(err, &detailed) && detailed.Stack != "" {
			return fmt.Errorf("%s\n%s", err, detailed.Stack)
		}
		return err
	}
	return errors.New("JavaScript exception")
}

func (r *runtime) unwrap(v engine.Value) (*quickjs.Value, error) {
	x, ok := v.(*value)
	if !ok || x == nil || x.r != r || x.v == nil {
		return nil, errors.New("value belongs to another JavaScript engine")
	}
	return x.v, nil
}

func (r *runtime) unwrapOrUndefined(v engine.Value) (*quickjs.Value, error) {
	if v == nil {
		return r.context.NewUndefined(), nil
	}
	return r.unwrap(v)
}

func (r *runtime) marshal(x any) (*quickjs.Value, error) {
	if buffer, ok := x.(engine.BinaryBuffer); ok {
		return r.context.NewArrayBuffer(buffer), nil
	}
	if x == nil {
		return r.context.NewNull(), nil
	}
	if v, ok := x.(engine.Value); ok {
		unwrapped, err := r.unwrap(v)
		if err != nil {
			return nil, err
		}
		return r.duplicate(unwrapped), nil
	}
	rv := reflect.ValueOf(x)
	if rv.Kind() == reflect.Map && rv.Type().Key().Kind() == reflect.String {
		object := r.context.NewObject()
		iter := rv.MapRange()
		for iter.Next() {
			v, err := r.marshal(iter.Value().Interface())
			if err != nil {
				return nil, err
			}
			object.Set(iter.Key().String(), v)
		}
		return object, nil
	}
	if rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array {
		array := r.context.Eval(`[]`)
		if array == nil || array.IsException() {
			return nil, r.jsError(context.Background())
		}
		for i := 0; i < rv.Len(); i++ {
			v, err := r.marshal(rv.Index(i).Interface())
			if err != nil {
				return nil, err
			}
			array.SetIdx(int64(i), v)
		}
		return array, nil
	}
	return r.context.Marshal(x)
}

// duplicate obtains an owned QuickJS reference using ordinary JavaScript
// return-value semantics. quickjs-go intentionally keeps JS_DupValue private,
// while JS_Call returns an owned reference to its result.
func (r *runtime) duplicate(v *quickjs.Value) *quickjs.Value {
	if v == nil || r.identity == nil {
		return r.context.NewUndefined()
	}
	undefined := r.context.NewUndefined()
	defer undefined.Free()
	return r.identity.Execute(undefined, v)
}

func (r *runtime) wrap(v *quickjs.Value) *value {
	if v != nil && r.values != nil {
		r.values[v] = struct{}{}
	}
	return &value{r: r, v: v}
}

var _ engine.Factory = Factory{}
var _ engine.Runtime = (*runtime)(nil)
