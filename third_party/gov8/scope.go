//go:build (windows || linux) && amd64

package gov8

import (
	"fmt"
	syscall "github.com/maclof/gov8/internal/native"
	"sync"
	"unsafe"
)

var (
	scopeHotProcsOnce sync.Once
	scopeEnterAddr    uintptr
	scopeExitAddr     uintptr
)

func ensureScopeHotProcs() {
	scopeHotProcsOnce.Do(func() {
		scopeEnterAddr = proc("gov8_scope_enter").Addr()
		scopeExitAddr = proc("gov8_scope_exit").Addr()
	})
}

// Scope owns a v8 HandleScope. Every local handle (Value) created through a
// Scope lives in that scope's slot storage and becomes invalid once the
// scope is closed; the Go wrapper enforces this by refusing to operate on
// values whose scope is closed. Open scopes per isolate must strictly nest
// (that is a V8 rule) and must be closed on the isolate's owning thread.
type Scope struct {
	iso                         *Isolate
	handle                      uintptr
	closed                      bool
	borrowed                    bool
	activeBorrowedContextScopes uint32
	javascriptExecutionGuards   *[]uintptr
}

// The Isolate-owned handleScopeStack mirrors the user-visible HandleScope
// nesting that V8 requires. It includes ordinary and escapable scopes.
// Borrowed callback scopes are already created as the active native scope and
// opt out through Scope.borrowed.
//
// Access is deliberately lock-free: all callers have already validated the
// isolate's owner thread. Keeping the empty slice on the isolate also reuses
// its backing storage across sequential scopes, which avoids allocating a new
// one-element stack for every operation. Isolate.Close clears and releases the
// storage after native teardown.

func pushHandleScope(iso *Isolate, scope any) {
	iso.handleScopeStack = append(iso.handleScopeStack, scope)
}

func currentHandleScope(iso *Isolate, scope any) bool {
	stack := iso.handleScopeStack
	return len(stack) != 0 && stack[len(stack)-1] == scope
}

func currentHandleScopeToken(iso *Isolate) any {
	stack := iso.handleScopeStack
	if len(stack) == 0 {
		return nil
	}
	return stack[len(stack)-1]
}

func popHandleScope(iso *Isolate, scope any) error {
	stack := iso.handleScopeStack
	if len(stack) == 0 || stack[len(stack)-1] != scope {
		return fmt.Errorf("gov8: handle scope is not the current innermost scope")
	}
	last := len(stack) - 1
	stack[last] = nil // do not retain a closed scope in reusable capacity
	iso.handleScopeStack = stack[:last]
	return nil
}

func (i *Isolate) clearHandleScopeStack() {
	for index := range i.handleScopeStack {
		i.handleScopeStack[index] = nil
	}
	i.handleScopeStack = nil
}

// NewScope opens a new HandleScope on the isolate.
func (i *Isolate) NewScope() (*Scope, error) {
	h, err := i.handleChecked()
	if err != nil {
		return nil, err
	}
	ensureScopeHotProcs()
	sh, _, _ := syscall.Syscall(scopeEnterAddr, 1, h, 0, 0)
	if sh == 0 {
		return nil, shimError("Scope.New", 0)
	}
	scope := &Scope{iso: i, handle: sh}
	pushHandleScope(i, scope)
	return scope, nil
}

// check validates the scope's own state and its isolate's thread affinity.
// The affinity check runs first so wrong-thread misuse returns before any
// scope-local state (written by the owning thread) is read.
func (s *Scope) check() error {
	if err := s.iso.check(); err != nil {
		return err
	}
	if s.closed {
		return fmt.Errorf("gov8: scope used after Close")
	}
	return nil
}

func (s *Scope) checkedHandle() (uintptr, error) {
	if err := s.check(); err != nil {
		return 0, err
	}
	return s.handle, nil
}

// checkedHandleAssumingIsolate returns the scope's handle for callers that
// already proved the isolate's lifecycle state and thread affinity in the
// same operation (Scope.check transit reach the isolate check; once that has
// passed, re-running it is redundant — the owner thread cannot interleave
// with itself — so only the scope-local closed flag still carries
// information here).
func (s *Scope) checkedHandleAssumingIsolate() (uintptr, error) {
	if s.closed {
		return 0, fmt.Errorf("gov8: scope used after Close")
	}
	return s.handle, nil
}

// requireCurrent rejects a live but non-innermost user scope before an API
// allocates locals and attributes them to that scope. Borrowed callback scopes
// are constructed around the native current scope and are trusted here.
func (s *Scope) requireCurrent() error {
	if s.borrowed || currentHandleScope(s.iso, s) {
		return nil
	}
	return fmt.Errorf("gov8: scope is not the current innermost handle scope")
}

