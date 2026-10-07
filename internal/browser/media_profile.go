package browser

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/moreveal/mimic/internal/camera"
	"github.com/moreveal/mimic/internal/microphone"
)

// A media profile is a Context-owned device catalog, not an Environment
// override. Native bindings are never projected into page-visible identity.
type MediaProcessing struct {
	Resize string  `json:"resize,omitempty"`
	Noise  float64 `json:"noise,omitempty"`
}

type MediaDeviceProfile struct {
	Key         string          `json:"key"`
	Kind        string          `json:"kind"`
	Source      any             `json:"source"`
	Profile     string          `json:"profile,omitempty"`
	Label       string          `json:"label"`
	Group       string          `json:"group,omitempty"`
	Modes       []camera.Format `json:"modes,omitempty"`
	DefaultMode camera.Format   `json:"defaultMode,omitzero"`
	Processing  MediaProcessing `json:"processing,omitzero"`
	nativeID    string
}

type MediaProfile struct {
	Seed    string               `json:"seed"`
	Devices []MediaDeviceProfile `json:"devices"`
}

type mediaProfileRequest struct {
	Seed       string                `json:"seed,omitempty"`
	Camera     *mediaCameraRequest   `json:"camera,omitempty"`
	Microphone *mediaCameraRequest   `json:"microphone,omitempty"`
	Devices    *[]MediaDeviceProfile `json:"devices,omitempty"`
}

type mediaCameraRequest struct {
	Source    any                  `json:"source"`
	Profile   string               `json:"profile"`
	Overrides *mediaDeviceOverride `json:"overrides,omitempty"`
}

type mediaDeviceOverride struct {
	Label       *string          `json:"label,omitempty"`
	Modes       *[]camera.Format `json:"modes,omitempty"`
	DefaultMode *camera.Format   `json:"defaultMode,omitempty"`
	Processing  *MediaProcessing `json:"processing,omitempty"`
}

type MediaSource struct {
	SourceID string `json:"sourceId"`
	Kind     string `json:"kind"`
	Label    string `json:"label"`
	Default  bool   `json:"default"`
	nativeID string
}

func (c *Context) mediaSourceID(kind, nativeID string) string {
	sum := sha256.Sum256([]byte(c.ID + "\x00media-source\x00" + kind + "\x00" + nativeID))
	return hex.EncodeToString(sum[:])
}

// Enumeration does not open native capture. One unavailable backend must not
// conceal devices from the other backend; diagnostics remain private to CDP.
func (c *Context) MediaSources() ([]MediaSource, map[string]string) {
	ctx, cancel := context.WithTimeout(c.lifetime, 10*time.Second)
	defer cancel()
	rows := []MediaSource{}
	diagnostics := map[string]string{}
	cameras, err := c.browser.cameraProvider.Devices(ctx)
	if err != nil {
		diagnostics["videoinput"] = err.Error()
	}
	for i, d := range cameras {
		rows = append(rows, MediaSource{SourceID: c.mediaSourceID("videoinput", d.ID), Kind: "videoinput", Label: d.Label, Default: i == 0, nativeID: d.ID})
	}
	microphones, err := c.browser.microphoneProvider.Devices(ctx)
	if err != nil {
		diagnostics["audioinput"] = err.Error()
	}
	for _, d := range microphones {
		rows = append(rows, MediaSource{SourceID: c.mediaSourceID("audioinput", d.ID), Kind: "audioinput", Label: d.Label, Default: d.Default, nativeID: d.ID})
	}
	return rows, diagnostics
}

func MediaPresets() []map[string]any {
	return []map[string]any{
		{"id": "integrated-webcam", "label": "Integrated Camera", "class": "physical", "variants": []string{"integrated-webcam-hd", "integrated-webcam-fhd"}},
		{"id": "usb-webcam", "label": "USB Camera", "class": "physical", "variants": []string{"usb-webcam-hd", "usb-webcam-fhd"}},
	}
}

