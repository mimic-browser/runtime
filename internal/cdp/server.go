package cdp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/moreveal/mimic/internal/browser"
	"github.com/moreveal/mimic/internal/dom"
	"github.com/moreveal/mimic/internal/network"
	"github.com/moreveal/mimic/internal/trace"
)

type Server struct {
	connectionOpened  func()
	certificateMu     sync.Mutex
	lifecycleMu       sync.Mutex
	connections       map[*websocket.Conn]context.CancelFunc
	clients           map[*connection]struct{}
	browserID         string
	targetObservers   map[*browser.Page]func()
	targetNavigations map[*browser.Page]string
	tabTargets        map[*browser.Page]string
	popupOpeners      map[*browser.Page]*browser.Page
	idlePages         map[*browser.Page]*pageIdle
	pumps             map[*browser.Page]context.CancelFunc
	executions        map[*browser.Page]context.CancelFunc
	pausedPumps       map[*browser.Page]int
	downloadPolicies  map[string]downloadPolicy
	closed            bool
	workers           sync.WaitGroup
	Browser           *browser.Browser
	Context           *browser.Context
	Page              *browser.Page
	http              *http.Server
	listener          net.Listener
	navigationTimeout time.Duration
}

func New(b *browser.Browser) (*Server, error) {
	c := b.NewContext()
	p, err := c.NewPage()
	if err != nil {
		return nil, err
	}
	return &Server{Browser: b, Context: c, Page: p, browserID: uuid.NewString(), clients: make(map[*connection]struct{}), connections: make(map[*websocket.Conn]context.CancelFunc), pumps: make(map[*browser.Page]context.CancelFunc), executions: make(map[*browser.Page]context.CancelFunc), targetObservers: make(map[*browser.Page]func()), targetNavigations: make(map[*browser.Page]string), popupOpeners: make(map[*browser.Page]*browser.Page)}, nil
}

// SetConnectionOpened installs a private runner observation before Serve.
// It does not intercept commands or alter their protocol semantics.
func (s *Server) SetConnectionOpened(observer func()) { s.connectionOpened = observer }
func (s *Server) SetNavigationTimeout(timeout time.Duration) {
	if timeout >= 0 {
		s.navigationTimeout = timeout
	}
}
func (s *Server) Serve(listener net.Listener) error {
	s.listener = listener
	mux := http.NewServeMux()
	if s.Browser.DevPreviewEnabled() {
		s.registerPreview(mux)
	}
	handleDiscoveryRoute(mux, "/json/version", s.version)
	handleDiscoveryRoute(mux, "/json", s.list)
	handleDiscoveryRoute(mux, "/json/list", s.list)
	mux.HandleFunc("/json/protocol", s.protocol)
	mux.HandleFunc("/json/new", s.newTarget)
	mux.HandleFunc("/json/close/", s.closeTargetHTTP)
	mux.HandleFunc("/json/activate/", s.activateTargetHTTP)
	mux.HandleFunc("/devtools/page/", s.ws)
	mux.HandleFunc("/devtools/browser/", s.ws)
	s.lifecycleMu.Lock()
	if s.closed {
		s.lifecycleMu.Unlock()
		return http.ErrServerClosed
	}
	s.http = &http.Server{Handler: mux}
	server := s.http
	s.lifecycleMu.Unlock()
	return server.Serve(listener)
}
func handleDiscoveryRoute(mux *http.ServeMux, pattern string, handler http.HandlerFunc) {
	mux.HandleFunc(pattern, handler)
	if !strings.HasSuffix(pattern, "/") {
		mux.HandleFunc(pattern+"/", handler)
	}
}
func (s *Server) Close(ctx context.Context) error {
	s.lifecycleMu.Lock()
	s.closed = true
	server := s.http
	for _, cancel := range s.pumps {
		cancel()
	}
	for _, cancel := range s.executions {
		cancel()
	}
	for conn, cancel := range s.connections {
		cancel()
		_ = conn.Close()
	}
	s.lifecycleMu.Unlock()
	var err error
	if server != nil {
		err = server.Shutdown(ctx)
	}
	for _, c := range s.Browser.Contexts() {
		c.Cancel()
	}
	s.workers.Wait()
	return errors.Join(err, s.Browser.Close())
}

func (s *Server) base(r *http.Request) string {
	return "ws://" + r.Host + "/devtools/browser/" + s.browserID
}
func (s *Server) version(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"Browser": s.Browser.String(), "Protocol-Version": "1.3", "User-Agent": s.Page.Environment().Navigator().UserAgent, "V8-Version": "virtual", "webSocketDebuggerUrl": s.base(r)})
}
func (s *Server) list(w http.ResponseWriter, r *http.Request) {
	targets := []any{}
	for _, page := range s.pages() {
		targets = append(targets, map[string]any{"id": page.ID, "type": "page", "title": page.Title(), "url": page.URL(), "webSocketDebuggerUrl": "ws://" + r.Host + "/devtools/page/" + page.ID})
	}
	writeJSON(w, targets)
}
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

var upgrader = websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}

var callFunctionSequence atomic.Uint64

type message struct {
	ID        int64           `json:"id"`
	Method    string          `json:"method"`
	Params    json.RawMessage `json:"params"`
	SessionID string          `json:"sessionId,omitempty"`
	timing    *commandTiming
}
type session struct {
	commandTimings          sync.Map   // diagnostic only: command id -> *commandTiming
	commandMu               sync.Mutex // protects binding changes against commands and asynchronous navigation
	inputOrderMu            sync.Mutex
	inputTail               <-chan struct{}
	ctx                     context.Context
	server                  *Server
	transport               *connection
	parent                  *session
	id                      string
	targetID                string
	targetType              string
	flat                    bool
	cancel                  context.CancelFunc
	stateMu                 sync.RWMutex
	attachMu                sync.Mutex
	detached                bool
	discover                bool
	autoAttach              bool
	waitForDebugger         bool
	autoFlat                bool
	targetFilter            []any
	autoFilter              []any
	domains                 map[string]bool
	lifecycleEvents         bool
	ignoreCertificateErrors bool
	browserSession          bool
	conn                    *websocket.Conn
	page                    *browser.Page
	debugger                *browser.Debugger
	bindMu                  sync.RWMutex
	interceptor             *ControlInterceptor
	unsub                   func()
	removeInterceptor       func()
	navigationTimeout       time.Duration
	contextMu               sync.Mutex
	nextContextID           int64
	contextByFrame          map[string]int64
	frameByContext          map[int64]string
	realmByFrame            map[string]string
	worldContexts           map[int64]runtimeWorldContext
}

