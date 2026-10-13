package browser

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strconv"
	"testing"
)

func TestGraphicsObservationOracles(t *testing.T) {
	parallelBrowserTest(t)
	for _, fixture := range []struct{ name, capture string }{{"canvas_paths", "canvas-paths"}, {"canvas_relations", "canvas-relations"}, {"canvas_dash", "canvas-dash"}, {"canvas_join", "canvas-join"}, {"canvas_arcto", "canvas-arcto"}, {"canvas_roundrect", "canvas-roundrect"}, {"canvas_filter_color", "canvas-filter-color"}, {"canvas_filter_spatial", "canvas-filter-spatial"}, {"webgl_programs", "webgl-programs"}, {"webgl_framebuffers", "webgl-framebuffers"}, {"webgl_validation", "webgl-validation"}, {"webgl_geometry", "webgl-geometry"}, {"webgl_framebuffer_lifecycle", "webgl-framebuffer-lifecycle"}} {
		t.Run(fixture.name, func(t *testing.T) {
			parallelOracle(t)
			source, err := os.ReadFile("testdata/" + fixture.name + "_oracle.js")
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile("../../compatibility/captures/semantic-checkpoints/" + fixture.capture + "-chrome152.json")
			if err != nil {
				t.Fatal(err)
			}
			var capture struct {
				Result map[string]any `json:"result"`
			}
			if err = json.Unmarshal(data, &capture); err != nil {
				t.Fatal(err)
			}
			historyTestPages(t, func(t *testing.T, p *Page) {
				for _, worker := range []bool{false, true} {
					expression := "JSON.stringify(" + string(source) + ")"
					if worker {
						code := "onmessage=()=>{try{postMessage(" + expression + ")}catch(e){postMessage(String(e))}}"
						expression = `new Promise((resolve,reject)=>{const u=URL.createObjectURL(new Blob([` + strconv.Quote(code) + `])),w=new Worker(u);w.onmessage=e=>{w.terminate();URL.revokeObjectURL(u);resolve(e.data)};w.onerror=e=>reject(Error(e.message));w.postMessage(null)})`
					}
					value, err := p.Evaluate(context.Background(), expression)
					if err != nil {
						t.Fatal(err)
					}
					var actual map[string]any
					if err = json.Unmarshal([]byte(value.(string)), &actual); err != nil {
						t.Fatalf("worker=%v result=%v: %v", worker, value, err)
					}
					for key, want := range capture.Result {
						if !reflect.DeepEqual(actual[key], want) {
							t.Errorf("worker=%v %s: got %v want %v", worker, key, actual[key], want)
						}
					}
				}
			})
		})
	}
}

// One selected profile must expose one deterministic graphics model. Canvas
// serialization is a projection of its canonical readback bytes, not a fresh
// random fingerprint per call or Page.
func TestCanvasSerializationIsStableAcrossPages(t *testing.T) {
	parallelBrowserTest(t)
	const expression = `(()=>{const canvas=document.createElement('canvas');canvas.width=12;canvas.height=5;const context=canvas.getContext('2d');context.fillStyle='#143250';context.fillRect(0,0,12,5);context.fillStyle='rgba(240,80,20,.75)';context.fillRect(2,1,5,3);const first=canvas.toDataURL(),second=canvas.toDataURL(),raw=atob(first.slice(first.indexOf(',')+1)),unaffected=Array.from(context.getImageData(1,1,1,1).data).join(',');context.fillStyle='#51a629';context.fillRect(11,4,1,1);const changed=canvas.toDataURL();return JSON.stringify({first,repeat:first===second,changed:first!==changed,unaffected:unaffected===Array.from(context.getImageData(1,1,1,1).data).join(','),png:Array.from(raw.slice(0,8),c=>c.charCodeAt(0)).join(','),read:Array.from(context.getImageData(1,1,3,2).data).join(',')})})()`
	historyTestPages(t, func(t *testing.T, page *Page) {
		first, err := page.Evaluate(context.Background(), expression)
		if err != nil {
			t.Fatal(err)
		}
		second, err := page.ctx.NewPage()
		if err != nil {
			t.Fatal(err)
		}
		other, err := second.Evaluate(context.Background(), expression)
		if err != nil {
			t.Fatal(err)
		}
		if first != other {
			t.Fatalf("same-profile Pages produced different canvas observations\nfirst: %v\nother: %v", first, other)
		}
		var observation struct {
			First      string `json:"first"`
			Repeat     bool   `json:"repeat"`
			Changed    bool   `json:"changed"`
			Unaffected bool   `json:"unaffected"`
			PNG        string `json:"png"`
			Read       string `json:"read"`
		}
		if err = json.Unmarshal([]byte(first.(string)), &observation); err != nil {
			t.Fatal(err)
		}
		if !observation.Repeat || !observation.Changed || !observation.Unaffected || observation.PNG != "137,80,78,71,13,10,26,10" || observation.First == "data:image/png;base64," || observation.Read == "" {
			t.Fatalf("invalid deterministic canvas serialization: %+v", observation)
		}
	})
}

