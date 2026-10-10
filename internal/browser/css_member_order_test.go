package browser

import (
	"os"
	"testing"
)

func TestCSSStyleDeclarationMemberOrderAcrossSecurityContexts(t *testing.T) {
	parallelBrowserTest(t)
	source, err := os.ReadFile("testdata/css_member_order.js")
	if err != nil {
		t.Fatal(err)
	}
	historyTestPages(t, func(t *testing.T, page *Page) {
		observe := string(source)
		historyEval(t, page, observe, true)
		navigateCapabilityFixture(t, page)
		historyEval(t, page, observe, true)
		navigateCapabilityFixtureMode(t, page, true)
		historyEval(t, page, observe, true)
	})
}
