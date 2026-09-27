//go:build (windows || linux) && amd64

package browser

import "testing"

func TestObservationIntegrityMatchesFrozenChrome(t *testing.T) {
	parallelBrowserTest(t)
	documentAllOracle(t, "observation_integrity")
}
