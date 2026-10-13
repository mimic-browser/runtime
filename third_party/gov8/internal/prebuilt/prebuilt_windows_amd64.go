//go:build windows && amd64

package prebuilt

import _ "embed"

const (
	ABI      = 45
	Size     = int64(46471680)
	SHA256   = "2d01a9a5b61a78f602d0785addc9aae17a415cd0ea1ffec097dea141c7d2c042"
	fileName = "gov8_shim.dll"
)

//go:embed windows_amd64/gov8_shim.dll.gz
var compressed []byte
