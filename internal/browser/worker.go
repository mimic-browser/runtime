package browser

import (
	"context"
	cryptorand "crypto/rand"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/moreveal/mimic/internal/csp"
	"github.com/moreveal/mimic/internal/engine"
	"github.com/moreveal/mimic/internal/monotime"
	"github.com/moreveal/mimic/internal/network"
	"github.com/moreveal/mimic/internal/scheduler"
	"github.com/moreveal/mimic/internal/state"
	"github.com/moreveal/mimic/internal/textmetrics"
	"github.com/moreveal/mimic/internal/trace"
	"github.com/moreveal/mimic/internal/webapi"
)

// DedicatedWorker owns an independent ECMAScript runtime and browser scheduler.
// Cross-agent callbacks are always posted as tasks; neither runtime calls into the
// other directly while executing observable JavaScript.
type DedicatedWorker struct {
	policy              csp.PolicySet
	mu                  sync.Mutex
	id                  int64
	parent              *Realm
	runtime             engine.Runtime
	scheduler           *scheduler.Scheduler
	deliverCallback     engine.Value
	errorCallback       engine.Value
	messageReceiver     engine.Value
	url                 *url.URL
	topLevelURL         *url.URL
	cookieContext       network.CookieContext
	securityURL         *url.URL // inherited creator URL for blob workers; independent of URL base
	closed              bool
	started             bool
	pending             []any
	cancel              context.CancelFunc
	done                chan struct{}
	wake                chan struct{}
	performanceOrigin   time.Time
	performanceIsolated bool
	performance         *performanceTimeline
	performanceCursor   uint64
	fetchCancels        map[string]context.CancelFunc // worker task/host callbacks only
	fetchWG             sync.WaitGroup
	timers              map[uint64]*workerTimer // worker task/host callbacks only
}

func (r *Realm) hostCreateWorker(_ engine.Value, args []engine.Value) (engine.Value, error) {
	if len(args) < 3 {
		return nil, fmt.Errorf("worker callbacks and URL are required")
	}
	r.workerSeq++
	id := r.workerSeq
	workerURL, err := r.resolveDocument(strarg(args, 2))
	if err != nil {
		return nil, err
	}
	w := &DedicatedWorker{id: id, parent: r, deliverCallback: args[0], errorCallback: args[1], url: workerURL, securityURL: r.documentURL(), topLevelURL: r.requestTopLevelURL(), done: make(chan struct{}), wake: make(chan struct{}, 1), performanceOrigin: r.scheduler.Now()}
	if workerURL.Scheme == "blob" || workerURL.Scheme == "data" {
		w.policy = append(csp.PolicySet(nil), r.contentPolicy()...)
	}
	w.performanceIsolated = r.securityState().crossOriginIsolated
	w.cookieContext = r.cookieContext()
	r.workers[id] = w
	source := strarg(args, 3)
	r.agent.Page().trace.Add(trace.Lifecycle, "workerCreated", map[string]any{"url": workerURL.String(), "worker": id, "realm": r.ID})
	w.Start(source)
	return r.val(id), nil
}

// Start launches the worker agent. JavaScript is executed only by the worker's
// own scheduler runner; the creating Window agent never calls into this runtime.
func (w *DedicatedWorker) Start(source string) {
	ctx, cancel := context.WithCancel(w.parent.resourceContext)
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		cancel()
		return
	}
	w.cancel = cancel
	w.mu.Unlock()
	// Worker startup is a browser task, not synchronous constructor work. This
	// deterministic boundary lets terminate() in the creating turn cancel the
	// fetch/realm/script before any of them become observable.
	w.parent.scheduler.Post(scheduler.DOM, 0, func(context.Context) error {
		w.mu.Lock()
		if ctx.Err() != nil || w.closed {
			w.mu.Unlock()
			return nil
		}
		w.started = true
		w.mu.Unlock()
		go w.run(ctx, source)
		return nil
	})
}

