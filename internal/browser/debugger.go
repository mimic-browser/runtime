package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/moreveal/mimic/internal/engine"
	"github.com/moreveal/mimic/internal/scheduler"
	"github.com/moreveal/mimic/internal/trace"
)

// Debugger owns the object groups for one protocol session. All operations,
// including Close, run under the Page command boundary. Live JavaScript values
// stay in their realm; protocol serialization never exports a user object to Go.
type Debugger struct {
	page       *Page
	id         string
	realms     map[string]*debuggerRealm
	exceptions int64
	// Pending promises release the command boundary only while waiting for a
	// Page task. The embedding supplies both hooks, or neither, and reacquires
	// exactly the locks it released before the debugger touches realm state.
	BeforeWait     func()
	AfterWait      func()
	Console        func(realmID, name string, args []any)
	ConsoleEnabled func() bool
	BindingCalled  func(realmID, name, payload string)
	bindings       map[string][]debuggerBindingScope
	inspected      []debuggerInspectedNode
}

type debuggerInspectedNode struct {
	realm *Realm
	value engine.Value
}

type debuggerRealm struct {
	realm       *Realm
	frameID     string
	bridge      engine.Value
	lastConsole engine.Value
}

type DebuggerOptions struct {
	RespectCSP     bool // Runtime.evaluate allowUnsafeEvalBlockedByCSP=false
	ObjectGroup    string
	ReturnByValue  bool
	AwaitPromise   bool
	CommandLineAPI bool
}

func NewDebugger(page *Page) *Debugger {
	d := &Debugger{page: page, id: uuid.NewString(), realms: make(map[string]*debuggerRealm)}
	if page.debuggers == nil {
		page.debuggers = make(map[*Debugger]struct{})
	}
	page.debuggers[d] = struct{}{}
	return d
}

func (p *Page) waitDebuggerProgress() <-chan struct{} {
	p.debuggerWaitMu.Lock()
	defer p.debuggerWaitMu.Unlock()
	if p.debuggerProgress == nil {
		p.debuggerProgress = make(chan struct{})
	}
	return p.debuggerProgress
}

func (p *Page) notifyDebuggerProgress() {
	p.debuggerWaitMu.Lock()
	if p.debuggerProgress != nil {
		close(p.debuggerProgress)
		p.debuggerProgress = nil
	}
	p.debuggerWaitMu.Unlock()
}

func releaseDebuggerValue(r *Realm, v engine.Value) {
	if v != nil && !r.closed {
		if owner, ok := r.runtime.(engine.ValueReleaser); ok {
			owner.ReleaseValue(v)
		}
	}
}

func (d *Debugger) Close() {
	for _, node := range d.inspected {
		releaseDebuggerValue(node.realm, node.value)
	}
	d.inspected = nil
	delete(d.page.debuggers, d)
	for id, state := range d.realms {
		releaseDebuggerValue(state.realm, state.lastConsole)
		releaseDebuggerValue(state.realm, state.bridge)
		delete(d.realms, id)
	}
}

// Prune drops session roots for destroyed execution contexts, including a
// document realm which remains alive only through a page-script reference.
func (d *Debugger) Prune() {
	kept := d.inspected[:0]
	for _, node := range d.inspected {
		if node.realm.closed || node.realm.inactive {
			releaseDebuggerValue(node.realm, node.value)
		} else {
			kept = append(kept, node)
		}
	}
	clear(d.inspected[len(kept):])
	d.inspected = kept
	for id, state := range d.realms {
		if !d.alive(state) {
			releaseDebuggerValue(state.realm, state.lastConsole)
			releaseDebuggerValue(state.realm, state.bridge)
			delete(d.realms, id)
		}
	}
}

func (d *Debugger) alive(state *debuggerRealm) bool {
	frame, ok := d.page.Frame(state.frameID)
	if !ok || frame.Realm == nil || state.realm.closed || state.realm.inactive {
		return false
	}
	return frame.Realm == state.realm || state.realm.mainWorld == frame.Realm
}

