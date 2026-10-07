package videocodec

import (
	"image"
	"image/color"
	"testing"
)

func TestBundledH264RoundTrip(t *testing.T) {
	e, err := New(64, 32, 30)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	d, err := NewDecoder()
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	src := image.NewRGBA(image.Rect(0, 0, 64, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 64; x++ {
			src.SetRGBA(x, y, color.RGBA{R: 220, G: 40, B: 30, A: 255})
		}
	}
	data, err := e.Encode(src, true)
	if err != nil {
		t.Fatal(err)
	}
	frame, err := d.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if frame == nil || frame.Bounds() != src.Bounds() {
		t.Fatalf("decoded frame: %v", frame)
	}
	c := frame.RGBAAt(32, 16)
	if c.R < 190 || c.G > 65 || c.B > 60 || c.A != 255 {
		t.Fatalf("decoded pixel: %v", c)
	}
	e.Close()
	d.Close()
	if _, err = e.Encode(src, false); err == nil {
		t.Fatal("closed encoder accepted a frame")
	}
	if _, err = d.Decode(data); err == nil {
		t.Fatal("closed decoder accepted a frame")
	}
}
