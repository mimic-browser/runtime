package scheduler

import (
	"container/heap"
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sync"
	"time"

	"github.com/moreveal/mimic/internal/monotime"
)

type Source string

const (
	DOM     Source = "dom"
	WebTask Source = "web-task"
	Timer   Source = "timer"
	// Rendering callbacks already queued by a frame run before author timers
	// that became ready while the frame task was executing.
	Rendering       Source = "rendering"
	Network         Source = "network"
	Navigation      Source = "navigation"
	Control         Source = "control"
	UserInteraction Source = "user-interaction"
	// PostedMessage represents the HTML posted message task source used by
	// MessagePort and related browser messaging primitives.
	PostedMessage Source = "posted-message"
	// ResourceScript and ResourceLow select transport-start work queued by DOM
	// mutations. Transport completions return through Network and do not inherit
	// this fetch-priority ordering.
	ResourceScript Source = "script-resource"
	ResourceLow    Source = "low-priority-resource"
)

type Callback func(context.Context) error
type Transition struct {
	Name   string
	TaskID uint64
	Source Source
	Due    time.Time
}
type ExecutionStatus struct {
	Running bool
	TaskID  uint64
	Source  Source
	Phase   string
	Elapsed time.Duration
}
type task struct {
	id, sequence uint64
	due          time.Time
	source       Source
	callback     Callback
	cancelled    bool
	webPriority  int
	continuation bool
	webSignal    uint64
}
type queue []*task

func (q queue) Len() int { return len(q) }
func (q queue) Less(i, j int) bool {
	if q[i].due.Equal(q[j].due) {
		return q[i].sequence < q[j].sequence
	}
	return q[i].due.Before(q[j].due)
}
func (q queue) Swap(i, j int) { q[i], q[j] = q[j], q[i] }
func (q *queue) Push(x any)   { *q = append(*q, x.(*task)) }
func (q *queue) Pop() any     { old := *q; n := len(old); x := old[n-1]; *q = old[:n-1]; return x }

type webTaskSignal struct {
	priority int
	changing bool
}

type Scheduler struct {
	webSignals         map[uint64]*webTaskSignal
	webSignalSequence  uint64
	runningWebPriority int
	runningWebSignal   uint64
	mu                 sync.Mutex
	runMu              sync.Mutex
	now                time.Time
	seq                uint64
	tasks              queue
	byID               map[uint64]*task
	checkpoint         func(context.Context) error
	paused             bool
	closed             bool
	observer           func(Transition)
	taskObserver       func(time.Time, time.Time)
	taskStarted        func()
	runningAt          time.Time
	runningBase        time.Time
	runningSample      time.Time
	runningTaskID      uint64
	runningSource      Source
	runningPhase       string
	wake               chan struct{}
	executionScale     float64
	sequenceSource     func() uint64
}

func New(start time.Time, checkpoint func(context.Context) error) *Scheduler {
	s := &Scheduler{now: start, byID: map[uint64]*task{}, checkpoint: checkpoint, wake: make(chan struct{}, 1), executionScale: 1}
	heap.Init(&s.tasks)
	return s
}
func (s *Scheduler) SetObserver(f func(Transition)) { s.mu.Lock(); s.observer = f; s.mu.Unlock() }

// SetTaskObserver observes complete event-loop turns, including their microtask
// checkpoints. It runs outside the clock mutex and never creates another loop.
func (s *Scheduler) SetTaskObserver(f func(time.Time, time.Time)) {
	s.mu.Lock()
	s.taskObserver = f
	s.mu.Unlock()
}

func (s *Scheduler) SetTaskStarted(f func()) { s.mu.Lock(); s.taskStarted = f; s.mu.Unlock() }

// SetSequenceSource supplies an event-loop-local enqueue order shared by realm
// queues. Configure it before posting tasks; standalone/worker queues use IDs.
func (s *Scheduler) SetSequenceSource(next func() uint64) {
	s.mu.Lock()
	s.sequenceSource = next
	s.mu.Unlock()
}
func (s *Scheduler) SetExecutionScale(scale float64) {
	if scale <= 0 {
		scale = 1
	}
	s.mu.Lock()
	s.executionScale = scale
	s.mu.Unlock()
}
func (s *Scheduler) Post(source Source, delay time.Duration, callback Callback) uint64 {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return 0
	}
	s.seq++
	t := &task{id: s.seq, sequence: s.seq, due: s.nowLocked().Add(delay), source: source, callback: callback}
	if s.sequenceSource != nil {
		t.sequence = s.sequenceSource()
	}
	heap.Push(&s.tasks, t)
	s.byID[t.id] = t
	observer := s.observer
	s.mu.Unlock()
	if observer != nil {
		observer(Transition{"posted", t.id, t.source, t.due})
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
	return t.id
}

