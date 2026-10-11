package cdp

import (
	"time"

	"github.com/moreveal/mimic/internal/trace"
)

// Opt-in command diagnostics contain no expressions, parameters or results.
// They use the same wall-clock timeline as the Page network and task trace.
// workMs is residual wall time, not CPU: it includes awaited execution and
// debugger pauses. CPU/block profiles distinguish those within the handler.
type commandTiming struct {
	received, queued, started, executionStarted, executed time.Time
	replyQueued, writeStarted, sent                       time.Time
	sessionWait, pageWait, serialize, writeWait, write    time.Duration
}

func (s *session) finishCommandTiming(m message) {
	d := m.timing
	s.commandTimings.Delete(m.ID)
	total := time.Since(d.queued)
	if !d.sent.IsZero() {
		total = d.sent.Sub(d.queued)
	}
	queue := d.started.Sub(d.queued)
	ms := func(value time.Duration) float64 { return float64(value) / float64(time.Millisecond) }
	s.page.Trace().Add(trace.CDP, "commandTiming", map[string]any{
		"method": m.Method, "commandId": m.ID, "sessionId": s.id,
		"queuedAt": d.queued.UTC(), "queueMs": ms(queue),
		"receivedAt": d.received.UTC(), "startedAt": d.started.UTC(),
		"executionStartedAt": d.executionStarted.UTC(), "executionEndedAt": d.executed.UTC(),
		"replyQueuedAt": d.replyQueued.UTC(), "writeStartedAt": d.writeStarted.UTC(), "sentAt": d.sent.UTC(),
		"sessionWaitMs": ms(d.sessionWait), "pageWaitMs": ms(d.pageWait),
		"workMs":      ms(max(0, total-queue-d.sessionWait-d.pageWait-d.serialize-d.writeWait-d.write)),
		"serializeMs": ms(d.serialize), "writeWaitMs": ms(d.writeWait), "writeMs": ms(d.write),
		"totalMs": ms(total), "wireBreakdownAvailable": true,
	})
}
