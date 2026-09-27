package browser

import (
	"crypto/rand"
	"fmt"
	"strings"
	"time"

	"github.com/moreveal/mimic/internal/network"
	"github.com/moreveal/mimic/internal/trace"
)

// A completion cursor consumes the existing loader record stream once. Entry
// identity no longer depends on equality of timestamps or repeated trace scans.
func (r *Realm) syncPerformanceEntries() {
	p := r.agent.Page()
	for _, event := range p.trace.EventsSince(r.performanceCursor) {
		r.performanceCursor = event.Sequence
		if event.Kind != trace.Network {
			continue
		}
		id := fmt.Sprint(event.Data["id"])
		if event.Name == "response" && id == r.navigationLoaderID && event.Data["context"] == r.agent.ContextID() {
			if numberValue(event.Data["status"]) < 300 || numberValue(event.Data["status"]) >= 400 {
				r.performanceNavigationResponse = event.Data
			}
			continue
		}
		if event.Name != "response" || event.Data["initiator"] == network.Navigation {
			continue
		}
		if owner, stamped := event.Data["performanceOwner"]; stamped {
			if owner != r.ID {
				continue
			}
		} else {
			if event.Time.Before(r.performanceOrigin) {
				continue
			}
			owner, _ := event.Data["context"].(string)
			if event.Data["initiator"] == network.Iframe {
				if frame := p.frame(owner); frame != nil && frame.parent != nil {
					owner = frame.parent.ID
				}
			}
			if owner != "" && owner != r.agent.ContextID() {
				continue
			}
		}
		if numberValue(event.Data["status"]) >= 300 && numberValue(event.Data["status"]) < 400 && numberValue(event.Data["status"]) != 304 {
			continue
		}
		entry := performanceResourceEntry(event.Data, r.performanceOrigin, r.origin, p.environmentView().Time.NetworkScale, p.performanceClamper, r.securityState().crossOriginIsolated)
		if entry != nil {
			r.performance.append(r.performance.create(entry, nil))
		}
	}
	if r.performance.navigationRecord == nil {
		record := r.performance.create(r.performanceNavigationEntry(), nil)
		r.performance.navigationRecord = record
		record.buffered = true
		r.performance.entries = append(r.performance.entries, record)
		visibility := r.performance.create(map[string]any{"entryType": "visibility-state", "name": "visible", "startTime": 0.0, "duration": 0.0}, nil)
		r.performance.append(visibility)
	}
	if !r.navigationLoadEnd.IsZero() && !r.performance.navigationDelivered {
		r.performance.navigationDelivered = true
		r.performance.publish(r.performance.navigationRecord)
	}
}

