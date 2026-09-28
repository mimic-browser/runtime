//go:build (windows || linux) && amd64

package gov8

import (
	"fmt"
	"unsafe"
)

// ContextOptions are the construction-time options exercised by the pinned
// v8 crate. The local template/global handles and queue must all belong to the
// isolate passed to NewContextWithOptions.
type ContextOptions struct {
	GlobalTemplate *ObjectTemplate
	GlobalObject   *Object
	MicrotaskQueue *MicrotaskQueue
}

// NewContextWithOptions constructs a persistent context from scope-local
// options. Unlike NewContext, it needs an explicit Scope because V8 consumes
// Local<ObjectTemplate> and Local<Value> arguments during construction.
func (i *Isolate) NewContextWithOptions(s *Scope, options *ContextOptions) (*Context, error) {
	if err := s.check(); err != nil {
		return nil, err
	}
	ih, err := i.handleChecked()
	if err != nil {
		return nil, err
	}
	if s.iso != i {
		return nil, foreignIsolate("scope")
	}
	var templateH, globalH, queueH uintptr
	var queue *MicrotaskQueue
	if options != nil {
		if options.GlobalTemplate != nil {
			if err := options.GlobalTemplate.check(); err != nil {
				return nil, err
			}
			if options.GlobalTemplate.iso != i {
				return nil, foreignIsolate("global template")
			}
			templateH = options.GlobalTemplate.h
		}
		if options.GlobalObject != nil {
			if err := options.GlobalObject.Value.check(); err != nil {
				return nil, err
			}
			if options.GlobalObject.iso != i {
				return nil, foreignIsolate("global object")
			}
			globalH = options.GlobalObject.h
		}
		if options.MicrotaskQueue != nil {
			if err := options.MicrotaskQueue.check(); err != nil {
				return nil, err
			}
			if options.MicrotaskQueue.iso != i {
				return nil, foreignIsolate("microtask queue")
			}
			queue = options.MicrotaskQueue
			queueH = queue.handle
		}
	}
	ch, err := callHandle("Context.NewWithOptions",
		proc("gov8_csa_context_new_options"), ih, s.handle, templateH, globalH, queueH)
	if err != nil {
		return nil, err
	}
	i.mu.Lock()
	i.contextsCreated = true
	i.mu.Unlock()
	if queue != nil {
		queue.attachments++
	}
	return &Context{iso: i, handle: ch, microtaskQueue: queue}, nil
}

// GetExtrasBindingObject returns V8's stable extras binding object as a local
// Object owned by s.
func (c *Context) GetExtrasBindingObject(s *Scope) (*Object, error) {
	if err := c.check(); err != nil {
		return nil, err
	}
	sh, err := s.checkedHandleAssumingIsolate()
	if err != nil {
		return nil, err
	}
	if s.iso != c.iso {
		return nil, foreignIsolate("scope")
	}
	var out uintptr
	r1, _, _ := proc("gov8_csa_context_extras_binding_object").Call(
		c.iso.handleAssumingCheck(), c.handle, sh, uintptr(unsafe.Pointer(&out)))
	if int64(r1) < 0 {
		return nil, shimError("Context.GetExtrasBindingObject", r1)
	}
	return &Object{Value{iso: c.iso, sc: s, h: out}}, nil
}

// SetContinuationPreservedEmbedderData stores isolate-wide continuation data.
// The value is retained by V8 and remains visible from every context.
func (s *Scope) SetContinuationPreservedEmbedderData(value Value) error {
	if err := s.check(); err != nil {
		return err
	}
	if err := value.check(); err != nil {
		return err
	}
	if value.iso != s.iso {
		return foreignIsolate("value")
	}
	r1, _, _ := proc("gov8_csa_set_continuation_preserved_data").Call(
		s.iso.handleAssumingCheck(), s.handle, value.h)
	if int64(r1) < 0 {
		return shimError("Scope.SetContinuationPreservedEmbedderData", r1)
	}
	return nil
}

