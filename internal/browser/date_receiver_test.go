package browser

import "testing"

func TestDateReceiverMatchesFrozenChrome(t *testing.T) {
	parallelBrowserTest(t)
	documentAllOracle(t, "date_receiver")
}
