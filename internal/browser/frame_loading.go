package browser

import "github.com/moreveal/mimic/internal/trace"

type frameLoadingEvent struct {
	name     string
	loaderID string
}

// Loading belongs to the frame/navigation lifetime, rather than a CDP session
// or a client's network-idle timer. A replaced navigation cannot finish the
// loading interval of its successor.
func (p *Page) beginFrameLoading(frame *Frame, loaderID string) {
	p.changeFrameLoading(frame, loaderID, true)
}

func (p *Page) endFrameLoading(frame *Frame, loaderID string) {
	p.changeFrameLoading(frame, loaderID, false)
}

func (p *Page) changeFrameLoading(frame *Frame, loaderID string, start bool) {
	// Cancellation can arrive outside the Page turn. Serialize state transitions
	// and enqueue their projection together, then publish in that order without
	// holding Page.mu or loadingMu across reentrant trace subscribers.
	frame.loadingMu.Lock()
	p.mu.Lock()
	name := ""
	if start {
		if frame.loadingLoaderID == "" {
			name = "frameStartedLoading"
		}
		frame.loadingLoaderID = loaderID
	} else if loaderID != "" && frame.loadingLoaderID == loaderID {
		frame.loadingLoaderID, name = "", "frameStoppedLoading"
	}
	p.mu.Unlock()
	if name != "" {
		frame.loadingEvents = append(frame.loadingEvents, frameLoadingEvent{name, loaderID})
	}
	if frame.loadingPublishing || len(frame.loadingEvents) == 0 {
		frame.loadingMu.Unlock()
		return
	}
	frame.loadingPublishing = true
	for len(frame.loadingEvents) > 0 {
		event := frame.loadingEvents[0]
		frame.loadingEvents[0] = frameLoadingEvent{}
		frame.loadingEvents = frame.loadingEvents[1:]
		frame.loadingMu.Unlock()
		p.trace.Add(trace.Lifecycle, event.name, map[string]any{"frameId": frame.ID, "loaderId": event.loaderID})
		frame.loadingMu.Lock()
	}
	frame.loadingEvents = nil
	frame.loadingPublishing = false
	frame.loadingMu.Unlock()
}
