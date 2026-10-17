package cdp

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/moreveal/mimic/internal/network"
)

type interceptAnswer struct {
	action  string
	url     string
	method  string
	headers http.Header
	body    []byte
	status  int
	// nil preserves the configured response-stage patterns. An explicit value
	// overrides those patterns for this request, as Fetch.continueRequest does.
	interceptResponse *bool
}
type interceptionPattern struct {
	url          *regexp.Regexp
	resourceType string
	stage        string
}
type interceptPending struct {
	answer   chan interceptAnswer
	fetch    bool
	response *network.Response
}
type responseOverride struct {
	enabled bool
	stop    func() bool
}
type ControlInterceptor struct {
	mu                sync.Mutex
	fetchEnabled      bool
	networkEnabled    bool
	emit              func(string, any)
	fetchPatterns     []interceptionPattern
	networkPatterns   []interceptionPattern
	pending           map[string]*interceptPending
	responseOverrides map[string]*responseOverride
}

func NewControlInterceptor(emit func(string, any)) *ControlInterceptor {
	return &ControlInterceptor{emit: emit, pending: map[string]*interceptPending{}, responseOverrides: map[string]*responseOverride{}}
}
func (i *ControlInterceptor) Enable(v bool) {
	if v {
		_ = i.ConfigureFetch(nil)
		return
	}
	i.mu.Lock()
	i.fetchEnabled = false
	i.fetchPatterns = nil
	pending := i.releaseLocked(true)
	for id, override := range i.responseOverrides {
		override.stop()
		delete(i.responseOverrides, id)
	}
	i.mu.Unlock()
	resumeInterceptions(pending, "continue")
}
func (i *ControlInterceptor) EnableNetwork(v bool) {
	if v {
		_ = i.ConfigureNetwork([]any{map[string]any{"urlPattern": "*"}})
	} else {
		_ = i.ConfigureNetwork(nil)
	}
}

// ConfigureFetch distinguishes an omitted pattern list (all request-stage
// requests) from an explicit empty list (no requests). Both were measured on
// frozen Chrome 152 by compatibility/cdp_automation_interception.py.
func (i *ControlInterceptor) ConfigureFetch(patterns []any) error {
	if patterns == nil {
		patterns = []any{map[string]any{}}
	}
	compiled, err := compileInterceptionPatterns(patterns, "requestStage")
	if err != nil {
		return err
	}
	i.mu.Lock()
	i.fetchEnabled = true
	i.fetchPatterns = compiled
	i.mu.Unlock()
	return nil
}
func (i *ControlInterceptor) ConfigureNetwork(patterns []any) error {
	compiled, err := compileInterceptionPatterns(patterns, "interceptionStage")
	if err != nil {
		return err
	}
	i.mu.Lock()
	i.networkEnabled = len(compiled) != 0
	i.networkPatterns = compiled
	var pending []*interceptPending
	if !i.networkEnabled {
		pending = i.releaseLocked(false)
	}
	i.mu.Unlock()
	resumeInterceptions(pending, "continue")
	return nil
}

func compileInterceptionPatterns(patterns []any, stageKey string) ([]interceptionPattern, error) {
	compiled := make([]interceptionPattern, 0, len(patterns))
	for _, item := range patterns {
		pattern, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid interception pattern")
		}
		urlPattern, present := pattern["urlPattern"].(string)
		if !present {
			urlPattern = "*"
		}
		stage := stringValue(pattern[stageKey])
		if stage == "" {
			stage = "Request"
		}
		switch stage {
		case "Request":
			stage = "request"
		case "Response":
			if stageKey != "requestStage" {
				return nil, fmt.Errorf("invalid interception stage %q", stage)
			}
			stage = "response"
		case "HeadersReceived":
			if stageKey != "interceptionStage" {
				return nil, fmt.Errorf("invalid interception stage %q", stage)
			}
			stage = "response"
		default:
			return nil, fmt.Errorf("invalid interception stage %q", stage)
		}
		compiled = append(compiled, interceptionPattern{url: interceptionGlob(urlPattern), resourceType: stringValue(pattern["resourceType"]), stage: stage})
	}
	return compiled, nil
}