// Wait blocks until external work is posted or the next scheduled task becomes
// ready in real elapsed time. It deliberately does not jump the canonical
// clock to a future timer: another browser agent may complete first.
func (s *Scheduler) Wait(ctx context.Context) error {
	s.mu.Lock()
	var timer *time.Timer
	var readyAt time.Time
	if len(s.tasks) > 0 {
		readyAt = s.tasks[0].due
		delay := readyAt.Sub(s.nowLocked())
		if delay <= 0 {
			s.mu.Unlock()
			return nil
		}
		scale := s.executionScale
		if scale <= 0 {
			scale = 1
		}
		timer = time.NewTimer(time.Duration(float64(delay) / scale))
	}
	s.mu.Unlock()
	if timer == nil {
		select {
		case <-s.wake:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	defer timer.Stop()
	select {
	case <-s.wake:
		return nil
	case <-timer.C:
		s.mu.Lock()
		if readyAt.After(s.now) {
			s.now = readyAt
		}
		s.mu.Unlock()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// WaitAny waits for work in any queue owned by one browser event loop. A
// Promise evaluated in one realm may depend on a task in a sibling or child
// realm, so waiting on only the evaluated realm's wake channel is insufficient.
// The caller serializes event-loop turns; posting from transport goroutines is
// safe. No goroutines or polling are needed to combine the wake channels.
func WaitAny(ctx context.Context, queues []*Scheduler) error {
	cases := []reflect.SelectCase{{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(ctx.Done())}}
	waitStarts := make([]time.Time, len(queues))
	var delay time.Duration
	hasDeadline := false
	for index, s := range queues {
		cases = append(cases, reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(s.wake)})
		s.mu.Lock()
		waitStarts[index] = s.nowLocked()
		if !s.paused && len(s.tasks) > 0 {
			remaining := s.tasks[0].due.Sub(waitStarts[index])
			if remaining <= 0 {
				s.mu.Unlock()
				return ctx.Err()
			}
			remaining = time.Duration(math.Ceil(float64(remaining) / s.executionScale))
			if !hasDeadline || remaining < delay {
				delay, hasDeadline = remaining, true
			}
		}
		s.mu.Unlock()
	}
	start := monotime.Now()
	if hasDeadline {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		cases = append(cases, reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(timer.C)})
	}
	selected, _, _ := reflect.Select(cases)
	// Every realm observes the same elapsed wait. Advancing only the queue
	// owning the earliest timer would leave another realm's clock frozen.
	elapsed := monotime.Since(start)
	// The platform timer and the canonical monotonic clock can have different
	// resolution (notably Go's Windows timer versus QPC). If the due timer won,
	// observe at least its requested interval so its task is actually ready.
	// An enqueue notification or cancellation must never fast-forward time.
	if hasDeadline && selected == len(cases)-1 && elapsed < delay {
		elapsed = delay
	}
	for index, s := range queues {
		s.mu.Lock()
		// An external completion may already have observed this interval.
		// Add elapsed time to the pre-wait sample, never to that newer stamp.
		if observed := waitStarts[index].Add(time.Duration(float64(elapsed) * s.executionScale)); observed.After(s.now) {
			s.now = observed
		}
		s.mu.Unlock()
	}
	if selected == 0 {
		return ctx.Err()
	}
	return nil
}
func (s *Scheduler) Cancel(id uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t := s.byID[id]; t != nil {
		t.cancelled = true
		delete(s.byID, id)
	}
}

// Close discards pending callbacks and rejects future work for a destroyed or
// inactive document. Unlike Pause it cannot be reversed by Resume.
func (s *Scheduler) Close() {
	s.mu.Lock()
	s.closed = true
	s.paused = true
	s.tasks = nil
	s.byID = make(map[uint64]*task)
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
func (s *Scheduler) Pause()  { s.mu.Lock(); s.paused = true; s.mu.Unlock() }
func (s *Scheduler) Resume() { s.mu.Lock(); s.paused = s.closed; s.mu.Unlock() }
func (s *Scheduler) RunUntilIdle(ctx context.Context, maxTasks int) error {
	return s.run(ctx, maxTasks, true, true)
}
func (s *Scheduler) RunReady(ctx context.Context, maxTasks int) error {
	return s.run(ctx, maxTasks, false, true)
}

// RunInline executes an externally initiated turn (such as debugger evaluation)
// without dequeuing unrelated work. Its clock and microtask checkpoint obey the
// same boundaries as queued tasks. Like RunReady, it must not be called from a
// callback already running on this scheduler.
func (s *Scheduler) RunInline(ctx context.Context, callback Callback) error {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	s.beginExecution()
	defer s.endExecution()
	var err error
	if callback != nil {
		err = callback(ctx)
	}
	if s.checkpoint != nil {
		err = errors.Join(err, s.checkpoint(ctx))
	}
	return err
}

func (s *Scheduler) beginExecution() {
	s.mu.Lock()
	s.runningBase = s.now
	s.runningSample = time.Time{}
	s.runningAt = monotime.Now()
	started := s.taskStarted
	s.mu.Unlock()
	if started != nil {
		started()
	}
}

func (s *Scheduler) endExecution() {
	s.mu.Lock()
	start := s.runningBase
	s.now = s.nowLocked()
	end, observer := s.now, s.taskObserver
	s.runningAt = time.Time{}
	s.runningBase = time.Time{}
	s.runningSample = time.Time{}
	s.runningTaskID = 0
	s.runningSource = ""
	s.runningPhase = ""
	s.runningWebPriority, s.runningWebSignal = 1, 0
	s.mu.Unlock()
	if observer != nil && !start.IsZero() {
		observer(start, end)
	}
}

// RunReadyStep executes at most one task, including its microtask checkpoint.
// A Page uses this boundary to service other realm queues before returning to
// a realm which posts more work from every callback. It never advances time to
// a future timer. A cancelled queue entry also counts as progress.
func (s *Scheduler) RunReadyStep(ctx context.Context) (bool, error) {
	s.mu.Lock()
	ready := !s.paused && len(s.tasks) > 0 && !s.tasks[0].due.After(s.now)
	s.mu.Unlock()
	if !ready {
		return false, nil
	}
	return true, s.run(ctx, 1, false, false)
}

// RunReadyAcross selects one task across the realm queues of a single Page.
// Compare the same due times and source priorities used within each queue;
// merely alternating realms can let a newer timer overtake an older message.
// The caller serializes Page turns. Workers are separate event loops.
func RunReadyAcross(ctx context.Context, queues []*Scheduler) (bool, error) {
	// All realms of a Page observe time spent in the preceding task. Keep
	// absolute due times comparable before selecting work from another queue.
	var now time.Time
	for _, queue := range queues {
		if observed := queue.Now(); observed.After(now) {
			now = observed
		}
	}
	for _, queue := range queues {
		queue.mu.Lock()
		if now.After(queue.now) {
			queue.now = now
		}
		queue.mu.Unlock()
	}
	var selected *Scheduler
	var best task
	for _, queue := range queues {
		queue.mu.Lock()
		if !queue.paused {
			for _, candidate := range queue.tasks {
				if candidate.due.After(queue.now) {
					continue
				}
				snapshot := queue.webTaskSnapshotLocked(candidate)
				if selected == nil || taskBefore(&snapshot, &best) {
					selected, best = queue, snapshot
				}
			}
		}
		queue.mu.Unlock()
	}
	if selected == nil {
		return false, nil
	}
	return selected.RunReadyStep(ctx)
}

func (s *Scheduler) run(ctx context.Context, maxTasks int, advance, limitError bool) error {
	// Multiple Go goroutines may wake or drive one browsing context (for
	// example navigation and a CDP clock pump), but Chrome still has one event
	// loop per agent. Serialize complete turns so callbacks and their microtask
	// checkpoints can never execute concurrently or overtake queued work.
	s.runMu.Lock()
	defer s.runMu.Unlock()
	var taskErrors []error
	for i := 0; i < maxTasks; i++ {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(taskErrors, err)...)
		}
		s.mu.Lock()
		if s.paused || len(s.tasks) == 0 {
			s.mu.Unlock()
			return errors.Join(taskErrors...)
		}
		t := s.popNextLocked(advance)
		if t == nil {
			s.mu.Unlock()
			return errors.Join(taskErrors...)
		}
		delete(s.byID, t.id)
		if t.due.After(s.now) {
			s.now = t.due
		}
		observer := s.observer
		s.mu.Unlock()
		if t.cancelled {
			continue
		}
		if observer != nil {
			observer(Transition{"start", t.id, t.source, t.due})
		}
		s.beginTask(t)
		callbackErr := t.callback(ctx)
		if callbackErr != nil {
			if observer != nil {
				observer(Transition{"error", t.id, t.source, t.due})
			}
			taskErrors = append(taskErrors, fmt.Errorf("%s task: %w", t.source, callbackErr))
		}
		if s.checkpoint != nil {
			s.mu.Lock()
			s.runningPhase = "checkpoint"
			s.mu.Unlock()
			if observer != nil {
				observer(Transition{"microtaskCheckpoint", t.id, t.source, t.due})
			}
			if err := s.checkpoint(ctx); err != nil {
				taskErrors = append(taskErrors, fmt.Errorf("microtask checkpoint: %w", err))
			}
		}
		// A microtask checkpoint is part of the same browser event-loop turn as
		// the task callback. Keep the canonical monotonic clock live until every
		// Promise job has run: JavaScript may observe time from a microtask, and
		// freezing it here can turn an otherwise finite polling chain into an
		// infinite one.
		s.endExecution()
		if observer != nil {
			observer(Transition{"end", t.id, t.source, t.due})
		}
	}
	if limitError {
		taskErrors = append(taskErrors, fmt.Errorf("scheduler task limit %d exceeded", maxTasks))
	}
	return errors.Join(taskErrors...)
}

func (s *Scheduler) beginTask(t *task) {
	s.mu.Lock()
	s.runningTaskID = t.id
	s.runningSource = t.source
	s.runningWebPriority, s.runningWebSignal = t.webPriority, t.webSignal
	s.runningPhase = "callback"
	s.runningBase = s.now
	s.runningSample = time.Time{}
	s.runningAt = monotime.Now()
	started := s.taskStarted
	s.mu.Unlock()
	if started != nil {
		started()
	}
}

func (s *Scheduler) ExecutionStatus() ExecutionStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	status := ExecutionStatus{Running: !s.runningAt.IsZero(), TaskID: s.runningTaskID, Source: s.runningSource, Phase: s.runningPhase}
	if status.Running {
		status.Elapsed = monotime.Since(s.runningAt)
	}
	return status
}

