package browser

// ProtocolSecurityState projects the same realm security and permissions policy
// used by JavaScript exposure. Callers own the Page command/trace boundary, as
// for Frame.URL and Frame.ReadyState; this never evaluates author JavaScript.
func (f *Frame) ProtocolSecurityState() (string, string, []string) {
	secureType, isolatedType := "InsecureScheme", "NotIsolated"
	features := []string{}
	if f == nil || f.Realm == nil {
		return secureType, isolatedType, features
	}
	r := f.Realm
	security := r.securityState()
	if security.secureContext {
		secureType = "Secure"
		if r.url.Scheme == "http" && potentiallyTrustworthyURL(r.url) {
			secureType = "SecureLocalhost"
		} else if r.url.Scheme == "about" && f.parent != nil {
			secureType, _, _ = f.parent.ProtocolSecurityState()
		}
	} else if potentiallyTrustworthyURL(r.url) {
		secureType = "InsecureAncestor"
	}
	allowed := true
	if f.parent != nil {
		allowed = r.isolationDelegated(f)
	}
	if policy, declared := hintPolicy(security.permissionsPolicy, r.origin)["cross-origin-isolated"]; declared && !hintAllows(policy, r.origin) {
		allowed = false
	}
	if security.crossOriginIsolated {
		isolatedType = "Isolated"
		features = []string{"SharedArrayBuffers", "SharedArrayBuffersTransferAllowed"}
	} else if !allowed {
		isolatedType = "NotIsolatedFeatureDisabled"
	}
	return secureType, isolatedType, features
}