func TestCanvasFilterReadbackAndPNGShareCanonicalSurface(t *testing.T) {
	parallelBrowserTest(t)
	const expression = `(()=>{const canvas=document.createElement('canvas');canvas.width=6;canvas.height=4;const context=canvas.getContext('2d');context.filter='brightness(50%)';context.fillStyle='rgba(100,150,200,.8)';context.fillRect(1,1,3,2);context.filter='none';const read=Array.from(context.getImageData(2,1,1,1).data).join(','),first=canvas.toDataURL(),second=canvas.toDataURL();context.fillStyle='white';context.fillRect(5,3,1,1);return JSON.stringify({read,repeat:first===second,changed:first!==canvas.toDataURL(),png:atob(first.slice(first.indexOf(',')+1)).slice(0,8).split('').map(c=>c.charCodeAt(0)).join(',')})})()`
	historyTestPages(t, func(t *testing.T, page *Page) {
		value, err := page.Evaluate(context.Background(), expression)
		if err != nil {
			t.Fatal(err)
		}
		var observation struct {
			Read    string `json:"read"`
			Repeat  bool   `json:"repeat"`
			Changed bool   `json:"changed"`
			PNG     string `json:"png"`
		}
		if err = json.Unmarshal([]byte(value.(string)), &observation); err != nil {
			t.Fatal(err)
		}
		if observation.Read != "50,75,100,204" || !observation.Repeat || !observation.Changed || observation.PNG != "137,80,78,71,13,10,26,10" {
			t.Fatalf("filter readback and PNG diverged from the canonical surface: %+v", observation)
		}
	})
}

func TestCanvasSpatialFilterReadbackAndPNGShareCanonicalSurface(t *testing.T) {
	parallelBrowserTest(t)
	const expression = `(()=>{const canvas=document.createElement('canvas');canvas.width=12;canvas.height=8;const context=canvas.getContext('2d');context.filter='blur(1px)';context.fillStyle='rgba(200,40,20,.8)';context.fillRect(4,2,4,4);context.filter='none';const edge=context.getImageData(3,3,1,1).data[3],first=canvas.toDataURL(),second=canvas.toDataURL();context.fillStyle='white';context.fillRect(11,7,1,1);return JSON.stringify({edge,repeat:first===second,changed:first!==canvas.toDataURL(),png:atob(first.slice(first.indexOf(',')+1)).slice(0,8).split('').map(c=>c.charCodeAt(0)).join(',')})})()`
	historyTestPages(t, func(t *testing.T, page *Page) {
		value, err := page.Evaluate(context.Background(), expression)
		if err != nil {
			t.Fatal(err)
		}
		var observation struct {
			Edge    int    `json:"edge"`
			Repeat  bool   `json:"repeat"`
			Changed bool   `json:"changed"`
			PNG     string `json:"png"`
		}
		if err = json.Unmarshal([]byte(value.(string)), &observation); err != nil {
			t.Fatal(err)
		}
		if observation.Edge <= 0 || !observation.Repeat || !observation.Changed || observation.PNG != "137,80,78,71,13,10,26,10" {
			t.Fatalf("spatial filter readback and PNG diverged from the canonical surface: %+v", observation)
		}
	})
}
