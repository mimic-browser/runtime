package browser

import (
	"context"
	"errors"
	"github.com/moreveal/mimic/internal/dom"
	"github.com/moreveal/mimic/internal/network"
	"github.com/moreveal/mimic/internal/scheduler"
	"github.com/moreveal/mimic/internal/trace"
	"net/url"
	"sync"
	"time"
)

// Navigation owns its existing lifecycle and history commit. It shares the
// document insertion stream with document.write without implicitly opening or
// replacing the already established Document/Window.
func (r *Realm) initializeNavigationStream(navigationContext ...context.Context) (*documentStream, error) {
	parser, err := r.document.NewStream()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(r.resourceContext)
	stopNavigationCancel := func() bool { return false }
	if len(navigationContext) > 0 && navigationContext[0] != nil {
		stopNavigationCancel = context.AfterFunc(navigationContext[0], cancel)
	}
	s := &documentStream{
		parser: parser,
		ctx:    ctx,
		cancel: func() {
			stopNavigationCancel()
			cancel()
		},
		scripts:    map[int64]*streamScriptResponse{},
		navigation: true,
	}
	r.documentStream = s
	return s, nil
}

func (r *Realm) executeChildNavigationScript(ctx context.Context, navigation *childNavigation, stream *documentStream, node dom.Node) error {
	realm := navigation.realm
	realm.preloadResources()
	stylesheets := realm.startParserStylesheets()
	if !r.childNavigationCurrent(navigation) || realm.documentStream != stream || realm.document.ScriptStarted(node.ID) {
		return nil
	}
	if scriptExecutionKind(node.Attributes["type"], node.Attributes["language"]) == "" {
		return nil
	}
	code, name := realm.document.TextContent(node.ID), realm.documentURL().String()
	if src := node.Attributes["src"]; src != "" {
		target, err := realm.resolveDocument(src)
		if err != nil {
			return err
		}
		loaded := stream.scripts[node.ID]
		if loaded == nil {
			loaded = &streamScriptResponse{}
			stream.scripts[node.ID] = loaded
			eventLoop := r.browserEventLoop()
			request := realm.elementRequest(target, node.Attributes, network.Script)
			realm.resourceWG.Add(1)
			go func() {
				defer realm.resourceWG.Done()
				response, loadErr := realm.loadResource(stream.ctx, request)
				if loadErr == nil {
					loadErr = scriptResponseError(response)
				}
				if stream.ctx.Err() != nil {
					return
				}
				eventLoop.Post(scheduler.Network, 0, func(taskContext context.Context) error {
					if !r.childNavigationCurrent(navigation) || realm.documentStream != stream {
						r.finishChildNavigation(navigation)
						return nil
					}
					loaded.response, loaded.err, loaded.ready = response, loadErr, true
					restoreTaskContext := stream.useTaskContext(taskContext)
					defer restoreTaskContext()
					stream.depth++
					resumeErr := stream.parser.Resume(func(node dom.Node) error { return realm.executeStreamScript(stream, node) })
					stream.depth--
					if errors.Is(resumeErr, dom.ErrStreamPaused) {
						return nil
					}
					if resumeErr != nil {
						return resumeErr
					}
					if stream.closing {
						return realm.finishDocumentStream(stream)
					}
					return nil
				})
			}()
			return dom.ErrStreamPaused
		}
		if !loaded.ready {
			return dom.ErrStreamPaused
		}
		if loaded.err != nil {
			realm.document.MarkScriptStarted(node.ID)
			r.agent.Page().trace.Add(trace.Error, "frameScriptLoad", map[string]any{"frameId": navigation.frame.ID, "url": target.String(), "error": loaded.err.Error()})
			return realm.dispatchResourceEvent(ctx, node.ID, "error")
		}
		code, name = string(loaded.response.Body), target.String()
	}
	if realm.deferNavigationStylesheets(stream, stylesheets, func() error { return realm.resumeNavigationStream(stream.executionContext(), stream) }) {
		return dom.ErrStreamPaused
	}
	realm.document.MarkScriptStarted(node.ID)
	if code == "" {
		return nil
	}
	previous := stream.insideScript
	stream.insideScript = true
	defer func() { stream.insideScript = previous }()
	r.agent.Page().trace.Add(trace.JS, "scriptStart", map[string]any{"frameId": navigation.frame.ID, "url": name, "realm": realm.ID})
	if err := realm.evaluateClassicScript(ctx, code, name, node.ID); err != nil {
		r.agent.Page().trace.Add(trace.Error, "frameScriptTask", map[string]any{"frameId": navigation.frame.ID, "url": name, "error": err.Error()})
	}
	return nil
}

