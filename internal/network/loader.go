package network

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/google/uuid"
	"github.com/klauspost/compress/zstd"
	"github.com/moreveal/mimic/internal/csp"
	"github.com/moreveal/mimic/internal/monotime"
	"github.com/moreveal/mimic/internal/state"
	"github.com/moreveal/mimic/internal/trace"
)

type Initiator string

const (
	Navigation Initiator = "navigation"
	Fetch      Initiator = "fetch"
	XHR        Initiator = "xhr"
	Script     Initiator = "script"
	Stylesheet Initiator = "stylesheet"
	Iframe     Initiator = "iframe"
	Image      Initiator = "image"
	Worker     Initiator = "worker"
	Other      Initiator = "other"
)

type Request struct {
	// ContentPolicy is captured from the initiating realm and remains fixed
	// across redirects. Its source URL must not follow the request destination.
	ContentPolicy          csp.PolicySet
	ContentPolicyURL       *url.URL
	ContentPolicyDirective string

	// Kind, Owner and Mechanism describe why the browser requested this URL.
	// They are policy inputs, separate from wire headers and CDP Initiator.
	Kind, Owner, Mechanism string
	policySnapshot         *compiledResourcePolicy
	// ClientIsWorker identifies the initiating realm, not the resource type:
	// a worker script loaded by a document still has a document client.
	ClientIsWorker       bool
	TopLevelURL          *url.URL
	Credentials          string
	OpaqueOrigin         bool
	HasCrossSiteAncestor bool
	// Worker script and worker Fetch requests do not acquire document hints.
	OmitClientHints bool
	ClientHints     *ClientHintsContext
	ID, ContextID   string
	URL             *url.URL
	Referrer        *url.URL
	// SourceURL is the initiating document/worker URL. It remains available
	// for Fetch Metadata and origin decisions when Referrer-Policy suppresses
	// the wire Referer header.
	SourceURL *url.URL
	// SourceOrigin is the client's security origin when it differs from its
	// document URL, as for inherited about:blank/srcdoc and blob contexts. It
	// governs CORS, credentials and Fetch Metadata without inventing a Referer.
	SourceOrigin *url.URL
	// UserActivation is captured from the navigation initiator, not inferred from
	// its destination. Script and iframe requests need not be user activated.
	UserActivation bool
	ReferrerPolicy string
	Method         string
	Headers        http.Header
	// AuthorHeaderOrder preserves the order in which script supplied distinct
	// header names. Blink stores those fields in HTTPHeaderMap; its pinned WTF
	// HashMap iteration, rather than Go map iteration, feeds the network stack.
	AuthorHeaderOrder []string
	Body              []byte
	Initiator         Initiator
	// Mode and Destination are Fetch-layer properties, not transport guesses.
	// They are populated by browser consumers when an element changes the
	// default request mode (for example, a crossorigin classic script).
	Mode        string
	Destination string
	// CORSPreflightRequired is the consumer's Fetch preflight flag, such as
	// XHR upload listeners. It does not change the request's initiator.
	CORSPreflightRequired bool
	// Redirect is the Fetch redirect mode; the empty value means follow.
	Redirect string
	// PerformanceInitiatorType is the Resource Timing projection. It is kept
	// separate from Initiator because CDP may classify a browser-owned favicon
	// lookup as Other while PerformanceResourceTiming exposes "img".
	PerformanceInitiatorType string
	// Resource Timing belongs to the initiating document/worker, including
	// across redirects and navigation of its browsing context. Capture these
	// values on the agent's clock; transport diagnostics keep their wall clock.
	// They deliberately live on Request, never on cached Response objects.
	PerformanceOwner string
	PerformanceStart time.Time
	// criticalCHRestarted is loader-owned navigation state. It prevents a
	// malformed or changing response from causing an unbounded internal retry.
	criticalCHRestarted bool
	operationStarted    time.Time
	redirectEnd         float64
	timingAllowFailed   bool
	redirectCount       int
	corsPrepared        bool
	corsPreflight       bool
	corsUnsafeHeaders   []string
	reportingFailure    bool
	chain               requestChain
}
type Response struct {
	// Partial means policy stopped body consumption before the full resource.
	Partial bool
	// DecodeDisallowed survives delivery so later consumers cannot silently
	// materialize pixels from a resource loaded under a restricted policy.
	DecodeDisallowed bool
	// A weak internal sharing hint, never an extra owner. Public consumers get
	// independent mutable Body bytes and never carry this hint across calls.
	sharedBody     *storedBody
	bodyStorageErr error
	policyOwner    *ResourcePolicyState
	policySnapshot *compiledResourcePolicy
	// Referrer is the committed navigation referrer after request policy/redirects.
	Referrer        string
	Redirected      bool
	Type            string
	Status          int
	Headers         http.Header
	Body            []byte
	URL             *url.URL
	Synthetic       bool
	FromCache       bool
	Duration        time.Duration
	EncodedBodySize int64
	Protocol        string
	TransportTiming TransportTimingSnapshot
	// BrowserVisibleTiming is a projection over the complete browser operation.
	// For a Critical-CH navigation restart it includes the real elapsed offset
	// before the final transport attempt; TransportTiming remains per-request.
	BrowserVisibleTiming TransportTimingSnapshot
}

// ReserveDecodedPixelBytes applies the request's captured Context budget to
// image decode work. A cache hit uses the consuming request's generation.
func (r Response) ReserveDecodedPixelBytes(bytes int64) error {
	if r.policyOwner == nil {
		return nil
	}
	return r.policyOwner.reserveDecoded(r.policySnapshot, bytes)
}

func (r Response) DecodedPixelBudgetEnabled() bool {
	return r.policySnapshot != nil && r.policySnapshot.config.Budgets.MaxDecodedBytes > 0
}

type Decision struct {
	Block    error
	Redirect *url.URL
	Request  *Request
	Response *Response
}
type Interceptor interface {
	Before(context.Context, Request) (Decision, error)
	After(context.Context, Request, Response) (Response, error)
}
type Transport interface {
	RoundTrip(*http.Request) (*http.Response, error)
}
type HTTPTransport struct{ Client *http.Client }

func (t HTTPTransport) RoundTrip(r *http.Request) (*http.Response, error) { return t.Client.Do(r) }

