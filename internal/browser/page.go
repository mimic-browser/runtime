package browser

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/moreveal/mimic/compatibility"
	"github.com/moreveal/mimic/internal/dom"
	"github.com/moreveal/mimic/internal/engine"
	"github.com/moreveal/mimic/internal/network"
	"github.com/moreveal/mimic/internal/scheduler"
	"github.com/moreveal/mimic/internal/state"
	"github.com/moreveal/mimic/internal/textmetrics"
	"github.com/moreveal/mimic/internal/trace"
)

type InitScript struct{ ID, Source, WorldName string }
type documentSecurity struct {
	secureContext       bool
	crossOriginIsolated bool
	credentialless      bool
	originAgentCluster  bool
	permissionsPolicy   string
}
type Page struct {
	geolocationOverride *GeolocationOverride // Protected by mu; persists across navigation.

	previewObservers   map[*PreviewSubscription]struct{} // command-owned; nil without viewers
	debuggers          map[*Debugger]struct{}
	inputIgnored       bool   // Page command owned; survives document navigation.
	inputHintRealm     *Realm // Last protocol-scrolled target; Page command owned.
	inputHintNodeID    int64
	focusedFrameID     string // Page-owned focus chain; empty selects the top document.
	pageFocused        bool
	focusEmulated      bool
	debuggerWaitMu     sync.Mutex
	debuggerProgress   chan struct{}
	launches           []string
	performanceClamper performanceClamper
	commandMu          sync.Mutex
	commandWaiters     atomic.Int32
	previewScheduleMu  sync.Mutex
	previewScheduled   bool
	eventLoopWake      chan struct{} // Coalesced readiness hints; never executes Page work.
	taskSequence       atomic.Uint64
	mu                 sync.RWMutex
	ID                 string
	ctx                *Context
	env                state.Environment
	loader             *network.Loader
	trace              *trace.Recorder
	downloads          map[string]Download
	Top                *Frame
	loaderID           string
	navigationCancel   context.CancelFunc
	clock              time.Time
	activeClock        atomic.Pointer[scheduler.Scheduler]
	performanceOrigin  time.Time
	proxy              *WindowProxy
	current            *url.URL
	history            []*sessionHistoryEntry
	historyIndex       int
	historySequence    int
	initScripts        []InitScript
	sessionStorage     map[string]map[string]string
	bypassCSP          bool
	frames             map[string]*Frame
	// retiredRealms keeps document realms alive until the browser task which
	// initiated a navigation has unwound. Closing the old JS runtime while one
	// of its callbacks is still on the stack is observably different from
	// Chrome and also makes the scheduler's microtask checkpoint fail.
	retiredRealms         []*Realm
	realmOwners           map[string]*Realm
	realmEvaluationDepth  int
	directEvaluationDepth int
	documentSecurity      documentSecurity
	loadEventEnded        bool
	messagePorts          map[string]*messagePortState
	textMetrics           *textmetrics.Engine // Page event-loop owned; lazy local font resources.
	// Cross-realm calls can enqueue jobs in an isolate other than the caller's.
	// These fields are owned by the Page event loop, never by network goroutines.
	pendingCheckpoints  []*Realm
	checkpointDraining  bool
	userScriptDepth     int
	databaseScriptEpoch uint64
	crossRealmDepth     int
}

// LockCommands serializes an external command and its complete event-loop turn.
// The boundary belongs to the Page, so all CDP sessions observe the same loop.
// Library callers must use the same boundary when sharing a Page concurrently.
func (p *Page) LockCommands() { p.commandMu.Lock() }

// LockExternalCommand records protocol/library commands while they wait for
// the Page turn boundary. The background event-loop pump uses this signal to
// yield between tasks instead of repeatedly reacquiring the mutex ahead of an
// already queued user command.
func (p *Page) LockExternalCommand() {
	p.commandWaiters.Add(1)
	p.commandMu.Lock()
	p.commandWaiters.Add(-1)
}

// ExternalCommandWaiting reports whether an external command is queued at the
// Page turn boundary. It is only a scheduling hint; commandMu remains the sole
// ownership boundary for Page state.
func (p *Page) ExternalCommandWaiting() bool { return p.commandWaiters.Load() != 0 }

// EventLoopWake lets an external pump react to newly posted work without
// consuming scheduler waits or changing the Page's canonical clock.
func (p *Page) EventLoopWake() <-chan struct{} { return p.eventLoopWake }

// WakeEventLoop is safe from resource goroutines and after teardown. The channel
// stays open: a hint cannot extend the Page lifetime or bypass its command lock.
func (p *Page) WakeEventLoop() {
	select {
	case p.eventLoopWake <- struct{}{}:
	default:
	}
}

func (p *Page) UnlockCommands() {
	hasPreview := p.previewObservers != nil
	p.commandMu.Unlock()
	if hasPreview {
		p.schedulePreviewPublish()
	}
}

// UnlockCommandsWithoutPreview completes an intermediate protocol input phase.
// A mouse gesture is delivered as move/down/up commands; serializing the whole
// preview between those phases blocks the gesture and exposes frames Chrome
// never paints. The completed release publishes the coalesced state.
func (p *Page) UnlockCommandsWithoutPreview() { p.commandMu.Unlock() }

func (p *Page) schedulePreviewPublish() {
	p.previewScheduleMu.Lock()
	if p.previewScheduled {
		p.previewScheduleMu.Unlock()
		return
	}
	p.previewScheduled = true
	p.previewScheduleMu.Unlock()
	go func() {
		// Coalesce bursts, but never restart this delay: continuous Page turns
		// would otherwise starve even the first snapshot. Read the latest state
		// under the command lock; slow viewers still use a one-item mailbox.
		time.Sleep(50 * time.Millisecond)
		p.commandMu.Lock()
		if p.previewObservers != nil {
			p.publishPreview()
		}
		p.previewScheduleMu.Lock()
		p.previewScheduled = false
		p.previewScheduleMu.Unlock()
		p.commandMu.Unlock()
	}()
}