// Network workers produce immutable responses. Only a Page-owned continuation
// mutates parser state or enters JavaScript; no command lock is released with
// a script stack active.
func (r *Realm) fetchNavigationScript(s *documentStream, loaded *streamScriptResponse, request network.Request) {
	eventLoop := r.browserEventLoop()
	r.resourceWG.Add(1)
	go func() {
		defer r.resourceWG.Done()
		response, err := r.loadResource(s.ctx, request)
		if s.ctx.Err() != nil {
			return
		}
		eventLoop.Post(scheduler.Network, 0, func(taskContext context.Context) error {
			if s.ctx.Err() != nil || r.documentStream != s || r.inactive {
				return nil
			}
			loaded.response, loaded.err, loaded.ready = response, err, true
			return r.resumeNavigationStream(taskContext, s)
		})
	}()
}

func (r *Realm) resumeNavigationStream(taskContext context.Context, s *documentStream) error {
	restoreTaskContext := s.useTaskContext(taskContext)
	defer restoreTaskContext()
	if s.ctx.Err() != nil || r.documentStream != s || r.inactive {
		return nil
	}
	previous := s.inNavigationTask
	s.inNavigationTask = true
	defer func() { s.inNavigationTask = previous }()
	s.depth++
	err := s.parser.Resume(func(node dom.Node) error { return r.executeStreamScript(s, node) })
	s.depth--
	if errors.Is(err, dom.ErrStreamPaused) {
		return nil
	}
	if err != nil {
		return err
	}
	if s.closing {
		return r.finishDocumentStream(s)
	}
	return nil
}

// A stylesheet barrier suspends the parser rather than its owning Page. It
// also handles end-of-parser barriers, where there is no script to resume.
func (r *Realm) deferNavigationStylesheets(s *documentStream, pending []*resourcePreload, resume func() error) bool {
	if s.waitingStylesheets {
		return true
	}
	waiting := false
	for _, load := range pending {
		select {
		case <-load.done:
		default:
			waiting = true
		}
	}
	if !waiting {
		return false
	}
	s.waitingStylesheets = true
	eventLoop := r.browserEventLoop()
	r.resourceWG.Add(1)
	go func() {
		defer r.resourceWG.Done()
		r.waitParserStylesheets(s.ctx, pending)
		if s.ctx.Err() != nil {
			return
		}
		eventLoop.Post(scheduler.Network, 0, func(taskContext context.Context) error {
			if s.ctx.Err() != nil || r.documentStream != s || r.inactive {
				return nil
			}
			s.waitingStylesheets = false
			restoreTaskContext := s.useTaskContext(taskContext)
			defer restoreTaskContext()
			previous := s.inNavigationTask
			s.inNavigationTask = true
			defer func() { s.inNavigationTask = previous }()
			return resume()
		})
	}()
	return true
}

// A resumed parser already executes on its scheduler. Never recursively enter
// that scheduler's run mutex; script cleanup still checkpoints microtasks.
func (r *Realm) runNavigationTask(ctx context.Context, s *documentStream, source scheduler.Source, callback scheduler.Callback) error {
	ctx = s.executionContext()
	if s.inNavigationTask {
		return errors.Join(callback(ctx), r.checkpoint(ctx))
	}
	return r.runTask(ctx, source, callback)
}