func (d *Debugger) state(ctx context.Context, frameID, realmID string) (*debuggerRealm, error) {
	if frameID == "" {
		frameID = d.page.Top.ID
	}
	frame, ok := d.page.Frame(frameID)
	if !ok || frame.Realm == nil || frame.Realm.closed || frame.Realm.inactive {
		return nil, fmt.Errorf("Cannot find context with specified id")
	}
	r := frame.Realm
	if realmID != "" && realmID != r.ID {
		r = nil
		for _, world := range frame.Realm.isolatedWorlds {
			if world.ID == realmID {
				r = world
				break
			}
		}
		if r == nil {
			return nil, fmt.Errorf("Cannot find context with specified id")
		}
	}
	if old := d.realms[r.ID]; old != nil {
		return old, nil
	}
	d.Prune()
	if deferred, ok := r.runtime.(*deferredRuntime); ok {
		if _, err := deferred.ready(); err != nil {
			return nil, err
		}
	}
	prefix := r.val(d.id + "." + r.ID)
	defer releaseDebuggerValue(r, prefix)
	bridge, err := r.runtime.Call(ctx, r.debuggerFactory, nil, r.frameNodeDescribe, prefix)
	if err != nil {
		return nil, err
	}
	state := &debuggerRealm{realm: r, frameID: frameID, bridge: bridge}
	d.realms[r.ID] = state
	return state, nil
}

func (d *Debugger) objectState(id string) (*debuggerRealm, error) {
	parts := strings.SplitN(id, ".", 3)
	if len(parts) != 3 || parts[0] != d.id {
		return nil, fmt.Errorf("Could not find object with given id")
	}
	state := d.realms[parts[1]]
	if state == nil || !d.alive(state) {
		return nil, fmt.Errorf("Could not find object with given id")
	}
	return state, nil
}

func (state *debuggerRealm) invoke(ctx context.Context, operation string, params any, value engine.Value) (engine.Value, error) {
	started := time.Now()
	encoded, err := json.Marshal(params)
	state.realm.agent.Page().profileDebuggerSubphase("invoke.marshal", started)
	if err != nil {
		return nil, err
	}
	operationValue, paramsValue := state.realm.val(operation), state.realm.val(string(encoded))
	defer releaseDebuggerValue(state.realm, operationValue)
	defer releaseDebuggerValue(state.realm, paramsValue)
	started = time.Now()
	if value == nil {
		result, err := state.realm.runtime.Call(ctx, state.bridge, nil, operationValue, paramsValue)
		state.realm.agent.Page().profileDebuggerSubphase("invoke.runtimeCall", started)
		return result, err
	}
	result, err := state.realm.runtime.Call(ctx, state.bridge, nil, operationValue, paramsValue, value)
	state.realm.agent.Page().profileDebuggerSubphase("invoke.runtimeCall", started)
	return result, err
}

func (state *debuggerRealm) json(ctx context.Context, operation string, params any, value engine.Value) (map[string]any, error) {
	result, err := state.invoke(ctx, operation, params, value)
	if err != nil {
		return nil, err
	}
	defer releaseDebuggerValue(state.realm, result)
	started := time.Now()
	var decoded map[string]any
	if err := json.Unmarshal([]byte(result.String()), &decoded); err != nil {
		return nil, fmt.Errorf("debugger serialization: %w", err)
	}
	state.realm.agent.Page().profileDebuggerSubphase("json.unmarshal", started)
	return decoded, nil
}

func (d *Debugger) enter() func() {
	d.page.realmEvaluationDepth++
	return func() { d.page.realmEvaluationDepth--; d.page.collectRealmOwners() }
}

