package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/moreveal/mimic/internal/dom"
	"github.com/moreveal/mimic/internal/engine"
	"github.com/moreveal/mimic/internal/network"
	"github.com/moreveal/mimic/internal/scheduler"
	"github.com/moreveal/mimic/internal/trace"
)

func (p *Page) frame(id string) *Frame {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if frame := p.frames[id]; frame != nil {
		return frame
	}
	var retained func(*Realm) *Frame
	retained = func(realm *Realm) *Frame {
		if realm == nil {
			return nil
		}
		if frame := realm.retainedFrames[id]; frame != nil {
			return frame
		}
		for _, frame := range realm.retainedFrames {
			if found := retained(frame.Realm); found != nil {
				return found
			}
		}
		return nil
	}
	for _, frame := range p.frames {
		if found := retained(frame.Realm); found != nil {
			return found
		}
	}
	for _, realm := range p.realmOwners {
		if frame, ok := realm.agent.(*Frame); ok && frame.ID == id {
			return frame
		}
	}
	return nil
}

// A committed document replaces its descendant browsing contexts. The old
// Realm retains its own childFrames until deferred teardown, but those frames
// must no longer appear in the active Page tree or event-loop enumeration.
func (p *Page) removeDescendantFramesLocked(frame *Frame) {
	for _, child := range frame.children {
		p.removeDescendantFramesLocked(child)
		delete(p.frames, child.ID)
	}
	frame.children = make(map[string]*Frame)
}

func (r *Realm) ensureChildFrame(elementID int64, connectedThroughShadow ...bool) (*Frame, error) {
	shadowConnected := len(connectedThroughShadow) != 0 && connectedThroughShadow[0]
	return r.ensureChildFrameInternal(elementID, shadowConnected, true)
}

func (r *Realm) ensureChildFrameInternal(elementID int64, shadowConnected, scheduleNavigation bool) (*Frame, error) {
	if r.inactive || !r.document.IsConnected(elementID) && !shadowConnected {
		return nil, nil
	}
	if frame := r.childFrames[elementID]; frame != nil {
		if scheduleNavigation {
			r.scheduleChildFrameNavigation(frame, elementID)
		}
		return frame, r.publishWindowFrameIndices()
	}
	node, ok := r.document.Get(elementID)
	if !ok || node.TagName != "IFRAME" || (node.Parent == 0 && !shadowConnected) {
		return nil, nil
	}
	parent, ok := r.agent.(*Frame)
	if !ok {
		return nil, fmt.Errorf("only a frame realm can own an iframe")
	}
	page := r.agent.Page()
	frame := &Frame{ID: uuid.NewString(), page: page, parent: parent, elementID: elementID, children: map[string]*Frame{}, loadBlockers: map[uint64]string{}}
	document, err := dom.Parse("<html><head></head><body></body></html>")
	if err != nil {
		return nil, err
	}
	blank, _ := url.Parse("about:blank")
	// As with a Page's initial empty document, preserve canonical state now
	// and install JavaScript only if this document is actually observed.
	realm, err := newRealmState(page, frame, document, blank, true)
	if err != nil {
		return nil, err
	}
	page.ctx.mu.Lock()
	realm.origin = r.origin
	realm.referrerPolicy = r.referrerPolicy
	page.ctx.mu.Unlock()
	realm.initializeClientHints("")
	realm.documentReferrer = (network.Request{URL: r.documentURL(), Referrer: r.documentURL(), ReferrerPolicy: "unsafe-url"}).ReferrerValue()
	realm.readyState = "complete"
	frame.Realm = realm
	frame.windowName = node.Attributes["name"]
	page.mu.Lock()
	page.frames[frame.ID] = frame
	parent.children[frame.ID] = frame
	page.mu.Unlock()
	r.childFrames[elementID] = frame
	if err := r.publishWindowFrameIndices(); err != nil {
		return nil, err
	}
	page.commitHistory(frame, blank, true, nil)
	page.trace.Add(trace.Lifecycle, "frameAttached", map[string]any{"frameId": frame.ID, "parentFrameId": parent.ID, "elementNodeId": elementID, "realm": realm.ID, "initialContext": scheduleNavigation})
	if scheduleNavigation {
		r.scheduleChildFrameNavigation(frame, elementID)
	}
	return frame, nil
}