func (p *Page) fetchNavigationResponse(ctx context.Context, u *url.URL, loaderID string, performanceOrigin time.Time, request network.Request, historyTarget int, committed func(error), replace ...bool) error {
	// The initiating CDP command returns before this request does. Copy its
	// deadline, but give cancellation to the Page rather than the command.
	navigationContext, cancel := context.WithCancel(context.Background())
	if deadline, ok := ctx.Deadline(); ok {
		cancel()
		navigationContext, cancel = context.WithDeadline(context.Background(), deadline)
	}
	var commitOnce sync.Once
	finishCommit := func(err error) {
		commitOnce.Do(func() {
			if err != nil {
				p.endFrameLoading(p.Top, loaderID)
			}
			if committed != nil {
				committed(err)
			}
		})
	}
	if committed != nil {
		// stopLoading, replacement and teardown must release a pending reply
		// even when the old document's scheduler can no longer run a callback.
		context.AfterFunc(navigationContext, func() { finishCommit(navigationContext.Err()) })
	}
	p.mu.Lock()
	owner := p.Top.Realm
	var targetEntry *sessionHistoryEntry
	if historyTarget > 0 && historyTarget <= len(p.history) {
		targetEntry = p.history[historyTarget-1]
	}
	p.navigationCancel = cancel
	p.mu.Unlock()
	if owner == nil {
		cancel()
		return errors.New("page has no realm for navigation")
	}
	// Publish loading only after stopLoading can cancel its ownership handle,
	// but before the resource request starts.
	p.beginFrameLoading(p.Top, loaderID)
	owner.resourceWG.Add(1)
	go func() {
		defer owner.resourceWG.Done()
		response, err := p.loader.Load(navigationContext, request)
		owner.scheduler.Post(scheduler.Navigation, 0, func(taskContext context.Context) error {
			defer cancel()
			if p.LoaderID() != loaderID || p.Top.Realm != owner {
				finishCommit(context.Canceled)
				return nil
			}
			if navigationContext.Err() != nil {
				finishCommit(navigationContext.Err())
				if navigationContext.Err() == context.DeadlineExceeded {
					p.trace.Add(trace.Error, "navigation", map[string]any{"url": u.String(), "error": navigationContext.Err().Error()})
				}
				return nil
			}
			if historyTarget > 0 {
				found := false
				for index, entry := range p.history {
					if entry == targetEntry {
						historyTarget, found = index+1, true
						break
					}
				}
				if !found {
					err = errors.New("navigation history target was replaced while loading")
				}
			}
			if err == nil {
				if p.captureDownload(response) {
					p.endFrameLoading(p.Top, loaderID)
					finishCommit(nil)
					return nil
				}
				err = p.commitNavigationResponse(navigationContext, taskContext, u, loaderID, performanceOrigin, response, historyTarget, true, finishCommit, replace...)
			}
			if err != nil {
				finishCommit(err)
				p.trace.Add(trace.Error, "navigation", map[string]any{"url": u.String(), "error": err.Error()})
			}
			return err
		})
	}()
	return nil
}

func (r *Realm) deferNavigationScriptFetch(s *documentStream, pending *scriptFetch, resume func() error) bool {
	select {
	case <-pending.done:
		return false
	default:
	}
	if s.waitingModule {
		return true
	}
	s.waitingModule = true
	r.resourceWG.Add(1)
	go func() {
		defer r.resourceWG.Done()
		_, _ = pending.wait(s.ctx)
		if s.ctx.Err() != nil {
			return
		}
		r.scheduler.Post(scheduler.Network, 0, func(taskContext context.Context) error {
			if s.ctx.Err() != nil || r.documentStream != s || r.inactive {
				return nil
			}
			s.waitingModule = false
			restoreTaskContext := s.useTaskContext(taskContext)
			defer restoreTaskContext()
			previous := s.inNavigationTask
			s.inNavigationTask = true
			defer func() { s.inNavigationTask = previous }()
			return resume()
		})
	}()
	return true
}
