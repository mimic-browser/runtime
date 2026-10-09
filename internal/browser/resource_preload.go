package browser

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/moreveal/mimic/internal/imageresource"
	"github.com/moreveal/mimic/internal/network"
	"github.com/moreveal/mimic/internal/scheduler"
)

type preloadKey struct{ url, destination, mode, credentials string }

type resourcePreload struct {
	done     chan struct{}
	response network.Response
	err      error
}

func preloadRequestKey(request network.Request) preloadKey {
	u := *request.URL
	u.Fragment, u.RawFragment = "", ""
	destination := request.Destination
	if destination == "" {
		switch request.Initiator {
		case network.Image:
			destination = "image"
		case network.Script:
			destination = "script"
		case network.Stylesheet:
			destination = "style"
		}
	}
	mode, credentials := request.Mode, request.Credentials
	if mode == "" {
		mode = "no-cors"
		if request.Initiator == network.Fetch || request.Initiator == network.XHR {
			mode = "cors"
		}
	}
	if credentials == "" {
		credentials = "include"
		if mode == "cors" {
			credentials = "same-origin"
		}
	}
	return preloadKey{u.String(), destination, mode, credentials}
}

func (r *Realm) elementRequest(u *url.URL, attributes map[string]string, initiator network.Initiator) network.Request {
	referrer := r.documentURL()
	if strings.EqualFold(attributes["referrerpolicy"], "no-referrer") {
		referrer = nil
	}
	headers := make(http.Header)
	mode, credentials := "no-cors", "include"
	if crossOrigin, present := attributes["crossorigin"]; present {
		mode = "cors"
		headers.Set("Origin", r.origin)
		if !strings.EqualFold(crossOrigin, "use-credentials") {
			credentials = "same-origin"
		}
	}
	request := network.Request{Credentials: credentials, ContextID: r.agent.ContextID(), URL: u, Referrer: referrer, SourceURL: r.documentURL(), Headers: headers, Initiator: initiator, Mode: mode}
	if initiator == network.Image {
		request.ContentPolicyDirective = "img-src"
		request.ContentPolicy = r.contentPolicy()
		request.ContentPolicyURL = r.documentURL()
	}
	r.applyClientHints(&request)
	return request
}

// The document owns preloads independently of consumers and of the HTTP cache.
// Only this small per-document registry is locked, never transport work or the
// event loop. Cancellation of a consumer stops its wait, not the link's fetch.
// Modules retain their separate module-map identity but use this same load path.
func (r *Realm) preloadResource(id int64, attributes map[string]string) {
	if r.preloadedLinks == nil {
		r.preloadedLinks = make(map[int64]bool)
	}
	if r.preloadedLinks[id] || attributes["href"] == "" {
		return
	}
	u, err := r.resolveDocument(attributes["href"])
	if err != nil {
		return
	}
	var initiator network.Initiator
	switch strings.ToLower(attributes["as"]) {
	case "image":
		initiator = network.Image
	case "script":
		initiator = network.Script
	case "style":
		initiator = network.Stylesheet
	case "fetch":
		initiator = network.Fetch
	default:
		return // No consumer for this destination in the runtime yet.
	}
	if initiator == network.Script && !r.allowsScript(u, false, false, attributes["nonce"]) {
		return
	}
	r.preloadedLinks[id] = true
	request := r.elementRequest(u, attributes, initiator)
	request.Mechanism = "preload"
	request.PerformanceInitiatorType = "link"
	if !r.agent.Page().loader.SpeculationAllowed(r.withResourceTiming(request)) {
		r.scheduler.Post(scheduler.Network, 0, func(ctx context.Context) error { return r.dispatchResourceEvent(ctx, id, "error") })
		return
	}
	if r.preloadContext == nil {
		r.preloadContext, r.cancelPreloads = context.WithCancel(r.resourceContext)
	}
	preloadContext := r.preloadContext
	pending := r.startResourcePreload(preloadContext, request)
	r.resourceWG.Add(1)
	go func() {
		defer r.resourceWG.Done()
		response, err := pending.wait(preloadContext)
		if preloadContext.Err() != nil {
			return
		}
		_, corsErr := resourceResponseOrigin(request, response)
		kind := "load"
		if initiator == network.Image && err == nil {
			_, err = imageresource.New(response.Body, response.Headers.Get("Content-Type")).RequireValidatedImage()
		}
		if err != nil || corsErr != nil || initiator != network.Image && (response.Status < 200 || response.Status >= 300) {
			kind = "error"
		}
		r.scheduler.Post(scheduler.Network, 0, func(ctx context.Context) error {
			if preloadContext.Err() != nil {
				return nil
			}
			r.notifyPerformanceObservers(ctx)
			return r.dispatchResourceEvent(ctx, id, kind)
		})
	}()
}

func (r *Realm) resetPreloads() {
	if r.cancelPreloads != nil {
		r.cancelPreloads()
	}
	r.preloadContext, r.cancelPreloads = nil, nil
	r.preloadedLinks = nil
	r.preloadsMu.Lock()
	r.preloads = nil
	r.preloadsMu.Unlock()
}

func (p *resourcePreload) wait(ctx context.Context) (network.Response, error) {
	select {
	case <-ctx.Done():
		return network.Response{}, ctx.Err()
	case <-p.done:
		return p.response, p.err
	}
}

func (r *Realm) consumePreload(request network.Request) *resourcePreload {
	if request.Method != "" && request.Method != http.MethodGet || len(request.Body) != 0 || request.Redirect != "" && request.Redirect != "follow" {
		return nil
	}
	key := preloadRequestKey(request)
	r.preloadsMu.Lock()
	defer r.preloadsMu.Unlock()
	pending := r.preloads[key]
	delete(r.preloads, key)
	return pending
}

func (r *Realm) loadResource(ctx context.Context, request network.Request) (network.Response, error) {
	if err := ctx.Err(); err != nil {
		return network.Response{}, err
	}
	if err := request.CheckContentPolicy(); err != nil {
		return network.Response{}, err
	}
	if r.agent.Page().loader.ResourceReuseAllowed(request) {
		if pending := r.consumePreload(request); pending != nil {
			return pending.wait(ctx)
		}
	}
	return r.agent.Page().loader.Load(ctx, r.withResourceTiming(request))
}

func (r *Realm) preloadResources() {
	for _, link := range r.document.FindAllByTagName("link") {
		if hasLinkRelation(link.Attributes["rel"], "preload") {
			r.preloadResource(link.ID, link.Attributes)
		}
	}
}

// Pending responses belong to this Document. Only transport work runs outside
// the Page loop; demand consumes the same immutable result and still performs
// its ordinary admission, response validation and execution lifecycle.
func (r *Realm) startResourcePreload(preloadContext context.Context, request network.Request) *resourcePreload {
	key := preloadRequestKey(request)
	r.preloadsMu.Lock()
	if r.preloads == nil {
		r.preloads = make(map[preloadKey]*resourcePreload)
	}
	pending := r.preloads[key]
	if pending == nil {
		pending = &resourcePreload{done: make(chan struct{})}
		r.preloads[key] = pending
		r.resourceWG.Add(1)
		go func() {
			defer r.resourceWG.Done()
			defer close(pending.done)
			pending.response, pending.err = r.agent.Page().loader.Load(preloadContext, r.withResourceTiming(request))
		}()
	}
	r.preloadsMu.Unlock()
	return pending
}
