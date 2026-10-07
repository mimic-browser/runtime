package browser

import (
	"context"
	"fmt"
	"time"

	"github.com/moreveal/mimic/internal/engine"
	"github.com/moreveal/mimic/internal/scheduler"
)

// A public timer ID identifies its registration, including every interval
// tick. Scheduler task IDs change when a repeating registration is requeued.
type windowTimer struct {
	taskID uint64
	active bool
	timerInvocation
}

func (r *Realm) hostTimer(_ engine.Value, args []engine.Value) (engine.Value, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("timer callback required")
	}
	delay := time.Duration(numarg(args, 1)) * time.Millisecond
	repeat, _ := arg(args, 2).(bool)
	if r.timers == nil {
		r.timers = make(map[uint64]*windowTimer)
	}
	invocation, err := newTimerInvocation(r.runtime, args)
	if err != nil {
		return nil, err
	}
	registration := &windowTimer{active: true, timerInvocation: invocation}
	release := func() {
		registration.release(r.runtime)
	}
	var id uint64
	var callback func(context.Context) error
	callback = func(ctx context.Context) error {
		if !registration.active {
			return nil
		}
		if !repeat {
			registration.active = false
			delete(r.timers, id)
		}
		p := r.agent.Page()
		p.userScriptDepth++
		defer func() { p.userScriptDepth-- }()
		invoke := func(ctx context.Context) error {
			receiver := r.runtime.Get("window")
			err := registration.invoke(ctx, r.runtime, receiver, r.trustedTypesState().EvalBlocked != "")
			releaseRuntimeValues(r.runtime, receiver)
			if !repeat || err != nil || !registration.active {
				release()
			}
			return err
		}
		var err error
		if owner, ok := r.runtime.(engine.OwnerRuntime); ok {
			err = owner.RunOnOwner(ctx, invoke)
		} else {
			err = invoke(ctx)
		}
		if err != nil {
			registration.active = false
			delete(r.timers, id)
			// Owner dispatch can fail before invoke starts (for example when
			// the task context expires), so cleanup must cover that path too.
			release()
		}
		if err == nil && repeat && registration.active {
			registration.taskID = r.scheduler.Post(scheduler.Timer, delay, callback)
		}
		return err
	}
	id = r.scheduler.Post(scheduler.Timer, delay, callback)
	registration.taskID = id
	r.timers[id] = registration
	// Window timer handles are Web IDL numbers, including in runtimes whose
	// uint64 marshaling otherwise produces a JavaScript BigInt.
	return r.val(float64(id)), nil
}

func (r *Realm) hostClearTimer(_ engine.Value, args []engine.Value) (engine.Value, error) {
	id := uint64(numarg(args, 0))
	if registration := r.timers[id]; registration != nil {
		registration.active = false
		r.scheduler.Cancel(registration.taskID)
		delete(r.timers, id)
		registration.release(r.runtime)
	}
	return nil, nil
}
