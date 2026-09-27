package browser

import "testing"

func TestDatasetPrototypeMatchesFrozenChrome(t *testing.T) {
	parallelBrowserTest(t)
	documentAllOracle(t, "dataset_prototype")
}
