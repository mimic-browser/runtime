//go:build (windows || linux) && amd64

package gov8

import (
	"fmt"
)

// MicrotaskQueue is a native microtask queue (v8::MicrotaskQueue). The
// engine object is owned by the pinned artifact's MicrotaskQueueHandle
// binding; the Go wrapper holds the wrapper pointer and the raw queue
// pointer (used for identity comparisons after attaching to a context).
type MicrotaskQueue struct {
	iso         *Isolate
	handle      uintptr // MicrotaskQueueHandle
	raw         uintptr // MicrotaskQueue*
	closed      bool
	attachments int
}

// NewMicrotaskQueue creates a native queue with the given policy.
func (i *Isolate) NewMicrotaskQueue(policy MicrotasksPolicy) (*MicrotaskQueue, error) {
	if policy > PolicyExplicit {
		return nil, fmt.Errorf("gov8: invalid microtasks policy %d", policy)
	}
	ih, err := i.handleChecked()
	if err != nil {
		return nil, err
	}
	h, err := callHandle("MicrotaskQueue.New",
		proc("gov8_microtask_queue_new"), ih, uintptr(policy))
	if err != nil {
		return nil, err
	}
	raw, err := callHandle("MicrotaskQueue.Raw", proc("gov8_microtask_queue_raw"), h)
	if err != nil {
		_, _, _ = proc("gov8_microtask_queue_dispose").Call(h)
		return nil, err
	}
	return &MicrotaskQueue{iso: i, handle: h, raw: raw}, nil
}

// check validates the queue's own state and its isolate's thread affinity;
// affinity first, so wrong-thread misuse returns before the queue-local
// closed flag is read.
func (m *MicrotaskQueue) check() error {
	if err := m.iso.check(); err != nil {
		return err
	}
	if m.closed {
		return fmt.Errorf("gov8: microtask queue used after Close")
	}
	return nil
}

// Raw returns the underlying v8::MicrotaskQueue pointer for identity
// comparison against Context.GetMicrotaskQueue.
func (m *MicrotaskQueue) Raw() (uintptr, error) {
	if err := m.check(); err != nil {
		return 0, err
	}
	return m.raw, nil
}

// PerformCheckpoint runs all queued microtasks (draining nested jobs). The
// context in which the checkpoint was logically taken may be supplied and is
// entered for the duration, mirroring the oracle's long-lived ContextScope
// around checkpoints; pass nil for none. The context must belong to the same
// isolate as the queue.
func (m *MicrotaskQueue) PerformCheckpoint(c *Context) error {
	if err := m.check(); err != nil {
		return err
	}
	if err := requireInitialized(); err != nil {
		return err
	}
	ih := m.iso.handleAssumingCheck()
	var ctxH uintptr
	if c != nil {
		// m.check proved the isolate's state and affinity for this
		// operation, so the context check only inspects its own closed
		// flag.
		if err := c.checkAssumingIsolate(); err != nil {
			return err
		}
		if c.iso != m.iso {
			return foreignIsolate("context")
		}
		ctxH = c.handle
	}
	r1, _, _ := proc("gov8_microtask_queue_perform_checkpoint").Call(ih, m.handle, ctxH)
	if int64(r1) < 0 {
		return shimError("MicrotaskQueue.PerformCheckpoint", r1)
	}
	return nil
}

// Enqueue adds a JS function to the queue through the native API. The
// context may be supplied and is entered for the duration (mirroring the
// oracle); pass nil for none. The function value and the context must belong
// to the same isolate as the queue.
func (m *MicrotaskQueue) Enqueue(c *Context, fn Value) error {
	if err := m.check(); err != nil {
		return err
	}
	if err := fn.check(); err != nil {
		return err
	}
	if fn.iso != m.iso {
		return foreignIsolate("value")
	}
	if err := requireInitialized(); err != nil {
		return err
	}
	ih := m.iso.handleAssumingCheck()
	var ctxH uintptr
	if c != nil {
		// m.check proved the isolate's state and affinity for this
		// operation, so the context check only inspects its own closed
		// flag.
		if err := c.checkAssumingIsolate(); err != nil {
			return err
		}
		if c.iso != m.iso {
			return foreignIsolate("context")
		}
		ctxH = c.handle
	}
	r1, _, _ := proc("gov8_microtask_queue_enqueue").Call(ih, m.handle, ctxH, fn.h)
	if int64(r1) < 0 {
		return shimError("MicrotaskQueue.Enqueue", r1)
	}
	return nil
}