func newPage(c *Context) (*Page, error) {
	environment := c.env.Fork()
	p := &Page{performanceClamper: newPerformanceClamper(), ID: uuid.NewString(), ctx: c, env: environment, trace: trace.New(), historyIndex: -1, clock: environment.Time.WallOrigin, performanceOrigin: environment.Time.WallOrigin, sessionStorage: map[string]map[string]string{}, frames: map[string]*Frame{}, messagePorts: map[string]*messagePortState{}, pageFocused: true}
	p.trace.SetObservationChange(p.setPropertyTraceEnabled)
	p.eventLoopWake = make(chan struct{}, 1)
	p.loader = network.NewLoaderWithSession(func() state.Environment { p.mu.RLock(); defer p.mu.RUnlock(); return p.env }, c.cookies, c.network, p.trace)
	p.loader.SetResourcePolicy(c.resourcePolicy)
	if c.transport != nil {
		p.loader.SetTransport(c.transport)
	}
	// Chrome identifies the page target and its top-level frame with the same ID.
	p.Top = &Frame{ID: p.ID, page: p, children: map[string]*Frame{}}
	p.frames[p.Top.ID] = p.Top
	p.proxy = &WindowProxy{frame: p.Top}
	return p, nil
}
func (p *Page) Compatibility() compatibility.Bundle { return p.ctx.browser.Compatibility() }
func (p *Page) initBlank() error {
	d, err := dom.Parse("<html><head></head><body></body></html>")
	if err != nil {
		return err
	}
	u, _ := url.Parse("about:blank")
	r, err := newRealmState(p, p.Top, d, u, true)
	if err != nil {
		return err
	}
	r.readyState = "complete"
	p.Top.Realm = r
	p.loaderID = uuid.NewString()
	p.current = u
	p.history = []*sessionHistoryEntry{{URL: u, frames: map[string]*historyFrameState{p.Top.ID: {url: u, realmID: r.ID}}}}
	p.historyIndex = 0
	p.loadEventEnded = true
	return nil
}
func (p *Page) Trace() *trace.Recorder                { return p.trace }
func (p *Page) Loader() *network.Loader               { return p.loader }
func (p *Page) Cookies() *network.CookieStore         { return p.ctx.cookies }
func (p *Page) NetworkSession() *network.SessionState { return p.ctx.network }
func (p *Page) Close() error {
	p.CancelNavigation()
	defer p.loader.CloseOwnedTransport()
	p.mu.Lock()
	p.Top.Realm = nil
	p.launches = nil
	realms := make([]*Realm, 0, len(p.realmOwners))
	for _, r := range p.realmOwners {
		realms = append(realms, r)
	}
	p.realmOwners = nil
	p.retiredRealms = nil
	p.frames = make(map[string]*Frame)
	p.history = nil
	p.historyIndex = -1
	p.mu.Unlock()
	for _, r := range realms {
		_ = r.Close()
	}
	p.loader.CloseResponseBodies()
	if p.textMetrics != nil {
		_ = p.textMetrics.Close()
		p.textMetrics = nil
	}
	p.pendingCheckpoints = nil
	return nil
}
func (p *Page) URL() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.current == nil {
		return "about:blank"
	}
	return p.current.String()
}
func (p *Page) LoaderID() string { p.mu.RLock(); defer p.mu.RUnlock(); return p.loaderID }
func (p *Page) ClockNow() time.Time {
	p.mu.RLock()
	now := p.clock
	realm := p.Top.Realm
	p.mu.RUnlock()
	if active := p.activeClock.Load(); active != nil {
		if observed := active.Now(); observed.After(now) {
			now = observed
		}
	}
	if realm != nil {
		if realmNow := realm.scheduler.Now(); realmNow.After(now) {
			return realmNow
		}
	}
	return now
}
func (p *Page) PerformanceOrigin() time.Time {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.performanceOrigin
}
func (p *Page) LoadEventEnded() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.loadEventEnded
}
func (p *Page) Title() string {
	p.mu.RLock()
	r := p.Top.Realm
	p.mu.RUnlock()
	if r == nil || r.document == nil {
		return ""
	}
	return r.document.Title()
}
func (p *Page) AddInitScript(source string) string {
	return p.AddInitScriptWorld(source, "")
}

func (p *Page) AddInitScriptWorld(source, worldName string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	id := uuid.NewString()
	p.initScripts = append(p.initScripts, InitScript{ID: id, Source: source, WorldName: worldName})
	return id
}

func (p *Page) runInitScripts(ctx context.Context, realm *Realm) {
	p.mu.RLock()
	scripts := append([]InitScript(nil), p.initScripts...)
	p.mu.RUnlock()
	for _, script := range scripts {
		target := realm
		if script.WorldName != "" {
			world, err := p.isolatedWorld(ctx, realm, script.WorldName)
			if err != nil {
				p.trace.Add(trace.Exception, "initScript", map[string]any{"scriptId": script.ID, "error": err.Error(), "realm": realm.ID})
				continue
			}
			target = world
		}
		// Creating a named world is observable through CDP, but a script made
		// exclusively of whitespace/comments has no JavaScript effects. Keep its
		// realm logical and deferred until the first actual operation targets it.
		if semanticallyEmptyScript(script.Source) {
			continue
		}
		if _, err := target.Evaluate(ctx, script.Source, "mimic:init-script"); err != nil {
			p.trace.Add(trace.Exception, "initScript", map[string]any{"scriptId": script.ID, "error": err.Error(), "realm": realm.ID})
			continue
		}
		if err := target.checkpoint(ctx); err != nil {
			p.trace.Add(trace.Error, "initScriptMicrotaskCheckpoint", map[string]any{"scriptId": script.ID, "error": err.Error(), "realm": realm.ID})
		}
	}
}

func semanticallyEmptyScript(source string) bool {
	for at := 0; at < len(source); {
		switch source[at] {
		case ' ', '\t', '\r', '\n', '\f', '\v':
			at++
		case '/':
			if at+1 >= len(source) {
				return false
			}
			if source[at+1] == '/' {
				at += 2
				for at < len(source) && source[at] != '\r' && source[at] != '\n' {
					at++
				}
				continue
			}
			if source[at+1] != '*' {
				return false
			}
			end := strings.Index(source[at+2:], "*/")
			if end < 0 {
				return false
			}
			at += end + 4
		default:
			return false
		}
	}
	return true
}
func (p *Page) SetBypassCSP(bypass bool) { p.mu.Lock(); p.bypassCSP = bypass; p.mu.Unlock() }
func (r *Realm) allowsScript(resource *url.URL, inline, dynamic bool, nonce string) bool {
	p := r.agent.Page()
	policy, documentURL := r.contentPolicy(), r.documentURL()
	p.mu.RLock()
	bypass := p.bypassCSP
	p.mu.RUnlock()
	allowed, reason := true, "CSP bypass enabled"
	if !bypass {
		allowed, reason = policy.AllowsScript(documentURL, resource, inline, dynamic, nonce)
	}
	p.trace.Add(trace.CSP, "scriptDecision", map[string]any{"allowed": allowed, "inline": inline, "dynamic": dynamic, "nonce": nonce != "", "resource": urlString(resource), "reason": reason})
	return allowed
}

