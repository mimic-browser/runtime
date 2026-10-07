package browser

import (
	"context"
	"errors"
	"net/url"
	"time"

	"github.com/moreveal/mimic/internal/dom"
	"github.com/moreveal/mimic/internal/engine"
	"github.com/moreveal/mimic/internal/network"
	"github.com/moreveal/mimic/internal/scheduler"
	"github.com/moreveal/mimic/internal/trace"
)

// The stream replaces input, not the realm or canonical Document. The parser
// owns insertion state; this layer owns script execution and load events.
type documentStream struct {
	deferred                        []deferredParserScript
	nextDeferred                    int
	completionStarted               bool
	parser                          *dom.Stream
	depth                           int
	closing, finished, insideScript bool
	ctx                             context.Context
	cancel                          context.CancelFunc
	scripts                         map[int64]*streamScriptResponse
	navigation                      bool
	waitingStylesheets              bool
	waitingModule                   bool
	inNavigationTask                bool
	navigationTaskContext           context.Context
	onScript                        func(dom.Node) error
	onFinished                      func() error
}

type streamScriptResponse struct {
	response network.Response
	err      error
	ready    bool
}

func (r *Realm) installDocumentStream(host map[string]any) {
	host["documentStream"] = r.fn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		target, resolveErr := r.referenceRealm(strarg(args, 0), strarg(args, 3))
		if resolveErr != nil {
			return nil, resolveErr
		}
		if target.mainWorld != nil {
			target = target.mainWorld
		}
		caller := r
		if r.documentEntry != nil {
			caller = r.documentEntry
		}
		operation, text := strarg(args, 1), strarg(args, 2)
		err := r.runWorld(target, func(context.Context) error {
			switch operation {
			case "open":
				return target.openDocumentStream(caller)
			case "write":
				return target.writeDocumentStream(caller, text)
			case "close":
				return target.closeDocumentStream()
			}
			return nil
		})
		return r.val(nil), err
	})
	host["documentBaseURI"] = r.transientFn(func(engine.Value, []engine.Value) (engine.Value, error) {
		return r.val(r.documentBaseURL().String()), nil
	})
}

func (r *Realm) documentBaseURL() *url.URL {
	base := r.documentURL()
	if base.Scheme == "about" {
		if frame, ok := r.agent.(*Frame); ok && frame.parent != nil && frame.parent.Realm != nil {
			base = frame.parent.Realm.documentBaseURL()
		} else if ok && frame.auxiliaryBase != nil {
			base = frame.auxiliaryBase
		}
	}
	// Cache the parsed reference, not the resolved URL: history changes and
	// about:blank's inherited base must take effect without a DOM mutation.
	// The canonical arena revision includes parser and cross-realm writes.
	revision := r.document.Revision()
	if r.baseCacheDocument != r.document || r.baseCacheRevision != revision {
		r.baseCacheDocument, r.baseCacheRevision = r.document, revision
		r.baseCacheReference = nil
		for _, node := range r.document.FindAllByTagName("base") {
			if raw, exists := node.Attributes["href"]; exists {
				r.baseCacheReference, _ = url.Parse(raw)
				break
			}
		}
	}
	if r.baseCacheReference != nil {
		return base.ResolveReference(r.baseCacheReference)
	}
	return base
}

func (r *Realm) openDocumentStream(caller *Realm) error {
	if r.documentStream != nil && r.documentStream.insideScript {
		return nil
	}
	r.resetPreloads()
	if r.documentStream != nil {
		r.documentStream.cancel()
		r.documentStream.parser.Abort()
	}
	// Reset before detaching: previously detached nodes keep their listeners.
	for _, world := range r.documentWorlds() {
		if world.documentStreamReset == nil {
			continue
		}
		if err := r.callWorld(world, world.documentStreamReset); err != nil {
			return err
		}
	}
	for id := range r.childFrames {
		r.detachChildFrame(id)
	}
	parser, err := r.document.NewStream()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(r.resourceContext)
	r.documentStream = &documentStream{parser: parser, ctx: ctx, cancel: cancel, scripts: map[int64]*streamScriptResponse{}}
	r.readyState = "loading"
	r.cssImageEpochValid = false
	r.loadEpoch++
	r.loadRequested, r.loadScheduled, r.loadCompleted = false, false, false
	r.navigationLoadEnd = time.Time{}
	if frame, ok := r.agent.(*Frame); ok {
		if frame == r.agent.Page().Top {
			p := r.agent.Page()
			p.mu.Lock()
			p.loadEventEnded = false
			p.mu.Unlock()
		}
		if frame.navigationCancel != nil {
			frame.navigationCancel()
		}
		frame.navigationSequence++
		frame.navigationPending = false
		u := caller.documentURL()
		if caller != r {
			u.Fragment, u.RawFragment = "", ""
		}
		r.agent.Page().commitHistory(frame, u, true, r.historyState())
	}
	return nil
}

