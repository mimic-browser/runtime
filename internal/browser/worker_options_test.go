//go:build (windows || linux) && amd64

package browser

import "testing"

func TestWorkerOptionsMatchFrozenChrome(t *testing.T) {
	serialBrowserTest(t)
	documentAllOracle(t, "worker_options")
}
