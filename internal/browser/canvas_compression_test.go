package browser

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/base64"
	"encoding/binary"
	"image/png"
	"io"
	"math"
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
	var idat bytes.Buffer
	var idatSizes []int
	for offset := 8; offset+12 <= len(data); {
		size := int(binary.BigEndian.Uint32(data[offset:]))
		if offset+12+size > len(data) {
			t.Fatal("truncated PNG chunk")
		}
		if string(data[offset+4:offset+8]) == "IDAT" {
			idatSizes = append(idatSizes, size)
			idat.Write(data[offset+8 : offset+8+size])
		}
		offset += 12 + size
	}
	if len(idatSizes) < 2 || idatSizes[len(idatSizes)-1] != 6 {
		t.Fatalf("frozen Chrome 152 IDAT chunk sizes = %v, want final six-byte trailer", idatSizes)
	}
	for _, size := range idatSizes[:len(idatSizes)-1] {
		if size < 1 || size > 4096 {
			t.Fatalf("frozen Chrome 152 IDAT chunk size = %d, want 1..4096", size)
		}
	}
	reader, err := zlib.NewReader(&idat)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	scanlines, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	for row := 0; row < 150; row++ {
		if filter := scanlines[row*(300*4+1)]; filter != 2 {
			t.Fatalf("frozen Chrome 152 PNG row %d filter = %d, want Up (2)", row, filter)
		}
	}
}

func TestCanvasBlurMaskCropsWithoutChangingConvolution(t *testing.T) {
	const width, height = 9, 7
	source := make([]byte, width*height*4)
	for i := 0; i < width*height; i++ {
		value := float32((i%11)+1) / 13
		binary.LittleEndian.PutUint32(source[i*4:], math.Float32bits(value))
	}
	weights := []float64{0.4, 0.2, 0.1}
	weightBytes := make([]byte, len(weights)*8)
	for i, value := range weights {
		binary.LittleEndian.PutUint64(weightBytes[i*8:], math.Float64bits(value))
	}
	full, err := blurCanvasMask(source, width, height, weightBytes, 0, 0, width, height)
	if err != nil {
		t.Fatal(err)
	}
	cropped, err := blurCanvasMask(source, width, height, weightBytes, 2, 1, 4, 3)
	if err != nil {
		t.Fatal(err)
	}
	for y := 0; y < 3; y++ {
		for x := 0; x < 4; x++ {
			got := binary.LittleEndian.Uint32(cropped[(y*4+x)*4:])
			want := binary.LittleEndian.Uint32(full[((y+1)*width+x+2)*4:])
			if got != want {
				t.Fatalf("blur pixel (%d,%d) = %08x, want %08x", x, y, got, want)
			}
		}
	}
}
