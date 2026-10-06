package browser

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/moreveal/mimic/internal/network"
	"github.com/moreveal/mimic/internal/scheduler"
)

type scriptFetch struct {
	done     chan struct{}
	response network.Response
	err      error
}

// The realm's module map shares fetches (including failures), independently of
// HTTP caching. Only network work runs concurrently; linking, evaluation and
// observable events remain on the Page's existing event loop.
func (r *Realm) fetchModule(request network.Request) *scriptFetch {
	if r.moduleFetches == nil {
		r.moduleFetches = make(map[string]*scriptFetch)
	}
	key := request.URL.String()
	if pending := r.moduleFetches[key]; pending != nil {
		return pending
	}
	pending := r.fetchScript(r.resourceContext, request)
	r.moduleFetches[key] = pending
	return pending
}

// A fetch owns only its immutable result. Classic script fetches do not enter
// the module map: identical URLs can represent separate classic executions.
func (r *Realm) fetchScript(ctx context.Context, request network.Request) *scriptFetch {
	pending := &scriptFetch{done: make(chan struct{})}
	r.resourceWG.Add(1)
	go func() {
		defer r.resourceWG.Done()
		defer close(pending.done)
		pending.response, pending.err = r.loadResource(ctx, request)
		if pending.err == nil {
			pending.err = scriptResponseError(pending.response)
		}
	}()
	return pending
}

func (m *scriptFetch) wait(ctx context.Context) (network.Response, error) {
	select {
	case <-ctx.Done():
		return network.Response{}, ctx.Err()
	case <-m.done:
		return m.response, m.err
	}
}

func (r *Realm) moduleRequest(target, referrer *url.URL) network.Request {
	headers := make(http.Header)
	headers.Set("Origin", r.origin)
	request := network.Request{ContextID: r.agent.ContextID(), URL: target, Referrer: referrer, SourceURL: referrer, Initiator: network.Script, Mode: "cors", Credentials: "same-origin", Headers: headers}
	if referrer != nil {
		request.CauseURL = referrer.String()
	}
	r.applyClientHints(&request)
	return request
}

func (r *Realm) preloadModules() {
	base := r.documentURL()
	if r.preloadedModuleLinks == nil {
		r.preloadedModuleLinks = map[int64]bool{}
	}
	for _, link := range r.document.FindAllByTagName("link") {
		if r.preloadedModuleLinks[link.ID] {
			continue
		}
		if !hasLinkRelation(link.Attributes["rel"], "modulepreload") || link.Attributes["href"] == "" {
			continue
		}
		if as := link.Attributes["as"]; as != "" && as != "script" {
			continue
		}
		target, err := base.Parse(link.Attributes["href"])
		if err != nil || !r.allowsScript(target, false, false, link.Attributes["nonce"]) {
			continue
		}
		request := r.moduleRequest(target, base)
		if strings.EqualFold(link.Attributes["crossorigin"], "use-credentials") {
			request.Credentials = "include"
		}
		r.preloadedModuleLinks[link.ID] = true
		request.PerformanceInitiatorType = "link"
		request.Mechanism = "preload"
		if !r.agent.Page().loader.SpeculationAllowed(r.withResourceTiming(request)) {
			id := link.ID
			r.scheduler.Post(scheduler.Network, 0, func(ctx context.Context) error { return r.dispatchResourceEvent(ctx, id, "error") })
			continue
		}
		pending := r.fetchModule(request)
		id := link.ID
		r.resourceWG.Add(1)
		go func() {
			defer r.resourceWG.Done()
			_, err := pending.wait(r.resourceContext)
			if r.resourceContext.Err() != nil {
				return
			}
			kind := "load"
			if err != nil {
				kind = "error"
			}
			r.scheduler.Post(scheduler.Network, 0, func(ctx context.Context) error { return r.dispatchResourceEvent(ctx, id, kind) })
		}()
	}
}