func (w *DedicatedWorker) run(ctx context.Context, source string) {
	defer close(w.done)
	if ctx.Err() != nil || w.isClosed() {
		return
	}
	p := w.parent.agent.Page()
	runtime := p.ctx.browser.factory.New()
	var workerScheduler *scheduler.Scheduler
	var nativePollQueued bool // Owned by the worker event loop.
	workerScheduler = scheduler.New(w.performanceOrigin, func(ctx context.Context) error {
		var err error
		if checkpoint, ok := runtime.(interface{ MicrotaskCheckpointContext(context.Context) error }); ok {
			err = checkpoint.MicrotaskCheckpointContext(ctx)
		} else {
			err = runtime.MicrotaskCheckpoint()
		}
		if err != nil {
			return err
		}
		// Native compilation posts foreground work outside the Promise queue.
		// An otherwise idle worker must service it without waiting for an
		// unrelated message or timer, just like a Window realm does.
		if native, ok := runtime.(interface{ NativeTasksPending() bool }); ok && native.NativeTasksPending() && !nativePollQueued {
			nativePollQueued = true
			workerScheduler.Post(scheduler.Control, time.Millisecond, func(context.Context) error {
				nativePollQueued = false
				return nil // Service native work at this task's checkpoint.
			})
		}
		return nil
	})
	workerScheduler.SetExecutionScale(p.environmentView().Time.ExecutionScale)
	w.mu.Lock()
	w.runtime = runtime
	w.scheduler = workerScheduler
	w.mu.Unlock()
	var workerFonts *textmetrics.Engine
	defer func() {
		w.stopFetches()
		p.ctx.network.RevokeBlobsForOwner(fmt.Sprintf("%s/worker/%d", w.parent.ID, w.id))
		workerScheduler.Close()
		clear(w.timers)
		if workerFonts != nil {
			_ = workerFonts.Close()
		}
		_ = runtime.Close()
		// A terminated Worker object can remain reachable from its creating
		// document. Do not retain its abandoned task queue or response bodies.
		w.mu.Lock()
		w.closed = true
		w.runtime, w.scheduler, w.messageReceiver = nil, nil, nil
		w.pending = nil
		w.performance = nil
		w.mu.Unlock()
	}()
	workerScheduler.SetObserver(func(t scheduler.Transition) {
		p.trace.Add(trace.Scheduler, t.Name, map[string]any{"taskId": t.TaskID, "source": t.Source, "due": t.Due, "worker": w.id})
	})
	runtime.SetTimeSource(workerScheduler.Now)
	runtime.SetGlobalAccessObserver(func(name string, supported bool) {
		p.trace.Add(trace.API, "propertyAccess", map[string]any{"property": "DedicatedWorkerGlobalScope." + name, "supported": supported, "worker": w.id})
		if !supported {
			p.trace.Add(trace.Unsupported, "DedicatedWorkerGlobalScope."+name, map[string]any{"worker": w.id, "access": "property"})
		}
	})
	host := map[string]any{}
	if native, ok := runtime.(engine.ExceptionStateRuntime); ok {
		host["initializeExceptionState"] = native.ExceptionStateFactory()
	}
	if native, ok := runtime.(engine.ReceiverDispatchRuntime); ok {
		host["createReceiverDispatch"] = native.ReceiverDispatchFactory()
	}
	installLocaleHost(host, runtime, func() state.Locale { return p.environmentView().Locale })
	installStructuredCloneHost(host, runtime)
	files := installOPFSHost(host, runtime, p.ctx, func() string {
		u := w.url
		if u.Scheme == "blob" {
			return w.parent.origin
		}
		return originOf(u.String())
	})
	defer files.close()
	installConsoleKind(host, runtime)
	host["console"] = runtime.Function(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		p.trace.Add(trace.Console, strarg(args, 0), map[string]any{"args": arg(args, 1), "worker": w.id})
		return nil, nil
	})
	installExceptionDescription(host, runtime)
	host["reportUnhandledException"] = transientRuntimeFunction(runtime, func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		return nil, w.reportError(fmt.Errorf("%s", strarg(args, 0)))
	})
	installFontResourceHosts(host, runtime, func() *textmetrics.Engine {
		if workerFonts == nil {
			workerFonts = newTextMetricsEngine(p.environmentView().Fonts)
		}
		return workerFonts
	})
	installURLHost(host, runtime, func() *url.URL { return w.url })
	installWebCryptoHost(host, runtime)
	installTextDecoderHost(host, runtime)
	host["createObjectURL"] = runtime.Function(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		source := w.url
		if source.Scheme == "blob" {
			source = w.securityURL
		}
		raw := "blob:" + originOf(source.String()) + "/" + uuid.NewString()
		p.ctx.network.PutBlob(raw, byteSlice(arg(args, 0)), strarg(args, 1), fmt.Sprintf("%s/worker/%d", w.parent.ID, w.id))
		return runtime.Value(raw), nil
	})
	host["revokeObjectURL"] = runtime.Function(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		p.ctx.network.RevokeBlob(strarg(args, 0))
		return nil, nil
	})
	w.installFetch(host, ctx)
	workerToken := uuid.NewString()
	host["token"] = runtime.Function(func(engine.Value, []engine.Value) (engine.Value, error) {
		return runtime.Value(workerToken), nil
	})
	host["postMessage"] = transientRuntimeFunction(runtime, func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		w.deliverToParent(arg(args, 0), strarg(args, 1))
		return nil, nil
	})
	host["setTimer"] = transientRuntimeFunction(runtime, w.hostTimer)
	host["clearTimer"] = transientRuntimeFunction(runtime, w.hostClearTimer)
	host["close"] = runtime.Function(func(engine.Value, []engine.Value) (engine.Value, error) {
		w.markClosed()
		return nil, nil
	})
	host["semanticMissing"] = runtime.Function(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		recordSemanticBoundary(p.trace, "worker", w.id, "legacy-host", args)
		return nil, nil
	})
	host["semanticMissingAt"] = runtime.Function(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		recordSemanticBoundary(p.trace, "worker", w.id, strarg(args, 0), args[1:])
		return nil, nil
	})
	host["workerStorageState"] = runtime.Function(func(engine.Value, []engine.Value) (engine.Value, error) {
		origin := w.parent.origin
		if w.url.Scheme != "blob" {
			origin = originOf(w.url.String())
		}
		p.ctx.mu.Lock()
		persisted := p.ctx.originCapabilities(origin).permissions["persistent-storage"] == "granted"
		p.ctx.mu.Unlock()
		return runtime.Value(map[string]any{"opaque": origin == "null", "persisted": persisted, "estimate": map[string]any{"quota": p.environmentView().Capabilities.StorageQuotaBytes, "usage": 0, "usageDetails": map[string]any{}}}), nil
	})
	host["navigator"] = runtime.Function(func(engine.Value, []engine.Value) (engine.Value, error) {
		return runtime.Value(p.environmentView().NavigatorProjection(p.NetworkPolicy().Offline(), false)), nil
	})
	if detacher, ok := runtime.(engine.ArrayBufferDetacher); ok {
		host["detachArrayBuffer"] = runtime.Function(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("expected ArrayBuffer")
			}
			return nil, detacher.DetachArrayBuffer(args[0])
		})
	}
	host["systemColors"] = runtime.Function(func(engine.Value, []engine.Value) (engine.Value, error) {
		return runtime.Value(p.environmentView().SystemColorPalette()), nil
	})
	host["gpuCapabilities"] = runtime.Function(func(engine.Value, []engine.Value) (engine.Value, error) {
		return runtime.Value(p.environmentView().Graphics.WebGPUProjection()), nil
	})
	host["gpuRequestAdapter"] = runtime.Function(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		promise := newHostPromise(runtime)
		delay := time.Duration(p.environmentView().Graphics.WebGPU.InitializationDelayMillis * float64(time.Millisecond))
		preference, fallback := strarg(args, 0), strarg(args, 1) == "fallback"
		workerScheduler.Post(scheduler.Control, delay, func(context.Context) error {
			g := p.environmentView().Graphics
			adapter, ok := g.SelectWebGPUAdapter(preference, fallback)
			if !ok {
				return promise.Resolve(nil)
			}
			return promise.Resolve(adapter)
		})
		return promise.Value, nil
	})
	installTextureDecoder(host, runtime)
	host["graphics"] = runtime.Function(func(engine.Value, []engine.Value) (engine.Value, error) {
		g := p.environmentView().Graphics
		return runtime.Value(map[string]any{"vendor": g.Vendor, "renderer": g.Renderer, "maxTextureSize": g.MaxTextureSize, "capabilitiesJSON": g.WebGLCapabilities()}), nil
	})
	host["location"] = runtime.Function(func(engine.Value, []engine.Value) (engine.Value, error) {
		href := w.url.String()
		pathname := w.url.EscapedPath()
		origin := "null"
		if w.url.Scheme == "blob" {
			inner := w.url.Opaque
			if inner == "" {
				inner = strings.TrimPrefix(href, "blob:")
			}
			pathname = inner
			if innerURL, err := url.Parse(inner); err == nil && (innerURL.Scheme == "http" || innerURL.Scheme == "https") {
				origin = innerURL.Scheme + "://" + innerURL.Host
			}
		} else if w.url.Scheme == "http" || w.url.Scheme == "https" {
			origin = w.url.Scheme + "://" + w.url.Host
		}
		return runtime.Value(map[string]any{"href": href, "origin": origin, "protocol": w.url.Scheme + ":", "host": w.url.Host, "hostname": w.url.Hostname(), "port": w.url.Port(), "pathname": pathname, "search": prefixed(w.url.RawQuery, "?"), "hash": prefixed(w.url.Fragment, "#")}), nil
	})
	host["importScripts"] = runtime.Function(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		for _, raw := range stringSlice(arg(args, 0)) {
			target, err := w.url.Parse(raw)
			if err != nil {
				return runtime.Value(map[string]any{"name": "SyntaxError", "message": "Invalid script URL"}), nil
			}
			allowed, _ := w.policy.AllowsScript(w.url, target, false, false, "")
			if !allowed {
				return runtime.Value(map[string]any{"name": "NetworkError", "message": "Script blocked by Content Security Policy"}), nil
			}
			response, err := p.loader.Load(ctx, w.parent.withResourceTiming(network.Request{ContextID: w.parent.agent.ContextID(), URL: target, Referrer: w.url, SourceURL: w.securityURL, TopLevelURL: w.topLevelURL, HasCrossSiteAncestor: w.cookieContext.HasCrossSiteAncestor, Initiator: network.Worker, OmitClientHints: true}))
			if err == nil {
				err = scriptResponseError(response)
			}
			if err != nil {
				return runtime.Value(map[string]any{"name": "NetworkError", "message": err.Error()}), nil
			}
			if _, err = runtime.Eval(ctx, string(response.Body), target.String()); err != nil {
				return nil, err
			}
		}
		return nil, nil
	})
	host["trustedTypesEventAttributes"] = runtime.Function(func(engine.Value, []engine.Value) (engine.Value, error) {
		return runtime.Value(p.Compatibility().Surface().TrustedTypeEventAttributes), nil
	})
	host["trustedTypesPolicy"] = runtime.Function(func(engine.Value, []engine.Value) (engine.Value, error) {
		return runtime.Value(w.policy.TrustedTypes().Projection()), nil
	})
	host["isSecureContext"] = runtime.Function(func(engine.Value, []engine.Value) (engine.Value, error) {
		return runtime.Value(w.isSecureContext()), nil
	})
	host["randomBytes"] = runtime.Function(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		n := int(numarg(args, 0))
		if n < 0 || n > 65536 {
			return nil, fmt.Errorf("Crypto.getRandomValues length must be between 0 and 65536 bytes")
		}
		buffer := make([]byte, n)
		if _, err := cryptorand.Read(buffer); err != nil {
			return nil, err
		}
		out := make([]int, n)
		for index, value := range buffer {
			out[index] = int(value)
		}
		p.trace.Add(trace.API, "Crypto.getRandomValues", map[string]any{"bytes": n, "worker": w.id})
		return runtime.Value(out), nil
	})
	host["randomUUID"] = runtime.Function(func(engine.Value, []engine.Value) (engine.Value, error) {
		p.trace.Add(trace.API, "Crypto.randomUUID", map[string]any{"worker": w.id})
		return runtime.Value(uuid.NewString()), nil
	})
	host["performanceNow"] = runtime.Function(func(engine.Value, []engine.Value) (engine.Value, error) {
		return runtime.Value(p.performanceClamper.now(workerScheduler.Now(), w.performanceOrigin, w.performanceIsolated)), nil
	})
	host["performanceTimeOrigin"] = runtime.Function(func(engine.Value, []engine.Value) (engine.Value, error) {
		return runtime.Value(float64(p.performanceClamper.micros(w.performanceOrigin.UnixMicro(), w.performanceIsolated)) / 1000), nil
	})
	w.performance = newPerformanceTimeline(runtime, workerScheduler, func() float64 {
		return p.performanceClamper.now(workerScheduler.Now(), w.performanceOrigin, w.performanceIsolated)
	}, func() int { return 0 }, true)
	w.performance.syncExternal = func() {
		owner := fmt.Sprintf("%s/worker/%d", w.parent.ID, w.id)
		for _, event := range p.trace.EventsSince(w.performanceCursor) {
			w.performanceCursor = event.Sequence
			if event.Kind != trace.Network || event.Name != "response" || event.Data["performanceOwner"] != owner {
				continue
			}
			status := numberValue(event.Data["status"])
			if status >= 300 && status < 400 && status != 304 {
				continue
			}
			entry := performanceResourceEntry(event.Data, w.performanceOrigin, originOf(w.securityURL.String()), p.environmentView().Time.NetworkScale, p.performanceClamper, w.performanceIsolated)
			if entry != nil {
				w.performance.appendResource(entry, event.Data)
			}
		}
	}
	w.performance.install(host)
	installPerformanceClone(runtime, host)
	host["catalogJSON"] = runtime.Function(func(engine.Value, []engine.Value) (engine.Value, error) {
		catalog := ""
		if bundle := p.Compatibility(); bundle != nil && bundle.Surface() != nil {
			catalog = bundle.Surface().GeneratedCatalogJSON
		}
		return runtime.Value(catalog), nil
	})
	if err := runtime.Set("__workerHost", host); err != nil {
		_ = w.reportError(err)
		return
	}
	if err := runtime.Set("__mimic", host); err != nil {
		_ = w.reportError(err)
		return
	}
	if err := runtime.Set("__mimicIDLExposure", "DedicatedWorker"); err != nil {
		_ = w.reportError(err)
		return
	}
	bootstrap := ""
	if bundle := p.Compatibility(); bundle != nil && bundle.Surface() != nil {
		key := "worker.insecure.non-isolated"
		if w.isSecureContext() {
			key = "worker.secure.non-isolated"
		}
		bootstrap = webapi.WorkerSurfaceFor(bundle.Surface(), key)
	}
	if bootstrap == "" {
		bootstrap = webapi.WorkerSurface("", nil)
	}
	// Native Worker globals already exist when the initial script job is
	// queued. Our generated bindings are the implementation of those globals,
	// not page work, so install them while establishing the agent. Otherwise a
	// fast parent-side terminate() can cancel hundreds of milliseconds of host
	// binding setup and prevent even tiny Blob worker scripts from starting.
	p.trace.Add(trace.JS, "workerBootstrapStart", map[string]any{"url": w.url.String(), "worker": w.id})
	var bootstrapErr error
	if installer, ok := runtime.(engine.BootstrapRuntime); ok {
		_, bootstrapErr = installer.EvalBootstrap(ctx, bootstrap, "mimic:worker-surface")
	} else {
		_, bootstrapErr = runtime.Eval(ctx, bootstrap, "mimic:worker-surface")
	}
	if err := bootstrapErr; err != nil {
		p.trace.Add(trace.Error, "workerBootstrap", map[string]any{"url": w.url.String(), "worker": w.id, "error": err.Error()})
		_ = w.reportError(err)
		return
	}
	if err := installNativeFunctionSource(runtime); err != nil {
		_ = w.reportError(err)
		return
	}
	if err := installEvalSourceResolver(runtime); err != nil {
		_ = w.reportError(err)
		return
	}
	w.mu.Lock()
	w.messageReceiver = runtime.Get("__deliver")
	w.mu.Unlock()
	if _, err := runtime.Eval(ctx, `delete globalThis.__deliver;delete globalThis.__workerHost;delete globalThis.__mimicEvalSourceResolver`, "mimic:hide-worker-internals"); err != nil {
		_ = w.reportError(err)
		return
	}
	p.trace.Add(trace.JS, "workerBootstrapEnd", map[string]any{"url": w.url.String(), "worker": w.id})
	workerScheduler.Post(scheduler.DOM, 0, func(taskContext context.Context) error {
		// Worker lifecycle is not a Page lifecycle event in CDP. Keep it in the
		// structured JS trace so protocol subscribers do not misroute it.
		p.trace.Add(trace.JS, "workerStart", map[string]any{"url": w.url.String(), "worker": w.id})
		if source == "" {
			res, loadErr := p.loader.Load(taskContext, w.parent.withResourceTiming(network.Request{ContextID: w.parent.agent.ContextID(), URL: w.url, Referrer: w.securityURL, SourceURL: w.securityURL, TopLevelURL: w.topLevelURL, HasCrossSiteAncestor: w.cookieContext.HasCrossSiteAncestor, Initiator: network.Worker, OmitClientHints: true}))
			if loadErr != nil {
				return loadErr
			}
			source = string(res.Body)
			if w.url.Scheme != "blob" && w.url.Scheme != "data" {
				w.policy = parseResponseCSP(res.Headers)
			}
			if res.URL != nil {
				w.url = res.URL
			}
		}
		p.trace.Add(trace.JS, "scriptStart", map[string]any{"url": w.url.String(), "worker": w.id, "source": source})
		_, err := runtime.Eval(taskContext, source, w.url.String())
		p.trace.Add(trace.JS, "scriptEnd", map[string]any{"url": w.url.String(), "worker": w.id, "error": errorString(err)})
		if err != nil {
			return err
		}
		w.mu.Lock()
		pending := append([]any(nil), w.pending...)
		w.pending = nil
		w.mu.Unlock()
		for _, data := range pending {
			w.postMessageReady(data)
		}
		return nil
	})
	last := monotime.Now()
	ticker := time.NewTicker(2 * time.Millisecond)
	defer ticker.Stop()
	for {
		now := monotime.Now()
		workerScheduler.AdvanceBy(now.Sub(last))
		last = now
		if err := workerScheduler.RunReady(ctx, 10000); err != nil && ctx.Err() == nil {
			_ = w.reportError(err)
		}
		if w.isClosed() {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-w.wake:
		case <-ticker.C:
		}
	}
}

