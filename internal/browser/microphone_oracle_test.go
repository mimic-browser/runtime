package browser

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Retain the successful frozen control verbatim. Codec tests use synthetic
// tones; this diagnostic stores levels and metadata rather than microphone PCM.
func TestMicrophoneChrome152Evidence(t *testing.T) {
	directory := "testdata/microphone_webrtc_chrome152"
	inventory, err := os.ReadFile(filepath.Join(directory, "sha256.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(inventory)), "\n") {
		fields := strings.SplitN(strings.TrimSpace(line), "  ", 2)
		if len(fields) != 2 || filepath.Base(fields[1]) != fields[1] {
			t.Fatal("invalid capture inventory")
		}
		data, err := os.ReadFile(filepath.Join(directory, fields[1]))
		if err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256(data)
		if hex.EncodeToString(hash[:]) != fields[0] {
			t.Fatalf("capture changed: %s", fields[1])
		}
	}
	data, err := os.ReadFile(filepath.Join(directory, "capture.json"))
	if err != nil {
		t.Fatal(err)
	}
	var capture struct {
		Metadata struct {
			Chrome   map[string]any `json:"chrome"`
			Identity map[string]any `json:"identity"`
		} `json:"captureMetadata"`
		Chrome, Mimic struct {
			Connection       string                                             `json:"connection"`
			Peak             float64                                            `json:"peak"`
			RemoteAfterClose string                                             `json:"remoteAfterClose"`
			Errors           []string                                           `json:"errors"`
			Local            struct{ Settings, StoppedSettings map[string]any } `json:"local"`
			Transport        []map[string]any                                   `json:"transport"`
		}
	}
	if err = json.Unmarshal(data, &capture); err != nil {
		t.Fatal(err)
	}
	if capture.Metadata.Chrome["Browser"] != "Chrome/152.0.7977.82" || capture.Metadata.Identity["webdriver"] != false {
		t.Fatal("unexpected frozen control")
	}
	for name, result := range map[string]struct {
		connection string
		peak       float64
		stats      []map[string]any
	}{"Chrome": {capture.Chrome.Connection, capture.Chrome.Peak, capture.Chrome.Transport}, "Mimic": {capture.Mimic.Connection, capture.Mimic.Peak, capture.Mimic.Transport}} {
		if result.connection != "connected" || result.peak <= 0 {
			t.Fatalf("%s did not decode microphone audio", name)
		}
		incoming, outgoing := false, false
		for _, stat := range result.stats {
			if stat["type"] == "inbound-rtp" {
				count, _ := stat["packetsReceived"].(float64)
				incoming = incoming || count > 0
			}
			if stat["type"] == "outbound-rtp" {
				count, _ := stat["packetsSent"].(float64)
				outgoing = outgoing || count > 0
			}
		}
		if !incoming || !outgoing {
			t.Fatalf("%s lacks bilateral RTP evidence", name)
		}
	}
	for name, value := range capture.Chrome.Local.Settings {
		if capture.Chrome.Local.StoppedSettings[name] != value {
			t.Fatalf("Chrome audio stop discarded %s", name)
		}
	}
	if capture.Chrome.RemoteAfterClose != "live" || capture.Mimic.RemoteAfterClose != "live" || len(capture.Chrome.Errors) > 0 || len(capture.Mimic.Errors) > 0 {
		t.Fatal("unexpected transport lifecycle")
	}
}

func TestCaptureTimerHandlesAreNumbers(t *testing.T) {
	serialBrowserTest(t)
	cameraTestPages(t, func(t *testing.T, p *Page) {
		navigateCapabilityFixture(t, p)
		value, err := p.Evaluate(t.Context(), `(() => {
  const timeout = setTimeout(() => {}, 1000),
    interval = setInterval(() => {}, 1000);
  clearTimeout(timeout);
  clearInterval(interval);
  return typeof timeout === 'number' && typeof interval === 'number';
})();`)
		if err != nil || value != true {
			t.Fatalf("timer ID boundary: %v %v", value, err)
		}
	})
}
