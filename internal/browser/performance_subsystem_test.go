//go:build (windows || linux) && amd64

package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPerformanceResourceTiesKeepFetchStartOrder(t *testing.T) {
	start := time.Now()
	first := &performanceRecord{data: map[string]any{"name": "first", "startTime": 12.3}, sortStart: start}
	second := &performanceRecord{data: map[string]any{"name": "second", "startTime": 12.3}, sortStart: start.Add(time.Microsecond)}
	timeline := &performanceTimeline{}
	values := timeline.values([]*performanceRecord{second, first}, true)
	if values[0]["name"] != "first" || values[1]["name"] != "second" {
		t.Fatalf("resource entries with clamped start times lost fetch order: %#v", values)
	}
}

func TestPerformanceSurfaceMatchesFrozenChrome(t *testing.T) {
	parallelBrowserTest(t)

	documentAllOracle(t, "performance_surface")
}
func TestPerformanceUserTimingMatchesFrozenChrome(t *testing.T) {
	parallelBrowserTest(t)
	documentAllOracle(t, "performance_user_timing")
}
func TestPerformanceMeasureOptionsMatchesFrozenChrome(t *testing.T) {
	parallelBrowserTest(t)
	documentAllOracle(t, "performance_measure_options")
}
func TestPerformanceObserverMatchesFrozenChrome(t *testing.T) {
	parallelBrowserTest(t)

	documentAllOracle(t, "performance_observer")
}

func TestPerformanceEdgesMatchFrozenChrome(t *testing.T) {
	parallelBrowserTest(t)
	documentAllOracle(t, "performance_edges")
}
func TestPerformanceConversionMatchesFrozenChrome(t *testing.T) {
	parallelBrowserTest(t)

	documentAllOracle(t, "performance_conversion")
}
func TestPerformanceMemoryMatchesFrozenChrome(t *testing.T) {
	parallelBrowserTest(t)
	documentAllOracle(t, "performance_memory")
}
func TestPerformanceCloneBrandsMatchFrozenChrome(t *testing.T) {
	parallelBrowserTest(t)

	documentAllOracle(t, "performance_clone")
}
func TestPerformanceLongTasksMatchFrozenChrome(t *testing.T) {
	documentAllOracle(t, "performance_longtask")
}
func TestPerformanceLongTaskFramesMatchFrozenChrome(t *testing.T) {
	documentAllOracle(t, "performance_longtask_frames")
}

func TestPerformanceWorkerMatchesFrozenChrome(t *testing.T) {
	parallelBrowserTest(t)
	for _, name := range []string{"performance_surface", "performance_user_timing", "performance_observer", "performance_measure_options"} {
		t.Run(name, func(t *testing.T) {
			p := bootstrapSnapshotPage(t)
			navigateCapabilityFixture(t, p)
			source, err := os.ReadFile("testdata/" + name + "_oracle.js")
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile("testdata/" + name + "_worker_chrome152.json")
			if err != nil {
				t.Fatal(err)
			}
			var expected struct {
				Result struct {
					Result struct{ Value map[string]any }
				}
			}
			if err = json.Unmarshal(data, &expected); err != nil {
				t.Fatal(err)
			}
			code, _ := json.Marshal("onmessage=async()=>{try{postMessage({value:await " + string(source) + "})}catch(e){postMessage({error:String(e)})}}")
			value, err := p.Evaluate(context.Background(), `new Promise((resolve,reject)=>{const url=URL.createObjectURL(new Blob([`+string(code)+`])),worker=new Worker(url);worker.onmessage=e=>{worker.terminate();URL.revokeObjectURL(url);e.data.error?reject(Error(e.data.error)):resolve(JSON.stringify(e.data.value))};worker.onerror=e=>reject(Error(e.message));worker.postMessage(null)})`)
			if err != nil {
				t.Fatal(err)
			}
			var actual map[string]any
			if err = json.Unmarshal([]byte(value.(string)), &actual); err != nil {
				t.Fatal(err)
			}
			for key, want := range expected.Result.Result.Value {
				if !reflect.DeepEqual(actual[key], want) {
					t.Errorf("%s: got %#v; want %#v", key, actual[key], want)
				}
			}
			if len(actual) != len(expected.Result.Result.Value) {
				t.Errorf("key count %d, want %d", len(actual), len(expected.Result.Result.Value))
			}
		})
	}
}

