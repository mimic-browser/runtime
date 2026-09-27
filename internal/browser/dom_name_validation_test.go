package browser

import (
	"context"
	_ "embed"
	"testing"
)

//go:embed testdata/dom_name_validation.js
var domNameValidationProbe string

func TestDOMNameValidationMatchesFrozenChrome(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		got, err := p.Evaluate(context.Background(), domNameValidationProbe)
		if err != nil || got != "ok" {
			t.Fatalf("DOM name validation: %v %v", got, err)
		}
	})
}
