//go:build (windows || linux) && amd64

package browser

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"runtime"
	"strconv"
	"testing"
)

func TestCanvasFontMetricsFrozenChrome(t *testing.T) {
	serialBrowserTest(t)
	source, err := os.ReadFile("testdata/canvas_font_metrics_oracle.js")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("testdata/canvas_font_metrics_chrome152.json")
	if err != nil {
		t.Fatal(err)
	}
	var capture struct {
		Observation map[string]any `json:"observation"`
	}
	if err = json.Unmarshal(data, &capture); err != nil {
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
			actual := value.(map[string]any)
			// Native ink edges come from DirectWrite alpha-texture bounds, whereas
			// this platform-neutral model uses font outlines. Keep this explicit
			// one-pixel observation boundary separate from exact advances/baselines.
			for name, raw := range capture.Observation {
				want, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				got, ok := actual[name].(map[string]any)
				if !ok {
					t.Fatalf("missing %s", name)
				}
				for _, key := range []string{"actualBoundingBoxLeft", "actualBoundingBoxRight", "left", "right"} {
					w, ok := want[key].(float64)
					if !ok {
						continue
					}
					g, ok := got[key].(float64)
					if !ok || math.Abs(w-g) > 1 {
						t.Fatalf("%s/%s: ink bound %v, Chrome %v", name, key, g, w)
					}
					got[key] = w
				}
			}
			if diff := bootstrapSnapshotDifference("Canvas metrics", capture.Observation, actual); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}

func TestCanvasTypographicMetricsFontUsesChromeFontBoxAndGridFit(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("frozen control uses the installed Windows Cambria Math face")
	}
	serialBrowserTest(t)
	p := bootstrapSnapshotPage(t)
	defer p.Close()
	navigateCapabilityFixture(t, p)
	value, err := p.Evaluate(context.Background(), `(()=>{
		const context = new OffscreenCanvas(200, 100).getContext('2d');
		context.font = '16px "Cambria Math"';
		const metrics = context.measureText('mwmwmwmwlli');
		return [metrics.width, metrics.fontBoundingBoxAscent,
			metrics.fontBoundingBoxDescent, metrics.actualBoundingBoxRight];
	})()`)
	if err != nil {
		t.Fatal(err)
	}
	want := []any{float64(111), float64(12), float64(4), float64(111)}
	if diff := bootstrapSnapshotDifference("Cambria Math Canvas metrics", want, value); diff != "" {
		t.Fatal(diff)
	}
}
