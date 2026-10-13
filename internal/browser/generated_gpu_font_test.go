package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	chrome152 "github.com/moreveal/mimic/chrome/152"
	v8engine "github.com/moreveal/mimic/internal/engine/v8"
	"github.com/moreveal/mimic/internal/profile"
)

func TestGeneratedGPUFontRecipesAgreeAcrossRealms(t *testing.T) {
	serialBrowserTest(t)
	b, err := New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<!doctype html><html><head><title>GPU recipe fixture</title></head><body></body></html>"))
	}))
	defer fixture.Close()
	profiles := map[string]profile.Document{}
	for i := 0; i < 1000; i++ {
		raw := []byte(fmt.Sprintf(`{"seed":"gpu-realm-%d"}`, i))
		d, _, err := profile.Generate(raw, b.Environment())
		if err != nil {
			t.Fatal(err)
		}
		if d.Graphics.WebGLCapabilitiesJSON != "" {
			key := d.Graphics.WebGPU.Vendor
			if key == "intel" && d.Fonts.System["menu"] == `12px "Segoe UI"` {
				key = "intel12"
			}
			profiles[key] = d
		}
	}
	if len(profiles) != 4 {
		t.Fatalf("wanted four generated GPU/font recipes; got %d", len(profiles))
	}
	baselineGPU := b.Environment().Graphics.WebGPU
	for name, d := range profiles {
		if !reflect.DeepEqual(d.Graphics.WebGPU.WGSLLanguageFeatures, baselineGPU.WGSLLanguageFeatures) || d.Graphics.WebGPU.InitializationDelayMillis != baselineGPU.InitializationDelayMillis {
			t.Fatalf("%s changed Chrome WGSL language support or unlinked adapter timing: wgsl=%v/%v delay=%v/%v", name, d.Graphics.WebGPU.WGSLLanguageFeatures, baselineGPU.WGSLLanguageFeatures, d.Graphics.WebGPU.InitializationDelayMillis, baselineGPU.InitializationDelayMillis)
		}
	}
	const observation = `(async () => {
      const gl = new OffscreenCanvas(2, 2).getContext('webgl2', { antialias: false });
      gl.getExtension('WEBGL_debug_renderer_info');
      const adapter = await navigator.gpu.requestAdapter();
      const canvas = new OffscreenCanvas(100, 30).getContext('2d');
      canvas.font = '12px system-ui';
      gl.clearColor(0.25, 0.5, 0.75, 1);
      gl.clear(gl.COLOR_BUFFER_BIT);
      const first = new Uint8Array(16);
      const second = new Uint8Array(16);
      gl.readPixels(0, 0, 2, 2, gl.RGBA, gl.UNSIGNED_BYTE, first);
      gl.readPixels(0, 0, 2, 2, gl.RGBA, gl.UNSIGNED_BYTE, second);
      return {
        vendor: gl.getParameter(37445),
        renderer: gl.getParameter(37446),
        maxTexture: gl.getParameter(3379),
        maxRenderbuffer: gl.getParameter(34024),
        precision: gl.getShaderPrecisionFormat(gl.VERTEX_SHADER, gl.HIGH_FLOAT).precision,
        extensions: gl.getSupportedExtensions(),
        gpuVendor: adapter.info.vendor,
        gpuTexture: adapter.limits.maxTextureDimension2D,
        fontWidth: canvas.measureText('Mimic').width,
        pixelsStable: String(first) === String(second),
      };
    })()`
	for _, vendor := range []string{"intel12", "amd", "intel", "nvidia"} {
		d := profiles[vendor]
		c, err := b.NewResolvedProfileContext(d, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		p, err := c.NewPage()
		if err != nil {
			_ = b.CloseContext(c.ID)
			t.Fatal(err)
		}
		if err := p.Navigate(context.Background(), fixture.URL); err != nil {
			_ = b.CloseContext(c.ID)
			t.Fatal(err)
		}
		if v, err := p.Evaluate(context.Background(), `(()=>{const e=document.createElement('div');e.style.font='menu';document.body.append(e);const s=getComputedStyle(e),v=s.fontSize+' '+s.fontFamily;e.remove();return v})()`); err != nil || v != d.Fonts.System["menu"] {
			t.Fatalf("%s system font observation: %v, err=%v, expected=%q", vendor, v, err, d.Fonts.System["menu"])
		}
		value, err := p.Evaluate(context.Background(), "(async()=>JSON.stringify(await "+observation+"))()")
		if err != nil {
			_ = b.CloseContext(c.ID)
			t.Fatal(err)
		}
		var main map[string]any
		if err := json.Unmarshal([]byte(value.(string)), &main); err != nil {
			t.Fatal(err)
		}
		workerCode := "onmessage = async () => postMessage(JSON.stringify(await " + observation + "));"
		workerExpr := fmt.Sprintf(`new Promise((resolve, reject) => {
          const url = URL.createObjectURL(new Blob([%q], { type: 'text/javascript' }));
          const worker = new Worker(url);
          worker.onmessage = (event) => {
            worker.terminate();
            URL.revokeObjectURL(url);
            resolve(event.data);
          };
          worker.onerror = reject;
          worker.postMessage(null);
        })`, workerCode)
		value, err = p.Evaluate(context.Background(), workerExpr)
		if err != nil {
			t.Fatal(err)
		}
		var worker map[string]any
		if err := json.Unmarshal([]byte(value.(string)), &worker); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(main, worker) {
			t.Fatalf("%s main/Worker GPU-font observations differ: main=%v worker=%v", vendor, main, worker)
		}
		if main["vendor"] != d.Graphics.Vendor || main["renderer"] != d.Graphics.Renderer || main["gpuVendor"] != d.Graphics.WebGPU.Vendor || main["maxTexture"] != float64(d.Graphics.MaxTextureSize) || main["gpuTexture"] != float64(d.Graphics.MaxTextureSize) || main["pixelsStable"] != true {
			t.Fatalf("%s selected recipe disagrees with observations: %v", vendor, main)
		}
		menu, err := p.Evaluate(context.Background(), `(()=>{const e=document.createElement('div');e.style.font='menu';document.body.append(e);const s=getComputedStyle(e),result=s.fontSize+' '+s.fontFamily;e.remove();return result})()`)
		if err != nil {
			t.Fatal(err)
		}
		if menu != d.Fonts.System["menu"] {
			t.Fatalf("%s selected system font disagrees with CSS after graphics observations: got %v, want %q", vendor, menu, d.Fonts.System["menu"])
		}
		if err := b.CloseContext(c.ID); err != nil {
			t.Fatal(err)
		}
	}
	baseline := b.NewContext()
	defer baseline.Close()
	p, err := baseline.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Navigate(context.Background(), fixture.URL); err != nil {
		t.Fatal(err)
	}
	counts, err := p.Evaluate(context.Background(), `(()=>[new OffscreenCanvas(1,1).getContext('webgl').getSupportedExtensions().length,new OffscreenCanvas(1,1).getContext('webgl2').getSupportedExtensions().length])()`)
	if err != nil {
		t.Fatal(err)
	}
	// Keep this pinned to the installed profile's implemented Chrome 152
	// inventory. Generated recipes above may select another coherent inventory,
	// but closing their Contexts must restore the baseline rather than leak the
	// last selected recipe into a new Context.
	if got := counts.([]any); !reflect.DeepEqual(got, []any{float64(24), float64(32)}) {
		t.Fatalf("installed Chrome 152 extension inventory leaked a generated recipe: %v", got)
	}
}
