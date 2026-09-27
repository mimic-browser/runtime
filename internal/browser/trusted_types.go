package browser

import (
	"context"
	"net/http"

	"github.com/moreveal/mimic/internal/csp"
	"github.com/moreveal/mimic/internal/dom"
	"github.com/moreveal/mimic/internal/engine"
)

// Policy state belongs to the committed document. A saved Document or iframe
// must never start using a later top-level navigation's policy/default factory.
func (r *Realm) contentPolicy() csp.PolicySet {
	if r.mainWorld != nil {
		return r.mainWorld.contentPolicy()
	}
	r.refreshContentPolicy()
	return r.policy
}

func (r *Realm) refreshContentPolicy() {
	if r.document == nil {
		return
	}
	if r.policyMeta == nil {
		r.policyMeta = map[int64]string{}
	}
	var nodes []dom.Node
	r.policyMetaCursor, r.policyMetaCandidates, nodes = r.document.ContentSecurityPolicyMeta(r.policyMetaCursor, r.policyMetaCandidates)
	for _, node := range nodes {
		if content, seen := r.policyMeta[node.ID]; seen && content == node.Attributes["content"] {
			continue
		}
		r.policyMeta[node.ID] = node.Attributes["content"]
		if r.agent.Page().cspBypassed() {
			continue
		}
		r.policy = append(r.policy, csp.Parse(node.Attributes["content"])...)
	}
}

// Dynamic compilation remembers the CSP delivered to the Document. CDP bypass
// suppresses future policy delivery; it does not remove an existing TT policy.
func (r *Realm) trustedTypesState() csp.TrustedTypesState {
	policy := r.contentPolicy()
	if r.inactive {
		policy = nil
	}
	return policy.TrustedTypes()
}

func (p *Page) cspBypassed() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.bypassCSP
}

func (p *Page) responseCSP(headers http.Header) csp.PolicySet {
	if p.cspBypassed() {
		return nil
	}
	return parseResponseCSP(headers)
}

func (r *Realm) installTrustedTypes(host map[string]any) {
	host["trustedTypesEventAttributes"] = r.transientFn(func(engine.Value, []engine.Value) (engine.Value, error) {
		return r.val(r.agent.Page().Compatibility().Surface().TrustedTypeEventAttributes), nil
	})
	host["nativeEvalSource"] = r.transientFn(func(engine.Value, []engine.Value) (engine.Value, error) {
		_, supported := r.runtime.(engine.EvalSourceRuntime)
		return r.val(supported), nil
	})
	host["trustedTypesPolicy"] = r.transientFn(func(engine.Value, []engine.Value) (engine.Value, error) {
		return r.val(r.trustedTypesState().Projection()), nil
	})
	host["compileContentHandler"] = r.fn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		return r.runtime.Eval(context.Background(), "("+strarg(args, 0)+")", "event-handler")
	})
	host["markTrustedScriptText"] = r.fn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		id, text := int64(numarg(args, 0)), strarg(args, 1)
		r.document.SetScriptText(id, text)
		return nil, nil
	})
	host["installTrustedTypesEnforcer"] = r.fn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		r.trustedTypesEnforcer = args[0]
		return nil, nil
	})
	host["trustedTypesOwner"] = r.transientFn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		owner := r.document.OwnerDocumentID(int64(numarg(args, 0)))
		if owner == r.document.Root().ID {
			return nil, nil
		}
		p := r.agent.Page()
		p.mu.RLock()
		var target *Realm
		for _, candidate := range p.realmOwners {
			if candidate.mainWorld == nil && !candidate.closed && !candidate.inactive && candidate.document != nil && candidate.document.Root().ID == owner && candidate.document.SharesNodeArena(r.document) {
				target = candidate
				break
			}
		}
		p.mu.RUnlock()
		if target == nil || target == r || target == r.mainWorld {
			return nil, nil
		}
		return r.crossFrameResult(target, func(ctx context.Context) (engine.Value, error) { return target.trustedTypesEnforcer, nil })
	})
}

func parseResponseCSP(headers http.Header) csp.PolicySet {
	return append(csp.Parse(headers.Values("Content-Security-Policy")...), csp.ParseReportOnly(headers.Values("Content-Security-Policy-Report-Only")...)...)
}

func (r *Realm) prepareChangedScript(id int64) error {
	if !r.document.IsHTMLScript(id) {
		return nil
	}
	_, err := r.prepareConnectedResource(id, nil, nil)
	return err
}

func (r *Realm) prepareInsertedScripts(parent, inserted int64) error {
	if err := r.prepareChangedScript(parent); err != nil {
		return err
	}
	for _, id := range r.document.UnstartedScriptsWithin(inserted) {
		if _, err := r.prepareConnectedResource(id, nil, nil); err != nil {
			return err
		}
	}
	return nil
}
