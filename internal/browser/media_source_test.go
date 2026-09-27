package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"

	"github.com/moreveal/mimic/internal/network"
)

func TestMediaElementBufferedMatchesFrozenChrome(t *testing.T) {
	parallelBrowserTest(t)
	documentAllOracle(t, "media_element_buffered")
}

func TestMediaElementPlayedMatchesFrozenChrome(t *testing.T) {
	parallelBrowserTest(t)
	documentAllOracle(t, "media_element_played")
}

func TestMediaElementSeekableMatchesFrozenChrome(t *testing.T) {
	parallelBrowserTest(t)
	documentAllOracle(t, "media_element_seekable")
}

func TestMediaSourceSeekableMatchesFrozenChrome(t *testing.T) {
	parallelBrowserTest(t)
	documentAllOracle(t, "media_source_seekable")
}

func TestMediaSourceSeekableBoundsMatchesFrozenChrome(t *testing.T) {
	parallelBrowserTest(t)
	documentAllOracle(t, "media_source_seekable_bounds")
}

func TestMediaSourceClosedMatchesFrozenChrome(t *testing.T) {
	parallelBrowserTest(t)
	documentAllOracle(t, "media_source_closed")
}

func TestMediaSourceAttachmentMatchesFrozenChrome(t *testing.T) {
	parallelBrowserTest(t)
	documentAllOracle(t, "media_source_attachment")
}

func TestMediaSourceLifecycleMatchesFrozenChrome(t *testing.T) {
	parallelBrowserTest(t)
	documentAllOracle(t, "media_source_lifecycle")
}

func TestMediaSourceBufferEdgesMatchesFrozenChrome(t *testing.T) {
	parallelBrowserTest(t)
	documentAllOracle(t, "media_source_buffer_edges")
}

func TestMediaSourceMP4InitializationMatchesFrozenChrome(t *testing.T) {
	parallelBrowserTest(t)
	documentAllOracle(t, "media_source_mp4_init")
}

func TestMediaSourceMP4MetadataLifetimeMatchesFrozenChrome(t *testing.T) {
	parallelBrowserTest(t)
	documentAllOracle(t, "media_source_mp4_metadata_lifetime")
}

func TestMediaSourceMP4AudioInitializationMatchesFrozenChrome(t *testing.T) {
	parallelBrowserTest(t)
	documentAllOracle(t, "media_source_mp4_audio_init")
}

func TestMediaSourceMP4DimensionsMatchesFrozenChrome(t *testing.T) {
	parallelBrowserTest(t)
	for _, fixture := range []string{"media_source_mp4_dimensions", "media_source_mp4_sample_dimensions", "media_source_mp4_track_dimensions"} {
		t.Run(fixture, func(t *testing.T) { documentAllOracle(t, fixture) })
	}
}