func (s *session) bindPage(page *browser.Page) {
	s.bindMu.Lock()
	defer s.bindMu.Unlock()
	if s.unsub != nil {
		s.unsub()
	}
	if s.removeInterceptor != nil {
		s.removeInterceptor()
	}
	if s.interceptor != nil {
		s.interceptor.Close()
	}
	s.page = page
	s.contextMu.Lock()
	s.nextContextID = 0
	s.contextByFrame = map[string]int64{}
	s.frameByContext = map[int64]string{}
	s.realmByFrame = map[string]string{}
	s.contextMu.Unlock()
	s.interceptor = NewControlInterceptor(s.event)
	s.removeInterceptor = page.Loader().Use(s.interceptor)
	s.unsub = page.Trace().SubscribeKinds([]trace.Kind{trace.Console, trace.Exception, trace.Lifecycle, trace.Network}, s.traceEvent)
	s.server.ensurePump(page)
}
func (s *session) unbindPage() {
	s.bindMu.Lock()
	defer s.bindMu.Unlock()
	// Release paused requests before waiting for their owning Page turn.
	if s.interceptor != nil {
		s.interceptor.Close()
	}
	if s.debugger != nil {
		s.page.LockCommands()
		s.debugger.Close()
		s.debugger = nil
		s.page.UnlockCommands()
	}
	if s.unsub != nil {
		s.unsub()
	}
	if s.removeInterceptor != nil {
		s.removeInterceptor()
	}
}

