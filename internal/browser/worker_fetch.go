package browser

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/moreveal/mimic/internal/engine"
	"github.com/moreveal/mimic/internal/scheduler"
)

func (w *DedicatedWorker) installFetch(host map[string]any, lifetime context.Context) {
	w.fetchCancels = make(map[string]context.CancelFunc)
	function := w.runtime.Function
	if runtime, ok := w.runtime.(interface{ TransientFunction(engine.Function) any }); ok {
		function = runtime.TransientFunction
	}
	host["fetch"] = function(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		promise := newHostPromise(w.runtime)
		target, err := resolveURL(w.url, strarg(args, 0))
		if err != nil {
			_ = promise.Reject(err.Error())
			return promise.Value, nil
		}
		request := fetchRequest(w.parent.agent.ContextID(), target, w.url, args)
		request.ContentPolicyDirective = "connect-src"
		request.ContentPolicy = w.policy
		request.ContentPolicyURL = w.url
		if w.url.Scheme == "blob" || w.url.Scheme == "data" {
			request.ContentPolicyURL = w.securityURL
		}
		request.Owner = "worker"
		request.OmitClientHints = true
		request.ClientIsWorker = true
		request.PerformanceOwner = fmt.Sprintf("%s/worker/%d", w.parent.ID, w.id)
		request.PerformanceStart = w.scheduler.Now()
		start, clock, scale := request.PerformanceStart, w.scheduler, w.parent.agent.Page().environmentView().Time.NetworkScale
		request.ObservePerformanceCompletion = func(completedMillis float64) {
			clock.AdvanceTo(start.Add(time.Duration(completedMillis * scale * float64(time.Millisecond))))
		}
		request.TopLevelURL = w.topLevelURL
		request.HasCrossSiteAncestor = w.cookieContext.HasCrossSiteAncestor
		if w.url.Scheme == "blob" {
			request.SourceOrigin, _ = url.Parse(w.parent.origin)
			request.OpaqueOrigin = w.parent.origin == "null"
		}
		requestID := strarg(args, 4)
		ctx, cancel := context.WithCancel(lifetime)
		if requestID != "" {
			w.fetchCancels[requestID] = cancel
		}
		w.scheduler.Post(scheduler.Network, 0, func(context.Context) error {
			if lifetime.Err() != nil || w.isClosed() {
				cancel()
				delete(w.fetchCancels, requestID)
				return nil
			}
			w.fetchWG.Add(1)
			go func() {
				defer w.fetchWG.Done()
				response, loadErr := w.parent.agent.Page().loader.Load(ctx, request)
				cancel()
				if lifetime.Err() != nil {
					return
				}
				w.scheduler.Post(scheduler.Network, 0, func(context.Context) error {
					delete(w.fetchCancels, requestID)
					if lifetime.Err() != nil || w.isClosed() {
						return nil
					}
					if loadErr != nil {
						return promise.Reject(loadErr.Error())
					}
					w.performance.sync()
					return promise.Resolve(fetchResponse(response))
				})
				w.signal()
			}()
			return nil
		})
		w.signal()
		return promise.Value, nil
	})
	host["abortFetch"] = function(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		if cancel := w.fetchCancels[strarg(args, 0)]; cancel != nil {
			cancel()
		}
		return nil, nil
	})
}

// The worker runner calls this before closing its runtime. Transport goroutines
// never settle JS promises directly, and no request survives worker teardown.
func (w *DedicatedWorker) stopFetches() {
	w.mu.Lock()
	cancel := w.cancel
	w.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	w.fetchWG.Wait()
	w.fetchCancels = nil
}