func (s *Scheduler) popNextLocked(advance bool) *task {
	if len(s.tasks) == 0 {
		return nil
	}
	if s.tasks[0].due.After(s.now) {
		if !advance {
			return nil
		}
		s.now = s.tasks[0].due
	}
	best := -1
	for index, candidate := range s.tasks {
		if candidate.due.After(s.now) {
			continue
		}
		snapshot := s.webTaskSnapshotLocked(candidate)
		var previous task
		if best >= 0 {
			previous = s.webTaskSnapshotLocked(s.tasks[best])
		}
		if best < 0 || taskBefore(&snapshot, &previous) {
			best = index
		}
	}
	if best < 0 {
		return nil
	}
	return heap.Remove(&s.tasks, best).(*task)
}

// A TaskSignal's priority has one owner. Queue entries retain only its ID;
// each ready selection resolves the current value under the scheduler lock.
func (s *Scheduler) webTaskSnapshotLocked(t *task) task {
	snapshot := *t
	if signal := s.webSignals[t.webSignal]; signal != nil {
		snapshot.webPriority = signal.priority
	}
	return snapshot
}
func (s *Scheduler) NewWebTaskSignal(priority int) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.webSignals == nil {
		s.webSignals = make(map[uint64]*webTaskSignal)
	}
	s.webSignalSequence++
	s.webSignals[s.webSignalSequence] = &webTaskSignal{priority: priority}
	return s.webSignalSequence
}
func (s *Scheduler) WebTaskSignalPriority(id uint64) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if signal := s.webSignals[id]; signal != nil {
		return signal.priority
	}
	return 1
}