type Loader struct {
	resourcePolicy          *ResourcePolicyState
	ignoreCertificateErrors atomic.Bool
	ownsTransport           bool
	ownsSession             bool
	activityMu              sync.Mutex
	activeLoads             int
	documentLoadSequence    uint64
	documentLoads           map[uint64]context.CancelCauseFunc
	idleSince               [2]time.Time
	transport               Transport
	env                     func() state.Environment
	cookies                 *CookieStore
	session                 *SessionState
	policy                  RequestPolicy
	interceptors            []Interceptor
	interceptorsMu          sync.RWMutex
	trace                   *trace.Recorder
	completedMu             sync.RWMutex
	completed               map[string]Response
	completedOrder          []string
	completedClosed         bool
}

func (l *Loader) SetResourcePolicy(policy *ResourcePolicyState) { l.resourcePolicy = policy }

// CheckLocalResourcePolicy applies the shared whole-resource decision to a
// browser-owned attachment. It does not manufacture a request/response body,
// acquire a connection, or charge HTTP body/cache budgets.
func (l *Loader) CheckLocalResourcePolicy(request Request) error {
	if l.resourcePolicy == nil {
		return nil
	}
	snapshot := l.resourcePolicy.Capture()
	if snapshot == nil {
		return nil
	}
	decision := snapshot.decide(request)
	l.resourcePolicy.recordDecision(decision, false)
	l.trace.Add(trace.Resource, "policyDecision", map[string]any{"url": request.URL.String(), "kind": request.ResourceKind(), "ruleId": decision.RuleID, "generation": decision.Generation, "reportOnly": decision.ReportOnly, "localAttachment": true})
	_, err := l.checkWholeResourcePolicy(decision, true)
	return err
}

func (l *Loader) checkWholeResourcePolicy(decision ResourceDecision, localAttachment bool) (bool, error) {
	if decision.Work.CacheRead == nil || *decision.Work.CacheRead || decision.Work.Network == nil || *decision.Work.Network {
		return false, nil
	}
	l.resourcePolicy.recordBlocked(decision.ReportOnly)
	if decision.ReportOnly {
		return true, nil
	}
	if !localAttachment {
		l.resourcePolicy.recordUnknownAvoidance()
	}
	return false, fmt.Errorf("net::ERR_BLOCKED_BY_CLIENT: resource policy rule %q", decision.RuleID)
}

// ResourceReuseAllowed covers document preloads and already available images,
// which can satisfy a consumer without entering the transport/cache loader.
func (l *Loader) ResourceReuseAllowed(request Request) bool {
	if l.resourcePolicy == nil {
		return true
	}
	snapshot := l.resourcePolicy.Capture()
	if snapshot == nil {
		return true
	}
	decision := snapshot.decide(request)
	return decision.ReportOnly || decision.Work.CacheRead == nil || *decision.Work.CacheRead
}

func (l *Loader) ResourceDecodeAllowed(request Request) bool {
	if l.resourcePolicy == nil {
		return true
	}
	snapshot := l.resourcePolicy.Capture()
	if snapshot == nil {
		return true
	}
	d := snapshot.decide(request)
	return d.ReportOnly || d.Work.Decode == nil || *d.Work.Decode
}

