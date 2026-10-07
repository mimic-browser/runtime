package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/moreveal/mimic/internal/camera"
	"github.com/moreveal/mimic/internal/engine"
	"github.com/moreveal/mimic/internal/microphone"
	"github.com/moreveal/mimic/internal/scheduler"
	"github.com/moreveal/mimic/internal/trace"
)

func selectMicrophoneFormat(constraints map[string]any) (microphone.Format, error) {
	format := microphone.Format{SampleRate: 48000, Channels: 1}
	best := math.Inf(1)
	for _, channels := range []int{1, 2} {
		if ok, distance := cameraNumberConstraint(constraints["channelCount"], float64(channels)); ok && distance < best {
			format.Channels = channels
			best = distance
		}
	}
	if math.IsInf(best, 1) {
		return format, &cameraConstraintError{"channelCount"}
	}
	return format, validateMicrophoneFormat(format, constraints)
}
func validateMicrophoneFormat(format microphone.Format, constraints map[string]any) error {
	for _, parameter := range []struct {
		name  string
		value float64
	}{{"sampleRate", float64(format.SampleRate)}, {"sampleSize", 16}, {"channelCount", float64(format.Channels)}, {"latency", 0.02}} {
		if ok, _ := cameraNumberConstraint(constraints[parameter.name], parameter.value); !ok {
			return &cameraConstraintError{parameter.name}
		}
	}
	for _, name := range []string{"echoCancellation", "noiseSuppression", "autoGainControl", "voiceIsolation"} {
		if dictionary, ok := constraints[name].(map[string]any); ok && dictionary["exact"] == true {
			return &cameraConstraintError{name}
		}
	}
	return nil
}
func (r *Realm) applyMicrophoneConstraints(track *cameraTrack, constraints map[string]any) map[string]any {
	for _, property := range []struct{ name, value string }{{"deviceId", track.deviceID}, {"groupId", track.groupID}} {
		if exact, _ := cameraDeviceConstraint(constraints[property.name]); len(exact) > 0 && !containsString(exact, property.value) {
			return cameraFailure("OverconstrainedError", "Device cannot be changed", property.name)
		}
	}
	if err := validateMicrophoneFormat(track.source.audioFormat, constraints); err != nil {
		return cameraFailure("OverconstrainedError", err.Error(), err.(*cameraConstraintError).name)
	}
	return nil
}
func microphoneCapabilities(track *cameraTrack) map[string]any {
	f := track.source.audioFormat
	return map[string]any{"deviceId": track.deviceID, "groupId": track.groupID, "sampleRate": map[string]any{"min": f.SampleRate, "max": f.SampleRate}, "sampleSize": map[string]any{"min": 16, "max": 16}, "channelCount": map[string]any{"min": f.Channels, "max": f.Channels}, "latency": map[string]any{"min": 0.02, "max": 0.02}, "echoCancellation": []bool{false}, "noiseSuppression": []bool{false}, "autoGainControl": []bool{false}, "voiceIsolation": []bool{false}}
}

// Publish one immutable PCM block to bounded per-sender queues. Audio encoding
// needs neither a polling timer nor a Go-to-JS crossing per packet.
func (r *Realm) publishMicrophonePCM(source *cameraSource, pcm []float32) bool {
	source.mu.Lock()
	defer source.mu.Unlock()
	if source.context.Err() != nil {
		return false
	}
	first := source.pcm == nil
	source.pcm = pcm
	channels := source.audioFormat.Channels
	if len(source.pcmHistory) == 0 {
		source.pcmHistory = make([]float32, 7680*channels)
	}
	for offset := 0; offset < len(pcm); offset += channels {
		position := (source.pcmSamples % 7680) * uint64(channels)
		copy(source.pcmHistory[position:position+uint64(channels)], pcm[offset:offset+channels])
		source.pcmSamples++
	}
	source.sequence++
	source.stamp = time.Now()
	for channel := range source.audioSubscribers {
		select {
		case channel <- pcm:
		default:
			select {
			case <-channel:
			default:
			}
			select {
			case channel <- pcm:
			default:
			}
		}
	}
	return first
}