// Resize observations share the existing environment and iframe box state.
func (p *Page) viewportObservationChange(before bool) {
	p.mu.RLock()
	realms := make([]*Realm, 0, len(p.frames))
	for _, f := range p.frames {
		if f.Realm != nil && f.Realm.viewportNotifier != nil {
			realms = append(realms, f.Realm)
		}
	}
	p.mu.RUnlock()
	for _, r := range realms {
		notify := func(ctx context.Context) error {
			_, err := r.runtime.Call(ctx, r.viewportNotifier, nil, r.val(before))
			return err
		}
		if before {
			if err := r.runOnOwner(context.Background(), notify); err != nil {
				p.trace.Add(trace.Error, "viewportObservation", map[string]any{"realm": r.ID, "error": err.Error()})
			}
		} else {
			r.scheduler.Post(scheduler.UserInteraction, 0, notify)
		}
	}
}

func (p *Page) SetViewport(width, height int) error {
	if err := p.CheckProfileMutation(); err != nil {
		return err
	}
	p.viewportObservationChange(true)
	defer p.viewportObservationChange(false)
	p.mu.Lock()
	defer p.mu.Unlock()
	if width <= 0 || height <= 0 {
		return fmt.Errorf("viewport dimensions must be positive")
	}
	if width > p.env.Window.OuterWidth || height > p.env.Window.OuterHeight {
		return fmt.Errorf("viewport cannot exceed outer window")
	}
	p.env.Window.ViewportWidth = width
	p.env.Window.ViewportHeight = height
	p.trace.Add(trace.Lifecycle, "viewportChanged", map[string]any{"width": width, "height": height})
	return nil
}
func (p *Page) Navigate(ctx context.Context, raw string) error {
	defer p.collectRealmOwners()
	if err := p.navigate(ctx, raw, uuid.NewString()); err != nil {
		return err
	}
	// The direct Go API is synchronous through the document load boundary.
	// CDP uses NavigateReserved and observes the independently emitted lifecycle
	// events, matching Page.navigate's asynchronous protocol contract.
	for !p.LoadEventEnded() {
		p.mu.RLock()
		realm := p.Top.Realm
		p.mu.RUnlock()
		if realm == nil {
			return fmt.Errorf("page has no realm while waiting for load")
		}
		if err := realm.scheduler.Wait(ctx); err != nil {
			return err
		}
		if err := realm.RunReady(ctx); err != nil {
			return err
		}
	}
	return nil
}

// CancelNavigation cancels suspended parser work without entering its realm.
// The current JavaScript task keeps its own execution cancellation boundary.
func (p *Page) CancelNavigation() bool {
	p.mu.Lock()
	cancel := p.navigationCancel
	p.navigationCancel = nil
	p.mu.Unlock()
	if cancel != nil {
		cancel()
		return true
	}
	return false
}

func (p *Page) ReserveNavigation() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.loaderID = uuid.NewString()
	return p.loaderID
}
func (p *Page) NavigateReserved(ctx context.Context, raw, loaderID string) error {
	return p.navigate(ctx, raw, loaderID)
}
func (p *Page) navigate(ctx context.Context, raw, loaderID string, replace ...bool) error {
	return p.navigateRequest(ctx, raw, loaderID, network.Request{UserActivation: true}, replace...)
}
func (p *Page) navigateRequest(ctx context.Context, raw, loaderID string, request network.Request, replace ...bool) error {
	return p.navigateRequestWithHistory(ctx, raw, loaderID, request, 0, replace...)
}
func (p *Page) navigateRequestWithHistory(ctx context.Context, raw, loaderID string, request network.Request, historyTarget int, replace ...bool) error {
	return p.beginNavigationRequest(ctx, raw, loaderID, request, historyTarget, false, replace...)
}

// StartNavigation is the asynchronous external-command entry point. The Go
// Navigate/NavigateReserved entry points retain their existing initial-response
// boundary; CDP can continue reading the old document while the response loads.
func (p *Page) StartNavigation(ctx context.Context, raw, loaderID string, committed ...func(error)) error {
	var notify func(error)
	if len(committed) != 0 {
		notify = func(err error) {
			for _, callback := range committed {
				callback(err)
			}
		}
	}
	if loaderID != p.LoaderID() {
		if notify != nil {
			notify(context.Canceled)
		}
		return nil // A newer command reserved a replacement before this turn.
	}
	return p.beginNavigationRequestWithCommit(ctx, raw, loaderID, network.Request{UserActivation: true}, 0, true, notify)
}

func (p *Page) beginNavigationRequest(ctx context.Context, raw, loaderID string, request network.Request, historyTarget int, asynchronous bool, replace ...bool) error {
	return p.beginNavigationRequestWithCommit(ctx, raw, loaderID, request, historyTarget, asynchronous, nil, replace...)
}

