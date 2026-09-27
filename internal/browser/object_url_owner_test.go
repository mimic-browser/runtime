package browser

import "testing"

func TestObjectURLOwnerMatchesFrozenChrome(t *testing.T) {
	parallelBrowserTest(t)
	documentAllOracle(t, "object_url_owner")
}

func TestObjectURLWorkerOwnerMatchesFrozenChrome(t *testing.T) {
	parallelBrowserTest(t)
	documentAllOracle(t, "object_url_worker_owner")
}
