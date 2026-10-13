//go:build (windows || linux) && amd64

package browser

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strconv"
	"testing"
)

func TestWebGLAttachmentPixelsFrozenChrome(t *testing.T) {
	serialBrowserTest(t)
	for _, name := range []string{"webgl_attachment_pixels", "webgl1_attachment_pixels", "webgl_texture_attachment_lifecycle"} {
		t.Run(name, func(t *testing.T) {
			source, err := os.ReadFile("testdata/" + name + "_oracle.js")
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile("testdata/" + name + "_chrome152.json")
			if err != nil {
				t.Fatal(err)
			}
			var capture struct {
				SourceSHA256 string `json:"sourceSHA256"`
				Observation  any    `json:"observation"`
			}
			if err := json.Unmarshal(data, &capture); err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(source)
			if hex.EncodeToString(sum[:]) != capture.SourceSHA256 {
				t.Fatal("frozen WebGL source changed")
			}
			for _, restored := range []bool{false, true} {
				t.Run(strconv.FormatBool(restored), func(t *testing.T) {
					t.Setenv("MIMIC_DISABLE_BOOTSTRAP_SNAPSHOT", map[bool]string{false: "1", true: "0"}[restored])
					seed := bootstrapSnapshotPage(t)
					navigateCapabilityFixture(t, seed)
					if restored {
						bootstrapSnapshotWarm(t, seed)
					}
					page, err := seed.ctx.NewPage()
					if err != nil {
						t.Fatal(err)
					}
					defer page.Close()
					navigateCapabilityFixture(t, page)
					value, err := page.Evaluate(context.Background(), string(source))
					if err != nil {
						t.Fatal(err)
					}
					if restored && !page.Top.Realm.bootstrapRestored {
						t.Fatal("WebGL Page did not restore bootstrap snapshot")
					}
					if d := bootstrapSnapshotDifference("WebGL attachment pixels", capture.Observation, value); d != "" {
						t.Fatal(d)
					}
				})
			}
		})
	}
}
