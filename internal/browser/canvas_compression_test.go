package browser

import (
	"bytes"
	"context"
	"encoding/base64"
	"image/png"
	"strings"
	"testing"
)

func TestCanvasSerializationCompressesRepeatedPixels(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	value, err := p.Evaluate(context.Background(), `(()=>{
  const canvas=document.createElement('canvas');
  canvas.width=300;canvas.height=150;
  const context=canvas.getContext('2d');
  context.fillStyle='#183c78';context.fillRect(0,0,300,150);
  return canvas.toDataURL();
})()`)
	if err != nil {
		t.Fatal(err)
	}
	encoded, ok := value.(string)
	if !ok || !strings.HasPrefix(encoded, "data:image/png;base64,") {
		t.Fatalf("canvas data URL = %#v", value)
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(encoded, "data:image/png;base64,"))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) >= 10000 {
		t.Fatalf("repeated canvas pixels encoded as %d bytes", len(data))
	}
	image, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if image.Bounds().Dx() != 300 || image.Bounds().Dy() != 150 {
		t.Fatalf("decoded canvas bounds = %v", image.Bounds())
	}
	r, g, b, a := image.At(150, 75).RGBA()
	if r>>8 != 0x18 || g>>8 != 0x3c || b>>8 != 0x78 || a>>8 != 0xff {
		t.Fatalf("decoded canvas pixel = %#x %#x %#x %#x", r, g, b, a)
	}
}
