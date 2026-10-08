package renderer

import (
	"context"
	"image/color"
	"net/http"
	"testing"
)

func TestRenderUsesOnlySnapshotResources(t *testing.T) {
	files := map[string][]byte{
		"index.html":       []byte(`<!doctype html><html><head><link rel="stylesheet" href="assets/style.css"></head><body><div class="block"></div></body></html>`),
		"assets/style.css": []byte(`body{margin:0}.block{width:80px;height:60px;background-color:#f00}`),
	}
	image, err := Render(context.Background(), files, 120, 90)
	if err != nil {
		t.Fatal(err)
	}
	if image.Bounds().Dx() != 120 || image.Bounds().Dy() < 90 {
		t.Fatalf("unexpected image size: %v", image.Bounds())
	}
	if got := color.RGBAModel.Convert(image.At(20, 20)).(color.RGBA); got.R < 240 || got.G > 20 || got.B > 20 {
		t.Fatalf("snapshot stylesheet was not painted: %v", got)
	}
	request, _ := http.NewRequest("GET", "https://example.com/private", nil)
	if _, err := (snapshotTransport{files: files}).RoundTrip(request); err == nil {
		t.Fatal("renderer transport allowed an external request")
	}
}

func TestRenderResolvesStylesheetRelativeSVGMask(t *testing.T) {
	files := map[string][]byte{
		"index.html":       []byte(`<!doctype html><html><head><link rel="stylesheet" href="assets/style.css"></head><body><span class="icon"></span></body></html>`),
		"assets/style.css": []byte(`body{margin:0;background:white}.icon{display:block;width:20px;height:20px;background:#f00;mask-image:url(icon.svg)}`),
		"assets/icon.svg":  []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="20" height="20" viewBox="0 0 20 20"><rect width="10" height="20" fill="black"/></svg>`),
	}
	img, err := Render(context.Background(), files, 40, 30)
	if err != nil {
		t.Fatal(err)
	}
	left := color.RGBAModel.Convert(img.At(5, 10)).(color.RGBA)
	right := color.RGBAModel.Convert(img.At(15, 10)).(color.RGBA)
	if left.R < 200 || left.G > 50 || right.R < 200 || right.G < 200 || right.B < 200 {
		t.Fatalf("SVG mask did not stencil stylesheet background: left=%v right=%v", left, right)
	}
}

func TestRenderKeepsSupportedGridSidebars(t *testing.T) {
	files := map[string][]byte{
		"index.html": []byte(`<!doctype html><html><head><style>
		body{margin:0}.shell{display:grid;grid-template:'left center right' 20px / 20px 40px 20px}
		.left{grid-area:left;display:none;background:red}.center{grid-area:center;background:green}
		.right{grid-area:right;display:none;background:blue}
		@supports (display:grid){.left,.right{display:block}}
		</style></head><body><div class="shell"><div class="left"></div><div class="center"></div><div class="right"></div></div></body></html>`),
	}
	img, err := Render(context.Background(), files, 80, 30)
	if err != nil {
		t.Fatal(err)
	}
	for _, sample := range []struct {
		x    int
		want color.RGBA
	}{
		{10, color.RGBA{R: 255, A: 255}},
		{40, color.RGBA{G: 128, A: 255}},
		{70, color.RGBA{B: 255, A: 255}},
	} {
		got := color.RGBAModel.Convert(img.At(sample.x, 10)).(color.RGBA)
		if got != sample.want {
			t.Errorf("x=%d: got %v, want %v", sample.x, got, sample.want)
		}
	}
}
