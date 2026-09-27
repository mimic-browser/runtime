package browser

import "testing"

func TestHTMLElementDirMatchesFrozenChrome(t *testing.T) {
	parallelBrowserTest(t)
	documentAllOracle(t, "html_dir")
}