// A Page owns one clock regardless of how many debugger connections observe it.
// Its pump survives debugger disconnection and ends with the Page or server.
func (s *Server) ensurePump(page *browser.Page) {
	s.applyCertificatePolicy(page)
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.closed || s.pumps[page] != nil {
		return
	}
	if live, ok := s.page(page.ID); !ok || live != page {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.pumps[page] = cancel
	if s.idlePages == nil {
		s.idlePages = make(map[*browser.Page]*pageIdle)
	}
	state := &pageIdle{loaderID: page.LoaderID()}
	if page.LoadEventEnded() {
		state.loaded = time.Now()
	}
	s.idlePages[page] = state
	s.targetObservers[page] = page.Trace().SubscribeKinds([]trace.Kind{trace.Lifecycle}, func(e trace.Event) {
		s.observePageLifecycle(page, e)
	})
	s.workers.Add(1)
	go func() { defer s.workers.Done(); s.pumpEventLoop(ctx, page) }()
}
func (s *Server) stopPump(page *browser.Page) {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	delete(s.targetNavigations, page)
	delete(s.idlePages, page)
	if unsub := s.targetObservers[page]; unsub != nil {
		unsub()
		delete(s.targetObservers, page)
	}
	if cancel := s.executions[page]; cancel != nil {
		cancel()
	}
	if cancel := s.pumps[page]; cancel != nil {
		cancel()
		delete(s.pumps, page)
	}
}
func (s *Server) pumpEventLoop(lifetime context.Context, page *browser.Page) {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	s.pumpEventLoopWithTicks(lifetime, page, ticker.C)
}

func (s *Server) pumpEventLoopWithTicks(lifetime context.Context, page *browser.Page, ticks <-chan time.Time) {
	last := time.Now()
	for {
		select {
		case <-ticks:
		case <-page.EventLoopWake():
			// A post is only a readiness hint. Future tasks still wait for their
			// due time; the ticker advances time when no new work arrives.
		case <-lifetime.Done():
			return
		}
		{
			delta := time.Since(last)
			for turn := 0; turn < 32; turn++ {
				page.LockCommands()
				if lifetime.Err() != nil {
					page.UnlockCommands()
					return
				}
				turnContext, cancelTurn := context.WithCancel(lifetime)
				s.lifecycleMu.Lock()
				if s.pausedPumps[page] > 0 {
					s.lifecycleMu.Unlock()
					cancelTurn()
					page.UnlockCommands()
					break
				}
				s.executions[page] = cancelTurn
				s.lifecycleMu.Unlock()
				// One debugger pump turn must not monopolize the Page when an
				// application continuously posts ready timers/network callbacks.
				// Background JavaScript has the Page lifetime, not the unrelated
				// navigation timeout: terminating a valid hydration callback at 30s
				// leaves an otherwise recoverable committed document half-built.
				more, err := page.AdvanceTimeBudget(turnContext, delta, 1)
				delta = 0
				s.lifecycleMu.Lock()
				delete(s.executions, page)
				s.lifecycleMu.Unlock()
				cancelTurn()
				s.emitIdle(page)
				page.UnlockCommands()
				// Exclude time spent executing or waiting for other Page turns.
				last = time.Now()
				if more && page.ExternalCommandWaiting() {
					// A ready background task must not win another burst ahead of
					// an already queued protocol command. Its wake hint remains
					// coalesced so the pump resumes after the command completes.
					page.WakeEventLoop()
					break
				}
				if err != nil && lifetime.Err() == nil {
					page.Trace().Add(trace.Error, "scheduler", map[string]any{"error": err.Error(), "during": "CDP event-loop pump"})
				}
				if !more || err != nil {
					break
				}
			}
		}
	}
}

// Snapshot serialization owns a consistent task boundary. Prevent the pump
// from starting another task while capture waits for that boundary. Explicit
// interrupted captures may cancel the current task; ordinary captures do not.
func (s *Server) pausePump(page *browser.Page, interrupt bool) func() {
	s.lifecycleMu.Lock()
	if s.pausedPumps == nil {
		s.pausedPumps = make(map[*browser.Page]int)
	}
	s.pausedPumps[page]++
	if interrupt {
		if cancel := s.executions[page]; cancel != nil {
			cancel()
		}
	}
	s.lifecycleMu.Unlock()
	return func() {
		s.lifecycleMu.Lock()
		s.pausedPumps[page]--
		if s.pausedPumps[page] == 0 {
			delete(s.pausedPumps, page)
		}
		s.lifecycleMu.Unlock()
		// A paused pump may have consumed the post hint without running it.
		page.WakeEventLoop()
	}
}

func (s *Server) cancelExecution(page *browser.Page) bool {
	s.lifecycleMu.Lock()
	cancel := s.executions[page]
	s.lifecycleMu.Unlock()
	navigationCancelled := page.CancelNavigation()
	if cancel == nil {
		return navigationCancelled
	}
	cancel()
	return true
}
func (s *session) traceEvent(e trace.Event) {
	if s.browserSession || s.targetType == "tab" {
		return
	}
	switch e.Kind {
	case trace.Console:
		if raw, _ := e.Data["remoteValues"].(bool); raw {
			return
		}
		contextID, ok := s.contextForRealm(stringValue(e.Data["realm"]))
		if !ok {
			// A queued callback can outlive a navigated or detached realm. Chrome
			// does not expose console events for an execution context after sending
			// its destruction, and clients such as Pyppeteer reject such events.
			return
		}
		s.event("Runtime.consoleAPICalled", map[string]any{"type": e.Name, "args": remoteObjects(e.Data["args"]), "executionContextId": contextID, "timestamp": float64(e.Time.UnixMilli())})
	case trace.Exception:
		details := map[string]any{"exceptionId": e.Sequence, "text": stringValue(e.Data["error"]), "lineNumber": intValue(e.Data["lineNumber"], 0), "columnNumber": intValue(e.Data["columnNumber"], 0)}
		if kind := stringValue(e.Data["valueType"]); kind != "" {
			object := map[string]any{"type": kind}
			if e.Data["errorObject"] == true {
				object["type"] = "object"
				object["subtype"] = "error"
				object["className"] = "Error"
				object["description"] = stringValue(e.Data["errorStack"])
			} else {
				object["description"] = stringValue(e.Data["error"])
			}
			details["exception"] = object
		}
		if contextID, ok := s.contextForRealm(stringValue(e.Data["realm"])); ok {
			details["executionContextId"] = contextID
		}
		if rawURL := stringValue(e.Data["url"]); rawURL != "" {
			details["url"] = rawURL
		}
		if rawURL := stringValue(e.Data["url"]); rawURL != "" {
			frames := make([]any, 0, 1)
			if rawFrames, ok := e.Data["stackFrames"].([]any); ok {
				for _, item := range rawFrames {
					frame, ok := item.(map[string]any)
					if !ok {
						continue
					}
					frames = append(frames, map[string]any{"functionName": stringValue(frame["functionName"]), "scriptId": "", "url": stringValue(frame["url"]), "lineNumber": max(intValue(frame["lineNumber"], 1)-1, 0), "columnNumber": max(intValue(frame["columnNumber"], 1)-1, 0)})
				}
			}
			if len(frames) == 0 {
				frames = append(frames, map[string]any{"functionName": "", "scriptId": "", "url": rawURL, "lineNumber": details["lineNumber"], "columnNumber": details["columnNumber"]})
			}
			details["stackTrace"] = map[string]any{"callFrames": frames}
		}
		s.event("Runtime.exceptionThrown", map[string]any{"timestamp": float64(e.Time.UnixMilli()), "exceptionDetails": details})
	case trace.Lifecycle:
		frameID := stringValue(e.Data["frameId"])
		if frameID == "" {
			frameID = s.page.Top.ID
		}
		timestamp := float64(e.Time.UnixMilli()) / 1000
		switch e.Name {
		case "isolatedWorldCreated":
			s.event("Runtime.executionContextCreated", map[string]any{"context": s.ensureRuntimeWorldContext(frameID, stringValue(e.Data["realm"]), stringValue(e.Data["mainRealm"]), stringValue(e.Data["worldName"]), stringValue(e.Data["url"]))})
		case "navigatedWithinDocument":
			s.event("Page.navigatedWithinDocument", map[string]any{"frameId": frameID, "url": e.Data["url"], "navigationType": e.Data["navigationType"]})
		case "frameAttached":
			parentFrameID := stringValue(e.Data["parentFrameId"])
			s.event("Page.frameAttached", map[string]any{"frameId": frameID, "parentFrameId": parentFrameID})
			s.event("Page.lifecycleEvent", map[string]any{"name": "init", "timestamp": timestamp, "frameId": frameID, "loaderId": s.frameLoaderID(frameID)})
			if initial, _ := e.Data["initialContext"].(bool); initial {
				s.event("Runtime.executionContextCreated", map[string]any{"context": s.ensureContext(frameID, stringValue(e.Data["realm"]), "about:blank")})
			}
		case "frameNavigated":
			isTop := frameID == s.page.Top.ID
			if isTop {
				s.event("Runtime.executionContextsCleared", map[string]any{})
				s.clearContexts()
			} else {
				s.destroyFrameContext(frameID)
			}
			loaderID := stringValue(e.Data["loaderId"])
			if loaderID == "" {
				loaderID = s.frameLoaderID(frameID)
			}
			s.event("Page.lifecycleEvent", map[string]any{"name": "init", "timestamp": timestamp, "frameId": frameID, "loaderId": loaderID})
			s.event("Page.frameNavigated", map[string]any{"frame": s.framePayloadByID(frameID, stringValue(e.Data["url"]), loaderID, stringValue(e.Data["parentFrameId"]))})
			s.event("Runtime.executionContextCreated", map[string]any{"context": s.ensureContext(frameID, stringValue(e.Data["realm"]), stringValue(e.Data["url"]))})
		case "frameDetached":
			s.event("Page.frameDetached", map[string]any{"frameId": frameID, "reason": "remove"})
			s.destroyFrameContext(frameID)
		case "DOMContentLoaded", "load":
			if frameID == s.page.Top.ID && e.Name == "DOMContentLoaded" {
				s.event("Page.domContentEventFired", map[string]any{"timestamp": timestamp})
			}
			if frameID == s.page.Top.ID && e.Name == "load" {
				s.event("Page.loadEventFired", map[string]any{"timestamp": timestamp})
			}
			s.event("Page.lifecycleEvent", map[string]any{"name": e.Name, "timestamp": timestamp, "frameId": frameID, "loaderId": s.frameLoaderID(frameID)})
		}
	case trace.Network:
		if strings.HasPrefix(stringValue(e.Data["url"]), "data:") {
			return
		}
		frameID := stringValue(e.Data["context"])
		if frameID == "" {
			frameID = s.page.Top.ID
		}
		loaderID := s.frameLoaderID(frameID)
		if e.Data["initiator"] == network.Iframe {
			loaderID = stringValue(e.Data["id"])
		}
		if e.Name == "request" {
			postData := stringValue(e.Data["postData"])
			request := map[string]any{"url": e.Data["url"], "method": e.Data["method"], "headers": e.Data["headers"], "postData": postData}
			if postData != "" {
				request["hasPostData"] = true
				request["postDataEntries"] = []any{map[string]any{"bytes": base64.StdEncoding.EncodeToString([]byte(postData))}}
			}
			s.event("Network.requestWillBeSent", map[string]any{"requestId": e.Data["id"], "loaderId": loaderID, "documentURL": e.Data["url"], "request": request, "timestamp": float64(e.Time.UnixMilli()) / 1000, "wallTime": float64(e.Time.Unix()), "initiator": map[string]any{"type": "other"}, "type": resourceTypeFromTrace(e.Data["initiator"]), "frameId": frameID})
		} else if e.Name == "response" {
			s.event("Network.responseReceived", map[string]any{"requestId": e.Data["id"], "loaderId": loaderID, "timestamp": float64(e.Time.UnixMilli()) / 1000, "type": resourceTypeFromTrace(e.Data["initiator"]), "response": map[string]any{"url": e.Data["url"], "status": e.Data["status"], "statusText": "", "headers": e.Data["headers"], "mimeType": e.Data["mimeType"], "connectionReused": e.Data["connectionReused"], "connectionId": e.Data["connectionId"], "protocol": cdpProtocol(e.Data["protocol"]), "timing": cdpResourceTiming(e.Data["transportTiming"]), "encodedDataLength": e.Data["encodedDataLength"], "securityState": "unknown"}, "frameId": frameID})
			if partial, _ := e.Data["partial"].(bool); !partial {
				s.event("Network.loadingFinished", map[string]any{"requestId": e.Data["id"], "timestamp": float64(e.Time.UnixMilli()) / 1000, "encodedDataLength": e.Data["encodedDataLength"]})
			}
		} else if e.Name == "failed" {
			canceled, _ := e.Data["canceled"].(bool)
			errorText := e.Data["error"]
			if canceled {
				errorText = "net::ERR_ABORTED"
			}
			s.event("Network.loadingFailed", map[string]any{"requestId": e.Data["id"], "timestamp": float64(e.Time.UnixMilli()) / 1000, "type": resourceTypeFromTrace(e.Data["initiator"]), "errorText": errorText, "canceled": canceled})
		}
	}
}
func (s *session) handle(m message) {
	if m.timing != nil {
		m.timing.started = time.Now()
		defer s.finishCommandTiming(m)
	}
	if afterUnlock := s.handleCommand(m); afterUnlock != nil {
		afterUnlock()
	}
}

// A command may defer its reply until browser work completes. That wait stays
// in the original transport worker, after the session and Page locks unwind.
func (s *session) handleCommand(m message) (afterUnlock func()) {
	if value, handled, err := s.handleMediaProfile(m); handled {
		s.reply(m.ID, value, err)
		return
	}
	if value, handled, err := s.handleResourcePolicy(m); handled {
		s.reply(m.ID, value, err)
		return
	}
	if value, handled, err := s.handleProfile(m); handled {
		s.reply(m.ID, value, err)
		return
	}
	commandData := map[string]any{"method": m.Method, "sessionId": s.id}
	if s.server.Browser.DevPreviewEnabled() && s.page.Trace().Wants(trace.CDP) {
		commandData["commandId"] = m.ID
		commandData["params"] = m.Params
	}
	s.page.Trace().Add(trace.CDP, "method", commandData)
	if !strings.HasPrefix(m.Method, "Mimic.") {
		if err := validateCommand(m.Method, m.Params); err != nil {
			s.reply(m.ID, nil, err)
			return
		}
	}
	var p map[string]any
	params := bytes.TrimSpace(m.Params)
	if len(params) > 0 && string(params) != "null" && (params[0] == '{' || strings.HasPrefix(m.Method, "Mimic.")) {
		if err := json.Unmarshal(m.Params, &p); err != nil {
			s.reply(m.ID, nil, fmt.Errorf("invalid command parameters: %w", err))
			return
		}
	}
	if strings.HasPrefix(m.Method, "Target.") || strings.HasPrefix(m.Method, "Browser.") {
		if result, handled, err := s.handleTarget(m, p); handled {
			if err != errReplySent {
				s.reply(m.ID, result, err)
			}
			return
		}
	}
	if value, handled, cookieErr := s.handleStorageCookies(m.Method, p); handled {
		s.reply(m.ID, value, cookieErr)
		return
	}
	control := m.Method == "Fetch.disable" || m.Method == "Fetch.getResponseBody" || m.Method == "Network.setRequestInterception" || m.Method == "Fetch.continueRequest" || m.Method == "Fetch.continueResponse" || m.Method == "Fetch.failRequest" || m.Method == "Fetch.fulfillRequest" || m.Method == "Network.continueInterceptedRequest" || m.Method == "Mimic.startTrace" || m.Method == "Mimic.stopTrace" || m.Method == "Mimic.getTrace" || m.Method == "Mimic.getStatus" || m.Method == "Mimic.getDiagnostics" || m.Method == "Mimic.cancelExecution" || m.Method == "Page.stopLoading" || m.Method == "Target.closeTarget"
	if !control {
		var waitStarted time.Time
		if m.timing != nil {
			waitStarted = time.Now()
		}
		s.commandMu.Lock()
		if m.timing != nil {
			m.timing.sessionWait = time.Since(waitStarted)
		}
		defer s.commandMu.Unlock()
		// Target commands operate on the registry or explicitly lock their target.
		// Never hold the control Page while bootstrapping an independent Page.
		if !strings.HasPrefix(m.Method, "Target.") {
			page := s.page
			if m.Method == "Mimic.captureSnapshot" {
				interrupt, _ := p["interrupt"].(bool)
				resume := s.server.pausePump(page, interrupt)
				defer resume()
			}
			if m.timing != nil {
				waitStarted = time.Now()
			}
			page.LockExternalCommand()
			if m.timing != nil {
				m.timing.pageWait = time.Since(waitStarted)
			}
			intermediateMouse := m.Method == "Input.dispatchMouseEvent" && stringValue(p["type"]) != "mouseReleased"
			if intermediateMouse {
				defer page.UnlockCommandsWithoutPreview()
			} else {
				defer page.UnlockCommands()
			}
		}
	}
	var result any = map[string]any{}
	var err error
	if s.ctx.Err() != nil {
		s.reply(m.ID, nil, fmt.Errorf("Session closed"))
		return
	}
	runtimeCtx := s.ctx
	finishRuntimeCall := func() {}
	if m.Method == "Runtime.callFunctionOn" && os.Getenv("MIMIC_PROFILE_CDP") == "1" {
		callID := fmt.Sprintf("CF#%d", callFunctionSequence.Add(1))
		callStarted := time.Now()
		runtimeCtx = trace.WithCorrelation(s.ctx, callID, callStarted, s.page.Trace())
		trace.Record(runtimeCtx, trace.CDP, "callFunctionOn.begin", map[string]any{"commandId": m.ID, "method": m.Method, "functionDeclaration": stringValue(p["functionDeclaration"])})
		finished := false
		finishRuntimeCall = func() {
			if finished {
				return
			}
			finished = true
			trace.Record(runtimeCtx, trace.CDP, "callFunctionOn.end", map[string]any{"commandId": m.ID, "method": m.Method, "durationNs": time.Since(callStarted).Nanoseconds()})
		}
		defer finishRuntimeCall()
	}
	if value, handled, runtimeErr := s.handleRuntime(runtimeCtx, m.Method, p); handled {
		finishRuntimeCall()
		s.reply(m.ID, value, runtimeErr)
		return
	}
	if value, handled, domErr := s.handleDOM(s.ctx, m.Method, p); handled {
		s.reply(m.ID, value, domErr)
		return
	}
	if value, handled, pageErr := s.handlePage(s.ctx, m.Method, p); handled {
		s.reply(m.ID, value, pageErr)
		return
	}
	if value, handled, emuErr := s.handleEmulation(m.Method, p); handled {
		s.reply(m.ID, value, emuErr)
		return
	}
	if value, handled, browserErr := s.handleBrowserWindow(m.Method, p); handled {
		s.reply(m.ID, value, browserErr)
		return
	}
	switch m.Method {
	case "Input.dispatchKeyEvent", "Input.insertText", "Input.dispatchMouseEvent", "Input.setIgnoreInputEvents":
		err = s.page.DispatchProtocolInput(s.ctx, m.Method, p)
	case "Page.enable", "Network.enable", "DOM.enable", "Log.enable", "Performance.enable", "Security.enable", "Inspector.enable":
		s.setDomain(strings.SplitN(m.Method, ".", 2)[0], true)
	case "Page.disable", "Network.disable", "DOM.disable", "Log.disable", "Performance.disable", "Security.disable", "Inspector.disable":
		s.setDomain(strings.SplitN(m.Method, ".", 2)[0], false)
	case "Runtime.disable":
		s.setDomain("Runtime", false)
		s.clearContexts()
	case "Runtime.runIfWaitingForDebugger":
		s.stateMu.Lock()
		s.waitForDebugger = false
		s.stateMu.Unlock()
		s.server.resumeTarget(s.page)
	case "Security.setIgnoreCertificateErrors":
		ignore, _ := p["ignore"].(bool)
		err = s.setCertificateOverride(ignore)
	case "Emulation.setTouchEmulationEnabled":
		if enabled, _ := p["enabled"].(bool); enabled {
			err = fmt.Errorf("Touch emulation is not supported")
		}
	case "Page.setLifecycleEventsEnabled":
		enabled, _ := p["enabled"].(bool)
		s.stateMu.Lock()
		s.lifecycleEvents = enabled
		s.stateMu.Unlock()
		if enabled {
			timestamp := float64(time.Now().UnixMilli()) / 1000
			frameID, loaderID := s.page.Top.ID, s.page.LoaderID()
			s.event("Page.lifecycleEvent", map[string]any{"name": "commit", "timestamp": timestamp, "frameId": frameID, "loaderId": loaderID})
			if s.page.Top.ReadyState() != "loading" {
				s.event("Page.lifecycleEvent", map[string]any{"name": "DOMContentLoaded", "timestamp": timestamp, "frameId": frameID, "loaderId": loaderID})
			}
			if s.page.Top.ReadyState() == "complete" {
				for _, name := range []string{"load"} {
					s.event("Page.lifecycleEvent", map[string]any{"name": name, "timestamp": timestamp, "frameId": frameID, "loaderId": loaderID})
				}
				s.replayIdle()
			}
		}
	case "Runtime.enable":
		if !s.domainEnabled("Runtime") {
			s.setDomain("Runtime", true)
			s.runtimeDebugger()
			var emitContexts func(*browser.Frame)
			emitContexts = func(frame *browser.Frame) {
				s.event("Runtime.executionContextCreated", map[string]any{"context": s.ensureContext(frame.ID, frame.RealmID(), frame.URL())})
				for _, child := range frame.Children() {
					emitContexts(child)
				}
			}
			emitContexts(s.page.Top)
			for _, world := range s.page.IsolatedWorlds() {
				s.event("Runtime.executionContextCreated", map[string]any{"context": s.ensureRuntimeWorldContext(world.FrameID, world.RealmID, world.MainRealmID, world.Name, world.URL)})
			}
		}
	case "Page.getFrameTree":
		result = map[string]any{"frameTree": s.frameTree(s.page.Top)}
	case "Page.navigate":
		navigationURL := stringValue(p["url"])
		// A replacement navigation must be able to interrupt application work
		// from the current document before its worker waits for the Page lock.
		// Otherwise one long-running parser task can make every later CDP
		// command, including the replacement Page.navigate, appear deadlocked.
		s.server.cancelExecution(s.page)
		loaderID := s.page.ReserveNavigation()
		result = map[string]any{"frameId": s.page.Top.ID, "loaderId": loaderID}
		committed := make(chan error, 1)
		var once sync.Once
		err = s.server.startNavigation(s.page, navigationURL, loaderID, s.navigationTimeout, func(commitErr error) {
			once.Do(func() { committed <- commitErr })
		})
		if err == nil {
			return func() {
				select {
				case commitErr := <-committed:
					if commitErr != nil {
						result.(map[string]any)["errorText"] = navigationReplyError(commitErr)
					}
					s.reply(m.ID, result, nil)
				case <-s.ctx.Done():
					s.reply(m.ID, nil, fmt.Errorf("Session closed"))
				}
			}
		}
	case "Page.stopLoading":
		s.server.cancelExecution(s.page)
		s.page.StopLoading()
	case "Page.addScriptToEvaluateOnNewDocument":
		id := s.page.AddInitScriptWorld(stringValue(p["source"]), stringValue(p["worldName"]))
		result = map[string]any{"identifier": id}
	case "Page.setBypassCSP":
		bypass, _ := p["enabled"].(bool)
		s.page.SetBypassCSP(bypass)
	case "Page.setFontFamilies":
		families, _ := p["fontFamilies"].(map[string]any)
		serif := stringValue(families["serif"])
		if serif == "" {
			serif = stringValue(families["standard"])
		}
		err = s.page.SetGenericFontFamilies(serif, stringValue(families["sansSerif"]), stringValue(families["fixed"]))
	case "DOM.getDocument":
		s.setDomain("DOM", true)
		var d *dom.Document
		var ok bool
		if d, ok = s.page.Document(); !ok {
			err = fmt.Errorf("no document")
		} else {
			result = map[string]any{"root": cdpNode(d, d.Root(), intValue(p["depth"], 1))}
		}
	case "DOM.querySelector", "DOM.querySelectorAll":
		var ids []int64
		ids, err = s.page.QueryDOM(s.ctx, int64(intValue(p["nodeId"], 0)), stringValue(p["selector"]), m.Method == "DOM.querySelectorAll")
		if err == nil {
			if m.Method == "DOM.querySelectorAll" {
				result = map[string]any{"nodeIds": ids}
			} else {
				var id int64
				if len(ids) > 0 {
					id = ids[0]
				}
				result = map[string]any{"nodeId": id}
			}
		}
	case "DOM.getOuterHTML":
		var d *dom.Document
		var ok bool
		if d, ok = s.page.Document(); !ok {
			err = fmt.Errorf("no document")
		} else {
			id := int64(intValue(p["nodeId"], 0))
			if id == 0 {
				id = int64(intValue(p["backendNodeId"], 0))
			}
			var markup string
			markup, err = d.OuterHTML(id)
			if err == nil {
				result = map[string]any{"outerHTML": markup}
			}
		}
	case "Network.getAllCookies":
		result = map[string]any{"cookies": s.pageCookies()}
	case "Network.setCookie":
		err = s.setCookie(p)
		if err == nil {
			result = map[string]any{"success": true}
		}
	case "Network.deleteCookies":
		err = s.deleteCookies(p)
	case "Network.clearBrowserCookies":
		s.page.Cookies().Clear()
	case "Network.setCacheDisabled":
		disabled, _ := p["cacheDisabled"].(bool)
		s.page.NetworkPolicy().SetCacheDisabled(disabled)
	case "Network.clearBrowserCache":
		s.page.NetworkSession().ClearCache()
	case "Network.setExtraHTTPHeaders":
		s.page.NetworkPolicy().SetExtraHeaders(headerObject(p["headers"]))
	case "Network.emulateNetworkConditions":
		offline, _ := p["offline"].(bool)
		s.page.SetNetworkOffline(offline)
	case "Network.setRequestInterception":
		patterns, _ := p["patterns"].([]any)
		err = s.interceptor.ConfigureNetwork(patterns)
	case "Network.continueInterceptedRequest":
		a := interceptAnswer{action: "continue", url: stringValue(p["url"]), method: stringValue(p["method"]), headers: headerObjectOrNil(p["headers"])}
		if post, ok := p["postData"].(string); ok {
			a.body = []byte(post)
		}
		if stringValue(p["errorReason"]) != "" {
			a.action = "fail"
		}
		if raw := stringValue(p["rawResponse"]); raw != "" {
			a, err = parseRawResponse(raw)
		}
		if err == nil {
			err = s.interceptor.Resolve(stringValue(p["interceptionId"]), a)
		}
	case "Network.getResponseBody":
		if body, encoded, ok, bodyErr := s.page.Loader().CompletedBody(stringValue(p["requestId"])); bodyErr != nil {
			err = bodyErr
		} else if ok {
			result = map[string]any{"body": body, "base64Encoded": encoded}
		} else {
			err = fmt.Errorf("unknown request id")
		}
	case "Network.getCookies":
		result = map[string]any{"cookies": s.cookiesForURLs(p)}
	case "Network.setCookies":
		if list, ok := p["cookies"].([]any); ok {
			for _, item := range list {
				if c, ok := item.(map[string]any); ok {
					if err = s.setCookie(c); err != nil {
						break
					}
				}
			}
		}
	case "Fetch.enable":
		patterns, _ := p["patterns"].([]any)
		err = s.interceptor.ConfigureFetch(patterns)
	case "Fetch.disable":
		s.interceptor.Enable(false)
	case "Fetch.continueRequest":
		a := interceptAnswer{action: "continue", url: stringValue(p["url"]), method: stringValue(p["method"]), headers: headersValue(p["headers"]), body: bodyParameter(p, "postData")}
		if enabled, ok := p["interceptResponse"].(bool); ok {
			a.interceptResponse = &enabled
		}
		err = s.interceptor.Resolve(stringValue(p["requestId"]), a)
	case "Fetch.continueResponse":
		err = s.interceptor.Resolve(stringValue(p["requestId"]), interceptAnswer{action: "continueResponse", status: intValue(p["responseCode"], 0), headers: headersValue(p["responseHeaders"])})
	case "Fetch.failRequest":
		err = s.interceptor.Resolve(stringValue(p["requestId"]), interceptAnswer{action: "fail"})
	case "Fetch.fulfillRequest":
		err = s.interceptor.Resolve(stringValue(p["requestId"]), interceptAnswer{action: "fulfill", status: intValue(p["responseCode"], 200), headers: headersValue(p["responseHeaders"]), body: bodyParameter(p, "body")})
	case "Fetch.getResponseBody":
		var body []byte
		body, err = s.interceptor.ResponseBody(stringValue(p["requestId"]))
		encoded := !utf8.Valid(body)
		text := string(body)
		if encoded {
			text = base64.StdEncoding.EncodeToString(body)
		}
		result = map[string]any{"body": text, "base64Encoded": encoded}
	case "Performance.getMetrics":
		result = map[string]any{"metrics": []any{map[string]any{"name": "Timestamp", "value": float64(time.Now().UnixNano()) / 1e9}}}
	case "Mimic.captureSnapshot":
		ctx, cancel := context.WithTimeout(s.ctx, 30*time.Second)
		defer cancel()
		result, err = s.page.CaptureSnapshot(ctx)
	case "Mimic.startTrace":
		s.page.Trace().Start()
	case "Mimic.stopTrace":
		s.page.Trace().Stop()
	case "Mimic.getTrace":
		result = map[string]any{"events": s.page.Trace().Events(), "crashReports": s.page.CrashReports()}
	case "Mimic.getStatus":
		activity := s.page.ExecutionStatus()
		result = map[string]any{
			"execution": map[string]any{
				"running":   activity.Running,
				"taskId":    activity.TaskID,
				"source":    activity.Source,
				"phase":     activity.Phase,
				"elapsedMs": activity.Elapsed.Milliseconds(),
			},
		}
	case "Mimic.getDiagnostics":
		result = s.page.LiveDiagnostics()
	case "Mimic.cancelExecution":
		result = map[string]any{"cancelled": s.server.cancelExecution(s.page)}
	case "Mimic.clearTrace":
		s.page.Trace().Clear()
	case "Mimic.getCompatibilityMatrix":
		result = map[string]any{"chromeVersion": s.page.Environment().Product.FullVersion, "protocol": protocolMatrix(protocolCommandNames(), protocolEventNames()), "extensions": protocolExtensionMatrix()}
	case "Mimic.setViewport":
		err = s.page.SetViewport(intValue(p["width"], 0), intValue(p["height"], 0))
	case "Mimic.pause":
		s.page.Pause()
	case "Mimic.resume":
		s.page.Resume()
	default:
		if _, registered := protocolCommands[m.Method]; registered {
			s.page.Trace().Add(trace.SemanticMissing, "CDP."+m.Method, map[string]any{"sessionId": s.id})
			err = fmt.Errorf("method %s is registered for the pinned CDP schema but its semantics are not implemented", m.Method)
		} else {
			s.page.Trace().Add(trace.SurfaceMissing, "CDP."+m.Method, map[string]any{"sessionId": s.id})
			err = fmt.Errorf("method %s is absent from the pinned CDP schema", m.Method)
		}
	}
	s.reply(m.ID, result, err)
	return nil
}
func targetInfo(page *browser.Page, attached bool) map[string]any {
	return map[string]any{"targetId": page.ID, "type": "page", "title": page.Title(), "url": page.URL(), "attached": attached}
}
func browserTargetInfo(id string) map[string]any {
	return map[string]any{"targetId": id, "type": "browser", "title": "", "url": "", "attached": true, "canAccessOpener": false}
}
func (s *session) frameLoaderID(frameID string) string {
	if frameID == "" || frameID == s.page.Top.ID {
		return s.page.LoaderID()
	}
	if frame, ok := s.page.Frame(frameID); ok {
		return frame.LoaderID()
	}
	return ""
}

func (s *session) framePayload(frame *browser.Frame) map[string]any {
	if frame == nil {
		return map[string]any{}
	}
	parentID := ""
	if frame.Parent() != nil {
		parentID = frame.Parent().ID
	}
	return s.framePayloadByID(frame.ID, frame.URL(), s.frameLoaderID(frame.ID), parentID)
}

func (s *session) framePayloadByID(frameID, rawURL, loaderID, parentID string) map[string]any {
	if rawURL == "" {
		if frame, ok := s.page.Frame(frameID); ok {
			rawURL = frame.URL()
		}
	}
	if rawURL == "" && frameID == s.page.Top.ID {
		rawURL = s.page.URL()
	}
	securityOrigin := originURL(rawURL)
	if securityOrigin == "://" && parentID != "" {
		if parent, ok := s.page.Frame(parentID); ok {
			securityOrigin = originURL(parent.URL())
		}
	}
	payload := map[string]any{"id": frameID, "loaderId": loaderID, "url": rawURL, "domainAndRegistry": "", "securityOrigin": securityOrigin, "mimeType": "text/html"}
	if frame, ok := s.page.Frame(frameID); ok {
		payload["name"] = frame.Name()
	}
	if parentID != "" {
		payload["parentId"] = parentID
	}
	return payload
}

func (s *session) frameTree(frame *browser.Frame) map[string]any {
	tree := map[string]any{"frame": s.framePayload(frame)}
	children := frame.Children()
	if len(children) != 0 {
		childTrees := make([]any, 0, len(children))
		for _, child := range children {
			childTrees = append(childTrees, s.frameTree(child))
		}
		tree["childFrames"] = childTrees
	}
	return tree
}

func (s *session) ensureContext(frameID, realmID, rawURL string) map[string]any {
	s.contextMu.Lock()
	if contextID, ok := s.contextByFrame[frameID]; ok && (realmID == "" || s.realmByFrame[frameID] == realmID) {
		s.contextMu.Unlock()
		return s.contextPayload(contextID, frameID, rawURL)
	}
	s.nextContextID++
	contextID := s.nextContextID
	s.contextByFrame[frameID] = contextID
	s.frameByContext[contextID] = frameID
	s.realmByFrame[frameID] = realmID
	s.contextMu.Unlock()
	return s.contextPayload(contextID, frameID, rawURL)
}

func (s *session) contextPayload(contextID int64, frameID, rawURL string) map[string]any {
	origin := originURL(rawURL)
	if origin == "://" {
		if frame, ok := s.page.Frame(frameID); ok && frame.Parent() != nil {
			origin = originURL(frame.Parent().URL())
		}
	}
	s.contextMu.Lock()
	uniqueID := s.realmByFrame[frameID]
	s.contextMu.Unlock()
	return map[string]any{"id": contextID, "origin": origin, "name": "", "uniqueId": uniqueID, "auxData": map[string]any{"isDefault": true, "type": "default", "frameId": frameID}}
}

func (s *session) contextForRealm(realmID string) (int64, bool) {
	s.contextMu.Lock()
	defer s.contextMu.Unlock()
	for id, world := range s.worldContexts {
		if world.RealmID == realmID {
			return id, true
		}
	}
	for frameID, currentRealmID := range s.realmByFrame {
		if currentRealmID == realmID {
			contextID, ok := s.contextByFrame[frameID]
			return contextID, ok
		}
	}
	return 0, false
}

func (s *session) clearContexts() {
	if s.debugger != nil {
		s.debugger.Prune()
	}
	s.clearRuntimeWorldContexts()
	s.contextMu.Lock()
	s.contextByFrame = map[string]int64{}
	s.frameByContext = map[int64]string{}
	s.realmByFrame = map[string]string{}
	s.contextMu.Unlock()
}

func (s *session) destroyFrameContext(frameID string) {
	if s.debugger != nil {
		s.debugger.Prune()
	}
	s.destroyRuntimeWorldContexts(frameID)
	s.contextMu.Lock()
	contextID, ok := s.contextByFrame[frameID]
	if ok {
		delete(s.contextByFrame, frameID)
		delete(s.frameByContext, contextID)
		delete(s.realmByFrame, frameID)
	}
	s.contextMu.Unlock()
	if ok {
		s.event("Runtime.executionContextDestroyed", map[string]any{"executionContextId": contextID, "executionContextUniqueId": ""})
	}
}

func originURL(raw string) string {
	u, e := url.Parse(raw)
	if e != nil || u.Host == "" {
		return "://"
	}
	return u.Scheme + "://" + u.Host
}
func resourceTypeFromTrace(v any) string {
	switch fmt.Sprint(v) {
	case "navigation", "iframe":
		return "Document"
	case "script", "worker":
		return "Script"
	case "stylesheet":
		return "Stylesheet"
	case "fetch":
		return "Fetch"
	case "xhr":
		return "XHR"
	case "image":
		return "Image"
	}
	return "Other"
}
func stringValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
func intValue(v any, d int) int {
	switch n := v.(type) {
	case int:
		return n
	case int32:
		return int(n)
	case int64:
		return int(n)
	case uint32:
		return int(n)
	case uint64:
		return int(n)
	case float32:
		return int(n)
	case float64:
		return int(n)
	}
	return d
}
func remoteObject(v any) map[string]any {
	if v == nil {
		return map[string]any{"type": "undefined"}
	}
	typ := "object"
	switch v.(type) {
	case string:
		typ = "string"
	case bool:
		typ = "boolean"
	case int, int32, int64, float32, float64:
		typ = "number"
	}
	return map[string]any{"type": typ, "value": v, "description": fmt.Sprint(v)}
}

func remoteObjects(value any) []map[string]any {
	items, ok := value.([]any)
	if !ok {
		if value == nil {
			return []map[string]any{}
		}
		items = []any{value}
	}
	objects := make([]map[string]any, 0, len(items))
	for _, item := range items {
		objects = append(objects, remoteObject(item))
	}
	return objects
}

// cdpProtocol translates the transport's factual response protocol into the
// spelling exposed by Chromium's DevTools protocol. The transport trace keeps
// the original value (for example HTTP/3.0); this is only an adapter view.
func cdpProtocol(value any) string {
	protocol := strings.ToLower(strings.TrimSpace(fmt.Sprint(value)))
	switch {
	case protocol == "h3" || strings.HasPrefix(protocol, "http/3"):
		return "h3"
	case protocol == "h2" || strings.HasPrefix(protocol, "http/2"):
		return "h2"
	case strings.HasPrefix(protocol, "http/1.1"):
		return "http/1.1"
	case strings.HasPrefix(protocol, "http/1.0"):
		return "http/1.0"
	default:
		return protocol
	}
}

func cdpResourceTiming(value any) map[string]any {
	timing, ok := value.(network.TransportTimingSnapshot)
	if !ok {
		return map[string]any{}
	}
	phase := func(name string) float64 {
		if observed, exists := timing.Phases[name]; exists {
			return observed
		}
		return -1
	}
	return map[string]any{
		"requestTime": 0,
		"proxyStart":  -1, "proxyEnd": -1,
		"dnsStart": phase("dnsStart"), "dnsEnd": phase("dnsEnd"),
		"connectStart": phase("tcpConnectStart"), "connectEnd": phase("tcpConnectEnd"),
		"sslStart": phase("tlsHandshakeStart"), "sslEnd": phase("tlsHandshakeEnd"),
		"workerStart": -1, "workerReady": -1, "workerFetchStart": -1, "workerRespondWithSettled": -1,
		"sendStart": phase("requestHeadersSent"), "sendEnd": phase("requestComplete"),
		"pushStart": 0, "pushEnd": 0,
		"receiveHeadersStart": phase("firstResponseByte"), "receiveHeadersEnd": phase("firstResponseByte"),
	}
}
func cdpNode(d *dom.Document, n dom.Node, depth int) map[string]any {
	attrs := []string{}
	for k, v := range n.Attributes {
		attrs = append(attrs, k, v)
	}
	nodeType, nodeName, localName := 1, n.TagName, strings.ToLower(n.TagName)
	switch n.Type {
	case "document":
		nodeType, nodeName, localName = 9, "#document", ""
	case "text":
		nodeType, nodeName, localName = 3, "#text", ""
	case "comment":
		nodeType, nodeName, localName = 8, "#comment", ""
	case "doctype":
		nodeType, localName = 10, ""
	}
	out := map[string]any{"nodeId": n.ID, "backendNodeId": n.ID, "nodeType": nodeType, "nodeName": nodeName, "localName": localName, "nodeValue": n.Text, "attributes": attrs, "childNodeCount": len(n.Children)}
	if depth != 0 {
		children := []any{}
		for _, id := range n.Children {
			if c, ok := d.Get(id); ok {
				children = append(children, cdpNode(d, c, depth-1))
			}
		}
		out["children"] = children
	}
	return out
}
func headersValue(v any) http.Header {
	if a, ok := v.([]any); ok {
		h := make(http.Header)
		for _, x := range a {
			if m, ok := x.(map[string]any); ok {
				h.Add(stringValue(m["name"]), stringValue(m["value"]))
			}
		}
		return h
	}
	return nil
}
func headerObject(v any) http.Header {
	h := make(http.Header)
	if m, ok := v.(map[string]any); ok {
		for k, x := range m {
			h.Set(k, fmt.Sprint(x))
		}
	}
	return h
}
func headerObjectOrNil(v any) http.Header {
	if _, ok := v.(map[string]any); !ok {
		return nil
	}
	return headerObject(v)
}
func parseRawResponse(encoded string) (interceptAnswer, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return interceptAnswer{}, err
	}
	res, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(raw)), &http.Request{Method: http.MethodGet})
	if err != nil {
		return interceptAnswer{}, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return interceptAnswer{}, err
	}
	return interceptAnswer{action: "fulfill", status: res.StatusCode, headers: res.Header.Clone(), body: body}, nil
}
func (s *session) pageCookies() []any {
	return cookieRows(s.page.Cookies().Snapshots())
}
func cookieRows(snapshots []network.CookieSnapshot) []any {
	out := []any{}
	for _, snapshot := range snapshots {
		c := snapshot.Cookie
		domain := c.Domain
		if !snapshot.HostOnly && !strings.HasPrefix(domain, ".") {
			domain = "." + domain
		}
		expires := float64(-1)
		if !c.Expires.IsZero() {
			expires = float64(c.Expires.UnixNano()) / 1e9
		}
		row := map[string]any{"name": c.Name, "value": c.Value, "domain": domain, "path": c.Path, "secure": c.Secure, "httpOnly": c.HttpOnly,
			"expires": expires, "session": c.Expires.IsZero(), "size": len(c.Name) + len(c.Value)}
		if snapshot.PartitionKey != nil {
			row["partitionKey"] = snapshot.PartitionKey
		}
		switch c.SameSite {
		case http.SameSiteNoneMode:
			row["sameSite"] = "None"
		case http.SameSiteLaxMode:
			row["sameSite"] = "Lax"
		case http.SameSiteStrictMode:
			row["sameSite"] = "Strict"
		}
		out = append(out, row)
	}
	return out
}
