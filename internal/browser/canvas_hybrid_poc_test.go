package browser

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestCanvasHybridSpansMatchScalarCoverage(t *testing.T) {
	parallelBrowserTest(t)
	source, err := os.ReadFile("../webapi/canvas_path_observations.js")
	if err != nil {
		t.Fatal(err)
	}
	prefix, _, found := strings.Cut(string(source), "const rectanglePath =")
	if !found {
		t.Fatal("canvas source boundary missing")
	}
	colors, err := os.ReadFile("../webapi/canvas_color_spaces.js")
	if err != nil {
		t.Fatal(err)
	}
	prefix = string(colors) + "\n" + prefix
	fixture, err := os.ReadFile("testdata/canvas_hybrid_spans.js")
	if err != nil {
		t.Fatal(err)
	}
	p := blitzStandardsPage(t)
	value, err := p.Evaluate(context.Background(), "(()=>{\n"+prefix+"\n"+string(fixture)+"\n})()")
	if err != nil || value != "ok" {
		t.Fatalf("canvas spans: %v %v", value, err)
	}
}

func TestCanvasDecodedImageCacheTracksSourceAndSnapshots(t *testing.T) {
	serialBrowserTest(t)
	encode := func(width, height int, c color.NRGBA) []byte {
		img := image.NewNRGBA(image.Rect(0, 0, width, height))
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				img.SetNRGBA(x, y, c)
			}
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	red := encode(2, 1, color.NRGBA{R: 255, A: 255})
	tall := encode(1, 2, color.NRGBA{R: 255, A: 255})
	green := encode(2, 1, color.NRGBA{G: 255, A: 255})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		switch r.URL.Path {
		case "/red":
			w.Write(red)
		case "/tall":
			w.Write(tall)
		case "/green":
			w.Write(green)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	fixture, err := os.ReadFile("testdata/canvas_hybrid_images.js")
	if err != nil {
		t.Fatal(err)
	}
	historyTestPages(t, func(t *testing.T, p *Page) {
		navigateCapabilityFixture(t, p)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		value, err := p.Evaluate(ctx, "(async()=>{const base="+strconv.Quote(server.URL)+";\n"+string(fixture)+"\n})()")
		if err != nil || value != "ok" {
			t.Fatalf("canvas image cache: %v %v", value, err)
		}
	})
}
