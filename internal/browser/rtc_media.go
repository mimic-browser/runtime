package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/moreveal/mimic/internal/camera"
	"github.com/moreveal/mimic/internal/engine"
	"github.com/moreveal/mimic/internal/microphone"
	"github.com/moreveal/mimic/internal/scheduler"
	"github.com/moreveal/mimic/internal/videocodec"
	"github.com/pion/interceptor"
	"github.com/pion/rtcp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
	"github.com/pion/webrtc/v4/pkg/media/samplebuilder"
	"golang.org/x/image/draw"
)

type rtcMediaPeer struct {
	id            string
	pc            *webrtc.PeerConnection
	senders       map[string]*rtcMediaSender
	transceivers  map[*webrtc.RTPTransceiver]*rtcMediaTransceiver
	stats         *rtcStatsInterceptor
	channels      map[string]*rtcMediaDataChannel
	connected     chan struct{}
	everConnected atomic.Bool
}

func rtcReadTimeout(err error) bool {
	var timeout net.Error
	return errors.As(err, &timeout) && timeout.Timeout()
}

type rtcMediaTransceiver struct {
	announced                         bool
	id, senderID, receiverID, trackID string
	native                            *webrtc.RTPTransceiver
}
type rtcMediaSender struct {
	native   *webrtc.RTPSender
	local    *webrtc.TrackLocalStaticSample
	cancel   context.CancelFunc
	trackID  string
	keyFrame atomic.Bool
	kind     webrtc.RTPCodecType
}

func rtcRemoteMediaMeta(peer *rtcMediaPeer, mid string) (streamID, trackID string, sends bool) {
	description := peer.pc.RemoteDescription()
	if description == nil || mid == "" {
		return
	}
	for _, section := range strings.Split(description.SDP, "\r\nm=") {
		if !strings.Contains("\r\n"+section, "\r\na=mid:"+mid+"\r\n") {
			continue
		}
		sends = !strings.Contains(section, "\r\na=recvonly\r\n") && !strings.Contains(section, "\r\na=inactive\r\n")
		for _, line := range strings.Split(section, "\r\n") {
			if strings.HasPrefix(line, "a=msid:") {
				parts := strings.Fields(strings.TrimPrefix(line, "a=msid:"))
				if len(parts) >= 2 {
					return parts[0], parts[1], sends
				}
			}
		}
	}
	return
}
func (r *Realm) announceRTCMedia(peer *rtcMediaPeer) {
	r.rtcMediaTransceivers(peer)
	for _, t := range peer.transceivers {
		streamID, trackID, sends := rtcRemoteMediaMeta(peer, t.native.Mid())
		if !sends || t.announced {
			continue
		}
		if trackID != "" {
			r.cameraTracks[t.trackID].publicID = trackID
		}
		t.announced = true
		r.emitRTCMedia(peer.id, "track", map[string]any{"transceiverID": t.id, "streamID": streamID})
	}
}