func (d *Debugger) Evaluate(ctx context.Context, frameID, realmID, source string, options DebuggerOptions) (map[string]any, error) {
	defer d.enter()()
	state, err := d.state(ctx, frameID, realmID)
	if err != nil {
		return nil, err
	}
	if err := state.realm.syncShadowSnapshots(ctx); err != nil {
		return nil, err
	}
	var value engine.Value
	err = state.realm.debuggerInline(ctx, !options.RespectCSP, func(ctx context.Context) error {
		var err error
		if options.CommandLineAPI {
			native, ok := state.realm.runtime.(engine.CommandLineRuntime)
			if !ok {
				return fmt.Errorf("Command-line API is unsupported by this engine")
			}
			selected := make([]engine.Value, 0, len(d.inspected))
			var imported []engine.Value
			defer func() {
				for _, value := range imported {
					releaseDebuggerValue(state.realm, value)
				}
			}()
			for _, node := range d.inspected {
				if node.realm.closed || node.realm.inactive {
					selected = append(selected, nil)
				} else if node.realm == state.realm {
					selected = append(selected, node.value)
				} else {
					encoded, err := node.realm.crossRealmValue(node.value)
					if err != nil {
						return err
					}
					state.realm.retainRealm(node.realm)
					value, err := state.realm.importFrameReference(encoded)
					if err != nil {
						return err
					}
					imported = append(imported, value)
					selected = append(selected, value)
				}
			}
			value, err = native.EvalCommandLine(ctx, source, selected, state.lastConsole)
		} else {
			value, err = state.realm.Evaluate(ctx, source, "")
		}
		return err
	})
	if err != nil {
		return d.exception(ctx, state, err, options, false)
	}
	defer releaseDebuggerValue(state.realm, value)
	return d.finish(ctx, state, value, options)
}

