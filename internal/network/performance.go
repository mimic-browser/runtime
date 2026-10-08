package network

import (
	"net/http"
	"strings"
)

// PerformanceCompletionMillis is the latest completed transport phase in the
// initiating request's time coordinate. Resource entries and agent clocks use
// this same boundary, including redirects, cache hits and synthetic responses.
func PerformanceCompletionMillis(phases map[string]float64, redirectEnd, duration float64) float64 {
	end := redirectEnd
	for _, name := range []string{"dnsStart", "dnsEnd", "tcpConnectStart", "requestHeadersSent", "firstResponseByte"} {
		end = max(end, phases[name])
	}
	connectEnd, ok := phases["tlsHandshakeEnd"]
	if !ok {
		connectEnd = phases["tcpConnectEnd"]
	}
	responseEnd, ok := phases["responseComplete"]
	if !ok {
		responseEnd = duration
	}
	return max(end, connectEnd, responseEnd)
}

// The immutable request URL list already owns redirect history. Timing
// visibility is accumulated across its responses, so returning to the client's
// origin cannot expose phases hidden by an intermediate response.
func requestTimingAllowed(r Request, headers http.Header) bool {
	source := r.initiatingURL()
	if !r.OpaqueOrigin && !r.redirectTaintedOrigin() && source != nil && sameRequestOrigin(source, r.URL) {
		return true
	}
	origin := "null"
	if !r.OpaqueOrigin && !r.redirectTaintedOrigin() && source != nil {
		origin = source.Scheme + "://" + source.Host
	}
	for _, field := range headers.Values("Timing-Allow-Origin") {
		for _, value := range strings.Split(field, ",") {
			value = strings.TrimSpace(value)
			if value == "*" || value == origin {
				return true
			}
		}
	}
	return false
}

func (r Request) performanceTimingAllowFailed(headers http.Header) bool {
	return r.timingAllowFailed || (r.Initiator == Fetch || r.Initiator == XHR) && !requestTimingAllowed(r, headers)
}

func (r Request) performanceURL() string {
	if len(r.chain.urls) != 0 {
		return r.chain.urls[0].String()
	}
	return r.URL.String()
}