func (r *Realm) scheduleChildFrameNavigation(frame *Frame, elementID int64) {
	if frame.navigationStarted {
		return
	}
	node, ok := r.document.Get(elementID)
	if !ok {
		return
	}
	if content, present := node.Attributes["srcdoc"]; present {
		target := &url.URL{Scheme: "about", Opaque: "srcdoc"}
		r.scheduleChildNavigationContent(frame, target, frame.loaderID == "", r, &content, "navigate")
		return
	}
	src := node.Attributes["src"]
	if src == "" {
		src = "about:blank"
	}
	target, err := r.resolveDocument(src)
	blank := err == nil && target.Scheme == "about" && target.Opaque == "blank"
	if err != nil || (!blank && target.Scheme != "http" && target.Scheme != "https") {
		return
	}
	r.scheduleChildNavigationTo(frame, target, frame.loaderID == "", r, "navigate")
}

// The embedding realm owns the child navigation lifecycle, irrespective of
// whether navigation was requested by an iframe attribute or child Location.
func (r *Realm) scheduleChildNavigationTo(frame *Frame, target *url.URL, replace bool, initiator *Realm, navigationType string, traversal ...int) {
	r.scheduleChildNavigationContent(frame, target, replace, initiator, nil, navigationType, traversal...)
}

// Inline iframe content uses the same navigation sequence, parser and retirement
// path as a fetched document. Capture the attribute now: later mutations must
// cancel this navigation rather than alter the document already being committed.
func (r *Realm) scheduleChildNavigationContent(frame *Frame, target *url.URL, replace bool, initiator *Realm, content *string, navigationType string, traversal ...int) {
	request := network.Request{URL: target, SourceURL: initiator.documentURL(), Referrer: initiator.documentURL(), UserActivation: initiator.navigationActivated()}
	request.ReferrerPolicy = initiator.referrerPolicy
	r.applyChildNavigationClientHints(&request, frame, target)
	if initiator == r {
		if node, ok := r.document.Get(frame.elementID); ok {
			switch policy := strings.ToLower(node.Attributes["referrerpolicy"]); policy {
			case "no-referrer", "no-referrer-when-downgrade", "same-origin", "origin", "strict-origin", "origin-when-cross-origin", "strict-origin-when-cross-origin", "unsafe-url":
				request.ReferrerPolicy = policy
			}
		}
	}
	blank := target.Scheme == "about" && target.Opaque == "blank"
	if !blank && content == nil && target.Scheme != "http" && target.Scheme != "https" {
		return
	}
	if frame.navigationCancel != nil {
		frame.navigationCancel()
	}
	frame.navigationStarted = true
	frame.navigationPending = true
	frame.navigationSequence++
	sequence := frame.navigationSequence
	loaderID := uuid.NewString()
	blockerReason := fmt.Sprintf("iframe:%s:%d", frame.ID, sequence)
	blocksLoad := r.beginLoadBlocker(blockerReason)
	if blocksLoad {
		frame.loadBlockers[sequence] = blockerReason
	}
	if blank && frame.loaderID == "" {
		historyTarget := 0
		if len(traversal) > 0 {
			historyTarget = traversal[0]
		}
		navigation := &childNavigation{historyTarget: historyTarget, embeddingRealm: r, frame: frame, target: target, sequence: sequence, loaderID: loaderID, blockerReason: blockerReason, blocksLoad: blocksLoad, realm: frame.Realm}
		frame.loaderID = loaderID
		frame.navigationPending = false
		p := r.agent.Page()
		p.trace.Add(trace.Lifecycle, "frameNavigated", map[string]any{"frameId": frame.ID, "parentFrameId": frame.parent.ID, "loaderId": loaderID, "url": "about:blank", "realm": frame.Realm.ID})
		// Chrome 152 completes the initial empty Document and fires its owner's
		// load synchronously during insertion. Its readyState is already complete
		// when appendChild returns; this lifecycle also belongs to the frame in CDP.
		p.trace.Add(trace.Lifecycle, "DOMContentLoaded", map[string]any{"frameId": frame.ID, "loaderId": loaderID, "url": "about:blank", "realm": frame.Realm.ID})
		p.trace.Add(trace.Lifecycle, "load", map[string]any{"frameId": frame.ID, "loaderId": loaderID, "url": "about:blank", "realm": frame.Realm.ID})
		if r.frameLoadDispatcher != nil {
			p.trace.Add(trace.Lifecycle, "iframeOwnerLoad", map[string]any{"frameId": frame.ID, "elementNodeId": frame.elementID, "realm": r.ID})
			if _, err := r.runtime.Call(r.resourceContext, r.frameLoadDispatcher, nil, r.runtime.Value(frame.elementID)); err != nil {
				p.trace.Add(trace.Error, "frameLoad", map[string]any{"frameId": frame.ID, "url": "about:blank", "error": err.Error()})
			}
		}
		r.finishChildNavigation(navigation)
		return
	}
	r.scheduler.Post(scheduler.Navigation, 0, func(ctx context.Context) error {
		if blank || content != nil {
			historyTarget := 0
			if len(traversal) > 0 {
				historyTarget = traversal[0]
			}
			navigation := &childNavigation{historyTarget: historyTarget, embeddingRealm: r, frame: frame, target: target, sequence: sequence, loaderID: loaderID, blockerReason: blockerReason, blocksLoad: blocksLoad, replace: replace, navigationType: navigationType}
			body := "<html><head></head><body></body></html>"
			if content != nil {
				body = *content
			}
			return r.commitChildFrameNavigation(ctx, navigation, network.Response{URL: target, Body: []byte(body), Referrer: request.ReferrerValue()}, nil)
		}
		r.startChildFrameNavigation(frame, target, sequence, loaderID, blockerReason, blocksLoad, replace, request, navigationType, traversal...)
		return nil
	})
}

