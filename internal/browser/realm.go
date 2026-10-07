package browser

import (
	"context"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	cryptorand "crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/moreveal/mimic/internal/csp"
	"github.com/moreveal/mimic/internal/dom"
	"github.com/moreveal/mimic/internal/engine"
	"github.com/moreveal/mimic/internal/network"
	"github.com/moreveal/mimic/internal/scheduler"
	"github.com/moreveal/mimic/internal/state"
	"github.com/moreveal/mimic/internal/textmetrics"
	"github.com/moreveal/mimic/internal/trace"
	"github.com/moreveal/mimic/internal/webapi"
)

type Realm struct {
	agentClusters            *agentClusterState
	blitz                    *blitzDocument
	blitzInputs              engine.Value
	blitzCalls               map[string]blitzCallStat
	files                    *opfsOwner
	policyMetaCursor         int64
	policyMetaCandidates     []int64
	policy                   csp.PolicySet
	policyMeta               map[int64]string
	trustedTypesEnforcer     engine.Value
	debuggerFactory          engine.Value
	debuggerBindings         map[string]bool
	mainWorld                *Realm
	runtimeGroup             *realmRuntimeGroup
	isolatedWorlds           map[string]*Realm
	worldName                string
	worldMutationReceiver    engine.Value
	worldHasObservers        bool
	speech                   *speechSynthesisState
	speechNotifier           engine.Value
	cameraTracks             map[string]*cameraTrack
	cameraNotifier           engine.Value
	rtcMediaPeers            map[string]*rtcMediaPeer
	rtcMediaNotifier         engine.Value
	webTaskAbort             engine.Value
	auxiliaryState           engine.Value
	auxiliaryStorage         engine.Value
	pictureInPicture         *Frame
	pipLifecycle             engine.Value
	activationConsumed       bool
	cookieNotifier           engine.Value
	launchNotifier           engine.Value
	cookieUnsubscribe        func()
	cacheHandles             map[uint64]*cacheBucket
	crashReport              crashReportState
	windowStatus             string
	historyCloneFunction     engine.Value
	inactive                 bool
	closed                   bool
	realmReferences          map[string]struct{}
	windowReferences         map[string]*Frame
	bootstrapPlan            *bootstrapSource
	bootstrapCapture         *bootstrapSnapshotEntry
	bootstrapRestored        bool
	fontChoices              []textmetrics.FontReference
	fontCollectionRevision   uint64
	textShapeCache           *textShapeCache
	textCacheProfile         *textCacheProfile
	lastModified             time.Time
	clientHints              *clientHintsDocument
	documentSecurity         *documentSecurity
	profileWrappers          uint64
	profilePhases            map[string]float64
	ID                       string
	activationAt             time.Time
	documentReferrer         string
	referrerPolicy           string
	inputDispatcher          engine.Value
	protocolScroll           engine.Value
	permissionNotifier       engine.Value
	geolocationNotifier      engine.Value
	networkStateEvent        engine.Value
	agent                    ExecutionAgent
	runtime                  engine.Runtime
	scheduler                *scheduler.Scheduler
	timers                   map[uint64]*windowTimer
	document                 *dom.Document
	documentStream           *documentStream
	documentStreamReset      engine.Value
	frameReflection          *frameReflection
	viewportNotifier         engine.Value
	eventListenerInvoker     engine.Value
	frameViewportRead        engine.Value
	frameLayoutRead          engine.Value
	computedStyleFlatRead    engine.Value
	protocolBoxModelRead     engine.Value
	styleProjections         styleProjectionCache
	previewRead              engine.Value
	frameReferenceImport     engine.Value
	frameTransaction         engine.Value
	frameReferenceDescribe   engine.Value
	frameGlobalRead          engine.Value
	frameValueEncoder        engine.Value
	frameValueRetain         engine.Value
	frameValueEncoderJSON    bool
	frameNodeDescribe        engine.Value
	frameEventDescribe       engine.Value
	windowErrorReporter      engine.Value
	frameBindingDescribe     engine.Value
	frameNativeNameDescribe  engine.Value
	frameSourceDescribe      engine.Value
	documentStreamEvent      engine.Value
	url                      *url.URL
	token                    string
	detached                 map[int64]dom.Node
	apiSeen                  map[string]bool
	apiTraceSet              engine.Value
	apiTraceActive           bool
	readyState               string
	apiTracking              bool
	workers                  map[int64]*DedicatedWorker
	workerSeq                int64
	childFrames              map[int64]*Frame
	retainedFrames           map[string]*Frame
	crossValues              map[int64]engine.Value
	crossValueSeq            int64
	origin                   string
	ignoreDestructiveWrites  int
	currentScript            int64
	messageReceiver          engine.Value
	messagePortReceiver      engine.Value
	frameLoadDispatcher      engine.Value
	frameIndexNotifier       engine.Value
	resourceEventDispatcher  engine.Value
	performanceNotifier      engine.Value
	domQueryCallback         engine.Value
	shadowSnapshotCallback   engine.Value
	shadowSnapshotRestore    engine.Value
	shadowSnapshotVersion    engine.Value
	shadowSnapshotRevision   string
	formSnapshotCallback     engine.Value
	selectorTargetID         int64
	loadBlockers             int
	orderedScripts           []*orderedScript
	loadRequested            bool
	loadScheduled            bool
	loadCompleted            bool
	loadEpoch                uint64
	loadCallback             func(context.Context)
	resourceContext          context.Context
	cancelResources          context.CancelFunc
	resourceWG               sync.WaitGroup
	moduleFetches            map[string]*scriptFetch
	moduleGraphs             map[string]*moduleGraph
	preparedModules          map[string]bool
	importMap                map[string]string
	preloadedModuleLinks     map[int64]bool
	imageLoads               map[int64]*imageLoad
	mediaLoads               map[int64]*mediaLoad
	mediaSources             map[string]*mediaSource
	mediaSourceNotifier      engine.Value
	availableImages          *availableImageCache
	preloads                 map[preloadKey]*resourcePreload
	preloadsMu               sync.Mutex
	preloadContext           context.Context
	cancelPreloads           context.CancelFunc
	preloadedLinks           map[int64]bool
	stylesheetLoads          map[preloadKey]*resourcePreload
	parserStylesheetEvents   map[int64]preloadKey
	stylesheetFetchSlots     chan struct{}
	resourceRevision         atomic.Uint64
	styleResourceRevision    atomic.Uint64
	baseCacheDocument        *dom.Document
	baseCacheRevision        uint64
	baseCacheReference       *url.URL
	fetchCancels             map[string]context.CancelFunc
	xhrCancels               map[string]context.CancelFunc
	webSockets               map[string]*realmWebSocket
	nativePollQueued         bool
	checkpointQueued         bool
	checkpointClosed         bool
	navigationCallback       engine.Value
	navigationEncoder        engine.Value
	navigationDecoder        engine.Value
	navigationActivationFrom *historyFrameState
	navigationActivationType string
	historyTraversalTarget   int
	indexedNotifier          engine.Value
	indexedEncoder           engine.Value
	// Navigation timing belongs to the committed document, not the mutable
	// same-document History URL or another frame's most recent navigation.
	navigationURL                 string
	navigationLoaderID            string
	navigationType                string
	performanceOrigin             time.Time
	navigationLoadEnd             time.Time
	performance                   *performanceTimeline
	performanceCursor             uint64
	performanceNavigationResponse map[string]any
	performanceConfidence         map[string]any
	performanceLifecycleTimes     map[string]time.Time
	memoryProjection              memoryProjection
	documentEntry                 *Realm
	ancestorOrigins               []string
}

// documentURL is the URL observed by this realm. For the top-level realm it
// follows the Page history state so pushState/replaceState immediately affect
// referrers and relative URL resolution without mutating unrelated subsystems.
func (r *Realm) documentURL() *url.URL {
	if frame, ok := r.agent.(*Frame); ok && frame == r.agent.Page().Top {
		u, err := url.Parse(r.agent.Page().URL())
		if err == nil {
			return u
		}
	}
	copy := *r.url
	return &copy
}

func (r *Realm) resolveDocument(raw string) (*url.URL, error) {
	reference, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	return r.documentBaseURL().ResolveReference(reference), nil
}

func (r *Realm) securityState() documentSecurity {
	p := r.agent.Page()
	p.mu.RLock()
	security := p.documentSecurity
	p.mu.RUnlock()
	if frame, ok := r.agent.(*Frame); ok && frame.parent != nil {
		if frame.parent.Realm != nil {
			security = frame.parent.Realm.securityState()
		}
		if r.url.Scheme != "about" {
			trustworthy := potentiallyTrustworthyURL(r.url)
			security.secureContext = security.secureContext && trustworthy
		}
		security.crossOriginIsolated = security.crossOriginIsolated && security.secureContext && r.isolationDelegated(frame)
	}
	if r.documentSecurity != nil {
		security.permissionsPolicy = r.documentSecurity.permissionsPolicy
	}
	if r.agentClusters != nil {
		if choice, exists := r.agentClusters.choice(r.origin); exists {
			security.originAgentCluster = choice
		}
	}
	if allow, declared := hintPolicy(security.permissionsPolicy, r.origin)["cross-origin-isolated"]; declared && !hintAllows(allow, r.origin) {
		security.crossOriginIsolated = false
	}
	return security
}

// The default permission is same-origin. An iframe's allow declaration can
// delegate it to another origin, bounded by its parent's response policy.
func (r *Realm) isolationDelegated(frame *Frame) bool {
	parent := frame.parent.Realm
	if parent == nil {
		return false
	}
	policy := parent.securityState().permissionsPolicy
	if allow, declared := hintPolicy(policy, parent.origin)["cross-origin-isolated"]; declared && !hintAllows(allow, r.origin) {
		return false
	}
	if allow, declared := parent.frameClientHintsContainer(frame, r.origin)["cross-origin-isolated"]; declared {
		return hintAllows(allow, r.origin)
	}
	return r.origin == parent.origin
}

func newRealm(p *Page, agent ExecutionAgent, d *dom.Document, u *url.URL) (*Realm, error) {
	// Commit canonical document and parser state before materializing language
	// bindings. External script transport must not wait for cold JS admission.
	return newRealmState(p, agent, d, u, true)
}

func newRealmState(p *Page, agent ExecutionAgent, d *dom.Document, u *url.URL, deferred bool) (*Realm, error) {
	origin, loaderID := p.PerformanceOrigin(), p.LoaderID()
	if frame, ok := agent.(*Frame); ok && frame.parent != nil {
		origin, loaderID = p.ClockNow(), frame.loaderID
	}
	return newRealmStateWithNavigation(p, agent, d, u, deferred, origin, loaderID)
}

type realmNavigationPolicies struct {
	permissionsPolicy  string
	originAgentCluster string
}

func newRealmStateWithNavigation(p *Page, agent ExecutionAgent, d *dom.Document, u *url.URL, deferred bool, performanceOrigin time.Time, loaderID string, policies ...realmNavigationPolicies) (*Realm, error) {
	// Every child document, including a navigation replacement, participates
	// in its tree's authoritative node arena before any IDs are published.
	if frame, ok := agent.(*Frame); ok && frame.parent != nil && frame.parent.Realm != nil {
		d.ShareNodeArena(frame.parent.Realm.document)
	}
	resourceContext, cancelResources := context.WithCancel(p.ctx.lifetime)
	r := &Realm{ID: uuid.NewString(), agent: agent, document: d, url: u, origin: originOf(u.String()), token: uuid.NewString(), detached: map[int64]dom.Node{}, apiSeen: map[string]bool{}, readyState: "loading", workers: map[int64]*DedicatedWorker{}, childFrames: map[int64]*Frame{}, retainedFrames: map[string]*Frame{}, crossValues: map[int64]engine.Value{}, resourceContext: resourceContext, cancelResources: cancelResources}
	if frame, ok := agent.(*Frame); ok {
		switch {
		case frame.Realm != nil && frame.Realm.document == d:
			// An isolated world observes this exact document.
			r.runtimeGroup = frame.Realm.runtimeGroup
		case frame.parent != nil && frame.parent.Realm != nil:
			r.runtimeGroup = frame.parent.Realm.runtimeGroup
		}
	}
	if r.runtimeGroup == nil {
		r.runtimeGroup = &realmRuntimeGroup{}
	}
	r.navigationURL, r.navigationLoaderID, r.performanceOrigin = u.String(), loaderID, performanceOrigin
	r.navigationType = "navigate"
	if frame, ok := agent.(*Frame); ok {
		for ancestor := frame.parent; ancestor != nil; ancestor = ancestor.parent {
			if ancestor.Realm != nil {
				r.ancestorOrigins = append(r.ancestorOrigins, ancestor.Realm.origin)
			}
		}
	}
	policyHeader := ""
	if frame, ok := agent.(*Frame); ok && frame.parent == nil {
		policyHeader = r.securityState().permissionsPolicy
	}
	if len(policies) != 0 {
		policyHeader = policies[0].permissionsPolicy
	}
	// about:blank inherits its creator's origin before installing realm state.
	if frame, ok := agent.(*Frame); ok && frame.parent != nil && frame.parent.Realm != nil && u.Scheme == "about" && (u.Opaque == "blank" || u.Opaque == "srcdoc") {
		r.origin = frame.parent.Realm.origin
		r.policy = append(csp.PolicySet(nil), frame.parent.Realm.contentPolicy()...)
	}
	clusterHeader := ""
	if len(policies) != 0 {
		clusterHeader = policies[0].originAgentCluster
	}
	r.initializeAgentCluster(clusterHeader)
	r.initializeClientHints(policyHeader)
	r.updateSelectorTarget(u.Fragment)
	if deferred {
		r.runtime = &deferredRuntime{realm: r}
	} else {
		runtime, err := r.newRuntime()
		if err != nil {
			cancelResources()
			return nil, err
		}
		r.runtime = runtime
		r.runtimeGroup.add(r)
	}
	r.scheduler = scheduler.New(p.ClockNow(), func(ctx context.Context) error {
		return r.checkpoint(ctx)
	})
	r.scheduler.SetExecutionScale(p.environmentView().Time.ExecutionScale)
	var previousClock *scheduler.Scheduler
	r.scheduler.SetTaskStarted(func() { previousClock = p.activeClock.Swap(r.scheduler) })
	r.scheduler.SetTaskObserver(func(start, end time.Time) {
		p.mu.Lock()
		if end.After(p.clock) {
			p.clock = end
		}
		p.mu.Unlock()
		if r.apiTracking && r.performance != nil {
			r.recordPerformanceTask(start, end)
		}
		p.activeClock.Store(previousClock)
	})
	r.scheduler.SetSequenceSource(func() uint64 { return p.taskSequence.Add(1) })
	r.scheduler.SetObserver(func(t scheduler.Transition) {
		if t.Name == "posted" {
			p.WakeEventLoop()
		}
		if t.Name == "end" {
			r.webTaskAbort = nil
		}
		p.trace.Add(trace.Scheduler, t.Name, map[string]any{"taskId": t.TaskID, "source": t.Source, "due": t.Due, "realm": r.ID})
	})
	r.runtime.SetTimeSource(r.performanceClockNow)
	r.runtime.SetGlobalAccessObserver(func(name string, supported bool) {
		if r.apiTracking {
			r.recordAPIAccess("Window."+name, supported)
		}
	})
	p.registerRealm(r)
	if deferred {
		return r, nil
	}
	if err := r.install(); err != nil {
		r.Close()
		return nil, err
	}
	r.registerPermissions()
	return r, nil
}

