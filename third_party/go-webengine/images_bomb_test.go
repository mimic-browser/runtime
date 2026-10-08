// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package engine

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"testing"
)

// pngHeaderOnly is a PNG whose IHDR declares w×h but whose body is just the
// signature and that header: a few dozen bytes that claim a huge canvas.
func pngHeaderOnly(w, h uint32) []byte {
	var b bytes.Buffer
	b.Write([]byte("\x89PNG\r\n\x1a\n"))
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:4], w)
	binary.BigEndian.PutUint32(ihdr[4:8], h)
	ihdr[8], ihdr[9] = 8, 6 // 8-bit RGBA
	chunk := append([]byte("IHDR"), ihdr...)
	_ = binary.Write(&b, binary.BigEndian, uint32(len(ihdr)))
	b.Write(chunk)
	_ = binary.Write(&b, binary.BigEndian, crc32.ChecksumIEEE(chunk))
	return b.Bytes()
}

// A header declaring far more pixels than maxImagePixels is refused before any
// decoding. (A header alone does not allocate on the old path, which fails at
// the missing pixel data; the allocation risk needs a valid compressed stream,
// measured in FIDELITY.md round 161.)
func TestDecodeRasterRefusesOversizedDeclaredImage(t *testing.T) {
	_, err := decodeRaster(pngHeaderOnly(50000, 50000))
	if err != errImageTooLarge {
		t.Fatalf("decodeRaster(50000x50000 header) err = %v, want errImageTooLarge", err)
	}
}

// Ordinary images still decode.
func TestDecodeRasterDecodesOrdinaryImages(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 8, 4))
	img.Set(1, 1, color.NRGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	got, err := decodeRaster(buf.Bytes())
	if err != nil {
		t.Fatalf("decodeRaster(8x4 png) error: %v", err)
	}
	if b := got.Bounds(); b.Dx() != 8 || b.Dy() != 4 {
		t.Errorf("decoded bounds = %v, want 8x4", b)
	}
}
