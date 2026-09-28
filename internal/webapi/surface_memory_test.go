package webapi

import "testing"

func TestComposedSurfaceFitsOneByteV8String(t *testing.T) {
	// A single non-Latin-1 code point promotes the entire composed source to
	// a two-byte V8 string in every Page isolate.
	for _, char := range composeSurface("", "") {
		if char > 0xff {
			t.Fatalf("composed surface contains non-Latin-1 code point U+%04X", char)
		}
	}
}