func (r *Realm) childFrameAttributeChanged(id int64, name string) {
	if !strings.EqualFold(name, "src") && !strings.EqualFold(name, "srcdoc") {
		return
	}
	if strings.EqualFold(name, "src") {
		if node, ok := r.document.Get(id); ok {
			if _, present := node.Attributes["srcdoc"]; present {
				return
			}
		}
	}
	if frame := r.childFrames[id]; frame != nil {
		frame.navigationStarted = false
		r.scheduleChildFrameNavigation(frame, id)
	}
}

type childNavigation struct {
	historyTarget     int
	navigationType    string
	performanceOrigin time.Time
	embeddingRealm    *Realm
	frame             *Frame
	target            *url.URL
	sequence          uint64
	loaderID          string
	blockerReason     string
	blocksLoad        bool
	replace           bool
	document          *dom.Document
	realm             *Realm
}

func (r *Realm) browserEventLoop() *scheduler.Scheduler {
	p := r.agent.Page()
	if p.Top != nil && p.Top.Realm != nil {
		return p.Top.Realm.scheduler
	}
	return r.scheduler
}

func (r *Realm) childNavigationCurrent(navigation *childNavigation) bool {
	return navigation != nil && navigation.frame != nil &&
		navigation.frame.navigationSequence == navigation.sequence &&
		r.childFrames[navigation.frame.elementID] == navigation.frame
}

func (r *Realm) finishChildNavigation(navigation *childNavigation) {
	if navigation != nil && navigation.blocksLoad {
		navigation.blocksLoad = false
		frame := navigation.frame
		if frame != nil {
			if reason, exists := frame.loadBlockers[navigation.sequence]; exists {
				delete(frame.loadBlockers, navigation.sequence)
				r.endLoadBlocker(reason)
			}
		}
	}
}

// prepareChildFrameRemoval stops an iframe from delaying its owner Document's
// load before the DOM mutation disconnects the element. Chrome synchronously
// dispatches the owner's load when this removes the final blocker, so the
// listener still observes a connected iframe and a live contentWindow.
func (r *Realm) prepareChildFrameRemoval(elementID int64) bool {
	frame := r.childFrames[elementID]
	if frame == nil || len(frame.loadBlockers) == 0 {
		return false
	}
	sequences := make([]uint64, 0, len(frame.loadBlockers))
	for sequence := range frame.loadBlockers {
		sequences = append(sequences, sequence)
	}
	sort.Slice(sequences, func(i, j int) bool { return sequences[i] < sequences[j] })
	for _, sequence := range sequences {
		reason := frame.loadBlockers[sequence]
		delete(frame.loadBlockers, sequence)
		if r.loadBlockers > 0 {
			r.loadBlockers--
		}
		r.agent.Page().trace.Add(trace.Lifecycle, "loadBlockerRemoved", map[string]any{"reason": reason, "count": r.loadBlockers, "realm": r.ID})
	}
	if !r.loadRequested || r.loadCompleted || r.loadScheduled || r.loadBlockers != 0 {
		r.scheduleLoadIfReady()
		return false
	}
	r.readyState = "complete"
	r.loadCompleted = true
	r.agent.Page().trace.Add(trace.Lifecycle, "readyStateComplete", map[string]any{"realm": r.ID})
	return true
}