func (r *Realm) closeRTCMedia() {
	for _, peer := range r.rtcMediaPeers {
		for _, channel := range peer.channels {
			channel.cancel()
		}
		for _, s := range peer.senders {
			if s.cancel != nil {
				s.cancel()
			}
		}
		peer.pc.Close()
	}
}
func (r *Realm) emitRTCMedia(id, kind string, data any) {
	if r.resourceContext.Err() != nil {
		return
	}
	r.scheduler.Post(scheduler.DOM, 0, func(ctx context.Context) error {
		if r.closed || r.inactive || r.rtcMediaNotifier == nil {
			return nil
		}
		_, err := r.runtime.Call(ctx, r.rtcMediaNotifier, nil, r.val(id), r.val(kind), r.val(data))
		return err
	})
}
func (r *Realm) newRTCMedia(config webrtc.Configuration) (string, error) {
	m := &webrtc.MediaEngine{}
	// Advertise only a codec we can actually encode. Packetization mode 1 is
	// interoperable with Chrome's constrained baseline H264 negotiation.
	if err := m.RegisterCodec(webrtc.RTPCodecParameters{RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264, ClockRate: 90000, SDPFmtpLine: "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f", RTCPFeedback: []webrtc.RTCPFeedback{{Type: "nack"}, {Type: "nack", Parameter: "pli"}, {Type: "ccm", Parameter: "fir"}, {Type: "goog-remb"}}}, PayloadType: 102}, webrtc.RTPCodecTypeVideo); err != nil {
		return "", err
	}
	if err := m.RegisterCodec(webrtc.RTPCodecParameters{RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2, SDPFmtpLine: "minptime=10;useinbandfec=1"}, PayloadType: 111}, webrtc.RTPCodecTypeAudio); err != nil {
		return "", err
	}
	registry := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(m, registry); err != nil {
		return "", err
	}
	stats := &rtcStatsInterceptor{}
	registry.Add(stats)
	api := webrtc.NewAPI(webrtc.WithMediaEngine(m), webrtc.WithInterceptorRegistry(registry))
	pc, err := api.NewPeerConnection(config)
	if err != nil {
		return "", err
	}
	id := uuid.NewString()
	peer := &rtcMediaPeer{id: id, pc: pc, senders: map[string]*rtcMediaSender{}, transceivers: map[*webrtc.RTPTransceiver]*rtcMediaTransceiver{}, stats: stats, channels: map[string]*rtcMediaDataChannel{}, connected: make(chan struct{})}
	if r.rtcMediaPeers == nil {
		r.rtcMediaPeers = map[string]*rtcMediaPeer{}
	}
	r.rtcMediaPeers[id] = peer
	pc.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		var data any
		if candidate != nil {
			encoded, _ := json.Marshal(candidate.ToJSON())
			json.Unmarshal(encoded, &data)
		}
		r.emitRTCMedia(id, "icecandidate", data)
	})
	pc.OnICEGatheringStateChange(func(state webrtc.ICEGatheringState) { r.emitRTCMedia(id, "icegatheringstatechange", state.String()) })
	pc.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) { r.emitRTCMedia(id, "iceconnectionstatechange", state.String()) })
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		if state == webrtc.PeerConnectionStateConnected && peer.everConnected.CompareAndSwap(false, true) {
			close(peer.connected)
		}
		r.emitRTCMedia(id, "connectionstatechange", state.String())
	})
	pc.OnSignalingStateChange(func(state webrtc.SignalingState) { r.emitRTCMedia(id, "signalingstatechange", state.String()) })
	pc.OnNegotiationNeeded(func() { r.emitRTCMedia(id, "negotiationneeded", nil) })
	pc.OnDataChannel(func(channel *webrtc.DataChannel) { r.receiveRTCMediaData(peer, channel) })
	pc.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		r.receiveRTCMedia(peer, track, receiver)
	})
	return id, nil
}

// Native transceivers and tracks have one canonical wrapper identity for their
// lifetime, including the muted receiver which exists before the first packet.
func (r *Realm) rtcMediaTransceivers(peer *rtcMediaPeer) []map[string]any {
	rows := []map[string]any{}
	for _, native := range peer.pc.GetTransceivers() {
		t := peer.transceivers[native]
		if t == nil {
			t = &rtcMediaTransceiver{id: uuid.NewString(), senderID: uuid.NewString(), receiverID: uuid.NewString(), trackID: uuid.NewString(), native: native}
			peer.transceivers[native] = t
			ctx, cancel := context.WithCancel(r.resourceContext)
			source := &cameraSource{context: ctx, cancel: cancel, device: camera.Device{Label: "remote video"}}
			kind := "video"
			if native.Kind() == webrtc.RTPCodecTypeAudio {
				kind = "audio"
				source.device.Label = "remote audio"
				source.audioFormat = microphone.Format{SampleRate: 48000, Channels: 2}
			}
			if r.cameraTracks == nil {
				r.cameraTracks = map[string]*cameraTrack{}
			}
			r.cameraTracks[t.trackID] = &cameraTrack{kind: kind, id: t.trackID, source: source, enabled: true, remote: true, constraints: map[string]any{}}
		}
		trackID := ""
		for id, sender := range peer.senders {
			if sender.native == native.Sender() {
				t.senderID = id
				trackID = sender.trackID
				break
			}
		}
		var mid any
		if native.Mid() != "" {
			mid = native.Mid()
		}
		rows = append(rows, map[string]any{"id": t.id, "senderID": t.senderID, "receiverID": t.receiverID, "receiverTrack": r.cameraTracks[t.trackID].snapshot(), "senderTrackID": trackID, "mid": mid, "direction": native.Direction().String()})
	}
	return rows
}