func (p *Page) beginNavigationRequestWithCommit(ctx context.Context, raw, loaderID string, request network.Request, historyTarget int, asynchronous bool, committed func(error), replace ...bool) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	blank := u.Scheme == "about" && u.Opaque == "blank"
	if !blank && u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsupported navigation scheme %q", u.Scheme)
	}
	p.CancelNavigation()
	performanceOrigin := p.ClockNow()
	// A controllable CDP process runs against the real wall clock by default.
	// Network operations may hold the browser state-machine lock while Go's
	// monotonic clock continues; catch the canonical clock up at the navigation
	// boundary rather than letting Date/performance drift behind the server.
	if wallNow := time.Now().UTC(); wallNow.After(performanceOrigin) {
		performanceOrigin = wallNow
	}
	p.mu.Lock()
	p.loaderID = loaderID
	p.clock = performanceOrigin
	p.performanceOrigin = performanceOrigin
	p.loadEventEnded = false
	p.mu.Unlock()
	if blank {
		// about:blank is a document navigation, but has no network request or
		// response. Commit through the ordinary document/lifecycle pipeline.
		response := network.Response{URL: u, Body: []byte("<html><head></head><body></body></html>"), Headers: make(http.Header), Synthetic: true}
		return p.commitNavigationResponse(ctx, ctx, u, loaderID, performanceOrigin, response, historyTarget, false, committed, replace...)
	}
	// CDP defines the main resource request id as the navigation loader id.
	// Puppeteer/Pyppeteer use this equality (together with type=Document) to
	// recognize the navigation request and return its Response from goto().
	request.ID, request.ContextID, request.URL, request.Initiator = loaderID, p.Top.ID, u, network.Navigation
	request.Owner, request.TopLevelURL = "frame", u
	if request.Method == "" {
		request.Method = http.MethodGet
	}
	if asynchronous && p.directEvaluationDepth == 0 {
		return p.fetchNavigationResponse(ctx, u, loaderID, performanceOrigin, request, historyTarget, committed, replace...)
	}
	res, err := p.loader.Load(ctx, request)
	if err != nil {
		return err
	}
	if p.captureDownload(res) {
		return nil
	}
	return p.commitNavigationResponse(ctx, ctx, u, loaderID, performanceOrigin, res, historyTarget, false, committed, replace...)
}

