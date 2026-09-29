//go:build linux && amd64

package prebuilt

import _ "embed"

const (
	ABI      = 44
	Size     = int64(59005080)
	SHA256   = "ea36c49f00e3ad723a29b1daf28b24472853cb6cdcb29a551d3ac908b02abf66"
	fileName = "libgov8_shim.so"
)

//go:embed linux_amd64/libgov8_shim.so.gz
var compressed []byte