func (r *Realm) receiveRTCMedia(peer *rtcMediaPeer, remote *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
	if remote.Codec().MimeType != webrtc.MimeTypeH264 && remote.Codec().MimeType != webrtc.MimeTypeOpus {
		return
	}
	r.scheduler.Post(scheduler.DOM, 0, func(context.Context) error {
		if r.closed || r.inactive {
			return nil
		}
		r.rtcMediaTransceivers(peer)
		var row *rtcMediaTransceiver
		for native, t := range peer.transceivers {
			if native.Receiver() == receiver {
				row = t
				break
			}
		}
		if row == nil {
			return fmt.Errorf("incoming track has no transceiver")
		}
		track := r.cameraTracks[row.trackID]
		// Remote wire identifiers are exposed on this same canonical object.
		track.source.device.Label = remote.ID()
		track.publicID = remote.ID()
		if !row.announced {
			row.announced = true
			r.emitRTCMedia(peer.id, "track", map[string]any{"transceiverID": row.id, "streamID": remote.StreamID()})
		}
		if track.kind == "audio" {
			r.receiveRTCAudio(peer, remote, track)
			return nil
		}
		r.resourceWG.Add(1)
		go func() {
			defer r.resourceWG.Done()
			s := track.source
			if s.context.Err() != nil {
				return
			}
			decoder, err := videocodec.NewDecoder()
			if err != nil {
				r.emitRTCMedia(peer.id, "error", err.Error())
				return
			}
			defer decoder.Close()
			builder := samplebuilder.New(64, &codecs.H264Packet{}, 90000, samplebuilder.WithMaxTimeDelay(time.Second))
			_ = peer.pc.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: uint32(remote.SSRC())}})
			for {
				if s.context.Err() != nil {
					return
				}
				if err := remote.SetReadDeadline(time.Now().Add(250 * time.Millisecond)); err != nil {
					r.emitRTCMedia(peer.id, "error", err.Error())
					return
				}
				packet, _, readErr := remote.ReadRTP()
				if readErr != nil {
					if rtcReadTimeout(readErr) {
						continue
					}
					return
				}
				builder.Push(packet)
				for sample := builder.Pop(); sample != nil; sample = builder.Pop() {
					frame, decodeErr := decoder.Decode(sample.Data)
					if decodeErr != nil {
						peer.stats.decodedFrame(uint32(remote.SSRC()), 0, 0, true, time.Now())
						_ = peer.pc.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: uint32(remote.SSRC())}})
						continue
					}
					if frame == nil {
						continue
					}
					s.mu.Lock()
					if s.context.Err() != nil {
						s.mu.Unlock()
						return
					}
					first := s.frame == nil
					s.frame = frame
					peer.stats.decodedFrame(uint32(remote.SSRC()), frame.Bounds().Dx(), frame.Bounds().Dy(), false, time.Now())
					s.sequence++
					s.stamp = time.Now()
					if sample.Duration > 0 {
						s.format.FrameRate = 1 / sample.Duration.Seconds()
					}
					s.mu.Unlock()
					if first {
						r.scheduler.Post(scheduler.DOM, 0, func(ctx context.Context) error {
							if r.cameraNotifier != nil && !track.stopped {
								_, err := r.runtime.Call(ctx, r.cameraNotifier, nil, r.val(track.id), r.val("unmute"))
								return err
							}
							return nil
						})
					}
					r.queueCameraFrame(s)
				}
			}
		}()
		return nil
	})
}

