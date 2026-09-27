package browser

import "testing"

func TestXHRConstantsMatchFrozenChrome(t *testing.T) {
	parallelBrowserTest(t)
	documentAllOracle(t, "xhr_constants")
}