func (r *Realm) startChildFrameNavigation(frame *Frame, target *url.URL, sequence uint64, loaderID, blockerReason string, blocksLoad, replace bool, request network.Request, navigationType string, traversal ...int) {
	p := r.agent.Page()
	performanceOrigin := p.ClockNow()
	historyTarget := 0
	if len(traversal) > 0 {
		historyTarget = traversal[0]
	}
	navigation := &childNavigation{historyTarget: historyTarget, embeddingRealm: r, frame: frame, target: target, sequence: sequence, loaderID: loaderID, blockerReason: blockerReason, blocksLoad: blocksLoad, replace: replace, navigationType: navigationType}
	navigation.performanceOrigin = performanceOrigin
	if !r.childNavigationCurrent(navigation) {
		r.finishChildNavigation(navigation)
		return
	}
	eventLoop := r.browserEventLoop()
	loadContext, cancel := context.WithCancel(r.resourceContext)
	frame.navigationCancel = cancel
	request.ID, request.ContextID, request.URL, request.Initiator = loaderID, frame.ID, target, network.Iframe
	r.resourceWG.Add(1)
	go func() {
		defer r.resourceWG.Done()
		defer cancel()
		res, err := p.loader.Load(loadContext, r.withResourceTiming(request))
		if loadContext.Err() != nil {
			return
		}
		eventLoop.Post(scheduler.Network, 0, func(ctx context.Context) error {
			return r.commitChildFrameNavigation(ctx, navigation, res, err)
		})
	}()
}

func (r *Realm) commitChildFrameNavigation(ctx context.Context, navigation *childNavigation, res network.Response, loadErr error) error {
	p := r.agent.Page()
	if !r.childNavigationCurrent(navigation) {
		r.finishChildNavigation(navigation)
		return nil
	}
	if loadErr != nil {
		p.trace.Add(trace.Error, "frameLoad", map[string]any{"frameId": navigation.frame.ID, "url": navigation.target.String(), "error": loadErr.Error()})
		r.finishChildNavigation(navigation)
		return nil
	}
	documentURL := navigation.target
	if res.URL != nil {
		documentURL = res.URL
	}
	document, err := dom.Parse("")
	if err != nil {
		r.finishChildNavigation(navigation)
		return err
	}
	if navigation.performanceOrigin.IsZero() {
		navigation.performanceOrigin = p.ClockNow()
	}
	p.advanceNavigationResponseClock(navigation.performanceOrigin, res)
	realm, err := newRealmStateWithNavigation(p, navigation.frame, document, documentURL, true, navigation.performanceOrigin, navigation.loaderID, realmNavigationPolicies{permissionsPolicy: res.Headers.Get("Permissions-Policy"), originAgentCluster: res.Headers.Get("Origin-Agent-Cluster")})
	if err != nil {
		r.finishChildNavigation(navigation)
		return err
	}
	if documentURL.Scheme != "about" {
		realm.policy = r.agent.Page().responseCSP(res.Headers)
	}
	if navigation.navigationType != "" {
		realm.navigationType = navigation.navigationType
	}
	if documentURL.Scheme == "about" && documentURL.Opaque == "blank" {
		p.ctx.mu.Lock()
		realm.origin = r.origin
		p.ctx.mu.Unlock()
	}
	realm.documentReferrer = res.Referrer
	realm.referrerPolicy = res.Headers.Get("Referrer-Policy")
	realm.lastModified, _ = http.ParseTime(res.Headers.Get("Last-Modified"))
	realm.initializeClientHints(res.Headers.Get("Permissions-Policy"))
	if documentURL.Scheme == "about" {
		realm.referrerPolicy = r.referrerPolicy
	}
	old := navigation.frame.Realm
	p.mu.Lock()
	p.removeDescendantFramesLocked(navigation.frame)
	navigation.frame.Realm = realm
	navigation.frame.loaderID = navigation.loaderID
	p.mu.Unlock()
	p.mu.Lock()
	if p.historyIndex >= 0 {
		realm.navigationActivationFrom = p.history[p.historyIndex].frames[navigation.frame.ID]
	}
	p.mu.Unlock()
	realm.navigationActivationType = "push"
	if navigation.replace {
		realm.navigationActivationType = "replace"
	}
	if navigation.navigationType == "reload" {
		realm.navigationActivationType = "reload"
	}
	if navigation.historyTarget > 0 {
		realm.navigationActivationType = "traverse"
	}
	p.commitHistory(navigation.frame, documentURL, navigation.replace, nil, navigation.historyTarget)
	if navigation.navigationType == "reload" && navigation.historyTarget == 0 && realm.navigationActivationFrom != nil {
		p.mu.Lock()
		previous := realm.navigationActivationFrom
		previous.ensureNavigationIdentity()
		next := p.history[p.historyIndex].frames[navigation.frame.ID]
		next.navigationID = previous.navigationID
		next.navigationKey = previous.navigationKey
		next.navigationState = previous.navigationState
		next.storageData = previous.storageData
		p.mu.Unlock()
	}

	p.retireRealm(old)
	navigation.document = document
	navigation.realm = realm
	// The commit callback's pump context ends at its task boundary; the
	// parser can remain paused on an external script across many later turns.
	// Its cancellation belongs to the containing navigation/document lifetime.
	streamState, err := realm.initializeNavigationStream(r.resourceContext)
	if err != nil {
		r.finishChildNavigation(navigation)
		return err
	}
	streamState.onScript = func(node dom.Node) error {
		return r.executeChildNavigationScript(streamState.executionContext(), navigation, streamState, node)
	}
	streamState.onFinished = func() error { return r.finishChildFrameParsing(streamState.ctx, navigation) }
	restoreTaskContext := streamState.useTaskContext(ctx)
	defer restoreTaskContext()
	p.trace.Add(trace.Lifecycle, "frameNavigated", map[string]any{"frameId": navigation.frame.ID, "parentFrameId": navigation.frame.parent.ID, "loaderId": navigation.loaderID, "url": documentURL.String(), "realm": realm.ID})
	p.runInitScripts(ctx, realm)
	if err := realm.writeDocumentStream(realm, string(res.Body)); err != nil {
		return err
	}
	return realm.closeDocumentStream()
}