func NewLoader(env func() state.Environment, cookies *CookieStore, tr *trace.Recorder) *Loader {
	loader := NewLoaderWithSession(env, cookies, NewSessionState(), tr)
	loader.ownsSession = true
	return loader
}
func NewLoaderWithSession(env func() state.Environment, cookies *CookieStore, session *SessionState, tr *trace.Recorder) *Loader {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	client := &http.Client{Transport: transport, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &Loader{transport: &httpCertificateTransport{HTTPTransport: HTTPTransport{client}}, ownsTransport: true, env: env, cookies: cookies, session: session, trace: tr, completed: map[string]Response{}}
}
func (l *Loader) SetTransport(t Transport) {
	l.CloseOwnedTransport()
	l.transport = t
	l.ownsTransport = false
	l.ignoreCertificateErrors.Store(false)
}

func (l *Loader) Policy() *RequestPolicy { return &l.policy }
func (l *Loader) Use(i Interceptor) func() {
	l.interceptorsMu.Lock()
	l.interceptors = append(l.interceptors, i)
	l.interceptorsMu.Unlock()
	return func() {
		l.interceptorsMu.Lock()
		defer l.interceptorsMu.Unlock()
		for n, x := range l.interceptors {
			if x == i {
				l.interceptors = append(l.interceptors[:n], l.interceptors[n+1:]...)
				break
			}
		}
	}
}
func (l *Loader) Load(ctx context.Context, r Request) (response Response, loadErr error) {
	if l.resourcePolicy != nil && r.policySnapshot == nil {
		r.policySnapshot = l.resourcePolicy.Capture()
	}
	capturedResourcePolicy := r.policySnapshot
	defer func() { response.sharedBody = nil }()
	l.beginActivity()
	defer l.endActivity()
	if err := ctx.Err(); err != nil {
		return Response{}, err
	}
	if !r.reportingFailure && !r.ClientIsWorker {
		var release func()
		ctx, release = l.trackDocumentLoad(ctx)
		defer release()
		defer func() {
			if loadErr != nil && context.Cause(ctx) == ErrDocumentLoadingStopped {
				loadErr = ErrDocumentLoadingStopped
			}
		}()
	}
	if r.operationStarted.IsZero() {
		r.operationStarted = monotime.Now()
	}
	if r.ID == "" {
		r.ID = uuid.NewString()
	}
	if !r.reportingFailure {
		r.reportingFailure = true
		defer func() {
			if loadErr != nil {
				l.trace.Add(trace.Network, "failed", map[string]any{"id": r.ID, "url": r.URL.String(), "error": loadErr.Error(), "initiator": r.Initiator, "context": r.ContextID, "canceled": errors.Is(loadErr, context.Canceled) || ctx.Err() == context.Canceled})
				l.trace.Add(trace.Resource, "loadEnd", map[string]any{"id": r.ID, "url": r.URL.String(), "error": loadErr.Error()})
			}
		}()
	}
	if r.Method == "" {
		r.Method = http.MethodGet
	}
	if r.Headers == nil {
		r.Headers = make(http.Header)
	}
	if err := r.CheckContentPolicy(); err != nil {
		return Response{}, err
	}
	r.beginChain()
	deferDeniedPreflight := false
	if r.policySnapshot != nil && !r.policySnapshot.config.ReportOnly {
		early := r.policySnapshot.decide(r)
		deferDeniedPreflight = early.Work.Network != nil && !*early.Work.Network
	}
	if !deferDeniedPreflight {
		if err := l.prepareFetchCORS(ctx, &r); err != nil {
			return Response{}, err
		}
	}
	snapshot := l.policy.Snapshot()
	if r.URL.Scheme == "data" {
		body, contentType, err := decodeDataURL(r.URL)
		if err != nil {
			return Response{}, err
		}
		headers := make(http.Header)
		headers.Set("Content-Type", contentType)
		return l.after(ctx, r, Response{Status: http.StatusOK, Headers: headers, Body: body, URL: r.URL, Synthetic: true})
	}
	if r.URL.Scheme == "blob" {
		if r.Headers.Get("Accept") == "" {
			r.Headers.Set("Accept", "*/*")
		}
	} else {
		headers := l.env().RequestHeaders()
		if r.ClientIsWorker {
			headers = l.env().WorkerRequestHeaders()
		}
		for k, v := range headers {
			if r.OmitClientHints && strings.HasPrefix(strings.ToLower(k), "sec-ch-") {
				continue
			}
			if r.Headers.Get(k) == "" {
				r.Headers.Set(k, v)
			}
		}
		for k, v := range l.env().ClientHintHeaders(l.acceptedClientHints(r)) {
			if r.Headers.Get(k) == "" {
				r.Headers.Set(k, v)
			}
		}
		applyBrowserRequestHeaders(&r)
		applyStorageAccessHeader(&r, l.env().Network.CookiesEnabled)
		if r.Headers.Get("Accept-Encoding") == "" {
			r.Headers.Set("Accept-Encoding", "gzip, deflate, br, zstd")
		}
	}
	if r.Headers.Get("Referer") == "" {
		if referrer := r.ReferrerValue(); referrer != "" {
			r.Headers.Set("Referer", referrer)
		}
	}
	for k, values := range snapshot.ExtraHeaders {
		r.Headers.Del(k)
		for _, v := range values {
			r.Headers.Add(k, v)
		}
	}
	if !requestIncludesCredentials(r) {
		r.Headers.Del("Cookie")
	}
	var cookiePairs []string
	if l.env().Network.CookiesEnabled && requestIncludesCredentials(r) {
		for _, c := range l.cookies.ForURL(r.URL, r.cookieContext()) {
			cookiePairs = append(cookiePairs, c.Name+"="+c.Value)
		}
	}
	if len(cookiePairs) > 0 && r.Headers.Get("Cookie") == "" {
		r.Headers.Set("Cookie", strings.Join(cookiePairs, "; "))
	}
	visibleHeaders := headerStrings(r.Headers)
	requestStarted := monotime.Now()
	l.trace.Add(trace.Network, "request", map[string]any{"id": r.ID, "url": r.URL.String(), "method": r.Method, "headers": visibleHeaders, "postData": string(r.Body), "initiator": r.Initiator, "context": r.ContextID, "performanceOwner": r.PerformanceOwner, "performanceStart": r.PerformanceStart})
	l.trace.Add(trace.Resource, "loadStart", map[string]any{"id": r.ID, "url": r.URL.String(), "type": r.Initiator, "context": r.ContextID})
	decision := ResourceDecision{}
	reportedWholeBlock := false
	if r.policySnapshot != nil {
		decision = r.policySnapshot.decide(r)
		l.resourcePolicy.recordDecision(decision, false)
		l.trace.Add(trace.Resource, "policyDecision", map[string]any{"id": r.ID, "url": r.URL.String(), "kind": r.ResourceKind(), "ruleId": decision.RuleID, "generation": decision.Generation, "reportOnly": decision.ReportOnly})
		var policyErr error
		reportedWholeBlock, policyErr = l.checkWholeResourcePolicy(decision, false)
		if policyErr != nil {
			return Response{}, policyErr
		}
	}
	if snapshot.Offline && r.URL.Scheme != "blob" {
		return Response{}, fmt.Errorf("net::ERR_INTERNET_DISCONNECTED")
	}
	interceptors := l.interceptorSnapshot()
	originalURL := r.URL.String()
	for _, i := range interceptors {
		d, err := i.Before(ctx, r)
		if err != nil {
			return Response{}, err
		}
		if d.Block != nil {
			return Response{}, d.Block
		}
		if d.Redirect != nil {
			r.URL = d.Redirect
		}
		if d.Request != nil {
			r = *d.Request
			// An interceptor may replace the request object, but it cannot
			// replace the policy generation captured by this operation.
			r.policySnapshot = capturedResourcePolicy
		}
		if d.Response != nil {
			res := *d.Response
			res.Synthetic = true
			// Fulfillment replaces transport, not the browser's response handling.
			// Its wait is observable even though no socket phases took place.
			if res.BrowserVisibleTiming.Phases == nil {
				begin := float64(requestStarted.Sub(r.operationStarted)) / float64(time.Millisecond)
				end := float64(monotime.Since(r.operationStarted)) / float64(time.Millisecond)
				res.BrowserVisibleTiming.Phases = map[string]float64{
					"dnsStart": begin, "dnsEnd": begin,
					"tcpConnectStart": begin, "tcpConnectEnd": begin,
					"tlsHandshakeStart": begin, "tlsHandshakeEnd": begin,
					"requestHeadersSent": begin, "firstResponseByte": end,
					"responseComplete": end,
				}
				res.Duration = monotime.Since(requestStarted)
			}
			l.acceptResponseState(r, res)
			return l.after(ctx, r, res)
		}
	}
	if r.policySnapshot != nil && r.URL.String() != originalURL {
		decision = r.policySnapshot.decide(r)
		l.trace.Add(trace.Resource, "policyDecision", map[string]any{"id": r.ID, "url": r.URL.String(), "kind": r.ResourceKind(), "ruleId": decision.RuleID, "generation": decision.Generation, "reportOnly": decision.ReportOnly, "afterInterception": true})
	}
	if r.URL.Scheme == "blob" {
		body, contentType, ok := l.session.Blob(r.URL.String())
		if !ok {
			return Response{}, fmt.Errorf("blob URL has been revoked or is unknown")
		}
		headers := make(http.Header)
		if contentType != "" {
			headers.Set("Content-Type", contentType)
		}
		return l.after(ctx, r, Response{Status: http.StatusOK, Headers: headers, Body: body, URL: r.URL, Synthetic: true})
	}
	cacheAllowed := decision.ReportOnly || decision.Work.CacheRead == nil || *decision.Work.CacheRead
	cacheLimit := int64(-1)
	if !decision.ReportOnly {
		if decision.Work.Body == "none" {
			cacheLimit = 0
		}
		if decision.Work.Body == "prefix" {
			cacheLimit = decision.Work.PrefixBytes
		}
	}
	if cached, ok, err := l.cachedResponseForPolicy(r, snapshot, cacheAllowed, cacheLimit); err != nil {
		l.trace.Add(trace.Error, "responseBodyRead", map[string]any{"id": r.ID, "error": err.Error(), "owner": "cache"})
	} else if ok {
		if r.policySnapshot != nil {
			if budgetErr := l.resourcePolicy.consumeCachedBody(r.policySnapshot, int64(len(cached.Body))); budgetErr != nil {
				return Response{}, budgetErr
			}
			if decision.ReportOnly {
				cacheDenied := decision.Work.CacheRead != nil && !*decision.Work.CacheRead
				networkDenied := decision.Work.Network != nil && !*decision.Work.Network
				if cacheDenied {
					l.resourcePolicy.recordWouldBypassCache()
				}
				if cacheDenied && networkDenied {
					if !reportedWholeBlock {
						l.resourcePolicy.recordBlocked(true)
					}
					l.resourcePolicy.recordKnownAvoidance(int64(len(cached.Body)))
				} else if !cacheDenied {
					switch decision.Work.Body {
					case "none":
						l.resourcePolicy.recordKnownAvoidance(int64(len(cached.Body)))
					case "prefix":
						l.resourcePolicy.recordKnownAvoidance(max(0, int64(len(cached.Body))-decision.Work.PrefixBytes))
					}
				}
			} else if cached.Partial && cached.sharedBody != nil {
				l.resourcePolicy.recordKnownAvoidance(cached.sharedBody.size - int64(len(cached.Body)))
			}
			l.resourcePolicy.recordCache(true)
			mode := decision.Work.Body
			if decision.ReportOnly {
				mode = "full"
			}
			l.resourcePolicy.recordBody(0, 0, mode)
		}
		l.trace.Add(trace.Network, "cacheHit", map[string]any{"id": r.ID, "url": r.URL.String()})
		cached.FromCache = true
		// The bytes/headers describe the stored representation; elapsed time and
		// transport phases belong to this retrieval, not its original download.
		cached.Duration = monotime.Since(requestStarted)
		cached.TransportTiming.Phases = nil
		cached.BrowserVisibleTiming = TransportTimingSnapshot{Phases: map[string]float64{
			"firstResponseByte": float64(cached.Duration) / float64(time.Millisecond),
			"responseComplete":  float64(cached.Duration) / float64(time.Millisecond),
		}}
		return l.after(ctx, r, cached)
	}
	if r.policySnapshot != nil {
		if decision.Work.Network != nil && !*decision.Work.Network && !decision.ReportOnly {
			l.resourcePolicy.recordBlocked(false)
			l.resourcePolicy.recordUnknownAvoidance()
			return Response{}, fmt.Errorf("net::ERR_BLOCKED_BY_CLIENT: resource policy rule %q", decision.RuleID)
		}
		if decision.Work.Network != nil && !*decision.Work.Network && decision.ReportOnly && !reportedWholeBlock {
			l.resourcePolicy.recordBlocked(true)
		}
	}
	releaseAcquisition := func() {}
	if r.policySnapshot != nil {
		var err error
		releaseAcquisition, err = l.resourcePolicy.beginAcquisition(r.policySnapshot)
		if err != nil {
			return Response{}, err
		}
		defer func() { releaseAcquisition() }()
	}
	attempt := l.session.BeginConnection(r.URL)
	timing := newTransportTiming(attempt.Origin, attempt.Key)
	start := timing.started
	req, err := http.NewRequestWithContext(ctx, r.Method, r.URL.String(), bytes.NewReader(r.Body))
	if err != nil {
		return Response{}, err
	}
	req.Header = r.Headers.Clone()
	req = req.WithContext(withBrowserHeaderLayout(req.Context(), r.Initiator, r.AuthorHeaderOrder))
	req = req.WithContext(withTransportTiming(httptrace.WithClientTrace(req.Context(), timing.standardTrace()), timing))
	raw, err := l.roundTrip(req)
	if err != nil {
		timing.mark("responseComplete")
		timingSnapshot := timing.snapshot()
		record := l.session.CompleteConnection(attempt, timingSnapshot, "", true)
		l.trace.Add(trace.Network, "transport", map[string]any{"id": r.ID, "url": r.URL.String(), "timing": timingSnapshot, "sessionCold": attempt.Cold, "connectionState": record.Status, "error": err.Error()})
		return Response{}, err
	}
	stopBodyCancellation := context.AfterFunc(ctx, func() { _ = raw.Body.Close() })
	bodyMode := decision.Work.Body
	if bodyMode == "" || decision.ReportOnly {
		bodyMode = "full"
	}
	bodyLimit := int64(32 << 20)
	if bodyMode == "none" {
		bodyLimit = 0
	}
	if bodyMode == "prefix" {
		bodyLimit = decision.Work.PrefixBytes
	}
	responseBudget := int64(0)
	responseBudgetReported := false
	if r.policySnapshot != nil && bodyMode == "full" {
		responseBudget = r.policySnapshot.config.Budgets.MaxResponseBytes
	}
	if responseBudget > 0 {
		if raw.ContentLength > responseBudget {
			l.resourcePolicy.recordBudgetExceeded()
			responseBudgetReported = true
			if !decision.ReportOnly {
				stopBodyCancellation()
				_ = raw.Body.Close()
				return Response{}, fmt.Errorf("resource policy: response size budget exceeded")
			}
		} else if !decision.ReportOnly && responseBudget < bodyLimit {
			bodyLimit = responseBudget + 1
		}
	}
	bodyReader := io.Reader(raw.Body)
	if r.policySnapshot != nil {
		bodyReader = &resourcePolicyBodyReader{state: l.resourcePolicy, policy: r.policySnapshot, source: bodyReader, expected: raw.ContentLength}
	}
	encodedBody, err := io.ReadAll(io.LimitReader(bodyReader, bodyLimit))
	if r.policySnapshot != nil {
		l.resourcePolicy.recordBody(0, int64(len(encodedBody)), bodyMode)
	}
	stopBodyCancellation()
	closeErr := raw.Body.Close()
	if contextErr := ctx.Err(); contextErr != nil {
		return Response{}, contextErr
	}
	if err != nil {
		return Response{}, err
	}
	if closeErr != nil {
		return Response{}, closeErr
	}
	if responseBudget > 0 && int64(len(encodedBody)) > responseBudget {
		if !responseBudgetReported {
			l.resourcePolicy.recordBudgetExceeded()
		}
		if !decision.ReportOnly {
			return Response{}, fmt.Errorf("resource policy: response size budget exceeded")
		}
	}
	encodedBodySize := raw.ContentLength
	if encodedBodySize < 0 {
		encodedBodySize = int64(len(encodedBody))
	}
	body := encodedBody
	if bodyMode == "full" {
		body, err = decodeContent(encodedBody, raw.Header.Get("Content-Encoding"))
		if err != nil {
			return Response{}, fmt.Errorf("decode %s response: %w", raw.Header.Get("Content-Encoding"), err)
		}
	}
	if r.policySnapshot != nil {
		if decision.ReportOnly {
			if decision.Work.Network != nil && !*decision.Work.Network {
				l.resourcePolicy.recordKnownAvoidance(int64(len(encodedBody)))
			} else if decision.Work.Body == "none" {
				l.resourcePolicy.recordKnownAvoidance(int64(len(encodedBody)))
			} else if decision.Work.Body == "prefix" {
				l.resourcePolicy.recordKnownAvoidance(max(0, int64(len(encodedBody))-decision.Work.PrefixBytes))
			}
		} else if bodyMode != "full" && raw.ContentLength >= 0 {
			l.resourcePolicy.recordKnownAvoidance(max(0, raw.ContentLength-int64(len(encodedBody))))
		}
	}
	timing.mark("responseComplete")
	timing.setResponseProtocol(raw.Proto)
	timingSnapshot := timing.snapshot()
	record := l.session.CompleteConnection(attempt, timingSnapshot, raw.Proto, raw.Close)
	l.trace.Add(trace.Network, "transport", map[string]any{"id": r.ID, "url": r.URL.String(), "timing": timingSnapshot, "sessionCold": attempt.Cold, "connectionState": record.Status})
	browserTiming := timingSnapshot.shifted(float64(start.Sub(r.operationStarted)) / float64(time.Millisecond))
	partial := bodyMode != "full" && (raw.ContentLength < 0 || int64(len(encodedBody)) < raw.ContentLength)
	if isRedirectStatus(raw.StatusCode) && raw.Header.Get("Location") != "" {
		partial = false
	}
	res := Response{Status: raw.StatusCode, Headers: raw.Header.Clone(), Body: body, URL: r.URL, Partial: partial, Duration: monotime.Since(start), EncodedBodySize: encodedBodySize, Protocol: raw.Proto, TransportTiming: timingSnapshot, BrowserVisibleTiming: browserTiming}
	res.policyOwner, res.policySnapshot = l.resourcePolicy, r.policySnapshot
	acceptedBefore := l.session.ClientHints(r.URL)
	l.acceptResponseState(r, res)
	if missing := criticalClientHintsForRestart(l.env(), r, res, acceptedBefore); len(missing) != 0 {
		releaseAcquisition()
		releaseAcquisition = func() {}
		l.trace.Add(trace.Network, "criticalClientHintsRestart", map[string]any{"id": r.ID, "url": r.URL.String(), "missing": missing, "connectionId": timingSnapshot.ConnectionID})
		r.criticalCHRestarted = true
		return l.Load(ctx, r)
	}
	cacheRetain := decision.ReportOnly || decision.Work.CacheRetain == nil || *decision.Work.CacheRetain
	if cacheRetain && !res.Partial {
		res, err = l.session.putCached(r, res, time.Now())
	}
	if err != nil {
		// Retaining an inspector/cache copy is independent of successful
		// resource delivery. Preserve the storage failure as a diagnostic.
		l.trace.Add(trace.Error, "responseBodyStorage", map[string]any{"id": r.ID, "error": err.Error(), "owner": "cache"})
	}
	releaseAcquisition()
	releaseAcquisition = func() {}
	return l.after(ctx, r, res)
}

// Both received and fulfilled responses update the context-owned session.
// Cache reads do not repeat these effects (notably Set-Cookie expiry/order).
func (l *Loader) acceptResponseState(r Request, res Response) {
	// Accept-CH is list-valued; all field lines contribute to the opt-in.
	l.session.AcceptClientHints(r.URL, strings.Join(res.Headers.Values("Accept-CH"), ","))
	if l.env().Network.CookiesEnabled && requestIncludesCredentials(r) {
		l.cookies.SetFromResponse(r.URL, res.Headers, r.cookieContext())
	}
}

func (l *Loader) cachedResponse(request Request, policy PolicySnapshot) (Response, bool, error) {
	return l.cachedResponseForPolicy(request, policy, true, -1)
}

func (l *Loader) cachedResponseForPolicy(request Request, policy PolicySnapshot, allowed bool, limit int64) (Response, bool, error) {
	// Chrome's cacheDisabled bypasses reads, while the successful network
	// response still refreshes the shared HTTP cache for other Pages.
	if policy.CacheDisabled || !allowed {
		return Response{}, false, nil
	}
	return l.session.getCachedLimited(request, time.Now(), limit)
}

func criticalClientHintsForRestart(environment state.Environment, request Request, response Response, acceptedBefore map[string]bool) []string {
	if request.criticalCHRestarted || request.Initiator != Navigation || request.URL == nil || request.URL.Scheme != "https" {
		return nil
	}
	accept := headerTokens(response.Headers.Values("Accept-CH"))
	critical := headerTokens(response.Headers.Values("Critical-CH"))
	if len(accept) == 0 || len(critical) == 0 {
		return nil
	}
	available := environment.ClientHintHeaders(accept)
	missing := make([]string, 0)
	for name := range available {
		lower := strings.ToLower(name)
		if critical[lower] && !acceptedBefore[lower] {
			missing = append(missing, name)
		}
	}
	slices.Sort(missing)
	return missing
}

func headerTokens(values []string) map[string]bool {
	tokens := map[string]bool{}
	for _, value := range values {
		for _, token := range strings.Split(value, ",") {
			if token = strings.ToLower(strings.TrimSpace(token)); token != "" {
				tokens[token] = true
			}
		}
	}
	return tokens
}

func decodeContent(encoded []byte, contentEncoding string) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(contentEncoding)) {
	case "", "identity":
		return encoded, nil
	case "gzip":
		reader, err := gzip.NewReader(bytes.NewReader(encoded))
		if err != nil {
			return nil, err
		}
		defer reader.Close()
		return io.ReadAll(io.LimitReader(reader, 32<<20))
	case "deflate":
		reader, err := zlib.NewReader(bytes.NewReader(encoded))
		if err == nil {
			defer reader.Close()
			return io.ReadAll(io.LimitReader(reader, 32<<20))
		}
		raw := flate.NewReader(bytes.NewReader(encoded))
		defer raw.Close()
		return io.ReadAll(io.LimitReader(raw, 32<<20))
	case "br":
		return io.ReadAll(io.LimitReader(brotli.NewReader(bytes.NewReader(encoded)), 32<<20))
	case "zstd":
		decoder, err := zstd.NewReader(nil, zstd.WithDecoderMaxMemory(64<<20))
		if err != nil {
			return nil, err
		}
		defer decoder.Close()
		return decoder.DecodeAll(encoded, nil)
	default:
		return nil, fmt.Errorf("unsupported content encoding %q", contentEncoding)
	}
}

