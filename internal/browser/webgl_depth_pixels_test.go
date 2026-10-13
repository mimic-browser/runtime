//go:build (windows || linux) && amd64

package browser

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"testing"
)

func TestWebGLDepthPixelsFrozenChrome(t *testing.T) {
	serialBrowserTest(t)
	source, err := os.ReadFile("testdata/webgl_depth_pixels_oracle.js")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("testdata/webgl_depth_pixels_chrome152.json")
	if err != nil {
		t.Fatal(err)
	}
	var capture struct {
		Result []any `json:"result"`
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
			if difference := bootstrapSnapshotDifference("WebGL depth pixels", capture.Result, value); difference != "" {
				t.Fatal(difference)
			}
		})
	}
}

func TestWebGLDepthDeferredClearFrozenChrome(t *testing.T) {
	serialBrowserTest(t)
	source, err := os.ReadFile("testdata/webgl_depth_deferred_oracle.js")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("testdata/webgl_depth_deferred_chrome152.json")
	if err != nil {
		t.Fatal(err)
	}
	var capture struct {
		Result any `json:"result"`
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
			if difference := bootstrapSnapshotDifference("WebGL deferred depth clear", capture.Result, value); difference != "" {
				t.Fatal(difference)
			}
		})
	}
}

func TestWebGLDepthResizeFrozenChrome(t *testing.T) {
	serialBrowserTest(t)
	for _, name := range []string{"resize", "resize_changed", "resize_html", "resize_html_changed"} {
		t.Run(name, func(t *testing.T) {
			source, err := os.ReadFile("testdata/webgl_depth_" + name + "_oracle.js")
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile("testdata/webgl_depth_" + name + "_chrome152.json")
			if err != nil {
				t.Fatal(err)
			}
			var capture struct {
				Result any `json:"result"`
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
					if difference := bootstrapSnapshotDifference("WebGL depth resize", capture.Result, value); difference != "" {
						t.Fatal(difference)
					}
				})
			}
		})
	}
}