func (r *Realm) registerPermissions() {
	c := r.agent.Page().ctx
	c.mu.Lock()
	if c.permissionRealms == nil {
		c.permissionRealms = map[*Realm]struct{}{}
	}
	c.permissionRealms[r] = struct{}{}
	c.mu.Unlock()
}
func (r *Realm) checkpoint(ctx context.Context) error {
	if r.checkpointClosed {
		return ctx.Err()
	}
	p := r.agent.Page()
	if p.crossRealmDepth > 0 || p.checkpointDraining {
		p.requireCheckpoint(r)
		return ctx.Err()
	}
	p.checkpointDraining = true
	defer func() { p.checkpointDraining = false }()
	if err := r.checkpointRuntime(ctx); err != nil {
		return err
	}
	for len(p.pendingCheckpoints) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		target := p.pendingCheckpoints[0]
		p.pendingCheckpoints[0] = nil
		p.pendingCheckpoints = p.pendingCheckpoints[1:]
		target.checkpointQueued = false
		if !target.checkpointClosed {
			// Script cleanup may own this realm's V8 thread. A foreign job
			// can synchronously call back into it, so keep that owner receptive
			// while draining the Page's other microtask queues.
			checkpoint := target.checkpointRuntime
			var err error
			if nested, ok := r.runtime.(engine.ReentrantRuntime); ok && target != r {
				err = nested.RunNested(ctx, checkpoint)
			} else {
				err = checkpoint(ctx)
			}
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *Page) requireCheckpoint(r *Realm) {
	if r.checkpointQueued || r.checkpointClosed {
		return
	}
	r.checkpointQueued = true
	p.pendingCheckpoints = append(p.pendingCheckpoints, r)
}

func (r *Realm) checkpointRuntime(ctx context.Context) error {
	defer r.agent.Page().notifyDebuggerProgress()
	if r.checkpointClosed {
		return ctx.Err()
	}
	var err error
	if checkpoint, ok := r.runtime.(interface{ MicrotaskCheckpointContext(context.Context) error }); ok {
		err = checkpoint.MicrotaskCheckpointContext(ctx)
	} else {
		err = r.runtime.MicrotaskCheckpoint()
	}
	if err != nil {
		return err
	}
	if native, ok := r.runtime.(interface{ NativeTasksPending() bool }); ok && native.NativeTasksPending() && !r.nativePollQueued {
		r.nativePollQueued = true
		r.scheduler.Post(scheduler.Control, time.Millisecond, func(context.Context) error {
			r.nativePollQueued = false
			return nil // The scheduler performs the checkpoint after this task.
		})
	}
	return nil
}

// A classic script's cleanup checkpoint runs before restoring currentScript.
// Promise reactions at that checkpoint still observe the executing script in
// Chrome; subsequent tasks (including its load event) must not observe it.
func (r *Realm) evaluateClassicScript(ctx context.Context, source, name string, scriptID int64) error {
	frame, topFrame := r.agent.(*Frame)
	if gate := r.agent.Page().executionGate; gate != nil && topFrame && frame == r.agent.Page().Top {
		_, external := r.document.GetAttribute(scriptID, "src")
		if !gate.AdmitClassic(r.documentURL().String(), name, source, external) {
			if observe := r.agent.Page().ctx.browser.classicScriptSkipped; observe != nil {
				observe(name, source, external)
			}
			return r.checkpoint(ctx)
		}
	}
	if admit := r.agent.Page().ctx.browser.classicScriptAdmission; admit != nil {
		_, external := r.document.GetAttribute(scriptID, "src")
		if !admit(name, source, external) {
			// Acquisition, parser scheduling and element load events stay on their
			// ordinary paths. Only author execution is omitted by this experiment.
			return r.checkpoint(ctx)
		}
	}
	previous := r.currentScript
	r.currentScript = scriptID
	defer func() { r.currentScript = previous }()
	p := r.agent.Page()
	if p.userScriptDepth == 0 {
		p.databaseScriptEpoch++
	}
	p.userScriptDepth++
	var evalErr error
	if origin, ok := r.runtime.(engine.ScriptOriginRuntime); ok {
		var lineOffset, columnOffset int32
		if position, found := r.document.ScriptPosition(scriptID); found && name == r.documentURL().String() {
			lineOffset, columnOffset = int32(position.Line-1), int32(position.Column-1)
		}
		_, evalErr = origin.EvalWithOrigin(ctx, source, name, lineOffset, columnOffset)
	} else {
		_, evalErr = r.runtime.Eval(ctx, source, name)
	}
	p.userScriptDepth--
	if evalErr != nil && r.windowErrorReporter != nil {
		var thrown engine.ThrownValue
		if errors.As(evalErr, &thrown) {
			var details engine.ExceptionDetails
			var frames []engine.NativeStackFrame
			var located engine.ExceptionLocation
			if errors.As(evalErr, &located) {
				details, frames = located.SourceLocation()
			}
			var stack []any
			for _, frame := range frames {
				stack = append(stack, map[string]any{"functionName": frame.Function, "url": frame.URL, "lineNumber": frame.Line, "columnNumber": frame.Column})
			}
			location := map[string]any{"message": "Uncaught " + details.Message, "filename": details.Filename, "lineno": details.Line, "colno": details.Column}
			_, reportErr := r.runtime.Call(ctx, r.windowErrorReporter, nil, thrown.ThrownValue(), r.val(location), r.val(stack))
			if reportErr != nil {
				p.trace.Add(trace.Error, "windowErrorReporting", map[string]any{"url": name, "realm": r.ID, "error": reportErr.Error()})
			}
		}
	}
	return errors.Join(evalErr, r.checkpoint(ctx))
}

func (r *Realm) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	r.runtimeGroup.remove(r)
	releaseRuntimeValues(r.runtime, r.apiTraceSet)
	r.apiTraceSet = nil
	r.deactivateMediaSources()
	r.stopMediaLoads()
	r.stopCameraTracks()
	r.closeRTCMedia()
	r.agent.Page().ctx.network.RevokeBlobsForOwner(r.ID)
	r.reportBlitzCalls()
	var nativeCloseErr error
	if r.blitz != nil {
		nativeCloseErr = r.runOnOwner(context.Background(), func(context.Context) error {
			r.blitz.document.Close()
			r.blitz = nil
			return nil
		})
	}
	r.blitzInputs = nil
	r.closeSpeechProvider()
	r.speech = nil
	r.speechNotifier = nil
	r.cameraNotifier = nil
	r.rtcMediaNotifier = nil
	r.closeIndexedDatabases()
	r.files.close()
	if r.cookieUnsubscribe != nil {
		r.cookieUnsubscribe()
		r.cookieUnsubscribe = nil
	}
	p := r.agent.Page()
	p.mu.Lock()
	delete(p.realmOwners, r.ID)
	p.mu.Unlock()
	r.checkpointClosed = true
	r.closeWebSockets()
	r.scheduler.Close()
	clear(r.timers)
	c := r.agent.Page().ctx
	c.mu.Lock()
	delete(c.permissionRealms, r)
	for _, state := range c.capabilities {
		remaining := state.locks[:0]
		for _, lock := range state.locks {
			if lock.ClientID != r.ID {
				remaining = append(remaining, lock)
			}
		}
		state.locks = remaining
	}
	c.mu.Unlock()
	if r.documentStream != nil {
		r.documentStream.cancel()
		r.documentStream.parser.Abort()
		r.documentStream = nil
	}
	r.cancelResources()
	r.resourceWG.Wait()
	r.orderedScripts = nil
	r.moduleFetches = nil
	r.moduleGraphs = nil
	r.preparedModules = nil
	r.imageLoads = nil
	r.mediaLoads = nil
	r.cameraTracks = nil
	r.rtcMediaPeers = nil
	r.rtcMediaNotifier = nil
	r.mediaSources = nil
	r.mediaSourceNotifier = nil
	r.availableImages = nil
	r.fontChoices = nil
	r.textShapeCache = nil
	r.textCacheProfile = nil
	r.preloads = nil
	r.stylesheetLoads = nil
	r.parserStylesheetEvents = nil
	for _, worker := range r.workers {
		_ = worker.Close()
	}
	r.childFrames = nil
	r.retainedFrames = nil
	r.crossValues = nil
	r.realmReferences = nil
	r.windowReferences = nil
	r.cacheHandles = nil
	r.cookieNotifier = nil
	r.launchNotifier = nil
	r.performance = nil
	r.performanceNavigationResponse = nil
	r.performanceLifecycleTimes = nil
	r.performanceConfidence = nil
	runtimeCloseErr := r.runtime.Close()
	// A failed owner dispatch must not skip the rest of Page teardown. After
	// closing the runtime no observer can touch the native handle, so release
	// a handle left behind by an already-closed owner as well.
	if r.blitz != nil && runtimeCloseErr == nil {
		r.blitz.document.Close()
		r.blitz = nil
	}
	return errors.Join(nativeCloseErr, runtimeCloseErr)
}
func (r *Realm) Evaluate(ctx context.Context, source, name string) (engine.Value, error) {
	p := r.agent.Page()
	if p.userScriptDepth == 0 {
		p.databaseScriptEpoch++
	}
	p.userScriptDepth++
	defer func() { p.userScriptDepth-- }()
	v, err := r.runtime.Eval(ctx, source, name)
	if err != nil {
		r.agent.Page().trace.Add(trace.Exception, "evaluation", map[string]any{"source": name, "error": err.Error()})
		msg := err.Error()
		if strings.Contains(msg, "ReferenceError:") && strings.Contains(msg, " is not defined") {
			name := strings.TrimSpace(strings.SplitN(strings.TrimPrefix(msg, "ReferenceError:"), " is not defined", 2)[0])
			r.agent.Page().trace.Add(trace.Unsupported, name, map[string]any{"realm": r.ID})
		}
	}
	return v, err
}

func (r *Realm) EvaluateModule(ctx context.Context, source, name string, loader engine.ModuleLoader) (engine.Value, error) {
	moduleRuntime, ok := r.runtime.(engine.ModuleRuntime)
	if !ok {
		return nil, fmt.Errorf("JavaScript engine does not support ECMAScript modules")
	}
	v, err := moduleRuntime.EvalModule(ctx, source, name, loader)
	// V8 returns a rejected evaluation promise for a synchronous module throw,
	// rather than a failed embedder call. Inspect without pumping jobs or
	// blocking on top-level await so script diagnostics don't report success.
	if err == nil {
		_, _, err = r.runtime.Await(v)
	}
	if err != nil {
		r.agent.Page().trace.Add(trace.Exception, "evaluation", map[string]any{"source": name, "error": err.Error(), "module": true})
	}
	return v, err
}
func (r *Realm) RunUntilIdle(ctx context.Context) error {
	if err := r.scheduler.RunUntilIdle(ctx, 10000); err != nil {
		return err
	}
	for _, frame := range r.childFrames {
		if frame.Realm != nil {
			if err := frame.Realm.RunUntilIdle(ctx); err != nil {
				return err
			}
		}
	}
	return r.scheduler.RunUntilIdle(ctx, 10000)
}
func (r *Realm) RunReady(ctx context.Context) error {
	for turn := 0; turn < 10000; turn++ {
		progress, err := scheduler.RunReadyAcross(ctx, r.readyQueues(nil))
		if err != nil || !progress {
			return err
		}
	}
	return fmt.Errorf("page scheduler task limit exceeded")
}

// runTask queues and completes one browser-owned event-loop turn without
// draining work scheduled by that turn. Parser scripts and lifecycle events
// are navigation boundaries: their callbacks and microtask checkpoints must
// finish before parsing can continue, while timers and network callbacks they
// enqueue belong to later turns driven by the Page event-loop pump.
func (r *Realm) runTask(ctx context.Context, source scheduler.Source, callback scheduler.Callback) error {
	completed := false
	r.scheduler.Post(source, 0, func(taskContext context.Context) error {
		defer func() { completed = true }()
		return callback(taskContext)
	})
	for turn := 0; turn < 10000; turn++ {
		progress, err := scheduler.RunReadyAcross(ctx, r.readyQueues(nil))
		if err != nil {
			return err
		}
		if completed {
			return nil
		}
		if !progress {
			return fmt.Errorf("browser task did not become ready")
		}
	}
	return fmt.Errorf("page scheduler task limit exceeded before browser task")
}

func (r *Realm) readyQueues(queues []*scheduler.Scheduler) []*scheduler.Scheduler {
	queues = append(queues, r.scheduler)
	for _, world := range r.isolatedWorlds {
		queues = append(queues, world.scheduler)
	}
	if f := r.pictureInPicture; f != nil && f.Realm != nil && !f.Realm.inactive {
		queues = f.Realm.readyQueues(queues)
	}
	for _, frame := range r.childFrames {
		if frame.Realm != nil {
			queues = frame.Realm.readyQueues(queues)
		}
	}
	return queues
}
func (r *Realm) AdvanceBy(ctx context.Context, delta time.Duration) error {
	r.scheduler.AdvanceBy(delta)
	if err := r.scheduler.RunReady(ctx, 10000); err != nil {
		return err
	}
	return r.scheduler.RunReady(ctx, 10000)
}
func arg(args []engine.Value, n int) any {
	if n >= len(args) || args[n] == nil {
		return nil
	}
	return args[n].Export()
}
func strarg(args []engine.Value, n int) string {
	if n < len(args) && args[n] != nil {
		if text, ok := args[n].(interface{ Text() string }); ok {
			return text.Text()
		}
	}
	v := arg(args, n)
	if v == nil {
		return ""
	}
	if text, ok := v.(string); ok {
		return text
	}
	return fmt.Sprint(v)
}
func numarg(args []engine.Value, n int) float64 {
	if n < len(args) && args[n] != nil {
		if number, ok := args[n].(interface{ Number() float64 }); ok {
			return number.Number()
		}
	}
	switch v := arg(args, n).(type) {
	case int64:
		return float64(v)
	case int32:
		return float64(v)
	case int:
		return float64(v)
	case float64:
		return v
	case float32:
		return float64(v)
	}
	return 0
}
func numberValue(value any) float64 {
	switch number := value.(type) {
	case int:
		return float64(number)
	case int32:
		return float64(number)
	case int64:
		return float64(number)
	case float32:
		return float64(number)
	case float64:
		return number
	default:
		return 0
	}
}
func transportPhases(value any) map[string]float64 {
	if timing, ok := value.(network.TransportTimingSnapshot); ok {
		return timing.Phases
	}
	return nil
}
func transportPhase(phases map[string]float64, name string, fallback float64) float64 {
	if value, ok := phases[name]; ok {
		return value
	}
	return fallback
}
func performanceProtocol(value any) string {
	protocol := strings.ToLower(fmt.Sprint(value))
	switch {
	case strings.Contains(protocol, "3"):
		return "h3"
	case strings.Contains(protocol, "2"):
		return "h2"
	case strings.Contains(protocol, "1.1"):
		return "http/1.1"
	case strings.Contains(protocol, "1.0"):
		return "http/1.0"
	default:
		return ""
	}
}
func (r *Realm) fn(f engine.Function) any { return r.runtime.Function(f) }

func (r *Realm) transientFn(f engine.Function) any {
	if runtime, ok := r.runtime.(interface{ TransientFunction(engine.Function) any }); ok {
		return runtime.TransientFunction(f)
	}
	return r.fn(f)
}
func (r *Realm) val(v any) engine.Value { return r.runtime.Value(v) }

func (r *Realm) packedFn(f engine.Function, signature string) any {
	if runtime, ok := r.runtime.(interface {
		PackedFunction(engine.Function, string) any
	}); ok {
		return runtime.PackedFunction(f, signature)
	}
	return r.transientFn(f)
}
func (r *Realm) install() error {
	if native, ok := r.runtime.(engine.RealmSecurityRuntime); ok {
		if err := native.SetSecurityOrigin(r.nativeSecurityOrigin()); err != nil {
			return err
		}
	}
	err := r.installBindings()
	if err != nil && r.bootstrapRestored && r.agent.Page().ctx.lifetime.Err() == nil {
		return r.retryBootstrap(err)
	}
	return err
}

