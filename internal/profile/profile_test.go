package profile

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/moreveal/mimic/internal/state"
)

func testBase() state.Environment {
	return state.ChromeDesktopWindows(state.Product{Name: "Chrome", Version: "152.0.0.0", FullVersion: "152.0.7977.82"})
}
func input(base state.Environment, tail string) []byte {
	return []byte(`{"schemaVersion":1,"baseProfile":"` + base.ProfileID + `"` + tail + `}`)
}
func TestNormalizeProfileRoundTrip(t *testing.T) {
	b := testBase()
	d, err := Normalize(input(b, `,"hardware":{"logicalProcessors":4},"locale":{"languages":["fr-FR","en"]},"display":{"width":1440,"availableWidth":1440}`), b, nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.Hardware.LogicalProcessors != 4 || d.Display.Width != 1440 || len(d.Locale.Languages) != 2 {
		t.Fatal(d)
	}
	raw, _ := json.Marshal(d.Object())
	next, err := Normalize(raw, b, nil)
	if err != nil {
		t.Fatal(err)
	}
	if next.Hardware != d.Hardware {
		t.Fatal(next.Hardware)
	}
	e := next.Apply(b)
	if e.Navigator().HardwareConcurrency != 4 || e.Screen().Width != 1440 || e.RequestHeaders()["Accept-Language"] != "fr-FR,en;q=0.9" {
		t.Fatal(e.Navigator(), e.Screen(), e.RequestHeaders())
	}
	e.Locale.Languages[0] = "changed"
	if b.Locale.Languages[0] != "en-US" || d.Locale.Languages[0] != "fr-FR" {
		t.Fatal("alias")
	}
}
func TestProfileRejections(t *testing.T) {
	b := testBase()
	for _, tail := range []string{`,"unknown":1`, `,"display":{"width":null}`, `,"hardware":{"logicalProcessors":1.5}`, `,"hardware":{"logicalProcessors":"8"}`, `,"window":{"viewportWidth":99999}`, `,"locale":{"timezone":"Invalid/Zone"}`, `,"locale":{"timezone":"Local"}`, `,"locale":{"intlLocale":"not_a_locale!"}`, `,"network":{"proxy":{"server":"http://user:secret@localhost:8080"}}`, `,"identity":{"metadata":{"mobile":true}}`, `,"locale":{"languages":[]}`} {
		t.Run(tail, func(t *testing.T) {
			_, err := Normalize(input(b, tail), b, nil)
			if err == nil {
				t.Fatal("accepted invalid profile")
			}
			if _, ok := err.(*Error); !ok {
				t.Fatal(err)
			}
		})
	}
}

func TestProfileCannotFabricateICE(t *testing.T) {
	base := testBase()
	for _, fields := range []string{`"publicAddress":"203.0.113.10"`, `"hostCandidateCount":3`, `"hostDelayMillis":10`} {
		_, err := Normalize(input(base, `,"network":{"ice":{`+fields+`}}`), base, nil)
		if value, ok := err.(*Error); !ok || !strings.HasPrefix(value.Path, "network.ice") || value.Reason != "unsupported" {
			t.Fatalf("fabricated ICE accepted: %v", err)
		}
	}
}
func TestProfilePatchAtomicAndSecretFree(t *testing.T) {
	b := testBase()
	d, err := Normalize(input(b, `,"network":{"proxy":{"server":"socks5://localhost:9000","username":"private-user","password":"private-password"}}`), b, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Normalize([]byte(`{"window":{"viewportWidth":800},"hardware":{"logicalProcessors":4}}`), b, &d)
	if e, ok := err.(*Error); !ok || e.Reason != "requiresNewContext" {
		t.Fatal(err)
	}
	if d.Window.ViewportWidth != 1280 {
		t.Fatal("partial mutation")
	}
	public, _ := json.Marshal(d.Public())
	if strings.Contains(string(public), "private-") {
		t.Fatal("credentials exposed")
	}
	if !strings.Contains(d.Network.Proxy.URL(), "private-user:private-password@") {
		t.Fatal("missing credentials")
	}
}
func TestProfileSchemaNaming(t *testing.T) {
	raw, _ := json.Marshal(Schema(testBase()))
	for _, key := range []string{`"cpuPerformance"`, `"rttMillis"`, `"deviceMemoryGB"`, `"wow64"`, `"additionalProperties":false`, `"unsupported"`} {
		if !strings.Contains(string(raw), key) {
			t.Fatal(key)
		}
	}
}
