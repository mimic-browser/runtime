package cdp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCameraPermissionCommands(t *testing.T) {
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "<!doctype html>") }))
	defer fixture.Close()
	s, addr := runningServer(t)
	c := browserConnection(t, addr)
	if err := s.Page.Navigate(context.Background(), fixture.URL); err != nil {
		t.Fatal(err)
	}
	read := func(name string) any {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		v, err := s.Page.Evaluate(ctx, fmt.Sprintf(`navigator.permissions.query({ name: %q }).then((p) => p.state);`, name))
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	wireCall(t, c, 1, "Browser.grantPermissions", map[string]any{"permissions": []string{"videoCapture"}, "origin": fixture.URL})
	if read("camera") != "granted" || read("microphone") != "denied" {
		t.Fatal("camera allowlist not projected")
	}
	wireCall(t, c, 2, "Browser.setPermission", map[string]any{"permission": map[string]any{"name": "camera"}, "setting": "denied", "origin": fixture.URL})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := s.Page.Evaluate(ctx, `navigator.mediaDevices.getUserMedia({ video: true }).then(
  () => false,
  (e) => e.name === 'NotAllowedError',
);`)
	if err != nil || result != true {
		t.Fatalf("denied completion: %v %v", result, err)
	}
	wireCall(t, c, 3, "Browser.resetPermissions", map[string]any{})
	if read("camera") != "prompt" {
		t.Fatal("reset did not restore camera default")
	}
	result, err = s.Page.Evaluate(ctx, `navigator.mediaDevices.getUserMedia({ video: true }).then(
  () => false,
  (e) => e.name === 'NotAllowedError',
);`)
	if err != nil || result != true {
		t.Fatalf("prompt completion: %v %v", result, err)
	}
	wireCall(t, c, 4, "Browser.setPermission", map[string]any{"permission": map[string]any{"name": "camera"}, "setting": "granted"})
	if read("camera") != "granted" {
		t.Fatal("context-wide camera grant failed")
	}
	wireCall(t, c, 5, "Browser.grantPermissions", map[string]any{"permissions": []string{"audioCapture"}, "origin": fixture.URL})
	if read("microphone") != "granted" || read("camera") != "denied" {
		t.Fatal("microphone allowlist not projected")
	}
	wireCall(t, c, 6, "Browser.setPermission", map[string]any{"permission": map[string]any{"name": "microphone"}, "setting": "denied", "origin": fixture.URL})
	result, err = s.Page.Evaluate(ctx, `navigator.mediaDevices.getUserMedia({ audio: true }).then(
  () => false,
  (e) => e.name === 'NotAllowedError',
);`)
	if err != nil || result != true {
		t.Fatalf("denied microphone completion: %v %v", result, err)
	}
	wireCall(t, c, 7, "Browser.resetPermissions", map[string]any{})
	if read("microphone") != "prompt" {
		t.Fatal("reset did not restore microphone default")
	}
}