func (p *Page) commitNavigationResponse(ctx, taskContext context.Context, u *url.URL, loaderID string, performanceOrigin time.Time, res network.Response, historyTarget int, suspended bool, committed func(error), replace ...bool) error {
	// The response URL is the committed document URL after redirects. Use it
	// for the realm origin, history, policy checks and relative resource URLs.
	if res.URL != nil {
		u = res.URL
	}
	p.advanceNavigationResponseClock(performanceOrigin, res)
	doc, err := dom.Parse("")
	if err != nil {
		return err
	}
	coep := strings.ToLower(strings.TrimSpace(res.Headers.Get("Cross-Origin-Embedder-Policy")))
	coop := strings.ToLower(strings.TrimSpace(res.Headers.Get("Cross-Origin-Opener-Policy")))
	secureContext := potentiallyTrustworthyURL(u)
	p.mu.Lock()
	p.documentSecurity = documentSecurity{
		secureContext:       secureContext,
		crossOriginIsolated: secureContext && strings.HasPrefix(coop, "same-origin") && (strings.HasPrefix(coep, "require-corp") || strings.HasPrefix(coep, "credentialless")),
		credentialless:      strings.HasPrefix(coep, "credentialless"),
		originAgentCluster:  originAgentClusterForResponse(secureContext, res.Headers.Get("Origin-Agent-Cluster")),
		permissionsPolicy:   res.Headers.Get("Permissions-Policy"),
	}
	p.mu.Unlock()
	realm, err := newRealm(p, p.Top, doc, u)
	if err != nil {
		return err
	}
	// Reload is an explicit navigation reason. Neither equal URLs nor replacing
	// a history entry imply a reload (Location.replace can do both).
	if len(replace) > 1 && replace[1] {
		realm.navigationType = "reload"
	}
	realm.policy = p.responseCSP(res.Headers)
	realm.documentReferrer = res.Referrer
	realm.referrerPolicy = res.Headers.Get("Referrer-Policy")
	realm.lastModified, _ = http.ParseTime(res.Headers.Get("Last-Modified"))
	realm.initializeClientHints(res.Headers.Get("Permissions-Policy"))
	p.mu.Lock()
	old := p.Top.Realm
	if p.historyIndex >= 0 && old != nil {
		p.history[p.historyIndex].title = old.document.Title()
	}
	p.removeDescendantFramesLocked(p.Top)
	p.Top.Realm = realm
	p.current = u
	if p.historyIndex >= 0 {
		realm.navigationActivationFrom = p.history[p.historyIndex].frames[p.Top.ID]
	}
	realm.navigationActivationType = "push"
	if len(replace) > 0 && replace[0] {
		realm.navigationActivationType = "replace"
	}
	if len(replace) > 1 && replace[1] {
		realm.navigationActivationType = "reload"
	}
	if historyTarget > 0 {
		realm.navigationActivationType = "traverse"
	}
	entry := &sessionHistoryEntry{URL: u, frames: map[string]*historyFrameState{p.Top.ID: {url: u, realmID: realm.ID}}}
	if len(replace) > 1 && replace[1] && p.historyIndex >= 0 && historyTarget == 0 {
		previous := p.history[p.historyIndex].frames[p.Top.ID]
		entry.frames[p.Top.ID].navigationState = previous.navigationState
		entry.frames[p.Top.ID].storageData = previous.storageData
		oldRealmID := previous.realmID
		for _, oldEntry := range p.history {
			if oldState := oldEntry.frames[p.Top.ID]; oldState != nil && oldState.realmID == oldRealmID {
				oldState.realmID = realm.ID
				oldState.state = nil
				oldState.storageState = nil
			}
		}
	}
	entry.frames[p.Top.ID].ensureNavigationIdentity()
	if len(replace) > 0 && replace[0] && p.historyIndex >= 0 {
		previous := p.history[p.historyIndex].frames[p.Top.ID]
		previous.ensureNavigationIdentity()
		entry.frames[p.Top.ID].navigationKey = previous.navigationKey
	}
	if historyTarget > 0 {
		target := historyTarget - 1
		entry.id = p.history[target].id
		previous := p.history[target].frames[p.Top.ID]
		previous.ensureNavigationIdentity()
		entry.frames[p.Top.ID].navigationKey = previous.navigationKey
		entry.frames[p.Top.ID].navigationID = previous.navigationID
		entry.frames[p.Top.ID].navigationState = previous.navigationState
		entry.frames[p.Top.ID].storageData = previous.storageData
		oldRealmID := previous.realmID
		for _, oldEntry := range p.history {
			if state := oldEntry.frames[p.Top.ID]; state != nil && state.realmID == oldRealmID {
				state.realmID = realm.ID
				state.state = nil
				state.storageState = nil
			}
		}
		p.history[target] = entry
		p.historyIndex = target
	} else if len(replace) > 0 && replace[0] && p.historyIndex >= 0 {
		entry.id = p.history[p.historyIndex].id
		p.history[p.historyIndex] = entry
	} else {
		p.history = p.history[:p.historyIndex+1]
		p.history = append(p.history, entry)
		p.historyIndex++
	}
	p.mu.Unlock()
	p.retireRealm(old)
	p.trace.Add(trace.Lifecycle, "frameNavigated", map[string]any{"url": u.String(), "realm": realm.ID, "frameId": p.Top.ID, "loaderId": loaderID})
	streamState, err := realm.initializeNavigationStream()
	if err != nil {
		return err
	}
	// Parser continuations outlive the command that starts navigation, while
	// retaining its navigation deadline and an explicit stop-loading handle.
	if deadline, ok := ctx.Deadline(); ok {
		deadlineContext, deadlineCancel := context.WithDeadline(streamState.ctx, deadline)
		streamCancel := streamState.cancel
		streamState.ctx = deadlineContext
		context.AfterFunc(deadlineContext, func() {
			if deadlineContext.Err() == context.DeadlineExceeded && p.LoaderID() == loaderID && !p.LoadEventEnded() {
				p.trace.Add(trace.Error, "navigation", map[string]any{"url": u.String(), "realm": realm.ID, "error": context.DeadlineExceeded.Error()})
			}
		})
		streamState.cancel = func() { deadlineCancel(); streamCancel() }
	}
	p.mu.Lock()
	// CancelNavigation clears the canonical handle before invoking it outside
	// p.mu. A suspended main response must not overwrite that cleared handle
	// with a fresh parser lifetime and thereby lose a concurrent stop/close.
	cancelled := ctx.Err() != nil || suspended && p.navigationCancel == nil
	if !cancelled {
		p.navigationCancel = streamState.cancel
	}
	p.mu.Unlock()
	if cancelled {
		streamState.cancel()
	}
	if committed != nil {
		// Publish the parser cancellation handle before acknowledging commit.
		// The callback only notifies its waiter; parser and JS have not started.
		committed(nil)
	}
	if streamState.ctx.Err() != nil {
		return nil
	}
	ctx = streamState.ctx
	restoreTaskContext := streamState.useTaskContext(taskContext)
	defer restoreTaskContext()
	p.runInitScripts(streamState.executionContext(), realm)
	streamState.onScript = func(s dom.Node) error {
		realm.preloadModules()
		realm.preloadResources()
		stylesheets := realm.startParserStylesheets()
		if strings.EqualFold(strings.TrimSpace(s.Attributes["type"]), "importmap") {
			doc.MarkScriptStarted(s.ID)
			if err := realm.installImportMap(doc.TextContent(s.ID)); err != nil {
				p.trace.Add(trace.Exception, "script", map[string]any{"url": u.String(), "error": err.Error(), "importMap": true})
			}
			return nil
		}
		kind := scriptExecutionKind(s.Attributes["type"], s.Attributes["language"])
		if kind == "" {
			return nil
		}
		if doc.ScriptStarted(s.ID) {
			return nil
		}
		code := doc.TextContent(s.ID)
		name := u.String()
		if src := s.Attributes["src"]; src != "" {
			su, err := u.Parse(src)
			if err != nil {
				p.trace.Add(trace.Error, "scriptURL", map[string]any{"src": src, "error": err.Error()})
				return nil
			}
			if !realm.allowsScript(su, false, false, s.Nonce) {
				return nil
			}
			request := realm.elementRequest(su, s.Attributes, network.Script)
			if _, crossOrigin := s.Attributes["crossorigin"]; crossOrigin || kind == "module" {
				request.Mode = "cors"
				if !strings.EqualFold(s.Attributes["crossorigin"], "use-credentials") {
					request.Credentials = "same-origin"
				}
				request.Headers = make(http.Header)
				request.Headers.Set("Origin", originOf(u.String()))
			}
			if kind == "module" {
				doc.MarkScriptStarted(s.ID)
				streamState.deferred = append(streamState.deferred, deferredParserScript{module: true, name: su.String(), pending: realm.fetchModule(request), nodeID: s.ID})
				return nil
			}
			loaded := streamState.scripts[s.ID]
			if loaded == nil {
				loaded = &streamScriptResponse{}
				streamState.scripts[s.ID] = loaded
				realm.fetchNavigationScript(streamState, loaded, request)
				return dom.ErrStreamPaused
			}
			if !loaded.ready {
				return dom.ErrStreamPaused
			}
			rr := loaded.response
			err = loaded.err
			if err == nil {
				err = scriptResponseError(rr)
			}
			if err != nil {
				p.trace.Add(trace.Error, "scriptLoad", map[string]any{"url": su.String(), "error": err.Error()})
				scriptID := s.ID
				dispatchError := func(eventContext context.Context) error {
					return realm.dispatchResourceEvent(eventContext, scriptID, "error")
				}
				if !streamState.insideScript {
					if eventErr := realm.runNavigationTask(ctx, streamState, scheduler.DOM, dispatchError); eventErr != nil {
						p.trace.Add(trace.Error, "scriptErrorEvent", map[string]any{"url": su.String(), "error": eventErr.Error()})
					}
				} else {
					_ = dispatchError(streamState.executionContext())
				}
				return nil
			}
			code = string(rr.Body)
			name = su.String()
		} else if !realm.allowsScript(nil, true, false, s.Nonce) {
			return nil
		}
		if kind == "module" {
			doc.MarkScriptStarted(s.ID)
			if code != "" {
				streamState.deferred = append(streamState.deferred, deferredParserScript{module: true, code: code, name: name, nodeID: s.ID})
			}
			return nil
		}
		if realm.deferNavigationStylesheets(streamState, stylesheets, func() error { return realm.resumeNavigationStream(streamState.executionContext(), streamState) }) {
			return dom.ErrStreamPaused
		}
		doc.MarkScriptStarted(s.ID)
		if code != "" {
			// Parser scripts are browser-observable tasks too. Running them directly
			// from navigation freezes the canonical monotonic clock and lets engine
			// microtasks escape the scheduler. Queue each parser-blocking script and
			// drain the ready turn before the parser proceeds to the next script.
			scriptCode, scriptName, scriptID := code, name, s.ID
			runScript := func(taskContext context.Context) error {
				p.trace.Add(trace.JS, "scriptStart", map[string]any{"url": scriptName, "realm": realm.ID})
				previous := streamState.insideScript
				streamState.insideScript = true
				defer func() { streamState.insideScript = previous }()
				evalErr := realm.evaluateClassicScript(taskContext, scriptCode, scriptName, scriptID)
				if evalErr != nil {
					p.trace.Add(trace.JS, "scriptEnd", map[string]any{"url": scriptName, "realm": realm.ID, "error": evalErr.Error()})
					return evalErr
				}
				p.trace.Add(trace.JS, "scriptEnd", map[string]any{"url": scriptName, "realm": realm.ID})
				return nil
			}
			if streamState.insideScript {
				// document.write executes inserted classic scripts synchronously
				// within this parser task; the outer task owns its checkpoint.
				_ = runScript(streamState.executionContext())
			} else {
				if err := realm.runNavigationTask(ctx, streamState, scheduler.DOM, runScript); err != nil {
					p.trace.Add(trace.Error, "parserScriptTask", map[string]any{"url": scriptName, "error": err.Error()})
				}
			}
		}
		return nil
	}
	streamState.onFinished = func() error {
		if streamState.ctx.Err() != nil || realm.documentStream != streamState || p.Top.Realm != realm {
			return nil
		}
		// The HTML parser creates browsing contexts for iframe elements without
		// waiting for script to read contentWindow.  Attach those contexts now, but
		// queue their network navigations from the DOMContentLoaded turn below. This
		// preserves parser iframe order relative to frames inserted by that event's
		// handlers while keeping all child execution on browser scheduler tasks.
		type parserFrame struct {
			elementID int64
			frame     *Frame
		}
		parserFrames := make([]parserFrame, 0)
		for _, iframe := range doc.FindAllByTagName("iframe") {
			frame, frameErr := realm.ensureChildFrameInternal(iframe.ID, false, false)
			if frameErr != nil {
				p.trace.Add(trace.Error, "parserFrameAttach", map[string]any{"elementNodeId": iframe.ID, "error": frameErr.Error(), "realm": realm.ID})
				continue
			}
			if frame != nil {
				parserFrames = append(parserFrames, parserFrame{elementID: iframe.ID, frame: frame})
			}
		}
		// DOMContentLoaded is a browser task, not merely a CDP notification.  Page
		// scripts observe it on Document and its Promise jobs checkpoint before the
		// next lifecycle/resource task is selected.
		if err := realm.runNavigationTask(ctx, streamState, scheduler.DOM, func(taskContext context.Context) error {
			for _, parserFrame := range parserFrames {
				realm.scheduleChildFrameNavigation(parserFrame.frame, parserFrame.elementID)
			}
			realm.performanceLifecycle("domContentLoadedEventStart")
			_, eventErr := realm.Evaluate(taskContext, `document.dispatchEvent(new Event('DOMContentLoaded'))`, "mimic:dom-content-loaded")
			realm.performanceLifecycle("domContentLoadedEventEnd")
			if eventErr == nil {
				p.trace.Add(trace.Lifecycle, "DOMContentLoaded", map[string]any{"url": u.String(), "frameId": p.Top.ID, "realm": realm.ID})
			}
			return eventErr
		}); err != nil {
			p.trace.Add(trace.Error, "domContentLoaded", map[string]any{"url": u.String(), "error": err.Error(), "realm": realm.ID})
		}
		// Chrome's browser-owned favicon discovery begins once the parser has a
		// complete document. It is not load-blocking, but starting it from the load
		// event itself is observably too late: Resource Timing can contain its entry
		// before page challenge/application work performed around load completes.
		iconURLs := documentIconURLs(doc, u)
		iconInitiatorType := "link"
		if len(iconURLs) == 0 {
			iconURLs = []*url.URL{u.ResolveReference(&url.URL{Path: "/favicon.ico"})}
			iconInitiatorType = "other"
		}
		for _, favicon := range iconURLs {
			favicon := favicon
			realm.scheduler.Post(scheduler.ResourceLow, 0, func(taskContext context.Context) error {
				request := network.Request{ContextID: p.Top.ID, URL: favicon, Referrer: u, SourceURL: u, Initiator: network.Other, PerformanceInitiatorType: iconInitiatorType}
				request.Kind = "favicon"
				realm.applyClientHints(&request)
				realm.resourceWG.Add(1)
				go func() {
					defer realm.resourceWG.Done()
					_, loadErr := p.loader.Load(realm.resourceContext, realm.withResourceTiming(request))
					if realm.resourceContext.Err() != nil {
						return
					}
					if loadErr != nil {
						p.trace.Add(trace.Error, "browserResourceLoad", map[string]any{"url": favicon.String(), "error": loadErr.Error()})
					}
					realm.scheduler.Post(scheduler.Network, 0, func(callbackContext context.Context) error {
						realm.notifyPerformanceObservers(callbackContext)
						return nil
					})
				}()
				return nil
			})
			// The Page event-loop pump begins this non-blocking transport after the
			// navigation turn releases ownership of the Page.
		}
		// Load is scheduled only after parser-time and transitively inserted
		// load-blocking resources complete. The realm owns that accounting so DOM,
		// resource loading, readyState and NavigationTiming share one lifecycle.
		realm.requestLoad(func(taskContext context.Context) {
			p.trace.Add(trace.Lifecycle, "load", map[string]any{"url": u.String(), "frameId": p.Top.ID, "realm": realm.ID, "loaderId": loaderID})
			p.mu.Lock()
			p.loadEventEnded = true
			p.mu.Unlock()
			// NavigationTiming is a live entry while the document loads. Chrome
			// delivers it to observers again when loadEventEnd finalizes duration;
			// resource-only notifications cannot represent that transition.
			realm.notifyPerformanceObservers(taskContext)
		})
		return nil
	}
	if err := realm.writeDocumentStream(realm, string(res.Body)); err != nil {
		return err
	}
	return realm.closeDocumentStream()
}
func documentIconURLs(document *dom.Document, base *url.URL) []*url.URL {
	var result []*url.URL
	for _, link := range document.FindAllByTagName("link") {
		if hasLinkRelation(link.Attributes["rel"], "icon") || hasLinkRelation(link.Attributes["rel"], "shortcut") {
			if href := strings.TrimSpace(link.Attributes["href"]); href != "" {
				if resolved, err := base.Parse(href); err == nil {
					result = append(result, resolved)
				}
			}
		}
	}
	return result
}
func hasLinkRelation(value, wanted string) bool {
	for _, relation := range strings.Fields(strings.ToLower(value)) {
		if relation == wanted {
			return true
		}
	}
	return false
}