func applyBrowserRequestHeaders(r *Request) {
	setDefault := func(name, value string) {
		if r.Headers.Get(name) == "" {
			r.Headers.Set(name, value)
		}
	}
	site := r.chainSite()
	source := r.initiatingURL()
	mode, destination := "no-cors", "empty"
	switch r.Initiator {
	case Navigation, Iframe:
		mode, destination = "navigate", "document"
		if source != nil && r.Method != http.MethodGet && r.Method != http.MethodHead {
			origin := source.Scheme + "://" + source.Host
			policy := strings.ToLower(r.ReferrerPolicy)
			downgrade := source.Scheme == "https" && r.URL.Scheme != "https"
			if r.OpaqueOrigin || r.redirectTaintedOrigin() || source.Scheme != "http" && source.Scheme != "https" || policy == "no-referrer" || policy == "same-origin" && !sameRequestOrigin(source, r.URL) || downgrade && (policy == "strict-origin" || policy == "strict-origin-when-cross-origin" || policy == "no-referrer-when-downgrade" || policy == "") {
				origin = "null"
			}
			setDefault("Origin", origin)
		}
		if r.Initiator == Iframe {
			destination = "iframe"
		}
		setDefault("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7")
		setDefault("Upgrade-Insecure-Requests", "1")
		if r.UserActivation {
			setDefault("Sec-Fetch-User", "?1")
		}
	case Worker:
		mode, destination = "same-origin", "worker"
		setDefault("Accept", "*/*")
	case Script:
		destination = "script"
		setDefault("Accept", "*/*")
	case Stylesheet:
		destination = "style"
		setDefault("Accept", "text/css,*/*;q=0.1")
	case Image:
		destination = "image"
		setDefault("Accept", "image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8")
	case Other:
		// The current browser-owned Other consumer is the fallback favicon.
		// CDP classifies that lookup as Other, while Fetch still treats it as
		// an image request on the wire.
		destination = "image"
		setDefault("Accept", "image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8")
	case Fetch, XHR:
		mode = "cors"
		setDefault("Accept", "*/*")
		if source != nil && r.Method != http.MethodGet && r.Method != http.MethodHead {
			origin := source.Scheme + "://" + source.Host
			if r.OpaqueOrigin || r.redirectTaintedOrigin() || source.Scheme == "file" || source.Scheme == "data" {
				origin = "null"
			}
			setDefault("Origin", origin)
		}
	}
	if r.Mode != "" {
		mode = r.Mode
	}
	if r.Destination != "" {
		destination = r.Destination
	}
	setDefault("Sec-Fetch-Dest", destination)
	setDefault("Sec-Fetch-Mode", mode)
	setDefault("Sec-Fetch-Site", site)
	switch r.Initiator {
	case Navigation, Iframe:
		setDefault("Priority", "u=0, i")
	case Stylesheet:
		setDefault("Priority", "u=0")
	case Fetch, XHR, Other:
		setDefault("Priority", "u=1, i")
	case Image:
		setDefault("Priority", "i")
	}
}
func (l *Loader) after(ctx context.Context, r Request, res Response) (Response, error) {
	res.policyOwner, res.policySnapshot = l.resourcePolicy, r.policySnapshot
	if r.policySnapshot != nil {
		d := r.policySnapshot.decide(r)
		res.DecodeDisallowed = !d.ReportOnly && d.Work.Decode != nil && !*d.Work.Decode
		if d.ReportOnly && res.Synthetic && d.Work.CacheRead != nil && !*d.Work.CacheRead && d.Work.Network != nil && !*d.Work.Network {
			l.resourcePolicy.recordKnownAvoidance(int64(len(res.Body)))
		}
	}
	res.Referrer = r.Headers.Get("Referer")
	interceptors := l.interceptorSnapshot()
	originalBody := res.sharedBody
	for n := len(interceptors) - 1; n >= 0; n-- {
		// After owns a mutable response and can change bytes in place. The HTTP
		// cache retains the original representation; inspector history retains
		// the final response. Do not reuse a pre-interception body hint.
		res.sharedBody = nil
		var err error
		res, err = interceptors[n].After(ctx, r, res)
		if err != nil {
			return Response{}, err
		}
	}
	if len(interceptors) != 0 && originalBody != nil {
		if originalBody.matches(res.Body) {
			res.sharedBody = originalBody
		}
	}
	encodedBodySize := res.EncodedBodySize
	if encodedBodySize <= 0 {
		encodedBodySize = int64(len(res.Body))
	}
	transferSize := encodedBodySize + 300
	if res.FromCache || res.Synthetic {
		transferSize = 0
	}
	performanceInitiatorType := r.PerformanceInitiatorType
	if performanceInitiatorType == "" {
		performanceInitiatorType = string(r.Initiator)
		switch r.Initiator {
		case Image:
			performanceInitiatorType = "img"
		case XHR:
			performanceInitiatorType = "xmlhttprequest"
		case Stylesheet:
			performanceInitiatorType = "link"
		}
	}
	l.trace.Add(trace.Network, "response", map[string]any{"id": r.ID, "url": r.URL.String(), "status": res.Status, "headers": headerStrings(res.Headers), "mimeType": strings.Split(res.Headers.Get("Content-Type"), ";")[0], "encodedDataLength": len(res.Body), "encodedBodySize": encodedBodySize, "decodedBodySize": len(res.Body), "transferSize": transferSize, "durationMs": float64(res.Duration) / float64(time.Millisecond), "protocol": res.Protocol, "transportTiming": res.TransportTiming, "browserVisibleTiming": res.BrowserVisibleTiming, "connectionReused": res.TransportTiming.Reused, "connectionId": res.TransportTiming.ConnectionID, "fromCache": res.FromCache, "partial": res.Partial, "initiator": r.Initiator, "performanceInitiatorType": performanceInitiatorType,
		"performanceURL": r.performanceURL(), "performanceRedirectEnd": r.redirectEnd,
		"performanceRedirectCount": r.redirectCount, "performanceTimingAllowFailed": r.performanceTimingAllowFailed(res.Headers),
		"performanceCORSAccessible": (r.Initiator == Fetch || r.Initiator == XHR) && r.Mode != "no-cors" && corsResponseAllowed(r, res.Headers), "synthetic": res.Synthetic, "context": r.ContextID, "performanceOwner": r.PerformanceOwner, "performanceStart": r.PerformanceStart})
	debugRetain := r.policySnapshot == nil || r.policySnapshot.config.ReportOnly || r.policySnapshot.decide(r).Work.DebugRetain == nil || *r.policySnapshot.decide(r).Work.DebugRetain
	if debugRetain && !res.Partial {
		res.policyOwner, res.policySnapshot = l.resourcePolicy, r.policySnapshot
		if err := l.remember(r.ID, res); err != nil {
			l.trace.Add(trace.Error, "responseBodyStorage", map[string]any{"id": r.ID, "error": err.Error(), "owner": "history"})
		}
	}
	if !res.Partial {
		l.trace.Add(trace.Resource, "loadEnd", map[string]any{"id": r.ID, "url": r.URL.String(), "status": res.Status, "type": r.Initiator})
	}
	if fetchCrossOrigin(r) && !r.corsPreflight && r.Mode != "no-cors" && !corsResponseAllowed(r, res.Headers) {
		return Response{}, fmt.Errorf("CORS response denied")
	}
	if isRedirectStatus(res.Status) {
		if r.Redirect == "manual" {
			return Response{Status: 0, Type: "opaqueredirect", URL: r.URL, Headers: make(http.Header)}, nil
		}
		if loc := res.Headers.Get("Location"); loc != "" {
			if r.Redirect == "error" {
				return Response{}, fmt.Errorf("redirect forbidden by request redirect mode")
			}
			if r.redirectCount >= 20 {
				return Response{}, fmt.Errorf("too many redirects")
			}
			u, err := r.URL.Parse(loc)
			if err != nil {
				return Response{}, err
			}
			if u.Scheme != "http" && u.Scheme != "https" {
				return Response{}, fmt.Errorf("redirect to unsupported scheme %q", u.Scheme)
			}
			if (r.Initiator == Fetch || r.Initiator == XHR) && u.User != nil {
				return Response{}, fmt.Errorf("redirect URL contains credentials")
			}
			if !strings.Contains(loc, "#") {
				u.Fragment, u.RawFragment = r.URL.Fragment, r.URL.RawFragment
			}
			r.Headers = r.Headers.Clone()
			for _, name := range []string{"Sec-CH-UA-Arch", "Sec-CH-UA-Bitness", "Sec-CH-UA-Full-Version", "Sec-CH-UA-Full-Version-List", "Sec-CH-UA-Model", "Sec-CH-UA-Platform-Version", "Sec-CH-UA-Form-Factors", "Sec-CH-UA-WoW64"} {
				r.Headers.Del(name)
			}
			if !sameRedirectOrigin(r.URL, u) {
				r.Headers.Del("Authorization")
				r.Headers.Del("Proxy-Authorization")
			}
			if (res.Status == 301 || res.Status == 302) && r.Method == http.MethodPost || res.Status == 303 && r.Method != http.MethodGet && r.Method != http.MethodHead {
				r.Method, r.Body = http.MethodGet, nil
				for _, name := range []string{"Content-Encoding", "Content-Language", "Content-Location", "Content-Type", "Content-Length"} {
					r.Headers.Del(name)
				}
			}
			// These fields belong to the new request, not to the previous hop.
			// In particular, do not forward the source host's cookie jar entry.
			for _, name := range []string{"Cookie", "Referer", "Origin", "Sec-Fetch-Site", "Sec-Fetch-Storage-Access"} {
				r.Headers.Del(name)
			}
			r.redirectChain(u)
			r.redirectEnd = float64(monotime.Since(r.operationStarted)) / float64(time.Millisecond)
			r.timingAllowFailed = r.timingAllowFailed || !requestTimingAllowed(r, res.Headers)
			r.URL = u
			r.redirectCount++
			return l.Load(ctx, r)
		}
	}
	res.Redirected = r.redirectCount > 0
	if res.Partial {
		return res, fmt.Errorf("resource policy: response body unavailable after %s", r.policySnapshot.decide(r).Work.Body)
	}
	return filterFetchResponse(r, res), nil
}

