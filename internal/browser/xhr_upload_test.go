package browser

import "testing"

func TestXHRUploadMatchesFrozenChrome(t *testing.T) {
	parallelBrowserTest(t)
	documentAllOracle(t, "xhr_upload")
}
