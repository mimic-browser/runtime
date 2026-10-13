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
				// The retained frozen Chrome 152 control paints 195 pixels for
				// this glyph. Pin a tight edge-coverage envelope as well as the
				// shared surface relationships so the CPU rasterizer cannot
				// quietly grow faint fringe pixels.
				if got.Colors < 100 || got.Nonzero < 190 || got.Nonzero > 195 || !got.Stable || !got.PNG {
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

func TestCanvasColorGlyphMetricsUseRasterInkBounds(t *testing.T) {
	serialBrowserTest(t)
	p := bootstrapSnapshotPage(t)
	defer p.Close()
	value, err := p.Evaluate(context.Background(), `(() => {
  const context = new OffscreenCanvas(200, 100).getContext('2d');
  context.font = '16px "Segoe UI Emoji", sans-serif';
  return ['😀', '🤵‍♂️', '♨'].map(text => {
    const metric = context.measureText(text);
    return [metric.actualBoundingBoxAscent, metric.actualBoundingBoxDescent,
      metric.actualBoundingBoxLeft, metric.actualBoundingBoxRight, metric.width];
  });
})()`)
	if err != nil {
		t.Fatal(err)
	}
	// Direct frozen headful Chrome 152 observations. Color-paint layers extend
	// beyond a glyph's base outline, so these edges must come from the same CPU
	// raster surface used by fillText rather than the monochrome outline box.
	want := []any{
		[]any{float64(14), float64(3), float64(-2), float64(19), float64(21.96875)},
		[]any{float64(14), float64(3), float64(-4), float64(18), float64(21.96875)},
		[]any{float64(14), float64(3), float64(-3), float64(19), float64(21.96875)},
	}
	if diff := bootstrapSnapshotDifference("Canvas color glyph metrics", want, value); diff != "" {
		t.Fatal(diff)
	}
}

// Direct frozen headful Chrome 152 keeps emoji painted in color during
// strokeText, while the monochrome letters use stroke outlines. The two text
// modes therefore have different alpha coverage on the same font run.
func TestCanvasStrokeTextPreservesColorGlyphs(t *testing.T) {
	serialBrowserTest(t)
	p := bootstrapSnapshotPage(t)
	value, err := p.Evaluate(context.Background(), `(() => {
  const draw = method => {
    const canvas=document.createElement('canvas');
    canvas.width=canvas.height=20;
    const ctx=canvas.getContext('2d');
    ctx.font='7px Segoe UI Emoji, sans-serif';
    ctx.strokeStyle='#000000';
    ctx.fillStyle='#000000';
    ctx[method+'Text']('xyz😃👾',0,12,16);
    const pixels=ctx.getImageData(0,0,20,20).data;
    let alpha=0;
    for(let i=3;i<pixels.length;i+=4)alpha+=pixels[i];
    return {alpha,sample:Array.from(pixels.slice((7*20+6)*4,(7*20+6)*4+4))};
  };
  return JSON.stringify({stroke:draw('stroke'),fill:draw('fill')});
})()`)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Stroke struct {
			Alpha  int   `json:"alpha"`
			Sample []int `json:"sample"`
		} `json:"stroke"`
		Fill struct {
			Alpha int `json:"alpha"`
		} `json:"fill"`
	}
	if err := json.Unmarshal([]byte(value.(string)), &got); err != nil {
		t.Fatal(err)
	}
	// Frozen Chrome 152 sums to 13506 for this stroke. Its color glyphs are
	// retained while the monochrome outline uses antialiased stroke coverage.
	if got.Stroke.Alpha < 13000 || got.Stroke.Alpha > 14000 ||
		got.Stroke.Alpha <= got.Fill.Alpha+2000 || len(got.Stroke.Sample) != 4 ||
		got.Stroke.Sample[0] <= 150 || got.Stroke.Sample[1] <= 80 ||
		got.Stroke.Sample[2] <= 40 || got.Stroke.Sample[3] <= 70 {
		t.Fatalf("strokeText lost Chrome's color glyph or outline behavior: %+v", got)
	}
}
