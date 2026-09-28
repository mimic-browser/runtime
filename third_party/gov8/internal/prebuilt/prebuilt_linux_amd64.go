//go:build linux && amd64

package prebuilt

import _ "embed"

const (
	ABI      = 44
	Size     = int64(57309264)
	SHA256   = "0845a289130d570d60889443e16b1289bd1adadb725c18ecd979561056269e7a"
	fileName = "libgov8_shim.so"
)

//go:embed linux_amd64/libgov8_shim.so.gz
var compressed []byte