func (w *DedicatedWorker) PostMessage(data any, traceJSON string) {
	w.parent.agent.Page().trace.Add(trace.Scheduler, "workerMessageQueued", serializedWorkerMessageTrace(w.id, "parent-to-worker", data, traceJSON))
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	if w.runtime == nil {
		w.pending = append(w.pending, data)
		w.mu.Unlock()
		return
	}
	w.mu.Unlock()
	w.postMessageReady(data)
}

func (w *DedicatedWorker) postMessageReady(data any) {
	w.mu.Lock()
	runtime, workerScheduler := w.runtime, w.scheduler
	w.mu.Unlock()
	if runtime == nil || workerScheduler == nil {
		return
	}
	workerScheduler.Post(scheduler.DOM, 0, func(ctx context.Context) error {
		w.parent.agent.Page().trace.Add(trace.Scheduler, "workerMessageDispatch", map[string]any{"direction": "parent-to-worker", "worker": w.id})
		w.mu.Lock()
		fn := w.messageReceiver
		w.mu.Unlock()
		invoke := func(ctx context.Context) error {
			receiver, argument := runtime.Get("self"), runtime.Value(data)
			result, err := runtime.Call(ctx, fn, receiver, argument)
			releaseRuntimeValues(runtime, result, receiver, argument)
			return err
		}
		var err error
		if owner, ok := runtime.(engine.OwnerRuntime); ok {
			err = owner.RunOnOwner(ctx, invoke)
		} else {
			err = invoke(ctx)
		}
		if err != nil {
			w.parent.agent.Page().trace.Add(trace.Error, "workerMessageDispatch", map[string]any{"direction": "parent-to-worker", "worker": w.id, "error": err.Error()})
		}
		return err
	})
	w.signal()
}

