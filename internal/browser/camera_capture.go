package browser

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"math"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/moreveal/mimic/internal/camera"
	"github.com/moreveal/mimic/internal/engine"
	"github.com/moreveal/mimic/internal/microphone"
	"github.com/moreveal/mimic/internal/scheduler"
	"golang.org/x/image/draw"
)

// Native frames belong to the source. Tracks (including clones) hold independent
// lifetime/enabled state and project that same source into script observations.
type cameraSource struct {
	mu                  sync.RWMutex
	context             context.Context
	frame               *image.RGBA
	pcm                 []float32
	pcmHistory          []float32
	pcmSamples          uint64
	audioFormat         microphone.Format
	audioSubscribers    map[chan []float32]struct{}
	sequence            uint64
	stamp               time.Time
	cancel              context.CancelFunc
	format              camera.Format
	device              camera.Device
	notificationPending atomic.Bool
}
type cameraTrack struct {
	kind              string
	id                string
	source            *cameraSource
	enabled           bool
	stopped           bool
	width, height     int
	frameRate         float64
	deviceID, groupID string
	constraints       map[string]any
	remote            bool
	publicID          string
}

func (r *Realm) cameraDeviceID(raw string) string {
	hash := sha256.Sum256([]byte(r.agent.Page().ctx.ID + "\x00" + r.origin + "\x00" + raw))
	return hex.EncodeToString(hash[:])
}
func cameraFailure(name, message, constraint string) map[string]any {
	return map[string]any{"error": name, "message": message, "constraint": constraint}
}

func (r *Realm) cameraPermission() string {
	return r.capturePermission("camera")
}
func (r *Realm) capturePermission(name string) string {
	c := r.agent.Page().ctx
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.originCapabilities(r.origin)
	value := s.permissions[name]
	if value == "" {
		value = s.permissionFallback
	}
	if value == "" {
		value = "prompt"
	}
	return value
}

func (r *Realm) stopCameraTracks() {
	seen := map[*cameraSource]bool{}
	for _, t := range r.cameraTracks {
		t.source.mu.Lock()
		t.stopped = true
		t.source.mu.Unlock()
		if !seen[t.source] {
			seen[t.source] = true
			t.source.cancel()
		}
	}
}
func (r *Realm) stopCameraTrack(t *cameraTrack) {
	t.source.mu.Lock()
	t.stopped = true
	t.source.mu.Unlock()
	for _, other := range r.cameraTracks {
		if other.source == t.source && !other.stopped {
			return
		}
	}
	t.source.cancel()
	t.source.mu.Lock()
	t.source.frame = nil
	t.source.pcm = nil
	t.source.pcmHistory = nil
	t.source.mu.Unlock()
}

func (t *cameraTrack) settings() map[string]any {
	if t.kind == "audio" {
		t.source.mu.RLock()
		defer t.source.mu.RUnlock()
		if t.remote {
			deviceID := t.publicID
			if deviceID == "" {
				deviceID = t.id
			}
			return map[string]any{"deviceId": deviceID, "sampleRate": t.source.audioFormat.SampleRate, "sampleSize": 16, "channelCount": t.source.audioFormat.Channels, "latency": 0.02}
		}
		result := map[string]any{"deviceId": t.deviceID, "groupId": t.groupID}
		// Chrome 152 audio tracks retain capture settings after stop, unlike
		// local video tracks. Keep the source format independently of PCM.
		{
			result["sampleRate"] = t.source.audioFormat.SampleRate
			result["sampleSize"] = 16
			result["channelCount"] = t.source.audioFormat.Channels
			result["latency"] = 0.02
			result["echoCancellation"] = false
			result["noiseSuppression"] = false
			result["autoGainControl"] = false
			result["voiceIsolation"] = false
		}
		return result
	}
	if t.remote {
		deviceID := t.publicID
		if deviceID == "" {
			deviceID = t.id
		}
		result := map[string]any{"deviceId": deviceID}
		if t.stopped {
			return result
		}
		t.source.mu.RLock()
		defer t.source.mu.RUnlock()
		if t.source.frame != nil {
			w, h := t.source.frame.Bounds().Dx(), t.source.frame.Bounds().Dy()
			result["width"], result["height"], result["aspectRatio"], result["frameRate"], result["resizeMode"] = w, h, float64(w)/float64(h), t.source.format.FrameRate, "none"
		}
		return result
	}
	if t.stopped {
		return map[string]any{"deviceId": t.deviceID, "groupId": t.groupID}
	}
	return map[string]any{"deviceId": t.deviceID, "groupId": t.groupID, "width": t.width, "height": t.height, "aspectRatio": float64(t.width) / float64(t.height), "frameRate": t.frameRate, "resizeMode": "none"}
}
func (t *cameraTrack) snapshot() map[string]any {
	ready := "live"
	if t.stopped {
		ready = "ended"
	}
	t.source.mu.RLock()
	muted := t.remote && t.source.frame == nil && t.source.pcm == nil
	t.source.mu.RUnlock()
	publicID := t.publicID
	if publicID == "" {
		publicID = t.id
	}
	kind := t.kind
	if kind == "" {
		kind = "video"
	}
	return map[string]any{"id": t.id, "publicID": publicID, "kind": kind, "label": t.source.device.Label, "enabled": t.enabled, "muted": muted, "readyState": ready, "settings": t.settings(), "constraints": t.constraints}
}