func (r *Realm) nativeSecurityOrigin() string {
	if r.origin != "" && r.origin != "null" {
		return r.origin
	}
	if frame, ok := r.agent.(*Frame); ok && frame.parent != nil && frame.parent.Realm != nil && r.url.Scheme == "about" && (r.url.Opaque == "blank" || r.url.Opaque == "srcdoc") {
		return frame.parent.Realm.nativeSecurityOrigin()
	}
	return "opaque:" + r.ID
}

func (r *Realm) installBindings() error {
	wanted := r.agent.Page().trace.ObservationWanted()
	r.apiTraceActive = wanted
	if runtime, ok := r.runtime.(interface{ SetPropertyObservationEnabled(bool) error }); ok {
		if err := runtime.SetPropertyObservationEnabled(wanted); err != nil {
			return err
		}
	}
	// Bootstrap is one synchronous Page operation. Keep its host installation,
	// restore hooks and intrinsic reads on the existing runtime owner instead of
	// dispatching each Get/Call/Eval separately. This neither pumps tasks nor
	// checkpoints jobs, and a failed snapshot is disposed only after we return.
	return r.runOnOwner(context.Background(), func(context.Context) error {
		return r.installBindingsOnOwner()
	})
}

func (r *Realm) installBindingsOnOwner() error {
	defer func() {
		if r.bootstrapCapture != nil {
			r.agent.Page().ctx.browser.bootstrapSnapshots.abandon(r.bootstrapCapture)
			r.bootstrapCapture = nil
		}
	}()

	p := r.agent.Page()
	host := map[string]any{}
	host["lazyDomainSource"] = r.transientFn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		name := strarg(args, 0)
		source, ok := webapi.LazyDomainSource(name)
		if !ok {
			return nil, fmt.Errorf("unknown lazy WebAPI domain %q", name)
		}
		return r.val(source), nil
	})
	_, supportsInternalEvalCodeObjects := r.runtime.(engine.EvalSourceRuntime)
	host["internalEvalCodeObjects"] = supportsInternalEvalCodeObjects
	r.installBlitzProducer(host)
	host["layoutTaffy"] = r.transientFn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		output, err := r.document.FlatLayoutState().PublishJSON(r.document.Revision(), strarg(args, 0))
		if err != nil {
			return nil, err
		}
		return r.val(output), nil
	})
	installLocaleHost(host, r.runtime, func() state.Locale { return p.environmentView().Locale })
	installStructuredCloneHost(host, r.runtime)
	r.files = installOPFSHost(host, r.runtime, r.agent.Page().ctx, func() string { return r.origin })
	r.installDocumentStream(host)
	host["token"] = r.transientFn(func(engine.Value, []engine.Value) (engine.Value, error) { return r.val(r.token), nil })
	host["ready"] = r.transientFn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		if len(args) != 0 {
			releaseRuntimeValues(r.runtime, r.apiTraceSet)
			r.apiTraceSet = retainRuntimeValue(r.runtime, args[0])
		}
		r.apiTracking = true
		r.initializeMemoryProjection()
		return r.val(p.trace.ObservationWanted()), nil
	})
	host["selfFrameID"] = r.transientFn(func(engine.Value, []engine.Value) (engine.Value, error) { return r.val(r.agent.ContextID()), nil })
	host["windowRelations"] = r.transientFn(func(engine.Value, []engine.Value) (engine.Value, error) {
		frame, ok := r.agent.(*Frame)
		if !ok {
			return r.val(map[string]any{"self": r.agent.ContextID(), "parent": r.agent.ContextID(), "top": r.agent.ContextID()}), nil
		}
		parent := frame
		if frame.parent != nil {
			parent = frame.parent
		}
		return r.val(map[string]any{"self": frame.ID, "parent": parent.ID, "top": frame.Top().ID}), nil
	})
	host["windowFrames"] = r.transientFn(func(engine.Value, []engine.Value) (engine.Value, error) {
		frameIDs := make([]string, 0, len(r.childFrames))
		var visit func(int64)
		visit = func(parentID int64) {
			for _, child := range r.document.Children(parentID) {
				if frame := r.childFrames[child.ID]; frame != nil {
					frameIDs = append(frameIDs, frame.ID)
					continue
				}
				visit(child.ID)
			}
		}
		visit(r.document.Root().ID)
		return r.val(frameIDs), nil
	})
	host["documentActive"] = r.transientFn(func(engine.Value, []engine.Value) (engine.Value, error) { return r.val(!r.inactive), nil })
	host["documentHasLayout"] = r.transientFn(func(engine.Value, []engine.Value) (engine.Value, error) {
		if r.inactive {
			return r.val(false), nil
		}
		frame, ok := r.agent.(*Frame)
		if !ok {
			return r.val(false), nil
		}
		for frame != nil && frame.parent != nil {
			p.mu.RLock()
			attached := p.frames[frame.ID] == frame
			p.mu.RUnlock()
			parent := frame.parent.Realm
			if !attached || parent == nil || parent.inactive || parent.frameLayoutRead == nil {
				return r.val(false), nil
			}
			visible := false
			err := parent.runOnOwner(context.Background(), func(ctx context.Context) error {
				value, err := parent.runtime.Call(ctx, parent.frameLayoutRead, nil, parent.val(frame.elementID))
				if err == nil {
					visible, _ = value.Export().(bool)
				}
				return err
			})
			if err != nil {
				return nil, err
			}
			if !visible {
				return r.val(false), nil
			}
			frame = frame.parent
		}
		return r.val(true), nil
	})
	host["frameElement"] = r.transientFn(func(engine.Value, []engine.Value) (engine.Value, error) {
		frame, ok := r.agent.(*Frame)
		if !ok || r.inactive || frame.parent == nil || !r.canAccess(frame.parent) {
			return r.val(nil), nil
		}
		target, err := r.worldForFrame(frame.parent)
		if err != nil {
			return nil, err
		}
		if _, ok := target.document.Get(frame.elementID); !ok {
			return r.val(nil), nil
		}
		return r.crossFrameResult(target, func(ctx context.Context) (engine.Value, error) {
			return target.runtime.Call(ctx, target.frameNodeDescribe, nil, target.val(frame.elementID), target.val(true))
		})
	})
	host["iframeWindow"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		connectedThroughShadow, _ := arg(a, 1).(bool)
		frame, err := r.ensureChildFrame(int64(numarg(a, 0)), connectedThroughShadow)
		if err != nil || frame == nil {
			return r.val(nil), err
		}
		return r.val(frame.ID), nil
	})
	host["frameRelation"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		frame := p.frame(strarg(a, 0))
		if frame == nil {
			return r.val(nil), nil
		}
		if frame.Realm != nil && frame.Realm.inactive {
			return r.val(nil), nil
		}
		switch strarg(a, 1) {
		case "parent":
			if frame.parent != nil {
				return r.val(frame.parent.ID), nil
			}
		case "top":
			return r.val(frame.Top().ID), nil
		}
		return r.val(frame.ID), nil
	})
	host["frameEval"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		target, err := r.referenceRealm(strarg(a, 0), strarg(a, 2))
		if err != nil {
			return nil, err
		}
		if !r.canAccess(p.frame(strarg(a, 0))) {
			return nil, nil
		}
		// Native eval leaves non-string inputs unchanged. The caller resolves
		// TrustedScript source using its private slots, then validates access.
		if len(a) < 2 || r.runtime.TypeOf(a[1]) == "undefined" {
			return nil, nil
		}
		source := strarg(a, 1)
		return r.crossFrameResult(target, func(ctx context.Context) (engine.Value, error) {
			return target.Evaluate(ctx, source, "frame-eval")
		})
	})
	host["frameCall"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		return r.callFrameReference(a)
	})
	host["frameConsoleKind"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		target, err := r.referenceRealm(strarg(a, 0), strarg(a, 2))
		if err != nil {
			return nil, err
		}
		value := target.crossValues[int64(numarg(a, 1))]
		classifier, ok := target.runtime.(engine.ConsoleValueRuntime)
		if value == nil || !ok {
			return r.val(""), nil
		}
		kind, err := classifier.ConsoleValueKind(value)
		if err != nil {
			return nil, err
		}
		return r.val(kind), nil
	})
	r.installFrameDocumentBridge(host)
	r.installWorldObservationBridge(host)
	r.installWindowReflection(host)
	host["frameGet"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		target, err := r.referenceRealm(strarg(a, 0), strarg(a, 3))
		if err != nil {
			return nil, err
		}
		value := target.crossValues[int64(numarg(a, 1))]
		if value == nil {
			return r.val(map[string]any{"__mimicCrossRealm": "undefined"}), nil
		}
		return r.reflectFrameGet(target, value, arg(a, 2))
	})
	host["frameGlobalGet"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		frame := p.frame(strarg(a, 0))
		if !r.canAccess(frame) {
			// postMessage is exposed by the cross-origin WindowProxy whitelist,
			// independently of properties installed by the target document.
			key, _ := arg(a, 1).(map[string]any)
			if frame != nil && key["kind"] == "string" && key["value"] == "postMessage" {
				return r.val(map[string]any{"__mimicCrossRealm": "undefined", "intrinsic": true}), nil
			}
			return nil, fmt.Errorf("SecurityError: Blocked cross-origin frame access")
		}
		target, err := r.worldForFrame(frame)
		if err != nil {
			return nil, err
		}
		rawKey := arg(a, 1)
		var intrinsic bool
		result, err := r.crossFrameResult(target, func(ctx context.Context) (engine.Value, error) {
			if key, ok := rawKey.(map[string]any); ok && key["kind"] == "string" {
				name, _ := key["value"].(string)
				if name != "eval" && name != "postMessage" {
					return target.runtime.Get(name), nil
				}
			}
			key, err := target.decodeFrameKey(ctx, rawKey)
			if err != nil {
				return nil, err
			}
			if keyData, ok := rawKey.(map[string]any); ok && keyData["kind"] == "symbol" {
				record, err := target.callFrameReflection(ctx, "get", target.runtime.Get("globalThis"), key, nil)
				if err != nil {
					return nil, err
				}
				return target.runtime.GetProperty(record, "value"), nil
			}
			record, err := target.runtime.Call(ctx, target.frameGlobalRead, nil, key)
			if err != nil {
				return nil, err
			}
			intrinsic, _ = target.runtime.GetProperty(record, "intrinsic").Export().(bool)
			return target.runtime.GetProperty(record, "value"), nil
		})
		if err != nil {
			return nil, err
		}
		if intrinsic {
			// crossFrameResult may still be a lazy host map in V8. Mutate
			// the encoded envelope before conversion, not a temporary object.
			encoded := result.Export().(map[string]any)
			encoded["intrinsic"] = true
			return r.val(encoded), nil
		}
		return result, nil
	})
	host["framePrototype"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		target, err := r.referenceRealm(strarg(a, 0), strarg(a, 2))
		if err != nil {
			return nil, err
		}
		value := target.crossValues[int64(numarg(a, 1))]
		if value == nil {
			return r.val(map[string]any{"__mimicCrossRealm": "null"}), nil
		}
		return r.crossFrameResult(target, func(ctx context.Context) (engine.Value, error) {
			return target.callFrameReflection(ctx, "prototype", value, nil, nil)
		})
	})
	host["frameLocation"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		frame := p.frame(strarg(a, 0))
		if frame == nil || frame.Realm == nil {
			return r.val(""), nil
		}
		return r.val(frame.Realm.url.String()), nil
	})
	host["framePost"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		return nil, r.postToFrame(strarg(a, 0), arg(a, 1), strarg(a, 2), stringSlice(arg(a, 3)))
	})
	r.installDocumentFocus(host)
	host["navigator"] = r.transientFn(func(engine.Value, []engine.Value) (engine.Value, error) {
		p.trace.Add(trace.API, "Navigator", map[string]any{"realm": r.ID})
		environment := p.environmentView()
		values := environment.NavigatorProjection(p.NetworkPolicy().Offline(), navigatorWebDriver)
		return r.val(values), nil
	})
	host["screen"] = r.transientFn(func(engine.Value, []engine.Value) (engine.Value, error) {
		p.trace.Add(trace.API, "Screen", map[string]any{"realm": r.ID})
		environment := p.environmentView()
		s := environment.Screen()
		orientation := environment.ScreenOrientation
		orientationType, orientationAngle := "landscape-primary", 0
		if orientation != nil {
			orientationType, orientationAngle = orientation.Type, orientation.Angle
		}
		return r.val(map[string]any{"width": s.Width, "height": s.Height, "availWidth": s.AvailWidth, "availHeight": s.AvailHeight, "colorDepth": s.ColorDepth, "pixelDepth": s.PixelDepth, "devicePixelRatio": s.DevicePixelRatio, "orientationType": orientationType, "orientationAngle": orientationAngle}), nil
	})
	host["windowState"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		frame, ok := r.agent.(*Frame)
		if !ok {
			return r.val(nil), nil
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		switch strarg(a, 0) {
		case "name":
			if len(a) > 1 {
				frame.windowName = strarg(a, 1)
			}
			return r.val(frame.windowName), nil
		case "status":
			if len(a) > 1 {
				r.windowStatus = strarg(a, 1)
			}
			return r.val(r.windowStatus), nil
		case "closed":
			return r.val(frame.windowClosing || p.frames[frame.ID] != frame), nil
		case "documentHidden":
			return r.val(p.frames[frame.ID] != frame), nil
		}
		return r.val(nil), nil
	})
	host["installViewportNotifier"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		r.viewportNotifier = a[0]
		return nil, nil
	})
	host["installEventInvoker"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		r.eventListenerInvoker = a[0]
		return nil, nil
	})
	host["eventCallbackCheckpoint"] = r.transientFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		if p.userScriptDepth != 0 {
			return nil, nil
		}
		if runtime, ok := r.runtime.(interface{ NativeCallbackCheckpoint() error }); ok {
			return nil, runtime.NativeCallbackCheckpoint()
		}
		return nil, nil
	})
	host["installFrameViewport"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		r.frameViewportRead = a[0]
		if len(a) > 1 {
			r.frameLayoutRead = a[1]
		}
		return nil, nil
	})
	host["viewport"] = r.transientFn(func(engine.Value, []engine.Value) (engine.Value, error) {
		w := p.environmentView().Window
		p.mu.RLock()
		frame, isFrame := r.agent.(*Frame)
		detached := isFrame && frame != p.Top && p.frames[frame.ID] != frame
		p.mu.RUnlock()
		if detached {
			return r.val(map[string]any{"width": 0, "height": 0, "outerWidth": 0, "outerHeight": 0, "screenX": w.X, "screenY": w.Y}), nil
		}
		if isFrame && frame.auxiliaryOpener != nil {
			w.ViewportWidth, w.ViewportHeight = frame.auxiliaryWidth, frame.auxiliaryHeight
			w.OuterWidth, w.OuterHeight = frame.auxiliaryWidth, frame.auxiliaryHeight
		}
		if frame, ok := r.agent.(*Frame); ok && frame.parent != nil && frame.parent.Realm != nil {
			parent := frame.parent.Realm
			if parent.frameViewportRead != nil {
				var size []any
				err := parent.runOnOwner(context.Background(), func(ctx context.Context) error {
					v, e := parent.runtime.Call(ctx, parent.frameViewportRead, nil, parent.val(frame.elementID))
					if e == nil {
						size, _ = v.Export().([]any)
					}
					return e
				})
				if err != nil {
					return nil, err
				}
				if len(size) == 2 {
					w.ViewportWidth = int(numberValue(size[0]))
					w.ViewportHeight = int(numberValue(size[1]))
				}
			}
		}
		return r.val(map[string]any{"width": w.ViewportWidth, "height": w.ViewportHeight, "outerWidth": w.OuterWidth, "outerHeight": w.OuterHeight, "screenX": w.X, "screenY": w.Y}), nil
	})
	host["observationVersion"] = r.transientFn(func(engine.Value, []engine.Value) (engine.Value, error) {
		environment := p.environmentView()
		w := environment.Window
		owner := r
		if r.mainWorld != nil {
			owner = r.mainWorld
		}
		preferences := environment.Preferences
		// The suffix is the environment-only epoch used by retained selector
		// matches; unrelated DOM mutations must not discard their rule programs.
		// Style inputs and geometry resources have different lifetimes. Return
		// both in one crossing so JS can retain selector/cascade work across
		// image-only completions while invalidating boxes and used values.
		return r.val(fmt.Sprintf("%d:%d:%d|%d:%d:%s:%t:%g:%t|%d", r.styleDocumentRevision(), owner.styleResourceRevision.Load(), r.selectorTargetID, w.ViewportWidth, w.ViewportHeight, preferences.ColorScheme, preferences.ReducedMotion, environment.Display.DeviceScaleFactor, owner.styleProjections.isDynamic(), owner.resourceRevision.Load())), nil
	})
	host["observationRevision"] = r.packedFn(func(_ engine.Value, _ []engine.Value) (engine.Value, error) {
		return r.val(r.styleDocumentRevision()), nil
	}, "")
	if detacher, ok := r.runtime.(engine.ArrayBufferDetacher); ok {
		host["detachArrayBuffer"] = r.runtime.Function(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("expected ArrayBuffer")
			}
			return nil, detacher.DetachArrayBuffer(args[0])
		})
	}
	installTextureDecoder(host, r.runtime)
	host["graphics"] = r.transientFn(func(engine.Value, []engine.Value) (engine.Value, error) {
		g := p.environmentView().Graphics
		return r.val(map[string]any{"vendor": g.Vendor, "renderer": g.Renderer, "maxTextureSize": g.MaxTextureSize, "capabilitiesJSON": g.WebGLCapabilities()}), nil
	})
	host["rtcEnvironment"] = r.transientFn(func(engine.Value, []engine.Value) (engine.Value, error) {
		ice := p.environmentView().Network.ICE
		count := ice.HostCandidateCount
		if count < 0 {
			count = 0
		}
		offsets := append([]int(nil), ice.PortOffsets...)
		if len(offsets) != count {
			offsets = make([]int, count)
			for index := range offsets {
				offsets[index] = index * 2
			}
		}
		reflexiveOffsets := append([]int(nil), ice.ReflexivePortOffsets...)
		reflexiveCount := ice.ReflexiveCandidateCount
		if reflexiveCount < 0 {
			reflexiveCount = 0
		}
		if len(reflexiveOffsets) != reflexiveCount {
			reflexiveOffsets = make([]int, reflexiveCount)
			for index := range reflexiveOffsets {
				reflexiveOffsets[index] = index * 2
			}
		}
		return r.val(map[string]any{"hostCandidateCount": count, "reflexiveCandidateCount": reflexiveCount, "portOffsets": offsets, "reflexivePortOffsets": reflexiveOffsets, "publicAddress": ice.PublicAddress, "networkCost": ice.NetworkCost, "hostDelayMillis": ice.HostDelayMillis, "reflexiveDelayMillis": ice.ReflexiveDelayMillis, "endDelayMillis": ice.EndDelayMillis}), nil
	})
	host["hasStorageAccess"] = r.transientFn(func(engine.Value, []engine.Value) (engine.Value, error) {
		return r.val(p.environmentView().Network.CookiesEnabled && r.origin != "null"), nil
	})
	host["systemFonts"] = r.transientFn(func(engine.Value, []engine.Value) (engine.Value, error) {
		return r.val(p.environmentView().SystemFontPalette()), nil
	})
	host["audioDevice"] = r.transientFn(func(engine.Value, []engine.Value) (engine.Value, error) {
		a := p.environmentView().Audio
		if a.SampleRate <= 0 {
			a.SampleRate = 48000
		}
		if a.Channels <= 0 {
			a.Channels = 2
		}
		if a.BufferDuration <= 0 {
			a.BufferDuration = 0.01
		}
		if a.MaxBufferFrames <= 0 {
			a.MaxBufferFrames = 7680
		}
		return r.val(map[string]any{"sampleRate": a.SampleRate, "channels": a.Channels, "bufferDuration": a.BufferDuration, "maxBufferFrames": a.MaxBufferFrames}), nil
	})
	host["systemColors"] = r.transientFn(func(engine.Value, []engine.Value) (engine.Value, error) {
		return r.val(p.environmentView().SystemColorPalette()), nil
	})
	host["gpuCapabilities"] = r.transientFn(func(engine.Value, []engine.Value) (engine.Value, error) {
		return r.val(p.environmentView().Graphics.WebGPUProjection()), nil
	})
	host["gpuRequestAdapter"] = r.transientFn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		promise := newHostPromise(r.runtime)
		delay := time.Duration(p.environmentView().Graphics.WebGPU.InitializationDelayMillis * float64(time.Millisecond))
		preference, fallback := strarg(args, 0), strarg(args, 1) == "fallback"
		r.scheduler.Post(scheduler.Control, delay, func(context.Context) error {
			g := p.environmentView().Graphics
			adapter, ok := g.SelectWebGPUAdapter(preference, fallback)
			if !ok {
				return promise.Resolve(nil)
			}
			return promise.Resolve(adapter)
		})
		return promise.Value, nil
	})
	performanceIsolated := r.securityState().crossOriginIsolated
	host["performanceNow"] = r.transientFn(func(engine.Value, []engine.Value) (engine.Value, error) {
		return r.val(p.performanceClamper.now(r.performanceClockNow(), r.performanceOrigin, performanceIsolated)), nil
	})
	host["animationTimelineNow"] = r.transientFn(func(engine.Value, []engine.Value) (engine.Value, error) {
		// Borrowed frame/world operations observe the active Page turn's sample.
		clock := p.activeClock.Load()
		if clock == nil {
			clock = r.scheduler
		}
		return r.val(p.performanceClamper.now(clock.SampledNow(), r.performanceOrigin, performanceIsolated)), nil
	})
	host["performanceTimeOrigin"] = r.transientFn(func(engine.Value, []engine.Value) (engine.Value, error) {
		return r.val(float64(p.performanceClamper.micros(r.performanceOrigin.UnixMicro(), performanceIsolated)) / 1000), nil
	})
	r.initPerformance(host)
	host["queuePerformanceObserver"] = r.transientFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		if len(a) == 0 {
			return nil, nil
		}
		callback := retainRuntimeValue(r.runtime, a[0])
		r.scheduler.Post(scheduler.DOM, 0, func(ctx context.Context) error {
			defer releaseRuntimeValues(r.runtime, callback)
			receiver := r.runtime.Get("window")
			result, err := r.runtime.Call(ctx, callback, receiver)
			releaseRuntimeValues(r.runtime, result, receiver)
			return err
		})
		return nil, nil
	})
	// A sampled frame's callbacks must run before an author timer that became
	// ready while the frame task was executing.
	host["queueIntersectionObserver"] = r.transientFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		if len(a) == 0 {
			return nil, nil
		}
		callback := retainRuntimeValue(r.runtime, a[0])
		r.scheduler.Post(scheduler.Rendering, 0, func(ctx context.Context) error {
			defer releaseRuntimeValues(r.runtime, callback)
			receiver := r.runtime.Get("window")
			result, err := r.runtime.Call(ctx, callback, receiver)
			releaseRuntimeValues(r.runtime, result, receiver)
			return err
		})
		return nil, nil
	})
	host["queuePostedMessage"] = r.transientFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		if len(a) == 0 {
			return nil, nil
		}
		callback := retainRuntimeValue(r.runtime, a[0])
		r.scheduler.Post(scheduler.PostedMessage, 0, func(ctx context.Context) error {
			defer releaseRuntimeValues(r.runtime, callback)
			receiver := r.runtime.Get("window")
			result, err := r.runtime.Call(ctx, callback, receiver)
			releaseRuntimeValues(r.runtime, result, receiver)
			return err
		})
		return nil, nil
	})
	host["newMessageChannel"] = r.fn(func(_ engine.Value, _ []engine.Value) (engine.Value, error) {
		return r.val(p.newMessageChannel(r)), nil
	})
	host["messagePortPost"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		return nil, p.postMessagePort(r, strarg(a, 0), arg(a, 1), stringSlice(arg(a, 2)))
	})
	host["messagePortClose"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		p.closeMessagePort(strarg(a, 0))
		return nil, nil
	})
	host["randomBytes"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		n := int(numarg(a, 0))
		if n < 0 || n > 65536 {
			return nil, fmt.Errorf("Crypto.getRandomValues length must be between 0 and 65536 bytes")
		}
		buf := make([]byte, n)
		if _, err := cryptorand.Read(buf); err != nil {
			return nil, err
		}
		out := make([]int, n)
		for i, b := range buf {
			out[i] = int(b)
		}
		p.trace.Add(trace.API, "Crypto.getRandomValues", map[string]any{"bytes": n, "realm": r.ID})
		return r.val(out), nil
	})
	host["internalRandomBytes"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		n := int(numarg(a, 0))
		if n < 0 || n > 65536 {
			return nil, fmt.Errorf("internal random length must be between 0 and 65536 bytes")
		}
		buf := make([]byte, n)
		if _, err := cryptorand.Read(buf); err != nil {
			return nil, err
		}
		out := make([]int, n)
		for i, b := range buf {
			out[i] = int(b)
		}
		return r.val(out), nil
	})
	host["randomUUID"] = r.fn(func(engine.Value, []engine.Value) (engine.Value, error) {
		p.trace.Add(trace.API, "Crypto.randomUUID", map[string]any{"realm": r.ID})
		return r.val(uuid.NewString()), nil
	})
	host["internalRandomUUID"] = r.fn(func(engine.Value, []engine.Value) (engine.Value, error) {
		return r.val(uuid.NewString()), nil
	})
	host["subtleDigest"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		algorithm, input := strings.ToUpper(strings.ReplaceAll(strarg(a, 0), "_", "-")), byteSlice(arg(a, 1))
		var digest []byte
		switch algorithm {
		case "SHA-1":
			sum := sha1.Sum(input)
			digest = sum[:]
		case "SHA-256":
			sum := sha256.Sum256(input)
			digest = sum[:]
		case "SHA-384":
			sum := sha512.Sum384(input)
			digest = sum[:]
		case "SHA-512":
			sum := sha512.Sum512(input)
			digest = sum[:]
		default:
			return nil, fmt.Errorf("unsupported digest algorithm %q", algorithm)
		}
		out := make([]int, len(digest))
		for i, value := range digest {
			out[i] = int(value)
		}
		return r.val(out), nil
	})
	host["subtleImportRSAOAEP"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		der := byteSlice(arg(a, 0))
		parsed, err := x509.ParsePKIXPublicKey(der)
		if err != nil {
			return nil, fmt.Errorf("invalid SPKI key: %w", err)
		}
		key, ok := parsed.(*rsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("SPKI key is not RSA")
		}
		exponent := key.E
		publicExponent := []int{}
		for shift := 24; shift >= 0; shift -= 8 {
			value := (exponent >> shift) & 0xff
			if value != 0 || len(publicExponent) != 0 {
				publicExponent = append(publicExponent, value)
			}
		}
		parts := make([]string, len(publicExponent))
		for i, value := range publicExponent {
			parts[i] = strconv.Itoa(value)
		}
		return r.val(strconv.Itoa(key.N.BitLen()) + "|" + strings.Join(parts, ",")), nil
	})
	host["subtleRSAOAEPEncrypt"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		algorithm, der, input, label := strings.ToUpper(strings.ReplaceAll(strarg(a, 0), "_", "-")), byteSlice(arg(a, 1)), byteSlice(arg(a, 2)), byteSlice(arg(a, 3))
		parsed, err := x509.ParsePKIXPublicKey(der)
		if err != nil {
			return nil, fmt.Errorf("invalid SPKI key: %w", err)
		}
		key, ok := parsed.(*rsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("SPKI key is not RSA")
		}
		var hash crypto.Hash
		switch algorithm {
		case "SHA-1":
			hash = crypto.SHA1
		case "SHA-256":
			hash = crypto.SHA256
		case "SHA-384":
			hash = crypto.SHA384
		case "SHA-512":
			hash = crypto.SHA512
		default:
			return nil, fmt.Errorf("unsupported RSA-OAEP hash %q", algorithm)
		}
		ciphertext, err := rsa.EncryptOAEP(hash.New(), cryptorand.Reader, key, input, label)
		if err != nil {
			return nil, err
		}
		out := make([]int, len(ciphertext))
		for i, value := range ciphertext {
			out[i] = int(value)
		}
		return r.val(out), nil
	})
	host["subtleAESGCM"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		operation, key, iv, additionalData, input, tagBits := strarg(a, 0), byteSlice(arg(a, 1)), byteSlice(arg(a, 2)), byteSlice(arg(a, 3)), byteSlice(arg(a, 4)), int(numarg(a, 5))
		block, err := aes.NewCipher(key)
		if err != nil {
			return nil, err
		}
		tagSize := tagBits / 8
		var aead cipher.AEAD
		switch {
		case len(iv) == 12 && tagSize == 16:
			aead, err = cipher.NewGCM(block)
		case len(iv) == 12:
			aead, err = cipher.NewGCMWithTagSize(block, tagSize)
		case tagSize == 16:
			aead, err = cipher.NewGCMWithNonceSize(block, len(iv))
		default:
			err = fmt.Errorf("non-standard AES-GCM nonce and tag sizes cannot be combined")
		}
		if err != nil {
			return nil, err
		}
		var result []byte
		if operation == "encrypt" {
			result = aead.Seal(nil, iv, input, additionalData)
		} else {
			result, err = aead.Open(nil, iv, input, additionalData)
			if err != nil {
				return nil, err
			}
		}
		out := make([]int, len(result))
		for i, value := range result {
			out[i] = int(value)
		}
		return r.val(out), nil
	})
	host["title"] = r.transientFn(func(engine.Value, []engine.Value) (engine.Value, error) { return r.val(r.document.Title()), nil })
	host["readyState"] = r.transientFn(func(engine.Value, []engine.Value) (engine.Value, error) {
		state := r.readyState
		if r.mainWorld != nil {
			state = r.mainWorld.readyState
		}
		p.trace.Add(trace.API, "Document.readyStateValue", map[string]any{"value": state, "realm": r.ID})
		return r.val(state), nil
	})
	if native, ok := r.runtime.(engine.UndetectableRuntime); ok {
		host["createUndetectable"] = r.fn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
			if len(args) == 0 {
				return nil, fmt.Errorf("undetectable handlers are required")
			}
			return native.NewUndetectableObject(args[0])
		})
	}
	host["currentScript"] = r.transientFn(func(engine.Value, []engine.Value) (engine.Value, error) {
		if r.currentScript == 0 {
			return r.val(nil), nil
		}
		node, ok := r.document.Get(r.currentScript)
		if !ok {
			return r.val(nil), nil
		}
		return r.val(nodeData(node)), nil
	})
	host["setTitle"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		r.document.SetTitle(strarg(a, 0))
		return nil, nil
	})
	host["query"] = r.transientFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		n, ok := r.document.FindWithin(r.document.Root().ID, strarg(a, 0))
		if p.trace.Wants(trace.API) {
			p.trace.Add(trace.API, "Document.querySelector", map[string]any{"selector": strarg(a, 0), "realm": r.ID, "result": queryTraceResult(n, ok)})
		}
		if !ok {
			return r.val(nil), nil
		}
		return r.val(nodeData(n)), nil
	})
	host["queryWithin"] = r.packedFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		selector := strarg(a, 1)
		n, ok := r.document.FindWithin(int64(numarg(a, 0)), selector)
		if p.trace.Wants(trace.API) {
			p.trace.Add(trace.API, "Element.querySelector", map[string]any{"nodeId": int64(numarg(a, 0)), "selector": selector, "realm": r.ID, "result": queryTraceResult(n, ok)})
		}
		if !ok {
			return r.val(nil), nil
		}
		return r.val(nodeData(n)), nil
	}, "ns")
	host["queryAll"] = r.transientFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		ids := r.document.FindAllIDs(0, strarg(a, 0))
		if p.trace.Wants(trace.API) {
			p.trace.Add(trace.API, "Document.querySelectorAll", map[string]any{"selector": strarg(a, 0), "realm": r.ID, "resultNodeIds": append([]int64{}, ids...)})
		}
		return r.val(ids), nil
	})
	host["queryAllWithin"] = r.packedFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		ids := r.document.FindAllIDs(int64(numarg(a, 0)), strarg(a, 1))
		if p.trace.Wants(trace.API) {
			p.trace.Add(trace.API, "Element.querySelectorAll", map[string]any{"nodeId": int64(numarg(a, 0)), "selector": strarg(a, 1), "realm": r.ID, "resultNodeIds": append([]int64{}, ids...)})
		}
		return r.val(ids), nil
	}, "ns")
	host["classIDs"] = r.packedFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		return r.val(r.document.FindClassIDs(int64(numarg(a, 0)), strarg(a, 1), numarg(a, 2) != 0)), nil
	}, "nsn")
	host["domRevision"] = r.packedFn(func(_ engine.Value, _ []engine.Value) (engine.Value, error) {
		return r.val(r.document.Revision()), nil
	}, "")
	host["mutationJournal"] = r.packedFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		encoded, err := json.Marshal(r.document.MutationsSince(uint64(numarg(a, 0))))
		if err != nil {
			return nil, err
		}
		return r.val(string(encoded)), nil
	}, "n")
	host["observationMutationJournal"] = r.packedFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		encoded, err := json.Marshal(r.document.ObservationMutationsSince(uint64(numarg(a, 0))))
		if err != nil {
			return nil, err
		}
		return r.val(string(encoded)), nil
	}, "n")
	host["matches"] = r.packedFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		return r.val(r.document.Matches(int64(numarg(a, 0)), strarg(a, 1))), nil
	}, "ns")
	host["attributeNames"] = r.packedFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		return r.val(r.document.AttributeNameList(int64(numarg(a, 0)))), nil
	}, "n")
	host["styleObservationState"] = r.packedFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		return r.val(r.document.StyleObservationState(int64(numarg(a, 0)))), nil
	}, "n")
	host["nodeData"] = r.transientFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		n, ok := r.document.Get(int64(numarg(a, 0)))
		if !ok {
			return r.val(nil), nil
		}
		return r.val(nodeData(n)), nil
	})
	host["scriptAsync"] = r.transientFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		id := int64(numarg(a, 0))
		if len(a) > 1 {
			r.document.ClearScriptForceAsync(id)
		}
		n, _ := r.document.Get(id)
		_, present := n.Attributes["async"]
		return r.val(n.ScriptForceAsync || present), nil
	})
	host["setDOMQueryCallback"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		if len(a) > 0 {
			r.domQueryCallback = a[0]
		}
		return nil, nil
	})
	host["registerShadowSnapshot"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		if len(a) > 0 {
			r.shadowSnapshotCallback = a[0]
		}
		if len(a) > 1 {
			r.shadowSnapshotRestore = a[1]
		}
		if len(a) > 2 {
			r.shadowSnapshotVersion = a[2]
		}
		return nil, nil
	})
	r.installFormNavigation(host)
	host["registerFormSnapshot"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		if len(a) > 0 {
			r.formSnapshotCallback = a[0]
		}
		return nil, nil
	})
	host["serializeNodeList"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		encoded, err := json.Marshal(arg(a, 0))
		if err != nil {
			return nil, err
		}
		var ids []int64
		if err := json.Unmarshal(encoded, &ids); err != nil {
			return nil, err
		}
		markup, err := r.document.SerializeNodeList(ids)
		return r.val(markup), err
	})
	host["selectorTargetID"] = r.transientFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) { return r.val(r.selectorTargetID), nil })
	host["templateContent"] = r.transientFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		n, ok := r.document.TemplateContent(int64(numarg(a, 0)))
		if !ok {
			return nil, nil
		}
		return r.val(nodeData(n)), nil
	})
	host["hostIncludingContains"] = r.packedFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		return r.val(r.document.HostIncludingContains(int64(numarg(a, 0)), int64(numarg(a, 1)))), nil
	}, "nn")
	host["nodeMarkup"] = r.packedFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		markup, err := r.document.OuterHTML(int64(numarg(a, 0)))
		return r.val(markup), err
	}, "n")
	host["copyNodeState"] = r.packedFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		r.document.CopyNodeState(int64(numarg(a, 0)), int64(numarg(a, 1)))
		return nil, nil
	}, "nn")
	host["documentRootID"] = r.transientFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		return r.val(r.document.Root().ID), nil
	})
	host["getAttribute"] = r.packedFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		value, ok := r.document.GetAttribute(int64(numarg(a, 0)), strarg(a, 1))
		if !ok {
			return r.val(nil), nil
		}
		return r.val(value), nil
	}, "ns")
	host["toggleToken"] = r.packedFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		value, err := r.document.ToggleToken(int64(numarg(a, 0)), strarg(a, 1), strarg(a, 2), int(numarg(a, 3)))
		return r.val(value), err
	}, "nssn")
	host["inlineStyleState"] = r.packedFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		return r.val(r.document.InlineStyleState(int64(numarg(a, 0)))), nil
	}, "n")
	host["copyInlineStyle"] = r.packedFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		r.document.CopyInlineStyle(int64(numarg(a, 0)), int64(numarg(a, 1)))
		return nil, nil
	}, "nn")
	host["setInlineStyle"] = r.packedFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		old, err := r.document.SetInlineStyle(int64(numarg(a, 0)), strarg(a, 1), strarg(a, 2))
		return r.val(old), err
	}, "nss")
	host["setAttribute"] = r.packedFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		id, name, value := int64(numarg(a, 0)), strarg(a, 1), strarg(a, 2)
		if err := r.document.SetAttribute(id, name, value); err != nil {
			return nil, err
		}
		if strings.EqualFold(name, "src") {
			if node, ok := r.document.Get(id); ok {
				if node.TagName == "IMG" {
					r.updateImage(id, true)
				} else if node.TagName == "AUDIO" || node.TagName == "VIDEO" {
					r.updateMedia(id, false)
				} else if node.TagName == "SOURCE" {
					if parent, ok := r.document.Get(node.Parent); ok && (parent.TagName == "AUDIO" || parent.TagName == "VIDEO") {
						r.updateMedia(parent.ID, false)
					}
				}
			}
		}
		if strings.EqualFold(name, "loading") {
			if node, ok := r.document.Get(id); ok && node.TagName == "IMG" {
				r.updateImage(id, false)
			}
		}
		r.childFrameAttributeChanged(id, name)
		if node, ok := r.document.Get(id); ok && node.TagName == "META" {
			r.refreshContentPolicy()
		}
		return nil, nil
	}, "nss")
	host["removeAttribute"] = r.transientFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		id, name := int64(numarg(a, 0)), strarg(a, 1)
		node, _ := r.document.Get(id)
		_, existed := node.Attributes[strings.ToLower(name)]
		err := r.document.RemoveAttribute(id, name)
		if err == nil && strings.EqualFold(name, "src") {
			if node, ok := r.document.Get(id); ok {
				if node.TagName == "IMG" {
					r.updateImage(id, true)
				} else if node.TagName == "AUDIO" || node.TagName == "VIDEO" {
					r.updateMedia(id, false)
				}
			}
		}
		if err == nil && strings.EqualFold(name, "loading") {
			if node, ok := r.document.Get(id); ok && node.TagName == "IMG" {
				r.updateImage(id, false)
			}
		}
		if err == nil && existed {
			r.childFrameAttributeChanged(id, name)
		}
		return nil, err
	})
	host["elementsByTagName"] = r.transientFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		tag := strarg(a, 0)
		p.trace.Add(trace.API, "Document.getElementsByTagName", map[string]any{"tag": tag, "realm": r.ID})
		nodes := r.document.FindAllByTagName(tag)
		out := make([]map[string]any, 0, len(nodes))
		for _, n := range nodes {
			out = append(out, nodeData(n))
		}
		return r.val(out), nil
	})
	host["create"] = r.packedFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		tag := strarg(a, 0)
		n := r.document.CreateElement(tag)
		r.detached[n.ID] = n
		// ASCII case normalization is identical in Go and JavaScript. For
		// other names preserve Go's existing Unicode normalization exactly.
		for i := 0; i < len(tag); i++ {
			if tag[i] >= 128 {
				return r.val(nodeData(n)), nil
			}
		}
		return r.val(n.ID), nil
	}, "s")
	host["createNS"] = r.transientFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		n := r.document.CreateElementNS(strarg(a, 0), strarg(a, 1))
		r.detached[n.ID] = n
		return r.val(nodeData(n)), nil
	})
	host["createComment"] = r.packedFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		n := r.document.CreateComment(strarg(a, 0))
		r.detached[n.ID] = n
		return r.val(n.ID), nil
	}, "s")
	host["createText"] = r.packedFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		n := r.document.CreateText(strarg(a, 0))
		r.detached[n.ID] = n
		return r.val(n.ID), nil
	}, "s")
	host["append"] = r.fn(r.hostAppend)
	host["insert"] = r.fn(r.hostInsert)
	// Non-resource insertion consumes only canonical IDs; resource insertion
	// retains its callback-bearing path and browser scheduling semantics.
	host["insertPlain"] = r.packedFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		child := int64(numarg(a, 1))
		if err := r.document.InsertNode(int64(numarg(a, 0)), child, int64(numarg(a, 2))); err != nil {
			if errors.Is(err, dom.ErrInsertionReference) {
				return r.val(-1), nil
			}
			if errors.Is(err, dom.ErrInsertionCycle) {
				return r.val(-2), nil
			}
			return nil, err
		}
		delete(r.detached, child)
		r.refreshContentPolicy()
		if err := r.prepareInsertedScripts(int64(numarg(a, 0)), child); err != nil {
			return nil, err
		}
		return r.val(r.document.HasFrameElements()), nil
	}, "nnn")
	host["contains"] = r.packedFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		return r.val(r.document.Contains(int64(numarg(a, 0)), int64(numarg(a, 1)))), nil
	}, "nn")
	host["firstElementChild"] = r.packedFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		n, ok := r.document.FirstElementChild(int64(numarg(a, 0)))
		if !ok {
			return r.val(nil), nil
		}
		return r.val(n.ID), nil
	}, "n")
	host["firstChild"] = r.packedFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		n, ok := r.document.FirstChild(int64(numarg(a, 0)))
		if !ok {
			return r.val(nil), nil
		}
		return r.val(n.ID), nil
	}, "n")
	host["nodeChildren"] = r.transientFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		return r.val(nodesData(r.document.Children(int64(numarg(a, 0))))), nil
	})
	host["nodeChildCount"] = r.packedFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		return r.val(r.document.ChildCount(int64(numarg(a, 0)))), nil
	}, "n")
	host["nodeChildAt"] = r.transientFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		n, ok := r.document.ChildAt(int64(numarg(a, 0)), int(numarg(a, 1)))
		if !ok {
			return nil, nil
		}
		return r.val(n.ID), nil
	})
	host["parentNode"] = r.packedFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		n, ok := r.document.Parent(int64(numarg(a, 0)))
		if !ok {
			return r.val(nil), nil
		}
		// Parent access needs identity, not a fresh copy of all attributes and
		// children on every ancestor read. wrap resolves uncached IDs lazily.
		return r.val(n.ID), nil
	}, "n")
	host["isConnected"] = r.transientFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		return r.val(r.document.IsConnected(int64(numarg(a, 0)))), nil
	})
	host["sibling"] = r.packedFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		n, ok := r.document.Sibling(int64(numarg(a, 0)), int(numarg(a, 1)))
		if !ok {
			return r.val(nil), nil
		}
		return r.val(n.ID), nil
	}, "nn")
	host["elementChildren"] = r.packedFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		return r.val(r.document.ChildIDs(int64(numarg(a, 0)), true)), nil
	}, "n")
	host["childIDs"] = r.packedFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		return r.val(r.document.ChildIDs(int64(numarg(a, 0)), false)), nil
	}, "n")
	host["insertPlainFragment"] = r.packedFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		inserted, err := r.document.InsertPlainFragment(int64(numarg(a, 0)), int64(numarg(a, 1)), int64(numarg(a, 2)))
		if errors.Is(err, dom.ErrInsertionReference) {
			return r.val(-1), nil
		}
		if errors.Is(err, dom.ErrInsertionCycle) {
			return r.val(-2), nil
		}
		if err != nil {
			return nil, err
		}
		if inserted {
			r.refreshContentPolicy()
		}
		return r.val(inserted), nil
	}, "nnn")
	host["drainFragment"] = r.packedFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		// Frame removal can synchronously complete a pending navigation. Keep
		// that callback-bearing path until it can be combined without moving
		// the load event relative to the removal of individual children.
		if r.document.HasFrameElements() {
			return r.val(nil), nil
		}
		children, err := r.document.DrainFragment(int64(numarg(a, 0)))
		if err != nil {
			return nil, err
		}
		if children == nil {
			children = []int64{}
		}
		return r.val(children), nil
	}, "n")
	host["removeNode"] = r.packedFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		childID := int64(numarg(a, 1))
		if err := r.document.RemoveNode(int64(numarg(a, 0)), childID); err != nil {
			return nil, err
		}
		// Removing a container disconnects all embedded browsing contexts in
		// its subtree, not only an iframe which is the removed node itself.
		for elementID := range r.childFrames {
			if r.document.Contains(childID, elementID) {
				r.detachChildFrame(elementID)
			}
		}
		return nil, nil
	}, "nn")
	host["prepareNodeRemoval"] = r.packedFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		return r.val(r.prepareChildFrameRemoval(int64(numarg(a, 0)))), nil
	}, "n")
	host["completeSynchronousLoad"] = r.transientFn(func(_ engine.Value, _ []engine.Value) (engine.Value, error) {
		if r.loadCallback != nil {
			r.navigationLoadEnd = r.scheduler.Now()
			r.loadCallback(context.Background())
		}
		return nil, nil
	})
	host["textContent"] = r.packedFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		return r.val(r.document.TextContent(int64(numarg(a, 0)))), nil
	}, "n")
	host["textContentJSON"] = r.packedFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		return r.val(r.document.TextContentJSON(int64(numarg(a, 0)))), nil
	}, "n")
	host["setCharacterDataJSON"] = r.packedFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		return nil, r.document.SetCharacterDataJSON(int64(numarg(a, 0)), strarg(a, 1))
	}, "ns")
	host["mediaLoad"] = r.packedFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		r.updateMedia(int64(numarg(a, 0)), true)
		return nil, nil
	}, "n")
	r.installMediaSourceHosts(host)
	host["mediaCurrentSrc"] = r.packedFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		load := r.mediaLoads[int64(numarg(a, 0))]
		if load == nil {
			return r.val(""), nil
		}
		return r.val(load.currentSrc), nil
	}, "n")
	host["setTextContent"] = r.transientFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		id := int64(numarg(a, 0))
		var err error
		if len(a) > 2 {
			err = r.document.SetTextContentJSON(id, strarg(a, 2))
		} else {
			err = r.document.SetTextContent(id, strarg(a, 1))
		}
		if err != nil {
			return nil, err
		}
		return nil, r.prepareChangedScript(id)
	})
	host["innerHTML"] = r.transientFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		value, err := r.document.InnerHTML(int64(numarg(a, 0)))
		return r.val(value), err
	})
	host["outerHTML"] = r.transientFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		value, err := r.document.OuterHTML(int64(numarg(a, 0)))
		return r.val(value), err
	})
	host["setOuterHTML"] = r.transientFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		name, err := r.document.SetOuterHTML(int64(numarg(a, 0)), strarg(a, 1))
		return r.val(name), err
	})
	host["insertAdjacentHTML"] = r.transientFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		name, err := r.document.InsertAdjacentHTML(int64(numarg(a, 0)), strarg(a, 1), strarg(a, 2))
		return r.val(name), err
	})
	host["setInnerHTML"] = r.transientFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		id := int64(numarg(a, 0))
		if err := r.document.SetInnerHTML(id, strarg(a, 1)); err != nil {
			return nil, err
		}
		return nil, r.prepareChangedScript(id)
	})
	host["elementNonce"] = r.transientFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		id := int64(numarg(a, 0))
		if len(a) > 1 {
			return nil, r.document.SetNonce(id, strarg(a, 1))
		}
		node, ok := r.document.Get(id)
		if !ok {
			return r.val(""), nil
		}
		return r.val(node.Nonce), nil
	})
	host["documentReferrer"] = r.transientFn(func(engine.Value, []engine.Value) (engine.Value, error) { return r.val(r.documentReferrer), nil })
	host["documentDomain"] = r.transientFn(func(engine.Value, []engine.Value) (engine.Value, error) {
		u, _ := url.Parse(r.origin)
		if u == nil {
			return r.val(""), nil
		}
		return r.val(u.Hostname()), nil
	})
	host["location"] = r.transientFn(func(engine.Value, []engine.Value) (engine.Value, error) { return r.val(r.documentURL().String()), nil })
	host["locationPart"] = r.transientFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		u := r.documentURL()
		part := strarg(a, 0)
		switch part {
		case "href":
			return r.val(u.String()), nil
		case "origin":
			return r.val(originOf(u.String())), nil
		case "protocol":
			return r.val(u.Scheme + ":"), nil
		case "host":
			return r.val(u.Host), nil
		case "hostname":
			return r.val(u.Hostname()), nil
		case "port":
			return r.val(u.Port()), nil
		case "pathname":
			return r.val(u.Path), nil
		case "search":
			if u.RawQuery != "" {
				return r.val("?" + u.RawQuery), nil
			}
		case "hash":
			if u.Fragment != "" {
				return r.val("#" + u.Fragment), nil
			}
		}
		return r.val(""), nil
	})
	host["locationAncestorOrigins"] = r.transientFn(func(engine.Value, []engine.Value) (engine.Value, error) {
		origins := append([]string{}, r.ancestorOrigins...)
		return r.val(origins), nil
	})
	host["windowOrigin"] = r.transientFn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		if id := strarg(args, 0); id != "" {
			frame := p.frame(id)
			if !r.canAccess(frame) {
				return nil, fmt.Errorf("blocked cross-origin Window access")
			}
			return r.val(frame.Realm.origin), nil
		}
		return r.val(r.origin), nil
	})
	installURLHost(host, r.runtime, r.documentURL)
	host["setLocationPart"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		part, v := strarg(a, 0), strarg(a, 1)
		u := r.documentURL()
		switch part {
		case "href":
			return nil, r.postNavigate(v)
		case "hash":
			reference, err := url.Parse("#" + strings.TrimPrefix(v, "#"))
			if err != nil {
				return nil, err
			}
			if reference.Fragment == u.Fragment && reference.RawFragment == u.RawFragment {
				return nil, nil
			}
			u.Fragment, u.RawFragment = reference.Fragment, reference.RawFragment
			r.navigateFragment(u, false)
			return nil, nil
		case "search":
			u.RawQuery = strings.TrimPrefix(v, "?")
		case "pathname":
			u.Path, u.RawPath = v, ""
		}
		return nil, r.postNavigate(u.String())
	})
	host["navigate"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		replace, _ := arg(a, 1).(bool)
		reload, _ := arg(a, 2).(bool)
		return nil, r.postNavigate(strarg(a, 0), replace, reload)
	})
	host["installHistoryClone"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		r.historyCloneFunction = a[0]
		return nil, nil
	})
	if detector, ok := r.runtime.(engine.StructuredCloneProxyRuntime); ok {
		host["historyCloneIsProxy"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
			return r.val(detector.IsStructuredCloneProxy(a[0])), nil
		})
	}
	if cloner, ok := r.runtime.(engine.StructuredCloneRuntime); ok {
		host["cloneHistoryValue"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
			value, err := cloner.StructuredClone(a[0], a[1])
			var cloneError *engine.DataCloneError
			if errors.As(err, &cloneError) {
				return r.val([]any{false, cloneError.Message}), nil
			}
			if err != nil {
				return nil, err
			}
			reply, err := r.runtime.Eval(context.Background(), "[true,null]", "mimic:history-clone-result")
			if err != nil {
				return nil, err
			}
			if err := r.runtime.SetProperty(reply, "1", value); err != nil {
				return nil, err
			}
			return reply, nil
		})
	}
	host["historyPush"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		message, err := r.historyPush(strarg(a, 0), false, a[1])
		return r.val(message), err
	})
	host["historyReplace"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		message, err := r.historyPush(strarg(a, 0), true, a[1])
		return r.val(message), err
	})
	host["historyState"] = r.transientFn(func(engine.Value, []engine.Value) (engine.Value, error) { return r.historyState(), nil })
	host["historyIsActive"] = r.transientFn(func(engine.Value, []engine.Value) (engine.Value, error) { return r.val(r.activeHistoryDocument()), nil })
	host["historyGo"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		r.historyGo(int(numarg(a, 0)))
		return nil, nil
	})
	host["historyLength"] = r.transientFn(func(engine.Value, []engine.Value) (engine.Value, error) {
		if f, ok := r.agent.(*Frame); ok && disabledSessionHistory(f) {
			return r.val(0), nil
		}
		return r.val(p.historyLength()), nil
	})
	host["setTimer"] = r.transientFn(r.hostTimer)
	host["clearTimer"] = r.transientFn(r.hostClearTimer)
	r.installWindowExceptionReporting(host)
	host["createWorker"] = r.fn(r.hostCreateWorker)
	host["createObjectURL"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		// Chrome returns the empty string when a retained function is called
		// after its document's execution context has been deactivated.
		if r.inactive || r.closed {
			return r.val(""), nil
		}
		raw := "blob:" + r.origin + "/" + uuid.NewString()
		p.ctx.network.PutBlob(raw, byteSlice(arg(a, 0)), strarg(a, 1), r.ID)
		return r.val(raw), nil
	})
	host["revokeObjectURL"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		p.ctx.network.RevokeBlob(strarg(a, 0))
		return nil, nil
	})
	host["workerPost"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		id := int64(numarg(a, 0))
		p.trace.Add(trace.JS, "workerPostMessage", map[string]any{"worker": id, "realm": r.ID})
		worker := r.workers[id]
		if worker != nil {
			worker.PostMessage(arg(a, 1), strarg(a, 2))
		}
		return nil, nil
	})
	host["terminateWorker"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		id := int64(numarg(a, 0))
		p.trace.Add(trace.JS, "workerTerminate", map[string]any{"worker": id, "realm": r.ID})
		if worker := r.workers[id]; worker != nil {
			_ = worker.Close()
			delete(r.workers, id)
		}
		return nil, nil
	})
	host["fetch"] = r.transientFn(r.hostFetch)
	host["openWindow"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		u, err := r.resolveDocument(strarg(a, 0))
		if err != nil {
			return r.val(nil), nil
		}
		popup, err := p.ctx.NewPage()
		if err != nil {
			return r.val(nil), nil
		}
		p.trace.Add(trace.Lifecycle, "windowOpen", map[string]any{"targetId": popup.ID, "url": u.String(), "windowName": strarg(a, 1), "userGesture": r.navigationActivated()})
		return r.val(nil), nil
	})
	host["abortFetch"] = r.transientFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		if cancel := r.fetchCancels[strarg(a, 0)]; cancel != nil {
			cancel()
		}
		return nil, nil
	})
	host["abortXHR"] = r.transientFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		if cancel := r.xhrCancels[strarg(a, 0)]; cancel != nil {
			cancel()
			delete(r.xhrCancels, strarg(a, 0))
		}
		return nil, nil
	})
	host["xhr"] = r.fn(r.hostXHR)
	host["openWebSocket"] = r.fn(r.hostOpenWebSocket)
	host["sendWebSocket"] = r.fn(r.hostSendWebSocket)
	host["closeWebSocket"] = r.fn(r.hostCloseWebSocket)
	installConsoleKind(host, r.runtime)
	host["executionContextActive"] = r.transientFn(func(_ engine.Value, _ []engine.Value) (engine.Value, error) { return r.val(!r.inactive), nil })
	host["console"] = r.transientFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		if len(a) > 2 {
			r.debuggerConsole(strarg(a, 0), a[2])
		}
		p.trace.Add(trace.Console, strarg(a, 0), map[string]any{"args": arg(a, 1), "realm": r.ID, "remoteValues": len(a) > 2})
		return nil, nil
	})
	host["unsupported"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		p.trace.Add(trace.Unsupported, strarg(a, 0), map[string]any{"realm": r.ID})
		return nil, nil
	})
	host["apiAccess"] = r.transientFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		name := strarg(a, 0)
		supported, _ := arg(a, 1).(bool)
		r.recordAPIAccess(name, supported)
		return nil, nil
	})
	host["semanticMissing"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		recordSemanticBoundary(p.trace, "realm", r.ID, "legacy-host", a)
		return nil, nil
	})
	host["semanticMissingAt"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		recordSemanticBoundary(p.trace, "realm", r.ID, strarg(a, 0), a[1:])
		return nil, nil
	})
	host["media"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		q := strings.ToLower(strarg(a, 0))
		e := p.environmentView()
		match := strings.TrimSpace(q) == "screen" || strings.TrimSpace(q) == "all" || strings.TrimSpace(q) == ""
		if strings.Contains(q, "prefers-color-scheme: light") {
			match = e.Preferences.ColorScheme == "light"
		}
		if strings.Contains(q, "prefers-color-scheme: dark") {
			match = e.Preferences.ColorScheme == "dark"
		}
		if strings.Contains(q, "prefers-reduced-motion: reduce") {
			match = e.Preferences.ReducedMotion
		}
		if strings.Contains(q, "prefers-reduced-motion: no-preference") {
			match = !e.Preferences.ReducedMotion
		}
		if strings.Contains(q, "min-width") {
			match = e.Window.ViewportWidth >= 1
		}
		return r.val(match), nil
	})
	host["documentSecurity"] = r.transientFn(func(engine.Value, []engine.Value) (engine.Value, error) {
		security := r.securityState()
		return r.val(map[string]any{
			"secureContext":       security.secureContext,
			"crossOriginIsolated": security.crossOriginIsolated,
			"credentialless":      security.credentialless,
			"originAgentCluster":  security.originAgentCluster,
		}), nil
	})
	host["permissionsPolicy"] = r.transientFn(func(engine.Value, []engine.Value) (engine.Value, error) {
		policy := r.securityState().permissionsPolicy
		origin := r.origin
		request := network.Request{}
		r.applyClientHints(&request)
		hints := map[string][]string{}
		if request.ClientHints != nil {
			for _, feature := range clientHintFeatures {
				hints[feature] = request.ClientHints.Allowed["sec-"+feature]
			}
		}
		return r.val(map[string]any{"header": policy, "origin": origin, "clientHints": hints}), nil
	})
	host["documentCookie"] = r.transientFn(func(engine.Value, []engine.Value) (engine.Value, error) {
		if !p.environmentView().Network.CookiesEnabled {
			return r.val(""), nil
		}
		pairs := []string{}
		for _, c := range p.ctx.cookies.ForURL(r.documentURL(), r.cookieContext()) {
			if !c.HttpOnly {
				pairs = append(pairs, c.Name+"="+c.Value)
			}
		}
		return r.val(strings.Join(pairs, "; ")), nil
	})
	host["setDocumentCookie"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		if !p.environmentView().Network.CookiesEnabled {
			return nil, nil
		}
		p.ctx.cookies.SetFromDocument(r.documentURL(), strarg(a, 0), r.cookieContext())
		return nil, nil
	})
	r.installDocumentCompatibility(host)
	r.installTextMetrics(host)
	r.installImageResources(host)
	r.installTrustedTypes(host)
	r.installProtocolInput(host)
	addStorageHosts(r, host)
	addCapabilityHosts(r, host)
	addWindowServiceHosts(r, host)
	profiling := false
	if diagnostic, ok := r.runtime.(interface{ ProfileEnabled() bool }); ok {
		profiling = diagnostic.ProfileEnabled()
	}
	if profiling {
		r.profilePhases = map[string]float64{}
		var start time.Time
		host["profilePhase"] = r.transientFn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
			if start.IsZero() {
				start = time.Now()
			}
			r.profilePhases[strarg(a, 0)] = float64(time.Since(start)) / 1e6
			return nil, nil
		})
		host["profileWrapper"] = r.fn(func(engine.Value, []engine.Value) (engine.Value, error) { r.profileWrappers++; return nil, nil })
	}
	var exposureJSON string
	host["exposureJSON"] = r.transientFn(func(engine.Value, []engine.Value) (engine.Value, error) { return r.val(exposureJSON), nil })
	var catalogJSON string
	host["catalogJSON"] = r.transientFn(func(engine.Value, []engine.Value) (engine.Value, error) { return r.val(catalogJSON), nil })
	if err := r.runtime.Set("__mimic", host); err != nil {
		return err
	}
	plan := r.bootstrapSource()
	source := plan.source
	exposureJSON, catalogJSON = plan.exposureJSON, plan.catalogJSON
	generated := ""
	if bundle := p.Compatibility(); bundle != nil && bundle.Surface() != nil {
		generated = bundle.Surface().GeneratedJavaScript
	}
	if profiling {
		source = strings.Replace(source, "  'use strict';", "  'use strict';host.profilePhase('begin');", 1)
		if generated != "" {
			source = strings.Replace(source, generated, "host.profilePhase('generated-start');\n"+generated+"\nhost.profilePhase('generated-end');", 1)
		}
		for _, phase := range []string{"finalizeBindings();", "applyTargetExposure(JSON.parse(host.exposureJSON()));", "installNavigatorCapabilities();"} {
			source = strings.Replace(source, phase, "host.profilePhase('before:"+phase+"');"+phase+"host.profilePhase('after:"+phase+"');", 1)
		}
		for _, marker := range []struct{ text, phase string }{
			{"    for(const property of properties){", "exposure:globals"},
			{"    for(const [interfaceName,members] of Object.entries(exposure.prototypes||{})){", "exposure:prototypes"},
			{"    const prototypeTargets=new Map();", "exposure:native-marking"},
			{"    for(const [prototype,expectedMembers] of prototypeTargets){", "exposure:ownership"},
		} {
			source = strings.Replace(source, marker.text, "host.profilePhase('"+marker.phase+"');"+marker.text, 1)
		}
		source = strings.Replace(source, "elementWrappers.set(key,proxy)", "host.profileWrapper();elementWrappers.set(key,proxy)", 1)
	}
	var err error
	if r.bootstrapRestored {
		restore := r.runtime.Get("__mimicRestoreBootstrap")
		if r.runtime.TypeOf(restore) != "function" {
			return fmt.Errorf("bootstrap snapshot has no restore hook")
		}
		_, err = r.runtime.Call(context.Background(), restore, nil, r.runtime.Get("__mimic"))
	} else {
		var finish engine.Value
		// Instrumented profile sources contain diagnostic callbacks and must not
		// become reusable profile seeds.
		if profiling {
			r.agent.Page().ctx.browser.bootstrapSnapshots.captured(r.bootstrapCapture, nil, fmt.Errorf("instrumented bootstrap is not snapshot eligible"))
			r.bootstrapCapture = nil
		}
		finish, err = r.beginBootstrapCapture()
		if err == nil {
			if bootstrap, ok := r.runtime.(engine.BootstrapRuntime); ok {
				_, err = bootstrap.EvalBootstrap(context.Background(), source, "mimic:webapi-surface")
			} else {
				_, err = r.runtime.Eval(context.Background(), source, "mimic:webapi-surface")
			}
		}
		r.finishBootstrapCapture(finish, source, err)
	}
	if err != nil {
		return err
	}
	if err := installNativeFunctionSource(r.runtime); err != nil {
		return err
	}
	if native, ok := r.runtime.(engine.ImportMetaRuntime); ok {
		if err := native.SetImportMetaResolveFactory(r.runtime.Get("__mimicImportMetaResolveFactory")); err != nil {
			return err
		}
	}
	r.enableAsyncModules()
	if err := installEvalSourceResolver(r.runtime); err != nil {
		return err
	}
	r.messageReceiver = r.runtime.Get("__receiveFrameMessage")
	r.documentStreamReset = r.runtime.Get("__mimicResetDocumentStream")
	r.documentStreamEvent = r.runtime.Get("__mimicDocumentStreamEvent")
	r.inputDispatcher = r.runtime.Get("__mimicDispatchInput")
	r.permissionNotifier = r.runtime.Get("__mimicPermissionChanged")
	r.geolocationNotifier = r.runtime.Get("__mimicGeolocationChanged")
	r.networkStateEvent = r.runtime.Get("__mimicNetworkStateEvent")
	if _, err = r.runtime.Eval(context.Background(), `delete globalThis.__mimicImportMetaResolveFactory;delete globalThis.__mimicDispatchInput;delete globalThis.__mimicPermissionChanged;delete globalThis.__mimicGeolocationChanged;delete globalThis.__mimicNetworkStateEvent;delete globalThis.__mimicEvalSourceResolver;delete globalThis.__mimicResetDocumentStream;delete globalThis.__mimicDocumentStreamEvent`, "mimic:hide-input"); err != nil {
		return err
	}
	r.messagePortReceiver = r.runtime.Get("__receiveMessagePort")
	r.frameLoadDispatcher = r.runtime.Get("__mimicDispatchFrameLoad")
	r.frameIndexNotifier = r.runtime.Get("__mimicSyncWindowFrames")
	if err := r.publishWindowFrameIndices(); err != nil {
		return err
	}
	r.resourceEventDispatcher = r.runtime.Get("__mimicDispatchResourceEvent")
	r.performanceNotifier = r.runtime.Get("__mimicNotifyPerformanceObservers")
	_, err = r.runtime.Eval(context.Background(), `delete globalThis.__mimic;delete globalThis.__mimicLazySurface;delete globalThis.__mimicRestoreBootstrap;delete globalThis.__mimicUnsupportedProbe;delete globalThis.__receiveFrameMessage;delete globalThis.__receiveMessagePort;delete globalThis.__mimicDispatchFrameLoad;delete globalThis.__mimicSyncWindowFrames;delete globalThis.__mimicDispatchResourceEvent;delete globalThis.__mimicNotifyPerformanceObservers`, "mimic:hide-internals")
	if err == nil {
		r.debuggerFactory, err = evalPlatformExpression(r.runtime, debuggerFactorySource, "mimic:debugger-intrinsics")
	}
	if err == nil {
		err = r.installDebuggerBindings()
	}
	return err
}

