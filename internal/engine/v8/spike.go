//go:build (windows || linux) && amd64

// Package v8 implements the primary engine on a thread-affine isolate owner.
// The low-level Realm helpers are also used by the engine differential harness.
package v8

import (
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"sync"

	gov8 "github.com/maclof/gov8"
	"github.com/moreveal/mimic/internal/engine"
)

type HostFunction func(args []string) (any, error)

type response struct {
	value any
	err   error
}

type command func(*state) response

type actorCommand struct {
	run       command
	reject    func(error)
	outerOnly bool // disposal must wait until the active JavaScript stack unwinds
}

type state struct {
	isolate             *gov8.Isolate
	realms              map[uint64]*gov8.Context
	activeAdapter       *adapter
	adapters            map[uint64]*adapter
	securityTokens      map[string]*gov8.Global
	retiredMicrotasks   map[uint64]bool
	next                uint64
	restoreThreadPolicy func() error
}

// Runtime owns one V8 isolate on a dedicated, permanently thread-affine
// goroutine. V8 isolates on either host cannot follow a Go
// goroutine when it migrates between OS threads.
type Runtime struct {
	commands   chan actorCommand
	done       chan struct{}
	snapshot   *gov8.StartupData // consumer copy, released after isolate disposal
	once       sync.Once
	actorTID   uint32
	actorState *state         // actor-thread only after initialization
	deferred   []actorCommand // actor-thread only; drained at the outer boundary
}

type Realm struct {
	runtime *Runtime
	id      uint64
}

// Leave room for short-lived object graphs to die in the young generation.
// The former 4 MiB cap increased retained heap and CPU cost under
// allocation-heavy workloads. Eight MiB won the paired ten-Page gate; sixteen
// increased footprint. This is a growth ceiling, not an eager reservation.
const maxYoungGenerationBytes = 8 << 20

var initialize = sync.OnceValues(func() (bool, error) {
	// The pinned V8 shares read-only artifacts across isolates in its default
	// isolate group. Extending that heap for independently built snapshots gives
	// their read-only references incompatible layouts; release V8 omits the
	// artifact checksum assertion and can restore invalid string pointers.
	// Keep the shared heap at the stock layout. Custom bootstrap objects remain
	// serialized in each snapshot's private heap; Pages still run concurrently.
	// Independent compression groups now separate Page ownership too. Retain
	// this serialization policy rather than change both invariants together.
	if err := gov8.SetFlagsFromString("--no-extensible-ro-snapshot"); err != nil {
		return false, err
	}
	return true, gov8.Initialize()
})

func NewRuntime() (*Runtime, error) { return newRuntime(nil) }

func newRuntime(snapshot *gov8.StartupData) (*Runtime, error) {
	if _, err := initialize(); err != nil {
		return nil, err
	}
	r := &Runtime{commands: make(chan actorCommand), done: make(chan struct{}), snapshot: snapshot}
	ready := make(chan error, 1)
	go r.loop(ready)
	if err := <-ready; err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Runtime) loop(ready chan<- error) {
	// Isolate construction and snapshot restoration are latency-sensitive too.
	// Keep an outer pin until the owner exits: gov8 owns a separate pin and
	// releases it when construction fails or the isolate is disposed.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	restorePolicy := configurePageThreadPolicy()
	// Each Page has an independent isolate. Process-sized nursery defaults
	// retain 16 MiB per tiny Page after bootstrap with under 2 MiB in use.
	// Bound only the young generation; the old generation keeps V8 defaults.
	var iso *gov8.Isolate
	var err error
	fail := func(err error) {
		if restorePolicy != nil {
			err = errors.Join(err, restorePolicy())
		}
		if iso != nil {
			err = errors.Join(err, iso.Close())
		}
		ready <- errors.Join(err, r.releaseSnapshot())
		close(r.done)
	}
	references, err := bootstrapNativeReferences()
	if err != nil {
		fail(err)
		return
	}
	if r.snapshot == nil {
		iso, err = gov8.NewIsolateWithParams(gov8.NewCreateParams().SetExternalReferences(references).SetMaxYoungGenerationSizeInBytes(maxYoungGenerationBytes))
	} else {
		var params *gov8.SnapshotCreateParams
		params, err = gov8.NewSnapshotCreateParams(r.snapshot)
		if err == nil {
			params.SetExternalReferences(references)
			params.SetMaxYoungGenerationSizeInBytes(maxYoungGenerationBytes)
			iso, err = gov8.NewIsolateWithSnapshotParams(params)
		}
	}
	if err != nil {
		fail(err)
		return
	}
	r.actorTID = currentThreadID() // NewIsolate has locked the OS thread.
	if err := iso.SetCaptureStackTraceForUncaughtExceptions(true, 32); err != nil {
		fail(err)
		return
	}
	// Chrome performs Promise jobs at browser event-loop microtask checkpoints,
	// not whenever an arbitrary embedder call happens to return. Leaving V8's
	// default kAuto policy enabled lets microtasks run inside Script::Run and
	// host Function::Call, bypassing Mimic's scheduler and deadlocking async Web
	// APIs whose completion is queued as a browser task.
	if err := iso.SetMicrotasksPolicy(gov8.PolicyExplicit); err != nil {
		fail(err)
		return
	}
	s := &state{isolate: iso, realms: make(map[uint64]*gov8.Context), restoreThreadPolicy: restorePolicy}
	r.actorState = s
	ready <- nil
	for {
		var command actorCommand
		if len(r.deferred) != 0 {
			command = r.deferred[0]
			r.deferred[0] = actorCommand{}
			r.deferred = r.deferred[1:]
		} else {
			command = <-r.commands
		}
		result := command.run(s)
		if result.err != nil && errors.Is(result.err, errDispose) {
			for _, pending := range r.deferred {
				pending.reject(errors.New("V8 runtime is closed"))
			}
			r.deferred = nil
			break
		}
	}
	close(r.done)
}