// Close releases the queue. A queue still attached to a context must be
// detached (or the context closed) first by the caller; V8 keeps the
// context's pointer valid until the context dies.
func (m *MicrotaskQueue) Close() error {
	if err := m.iso.check(); err != nil {
		return err
	}
	if m.closed {
		return fmt.Errorf("gov8: microtask queue already closed")
	}
	if m.attachments != 0 {
		return fmt.Errorf("gov8: microtask queue is attached to %d context(s)", m.attachments)
	}
	if err := requireInitialized(); err != nil {
		return err
	}
	r1, _, _ := proc("gov8_microtask_queue_dispose").Call(m.handle)
	m.closed = true
	if int64(r1) < 0 {
		return shimError("MicrotaskQueue.Close", r1)
	}
	return nil
}

// SetMicrotasksPolicy sets the isolate-level microtasks policy.
func (i *Isolate) SetMicrotasksPolicy(p MicrotasksPolicy) error {
	if p > PolicyExplicit {
		return fmt.Errorf("gov8: invalid microtasks policy %d", p)
	}
	ih, err := i.handleChecked()
	if err != nil {
		return err
	}
	r1, _, _ := proc("gov8_isolate_set_microtasks_policy").Call(ih, uintptr(p))
	if int64(r1) < 0 {
		return shimError("SetMicrotasksPolicy", r1)
	}
	return nil
}

// GetMicrotasksPolicy reports the isolate-level microtasks policy.
func (i *Isolate) GetMicrotasksPolicy() (MicrotasksPolicy, error) {
	ih, err := i.handleChecked()
	if err != nil {
		return 0, err
	}
	r1, _, _ := proc("gov8_isolate_get_microtasks_policy").Call(ih)
	if int64(r1) < 0 {
		return 0, shimError("GetMicrotasksPolicy", r1)
	}
	return MicrotasksPolicy(r1), nil
}

// PerformMicrotaskCheckpoint drains the isolate's default microtask queue.
func (i *Isolate) PerformMicrotaskCheckpoint() error {
	ih, err := i.handleChecked()
	if err != nil {
		return err
	}
	r1, _, _ := proc("gov8_isolate_perform_microtask_checkpoint").Call(ih)
	if int64(r1) < 0 {
		return shimError("PerformMicrotaskCheckpoint", r1)
	}
	return nil
}

// SetMicrotaskQueue attaches a native queue to the context (replacing the
// isolate default). The queue must belong to the same isolate as the
// context.
func (c *Context) SetMicrotaskQueue(m *MicrotaskQueue) error {
	if err := c.check(); err != nil {
		return err
	}
	if m == nil {
		return fmt.Errorf("gov8: nil microtask queue")
	}
	// c.check proved the isolate's state and affinity for this operation,
	// so the queue check only inspects its own closed flag.
	if m.closed {
		return fmt.Errorf("gov8: microtask queue used after Close")
	}
	if m.iso != c.iso {
		return foreignIsolate("microtask queue")
	}
	r1, _, _ := proc("gov8_context_set_microtask_queue").Call(c.handle, m.handle)
	if int64(r1) < 0 {
		return shimError("Context.SetMicrotaskQueue", r1)
	}
	if c.microtaskQueue != m {
		if c.microtaskQueue != nil {
			c.microtaskQueue.attachments--
		}
		m.attachments++
		c.microtaskQueue = m
	}
	return nil
}

// GetMicrotaskQueue returns the raw pointer of the context's attached queue
// (0 when none is attached).
func (c *Context) GetMicrotaskQueue() (uintptr, error) {
	if err := c.check(); err != nil {
		return 0, err
	}
	// The shim returns null for "no queue"; misuse errors are already
	// excluded by the context check above.
	r1, _, _ := proc("gov8_context_get_microtask_queue").Call(c.handle)
	return r1, nil
}

// DisableMicrotasks permanently shuts down this context's native promise-job
// admission without detaching its global or disposing retained objects. Call
// only at an idle boundary; other contexts on the isolate retain their queue.
func (c *Context) DisableMicrotasks() error {
	if err := c.check(); err != nil {
		return err
	}
	r1, _, _ := proc("gov8_context_set_microtask_queue").Call(c.handle, 0)
	if int64(r1) < 0 {
		return shimError("Context.DisableMicrotasks", r1)
	}
	if c.microtaskQueue != nil {
		c.microtaskQueue.attachments--
		c.microtaskQueue = nil
	}
	return nil
}