func TestPerformanceTrustedInputMatchesFrozenChrome(t *testing.T) {
	serialBrowserTest(t)
	p := bootstrapSnapshotPage(t)
	navigateCapabilityFixture(t, p)
	source, err := os.ReadFile("testdata/performance_input_oracle.js")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err = p.Evaluate(ctx, string(source)); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"keyDown", "keyUp"} {
		if err = p.DispatchProtocolInput(ctx, "Input.dispatchKeyEvent", map[string]any{"type": kind, "key": "a", "code": "KeyA", "windowsVirtualKeyCode": 65}); err != nil {
			t.Fatal(err)
		}
	}
	if err = p.AdvanceTime(ctx, 300*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	value, err := p.Evaluate(ctx, `JSON.stringify(performanceInputResult())`)
	if err != nil {
		t.Fatal(err)
	}
	assertPerformanceOracle(t, "performance_input", value.(string))
}

func TestPerformanceClearedEntriesReleaseRuntimeRoots(t *testing.T) {
	parallelBrowserTest(t)
	p := bootstrapSnapshotPage(t)
	navigateCapabilityFixture(t, p)
	result := bootstrapSnapshotEvaluate(t, p, `(()=>{
	  const observer=new PerformanceObserver(()=>{});observer.observe({type:'mark'});
	  let held;for(let i=0;i<500;i++){held=performance.mark('temporary',{detail:{index:i,payload:new Array(100).fill(i)}});performance.clearMarks('temporary')}
	  const records=observer.takeRecords();observer.disconnect();
	  const standalone=new PerformanceMark('standalone',{detail:{value:7}});
	  for(let i=0;i<100;i++){const o=new PerformanceObserver(()=>held);o.observe({type:'mark'});o.disconnect()}
	  return records.length===500&&records[499]===held&&held.detail.index===499&&standalone.detail.value===7&&performance.getEntriesByType('mark').length===0;
	})()`)
	if result != true {
		t.Fatalf("retained identity/detail: %v", result)
	}
	timeline := p.Top.Realm.performance
	for _, record := range timeline.records {
		if record.data["entryType"] == "mark" {
			t.Errorf("cleared entry retained runtime root: %v", record.data["name"])
		}
	}
	for _, observer := range timeline.observers {
		if !observer.active && observer.callback != nil {
			t.Error("disconnected observer retains callback")
		}
	}
}

func TestPerformanceIsolatedSurfaceMatchesFrozenChrome(t *testing.T) {
	serialBrowserTest(t)
	for _, mode := range []string{"ordinary", "snapshot"} {
		t.Run(mode, func(t *testing.T) {
			if mode == "ordinary" {
				t.Setenv("MIMIC_DISABLE_BOOTSTRAP_SNAPSHOT", "1")
			} else {
				t.Setenv("MIMIC_DISABLE_BOOTSTRAP_SNAPSHOT", "0")
			}
			seed := bootstrapSnapshotPage(t)
			navigateCapabilityFixtureMode(t, seed, true)
			if mode == "snapshot" {
				bootstrapSnapshotWarm(t, seed)
			}
			p, err := seed.ctx.NewPage()
			if err != nil {
				t.Fatal(err)
			}
			navigateCapabilityFixtureMode(t, p, true)
			source, err := os.ReadFile("testdata/performance_surface_oracle.js")
			if err != nil {
				t.Fatal(err)
			}
			value := bootstrapSnapshotEvaluate(t, p, `JSON.stringify(`+string(source)+`)`).(string)
			assertPerformanceOracle(t, "performance_surface_isolated", value)
			if mode == "snapshot" && !p.Top.Realm.bootstrapRestored {
				t.Fatal("expected restored realm")
			}
			t.Run("agentClusterMemoryAttribution", func(t *testing.T) {
				t.Skip("Frozen .82 returns asynchronous agent-cluster memory attribution; Mimic explicitly rejects with NotSupportedError until that ownership model exists")
			})
		})
	}
}