func (r *Runtime) execute(command command) (any, error) {
	return r.executeCommand(command, false)
}

func (r *Runtime) executeCommand(command command, outerOnly bool) (any, error) {
	select {
	case <-r.done:
		return nil, errors.New("V8 runtime is closed")
	default:
	}
	if currentThreadID() == r.actorTID {
		if outerOnly {
			return nil, errors.New("cannot dispose V8 runtime from its active owner operation")
		}
		result := command(r.actorState)
		return result.value, result.err
	}
	replies := make(chan response, 1)
	select {
	case r.commands <- actorCommand{run: func(s *state) response {
		result := command(s)
		replies <- result
		return result
	}, reject: func(err error) { replies <- response{err: err} }, outerOnly: outerOnly}:
	case <-r.done:
		return nil, errors.New("V8 runtime is closed")
	}
	result := <-replies
	return result.value, result.err
}

func (r *Runtime) NewRealm() (*Realm, error) {
	return r.newRealm(false)
}

func (r *Runtime) newBareRealm() (*Realm, error) {
	return r.newRealm(true)
}

func (r *Runtime) newRealm(bare bool) (*Realm, error) {
	value, err := r.execute(func(s *state) response {
		var ctx *gov8.Context
		var err error
		if r.snapshot != nil && !bare {
			scope, scopeErr := s.isolate.NewScope()
			if scopeErr != nil {
				return response{err: scopeErr}
			}
			var ok bool
			ctx, ok, err = scope.ContextFromSnapshotWithOptions(0, nil)
			err = errors.Join(err, scope.Close())
			if err == nil && !ok {
				err = errors.New("platform context is absent from bootstrap snapshot")
			}
		} else {
			ctx, err = s.isolate.NewContext()
		}
		if err != nil {
			return response{err: err}
		}
		s.next++
		s.realms[s.next] = ctx
		return response{value: s.next}
	})
	if err != nil {
		return nil, err
	}
	return &Realm{runtime: r, id: value.(uint64)}, nil
}

// Eval evaluates an expression and returns its JSON-compatible observable
// value. The differential corpus intentionally consists of expressions whose
// results can cross CDP by value, so this is also the right spike boundary.
func (r *Realm) Eval(source, name string) (any, error) {
	return r.runtime.execute(func(s *state) response {
		ctx := s.realms[r.id]
		if ctx == nil {
			return response{err: errors.New("V8 realm is closed")}
		}
		encodedSource, _ := json.Marshal(source)
		// Indirect eval executes as a global script and returns the script's
		// completion value. Encoding the source also makes arbitrary statements
		// valid here, rather than limiting the spike to parenthesized expressions.
		wrapped := "JSON.stringify((0,eval)(" + string(encodedSource) + "))"
		text, err := evalText(s.isolate, ctx, wrapped, name)
		if err != nil {
			return response{err: err}
		}
		if text == "undefined" || text == "" {
			return response{}
		}
		var value any
		if err := json.Unmarshal([]byte(text), &value); err != nil {
			return response{err: fmt.Errorf("decode V8 result: %w", err)}
		}
		return response{value: value}
	})
}

