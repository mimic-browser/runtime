//go:build linux && amd64

package prebuilt

import _ "embed"

const (
	ABI      = 45
	Size     = int64(59013280)
	SHA256   = "b2ef45b0a84d94c4b7772fecbbe035ec1858cf54f893bc52bb7eede5d4be6b1b"
	fileName = "libgov8_shim.so"
)

//go:embed linux_amd64/libgov8_shim.so.gz
var compressed []byte
