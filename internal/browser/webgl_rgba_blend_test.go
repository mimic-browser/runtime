//go:build (windows || linux) && amd64

package browser

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"testing"
)

func TestWebGLRGBA8BlendFrozenChrome(t *testing.T) {
	serialBrowserTest(t)
	source, err := os.ReadFile("testdata/webgl_rgba_blend_oracle.js")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("testdata/webgl_rgba_blend_chrome152.json")
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
			if difference := bootstrapSnapshotDifference("WebGL RGBA8 blend", capture.Result, value); difference != "" {
				t.Fatal(difference)
			}
		})
	}
}