// SetHostObject installs a plain host object. HostFunction members execute on
// the isolate thread; primitive members are copied by value.
func (r *Realm) SetHostObject(name string, members map[string]any) error {
	_, err := r.runtime.execute(func(s *state) response {
		ctx := s.realms[r.id]
		scope, err := s.isolate.NewScope()
		if err != nil {
			return response{err: err}
		}
		defer scope.Close()
		object, err := scope.NewObject(ctx)
		if err != nil {
			return response{err: err}
		}
		for key, member := range members {
			var value gov8.Value
			switch member := member.(type) {
			case HostFunction:
				function, makeErr := s.isolate.NewFunction(scope, ctx, func(cs *gov8.CallbackScope, args gov8.FunctionCallbackArguments, rv gov8.ReturnValue) {
					strings := make([]string, args.Length())
					for i := range strings {
						argument, getErr := args.Get(i)
						if getErr != nil {
							return
						}
						strings[i], _ = cs.ToString(argument)
					}
					result, callErr := member(strings)
					if callErr != nil {
						exception, _ := cs.NewError(callErr.Error())
						_ = cs.ThrowException(exception)
						return
					}
					setCallbackResult(cs, rv, result)
				}, nil)
				if makeErr != nil {
					return response{err: makeErr}
				}
				value = function.Value
			default:
				value, err = primitive(scope, member)
				if err != nil {
					return response{err: err}
				}
			}
			ok, setErr := object.SetByName(scope, ctx, key, value)
			if setErr != nil || !ok {
				return response{err: fmt.Errorf("set host member %s: %w", key, setErr)}
			}
		}
		global, err := ctx.GlobalObject(scope)
		if err != nil {
			return response{err: err}
		}
		ok, err := global.SetByName(scope, ctx, name, object.Value)
		if err != nil || !ok {
			return response{err: fmt.Errorf("set host object %s: %w", name, err)}
		}
		return response{}
	})
	return err
}

func (r *Realm) MicrotaskCheckpoint() error {
	_, err := r.runtime.execute(func(s *state) response {
		return response{err: s.isolate.PerformMicrotaskCheckpoint()}
	})
	return err
}

func (r *Realm) Dispose() error {
	_, err := r.runtime.executeCommand(func(s *state) response {
		ctx := s.realms[r.id]
		if ctx == nil {
			return response{}
		}
		delete(s.realms, r.id)
		return response{err: ctx.Close()}
	}, true)
	return err
}

var errDispose = errors.New("dispose V8 runtime")

func (r *Runtime) Dispose() error {
	var disposeErr error
	r.once.Do(func() {
		_, dispatchErr := r.executeCommand(func(s *state) response {
			for id, ctx := range s.realms {
				if err := ctx.Close(); err != nil && disposeErr == nil {
					disposeErr = err
				}
				delete(s.realms, id)
			}
			for key, token := range s.securityTokens {
				_ = token.Close()
				delete(s.securityTokens, key)
			}
			// All contexts and adapter roots have been released. Ask V8 to
			// return empty heap pages before disposal transfers them to its
			// process-wide allocator pools.
			if err := s.isolate.LowMemoryNotification(); err != nil && disposeErr == nil {
				disposeErr = err
			}
			// Isolate.Close disposes V8, but gov8's Go callback registry has
			// an explicit lifetime. Release it on the owner thread while the
			// native isolate still exists; otherwise callbacks retain closed
			// Realms, their DOM, response bodies and traces indefinitely.
			if err := gov8.ReleaseIsolateHostState(s.isolate); err != nil && disposeErr == nil {
				disposeErr = err
			}
			// The observation gate is thread-local because each isolate is pinned
			// to this owner. Reset it before the thread can be reused.
			if err := s.isolate.SetPropertyObservationEnabled(true); err != nil && disposeErr == nil {
				disposeErr = err
			}
			if s.restoreThreadPolicy != nil {
				if err := s.restoreThreadPolicy(); err != nil && disposeErr == nil {
					disposeErr = err
				}
				s.restoreThreadPolicy = nil
			}
			if err := s.isolate.Close(); err != nil && disposeErr == nil {
				disposeErr = err
			}
			disposeErr = errors.Join(disposeErr, r.releaseSnapshot())
			return response{err: errDispose}
		}, true)
		if dispatchErr != nil && !errors.Is(dispatchErr, errDispose) && disposeErr == nil {
			disposeErr = dispatchErr
		}
		<-r.done
		requestNativeHeapTrim()
	})
	return disposeErr
}

