package browser

import (
	"sort"
	"strconv"
	"strings"

	"github.com/moreveal/mimic/internal/state"
	"github.com/pion/sdp/v3"
)

// rtcChromeOfferCodecs projects the negotiated direction onto the profile's
// codec catalog. A recvonly offer can include decoding formats that do not
// appear in a sendrecv offer. Answers retain Pion's remote intersection.
func rtcChromeOfferCodecs(description *sdp.SessionDescription, catalog state.RTPCatalog) {
	for _, media := range description.MediaDescriptions {
		kind := media.MediaName.Media
		if kind != "audio" && kind != "video" {
			continue
		}
		direction := "sendrecv"
		for _, attribute := range media.Attributes {
			switch attribute.Key {
			case "recvonly", "sendonly", "sendrecv", "inactive":
				direction = attribute.Key
			}
		}
		policy := catalog.Media(kind, direction)
		if len(policy.Payloads) == 0 {
			continue
		}
		media.MediaName.Formats = media.MediaName.Formats[:0]
		for _, payload := range policy.Payloads {
			media.MediaName.Formats = append(media.MediaName.Formats, strconv.Itoa(payload))
		}
		attributes := make([]sdp.Attribute, 0, len(media.Attributes)+len(policy.Extensions)+len(policy.Attributes))
		for _, attribute := range media.Attributes {
			codecAttribute := false
			for _, key := range []string{"extmap", "rtpmap", "fmtp", "rtcp-fb", "rtcp-xr"} {
				if attribute.Key == key || strings.HasPrefix(attribute.Key, key+":") {
					codecAttribute = true
					break
				}
			}
			if codecAttribute {
				continue
			}
			attributes = append(attributes, attribute)
		}
		sort.SliceStable(attributes, func(i, j int) bool {
			return rtcSDPAttributeRank(attributes[i].Key, false) < rtcSDPAttributeRank(attributes[j].Key, false)
		})
		insertPolicy := func(lines []string, beforeRank int) {
			at := len(attributes)
			for index, attribute := range attributes {
				if rtcSDPAttributeRank(attribute.Key, false) >= beforeRank {
					at = index
					break
				}
			}
			projected := make([]sdp.Attribute, 0, len(lines))
			for _, line := range lines {
				key, value, found := strings.Cut(strings.TrimPrefix(line, "a="), ":")
				if found {
					projected = append(projected, sdp.Attribute{Key: key, Value: value})
				}
			}
			attributes = append(attributes[:at], append(projected, attributes[at:]...)...)
		}
		insertPolicy(policy.Extensions, rtcSDPAttributeRank("extmap", false))
		insertPolicy(policy.Attributes, rtcSDPAttributeRank("rtcp-xr", false))
		media.Attributes = attributes
	}
}

// rtcChromeSDP is part of native SDP generation, before Pion records the offer
// it will accept. It changes serialization conventions, never transport values
// or the set of codecs which our CPU media implementation can actually use.
func rtcChromeSDP(description *sdp.SessionDescription) error {
	var fingerprints []sdp.Attribute
	var streamIDs []string
	for _, media := range description.MediaDescriptions {
		for _, attribute := range media.Attributes {
			if attribute.Key != "msid" {
				continue
			}
			fields := strings.Fields(attribute.Value)
			if len(fields) == 0 || fields[0] == "-" {
				continue
			}
			found := false
			for _, id := range streamIDs {
				if id == fields[0] {
					found = true
					break
				}
			}
			if !found {
				streamIDs = append(streamIDs, fields[0])
			}
		}
	}
	attributes := make([]sdp.Attribute, 0, len(description.Attributes)+1)
	for _, attribute := range description.Attributes {
		switch attribute.Key {
		case "fingerprint":
			fingerprints = append(fingerprints, attribute)
		case "extmap-allow-mixed":
			// Installed below exactly once.
		case "msid-semantic":
			attribute.Value = " WMS"
			if len(streamIDs) != 0 {
				attribute.Value += " " + strings.Join(streamIDs, " ")
			}
			attributes = append(attributes, attribute)
		default:
			attributes = append(attributes, attribute)
		}
	}
	attributes = append(attributes, sdp.Attribute{Key: "extmap-allow-mixed"})
	sort.SliceStable(attributes, func(i, j int) bool {
		return rtcSDPAttributeRank(attributes[i].Key, true) < rtcSDPAttributeRank(attributes[j].Key, true)
	})
	description.Attributes = attributes
	for _, media := range description.MediaDescriptions {
		attributes = make([]sdp.Attribute, 0, len(media.Attributes)+len(fingerprints)+1)
		iceOptions := "trickle"
		for _, attribute := range media.Attributes {
			if attribute.Key == "ice-options" {
				if !strings.Contains(attribute.Value, "trickle") {
					iceOptions += " " + attribute.Value
				} else {
					iceOptions = attribute.Value
				}
				continue
			}
			if media.MediaName.Media == "application" && attribute.Key == "sendrecv" {
				continue // SCTP has no RTP direction attribute.
			}
			attributes = append(attributes, attribute)
		}
		attributes = append(attributes, fingerprints...)
		attributes = append(attributes, sdp.Attribute{Key: "ice-options", Value: iceOptions})
		if media.MediaName.Media == "audio" || media.MediaName.Media == "video" {
			attributes = append(attributes, sdp.Attribute{Key: "rtcp", Value: "9 IN IP4 0.0.0.0"})
		}
		sort.SliceStable(attributes, func(i, j int) bool {
			return rtcSDPAttributeRank(attributes[i].Key, false) < rtcSDPAttributeRank(attributes[j].Key, false)
		})
		media.Attributes = attributes
	}
	return nil
}

func rtcSDPAttributeRank(key string, session bool) int {
	if session {
		switch key {
		case "group":
			return 0
		case "extmap-allow-mixed":
			return 1
		case "msid-semantic":
			return 2
		}
		return 3
	}
	order := []string{"rtcp", "ice-ufrag", "ice-pwd", "ice-options", "fingerprint", "setup", "mid", "extmap", "recvonly", "sendrecv", "sendonly", "inactive", "rtcp-mux", "rtcp-rsize", "rtcp-xr", "rtpmap", "rtcp-fb", "fmtp", "msid", "ssrc-group", "ssrc", "sctp-port", "max-message-size", "candidate", "end-of-candidates"}
	for rank, name := range order {
		if key == name || strings.HasPrefix(key, name+":") {
			return rank
		}
	}
	return len(order)
}
