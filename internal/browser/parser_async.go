package browser

import (
	"context"
	"strings"

	"github.com/moreveal/mimic/internal/dom"
	"github.com/moreveal/mimic/internal/scheduler"
	"github.com/moreveal/mimic/internal/trace"
)

// Parser-inserted async classic scripts fetch independently of the HTML parser.
// Their evaluation and resource events return to the owning page event loop.
func (r *Realm) queueParserAsyncClassic(s *documentStream, node dom.Node) (bool, error) {
	_, async := node.Attributes["async"]
	src := node.Attributes["src"]
	if !async || src == "" || scriptExecutionKind(node.Attributes["type"], node.Attributes["language"]) != "classic" {
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
	request := r.parserScriptRequest(target, node.Attributes)
	integrity := node.Attributes["integrity"]
	if _, cors := node.Attributes["crossorigin"]; cors {
		request.Mode = "cors"
		if !strings.EqualFold(node.Attributes["crossorigin"], "use-credentials") {
			request.Credentials = "same-origin"
		}
	}
	r.document.MarkScriptStarted(node.ID)
	const reason = "parser-async-classic"
	blocked := r.beginLoadBlocker(reason)
	pending := r.fetchScript(s.ctx, request)
	loop := r.browserEventLoop()
	r.resourceWG.Add(1)
	go func() {
		defer r.resourceWG.Done()
		response, loadErr := pending.wait(s.ctx)
		if loadErr == nil {
			loadErr = scriptIntegrityError(response.Body, integrity)
		}
		if s.ctx.Err() != nil {
			return
		}
		loop.Post(scheduler.Network, 0, func(ctx context.Context) error {
			if r.documentStream != s || s.ctx.Err() != nil || r.inactive {
				return nil
			}
			if blocked {
				defer r.endLoadBlocker(reason)
			}
			if loadErr != nil {
				r.agent.Page().trace.Add(trace.Error, "scriptLoad", map[string]any{"url": target.String(), "error": loadErr.Error()})
				return r.dispatchResourceEvent(ctx, node.ID, "error")
			}
			r.ignoreDestructiveWrites++
			evalErr := r.evaluateClassicScript(ctx, string(response.Body), target.String(), node.ID)
			r.ignoreDestructiveWrites--
			if evalErr != nil {
				r.agent.Page().trace.Add(trace.Exception, "script", map[string]any{"url": target.String(), "error": evalErr.Error()})
			}
			return r.dispatchResourceEvent(ctx, node.ID, "load")
		})
	}()
	return true, nil
}