// GetContinuationPreservedEmbedderData returns the isolate-wide continuation
// data in s. Before the first Set it is the JavaScript undefined value.
func (s *Scope) GetContinuationPreservedEmbedderData() (Value, error) {
	if err := s.check(); err != nil {
		return Value{}, err
	}
	var out uintptr
	r1, _, _ := proc("gov8_csa_get_continuation_preserved_data").Call(
		s.iso.handleAssumingCheck(), s.handle, uintptr(unsafe.Pointer(&out)))
	if int64(r1) < 0 {
		return Value{}, shimError("Scope.GetContinuationPreservedEmbedderData", r1)
	}
	return Value{iso: s.iso, sc: s, h: out}, nil
}

// ContextPromiseHooks are context-local Promise lifecycle callbacks. Nil
// fields disable the corresponding hook; an all-nil value disables all hooks.
type ContextPromiseHooks struct {
	Init    *Function
	Before  *Function
	After   *Function
	Resolve *Function
}

// SetPromiseHooks installs hooks on this context. The Go API names the target
// Context explicitly; the Rust API infers it from its entered ContextScope.
func (c *Context) SetPromiseHooks(hooks ContextPromiseHooks) error {
	if err := c.check(); err != nil {
		return err
	}
	values := [4]*Function{hooks.Init, hooks.Before, hooks.After, hooks.Resolve}
	var wires [4]uintptr
	for index, function := range values {
		if function == nil {
			continue
		}
		if err := function.check(); err != nil {
			return err
		}
		if function.iso != c.iso {
			return foreignIsolate("promise hook")
		}
		wires[index] = function.h
	}
	r1, _, _ := proc("gov8_csa_context_set_promise_hooks").Call(
		c.iso.handleAssumingCheck(), c.handle,
		wires[0], wires[1], wires[2], wires[3])
	if int64(r1) < 0 {
		return shimError("Context.SetPromiseHooks", r1)
	}
	return nil
}

// IsRunningMicrotasks reports whether this queue is currently draining.
func (m *MicrotaskQueue) IsRunningMicrotasks() (bool, error) {
	if err := m.check(); err != nil {
		return false, err
	}
	var out int32
	r1, _, _ := proc("gov8_csa_microtask_queue_is_running").Call(
		m.handle, uintptr(unsafe.Pointer(&out)))
	if int64(r1) < 0 {
		return false, shimError("MicrotaskQueue.IsRunningMicrotasks", r1)
	}
	return out != 0, nil
}

// GetMicrotasksScopeDepth returns the number of nested run-microtasks scopes.
// A direct PerformCheckpoint has depth zero, including from inside a callback.
func (m *MicrotaskQueue) GetMicrotasksScopeDepth() (int32, error) {
	if err := m.check(); err != nil {
		return 0, err
	}
	var out int32
	r1, _, _ := proc("gov8_csa_microtask_queue_scope_depth").Call(
		m.handle, uintptr(unsafe.Pointer(&out)))
	if int64(r1) < 0 {
		return 0, shimError("MicrotaskQueue.GetMicrotasksScopeDepth", r1)
	}
	return out, nil
}

// JavascriptExecutionFailure controls a DisallowJavascriptExecutionScope.
type JavascriptExecutionFailure uint8

const (
	// CrashOnFailure terminates the process when JavaScript execution is
	// attempted. Exercise it only in a subprocess.
	CrashOnFailure JavascriptExecutionFailure = iota
	// ThrowOnFailure throws the string "illegal access" into the active V8
	// TryCatch when execution is attempted.
	ThrowOnFailure
	// DumpOnFailure permits execution (the pinned build emits no diagnostic).
	DumpOnFailure
)

// DisallowJavascriptExecutionScope is a lexical engine guard. It must Close
// in strict LIFO order on the isolate's owner thread.
type DisallowJavascriptExecutionScope struct {
	scope  *Scope
	handle uintptr
	closed bool
}