func (r *Realm) notifyPerformanceObservers(ctx context.Context) {
	if r.performance != nil {
		r.performance.sync()
		return
	}
	if r.performanceNotifier == nil {
		return
	}
	if _, err := r.runtime.Call(ctx, r.performanceNotifier, nil, r.val(!r.navigationLoadEnd.IsZero())); err != nil {
		r.agent.Page().trace.Add(trace.Error, "performanceObserverNotification", map[string]any{"realm": r.ID, "error": err.Error()})
	}
}

func passwordOf(u *url.URL) string {
	if u.User == nil {
		return ""
	}
	password, _ := u.User.Password()
	return password
}

func usernameOf(u *url.URL) string {
	if u.User == nil {
		return ""
	}
	return u.User.Username()
}

func nodeData(n dom.Node) map[string]any {
	attrs := map[string]any{}
	for k, v := range n.Attributes {
		attrs[k] = v
	}
	data := map[string]any{"nodeId": n.ID, "type": n.Type, "tagName": n.TagName, "namespaceURI": n.Namespace, "qualifiedName": n.QualifiedName, "contentType": n.ContentType, "text": n.Text, "attributes": attrs, "attributeNames": n.AttributeNames, "attributeNamespaces": n.AttributeNamespaces, "parentId": n.Parent, "children": n.Children}
	if n.ParsedDocument {
		data["documentURL"] = n.DocumentURL
		data["parsedDocument"] = true
	}
	return data
}
func nodesData(nodes []dom.Node) []map[string]any {
	out := make([]map[string]any, 0, len(nodes))
	for _, node := range nodes {
		out = append(out, nodeData(node))
	}
	return out
}
func (r *Realm) SetReadyState(state string) {
	r.readyState = state
	if state == "interactive" {
		r.performanceLifecycle("domInteractive")
	}
	if state == "complete" {
		r.performanceLifecycle("domComplete")
	}
}

