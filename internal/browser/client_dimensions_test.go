package browser

import (
	"context"
	"os"
	"testing"
)

func TestClientDimensionsAvoidOrdinaryElementRootLookups(t *testing.T) {
	parallelBrowserTest(t)
	source, err := os.ReadFile("testdata/client_dimensions_root_lookups.js")
	if err != nil {
		t.Fatal(err)
	}
	historyTestPages(t, func(t *testing.T, p *Page) {
		navigateCapabilityFixture(t, p)
		got, err := p.Evaluate(context.Background(), string(source))
		if err != nil || got != true {
			t.Fatalf("ordinary element root lookups: %v %v", got, err)
		}
	})
}
