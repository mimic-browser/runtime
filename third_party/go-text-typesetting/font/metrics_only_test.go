package font

import (
	"bytes"
	"os"
	"reflect"
	"testing"

	ot "github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/font/opentype/tables"
	"golang.org/x/image/font/gofont/goregular"
)

func loadTestFont(t *testing.T, data []byte, metricsOnly bool) *Font {
	t.Helper()
	loader, err := ot.NewLoader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	var result *Font
	if metricsOnly {
		result, err = NewFontMetricsOnly(loader)
	} else {
		result, err = NewFont(loader)
	}
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestMetricsOnlyStaticHorizontalMetrics(t *testing.T) {
	full := loadTestFont(t, goregular.TTF, false)
	compact := loadTestFont(t, goregular.TTF, true)
	if !compact.metricsOnlyGlyf || len(compact.glyf) != len(full.glyf) {
		t.Fatalf("static font was not compacted: compact=%t glyphs=%d/%d", compact.metricsOnlyGlyf, len(compact.glyf), len(full.glyf))
	}
	fullFace, compactFace := NewFace(full), NewFace(compact)
	for gid := 0; gid < full.nGlyphs; gid++ {
		id := GID(gid)
		if got, want := compactFace.HorizontalAdvance(id), fullFace.HorizontalAdvance(id); got != want {
			t.Fatalf("glyph %d horizontal advance: got %v, want %v", gid, got, want)
		}
		got, gotOK := compactFace.GlyphExtents(id)
		want, wantOK := fullFace.GlyphExtents(id)
		if got != want || gotOK != wantOK {
			t.Fatalf("glyph %d extents: got %+v/%t, want %+v/%t", gid, got, gotOK, want, wantOK)
		}
		if _, simple := full.glyf[gid].Data.(tables.SimpleGlyph); simple {
			if compact.glyf[gid].Data != nil {
				t.Fatalf("glyph %d retained simple outline", gid)
			}
			// Headers must be preserved even when contours are dropped.
			if compact.glyf[gid].XMin != full.glyf[gid].XMin || compact.glyf[gid].YMax != full.glyf[gid].YMax {
				t.Fatalf("glyph %d header changed", gid)
			}
		}
	}
	if gid, ok := compact.NominalGlyph('A'); !ok {
		t.Fatal("missing A")
	} else if _, ok := compactFace.GlyphDataOutline(gID(gid)); ok {
		t.Fatal("compacted TrueType font must not return a misleading empty outline")
	}
}

func TestMetricsOnlyVariableRetainsOutlines(t *testing.T) {
	data, err := os.ReadFile("testdata/Selawik-VF-Subset.ttf")
	if err != nil {
		t.Fatal(err)
	}
	full := loadTestFont(t, data, false)
	compact := loadTestFont(t, data, true)
	if len(full.fvar) == 0 || compact.metricsOnlyGlyf {
		t.Fatalf("variable font was compacted: axes=%d compact=%t", len(full.fvar), compact.metricsOnlyGlyf)
	}
	if !reflect.DeepEqual(compact.glyf, full.glyf) {
		t.Fatal("variable glyph points or components differ")
	}
	fullFace, compactFace := NewFace(full), NewFace(compact)
	for _, r := range []rune{'A', 'é', 'Ж'} {
		gid, ok := full.NominalGlyph(r)
		if !ok {
			continue
		}
		if got, want := compactFace.HorizontalAdvance(gid), fullFace.HorizontalAdvance(gid); got != want {
			t.Fatalf("glyph %q variable advance: got %v, want %v", r, got, want)
		}
		got, gotOK := compactFace.GlyphExtents(gid)
		want, wantOK := fullFace.GlyphExtents(gid)
		if got != want || gotOK != wantOK {
			t.Fatalf("glyph %q variable extents: got %+v/%t, want %+v/%t", r, got, gotOK, want, wantOK)
		}
	}
}