func (r *Realm) openMicrophone(constraints map[string]any, promise engine.Promise) {
	format, err := selectMicrophoneFormat(constraints)
	if err != nil {
		promise.Resolve(cameraFailure("OverconstrainedError", err.Error(), err.(*cameraConstraintError).name))
		return
	}
	releaseCapture := r.agent.Page().ctx.reserveMediaCapture()
	ctx, stop := context.WithCancel(r.resourceContext)
	loadContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	r.resourceWG.Add(1)
	go func() {
		defer r.resourceWG.Done()
		defer releaseCapture()
		defer stop()
		defer cancel()
		resolveFailure := func(err error, name, constraint string) {
			message := err.Error()
			if name == "NotReadableError" {
				message = r.mediaBackendFailure("audioinput", err)
			}
			r.scheduler.Post(scheduler.DOM, 0, func(context.Context) error {
				return promise.Resolve(cameraFailure(name, message, constraint))
			})
		}
		provider := r.agent.Page().ctx.browser.microphoneProvider
		nativeDevices, err := provider.Devices(loadContext)
		_, devices := projectMediaCatalog(r.agent.Page().ctx.currentMediaProfile(), nil, nativeDevices)
		if err != nil {
			resolveFailure(err, "NotReadableError", "")
			return
		}
		exact, ideal := cameraDeviceConstraint(constraints["deviceId"])
		groupExact, groupIdeal := cameraDeviceConstraint(constraints["groupId"])
		var chosen *profileMicrophoneDevice
		for i := range devices {
			d := &devices[i]
			id := r.mediaDeviceID("audioinput", d.ID, d.media)
			group := r.mediaGroupID("audioinput", d.ID, d.media)
			if len(exact) > 0 && !containsString(exact, id) || len(groupExact) > 0 && !containsString(groupExact, group) {
				continue
			}
			if chosen == nil || d.Default {
				chosen = d
			}
			if containsString(ideal, id) || containsString(groupIdeal, group) {
				chosen = d
				break
			}
		}
		if chosen == nil {
			name, constraint := "NotFoundError", ""
			if len(exact) > 0 {
				name, constraint = "OverconstrainedError", "deviceId"
			} else if len(groupExact) > 0 {
				name, constraint = "OverconstrainedError", "groupId"
			}
			resolveFailure(fmt.Errorf("requested microphone not found"), name, constraint)
			return
		}
		nativeID := chosen.ID
		if chosen.media != nil {
			nativeID = chosen.media.nativeID
		}
		capture, err := provider.Open(loadContext, nativeID, format)
		if err != nil {
			resolveFailure(err, "NotReadableError", "")
			return
		}
		defer capture.Close()
		pcm, err := capture.Read(loadContext)
		if err != nil {
			resolveFailure(err, "NotReadableError", "")
			return
		}
		if len(pcm) != 960*format.Channels {
			resolveFailure(fmt.Errorf("microphone delivered an invalid PCM block"), "NotReadableError", "")
			return
		}
		cancel()
		source := &cameraSource{context: ctx, cancel: stop, device: camera.Device{ID: chosen.ID, Label: chosen.Label}, audioFormat: format, mediaDevice: chosen.media}
		r.publishMicrophonePCM(source, pcm)
		accepted := make(chan bool, 1)
		r.scheduler.Post(scheduler.DOM, 0, func(context.Context) error {
			if r.closed || r.inactive {
				accepted <- false
				return nil
			}
			if r.capturePermission("microphone") != "granted" {
				accepted <- false
				return promise.Resolve(cameraFailure("NotAllowedError", "Permission denied", ""))
			}
			track := &cameraTrack{kind: "audio", id: uuid.NewString(), source: source, enabled: true, deviceID: r.mediaDeviceID("audioinput", chosen.ID, chosen.media), groupID: r.mediaGroupID("audioinput", chosen.ID, chosen.media), constraints: constraints}
			if r.cameraTracks == nil {
				r.cameraTracks = map[string]*cameraTrack{}
			}
			r.cameraTracks[track.id] = track
			accepted <- true
			return promise.Resolve(track.snapshot())
		})
		select {
		case ok := <-accepted:
			if !ok {
				return
			}
		case <-ctx.Done():
			return
		}
		lastEvent := time.Time{}
		endCapture := func(failure error) {
			r.mediaBackendFailure("audioinput", failure)
			r.agent.Page().Trace().Add(trace.Error, "microphoneCapture", map[string]any{"error": failure.Error()})
			r.scheduler.Post(scheduler.DOM, 0, func(taskContext context.Context) error {
				for _, track := range r.cameraTracks {
					if track.source == source && !track.stopped {
						r.stopCameraTrack(track)
						if r.cameraNotifier != nil {
							if _, err := r.runtime.Call(taskContext, r.cameraNotifier, nil, r.val(track.id), r.val("ended")); err != nil {
								return err
							}
						}
					}
				}
				return nil
			})
		}
		for ctx.Err() == nil {
			pcm, err = capture.Read(ctx)
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					endCapture(err)
				}
				return
			}
			if len(pcm) != 960*format.Channels {
				endCapture(fmt.Errorf("microphone PCM block size changed"))
				return
			}
			r.publishMicrophonePCM(source, pcm)
			if time.Since(lastEvent) >= time.Second/30 {
				lastEvent = time.Now()
				r.queueCameraFrame(source)
			}
		}
	}()
}

func addMicrophoneHosts(r *Realm, h map[string]any) {
	h["microphoneOpen"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		promise := newHostPromise(r.runtime)
		constraints := map[string]any{}
		if err := json.Unmarshal([]byte(strarg(a, 0)), &constraints); err != nil {
			return nil, err
		}
		if r.capturePermission("microphone") != "granted" {
			promise.Resolve(cameraFailure("NotAllowedError", "Permission denied", ""))
		} else {
			r.openMicrophone(constraints, promise)
		}
		return promise.Value, nil
	})
}
