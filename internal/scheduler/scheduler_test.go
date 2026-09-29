package scheduler

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRunReadyStepIncludesCheckpointAndYields(t *testing.T) {
	var order []string
	s := New(time.Unix(0, 0), func(context.Context) error {
		order = append(order, "microtask")
		return nil
	})
	s.Post(Timer, 0, func(context.Context) error {
		order = append(order, "first")
		s.Post(Timer, 0, func(context.Context) error { order = append(order, "second"); return nil })
		return nil
	})
	if worked, err := s.RunReadyStep(context.Background()); !worked || err != nil {
		t.Fatalf("step: %v %v", worked, err)
	}
	if !reflect.DeepEqual(order, []string{"first", "microtask"}) {
		t.Fatal(order)
	}
	if worked, err := s.RunReadyStep(context.Background()); !worked || err != nil {
		t.Fatalf("second step: %v %v", worked, err)
	}
	if !reflect.DeepEqual(order, []string{"first", "microtask", "second", "microtask"}) {
		t.Fatal(order)
	}
	s.Post(Timer, time.Hour, func(context.Context) error { t.Fatal("future timer fired"); return nil })
	if worked, err := s.RunReadyStep(context.Background()); worked || err != nil {
		t.Fatalf("future work: %v %v", worked, err)
	}
}

func TestOverdueAuthorTimerDoesNotOvertakeQueuedRendering(t *testing.T) {
	s := New(time.Unix(0, 0), nil)
	var order []string
	s.Post(Timer, 0, func(context.Context) error {
		// A busy frame can make an author timer ready before it queues delivery.
		s.AdvanceBy(80 * time.Millisecond)
		s.Post(Rendering, 0, func(context.Context) error {
			order = append(order, "rendering")
			return nil
		})
		return nil
	})
	s.Post(Timer, 80*time.Millisecond, func(context.Context) error {
		order = append(order, "timer")
		return nil
	})
	if err := s.RunUntilIdle(context.Background(), 4); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(order, []string{"rendering", "timer"}) {
		t.Fatal(order)
	}
}

func TestCancelledRunDoesNotConsumeQueuedTask(t *testing.T) {
	s := New(time.Unix(0, 0), nil)
	ran := false
	s.Post(Timer, 0, func(context.Context) error { ran = true; return nil })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.RunReadyStep(ctx); err == nil {
		t.Fatal("cancelled run succeeded")
	}
	if ran {
		t.Fatal("cancelled run executed task")
	}
	if progress, err := s.RunReadyStep(context.Background()); err != nil || !progress || !ran {
		t.Fatalf("queued task lost: %v, %v", progress, err)
	}
}