// Begin returns -1 for a reentrant update, 0 for no change, and 1 when the
// binding should dispatch prioritychange. End must run even if a listener throws.
func (s *Scheduler) BeginWebTaskPriorityChange(id uint64, priority int) (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	signal := s.webSignals[id]
	if signal == nil {
		return 1, 0
	}
	previous := signal.priority
	if signal.changing {
		return previous, -1
	}
	if previous == priority {
		return previous, 0
	}
	signal.priority = priority
	signal.changing = true
	return previous, 1
}
func (s *Scheduler) EndWebTaskPriorityChange(id uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if signal := s.webSignals[id]; signal != nil {
		signal.changing = false
	}
}
func (s *Scheduler) SetWebTaskSignal(taskID, signalID uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if task := s.byID[taskID]; task != nil && s.webSignals[signalID] != nil {
		task.webSignal = signalID
	}
}
func (s *Scheduler) CurrentWebTask() (uint64, int, uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runningSource != WebTask {
		return 0, 1, 0
	}
	priority := s.runningWebPriority
	if signal := s.webSignals[s.runningWebSignal]; signal != nil {
		priority = signal.priority
	}
	return s.runningTaskID, priority, s.runningWebSignal
}

// Web scheduling priorities choose among ready tasks, never change due times.
func (s *Scheduler) SetWebTaskPriority(id uint64, priority int, continuation bool) {
	s.mu.Lock()
	if t := s.byID[id]; t != nil {
		t.webPriority = priority
		t.continuation = continuation
	}
	s.mu.Unlock()
}

