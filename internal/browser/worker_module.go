package browser

import (
	"context"
	"fmt"
	"mime"
	"net/url"
	"strings"

	"github.com/moreveal/mimic/internal/engine"
	"github.com/moreveal/mimic/internal/network"
	"github.com/moreveal/mimic/internal/scheduler"
)

// The worker owns its module map. Network goroutines only return immutable
// responses; compilation, graph traversal and completions run on its event loop.
// Unlike a document, a worker has no import map or document lifecycle hooks.
type workerModules struct {
	worker   *DedicatedWorker
	lifetime context.Context
	entries  map[string]*workerModule
}

type workerModule struct {
	response network.Response
	err      error
	ready    bool
	waiters  []func(*workerModule) error
}

func (w *DedicatedWorker) installModules(lifetime context.Context) error {
	prepared, ok := w.runtime.(engine.PreparedModuleRuntime)
	if !ok {
		if w.scriptType == "module" {
			return fmt.Errorf("engine does not support module workers")
		}
		return nil
	}
	w.modules = &workerModules{worker: w, lifetime: lifetime, entries: make(map[string]*workerModule)}
	if native, ok := w.runtime.(engine.ImportMetaRuntime); ok {
		factory := w.runtime.Get("__mimicImportMetaResolveFactory")
		err := native.SetImportMetaResolveFactory(factory)
		releaseRuntimeValues(w.runtime, factory)
		if err != nil {
			return err
		}
	}
	prepared.SetDynamicModuleHandler(func(specifier, referrer string, complete engine.DynamicModuleCompletion) {
		w.scheduler.Post(scheduler.Network, 0, func(ctx context.Context) error {
			if lifetime.Err() != nil || w.isClosed() {
				return nil
			}
			target, err := w.modules.resolve(specifier, referrer)
			if err != nil {
				return complete(ctx, "", "", nil, err)
			}
			w.modules.graph(target, func(err error) error {
				entry := w.modules.entries[target.String()]
				if err != nil {
					return complete(ctx, "", "", nil, err)
				}
				return complete(ctx, string(entry.response.Body), target.String(), w.modules.loaded, nil)
			})
			return nil
		})
		w.signal()
	})
	return nil
}

func (m *workerModules) resolve(specifier, referrer string) (*url.URL, error) {
	base, err := url.Parse(referrer)
	if err != nil {
		return nil, err
	}
	if entry := m.entries[referrer]; entry != nil && entry.response.URL != nil {
		base = entry.response.URL
	}
	if !strings.Contains(specifier, ":") && !strings.HasPrefix(specifier, "/") && !strings.HasPrefix(specifier, "./") && !strings.HasPrefix(specifier, "../") {
		return nil, fmt.Errorf("Failed to resolve module specifier %q", specifier)
	}
	return resolveURL(base, specifier)
}

func (m *workerModules) loaded(specifier, referrer string) (string, string, error) {
	target, err := m.resolve(specifier, referrer)
	if err != nil {
		return "", "", err
	}
	entry := m.entries[target.String()]
	if entry == nil || !entry.ready {
		return "", "", fmt.Errorf("module graph edge was not prepared: %s", target)
	}
	return string(entry.response.Body), target.String(), entry.err
}

func (m *workerModules) fetch(target, referrer *url.URL, callback func(*workerModule) error) {
	key := target.String()
	if entry := m.entries[key]; entry != nil {
		if entry.ready {
			if err := callback(entry); err != nil {
				_ = m.worker.reportError(err)
			}
		} else {
			entry.waiters = append(entry.waiters, callback)
		}
		return
	}
	entry := &workerModule{waiters: []func(*workerModule) error{callback}}
	m.entries[key] = entry
	w := m.worker
	request := network.Request{ContextID: w.parent.agent.ContextID(), URL: target, Referrer: referrer, SourceURL: w.securityURL,
		TopLevelURL: w.topLevelURL, HasCrossSiteAncestor: w.cookieContext.HasCrossSiteAncestor, Initiator: network.Script,
		OmitClientHints: true, ClientIsWorker: true, Mode: "cors", Credentials: w.credentials, EnforceCORS: true,
		Destination: "script", Owner: "worker", ContentPolicy: w.policy, ContentPolicyURL: w.url, ContentPolicyDirective: "script-src"}
	if key == w.url.String() && w.scriptType == "module" {
		request.Mode, request.Destination, request.Initiator = "same-origin", "worker", network.Worker
		request.ClientIsWorker = false
		request.Referrer = w.securityURL
		// The creator's worker-src admission is separate from the policy of
		// the fetched worker global that governs its dependencies.
		request.ContentPolicy, request.ContentPolicyURL, request.ContentPolicyDirective = w.parent.contentPolicy(), w.securityURL, "worker-src"
	}
	if w.url.Scheme == "blob" || w.url.Scheme == "data" {
		request.ContentPolicyURL = w.securityURL
	}
	w.fetchWG.Add(1)
	go func() {
		defer w.fetchWG.Done()
		response, err := w.parent.agent.Page().loader.Load(m.lifetime, request)
		if err == nil {
			err = scriptResponseError(response)
		}
		if err == nil {
			contentType, _, parseErr := mime.ParseMediaType(response.Headers.Get("Content-Type"))
			if parseErr != nil || !javascriptMIME(contentType) {
				err = fmt.Errorf("module script has invalid JavaScript MIME type %q", response.Headers.Get("Content-Type"))
			}
		}
		if m.lifetime.Err() != nil {
			return
		}
		w.scheduler.Post(scheduler.Network, 0, func(context.Context) error {
			if m.lifetime.Err() != nil || w.isClosed() {
				return nil
			}
			entry.response, entry.err, entry.ready = response, err, true
			waiters := entry.waiters
			entry.waiters = nil
			for _, waiter := range waiters {
				if err := waiter(entry); err != nil {
					_ = w.reportError(err)
				}
			}
			return nil
		})
		w.signal()
	}()
}