func (r *Realm) writeDocumentStream(caller *Realm, source string) error {
	if r.documentStream == nil || r.documentStream.finished {
		if r.ignoreDestructiveWrites > 0 {
			return nil
		}
		if err := r.openDocumentStream(caller); err != nil {
			return err
		}
	}
	s := r.documentStream
	s.depth++
	err := s.parser.Write(source, func(node dom.Node) error { return r.executeStreamScript(s, node) })
	s.depth--
	if errors.Is(err, dom.ErrStreamPaused) {
		return nil
	}
	if err != nil {
		return err
	}
	if s.closing && s.depth == 0 && r.documentStream == s {
		return r.finishDocumentStream(s)
	}
	return nil
}

func (r *Realm) closeDocumentStream() error {
	s := r.documentStream
	if s == nil || s.finished {
		return nil
	}
	s.closing = true
	if s.depth != 0 {
		return nil
	}
	return r.finishDocumentStream(s)
}

func (r *Realm) executeStreamScript(s *documentStream, node dom.Node) error {
	r.preloadResources()
	r.startDocumentImages()
	if queued, err := r.queueDeferredClassic(s, node); queued || err != nil {
		return err
	}
	if s.onScript != nil {
		return s.onScript(node)
	}
	if r.documentStream != s || r.document.ScriptStarted(node.ID) {
		return nil
	}
	kind := scriptExecutionKind(node.Attributes["type"], node.Attributes["language"])
	if kind == "" {
		return nil
	}
	ctx := s.ctx
	code, name := r.document.TextContent(node.ID), r.documentURL().String()
	src := node.Attributes["src"]
	if src != "" {
		u, err := r.resolveDocument(src)
		if err != nil {
			return err
		}
		if !r.allowsScript(u, false, false, node.Nonce) {
			return nil
		}
		loaded := s.scripts[node.ID]
		if loaded == nil {
			loaded = &streamScriptResponse{}
			s.scripts[node.ID] = loaded
			request := r.elementRequest(u, node.Attributes, network.Script)
			eventLoop := r.browserEventLoop()
			r.resourceWG.Add(1)
			go func() {
				defer r.resourceWG.Done()
				response, loadErr := r.loadResource(ctx, request)
				if loadErr == nil {
					loadErr = scriptResponseError(response)
				}
				if ctx.Err() != nil {
					return
				}
				eventLoop.Post(scheduler.Network, 0, func(taskContext context.Context) error {
					if ctx.Err() != nil || r.documentStream != s {
						return nil
					}
					loaded.response, loaded.err, loaded.ready = response, loadErr, true
					s.depth++
					resumeErr := s.parser.Resume(func(node dom.Node) error { return r.executeStreamScript(s, node) })
					s.depth--
					if errors.Is(resumeErr, dom.ErrStreamPaused) {
						return nil
					}
					if resumeErr != nil {
						return resumeErr
					}
					if s.closing {
						return r.finishDocumentStream(s)
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
			r.document.MarkScriptStarted(node.ID)
			r.agent.Page().trace.Add(trace.Error, "scriptLoad", map[string]any{"url": u.String(), "error": loaded.err.Error()})
			return r.dispatchResourceEvent(ctx, node.ID, "error")
		}
		code, name = string(loaded.response.Body), u.String()
	} else if !r.allowsScript(nil, true, false, node.Nonce) {
		return nil
	}
	if kind != "classic" {
		r.agent.Page().trace.Add(trace.Unsupported, "document.stream.module", map[string]any{"realm": r.ID})
		return nil
	}
	previous := s.insideScript
	r.document.MarkScriptStarted(node.ID)
	s.insideScript = true
	_ = r.evaluateClassicScript(ctx, code, name, node.ID)
	s.insideScript = previous
	if src != "" {
		return r.dispatchResourceEvent(ctx, node.ID, "load")
	}
	return nil
}

func (r *Realm) finishDocumentStream(s *documentStream) error {
	s.depth++
	err := s.parser.Close(func(node dom.Node) error { return r.executeStreamScript(s, node) })
	s.depth--
	if errors.Is(err, dom.ErrStreamPaused) {
		return nil
	}
	if err != nil {
		return err
	}
	if r.documentStream != s || s.finished || !s.parser.Closed() {
		return nil
	}
	s.finished = true
	if s.navigation {
		r.updateSelectorTarget(r.documentURL().Fragment)
		if err := r.scrollToFragment(r.resourceContext); err != nil {
			return err
		}
	}
	if !s.navigation && len(s.deferred) > 0 {
		// document.close may be a cross-realm host call on an active JS stack.
		// Finish deferred execution in a Page task, after that caller unwinds.
		r.scheduler.Post(scheduler.DOM, 0, func(ctx context.Context) error {
			restore := s.useTaskContext(ctx)
			defer restore()
			s.inNavigationTask = true
			defer func() { s.inNavigationTask = false }()
			return r.finishParsedDocument(s)
		})
		return nil
	}
	return r.finishParsedDocument(s)
}

func (r *Realm) finishParsedDocument(s *documentStream) error {
	if r.documentStream != s || s.ctx.Err() != nil || r.inactive || s.completionStarted {
		return nil
	}
	r.preloadModules()
	r.preloadResources()
	r.startDocumentImages()
	if r.readyState == "loading" {
		r.SetReadyState("interactive")
	}
	resume := func() error { return r.finishParsedDocument(s) }
	if r.deferNavigationStylesheets(s, r.startParserStylesheets(), resume) {
		return nil
	}
	if waiting, err := r.runDeferredParserScripts(s, resume); waiting || err != nil {
		return err
	}
	if r.documentStream != s || s.ctx.Err() != nil || r.inactive {
		return nil
	}
	s.completionStarted = true
	if s.navigation {
		if s.onFinished != nil {
			return s.onFinished()
		}
		return nil
	}
	ctx := r.resourceContext
	for _, iframe := range r.document.FindAllByTagName("iframe") {
		if _, err := r.ensureChildFrame(iframe.ID); err != nil {
			return err
		}
	}
	r.readyState = "interactive"
	if _, err := r.runtime.Call(ctx, r.documentStreamEvent, nil, r.val("DOMContentLoaded")); err != nil {
		return err
	}
	if r.documentStream != s {
		return nil
	}
	if r.loadBlockers != 0 {
		r.requestLoad(func(ctx context.Context) { r.finishStreamLoad(ctx, s) })
		return nil
	}
	r.readyState = "complete"
	r.loadCompleted = true
	if _, err := r.runtime.Call(ctx, r.documentStreamEvent, nil, r.val("load")); err != nil {
		return err
	}
	r.finishStreamLoad(ctx, s)
	return nil
}

func (r *Realm) finishStreamLoad(ctx context.Context, s *documentStream) {
	if r.documentStream != s {
		return
	}
	r.navigationLoadEnd = r.scheduler.Now()
	p := r.agent.Page()
	p.trace.Add(trace.Lifecycle, "load", map[string]any{"frameId": r.agent.ContextID(), "url": r.documentURL().String(), "realm": r.ID})
	frame, ok := r.agent.(*Frame)
	if ok && frame.parent != nil && frame.parent.Realm != nil {
		parent := frame.parent.Realm
		if _, err := parent.runtime.Call(ctx, parent.frameLoadDispatcher, nil, parent.val(frame.elementID)); err != nil {
			p.trace.Add(trace.Error, "frameLoad", map[string]any{"error": err.Error()})
		}
	} else {
		p.mu.Lock()
		p.loadEventEnded = true
		p.mu.Unlock()
	}
	r.notifyPerformanceObservers(ctx)
}

// Task interruption and navigation cancellation have different lifetimes. A
// continuation listens to both, without retaining a completed pump task's
// context for a later response or parser continuation.
func (s *documentStream) useTaskContext(taskContext context.Context) func() {
	previous := s.navigationTaskContext
	ctx, cancel := context.WithCancel(taskContext)
	stop := context.AfterFunc(s.ctx, cancel)
	if s.ctx.Err() != nil {
		cancel()
	}
	s.navigationTaskContext = ctx
	return func() { stop(); cancel(); s.navigationTaskContext = previous }
}

func (s *documentStream) executionContext() context.Context {
	if s.navigationTaskContext != nil {
		return s.navigationTaskContext
	}
	return s.ctx
}