func scriptExecutionKind(typeValue, language string) string {
	typeValue = strings.ToLower(strings.TrimSpace(typeValue))
	if typeValue == "module" {
		return "module"
	}
	if typeValue == "" && strings.TrimSpace(language) != "" {
		typeValue = "text/" + strings.ToLower(strings.TrimSpace(language))
	}
	switch typeValue {
	case "", "text/javascript", "application/javascript", "application/ecmascript", "text/ecmascript",
		"application/x-javascript", "text/javascript1.0", "text/javascript1.1", "text/javascript1.2",
		"text/javascript1.3", "text/javascript1.4", "text/javascript1.5", "text/jscript", "text/livescript":
		return "classic"
	default:
		return ""
	}
}
func urlString(u *url.URL) string {
	if u == nil {
		return ""
	}
	return u.String()
}
func (p *Page) Evaluate(ctx context.Context, source string) (any, error) {
	p.mu.RLock()
	r := p.Top.Realm
	p.mu.RUnlock()
	return p.evaluateRealm(ctx, r, source, true)
}

func (p *Page) Frame(frameID string) (*Frame, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	frame, ok := p.frames[frameID]
	return frame, ok
}

func (p *Page) EvaluateFrame(ctx context.Context, frameID, source string) (any, error) {
	frame, ok := p.Frame(frameID)
	if !ok || frame.Realm == nil {
		return nil, fmt.Errorf("unknown frame %q", frameID)
	}
	return p.evaluateRealm(ctx, frame.Realm, source, true)
}