func taskBefore(left, right *task) bool {
	priority := func(t *task) int {
		if t.source == WebTask {
			return t.webPriority - 1
		}
		return sourcePriority(t.source)
	}
	leftPriority, rightPriority := priority(left), priority(right)
	if leftPriority != rightPriority {
		return leftPriority < rightPriority
	}
	if left.source == WebTask && right.source == WebTask {
		if left.continuation != right.continuation {
			return left.continuation
		}
		return left.sequence < right.sequence
	}
	if !left.due.Equal(right.due) {
		return left.due.Before(right.due)
	}
	return left.sequence < right.sequence
}

func sourcePriority(source Source) int {
	// A ready navigation replaces the current document. Let it overtake queued
	// work from that document, as Chromium does, instead of forcing history and
	// explicit navigations to drain stale DOM/observer turns first. This never
	// interrupts the currently running turn and does not make a future
	// navigation ready early.
	if source == Navigation {
		return -2
	}
	if source == ResourceScript {
		return -1
	}
	if source == Rendering {
		return -1
	}
	if source == ResourceLow {
		return 1
	}
	return 0
}
func (s *Scheduler) nowLocked() time.Time {
	if !s.runningAt.IsZero() {
		return s.runningBase.Add(time.Duration(float64(monotime.Since(s.runningAt)) * s.executionScale))
	}
	return s.now
}
func (s *Scheduler) Now() time.Time { s.mu.Lock(); defer s.mu.Unlock(); return s.nowLocked() }

// SampledNow projects the canonical clock once per event-loop turn, including
// its microtask checkpoint. Animation observations share this sample while
// performance.now and timer scheduling continue to use the live Now clock.
func (s *Scheduler) SampledNow() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runningAt.IsZero() {
		return s.now
	}
	if s.runningSample.IsZero() {
		s.runningSample = s.nowLocked()
	}
	return s.runningSample
}

func (s *Scheduler) HasPendingInput() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.nowLocked()
	for _, task := range s.tasks {
		if !task.cancelled && task.source == UserInteraction && !task.due.After(now) {
			return true
		}
	}
	return false
}
func (s *Scheduler) AdvanceBy(delta time.Duration) {
	if delta <= 0 {
		return
	}
	s.mu.Lock()
	s.now = s.now.Add(delta)
	s.mu.Unlock()
}

// AdvanceTo observes an already completed external operation on the canonical
// clock. When called inside a turn, retain its elapsed execution and sampled
// animation time; subsequent live reads and the turn's final stamp must include
// the same completion. An older observation never moves time backwards.
func (s *Scheduler) AdvanceTo(completed time.Time) {
	s.mu.Lock()
	if delta := completed.Sub(s.nowLocked()); delta > 0 {
		s.now = s.now.Add(delta)
		if !s.runningAt.IsZero() {
			s.runningBase = s.runningBase.Add(delta)
		}
		s.mu.Unlock()
		select {
		case s.wake <- struct{}{}:
		default:
		}
		return
	}
	s.mu.Unlock()
}