func isRedirectStatus(status int) bool {
	return status == 301 || status == 302 || status == 303 || status == 307 || status == 308
}

func sameRedirectOrigin(a, b *url.URL) bool {
	port := func(u *url.URL) string {
		if p := u.Port(); p != "" {
			return p
		}
		if strings.EqualFold(u.Scheme, "https") {
			return "443"
		}
		return "80"
	}
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Hostname(), b.Hostname()) && port(a) == port(b)
}
func headerStrings(h http.Header) map[string]string {
	out := make(map[string]string, len(h))
	for k, values := range h {
		out[k] = strings.Join(values, ", ")
	}
	return out
}
func (l *Loader) interceptorSnapshot() []Interceptor {
	l.interceptorsMu.RLock()
	defer l.interceptorsMu.RUnlock()
	return append([]Interceptor(nil), l.interceptors...)
}
func (l *Loader) remember(id string, res Response) error {
	body, err := l.session.responseBodies.retainResponse(res)
	l.completedMu.Lock()
	if l.completedClosed {
		l.completedMu.Unlock()
		body.release()
		return nil
	}
	copy := res
	copy.Body, copy.sharedBody = nil, body
	copy.bodyStorageErr = err
	copy.Headers = res.Headers.Clone()
	var dropped []*storedBody
	if old, ok := l.completed[id]; ok {
		dropped = append(dropped, old.sharedBody)
		l.completedOrder = slices.DeleteFunc(l.completedOrder, func(previous string) bool { return previous == id })
	}
	l.completed[id] = copy
	l.completedOrder = append(l.completedOrder, id)
	if len(l.completedOrder) > 128 {
		old := l.completedOrder[0]
		l.completedOrder = l.completedOrder[1:]
		dropped = append(dropped, l.completed[old].sharedBody)
		delete(l.completed, old)
	}
	l.completedMu.Unlock()
	for _, body := range dropped {
		body.release()
	}
	return err
}
func (l *Loader) Completed(id string) (Response, bool) {
	l.completedMu.RLock()
	r, ok := l.completed[id]
	retained := ok && r.bodyStorageErr == nil && r.sharedBody.retain()
	l.completedMu.RUnlock()
	if !retained {
		return Response{}, false
	}
	defer r.sharedBody.release()
	var err error
	r.Body, err = r.sharedBody.copyBytes()
	if err != nil {
		l.trace.Add(trace.Error, "responseBodyRead", map[string]any{"id": id, "error": err.Error()})
		return Response{}, false
	}
	r.Headers = r.Headers.Clone()
	r.sharedBody = nil
	return r, true
}

