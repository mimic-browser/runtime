package browser

import (
	"context"
	"fmt"
	"testing"

	chrome152 "github.com/moreveal/mimic/chrome/152"
	"github.com/moreveal/mimic/compatibility"
	"github.com/moreveal/mimic/internal/engine"
	gojaengine "github.com/moreveal/mimic/internal/engine/goja"
	v8engine "github.com/moreveal/mimic/internal/engine/v8"
)

func TestSelectedBundleOptionalInstallers(t *testing.T) {
	parallelBrowserTest(t)
	for backend, factory := range map[string]engine.Factory{"goja": gojaengine.Factory{}, "v8": v8engine.Factory{}} {
		for mask := 0; mask < 8; mask++ {
			t.Run(fmt.Sprintf("%s/%d", backend, mask), func(t *testing.T) {
				generated := ""
				for i, name := range []string{"MediaSource", "StorageManager", "WGSLLanguageFeatures"} {
					if mask&(1<<i) != 0 {
						generated += "globalThis." + name + "=class " + name + "{};"
					}
				}
				bundle := surfaceTestBundle{Bundle: chrome152.New(), surface: compatibility.WebAPISurface{GeneratedJavaScript: generated}}
				b, err := New(factory, bundle)
				if err != nil {
					t.Fatal(err)
				}
				c := b.NewContext()
				defer c.Close()
				p, err := c.NewPage()
				if err != nil {
					t.Fatal(err)
				}
				navigateCapabilityFixture(t, p)
				expression := fmt.Sprintf(`(()=>{
     const selected=%d;
     for(const name of ['MediaStream','MediaStreamTrack']){
      if(Object.hasOwn(globalThis,name))throw Error('Capture constructor invented '+name);
     }
     for(const [index,name]of ['MediaSource','StorageManager','WGSLLanguageFeatures'].entries()){
      if((typeof globalThis[name]==='function')!==!!(selected&(1<<index)))throw Error('Unexpected exposure '+name);
     }
     if(selected&1){if(MediaSource.isTypeSupported('application/x-invalid')!==false)throw Error('MediaSource implementation missing')}
     else if(Object.hasOwn(globalThis,'MediaSource'))throw Error('MediaSource invented');
     if(selected&2){if(typeof navigator.storage.getDirectory!=='function')throw Error('Storage implementation missing')}
     else if(typeof FileSystemHandle!=='undefined')throw Error('Storage-dependent handle invented');
     if(selected&4){const features=navigator.gpu.wgslLanguageFeatures;if(!(features instanceof WGSLLanguageFeatures)||typeof features.has!=='function')throw Error('WGSL implementation missing')}
     else if('wgslLanguageFeatures' in GPU.prototype)throw Error('WGSL projection invented');
     return true;
    })()`, mask)
				value, err := p.Evaluate(context.Background(), expression)
				if err != nil || value != true {
					t.Fatalf("optional installers: %v %v", value, err)
				}
			})
		}
	}
}
