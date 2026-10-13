//go:build windows && amd64

package browser

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
)

func TestCanvasColorGlyphUsesCanonicalSurface(t *testing.T) {
	serialBrowserTest(t)
	const source = `(() => {
      const canvas = document.createElement('canvas');
      canvas.width = canvas.height = 20;
      const context = canvas.getContext('2d');
      context.font = '15px system-ui, sans-serif';
      const originalModule = WebAssembly.Module;
      const originalInstance = WebAssembly.Instance;
      let first;
      try {
        WebAssembly.Module = function() { throw Error('author module constructor'); };
        WebAssembly.Instance = function() { throw Error('author instance constructor'); };
        context.fillText('😀', 0, 15);
        first = context.getImageData(0, 0, 20, 20).data;
      } finally {
        WebAssembly.Module = originalModule;
        WebAssembly.Instance = originalInstance;
      }
      const second = context.getImageData(0, 0, 20, 20).data;
      const pixels = [];
      for (let i = 0; i < first.length; i += 4)
        pixels.push(Array.from(first.slice(i, i + 4)));
      const colors = new Set(pixels.filter(p => p[3]).map(p => p.join(',')));
      return JSON.stringify({colors: colors.size,
        stable: String(first) === String(second),
        nonzero: pixels.filter(p => p[3]).length,
        png: canvas.toDataURL().startsWith('data:image/png;base64,')});
    })()`
	for _, restored := range []bool{false, true} {
		t.Run(strconv.FormatBool(restored), func(t *testing.T) {
			t.Setenv("MIMIC_DISABLE_BOOTSTRAP_SNAPSHOT", map[bool]string{false: "1", true: "0"}[restored])
			var first string
			var seed *Page
			for pageIndex := range 2 {
				var p *Page
				if pageIndex == 0 {
					seed = bootstrapSnapshotPage(t)
					p = seed
					bootstrapSnapshotWarm(t, p)
				} else {
					var err error
					p, err = seed.ctx.NewPage()
					if err != nil {
						t.Fatal(err)
					}
				}
				value, err := p.Evaluate(context.Background(), source)
				if err != nil {
					t.Fatal(err)
				}
				if restored && pageIndex == 1 && !p.Top.Realm.bootstrapRestored {
					t.Fatal("second Page did not restore the bootstrap snapshot")
				}
				var got struct {
					Colors  int  `json:"colors"`
					Stable  bool `json:"stable"`
					Nonzero int  `json:"nonzero"`
					PNG     bool `json:"png"`
				}
				if err := json.Unmarshal([]byte(value.(string)), &got); err != nil {
					t.Fatal(err)
				}
				if got.Colors < 100 || got.Nonzero < 150 || !got.Stable || !got.PNG {
					t.Fatalf("page %d color glyph did not use one stable Canvas surface: %+v", pageIndex, got)
				}
				if pageIndex == 0 {
					first = value.(string)
				} else if value.(string) != first {
					t.Fatalf("independent Pages returned different color-glyph observations: first=%s second=%s", first, value)
				}
			}
		})
	}
}
