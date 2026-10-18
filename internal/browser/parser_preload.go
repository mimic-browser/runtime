package browser

import (
	"github.com/moreveal/mimic/internal/network"
	"golang.org/x/net/html"
	"strings"
)

// Scan received navigation bytes ahead of the blocking parser. This does not
// construct DOM nodes or execute scripts. Base/referrer/CSP transitions and
// foreign content end this conservative scan rather than inventing their state.
// Templates and noscript content do not initiate speculative requests. Demand retains
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
		if token.Data == "link" && hasLinkRelation(attributes["rel"], "preload") && attributes["href"] != "" {
			var initiator network.Initiator
			switch strings.ToLower(attributes["as"]) {
			case "script":
				initiator = network.Script
			case "style":
				initiator = network.Stylesheet
			}
			if initiator != "" {
				if target, err := r.resolveDocument(attributes["href"]); err == nil {
					explicit[preloadRequestKey(r.elementRequest(target, attributes, initiator))] = true
				}
			}
		}
		resource, initiator, performanceType := attributes["src"], network.Script, "script"
		if token.Data == "link" && hasLinkRelation(attributes["rel"], "stylesheet") {
			if _, disabled := attributes["disabled"]; disabled {
				continue
			}
			if kind := strings.TrimSpace(attributes["type"]); kind != "" && !strings.EqualFold(kind, "text/css") {
				continue
			}
			resource, initiator, performanceType = attributes["href"], network.Stylesheet, "link"
		} else if token.Data != "script" || scriptExecutionKind(attributes["type"], attributes["language"]) != "classic" {
			continue
		}
		if resource == "" {
			continue
		}
		target, err := r.resolveDocument(resource)
		if err != nil {
			continue
		}
		if initiator == network.Script && !r.allowsScript(target, false, false, attributes["nonce"]) {
			continue
		}
		if initiator == network.Stylesheet && !r.contentPolicy().AllowsResource("style-src-elem", r.documentURL(), target) {
			continue
		}
		var request network.Request
		if token.Data == "script" {
			request = r.parserScriptRequest(target, attributes)
		} else {
			request = r.elementRequest(target, attributes, initiator)
		}
		if explicit[preloadRequestKey(request)] {
			continue
		}
		request.Mechanism = "preload"
		request.PerformanceInitiatorType = performanceType
		if !r.agent.Page().loader.SpeculationAllowed(r.withResourceTiming(request)) {
			continue
		}
		r.startResourcePreload(s.ctx, request)
		count++
	}
}