func (r *Realm) finishChildFrameParsing(ctx context.Context, navigation *childNavigation) error {
	if !r.childNavigationCurrent(navigation) || navigation.frame.Realm != navigation.realm {
		r.finishChildNavigation(navigation)
		return nil
	}
	p := r.agent.Page()
	navigation.frame.navigationPending = false
	for _, message := range navigation.frame.pendingMessages {
		r.queueFrameMessage(navigation.frame.ID, message)
	}
	navigation.frame.pendingMessages = nil
	if navigation.realm.readyState == "loading" {
		navigation.realm.SetReadyState("interactive")
	}
	eventLoop := r.browserEventLoop()
	eventLoop.Post(scheduler.DOM, 0, func(eventContext context.Context) error {
		if !r.childNavigationCurrent(navigation) || navigation.frame.Realm != navigation.realm {
			r.finishChildNavigation(navigation)
			return nil
		}
		// Queue the Window load turn before DOMContentLoaded listeners enqueue
		// posted-message tasks. Chrome completes the child and fires the owner
		// iframe load before those newly posted messages are delivered.
		eventLoop.Post(scheduler.DOM, 0, func(loadContext context.Context) error {
			return r.completeChildFrameLoad(loadContext, navigation)
		})
		navigation.realm.performanceLifecycle("domContentLoadedEventStart")
		_, eventErr := navigation.realm.Evaluate(eventContext, `document.dispatchEvent(new Event('DOMContentLoaded'))`, "mimic:frame-dom-content-loaded")
		navigation.realm.performanceLifecycle("domContentLoadedEventEnd")
		if eventErr != nil {
			return eventErr
		}
		p.trace.Add(trace.Lifecycle, "DOMContentLoaded", map[string]any{"frameId": navigation.frame.ID, "loaderId": navigation.loaderID, "url": navigation.realm.documentURL().String(), "realm": navigation.realm.ID})
		// Promise reactions belong to this DCL task, including its interrupt
		// boundary, rather than the parser's longer navigation lifetime.
		return navigation.realm.checkpoint(eventContext)
	})
	return nil
}

