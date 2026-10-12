//go:build linux && amd64

package prebuilt

import _ "embed"

const (
	ABI      = 45
	Size     = int64(59009184)
	SHA256   = "1a1b716e400c4b2ef20b3d84bf8ddcc9d3b862117983d9891c8aec1146ef461d"
	fileName = "libgov8_shim.so"
)

//go:embed linux_amd64/libgov8_shim.so.gz
var compressed []byte
