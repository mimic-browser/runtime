package cdp

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	chrome152 "github.com/moreveal/mimic/chrome/152"
	"github.com/moreveal/mimic/internal/browser"
	"github.com/moreveal/mimic/internal/camera"
	v8engine "github.com/moreveal/mimic/internal/engine/v8"
	"github.com/moreveal/mimic/internal/microphone"
)

type mediaProfileCameraFixture struct{}

func (mediaProfileCameraFixture) Devices(context.Context) ([]camera.Device, error) {
	return []camera.Device{{ID: "native-obs-path", Label: "OBS Virtual Camera"}}, nil
}
func (mediaProfileCameraFixture) Open(context.Context, string, func([]camera.Format) (camera.Format, error)) (camera.Capture, camera.Format, error) {
	return nil, camera.Format{}, fmt.Errorf("OBS Virtual Camera native-obs-path unavailable")
}

type mediaProfileMicrophoneFixture struct{}

func (mediaProfileMicrophoneFixture) Devices(context.Context) ([]microphone.Device, error) {
	return []microphone.Device{{ID: "native-mic-path", Label: "Native microphone", Default: true}}, nil
}
func (mediaProfileMicrophoneFixture) Open(context.Context, string, microphone.Format) (microphone.Capture, error) {
	return nil, fmt.Errorf("Native microphone native-mic-path unavailable")
}

func TestMimicMediaProfileCommands(t *testing.T) {
	b, err := browser.NewWithOptions(v8engine.Factory{}, chrome152.New(), browser.Options{CameraProvider: mediaProfileCameraFixture{}, MicrophoneProvider: mediaProfileMicrophoneFixture{}})
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(b)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = s.Serve(listener) }()
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	c := browserConnection(t, listener.Addr().String())
	devices := wireCall(t, c, 1, "Mimic.getMediaSources", map[string]any{})
	if len(devices["sources"].([]any)) != 2 {
		t.Fatal(devices)
	}
	source := devices["sources"].([]any)[0].(map[string]any)
	if source["sourceId"] == "native-obs-path" {
		t.Fatal("native ID used as binding token")
	}
	presets := wireCall(t, c, 2, "Mimic.getMediaPresets", map[string]any{})
	if len(presets["presets"].([]any)) != 2 {
		t.Fatal(presets)
	}
	config := map[string]any{"seed": "persona-42", "camera": map[string]any{"source": "obs", "profile": "auto"}, "microphone": map[string]any{"source": "default", "profile": "webcam"}}
	validated := wireCall(t, c, 3, "Mimic.validateMediaProfile", config)
	if validated["nativeModesVerified"] != false || s.Context.MediaProfile() != nil {
		t.Fatal("validation mutated capture/catalog")
	}
	configured := wireCall(t, c, 4, "Mimic.setMediaProfile", config)
	got := wireCall(t, c, 5, "Mimic.getMediaProfile", map[string]any{})
	first, _ := json.Marshal(configured["profile"])
	second, _ := json.Marshal(got["profile"])
	if string(first) != string(second) {
		t.Fatal("different normalized catalog")
	}
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "<!doctype html>") }))
	defer fixture.Close()
	if err := s.Page.Navigate(context.Background(), fixture.URL); err != nil {
		t.Fatal(err)
	}
	for i, kind := range []string{"camera", "microphone"} {
		wireCall(t, c, 10+i, "Browser.setPermission", map[string]any{"permission": map[string]any{"name": kind}, "setting": "granted", "origin": fixture.URL})
	}
	value, err := evaluatePageFixture(s.Page, `navigator.mediaDevices
  .enumerateDevices()
  .then(
    (ds) =>
      ds.length === 2 &&
      ds[0].groupId === ds[1].groupId &&
      ds.every(
        (d) =>
          !d.label.includes('OBS') && !d.label.includes('Native') && !d.deviceId.includes('native'),
      ),
  );`)
	if err != nil || value != true {
		t.Fatalf("web projection: %v %v", value, err)
	}
	value, err = evaluatePageFixture(s.Page, `navigator.mediaDevices.getUserMedia({ video: true }).then(
  () => false,
  (e) =>
    e.name === 'NotReadableError' &&
    !e.message.includes('OBS') &&
    !e.message.includes('native-obs-path'),
);`)
	if err != nil || value != true {
		t.Fatalf("backend failure redaction: %v %v", value, err)
	}
	got = wireCall(t, c, 12, "Mimic.getMediaProfile", map[string]any{})
	if got["diagnostics"].(map[string]any)["videoinput"] == nil {
		t.Fatal("native diagnostic swallowed")
	}
	value, err = evaluatePageFixture(s.Page, `navigator.mediaDevices.getUserMedia({ audio: true }).then(
  () => false,
  (error) =>
    error.name === 'NotReadableError' &&
    !error.message.includes('Native microphone') &&
    !error.message.includes('native-mic-path'),
);`)
	if err != nil || value != true {
		t.Fatalf("microphone backend privacy: %v %v", value, err)
	}
	created := wireCall(t, c, 13, "Mimic.createContext", map[string]any{"profile": map[string]any{"generate": map[string]any{"seed": "persona-42"}}, "media": map[string]any{"camera": map[string]any{"source": "obs", "profile": "auto"}}})
	contextID := created["browserContextId"].(string)
	ctx, ok := s.Browser.Context(contextID)
	if !ok || ctx.MediaProfile() == nil || ctx.MediaProfile().Seed != "persona-42" {
		t.Fatal("context media not installed with profile seed")
	}
	if len(ctx.Pages()) != 0 {
		t.Fatal("configuration started a Page")
	}
	for id, params := range []map[string]any{
		{"browserContextId": "missing"},
		{"camera": map[string]any{"source": "obs", "profile": "virtual"}},
		{"camera": map[string]any{"source": "obs", "profile": "usb-webcam-hd", "overrides": map[string]any{"modes": []any{map[string]any{"width": 3840, "height": 2160, "frameRate": 120}}}}},
	} {
		if err := c.WriteJSON(map[string]any{"id": 20 + id, "method": "Mimic.setMediaProfile", "params": params}); err != nil {
			t.Fatal(err)
		}
		if readReply(t, c, float64(20+id))["error"] == nil {
			t.Fatal("invalid configuration accepted")
		}
	}
	before := len(s.Browser.Contexts())
	if err := c.WriteJSON(map[string]any{"id": 30, "method": "Mimic.createContext", "params": map[string]any{"media": map[string]any{"camera": map[string]any{"source": "obs", "profile": "virtual"}}}}); err != nil {
		t.Fatal(err)
	}
	if readReply(t, c, 30)["error"] == nil || len(s.Browser.Contexts()) != before {
		t.Fatal("failed createContext leaked a Context")
	}
}
