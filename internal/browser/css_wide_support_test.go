package browser

import (
	"context"
	"os"
	"strconv"
	"testing"
)

func TestCSSWideValuesShareDeclarationAndSupportsSemantics(t *testing.T) {
	parallelBrowserTest(t)
	source, err := os.ReadFile("testdata/css_wide_support.js")
	if err != nil {
		t.Fatal(err)
	}
	historyTestPages(t, func(t *testing.T, page *Page) {
		got, err := page.Evaluate(context.Background(), "JSON.stringify(eval("+strconv.Quote(string(source))+"))")
		want := `{"rows":[true,true,true,true,true,true],"conditional":true,"priority":[true,true,false,false,false],"invalid":[false,false,false,false]}`
		if err != nil || got != want {
			t.Fatalf("CSS-wide values: got %v, %v; want %s", got, err, want)
		}
	})
}
