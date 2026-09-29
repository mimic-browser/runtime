package font_test

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/go-text/typesetting/di"
	"github.com/go-text/typesetting/font"
	ot "github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/language"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/math/fixed"
)

func TestMetricsOnlyHorizontalShaping(t *testing.T) {
	makeFace := func(compact bool) *font.Face {
		t.Helper()
		loader, err := ot.NewLoader(bytes.NewReader(goregular.TTF))
		if err != nil {
			t.Fatal(err)
		}
		var parsed *font.Font
		if compact {
			parsed, err = font.NewFontMetricsOnly(loader)
		} else {
			parsed, err = font.NewFont(loader)
		}
		if err != nil {
			t.Fatal(err)
		}
		return font.NewFace(parsed)
	}
	text := []rune("office fiancé—Wikipedia 2026")
	shape := func(face *font.Face) shaping.Output {
		input := shaping.Input{
			Text: text, RunStart: 0, RunEnd: len(text),
			Direction: di.DirectionLTR, Face: face,
			Size: fixed.I(16), Script: language.Latin,
			Language: language.NewLanguage("en"),
		}
		return (&shaping.HarfbuzzShaper{}).Shape(input)
	}
	full, compact := shape(makeFace(false)), shape(makeFace(true))
	if !reflect.DeepEqual(compact.Glyphs, full.Glyphs) || compact.Advance != full.Advance {
		t.Fatalf("horizontal shaping differs: full=%+v compact=%+v", full.Glyphs, compact.Glyphs)
	}
}