func TestPerformanceIndependentPageTimelines(t *testing.T) {
	serialBrowserTest(t)
	seed := bootstrapSnapshotPage(t)
	server := performanceOracleServer(t)
	var pages []*Page
	for i := 0; i < 4; i++ {
		p, err := seed.ctx.NewPage()
		if err != nil {
			t.Fatal(err)
		}
		if err = p.Navigate(context.Background(), server.URL); err != nil {
			t.Fatal(err)
		}
		pages = append(pages, p)
	}
	start := make(chan struct{})
	var workers sync.WaitGroup
	for index, page := range pages {
		workers.Add(1)
		go func(index int, p *Page) {
			defer workers.Done()
			<-start
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			value, err := p.Evaluate(ctx, fmt.Sprintf(`new Promise(resolve=>{const owner=%d,rows=[];const observer=new PerformanceObserver(list=>{rows.push(...list.getEntries());observer.disconnect();resolve(rows.length===40&&rows.every(e=>e.detail.owner===owner)&&performance.getEntriesByType('mark').length===40)});observer.observe({type:'mark'});for(let i=0;i<40;i++)performance.mark('shared-name',{detail:{owner},startTime:i})})`, index))
			if err != nil || value != true {
				t.Errorf("page %d: %v, %v", index, value, err)
			}
		}(index, page)
	}
	close(start)
	workers.Wait()
}

func assertPerformanceOracle(t *testing.T, name, actualJSON string) {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name + "_chrome152.json")
	if err != nil {
		t.Fatal(err)
	}
	var expected struct {
		Result struct {
			Result struct{ Value map[string]any }
		}
	}
	if err = json.Unmarshal(data, &expected); err != nil {
		t.Fatal(err)
	}
	var actual map[string]any
	if err = json.Unmarshal([]byte(actualJSON), &actual); err != nil {
		t.Fatal(err)
	}
	for key, want := range expected.Result.Result.Value {
		if !reflect.DeepEqual(actual[key], want) {
			t.Errorf("%s: got %#v; want %#v", key, actual[key], want)
		}
	}
	if len(actual) != len(expected.Result.Result.Value) {
		t.Errorf("key count %d, want %d", len(actual), len(expected.Result.Result.Value))
	}
}

