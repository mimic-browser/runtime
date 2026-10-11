//go:build (windows || linux) && amd64

package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	chrome152 "github.com/moreveal/mimic/chrome/152"
	v8engine "github.com/moreveal/mimic/internal/engine/v8"
	"github.com/moreveal/mimic/internal/profile"
)

func TestPluginGraphMatchesFrozenChrome(t *testing.T) {
	parallelBrowserTest(t)
	documentAllOracle(t, "plugin_graph")
}

// Generated profiles vary graphics/fonts but share the legacy PDF capability.
// Exercise every distinct recipe selected by the deterministic generator corpus.
func TestPluginGraphGeneratedProfiles(t *testing.T) {
	serialBrowserTest(t)
	b, err := New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	source, err := os.ReadFile("testdata/plugin_graph_oracle.js")
	if err != nil {
		t.Fatal(err)
	}
	c := b.NewContext()
	p, err := c.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	expected, err := p.Evaluate(context.Background(), "JSON.stringify("+string(source)+")")
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		d, _, err := profile.Generate([]byte(fmt.Sprintf(`{"seed":"plugin-graph-%d"}`, i)), b.Environment())
		if err != nil {
			t.Fatal(err)
		}
		key, err := json.Marshal([]any{d.Graphics, d.Fonts})
		if err != nil {
			t.Fatal(err)
		}
		if seen[string(key)] {
			continue
		}
		seen[string(key)] = true
		c, err := b.NewResolvedProfileContext(d, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		p, err := c.NewPage()
		if err != nil {
			c.Close()
			t.Fatal(err)
		}
		actual, err := p.Evaluate(context.Background(), "JSON.stringify("+string(source)+")")
		c.Close()
		if err != nil || actual != expected {
			t.Fatalf("seed %d: graph differs: %v", i, err)
		}
	}
	if len(seen) != 48 {
		t.Fatalf("covered %d recipes, want 47 generated plus canonical", len(seen))
	}
}