func (r *Realm) completeChildFrameLoad(ctx context.Context, navigation *childNavigation) error {
	if !r.childNavigationCurrent(navigation) || navigation.frame.Realm != navigation.realm {
		r.finishChildNavigation(navigation)
		return nil
	}
	p := r.agent.Page()
	navigation.realm.SetReadyState("complete")
	navigation.realm.performanceLifecycle("loadEventStart")
	if _, eventErr := navigation.realm.Evaluate(ctx, `dispatchEvent(new Event('load'))`, "mimic:frame-load"); eventErr != nil {
		r.finishChildNavigation(navigation)
		return eventErr
	}
	if checkpointErr := navigation.realm.checkpoint(ctx); checkpointErr != nil {
		r.finishChildNavigation(navigation)
		return checkpointErr
	}
	navigation.realm.navigationLoadEnd = navigation.realm.scheduler.Now()
	p.trace.Add(trace.Lifecycle, "load", map[string]any{"frameId": navigation.frame.ID, "loaderId": navigation.loaderID, "url": navigation.realm.documentURL().String(), "realm": navigation.realm.ID})
	navigation.realm.notifyPerformanceObservers(ctx)
	if r.frameLoadDispatcher != nil {
		p.trace.Add(trace.Lifecycle, "iframeOwnerLoad", map[string]any{"frameId": navigation.frame.ID, "elementNodeId": navigation.frame.elementID, "realm": r.ID})
		if _, err := r.runtime.Call(ctx, r.frameLoadDispatcher, r.runtime.Get("window"), r.runtime.Value(navigation.frame.elementID)); err != nil {
			r.finishChildNavigation(navigation)
			return err
		}
	}
	r.finishChildNavigation(navigation)
	// The owner load callback ran in the embedding realm, which can differ
	// from both the loaded document and the Page scheduler's top realm. Drain
	// its jobs now; otherwise nested-frame promises wait for an unrelated timer.
	return r.checkpoint(ctx)
}

func (r *Realm) canAccess(frame *Frame) bool {
	return frame != nil && frame.Realm != nil && frame.Realm.origin == r.origin
}

func (r *Realm) detachChildFrame(elementID int64) {
	frame := r.childFrames[elementID]
	if frame == nil {
		return
	}
	if frame.navigationCancel != nil {
		frame.navigationCancel()
	}
	if frame.Realm != nil {
		frame.Realm.deactivateContext(true)
	}
	delete(r.childFrames, elementID)
	page := r.agent.Page()
	page.mu.Lock()
	page.removeDescendantFramesLocked(frame)
	delete(page.frames, frame.ID)
	if frame.parent != nil {
		delete(frame.parent.children, frame.ID)
	}
	page.mu.Unlock()
	if err := r.publishWindowFrameIndices(); err != nil {
		page.trace.Add(trace.Error, "windowFrameIndexPublicationFailed", map[string]any{"error": err.Error()})
	}
	// Removing an iframe detaches its browsing context from the active frame
	// tree, but references to its WindowProxy/functions keep the Window/Realm
	// alive. Index it for lookup; owner collection traces only exported references.
	r.retainedFrames[frame.ID] = frame
	page.trace.Add(trace.Lifecycle, "frameDetached", map[string]any{"frameId": frame.ID, "elementNodeId": elementID})
}

// Reflect the canonical connected frame tree at the mutation boundary. Reading
// Window.length must not be necessary to refresh indexed Window properties.
func (r *Realm) publishWindowFrameIndices() error {
	if r.frameIndexNotifier == nil {
		return nil
	}
	// Retained detached Windows can initialize their language projection after
	// resource cancellation. This publication performs no resource operation.
	value, err := r.runtime.Call(context.Background(), r.frameIndexNotifier, nil)
	releaseRuntimeValues(r.runtime, value)
	return err
}

func (r *Realm) evalInFrame(ctx context.Context, frameID, source string) (engine.Value, error) {
	frame := r.agent.Page().frame(frameID)
	if !r.canAccess(frame) {
		return nil, fmt.Errorf("SecurityError: Blocked cross-origin frame access")
	}
	r.agent.Page().trace.Add(trace.JS, "frameEvalStart", map[string]any{"frameId": frameID, "realm": frame.Realm.ID, "sourceLength": len(source)})
	value, err := func() (engine.Value, error) {
		restore := r.enterFrameDocumentEntry(frame.Realm)
		defer restore()
		return frame.Realm.Evaluate(ctx, source, "frame-eval")
	}()
	if err != nil {
		return nil, err
	}
	r.agent.Page().trace.Add(trace.JS, "frameEvalResult", map[string]any{"frameId": frameID, "realm": frame.Realm.ID, "type": frame.Realm.runtime.TypeOf(value), "sourceLength": len(source)})
	// Native eval returns synchronously, including when the result is a Promise.
	// The Page event loop owns subsequent jobs; pumping it here can reenter a
	// scheduler that is already running the caller's script task.
	return value, nil
}

