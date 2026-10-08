package cdp

import (
	"bytes"
	"encoding/base64"
	"image/png"
	"testing"
)

func TestCaptureScreenshotUsesStandardCDPResult(t *testing.T) {
	s, address := runningServer(t)
	c := browserConnection(t, address)
	sessionID := wireCall(t, c, 1, "Target.attachToTarget", map[string]any{"targetId": s.Page.ID, "flatten": true})["sessionId"].(string)
	flatCall(t, c, sessionID, 2, "Page.setDocumentContent", map[string]any{"frameId": s.Page.Top.ID, "html": `<!doctype html><html><body style="margin:0;background:#f00"><div id="block" style="width:60px;height:50px;background:#00f"></div></body></html>`})
	viewport := flatCall(t, c, sessionID, 3, "Page.captureScreenshot", map[string]any{})
	viewportData, err := base64.StdEncoding.DecodeString(viewport["data"].(string))
	if err != nil {
		t.Fatal(err)
	}
	viewportImage, err := png.Decode(bytes.NewReader(viewportData))
	if err != nil {
		t.Fatal(err)
	}
	window := s.Page.Environment().Window
	if viewportImage.Bounds().Dx() != window.ViewportWidth || viewportImage.Bounds().Dy() != window.ViewportHeight {
		t.Fatalf("viewport screenshot size = %v, want %dx%d", viewportImage.Bounds(), window.ViewportWidth, window.ViewportHeight)
	}
	result := flatCall(t, c, sessionID, 4, "Page.captureScreenshot", map[string]any{"format": "png", "clip": map[string]any{"x": 0, "y": 0, "width": 30, "height": 25, "scale": 1}})
	if len(result) != 1 {
		t.Fatalf("unexpected screenshot response fields: %#v", result)
	}
	data, err := base64.StdEncoding.DecodeString(result["data"].(string))
	if err != nil {
		t.Fatal(err)
	}
	image, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if image.Bounds().Dx() != 30 || image.Bounds().Dy() != 25 {
		t.Fatalf("clip size = %v", image.Bounds())
	}
	r, g, b, _ := image.At(10, 10).RGBA()
	if b < 0xf000 || r > 0x1000 || g > 0x1000 {
		t.Fatalf("snapshot did not include current DOM style: %04x %04x %04x", r, g, b)
	}
	flatCall(t, c, sessionID, 5, "Runtime.evaluate", map[string]any{"expression": `document.querySelector('#block').style.backgroundColor = 'lime'`})
	changed := flatCall(t, c, sessionID, 6, "Page.captureScreenshot", map[string]any{"format": "png", "clip": map[string]any{"x": 0, "y": 0, "width": 30, "height": 25, "scale": 1}})
	data, err = base64.StdEncoding.DecodeString(changed["data"].(string))
	if err != nil {
		t.Fatal(err)
	}
	image, err = png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, _ = image.At(10, 10).RGBA()
	if g < 0xf000 || r > 0x1000 || b > 0x1000 {
		t.Fatalf("screenshot missed live DOM mutation: %04x %04x %04x", r, g, b)
	}
}