// CompletedURL returns the latest retained response for an absolute URL.
func (l *Loader) CompletedURL(rawURL string) (Response, bool) {
	l.completedMu.RLock()
	var response Response
	found := false
	for i := len(l.completedOrder) - 1; i >= 0; i-- {
		r, ok := l.completed[l.completedOrder[i]]
		if ok && r.URL != nil && r.URL.String() == rawURL {
			response = r
			found = r.bodyStorageErr == nil && r.sharedBody.retain()
			break
		}
	}
	l.completedMu.RUnlock()
	if !found {
		return Response{}, false
	}
	defer response.sharedBody.release()
	var err error
	response.Body, err = response.sharedBody.copyBytes()
	if err != nil {
		l.trace.Add(trace.Error, "responseBodyRead", map[string]any{"url": rawURL, "error": err.Error()})
		return Response{}, false
	}
	response.Headers = response.Headers.Clone()
	response.sharedBody = nil
	return response, true
}

// CompletedBody projects directly from retained immutable storage to CDP's
// string result without allocating an independent raw-body copy.
func (l *Loader) CompletedBody(id string) (string, bool, bool, error) {
	l.completedMu.RLock()
	response, found := l.completed[id]
	if found && response.bodyStorageErr != nil {
		l.completedMu.RUnlock()
		return "", false, true, response.bodyStorageErr
	}
	retained := found && response.sharedBody.retain()
	l.completedMu.RUnlock()
	if !retained {
		return "", false, false, nil
	}
	defer response.sharedBody.release()
	body, encoded, err := response.sharedBody.protocolBody()
	return body, encoded, true, err
}

