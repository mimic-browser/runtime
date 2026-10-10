package browser

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestCanvasCoverageAccelerationPreservesPredicates(t *testing.T) {
	parallelBrowserTest(t)
	source, err := os.ReadFile("../webapi/canvas_path_observations.js")
	if err != nil {
		t.Fatal(err)
	}
	prefix, _, found := strings.Cut(string(source), "const materializePath =")
	if !found {
		t.Fatal("canvas coverage source boundary missing")
	}
	fixture, err := os.ReadFile("testdata/canvas_coverage_acceleration.js")
	if err != nil {
		t.Fatal(err)
	}
	p := blitzStandardsPage(t)
	value, err := p.Evaluate(context.Background(), "(()=>{\n"+prefix+"\n"+string(fixture)+"\n})()")
	if err != nil || value != "ok" {
		t.Fatalf("canvas coverage acceleration: %v %v", value, err)
	}
}