// Close closes the HandleScope. All Values created through this scope must
// no longer be used afterwards.
func (s *Scope) Close() error {
	if err := s.iso.check(); err != nil {
		return err
	}
	if s.closed {
		return fmt.Errorf("gov8: scope already closed")
	}
	if s.borrowed {
		return fmt.Errorf("gov8: borrowed callback scope cannot be closed")
	}
	if s.javascriptExecutionGuards != nil && len(*s.javascriptExecutionGuards) != 0 {
		return fmt.Errorf("gov8: scope has active JavaScript execution guards")
	}
	if s.activeBorrowedContextScopes != 0 {
		return fmt.Errorf("gov8: scope has an active borrowed context scope")
	}
	if err := s.requireCurrent(); err != nil {
		return err
	}
	if err := requireInitialized(); err != nil {
		return err
	}
	ensureScopeHotProcs()
	r1, _, _ := syscall.Syscall(scopeExitAddr, 1, s.handle, 0, 0)
	s.closed = true
	if err := popHandleScope(s.iso, s); err != nil {
		return err
	}
	if int64(r1) < 0 {
		return shimError("Scope.Close", r1)
	}
	return nil
}

// Context is a persistent execution context (a rooted v8::Global<Context>).
// It is valid until Close and, like everything else, only usable on the
// isolate's owning thread.
type Context struct {
	iso            *Isolate
	handle         uintptr
	closed         bool
	microtaskQueue *MicrotaskQueue
	hostCallbacks  []uint64
}

// NewContext creates a default context on the isolate. A context is
// engine-persistent; it does not require a Scope to create or keep alive.
func (i *Isolate) NewContext() (*Context, error) {
	ih, err := i.handleChecked()
	if err != nil {
		return nil, err
	}
	ch, err := callHandle("Context.New", proc("gov8_context_new"), ih)
	if err != nil {
		return nil, err
	}
	i.mu.Lock()
	i.contextsCreated = true
	i.mu.Unlock()
	return &Context{iso: i, handle: ch}, nil
}

// check validates the context's own state and its isolate's thread
// affinity; affinity first, so wrong-thread misuse returns before the
// context-local closed flag is read.
func (c *Context) check() error {
	if err := c.iso.check(); err != nil {
		return err
	}
	if c.closed {
		return fmt.Errorf("gov8: context used after Close")
	}
	return nil
}

// checkAssumingIsolate validates the context's closed flag for callers that
// already proved the isolate's lifecycle state and thread affinity in the
// same operation (Context.check transits the isolate check; once that has
// passed, re-running it is redundant — the owner thread cannot interleave
// with itself — so only the context-local closed flag still carries
// information here).
func (c *Context) checkAssumingIsolate() error {
	if c.closed {
		return fmt.Errorf("gov8: context used after Close")
	}
	return nil
}

// Close releases the context. It must not be used afterwards.
func (c *Context) Close() error {
	if err := c.iso.check(); err != nil {
		return err
	}
	if c.closed {
		return fmt.Errorf("gov8: context already closed")
	}
	// V8Inspector retains the registered context. It must be unregistered
	// explicitly while both the context and isolate are still alive.
	if err := inspectorContextCloseError(c); err != nil {
		return err
	}
	if err := requireInitialized(); err != nil {
		return err
	}
	r1, _, _ := proc("gov8_context_dispose").Call(c.handle)
	c.closed = true
	if c.microtaskQueue != nil {
		c.microtaskQueue.attachments--
		c.microtaskQueue = nil
	}
	if int64(r1) < 0 {
		return shimError("Context.Close", r1)
	}
	for _, handle := range c.hostCallbacks {
		dropHostCallback(handle)
	}
	c.hostCallbacks = nil
	return nil
}

// Object is a value known to be a JS object (used for the global object).
type Object struct {
	Value
}

// GlobalObject returns the context's global object as a scope-local value.
// The scope must belong to the same isolate as the context.
func (c *Context) GlobalObject(s *Scope) (*Object, error) {
	// c.check proves isolate state and affinity for this operation, so the
	// scope's and context's own checks below skip the (identical, and on
	// this thread immutable) isolate validation the old code re-ran twice.
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
	r1, _, _ := proc("gov8_context_global_object").Call(
		c.iso.handleAssumingCheck(), c.handle, sh, uintptr(unsafe.Pointer(&out)))
	if int64(r1) < 0 {
		return nil, shimError("Context.GlobalObject", r1)
	}
	return &Object{Value{iso: c.iso, sc: s, h: out}}, nil
}
