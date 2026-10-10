package browser

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strconv"
	"testing"
)

func TestCSSTransformOriginWideKeywordsPreserveClientGeometry(t *testing.T) {
	parallelBrowserTest(t)
	source, err := os.ReadFile("testdata/css_transform_origin_wide.js")
	if err != nil {
		t.Fatal(err)
	}
	historyTestPages(t, func(t *testing.T, page *Page) {
		got, err := page.Evaluate(context.Background(), "JSON.stringify(eval("+strconv.Quote(string(source))+"))")
		if err != nil {
			t.Fatal(err)
		}
		var result map[string]struct {
			Computed []string  `json:"computed"`
			Rect     []float64 `json:"rect"`
			Client   []float64 `json:"client"`
		}
		if err := json.Unmarshal([]byte(got.(string)), &result); err != nil {
			t.Fatal(err)
		}
		// Frozen headful Chrome 152 local geometry probe, including Float32
		// rectangle projection. Explicit and CSS-wide origins share used values.
		want := []float64{-20.710678100585938, -20.710678100585938, 141.42135620117188, 141.42135620117188}
		for _, name := range []string{"unset", "initial", "explicit"} {
			row := result[name]
			if !reflect.DeepEqual(row.Rect, want) || !reflect.DeepEqual(row.Client, want) || !reflect.DeepEqual(row.Computed, []string{"matrix(0.707107, 0.707107, -0.707107, 0.707107, 0, 0)", "50px 50px", "absolute"}) {
				t.Errorf("%s: %+v", name, row)
			}
		}
	})
}