func TestExecutionStatusReportsActiveTask(t *testing.T) {
	s := New(time.Unix(0, 0), nil)
	started, release := make(chan struct{}), make(chan struct{})
	taskID := s.Post(Network, 0, func(context.Context) error {
		close(started)
		<-release
		return nil
	})
	done := make(chan error, 1)
	go func() {
		_, err := s.RunReadyStep(context.Background())
		done <- err
	}()
	<-started
	status := s.ExecutionStatus()
	if !status.Running || status.TaskID != taskID || status.Source != Network || status.Phase != "callback" {
		t.Fatalf("unexpected execution status: %+v", status)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if status := s.ExecutionStatus(); status.Running {
		t.Fatalf("task remained active: %+v", status)
	}
}

func TestRunReadyAcrossPreservesOrderWithMultipleParentTasks(t *testing.T) {
	start := time.Unix(0, 0)
	parent, child := New(start, nil), New(start, nil)
	var order []string
	parent.Post(Control, 0, func(context.Context) error { order = append(order, "control"); return nil })
	parent.Post(PostedMessage, 0, func(context.Context) error { order = append(order, "message"); return nil })
	child.Post(Timer, time.Millisecond, func(context.Context) error { order = append(order, "timer"); return nil })
	parent.AdvanceBy(time.Millisecond)
	child.AdvanceBy(time.Millisecond)
	for i := 0; i < 3; i++ {
		if worked, err := RunReadyAcross(context.Background(), []*Scheduler{parent, child}); !worked || err != nil {
			t.Fatalf("step %d: %v %v", i, worked, err)
		}
	}
	if !reflect.DeepEqual(order, []string{"control", "message", "timer"}) {
		t.Fatal(order)
	}
}

func TestRunReadyAcrossUsesSharedSequence(t *testing.T) {
	start := time.Unix(0, 0)
	parent, child := New(start, nil), New(start, nil)
	var sequence uint64
	next := func() uint64 { sequence++; return sequence }
	parent.SetSequenceSource(next)
	child.SetSequenceSource(next)
	// Local IDs differ, while the due times tie. A child's smaller local ID
	// must not let it overtake a message posted earlier in the Page.
	parent.Post(Control, time.Hour, func(context.Context) error { return nil })
	var order []string
	parent.Post(PostedMessage, 0, func(context.Context) error { order = append(order, "message"); return nil })
	child.Post(Timer, 0, func(context.Context) error { order = append(order, "timer"); return nil })
	for i := 0; i < 2; i++ {
		if worked, err := RunReadyAcross(context.Background(), []*Scheduler{child, parent}); !worked || err != nil {
			t.Fatalf("step %d: %v %v", i, worked, err)
		}
	}
	if !reflect.DeepEqual(order, []string{"message", "timer"}) {
		t.Fatal(order)
	}
}

func TestRunReadyAcrossSynchronizesRealmClocks(t *testing.T) {
	start := time.Unix(0, 0)
	parent, child := New(start, nil), New(start, nil)
	parent.AdvanceBy(20 * time.Millisecond)
	var observed time.Time
	child.Post(Timer, 10*time.Millisecond, func(context.Context) error {
		observed = child.Now()
		return nil
	})
	if worked, err := RunReadyAcross(context.Background(), []*Scheduler{parent, child}); !worked || err != nil {
		t.Fatalf("shared elapsed time: %v %v", worked, err)
	}
	if observed.Before(parent.Now()) {
		t.Fatalf("child clock %v precedes parent %v", observed, parent.Now())
	}
}

func TestDeterministicOrdering(t *testing.T) {
	s := New(time.Unix(0, 0), nil)
	var got []int
	s.Post(Timer, time.Second, func(context.Context) error { got = append(got, 2); return nil })
	s.Post(DOM, 0, func(context.Context) error { got = append(got, 1); return nil })
	if err := s.RunUntilIdle(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []int{1, 2}) {
		t.Fatal(got)
	}
}

func TestReadyNavigationOvertakesQueuedDocumentWork(t *testing.T) {
	s := New(time.Unix(0, 0), nil)
	var order []string
	s.Post(DOM, 0, func(context.Context) error { order = append(order, "dom"); return nil })
	s.Post(ResourceScript, 0, func(context.Context) error {
		order = append(order, "script")
		return nil
	})
	s.Post(Navigation, 0, func(context.Context) error {
		order = append(order, "navigation")
		return nil
	})
	if err := s.RunUntilIdle(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(order, []string{"navigation", "script", "dom"}) {
		t.Fatal(order)
	}
}

func TestFutureNavigationDoesNotOvertakeReadyDocumentWork(t *testing.T) {
	s := New(time.Unix(0, 0), nil)
	var order []string
	s.Post(DOM, 0, func(context.Context) error { order = append(order, "dom"); return nil })
	s.Post(Navigation, time.Millisecond, func(context.Context) error {
		order = append(order, "navigation")
		return nil
	})
	if worked, err := s.RunReadyStep(context.Background()); err != nil || !worked {
		t.Fatalf("ready DOM task: %v %v", worked, err)
	}
	if !reflect.DeepEqual(order, []string{"dom"}) {
		t.Fatal(order)
	}
}

func TestWaitAnyWakesForAnotherQueue(t *testing.T) {
	queues := []*Scheduler{New(time.Unix(0, 0), nil), New(time.Unix(0, 0), nil)}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- WaitAny(ctx, queues) }()
	queues[1].Post(Network, 0, func(context.Context) error { return nil })
	if err := <-done; err != nil {
		t.Fatalf("other queue did not wake wait: %v", err)
	}
}

func TestWaitAnyUsesEarliestTimerAndAdvancesAllClocks(t *testing.T) {
	start := time.Unix(0, 0)
	first, second := New(start, nil), New(start, nil)
	firstRan, secondRan := false, false
	first.Post(Timer, time.Hour, func(context.Context) error { firstRan = true; return nil })
	second.Post(Timer, 10*time.Millisecond, func(context.Context) error { secondRan = true; return nil })
	// Consume enqueue notifications so this wait is driven by the due timer.
	<-first.wake
	<-second.wake
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := WaitAny(ctx, []*Scheduler{first, second}); err != nil {
		t.Fatal(err)
	}
	if !first.Now().Equal(second.Now()) || first.Now().Sub(start) < 10*time.Millisecond {
		t.Fatalf("clocks diverged: %v %v", first.Now(), second.Now())
	}
	if err := first.RunReady(ctx, 10); err != nil {
		t.Fatal(err)
	}
	if err := second.RunReady(ctx, 10); err != nil {
		t.Fatal(err)
	}
	if firstRan || !secondRan {
		t.Fatalf("wrong due task: first=%v second=%v", firstRan, secondRan)
	}
}

func TestWaitAnyCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := WaitAny(ctx, []*Scheduler{New(time.Unix(0, 0), nil)}); err != context.Canceled {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestWaitAnyWakeDoesNotAdvanceToDeadline(t *testing.T) {
	start := time.Unix(0, 0)
	s := New(start, nil)
	called := false
	s.Post(Timer, time.Hour, func(context.Context) error { called = true; return nil })
	// Leave the enqueue notification pending: this wake must not be mistaken
	// for expiration of the timer that WaitAny also registers.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := WaitAny(ctx, []*Scheduler{s}); err != nil {
		t.Fatal(err)
	}
	if s.Now().Sub(start) >= time.Hour {
		t.Fatal("enqueue wake advanced the clock to the future timer")
	}
	if err := s.RunReady(ctx, 10); err != nil || called {
		t.Fatalf("future timer ran after enqueue wake: called=%v err=%v", called, err)
	}
}

func TestRunReadyDoesNotFastForwardTimers(t *testing.T) {
	s := New(time.Unix(0, 0), nil)
	called := false
	s.Post(Timer, time.Second, func(context.Context) error { called = true; return nil })
	if err := s.RunReady(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	if called || !s.Now().Equal(time.Unix(0, 0)) {
		t.Fatalf("future timer ran during ready-task checkpoint: called=%v now=%v", called, s.Now())
	}
	if err := s.RunUntilIdle(context.Background(), 10); err != nil || !called {
		t.Fatalf("timer did not run when virtual time advanced: called=%v error=%v", called, err)
	}
}

func TestAdvanceMakesTimerReady(t *testing.T) {
	s := New(time.Unix(0, 0), nil)
	called := false
	s.Post(Timer, 25*time.Millisecond, func(context.Context) error { called = true; return nil })
	s.AdvanceBy(24 * time.Millisecond)
	if err := s.RunReady(context.Background(), 10); err != nil || called {
		t.Fatalf("timer became ready too early: called=%v error=%v", called, err)
	}
	s.AdvanceBy(time.Millisecond)
	if err := s.RunReady(context.Background(), 10); err != nil || !called {
		t.Fatalf("timer did not become ready: called=%v error=%v", called, err)
	}
}

func TestClockAdvancesWhileJavaScriptTaskRuns(t *testing.T) {
	s := New(time.Unix(0, 0), nil)
	var elapsed time.Duration
	s.Post(DOM, 0, func(context.Context) error {
		start := s.Now()
		for elapsed < 5*time.Millisecond {
			elapsed = s.Now().Sub(start)
		}
		return nil
	})
	if err := s.RunReady(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	if elapsed < 5*time.Millisecond || s.Now().Before(time.Unix(0, 0).Add(elapsed)) {
		t.Fatalf("clock froze inside task: elapsed=%v now=%v", elapsed, s.Now())
	}
}

func TestClockAdvancesDuringMicrotaskCheckpoint(t *testing.T) {
	start := time.Unix(0, 0)
	var s *Scheduler
	var elapsed time.Duration
	s = New(start, func(context.Context) error {
		for elapsed < 5*time.Millisecond {
			elapsed = s.Now().Sub(start)
		}
		return nil
	})
	s.Post(DOM, 0, func(context.Context) error { return nil })
	if err := s.RunReady(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	if elapsed < 5*time.Millisecond || s.Now().Before(start.Add(elapsed)) {
		t.Fatalf("clock froze during microtask checkpoint: elapsed=%v now=%v", elapsed, s.Now())
	}
}

func TestReadyScriptFetchStartsBeforePassiveResourceFetch(t *testing.T) {
	s := New(time.Unix(0, 0), nil)
	var got []string
	s.Post(ResourceLow, 0, func(context.Context) error { got = append(got, "image"); return nil })
	s.Post(ResourceScript, 0, func(context.Context) error { got = append(got, "script"); return nil })
	if err := s.RunReady(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"script", "image"}) {
		t.Fatalf("resource fetch-start priority mismatch: %v", got)
	}
}

func TestReadyPassiveResourceYieldsToBrowserTask(t *testing.T) {
	s := New(time.Unix(0, 0), nil)
	var got []string
	s.Post(ResourceLow, 0, func(context.Context) error { got = append(got, "passive"); return nil })
	s.Post(Timer, 0, func(context.Context) error { got = append(got, "timer"); return nil })
	if err := s.RunReady(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"timer", "passive"}) {
		t.Fatalf("passive resource did not yield to ready browser task: %v", got)
	}
}

func TestReadyTaskDoesNotMoveCanonicalClockBackwards(t *testing.T) {
	s := New(time.Unix(0, 0), nil)
	s.Post(DOM, 0, func(context.Context) error { return nil })
	s.AdvanceBy(time.Second)
	if err := s.RunReady(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	if s.Now().Before(time.Unix(0, 0).Add(time.Second)) {
		t.Fatalf("canonical clock regressed to %v", s.Now())
	}
}

func TestConcurrentDriversNeverOverlapBrowserCallbacks(t *testing.T) {
	s := New(time.Unix(0, 0), nil)
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondStarted := make(chan struct{}, 1)
	s.Post(DOM, 0, func(context.Context) error {
		close(firstStarted)
		<-releaseFirst
		return nil
	})
	s.Post(Timer, 0, func(context.Context) error {
		secondStarted <- struct{}{}
		return nil
	})
	done := make(chan error, 2)
	go func() { done <- s.RunReady(context.Background(), 1) }()
	<-firstStarted
	go func() { done <- s.RunReady(context.Background(), 1) }()
	select {
	case <-secondStarted:
		t.Fatal("second browser callback overlapped the active event-loop turn")
	case <-time.After(20 * time.Millisecond):
	}
	close(releaseFirst)
	for range 2 {
		if err := <-done; err != nil && !strings.Contains(err.Error(), "task limit") {
			t.Fatal(err)
		}
	}
	select {
	case <-secondStarted:
	default:
		t.Fatal("serialized second callback did not run")
	}
}

func TestWebTaskSignalOwnsPriorityAndReadySelection(t *testing.T) {
	s := New(time.Unix(0, 0), nil)
	signal := s.NewWebTaskSignal(2)
	var order []string
	dynamic := s.Post(WebTask, 0, func(context.Context) error {
		id, priority, group := s.CurrentWebTask()
		if id == 0 || priority != 0 || group != signal {
			t.Errorf("task context: %d %d %d", id, priority, group)
		}
		order = append(order, "dynamic")
		return nil
	})
	s.SetWebTaskPriority(dynamic, 2, false)
	s.SetWebTaskSignal(dynamic, signal)
	fixed := s.Post(WebTask, 0, func(context.Context) error { order = append(order, "fixed"); return nil })
	s.SetWebTaskPriority(fixed, 1, false)
	previous, status := s.BeginWebTaskPriorityChange(signal, 0)
	if previous != 2 || status != 1 || s.WebTaskSignalPriority(signal) != 0 {
		t.Fatal("priority transition")
	}
	if _, status := s.BeginWebTaskPriorityChange(signal, 1); status != -1 {
		t.Fatal("reentrant update accepted")
	}
	s.EndWebTaskPriorityChange(signal)
	if _, status := s.BeginWebTaskPriorityChange(signal, 0); status != 0 {
		t.Fatal("unchanged priority dispatched")
	}
	if err := s.RunReady(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(order, []string{"dynamic", "fixed"}) {
		t.Fatal(order)
	}
	if id, _, _ := s.CurrentWebTask(); id != 0 {
		t.Fatal("finished task context retained")
	}
	s.Close()
	if s.WebTaskSignalPriority(signal) != 0 {
		t.Fatal("retained signal changed on queue close")
	}
}