// EvaluateCommand performs a CDP evaluation without draining unrelated timers
// after a synchronous result. The embedding Evaluate API retains its historical
// ready-task drain; CDP has its own Page pump driving those later turns.
func (p *Page) EvaluateCommand(ctx context.Context, frameID, source string) (any, error) {
	p.mu.RLock()
	r := p.Top.Realm
	if frameID != "" {
		frame := p.frames[frameID]
		if frame == nil || frame.Realm == nil {
			p.mu.RUnlock()
			return nil, fmt.Errorf("unknown frame %q", frameID)
		}
		r = frame.Realm
	}
	p.mu.RUnlock()
	return p.evaluateRealm(ctx, r, source, false)
}

func (p *Page) evaluateRealm(ctx context.Context, r *Realm, source string, drainReady bool) (any, error) {
	// The embedding API historically drains location/form navigation through
	// its initial response. CDP owns an independent pump and must not do this.
	if drainReady {
		p.directEvaluationDepth++
		defer func() { p.directEvaluationDepth-- }()
	}
	p.realmEvaluationDepth++
	defer func() { p.realmEvaluationDepth--; p.collectRealmOwners() }()
	if r == nil {
		return nil, fmt.Errorf("page has no realm; navigate first")
	}
	// Runtime.evaluate is itself a browser-observable task boundary. Promise
	// reactions queued by its synchronous body run before any timer task, with
	// the same live canonical clock as a task dequeued by the event loop.
	var v engine.Value
	release := func(value engine.Value) {
		if owner, ok := r.runtime.(engine.ValueReleaser); ok {
			owner.ReleaseValue(value)
		}
	}
	defer func() { release(v) }()
	await := func() (result any, done bool, err error) {
		operation := func(context.Context) error {
			resolved, settled, awaitErr := r.runtime.Await(v)
			done = settled
			if awaitErr != nil || !done {
				return awaitErr
			}
			result = resolved.Export()
			if _, retained := result.(engine.Value); retained {
				// Export may return the native value itself for non-JSON objects.
				// That handle becomes the embedding caller's responsibility.
				if r.runtime.StrictEqual(v, resolved) {
					v = nil
				}
			} else {
				release(resolved)
			}
			release(v)
			v = nil
			return nil
		}
		// Await, export and root cleanup are one synchronous owner operation.
		// Releasing each value in a separate actor turn measurably slows small
		// evaluations; this does not pump tasks or alter their checkpoints.
		if owner, ok := r.runtime.(engine.OwnerRuntime); ok {
			err = owner.RunOnOwner(ctx, operation)
		} else {
			err = operation(ctx)
		}
		return
	}
	if err := r.scheduler.RunInline(ctx, func(ctx context.Context) error {
		var err error
		v, err = r.Evaluate(ctx, source, "")
		return err
	}); err != nil {
		return nil, err
	}
	if drainReady {
		if err := r.RunReady(ctx); err != nil {
			p.trace.Add(trace.Error, "scheduler", map[string]any{"error": err.Error(), "during": "Evaluate"})
		}
	}
	if result, done, err := await(); err != nil {
		return nil, err
	} else if done {
		return result, nil
	}
	// Some engines expose Promise state by registering a reaction rather than
	// reading an internal slot. Give that reaction the same microtask checkpoint
	// that Chrome performs at the end of Runtime.evaluate before deciding that
	// external work is required.
	if err := r.scheduler.RunInline(ctx, nil); err != nil {
		return nil, err
	}
	if result, done, err := await(); err != nil {
		return nil, err
	} else if done {
		return result, nil
	}
	for {
		realms := p.evaluationRealms(r)
		queues := make([]*scheduler.Scheduler, 0, len(realms))
		for _, realm := range realms {
			queues = append(queues, realm.scheduler)
		}
		if err := scheduler.WaitAny(ctx, queues); err != nil {
			return nil, err
		}
		if err := p.runEvaluationTasks(ctx, r); err != nil {
			p.trace.Add(trace.Error, "scheduler", map[string]any{"error": err.Error(), "during": "Runtime.awaitPromise wake"})
		}
		if result, done, err := await(); err != nil {
			return nil, err
		} else if done {
			return result, nil
		}
	}
}

// Evaluation awaits work across the Page, not just the realm owning its
// Promise. Keep the existing realm queues and their microtask checkpoints,
// but include siblings and newly created frames at each task boundary.
func (p *Page) evaluationRealms(evaluating *Realm) []*Realm {
	p.mu.RLock()
	defer p.mu.RUnlock()
	realms := make([]*Realm, 0, len(p.frames)+1)
	found := false
	for _, frame := range p.frames {
		if frame.Realm != nil {
			realms = append(realms, frame.Realm)
			found = found || frame.Realm == evaluating
			for _, world := range frame.Realm.isolatedWorlds {
				realms = append(realms, world)
				found = found || world == evaluating
			}
		}
	}
	if !found {
		realms = append(realms, evaluating)
	}
	return realms
}