func (r *Realm) beginLoadBlocker(reason string) bool {
	if r.loadCompleted || r.readyState == "complete" {
		r.agent.Page().trace.Add(trace.Lifecycle, "loadBlockerIgnored", map[string]any{"reason": reason, "readyState": r.readyState, "realm": r.ID})
		return false
	}
	r.loadBlockers++
	r.agent.Page().trace.Add(trace.Lifecycle, "loadBlockerAdded", map[string]any{"reason": reason, "count": r.loadBlockers, "realm": r.ID})
	return true
}

func (r *Realm) endLoadBlocker(reason string) {
	if r.loadBlockers > 0 {
		r.loadBlockers--
	}
	r.agent.Page().trace.Add(trace.Lifecycle, "loadBlockerRemoved", map[string]any{"reason": reason, "count": r.loadBlockers, "realm": r.ID})
	r.scheduleLoadIfReady()
}

func (r *Realm) requestLoad(callback func(context.Context)) {
	r.loadRequested = true
	r.loadCallback = callback
	r.scheduleLoadIfReady()
}

func (r *Realm) scheduleLoadIfReady() {
	if !r.loadRequested || r.loadCompleted || r.loadScheduled || r.loadBlockers != 0 {
		return
	}
	// Queue the load transition rather than changing readyState inside the task
	// that removed the final blocker. Its microtask checkpoint may insert a
	// transitive load-blocking resource before this task is selected.
	r.loadScheduled = true
	epoch := r.loadEpoch
	r.scheduler.Post(scheduler.DOM, 0, func(taskContext context.Context) error {
		if epoch != r.loadEpoch {
			return nil
		}
		r.loadScheduled = false
		if r.loadBlockers != 0 {
			r.scheduleLoadIfReady()
			return nil
		}
		r.SetReadyState("complete")
		r.performanceLifecycle("loadEventStart")
		r.agent.Page().trace.Add(trace.Lifecycle, "readyStateComplete", map[string]any{"realm": r.ID})
		r.loadCompleted = true
		if _, eventErr := r.Evaluate(taskContext, `dispatchEvent(new Event('load'))`, "mimic:load"); eventErr != nil {
			r.agent.Page().trace.Add(trace.Exception, "loadEvent", map[string]any{"url": r.documentURL().String(), "error": eventErr.Error(), "realm": r.ID})
		}
		if r.loadCallback != nil {
			r.navigationLoadEnd = r.scheduler.Now()
			r.loadCallback(taskContext)
		}
		return nil
	})
}
func (r *Realm) recordAPIAccess(name string, supported bool) {
	if !r.apiTraceActive {
		return
	}
	key := fmt.Sprintf("%s:%t", name, supported)
	if r.apiSeen[key] {
		return
	}
	r.apiSeen[key] = true
	p := r.agent.Page()
	p.trace.Add(trace.API, "propertyAccess", map[string]any{"property": name, "supported": supported, "realm": r.ID})
	if !supported {
		p.trace.Add(trace.Unsupported, name, map[string]any{"realm": r.ID, "access": "property"})
	}
}
func (r *Realm) postNavigate(raw string, replaceOption ...bool) error {
	historyTarget := r.historyTraversalTarget
	r.historyTraversalTarget = 0
	r.recordNavigationDiagnostic(raw, replaceOption)
	u, err := r.resolveDocument(raw)
	if err != nil {
		return err
	}
	replace := len(replaceOption) > 0 && replaceOption[0]
	reload := len(replaceOption) > 1 && replaceOption[1]
	current := r.documentURL()
	withoutFragment := *u
	withoutFragment.Fragment, withoutFragment.RawFragment = current.Fragment, current.RawFragment
	if !reload && strings.Contains(raw, "#") && withoutFragment.String() == current.String() {
		r.navigateFragment(u, replace)
		return nil
	}
	if frame, ok := r.agent.(*Frame); ok && frame.auxiliaryOpener != nil {
		r.scheduler.Post(scheduler.Navigation, 0, func(context.Context) error { r.closePictureInPictureWindow(frame, false); return nil })
		return nil
	}
	reason := "crossDocument"
	if reload {
		reason = "reload"
	}
	if historyTarget > 0 {
		reason = "traverse"
	}
	if !r.navigationStart(u, replace, reason) {
		return nil
	}
	if frame, ok := r.agent.(*Frame); ok && frame.parent != nil {
		if frame.Realm == r && frame.parent.Realm != nil {
			kind := "navigate"
			if reload {
				kind = "reload"
			}
			frame.parent.Realm.scheduleChildNavigationTo(frame, u, replace, r, kind, historyTarget)
		}
		return nil
	}
	request := network.Request{SourceURL: current, Referrer: current, UserActivation: r.navigationActivated()}
	request.ReferrerPolicy = r.referrerPolicy
	r.scheduler.Post(scheduler.Navigation, 0, func(ctx context.Context) error {
		return r.agent.Page().beginNavigationRequest(ctx, u.String(), uuid.NewString(), request, historyTarget, true, replace, reload)
	})
	return nil
}
func (r *Realm) navigationActivated() bool {
	return !r.activationConsumed && !r.activationAt.IsZero() && r.scheduler.Now().Sub(r.activationAt) < 5*time.Second
}
func (r *Realm) hostFetch(_ engine.Value, a []engine.Value) (engine.Value, error) {
	promise := newHostPromise(r.runtime)
	raw := strarg(a, 0)
	requestID := strarg(a, 4)
	u, err := r.resolveDocument(raw)
	if err != nil {
		_ = promise.Reject(err.Error())
		return promise.Value, nil
	}
	request := fetchRequest(r.agent.ContextID(), u, r.documentURL(), a)
	request.ContentPolicyDirective = "connect-src"
	request.ContentPolicy = r.contentPolicy()
	request.ContentPolicyURL = r.documentURL()
	r.applyClientHints(&request)
	loadContext, cancel := context.WithCancel(r.resourceContext)
	if requestID != "" {
		if r.fetchCancels == nil {
			r.fetchCancels = make(map[string]context.CancelFunc)
		}
		r.fetchCancels[requestID] = cancel
	}
	r.scheduler.Post(scheduler.Network, 0, func(context.Context) error {
		if r.resourceContext.Err() != nil {
			cancel()
			return nil
		}
		r.resourceWG.Add(1)
		go func() {
			defer r.resourceWG.Done()
			res, loadErr := r.loadResource(loadContext, request)
			cancel()
			if r.resourceContext.Err() != nil {
				return
			}
			r.scheduler.Post(scheduler.Network, 0, func(ctx context.Context) error {
				delete(r.fetchCancels, requestID)
				if loadErr != nil {
					return promise.Reject(loadErr.Error())
				}
				r.notifyPerformanceObservers(ctx)
				return promise.Resolve(fetchResponse(res))
			})
		}()
		return nil
	})
	return promise.Value, nil
}
func (r *Realm) hostXHR(_ engine.Value, a []engine.Value) (engine.Value, error) {
	if len(a) < 5 {
		return nil, fmt.Errorf("invalid XHR")
	}
	callback := a[0]
	requestID := strarg(a, 9)
	u, err := r.resolveDocument(strarg(a, 2))
	if err != nil {
		return nil, err
	}
	headers := headerMap(arg(a, 3))
	authorHeaderOrder := stringSlice(arg(a, 6))
	body := []byte(strarg(a, 4))
	hasBody, _ := arg(a, 8).(bool)
	if method := strarg(a, 1); method == "GET" || method == "HEAD" {
		body, hasBody = nil, false
	}
	if hasBody {
		headers.Set("Content-Type", xhrStringContentType(headers.Get("Content-Type")))
	}
	timeout := time.Duration(numarg(a, 5)) * time.Millisecond
	request := network.Request{ContextID: r.agent.ContextID(), URL: u, Referrer: r.documentURL(), SourceURL: r.documentURL(), Method: strarg(a, 1), Headers: headers, AuthorHeaderOrder: authorHeaderOrder, Body: body, Initiator: network.XHR, Mode: "cors", Credentials: "same-origin"}
	request.ContentPolicyDirective = "connect-src"
	request.ContentPolicy = r.contentPolicy()
	request.ContentPolicyURL = r.documentURL()
	request.CORSPreflightRequired, _ = arg(a, 10).(bool)
	if value, ok := arg(a, 7).(bool); ok && value {
		request.Credentials = "include"
	}
	r.applyClientHints(&request)
	loadContext, cancelLoad := context.WithCancel(r.resourceContext)
	if requestID != "" {
		if r.xhrCancels == nil {
			r.xhrCancels = make(map[string]context.CancelFunc)
		}
		r.xhrCancels[requestID] = cancelLoad
	}
	r.scheduler.Post(scheduler.Network, 0, func(context.Context) error {
		if r.resourceContext.Err() != nil {
			cancelLoad()
			return nil
		}
		r.resourceWG.Add(1)
		go func() {
			defer r.resourceWG.Done()
			requestContext := loadContext
			cancelTimeout := func() {}
			if timeout > 0 {
				requestContext, cancelTimeout = context.WithTimeout(loadContext, timeout)
			}
			defer cancelLoad()
			defer cancelTimeout()
			res, loadErr := r.loadResource(requestContext, request)
			if r.resourceContext.Err() != nil {
				return
			}
			r.scheduler.Post(scheduler.Network, 0, func(ctx context.Context) error {
				delete(r.xhrCancels, requestID)
				if loadErr != nil {
					event := "error"
					if errors.Is(loadErr, context.DeadlineExceeded) {
						event = "timeout"
					} else if errors.Is(loadErr, network.ErrDocumentLoadingStopped) {
						event = "abort"
					}
					_, _ = r.runtime.Call(ctx, callback, nil, r.runtime.Value(map[string]any{"error": event}))
					return nil
				}
				r.notifyPerformanceObservers(ctx)
				responseHeaders := make(map[string]string, len(res.Headers))
				for name, values := range res.Headers {
					responseHeaders[strings.ToLower(name)] = strings.Join(values, ", ")
				}
				responseURL := *res.URL
				responseURL.Fragment, responseURL.RawFragment = "", ""
				_, _ = r.runtime.Call(ctx, callback, nil, r.runtime.Value(map[string]any{"status": res.Status, "statusText": http.StatusText(res.Status), "responseURL": responseURL.String(), "responseHeaders": responseHeaders, "responseText": string(res.Body), "responseBytes": engine.BinaryBuffer(res.Body)}))
				return nil
			})
		}()
		return nil
	})
	return nil, nil
}
func (r *Realm) hostAppend(_ engine.Value, a []engine.Value) (engine.Value, error) {
	return r.hostInsertArgs(a, false)
}
func (r *Realm) hostInsert(_ engine.Value, a []engine.Value) (engine.Value, error) {
	return r.hostInsertArgs(a, true)
}
func (r *Realm) hostInsertArgs(a []engine.Value, hasBefore bool) (engine.Value, error) {
	if len(a) < 2 {
		return nil, nil
	}
	m, ok := arg(a, 1).(map[string]any)
	if !ok {
		return nil, nil
	}
	parentID := int64(numarg(a, 0))
	childID := int64Number(m["nodeId"])
	beforeID, callbackOffset := int64(0), 2
	if hasBefore {
		if before, ok := arg(a, 2).(map[string]any); ok {
			beforeID = int64Number(before["nodeId"])
		}
		callbackOffset = 3
	}
	if err := r.document.InsertNode(parentID, childID, beforeID); err != nil {
		return nil, err
	}
	delete(r.detached, childID)
	r.refreshContentPolicy()
	loadCallback, errorCallback := engine.Value(nil), engine.Value(nil)
	if len(a) > callbackOffset {
		loadCallback = a[callbackOffset]
	}
	if len(a) > callbackOffset+1 {
		errorCallback = a[callbackOffset+1]
	}
	return r.prepareConnectedResource(childID, loadCallback, errorCallback)
}

