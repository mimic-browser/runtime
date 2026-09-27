package browser

import "testing"

func TestXHREventHandlersMatchFrozenChrome(t *testing.T) {
	parallelBrowserTest(t)
	documentAllOracle(t, "xhr_event_handlers")
}
