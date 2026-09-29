package tables

import (
	"strings"
	"testing"
)

func TestParseGlyfMetricsOnlyKeepsFullValidation(t *testing.T) {
	// One simple contour with its end point and zero instructions, but no
	// point flags or coordinates: both parsers must reject the same glyph.
	truncated := []byte{
		0, 1, // number of contours
		0, 0, 0, 0, 0, 1, 0, 1, // bounds
		0, 0, // end point index
		0, 0, // instruction length
	}
	loca := []uint32{0, uint32(len(truncated))}
	_, fullErr := ParseGlyf(truncated, loca)
	_, compactErr := ParseGlyfMetricsOnly(truncated, loca)
	if fullErr == nil || compactErr == nil || fullErr.Error() != compactErr.Error() {
		t.Fatalf("validation diverged: full=%v compact=%v", fullErr, compactErr)
	}
	if !strings.Contains(compactErr.Error(), "flags") {
		t.Fatalf("unexpected validation failure: %v", compactErr)
	}
}