func (d *Debugger) CallFunction(ctx context.Context, frameID, realmID, declaration string, params map[string]any, options DebuggerOptions) (map[string]any, error) {
	callStarted := time.Now()
	trace.Record(ctx, trace.CDP, "invoke.begin", map[string]any{"frameID": frameID, "realmID": realmID})
	defer func() {
		d.profileCallFunctionPhase("total", callStarted)
		trace.Record(ctx, trace.CDP, "invoke.end", map[string]any{"frameID": frameID, "realmID": realmID, "durationNs": time.Since(callStarted).Nanoseconds()})
	}()
	defer d.enter()()
	phaseStarted := time.Now()
	var state *debuggerRealm
	var err error
	if objectID, _ := params["objectId"].(string); objectID != "" {
		state, err = d.objectState(objectID)
		if err == nil && (frameID != "" && frameID != state.frameID || realmID != "" && realmID != state.realm.ID) {
			err = fmt.Errorf("Object belongs to a different JavaScript world than target execution context")
		}
		if err == nil {
			if _, explicit := params["objectGroup"]; !explicit {
				group, e := state.json(ctx, "group", params, nil)
				if e != nil {
					return nil, e
				}
				options.ObjectGroup, _ = group["group"].(string)
			}
		}
	} else {
		state, err = d.state(ctx, frameID, realmID)
	}
	if err != nil {
		return nil, err
	}
	d.profileCallFunctionPhase("state", phaseStarted)
	phaseStarted = time.Now()
	if err := state.realm.syncShadowSnapshots(ctx); err != nil {
		return nil, err
	}
	d.profileCallFunctionPhase("shadowSnapshots", phaseStarted)
	phaseStarted = time.Now()
	if arguments, ok := params["arguments"].([]any); ok {
		for _, argument := range arguments {
			if arg, ok := argument.(map[string]any); ok {
				if objectID, _ := arg["objectId"].(string); objectID != "" {
					owner, err := d.objectState(objectID)
					if err != nil {
						return nil, err
					}
					if owner != state {
						return nil, fmt.Errorf("Argument should belong to the same JavaScript world as target object")
					}
				}
			}
		}
	}
	d.profileCallFunctionPhase("arguments", phaseStarted)
	var value engine.Value
	phaseStarted = time.Now()
	err = state.realm.debuggerInline(ctx, true, func(ctx context.Context) error {
		declarationStarted := time.Now()
		// Chrome starts the declaration on the opening parenthesis's line.
		// The trailing newline keeps author sourceURL/comments intact.
		function, err := state.realm.Evaluate(ctx, "("+declaration+"\n)", "")
		if err != nil {
			return err
		}
		defer releaseDebuggerValue(state.realm, function)
		if state.realm.runtime.TypeOf(function) != "function" {
			return fmt.Errorf("Given expression does not evaluate to a function")
		}
		d.profileCallFunctionPhase("declaration", declarationStarted)
		invokeStarted := time.Now()
		invoke := func(ctx context.Context) error {
			// Resolve debugger handles in their owning realm, then enter the
			// author function through the engine. A JS apply wrapper would become
			// an observable caller frame and affect stack/caller inspection.
			prepared, err := state.invoke(ctx, "prepareCall", params, nil)
			if err != nil {
				return err
			}
			defer releaseDebuggerValue(state.realm, prepared)
			receiver := state.realm.runtime.GetProperty(prepared, "0")
			defer releaseDebuggerValue(state.realm, receiver)
			length := state.realm.runtime.GetProperty(prepared, "length")
			count, err := strconv.Atoi(length.String())
			releaseDebuggerValue(state.realm, length)
			if err != nil || count < 1 {
				return fmt.Errorf("invalid prepared debugger arguments")
			}
			arguments := make([]engine.Value, count-1)
			defer func() {
				for _, argument := range arguments {
					releaseDebuggerValue(state.realm, argument)
				}
			}()
			for i := range arguments {
				arguments[i] = state.realm.runtime.GetProperty(prepared, strconv.Itoa(i+1))
			}
			value, err = state.realm.runtime.Call(ctx, function, receiver, arguments...)
			return err
		}
		if owner, ok := state.realm.runtime.(engine.OwnerRuntime); ok {
			err = owner.RunOnOwner(ctx, invoke)
		} else {
			err = invoke(ctx)
		}
		d.profileCallFunctionPhase("invoke", invokeStarted)
		return err
	})
	d.profileCallFunctionPhase("inline", phaseStarted)
	if err != nil {
		return d.exception(ctx, state, err, options, false)
	}
	phaseStarted = time.Now()
	defer releaseDebuggerValue(state.realm, value)
	result, err := d.finish(ctx, state, value, options)
	d.profileCallFunctionPhase("finish", phaseStarted)
	return result, err
}

func (d *Debugger) profileCallFunctionPhase(phase string, started time.Time) {
	if os.Getenv("MIMIC_PROFILE_CDP") != "1" || d.page == nil {
		return
	}
	d.page.Trace().Add(trace.CDP, "callFunctionPhase", map[string]any{
		"phase": phase,
		"ms":    float64(time.Since(started)) / float64(time.Millisecond),
	})
}

func (p *Page) profileDebuggerSubphase(phase string, started time.Time) {
	if os.Getenv("MIMIC_PROFILE_CDP") != "1" || p == nil {
		return
	}
	p.Trace().Add(trace.CDP, "callFunctionSubphase", map[string]any{
		"phase": phase,
		"ms":    float64(time.Since(started)) / float64(time.Millisecond),
	})
}

func (d *Debugger) finish(ctx context.Context, state *debuggerRealm, value engine.Value, options DebuggerOptions) (map[string]any, error) {
	if options.AwaitPromise {
		resolved, err := d.await(ctx, state, value)
		if err != nil {
			return d.exception(ctx, state, err, options, true)
		}
		defer releaseDebuggerValue(state.realm, resolved)
		value = resolved
	}
	if !d.alive(state) {
		return nil, fmt.Errorf("Execution context was destroyed")
	}
	if options.ObjectGroup == "console" {
		retained, err := state.invoke(ctx, "identity", map[string]any{}, value)
		if err != nil {
			return nil, err
		}
		releaseDebuggerValue(state.realm, state.lastConsole)
		state.lastConsole = retained
	}
	return state.json(ctx, "hold", map[string]any{"objectGroup": options.ObjectGroup, "returnByValue": options.ReturnByValue}, value)
}