func (r *Realm) crossRealmValue(value engine.Value) (map[string]any, error) {
	if value == nil || r.frameValueEncoder == nil {
		return r.describeCrossRealmValue(value)
	}
	encoded, err := r.runtime.Call(context.Background(), r.frameValueEncoder, nil, value)
	if err != nil {
		return nil, err
	}
	var data map[string]any
	if r.frameValueEncoderJSON {
		if err := json.Unmarshal([]byte(encoded.String()), &data); err != nil {
			return nil, fmt.Errorf("invalid cross-realm value description: %w", err)
		}
		if data["__mimicCrossRealm"] == "special-number" {
			switch data["value"] {
			case "NaN":
				data["value"] = math.NaN()
			case "Infinity":
				data["value"] = math.Inf(1)
			case "-Infinity":
				data["value"] = math.Inf(-1)
			case "-0":
				data["value"] = math.Copysign(0, -1)
			}
			data["__mimicCrossRealm"] = "value"
		}
		return data, nil
	}
	data, ok := encoded.Export().(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid cross-realm value description")
	}
	return data, nil
}

func (r *Realm) describeCrossRealmValue(value engine.Value) (map[string]any, error) {
	if value == nil {
		return map[string]any{"__mimicCrossRealm": "undefined"}, nil
	}
	typeName := r.runtime.TypeOf(value)
	if typeName == "undefined" && r.runtime.StrictEqual(value, r.runtime.Get("undefined")) {
		return map[string]any{"__mimicCrossRealm": "undefined"}, nil
	}
	if typeName == "undefined" {
		typeName = "undetectable"
	}
	if typeName == "object" && r.runtime.StrictEqual(value, r.val(nil)) {
		return map[string]any{"__mimicCrossRealm": "null"}, nil
	}
	if typeName == "symbol" {
		info, err := r.callFrameReflection(context.Background(), "symbol", value, nil, nil)
		if err != nil {
			return nil, err
		}
		{
			encoded := r.encodeFrameKey(info)
			encoded["__mimicCrossRealm"] = "symbol"
			return encoded, nil
		}
	}
	if typeName == "bigint" {
		return map[string]any{"__mimicCrossRealm": "bigint", "value": value.String()}, nil
	}
	if (typeName == "object" || typeName == "function" || typeName == "undetectable") && r.frameReferenceDescribe != nil {
		info, err := r.runtime.Call(context.Background(), r.frameReferenceDescribe, nil, value)
		if err != nil {
			return nil, err
		}
		if r.runtime.TypeOf(info) == "object" && !r.runtime.StrictEqual(info, r.val(nil)) {
			if reference, ok := info.Export().(map[string]any); ok {
				if reference["__mimicCrossRealm"] == nil {
					reference["__mimicCrossRealm"] = reference["type"]
				}
				return reference, nil
			}
		}
	}
	if r.runtime.StrictEqual(value, r.runtime.Get("globalThis")) {
		return map[string]any{"__mimicCrossRealm": "window", "frame": r.agent.ContextID()}, nil
	}
	if r.runtime.StrictEqual(value, r.runtime.Get("parent")) {
		frame, _ := r.agent.(*Frame)
		if frame != nil && frame.parent != nil {
			return map[string]any{"__mimicCrossRealm": "window", "frame": frame.parent.ID}, nil
		}
	}
	if typeName != "object" && typeName != "function" && typeName != "undetectable" {
		return map[string]any{"__mimicCrossRealm": "value", "value": value.Export()}, nil
	}
	shape := func(id int64) (map[string]any, error) {
		out := map[string]any{"__mimicCrossRealm": typeName, "frame": r.agent.ContextID(), "realm": r.ID, "handle": id}
		if r.runtime.StrictEqual(value, r.runtime.Get("document")) {
			out["document"] = true
		}
		if r.frameNodeDescribe != nil && typeName == "object" {
			value, err := r.runtime.Call(context.Background(), r.frameNodeDescribe, nil, value)
			if err != nil {
				return nil, err
			}
			if nodeID := numberValue(value.Export()); nodeID != 0 {
				out["nodeId"] = nodeID
			}
		}
		metadata, err := r.callFrameReflection(context.Background(), "shape", value, nil, nil)
		if err != nil {
			return nil, err
		}
		{
			name := "array"
			if typeName == "function" {
				name = "constructable"
			}
			out[name] = r.runtime.GetProperty(metadata, name).Export()
		}
		return out, nil
	}
	// The captured WeakMap indexes identity inside the owning JS realm. A Go
	// scan would make every lookup perform O(n) native isolate crossings.
	candidate := r.crossValueSeq + 1
	r.crossValueSeq = candidate
	identity, err := r.callFrameReflection(context.Background(), "handle", value, r.val(candidate), nil)
	if err != nil {
		return nil, err
	}
	id := int64(numberValue(identity.Export()))
	if r.crossValues[id] == nil {
		r.crossValues[id] = value
	}
	return shape(id)
}