func interceptionGlob(pattern string) *regexp.Regexp {
	var expression strings.Builder
	expression.WriteString("(?s)^")
	escaped := false
	for _, character := range pattern {
		if escaped {
			expression.WriteString(regexp.QuoteMeta(string(character)))
			escaped = false
			continue
		}
		switch character {
		case '\\':
			escaped = true
		case '*':
			expression.WriteString(".*")
		case '?':
			expression.WriteByte('.')
		default:
			expression.WriteString(regexp.QuoteMeta(string(character)))
		}
	}
	if escaped {
		expression.WriteString(`\\`)
	}
	expression.WriteByte('$')
	return regexp.MustCompile(expression.String())
}

func matchesInterception(patterns []interceptionPattern, stage string, req network.Request) bool {
	if req.URL == nil {
		return false
	}
	for _, pattern := range patterns {
		if pattern.stage == stage && (pattern.resourceType == "" || pattern.resourceType == resourceType(req.Initiator)) && pattern.url.MatchString(req.URL.String()) {
			return true
		}
	}
	return false
}

func (i *ControlInterceptor) releaseLocked(fetch bool) []*interceptPending {
	var pending []*interceptPending
	for id, request := range i.pending {
		if request.fetch == fetch {
			pending = append(pending, request)
			delete(i.pending, id)
		}
	}
	return pending
}
func resumeInterceptions(pending []*interceptPending, action string) {
	for _, request := range pending {
		request.answer <- interceptAnswer{action: action}
	}
}
func (i *ControlInterceptor) Close() {
	i.mu.Lock()
	i.fetchEnabled = false
	i.networkEnabled = false
	pending := append(i.releaseLocked(true), i.releaseLocked(false)...)
	for id, override := range i.responseOverrides {
		override.stop()
		delete(i.responseOverrides, id)
	}
	i.mu.Unlock()
	// Detaching a debugger releases its pauses; the Page/network context owns
	// cancellation when the page itself is closing.
	resumeInterceptions(pending, "continue")
}
func (i *ControlInterceptor) pause(ctx context.Context, stage string, req network.Request, res *network.Response) (interceptAnswer, error) {
	if err := ctx.Err(); err != nil {
		return interceptAnswer{}, err
	}
	// Local data: decoding does not enter Chrome's network interception stages.
	// The loader bypasses Before for these resources but still calls After.
	if req.URL != nil && req.URL.Scheme == "data" {
		return interceptAnswer{action: "continue"}, nil
	}
	i.mu.Lock()
	fetch := i.fetchEnabled && matchesInterception(i.fetchPatterns, stage, req)
	if stage == "response" {
		if override := i.responseOverrides[req.ID]; override != nil {
			fetch = i.fetchEnabled && override.enabled
			override.stop()
			delete(i.responseOverrides, req.ID)
		}
	}
	legacy := i.networkEnabled && matchesInterception(i.networkPatterns, stage, req)
	if !fetch && !legacy {
		i.mu.Unlock()
		return interceptAnswer{action: "continue"}, nil
	}
	id := uuid.NewString()
	if req.ID != "" {
		// Fetch uses one interception identity at both request and response
		// stages. Derive it from the loader's request identity rather than
		// retaining a second request-lifetime registry solely for ID mapping.
		id = "interception-" + req.ID
	}
	pending := &interceptPending{answer: make(chan interceptAnswer, 1), fetch: fetch, response: res}
	i.pending[id] = pending
	i.mu.Unlock()
	defer func() {
		i.mu.Lock()
		if i.pending[id] == pending {
			delete(i.pending, id)
		}
		i.mu.Unlock()
	}()
	requestData := map[string]any{"url": req.URL.String(), "method": req.Method, "headers": flatHeaders(req.Headers), "postData": ""}
	setRequestPostData(requestData, req.Body)
	if !fetch {
		params := map[string]any{"interceptionId": id, "request": requestData, "frameId": req.ContextID, "resourceType": resourceType(req.Initiator), "isNavigationRequest": req.Initiator == network.Navigation || req.Initiator == network.Iframe, "isDownload": false}
		if req.ID != "" {
			params["requestId"] = req.ID
		}
		if res != nil {
			params["responseStatusCode"] = res.Status
			params["responseHeaders"] = flatHeaders(res.Headers)
		}
		i.emit("Network.requestIntercepted", params)
	} else {
		params := map[string]any{"requestId": id, "request": requestData, "resourceType": resourceType(req.Initiator), "frameId": req.ContextID}
		if req.ID != "" {
			params["networkId"] = req.ID
		}
		if res != nil {
			params["responseStatusCode"] = res.Status
			params["responseStatusText"] = http.StatusText(res.Status)
			params["responseHeaders"] = interceptionHeaders(res.Headers)
		}
		i.emit("Fetch.requestPaused", params)
	}
	select {
	case a := <-pending.answer:
		return a, nil
	case <-ctx.Done():
		return interceptAnswer{}, ctx.Err()
	}
}
func (i *ControlInterceptor) Before(ctx context.Context, req network.Request) (network.Decision, error) {
	a, err := i.pause(ctx, "request", req, nil)
	if err != nil {
		return network.Decision{}, err
	}
	switch a.action {
	case "fail":
		return network.Decision{Block: fmt.Errorf("request blocked by controller")}, nil
	case "fulfill":
		return network.Decision{Response: &network.Response{Status: a.status, Headers: a.headers, Body: a.body, URL: req.URL, Synthetic: true}}, nil
	}
	if a.interceptResponse != nil && req.ID != "" {
		i.mu.Lock()
		if i.fetchEnabled && ctx.Err() == nil {
			if previous := i.responseOverrides[req.ID]; previous != nil {
				previous.stop()
			}
			override := &responseOverride{enabled: *a.interceptResponse}
			override.stop = context.AfterFunc(ctx, func() {
				i.mu.Lock()
				if i.responseOverrides[req.ID] == override {
					delete(i.responseOverrides, req.ID)
				}
				i.mu.Unlock()
			})
			i.responseOverrides[req.ID] = override
		}
		i.mu.Unlock()
	}
	copy := req
	if a.url != "" {
		u, err := url.Parse(a.url)
		if err != nil {
			return network.Decision{}, err
		}
		copy.URL = u
	}
	if a.method != "" {
		copy.Method = a.method
	}
	if a.headers != nil {
		copy.Headers = a.headers
	}
	if a.body != nil {
		copy.Body = a.body
	}
	return network.Decision{Request: &copy}, nil
}
func (i *ControlInterceptor) After(ctx context.Context, req network.Request, res network.Response) (network.Response, error) {
	a, err := i.pause(ctx, "response", req, &res)
	if err != nil {
		return res, err
	}
	if a.action == "fail" {
		return res, fmt.Errorf("response blocked by controller")
	}
	if a.action == "fulfill" || a.action == "continueResponse" {
		if a.status != 0 {
			res.Status = a.status
		}
		if a.headers != nil {
			res.Headers = a.headers
		}
		if a.body != nil {
			res.Body = a.body
		}
		if a.action == "fulfill" {
			res.Synthetic = true
		}
	}
	return res, nil
}
func flatHeaders(h http.Header) map[string]string {
	out := map[string]string{}
	for k, v := range h {
		out[k] = strings.Join(v, ", ")
	}
	return out
}
func resourceType(i network.Initiator) string {
	switch i {
	case network.Navigation, network.Iframe:
		return "Document"
	case network.Script:
		return "Script"
	case network.Stylesheet:
		return "Stylesheet"
	case network.Fetch:
		return "Fetch"
	case network.XHR:
		return "XHR"
	case network.Image:
		return "Image"
	}
	return "Other"
}
func (i *ControlInterceptor) Resolve(id string, a interceptAnswer) error {
	i.mu.Lock()
	pending := i.pending[id]
	delete(i.pending, id)
	i.mu.Unlock()
	if pending == nil {
		return fmt.Errorf("unknown interception %q", id)
	}
	pending.answer <- a
	return nil
}

// ResponseBody observes the bytes held by the loader at a response-stage pause.
// It does not create a second body cache or expose bodies after the pause ends.
func (i *ControlInterceptor) ResponseBody(id string) ([]byte, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	pending := i.pending[id]
	if pending == nil || !pending.fetch {
		return nil, fmt.Errorf("unknown interception %q", id)
	}
	if pending.response == nil {
		return nil, fmt.Errorf("response body is only available at the response stage")
	}
	return append([]byte{}, pending.response.Body...), nil
}

func interceptionHeaders(headers http.Header) []map[string]string {
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	entries := make([]map[string]string, 0, len(keys))
	for _, key := range keys {
		for _, value := range headers[key] {
			entries = append(entries, map[string]string{"name": key, "value": value})
		}
	}
	return entries
}
func decodeBody(s string) []byte {
	if s == "" {
		return nil
	}
	b, _ := base64.StdEncoding.DecodeString(s)
	return b
}

func bodyParameter(p map[string]any, key string) []byte {
	if raw, ok := p[key].(string); ok {
		if raw == "" {
			return []byte{}
		}
		return decodeBody(raw)
	}
	return nil
}
