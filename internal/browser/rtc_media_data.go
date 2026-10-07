package browser

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sync/atomic"

	"github.com/google/uuid"
	"github.com/moreveal/mimic/internal/engine"
	"github.com/moreveal/mimic/internal/scheduler"
	"github.com/pion/webrtc/v4"
)

type rtcDataMessage struct {
	data []byte
	text bool
}

func rtcNullableUint16(value *uint16) any {
	if value == nil {
		return nil
	}
	return int(*value)
}

type rtcMediaDataChannel struct {
	native *webrtc.DataChannel
	queue  chan rtcDataMessage
	cancel context.CancelFunc
	queued atomic.Uint64
}

func (r *Realm) registerRTCMediaData(peer *rtcMediaPeer, native *webrtc.DataChannel) string {
	id := uuid.NewString()
	ctx, cancel := context.WithCancel(r.resourceContext)
	channel := &rtcMediaDataChannel{native: native, queue: make(chan rtcDataMessage, 16), cancel: cancel}
	peer.channels[id] = channel
	native.OnOpen(func() { r.emitRTCMedia(peer.id, "dataopen", id) })
	native.OnClose(func() { cancel(); r.emitRTCMedia(peer.id, "dataclose", id) })
	native.OnError(func(err error) {
		r.emitRTCMedia(peer.id, "dataerror", map[string]any{"id": id, "message": err.Error()})
	})
	native.OnBufferedAmountLow(func() { r.emitRTCMedia(peer.id, "databufferedamountlow", id) })
	native.OnMessage(func(message webrtc.DataChannelMessage) {
		r.emitRTCMedia(peer.id, "datamessage", map[string]any{"id": id, "text": message.IsString, "data": base64.StdEncoding.EncodeToString(message.Data)})
	})
	r.resourceWG.Add(1)
	go func() {
		defer r.resourceWG.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case message := <-channel.queue:
				var err error
				if message.text {
					err = native.SendText(string(message.data))
				} else {
					err = native.Send(message.data)
				}
				channel.queued.Add(0 - uint64(len(message.data)))
				if err != nil {
					r.emitRTCMedia(peer.id, "dataerror", map[string]any{"id": id, "message": err.Error()})
				}
			}
		}
	}()
	return id
}
func addRTCMediaDataHosts(r *Realm, h map[string]any) {
	h["rtcMediaData"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		peer := r.rtcMediaPeers[strarg(a, 0)]
		if peer == nil {
			return nil, fmt.Errorf("unknown peer")
		}
		action, id := strarg(a, 1), strarg(a, 2)
		if action == "create" {
			var init webrtc.DataChannelInit
			if err := json.Unmarshal([]byte(strarg(a, 3)), &init); err != nil {
				return nil, err
			}
			native, err := peer.pc.CreateDataChannel(id, &init)
			if err != nil {
				return r.val(cameraFailure("OperationError", err.Error(), "")), nil
			}
			return r.val(map[string]any{"id": r.registerRTCMediaData(peer, native)}), nil
		}
		channel := peer.channels[id]
		if channel == nil {
			return nil, fmt.Errorf("unknown data channel")
		}
		native := channel.native
		switch action {
		case "state":
			return r.val(map[string]any{"id": rtcNullableUint16(native.ID()), "readyState": native.ReadyState().String(), "bufferedAmount": native.BufferedAmount() + channel.queued.Load(), "bufferedAmountLowThreshold": native.BufferedAmountLowThreshold()}), nil
		case "close":
			channel.cancel()
			return r.val(map[string]any{}), native.Close()
		case "threshold":
			native.SetBufferedAmountLowThreshold(uint64(numarg(a, 3)))
			return r.val(map[string]any{}), nil
		case "send":
			if native.ReadyState() != webrtc.DataChannelStateOpen {
				return r.val(cameraFailure("InvalidStateError", "Data channel is not open", "")), nil
			}
			data, err := base64.StdEncoding.DecodeString(strarg(a, 3))
			if err != nil {
				return nil, err
			}
			if len(data) > 1<<20 || channel.queued.Load()+uint64(len(data)) > 1<<20 {
				return r.val(cameraFailure("OperationError", "Data channel send buffer is full", "")), nil
			}
			channel.queued.Add(uint64(len(data)))
			select {
			case channel.queue <- rtcDataMessage{data: data, text: a[4].Export() == true}:
			default:
				channel.queued.Add(0 - uint64(len(data)))
				return r.val(cameraFailure("OperationError", "Data channel send queue is full", "")), nil
			}
			return r.val(map[string]any{}), nil
		}
		return nil, fmt.Errorf("unsupported data channel operation")
	})
}

func (r *Realm) receiveRTCMediaData(peer *rtcMediaPeer, native *webrtc.DataChannel) {
	r.scheduler.Post(scheduler.DOM, 0, func(context.Context) error {
		if r.closed || r.inactive {
			return nil
		}
		id := r.registerRTCMediaData(peer, native)
		r.emitRTCMedia(peer.id, "datachannel", map[string]any{"id": id, "label": native.Label(), "ordered": native.Ordered(), "maxPacketLifeTime": rtcNullableUint16(native.MaxPacketLifeTime()), "maxRetransmits": rtcNullableUint16(native.MaxRetransmits()), "protocol": native.Protocol(), "negotiated": native.Negotiated()})
		if native.ReadyState() == webrtc.DataChannelStateOpen {
			r.emitRTCMedia(peer.id, "dataopen", id)
		}
		return nil
	})
}
