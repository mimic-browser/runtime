package network

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
)

var corsSimpleRange = regexp.MustCompile(`^bytes=[0-9]+-[0-9]*$`)

// Fetch policy lives in the shared loader so Window, Worker and XHR requests
// have identical checks, including cache and interceptor responses. XHR uses
// the Fetch CORS algorithm while retaining its own initiator and lifecycle.
// Remaining Fetch boundaries: preflight caching,
// the aggregate 1024-byte safelist limit, and the no-cors author-header guard.
// Element/module CORS has separate consumers and is not routed here.
func fetchCrossOrigin(r Request) bool {
	return (r.Initiator == Fetch || r.Initiator == XHR) && r.initiatingURL() != nil && r.URL != nil &&
		(r.URL.Scheme == "http" || r.URL.Scheme == "https") &&
		(r.OpaqueOrigin || r.chainSite() != "same-origin")
}

func fetchOrigin(r Request) string {
	source := r.initiatingURL()
	if r.OpaqueOrigin || r.redirectTaintedOrigin() || source == nil || source.Scheme != "http" && source.Scheme != "https" {
		return "null"
	}
	u := *source
	u.User, u.Path, u.RawPath, u.RawQuery, u.Fragment = nil, "", "", "", ""
	u.ForceQuery, u.RawFragment = false, ""
	if u.Port() == "80" && u.Scheme == "http" || u.Port() == "443" && u.Scheme == "https" {
		u.Host = u.Hostname()
		if strings.Contains(u.Host, ":") {
			u.Host = "[" + u.Host + "]"
		}
	}
	return u.String()
}

func corsResponseAllowed(r Request, h http.Header) bool {
	values := h.Values("Access-Control-Allow-Origin")
	if len(values) != 1 {
		return false
	}
	allow := strings.TrimSpace(values[0])
	if allow == "*" && r.Credentials != "include" {
		return true
	}
	return allow == fetchOrigin(r) && (r.Credentials != "include" || h.Get("Access-Control-Allow-Credentials") == "true")
}

func corsSafeHeader(name, value string) bool {
	if len(value) > 128 {
		return false
	}
	switch strings.ToLower(name) {
	case "accept", "content-type":
		for _, c := range []byte(value) {
			if c < 32 && c != 9 || c == 127 || strings.ContainsRune("\"():<>?@[\\]{}", rune(c)) {
				return false
			}
		}
		if strings.EqualFold(name, "accept") {
			return true
		}
		mime := strings.ToLower(strings.TrimSpace(strings.SplitN(value, ";", 2)[0]))
		return mime == "text/plain" || mime == "application/x-www-form-urlencoded" || mime == "multipart/form-data"
	case "accept-language", "content-language":
		for _, c := range value {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune(" *,-.;=", c)) {
				return false
			}
		}
		return true
	case "range":
		return corsSimpleRange.MatchString(value)
	}
	return false
}

// CORSSafelistedRequestHeader is the shared Fetch policy used when Beacon
// chooses its request mode from the extracted body's Content-Type.
func CORSSafelistedRequestHeader(name, value string) bool { return corsSafeHeader(name, value) }

func (l *Loader) prepareFetchCORS(ctx context.Context, r *Request) error {
	if r.Initiator != Fetch && r.Initiator != XHR || r.corsPreflight {
		return nil
	}
	// Capture author fields before the loader installs its own browser headers.
	if !r.corsPrepared {
		for name, values := range r.Headers {
			lower := strings.ToLower(name)
			// Browser-owned fields are not CORS-unsafe author request headers.
			if lower == "origin" || lower == "referer" || lower == "cookie" || lower == "user-agent" || strings.HasPrefix(lower, "sec-") {
				continue
			}
			if !corsSafeHeader(name, strings.Join(values, ", ")) {
				r.corsUnsafeHeaders = append(r.corsUnsafeHeaders, strings.ToLower(name))
			}
		}
		sort.Strings(r.corsUnsafeHeaders)
		r.corsPrepared = true
	}
	if !fetchCrossOrigin(*r) {
		return nil
	}
	if r.Mode == "same-origin" {
		return fmt.Errorf("cross-origin request forbidden by same-origin mode")
	}
	if r.Mode == "no-cors" {
		return nil
	}
	// Redirect processing can remove author fields (notably Authorization).
	unsafe := r.corsUnsafeHeaders[:0:0]
	for _, name := range r.corsUnsafeHeaders {
		if len(r.Headers.Values(name)) != 0 {
			unsafe = append(unsafe, name)
		}
	}
	r.corsUnsafeHeaders = unsafe
	r.Headers.Set("Origin", fetchOrigin(*r))
	if !r.CORSPreflightRequired && (r.Method == "GET" || r.Method == "HEAD" || r.Method == "POST") && len(r.corsUnsafeHeaders) == 0 {
		return nil
	}
	pre := *r
	pre.ID, pre.Method, pre.Body, pre.Credentials, pre.Redirect = "", "OPTIONS", nil, "omit", "error"
	pre.corsPreflight = true
	pre.Headers = make(http.Header)
	pre.Headers.Set("Origin", fetchOrigin(*r))
	pre.Headers.Set("Access-Control-Request-Method", r.Method)
	if len(r.corsUnsafeHeaders) > 0 {
		pre.Headers.Set("Access-Control-Request-Headers", strings.Join(r.corsUnsafeHeaders, ","))
	}
	res, err := l.Load(ctx, pre)
	if err != nil {
		return err
	}
	if res.Status < 200 || res.Status > 299 || !corsResponseAllowed(*r, res.Headers) {
		return fmt.Errorf("CORS preflight denied")
	}
	methods := strings.Split(res.Headers.Get("Access-Control-Allow-Methods"), ",")
	methodAllowed := r.Method == "GET" || r.Method == "HEAD" || r.Method == "POST"
	for _, method := range methods {
		method = strings.TrimSpace(method)
		methodAllowed = methodAllowed || method == r.Method || method == "*" && r.Credentials != "include"
	}
	allowed := headerTokens(res.Headers.Values("Access-Control-Allow-Headers"))
	if !methodAllowed {
		return fmt.Errorf("CORS preflight method denied")
	}
	for _, name := range r.corsUnsafeHeaders {
		if !allowed[name] && !(allowed["*"] && r.Credentials != "include" && name != "authorization") {
			return fmt.Errorf("CORS preflight header denied")
		}
	}
	return nil
}

func filterFetchResponse(r Request, res Response) Response {
	if r.Initiator != Fetch && r.Initiator != XHR || r.corsPreflight {
		return res
	}
	// Accept cookies and retain inspector evidence before applying this script
	// boundary. Forbidden response fields must never cross into either API's
	// JavaScript state, including basic same-origin responses.
	res.Headers = res.Headers.Clone()
	res.Headers.Del("Set-Cookie")
	res.Headers.Del("Set-Cookie2")
	if fetchCrossOrigin(r) {
		if r.Mode == "no-cors" {
			return Response{Type: "opaque", Headers: make(http.Header)}
		}
		res.Type = "cors"
		exposed := headerTokens(res.Headers.Values("Access-Control-Expose-Headers"))
		wildcard := exposed["*"] && r.Credentials != "include"
		for name := range res.Headers {
			lower := strings.ToLower(name)
			safe := lower == "cache-control" || lower == "content-language" || lower == "content-length" || lower == "content-type" || lower == "expires" || lower == "last-modified" || lower == "pragma"
			if lower == "set-cookie" || lower == "set-cookie2" || !safe && !wildcard && !exposed[lower] {
				res.Headers.Del(name)
			}
		}
	}
	return res
}
