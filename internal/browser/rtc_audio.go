package browser

import (
	"context"
	"fmt"
	"time"

	"github.com/moreveal/mimic/internal/scheduler"
	"github.com/pion/opus"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
	"github.com/pion/webrtc/v4/pkg/media/samplebuilder"
)

func rtcTrackCodec(track *cameraTrack) webrtc.RTPCodecCapability {
	if track.kind == "audio" {
		return webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2, SDPFmtpLine: "minptime=10;useinbandfec=1"}
	}
	return webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264, ClockRate: 90000, SDPFmtpLine: "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f"}
}

func (r *Realm) startRTCAudioSender(peer *rtcMediaPeer, sender *rtcMediaSender, track *cameraTrack) error {
	ctx, cancel := context.WithCancel(r.resourceContext)
	sender.cancel = cancel
	sender.trackID = track.id
	source := track.source
	r.resourceWG.Add(1)
	go func() {
		defer r.resourceWG.Done()
		defer cancel()
		select {
		case <-ctx.Done():
			return
		case <-source.context.Done():
			return
		case <-peer.connected:
		}
		source.mu.RLock()
		channels := source.audioFormat.Channels
		source.mu.RUnlock()
		if channels < 1 || channels > 2 {
			r.emitRTCMedia(peer.id, "error", "unsupported audio channel count")
			return
		}
		encoder, err := opus.NewEncoder(opus.WithChannels(channels), opus.WithBitrate(32000*channels), opus.WithComplexity(3))
		if err != nil {
			r.emitRTCMedia(peer.id, "error", err.Error())
			return
		}
		queue := make(chan []float32, 2)
		source.mu.Lock()
		if source.audioSubscribers == nil {
			source.audioSubscribers = map[chan []float32]struct{}{}
		}
		source.audioSubscribers[queue] = struct{}{}
		source.mu.Unlock()
		defer func() { source.mu.Lock(); delete(source.audioSubscribers, queue); source.mu.Unlock() }()
		packet := make([]byte, 1275)
		pcm := make([]float32, 0, 960*channels)
		for {
			var block []float32
			select {
			case <-ctx.Done():
				return
			case <-source.context.Done():
				return
			case block = <-queue:
			}
			source.mu.RLock()
			enabled, stopped := track.enabled, track.stopped
			source.mu.RUnlock()
			if stopped {
				return
			}
			for len(block) > 0 {
				count := min(len(block), cap(pcm)-len(pcm))
				if enabled {
					pcm = append(pcm, block[:count]...)
				} else {
					start := len(pcm)
					pcm = pcm[:start+count]
					clear(pcm[start:])
				}
				block = block[count:]
				if len(pcm) < cap(pcm) {
					continue
				}
				n, err := encoder.EncodeFloat32(pcm, packet)
				pcm = pcm[:0]
				if err != nil {
					r.emitRTCMedia(peer.id, "error", err.Error())
					return
				}
				if err = sender.local.WriteSample(media.Sample{Data: packet[:n], Duration: 20 * time.Millisecond}); err != nil {
					r.emitRTCMedia(peer.id, "error", err.Error())
					return
				}
			}
		}
	}()
	return nil
}

func (r *Realm) receiveRTCAudio(peer *rtcMediaPeer, remote *webrtc.TrackRemote, track *cameraTrack) {
	source := track.source
	r.resourceWG.Add(1)
	go func() {
		defer r.resourceWG.Done()
		if source.context.Err() != nil {
			return
		}
		// Opus RTP declares two channels even when the encoded source is mono.
		// Decode into stereo; packet TOC handling remains in the codec library.
		decoder, err := opus.NewDecoderWithOutput(48000, 2)
		if err != nil {
			r.emitRTCMedia(peer.id, "error", err.Error())
			return
		}
		builder := samplebuilder.New(64, &codecs.OpusPacket{}, 48000, samplebuilder.WithMaxTimeDelay(time.Second))
		buffer := make([]float32, 5760*2)
		for source.context.Err() == nil {
			if err := remote.SetReadDeadline(time.Now().Add(250 * time.Millisecond)); err != nil {
				r.emitRTCMedia(peer.id, "error", err.Error())
				return
			}
			packet, _, err := remote.ReadRTP()
			if err != nil {
				if rtcReadTimeout(err) {
					continue
				}
				return
			}
			builder.Push(packet)
			for sample := builder.Pop(); sample != nil; sample = builder.Pop() {
				count, err := decoder.DecodeToFloat32(sample.Data, buffer)
				if err != nil {
					r.emitRTCMedia(peer.id, "error", fmt.Sprintf("decode Opus: %v", err))
					return
				}
				pcm := append([]float32(nil), buffer[:count*2]...)
				if r.publishMicrophonePCM(source, pcm) {
					r.scheduler.Post(scheduler.DOM, 0, func(ctx context.Context) error {
						if !r.closed && !r.inactive && r.cameraNotifier != nil && !track.stopped {
							_, err := r.runtime.Call(ctx, r.cameraNotifier, nil, r.val(track.id), r.val("unmute"))
							return err
						}
						return nil
					})
				}
				r.queueCameraFrame(source)
			}
		}
	}()
}