func javascriptMIME(value string) bool {
	switch strings.ToLower(value) {
	case "application/ecmascript", "application/javascript", "application/x-ecmascript", "application/x-javascript",
		"text/ecmascript", "text/javascript", "text/javascript1.0", "text/javascript1.1", "text/javascript1.2",
		"text/javascript1.3", "text/javascript1.4", "text/javascript1.5", "text/jscript", "text/livescript",
		"text/x-ecmascript", "text/x-javascript":
		return true
	}
	return false
}

func (m *workerModules) graph(target *url.URL, complete func(error) error) {
	visited := make(map[string]bool)
	pending, finished := 0, false
	var graphErr error
	var visit func(*url.URL, *url.URL)
	visit = func(target, referrer *url.URL) {
		key := target.String()
		if visited[key] {
			return
		}
		visited[key] = true
		pending++
		m.fetch(target, referrer, func(entry *workerModule) error {
			if finished {
				return nil
			}
			if entry.err == nil {
				if native, ok := m.worker.runtime.(engine.ModuleSourceURLRuntime); ok && entry.response.URL != nil {
					entry.err = native.SetModuleSourceURL(key, entry.response.URL.String())
				}
			}
			if entry.err == nil {
				imports, err := m.worker.runtime.(engine.PreparedModuleRuntime).PrepareModule(m.lifetime, string(entry.response.Body), key)
				entry.err = err
				for _, specifier := range imports {
					dependency, err := m.resolve(specifier, key)
					if err != nil {
						entry.err = err
						break
					}
					visit(dependency, entry.response.URL)
				}
			}
			if entry.err != nil && graphErr == nil {
				graphErr = entry.err
			}
			pending--
			if pending == 0 {
				finished = true
				return complete(graphErr)
			}
			return nil
		})
	}
	visit(target, m.worker.url)
}

func (w *DedicatedWorker) startModule(ctx context.Context, source string) {
	entryURL := w.url
	if source != "" {
		w.modules.entries[entryURL.String()] = &workerModule{ready: true, response: network.Response{URL: entryURL, Body: []byte(source)}}
	}
	w.modules.fetch(entryURL, w.securityURL, func(entry *workerModule) error {
		if entry.err != nil {
			return w.reportError(entry.err)
		}
		if entry.response.URL != nil {
			w.url = entry.response.URL
		}
		if entryURL.Scheme != "blob" && entryURL.Scheme != "data" {
			w.policy = parseResponseCSP(entry.response.Headers)
		}
		w.modules.graph(entryURL, func(err error) error {
			if err != nil {
				return w.reportError(err)
			}
			runtime, ok := w.runtime.(engine.ModuleRuntime)
			if !ok {
				return fmt.Errorf("engine does not support module workers")
			}
			w.moduleEvaluation, err = runtime.EvalModule(ctx, string(entry.response.Body), entryURL.String(), w.modules.loaded)
			if err != nil {
				return w.reportError(err)
			}
			// Chrome dispatches messages while evaluation is suspended on TLA.
			w.enableMessages()
			return nil
		})
		return nil
	})
}

func (w *DedicatedWorker) checkModuleEvaluation() {
	if w.moduleEvaluation == nil {
		return
	}
	result, settled, err := w.runtime.Await(w.moduleEvaluation)
	if !settled && err == nil {
		return
	}
	releaseRuntimeValues(w.runtime, result, w.moduleEvaluation)
	w.moduleEvaluation = nil
	if err != nil {
		_ = w.reportError(err)
	}
}
