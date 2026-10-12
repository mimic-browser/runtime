//go:build (windows || linux) && amd64

package browser

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"strconv"
	"testing"
)

func TestWebGLFloatBlendReadbackFrozenChrome(t *testing.T) {
	serialBrowserTest(t)
	source, err := os.ReadFile("testdata/webgl_float_blend_oracle.js")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("testdata/webgl_float_blend_chrome152.json")
	if err != nil {
		t.Fatal(err)
	}
	var capture struct {
		Value map[string]any `json:"value"`
	}
	if err := json.Unmarshal(data, &capture); err != nil {
		t.Fatal(err)
	}
	for _, restored := range []bool{false, true} {
		t.Run(strconv.FormatBool(restored), func(t *testing.T) {
			t.Setenv("MIMIC_DISABLE_BOOTSTRAP_SNAPSHOT", map[bool]string{false: "1", true: "0"}[restored])
			seed := bootstrapSnapshotPage(t)
			navigateCapabilityFixture(t, seed)
			if restored {
				bootstrapSnapshotWarm(t, seed)
			}
			p, err := seed.ctx.NewPage()
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			navigateCapabilityFixture(t, p)
			value, err := p.Evaluate(context.Background(), string(source))
			if err != nil {
				t.Fatal(err)
			}
			observed := value.(map[string]any)
			pixels := observed["pixel"].([]any)
			wantPixels := capture.Value["pixel"].([]any)
			delete(observed, "pixel")
			wantState := make(map[string]any, len(capture.Value)-1)
			for key, value := range capture.Value {
				if key != "pixel" {
					wantState[key] = value
				}
			}
			if difference := bootstrapSnapshotDifference("WebGL float blend readback", wantState, observed); difference != "" {
				t.Fatal(difference)
			}
			for i := range pixels {
				if math.Abs(pixels[i].(float64)-wantPixels[i].(float64)) > 3e-8 {
					t.Fatalf("pixel[%d]: Chrome=%v Mimic=%v", i, wantPixels[i], pixels[i])
				}
			}
		})
	}
}
