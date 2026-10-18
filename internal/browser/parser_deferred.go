package browser

import (
	"context"
	"strings"

	"github.com/moreveal/mimic/internal/dom"
	"github.com/moreveal/mimic/internal/network"
	"github.com/moreveal/mimic/internal/scheduler"
	"github.com/moreveal/mimic/internal/trace"
)

// Parser-inserted defer scripts and non-async modules share document order.
// Fetching may run ahead, but execution and DCL remain on the owning Page loop.
type deferredParserScript struct {
	code, name string
	nodeID     int64
	module     bool
	integrity  string
	pending    *scriptFetch
}

func (r *Realm) queueDeferredClassic(s *documentStream, node dom.Node) (bool, error) {
	_, deferred := node.Attributes["defer"]
	_, async := node.Attributes["async"]
	src := node.Attributes["src"]
	if !deferred || async || src == "" || scriptExecutionKind(node.Attributes["type"], node.Attributes["language"]) != "classic" {
		return false, nil
	}
	if r.documentStream != s || r.document.ScriptStarted(node.ID) {
		return true, nil
	}
	target, err := r.resolveDocument(src)
	if err != nil {
		return true, err
	}
	if !r.allowsScript(target, false, false, node.Nonce) {
		return true, nil
	}
	request := r.elementRequest(target, node.Attributes, network.Script)
	if _, cors := node.Attributes["crossorigin"]; cors {
		request.Mode = "cors"
		if !strings.EqualFold(node.Attributes["crossorigin"], "use-credentials") {
			request.Credentials = "same-origin"
		}
	}
	r.document.MarkScriptStarted(node.ID)
	s.deferred = append(s.deferred, deferredParserScript{name: target.String(), nodeID: node.ID, integrity: node.Attributes["integrity"], pending: r.fetchScript(s.ctx, request)})
	return true, nil
}

func (r *Realm) runDeferredParserScripts(s *documentStream, resume func() error) (bool, error) {
	p := r.agent.Page()
	for s.nextDeferred < len(s.deferred) {
		if r.documentStream != s || s.ctx.Err() != nil || r.inactive {
			return true, nil
		}
		script := s.deferred[s.nextDeferred]
		if script.pending != nil {
			if r.deferNavigationScriptFetch(s, script.pending, resume) {
				return true, nil
			}
			response, err := script.pending.wait(s.ctx)
			if err == nil {
				err = scriptIntegrityError(response.Body, script.integrity)
			}
			if err != nil {
				s.nextDeferred++
				p.trace.Add(trace.Error, "scriptLoad", map[string]any{"url": script.name, "error": err.Error()})
				if eventErr := r.runNavigationTask(s.ctx, s, scheduler.DOM, func(ctx context.Context) error { return r.dispatchResourceEvent(ctx, script.nodeID, "error") }); eventErr != nil {
					return false, eventErr
				}
				continue
			}
			script.code = string(response.Body)
		}
		if script.module {
			waiting, err := r.deferNavigationModuleGraph(s, script.code, script.name, resume)
			if waiting {
				return true, nil
			}
			if err != nil {
				s.nextDeferred++
				p.trace.Add(trace.Exception, "script", map[string]any{"url": script.name, "error": err.Error(), "module": true})
				continue
			}
		}
		s.nextDeferred++
		err := r.runNavigationTask(s.ctx, s, scheduler.DOM, func(ctx context.Context) error {
			p.trace.Add(trace.JS, "scriptStart", map[string]any{"url": script.name, "realm": r.ID, "module": script.module, "deferred": true})
			var evalErr error
			if script.module {
				_, evalErr = r.EvaluateModule(ctx, script.code, script.name, r.loadedModule)
			} else {
				r.ignoreDestructiveWrites++
				evalErr = r.evaluateClassicScript(ctx, script.code, script.name, script.nodeID)
				r.ignoreDestructiveWrites--
			}
			data := map[string]any{"url": script.name, "realm": r.ID, "module": script.module, "deferred": true}
			if evalErr != nil {
				data["error"] = evalErr.Error()
			}
			p.trace.Add(trace.JS, "scriptEnd", data)
			// A successful external fetch fires load even when its script throws.
			if !script.module && r.documentStream == s && s.ctx.Err() == nil {
				return r.dispatchResourceEvent(ctx, script.nodeID, "load")
			}
			return evalErr
		})
		if err != nil {
			p.trace.Add(trace.Error, "deferredScriptTask", map[string]any{"url": script.name, "error": err.Error()})
		}
	}
	s.deferred = nil
	s.nextDeferred = 0
	return false, nil
}