func evalText(iso *gov8.Isolate, ctx *gov8.Context, source, name string) (string, error) {
	scope, err := iso.NewScope()
	if err != nil {
		return "", err
	}
	defer scope.Close()
	catcher, err := iso.NewTryCatch()
	if err != nil {
		return "", err
	}
	defer catcher.Close()
	script, err := ctx.Compile(scope, source, catcher)
	if err != nil {
		return "", exceptionError(catcher, scope, ctx, name, err)
	}
	defer script.Close()
	result, err := script.Run(scope, catcher)
	if err != nil {
		return "", exceptionError(catcher, scope, ctx, name, err)
	}
	text, err := result.ToString(ctx)
	if err != nil {
		return "", err
	}
	return text, nil
}

func exceptionError(catcher *gov8.TryCatch, scope *gov8.Scope, ctx *gov8.Context, name string, cause error) error {
	text, err := catcher.ExceptionText(scope, ctx)
	if err == nil && text != "" {
		// Read engine-owned source coordinates, not exception.stack: the latter
		// can execute application getters or Error.prepareStackTrace while we
		// are preserving the original exception across a host callback.
		if message, ok, messageErr := catcher.Message(scope); messageErr == nil && ok {
			resource, resourceErr := message.ResourceName(ctx)
			line, hasLine, lineErr := message.LineNumber(ctx)
			column, columnErr := message.StartColumn()
			if resourceErr == nil && resource != "" && resource != "undefined" && hasLine && lineErr == nil && columnErr == nil {
				location := engine.ExceptionDetails{Message: text, Filename: resource, Line: int(line), Column: int(column) + 1}
				var frames []engine.NativeStackFrame
				if stack, captured, stackErr := message.StackTrace(); stackErr == nil && captured {
					if count, countErr := stack.FrameCount(); countErr == nil {
						for index := 0; index < min(count, 32); index++ {
							frame, frameErr := stack.Frame(index)
							if frameErr != nil {
								break
							}
							function, _, _ := frame.FunctionName()
							url, _, _ := frame.ScriptNameOrSourceURL()
							frameLine, _ := frame.LineNumber()
							frameColumn, _ := frame.Column()
							frames = append(frames, engine.NativeStackFrame{Function: function, URL: url, Line: frameLine, Column: frameColumn})
						}
					}
				}
				if len(frames) > 0 && frames[0].URL != "" {
					location.Filename, location.Line, location.Column = frames[0].URL, int(frames[0].Line), int(frames[0].Column)
				}
				return &locatedException{error: fmt.Errorf("%s: %s (%s:%d:%d)", name, text, location.Filename, location.Line, location.Column), location: location, frames: frames}
			}
		}
		return fmt.Errorf("%s: %s", name, text)
	}
	return fmt.Errorf("%s: %w", name, cause)
}

type locatedException struct {
	error
	location engine.ExceptionDetails
	frames   []engine.NativeStackFrame
}

func (e *locatedException) SourceLocation() (engine.ExceptionDetails, []engine.NativeStackFrame) {
	return e.location, e.frames
}

func primitive(scope *gov8.Scope, value any) (gov8.Value, error) {
	switch value := value.(type) {
	case nil:
		return scope.Null()
	case string:
		return scope.NewString(value)
	case bool:
		return scope.Boolean(value)
	case int:
		return scope.Int32(int32(value))
	case int32:
		return scope.Int32(value)
	case uint32:
		return scope.Uint32(value)
	case float64:
		return scope.Number(value)
	default:
		return gov8.Value{}, fmt.Errorf("unsupported V8 spike host value %T", value)
	}
}

func setCallbackResult(cs *gov8.CallbackScope, rv gov8.ReturnValue, value any) {
	switch value := value.(type) {
	case nil:
		_ = rv.SetUndefined()
	case string:
		if value == "" {
			_ = rv.SetEmptyString()
			return
		}
		result, err := cs.NewString(value)
		if err == nil {
			_ = rv.Set(result)
		}
	case bool:
		_ = rv.SetBool(value)
	case int:
		_ = rv.SetInt32(int32(value))
	case int32:
		_ = rv.SetInt32(value)
	case uint32:
		_ = rv.SetUint32(value)
	case float64:
		_ = rv.SetFloat64(value)
	default:
		exception, _ := cs.NewError(fmt.Sprintf("unsupported host return %T", value))
		_ = cs.ThrowException(exception)
	}
}

// StartupData retains the native per-isolate copy until explicit Release.
func (r *Runtime) releaseSnapshot() error {
	if r.snapshot == nil {
		return nil
	}
	err := r.snapshot.Release()
	if err == nil {
		r.snapshot = nil
	}
	return err
}