// Preparation consumes canonical node state after a mutation. Source setters,
// Text-node insertion and nested script insertion must share the same check.
func (r *Realm) prepareConnectedResource(childID int64, loadCallback, errorCallback engine.Value) (engine.Value, error) {
	if !r.document.IsConnected(childID) {
		return nil, nil
	}
	node, exists := r.document.Get(childID)
	if !exists {
		return nil, nil
	}
	tag := node.TagName
	attrs := make(map[string]any, len(node.Attributes))
	for key, value := range node.Attributes {
		attrs[key] = value
	}
	src := fmt.Sprint(attrs["src"])
	if tag == "LINK" {
		src = fmt.Sprint(attrs["href"])
	}
	code := r.document.TextContent(childID)
	if tag == "SCRIPT" {
		if r.document.ScriptStarted(childID) || scriptExecutionKind(node.Attributes["type"], node.Attributes["language"]) == "" {
			return nil, nil
		}
		if (src == "" || src == "<nil>") && code == "" {
			return nil, nil
		}
		r.document.MarkScriptStarted(childID)
		if (src == "" || src == "<nil>") && node.ScriptText != code && r.contentPolicy().TrustedTypes().Required {
			converted, err := r.runtime.Call(context.Background(), r.trustedTypesEnforcer, nil, r.val(code), r.val("TrustedScript"), r.val("HTMLScriptElement text"), r.val(""))
			if err != nil {
				r.agent.Page().trace.Add(trace.CSP, "trustedScriptBlocked", map[string]any{"realm": r.ID, "nodeId": childID, "error": err.Error()})
				return nil, nil
			}
			code = converted.String()
		}
	}
	nonce := node.Nonce
	fire := func(ctx context.Context, callback engine.Value) error {
		if callback == nil || callback.String() == "undefined" {
			return nil
		}
		_, err := r.runtime.Call(ctx, callback, r.runtime.Get("window"))
		return err
	}
	if tag == "AUDIO" || tag == "VIDEO" {
		r.updateMedia(childID, false)
		return nil, nil
	}
	if tag == "SOURCE" {
		if parent, ok := r.document.Get(node.Parent); ok && (parent.TagName == "AUDIO" || parent.TagName == "VIDEO") {
			r.updateMedia(parent.ID, false)
		}
		return nil, nil
	}
	if tag != "SCRIPT" && tag != "IMG" && tag != "LINK" {
		if tag == "IFRAME" {
			_, err := r.ensureChildFrame(childID)
			if err != nil {
				return nil, err
			}
			return nil, nil
		}
		return nil, nil
	}
	if tag == "IMG" {
		r.updateImage(childID, false)
		return nil, nil
	}
	if tag == "LINK" && hasLinkRelation(node.Attributes["rel"], "preload") {
		r.preloadResource(childID, node.Attributes)
		return nil, nil
	}
	if tag == "LINK" && hasLinkRelation(node.Attributes["rel"], "modulepreload") {
		r.preloadModules()
		return nil, nil
	}
	if tag == "LINK" {
		relation := node.Attributes["rel"]
		// Metadata and unrecognized relations do not acquire an HTTP resource or
		// dispatch load/error. In particular, alternate language URLs must not
		// be fetched as stylesheets or change the document's cookie/cache state.
		if !hasLinkRelation(relation, "stylesheet") && !hasLinkRelation(relation, "icon") &&
			!hasLinkRelation(relation, "prefetch") && !hasLinkRelation(relation, "manifest") {
			if hasLinkRelation(relation, "preconnect") || hasLinkRelation(relation, "dns-prefetch") {
				r.agent.Page().trace.Add(trace.Unsupported, "linkConnectionHint", map[string]any{"realm": r.ID, "nodeId": childID, "rel": relation, "href": src})
			}
			return nil, nil
		}
	}
	blockerReason := strings.ToLower(tag) + ":" + src
	blocksLoad := r.beginLoadBlocker(blockerReason)
	r.agent.Page().trace.Add(trace.DOM, "dynamicResourceInsertion", map[string]any{"tag": tag, "src": src, "attributes": attrs, "realm": r.ID})
	// Chrome schedules low-priority image fetching behind script fetching that
	// is queued by the same task. This keeps script execution/resource timing
	// observable before a decorative image completion without blocking either
	// callback outside the browser scheduler.
	resourceSource := scheduler.DOM
	resourceDelay := time.Duration(0)
	if tag == "IMG" {
		resourceSource = scheduler.ResourceLow
		// Passive-resource fetching yields one event-loop boundary. This is a
		// scheduling distinction, not synthetic network latency: control work and
		// executable-resource starts made by the continuation can become ready
		// before the passive transport is dispatched.
		resourceDelay = time.Nanosecond
	} else if tag == "SCRIPT" {
		resourceSource = scheduler.ResourceScript
	}
	resourceTask := func(ctx context.Context, res *network.Response, loadErr error) error {
		defer func() {
			if blocksLoad {
				r.endLoadBlocker(blockerReason)
			}
		}()
		name := "dynamic-" + strings.ToLower(tag)
		if src != "" && src != "<nil>" {
			u, err := r.resolveDocument(src)
			if err != nil {
				return err
			}
			if tag == "IMG" {
				if loadErr != nil {
					return fire(ctx, errorCallback)
				}
				return fire(ctx, loadCallback)
			}
			if tag == "LINK" {
				if loadErr != nil || res != nil && (res.Status < 200 || res.Status >= 300) {
					return fire(ctx, errorCallback)
				}
				return fire(ctx, loadCallback)
			}
			if loadErr == nil {
				loadErr = scriptResponseError(*res)
			}
			if loadErr != nil {
				r.agent.Page().trace.Add(trace.Error, "dynamicScriptLoad", map[string]any{"url": u.String(), "error": loadErr.Error()})
				return fire(ctx, errorCallback)
			}
			code = string(res.Body)
			name = u.String()
		} else if tag == "SCRIPT" && !r.allowsScript(nil, true, true, nonce) {
			return nil
		}
		if tag == "SCRIPT" && code != "" {
			owner := r
			if r.mainWorld != nil {
				owner = r.mainWorld
			}
			r.agent.Page().trace.Add(trace.JS, "scriptStart", map[string]any{"url": name, "realm": owner.ID, "dynamic": true})
			err := r.runWorld(owner, func(ctx context.Context) error {
				// External scripts without a parser insertion point must not
				// implicitly replace the document through document.write. Keep
				// this on the document owner, including isolated-world insertion.
				if src != "" && src != "<nil>" {
					owner.ignoreDestructiveWrites++
					defer func() { owner.ignoreDestructiveWrites-- }()
				}
				return owner.evaluateClassicScript(ctx, code, name, childID)
			})
			if err != nil {
				r.agent.Page().trace.Add(trace.Error, "dynamicScriptExecution", map[string]any{"url": name, "error": err.Error()})
				r.agent.Page().trace.Add(trace.JS, "scriptEnd", map[string]any{"url": name, "realm": r.ID, "dynamic": true, "error": err.Error()})
				if src == "" || src == "<nil>" {
					return nil
				}
				return fire(ctx, errorCallback)
			}
			r.agent.Page().trace.Add(trace.JS, "scriptEnd", map[string]any{"url": name, "realm": r.ID, "dynamic": true})
		}
		// Inline classic scripts have no fetched resource whose completion
		// would dispatch a load event (including when execution throws).
		if tag == "SCRIPT" && (src == "" || src == "<nil>") {
			return nil
		}
		return fire(ctx, loadCallback)
	}
	if src == "" || src == "<nil>" {
		if tag == "SCRIPT" {
			return nil, resourceTask(context.Background(), nil, nil)
		}
		r.scheduler.Post(scheduler.DOM, 0, func(ctx context.Context) error {
			return resourceTask(ctx, nil, nil)
		})
		return nil, nil
	}
	u, err := r.resolveDocument(src)
	if err != nil {
		if blocksLoad {
			r.endLoadBlocker(blockerReason)
		}
		return nil, err
	}
	if tag == "SCRIPT" && !r.allowsScript(u, false, true, nonce) {
		if blocksLoad {
			r.endLoadBlocker(blockerReason)
		}
		return nil, nil
	}
	initiator := network.Script
	if tag == "IMG" {
		initiator = network.Image
	} else if tag == "LINK" {
		if hasLinkRelation(node.Attributes["rel"], "stylesheet") {
			initiator = network.Stylesheet
		} else {
			initiator = network.Other
		}
	}
	request := r.elementRequest(u, node.Attributes, initiator)
	var ordered *orderedScript
	orderOwner := r
	if r.mainWorld != nil {
		orderOwner = r.mainWorld
	}
	_, asyncAttribute := node.Attributes["async"]
	if tag == "SCRIPT" && !node.ScriptForceAsync && !asyncAttribute && scriptExecutionKind(node.Attributes["type"], node.Attributes["language"]) == "classic" {
		ordered = &orderedScript{}
		orderOwner.orderedScripts = append(orderOwner.orderedScripts, ordered)
	}
	r.scheduler.Post(resourceSource, resourceDelay, func(context.Context) error {
		r.resourceWG.Add(1)
		go func() {
			defer r.resourceWG.Done()
			res, loadErr := r.loadResource(r.resourceContext, request)
			if r.resourceContext.Err() != nil {
				return
			}
			r.scheduler.Post(scheduler.Network, 0, func(ctx context.Context) error {
				r.notifyPerformanceObservers(ctx)
				if ordered != nil {
					ordered.run = func(ctx context.Context) error { return resourceTask(ctx, &res, loadErr) }
					return orderOwner.runOrderedScripts(ctx)
				}
				return resourceTask(ctx, &res, loadErr)
			})
		}()
		return nil
	})
	return nil, nil
}

