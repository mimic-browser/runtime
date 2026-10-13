package browser

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestOfflineAudioOracles(t *testing.T) {
	serialBrowserTest(t)
	for _, name := range []string{"audio_buffers", "offline_audio", "audio_graph", "audio_relations", "audio_lifecycle", "audio_validation", "audio_channels", "audio_fractional", "audio_interpolation", "audio_rates", "audio_reverse", "audio_phase", "audio_source_events", "audio_ramps", "audio_automation_validation", "audio_scheduled_rate", "audio_automation_cancel", "audio_scheduled_duration", "audio_automation_edges"} {
		t.Run(name, func(t *testing.T) {
			parallelOracle(t)
			source, err := os.ReadFile("testdata/" + name + "_oracle.js")
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile("../../compatibility/captures/semantic-checkpoints/" + strings.ReplaceAll(name, "_", "-") + "-chrome152.json")
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
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				value, err := p.Evaluate(ctx, "(async()=>JSON.stringify(await "+string(source)+"))()")
				if err != nil {
					t.Fatal(err)
				}
				var actual map[string]any
				if err = json.Unmarshal([]byte(value.(string)), &actual); err != nil {
					t.Fatal(err)
				}
				for key, want := range capture.Result {
					if !reflect.DeepEqual(actual[key], want) {
						gotArray, gok := actual[key].([]any)
						wantArray, wok := want.([]any)
						if gok && wok && len(gotArray) == len(wantArray) {
							for i := range wantArray {
								if !reflect.DeepEqual(gotArray[i], wantArray[i]) {
									t.Errorf("%s[%d]: got %v want %v", key, i, gotArray[i], wantArray[i])
									break
								}
							}
						} else {
							t.Errorf("%s: got %v want %v", key, actual[key], want)
						}
					}
				}
			})
		})
	}
}

func TestOfflineAudioUnsupportedGraphRejects(t *testing.T) {
	serialBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		value, err := p.Evaluate(ctx, `(async()=>{
 const c=new OfflineAudioContext(1,8,8000),source=c.createBufferSource();
 source.buffer=c.createBuffer(1,4,8000);const gain=c.createGain();source.connect(gain);gain.connect(gain);gain.connect(c.destination);source.playbackRate.setValueAtTime(2,0);source.start(.5/8000,0,1/8000);
 let complete=false;c.oncomplete=()=>{complete=true};
 if(await c.startRendering().then(()=>'',e=>e.name)!=='NotSupportedError'||complete||c.state!=='closed')return false;
 if(await c.startRendering().then(()=>'',e=>e.name)!=='InvalidStateError')return false;
 const d=new OfflineAudioContext(1,8,8000);
   const filter=d.createBiquadFilter();
   if(!(filter instanceof BiquadFilterNode)||filter.context!==d||filter.type!=='lowpass')return false;
 return await d.decodeAudioData(new ArrayBuffer(8)).then(()=>false,e=>e.name==='EncodingError');
})()`)
		if err != nil || value != true {
			t.Fatalf("value=%v error=%v", value, err)
		}
	})
}

func TestOfflineAudioShortRenderDoesNotYieldBetweenEveryQuantum(t *testing.T) {
	serialBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		value, err := p.Evaluate(ctx, `(async()=>{
 const c=new OfflineAudioContext(1,5000,44100);
 const rendered=c.startRendering().then(()=>"audio");
 const timeout=new Promise(resolve=>setTimeout(()=>resolve("timeout"),80));
 const start=performance.now();while(performance.now()-start<350){}
 return Promise.race([rendered,timeout]);
})()`)
		if err != nil || value != "audio" {
			t.Fatalf("value=%v error=%v", value, err)
		}
	})
}