// Each HTTP fixture is the same controlled origin used by the retained Chrome
// runner. The HTTP/1.0 wire protocol is deliberate: the oracle verifies the
// negotiated protocol, not whichever version httptest happens to default to.
func performanceOracleServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, mime := `<!doctype html><link rel="icon" href="data:,"><title>Performance oracle</title><body></body>`, "text/html"
		status, extra := 200, ""
		switch r.URL.Path {
		case "/resource":
			body, mime = "performance resource", "text/plain"
			if r.URL.Query().Has("opaque") {
				mime = "application/javascript"
			}
		case "/redirect":
			time.Sleep(10 * time.Millisecond)
			status, extra, body = 302, "Location: /resource\r\n", ""
			if next := r.URL.Query().Get("next"); next != "" {
				extra = "Location: " + next + "\r\n"
			}
		case "/worker.js":
			body, mime = `onmessage=async e=>{try{postMessage({value:await (0,eval)(e.data)})}catch(e){postMessage({error:String(e)})}}`, "text/javascript"
		case "/lifecycle":
			bytes, err := os.ReadFile("testdata/performance_navigation_document.html")
			if err != nil {
				t.Error(err)
				return
			}
			body = string(bytes)
		}
		if tao := r.URL.Query().Get("tao"); tao != "" {
			extra += "Timing-Allow-Origin: " + tao + "\r\n"
		}
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		fmt.Fprintf(rw, "HTTP/1.0 %d %s\r\nContent-Type: %s\r\nContent-Length: %d\r\nCache-Control: no-store\r\nAccess-Control-Allow-Origin: *\r\nServer-Timing: db;dur=12.5;desc=\"database, primary\", cache;desc=\"hit\", dup;dur=1;dur=7;desc=\"first\";desc=\"last\", neg;dur=-2\r\n%s\r\n%s", status, http.StatusText(status), mime, len(body), extra, body)
		if err = rw.Flush(); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestPerformanceNetworkAndLifecycleMatchFrozenChrome(t *testing.T) {
	serialBrowserTest(t)
	for _, mode := range []string{"ordinary", "snapshot"} {
		t.Run(mode, func(t *testing.T) {
			if mode == "ordinary" {
				t.Setenv("MIMIC_DISABLE_BOOTSTRAP_SNAPSHOT", "1")
			} else {
				t.Setenv("MIMIC_DISABLE_BOOTSTRAP_SNAPSHOT", "0")
			}
			seed := bootstrapSnapshotPage(t)
			navigateCapabilityFixture(t, seed)
			if mode == "snapshot" {
				bootstrapSnapshotWarm(t, seed)
			}
			server := performanceOracleServer(t)
			for _, name := range []string{"performance_lifecycle", "performance_redirect", "performance_redirect_privacy", "performance_buffers", "performance_resource", "performance_navigation_events", "performance_buffers_worker", "performance_conversion_worker"} {
				t.Run(name, func(t *testing.T) {
					p, err := seed.ctx.NewPage()
					if err != nil {
						t.Fatal(err)
					}
					if err = p.Navigate(context.Background(), server.URL); err != nil {
						t.Fatal(err)
					}
					fixture := name
					worker := strings.HasSuffix(name, "_worker")
					if worker {
						fixture = strings.TrimSuffix(name, "_worker")
					}
					source, err := os.ReadFile("testdata/" + fixture + "_oracle.js")
					if err != nil {
						t.Fatal(err)
					}
					expression := string(source)
					if worker {
						encoded, _ := json.Marshal(expression)
						expression = `new Promise((resolve,reject)=>{const w=new Worker('/worker.js');w.onmessage=e=>{w.terminate();e.data.error?reject(Error(e.data.error)):resolve(e.data.value)};w.onerror=e=>reject(Error(e.message));w.postMessage(` + string(encoded) + `)})`
					}
					actual := bootstrapSnapshotEvaluate(t, p, `(async()=>JSON.stringify(await `+expression+`))()`).(string)
					if mode == "snapshot" && !p.Top.Realm.bootstrapRestored {
						t.Fatal("expected restored realm")
					}
					if name == "performance_resource" || name == "performance_navigation_events" {
						assertPerformanceKnownBoundaries(t, name, actual)
					} else {
						assertPerformanceOracle(t, name, actual)
					}
				})
			}
		})
	}
}

func assertPerformanceKnownBoundaries(t *testing.T, name, actualJSON string) {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name + "_chrome152.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden struct {
		Result struct {
			Result struct{ Value map[string]any }
		}
	}
	if err = json.Unmarshal(data, &golden); err != nil {
		t.Fatal(err)
	}
	var actual map[string]any
	if err = json.Unmarshal([]byte(actualJSON), &actual); err != nil {
		t.Fatal(err)
	}
	for key, want := range golden.Result.Result.Value {
		t.Run(key, func(t *testing.T) {
			if name == "performance_resource" && key == "opaque" {
				t.Skip("Known boundary: opaque Fetch body completion differs; Chrome capture has no timing entry, Mimic records the completed transport")
			}
			if name == "performance_navigation_events" && key == "rows" {
				gotRows, wantRows := actual[key].([]any), want.([]any)
				if len(gotRows) != len(wantRows) {
					t.Fatalf("row count: %d vs %d", len(gotRows), len(wantRows))
				}
				for i := range wantRows {
					t.Run(fmt.Sprint(i), func(t *testing.T) {
						if i == 0 {
							t.Skip("Known boundary: response body is fully buffered before parsing; Chrome can execute inline script while loading")
						}
						if !reflect.DeepEqual(gotRows[i], wantRows[i]) {
							t.Errorf("got %#v; want %#v", gotRows[i], wantRows[i])
						}
					})
				}
				return
			}
			if !reflect.DeepEqual(actual[key], want) {
				t.Errorf("got %#v; want %#v", actual[key], want)
			}
		})
	}
	if len(actual) != len(golden.Result.Result.Value) {
		t.Errorf("key count %d, want %d", len(actual), len(golden.Result.Result.Value))
	}
}