// Callback delivery is coalesced: a slow Page retains only the latest frame,
// rather than an unbounded queue of image buffers or execution tasks.
func (r *Realm) queueCameraFrame(s *cameraSource) {
	if s.notificationPending.Swap(true) {
		return
	}
	r.scheduler.Post(scheduler.DOM, 0, func(ctx context.Context) error {
		s.notificationPending.Store(false)
		if r.closed || r.inactive || r.cameraNotifier == nil {
			return nil
		}
		for _, t := range r.cameraTracks {
			if t.source == s && !t.stopped {
				_, err := r.runtime.Call(ctx, r.cameraNotifier, nil, r.val(t.id), r.val("frame"))
				if err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (r *Realm) openCamera(constraints map[string]any, promise engine.Promise) {
	loadContext, cancel := context.WithTimeout(r.resourceContext, 10*time.Second)
	sourceContext, stop := context.WithCancel(r.resourceContext)
	r.resourceWG.Add(1)
	go func() {
		defer r.resourceWG.Done()
		defer cancel()
		defer stop()
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		provider := r.agent.Page().ctx.browser.cameraProvider
		devices, err := provider.Devices(loadContext)
		result := cameraFailure("NotFoundError", "Requested device not found", "")
		var chosen *camera.Device
		exact, ideal := cameraDeviceConstraint(constraints["deviceId"])
		groupExact, groupIdeal := cameraDeviceConstraint(constraints["groupId"])
		for i := range devices {
			id := r.cameraDeviceID(devices[i].ID)
			if len(exact) > 0 && !containsString(exact, id) {
				continue
			}
			group := r.cameraDeviceID("group:" + devices[i].ID)
			if len(groupExact) > 0 && !containsString(groupExact, group) {
				continue
			}
			if chosen == nil {
				chosen = &devices[i]
			}
			if containsString(ideal, id) || containsString(groupIdeal, group) {
				chosen = &devices[i]
				break
			}
		}
		if len(exact) > 0 && chosen == nil {
			result = cameraFailure("OverconstrainedError", "No matching camera", "deviceId")
		} else if len(groupExact) > 0 && chosen == nil {
			result = cameraFailure("OverconstrainedError", "No matching camera group", "groupId")
		}
		if err != nil {
			result = cameraFailure("NotReadableError", err.Error(), "")
		}
		var capture camera.Capture
		var format camera.Format
		if err == nil && chosen != nil {
			capture, format, err = provider.Open(sourceContext, chosen.ID, func(formats []camera.Format) (camera.Format, error) { return selectCameraFormat(formats, constraints) })
			if err != nil {
				result = cameraFailure("NotReadableError", err.Error(), "")
				var constraint *cameraConstraintError
				if errors.As(err, &constraint) {
					result = cameraFailure("OverconstrainedError", err.Error(), constraint.name)
				}
			}
		}
		if capture == nil {
			cancel()
			r.scheduler.Post(scheduler.DOM, 0, func(context.Context) error { return promise.Resolve(result) })
			return
		}
		defer capture.Close()
		var first image.Image
		var release func()
		for loadContext.Err() == nil {
			first, release, err = capture.Read()
			if err == nil {
				break
			}
			if !errors.Is(err, camera.ErrFrameTimeout) {
				break
			}
		}
		if err != nil || first == nil || loadContext.Err() != nil {
			cancel()
			if release != nil {
				release()
			}
			result = cameraFailure("NotReadableError", "Camera did not deliver a frame", "")
			if err != nil {
				result["message"] = err.Error()
			}
			r.scheduler.Post(scheduler.DOM, 0, func(context.Context) error { return promise.Resolve(result) })
			return
		}
		// Replace only the startup deadline; capture lifetime follows the document.
		cancel()
		s := &cameraSource{context: sourceContext, cancel: stop, format: format, device: *chosen}
		defer stop()
		storeFrame := func(img image.Image) {
			rgba := image.NewRGBA(image.Rect(0, 0, img.Bounds().Dx(), img.Bounds().Dy()))
			draw.Draw(rgba, rgba.Bounds(), img, img.Bounds().Min, draw.Src)
			s.mu.Lock()
			if sourceContext.Err() != nil {
				s.mu.Unlock()
				return
			}
			s.frame = rgba
			s.sequence++
			s.stamp = time.Now()
			s.mu.Unlock()
		}
		storeFrame(first)
		release()
		accepted := make(chan bool, 1)
		r.scheduler.Post(scheduler.DOM, 0, func(context.Context) error {
			if r.closed || r.inactive {
				accepted <- false
				return nil
			}
			if r.cameraPermission() != "granted" {
				accepted <- false
				return promise.Resolve(cameraFailure("NotAllowedError", "Permission denied", ""))
			}
			t := &cameraTrack{id: uuid.NewString(), source: s, enabled: true, width: format.Width, height: format.Height, frameRate: format.FrameRate, deviceID: r.cameraDeviceID(chosen.ID), groupID: r.cameraDeviceID("group:" + chosen.ID), constraints: constraints}
			if r.cameraTracks == nil {
				r.cameraTracks = map[string]*cameraTrack{}
			}
			r.cameraTracks[t.id] = t
			accepted <- true
			return promise.Resolve(t.snapshot())
		})
		select {
		case ok := <-accepted:
			if !ok {
				return
			}
		case <-sourceContext.Done():
			return
		}
		lastEvent := time.Time{}
		for sourceContext.Err() == nil {
			img, release, readErr := capture.Read()
			if readErr != nil {
				if errors.Is(readErr, camera.ErrFrameTimeout) {
					continue
				}
				r.scheduler.Post(scheduler.DOM, 0, func(ctx context.Context) error {
					for _, t := range r.cameraTracks {
						if t.source == s && !t.stopped {
							r.stopCameraTrack(t)
							if r.cameraNotifier != nil {
								if _, err := r.runtime.Call(ctx, r.cameraNotifier, nil, r.val(t.id), r.val("ended")); err != nil {
									return err
								}
							}
						}
					}
					return nil
				})
				return
			}
			storeFrame(img)
			release()
			// At most one notification per frame period; retained frames remain coherent
			// across consumers even when events are throttled by a busy event loop.
			if time.Since(lastEvent) >= time.Second/30 {
				lastEvent = time.Now()
				r.queueCameraFrame(s)
			}
		}
	}()
}

func addCameraHosts(r *Realm, h map[string]any) {
	h["installCameraNotifier"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) { r.cameraNotifier = a[0]; return nil, nil })
	h["cameraDevices"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		promise := newHostPromise(r.runtime)
		r.resourceWG.Add(1)
		go func() {
			defer r.resourceWG.Done()
			devices, err := r.agent.Page().ctx.browser.cameraProvider.Devices(r.resourceContext)
			microphones, audioErr := r.agent.Page().ctx.browser.microphoneProvider.Devices(r.resourceContext)
			r.scheduler.Post(scheduler.DOM, 0, func(context.Context) error {
				rows := []map[string]any{}
				// The retained Chrome 152 enumeration groups audio inputs before video
				// inputs. Native backend availability still determines membership.
				audioAllowed := r.capturePermission("microphone") == "granted"
				for _, d := range microphones {
					row := map[string]any{"deviceId": "", "groupId": "", "label": "", "kind": "audioinput"}
					if audioAllowed {
						row["deviceId"] = r.cameraDeviceID("audio:" + d.ID)
						row["groupId"] = r.cameraDeviceID("audio-group:" + d.ID)
						row["label"] = d.Label
					}
					rows = append(rows, row)
					if !audioAllowed {
						break
					}
				}
				allowed := r.cameraPermission() == "granted"
				for _, d := range devices {
					if !allowed {
						rows = append(rows, map[string]any{"deviceId": "", "groupId": "", "label": "", "kind": "videoinput"})
						break
					}
					rows = append(rows, map[string]any{"deviceId": r.cameraDeviceID(d.ID), "groupId": r.cameraDeviceID("group:" + d.ID), "label": d.Label, "kind": "videoinput"})
				}
				// An unavailable native backend contributes no devices of its kind;
				// a capture request still reports the concrete backend failure.
				if err != nil && audioErr != nil {
					return promise.Resolve(cameraFailure("NotReadableError", fmt.Sprintf("camera: %v; microphone: %v", err, audioErr), ""))
				}
				return promise.Resolve(map[string]any{"devices": rows})
			})
		}()
		return promise.Value, nil
	})
	h["cameraOpen"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		promise := newHostPromise(r.runtime)
		constraints := map[string]any{}
		if err := json.Unmarshal([]byte(strarg(a, 0)), &constraints); err != nil {
			return nil, err
		}
		if r.cameraPermission() != "granted" {
			_ = promise.Resolve(cameraFailure("NotAllowedError", "Permission denied", ""))
			return promise.Value, nil
		}
		r.openCamera(constraints, promise)
		return promise.Value, nil
	})
	h["cameraTrack"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		t := r.cameraTracks[strarg(a, 0)]
		if t == nil {
			return nil, fmt.Errorf("unknown camera track")
		}
		switch strarg(a, 1) {
		case "stop":
			r.stopCameraTrack(t)
		case "enabled":
			t.source.mu.Lock()
			t.enabled = a[2].Export() == true
			t.source.mu.Unlock()
		case "clone":
			clone := *t
			clone.id = uuid.NewString()
			clone.publicID = ""
			r.cameraTracks[clone.id] = &clone
			return r.val(clone.snapshot()), nil
		case "apply":
			constraints := map[string]any{}
			if err := json.Unmarshal([]byte(strarg(a, 2)), &constraints); err != nil {
				return nil, err
			}
			if !t.stopped {
				if t.kind == "audio" {
					if failure := r.applyMicrophoneConstraints(t, constraints); failure != nil {
						return r.val(failure), nil
					}
					t.constraints = constraints
					break
				}
				if exact, _ := cameraDeviceConstraint(constraints["deviceId"]); len(exact) > 0 && !containsString(exact, t.deviceID) {
					return r.val(cameraFailure("OverconstrainedError", "Device cannot be changed", "deviceId")), nil
				}
				if exact, _ := cameraDeviceConstraint(constraints["groupId"]); len(exact) > 0 && !containsString(exact, t.groupID) {
					return r.val(cameraFailure("OverconstrainedError", "Device group cannot be changed", "groupId")), nil
				}
				f, err := selectCameraFormat([]camera.Format{t.source.format}, constraints)
				if err != nil {
					var ce *cameraConstraintError
					errors.As(err, &ce)
					name := ""
					if ce != nil {
						name = ce.name
					}
					return r.val(cameraFailure("OverconstrainedError", err.Error(), name)), nil
				}
				t.width, t.height, t.frameRate = f.Width, f.Height, f.FrameRate
				t.constraints = constraints
			}
		case "capabilities":
			if t.remote {
				return r.val(map[string]any{}), nil
			}
			if t.kind == "audio" {
				return r.val(microphoneCapabilities(t)), nil
			}
			f := t.source.format
			return r.val(map[string]any{"deviceId": t.deviceID, "groupId": t.groupID, "width": map[string]any{"min": f.Width, "max": f.Width}, "height": map[string]any{"min": f.Height, "max": f.Height}, "frameRate": map[string]any{"min": f.FrameRate, "max": f.FrameRate}, "aspectRatio": map[string]any{"min": float64(f.Width) / float64(f.Height), "max": float64(f.Width) / float64(f.Height)}, "resizeMode": []string{"none"}}), nil
		case "pcm":
			s := t.source
			s.mu.RLock()
			defer s.mu.RUnlock()
			if t.kind != "audio" {
				return r.val(nil), nil
			}
			start, count, rate := numarg(a, 2), int(numarg(a, 3)), numarg(a, 4)
			if count < 0 || count > 32768 || math.IsNaN(start) || math.IsInf(start, 0) || count > 0 && (math.IsNaN(rate) || math.IsInf(rate, 0) || rate < 8000 || rate > 192000) {
				return nil, fmt.Errorf("invalid PCM read range")
			}
			channels := s.audioFormat.Channels
			data := make([]byte, count*channels*4)
			if t.enabled && !t.stopped && len(s.pcmHistory) > 0 {
				capacity := uint64(len(s.pcmHistory) / channels)
				oldest := uint64(0)
				if s.pcmSamples > capacity {
					oldest = s.pcmSamples - capacity
				}
				for frame := 0; frame < count; frame++ {
					position := start + float64(frame)*float64(s.audioFormat.SampleRate)/rate
					index := int64(math.Floor(position))
					if index < 0 || uint64(index) < oldest || uint64(index) >= s.pcmSamples {
						continue
					}
					for channel := 0; channel < channels; channel++ {
						value := s.pcmHistory[(uint64(index)%capacity)*uint64(channels)+uint64(channel)]
						if fraction := float32(position - float64(index)); fraction != 0 && uint64(index)+1 < s.pcmSamples {
							next := s.pcmHistory[((uint64(index)+1)%capacity)*uint64(channels)+uint64(channel)]
							value += (next - value) * fraction
						}
						binary.LittleEndian.PutUint32(data[(frame*channels+channel)*4:], math.Float32bits(value))
					}
				}
			}
			return r.val(map[string]any{"pcm": base64.StdEncoding.EncodeToString(data), "channels": channels, "sampleRate": s.audioFormat.SampleRate, "end": float64(s.pcmSamples), "blockFrames": len(s.pcm) / channels}), nil
		case "frame", "info":
			s := t.source
			s.mu.RLock()
			defer s.mu.RUnlock()
			if t.kind == "audio" {
				if s.pcm == nil || t.stopped {
					return r.val(nil), nil
				}
				if strarg(a, 1) == "frame" {
					return r.val(nil), nil
				}
				return r.val(map[string]any{"width": 0, "height": 0, "sequence": s.sequence, "time": float64(s.stamp.UnixNano()) / 1e9}), nil
			}
			if s.frame == nil || t.stopped {
				return r.val(nil), nil
			}
			if strarg(a, 1) == "info" {
				return r.val(map[string]any{"width": s.frame.Bounds().Dx(), "height": s.frame.Bounds().Dy(), "sequence": s.sequence, "time": float64(s.stamp.UnixNano()) / 1e9}), nil
			}
			pixels := s.frame.Pix
			if !t.enabled {
				pixels = make([]byte, len(pixels))
				for i := 3; i < len(pixels); i += 4 {
					pixels[i] = 255
				}
			}
			return r.val(map[string]any{"width": s.frame.Bounds().Dx(), "height": s.frame.Bounds().Dy(), "pixels": base64.StdEncoding.EncodeToString(pixels), "sequence": s.sequence, "time": float64(s.stamp.UnixNano()) / 1e9, "originClean": true, "colorSpace": "srgb"}), nil
		}
		return r.val(t.snapshot()), nil
	})
}