func (r *Realm) debuggerInline(ctx context.Context, unsafeEval bool, operation func(context.Context) error) error {
	return r.scheduler.RunInline(ctx, func(ctx context.Context) error {
		if runtime, ok := r.runtime.(engine.DebuggerEvalRuntime); ok && unsafeEval {
			return runtime.RunWithUnsafeEval(ctx, operation)
		}
		return operation(ctx)
	})
}

func (d *Debugger) await(ctx context.Context, state *debuggerRealm, value engine.Value) (engine.Value, error) {
	for {
		if !d.alive(state) {
			return nil, fmt.Errorf("Execution context was destroyed")
		}
		resolved, done, err := state.realm.runtime.Await(value)
		if err != nil || done {
			return resolved, err
		}
		if d.BeforeWait != nil && d.AfterWait != nil {
			progress := d.page.waitDebuggerProgress()
			d.BeforeWait()
			select {
			case <-progress:
			case <-ctx.Done():
				err = ctx.Err()
			}
			d.AfterWait()
			if !d.alive(state) {
				return nil, fmt.Errorf("Execution context was destroyed")
			}
			if err != nil {
				return nil, err
			}
			continue
		}
		queues := make([]*scheduler.Scheduler, 0)
		for _, r := range d.page.evaluationRealms(state.realm) {
			queues = append(queues, r.scheduler)
		}
		err = scheduler.WaitAny(ctx, queues)
		if !d.alive(state) {
			return nil, fmt.Errorf("Execution context was destroyed")
		}
		if err != nil {
			return nil, err
		}
		if err := d.page.runEvaluationTasks(ctx, state.realm); err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			// An exception from another browser task is reported through its own
			// lifecycle/exception channel. It does not reject the Promise inspected
			// by Runtime.evaluate/callFunctionOn in Chrome.
			d.page.trace.Add(trace.Error, "scheduler", map[string]any{"error": err.Error(), "during": "debugger Promise wait"})
		}
	}
}

func (d *Debugger) exception(ctx context.Context, state *debuggerRealm, err error, options DebuggerOptions, promise bool) (map[string]any, error) {
	var thrown engine.ThrownValue
	if !errors.As(err, &thrown) {
		return nil, err
	}
	value := thrown.ThrownValue()
	defer releaseDebuggerValue(state.realm, value)
	result, describeErr := state.json(ctx, "hold", map[string]any{"objectGroup": options.ObjectGroup}, value)
	if describeErr != nil {
		return nil, describeErr
	}
	d.exceptions++
	text := "Uncaught"
	if promise {
		text = "Uncaught (in promise)"
	}
	result["exceptionDetails"] = map[string]any{"exceptionId": d.exceptions, "text": text, "lineNumber": 0, "columnNumber": 0, "exception": result["result"]}
	return result, nil
}

func (d *Debugger) AwaitPromise(ctx context.Context, objectID string, options DebuggerOptions) (map[string]any, error) {
	defer d.enter()()
	state, err := d.objectState(objectID)
	if err != nil {
		return nil, err
	}
	value, err := state.invoke(ctx, "lookup", map[string]any{"objectId": objectID}, nil)
	if err != nil {
		return nil, err
	}
	defer releaseDebuggerValue(state.realm, value)
	options.AwaitPromise = true
	return d.finish(ctx, state, value, options)
}

func (d *Debugger) GetProperties(ctx context.Context, params map[string]any) (map[string]any, error) {
	objectID, _ := params["objectId"].(string)
	state, err := d.objectState(objectID)
	if err != nil {
		return nil, err
	}
	return state.json(ctx, "properties", params, nil)
}