func TestMediaSourceMP4InitializationResourcePolicy(t *testing.T) {
	parallelBrowserTest(t)
	page := bootstrapSnapshotPage(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><link rel="icon" href="data:,"><body></body>`)
	}))
	t.Cleanup(server.Close)
	if err := page.Navigate(context.Background(), server.URL); err != nil {
		t.Fatal(err)
	}
	bootstrapSnapshotEvaluate(t, page, "true")
	falseValue := false
	policy := network.ResourcePolicy{Rules: []network.ResourceRule{{ID: "media-cost", Match: network.ResourceMatch{Kinds: []string{"media"}}, Work: network.ResourceWork{Network: &falseValue, Body: "none", Decode: &falseValue, CacheRetain: &falseValue, DebugRetain: &falseValue}}}}
	if _, err := page.ctx.UpdateResourcePolicy(policy); err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile("testdata/media_source_mp4_init_oracle.js")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("testdata/media_source_mp4_init_chrome152.json")
	if err != nil {
		t.Fatal(err)
	}
	var oracle struct {
		Result struct {
			Result struct{ Value map[string]any }
		}
	}
	if err := json.Unmarshal(data, &oracle); err != nil {
		t.Fatal(err)
	}
	before := page.ctx.ResourcePolicyStats()
	value := bootstrapSnapshotEvaluate(t, page, "(async()=>JSON.stringify(await "+string(source)+"))()")
	text, ok := value.(string)
	if !ok {
		t.Fatalf("expected JSON result, got %T", value)
	}
	var actual map[string]any
	if err := json.Unmarshal([]byte(text), &actual); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, oracle.Result.Result.Value) {
		t.Fatalf("policy changed initialization observations: %+v", actual)
	}
	after := page.ctx.ResourcePolicyStats()
	if after.NetworkAcquisitions != before.NetworkAcquisitions || after.EncodedNetworkBodyBytes != before.EncodedNetworkBodyBytes || after.RetainedBodyBytes != before.RetainedBodyBytes || after.DecodedPixelWorkBytes != before.DecodedPixelWorkBytes {
		t.Fatalf("initialization incurred network, body, retention or pixel work: before=%+v after=%+v", before, after)
	}
}

func TestMediaSourceAttachmentResourcePolicy(t *testing.T) {
	parallelBrowserTest(t)
	falseValue := false
	for _, test := range []struct {
		name   string
		policy network.ResourcePolicy
		opened bool
	}{
		{name: "blocked", policy: network.ResourcePolicy{Presets: []string{"noVisualAssets"}}},
		{name: "report-only", policy: network.ResourcePolicy{Presets: []string{"noVisualAssets"}, ReportOnly: true}, opened: true},
		{name: "no-body-work", policy: network.ResourcePolicy{Rules: []network.ResourceRule{{ID: "media-cost", Match: network.ResourceMatch{Kinds: []string{"media"}}, Work: network.ResourceWork{Network: &falseValue, Body: "none", Decode: &falseValue, CacheRetain: &falseValue, DebugRetain: &falseValue}}}}, opened: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			page := bootstrapSnapshotPage(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				fmt.Fprint(w, `<!doctype html><link rel="icon" href="data:,"><body></body>`)
			}))
			t.Cleanup(server.Close)
			if err := page.Navigate(context.Background(), server.URL); err != nil {
				t.Fatal(err)
			}
			bootstrapSnapshotEvaluate(t, page, "true")
			if _, err := page.ctx.UpdateResourcePolicy(test.policy); err != nil {
				t.Fatal(err)
			}
			before := page.ctx.ResourcePolicyStats()
			opened := bootstrapSnapshotEvaluate(t, page, `(async () => {
  const source = new MediaSource();
  const video = document.createElement('video');
  const url = URL.createObjectURL(source);
  const opened = await new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(Error('attachment did not settle')), 1000);
    const finish = value => { clearTimeout(timer); resolve(value); };
    source.addEventListener('sourceopen', () => finish(true), {once:true});
    video.addEventListener('error', () => finish(false), {once:true});
    video.src = url;
    video.load();
  });
  URL.revokeObjectURL(url);
  video.removeAttribute('src');
  video.load();
  return opened;
})()`)
			if opened != test.opened {
				t.Fatalf("opened=%v, want %v", opened, test.opened)
			}
			after := page.ctx.ResourcePolicyStats()
			if after.NetworkAcquisitions != before.NetworkAcquisitions || after.EncodedNetworkBodyBytes != before.EncodedNetworkBodyBytes || after.RetainedBodyBytes != before.RetainedBodyBytes || after.DecodedPixelWorkBytes != before.DecodedPixelWorkBytes {
				t.Fatalf("attachment incurred body, transport or pixel work: before=%+v after=%+v", before, after)
			}
			if after.Requests-before.Requests != 1 {
				t.Fatalf("attachment did not evaluate exactly one policy decision: before=%+v after=%+v", before, after)
			}
			if test.policy.ReportOnly && after.WouldBlock-before.WouldBlock != 1 {
				t.Fatal("report-only omitted the attachment decision")
			}
		})
	}
}
