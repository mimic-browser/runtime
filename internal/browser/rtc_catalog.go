package browser

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/moreveal/mimic/internal/state"
	"github.com/pion/webrtc/v4"
)

// registerRTPCatalog keeps the native offer's codec list aligned with the
// environment's advertised RTP capabilities. Track binding still selects a
// codec that the actual track can packetize.
func registerRTPCatalog(engine *webrtc.MediaEngine, catalog state.RTPCatalog) error {
	for _, kind := range []string{"audio", "video"} {
		media := catalog[kind]
		codecType := webrtc.RTPCodecTypeAudio
		if kind == "video" {
			codecType = webrtc.RTPCodecTypeVideo
		}
		for _, uri := range media.HeaderExtensions {
			if err := engine.RegisterHeaderExtension(webrtc.RTPHeaderExtensionCapability{URI: uri}, codecType); err != nil {
				return fmt.Errorf("register %s RTP header extension %s: %w", kind, uri, err)
			}
		}
		order := append(append([]string{}, media.OfferOrder...), media.ReceiverOrder...)
		seen := map[string]bool{}
		for _, key := range order {
			if seen[key] || key == "rtx" {
				continue
			}
			seen[key] = true
			codec, ok := media.Codecs[key]
			if !ok {
				return fmt.Errorf("RTP catalog %s: missing codec %q", kind, key)
			}
			if codec.Payload < 0 || codec.Payload > 127 {
				return fmt.Errorf("RTP catalog %s: invalid payload %d", kind, codec.Payload)
			}
			parameters := codec.Parameters
			if len(codec.Redundant) != 0 {
				payloads := make([]string, 0, len(codec.Redundant))
				for _, related := range codec.Redundant {
					other, found := media.Codecs[related]
					if !found {
						return fmt.Errorf("RTP catalog %s: missing redundant codec %q", kind, related)
					}
					payloads = append(payloads, strconv.Itoa(other.Payload))
				}
				parameters = strings.Join(payloads, "/")
			}
			feedback := make([]webrtc.RTCPFeedback, 0, len(codec.Feedback))
			for _, entry := range codec.Feedback {
				parts := strings.SplitN(entry, " ", 2)
				item := webrtc.RTCPFeedback{Type: parts[0]}
				if len(parts) == 2 {
					item.Parameter = parts[1]
				}
				feedback = append(feedback, item)
			}
			capability := webrtc.RTPCodecCapability{
				MimeType:     kind + "/" + codec.Name,
				ClockRate:    uint32(codec.ClockRate),
				Channels:     uint16(codec.Channels),
				SDPFmtpLine:  parameters,
				RTCPFeedback: feedback,
			}
			if err := engine.RegisterCodec(webrtc.RTPCodecParameters{
				RTPCodecCapability: capability,
				PayloadType:        webrtc.PayloadType(codec.Payload),
			}, codecType); err != nil {
				return fmt.Errorf("register %s payload %d: %w", kind, codec.Payload, err)
			}
			if codec.RTXPayload >= 0 {
				if err := engine.RegisterCodec(webrtc.RTPCodecParameters{
					RTPCodecCapability: webrtc.RTPCodecCapability{
						MimeType: kind + "/rtx", ClockRate: uint32(codec.ClockRate),
						SDPFmtpLine: "apt=" + strconv.Itoa(codec.Payload),
					},
					PayloadType: webrtc.PayloadType(codec.RTXPayload),
				}, codecType); err != nil {
					return fmt.Errorf("register %s RTX payload %d: %w", kind, codec.RTXPayload, err)
				}
			}
		}
	}
	return nil
}
