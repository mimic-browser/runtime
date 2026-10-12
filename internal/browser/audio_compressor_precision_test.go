//go:build (windows || linux) && amd64

package browser

import "testing"

// A buffer source isolates compressor arithmetic from the portable oscillator
// transform. Every PCM sample and observable state is compared exactly.
func TestAudioCompressorPrecisionMatchesFrozenChrome(t *testing.T) {
	parallelBrowserTest(t)

	documentAllOracle(t, "audio_compressor_precision")
}

func TestAudioFFTPrecisionMatchesFrozenChrome(t *testing.T) {
	parallelBrowserTest(t)
	documentAllOracle(t, "audio_fft_precision")
}

func TestAudioAnalyserPrecisionMatchesFrozenChrome(t *testing.T) {
	parallelBrowserTest(t)
	documentAllOracle(t, "audio_analyser_precision")
}