func mediaPreset(name, seed, key string) (MediaDeviceProfile, error) {
	hash := sha256.Sum256([]byte("mimic-media\x00" + seed + "\x00" + key))
	if name == "auto" {
		name = []string{"integrated-webcam-hd", "integrated-webcam-fhd", "usb-webcam-hd", "usb-webcam-fhd"}[int(hash[0])%4]
	} else if name == "integrated-webcam" || name == "usb-webcam" {
		name += []string{"-hd", "-fhd"}[int(hash[1])%2]
	}
	label := "USB Camera"
	if strings.HasPrefix(name, "integrated-webcam-") {
		label = "Integrated Camera"
	}
	switch name {
	case "integrated-webcam-hd", "integrated-webcam-fhd", "usb-webcam-hd", "usb-webcam-fhd":
	default:
		return MediaDeviceProfile{}, fmt.Errorf("camera.profile: unknown physical-camera profile %q", name)
	}
	modes := []camera.Format{{Width: 640, Height: 480, FrameRate: 30}, {Width: 1280, Height: 720, FrameRate: 30}}
	if strings.HasSuffix(name, "-fhd") {
		modes = append(modes, camera.Format{Width: 1920, Height: 1080, FrameRate: 30})
	}
	return MediaDeviceProfile{Key: key, Kind: "videoinput", Profile: name, Label: label, Group: key, Modes: modes, DefaultMode: modes[1], Processing: MediaProcessing{Resize: "crop-and-scale"}}, nil
}