func rtcMediaDescription(d *webrtc.SessionDescription) any {
	if d == nil {
		return nil
	}
	return map[string]any{"type": d.Type.String(), "sdp": d.SDP}
}
func rtcMediaState(peer *rtcMediaPeer) map[string]any {
	pc := peer.pc
	return map[string]any{"localDescription": rtcMediaDescription(pc.LocalDescription()), "currentLocalDescription": rtcMediaDescription(pc.CurrentLocalDescription()), "pendingLocalDescription": rtcMediaDescription(pc.PendingLocalDescription()), "remoteDescription": rtcMediaDescription(pc.RemoteDescription()), "currentRemoteDescription": rtcMediaDescription(pc.CurrentRemoteDescription()), "pendingRemoteDescription": rtcMediaDescription(pc.PendingRemoteDescription()), "signalingState": pc.SignalingState().String(), "iceGatheringState": pc.ICEGatheringState().String(), "iceConnectionState": pc.ICEConnectionState().String(), "connectionState": pc.ConnectionState().String()}
}

type rtcCameraFrame struct {
	image         *image.RGBA
	enabled       bool
	stopped       bool
	sequence      uint64
	stamp         time.Time
	width, height int
	fps, noise    float64
	output        *cameraOutput
	generation    uint64
}

func (r *Realm) startRTCMediaSender(peer *rtcMediaPeer, sender *rtcMediaSender, track *cameraTrack) error {
	if track.kind == "audio" {
		return r.startRTCAudioSender(peer, sender, track)
	}
	ctx, cancel := context.WithCancel(r.resourceContext)
	sender.cancel = cancel
	sender.trackID = track.id
	sender.keyFrame.Store(true)
	r.resourceWG.Add(1)
	go func() {
		defer r.resourceWG.Done()
		var encoder *videocodec.Encoder
		defer func() {
			if encoder != nil {
				encoder.Close()
			}
		}()
		defer cancel()
		select {
		case <-ctx.Done():
			return
		case <-peer.connected:
		}
		period := time.Second / 30
		ticker := time.NewTicker(period)
		defer ticker.Stop()
		frameChannel := make(chan rtcCameraFrame, 1)
		var sequence uint64
		var scaled *image.RGBA
		var encoderFPS float64
		var lastStamp time.Time
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			if peer.pc.ConnectionState() != webrtc.PeerConnectionStateConnected {
				continue
			}
			r.scheduler.Post(scheduler.DOM, 0, func(context.Context) error {
				frame := rtcCameraFrame{enabled: track.enabled, stopped: track.stopped, width: track.width, height: track.height, fps: track.frameRate}
				if track.output == nil {
					track.output = &cameraOutput{}
				}
				frame.output = track.output
				frame.generation = track.output.currentGeneration()
				if track.source.mediaDevice != nil {
					frame.noise = track.source.mediaDevice.Processing.Noise
				}
				track.source.mu.RLock()
				frame.image = track.source.frame
				frame.sequence = track.source.sequence
				frame.stamp = track.source.stamp
				if frame.fps <= 0 {
					frame.fps = max(1.0, track.source.format.FrameRate)
				}
				if track.source.mediaDevice == nil && frame.image != nil {
					frame.width, frame.height = frame.image.Bounds().Dx(), frame.image.Bounds().Dy()
				}
				track.source.mu.RUnlock()
				select {
				case frameChannel <- frame:
				default:
				}
				return nil
			})
			var frame rtcCameraFrame
			select {
			case <-ctx.Done():
				return
			case frame = <-frameChannel:
			}
			if frame.stopped {
				return
			}
			if frame.image == nil {
				continue
			}
			nextPeriod := time.Duration(float64(time.Second) / frame.fps)
			if nextPeriod != period {
				period = nextPeriod
				ticker.Reset(period)
			}
			observation := frame.output.observeAt(frame.generation, frame.image, frame.stamp, frame.sequence, frame.width, frame.height, frame.fps, frame.noise, frame.enabled)
			frame.image, frame.sequence, frame.stamp = observation.image, observation.sequence, observation.stamp
			if frame.image == nil || frame.sequence == sequence {
				continue
			}
			sequence = frame.sequence
			// Allocate image/codec state only for a connected peer with a frame.
			// Native dimensions can change on a received track used for forwarding.
			w, h := frame.image.Bounds().Dx(), frame.image.Bounds().Dy()
			scale := min(1.0, min(1280.0/float64(w), 720.0/float64(h)))
			width, height := max(16, int(float64(w)*scale)/2*2), max(16, int(float64(h)*scale)/2*2)
			if scaled == nil || scaled.Bounds().Dx() != width || scaled.Bounds().Dy() != height || encoderFPS != frame.fps {
				if encoder != nil {
					encoder.Close()
					encoder = nil
				}
				var err error
				encoder, err = videocodec.New(width, height, frame.fps)
				if err != nil {
					r.emitRTCMedia(peer.id, "error", err.Error())
					return
				}
				scaled = image.NewRGBA(image.Rect(0, 0, width, height))
				encoderFPS = frame.fps
				sender.keyFrame.Store(true)
			}
			if frame.enabled {
				draw.ApproxBiLinear.Scale(scaled, scaled.Bounds(), frame.image, frame.image.Bounds(), draw.Src, nil)
			} else {
				clear(scaled.Pix)
			}
			data, encodeErr := encoder.Encode(scaled, sender.keyFrame.Swap(false))
			if encodeErr != nil {
				r.emitRTCMedia(peer.id, "error", encodeErr.Error())
				return
			}
			if len(data) > 0 {
				duration := period
				if !lastStamp.IsZero() && frame.stamp.After(lastStamp) {
					duration = frame.stamp.Sub(lastStamp)
				}
				if err := sender.local.WriteSample(media.Sample{Data: data, Duration: duration}); err != nil {
					r.emitRTCMedia(peer.id, "error", err.Error())
					return
				}
				lastStamp = frame.stamp
				parameters := sender.native.GetParameters()
				if len(parameters.Encodings) > 0 {
					peer.stats.encodedFrame(uint32(parameters.Encodings[0].SSRC), width, height, time.Now())
				}
			}
		}
	}()
	return nil
}

