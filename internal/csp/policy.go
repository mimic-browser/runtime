package csp

import (
	"net/url"
	"strings"
)

type Policy struct {
	directives map[string][]string
	reportOnly bool
}
type PolicySet []Policy

// AllowsConnection intersects enforced connect-src lists, falling back to
// default-src only when connect-src is absent. Report-only lists do not block.
func (set PolicySet) AllowsConnection(documentURL, target *url.URL) bool {
	return set.AllowsResource("connect-src", documentURL, target)
}

// AllowsResource applies the selected fetch directive and its default fallback.
// An empty directive means this request has no source-list admission attached.
func (set PolicySet) AllowsResource(directive string, documentURL, target *url.URL) bool {
	if directive == "" {
		return true
	}
	for _, policy := range set {
		if policy.reportOnly {
			continue
		}
		sources, present := policy.directives[directive]
		if !present {
			sources, present = policy.directives["default-src"]
		}
		if !present {
			continue
		}
		allowed := false
		for _, source := range sources {
			if target == nil {
				continue
			}
			matches := sourceMatches(source, documentURL, target)
			if directive == "connect-src" {
				matches = connectionSourceMatches(source, documentURL, target)
			}
			if matches {
				allowed = true
				break
			}
		}
		if !allowed {
			return false
		}
	}
	return true
}

func (set PolicySet) AllowsFormAction(documentURL, target *url.URL) bool {
	for _, policy := range set {
		if policy.reportOnly {
			continue
		}
		sources, present := policy.directives["form-action"]
		if !present {
			continue
		} // form-action has no default-src fallback.
		allowed := false
		for _, source := range sources {
			if sourceMatches(source, documentURL, target) {
				allowed = true
				break
			}
		}
		if !allowed {
			return false
		}
	}
	return true
}

func Parse(values ...string) PolicySet {
	set := PolicySet{}
	for _, header := range values {
		for _, value := range strings.Split(header, ",") {
			p := Policy{directives: map[string][]string{}}
			for _, raw := range strings.Split(value, ";") {
				fields := strings.Fields(raw)
				if len(fields) == 0 {
					continue
				}
				name := strings.ToLower(fields[0])
				if _, exists := p.directives[name]; !exists {
					p.directives[name] = fields[1:]
				}
			}
			if len(p.directives) > 0 {
				set = append(set, p)
			}
		}
	}
	return set
}

func (set PolicySet) AllowsScript(documentURL, resourceURL *url.URL, inline, dynamic bool, nonce string) (bool, string) {
	for _, policy := range set {
		if policy.reportOnly {
			continue
		}
		allowed, reason := policy.allowsScript(documentURL, resourceURL, inline, dynamic, nonce)
		if !allowed {
			return false, reason
		}
	}
	return true, "allowed by all enforced policies"
}

func (p Policy) allowsScript(documentURL, resourceURL *url.URL, inline, dynamic bool, nonce string) (bool, string) {
	sources, exists := p.directives["script-src-elem"]
	if !exists {
		sources, exists = p.directives["script-src"]
	}
	if !exists {
		sources, exists = p.directives["default-src"]
	}
	if !exists {
		return true, "no applicable directive"
	}
	for _, source := range sources {
		if nonce != "" && source == "'nonce-"+nonce+"'" {
			return true, "matching nonce"
		}
	}
	if inline {
		hasNonceOrHash := false
		for _, source := range sources {
			if strings.HasPrefix(source, "'nonce-") || strings.HasPrefix(source, "'sha256-") || strings.HasPrefix(source, "'sha384-") || strings.HasPrefix(source, "'sha512-") {
				hasNonceOrHash = true
			}
		}
		for _, source := range sources {
			if source == "'unsafe-inline'" && !hasNonceOrHash {
				return true, "unsafe-inline"
			}
		}
		return false, "inline script rejected by script source list"
	}
	if resourceURL == nil {
		return false, "missing script resource URL"
	}
	strictDynamic := contains(sources, "'strict-dynamic'") && hasNonceOrHash(sources)
	if strictDynamic {
		if dynamic {
			return false, "strict-dynamic trust propagation is not established"
		}
		return false, "strict-dynamic rejects parser-inserted script without a nonce"
	}
	for _, source := range sources {
		if sourceMatches(source, documentURL, resourceURL) {
			return true, "matching source expression"
		}
	}
	return false, "script URL rejected by script source list"
}

func sourceMatches(source string, documentURL, resourceURL *url.URL) bool {
	switch source {
	case "*":
		return resourceURL.Scheme == "http" || resourceURL.Scheme == "https"
	case "'self'":
		return sameOrigin(documentURL, resourceURL)
	case "'none'", "'unsafe-inline'", "'unsafe-eval'", "'strict-dynamic'":
		return false
	}
	if strings.HasSuffix(source, ":") && !strings.Contains(source, "/") {
		return resourceURL.Scheme == strings.TrimSuffix(strings.ToLower(source), ":")
	}
	candidate := source
	if !strings.Contains(candidate, "://") {
		candidate = documentURL.Scheme + "://" + candidate
	}
	u, err := url.Parse(candidate)
	if err != nil || u.Host == "" || u.Scheme != resourceURL.Scheme {
		return false
	}
	if strings.HasPrefix(u.Hostname(), "*.") {
		suffix := strings.TrimPrefix(u.Hostname(), "*")
		return strings.HasSuffix(resourceURL.Hostname(), suffix)
	}
	return u.Host == resourceURL.Host
}

func sameOrigin(a, b *url.URL) bool {
	return a != nil && b != nil && strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Host, b.Host)
}
func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
func hasNonceOrHash(values []string) bool {
	for _, value := range values {
		if strings.HasPrefix(value, "'nonce-") || strings.HasPrefix(value, "'sha256-") || strings.HasPrefix(value, "'sha384-") || strings.HasPrefix(value, "'sha512-") {
			return true
		}
	}
	return false
}

// Report-only policies participate in default Trusted Types conversion but do
// not prohibit policy creation or ordinary string assignment.
func ParseReportOnly(values ...string) PolicySet {
	policies := Parse(values...)
	for i := range policies {
		policies[i].reportOnly = true
	}
	return policies
}

func connectionSourceMatches(source string, documentURL, target *url.URL) bool {
	if target.Scheme == "ws" || target.Scheme == "wss" {
		if source == "*" {
			return true
		}
		if source == "'self'" {
			equivalent := *target
			if equivalent.Scheme == "ws" {
				equivalent.Scheme = "http"
			} else {
				equivalent.Scheme = "https"
			}
			return sameOrigin(documentURL, &equivalent)
		}
	}
	return sourceMatches(source, documentURL, target)
}