func decodeMediaJSON(raw []byte, value any) error {
	var input any
	if err := json.Unmarshal(raw, &input); err != nil {
		return fmt.Errorf("media profile: %w", err)
	}
	var checkNull func(any, string) error
	checkNull = func(value any, path string) error {
		if value == nil {
			return fmt.Errorf("%s: null is not accepted", path)
		}
		switch value := value.(type) {
		case map[string]any:
			for key, child := range value {
				if err := checkNull(child, path+"."+key); err != nil {
					return err
				}
			}
		case []any:
			for i, child := range value {
				if err := checkNull(child, fmt.Sprintf("%s[%d]", path, i)); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := checkNull(input, "media"); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return fmt.Errorf("media profile: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("media profile: expected one JSON object")
	}
	return nil
}

func resolveMediaSource(selector any, kind string, sources []MediaSource) (MediaSource, error) {
	var matches []MediaSource
	for _, source := range sources {
		if source.Kind != kind {
			continue
		}
		match := false
		switch selector := selector.(type) {
		case string:
			match = selector == "obs" && kind == "videoinput" && strings.EqualFold(source.Label, "OBS Virtual Camera") || selector == "default" && source.Default
		case map[string]any:
			if len(selector) == 1 {
				match = selector["sourceId"] == source.SourceID || selector["label"] == source.Label
			}
		}
		if match {
			matches = append(matches, source)
		}
	}
	if len(matches) != 1 {
		return MediaSource{}, fmt.Errorf("source: selector must match exactly one %s input (matched %d); inspect Mimic.getMediaSources", kind, len(matches))
	}
	return matches[0], nil
}

func (c *Context) ValidateMediaProfileJSON(raw []byte) (MediaProfile, error) {
	var request mediaProfileRequest
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return MediaProfile{}, fmt.Errorf("media profile: expected an object; devices: [] disables capture exposure")
	}
	if err := decodeMediaJSON(raw, &request); err != nil {
		return MediaProfile{}, err
	}
	if request.Devices != nil && (request.Camera != nil || request.Microphone != nil) {
		return MediaProfile{}, fmt.Errorf("devices cannot be combined with camera or microphone shorthand")
	}
	if request.Devices == nil && request.Camera == nil && request.Microphone == nil {
		return MediaProfile{}, fmt.Errorf("camera, microphone or devices is required")
	}
	seed := request.Seed
	if seed == "" {
		c.mu.RLock()
		seed = c.mediaSeed
		c.mu.RUnlock()
		if seed == "" {
			seed = c.ID
		}
	}
	if len(seed) > 1024 {
		return MediaProfile{}, fmt.Errorf("seed exceeds 1024 bytes")
	}
	result := MediaProfile{Seed: seed, Devices: []MediaDeviceProfile{}}
	if request.Devices != nil {
		result.Devices = append(result.Devices, (*request.Devices)...)
	}
	if request.Camera != nil {
		name := request.Camera.Profile
		if name == "" {
			name = "auto"
		}
		device, err := mediaPreset(name, seed, "camera")
		if err != nil {
			return MediaProfile{}, err
		}
		device.Source = request.Camera.Source
		if device.Source == nil {
			device.Source = "default"
		}
		if override := request.Camera.Overrides; override != nil {
			if override.Label != nil {
				device.Label = *override.Label
			}
			if override.Modes != nil {
				device.Modes = *override.Modes
			}
			if override.DefaultMode != nil {
				device.DefaultMode = *override.DefaultMode
			}
			if override.Processing != nil {
				device.Processing = *override.Processing
			}
		}
		result.Devices = append(result.Devices, device)
	}
	if request.Microphone != nil {
		if request.Microphone.Profile != "" && request.Microphone.Profile != "webcam" {
			return MediaProfile{}, fmt.Errorf("microphone.profile: only webcam is supported")
		}
		if request.Microphone.Overrides != nil {
			return MediaProfile{}, fmt.Errorf("microphone overrides are unsupported")
		}
		label, group := "Microphone", "microphone"
		if request.Camera != nil {
			label, group = "Microphone ("+result.Devices[0].Label+")", "camera"
		}
		source := request.Microphone.Source
		if source == nil {
			source = "default"
		}
		result.Devices = append(result.Devices, MediaDeviceProfile{Key: "microphone", Kind: "audioinput", Source: source, Label: label, Group: group})
	}
	if len(result.Devices) > 16 {
		return MediaProfile{}, fmt.Errorf("media profile: at most 16 configured inputs")
	}
	var sources []MediaSource
	var diagnostics map[string]string
	if len(result.Devices) != 0 {
		sources, diagnostics = c.MediaSources()
	}
	keys := map[string]bool{}
	for i := range result.Devices {
		d := &result.Devices[i]
		if d.Key == "" || len(d.Key) > 256 || keys[d.Key] {
			return MediaProfile{}, fmt.Errorf("devices[%d].key: expected a unique nonempty key", i)
		}
		keys[d.Key] = true
		if d.Kind != "videoinput" && d.Kind != "audioinput" {
			return MediaProfile{}, fmt.Errorf("devices[%d].kind: unsupported capture kind", i)
		}
		if d.Label == "" || len(d.Label) > 1024 || len(d.Group) > 256 {
			return MediaProfile{}, fmt.Errorf("devices[%d]: invalid label or group", i)
		}
		if d.Kind == "videoinput" {
			if err := validateMediaCamera(d); err != nil {
				return MediaProfile{}, fmt.Errorf("devices[%d]: %w", i, err)
			}
		} else if d.Profile != "" || len(d.Modes) != 0 || d.DefaultMode != (camera.Format{}) || d.Processing != (MediaProcessing{}) {
			return MediaProfile{}, fmt.Errorf("devices[%d]: video processing on audio input", i)
		}
		source, err := resolveMediaSource(d.Source, d.Kind, sources)
		if err != nil {
			if detail := diagnostics[d.Kind]; detail != "" {
				return MediaProfile{}, fmt.Errorf("devices[%d]: input enumeration failed: %s", i, detail)
			}
			return MediaProfile{}, fmt.Errorf("devices[%d]: %w", i, err)
		}
		d.nativeID = source.nativeID
		d.Source = map[string]any{"sourceId": source.SourceID}
	}
	return result, nil
}

func validateMediaCamera(d *MediaDeviceProfile) error {
	if d.Profile != "" {
		if d.Profile == "auto" || d.Profile == "integrated-webcam" || d.Profile == "usb-webcam" {
			return fmt.Errorf("profile: advanced devices require a resolved variant; use camera shorthand to generate a complete preset")
		}
		base, err := mediaPreset(d.Profile, "", d.Key)
		if err != nil {
			return err
		}
		width, height, fps := mediaCameraLimits(&base)
		for _, mode := range d.Modes {
			if mode.Width > width || mode.Height > height || mode.FrameRate > fps {
				return fmt.Errorf("modes exceed the selected physical profile's dimensions or FPS; use a custom recipe without profile for different limits")
			}
		}
	}
	if d.Processing.Resize != "" && d.Processing.Resize != "none" && d.Processing.Resize != "crop-and-scale" {
		return fmt.Errorf("processing.resize: unsupported transform")
	}
	if math.IsNaN(d.Processing.Noise) || math.IsInf(d.Processing.Noise, 0) || d.Processing.Noise < 0 || d.Processing.Noise > 8 {
		return fmt.Errorf("processing.noise: expected 0..8")
	}
	if len(d.Modes) == 0 || len(d.Modes) > 64 {
		return fmt.Errorf("modes: expected 1..64 output recipes")
	}
	if d.Processing.Resize != "crop-and-scale" && len(d.Modes) != 1 {
		return fmt.Errorf("multiple output modes require crop-and-scale processing")
	}
	defaultFound := false
	for _, mode := range d.Modes {
		if mode.Width < 1 || mode.Height < 1 || mode.Width > 8192 || mode.Height > 8192 || mode.Width*mode.Height > 16*1024*1024 || mode.FrameRate < 1 || mode.FrameRate > 120 || math.IsNaN(mode.FrameRate) || math.IsInf(mode.FrameRate, 0) {
			return fmt.Errorf("modes: invalid output dimensions or frame rate")
		}
		if mode.Width == d.DefaultMode.Width && mode.Height == d.DefaultMode.Height && mode.FrameRate == d.DefaultMode.FrameRate {
			defaultFound = true
		}
	}
	if !defaultFound {
		return fmt.Errorf("defaultMode must be a member of modes")
	}
	return nil
}

func (c *Context) SetMediaProfileJSON(raw []byte) (MediaProfile, error) {
	profile, err := c.ValidateMediaProfileJSON(raw)
	if err != nil {
		return MediaProfile{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.lifetime.Err(); err != nil {
		return MediaProfile{}, err
	}
	if c.mediaCaptures != 0 {
		return MediaProfile{}, fmt.Errorf("media profile cannot change while capture is starting or live")
	}
	c.mediaProfile = &profile
	c.mediaSeed = profile.Seed
	return cloneMediaProfile(profile), nil
}

func cloneMediaProfile(profile MediaProfile) MediaProfile {
	// Public snapshots never expose native IDs or share mutable config slices.
	raw, _ := json.Marshal(profile)
	var result MediaProfile
	_ = json.Unmarshal(raw, &result)
	return result
}

func (c *Context) MediaProfile() *MediaProfile {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.mediaProfile == nil {
		return nil
	}
	profile := cloneMediaProfile(*c.mediaProfile)
	return &profile
}

func (c *Context) reserveMediaCapture() func() {
	c.mu.Lock()
	c.mediaCaptures++
	c.mu.Unlock()
	return func() { c.mu.Lock(); c.mediaCaptures--; c.mu.Unlock() }
}

type profileCameraDevice struct {
	camera.Device
	media *MediaDeviceProfile
}
type profileMicrophoneDevice struct {
	microphone.Device
	media *MediaDeviceProfile
}

func projectMediaCatalog(profile *MediaProfile, cameras []camera.Device, microphones []microphone.Device) ([]profileCameraDevice, []profileMicrophoneDevice) {
	video := []profileCameraDevice{}
	audio := []profileMicrophoneDevice{}
	if profile == nil {
		for _, d := range cameras {
			video = append(video, profileCameraDevice{Device: d})
		}
		for _, d := range microphones {
			audio = append(audio, profileMicrophoneDevice{Device: d})
		}
		return video, audio
	}
	for i := range profile.Devices {
		d := &profile.Devices[i]
		if d.Kind == "videoinput" {
			for _, native := range cameras {
				if native.ID == d.nativeID {
					video = append(video, profileCameraDevice{Device: camera.Device{ID: d.Key, Label: d.Label}, media: d})
					break
				}
			}
		}
		if d.Kind == "audioinput" {
			for _, native := range microphones {
				if native.ID == d.nativeID {
					audio = append(audio, profileMicrophoneDevice{Device: microphone.Device{ID: d.Key, Label: d.Label, Default: len(audio) == 0}, media: d})
					break
				}
			}
		}
	}
	return video, audio
}
func (c *Context) currentMediaProfile() *MediaProfile {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.mediaProfile
}
func (r *Realm) mediaGroupID(kind, key string, d *MediaDeviceProfile) string {
	if d != nil {
		group := d.Group
		if group == "" {
			group = d.Key
		}
		return r.cameraDeviceID("media-group:" + group)
	}
	if kind == "audioinput" {
		return r.cameraDeviceID("audio-group:" + key)
	}
	return r.cameraDeviceID("group:" + key)
}
func (r *Realm) mediaDeviceID(kind, key string, d *MediaDeviceProfile) string {
	if d != nil {
		return r.cameraDeviceID("media-device:" + kind + ":" + key)
	}
	if kind == "audioinput" {
		return r.cameraDeviceID("audio:" + key)
	}
	return r.cameraDeviceID(key)
}

func (r *Realm) mediaBackendFailure(kind string, err error) string {
	c := r.agent.Page().ctx
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.mediaProfile == nil {
		return err.Error()
	}
	if c.mediaDiagnostics == nil {
		c.mediaDiagnostics = map[string]string{}
	}
	c.mediaDiagnostics[kind] = err.Error()
	return "Capture input is unavailable"
}

func (c *Context) MediaDiagnostics() map[string]string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := map[string]string{}
	for kind, message := range c.mediaDiagnostics {
		result[kind] = message
	}
	return result
}
