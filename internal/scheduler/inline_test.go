package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/moreveal/mimic/internal/monotime"
)

func TestInlineClockIncludesBodyAndCheckpointWithoutDequeuing(t *testing.T) {
	var s *Scheduler
	var bodyEnd, checkpointEnd time.Time
	sentinel := errors.New("body failed")
	waitMonotonic := func() {
		start := monotime.Now()
		for monotime.Since(start) < 5*time.Millisecond {
			time.Sleep(time.Millisecond)
		}
	}
	s = New(time.Unix(0, 0), func(context.Context) error {
		start := s.Now()
		waitMonotonic()
		checkpointEnd = s.Now()
		if checkpointEnd.Sub(start) < 5*time.Millisecond {
			t.Error("clock stopped in checkpoint")
		}
		return nil
	})
	s.Post(Timer, 0, func(context.Context) error { t.Error("inline turn dequeued a timer"); return nil })
	err := s.RunInline(context.Background(), func(context.Context) error {
		start := s.Now()
		waitMonotonic()
		bodyEnd = s.Now()
		if bodyEnd.Sub(start) < 5*time.Millisecond {
			t.Error("clock stopped in body")
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) || !checkpointEnd.After(bodyEnd) || s.Now().Before(checkpointEnd) {
		t.Fatalf("turn completion: err=%v body=%v checkpoint=%v now=%v", err, bodyEnd, checkpointEnd, s.Now())
	}
	end := s.Now()
	time.Sleep(time.Millisecond)
	if s.Now() != end {
		t.Fatal("execution clock leaked beyond turn")
	}
}

func TestExternalCompletionAdvancesCanonicalTurnClock(t *testing.T) {
	origin := time.Unix(0, 0)
	s := New(origin, nil)
	other := New(origin, nil)
	s.AdvanceTo(origin.Add(time.Second))
	s.AdvanceTo(origin)
	if s.Now() != origin.Add(time.Second) || other.Now() != origin {
		t.Fatal("idle completion moved backwards or affected another clock")
	}
	completed := origin.Add(time.Hour)
	var live, sampled time.Time
	timerRan := false
	s.Post(Timer, time.Minute, func(context.Context) error { timerRan = true; return nil })
	err := s.RunInline(context.Background(), func(context.Context) error {
		sampled = s.SampledNow()
		s.AdvanceTo(completed)
		live = s.Now()
		if live.Before(completed) || s.SampledNow() != sampled || timerRan {
			t.Fatal("completion lost its stamp, changed the task sample, or dequeued a timer")
		}
		s.AdvanceTo(origin)
		if s.Now().Before(live) {
			t.Fatal("older completion moved the running clock backwards")
		}
		return nil
	})
	if err != nil || s.Now().Before(live) || other.Now() != origin {
		t.Fatalf("completion did not survive turn teardown: %v", err)
	}
	if _, err := s.RunReadyStep(context.Background()); err != nil || !timerRan {
		t.Fatalf("timer was not ready on the advanced clock: %v", err)
	}
}

func TestExternalCompletionWakesWaitWithoutCountingElapsedTwice(t *testing.T) {
	origin := time.Unix(0, 0)
	s, other := New(origin, nil), New(origin, nil)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- WaitAny(ctx, []*Scheduler{s, other}) }()
	// There is no queued task or wake notification: the completion itself
	// must release a waiter, irrespective of when its goroutine begins.
	time.Sleep(5 * time.Millisecond)
	completed := origin.Add(time.Hour)
	s.AdvanceTo(completed)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if s.Now() != completed || !other.Now().Before(completed) {
		t.Fatalf("completion counted twice or changed another owner: own=%v other=%v", s.Now(), other.Now())
	}
}