func (w *DedicatedWorker) deliverToParent(data any, traceJSON string) {
	if w.isClosed() {
		return
	}
	w.parent.agent.Page().trace.Add(trace.Scheduler, "workerMessageQueued", serializedWorkerMessageTrace(w.id, "worker-to-parent", data, traceJSON))
	w.parent.scheduler.Post(scheduler.DOM, 0, func(ctx context.Context) error {
		w.parent.agent.Page().trace.Add(trace.Scheduler, "workerMessageDispatch", map[string]any{"direction": "worker-to-parent", "worker": w.id})
		if w.isClosed() || w.parent.resourceContext.Err() != nil {
			return nil
		}
		_, err := w.parent.runtime.Call(ctx, w.deliverCallback, nil, w.parent.runtime.Value(data))
		return err
	})
}

func (w *DedicatedWorker) reportError(cause error) error {
	message := cause.Error()
	var thrown engine.ThrownValue
	if errors.As(cause, &thrown) {
		releaseRuntimeValues(w.runtime, thrown.ThrownValue())
	}
	if w.isClosed() {
		return nil
	}
	w.parent.agent.Page().trace.Add(trace.Error, "worker", map[string]any{"url": w.url.String(), "worker": w.id, "error": message})
	w.parent.scheduler.Post(scheduler.DOM, 0, func(ctx context.Context) error {
		if w.isClosed() || w.parent.resourceContext.Err() != nil {
			return nil
		}
		runtime := w.parent.runtime
		invoke := func(ctx context.Context) error {
			argument := runtime.Value(message)
			result, err := runtime.Call(ctx, w.errorCallback, nil, argument)
			releaseRuntimeValues(runtime, result, argument)
			var thrown engine.ThrownValue
			if errors.As(err, &thrown) {
				releaseRuntimeValues(runtime, thrown.ThrownValue())
			}
			return nil // Delivery errors were already consumed by this task.
		}
		if owner, ok := runtime.(engine.OwnerRuntime); ok {
			_ = owner.RunOnOwner(ctx, invoke)
		} else {
			_ = invoke(ctx)
		}
		return nil
	})
	return nil
}

func (w *DedicatedWorker) Close() error {
	w.mu.Lock()
	w.closed = true
	cancel, started := w.cancel, w.started
	w.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if started {
		<-w.done
	}
	return nil
}

func (w *DedicatedWorker) signal() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

func (w *DedicatedWorker) isClosed() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.closed
}

func (w *DedicatedWorker) markClosed() {
	w.mu.Lock()
	w.closed = true
	w.mu.Unlock()
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func prefixed(value, prefix string) string {
	if value == "" {
		return ""
	}
	return prefix + value
}

func (w *DedicatedWorker) isSecureContext() bool {
	u := w.url
	if u.Scheme == "blob" {
		inner := w.url.Opaque
		if inner == "" {
			inner = strings.TrimPrefix(w.url.String(), "blob:")
		}
		if innerURL, err := url.Parse(inner); err == nil {
			u = innerURL
		}
	}
	return potentiallyTrustworthyURL(u)
}