// CloseResponseBodies releases Page-owned history even when the closed Page
// object remains reachable through its Context. Cache owners are independent.
func (l *Loader) CloseResponseBodies() {
	l.completedMu.Lock()
	l.completedClosed = true
	completed := l.completed
	l.completed = make(map[string]Response)
	l.completedOrder = nil
	l.completedMu.Unlock()
	for _, response := range completed {
		response.sharedBody.release()
	}
	if l.ownsSession {
		l.session.Close()
	}
}

// ReferrerValue is shared by the wire request and inline document navigation.
func (r Request) ReferrerValue() string {
	if r.Referrer == nil || r.URL == nil {
		return ""
	}
	referrer := *r.Referrer
	referrer.User = nil
	referrer.Fragment = ""
	referrer.RawFragment = ""
	policy := "strict-origin-when-cross-origin"
	for _, token := range strings.Split(r.ReferrerPolicy, ",") {
		switch strings.TrimSpace(strings.ToLower(token)) {
		case "no-referrer", "no-referrer-when-downgrade", "same-origin", "origin", "strict-origin", "origin-when-cross-origin", "strict-origin-when-cross-origin", "unsafe-url":
			policy = strings.TrimSpace(strings.ToLower(token))
		}
	}
	sameOrigin := referrer.Scheme == r.URL.Scheme && referrer.Host == r.URL.Host
	downgrade := referrer.Scheme == "https" && r.URL.Scheme != "https" && r.URL.Scheme != "about"
	suppress := policy == "no-referrer" || policy == "same-origin" && !sameOrigin || downgrade && (policy == "no-referrer-when-downgrade" || policy == "strict-origin" || policy == "strict-origin-when-cross-origin")
	if (referrer.Scheme == "http" || referrer.Scheme == "https") && !suppress {
		if policy == "origin" || policy == "strict-origin" || !sameOrigin && (policy == "origin-when-cross-origin" || policy == "strict-origin-when-cross-origin") {
			referrer.Path = "/"
			referrer.RawPath = ""
			referrer.RawQuery = ""
			referrer.ForceQuery = false
		}
		return referrer.String()
	}
	return ""
}

// CheckContentPolicy runs before reuse, preflight, interception or transport.
func (r Request) CheckContentPolicy() error {
	if !r.ContentPolicy.AllowsResource(r.ContentPolicyDirective, r.ContentPolicyURL, r.URL) {
		return errors.New("resource blocked by Content Security Policy")
	}
	return nil
}
