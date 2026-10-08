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
