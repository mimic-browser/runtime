package browser

import (
	"context"
	"os"
	"strings"
	"testing"
)

// Frozen headful Chrome 152.0.7977.82, Windows, unmodified webdriver=false.
// Passive CDP evaluation on a local blank target; no application or network hooks.
func TestNodeEqualityMatchesFrozenChrome(t *testing.T) {
	parallelBrowserTest(t)
	probe, err := os.ReadFile("testdata/node_equality_oracle.js")
	if err != nil {
		t.Fatal(err)
	}
	expected, err := os.ReadFile("testdata/node_equality_chrome152.json")
	if err != nil {
		t.Fatal(err)
	}
	historyTestPages(t, func(t *testing.T, page *Page) {
		result, err := page.Evaluate(context.Background(), string(probe))
		if err != nil || result != strings.TrimSpace(string(expected)) {
			t.Fatalf("node equality: %v %v", result, err)
		}
	})
}