func performanceResourceEntry(data map[string]any, origin time.Time, securityOrigin string, scale float64, clamper performanceClamper, isolated bool) map[string]any {
	resourceURL := fmt.Sprint(data["url"])
	name := resourceURL
	if original, ok := data["performanceURL"].(string); ok && original != "" {
		name = original
	}
	// Local URL loads do not create Resource Timing entries in Chrome 152.
	// Keep this at the shared projection boundary: fetch and browser-owned
	// resources must agree, regardless of when their completion is observed.
	if strings.HasPrefix(resourceURL, "blob:") || strings.HasPrefix(resourceURL, "data:") {
		return nil
	}
	start := origin
	if value, ok := data["performanceStart"].(time.Time); ok && !value.IsZero() {
		start = value
	}
	stamp := func(ms float64) float64 {
		return clamper.now(start.Add(time.Duration(ms*scale*float64(time.Millisecond))), origin, isolated)
	}
	startTime := stamp(0)
	fetchOffset := numberValue(data["performanceRedirectEnd"])
	fetchStart := stamp(fetchOffset)
	phases := transportPhases(data["browserVisibleTiming"])
	if phases == nil {
		phases = transportPhases(data["transportTiming"])
	}
	rawDuration := numberValue(data["durationMs"])
	end := stamp(transportPhase(phases, "responseComplete", rawDuration))
	dnsStart := max(fetchStart, stamp(transportPhase(phases, "dnsStart", fetchOffset)))
	dnsEnd := max(dnsStart, stamp(transportPhase(phases, "dnsEnd", fetchOffset)))
	connectStart := max(dnsEnd, stamp(transportPhase(phases, "tcpConnectStart", fetchOffset)))
	connectEnd := max(connectStart, stamp(transportPhase(phases, "tlsHandshakeEnd", transportPhase(phases, "tcpConnectEnd", fetchOffset))))
	requestStart := max(connectEnd, stamp(transportPhase(phases, "requestHeadersSent", 0)))
	responseStart := max(requestStart, stamp(transportPhase(phases, "firstResponseByte", 0)))
	end = max(end, responseStart)
	secureStart := 0.0
	if strings.HasPrefix(resourceURL, "https:") {
		secureStart = max(connectStart, stamp(transportPhase(phases, "tlsHandshakeStart", 0)))
	}
	entry := map[string]any{"name": resourceURL, "entryType": "resource", "startTime": startTime, "duration": end - startTime, "fetchStart": startTime, "domainLookupStart": dnsStart, "domainLookupEnd": dnsEnd, "connectStart": connectStart, "secureConnectionStart": secureStart, "connectEnd": connectEnd, "requestStart": requestStart, "responseStart": responseStart, "finalResponseHeadersStart": responseStart, "responseEnd": end, "initiatorType": fmt.Sprint(data["performanceInitiatorType"]), "transferSize": data["transferSize"], "encodedBodySize": data["encodedBodySize"], "decodedBodySize": data["decodedBodySize"], "nextHopProtocol": performanceProtocol(data["protocol"]), "responseStatus": data["status"], "serverTiming": performanceServerTiming(data["headers"]), "contentType": fmt.Sprint(data["mimeType"]), "contentEncoding": performanceHeader(data["headers"], "content-encoding"), "renderBlockingStatus": "non-blocking"}
	if cached, _ := data["fromCache"].(bool); cached {
		entry["deliveryType"] = "cache"
		entry["nextHopProtocol"] = ""
		entry["secureConnectionStart"] = 0
		for _, field := range []string{"domainLookupStart", "domainLookupEnd", "connectStart", "connectEnd", "requestStart"} {
			entry[field] = fetchStart
		}
	}
	entry["name"], entry["fetchStart"] = name, fetchStart
	redirects := numberValue(data["performanceRedirectCount"])
	if redirects > 0 {
		entry["redirectStart"], entry["redirectEnd"], entry["redirectCount"] = startTime, fetchStart, redirects
	}
	failed, _ := data["performanceTimingAllowFailed"].(bool)
	if failed || !resourceTimingAllowed(securityOrigin, resourceURL, data["headers"]) {
		for _, field := range []string{"domainLookupStart", "domainLookupEnd", "connectStart", "secureConnectionStart", "connectEnd", "requestStart", "responseStart", "finalResponseHeadersStart", "transferSize", "encodedBodySize", "decodedBodySize"} {
			entry[field] = 0
		}
		entry["nextHopProtocol"] = ""
		entry["serverTiming"] = []map[string]any{}
		entry["redirectStart"], entry["redirectEnd"], entry["redirectCount"] = 0, 0, 0
		entry["startTime"], entry["duration"] = fetchStart, end-fetchStart
		if cors, _ := data["performanceCORSAccessible"].(bool); !cors {
			entry["responseStatus"] = 0
			entry["contentType"] = ""
			entry["contentEncoding"] = ""
		}
	}
	return entry
}

func (r *Realm) performanceNavigationEntry() map[string]any {
	p := r.agent.Page()
	entry := map[string]any{}
	if r.performanceNavigationResponse != nil {
		data := make(map[string]any, len(r.performanceNavigationResponse)+1)
		for k, v := range r.performanceNavigationResponse {
			data[k] = v
		}
		data["performanceStart"] = r.performanceOrigin
		entry = performanceResourceEntry(data, r.performanceOrigin, r.origin, p.environmentView().Time.NavigationScale, p.performanceClamper, r.securityState().crossOriginIsolated)
		if entry == nil {
			entry = map[string]any{}
		}
	}
	entry["name"], entry["entryType"], entry["initiatorType"] = r.navigationURL, "navigation", "navigation"
	entry["startTime"], entry["duration"] = 0.0, 0.0
	entry["navigationId"], entry["type"] = r.performanceNavigationID(), r.navigationType
	entry["renderBlockingStatus"] = "non-blocking"
	entry["confidence"] = nil
	if r.performanceNavigationResponse != nil {
		if r.performanceConfidence == nil {
			// Confidence is a noisy assessment of transient startup state. This
			// neutral policy deliberately does not expose hardware or heap facts.
			var random [2]byte
			if _, err := rand.Read(random[:]); err != nil {
				panic(err)
			}
			typical := r.performanceOrigin.Sub(p.environmentView().Time.WallOrigin) >= time.Second
			if random[0] < 128 {
				typical = random[1] < 128
			}
			value := "low"
			if typical {
				value = "high"
			}
			r.performanceConfidence = map[string]any{"randomizedTriggerRate": 0.5, "value": value}
		}
		entry["confidence"] = r.performanceConfidence
	}
	entry["notRestoredReasons"] = nil
	for _, field := range []string{"domInteractive", "domContentLoadedEventStart", "domContentLoadedEventEnd", "domComplete", "loadEventStart"} {
		value := 0.0
		if stamp := r.performanceLifecycleTimes[field]; !stamp.IsZero() {
			value = p.performanceClamper.now(stamp, r.performanceOrigin, r.securityState().crossOriginIsolated)
		}
		entry[field] = value
	}
	entry["loadEventEnd"] = 0.0
	if !r.navigationLoadEnd.IsZero() {
		entry["loadEventEnd"] = p.performanceClamper.now(r.navigationLoadEnd, r.performanceOrigin, r.securityState().crossOriginIsolated)
		entry["duration"] = entry["loadEventEnd"]
	}
	return entry
}
