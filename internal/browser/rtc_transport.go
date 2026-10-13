package browser

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/moreveal/mimic/internal/network"
	"github.com/pion/ice/v4"
	"github.com/pion/sdp/v3"
	"github.com/pion/stun/v4"
	"github.com/pion/webrtc/v4"
)

// Context routing is immutable. Author configuration can narrow transport but
// cannot restore a direct network, multicast DNS, host gathering or STUN.
func (r *Realm) rtcTransport(config webrtc.Configuration) (webrtc.SettingEngine, webrtc.Configuration, *network.RTCProxyDialer, bool, error) {
	settings := webrtc.SettingEngine{}
	settings.SetSCTPMaxMessageSize(262144)
	// Active ICE TCP in Pion has an OS dialer independent of transport.Net.
	// Only UDP relay endpoints are used even when TURN's control stream is TCP.
	settings.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4, webrtc.NetworkTypeUDP6})
	hideHost := r.capturePermission("camera") != "granted" && r.capturePermission("microphone") != "granted"
	proxy := r.agent.Page().ctx.proxy
	if proxy.Server == "" {
		mode := ice.MulticastDNSModeQueryOnly
		if hideHost {
			mode = ice.MulticastDNSModeQueryAndGather
		}
		settings.SetICEMulticastDNSMode(mode)
		return settings, config, nil, hideHost, nil
	}
	settings.SetNet(network.RTCIsolatedNet{})
	settings.SetICEMulticastDNSMode(ice.MulticastDNSModeDisabled)
	dialer, err := network.NewRTCProxyDialer(r.resourceContext, proxy.URL())
	if err != nil {
		return settings, config, nil, true, err
	}
	effective, endpoints, err := rtcProxyConfiguration(config)
	if err != nil {
		dialer.Close()
		return settings, config, nil, true, err
	}
	dialer.SetServers(endpoints)
	settings.SetICEProxyDialer(dialer)
	return settings, effective, dialer, true, nil
}

// Filtering unsupported servers saves fruitless work; RTCIsolatedNet is the
// enforcement boundary even if a future caller forgets this filter. Retain the
// author's configuration in JS, like a browser whose routing policy is stricter
// than iceTransportPolicy. No proxy endpoint or profile IP becomes a candidate.
func rtcProxyConfiguration(config webrtc.Configuration) (webrtc.Configuration, map[string]bool, error) {
	servers := []webrtc.ICEServer{}
	endpoints := map[string]bool{}
	for _, server := range config.ICEServers {
		copy := server
		copy.URLs = nil
		for _, raw := range server.URLs {
			u, err := stun.ParseURI(raw)
			if err != nil {
				return config, nil, err
			}
			if u.Proto != stun.ProtoTypeTCP || (u.Scheme != stun.SchemeTypeTURN && u.Scheme != stun.SchemeTypeTURNS) {
				continue
			}
			address := net.JoinHostPort(u.Host, strconv.Itoa(u.Port))
			secure := u.Scheme == stun.SchemeTypeTURNS
			if previous, exists := endpoints[address]; exists && previous != secure {
				return config, nil, fmt.Errorf("TURN TCP and TLS require distinct endpoints")
			}
			endpoints[address] = secure
			copy.URLs = append(copy.URLs, raw)
		}
		if len(copy.URLs) != 0 {
			servers = append(servers, copy)
		}
	}
	config.ICEServers = servers
	config.ICETransportPolicy = webrtc.ICETransportPolicyRelay
	return config, endpoints, nil
}

// Browser stats are a privacy projection of the same ICE agent, preserving
// report IDs, candidate pairs and measured counters. mDNS names are signaling
// identifiers, not IP addresses. Chrome permission-free host stats use blanks.
func rtcCandidateStatsPrivacy(rows map[string]any, hideHost bool) {
	for _, value := range rows {
		row, ok := value.(map[string]any)
		if !ok || (row["type"] != "local-candidate" && row["type"] != "remote-candidate") {
			continue
		}
		row["address"] = row["ip"]
		if hideHost && row["candidateType"] == "host" {
			row["address"], row["ip"] = "", ""
		}
	}
}

// Privacy serialization changes signaling metadata, never routing or candidate
// identity in the agent. This applies equally to events and every SDP getter.
// If the host cannot advertise mDNS, do not fall back to numeric host signaling.
func rtcPublicCandidate(candidate ice.Candidate, hideHost bool) (string, bool) {
	if !hideHost {
		return candidate.Marshal(), true
	}
	if candidate.Type() == ice.CandidateTypeHost && !strings.HasSuffix(candidate.Address(), ".local") {
		return "", false
	}
	if related := candidate.RelatedAddress(); related != nil {
		related.Address, related.Port = "0.0.0.0", 0
		// Pion omits a related endpoint with port zero. Chrome exposes the
		// explicit privacy sentinel; encode it as candidate extensions.
		candidate.AddExtension(ice.CandidateExtension{Key: "raddr", Value: "0.0.0.0"})
		candidate.AddExtension(ice.CandidateExtension{Key: "rport", Value: "0"})
	}
	return candidate.Marshal(), true
}

func rtcPublicDescription(description *webrtc.SessionDescription, hideHost bool) any {
	if description == nil {
		return nil
	}
	if !hideHost {
		return rtcMediaDescription(description)
	}
	var parsed sdp.SessionDescription
	if err := parsed.UnmarshalString(description.SDP); err != nil {
		return nil
	}
	for _, section := range parsed.MediaDescriptions {
		attributes := make([]sdp.Attribute, 0, len(section.Attributes))
		for _, attribute := range section.Attributes {
			if attribute.Key == "candidate" {
				candidate, err := ice.UnmarshalCandidate(attribute.Value)
				if err != nil {
					continue
				}
				value, allowed := rtcPublicCandidate(candidate, hideHost)
				if !allowed {
					continue
				}
				attribute.Value = value
			}
			attributes = append(attributes, attribute)
		}
		section.Attributes = attributes
	}
	encoded, err := parsed.Marshal()
	if err != nil {
		return nil
	}
	return map[string]any{"type": description.Type.String(), "sdp": string(encoded)}
}
