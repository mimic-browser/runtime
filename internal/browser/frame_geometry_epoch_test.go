package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	chrome152 "github.com/moreveal/mimic/chrome/152"
	v8engine "github.com/moreveal/mimic/internal/engine/v8"
)

func TestFrameGeometryEpochTracksAncestorChanges(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		ctx := context.Background()
		eval := func(source string) {
			t.Helper()
			got, err := p.Evaluate(ctx, source)
			if err != nil || got != true {
				t.Fatalf("frame observation: %v %v", got, err)
			}
		}
		source, err := os.ReadFile("testdata/frame_geometry_epoch.js")
		if err != nil {
			t.Fatal(err)
		}
		eval(string(source))
		for i := 0; i < 5; i++ {
			eval(fmt.Sprintf("frameGeometrySteps[%d]()", i))
		}

	})
}

func TestFrameGeometryReusesUnchangedObservations(t *testing.T) {
	serialBrowserTest(t)
	t.Setenv("MIMIC_PROFILE_HOSTS", "1")
	b, err := New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	c := b.NewContext()
	defer c.Close()
	p, err := c.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	source, err := os.ReadFile("testdata/frame_geometry_epoch.js")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Evaluate(ctx, string(source)); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Evaluate(ctx, "setupFrameGeometryReuse()"); err != nil {
		t.Fatal(err)
	}

	count := func() uint64 {
		data, err := json.Marshal(p.LiveDiagnostics())
		if err != nil {
			t.Fatal(err)
		}
		type costs struct {
			Costs map[string]struct{ Count uint64 }
		}
		var result struct {
			costs
			Realms []struct{ Diagnostics costs }
		}
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatal(err)
		}
		total := result.Costs["host:styleObservationState"].Count
		for _, realm := range result.Realms {
			total += realm.Diagnostics.Costs["host:styleObservationState"].Count
		}
		return total
	}
	read := func() {
		value, err := p.Evaluate(ctx, `frame.contentWindow.eval('document.querySelector("button").getBoundingClientRect().width')`)
		if err != nil || numberValue(value) != 100 {
			t.Fatalf("rect: %v %v", value, err)
		}
	}
	read()
	before := count()
	if before == 0 {
		t.Fatal("missing observation diagnostics")
	}
	for i := 0; i < 64; i++ {
		read()
	}
	if after := count(); after != before {
		t.Fatalf("unchanged iframe rebuilt canonical observations: %d -> %d", before, after)
	}
}