func (r *Realm) addRTCMediaTrack(peer *rtcMediaPeer, trackID, streamID string) (string, error) {
	track := r.cameraTracks[trackID]
	if track == nil || track.stopped {
		return "", fmt.Errorf("camera track is not live")
	}
	for _, sender := range peer.senders {
		if sender.trackID == trackID {
			return "", fmt.Errorf("track already added")
		}
	}
	wireID := track.publicID
	if wireID == "" {
		wireID = trackID
	}
	local, err := webrtc.NewTrackLocalStaticSample(rtcTrackCodec(track), wireID, streamID)
	if err != nil {
		return "", err
	}
	var native *webrtc.RTPSender
	// Pion creates a placeholder local track for addTransceiver(kind). Browser
	// senders instead begin with track=null and are reusable without changing
	// the transceiver or sender wrapper identity.
	for transceiver, record := range peer.transceivers {
		if transceiver.Kind() != local.Kind() {
			continue
		}
		if transceiver.Direction() != webrtc.RTPTransceiverDirectionSendrecv && transceiver.Direction() != webrtc.RTPTransceiverDirectionSendonly {
			continue
		}
		candidate := transceiver.Sender()
		if candidate != nil && peer.senders[record.senderID] == nil {
			if err = candidate.ReplaceTrack(local); err != nil {
				return "", err
			}
			native = candidate
			break
		}
	}
	if native == nil {
		native, err = peer.pc.AddTrack(local)
	}
	if err != nil {
		return "", err
	}
	sender := &rtcMediaSender{native: native, local: local, kind: local.Kind()}
	if err = r.startRTCMediaSender(peer, sender, track); err != nil {
		peer.pc.RemoveTrack(native)
		return "", err
	}
	id := uuid.NewString()
	for native, t := range peer.transceivers {
		if native.Sender() == sender.native {
			id = t.senderID
			break
		}
	}
	peer.senders[id] = sender
	r.resourceWG.Add(1)
	go func() {
		defer r.resourceWG.Done()
		for {
			packets, _, err := native.ReadRTCP()
			if err != nil {
				return
			}
			for _, packet := range packets {
				switch packet.(type) {
				case *rtcp.PictureLossIndication, *rtcp.FullIntraRequest:
					sender.keyFrame.Store(true)
				}
			}
		}
	}()
	return id, nil
}