type orderedScript struct{ run func(context.Context) error }

// Fetches stay concurrent; classic scripts with force-async cleared execute in
// insertion order, including load failures. Only the Page event loop owns this queue.
func (r *Realm) runOrderedScripts(ctx context.Context) error {
	for len(r.orderedScripts) > 0 && r.orderedScripts[0].run != nil {
		entry := r.orderedScripts[0]
		r.orderedScripts[0] = nil
		r.orderedScripts = r.orderedScripts[1:]
		if err := entry.run(ctx); err != nil {
			r.agent.Page().trace.Add(trace.Error, "orderedScript", map[string]any{"error": err.Error()})
		}
	}
	return nil
}
func int64Number(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	}
	return 0
}

func performanceHeader(raw any, name string) string {
	name = strings.ToLower(name)
	switch headers := raw.(type) {
	case map[string]string:
		for key, value := range headers {
			if strings.ToLower(key) == name {
				return value
			}
		}
	case map[string]any:
		for key, value := range headers {
			if strings.ToLower(key) == name {
				return fmt.Sprint(value)
			}
		}
	}
	return ""
}

func resourceTimingAllowed(documentOrigin, resourceURL string, headers any) bool {
	if originOf(resourceURL) == documentOrigin {
		return true
	}
	for _, value := range splitHTTPList(performanceHeader(headers, "timing-allow-origin"), ',') {
		if value == "*" || value == documentOrigin {
			return true
		}
	}
	return false
}