func (d *Debugger) ReleaseObject(ctx context.Context, objectID string) error {
	state, err := d.objectState(objectID)
	if err != nil {
		return nil
	} // Chrome permits releasing an already released handle.
	_, err = state.json(ctx, "release", map[string]any{"objectId": objectID}, nil)
	return err
}

func (d *Debugger) ReleaseObjectGroup(ctx context.Context, group string) error {
	for _, state := range d.realms {
		if !d.alive(state) {
			continue
		}
		if _, err := state.json(ctx, "releaseGroup", map[string]any{"objectGroup": group}, nil); err != nil {
			return err
		}
	}
	return nil
}

// ResolveNode imports the DOM's canonical wrapper into the selected realm.
func (d *Debugger) ResolveNode(ctx context.Context, frameID, realmID string, nodeID int64, group string) (map[string]any, error) {
	state, err := d.state(ctx, frameID, realmID)
	if err != nil {
		return nil, err
	}
	frame, _ := d.page.Frame(state.frameID)
	if _, ok := d.page.InspectorCanonicalNode(frame, nodeID); !ok {
		return nil, fmt.Errorf("Could not find node with given id")
	}
	result, err := state.json(ctx, "resolve", map[string]any{"nodeId": nodeID, "objectGroup": group}, nil)
	if err != nil {
		return nil, err
	}
	return result["result"].(map[string]any), nil
}

func (d *Debugger) RequestNode(ctx context.Context, objectID string) (int64, error) {
	_, id, err := d.NodeObjectOwner(ctx, objectID)
	return id, err
}

// NodeObjectOwner follows the canonical foreign-reference bridge. A DOM node
// returned to a parent Console still belongs to its child document, even though
// the remote object table and wrapper belong to the evaluating parent realm.
func (d *Debugger) NodeObjectOwner(ctx context.Context, objectID string) (string, int64, error) {
	state, err := d.objectState(objectID)
	if err != nil {
		return "", 0, err
	}
	value, err := state.invoke(ctx, "lookup", map[string]any{"objectId": objectID}, nil)
	if err != nil {
		return "", 0, err
	}
	defer releaseDebuggerValue(state.realm, value)
	owner := state.realm
	if owner.frameReferenceDescribe != nil {
		info, err := owner.runtime.Call(ctx, owner.frameReferenceDescribe, nil, value)
		if err != nil {
			return "", 0, err
		}
		defer releaseDebuggerValue(state.realm, info)
		if reference, ok := info.Export().(map[string]any); ok && reference["frame"] != nil {
			if reference["handle"] == nil {
				return state.frameID, 0, nil
			}
			owner, err = owner.referenceRealm(fmt.Sprint(reference["frame"]), fmt.Sprint(reference["realm"]))
			if err != nil {
				return "", 0, err
			}
			value = owner.crossValues[int64(numberValue(reference["handle"]))]
			if value == nil {
				return "", 0, fmt.Errorf("Node reference is no longer available")
			}
			id, err := owner.runtime.Call(ctx, owner.frameNodeDescribe, nil, value)
			if err != nil {
				return "", 0, err
			}
			defer releaseDebuggerValue(owner, id)
			return owner.agent.ContextID(), int64(numberValue(id.Export())), nil
		}
	}
	result, err := state.json(ctx, "node", map[string]any{"objectId": objectID}, nil)
	if err != nil {
		return "", 0, err
	}
	if id, ok := result["nodeId"].(float64); ok {
		return state.frameID, int64(id), nil
	}
	return state.frameID, 0, nil
}

// ObjectFrameID returns the owning frame recorded with a remote object. DOM
// node identifiers are document-local in Mimic, so an object-backed CDP call
// must retain this ownership instead of rediscovering it from the numeric id.
func (d *Debugger) ObjectFrameID(objectID string) (string, error) {
	state, err := d.objectState(objectID)
	if err != nil {
		return "", err
	}
	return state.frameID, nil
}
