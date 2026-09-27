package browser

import (
	"context"
	"fmt"
	"time"

	"github.com/moreveal/mimic/internal/engine"
	"github.com/moreveal/mimic/internal/scheduler"
)

// Like Window timers, an interval keeps its public registration ID while its
// scheduler task changes on every tick. Only the callback outlives registration.
type workerTimer struct {
	taskID uint64
	active bool
	timerInvocation
}

func (w *DedicatedWorker) hostTimer(_ engine.Value, args []engine.Value) (engine.Value, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("timer callback required")
	}
	delay := time.Duration(numarg(args, 1)) * time.Millisecond
	repeat, _ := arg(args, 2).(bool)
	if w.timers == nil {
		w.timers = make(map[uint64]*workerTimer)
	}
	invocation, err := newTimerInvocation(w.runtime, args)
	if err != nil {
		return nil, err
	}
	registration := &workerTimer{active: true, timerInvocation: invocation}
	release := func() {
		registration.release(w.runtime)
	}
	var id uint64
	var callback scheduler.Callback
	callback = func(ctx context.Context) error {
		if !registration.active {
			return nil
		}
		if !repeat {
			registration.active = false
			delete(w.timers, id)
		}
		invoke := func(ctx context.Context) error {
			receiver := w.runtime.Get("self")
			err := registration.invoke(ctx, w.runtime, receiver, w.policy.TrustedTypes().EvalBlocked != "")
			releaseRuntimeValues(w.runtime, receiver)
			if !repeat || err != nil || !registration.active || w.isClosed() {
				release()
			}
			return err
		}
		var err error
		if owner, ok := w.runtime.(engine.OwnerRuntime); ok {
			err = owner.RunOnOwner(ctx, invoke)
		} else {
			err = invoke(ctx)
		}
		if err != nil || w.isClosed() {
			registration.active = false
			delete(w.timers, id)
			release()
		}
		if err == nil && repeat && registration.active {
			registration.taskID = w.scheduler.Post(scheduler.Timer, delay, callback)
		}
		return err
	}
	id = w.scheduler.Post(scheduler.Timer, delay, callback)
	registration.taskID = id
	w.timers[id] = registration
	w.signal()
	return w.runtime.Value(id), nil
}

func (w *DedicatedWorker) hostClearTimer(_ engine.Value, args []engine.Value) (engine.Value, error) {
	id := uint64(numarg(args, 0))
	if registration := w.timers[id]; registration != nil {
		registration.active = false
		w.scheduler.Cancel(registration.taskID)
		delete(w.timers, id)
		registration.release(w.runtime)
	}
	return nil, nil
}