func addRTCMediaHosts(r *Realm, h map[string]any) {
	addRTCMediaDataHosts(r, h)
	h["installRTCMediaNotifier"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		r.rtcMediaNotifier = a[0]
		return nil, nil
	})
	h["rtcMediaCreate"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		var config webrtc.Configuration
		if err := json.Unmarshal([]byte(strarg(a, 0)), &config); err != nil {
			return nil, err
		}
		id, err := r.newRTCMedia(config)
		if err != nil {
			return r.val(cameraFailure("NotSupportedError", err.Error(), "")), nil
		}
		return r.val(map[string]any{"id": id}), nil
	})
	h["rtcMediaState"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		peer := r.rtcMediaPeers[strarg(a, 0)]
		if peer == nil {
			return nil, fmt.Errorf("unknown peer")
		}
		return r.val(rtcMediaState(peer)), nil
	})
	h["rtcMediaTrack"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		peer := r.rtcMediaPeers[strarg(a, 0)]
		if peer == nil {
			return nil, fmt.Errorf("unknown peer")
		}
		switch strarg(a, 1) {
		case "transceivers":
			return r.val(r.rtcMediaTransceivers(peer)), nil
		case "addTransceiver":
			var config struct {
				Direction string `json:"direction"`
				Kind      string `json:"kind"`
			}
			if err := json.Unmarshal([]byte(strarg(a, 2)), &config); err != nil {
				return nil, err
			}
			init := webrtc.RTPTransceiverInit{Direction: webrtc.NewRTPTransceiverDirection(config.Direction)}
			kind := webrtc.RTPCodecTypeVideo
			if config.Kind == "audio" {
				kind = webrtc.RTPCodecTypeAudio
			}
			if _, err := peer.pc.AddTransceiverFromKind(kind, init); err != nil {
				return r.val(cameraFailure("OperationError", err.Error(), "")), nil
			}
			return r.val(r.rtcMediaTransceivers(peer)), nil
		case "stopTransceiver":
			for _, t := range peer.transceivers {
				if t.id == strarg(a, 2) {
					if err := t.native.Stop(); err != nil {
						return nil, err
					}
					r.stopCameraTrack(r.cameraTracks[t.trackID])
				}
			}
			return r.val(map[string]any{}), nil
		case "configuration":
			var config webrtc.Configuration
			if err := json.Unmarshal([]byte(strarg(a, 2)), &config); err != nil {
				return nil, err
			}
			if err := peer.pc.SetConfiguration(config); err != nil {
				return r.val(cameraFailure("InvalidModificationError", err.Error(), "")), nil
			}
			return r.val(map[string]any{}), nil
		case "add":
			id, err := r.addRTCMediaTrack(peer, strarg(a, 2), strarg(a, 3))
			if err != nil {
				return r.val(cameraFailure("InvalidAccessError", err.Error(), "")), nil
			}
			return r.val(map[string]any{"id": id}), nil
		case "remove":
			sender := peer.senders[strarg(a, 2)]
			if sender == nil {
				return nil, fmt.Errorf("unknown sender")
			}
			if sender.cancel != nil {
				sender.cancel()
			}
			if err := peer.pc.RemoveTrack(sender.native); err != nil {
				return r.val(cameraFailure("InvalidStateError", err.Error(), "")), nil
			}
			sender.trackID = ""
			return r.val(map[string]any{}), nil
		case "replace":
			sender := peer.senders[strarg(a, 2)]
			if sender == nil {
				return nil, fmt.Errorf("unknown sender")
			}
			id := strarg(a, 3)
			if id == "" {
				sender.trackID = ""
				if err := sender.native.ReplaceTrack(nil); err != nil {
					return r.val(cameraFailure("InvalidModificationError", err.Error(), "")), nil
				}
				if sender.cancel != nil {
					sender.cancel()
				}
				return r.val(map[string]any{}), nil
			}
			track := r.cameraTracks[id]
			if track == nil || track.stopped {
				return r.val(cameraFailure("InvalidStateError", "Track is not live", "")), nil
			}
			kind := webrtc.RTPCodecTypeVideo
			if track.kind == "audio" {
				kind = webrtc.RTPCodecTypeAudio
			}
			if sender.kind != kind {
				return r.val(cameraFailure("TypeError", "Cannot replace a track with a different kind", "")), nil
			}
			if err := sender.native.ReplaceTrack(sender.local); err != nil {
				return r.val(cameraFailure("InvalidModificationError", err.Error(), "")), nil
			}
			if sender.cancel != nil {
				sender.cancel()
			}
			if err := r.startRTCMediaSender(peer, sender, track); err != nil {
				return r.val(cameraFailure("OperationError", err.Error(), "")), nil
			}
			return r.val(map[string]any{}), nil
		case "parameters":
			sender := peer.senders[strarg(a, 2)]
			if sender == nil {
				return nil, fmt.Errorf("unknown sender")
			}
			encoded, _ := json.Marshal(sender.native.GetParameters())
			var result any
			json.Unmarshal(encoded, &result)
			return r.val(result), nil
		case "close":
			for _, channel := range peer.channels {
				channel.cancel()
			}
			for _, sender := range peer.senders {
				if sender.cancel != nil {
					sender.cancel()
				}
			}
			peer.pc.Close()
			return r.val(map[string]any{}), nil
		}
		return nil, fmt.Errorf("unsupported RTC track operation")
	})
	h["rtcMediaOperation"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		peer := r.rtcMediaPeers[strarg(a, 0)]
		if peer == nil {
			return nil, fmt.Errorf("unknown peer")
		}
		operation, raw := strarg(a, 1), strarg(a, 2)
		promise := newHostPromise(r.runtime)
		r.resourceWG.Add(1)
		go func() {
			defer r.resourceWG.Done()
			var result any = map[string]any{}
			var err error
			switch operation {
			case "offer":
				var options webrtc.OfferOptions
				json.Unmarshal([]byte(raw), &options)
				var description webrtc.SessionDescription
				description, err = peer.pc.CreateOffer(&options)
				result = rtcMediaDescription(&description)
			case "answer":
				var description webrtc.SessionDescription
				description, err = peer.pc.CreateAnswer(nil)
				result = rtcMediaDescription(&description)
			case "local", "remote":
				var description webrtc.SessionDescription
				err = json.Unmarshal([]byte(raw), &description)
				if err == nil {
					if operation == "local" {
						err = peer.pc.SetLocalDescription(description)
					} else {
						err = peer.pc.SetRemoteDescription(description)
					}
				}
			case "ice":
				if raw != "null" {
					var candidate webrtc.ICECandidateInit
					err = json.Unmarshal([]byte(raw), &candidate)
					if err == nil {
						err = peer.pc.AddICECandidate(candidate)
					}
				}
			case "stats":
				encoded, _ := json.Marshal(peer.pc.GetStats())
				rows := map[string]any{}
				json.Unmarshal(encoded, &rows)
				peer.stats.mergeInto(rows)
				result = rows
			default:
				err = fmt.Errorf("unsupported RTC operation %s", operation)
			}
			if err != nil {
				result = cameraFailure("OperationError", err.Error(), "")
			}
			r.scheduler.Post(scheduler.DOM, 0, func(context.Context) error {
				if err == nil && operation == "remote" && !r.closed && !r.inactive {
					r.announceRTCMedia(peer)
				}
				return promise.Resolve(result)
			})
		}()
		return promise.Value, nil
	})
}
