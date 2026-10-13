//go:build linux && amd64

package prebuilt

import _ "embed"

const (
	ABI      = 45
	Size     = int64(57325656)
	SHA256   = "5c7cd8dc0305a6d64998a16b8630d42833496afe588b87c7d5f15ee41794f776"
	fileName = "libgov8_shim.so"
)

//go:embed linux_amd64/libgov8_shim.so.gz
var compressed []byte
