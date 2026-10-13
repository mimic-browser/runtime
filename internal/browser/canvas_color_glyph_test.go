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

func TestCanvasColorGlyphMetricsUsePaintClip(t *testing.T) {
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
	// beyond a glyph's base outline, so these edges come from the COLR paint
	// graph's clip rather than the monochrome outline box.
	want := []any{
		[]any{float64(14), float64(3), float64(-2), float64(19), float64(21.96875)},
		[]any{float64(14), float64(3), float64(-4), float64(18), float64(21.96875)},
		[]any{float64(14), float64(3), float64(-3), float64(19), float64(21.96875)},
	}
	if diff := bootstrapSnapshotDifference("Canvas color glyph metrics", want, value); diff != "" {
		t.Fatal(diff)
	}
}

func TestCanvasColorGlyphRasterFollowsCurrentTransform(t *testing.T) {
	serialBrowserTest(t)
	p := bootstrapSnapshotPage(t)
	defer p.Close()
	value, err := p.Evaluate(context.Background(), `(() => {
  const draw = transform => {
    const context = new OffscreenCanvas(64,64).getContext('2d',{willReadFrequently:true});
    context.font='15px system-ui, sans-serif';
    transform(context);
    context.fillText('😀',5,20);
    const pixels=context.getImageData(0,0,64,64).data;
    let nonzero=0,alpha=0;
    for(let i=3;i<pixels.length;i+=4){if(pixels[i])nonzero++;alpha+=pixels[i]}
    const at=(x,y)=>Array.from(pixels.slice((y*64+x)*4,(y*64+x)*4+4));
    return {nonzero,alpha,at};
  };
  const scale=draw(context=>context.scale(1.5,1.25));
  const affine=draw(context=>context.transform(1.25,.25,-.15,.9,8,7));
  return JSON.stringify({scale:{nonzero:scale.nonzero,alpha:scale.alpha,sample:scale.at(19,10)},
    affine:{nonzero:affine.nonzero,alpha:affine.alpha,sample:affine.at(23,18)}});
})()`)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Scale, Affine struct {
			Nonzero int   `json:"nonzero"`
			Alpha   int   `json:"alpha"`
			Sample  []int `json:"sample"`
		}
	}
	if err := json.Unmarshal([]byte(value.(string)), &got); err != nil {
		t.Fatal(err)
	}
	// Retained direct headful Chrome 152 observations are respectively
	// 361/85893 and 224/52695. Pin their transformed ink envelopes and two
	// exact opaque COLRv1 paint samples; a transformed bounding-box substitute
	// painted only 96-alpha black and covered 480/296 pixels.
	if got.Scale.Nonzero < 350 || got.Scale.Nonzero > 370 ||
		got.Scale.Alpha < 82000 || got.Scale.Alpha > 88000 ||
		got.Affine.Nonzero < 215 || got.Affine.Nonzero > 235 ||
		got.Affine.Alpha < 50000 || got.Affine.Alpha > 55000 ||
		len(got.Scale.Sample) != 4 || got.Scale.Sample[0] != 252 || got.Scale.Sample[1] != 188 || got.Scale.Sample[2] != 61 || got.Scale.Sample[3] != 255 ||
		len(got.Affine.Sample) != 4 || got.Affine.Sample[0] != 255 || got.Affine.Sample[1] != 195 || got.Affine.Sample[2] != 73 || got.Affine.Sample[3] != 255 {
		t.Fatalf("transformed color glyph differs from Chrome 152: %+v", got)
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