func splitHTTPList(value string, separator rune) []string {
	var parts []string
	start := 0
	quoted := false
	escaped := false
	for index, character := range value {
		if escaped {
			escaped = false
			continue
		}
		if quoted && character == '\\' {
			escaped = true
			continue
		}
		if character == '"' {
			quoted = !quoted
			continue
		}
		if character == separator && !quoted {
			parts = append(parts, strings.TrimSpace(value[start:index]))
			start = index + len(string(character))
		}
	}
	parts = append(parts, strings.TrimSpace(value[start:]))
	return parts
}

func unquoteHTTPValue(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		if unquoted, err := strconv.Unquote(value); err == nil {
			return unquoted
		}
	}
	return value
}

func performanceServerTiming(headers any) []map[string]any {
	result := []map[string]any{}
	for _, metricValue := range splitHTTPList(performanceHeader(headers, "server-timing"), ',') {
		parts := splitHTTPList(metricValue, ';')
		if len(parts) == 0 || parts[0] == "" {
			continue
		}
		metric := map[string]any{"name": parts[0], "duration": 0.0, "description": ""}
		seen := map[string]bool{}
		for _, parameter := range parts[1:] {
			key, value, found := strings.Cut(parameter, "=")
			if !found {
				continue
			}
			key = strings.ToLower(strings.TrimSpace(key))
			if seen[key] {
				continue
			}
			seen[key] = true
			switch key {
			case "dur":
				if duration, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
					metric["duration"] = duration
				}
			case "desc":
				metric["description"] = unquoteHTTPValue(value)
			}
		}
		result = append(result, metric)
	}
	return result
}

func byteSlice(value any) []byte {
	switch values := value.(type) {
	case []byte:
		return append([]byte(nil), values...)
	case []any:
		out := make([]byte, len(values))
		for index, value := range values {
			out[index] = byte(numargValue(value))
		}
		return out
	default:
		return []byte(fmt.Sprint(value))
	}
}

func stringSlice(value any) []string {
	values, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, fmt.Sprint(value))
	}
	return out
}

func numargValue(value any) int64 {
	switch number := value.(type) {
	case int64:
		return number
	case int32:
		return int64(number)
	case int:
		return int64(number)
	case float64:
		return int64(number)
	case float32:
		return int64(number)
	default:
		return 0
	}
}
func addStorageHosts(r *Realm, h map[string]any) {
	p := r.agent.Page()
	origin := r.origin
	storageFunction := func(fn engine.Function) engine.Function {
		return func(this engine.Value, args []engine.Value) (engine.Value, error) {
			p.ctx.storageMu.Lock()
			defer p.ctx.storageMu.Unlock()
			return fn(this, args)
		}
	}
	store := func(area string) map[string]string {
		if area == "session" {
			if p.sessionStorage[origin] == nil {
				p.sessionStorage[origin] = map[string]string{}
			}
			return p.sessionStorage[origin]
		}
		return p.ctx.store(origin)
	}
	h["storageLength"] = r.fn(storageFunction(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		return r.val(len(store(strarg(a, 0)))), nil
	}))
	h["storageKey"] = r.fn(storageFunction(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		s := store(strarg(a, 0))
		keys := make([]string, 0, len(s))
		for k := range s {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		i := int(numarg(a, 1))
		if i < 0 || i >= len(keys) {
			return r.val(nil), nil
		}
		return r.val(keys[i]), nil
	}))
	h["storageGet"] = r.fn(storageFunction(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		v, ok := store(strarg(a, 0))[strarg(a, 1)]
		if !ok {
			return r.val(nil), nil
		}
		return r.val(v), nil
	}))
	h["storageSet"] = r.fn(storageFunction(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		store(strarg(a, 0))[strarg(a, 1)] = strarg(a, 2)
		return nil, nil
	}))
	h["storageRemove"] = r.fn(storageFunction(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		delete(store(strarg(a, 0)), strarg(a, 1))
		return nil, nil
	}))
	h["storageClear"] = r.fn(storageFunction(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		s := store(strarg(a, 0))
		for k := range s {
			delete(s, k)
		}
		return nil, nil
	}))
}