// NewDisallowJavascriptExecutionScope starts a JavaScript execution guard.
func (s *Scope) NewDisallowJavascriptExecutionScope(onFailure JavascriptExecutionFailure) (*DisallowJavascriptExecutionScope, error) {
	if err := s.check(); err != nil {
		return nil, err
	}
	if onFailure > DumpOnFailure {
		return nil, fmt.Errorf("gov8: invalid JavaScript execution failure mode %d", onFailure)
	}
	h, err := callHandle("DisallowJavascriptExecutionScope.New",
		proc("gov8_csa_disallow_javascript_new"),
		s.iso.handleAssumingCheck(), uintptr(onFailure))
	if err != nil {
		return nil, err
	}
	if s.javascriptExecutionGuards == nil {
		s.javascriptExecutionGuards = new([]uintptr)
	}
	*s.javascriptExecutionGuards = append(*s.javascriptExecutionGuards, h)
	return &DisallowJavascriptExecutionScope{scope: s, handle: h}, nil
}

func (d *DisallowJavascriptExecutionScope) checkCurrent() error {
	if err := d.scope.iso.check(); err != nil {
		return err
	}
	if d.closed {
		return fmt.Errorf("gov8: disallow JavaScript execution scope used after Close")
	}
	if d.scope.closed {
		return fmt.Errorf("gov8: owning scope used after Close")
	}
	var stack []uintptr
	if d.scope.javascriptExecutionGuards != nil {
		stack = *d.scope.javascriptExecutionGuards
	}
	if len(stack) == 0 || stack[len(stack)-1] != d.handle {
		return fmt.Errorf("gov8: JavaScript execution scopes must close in LIFO order")
	}
	return nil
}

// NewAllowJavascriptExecutionScope temporarily permits JavaScript inside this
// currently active disallow guard.
func (d *DisallowJavascriptExecutionScope) NewAllowJavascriptExecutionScope() (*AllowJavascriptExecutionScope, error) {
	if err := d.checkCurrent(); err != nil {
		return nil, err
	}
	h, err := callHandle("AllowJavascriptExecutionScope.New",
		proc("gov8_csa_allow_javascript_new"),
		d.scope.iso.handleAssumingCheck())
	if err != nil {
		return nil, err
	}
	*d.scope.javascriptExecutionGuards = append(*d.scope.javascriptExecutionGuards, h)
	return &AllowJavascriptExecutionScope{scope: d.scope, handle: h}, nil
}

// Close restores the execution state that preceded the disallow guard.
func (d *DisallowJavascriptExecutionScope) Close() error {
	if err := d.checkCurrent(); err != nil {
		return err
	}
	r1, _, _ := proc("gov8_csa_disallow_javascript_dispose").Call(d.handle)
	if int64(r1) < 0 {
		return shimError("DisallowJavascriptExecutionScope.Close", r1)
	}
	stack := *d.scope.javascriptExecutionGuards
	*d.scope.javascriptExecutionGuards = stack[:len(stack)-1]
	d.closed = true
	return nil
}

// AllowJavascriptExecutionScope is a nested lexical exception to a disallow
// guard. It must close before its parent guard.
type AllowJavascriptExecutionScope struct {
	scope  *Scope
	handle uintptr
	closed bool
}

func (a *AllowJavascriptExecutionScope) checkCurrent() error {
	if err := a.scope.iso.check(); err != nil {
		return err
	}
	if a.closed {
		return fmt.Errorf("gov8: allow JavaScript execution scope used after Close")
	}
	if a.scope.closed {
		return fmt.Errorf("gov8: owning scope used after Close")
	}
	var stack []uintptr
	if a.scope.javascriptExecutionGuards != nil {
		stack = *a.scope.javascriptExecutionGuards
	}
	if len(stack) == 0 || stack[len(stack)-1] != a.handle {
		return fmt.Errorf("gov8: JavaScript execution scopes must close in LIFO order")
	}
	return nil
}

// Close restores the enclosing disallow state.
func (a *AllowJavascriptExecutionScope) Close() error {
	if err := a.checkCurrent(); err != nil {
		return err
	}
	r1, _, _ := proc("gov8_csa_allow_javascript_dispose").Call(a.handle)
	if int64(r1) < 0 {
		return shimError("AllowJavascriptExecutionScope.Close", r1)
	}
	stack := *a.scope.javascriptExecutionGuards
	*a.scope.javascriptExecutionGuards = stack[:len(stack)-1]
	a.closed = true
	return nil
}