func (p *Page) runEvaluationTasks(ctx context.Context, evaluating *Realm) error {
	// Check promise settlement between complete tasks. Draining every ready
	// timer first can starve even an already-settled evaluation indefinitely.
	var queues []*scheduler.Scheduler
	for _, realm := range p.evaluationRealms(evaluating) {
		queues = append(queues, realm.scheduler)
	}
	_, err := scheduler.RunReadyAcross(ctx, queues)
	return err
}
func (p *Page) Document() (*dom.Document, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.Top.Realm == nil {
		return nil, false
	}
	return p.Top.Realm.document, true
}
func (p *Page) Environment() state.Environment {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.env.Clone()
}
func (p *Page) Pause() {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.Top.Realm != nil {
		p.Top.Realm.scheduler.Pause()
	}
}
func (p *Page) AdvanceTime(ctx context.Context, delta time.Duration) error {
	exhausted, err := p.advanceTimeTasks(ctx, delta, 10000)
	if err != nil {
		return err
	}
	if exhausted {
		return fmt.Errorf("page scheduler task limit exceeded")
	}
	return nil
}

// AdvanceTimeBudget advances the Page clock and runs at most maxTasks event
// loop turns. CDP uses a bounded batch so debugger commands can be serviced
// between independent browser tasks even when a site continuously replenishes
// its ready queue.
func (p *Page) AdvanceTimeBudget(ctx context.Context, delta time.Duration, maxTasks int) (bool, error) {
	if maxTasks < 1 {
		maxTasks = 1
	}
	return p.advanceTimeTasks(ctx, delta, maxTasks)
}

func (p *Page) advanceTimeTasks(ctx context.Context, delta time.Duration, maxTasks int) (bool, error) {
	p.mu.Lock()
	p.clock = p.clock.Add(delta)
	realms := make([]*Realm, 0, len(p.frames))
	for _, frame := range p.frames {
		if frame.Realm != nil {
			realms = append(realms, frame.Realm)
			for _, world := range frame.Realm.isolatedWorlds {
				realms = append(realms, world)
			}
		}
	}
	p.mu.Unlock()
	for _, realm := range realms {
		// Advance all clocks before executing anything. Draining each realm
		// here would bypass the shared Page task-selection boundary.
		realm.scheduler.AdvanceBy(delta)
	}
	defer p.closeRetiredRealms()
	for turn := 0; turn < maxTasks; turn++ {
		// A callback can commit navigation. Rebuild from the active tree at
		// every task boundary so retired queues are never driven afterward.
		p.mu.RLock()
		queues := make([]*scheduler.Scheduler, 0, len(p.frames))
		for _, frame := range p.frames {
			if frame.Realm != nil {
				queues = append(queues, frame.Realm.scheduler)
				for _, world := range frame.Realm.isolatedWorlds {
					queues = append(queues, world.scheduler)
				}
			}
		}
		p.mu.RUnlock()
		progress, err := scheduler.RunReadyAcross(ctx, queues)
		if err != nil || !progress {
			return false, err
		}
	}
	return true, nil
}

func (p *Page) closeRetiredRealms() { p.collectRealmOwners() }

func (p *Page) Resume() {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.Top.Realm != nil {
		p.Top.Realm.scheduler.Resume()
	}
}

func (p *Page) ExecutionStatus() scheduler.ExecutionStatus {
	p.mu.RLock()
	queues := make([]*scheduler.Scheduler, 0, len(p.frames))
	for _, frame := range p.frames {
		if frame.Realm != nil {
			queues = append(queues, frame.Realm.scheduler)
			for _, world := range frame.Realm.isolatedWorlds {
				queues = append(queues, world.scheduler)
			}
		}
	}
	p.mu.RUnlock()
	var longest scheduler.ExecutionStatus
	for _, queue := range queues {
		status := queue.ExecutionStatus()
		if status.Running && (!longest.Running || status.Elapsed > longest.Elapsed) {
			longest = status
		}
	}
	return longest
}

func (p *Page) LiveDiagnostics() any {
	p.mu.RLock()
	realm := p.Top.Realm
	p.mu.RUnlock()
	if realm != nil {
		if diagnostic, ok := realm.runtime.(interface{ LiveDiagnostics() any }); ok {
			primary := diagnostic.LiveDiagnostics()
			if result, ok := primary.(map[string]any); ok {
				realms := make([]any, 0)
				for _, candidate := range p.evaluationRealms(nil) {
					if candidate == nil || candidate == realm {
						continue
					}
					if other, ok := candidate.runtime.(interface{ LiveDiagnostics() any }); ok {
						realms = append(realms, map[string]any{"realm": candidate.ID, "diagnostics": other.LiveDiagnostics()})
					}
				}
				result["realms"] = realms
			}
			return primary
		}
	}
	return map[string]any{"enabled": false}
}
func (p *Page) locationPart(k string) string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.current == nil {
		return "about:blank"
	}
	u := p.current
	switch k {
	case "href":
		return u.String()
	case "origin":
		return originOf(u.String())
	case "protocol":
		return u.Scheme + ":"
	case "host":
		return u.Host
	case "hostname":
		return u.Hostname()
	case "port":
		return u.Port()
	case "pathname":
		return u.Path
	case "search":
		if u.RawQuery != "" {
			return "?" + u.RawQuery
		}
	case "hash":
		return u.Fragment
	}
	return ""
}
func (p *Page) resolve(raw string) (*url.URL, error) {
	p.mu.RLock()
	base := p.current
	p.mu.RUnlock()
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if base != nil {
		return base.ResolveReference(u), nil
	}
	return u, nil
}
func (p *Page) historyLength() int { p.mu.RLock(); defer p.mu.RUnlock(); return len(p.history) }
func headerMap(v any) http.Header {
	h := make(http.Header)
	if m, ok := v.(map[string]any); ok {
		for k, x := range m {
			h.Set(k, fmt.Sprint(x))
		}
	}
	return h
}
func originOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "null"
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "null"
	}
	return strings.ToLower(u.Scheme + "://" + u.Host)
}
