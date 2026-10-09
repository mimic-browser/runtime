package browser

import (
	"github.com/moreveal/mimic/internal/network"
	"golang.org/x/net/html"
	"strings"
)

// Scan received navigation bytes ahead of the blocking parser. This does not
// construct DOM nodes or execute scripts. Base/referrer/CSP transitions and
// foreign content end this conservative scan rather than inventing their state.
// Templates and noscript content do not initiate script requests. Demand retains
// canonical URL/security checks and may decline speculative resource reuse.
func (r *Realm) preloadParserScripts(s *documentStream, source string) {
	if len(source) > 8<<20 || s.ctx.Err() != nil {
		return
	}
	tokens := html.NewTokenizer(strings.NewReader(source))
	inertDepth, count := 0, 0
	explicit := make(map[preloadKey]bool)
	for count < 64 {
		kind := tokens.Next()
		if kind == html.ErrorToken {
			return
		}
		if kind != html.StartTagToken && kind != html.SelfClosingTagToken && kind != html.EndTagToken {
			continue
		}
		token := tokens.Token()
		if token.Data == "template" || token.Data == "noscript" {
			if kind == html.EndTagToken {
				if inertDepth > 0 {
					inertDepth--
				}
			} else {
				// HTML ignores the self-closing flag on these non-void tags.
				inertDepth++
			}
			continue
		}
		if inertDepth > 0 || kind == html.EndTagToken {
			continue
		}
		if token.Data == "base" || token.Data == "svg" || token.Data == "math" {
			return
		}
		attributes := map[string]string{}
		for _, attr := range token.Attr {
			if _, exists := attributes[attr.Key]; !exists {
				attributes[attr.Key] = attr.Val
			}
		}
		if token.Data == "meta" && (strings.EqualFold(attributes["http-equiv"], "content-security-policy") || strings.EqualFold(attributes["http-equiv"], "content-security-policy-report-only") || strings.EqualFold(attributes["name"], "referrer")) {
			return
		}
		if token.Data == "link" && hasLinkRelation(attributes["rel"], "preload") && strings.EqualFold(attributes["as"], "script") && attributes["href"] != "" {
			if target, err := r.resolveDocument(attributes["href"]); err == nil {
				explicit[preloadRequestKey(r.elementRequest(target, attributes, network.Script))] = true
			}
		}
		if token.Data != "script" || attributes["src"] == "" || scriptExecutionKind(attributes["type"], attributes["language"]) != "classic" {
			continue
		}
		target, err := r.resolveDocument(attributes["src"])
		if err != nil || !r.allowsScript(target, false, false, attributes["nonce"]) {
			continue
		}
		request := r.elementRequest(target, attributes, network.Script)
		if explicit[preloadRequestKey(request)] {
			continue
		}
		request.Mechanism = "preload"
		request.PerformanceInitiatorType = "script"
		if !r.agent.Page().loader.SpeculationAllowed(r.withResourceTiming(request)) {
			continue
		}
		r.startResourcePreload(s.ctx, request)
		count++
	}
}