func (r *Realm) postToFrame(frameID string, data any, targetOrigin string, portIDs []string) error {
	page := r.agent.Page()
	frame := page.frame(frameID)
	if frame == nil || frame.Realm == nil {
		return nil
	}
	if targetOrigin != "*" && targetOrigin != "/" && targetOrigin != frame.Realm.origin {
		return nil
	}
	sourceID := r.agent.ContextID()
	encoded, _ := json.Marshal(data)
	page.trace.Add(trace.Lifecycle, "frameMessagePosted", map[string]any{"sourceFrameId": sourceID, "targetFrameId": frameID, "valueType": fmt.Sprintf("%T", data), "valueShape": messageValueShape(data, 0), "encodedSize": len(encoded), "portCount": len(portIDs)})
	message := frameMessage{sourceFrameID: sourceID, origin: r.origin, data: data, portIDs: append([]string(nil), portIDs...)}
	if frame.navigationPending {
		frame.pendingMessages = append(frame.pendingMessages, message)
		return nil
	}
	r.queueFrameMessage(frameID, message)
	return nil
}

func messageValueShape(value any, depth int) any {
	if depth >= 2 {
		return fmt.Sprintf("%T", value)
	}
	switch value := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		shape := make(map[string]any, len(keys))
		for _, key := range keys {
			shape[key] = messageValueShape(value[key], depth+1)
		}
		return shape
	case []any:
		shape := make([]any, 0, min(len(value), 8))
		for index := 0; index < len(value) && index < 8; index++ {
			shape = append(shape, messageValueShape(value[index], depth+1))
		}
		return map[string]any{"length": len(value), "items": shape}
	case nil:
		return "null"
	case bool:
		return "boolean"
	case string:
		return "string"
	case float64, float32, int, int32, int64, uint, uint32, uint64:
		return "number"
	default:
		return fmt.Sprintf("%T", value)
	}
}

type frameMessage struct {
	sourceFrameID string
	origin        string
	data          any
	portIDs       []string
}

func (r *Realm) queueFrameMessage(frameID string, message frameMessage) {
	page := r.agent.Page()
	eventLoop := r.scheduler
	if page.Top != nil && page.Top.Realm != nil {
		eventLoop = page.Top.Realm.scheduler
	}
	eventLoop.Post(scheduler.PostedMessage, 0, func(ctx context.Context) error {
		// postMessage targets a WindowProxy, not the Window which happened to
		// back it when the call was made. Resolve the active realm at delivery
		// time so a navigation commit cannot strand the task in about:blank.
		target := page.frame(frameID)
		if target == nil || target.Realm == nil {
			return nil
		}
		page.transferMessagePorts(message.portIDs, target.Realm)
		fn := target.Realm.messageReceiver
		_, err := target.Realm.runtime.Call(ctx, fn, target.Realm.runtime.Get("window"), target.Realm.runtime.Value(message.data), target.Realm.runtime.Value(message.origin), target.Realm.runtime.Value(message.sourceFrameID), target.Realm.runtime.Value(message.portIDs))
		if err != nil {
			return err
		}
		// Delivery runs on the shared browser loop, but Promise jobs belong
		// to the receiving realm. Drain them before the next message task.
		return target.Realm.checkpoint(ctx)
	})
}
